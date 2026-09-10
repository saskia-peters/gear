// Package ports declares the port interfaces of the Admin hexagon (AD-1):
// the inbound settings service consumed by the Admin HTTP router, and the
// read-only settings port other modules consume (AD-14) — the User module's
// reset-email sender reads the live SMTP row here, never a copy.
package ports

import (
	"context"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// Service is the Admin module's inbound SMTP-settings port (Story 3.1,
// FR-28): get, update (encrypt-on-write, keep-existing-password-on-absent)
// and test-send. Every method re-checks `admin.settings.email` against the
// actor's LIVE permission set defense-in-depth (AD-6). Update and test-send
// are audited with actor, timestamp and operation (NFR-O1/NFR-O2).
type Service interface {
	// GetSmtpSettings returns the current settings (zero defaults +
	// password_configured=false when no row exists yet). The encrypted
	// password is never exposed by the HTTP surface — only
	// password_configured reaches a client.
	GetSmtpSettings(ctx context.Context, actorID string) (*core.SmtpSettings, error)
	// UpdateSmtpSettings persists the settings atomically: a present password
	// is encrypted at rest and replaced, an absent one keeps the existing
	// ciphertext (write-only edit).
	UpdateSmtpSettings(ctx context.Context, actorID string, input core.UpdateSmtpSettingsInput) (*core.SmtpSettings, error)
	// TestSmtpSettings sends a test email to the acting admin's address
	// through the configured server and returns the inline German result
	// (ok + message). Delivery failures are logged structured (NFR-O1) and
	// audited; the action never answers a generic 5xx for an SMTP failure.
	TestSmtpSettings(ctx context.Context, actorID, actorEmail string) (*core.SmtpTestResult, error)
}

// SmtpSettingsPort is the read-only settings port consumed by other modules
// (AD-14). It carries the stored ciphertext so a consumer can decrypt it in
// memory at send time; it is deliberately NOT the HTTP get path (which never
// exposes the password). Implemented by the Admin core.
type SmtpSettingsPort interface {
	// CurrentSmtpSettings returns the live settings row (zero defaults when
	// none exists yet).
	CurrentSmtpSettings(ctx context.Context) (*core.SmtpSettings, error)
}