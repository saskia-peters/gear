// Package ports declares the port interfaces of the Admin hexagon (AD-1):
// the inbound settings service consumed by the Admin HTTP router, and the
// read-only settings ports other modules consume (AD-14/AD-15/AD-16) — the
// User module's reset-email sender reads the live SMTP row here, the future
// backup job reads destinations here, and the future Tool module reads the
// schedule catalog here, never a copy.
package ports

import (
	"context"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// Service is the Admin module's inbound settings port (Story 3.1 SMTP, FR-28;
// Story 3.2 backup destinations, FR-29/AD-15; Story 4.1 schedule catalog,
// FR-30/AD-16): get/update (encrypt-on-write, keep-existing-credential-on-absent)
// and test for SMTP; list/create/update/delete/test for backup destinations;
// list/create/update/archive for schedules (soft archive). Every method
// re-checks the relevant permission code against the actor's LIVE permission
// set defense-in-depth (AD-6). Writes and tests are audited with actor,
// timestamp and operation (NFR-O1/NFR-O2).
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

	// ListBackupDestinations returns every destination, oldest first. The
	// HTTP surface exposes only credential_configured per row — never the
	// ciphertext (NFR-S4).
	ListBackupDestinations(ctx context.Context, actorID string) ([]*core.BackupDestination, error)
	// CreateBackupDestination persists a new destination; S3/FTP/SFTP require
	// a credential, local destinations do not. A present credential is
	// encrypted at rest.
	CreateBackupDestination(ctx context.Context, actorID string, input core.BackupDestinationInput) (*core.BackupDestination, error)
	// UpdateBackupDestination persists a destination atomically: a present
	// credential is encrypted at rest and replaced, an absent one keeps the
	// existing ciphertext (atomic COALESCE in the store's statement).
	UpdateBackupDestination(ctx context.Context, actorID, id string, input core.BackupDestinationInput) (*core.BackupDestination, error)
	// DeleteBackupDestination removes one destination (audited).
	DeleteBackupDestination(ctx context.Context, actorID, id string) error
	// TestBackupDestination exercises the destination's mechanism/endpoint
	// and returns the inline German result (ok + message). Failures are logged
	// structured (NFR-O1) and audited; the action never answers a generic 5xx
	// for a test-connection failure.
	TestBackupDestination(ctx context.Context, actorID, id string) (*core.BackupTestResult, error)

	// ListSchedules returns every ACTIVE schedule, oldest first (GET_LIST /
	// GET_LIST_EMPTY). Archived rows are filtered server-side.
	ListSchedules(ctx context.Context, actorID string) ([]*core.Schedule, error)
	// CreateSchedule persists a new schedule (name + interval unit/magnitude;
	// the reserved weekday/time fields stay NULL).
	CreateSchedule(ctx context.Context, actorID string, input core.ScheduleInput) (*core.Schedule, error)
	// UpdateSchedule persists a schedule (updated_at refreshed). Updating an
	// already-archived schedule answers ErrScheduleNotFound (404 sentinel —
	// archive is irreversible in V1).
	UpdateSchedule(ctx context.Context, actorID, id string, input core.ScheduleInput) (*core.Schedule, error)
	// ArchiveSchedule soft-archives one schedule: archived_at is set, the row
	// leaves the active list. Audited. Archiving an already-archived schedule
	// answers ErrScheduleNotFound.
	ArchiveSchedule(ctx context.Context, actorID, id string) (*core.Schedule, error)
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

// BackupDestinationsPort is the read-only backup-destination consumer port
// (AD-15) — the first backup-job seam. The future backup job reads
// destinations here, never a copy. It carries the stored ciphertext so a
// consumer can decrypt it in memory at backup time; it is deliberately NOT the
// HTTP list path (which never exposes credentials). Implemented by the Admin
// core.
type BackupDestinationsPort interface {
	// CurrentBackupDestinations returns the live destinations, oldest first.
	CurrentBackupDestinations(ctx context.Context) ([]*core.BackupDestination, error)
}

// SchedulesPort is the read-only schedule-catalog consumer port (AD-16) — the
// seam the future Tool module consumes (tool-type default / per-tool choice,
// Story 4.2). It reads the ACTIVE catalog here, never a copy. Implemented by
// the Admin core.
type SchedulesPort interface {
	// CurrentSchedules returns the live ACTIVE schedules, oldest first
	// (archived rows are filtered out).
	CurrentSchedules(ctx context.Context) ([]*core.Schedule, error)
}