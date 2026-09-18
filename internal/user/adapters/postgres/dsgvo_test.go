package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresDsgvoExportReads exercises the Story 3.3 DSGVO data-access reads
// over the dev database (migration 000029 applied): GetUserByIDFull returns the
// FULL row (attributes + created_at/updated_at AND the secret columns — the
// core strips them before assembly, REPORT_SECRETS) and ListSessionsByUser
// returns the user's sessions newest-first WITHOUT any token material.
func TestPostgresDsgvoExportReads(t *testing.T) {
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
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	queries := New(pool)
	repo := NewRepository(queries)

	suffix := time.Now().Format("20060102150405.000000")
	testEmail := "dsgvo.test." + suffix + "@gear.local"
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Dsgvo Test", "Dsgvo", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", created.ID)
	})

	// GetUserByIDFull returns the FULL row: the secret columns AND the
	// attributes/timestamps are present (the export strips the secrets in the
	// core; this read is the profile source).
	full, err := repo.GetUserByIDFull(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByIDFull err = %v", err)
	}
	if full == nil {
		t.Fatal("GetUserByIDFull returned nil")
	}
	if full.Email != testEmail || full.FirstName != "Dsgvo" || full.LastName != "Test" {
		t.Errorf("full profile identity = %+v, want the created user", full)
	}
	if full.PasswordHash == "" {
		t.Error("full.PasswordHash empty — GetUserByIDFull must return the stored hash (the core strips it before assembly)")
	}
	if full.CreatedAt.IsZero() || full.UpdatedAt.IsZero() {
		t.Errorf("full timestamps = (%v, %v), want the stored values", full.CreatedAt, full.UpdatedAt)
	}
	if full.Attributes == nil {
		t.Error("full.Attributes nil, want the parsed JSONB object")
	}

	// An unknown id maps to ErrAdminUserNotFound.
	if _, err := repo.GetUserByIDFull(ctx, "00000000-0000-0000-0000-0000000000ff"); err != core.ErrAdminUserNotFound {
		t.Errorf("GetUserByIDFull(unknown) err = %v, want ErrAdminUserNotFound", err)
	}

	// ListSessionsByUser: two sessions (older first in creation, newer expiry)
	// → newest-first list, identity + timestamps only (no token material).
	olderHash := "hash-older-" + suffix
	newerHash := "hash-newer-" + suffix
	older, err := repo.CreateSession(ctx, created.ID, olderHash, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession(older) err = %v", err)
	}
	newer, err := repo.CreateSession(ctx, created.ID, newerHash, time.Now().UTC().Add(4*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession(newer) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM sessions WHERE id = ANY($1)", []string{older.ID, newer.ID})
	})

	sessions, err := repo.ListSessionsByUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListSessionsByUser err = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	// newest first by created_at DESC: newer has the later created_at.
	if sessions[0].ID != newer.ID || sessions[1].ID != older.ID {
		t.Errorf("sessions order = [%s, %s], want [%s, %s] newest first",
			sessions[0].ID, sessions[1].ID, newer.ID, older.ID)
	}
	if sessions[0].ExpiresAt.IsZero() || sessions[0].CreatedAt.IsZero() {
		t.Errorf("session timestamps missing: %+v", sessions[0])
	}

	// A user with no sessions answers an empty list, nil-safe.
	emptyUser, err := repo.CreateRegisteredUser(ctx, "dsgvo.empty."+suffix+"@gear.local", "Leer", "Leer", "Test", "$argon2id$v=19$dummy")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(empty) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", emptyUser.ID)
	})
	empty, err := repo.ListSessionsByUser(ctx, emptyUser.ID)
	if err != nil {
		t.Fatalf("ListSessionsByUser(empty) err = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty sessions = %+v, want an empty non-nil list", empty)
	}
}