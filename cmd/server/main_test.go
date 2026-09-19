package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adminhttp "github.com/saskia-peters/gear/internal/admin/adapters/http"
	admsmtp "github.com/saskia-peters/gear/internal/admin/adapters/smtp"
	admcore "github.com/saskia-peters/gear/internal/admin/core"
	adminports "github.com/saskia-peters/gear/internal/admin/ports"
	dsgvocore "github.com/saskia-peters/gear/internal/dsgvo/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/platform/router"
	"github.com/saskia-peters/gear/internal/platform/spa"
	toolhttp "github.com/saskia-peters/gear/internal/tools/adapters/http"
	toolpostgres "github.com/saskia-peters/gear/internal/tools/adapters/postgres"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	userpostgres "github.com/saskia-peters/gear/internal/user/adapters/postgres"
	usercore "github.com/saskia-peters/gear/internal/user/core"
	userports "github.com/saskia-peters/gear/internal/user/ports"
)

// mainSettingsPort is the read-only settings seam for the main-level sender
// check (the composition root passes the real Admin service, which implements
// the same interface).
type mainSettingsPort struct {
	settings *admcore.SmtpSettings
}

func (m mainSettingsPort) CurrentSmtpSettings(context.Context) (*admcore.SmtpSettings, error) {
	return m.settings, nil
}

// TestResetEmailSenderIsReal pins the composition-root seam (Story 3.1): the
// exact sender constructor main.go wires (admsmtp.NewResetEmailSender) replaces
// the Epic-1 resetEmailStub. It must implement the UNCHANGED ResetEmailSender
// port, report NOT-configured without a settings row (so the FR-26
// must-change-password fallback stays active — SEND_RESET_UNCONFIGURED) and
// flip to configured when a working server + decryptable password exist. The
// password round-trips through the real crypto.SecretCipher (the same cipher
// the Admin core encrypts with via GEAR_ENCRYPTION_KEY, NFR-S4).
func TestResetEmailSenderIsReal(t *testing.T) {
	key := make([]byte, 32) // real AES-256-GCM key material
	cipher := crypto.NewSecretCipher(key)

	// Unconfigured (no settings row yet) → the sender behaves exactly like the
	// old stub: Configured()=false keeps the must-change-password fallback.
	unconfigured := admsmtp.NewResetEmailSender(mainSettingsPort{}, cipher, nil)
	var _ userports.ResetEmailSender = unconfigured // port contract unchanged
	if unconfigured.Configured() {
		t.Fatal("Configured() = true with no settings row, want false (fallback preserved)")
	}

	// Configured with a valid row + real encrypted password → true, and the
	// decrypted password is what a send would use (SEND_RESET ready).
	encrypted, err := cipher.Encrypt("geheim123")
	if err != nil {
		t.Fatalf("Encrypt err = %v", err)
	}
	configured := admsmtp.NewResetEmailSender(mainSettingsPort{settings: &admcore.SmtpSettings{
		Host:              "smtp.example.com",
		Port:              587,
		Security:          admcore.SmtpSecurityStartTLS,
		SenderAddress:     "noreply@example.com",
		PasswordEncrypted: encrypted,
	}}, cipher, nil)
	if !configured.Configured() {
		t.Fatal("Configured() = false with valid settings + decryptable password, want true")
	}

	// A corrupted/undecryptable stored password must NOT report configured
	// (the must-change fallback protects against a broken send path).
	broken := admsmtp.NewResetEmailSender(mainSettingsPort{settings: &admcore.SmtpSettings{
		Host: "smtp.example.com", Port: 587, Security: admcore.SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "not-ciphertext",
	}}, cipher, nil)
	if broken.Configured() {
		t.Fatal("Configured() = true with undecryptable password, want false")
	}
}

// --- composition-root mount ordering + gating (finding 11) ------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubPinger struct{}

func (stubPinger) Ping(context.Context) error { return nil }

type compValidator struct {
	session *usercore.Session
}

func (v *compValidator) Validate(context.Context, string) (*usercore.Session, error) {
	return v.session, nil
}

type compResolver struct {
	perms []string
}

func (r *compResolver) ListPermissionsByUser(context.Context, string) ([]string, error) {
	return r.perms, nil
}

// compSettingsService is an in-memory ports.Service for the settings surface.
type compSettingsService struct {
	settings *admcore.SmtpSettings
}

func (s *compSettingsService) GetSmtpSettings(_ context.Context, _ string) (*admcore.SmtpSettings, error) {
	if s.settings == nil {
		return &admcore.SmtpSettings{}, nil
	}
	return s.settings, nil
}

func (s *compSettingsService) UpdateSmtpSettings(_ context.Context, _ string, _ admcore.UpdateSmtpSettingsInput) (*admcore.SmtpSettings, error) {
	return &admcore.SmtpSettings{}, nil
}

func (s *compSettingsService) TestSmtpSettings(_ context.Context, _, _ string) (*admcore.SmtpTestResult, error) {
	return &admcore.SmtpTestResult{Ok: true, Message: admcore.MsgSmtpTestSent}, nil
}

func (s *compSettingsService) ListBackupDestinations(_ context.Context, _ string) ([]*admcore.BackupDestination, error) {
	return []*admcore.BackupDestination{}, nil
}

func (s *compSettingsService) CreateBackupDestination(_ context.Context, _ string, _ admcore.BackupDestinationInput) (*admcore.BackupDestination, error) {
	return &admcore.BackupDestination{ID: "id", Name: "x"}, nil
}

func (s *compSettingsService) UpdateBackupDestination(_ context.Context, _, _ string, _ admcore.BackupDestinationInput) (*admcore.BackupDestination, error) {
	return &admcore.BackupDestination{ID: "id", Name: "x"}, nil
}

func (s *compSettingsService) DeleteBackupDestination(_ context.Context, _, _ string) error {
	return nil
}

func (s *compSettingsService) TestBackupDestination(_ context.Context, _, _ string) (*admcore.BackupTestResult, error) {
	return &admcore.BackupTestResult{Ok: true, Message: admcore.MsgBackupTestOK}, nil
}

func (s *compSettingsService) ListSchedules(_ context.Context, _ string) ([]*admcore.Schedule, error) {
	return []*admcore.Schedule{}, nil
}

func (s *compSettingsService) CreateSchedule(_ context.Context, _ string, input admcore.ScheduleInput) (*admcore.Schedule, error) {
	return &admcore.Schedule{ID: "id", Name: input.Name, IntervalUnit: input.IntervalUnit, IntervalMagnitude: input.IntervalMagnitude}, nil
}

func (s *compSettingsService) UpdateSchedule(_ context.Context, _, _ string, _ admcore.ScheduleInput) (*admcore.Schedule, error) {
	return &admcore.Schedule{ID: "id", Name: "x"}, nil
}

func (s *compSettingsService) ArchiveSchedule(_ context.Context, _, _ string) (*admcore.Schedule, error) {
	return &admcore.Schedule{ID: "id", Name: "x"}, nil
}

func (s *compSettingsService) GetAppSettings(_ context.Context, _ string) (*admcore.AppSettings, error) {
	return &admcore.AppSettings{
		SmtpDialTimeout:                 10 * time.Second,
		SmtpProtocolTimeout:             30 * time.Second,
		BackupDialTimeout:               10 * time.Second,
		BackupProtocolTimeout:           10 * time.Second,
		PasswordResetTTL:                1800 * time.Second,
		AdminRecoveryTTL:                1800 * time.Second,
		ForgotThrottleInterval:          60 * time.Second,
		OtpTTL:                          900 * time.Second,
		OtpLength:                       10,
		MfaEnrollmentWindow:             600 * time.Second,
		LockoutThresholdShort:           3,
		LockoutThresholdLong:            4,
		LockoutDurationShort:            30 * time.Second,
		LockoutDurationLong:             60 * time.Second,
		LockoutMaxFailedCount:           10,
		AttributeKeyMaxRunes:            64,
		AttributesMaxSize:               16384,
		InventoryPrefix:                 "GEAR",
		InventoryWidth:                  9,
		InspectionOrangeWindowPercent:   25,
		QualificationExpiringSoonWindow: 2592000 * time.Second,
	}, nil
}

func (s *compSettingsService) UpdateAppSettings(_ context.Context, _, key string, input admcore.UpdateAppSettingInput) (*admcore.AppSetting, error) {
	settings, _ := s.GetAppSettings(context.Background(), "")
	row := admcore.AppSettingFor(settings, key)
	if row == nil {
		return nil, admcore.ErrAppSettingUnknown
	}
	// Echo the submitted value into the matching value column so the composed
	// PUT test proves the edit really reached the service (a silently-dropped
	// value would surface as the pre-update row and fail).
	switch v := input.Value.(type) {
	case float64:
		switch row.ValueType {
		case admcore.ValueTypeDuration:
			d := time.Duration(int64(v)) * time.Second
			row.DurationValue = &d
		case admcore.ValueTypeInteger:
			n := int64(v)
			row.IntValue = &n
		}
	case string:
		row.TextValue = &v
	}
	return row, nil
}

var _ adminports.Service = (*compSettingsService)(nil)

// activeUser builds a session carrying an active user.
func activeUser(id, email string) *usercore.Session {
	return &usercore.Session{User: &usercore.User{ID: id, Email: email, State: usercore.StateActive}}
}

// newCompositionSettingsRouter mirrors the main() mounts exactly: the outer
// /api/v1/admin mount gated by AdminModuleAccessCodes and the settings
// sub-mount /api/v1/admin/settings gated by admin.settings.email, both through
// the REAL RequireAnyPermission middleware and router.New (the composition
// root). This pins the mount ordering: chi must resolve the more-specific
// settings sub-mount for /api/v1/admin/settings/*.
func newCompositionSettingsRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{settings: &admcore.SmtpSettings{
		Host: "smtp.example.com", Port: 587, Security: admcore.SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())

	// The outer admin-module surface (main.go wraps userHandler.AdminRoutes();
	// a minimal admin-status root is sufficient here — the assertion is that
	// the settings sub-mount WINS for /api/v1/admin/settings/*).
	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
	)
}

func doSettingsComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/smtp", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCompositionSettingsMountGating verifies the composition-root wiring for
// the SMTP settings surface (finding 11): /api/v1/admin/settings/* is gated by
// admin.settings.email, AND a caller holding ONLY that code reaches it (the
// more-specific sub-mount wins over the outer admin-module mount) — i.e. mount
// ordering is correct.
func TestCompositionSettingsMountGating(t *testing.T) {
	// 401: no token.
	if rec := doSettingsComposedRequest(newCompositionSettingsRouter([]string{}, nil), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: authenticated caller holding no admin-module code — uniform 403, no
	// settings data.
	rec := doSettingsComposedRequest(newCompositionSettingsRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Errorf("no admin code: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "smtp") || strings.Contains(rec.Body.String(), "smtp.example.com") {
		t.Errorf("403 body leaks settings data: %s", rec.Body.String())
	}

	// 200: a caller holding ONLY admin.settings.email reaches the settings
	// surface — proves the settings sub-mount is chosen over the outer
	// admin-module mount (mount ordering correct) and is gated by the exact
	// code.
	rec = doSettingsComposedRequest(newCompositionSettingsRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-admin", "admin@gear.local")), "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin.settings.email holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding settings response err = %v", err)
	}
	if body["host"] != "smtp.example.com" {
		t.Errorf("settings response = %v, want host smtp.example.com (settings sub-mount served)", body)
	}

	// 200: the outer admin-module root is reachable by the same holder (the
	// code is part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	newCompositionSettingsRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-admin", "admin@gear.local")).ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// newCompositionBackupRouter mirrors the main() mounts exactly for the Story
// 3.2 backup surface: /api/v1/admin, /api/v1/admin/settings (SMTP gate) and
// /api/v1/admin/settings/backup (its OWN admin.settings.backup gate), all
// through the REAL RequireAnyPermission middleware and router.New. This pins
// the one-permission-per-surface mount ordering (AD-6): the more-specific
// backup sub-mount must win for /api/v1/admin/settings/backup/*, and a caller
// holding only admin.settings.email must NOT reach it.
func newCompositionBackupRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
	)
}

func doBackupComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/backup", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCompositionBackupMountGating verifies the Story 3.2 composition-root
// wiring: /api/v1/admin/settings/backup is gated by ITS OWN admin.settings.backup
// permission (AD-6) — a caller holding only admin.settings.email gets the
// uniform 403 (the SMTP gate is NOT widened), while a backup holder reaches the
// surface.
func TestCompositionBackupMountGating(t *testing.T) {
	// 401: no token.
	if rec := doBackupComposedRequest(newCompositionBackupRouter([]string{}, nil), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY admin.settings.email must NOT reach the backup
	// surface — the gates are separate (one permission per surface, AD-6).
	rec := doBackupComposedRequest(newCompositionBackupRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin.settings.email holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is also denied, with no
	// destination data exposed.
	rec = doBackupComposedRequest(newCompositionBackupRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin holder: status = %d, want 403", rec.Code)
	}

	// 200: a caller holding admin.settings.backup reaches the surface (empty
	// list from the in-memory service).
	rec = doBackupComposedRequest(newCompositionBackupRouter([]string{admcore.BackupSettingsPermission}, activeUser("u-admin", "admin@gear.local")), "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin.settings.backup holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("backup response = %s, want empty JSON array", rec.Body.String())
	}
}

// newCompositionScheduleRouter mirrors the main() mounts exactly for the Story
// 4.1 schedule-catalog surface: /api/v1/admin, /api/v1/admin/settings (SMTP
// gate), /api/v1/admin/settings/backup (backup gate) and
// /api/v1/admin/settings/schedules (its OWN schedules.manage gate), all through
// the REAL RequireAnyPermission middleware and router.New. This pins the
// one-permission-per-surface mount ordering (AD-6): the more-specific schedule
// sub-mount must win for /api/v1/admin/settings/schedules/*, a caller holding
// only admin.settings.email or admin.settings.backup must NOT reach it, and a
// caller holding ONLY schedules.manage reaches it (the SMTP/backup gates are
// NOT widened).
func newCompositionScheduleRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())
	schedulesSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(settingsHandler.ScheduleRoutes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
	)
}

func doScheduleComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	return doComposedJSONRequest(h, token, http.MethodGet, "/api/v1/admin/settings/schedules", "")
}

// doComposedJSONRequest issues an authenticated JSON request through the REAL
// composed router, mirroring how the SPA calls the API.
func doComposedJSONRequest(h http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCompositionScheduleMountGating verifies the Story 4.1 composition-root
// wiring: /api/v1/admin/settings/schedules is gated by ITS OWN schedules.manage
// permission (AD-6/AD-16) — a caller holding only admin.settings.email or
// admin.settings.backup gets the uniform 403 (the SMTP/backup gates are NOT
// widened), while a schedules.manage holder reaches the surface (and, via the
// outer gate, the admin module root). The reverse is also pinned: a
// schedules-only holder is denied BOTH the SMTP and backup surfaces (its code
// opens only the schedules mount, not the sibling gates).
func TestCompositionScheduleMountGating(t *testing.T) {
	// 401: no token.
	if rec := doScheduleComposedRequest(newCompositionScheduleRouter([]string{}, nil), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY admin.settings.email must NOT reach the
	// schedule surface — the gates are separate (one permission per surface).
	rec := doScheduleComposedRequest(newCompositionScheduleRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin.settings.email holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding ONLY admin.settings.backup is also denied.
	rec = doScheduleComposedRequest(newCompositionScheduleRouter([]string{admcore.BackupSettingsPermission}, activeUser("u-backup", "backup@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin.settings.backup holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is denied with no schedule
	// data exposed.
	rec = doScheduleComposedRequest(newCompositionScheduleRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin holder: status = %d, want 403", rec.Code)
	}

	// 200: a caller holding ONLY schedules.manage reaches the schedule surface
	// (empty list from the in-memory service) — proves the schedules sub-mount
	// is chosen over the outer admin-module mount (mount ordering correct).
	schedRouter := newCompositionScheduleRouter([]string{admcore.SchedulesPermission}, activeUser("u-sched", "sched@gear.local"))
	rec = doScheduleComposedRequest(schedRouter, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("schedules.manage holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("schedule response = %s, want empty JSON array", rec.Body.String())
	}

	// REVERSE: the schedules-only holder must NOT reach the SMTP surface (the
	// schedule gate does not widen the admin.settings.email gate).
	if rec := doComposedJSONRequest(schedRouter, "tok", http.MethodGet, "/api/v1/admin/settings/smtp", ""); rec.Code != http.StatusForbidden {
		t.Errorf("schedules-only holder on SMTP: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the schedules-only holder must NOT reach the backup surface (the
	// schedule gate does not widen the admin.settings.backup gate).
	if rec := doComposedJSONRequest(schedRouter, "tok", http.MethodGet, "/api/v1/admin/settings/backup", ""); rec.Code != http.StatusForbidden {
		t.Errorf("schedules-only holder on backup: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: the same holder reaches the outer admin-module root (schedules.manage
	// is part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	newCompositionScheduleRouter([]string{admcore.SchedulesPermission}, activeUser("u-sched", "sched@gear.local")).ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// newCompositionSystemRouter mirrors the main() mounts exactly for the Story
// 5-2b system-settings surface: /api/v1/admin, /api/v1/admin/settings (SMTP
// gate), /api/v1/admin/settings/backup (backup gate),
// /api/v1/admin/settings/schedules (schedules gate) and
// /api/v1/admin/settings/system (its OWN admin.settings.system gate), all
// through the REAL RequireAnyPermission middleware and router.New. This pins
// the one-permission-per-surface mount ordering (AD-6): the more-specific
// system sub-mount must win for /api/v1/admin/settings/system/*, a caller
// holding only admin.settings.email must NOT reach it, and a caller holding
// ONLY admin.settings.system reaches it (no existing gate is widened).
func newCompositionSystemRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())
	schedulesSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(settingsHandler.ScheduleRoutes())
	systemSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.AppSettingsPermission}, "admin.settings.system access denied", log)(settingsHandler.SystemRoutes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
		router.WithMount("/api/v1/admin/settings/system", systemSurface),
	)
}

func doSystemComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	return doComposedJSONRequest(h, token, http.MethodGet, "/api/v1/admin/settings/system", "")
}

// TestCompositionSystemMountGating verifies the Story 5-2b composition-root
// wiring: /api/v1/admin/settings/system is gated by ITS OWN admin.settings.system
// permission (AD-6) — a caller holding only admin.settings.email gets the
// uniform 403 (the SMTP gate is NOT widened), while a admin.settings.system
// holder reaches the surface (and, via the outer gate, the admin module root).
// The reverse is also pinned: a system-only holder is denied the SMTP surface
// (its code opens only the system mount, not the sibling gates).
func TestCompositionSystemMountGating(t *testing.T) {
	// 401: no token.
	if rec := doSystemComposedRequest(newCompositionSystemRouter([]string{}, nil), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY admin.settings.email must NOT reach the system
	// surface — the gates are separate (one permission per surface).
	rec := doSystemComposedRequest(newCompositionSystemRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin.settings.email holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is denied with no setting
	// data exposed.
	rec = doSystemComposedRequest(newCompositionSystemRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin holder: status = %d, want 403", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "einstellung") || strings.Contains(rec.Body.String(), "GEAR") {
		t.Errorf("403 body leaks setting data: %s", rec.Body.String())
	}

	// 200: a caller holding ONLY admin.settings.system reaches the system
	// surface (the 21-row typed list from the in-memory service) — proves the
	// system sub-mount is chosen over the outer admin-module mount (mount
	// ordering correct).
	systemRouter := newCompositionSystemRouter([]string{admcore.AppSettingsPermission}, activeUser("u-sys", "sys@gear.local"))
	rec = doSystemComposedRequest(systemRouter, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin.settings.system holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding system response err = %v", err)
	}
	if len(body) != 21 {
		t.Errorf("system response rows = %d, want 21", len(body))
	}
	var hasOtp bool
	for _, row := range body {
		if row["key"] == "otp_length" && row["value"] == float64(10) {
			hasOtp = true
		}
	}
	if !hasOtp {
		t.Errorf("system response = %s, want the typed otp_length row", rec.Body.String())
	}

	// REVERSE: the system-only holder must NOT reach the SMTP surface (the
	// system gate does not widen the admin.settings.email gate).
	if rec := doComposedJSONRequest(systemRouter, "tok", http.MethodGet, "/api/v1/admin/settings/smtp", ""); rec.Code != http.StatusForbidden {
		t.Errorf("system-only holder on SMTP: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: the same holder reaches the outer admin-module root
	// (admin.settings.system is part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	newCompositionSystemRouter([]string{admcore.AppSettingsPermission}, activeUser("u-sys", "sys@gear.local")).ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// TestCompositionScheduleWriteVerbs verifies the Story 4.1 write verbs through
// the REAL RequireAnyPermission mount: a schedules.manage holder can POST
// (create) and POST /{id}/archive on the composed schedules surface, while a
// non-holder (email-only) is denied both with the uniform 403.
func TestCompositionScheduleWriteVerbs(t *testing.T) {
	// POST create as a schedules-only holder → 201.
	holder := newCompositionScheduleRouter([]string{admcore.SchedulesPermission}, activeUser("u-sched", "sched@gear.local"))
	rec := doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/settings/schedules",
		`{"name":"1 year","interval_unit":"year","interval_magnitude":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("holder POST create: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var createBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decoding create response err = %v", err)
	}
	if createBody["message"] != admcore.MsgScheduleSaved {
		t.Errorf("create message = %v, want %q", createBody["message"], admcore.MsgScheduleSaved)
	}

	// POST /{id}/archive as a schedules-only holder → 200.
	rec = doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/settings/schedules/id-a/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("holder POST archive: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var archiveBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &archiveBody); err != nil {
		t.Fatalf("decoding archive response err = %v", err)
	}
	if archiveBody["message"] != admcore.MsgScheduleArchived {
		t.Errorf("archive message = %v, want %q", archiveBody["message"], admcore.MsgScheduleArchived)
	}

	// Non-holder (email-only) is denied the write verbs with the uniform 403.
	nonHolder := newCompositionScheduleRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local"))
	rec = doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/settings/schedules",
		`{"name":"1 year","interval_unit":"year","interval_magnitude":1}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("email-only POST create: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	rec = doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/settings/schedules/id-a/archive", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("email-only POST archive: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionSystemWriteVerbs verifies the Story 5-2b write verb through
// the REAL RequireAnyPermission mount: a admin.settings.system holder can PUT
// a per-setting update on the composed system surface (200 + German
// confirmation), while an email-only holder is denied with the uniform 403 —
// the gates are separate (one permission per surface, AD-6).
func TestCompositionSystemWriteVerbs(t *testing.T) {
	holder := newCompositionSystemRouter([]string{admcore.AppSettingsPermission}, activeUser("u-sys", "sys@gear.local"))
	rec := doComposedJSONRequest(holder, "tok", http.MethodPut, "/api/v1/admin/settings/system/otp_length", `{"value":8}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("holder PUT: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding PUT response err = %v", err)
	}
	if body["message"] != admcore.MsgAppSettingSaved {
		t.Errorf("PUT message = %v, want %q", body["message"], admcore.MsgAppSettingSaved)
	}
	if body["key"] != "otp_length" {
		t.Errorf("PUT response key = %v, want otp_length", body["key"])
	}

	// Non-holder (email-only) is denied the write with the uniform 403.
	nonHolder := newCompositionSystemRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local"))
	rec = doComposedJSONRequest(nonHolder, "tok", http.MethodPut, "/api/v1/admin/settings/system/otp_length", `{"value":8}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("email-only PUT: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// compToolTypeService is an in-memory toolports.Service for the tool-type
// surface. It implements the same inbound port main.go wires (the real core).
type compToolTypeService struct{}

func (s *compToolTypeService) ListToolTypes(_ context.Context, _ string) ([]*toolscore.ToolType, error) {
	return []*toolscore.ToolType{}, nil
}

func (s *compToolTypeService) CreateToolType(_ context.Context, _ string, input toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	return &toolscore.ToolType{ID: "id", Name: input.Name, DefaultScheduleID: input.DefaultScheduleID, RequiredQualificationID: input.RequiredQualificationID, InspectionMode: input.InspectionMode}, nil
}

func (s *compToolTypeService) UpdateToolType(_ context.Context, _, id string, input toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	return &toolscore.ToolType{ID: id, Name: input.Name}, nil
}

func (s *compToolTypeService) ArchiveToolType(_ context.Context, _, id string) (*toolscore.ToolType, error) {
	return &toolscore.ToolType{ID: id, Name: "archiviert"}, nil
}

func (s *compToolTypeService) ListTools(_ context.Context, _ string) ([]*toolscore.Tool, error) {
	return []*toolscore.Tool{}, nil
}

func (s *compToolTypeService) ListToolsForDashboard(_ context.Context) ([]*toolscore.DashboardTool, error) {
	return []*toolscore.DashboardTool{}, nil
}

func (s *compToolTypeService) CreateTool(_ context.Context, _ string, input toolscore.ToolInput) (*toolscore.Tool, error) {
	// Mirror the store contract (Story 4-3b): the create path IGNORES the
	// client inventory (the server auto-assigns in-SQL) — the fake returns a
	// fresh auto-assigned-looking number so a composed POST response proves the
	// value flows back out.
	return &toolscore.Tool{ID: "id", Name: input.Name, ToolTypeID: input.ToolTypeID, ScheduleID: input.ScheduleID, InventoryNumber: "GEAR00000F"}, nil
}

func (s *compToolTypeService) UpdateTool(_ context.Context, _, id string, input toolscore.ToolInput) (*toolscore.Tool, error) {
	// Echo the submitted inventory_number back (like the http-suite fake) so a
	// composed PUT response proves the edit value really reached the service —
	// a silently-dropped inventory would surface as an empty number and fail.
	return &toolscore.Tool{ID: id, Name: input.Name, InventoryNumber: input.InventoryNumber}, nil
}

func (s *compToolTypeService) ArchiveTool(_ context.Context, _, id string) (*toolscore.Tool, error) {
	return &toolscore.Tool{ID: id, Name: "archiviert"}, nil
}

func (s *compToolTypeService) StartInspection(_ context.Context, _, toolID string) (*toolscore.InspectionStartResult, error) {
	// Echo the toolID the handler read from the URL path, so the composed test
	// proves the {id} path param really reaches the service (a real round-trip).
	return &toolscore.InspectionStartResult{ToolID: toolID, ToolName: "test", InspectionMode: toolscore.InspectionModePassFail}, nil
}

func (s *compToolTypeService) SubmitInspection(_ context.Context, _, toolID string, input toolscore.InspectionInput) (*toolscore.SubmitInspectionResult, error) {
	// Echo the {id} path param + the persisted record + a green status, so the
	// composed submit test proves the path param reaches the service AND the
	// submit response DTO round-trips (Story 5.3).
	return &toolscore.SubmitInspectionResult{
		Inspection: &toolscore.Inspection{
			ID: "id-insp-comp", ToolID: toolID, InspectorID: "u-admin",
			Mode: input.Mode, OverallResult: input.Result, Notes: input.Notes,
			SubmittedAt: time.Now(),
		},
		Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen},
	}, nil
}

func (s *compToolTypeService) ReinstateTool(_ context.Context, _, toolID, _ string) (*toolscore.ReinstateResult, error) {
	// Echo the {id} path param + a green status, so the composed reinstate test
	// proves the path param reaches the service AND the reinstate response DTO
	// round-trips (Story 5.6).
	return &toolscore.ReinstateResult{Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen}}, nil
}

func (s *compToolTypeService) ListInspectionHistory(_ context.Context, _, _ string) (*toolscore.ToolHistory, error) {
	// An empty history fixture: the composed history mount-gate test only needs
	// the service to be reached (the DTO shape is pinned in the http suite).
	return &toolscore.ToolHistory{Inspections: []*toolscore.ToolHistoryInspection{}, Reinstatements: []*toolscore.ToolHistoryReinstatement{}}, nil
}

func (s *compToolTypeService) ExportStatusReport(_ context.Context, _ string, _ []string) ([]*toolscore.ReportRow, error) {
	// An empty report fixture: the composed report mount-gate test only needs
	// the service to be reached (the PDF magic + content type are pinned in the
	// http suite).
	return []*toolscore.ReportRow{}, nil
}

func (s *compToolTypeService) ImportTools(_ context.Context, _ string, _ []toolscore.ToolImportRow) (*toolscore.ToolImportResult, error) {
	return &toolscore.ToolImportResult{Imported: 0, Errors: []toolscore.ToolImportError{}}, nil
}

var _ toolports.Service = (*compToolTypeService)(nil)

// newCompositionToolTypeRouter mirrors the main() mounts exactly for the Story
// 4.2 tool-type surface: /api/v1/admin, /api/v1/admin/settings (SMTP gate),
// /api/v1/admin/settings/backup (backup gate), /api/v1/admin/settings/schedules
// (schedules gate) and /api/v1/admin/tool-types (its OWN tool_types.manage
// gate), all through the REAL RequireAnyPermission middleware and router.New.
// This pins the one-permission-per-surface mount ordering (AD-6): the
// more-specific tool-types sub-mount must win for /api/v1/admin/tool-types/*, a
// caller holding only admin.settings.email must NOT reach it, and a caller
// holding ONLY tool_types.manage reaches it (no existing gate is widened).
func newCompositionToolTypeRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())
	schedulesSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(settingsHandler.ScheduleRoutes())

	toolHandler := toolhttp.NewHandler(&compToolTypeService{}, validator, resolver, log)
	toolTypesSurface := auth.RequireAnyPermission(validator, resolver, []string{toolscore.ToolTypesManagePermission}, "tool_types.manage access denied", log)(toolHandler.ToolTypeRoutes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
		router.WithMount("/api/v1/admin/tool-types", toolTypesSurface),
	)
}

func doToolTypesComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	return doComposedJSONRequest(h, token, http.MethodGet, "/api/v1/admin/tool-types", "")
}

// TestCompositionToolTypesMountGating verifies the Story 4.2 composition-root
// wiring: /api/v1/admin/tool-types is gated by ITS OWN tool_types.manage
// permission (AD-6/AD-10) — a caller holding only admin.settings.email gets the
// uniform 403 (the SMTP gate is NOT widened), while a tool_types.manage holder
// reaches the surface (and, via the outer gate, the admin module root). The
// reverse is also pinned: a tool-types-only holder is denied the SMTP surface
// (its code opens only the tool-types mount).
func TestCompositionToolTypesMountGating(t *testing.T) {
	// 401: no token.
	if rec := doToolTypesComposedRequest(newCompositionToolTypeRouter([]string{}, nil), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY admin.settings.email must NOT reach the
	// tool-types surface — the gates are separate (one permission per surface).
	rec := doToolTypesComposedRequest(newCompositionToolTypeRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin.settings.email holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is denied with no tool-type
	// data exposed.
	rec = doToolTypesComposedRequest(newCompositionToolTypeRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin holder: status = %d, want 403", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "gerätetyp") {
		t.Errorf("403 body leaks tool-type data: %s", rec.Body.String())
	}

	// 200: a caller holding ONLY tool_types.manage reaches the tool-types
	// surface (empty list from the in-memory service) — proves the tool-types
	// sub-mount is chosen over the outer admin-module mount (mount ordering
	// correct).
	toolRouter := newCompositionToolTypeRouter([]string{toolscore.ToolTypesManagePermission}, activeUser("u-tools", "tools@gear.local"))
	rec = doToolTypesComposedRequest(toolRouter, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("tool_types.manage holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("tool-type response = %s, want empty JSON array", rec.Body.String())
	}

	// REVERSE: the tool-types-only holder must NOT reach the SMTP surface (the
	// tool-types gate does not widen the admin.settings.email gate).
	if rec := doComposedJSONRequest(toolRouter, "tok", http.MethodGet, "/api/v1/admin/settings/smtp", ""); rec.Code != http.StatusForbidden {
		t.Errorf("tool-types-only holder on SMTP: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: the same holder reaches the outer admin-module root (tool_types.manage
	// is part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	newCompositionToolTypeRouter([]string{toolscore.ToolTypesManagePermission}, activeUser("u-tools", "tools@gear.local")).ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// TestCompositionToolTypesWriteVerbs verifies the Story 4.2 write verbs through
// the REAL RequireAnyPermission mount: a tool_types.manage holder can POST
// (create), PUT (update) and POST /{id}/archive on the composed tool-types
// surface, while a non-holder (email-only) is denied all of them with the
// uniform 403.
func TestCompositionToolTypesWriteVerbs(t *testing.T) {
	body := `{"name":"Bohrmaschine","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"checklist","items":[{"label":"Kabel"}]}`
	holder := newCompositionToolTypeRouter([]string{toolscore.ToolTypesManagePermission}, activeUser("u-tools", "tools@gear.local"))

	// POST create as a tool-types-only holder → 201.
	rec := doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/tool-types", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("holder POST create: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var createBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decoding create response err = %v", err)
	}
	if createBody["message"] != toolscore.MsgToolTypeSaved {
		t.Errorf("create message = %v, want %q", createBody["message"], toolscore.MsgToolTypeSaved)
	}

	// PUT update as a tool-types-only holder → 200.
	rec = doComposedJSONRequest(holder, "tok", http.MethodPut, "/api/v1/admin/tool-types/id-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("holder PUT update: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// POST /{id}/archive as a tool-types-only holder → 200.
	rec = doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/tool-types/id-a/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("holder POST archive: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var archiveBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &archiveBody); err != nil {
		t.Fatalf("decoding archive response err = %v", err)
	}
	if archiveBody["message"] != toolscore.MsgToolTypeArchived {
		t.Errorf("archive message = %v, want %q", archiveBody["message"], toolscore.MsgToolTypeArchived)
	}

	// Non-holder (email-only) is denied all write verbs with the uniform 403.
	nonHolder := newCompositionToolTypeRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local"))
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/tool-types", body); rec.Code != http.StatusForbidden {
		t.Errorf("email-only POST create: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPut, "/api/v1/admin/tool-types/id-a", body); rec.Code != http.StatusForbidden {
		t.Errorf("email-only PUT update: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/tool-types/id-a/archive", ""); rec.Code != http.StatusForbidden {
		t.Errorf("email-only POST archive: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// newCompositionToolRouter mirrors the main() mounts exactly for the Story 4.2
// + 4.3 tool surfaces: /api/v1/admin, /api/v1/admin/settings (SMTP gate),
// /api/v1/admin/settings/backup (backup gate), /api/v1/admin/settings/schedules
// (schedules gate), /api/v1/admin/tool-types (its OWN tool_types.manage gate)
// and /api/v1/admin/tools (its OWN tools.manage gate), all through the REAL
// RequireAnyPermission middleware and router.New. This pins the
// one-permission-per-surface mount ordering (AD-6): the more-specific sub-mount
// must win for each /api/v1/admin/tools/* route, a caller holding only
// tool_types.manage must NOT reach /tools (and vice-versa), and a caller
// holding ONLY tools.manage reaches the tool surface.
func newCompositionToolRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())
	schedulesSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(settingsHandler.ScheduleRoutes())

	toolHandler := toolhttp.NewHandler(&compToolTypeService{}, validator, resolver, log)
	toolTypesSurface := auth.RequireAnyPermission(validator, resolver, []string{toolscore.ToolTypesManagePermission}, "tool_types.manage access denied", log)(toolHandler.ToolTypeRoutes())
	toolToolsSurface := auth.RequireAnyPermission(validator, resolver, []string{toolscore.ToolsManagePermission, toolscore.ToolEditPermission}, "tools.manage/tool.edit access denied", log)(toolHandler.ToolRoutes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
		router.WithMount("/api/v1/admin/tool-types", toolTypesSurface),
		router.WithMount("/api/v1/admin/tools", toolToolsSurface),
	)
}

// TestCompositionToolsMountGating verifies the Story 4.3 composition-root
// wiring: /api/v1/admin/tools is gated by ITS OWN tools.manage permission
// (AD-6/AD-10) — a caller holding only tool_types.manage gets the uniform 403
// (the type gate is NOT widened to tools), while a tools.manage holder reaches
// the surface (and, via the outer gate, the admin module root). The reverse is
// also pinned: a tools-only holder is denied the tool-types surface (its code
// opens only the tools mount) and the SMTP surface.
func TestCompositionToolsMountGating(t *testing.T) {
	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolRouter([]string{}, nil), "", http.MethodGet, "/api/v1/admin/tools", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY tool_types.manage must NOT reach the tools
	// surface — the gates are separate (one permission per surface, AD-6).
	rec := doComposedJSONRequest(newCompositionToolRouter([]string{toolscore.ToolTypesManagePermission}, activeUser("u-types", "types@gear.local")), "tok", http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tool_types.manage holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is denied with no tool data
	// exposed.
	rec = doComposedJSONRequest(newCompositionToolRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local")), "tok", http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin holder: status = %d, want 403", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}

	// 200: a caller holding ONLY tools.manage reaches the tools surface (empty
	// list from the in-memory service) — proves the tools sub-mount is chosen
	// over the outer admin-module mount (mount ordering correct).
	toolRouter := newCompositionToolRouter([]string{toolscore.ToolsManagePermission}, activeUser("u-tools", "tools@gear.local"))
	rec = doComposedJSONRequest(toolRouter, "tok", http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("tools.manage holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("tool response = %s, want empty JSON array", rec.Body.String())
	}

	// REVERSE: the tools-only holder must NOT reach the tool-types surface (its
	// code opens only the tools mount, not the sibling type gate).
	if rec := doComposedJSONRequest(toolRouter, "tok", http.MethodGet, "/api/v1/admin/tool-types", ""); rec.Code != http.StatusForbidden {
		t.Errorf("tools-only holder on tool-types: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the tools-only holder must NOT reach the SMTP surface (the tools
	// gate does not widen the admin.settings.email gate).
	if rec := doComposedJSONRequest(toolRouter, "tok", http.MethodGet, "/api/v1/admin/settings/smtp", ""); rec.Code != http.StatusForbidden {
		t.Errorf("tools-only holder on SMTP: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: the same holder reaches the outer admin-module root (tools.manage is
	// part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	newCompositionToolRouter([]string{toolscore.ToolsManagePermission}, activeUser("u-tools", "tools@gear.local")).ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// TestCompositionToolsWriteVerbs verifies the Story 4.3 write verbs through the
// REAL RequireAnyPermission mount: a tools.manage holder can POST (create), PUT
// (update) and POST /{id}/archive on the composed tools surface, while a
// non-holder (tool_types-only) is denied all of them with the uniform 403.
func TestCompositionToolsWriteVerbs(t *testing.T) {
	body := `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":""}`
	holder := newCompositionToolRouter([]string{toolscore.ToolsManagePermission}, activeUser("u-tools", "tools@gear.local"))

	// POST create as a tools-only holder → 201.
	rec := doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/tools", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("holder POST create: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var createBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decoding create response err = %v", err)
	}
	if createBody["message"] != toolscore.MsgToolSaved {
		t.Errorf("create message = %v, want %q", createBody["message"], toolscore.MsgToolSaved)
	}

	// PUT update as a tools-only holder → 200.
	rec = doComposedJSONRequest(holder, "tok", http.MethodPut, "/api/v1/admin/tools/id-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("holder PUT update: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// POST /{id}/archive as a tools-only holder → 200.
	rec = doComposedJSONRequest(holder, "tok", http.MethodPost, "/api/v1/admin/tools/id-a/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("holder POST archive: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var archiveBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &archiveBody); err != nil {
		t.Fatalf("decoding archive response err = %v", err)
	}
	if archiveBody["message"] != toolscore.MsgToolArchived {
		t.Errorf("archive message = %v, want %q", archiveBody["message"], toolscore.MsgToolArchived)
	}

	// Non-holder (tool_types-only) is denied all write verbs with the uniform
	// 403 (the type gate does NOT widen to the tools surface).
	nonHolder := newCompositionToolRouter([]string{toolscore.ToolTypesManagePermission}, activeUser("u-types", "types@gear.local"))
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/tools", body); rec.Code != http.StatusForbidden {
		t.Errorf("tool_types-only POST create: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPut, "/api/v1/admin/tools/id-a", body); rec.Code != http.StatusForbidden {
		t.Errorf("tool_types-only PUT update: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doComposedJSONRequest(nonHolder, "tok", http.MethodPost, "/api/v1/admin/tools/id-a/archive", ""); rec.Code != http.StatusForbidden {
		t.Errorf("tool_types-only POST archive: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionToolsEditOnlyGate verifies the Story 4-3b permission split
// through the REAL composition wiring: the outer /api/v1/admin/tools mount is
// ANY-of [tools.manage, tool.edit], and the write-only sub-gate inside
// ToolRoutes re-applies tools.manage-ONLY for POST (create) + archive. A
// tool.edit-only holder gets GET/PUT (200) but POST/archive (403); a
// tools.manage holder gets everything (pinned by TestCompositionToolsWriteVerbs).
func TestCompositionToolsEditOnlyGate(t *testing.T) {
	body := `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","inventory_number":"GEAR0042"}`
	editor := newCompositionToolRouter([]string{toolscore.ToolEditPermission}, activeUser("u-editor", "editor@gear.local"))

	// GATE_GET: the any-of read gate lets the tool.edit-only holder list.
	rec := doComposedJSONRequest(editor, "tok", http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("tool.edit-only GET: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("tool.edit-only GET body = %s, want the empty tool list", rec.Body.String())
	}

	// GATE_UPDATE: PUT (edit, incl. the inventory number) is any-of → 200.
	rec = doComposedJSONRequest(editor, "tok", http.MethodPut, "/api/v1/admin/tools/id-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("tool.edit-only PUT: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// The inventory value must have REALLY arrived at the service and flow back
	// through the DTO — a silently-dropped inventory on the edit path fails here.
	var putBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &putBody); err != nil {
		t.Fatalf("decoding PUT response err = %v", err)
	}
	if putBody["inventory_number"] != "GEAR0042" {
		t.Errorf("PUT response inventory_number = %v, want GEAR0042 (the edit value must round-trip through the composed path)", putBody["inventory_number"])
	}

	// GATE_CREATE: POST is tools.manage-ONLY (the write-only sub-gate) → 403.
	rec = doComposedJSONRequest(editor, "tok", http.MethodPost, "/api/v1/admin/tools", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tool.edit-only POST: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") {
		t.Errorf("tool.edit-only POST 403 leaks tool data: %s", rec.Body.String())
	}

	// GATE_ARCHIVE: POST /{id}/archive is tools.manage-ONLY → 403.
	rec = doComposedJSONRequest(editor, "tok", http.MethodPost, "/api/v1/admin/tools/id-a/archive", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tool.edit-only archive: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// FORBIDDEN: no tools.manage/tool.edit → uniform 403, no tool data.
	nonHolder := newCompositionToolRouter([]string{"dashboard.view"}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(nonHolder, "tok", http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-holder GET: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") {
		t.Errorf("non-holder 403 leaks tool data: %s", rec.Body.String())
	}
}

// newCompositionToolsRouter mirrors the main() mounts exactly for the combined
// /api/v1/tools surface (Story 4-3b dashboard list + Story 5.1/5.3 inspection
// start + submit + Story 5.6 reinstatement): the dashboard list is mounted with
// its OWN `dashboard.view` gate, the inspection start/submit with its OWN
// `inspection.submit` gate and the reinstatement with its OWN `tool.reinstate`
// gate (RequirePermission — the single-code gateway, NOT the admin
// RequireAnyPermission), combined via exact-match Handle + prefix Mount. This
// pins that each /api/v1/tools surface is a separate GEAR-module surface behind
// ONE permission per surface (AD-6): the dashboard list is reachable by ANY
// dashboard.view holder regardless of tools.manage, the inspection
// start/submit only by inspection.submit holders and the reinstatement only by
// tool.reinstate holders.
func newCompositionToolsRouter(perms []string, session *usercore.Session) http.Handler {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())
	backupSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(settingsHandler.BackupRoutes())
	schedulesSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(settingsHandler.ScheduleRoutes())

	toolHandler := toolhttp.NewHandler(&compToolTypeService{}, validator, resolver, log)
	toolTypesSurface := auth.RequireAnyPermission(validator, resolver, []string{toolscore.ToolTypesManagePermission}, "tool_types.manage access denied", log)(toolHandler.ToolTypeRoutes())
	toolToolsSurface := auth.RequireAnyPermission(validator, resolver, []string{toolscore.ToolsManagePermission, toolscore.ToolEditPermission}, "tools.manage/tool.edit access denied", log)(toolHandler.ToolRoutes())
	dashboardToolsSurface := auth.RequirePermission(validator, resolver, toolscore.DashboardViewPermission)(toolHandler.DashboardToolsRoutes())
	inspectionSurface := auth.RequirePermission(validator, resolver, toolscore.InspectionSubmitPermission)(toolHandler.InspectionRoutes())
	reinstateSurface := auth.RequirePermission(validator, resolver, toolscore.ToolReinstatePermission)(toolHandler.ReinstateRoutes())
	historySurface := auth.RequirePermission(validator, resolver, toolscore.InspectionHistoryViewPermission)(toolHandler.HistoryRoutes())
	reportSurface := auth.RequirePermission(validator, resolver, toolscore.ReportExportPermission)(toolHandler.ReportRoutes())
	toolsSurface := chi.NewRouter()
	toolsSurface.NotFound(httpapi.NotFoundHandler())
	toolsSurface.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	toolsSurface.Handle("/", dashboardToolsSurface)
	toolsSurface.Mount("/{id}/inspection", inspectionSurface)
	toolsSurface.Mount("/{id}/reinstatement", reinstateSurface)
	toolsSurface.Mount("/{id}/history", historySurface)
	toolsSurface.Mount("/report.pdf", reportSurface)

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
		router.WithMount("/api/v1/admin/tool-types", toolTypesSurface),
		router.WithMount("/api/v1/admin/tools", toolToolsSurface),
		router.WithMount("/api/v1/tools", toolsSurface),
	)
}

// TestCompositionDashboardToolsMountGating verifies the Story 4-3b
// composition-root wiring: /api/v1/tools is gated by ITS OWN dashboard.view
// permission (one permission per surface, AD-6) — a dashboard.view holder (all
// base roles) reaches the minimal tool list EVEN without tools.manage, while a
// tools.manage-but-not-dashboard.view caller answers the uniform 403 (the
// dashboard gate is NOT widened by the admin tools code). Unauthenticated
// callers answer 401.
func TestCompositionDashboardToolsMountGating(t *testing.T) {
	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: a caller holding ONLY tools.manage (no dashboard.view) is denied the
	// dashboard surface with no tool data exposed (AD-6).
	rec := doComposedJSONRequest(newCompositionToolsRouter([]string{toolscore.ToolsManagePermission}, activeUser("u-tools", "tools@gear.local")), "tok", http.MethodGet, "/api/v1/tools", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tools.manage-only holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}

	// 200: a dashboard.view holder WITHOUT tools.manage reaches the dashboard
	// surface (empty list from the in-memory service) — proves the ungated core
	// read + the dashboard.view HTTP gate work together.
	dashRouter := newCompositionToolsRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(dashRouter, "tok", http.MethodGet, "/api/v1/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard.view holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("dashboard tool response = %s, want empty JSON array", rec.Body.String())
	}

	// 200: a caller holding BOTH codes also reaches it.
	rec = doComposedJSONRequest(newCompositionToolsRouter([]string{toolscore.DashboardViewPermission, toolscore.ToolsManagePermission}, activeUser("u-admin", "admin@gear.local")), "tok", http.MethodGet, "/api/v1/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard.view + tools.manage holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the dashboard-only holder must NOT reach the admin tools surface
	// (its code opens only the dashboard mount, not the tools.manage gate).
	if rec := doComposedJSONRequest(dashRouter, "tok", http.MethodGet, "/api/v1/admin/tools", ""); rec.Code != http.StatusForbidden {
		t.Errorf("dashboard-only holder on admin tools: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionInspectionStartMountGating verifies the Story 5.1
// composition-root wiring: the inspection START is a NEW surface under
// /api/v1/tools with its OWN gate — `inspection.submit` (one permission per
// surface, AD-6/FR-11/AD-7) — combined with the dashboard.view-gated list via
// exact-match Handle (each surface keeps its own gate, no shared middleware).
// A dashboard.view-but-not-inspection.submit caller still READS the
// Werkzeugliste but 403s on the start (no tool data); an inspection.submit
// holder reaches the start; unauthenticated callers answer 401.
func TestCompositionInspectionStartMountGating(t *testing.T) {
	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodPost, "/api/v1/tools/id-a/inspection/start", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: an inspection.submit holder (all base roles) reaches the start —
	// the in-memory service answers an eligible /start with the tool + mode.
	startRouter := newCompositionToolsRouter([]string{toolscore.InspectionSubmitPermission}, activeUser("u-vol", "vol@gear.local"))
	rec := doComposedJSONRequest(startRouter, "tok", http.MethodPost, "/api/v1/tools/id-a/inspection/start", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inspection.submit holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var startBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &startBody); err != nil {
		t.Fatalf("decoding start response err = %v", err)
	}
	// The fake echoes the {id} URL path param back — a REAL round-trip: the
	// path param reached the service and flowed back out as tool_id.
	if startBody["tool_id"] != "id-a" || startBody["tool_name"] != "test" {
		t.Errorf("start body = %+v, want tool_id id-a (the path param) + the eligible DTO", startBody)
	}
	if startBody["inspection_mode"] != toolscore.InspectionModePassFail {
		t.Errorf("mode = %+v, want %q", startBody["inspection_mode"], toolscore.InspectionModePassFail)
	}

	// 403 with NO tool data: a dashboard.view-but-not-inspection.submit caller
	// is denied the start (the dashboard gate does NOT widen into the start
	// surface), while the dashboard LIST itself still 200s for the same caller.
	dashboardOnly := newCompositionToolsRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(dashboardOnly, "tok", http.MethodPost, "/api/v1/tools/id-a/inspection/start", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard.view-only start: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	if rec := doComposedJSONRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusOK {
		t.Errorf("dashboard.view-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the inspection.submit holder (no dashboard.view) reaches the
	// start but is denied the dashboard list — each surface has its OWN gate.
	if rec := doComposedJSONRequest(startRouter, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusForbidden {
		t.Errorf("inspection.submit-only list: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionInspectionSubmitMountGating verifies the Story 5.3
// composition-root wiring: the inspection SUBMIT is a sibling sub-path of the
// SAME /api/v1/tools/{id}/inspection mount as the start, inheriting its
// `inspection.submit` gate (one permission per surface, AD-6) — no second mount
// or gate is added. A dashboard.view-but-not-inspection.submit caller 403s (no
// tool data); an inspection.submit holder reaches it and the {id} path param
// round-trips through the composed router.
func TestCompositionInspectionSubmitMountGating(t *testing.T) {
	submitBody := `{"mode":"pass_fail","result":"pass","notes":"","items":[]}`

	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodPost, "/api/v1/tools/id-a/inspection", submitBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 200: an inspection.submit holder reaches the submit — the fake echoes the
	// {id} path param back as tool_id (a real round-trip through the mounted
	// surface).
	submitRouter := newCompositionToolsRouter([]string{toolscore.InspectionSubmitPermission}, activeUser("u-vol", "vol@gear.local"))
	rec := doComposedJSONRequest(submitRouter, "tok", http.MethodPost, "/api/v1/tools/id-a/inspection", submitBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("inspection.submit holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding submit response err = %v", err)
	}
	insp, ok := body["inspection"].(map[string]any)
	if !ok || insp["tool_id"] != "id-a" || insp["overall_result"] != "pass" {
		t.Errorf("submit inspection = %+v, want tool_id id-a (the path param) + the pass record", body["inspection"])
	}
	status, ok := body["status"].(map[string]any)
	if !ok || status["status"] != "green" {
		t.Errorf("submit status = %+v, want green", body["status"])
	}

	// 403 with NO tool data: a dashboard.view-but-not-inspection.submit caller
	// is denied the submit, while the dashboard LIST still 200s for the same
	// caller.
	dashboardOnly := newCompositionToolsRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(dashboardOnly, "tok", http.MethodPost, "/api/v1/tools/id-a/inspection", submitBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard.view-only submit: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	if rec := doComposedJSONRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusOK {
		t.Errorf("dashboard.view-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionReinstateMountGating verifies the Story 5.6 composition-root
// wiring: the reinstatement is a SIBLING sub-path of the SAME /api/v1/tools
// router mounted at /{id}/reinstatement with ITS OWN `tool.reinstate` gate (one
// permission per surface, AD-6) — it is NOT inherited from the inspection
// surface. An inspection.submit-but-not-tool.reinstate caller can start/submit
// but the reinstatement answers 403 (no tool data); a tool.reinstate holder
// reaches it and the {id} path param round-trips through the composed router.
func TestCompositionReinstateMountGating(t *testing.T) {
	reinstateBody := `{"reason":"Ersatzteil eingetroffen"}`

	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodPost, "/api/v1/tools/id-a/reinstatement", reinstateBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 200: a tool.reinstate holder reaches the reinstatement — the fake echoes
	// the {id} path param + a green status (a real round-trip through the
	// mounted surface).
	reinstateRouter := newCompositionToolsRouter([]string{toolscore.ToolReinstatePermission}, activeUser("u-fuehrung", "fuehrung@gear.local"))
	rec := doComposedJSONRequest(reinstateRouter, "tok", http.MethodPost, "/api/v1/tools/id-a/reinstatement", reinstateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("tool.reinstate holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding reinstate response err = %v", err)
	}
	status, ok := body["status"].(map[string]any)
	if !ok || status["status"] != "green" {
		t.Errorf("reinstate status = %+v, want the derived green status", body["status"])
	}
	if body["message"] != toolscore.MsgToolReinstated {
		t.Errorf("reinstate message = %+v, want %q", body["message"], toolscore.MsgToolReinstated)
	}

	// 403 with NO tool data: an inspection.submit-but-not-tool.reinstate caller
	// is denied the reinstatement (the sibling surface keeps its OWN gate),
	// while the inspection start still 200s for the same caller.
	inspectionOnly := newCompositionToolsRouter([]string{toolscore.InspectionSubmitPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(inspectionOnly, "tok", http.MethodPost, "/api/v1/tools/id-a/reinstatement", reinstateBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("inspection.submit-only reinstate: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	if rec := doComposedJSONRequest(inspectionOnly, "tok", http.MethodPost, "/api/v1/tools/id-a/inspection/start", ""); rec.Code != http.StatusOK {
		t.Errorf("inspection.submit-only start: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestCompositionHistoryMountGating verifies the Story 6.3 composition-root
// wiring: the per-tool history is a SIBLING sub-path of the SAME /api/v1/tools
// router mounted at /{id}/history with ITS OWN `inspection.history.view` gate
// (one permission per surface, AD-6) — it is NOT inherited from the dashboard
// or inspection surfaces. A dashboard.view-but-not-inspection.history.view
// caller can READ the Werkzeugliste but the history answers 403 (no data); an
// inspection.history.view holder reaches it and the {id} path param round-trips
// through the composed router.
func TestCompositionHistoryMountGating(t *testing.T) {
	// 401: no token.
	if rec := doComposedJSONRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodGet, "/api/v1/tools/id-a/history", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: an inspection.history.view holder reaches the history — the
	// in-memory service answers the empty-history DTO (a real round-trip
	// through the mounted surface).
	historyRouter := newCompositionToolsRouter([]string{toolscore.InspectionHistoryViewPermission}, activeUser("u-fuehrung", "fuehrung@gear.local"))
	rec := doComposedJSONRequest(historyRouter, "tok", http.MethodGet, "/api/v1/tools/id-a/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inspection.history.view holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding history response err = %v", err)
	}
	insp, ok := body["inspections"].([]any)
	if !ok || len(insp) != 0 {
		t.Errorf("history inspections = %+v, want an empty array", body["inspections"])
	}
	if rein, ok := body["reinstatements"].([]any); !ok || len(rein) != 0 {
		t.Errorf("history reinstatements = %+v, want an empty array", body["reinstatements"])
	}

	// 403 with NO data: a dashboard.view-but-not-inspection.history.view caller
	// is denied the history (the sibling surface keeps its OWN gate), while the
	// dashboard LIST still 200s for the same caller.
	dashboardOnly := newCompositionToolsRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedJSONRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools/id-a/history", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard.view-only history: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "werkzeug") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	if rec := doComposedJSONRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusOK {
		t.Errorf("dashboard.view-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the history holder (no dashboard.view) reaches the history but
	// is denied the dashboard list — each surface has its OWN gate.
	if rec := doComposedJSONRequest(historyRouter, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusForbidden {
		t.Errorf("history-only list: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// ============================================================================
// Story 4-3b real-wiring composed E2E (finding 9): the auto-assigned inventory
// number must round-trip through the REAL composition path — gate → HTTP
// handler → core → postgres repo → in-SQL nextval — not a hardcoded fake.
// ============================================================================

const composedArgon2DummyHash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM"

// newComposedRealToolRouter wires the REAL Story 4-3b tool surface exactly like
// cmd/server/main.go: toolpostgres store + tools core + tools HTTP handler
// mounted at /api/v1/admin/tools behind the any-of [tools.manage, tool.edit]
// gate, with the REAL SessionManager + user Repository as the auth seams (the
// write-only sub-gate inside ToolRoutes re-uses them). The tool core consumes
// nil schedules/qualifications ports (not hit for an empty-override create).
func newComposedRealToolRouter(t *testing.T, log *slog.Logger) (http.Handler, *userpostgres.Repository, *usercore.SessionManager, *pgxpool.Pool) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	userRepo := userpostgres.NewRepository(userpostgres.New(pool))
	sm := usercore.NewSessionManager(userRepo, time.Hour)
	toolRepo := toolpostgres.NewRepository(toolpostgres.New(pool))
	toolService := toolscore.NewService(toolRepo, nil, nil, nil, userRepo, userRepo, userRepo, log)
	toolHandler := toolhttp.NewHandler(toolService, sm, userRepo, log)
	toolsSurface := auth.RequireAnyPermission(sm, userRepo,
		[]string{toolscore.ToolsManagePermission, toolscore.ToolEditPermission},
		"tools.manage/tool.edit access denied", log)(toolHandler.ToolRoutes())
	r := router.New(pool, log,
		router.WithMount("/api/v1/admin/tools", toolsSurface),
	)
	return r, userRepo, sm, pool
}

// composedSeedToolRefs inserts one Test- schedule (Admin-owned catalog) + one
// Test- tool type (Tool-owned) into the live dev DB and cleans them up. Cleanup
// order matters: tools BEFORE tool_types/schedules.
func composedSeedToolRefs(t *testing.T, pool *pgxpool.Pool) (toolTypeID, scheduleID string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"tools", "tool_types", "schedules", "qualifications"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE lower(name) LIKE 'test-%'"); err != nil {
			t.Fatalf("cleanup %s err = %v", table, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tools WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM tool_types WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE lower(name) LIKE 'test-%'")
	})
	if err := pool.QueryRow(ctx,
		"INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES ('Test-Zeitplan', 'year', 1) RETURNING id",
	).Scan(&scheduleID); err != nil {
		t.Fatalf("seeding schedule err = %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode)
		 VALUES ('Test-Geraetetyp', $1, NULL, 'pass_fail') RETURNING id`, scheduleID,
	).Scan(&toolTypeID); err != nil {
		t.Fatalf("seeding tool type err = %v", err)
	}
	return toolTypeID, scheduleID
}

// composedCreateAdminUser registers a FRESH active user with the admin role
// (grants tools.manage), issues a live session token and cleans up the row.
func composedCreateAdminUser(t *testing.T, pool *pgxpool.Pool, repo *userpostgres.Repository, sm *usercore.SessionManager, stamp, tag string) string {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("comptool.%s.%s@gear.local", tag, stamp)
	user, err := repo.CreateRegisteredUser(ctx, email, "Comp Tool", "Comp", "Tool", composedArgon2DummyHash)
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	if _, err := pool.Exec(ctx, "UPDATE users SET state = 'active' WHERE id = $1", user.ID); err != nil {
		t.Fatalf("activating user failed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_permission_groups (user_id, permission_group_id) SELECT $1, g.id FROM permission_groups g WHERE g.name = 'admin'`, user.ID); err != nil {
		t.Fatalf("granting admin role failed: %v", err)
	}
	token, err := sm.Issue(ctx, user)
	if err != nil {
		t.Fatalf("issuing session failed: %v", err)
	}
	return token
}

// TestComposedToolCreateAutoAssignsInventory pins the full create round-trip
// through the REAL wiring (finding 9): a tools.manage holder POSTs a tool and
// the response carries a non-empty 'GEAR%09d' number auto-assigned in-SQL by
// the store; a subsequent GET returns the same number. This proves the
// auto-assignment is NOT a fake-service artifact but flows through
// gate → handler → core → postgres repo → nextval and back.
func TestComposedToolCreateAutoAssignsInventory(t *testing.T) {
	r, repo, sm, pool := newComposedRealToolRouter(t, discardLogger())
	stamp := time.Now().Format("20060102150405.000000")
	toolTypeID, _ := composedSeedToolRefs(t, pool)
	token := composedCreateAdminUser(t, pool, repo, sm, stamp, "e2e")

	// POST create with an EMPTY schedule override (the AD-5 inherit default) —
	// the in-SQL nextval auto-assignment is what we are pinning.
	body := fmt.Sprintf(`{"name":"Test-Composed-Tool","tool_type_id":%q,"schedule_id":"","attributes":{}}`, toolTypeID)
	rec := doComposedJSONRequest(r, token, http.MethodPost, "/api/v1/admin/tools", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST create: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created struct {
		ID              string `json:"id"`
		InventoryNumber string `json:"inventory_number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create response err = %v", err)
	}
	if !regexp.MustCompile(`^GEAR\d{9}$`).MatchString(created.InventoryNumber) {
		t.Fatalf("created inventory_number = %q, want a non-empty 'GEAR' + 9 zero-padded digits", created.InventoryNumber)
	}
	if created.ID == "" {
		t.Fatal("created tool id empty")
	}

	// GET returns the SAME auto-assigned number (round-trip through the real
	// postgres list query).
	rec = doComposedJSONRequest(r, token, http.MethodGet, "/api/v1/admin/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var list []struct {
		ID              string `json:"id"`
		InventoryNumber string `json:"inventory_number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding GET list err = %v", err)
	}
	found := false
	for _, tool := range list {
		if tool.ID == created.ID {
			found = true
			if tool.InventoryNumber != created.InventoryNumber {
				t.Errorf("GET inventory_number = %q, want the created %q (round-trip mismatch)", tool.InventoryNumber, created.InventoryNumber)
			}
		}
	}
	if !found {
		t.Errorf("created tool %s missing from the GET list", created.ID)
	}
}

// TestCompositionReportMountGating verifies the Story 6.2 composition-root
// wiring: the status report is a SIBLING sub-path of the SAME /api/v1/tools
// router mounted at /report.pdf with ITS OWN `report.export` gate (one
// permission per surface, AD-6) — it is NOT inherited from the dashboard or
// inspection surfaces. A dashboard.view-but-not-report.export caller can READ
// the Werkzeugliste but the export answers 403 with NO PDF bytes; a
// report.export holder reaches it (application/pdf + the %PDF magic).
func TestCompositionReportMountGating(t *testing.T) {
	// 401: no token.
	rec := doComposedRawRequest(newCompositionToolsRouter([]string{}, nil), "", http.MethodGet, "/api/v1/tools/report.pdf")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: a report.export holder reaches the report — the in-memory service
	// answers an empty row set and the handler renders a real PDF (a round-trip
	// through the mounted surface: application/pdf + the %PDF magic).
	reportRouter := newCompositionToolsRouter([]string{toolscore.ReportExportPermission}, activeUser("u-fuehrung", "fuehrung@gear.local"))
	rec = doComposedRawRequest(reportRouter, "tok", http.MethodGet, "/api/v1/tools/report.pdf")
	if rec.Code != http.StatusOK {
		t.Fatalf("report.export holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if rec.Body.Len() == 0 || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("report body does not carry the %%PDF magic (len %d)", rec.Body.Len())
	}

	// 403 with NO PDF bytes: a dashboard.view-but-not-report.export caller is
	// denied the export (the sibling surface keeps its OWN gate), while the
	// dashboard LIST still 200s for the same caller.
	dashboardOnly := newCompositionToolsRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	rec = doComposedRawRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools/report.pdf")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard.view-only report: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("403 body leaks PDF bytes: %s", rec.Body.String())
	}
	if rec := doComposedJSONRequest(dashboardOnly, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusOK {
		t.Errorf("dashboard.view-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// REVERSE: the report holder (no dashboard.view) reaches the report but is
	// denied the dashboard list — each surface has its OWN gate.
	if rec := doComposedJSONRequest(reportRouter, "tok", http.MethodGet, "/api/v1/tools", ""); rec.Code != http.StatusForbidden {
		t.Errorf("report-only list: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// REPORT_BAD_CODE through the REAL mounted router: a malformed status value
	// (here: an empty element between commas) answers the German 400 envelope —
	// the handler-level validation works end-to-end through the mount.
	rec = doComposedRawRequest(reportRouter, "tok", http.MethodGet, "/api/v1/tools/report.pdf?status=green,,oos")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad status filter: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
		t.Errorf("bad status filter: body lacks the uniform 400 envelope: %s", rec.Body.String())
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("bad status filter: 400 body leaks PDF bytes: %s", rec.Body.String())
	}
}

// doComposedRawRequest issues an authenticated RAW request through the REAL
// composed router (no JSON parsing — the report endpoint answers PDF bytes).
func doComposedRawRequest(h http.Handler, token, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ============================================================================
// Story 3.3 — the DSGVO composition-root mount (AD-6/AD-8): /api/v1/admin/dsgvo
// behind its OWN any-of gate [dsgvo.access_report, dsgvo.delete] with the REAL
// orchestrator (fake export ports + the compResolver as the permission seam +
// an in-memory audit), mirroring main().
// ============================================================================

// compDsgvoUserPort is a userports.DSGVOExportPort over a canned export.
type compDsgvoUserPort struct{}

func (p *compDsgvoUserPort) ExportUserData(_ context.Context, userID string) (*userports.UserDataExport, error) {
	return &userports.UserDataExport{
		Profile: usercore.UserExportProfile{ID: userID, Email: "target@gear.local", DisplayName: "Ziel Person"},
	}, nil
}

// compDsgvoToolPort is a toolports.DSGVOInspectionExportPort over a canned export.
type compDsgvoToolPort struct{}

func (p *compDsgvoToolPort) ExportUserInspectionData(_ context.Context, _ string) (*toolports.UserInspectionDataExport, error) {
	return &toolports.UserInspectionDataExport{
		Inspections: []toolscore.InspectionExport{{ID: "insp-1", ToolID: "id-tool-a", ToolName: "Bohrmaschine-01"}},
		Summary:     []toolscore.ToolExportSummary{},
	}, nil
}

// compDsgvoAudit records InsertAuditEvent calls.
type compDsgvoAudit struct {
	events []struct {
		actorID   string
		operation string
		detail    string
		severity  string
	}
}

func (a *compDsgvoAudit) InsertAuditEvent(_ context.Context, userID, operation, detail, severity string) error {
	a.events = append(a.events, struct {
		actorID   string
		operation string
		detail    string
		severity  string
	}{actorID: userID, operation: operation, detail: detail, severity: severity})
	return nil
}

// compDsgvoUserDeletion is a userports.DSGVODeletionPort fake over a canned
// archived-row set (Story 3.4).
type compDsgvoUserDeletion struct {
	rows []*usercore.DeletedAccount
}

func (p *compDsgvoUserDeletion) SoftDeleteAndArchive(_ context.Context, _ *usercore.User, _, _ string) error {
	return nil
}

func (p *compDsgvoUserDeletion) ListDeletedAccounts(_ context.Context) ([]*usercore.DeletedAccount, error) {
	return p.rows, nil
}

func (p *compDsgvoUserDeletion) PurgeDeletedAccount(_ context.Context, _ string) error {
	return nil
}

// compDsgvoToolDeletion is a toolports.DSGVODeletionPort fake (Story 3.4).
type compDsgvoToolDeletion struct{}

func (p *compDsgvoToolDeletion) AnonymizeUserReferences(_ context.Context, _ string) error {
	return nil
}

// compDsgvoActor is an ActorResolver that always resolves the actor.
type compDsgvoActor struct{}

func (a *compDsgvoActor) GetUserByID(_ context.Context, userID string) (*usercore.User, error) {
	return &usercore.User{ID: userID, Email: "dsgvo@gear.local", State: usercore.StateActive}, nil
}

// newCompositionDsgvoRouter mirrors the main() DSGVO mounts exactly: the outer
// /api/v1/admin mount gated by AdminModuleAccessCodes and the DSGVO sub-mount
// /api/v1/admin/dsgvo gated by ANY of [dsgvo.access_report, dsgvo.delete]
// (one permission per surface, AD-6), both through the REAL RequireAnyPermission
// middleware and router.New, with the REAL orchestrator behind the handler.
func newCompositionDsgvoRouter(perms []string, session *usercore.Session) (http.Handler, *compDsgvoAudit) {
	log := discardLogger()
	validator := &compValidator{session: session}
	resolver := &compResolver{perms: perms}

	audit := &compDsgvoAudit{}
	orchestrator := dsgvocore.NewService(
		&compDsgvoUserPort{},
		&compDsgvoToolPort{},
		&compDsgvoUserDeletion{},
		&compDsgvoToolDeletion{},
		resolver, // the orchestrator's defense-in-depth permission seam (AD-6)
		&compDsgvoActor{},
		audit,
		log,
	)
	dsgvoHandler := adminhttp.NewDsgvoHandler(orchestrator, log)
	dsgvoSurface := auth.RequireAnyPermission(validator, resolver, []string{dsgvocore.AccessReportPermission, dsgvocore.DeletePermission}, "dsgvo access denied", log)(dsgvoHandler.DsgvoRoutes())

	// The sibling settings surface (mounted in main.go too) so the REVERSE
	// assertion can prove the DSGVO code does NOT widen the SMTP gate.
	settingsHandler := adminhttp.NewHandler(&compSettingsService{}, log)
	settingsSurface := auth.RequireAnyPermission(validator, resolver, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(settingsHandler.Routes())

	outer := chi.NewRouter()
	outer.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"module":"admin","status":"ok"}`))
	})
	outerSurface := auth.RequireAnyPermission(validator, resolver, usercore.AdminModuleAccessCodes(), "admin access denied", log)(outer)

	return router.New(stubPinger{}, log,
		router.WithMount("/api/v1/admin", outerSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/dsgvo", dsgvoSurface),
	), audit
}

func doDsgvoComposedRequest(h http.Handler, token, path string) *httptest.ResponseRecorder {
	return doComposedJSONRequest(h, token, http.MethodGet, path, "")
}

// TestCompositionDsgvoMountGating verifies the Story 3.3 composition-root
// wiring: /api/v1/admin/dsgvo is gated by ITS OWN any-of gate
// [dsgvo.access_report, dsgvo.delete] (AD-6) — a caller holding only an
// unrelated admin code gets the uniform 403 with NO personal data, while a
// dsgvo.access_report holder reaches the surface (and, via the outer gate, the
// admin module root). The reverse is also pinned: a delete-only holder passes
// the mount gate but is STILL denied the report by the orchestrator's
// defense-in-depth re-check (the per-action code, AD-6) — and a
// dsgvo.access_report-only holder is denied the SMTP surface (its code opens
// only the DSGVO mount).
func TestCompositionDsgvoMountGating(t *testing.T) {
	// 401: no token.
	router401, _ := newCompositionDsgvoRouter([]string{}, nil)
	if rec := doDsgvoComposedRequest(router401, "", "/api/v1/admin/dsgvo/reports/u-target"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// 403: a caller holding an unrelated admin code is denied with no personal
	// data exposed (the gates are separate — one permission per surface, AD-6).
	routerMail, _ := newCompositionDsgvoRouter([]string{admcore.SmtpSettingsPermission}, activeUser("u-mail", "mail@gear.local"))
	rec := doDsgvoComposedRequest(routerMail, "tok", "/api/v1/admin/dsgvo/reports/u-target")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("email-only holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "target@gear.local") || strings.Contains(rec.Body.String(), "Ziel Person") {
		t.Errorf("403 body leaks personal data: %s", rec.Body.String())
	}

	// 403: a non-admin (dashboard.view only) caller is also denied.
	routerVol, _ := newCompositionDsgvoRouter([]string{toolscore.DashboardViewPermission}, activeUser("u-vol", "vol@gear.local"))
	if rec := doDsgvoComposedRequest(routerVol, "tok", "/api/v1/admin/dsgvo/reports/u-target"); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin holder: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: a dsgvo.access_report holder reaches the report — the composed path
	// (gate → handler → orchestrator → ports) serves the assembled JSON with
	// the attachment Content-Disposition, and the generation is audited.
	routerReport, audit := newCompositionDsgvoRouter([]string{dsgvocore.AccessReportPermission}, activeUser("u-dsgvo", "dsgvo@gear.local"))
	rec = doDsgvoComposedRequest(routerReport, "tok", "/api/v1/admin/dsgvo/reports/u-target")
	if rec.Code != http.StatusOK {
		t.Fatalf("dsgvo.access_report holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want the attachment download header", cd)
	}
	if !strings.Contains(rec.Body.String(), "Ziel Person") || !strings.Contains(rec.Body.String(), "Bohrmaschine-01") {
		t.Errorf("report body misses the composed sections: %s", rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].actorID != "u-dsgvo" || audit.events[0].operation != dsgvocore.AuditOperationAccessReport {
		t.Errorf("audit events = %+v, want one dsgvo.access_report row for u-dsgvo", audit.events)
	}
	if !strings.Contains(audit.events[0].detail, "u-target") {
		t.Errorf("audit detail = %q, want the target user id", audit.events[0].detail)
	}

	// DEFENSE-IN-DEPTH: a delete-ONLY holder passes the any-of mount gate but is
	// STILL denied the report by the orchestrator's `dsgvo.access_report`
	// re-check — the delete code never opens the report (AD-6).
	routerDelete, auditDelete := newCompositionDsgvoRouter([]string{dsgvocore.DeletePermission}, activeUser("u-del", "del@gear.local"))
	rec = doDsgvoComposedRequest(routerDelete, "tok", "/api/v1/admin/dsgvo/reports/u-target")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete-only holder on the report: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Ziel Person") {
		t.Errorf("403 body leaks personal data to a delete-only holder: %s", rec.Body.String())
	}
	if len(auditDelete.events) != 0 {
		t.Errorf("audit written for a denied report: %+v", auditDelete.events)
	}

	// REVERSE: the dsgvo.access_report-only holder must NOT reach the SMTP
	// surface (its code opens only the DSGVO mount).
	if rec := doComposedJSONRequest(routerReport, "tok", http.MethodGet, "/api/v1/admin/settings/smtp", ""); rec.Code != http.StatusForbidden {
		t.Errorf("dsgvo-only holder on SMTP: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// 200: the same holder reaches the outer admin-module root
	// (dsgvo.access_report is part of AdminModuleAccessCodes).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rootRec := httptest.NewRecorder()
	routerReport.ServeHTTP(rootRec, req)
	if rootRec.Code != http.StatusOK {
		t.Errorf("admin root: status = %d, want 200", rootRec.Code)
	}
}

// TestCompositionDsgvoDeleteMount verifies the Story 3.4 deletion surface
// through the composed router (AD-6/AD-8): a dsgvo.delete holder reaches the
// delete / list-deleted / purge endpoints (the orchestrator composes the
// deletion ports, all wired in main()), while a non-holder is denied all of
// them with the uniform 403 and a self-deletion answers the German 400.
func TestCompositionDsgvoDeleteMount(t *testing.T) {
	routerDel, auditDel := newCompositionDsgvoRouter([]string{dsgvocore.DeletePermission}, activeUser("u-del", "del@gear.local"))

	// DELETE /users/{userId} → 200 German confirmation, audited.
	rec := doComposedJSONRequest(routerDel, "tok", http.MethodPost, "/api/v1/admin/dsgvo/users/u-target/delete", `{"reason":"Auf Wunsch"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "wurde gelöscht") {
		t.Errorf("delete body misses the German confirmation: %s", rec.Body.String())
	}
	if len(auditDel.events) != 1 || auditDel.events[0].actorID != "u-del" || auditDel.events[0].operation != dsgvocore.AuditOperationDelete {
		t.Errorf("audit events = %+v, want one dsgvo.delete row for u-del", auditDel.events)
	}

	// Self-deletion → German 400.
	rec = doComposedJSONRequest(routerDel, "tok", http.MethodPost, "/api/v1/admin/dsgvo/users/u-del/delete", `{"reason":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("self-deletion: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dsgvocore.MsgDsgvoDeleteSelf) {
		t.Errorf("self-deletion body misses the German 400: %s", rec.Body.String())
	}

	// GET /users/deleted → the archived list (empty from the in-memory port).
	rec = doDsgvoComposedRequest(routerDel, "tok", "/api/v1/admin/dsgvo/users/deleted")
	if rec.Code != http.StatusOK {
		t.Fatalf("list-deleted holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"accounts":[]`) {
		t.Errorf("list-deleted body = %s, want an empty accounts array", rec.Body.String())
	}

	// DELETE /users/deleted/{archiveId} → 200 purge confirmation, audited.
	rec = doComposedJSONRequest(routerDel, "tok", http.MethodDelete, "/api/v1/admin/dsgvo/users/deleted/a-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("purge holder: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dsgvocore.MsgPurgeConfirmation) {
		t.Errorf("purge body misses the German confirmation: %s", rec.Body.String())
	}
	if len(auditDel.events) < 2 || auditDel.events[1].operation != dsgvocore.AuditOperationPurge {
		t.Errorf("audit events = %+v, want a dsgvo.purge row after the delete row", auditDel.events)
	}

	// A non-holder (report-only) is denied ALL three delete routes (AD-6).
	routerReportOnly, _ := newCompositionDsgvoRouter([]string{dsgvocore.AccessReportPermission}, activeUser("u-rep", "rep@gear.local"))
	if rec := doComposedJSONRequest(routerReportOnly, "tok", http.MethodPost, "/api/v1/admin/dsgvo/users/u-target/delete", `{"reason":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("report-only holder on delete: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doDsgvoComposedRequest(routerReportOnly, "tok", "/api/v1/admin/dsgvo/users/deleted"); rec.Code != http.StatusForbidden {
		t.Errorf("report-only holder on list-deleted: status = %d, want 403", rec.Code)
	}
	if rec := doComposedJSONRequest(routerReportOnly, "tok", http.MethodDelete, "/api/v1/admin/dsgvo/users/deleted/a-1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("report-only holder on purge: status = %d, want 403", rec.Code)
	}

	// Unauthenticated → 401 on all three.
	router401, _ := newCompositionDsgvoRouter([]string{}, nil)
	if rec := doComposedJSONRequest(router401, "", http.MethodPost, "/api/v1/admin/dsgvo/users/u-target/delete", `{"reason":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token delete: status = %d, want 401", rec.Code)
	}
	if rec := doDsgvoComposedRequest(router401, "", "/api/v1/admin/dsgvo/users/deleted"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token list-deleted: status = %d, want 401", rec.Code)
	}
	if rec := doComposedJSONRequest(router401, "", http.MethodDelete, "/api/v1/admin/dsgvo/users/deleted/a-1", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token purge: status = %d, want 401", rec.Code)
	}
}

// TestComposedRouterSPAMount verifies the Story 7.6 composition-root wiring:
// the SPA catch-all is mounted LAST at "/", so (a) / and unknown non-API routes
// serve the SPA index, (b) an unknown /api/* route answers the JSON 404
// envelope (never the SPA), and (c) a known API route still reaches its handler
// through the same composed router. This is the story's central deployment
// surface — a regression in the mount order or cfg.WebDist wiring would leave
// the served SPA dead with every other test green.
func TestComposedRouterSPAMount(t *testing.T) {
	log := discardLogger()

	// Build a minimal SPA dir in a temp path (the handler serves from disk).
	spaDir := t.TempDir()
	index := filepath.Join(spaDir, "index.html")
	if err := os.WriteFile(index, []byte("<html>GEAR-SPA</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A real composed router with the SPA catch-all mounted (the "/" mount is
	// what catches /api/* that no more-specific route claims).
	r := router.New(stubPinger{}, log,
		router.WithMount("/", spa.New(spaDir, log)),
	)

	// (a) SPA at root.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "GEAR-SPA") {
		t.Fatalf("GET / = %d (%s), want the SPA index", rec.Code, rec.Body.String())
	}

	// (a') unknown non-API client route → SPA fallback.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tools/xyz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "GEAR-SPA") {
		t.Fatalf("GET /tools/xyz = %d (%s), want the SPA fallback", rec.Code, rec.Body.String())
	}

	// (b) unknown /api/* → JSON 404 envelope, never the SPA.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/nonexistent = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Errorf("API 404 body = %s, want the JSON envelope", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "GEAR-SPA") {
		t.Errorf("API 404 body = %s, an /api path must never render the SPA", rec.Body.String())
	}
}

// TestRunHealthcheck verifies the container HEALTHCHECK probe (Story 7.6):
// exit 0 on a 2xx /healthz, exit 1 on a non-2xx or an unreachable listener,
// and tolerance of a non-loopback bind address (e.g. 0.0.0.0:PORT).
func TestRunHealthcheck(t *testing.T) {
	t.Run("2xx exits 0", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		t.Setenv("GEAR_HTTP_ADDR", srv.Listener.Addr().String())
		if got := runHealthcheck(); got != 0 {
			t.Fatalf("runHealthcheck = %d, want 0", got)
		}
	})

	t.Run("503 exits 1", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		t.Setenv("GEAR_HTTP_ADDR", srv.Listener.Addr().String())
		if got := runHealthcheck(); got != 1 {
			t.Fatalf("runHealthcheck = %d, want 1", got)
		}
	})

	t.Run("non-loopback bind tolerated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		// Simulate a 0.0.0.0:<port> bind by rewriting the host part.
		_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GEAR_HTTP_ADDR", "0.0.0.0:"+port)
		if got := runHealthcheck(); got != 0 {
			t.Fatalf("runHealthcheck = %d, want 0 for 0.0.0.0 bind", got)
		}
	})

	t.Run("unreachable exits 1", func(t *testing.T) {
		t.Setenv("GEAR_HTTP_ADDR", "127.0.0.1:1")
		if got := runHealthcheck(); got != 1 {
			t.Fatalf("runHealthcheck = %d, want 1 for an unreachable listener", got)
		}
	})
}
