package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// Repository wraps the sqlc-generated Admin Queries to implement the Admin
// core's outbound ports: the smtp_settings store (Story 3.1, AD-11) and the
// backup_destinations store (Story 3.2, AD-15 — see backup_repo.go). Schedules
// land here in a later story.
type Repository struct {
	queries *Queries
}

// NewRepository creates a new postgres Admin repository.
func NewRepository(queries *Queries) *Repository {
	return &Repository{queries: queries}
}

// GetSmtpSettings reads the single smtp_settings row. Returns nil, nil when no
// row exists yet (the core turns that into zero defaults, GET_INITIAL).
func (r *Repository) GetSmtpSettings(ctx context.Context) (*core.SmtpSettings, error) {
	rows, err := r.queries.GetSmtpSettings(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return smtpSettingsFromRow(rows[0]), nil
}

// UpsertSmtpSettings writes the settings row atomically and returns the
// resulting row (PUT_VALID / PUT_NO_PASSWORD): it updates the single existing
// row, or inserts the first one when none exists (the single-row partial
// unique index guarantees at most one). The password COALESCE lives inside the
// UPDATE statement, so keep-existing is atomic with the write. The insert is
// ON CONFLICT DO NOTHING so a lost race between two concurrent first writes
// never errors — the follow-up UPDATE re-applies the values. All statements
// run in ONE transaction; the result is read back inside that transaction so
// password_configured reflects what was actually stored.
func (r *Repository) UpsertSmtpSettings(ctx context.Context, settings *core.SmtpSettings) (*core.SmtpSettings, error) {
	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)
	params := UpdateSmtpSettingsParams{
		Host:              settings.Host,
		Port:              int32(settings.Port),
		Security:          settings.Security,
		SenderAddress:     settings.SenderAddress,
		SenderName:        settings.SenderName,
		Username:          settings.Username,
		PasswordEncrypted: settings.PasswordEncrypted,
	}

	n, err := q.UpdateSmtpSettings(ctx, params)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		if err := q.InsertSmtpSettings(ctx, InsertSmtpSettingsParams(params)); err != nil {
			return nil, err
		}
		// Re-apply the values when the INSERT lost a race to another first
		// writer (ON CONFLICT DO NOTHING affected zero rows).
		if _, err := q.UpdateSmtpSettings(ctx, params); err != nil {
			return nil, err
		}
	}

	rows, err := q.GetSmtpSettings(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("admin postgres: smtp_settings row vanished during upsert")
	}
	result := smtpSettingsFromRow(rows[0])

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// smtpSettingsFromRow maps an sqlc smtp_settings row to the domain settings.
func smtpSettingsFromRow(row SmtpSetting) *core.SmtpSettings {
	return &core.SmtpSettings{
		Host:              row.Host,
		Port:              int(row.Port),
		Security:          row.Security,
		SenderAddress:     row.SenderAddress,
		SenderName:        row.SenderName,
		Username:          row.Username,
		PasswordEncrypted: row.PasswordEncrypted,
		CreatedAt:         row.CreatedAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}

// beginTx opens a fresh transaction on the underlying pool. A Queries handle
// built over anything that is not transaction-capable (only conceivable in a
// unit test) surfaces a clear error instead of panicking.
func (r *Repository) beginTx(ctx context.Context) (pgx.Tx, error) {
	if r.queries == nil {
		return nil, errors.New("admin postgres: nil queries")
	}
	pool, ok := r.queries.db.(interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	})
	if !ok {
		return nil, errors.New("admin postgres: db handle does not support transactions")
	}
	return pool.Begin(ctx)
}