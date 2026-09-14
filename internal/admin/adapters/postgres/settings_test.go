package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// adminTestPool connects to the local dev database (migration 000017 applied)
// or skips when no database is reachable, mirroring the user-module repository
// integration tests.
func adminTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}
	return pool
}

func TestPostgresSmtpSettingsStore(t *testing.T) {
	pool := adminTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	queries := New(pool)
	repo := NewRepository(queries)

	// SNAPSHOT for restore: the smtp_settings table is single-row and may hold
	// REAL user data in the shared dev DB. Capture any pre-existing row so the
	// test can restore it afterwards instead of destroying a real SMTP config.
	type snapRow struct {
		host, senderAddress, senderName, username, passwordEncrypted string
		port                                                         int
		security                                                     string
	}
	var existing *snapRow
	orig, err := repo.GetSmtpSettings(ctx)
	if err != nil {
		t.Fatalf("GetSmtpSettings(snapshot) err = %v", err)
	}
	if orig != nil {
		existing = &snapRow{
			host: orig.Host, senderAddress: orig.SenderAddress, senderName: orig.SenderName,
			username: orig.Username, passwordEncrypted: orig.PasswordEncrypted,
			port: orig.Port, security: orig.Security,
		}
	}
	t.Cleanup(func() {
		if existing != nil {
			_, _ = pool.Exec(ctx, `UPDATE smtp_settings SET host=$1, port=$2, security=$3,
				sender_address=$4, sender_name=$5, username=$6, password_encrypted=$7`,
				existing.host, existing.port, existing.security, existing.senderAddress,
				existing.senderName, existing.username, existing.passwordEncrypted)
		} else {
			_, _ = pool.Exec(ctx, "DELETE FROM smtp_settings")
		}
	})

	// GET_INITIAL: no row yet → nil (the core maps it to zero defaults). When a
	// real row exists in the shared dev DB this assertion is skipped — the
	// upsert semantics below still verify the store.
	if existing == nil {
		got, err := repo.GetSmtpSettings(ctx)
		if err != nil {
			t.Fatalf("GetSmtpSettings(fresh) err = %v", err)
		}
		if got != nil {
			t.Fatalf("fresh GetSmtpSettings = %+v, want nil", got)
		}
	}

	// PUT_VALID: first upsert creates the row and returns the resulting row.
	var got *core.SmtpSettings
	first := &core.SmtpSettings{
		Host: "smtp.example.com", Port: 587, Security: core.SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", SenderName: "G.E.A.R.",
		Username: "smtpuser", PasswordEncrypted: "enc:geheim",
	}
	result, err := repo.UpsertSmtpSettings(ctx, first)
	if err != nil {
		t.Fatalf("UpsertSmtpSettings(first) err = %v", err)
	}
	if result == nil || result.PasswordEncrypted != "enc:geheim" {
		t.Fatalf("first upsert result = %+v, want stored ciphertext", result)
	}
	got, err = repo.GetSmtpSettings(ctx)
	if err != nil {
		t.Fatalf("GetSmtpSettings err = %v", err)
	}
	if got == nil {
		t.Fatal("settings = nil after first upsert")
	}
	if got.Host != "smtp.example.com" || got.Port != 587 || got.Security != core.SmtpSecurityStartTLS {
		t.Errorf("settings = %+v, want persisted values", got)
	}
	if got.PasswordEncrypted != "enc:geheim" {
		t.Errorf("password_encrypted = %q, want ciphertext verbatim", got.PasswordEncrypted)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps missing: %+v", got)
	}

	// PUT_NO_PASSWORD: a second upsert with an EMPTY password atomically KEEPS
	// the existing ciphertext (COALESCE in one statement, finding 6) and the
	// returned row reflects the kept password.
	second := &core.SmtpSettings{
		Host: "mail.example.com", Port: 465, Security: core.SmtpSecurityTLS,
		SenderAddress: "noreply@example.com", SenderName: "",
		Username: "", PasswordEncrypted: "",
	}
	result, err = repo.UpsertSmtpSettings(ctx, second)
	if err != nil {
		t.Fatalf("UpsertSmtpSettings(second) err = %v", err)
	}
	if result == nil || result.PasswordEncrypted != "enc:geheim" {
		t.Fatalf("keep-existing upsert result = %+v, want kept ciphertext enc:geheim", result)
	}
	if !result.PasswordConfigured() {
		t.Error("password_configured = false after keep-existing, want true")
	}
	got, err = repo.GetSmtpSettings(ctx)
	if err != nil {
		t.Fatalf("GetSmtpSettings err = %v", err)
	}
	if got.Host != "mail.example.com" || got.Port != 465 || got.Security != core.SmtpSecurityTLS {
		t.Errorf("settings after second upsert = %+v, want updated values", got)
	}
	if got.PasswordEncrypted != "enc:geheim" {
		t.Errorf("password after keep-existing = %q, want unchanged enc:geheim", got.PasswordEncrypted)
	}

	// PUT_NEW_PASSWORD: a non-empty password REPLACES the ciphertext.
	third := &core.SmtpSettings{
		Host: "mail.example.com", Port: 465, Security: core.SmtpSecurityTLS,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:neu",
	}
	result, err = repo.UpsertSmtpSettings(ctx, third)
	if err != nil {
		t.Fatalf("UpsertSmtpSettings(third) err = %v", err)
	}
	if result.PasswordEncrypted != "enc:neu" {
		t.Errorf("replaced password = %q, want enc:neu", result.PasswordEncrypted)
	}

	// Single-row guarantee (partial unique index): exactly one row remains.
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM smtp_settings").Scan(&count); err != nil {
		t.Fatalf("counting rows err = %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1 (single-row settings table)", count)
	}
	// Restore of any pre-existing real row (or removal of the test row) is
	// handled by the snapshot t.Cleanup at the top of the test.
}