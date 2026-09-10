package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeStore is an in-memory SmtpSettingsStore.
type fakeStore struct {
	settings *SmtpSettings
	err      error
	upserts  []*SmtpSettings
}

func (f *fakeStore) GetSmtpSettings(context.Context) (*SmtpSettings, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.settings, nil
}

// UpsertSmtpSettings emulates the store's atomic COALESCE keep-existing
// semantics: an empty incoming password keeps the existing ciphertext (as the
// single SQL statement does), a non-empty one replaces it. It returns the
// resulting row.
func (f *fakeStore) UpsertSmtpSettings(_ context.Context, s *SmtpSettings) (*SmtpSettings, error) {
	if f.err != nil {
		return nil, f.err
	}
	persisted := *s
	if s.PasswordEncrypted == "" && f.settings != nil {
		persisted.PasswordEncrypted = f.settings.PasswordEncrypted
	}
	f.upserts = append(f.upserts, &persisted)
	f.settings = &persisted
	return &persisted, nil
}

// fakeCipher round-trips with a prefix so tests can distinguish ciphertext.
type fakeCipher struct {
	decryptErr error
}

func (f *fakeCipher) Encrypt(plaintext string) (string, error) { return "enc:" + plaintext, nil }
func (f *fakeCipher) Decrypt(encoded string) (string, error) {
	if f.decryptErr != nil {
		return "", f.decryptErr
	}
	if !strings.HasPrefix(encoded, "enc:") {
		return "", ErrInvalidCiphertext
	}
	return strings.TrimPrefix(encoded, "enc:"), nil
}

// ErrInvalidCiphertext mirrors the crypto adapter's sentinel for the fake.
var ErrInvalidCiphertext = errors.New("invalid ciphertext")

// fakePerms is a PermissionResolver with a fixed set.
type fakePerms struct {
	perms []string
	err   error
}

func (f *fakePerms) ListPermissionsByUser(context.Context, string) ([]string, error) {
	return f.perms, f.err
}

// fakeAudit records InsertAuditEvent calls.
type fakeAudit struct {
	events []auditEvent
	err    error
}

type auditEvent struct {
	actorID   string
	operation string
	detail    string
	severity  string
}

func (f *fakeAudit) InsertAuditEvent(_ context.Context, userID, operation, detail, severity string) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, auditEvent{actorID: userID, operation: operation, detail: detail, severity: severity})
	return nil
}

// fakeMailer records SmtpSendParams and can fail.
type fakeMailer struct {
	params []SmtpSendParams
	err    error
}

func (f *fakeMailer) SendEmail(_ context.Context, params SmtpSendParams) error {
	if f.err != nil {
		return f.err
	}
	f.params = append(f.params, params)
	return nil
}

// newTestService wires the fakes around a fresh Service.
func newTestService() (*Service, *fakeStore, *fakePerms, *fakeAudit, *fakeMailer) {
	store := &fakeStore{}
	perms := &fakePerms{perms: []string{SmtpSettingsPermission}}
	audit := &fakeAudit{}
	mailer := &fakeMailer{}
	svc := NewService(store, nil, &fakeCipher{}, perms, audit, mailer, nil, nil)
	return svc, store, perms, audit, mailer
}

const actorID = "u-admin"

func TestGetSmtpSettingsInitial(t *testing.T) {
	// GET_INITIAL: no settings row yet → zero defaults + password_configured=false.
	svc, _, _, _, _ := newTestService()
	got, err := svc.GetSmtpSettings(context.Background(), actorID)
	if err != nil {
		t.Fatalf("GetSmtpSettings err = %v", err)
	}
	if got.Host != "" || got.Port != 0 || got.Security != "" || got.SenderAddress != "" {
		t.Errorf("defaults = %+v, want zero defaults", got)
	}
	if got.PasswordConfigured() {
		t.Error("password_configured = true, want false for no row")
	}
}

func TestGetSmtpSettingsForbidden(t *testing.T) {
	// PUT_FORBIDDEN: caller without admin.settings.email → ErrForbidden.
	svc, _, perms, _, _ := newTestService()
	perms.perms = []string{"dashboard.view"}
	if _, err := svc.GetSmtpSettings(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateSmtpSettingsValid(t *testing.T) {
	// PUT_VALID: full settings incl. password → persisted, password encrypted
	// at rest, applied immediately, audited.
	svc, store, _, audit, _ := newTestService()
	password := "geheim123"
	input := UpdateSmtpSettingsInput{
		Host: "  smtp.example.com ", Port: 587, Security: SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", SenderName: "G.E.A.R. OV",
		Username: "smtpuser", Password: &password,
	}
	got, err := svc.UpdateSmtpSettings(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("UpdateSmtpSettings err = %v", err)
	}
	if got.Host != "smtp.example.com" {
		t.Errorf("host = %q, want trimmed", got.Host)
	}
	if got.PasswordEncrypted != "enc:"+password {
		t.Errorf("password_encrypted = %q, want encrypted ciphertext", got.PasswordEncrypted)
	}
	if len(store.upserts) != 1 || store.upserts[0].PasswordEncrypted != "enc:"+password {
		t.Errorf("upserted settings = %+v, want one encrypted write", store.upserts)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationSmtpSettingsUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
	if audit.events[0].actorID != actorID || audit.events[0].severity != AuditSeverityNormal {
		t.Errorf("audit actor/severity = %+v", audit.events[0])
	}
}

func TestUpdateSmtpSettingsNoPasswordKeepsExisting(t *testing.T) {
	// PUT_NO_PASSWORD: edit without password field → existing encrypted
	// password kept (atomically in the store's COALESCE), other fields updated.
	svc, store, _, _, _ := newTestService()
	store.settings = &SmtpSettings{PasswordEncrypted: "enc:alte-secret"}
	existing := store.settings.PasswordEncrypted

	input := UpdateSmtpSettingsInput{
		Host: "mail.example.com", Port: 465, Security: SmtpSecurityTLS,
		SenderAddress: "noreply@example.com",
	}
	got, err := svc.UpdateSmtpSettings(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("UpdateSmtpSettings err = %v", err)
	}
	if got.PasswordEncrypted != existing {
		t.Errorf("password_encrypted = %q, want kept %q", got.PasswordEncrypted, existing)
	}
	if got.Host != "mail.example.com" || got.Port != 465 || got.Security != SmtpSecurityTLS {
		t.Errorf("updated fields = %+v", got)
	}
	// The returned row is the persisted one (password_configured reflects the
	// kept ciphertext).
	if !got.PasswordConfigured() {
		t.Error("password_configured = false, want true (existing password kept)")
	}
}

func TestUpdateSmtpSettingsWhitespacePasswordKeepsExisting(t *testing.T) {
	// PUT with a whitespace-only password is treated as absent (finding): the
	// existing encrypted password is kept, never stored as configured.
	svc, store, _, _, _ := newTestService()
	store.settings = &SmtpSettings{PasswordEncrypted: "enc:alte-secret"}
	existing := store.settings.PasswordEncrypted

	ws := "   \t  "
	input := UpdateSmtpSettingsInput{
		Host: "mail.example.com", Port: 587, Security: SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", Password: &ws,
	}
	got, err := svc.UpdateSmtpSettings(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("UpdateSmtpSettings err = %v", err)
	}
	if got.PasswordEncrypted != existing {
		t.Errorf("password_encrypted = %q, want kept %q (whitespace treated as absent)", got.PasswordEncrypted, existing)
	}
	if got.PasswordConfigured() != true {
		t.Error("password_configured = false, want true (existing password kept)")
	}
}

func TestUpdateSmtpSettingsInvalid(t *testing.T) {
	// PUT_INVALID: bad security / empty host / bad port / header-injection
	// chars → 400-class typed error.
	cases := []struct {
		name    string
		mutate  func(*UpdateSmtpSettingsInput)
		wantMsg string
	}{
		{"empty host", func(i *UpdateSmtpSettingsInput) { i.Host = "  " }, "SMTP-Host"},
		{"host newline", func(i *UpdateSmtpSettingsInput) { i.Host = "host\r\nX" }, "ungültige Zeichen"},
		{"bad security", func(i *UpdateSmtpSettingsInput) { i.Security = "ssl" }, "Verschlüsselungsoption"},
		{"bad port", func(i *UpdateSmtpSettingsInput) { i.Port = 0 }, "Port"},
		{"bad port high", func(i *UpdateSmtpSettingsInput) { i.Port = 70000 }, "Port"},
		{"empty sender", func(i *UpdateSmtpSettingsInput) { i.SenderAddress = "" }, "Absenderadresse"},
		{"malformed sender", func(i *UpdateSmtpSettingsInput) { i.SenderAddress = "nope" }, "Absenderadresse"},
		{"sender newline", func(i *UpdateSmtpSettingsInput) { i.SenderAddress = "a@b.de\r\nX" }, "ungültige Zeichen"},
		{"sender name newline", func(i *UpdateSmtpSettingsInput) { i.SenderName = "G.E.A.R.\nX" }, "ungültige Zeichen"},
		{"oversized password", func(i *UpdateSmtpSettingsInput) { i.Password = strPtr(strings.Repeat("x", 1025)) }, "Passwort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _, _ := newTestService()
			input := UpdateSmtpSettingsInput{
				Host: "mail.example.com", Port: 25, Security: SmtpSecurityNone,
				SenderAddress: "noreply@example.com",
			}
			tc.mutate(&input)
			_, err := svc.UpdateSmtpSettings(context.Background(), actorID, input)
			var inv *InvalidSmtpSettingsError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidSmtpSettingsError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrSmtpSettingsInvalid) {
				t.Errorf("err = %v, want unwraps to ErrSmtpSettingsInvalid", err)
			}
		})
	}
}

func TestUpdateSmtpSettingsForbidden(t *testing.T) {
	svc, _, perms, _, _ := newTestService()
	perms.perms = []string{"dashboard.view"}
	input := UpdateSmtpSettingsInput{
		Host: "mail.example.com", Port: 25, Security: SmtpSecurityNone,
		SenderAddress: "noreply@example.com",
	}
	if _, err := svc.UpdateSmtpSettings(context.Background(), actorID, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestTestSmtpSettingsOK(t *testing.T) {
	// TEST_OK: configured server, admin sends test → delivered to admin email,
	// inline success, audited.
	svc, store, _, audit, mailer := newTestService()
	store.settings = &SmtpSettings{
		Host: "smtp.example.com", Port: 587, Security: SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", Username: "u", PasswordEncrypted: "enc:pw",
	}
	res, err := svc.TestSmtpSettings(context.Background(), actorID, "admin@example.com")
	if err != nil {
		t.Fatalf("TestSmtpSettings err = %v", err)
	}
	if !res.Ok || res.Message != MsgSmtpTestSent {
		t.Errorf("result = %+v, want ok + %q", res, MsgSmtpTestSent)
	}
	if len(mailer.params) != 1 {
		t.Fatalf("sent emails = %d, want 1", len(mailer.params))
	}
	params := mailer.params[0]
	if params.To != "admin@example.com" || params.From != "noreply@example.com" {
		t.Errorf("from/to = %q/%q", params.From, params.To)
	}
	if params.Password != "pw" {
		t.Errorf("plaintext password = %q, want decrypted in-memory value", params.Password)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationSmtpSettingsTest {
		t.Fatalf("audit events = %+v, want one test audit", audit.events)
	}
	if !strings.Contains(audit.events[0].detail, "result=ok") {
		t.Errorf("audit detail = %q, want result=ok", audit.events[0].detail)
	}
}

func TestTestSmtpSettingsNotConfigured(t *testing.T) {
	// GET_INITIAL-style: no row → inline German "not configured", audited.
	svc, _, _, audit, mailer := newTestService()
	res, err := svc.TestSmtpSettings(context.Background(), actorID, "admin@example.com")
	if err != nil {
		t.Fatalf("TestSmtpSettings err = %v", err)
	}
	if res.Ok || res.Message != MsgSmtpNotConfigured {
		t.Errorf("result = %+v, want not-configured", res)
	}
	if len(mailer.params) != 0 {
		t.Error("no email must be sent when not configured")
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "not_configured") {
		t.Errorf("audit = %+v, want skipped/not_configured", audit.events)
	}
}

func TestTestSmtpSettingsDecryptFail(t *testing.T) {
	// DECRYPT_FAIL: stored ciphertext unreadable → German error, audited.
	svc, store, _, audit, _ := newTestService()
	store.settings = &SmtpSettings{
		Host: "smtp.example.com", Port: 25, Security: SmtpSecurityNone,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "kaputt",
	}
	svc.cipher = &fakeCipher{decryptErr: ErrInvalidCiphertext}
	res, err := svc.TestSmtpSettings(context.Background(), actorID, "admin@example.com")
	if err != nil {
		t.Fatalf("TestSmtpSettings err = %v", err)
	}
	if res.Ok || res.Message != MsgSmtpPasswordInvalid {
		t.Errorf("result = %+v, want %q", res, MsgSmtpPasswordInvalid)
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "decrypt") {
		t.Errorf("audit = %+v, want failed/decrypt", audit.events)
	}
}

func TestTestSmtpSettingsSendFail(t *testing.T) {
	// TEST_FAIL: SMTP unreachable → GENERIC inline German error (the detailed
	// engine error is logged structured, never surfaced — finding 5).
	svc, store, _, audit, mailer := newTestService()
	store.settings = &SmtpSettings{
		Host: "smtp.example.com", Port: 25, Security: SmtpSecurityNone,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}
	mailer.err = errors.New("connection refused")
	res, err := svc.TestSmtpSettings(context.Background(), actorID, "admin@example.com")
	if err != nil {
		t.Fatalf("TestSmtpSettings err = %v", err)
	}
	if res.Ok {
		t.Error("result.Ok = true, want false")
	}
	if res.Message != MsgSmtpTestFailed {
		t.Errorf("message = %q, want generic %q (no raw engine detail)", res.Message, MsgSmtpTestFailed)
	}
	if strings.Contains(res.Message, "connection refused") || strings.Contains(res.Message, "smtp.example.com") {
		t.Errorf("message leaks engine detail: %q", res.Message)
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "result=failed") {
		t.Errorf("audit = %+v, want result=failed", audit.events)
	}
}

func TestTestSmtpSettingsAnonymousRelay(t *testing.T) {
	// A password-less config (none/starttls, no username) is usable: the test
	// send works with an empty password (finding 3).
	svc, store, _, _, mailer := newTestService()
	store.settings = &SmtpSettings{
		Host: "smtp.example.com", Port: 25, Security: SmtpSecurityNone,
		SenderAddress: "noreply@example.com",
	}
	res, err := svc.TestSmtpSettings(context.Background(), actorID, "admin@example.com")
	if err != nil {
		t.Fatalf("TestSmtpSettings err = %v", err)
	}
	if !res.Ok || res.Message != MsgSmtpTestSent {
		t.Errorf("result = %+v, want ok + %q", res, MsgSmtpTestSent)
	}
	if len(mailer.params) != 1 {
		t.Fatalf("sent emails = %d, want 1", len(mailer.params))
	}
	if mailer.params[0].Password != "" {
		t.Errorf("anonymous relay must send with an empty password, got %q", mailer.params[0].Password)
	}
}

func TestDeliveryUsable(t *testing.T) {
	base := &SmtpSettings{Host: "smtp.example.com", Port: 25, Security: SmtpSecurityNone, SenderAddress: "noreply@example.com"}
	cases := []struct {
		name string
		set  func(*SmtpSettings)
		want bool
	}{
		{"empty host", func(s *SmtpSettings) { s.Host = "" }, false},
		{"empty sender", func(s *SmtpSettings) { s.SenderAddress = "" }, false},
		{"none no username no password", func(s *SmtpSettings) {}, true},
		{"starttls no username no password", func(s *SmtpSettings) { s.Security = SmtpSecurityStartTLS }, true},
		{"tls no password", func(s *SmtpSettings) { s.Security = SmtpSecurityTLS }, false},
		{"none with username no password", func(s *SmtpSettings) { s.Username = "u" }, false},
		{"none with username + password", func(s *SmtpSettings) { s.Username = "u"; s.PasswordEncrypted = "enc:pw" }, true},
		{"tls with password", func(s *SmtpSettings) { s.Security = SmtpSecurityTLS; s.PasswordEncrypted = "enc:pw" }, true},
		{"invalid security", func(s *SmtpSettings) { s.Security = "ssl" }, false},
	}
	if (*SmtpSettings)(nil).DeliveryUsable() {
		t.Error("DeliveryUsable(nil) = true, want false")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := *base
			tc.set(&s)
			if got := s.DeliveryUsable(); got != tc.want {
				t.Errorf("DeliveryUsable(%+v) = %v, want %v", s, got, tc.want)
			}
		})
	}
}

func TestCurrentSmtpSettingsReadOnlyPort(t *testing.T) {
	// The read-only port (AD-14): returns live row incl. ciphertext for the
	// sender, no permission check.
	svc, store, _, _, _ := newTestService()
	store.settings = &SmtpSettings{Host: "mail.example.com", PasswordEncrypted: "enc:pw"}
	got, err := svc.CurrentSmtpSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentSmtpSettings err = %v", err)
	}
	if got.PasswordEncrypted != "enc:pw" {
		t.Errorf("ciphertext = %q, want carried for in-memory decrypt", got.PasswordEncrypted)
	}

	// No row → zero defaults.
	store.settings = nil
	got, err = svc.CurrentSmtpSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentSmtpSettings err = %v", err)
	}
	if got.Host != "" || got.PasswordConfigured() {
		t.Errorf("defaults = %+v, want zero + not configured", got)
	}
}

func strPtr(s string) *string { return &s }