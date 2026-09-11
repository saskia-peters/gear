package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	adminhttp "github.com/saskia-peters/gear/internal/admin/adapters/http"
	admcore "github.com/saskia-peters/gear/internal/admin/core"
	adminports "github.com/saskia-peters/gear/internal/admin/ports"
	admsmtp "github.com/saskia-peters/gear/internal/admin/adapters/smtp"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/platform/router"
	toolhttp "github.com/saskia-peters/gear/internal/tools/adapters/http"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
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

	toolHandler := toolhttp.NewHandler(&compToolTypeService{}, log)
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
