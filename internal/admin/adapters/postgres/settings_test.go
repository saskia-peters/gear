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

	// GET_INITIAL: no row yet → nil (the core maps it to zero defaults).
	got, err := repo.GetSmtpSettings(ctx)
	if err != nil {
		t.Fatalf("GetSmtpSettings(fresh) err = %v", err)
	}
	if got != nil {
		t.Fatalf("fresh GetSmtpSettings = %+v, want nil", got)
	}

	// PUT_VALID: first upsert creates the row and returns the resulting row.
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

	// Cleanup so the dev database stays pristine for other test runs.
	if _, err := pool.Exec(ctx, "DELETE FROM smtp_settings"); err != nil {
		t.Fatalf("cleanup err = %v", err)
	}
}