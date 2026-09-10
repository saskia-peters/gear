package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// SMTP connection-security modes (FR-28). Stored verbatim in smtp_settings.
const (
	SmtpSecurityNone     = "none"
	SmtpSecurityStartTLS = "starttls"
	SmtpSecurityTLS      = "tls"
)

// SmtpSettingsPermission is the server-authoritative gate code for the whole
// SMTP-settings surface (FR-28/AD-6). One Go const so the route mount, the
// core re-check and the SPA-facing documentation never drift.
const SmtpSettingsPermission = "admin.settings.email"

// Audit-operation tags for the SMTP-settings surface (NFR-O1/NFR-O2): settings
// updates and test-sends are audited with actor, timestamp and operation.
const (
	AuditOperationSmtpSettingsUpdate = "admin.settings.email.update"
	AuditOperationSmtpSettingsTest   = "admin.settings.email.test"
)

// AuditSeverityNormal is the standard audit severity for settings events.
const AuditSeverityNormal = "normal"

// ErrForbidden is returned when the acting user's live permission set does
// not hold admin.settings.email (defense-in-depth, AD-6). Handlers map it to
// the uniform 403 with no hint of what is missing.
var ErrForbidden = errors.New("admin core: forbidden")

// ErrSmtpSettingsInvalid is the sentinel wrapping a German validation message
// for a 400 invalid_request (empty host, bad security value, invalid port,
// malformed sender address, empty/oversized provided password).
var ErrSmtpSettingsInvalid = errors.New("admin core: invalid smtp settings")

// InvalidSmtpSettingsError carries the German validation message for a 400
// invalid_request. It unwraps to ErrSmtpSettingsInvalid so callers can match
// the sentinel while still rendering the field-specific microcopy.
type InvalidSmtpSettingsError struct {
	Message string
}

func (e *InvalidSmtpSettingsError) Error() string { return e.Message }
func (e *InvalidSmtpSettingsError) Unwrap() error { return ErrSmtpSettingsInvalid }

// German microcopy for the SMTP-settings surface (FR-28/UX-DR8).
const (
	MsgSmtpSettingsSaved   = "SMTP-Einstellungen gespeichert."
	MsgSmtpTestSent        = "Test-E-Mail erfolgreich gesendet."
	MsgSmtpNotConfigured   = "E-Mail-Versand ist nicht konfiguriert."
	MsgSmtpPasswordInvalid = "Das gespeicherte SMTP-Passwort kann nicht entschlüsselt werden."
	// MsgSmtpTestFailed is the generic inline message for a test-send failure.
	// The detailed engine/TLS/cert error is logged structured (NFR-O1) and
	// NEVER surfaced to the client (no SMTP/host/TLS detail leaks).
	MsgSmtpTestFailed = "Die Test-E-Mail konnte nicht gesendet werden."
)

// SmtpSettings is the domain representation of the single SMTP-settings row
// (FR-28). PasswordEncrypted is the AES-256-GCM ciphertext stored at rest
// (NFR-S4) — it is carried only inside the module and consumed in memory by
// the sender/test-send path; the HTTP surface renders PasswordConfigured()
// instead and never serializes the ciphertext.
type SmtpSettings struct {
	Host              string
	Port              int
	Security          string
	SenderAddress     string
	SenderName        string
	Username          string
	PasswordEncrypted string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PasswordConfigured reports whether a password is present: write-only
// semantics — the boolean is what a client may learn, never the password
// itself (NFR-S4).
func (s *SmtpSettings) PasswordConfigured() bool {
	return s != nil && s.PasswordEncrypted != ""
}

// DeliveryUsable reports whether the settings can actually deliver email
// (FR-28/AD-14): a valid row with a host and sender address, where any
// authentication the config would require is satisfiable. An anonymous
// no-auth relay (security none/starttls, no username) needs NO password; a
// username (or implicit-TLS) config requires a configured password. This is
// the single decision point used by Configured(), the reset sender and the
// test-send path so all three stay consistent.
func (s *SmtpSettings) DeliveryUsable() bool {
	if s == nil || s.Host == "" || s.SenderAddress == "" {
		return false
	}
	switch s.Security {
	case SmtpSecurityNone, SmtpSecurityStartTLS:
		if s.Username == "" {
			// Anonymous no-auth relay: no password required.
			return true
		}
	case SmtpSecurityTLS:
		// Implicit TLS always requires a password in this design.
	default:
		return false
	}
	return s.PasswordConfigured()
}

// UpdateSmtpSettingsInput is the PUT body. Password is a pointer so an absent
// field (nil) is distinct from an explicitly provided one: absent (or
// blank/whitespace-only) keeps the existing encrypted password, present is
// encrypted at rest and replaces it (write-only edit).
type UpdateSmtpSettingsInput struct {
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Security      string  `json:"security"`
	SenderAddress string  `json:"sender_address"`
	SenderName    string  `json:"sender_name"`
	Username      string  `json:"username"`
	Password      *string `json:"password,omitempty"`
}

// SmtpTestResult is the inline outcome of the Sendetest-E-Mail action: ok
// plus the server-authoritative German message. SMTP failures are a 200-style
// result (never a generic 5xx), so the SPA renders the German error inline.
type SmtpTestResult struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
}

// SmtpSendParams is the live, already-decrypted connection+message input for
// one SMTP send (in-memory only — the plaintext password never leaves it).
// SenderName is the display name rendered into the RFC 5322 From header; the
// From field carries the bare envelope address for MAIL FROM.
type SmtpSendParams struct {
	Host       string
	Port       int
	Security   string
	Username   string
	Password   string
	From       string
	SenderName string
	To         string
	Subject    string
	Body       string
}

// SmtpSettingsStore is the outbound persistence port over the Admin-owned
// smtp_settings table (AD-11). GetSmtpSettings returns nil when no row exists.
type SmtpSettingsStore interface {
	GetSmtpSettings(ctx context.Context) (*SmtpSettings, error)
	// UpsertSmtpSettings writes the settings atomically and returns the
	// resulting row. An empty settings.PasswordEncrypted KEEPS the existing
	// ciphertext in the same statement (COALESCE), so keep-existing is atomic —
	// there is no read-then-write window a concurrent update could race.
	UpsertSmtpSettings(ctx context.Context, settings *SmtpSettings) (*SmtpSettings, error)
}

// SecretCipher seals/unseals the SMTP password at rest (NFR-S4): the same
// AES-256-GCM contract the User module uses for TOTP secrets. The concrete
// adapter is internal/platform/crypto.SecretCipher wired at the composition
// root.
type SecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// PermissionResolver resolves a user's live permission set (AD-12) for the
// defense-in-depth re-check. The User module's postgres repository implements
// it (ListPermissionsByUser).
type PermissionResolver interface {
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
}

// AuditWriter appends to the User-owned audit trail (NFR-O1/NFR-O2). The User
// module's postgres repository implements it; the Admin module never authors
// another module's SQL (AD-8/AD-11).
type AuditWriter interface {
	InsertAuditEvent(ctx context.Context, userID, operation, detail, severity string) error
}

// SmtpMailer is the outbound email-delivery port used by the test-send path.
// The concrete adapter is internal/admin/adapters/smtp.Client (stdlib
// net/smtp + crypto/tls, no third-party dependency).
type SmtpMailer interface {
	SendEmail(ctx context.Context, params SmtpSendParams) error
}

// Service is the Admin module's SMTP-settings domain service (Story 3.1).
type Service struct {
	store  SmtpSettingsStore
	cipher SecretCipher
	perms  PermissionResolver
	audit  AuditWriter
	mailer SmtpMailer
	logger *slog.Logger
}

// NewService constructs the SMTP-settings service. logger may be nil (falls
// back to slog.Default()); it is used for structured logging of audit-write
// failures and test-send outcomes (NFR-O1).
func NewService(store SmtpSettingsStore, cipher SecretCipher, perms PermissionResolver, audit AuditWriter, mailer SmtpMailer, logger *slog.Logger) *Service {
	return &Service{store: store, cipher: cipher, perms: perms, audit: audit, mailer: mailer, logger: logger}
}

// log returns the configured logger or slog.Default().
func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// GetSmtpSettings returns the current settings (zero defaults +
// password_configured=false when no row exists yet — GET_INITIAL).
func (s *Service) GetSmtpSettings(ctx context.Context, actorID string) (*SmtpSettings, error) {
	if err := s.requireSettingsPermission(ctx, actorID); err != nil {
		return nil, err
	}
	settings, err := s.store.GetSmtpSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to read smtp settings: %w", err)
	}
	if settings == nil {
		return &SmtpSettings{}, nil
	}
	return settings, nil
}

// CurrentSmtpSettings implements the read-only SmtpSettingsPort (AD-14) that
// the User module's reset-email sender consumes at send time. No actor, no
// permission re-check: this is the trusted internal read path.
func (s *Service) CurrentSmtpSettings(ctx context.Context) (*SmtpSettings, error) {
	settings, err := s.store.GetSmtpSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to read smtp settings: %w", err)
	}
	if settings == nil {
		return &SmtpSettings{}, nil
	}
	return settings, nil
}

// UpdateSmtpSettings persists the settings atomically (PUT_VALID /
// PUT_NO_PASSWORD / PUT_INVALID). A present non-blank password is trimmed,
// encrypted at rest and replaces the stored one; an absent or whitespace-only
// password keeps the existing ciphertext (write-only edit). The keep-existing
// case is decided inside the store's single upsert statement (COALESCE), so
// the read+write is atomic — a concurrent update cannot overwrite the
// password with a stale value. The update is audited
// (admin.settings.email.update, NFR-O1/NFR-O2).
func (s *Service) UpdateSmtpSettings(ctx context.Context, actorID string, input UpdateSmtpSettingsInput) (*SmtpSettings, error) {
	if err := s.requireSettingsPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateSmtpSettings(input); err != nil {
		return nil, err
	}

	settings := &SmtpSettings{
		Host:          strings.TrimSpace(input.Host),
		Port:          input.Port,
		Security:      input.Security,
		SenderAddress: strings.TrimSpace(input.SenderAddress),
		SenderName:    strings.TrimSpace(input.SenderName),
		Username:      strings.TrimSpace(input.Username),
	}

	password := ""
	if input.Password != nil {
		// A blank/whitespace-only value is treated as absent (finding: PUT with
		// a whitespace-only password must not be stored as configured).
		password = strings.TrimSpace(*input.Password)
	}
	if password != "" {
		enc, err := s.cipher.Encrypt(password)
		if err != nil {
			return nil, fmt.Errorf("admin core: failed to encrypt smtp password: %w", err)
		}
		settings.PasswordEncrypted = enc
	}
	// When absent/blank, PasswordEncrypted stays "" → the store atomically
	// keeps the existing ciphertext (no read-then-write race).

	persisted, err := s.store.UpsertSmtpSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to persist smtp settings: %w", err)
	}

	s.auditSettings(ctx, actorID, AuditOperationSmtpSettingsUpdate, "target=smtp_settings")
	return persisted, nil
}

// TestSmtpSettings sends a test email to the acting admin through the
// configured server (TEST_OK / TEST_FAIL / DECRYPT_FAIL). Delivery failures
// return a 200-style result with a GENERIC German error — the detailed
// engine/TLS/cert error is logged structured (NFR-O1) and never surfaced
// inline. Every attempt is audited (admin.settings.email.test).
func (s *Service) TestSmtpSettings(ctx context.Context, actorID, actorEmail string) (*SmtpTestResult, error) {
	if err := s.requireSettingsPermission(ctx, actorID); err != nil {
		return nil, err
	}

	settings, err := s.store.GetSmtpSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to read smtp settings: %w", err)
	}
	if settings == nil || !settings.DeliveryUsable() {
		s.auditSettings(ctx, actorID, AuditOperationSmtpSettingsTest, fmt.Sprintf("to=%s result=skipped not_configured", actorEmail))
		return &SmtpTestResult{Ok: false, Message: MsgSmtpNotConfigured}, nil
	}

	password := ""
	if settings.PasswordConfigured() {
		password, err = s.cipher.Decrypt(settings.PasswordEncrypted)
		if err != nil {
			// DECRYPT_FAIL: stored ciphertext is unreadable (wrong/rotated key
			// or tampering). Clear German error, logged structured, audited.
			s.log().Warn("smtp test failed: stored password cannot be decrypted", "to", actorEmail, "error", err)
			s.auditSettings(ctx, actorID, AuditOperationSmtpSettingsTest, fmt.Sprintf("to=%s result=failed decrypt", actorEmail))
			return &SmtpTestResult{Ok: false, Message: MsgSmtpPasswordInvalid}, nil
		}
	}

	subject := "GEAR Test-E-Mail"
	body := "Diese E-Mail bestätigt, dass die SMTP-Konfiguration funktioniert.\n\nMit freundlichen Grüßen\nG.E.A.R."
	if err := s.mailer.SendEmail(ctx, SmtpSendParams{
		Host:       settings.Host,
		Port:       settings.Port,
		Security:   settings.Security,
		Username:   settings.Username,
		Password:   password,
		From:       settings.SenderAddress,
		SenderName: settings.SenderName,
		To:         actorEmail,
		Subject:    subject,
		Body:       body,
	}); err != nil {
		// TEST_FAIL: unreachable server / auth rejected. Generic inline German
		// message (no SMTP/TLS/cert detail leaks), full detail logged
		// structured (NFR-O1), audited — never a generic 5xx.
		s.log().Warn("smtp test email send failed", "to", actorEmail, "host", settings.Host, "error", err)
		s.auditSettings(ctx, actorID, AuditOperationSmtpSettingsTest, fmt.Sprintf("to=%s result=failed", actorEmail))
		return &SmtpTestResult{Ok: false, Message: MsgSmtpTestFailed}, nil
	}

	s.log().Info("smtp test email sent", "to", actorEmail, "host", settings.Host)
	s.auditSettings(ctx, actorID, AuditOperationSmtpSettingsTest, fmt.Sprintf("to=%s result=ok", actorEmail))
	return &SmtpTestResult{Ok: true, Message: MsgSmtpTestSent}, nil
}

// requireSettingsPermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds admin.settings.email. The route gateway
// already enforces it; the core re-checks so no future direct caller can skip
// it. An empty actor ID never passes.
func (s *Service) requireSettingsPermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("admin core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == SmtpSettingsPermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditSettings writes the audit row best-effort (NFR-O1): a failed audit
// write is logged, never rolled back into the triggering operation.
func (s *Service) auditSettings(ctx context.Context, actorID, operation, detail string) {
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("admin core: smtp settings audit write failed", "operation", operation, "error", err)
	}
}

// validateSmtpSettings enforces the PUT invariants (PUT_INVALID): non-empty
// host, valid port, valid security mode, non-empty sender address, bounded
// optional fields, no CR/LF in header-derived fields, and bounded provided
// passwords. A blank/whitespace-only provided password is NOT an error here —
// the caller treats it as absent (keep-existing).
func validateSmtpSettings(input UpdateSmtpSettingsInput) error {
	host := strings.TrimSpace(input.Host)
	if host == "" {
		return &InvalidSmtpSettingsError{Message: "Bitte gib einen SMTP-Host an."}
	}
	if len(host) > 255 {
		return &InvalidSmtpSettingsError{Message: "Der SMTP-Host ist zu lang."}
	}
	if strings.ContainsAny(host, "\r\n") {
		return &InvalidSmtpSettingsError{Message: "Der SMTP-Host enthält ungültige Zeichen."}
	}
	if input.Port < 1 || input.Port > 65535 {
		return &InvalidSmtpSettingsError{Message: "Bitte gib einen gültigen Port an (1–65535)."}
	}
	switch input.Security {
	case SmtpSecurityNone, SmtpSecurityStartTLS, SmtpSecurityTLS:
	default:
		return &InvalidSmtpSettingsError{Message: "Bitte wähle eine gültige Verschlüsselungsoption (Keine, STARTTLS oder TLS)."}
	}
	sender := strings.TrimSpace(input.SenderAddress)
	if sender == "" || !strings.Contains(sender, "@") {
		return &InvalidSmtpSettingsError{Message: "Bitte gib eine gültige Absenderadresse an."}
	}
	if len(sender) > 254 {
		return &InvalidSmtpSettingsError{Message: "Die Absenderadresse ist zu lang."}
	}
	if strings.ContainsAny(sender, "\r\n") {
		return &InvalidSmtpSettingsError{Message: "Die Absenderadresse enthält ungültige Zeichen."}
	}
	senderName := strings.TrimSpace(input.SenderName)
	if len(senderName) > 255 {
		return &InvalidSmtpSettingsError{Message: "Der Absendername ist zu lang."}
	}
	if strings.ContainsAny(senderName, "\r\n") {
		return &InvalidSmtpSettingsError{Message: "Der Absendername enthält ungültige Zeichen."}
	}
	if len(strings.TrimSpace(input.Username)) > 255 {
		return &InvalidSmtpSettingsError{Message: "Der Benutzername ist zu lang."}
	}
	if input.Password != nil && len(strings.TrimSpace(*input.Password)) > 1024 {
		return &InvalidSmtpSettingsError{Message: "Das Passwort ist zu lang."}
	}
	return nil
}