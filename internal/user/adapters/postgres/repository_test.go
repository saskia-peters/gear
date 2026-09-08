package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/user/core"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPostgresLoginAttemptsRepository(t *testing.T) {
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

	// login_attempts is keyed by email with no users FK, so an unknown email
	// can be tracked too (anti-enumeration).
	testEmail := "lockout.test." + time.Now().Format("20060102150405.000000") + "@gear.local"

	// 1. A fresh email has no attempts record.
	att, err := repo.GetLoginAttempts(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetLoginAttempts(fresh) failed: %v", err)
	}
	if att != nil {
		t.Fatalf("expected nil attempts for fresh email, got %+v", att)
	}

	// 2. Three atomic increments cross the 30s threshold (FR-3).
	for i := 0; i < 3; i++ {
		if err := repo.IncrementLoginAttempts(ctx, testEmail); err != nil {
			t.Fatalf("IncrementLoginAttempts (%d) failed: %v", i, err)
		}
	}
	att, err = repo.GetLoginAttempts(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetLoginAttempts failed: %v", err)
	}
	if att.Email != testEmail {
		t.Errorf("attempt email = %q, want %q", att.Email, testEmail)
	}
	if att.FailedCount != 3 {
		t.Errorf("attempt failed count = %d, want 3", att.FailedCount)
	}
	want := time.Now().UTC().Add(30 * time.Second)
	if att.LockoutUntil.IsZero() || att.LockoutUntil.Before(want.Add(-2*time.Second)) || att.LockoutUntil.After(want.Add(2*time.Second)) {
		t.Errorf("attempt lockout_until = %v, want ~now+30s", att.LockoutUntil)
	}

	// 3. A 4th increment escalates to the 60s window (LOCKOUT_4_PLUS).
	if err := repo.IncrementLoginAttempts(ctx, testEmail); err != nil {
		t.Fatalf("IncrementLoginAttempts (4th) failed: %v", err)
	}
	att, err = repo.GetLoginAttempts(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetLoginAttempts failed: %v", err)
	}
	if att.FailedCount != 4 {
		t.Errorf("attempt failed count = %d, want 4", att.FailedCount)
	}
	want = time.Now().UTC().Add(60 * time.Second)
	if att.LockoutUntil.IsZero() || att.LockoutUntil.Before(want.Add(-2*time.Second)) || att.LockoutUntil.After(want.Add(2*time.Second)) {
		t.Errorf("attempt lockout_until = %v, want ~now+60s", att.LockoutUntil)
	}

	// 4. The counter is capped (LockoutMaxFailedCount = 10).
	for i := 0; i < 10; i++ {
		if err := repo.IncrementLoginAttempts(ctx, testEmail); err != nil {
			t.Fatalf("IncrementLoginAttempts (cap) failed: %v", err)
		}
	}
	att, err = repo.GetLoginAttempts(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetLoginAttempts failed: %v", err)
	}
	if att.FailedCount != 10 {
		t.Errorf("attempt failed count = %d, want capped at 10", att.FailedCount)
	}

	// 5. ClearLoginAttempts resets the counter and window for a fresh cycle.
	if err := repo.ClearLoginAttempts(ctx, testEmail); err != nil {
		t.Fatalf("ClearLoginAttempts failed: %v", err)
	}
	att, err = repo.GetLoginAttempts(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetLoginAttempts after clear failed: %v", err)
	}
	if att == nil {
		t.Fatal("expected attempts row to remain after clear (reset to 0)")
	}
	if att.FailedCount != 0 {
		t.Errorf("attempt failed count = %d, want 0", att.FailedCount)
	}
	if !att.LockoutUntil.IsZero() {
		t.Errorf("attempt lockout_until = %v, want zero after clear", att.LockoutUntil)
	}
}

func TestPostgresRepository(t *testing.T) {
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
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	queries := New(pool)
	repo := NewRepository(queries)

	testEmail := "test.user." + time.Now().Format("20060102150405.000000") + "@gear.local"

	// 1. Check user does not exist initially
	existing, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if existing != nil {
		t.Fatalf("expected nil user, got %+v", existing)
	}

	// 2. Create registered user
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Test User", "Test", "User", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if created.Email != testEmail {
		t.Errorf("created.Email = %q, want %q", created.Email, testEmail)
	}
	if created.State != core.StatePendingApproval {
		t.Errorf("created.State = %q, want %q", created.State, core.StatePendingApproval)
	}
	if created.FirstName != "Test" || created.LastName != "User" {
		t.Errorf("created names = (%q, %q), want (Test, User)", created.FirstName, created.LastName)
	}

	// 3. Query newly created user
	fetched, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected user to be found, got nil")
	}
	if fetched.Email != testEmail {
		t.Errorf("fetched.Email = %q, want %q", fetched.Email, testEmail)
	}
	if fetched.State != core.StatePendingApproval {
		t.Errorf("fetched.State = %q, want %q", fetched.State, core.StatePendingApproval)
	}
}

func TestPostgresSessionAndPermissionRepository(t *testing.T) {
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

	testEmail := "auth.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	suffix := time.Now().Format("20060102150405.000000")

	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Auth Test", "Auth", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if created.State != core.StatePendingApproval {
		t.Fatalf("created.State = %q, want pending_approval", created.State)
	}

	// 1. CreateSession + GetSessionByTokenHash round-trip.
	expiry := time.Now().UTC().Add(time.Hour)
	sessHash := "hash-of-raw-token." + suffix
	sess, err := repo.CreateSession(ctx, created.ID, sessHash, expiry)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.UserID != created.ID {
		t.Errorf("session user id = %q, want %q", sess.UserID, created.ID)
	}

	fetched, err := repo.GetSessionByTokenHash(ctx, sessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if fetched.ID != sess.ID {
		t.Errorf("fetched session id = %q, want %q", fetched.ID, sess.ID)
	}
	if fetched.User == nil || fetched.User.ID != created.ID {
		t.Errorf("fetched session user = %+v, want attached user %q", fetched.User, created.ID)
	}

	// Unknown hash maps to core.ErrSessionNotFound.
	if _, err := repo.GetSessionByTokenHash(ctx, "no-such-hash"); err != core.ErrSessionNotFound {
		t.Errorf("unknown token error = %v, want core.ErrSessionNotFound", err)
	}

	// 2. ListPermissionsByUser for a fresh user resolves to the empty set.
	perms, err := repo.ListPermissionsByUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("permissions = %v, want empty set", perms)
	}

	// 3. The seeded admin resolves the admin.recovery.approve permission
	// (AD-12 additive union) and the admin group.
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil {
		t.Fatalf("GetUserByEmail(admin) failed: %v", err)
	}
	if admin == nil {
		t.Skip("seeded admin not present — skipping permission resolution assertion")
	}
	adminPerms, err := repo.ListPermissionsByUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser(admin) failed: %v", err)
	}
	found := false
	for _, p := range adminPerms {
		if p == "admin.recovery.approve" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("admin permissions %v missing admin.recovery.approve", adminPerms)
	}

	// 4. DeleteSessionByTokenHash invalidates the session server-side,
	// atomically by hashed token.
	if err := repo.DeleteSessionByTokenHash(ctx, sessHash); err != nil {
		t.Fatalf("DeleteSessionByTokenHash failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, sessHash); err != core.ErrSessionNotFound {
		t.Errorf("deleted token error = %v, want core.ErrSessionNotFound", err)
	}
}

func TestPostgresTotpRepository(t *testing.T) {
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

	testEmail := "totp.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	suffix := time.Now().Format("20060102150405.000000")
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "TOTP Test", "TOTP", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if created.IsMFAEnabled {
		t.Fatal("fresh user must have MFA disabled")
	}
	if created.TotpSecretEncrypted != "" {
		t.Fatal("fresh user must have no stored TOTP secret")
	}

	// 1. SetUserTotpSecret persists the encrypted secret and flips the flag.
	ciphertext := "base64(nonce||ciphertext)=" + testEmail
	if err := repo.SetUserTotpSecret(ctx, created.ID, ciphertext); err != nil {
		t.Fatalf("SetUserTotpSecret failed: %v", err)
	}
	fetched, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if !fetched.IsMFAEnabled {
		t.Error("MFA flag must be true after SetUserTotpSecret")
	}
	if fetched.TotpSecretEncrypted != ciphertext {
		t.Errorf("stored secret = %q, want %q", fetched.TotpSecretEncrypted, ciphertext)
	}

	// 1b. A session created for the MFA-enabled user must carry the encrypted
	// secret on its user snapshot so MFA disable can validate a current code
	// (FR-4). This pins the JOIN in GetSessionByTokenHash.
	totpSessHash := "hash-of-totp-session." + suffix
	sess, err := repo.CreateSession(ctx, created.ID, totpSessHash, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched, err := repo.GetSessionByTokenHash(ctx, totpSessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if sessFetched.User == nil || sessFetched.User.TotpSecretEncrypted != ciphertext {
		t.Errorf("session user secret = %q, want %q", sessFetched.User.TotpSecretEncrypted, ciphertext)
	}
	if sessFetched.User == nil || !sessFetched.User.IsMFAEnabled {
		t.Errorf("session user MFA flag = %v, want true", sessFetched.User.IsMFAEnabled)
	}
	_ = sess

	// 2. ClearUserTotpSecret disables MFA and clears the secret.
	if err := repo.ClearUserTotpSecret(ctx, created.ID); err != nil {
		t.Fatalf("ClearUserTotpSecret failed: %v", err)
	}
	fetched, err = repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.IsMFAEnabled {
		t.Error("MFA flag must be false after ClearUserTotpSecret")
	}
	if fetched.TotpSecretEncrypted != "" {
		t.Error("encrypted secret must be cleared after disable")
	}

	// 3. SetUserPendingTotpSecret persists a short-lived pending enrollment
	// (encrypted secret + expiry) that the confirm step validates against.
	pendingCipher := "base64(pending-nonce||ciphertext)=" + testEmail
	pendingExpiry := time.Now().UTC().Add(10 * time.Minute)
	if err := repo.SetUserPendingTotpSecret(ctx, created.ID, pendingCipher, pendingExpiry); err != nil {
		t.Fatalf("SetUserPendingTotpSecret failed: %v", err)
	}
	fetched, err = repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.PendingTotpSecretEncrypted != pendingCipher {
		t.Errorf("pending secret = %q, want %q", fetched.PendingTotpSecretEncrypted, pendingCipher)
	}
	if fetched.PendingTotpExpiresAt.IsZero() {
		t.Error("pending expiry must be persisted")
	}

	// 3b. The session user snapshot carries the pending enrollment so the
	// confirm step can act on it without an extra lookup.
	pendingSessHash := "hash-of-pending-session." + suffix
	sess2, err := repo.CreateSession(ctx, created.ID, pendingSessHash, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched2, err := repo.GetSessionByTokenHash(ctx, pendingSessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if sessFetched2.User == nil || sessFetched2.User.PendingTotpSecretEncrypted != pendingCipher {
		t.Errorf("session user pending secret = %q, want %q", sessFetched2.User.PendingTotpSecretEncrypted, pendingCipher)
	}
	_ = sess2

	// 4. ClearUserPendingTotpSecret clears the pending enrollment.
	if err := repo.ClearUserPendingTotpSecret(ctx, created.ID); err != nil {
		t.Fatalf("ClearUserPendingTotpSecret failed: %v", err)
	}
	fetched, err = repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.PendingTotpSecretEncrypted != "" || !fetched.PendingTotpExpiresAt.IsZero() {
		t.Error("pending enrollment must be cleared")
	}
}

func TestPostgresSessionRevocationRepository(t *testing.T) {
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

	testEmail := "revoke.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	suffix := time.Now().Format("20060102150405.000000")
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Revoke Test", "Revoke", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// Create three sessions.
	expiry := time.Now().UTC().Add(time.Hour)
	for i := 1; i <= 3; i++ {
		hash := fmt.Sprintf("hash-revoke-%d.%s", i, suffix)
		if _, err := repo.CreateSession(ctx, created.ID, hash, expiry); err != nil {
			t.Fatalf("CreateSession(%s) failed: %v", hash, err)
		}
	}

	// DeleteSessionsByUserExcept keeps the excepted session.
	revokeExcept := fmt.Sprintf("hash-revoke-2.%s", suffix)
	if err := repo.DeleteSessionsByUserExcept(ctx, created.ID, revokeExcept); err != nil {
		t.Fatalf("DeleteSessionsByUserExcept failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, revokeExcept); err != nil {
		t.Errorf("excepted session must survive, got %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, fmt.Sprintf("hash-revoke-1.%s", suffix)); !errors.Is(err, core.ErrSessionNotFound) {
		t.Errorf("non-excepted session must be revoked, got %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, fmt.Sprintf("hash-revoke-3.%s", suffix)); !errors.Is(err, core.ErrSessionNotFound) {
		t.Errorf("non-excepted session must be revoked, got %v", err)
	}

	// DeleteSessionsByUser revokes everything left.
	if err := repo.DeleteSessionsByUser(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSessionsByUser failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, revokeExcept); !errors.Is(err, core.ErrSessionNotFound) {
		t.Errorf("all sessions must be revoked, got %v", err)
	}
}

func TestPostgresChangePasswordRepository(t *testing.T) {
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

	testEmail := "changepw.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	suffix := time.Now().Format("20060102150405.000000")
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Password Change Test", "Password", "Test", "$argon2id$v=19$oldhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// 1. UpdateUserPassword persists the new hash and returns the updated user
	// (FR-25/AD-13); only the hash is written, never a plaintext.
	newHash := "$argon2id$v=19$newhash"
	updated, err := repo.UpdateUserPassword(ctx, created.ID, newHash)
	if err != nil {
		t.Fatalf("UpdateUserPassword failed: %v", err)
	}
	if updated.PasswordHash != newHash {
		t.Errorf("updated password hash = %q, want %q", updated.PasswordHash, newHash)
	}
	if updated.ID != created.ID {
		t.Errorf("updated user id = %q, want %q", updated.ID, created.ID)
	}

	fetched, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched == nil || fetched.PasswordHash != newHash {
		t.Errorf("stored password hash = %+v, want %q", fetched, newHash)
	}

	// 1b. A session issued for the user carries the password hash on its user
	// snapshot so the change-password flow can verify the current password
	// server-side (FR-25), like the MFA secrets for DisableMFA. This pins the
	// JOIN in GetSessionByTokenHash.
	changepwSessHash := "hash-of-changepw-session." + suffix
	if _, err := repo.CreateSession(ctx, created.ID, changepwSessHash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched, err := repo.GetSessionByTokenHash(ctx, changepwSessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if sessFetched.User == nil || sessFetched.User.PasswordHash != newHash {
		t.Errorf("session user password hash = %+v, want %q", sessFetched.User, newHash)
	}

	// 2. InsertAuditEvent appends an immutable audit row (actor, operation,
	// created_at) for the user (NFR-O1/NFR-O2, spine table 11).
	op := core.AuditOperationPasswordChange
	if err := repo.InsertAuditEvent(ctx, created.ID, op, "", ""); err != nil {
		t.Fatalf("InsertAuditEvent failed: %v", err)
	}

	var count int
	var actorUserID string
	var operation string
	var createdAt time.Time
	err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1`, created.ID).Scan(&count)
	if err != nil {
		t.Fatalf("counting audit rows failed: %v", err)
	}
	if count != 1 {
		t.Errorf("audit row count = %d, want 1", count)
	}
	err = pool.QueryRow(ctx,
		`SELECT actor_user_id::text, operation, created_at FROM audit_log WHERE actor_user_id = $1 AND operation = $2`,
		created.ID, op).Scan(&actorUserID, &operation, &createdAt)
	if err != nil {
		t.Fatalf("reading audit row failed: %v", err)
	}
	if actorUserID != created.ID {
		t.Errorf("audit actor = %q, want %q", actorUserID, created.ID)
	}
	if operation != op {
		t.Errorf("audit operation = %q, want %q", operation, op)
	}
	if createdAt.IsZero() {
		t.Error("audit row must carry a created_at timestamp")
	}

	// 3. Deleting a user ANONYMIZES the audit trail (ON DELETE SET NULL) instead
	// of destroying it (NFR-O1/NFR-O2): the row survives with a NULL actor.
	var auditID string
	err = pool.QueryRow(ctx,
		`SELECT id::text FROM audit_log WHERE actor_user_id = $1 AND operation = $2`,
		created.ID, op).Scan(&auditID)
	if err != nil {
		t.Fatalf("resolving audit row id failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("deleting user failed: %v", err)
	}
	var actorIsNull bool
	err = pool.QueryRow(ctx, `SELECT actor_user_id IS NULL FROM audit_log WHERE id = $1`, auditID).Scan(&actorIsNull)
	if err != nil {
		t.Fatalf("audit row must survive user deletion, got: %v", err)
	}
	if !actorIsNull {
		t.Error("audit row actor must be anonymized (NULL) after the user is deleted")
	}
}

func TestPostgresProfileRepository(t *testing.T) {
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
	emailA := "profile.a." + suffix + "@gear.local"
	emailB := "profile.b." + suffix + "@gear.local"
	stagedEmail := "neu.a." + suffix + "@example.com"

	a, err := repo.CreateRegisteredUser(ctx, emailA, "Profil A", "Profil", "A", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(A) failed: %v", err)
	}
	b, err := repo.CreateRegisteredUser(ctx, emailB, "Profil B", "Profil", "B", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(B) failed: %v", err)
	}

	// 1. UpdateUserProfile persists the editable base data and returns the
	// updated user; email and state are untouched (Story 2.1).
	updated, err := repo.UpdateUserProfile(ctx, a.ID, "Erika", "Musterfrau", "Erika", nil)
	if err != nil {
		t.Fatalf("UpdateUserProfile failed: %v", err)
	}
	if updated.FirstName != "Erika" || updated.LastName != "Musterfrau" || updated.DisplayName != "Erika" {
		t.Errorf("updated names = (%q,%q,%q), want (Erika, Musterfrau, Erika)", updated.FirstName, updated.LastName, updated.DisplayName)
	}
	if updated.Email != emailA || updated.State != core.StatePendingApproval {
		t.Errorf("email/state must be untouched, got (%q, %q)", updated.Email, updated.State)
	}
	if updated.ID != a.ID {
		t.Errorf("updated id = %q, want %q", updated.ID, a.ID)
	}

	// 2. StagePendingEmail persists the staged address; the current email stays
	// the login identifier.
	staged, err := repo.StagePendingEmail(ctx, a.ID, stagedEmail)
	if err != nil {
		t.Fatalf("StagePendingEmail failed: %v", err)
	}
	if staged.PendingEmail != stagedEmail {
		t.Errorf("pending_email = %q, want neu.a@example.com", staged.PendingEmail)
	}
	if staged.Email != emailA {
		t.Errorf("current email = %q, want unchanged %q", staged.Email, emailA)
	}

	// The session user snapshot carries pending_email so GetProfile can serve
	// it without a DB round-trip (Story 2.1).
	profileSessHash := "hash-of-profile-session." + suffix
	if _, err := repo.CreateSession(ctx, a.ID, profileSessHash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sess, err := repo.GetSessionByTokenHash(ctx, profileSessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if sess.User == nil || sess.User.PendingEmail != stagedEmail {
		t.Errorf("session user pending_email = %+v, want neu.a@example.com", sess.User)
	}
	if sess.User == nil || sess.User.FirstName != "Erika" {
		t.Errorf("session user first_name = %+v, want Erika", sess.User)
	}

	// 3. The conditional UPDATE (and the pending_email UNIQUE backstop) rejects
	// an address another user already staged (EMAIL_STAGE_DUPLICATE) with
	// core.ErrEmailInUse.
	if _, err := repo.StagePendingEmail(ctx, b.ID, stagedEmail); !errors.Is(err, core.ErrEmailInUse) {
		t.Errorf("staging another user's pending_email err = %v, want ErrEmailInUse", err)
	}

	// 3b. A CASE-VARIANT of another account's CURRENT email is rejected by the
	// conditional UPDATE's lower() comparison (TOCTOU guard): the mixed
	// pending_email == other's email collision cannot happen.
	caseVariant := strings.ToUpper(emailB)
	if _, err := repo.StagePendingEmail(ctx, a.ID, caseVariant); !errors.Is(err, core.ErrEmailInUse) {
		t.Errorf("staging a case-variant of an existing email err = %v, want ErrEmailInUse", err)
	}

	// 3c. A CASE-VARIANT of another account's already-staged pending_email is
	// rejected too (lower(pending_email) in the NOT EXISTS guard).
	if _, err := repo.StagePendingEmail(ctx, b.ID, strings.ToUpper(stagedEmail)); !errors.Is(err, core.ErrEmailInUse) {
		t.Errorf("staging a case-variant of another user's pending_email err = %v, want ErrEmailInUse", err)
	}

	// 4. The users.email UNIQUE constraint still governs the REAL login address:
	// a second account cannot register an address that is already in use, and
	// the adapter maps the 23505 violation to core.ErrUserAlreadyExists (pgx
	// PgError mapping, not string matching).
	if _, err := repo.CreateRegisteredUser(ctx, emailB, "Profil B2", "Profil", "B2", "$argon2id$v=19$dummyhash"); !errors.Is(err, core.ErrUserAlreadyExists) {
		t.Errorf("re-registering an existing email err = %v, want core.ErrUserAlreadyExists", err)
	}

	// 5. ClearPendingEmail clears the staged address (Epic 2 admin workflow).
	if err := repo.ClearPendingEmail(ctx, a.ID); err != nil {
		t.Fatalf("ClearPendingEmail failed: %v", err)
	}
	fetched, err := repo.GetUserByEmail(ctx, emailA)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched == nil || fetched.PendingEmail != "" {
		t.Errorf("pending_email after clear = %+v, want empty", fetched)
	}
	if fetched == nil || fetched.Email != emailA {
		t.Errorf("current email after clear = %+v, want %q", fetched, emailA)
	}

	// 6. UpdateUserProfile on an unknown user maps to ErrUserNotFound.
	if _, err := repo.UpdateUserProfile(ctx, "00000000-0000-0000-0000-000000000000", "X", "Y", "Z", nil); !errors.Is(err, core.ErrUserNotFound) {
		t.Errorf("UpdateUserProfile(unknown) err = %v, want ErrUserNotFound", err)
	}
	// StagePendingEmail on an unknown user affects zero rows → the in-use case
	// (review finding: "no row updated" == ErrEmailInUse).
	if _, err := repo.StagePendingEmail(ctx, "00000000-0000-0000-0000-000000000000", "x@example.com"); !errors.Is(err, core.ErrEmailInUse) {
		t.Errorf("StagePendingEmail(unknown) err = %v, want ErrEmailInUse", err)
	}
}

func TestPostgresProfileAttributesRepository(t *testing.T) {
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
	email := "attr." + suffix + "@gear.local"

	created, err := repo.CreateRegisteredUser(ctx, email, "Attribut Test", "Attribut", "Test", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if len(created.Attributes) != 0 {
		t.Fatalf("fresh user must have empty attributes, got %v", created.Attributes)
	}

	// 1. PROFILE_UPDATE_ATTRS: UpdateUserProfile persists the custom attributes
	// to the users.attributes JSONB column (FR-7) and the RETURNING clause maps
	// them back into the returned user.
	attrs := map[string]any{
		"note":          "Interne Notiz",
		"internal_tags": []string{"beta", "2026"},
	}
	updated, err := repo.UpdateUserProfile(ctx, created.ID, "Erika", "Musterfrau", "Erika", attrs)
	if err != nil {
		t.Fatalf("UpdateUserProfile(attrs) failed: %v", err)
	}
	if updated.FirstName != "Erika" {
		t.Errorf("first_name = %q, want Erika (base data still written)", updated.FirstName)
	}
	if got := updated.Attributes["note"]; got != "Interne Notiz" {
		t.Errorf("attributes.note = %v, want Interne Notiz", got)
	}

	// 2. PROFILE_READ_WITH_ATTRS: a fresh read (GetUserByEmail → userFromRow)
	// returns the stored attributes as a Go map.
	fetched, err := repo.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched == nil || fetched.Attributes == nil || fetched.Attributes["note"] != "Interne Notiz" {
		t.Errorf("fetched attributes = %+v, want note=Interne Notiz", fetched.Attributes)
	}

	// 2b. The session user snapshot carries attributes too (the GetSessionByTokenHash
	// JOIN), so GetProfile serves them without a DB round-trip.
	attrSessHash := "hash-of-attr-session." + suffix
	if _, err := repo.CreateSession(ctx, created.ID, attrSessHash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sess, err := repo.GetSessionByTokenHash(ctx, attrSessHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if sess.User == nil || sess.User.Attributes == nil || sess.User.Attributes["note"] != "Interne Notiz" {
		t.Errorf("session user attributes = %+v, want note=Interne Notiz", sess.User.Attributes)
	}

	// 3. PROFILE_UPDATE_CLEAR: an empty map REPLACES the whole JSONB map with
	// '{}' — no orphan keys survive (additive-union-free contract).
	cleared, err := repo.UpdateUserProfile(ctx, created.ID, "Erika", "Musterfrau", "Erika", map[string]any{})
	if err != nil {
		t.Fatalf("UpdateUserProfile(clear) failed: %v", err)
	}
	if len(cleared.Attributes) != 0 {
		t.Errorf("attributes after clear = %v, want empty", cleared.Attributes)
	}
	fetched, err = repo.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail(after clear) failed: %v", err)
	}
	if fetched == nil || len(fetched.Attributes) != 0 {
		t.Errorf("stored attributes after clear = %+v, want empty map", fetched.Attributes)
	}

	// 4. A nil attributes map also clears (COALESCE($5, '{}'::jsonb)).
	if _, err := repo.UpdateUserProfile(ctx, created.ID, "Erika", "Musterfrau", "Erika", nil); err != nil {
		t.Fatalf("UpdateUserProfile(nil attrs) failed: %v", err)
	}
	fetched, err = repo.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail(after nil clear) failed: %v", err)
	}
	if fetched == nil || len(fetched.Attributes) != 0 {
		t.Errorf("stored attributes after nil clear = %+v, want empty map", fetched.Attributes)
	}

	// 5. PROMOTION PATH (AD-3/NFR-R2) — demonstrated by fixture/note, NOT a
	// shipped migration: when a custom attribute becomes core, it is promoted to
	// a real typed column via a golang-migrate migration + backfill, e.g.:
	//
	//   -- 000009_promote_favorite_color.up.sql
	//   ALTER TABLE users ADD COLUMN favorite_color TEXT;
	//   UPDATE users SET favorite_color = attributes->>'favorite_color'
	//   WHERE attributes ? 'favorite_color';
	//   ALTER TABLE users DROP COLUMN favorite_color;  -- .down.sql
	//
	// The JSONB column is RETAINED for continued flexibility: core reads now use
	// the typed column, custom attributes keep flowing through `attributes`.
	// That migration would ship in the promotion story; this story only proves
	// the mechanism (write/read/clear) end to end above.
}

func TestPostgresMalformedStoredAttributes(t *testing.T) {
	// MALFORMED_STORED (Story 1.9 boundary, review finding): the users.attributes
	// jsonb column can never hold syntactically invalid JSON (Postgres validates
	// on write), but out-of-band writes can store a valid NON-OBJECT shape such
	// as an array or scalar. Reading such a value must surface a clear error —
	// never a crash and never a silent data-loss read that serves `{}`.
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
	email := "malformed." + suffix + "@gear.local"

	created, err := repo.CreateRegisteredUser(ctx, email, "Malformed Test", "Malformed", "Test", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// Simulate an out-of-band write that stored a non-object value in the
	// jsonb column. An array is valid JSONB, so Postgres accepts it; only the
	// Go-side object unmarshal rejects it.
	if _, err := pool.Exec(ctx, `UPDATE users SET attributes = $1::jsonb WHERE id = $2`, `[1,2,3]`, created.ID); err != nil {
		t.Fatalf("seeding malformed attributes failed: %v", err)
	}

	// 1. The repository profile read surfaces a clear error (no panic, no
	// silent nil).
	fetched, err := repo.GetUserByEmail(ctx, email)
	if err == nil {
		t.Fatalf("GetUserByEmail with a non-object attributes column must fail, got %+v", fetched)
	}
	if !strings.Contains(err.Error(), "invalid stored attributes jsonb") {
		t.Errorf("error = %q, want a clear 'invalid stored attributes jsonb' cause", err)
	}

	// 2. The session-resolution read (GetSessionByTokenHash) surfaces the same
	// clear error for the JOIN'd user — the RequireAuth gateway then answers
	// with the uniform envelope rather than crashing.
	sessHash := "hash-of-malformed-session." + suffix
	if _, err := repo.CreateSession(ctx, created.ID, sessHash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, sessHash); err == nil {
		t.Fatal("GetSessionByTokenHash with a non-object attributes column must fail")
	}
}

func TestPostgresPasswordResetRepository(t *testing.T) {
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

	testEmail := "reset.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	suffix := time.Now().Format("20060102150405.000000")
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Reset Test", "Reset", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if created.MustChangePassword {
		t.Error("fresh user must not carry must_change_password=true")
	}

	// 1. CreatePasswordResetToken stores the hash with a 30-min expiry; a second
	// request for the same user invalidates the first (only the latest valid).
	tokenHash1 := "hash-of-reset-token-1." + suffix
	if err := repo.CreatePasswordResetToken(ctx, created.ID, tokenHash1, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken(1) failed: %v", err)
	}
	tokenHash2 := "hash-of-reset-token-2." + suffix
	if err := repo.CreatePasswordResetToken(ctx, created.ID, tokenHash2, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken(2) failed: %v", err)
	}
	if _, err := repo.GetPasswordResetTokenByHash(ctx, tokenHash1); !errors.Is(err, core.ErrResetTokenInvalid) {
		t.Errorf("earlier token must be invalidated, got %v", err)
	}

	// 2. GetPasswordResetTokenByHash resolves the live token with its owner
	// (JOIN on users), including state and must_change_password.
	tok, err := repo.GetPasswordResetTokenByHash(ctx, tokenHash2)
	if err != nil {
		t.Fatalf("GetPasswordResetTokenByHash failed: %v", err)
	}
	if tok.UserID != created.ID {
		t.Errorf("token user = %q, want %q", tok.UserID, created.ID)
	}
	if tok.User == nil || tok.User.State != core.StatePendingApproval {
		t.Errorf("token owner = %+v, want the pending_approval owner", tok.User)
	}
	if tok.ExpiresAt.IsZero() {
		t.Error("token must carry an expiry")
	}

	// 3. Unknown hash maps to ErrResetTokenInvalid.
	if _, err := repo.GetPasswordResetTokenByHash(ctx, "no-such-hash"); !errors.Is(err, core.ErrResetTokenInvalid) {
		t.Errorf("unknown token error = %v, want ErrResetTokenInvalid", err)
	}

	// 4. Set/ClearUserMustChangePassword flip the forced-change flag.
	if err := repo.SetUserMustChangePassword(ctx, created.ID); err != nil {
		t.Fatalf("SetUserMustChangePassword failed: %v", err)
	}
	fetched, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if !fetched.MustChangePassword {
		t.Error("must_change_password must be true after SetUserMustChangePassword")
	}
	if err := repo.ClearUserMustChangePassword(ctx, created.ID); err != nil {
		t.Fatalf("ClearUserMustChangePassword failed: %v", err)
	}
	fetched, err = repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.MustChangePassword {
		t.Error("must_change_password must be false after ClearUserMustChangePassword")
	}

	// 5. DeletePasswordResetToken invalidates the token (single-use).
	if err := repo.DeletePasswordResetToken(ctx, tokenHash2); err != nil {
		t.Fatalf("DeletePasswordResetToken failed: %v", err)
	}
	if _, err := repo.GetPasswordResetTokenByHash(ctx, tokenHash2); !errors.Is(err, core.ErrResetTokenInvalid) {
		t.Errorf("deleted token error = %v, want ErrResetTokenInvalid", err)
	}

	// 6. IsUserInPermissionGroup: the fresh user is in no group; the seeded
	// admin IS in the admin group (Story 1.8).
	member, err := repo.IsUserInPermissionGroup(ctx, created.ID, "admin")
	if err != nil {
		t.Fatalf("IsUserInPermissionGroup(fresh) failed: %v", err)
	}
	if member {
		t.Error("fresh user must not be an admin-group member")
	}
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil {
		t.Fatalf("GetUserByEmail(admin) failed: %v", err)
	}
	if admin != nil {
		member, err = repo.IsUserInPermissionGroup(ctx, admin.ID, "admin")
		if err != nil {
			t.Fatalf("IsUserInPermissionGroup(admin) failed: %v", err)
		}
		if !member {
			t.Error("seeded admin must be an admin-group member")
		}
		member, err = repo.IsUserInPermissionGroup(ctx, admin.ID, "helfende")
		if err != nil {
			t.Fatalf("IsUserInPermissionGroup(admin, helfende) failed: %v", err)
		}
		if member {
			t.Error("admin must not be a member of the helfende group")
		}
	}
}

func TestPostgresConsumePasswordResetTokenAtomic(t *testing.T) {
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

	testEmail := "consume.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Consume Test", "Consume", "Test", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// 1. Sequential atomic consumption: the first Consume returns the token (and
	// deletes it); the second sees no row and maps to ErrResetTokenInvalid.
	tokenHash := "hash-of-consume-token." + time.Now().Format("20060102150405.000000")
	if err := repo.CreatePasswordResetToken(ctx, created.ID, tokenHash, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken failed: %v", err)
	}
	consumed, err := repo.ConsumePasswordResetToken(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ConsumePasswordResetToken(1) failed: %v", err)
	}
	if consumed == nil || consumed.UserID != created.ID {
		t.Fatalf("consumed token = %+v, want owner %q", consumed, created.ID)
	}
	if consumed.User == nil || consumed.User.State != core.StatePendingApproval {
		t.Errorf("consumed token owner = %+v, want the pending_approval owner", consumed.User)
	}
	if _, err := repo.ConsumePasswordResetToken(ctx, tokenHash); !errors.Is(err, core.ErrResetTokenInvalid) {
		t.Errorf("second consume error = %v, want ErrResetTokenInvalid (single-use)", err)
	}

	// 2. Concurrent atomic consumption: with a fresh token, exactly ONE of two
	// racing completions wins; the loser sees no row (review finding 1.8-5).
	raceHash := "hash-of-race-token." + time.Now().Format("20060102150405.000000")
	if err := repo.CreatePasswordResetToken(ctx, created.ID, raceHash, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken(race) failed: %v", err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := repo.ConsumePasswordResetToken(context.Background(), raceHash)
			results <- err
		}()
	}
	wins, rejects := 0, 0
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			wins++
		case errors.Is(err, core.ErrResetTokenInvalid):
			rejects++
		default:
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if wins != 1 || rejects != 1 {
		t.Errorf("concurrent consumes: wins=%d rejects=%d, want exactly 1 win and 1 reject", wins, rejects)
	}
	if _, err := repo.ConsumePasswordResetToken(ctx, raceHash); !errors.Is(err, core.ErrResetTokenInvalid) {
		t.Errorf("post-race consume error = %v, want ErrResetTokenInvalid", err)
	}
}

func TestPostgresExpiredTokenPurgeAndAnonymousAudit(t *testing.T) {
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

	testEmail := "purge.test." + time.Now().Format("20060102150405.000000") + "@gear.local"
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Purge Test", "Purge", "Test", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// 1. DeleteExpiredPasswordResetTokens removes only EXPIRED tokens of the
	// user (review finding 1.8-7); fresh ones survive.
	if err := repo.CreatePasswordResetToken(ctx, created.ID, "purge-expired."+time.Now().Format("20060102150405.000000"), time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("CreatePasswordResetToken(expired) failed: %v", err)
	}
	freshHash := "purge-fresh." + time.Now().Format("20060102150405.000000")
	if err := repo.CreatePasswordResetToken(ctx, created.ID, freshHash, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken(fresh) failed: %v", err)
	}
	if err := repo.DeleteExpiredPasswordResetTokens(ctx, created.ID); err != nil {
		t.Fatalf("DeleteExpiredPasswordResetTokens failed: %v", err)
	}
	if _, err := repo.GetPasswordResetTokenByHash(ctx, freshHash); err != nil {
		t.Errorf("fresh token must survive the purge, got %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`, created.ID).Scan(&remaining); err != nil {
		t.Fatalf("counting tokens failed: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining tokens = %d, want 1 (expired purged, fresh kept)", remaining)
	}

	// 2. InsertAuditEventAnonymous writes a row with a NULL actor (review
	// findings 1.8-3 / 1.8-10): unknown-email enumeration attempts leave a
	// trail (NFR-O1).
	op := core.AuditOperationPasswordResetRequestUnknown
	if err := repo.InsertAuditEventAnonymous(ctx, op); err != nil {
		t.Fatalf("InsertAuditEventAnonymous failed: %v", err)
	}
	var actorIsNull bool
	if err := pool.QueryRow(ctx,
		`SELECT actor_user_id IS NULL FROM audit_log WHERE operation = $1 ORDER BY created_at DESC LIMIT 1`,
		op).Scan(&actorIsNull); err != nil {
		t.Fatalf("reading anonymous audit row failed: %v", err)
	}
	if !actorIsNull {
		t.Error("anonymous audit row must have a NULL actor")
	}
}

func TestPostgresAdminRecoveryRepository(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	// Use the two seeded admins (FR-27/AD-13): admin.1 is the recovery target
	// (A), admin.2 the approving admin (B).
	adminA, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil || adminA == nil {
		t.Skip("seeded admin.1 not present — skipping admin recovery assertion")
	}
	adminB, err := repo.GetUserByEmail(ctx, "admin.2@gear.local")
	if err != nil || adminB == nil {
		t.Skip("seeded admin.2 not present — skipping admin recovery assertion")
	}

	// 1. CountActiveAdmins: with both seeded admins active, at least 2.
	n, err := repo.CountActiveAdmins(ctx)
	if err != nil {
		t.Fatalf("CountActiveAdmins failed: %v", err)
	}
	if n < 2 {
		t.Errorf("CountActiveAdmins = %d, want >= 2", n)
	}

	// Drive the REAL core service end-to-end (create request → approve →
	// complete) so the immutable audit rows (NFR-O2) are written by the core.
	hasher := crypto.NewHasher()
	sm := core.NewSessionManager(repo, time.Hour)
	svc := core.NewService(repo, hasher, sm, nil, discardLogger())

	// 2. RequestAdminRecovery creates a recovery request for admin A.
	if _, err := svc.RequestAdminRecovery(ctx, adminA, adminA.Email); err != nil {
		t.Fatalf("RequestAdminRecovery failed: %v", err)
	}

	// 3. ApproveAdminRecovery (admin B, with Begründung + confirmation) returns
	// the single-use raw token.
	approved, err := svc.ApproveAdminRecovery(ctx, adminB, adminA.Email, "Admin A ist ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("ApproveAdminRecovery failed: %v", err)
	}
	if approved.RecoveryToken == "" {
		t.Fatal("approve must return a raw recovery token")
	}

	// 3b. The approve audit row must carry the Begründung in operation_detail and
	// severity='high' (review finding 1.10) — persisted, not just logged.
	var approveDetail string
	var approveSeverity string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(operation_detail,''), severity FROM audit_log WHERE actor_user_id = $1 AND operation = $2 ORDER BY created_at DESC LIMIT 1`,
		adminB.ID, core.AuditOperationAdminRecoveryApprove).Scan(&approveDetail, &approveSeverity); err != nil {
		t.Fatalf("reading approve audit detail failed: %v", err)
	}
	if !strings.Contains(approveDetail, "Admin A ist ausgesperrt") || !strings.Contains(approveDetail, "target="+adminA.Email) {
		t.Errorf("approve audit detail = %q, want reason+target persisted", approveDetail)
	}
	if approveSeverity != core.AuditSeverityHigh {
		t.Errorf("approve audit severity = %q, want %q", approveSeverity, core.AuditSeverityHigh)
	}

	// 4. CompleteAdminRecovery consumes the approved token, setting a new
	// Argon2id password hash for admin A.
	if _, err := svc.CompleteAdminRecovery(ctx, approved.RecoveryToken, "neuesadminpass123", "neuesadminpass123"); err != nil {
		t.Fatalf("CompleteAdminRecovery failed: %v", err)
	}
	// The token is single-use: a second completion is rejected.
	if _, err := svc.CompleteAdminRecovery(ctx, approved.RecoveryToken, "anderespass456", "anderespass456"); !errors.Is(err, core.ErrAdminRecoveryInvalid) {
		t.Fatalf("second completion error = %v, want ErrAdminRecoveryInvalid (single-use)", err)
	}

	// 5. Audit rows (NFR-O2): request (actor A), approve (actor B), complete
	// (actor A) are all present.
	var requestRows, approveRows, completeRows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE actor_user_id = $1 AND operation = $2`,
		adminA.ID, core.AuditOperationAdminRecoveryRequest).Scan(&requestRows); err != nil {
		t.Fatalf("counting request audit rows failed: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE actor_user_id = $1 AND operation = $2`,
		adminB.ID, core.AuditOperationAdminRecoveryApprove).Scan(&approveRows); err != nil {
		t.Fatalf("counting approve audit rows failed: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE actor_user_id = $1 AND operation = $2`,
		adminA.ID, core.AuditOperationAdminRecoveryComplete).Scan(&completeRows); err != nil {
		t.Fatalf("counting complete audit rows failed: %v", err)
	}
	if requestRows < 1 || approveRows < 1 || completeRows < 1 {
		t.Errorf("audit rows: request=%d (want>=1), approve=%d (want>=1), complete=%d (want>=1)",
			requestRows, approveRows, completeRows)
	}

	// 6. A pending (not-yet-approved) token created via the repository is NOT
	// consumable through the repository's atomic consume.
	suffix := time.Now().Format("20060102150405.000000")
	if err := repo.CreateAdminRecoveryRequest(ctx, adminA.ID, adminB.ID, "pending-"+suffix, time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreateAdminRecoveryRequest(pending) failed: %v", err)
	}
	if _, err := repo.ConsumeAdminRecoveryToken(ctx, "pending-"+suffix); !errors.Is(err, core.ErrAdminRecoveryInvalid) {
		t.Fatalf("consuming a pending token error = %v, want ErrAdminRecoveryInvalid", err)
	}

	// 7. Restore the seeded admin.1 password hash (identity-only seed; the flow
	// above set a test hash) so later tests are not affected.
	if _, err := repo.UpdateUserPassword(ctx, adminA.ID, adminA.PasswordHash); err != nil {
		t.Errorf("restoring admin.1 password hash failed: %v", err)
	}
}

// basePermissionCodes is the AD-12 base series seeded by migration 000010
// (Story 2.2): the 22 codes the architecture spine maps every action to.
func basePermissionCodes() []string {
	return []string{
		"admin.recovery.approve",
		"admin.settings.backup",
		"admin.settings.email",
		"dashboard.view",
		"dsgvo.access_report",
		"dsgvo.delete",
		"inspection.history.view",
		"inspection.submit",
		"qualifications.manage",
		"report.export",
		"roles.assign",
		"roles.create",
		"roles.edit",
		"schedules.manage",
		"tool.reinstate",
		"tool_types.manage",
		"tools.manage",
		"user_groups.manage",
		"users.approve",
		"users.manage",
		"users.qualifications.manage",
		"users.view",
	}
}

// sameCodeSet reports whether a and b contain the same codes regardless of
// order (used to assert resolved permission sets against the AD-12 matrix).
// Each matched code is consumed as it is used, so a duplicate in `a` that is
// not matched by a duplicate in `b` fails (a=["x","x"], b=["x","y"] returns
// false) instead of slipping through the membership check.
func sameCodeSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(b))
	for _, c := range b {
		set[c]++
	}
	for _, c := range a {
		if set[c] == 0 {
			return false
		}
		set[c]--
	}
	return true
}

// containsAllCodes reports whether got contains every code in want (a subset
// check: a DB that already holds unrelated permission rows must not break the
// base-series assertion).
func containsAllCodes(got, want []string) bool {
	present := make(map[string]bool, len(got))
	for _, c := range got {
		present[c] = true
	}
	for _, c := range want {
		if !present[c] {
			return false
		}
	}
	return true
}

// TestPostgresBasePermissionSeedResolution verifies the seed migration 000010
// end to end (Story 2.2, I/O matrix): all 22 base codes are installed, each
// base role resolves its matrix, a multi-role user resolves a DEDUPLICATED
// union, a direct grant joins the union, and revocation is immediate (no
// cache). Test users are deleted via t.Cleanup (CASCADE removes memberships
// and direct grants).
func TestPostgresBasePermissionSeedResolution(t *testing.T) {
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

	// A dedicated pool for t.Cleanup: the test's own pool and context are torn
	// down (deferred cancel/close) BEFORE cleanup callbacks run, so the user
	// deletions must use independent resources. Registered first so it closes
	// LAST (t.Cleanup is LIFO).
	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })

	// 1. All 22 base codes are present in the permissions table. A set
	// comparison (subset check), so a DB that already holds unrelated
	// permission rows does not break the assertion.
	var permCodes []string
	codeRows, err := pool.Query(ctx, `SELECT code FROM permissions`)
	if err != nil {
		t.Fatalf("listing permissions failed: %v", err)
	}
	for codeRows.Next() {
		var c string
		if err := codeRows.Scan(&c); err != nil {
			t.Fatalf("scanning permission code failed: %v", err)
		}
		permCodes = append(permCodes, c)
	}
	codeRows.Close()
	if err := codeRows.Err(); err != nil {
		t.Fatalf("reading permissions failed: %v", err)
	}
	if !containsAllCodes(permCodes, basePermissionCodes()) {
		t.Errorf("permissions table = %v, missing base codes from the AD-12 series", permCodes)
	}

	// 2. The seeded admin resolves ALL 22 codes via the admin-group matrix.
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil || admin == nil {
		t.Skip("seeded admin not present — skipping admin resolution assertion")
	}
	adminPerms, err := repo.ListPermissionsByUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser(admin) failed: %v", err)
	}
	if !sameCodeSet(adminPerms, basePermissionCodes()) {
		t.Errorf("admin permissions = %v, want the full 22-code base series", adminPerms)
	}

	// newGroupUser creates a fresh user, assigns it to the named group(s) and
	// registers a cleanup that deletes it (CASCADE removes memberships and
	// direct grants).
	ts := time.Now().Format("20060102150405.000000")
	n := 0
	newGroupUser := func(groups ...string) *core.User {
		t.Helper()
		n++
		email := fmt.Sprintf("perm.test.%s.%d@gear.local", ts, n)
		u, err := repo.CreateRegisteredUser(ctx, email, "Perm Test", "Perm", "Test", "$argon2id$v=19$dummyhash")
		if err != nil {
			t.Fatalf("CreateRegisteredUser failed: %v", err)
		}
		for _, g := range groups {
			if _, err := pool.Exec(ctx, `
				INSERT INTO user_permission_groups (user_id, permission_group_id)
				SELECT $1, g.id FROM permission_groups g WHERE g.name = $2`, u.ID, g); err != nil {
				t.Fatalf("assigning %s group failed: %v", g, err)
			}
		}
		t.Cleanup(func() {
			// Runs after the test's deferred cancel()/pool.Close(), so use the
			// dedicated cleanup pool with a fresh context.
			if _, err := cleanupPool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID); err != nil {
				t.Errorf("cleaning up test user %s failed: %v", u.ID, err)
			}
		})
		return u
	}

	resolve := func(u *core.User) []string {
		t.Helper()
		perms, err := repo.ListPermissionsByUser(ctx, u.ID)
		if err != nil {
			t.Fatalf("ListPermissionsByUser failed: %v", err)
		}
		return perms
	}

	// 3. Base-role matrix resolution (AD-12).
	helfende := newGroupUser("helfende")
	if got := resolve(helfende); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit"}) {
		t.Errorf("helfende permissions = %v, want [dashboard.view inspection.submit]", got)
	}

	schirrmeister := newGroupUser("schirrmeister")
	// 9 codes after migrations 000011 + 000013 (user decision + Spec 2.9):
	// schirrmeister now also carries users.view + users.qualifications.manage.
	if got := resolve(schirrmeister); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage", "users.qualifications.manage", "users.view"}) {
		t.Errorf("schirrmeister permissions = %v, want [dashboard.view inspection.submit tools.manage tool_types.manage users.qualifications.manage users.view]", got)
	}

	fuehrende := newGroupUser("fuehrende")
	// 9 codes after migrations 000011 + 000013 (Story 2.3 user decision + Spec
	// 2.9): fuehrende carries tools.manage + tool_types.manage like
	// schirrmeister, plus users.view + users.qualifications.manage.
	if got := resolve(fuehrende); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit", "inspection.history.view", "report.export", "tool.reinstate", "tools.manage", "tool_types.manage", "users.qualifications.manage", "users.view"}) {
		t.Errorf("fuehrende permissions = %v, want [dashboard.view inspection.submit inspection.history.view report.export tool.reinstate tools.manage tool_types.manage users.qualifications.manage users.view]", got)
	}

	// 4. UNION/DISTINCT: a user in helfende + schirrmeister (BOTH grant
	// dashboard.view + inspection.submit) resolves a DEDUPLICATED set — no
	// repeated codes.
	multi := newGroupUser("helfende", "schirrmeister")
	if got := resolve(multi); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage", "users.qualifications.manage", "users.view"}) {
		t.Errorf("multi-role permissions = %v, want the deduplicated union (no repeated codes)", got)
	}

	// 5. A direct grant joins the union (user_permissions, AD-12).
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_permissions (user_id, permission_id)
		SELECT $1, p.id FROM permissions p WHERE p.code = 'report.export'`, helfende.ID); err != nil {
		t.Fatalf("granting direct permission failed: %v", err)
	}
	if got := resolve(helfende); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit", "report.export"}) {
		t.Errorf("permissions after direct grant = %v, want [dashboard.view inspection.submit report.export]", got)
	}

	// 6. Revocation is immediate: removing the direct grant is reflected on the
	// very next resolution (no cache, AD-2/FR-21/FR-22).
	if _, err := pool.Exec(ctx, `
		DELETE FROM user_permissions
		WHERE user_id = $1 AND permission_id IN (SELECT id FROM permissions WHERE code = 'report.export')`, helfende.ID); err != nil {
		t.Fatalf("revoking direct permission failed: %v", err)
	}
	if got := resolve(helfende); !sameCodeSet(got, []string{"dashboard.view", "inspection.submit"}) {
		t.Errorf("permissions after revoke = %v, want [dashboard.view inspection.submit]", got)
	}
}

// TestPostgresUserApprovalRepository covers the Story 2.4 persistence contract
// (FR-20): list pending users oldest-first (profile details only, no secrets),
// approve → active + helfende seed (idempotent, atomic), reject → deactivated
// and removed from the pending surface, plus the full core flow — audit rows
// (user.approve / user.reject) and login-after-approve — against the REAL
// postgres repository.
func TestPostgresUserApprovalRepository(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	stamp := time.Now().Format("20060102150405.000000")
	p1Email := "approval.p1." + stamp + "@gear.local"
	p2Email := "approval.p2." + stamp + "@gear.local"
	p3Email := "approval.p3." + stamp + "@gear.local"

	// Fresh pending users; cleanup deletes ONLY these rows (audit actor refs
	// are ON DELETE SET NULL, groups/sessions cascade), never shared data.
	for _, email := range []string{p1Email, p2Email, p3Email} {
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
		})
	}

	createPending := func(email, first, last string) *core.User {
		t.Helper()
		u, err := repo.CreateRegisteredUser(ctx, email, first+" "+last, first, last, "$argon2id$v=19$dummyhash")
		if err != nil {
			t.Fatalf("CreateRegisteredUser(%s) failed: %v", email, err)
		}
		return u
	}

	p1 := createPending(p1Email, "Tim", "Müller")
	p2 := createPending(p2Email, "Lena", "Schmidt")
	p3 := createPending(p3Email, "Nils", "Becker")

	// Deterministic ordering: backdate p1 so it is strictly the oldest.
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() - interval '2 minutes' WHERE id = $1", p1.ID); err != nil {
		t.Fatalf("backdating p1 failed: %v", err)
	}

	// LIST_PENDING: oldest first, profile details only. The shared dev DB holds
	// pending rows from other test binaries, so only the RELATIVE order of this
	// test's three users is asserted — p1 (backdated) must sort before p2, p2
	// before p3.
	pending, err := repo.ListPendingUsers(ctx)
	if err != nil {
		t.Fatalf("ListPendingUsers failed: %v", err)
	}
	idx := map[string]int{}
	for i, p := range pending {
		idx[p.ID] = i
		if p.Vorname == "" || p.Nachname == "" || p.Email == "" || p.CreatedAt.IsZero() {
			t.Errorf("pending[%d] missing profile detail: %+v", i, p)
		}
	}
	if i1, ok := idx[p1.ID]; !ok {
		t.Errorf("p1 missing from pending list")
	} else if i2, ok := idx[p2.ID]; !ok {
		t.Errorf("p2 missing from pending list")
	} else if i3, ok := idx[p3.ID]; !ok {
		t.Errorf("p3 missing from pending list")
	} else if i1 >= i2 || i2 >= i3 {
		t.Errorf("pending order wrong: p1=%d p2=%d p3=%d, want p1 < p2 < p3", i1, i2, i3)
	}

	// APPROVE_VALID: state → active, helfende role seeded exactly once.
	approved, err := repo.ApproveUser(ctx, p1.ID)
	if err != nil {
		t.Fatalf("ApproveUser(p1) failed: %v", err)
	}
	if approved.State != core.StateActive {
		t.Errorf("approved state = %q, want active", approved.State)
	}
	if !groupMembershipCount(t, pool, approved.ID, core.DefaultUserRoleGroup, 1) {
		t.Fatalf("helfende membership not exactly 1 after approve")
	}

	// APPROVE_NONPENDING: a second approve of the same (now active) user is a
	// no-op error — no duplicate role row.
	if _, err := repo.ApproveUser(ctx, p1.ID); !errors.Is(err, core.ErrUserNotPending) {
		t.Errorf("re-approve err = %v, want ErrUserNotPending", err)
	}
	if !groupMembershipCount(t, pool, approved.ID, core.DefaultUserRoleGroup, 1) {
		t.Errorf("helfende membership duplicated after re-approve")
	}

	// APPROVE_IDEMPOTENT: a pending user ALREADY in helfende is approved without
	// duplicating the membership (ON CONFLICT DO NOTHING).
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_permission_groups (user_id, permission_group_id)
		SELECT $1, g.id FROM permission_groups g WHERE g.name = 'helfende'`, p3.ID); err != nil {
		t.Fatalf("pre-seeding helfende for p3 failed: %v", err)
	}
	if _, err := repo.ApproveUser(ctx, p3.ID); err != nil {
		t.Fatalf("ApproveUser(p3, already in helfende) failed: %v", err)
	}
	if !groupMembershipCount(t, pool, p3.ID, core.DefaultUserRoleGroup, 1) {
		t.Errorf("helfende membership duplicated for pre-seeded p3")
	}

	// APPROVE_UNKNOWN: a nonexistent id maps to the uniform not-found error.
	if _, err := repo.ApproveUser(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrUserNotPending) {
		t.Errorf("approve unknown err = %v, want ErrUserNotPending", err)
	}

	// APPROVE_UNKNOWN (malformed id): a non-UUID is treated EXACTLY like an
	// unknown id — a UUID parse failure must NOT surface as a raw 500 error but
	// map to the same uniform not-found the admin already sees (FR-19).
	if _, err := repo.ApproveUser(ctx, "not-a-uuid"); !errors.Is(err, core.ErrUserNotPending) {
		t.Errorf("approve malformed id err = %v, want ErrUserNotPending", err)
	}
	if _, err := repo.RejectUser(ctx, "not-a-uuid"); !errors.Is(err, core.ErrUserNotPending) {
		t.Errorf("reject malformed id err = %v, want ErrUserNotPending", err)
	}

	// REJECT_VALID: state → deactivated and gone from the pending surface.
	rejected, err := repo.RejectUser(ctx, p2.ID)
	if err != nil {
		t.Fatalf("RejectUser(p2) failed: %v", err)
	}
	if rejected.State != core.StateDeactivated {
		t.Errorf("rejected state = %q, want deactivated", rejected.State)
	}
	pending, err = repo.ListPendingUsers(ctx)
	if err != nil {
		t.Fatalf("ListPendingUsers after reject failed: %v", err)
	}
	for _, p := range pending {
		if p.ID == p2.ID {
			t.Errorf("rejected user still in pending list: %+v", p)
		}
	}

	// REJECT_NONPENDING: a second reject of the (now deactivated) user errors.
	if _, err := repo.RejectUser(ctx, p2.ID); !errors.Is(err, core.ErrUserNotPending) {
		t.Errorf("re-reject err = %v, want ErrUserNotPending", err)
	}

	// Full core flow against the REAL repo: an admin approves → audit row
	// (user.approve, actor + target email) AND login succeeds (AD-2/AD-6);
	// a rejection writes the user.reject audit.
	hasher := crypto.NewHasher()
	sm := core.NewSessionManager(repo, time.Hour)
	svc := core.NewService(repo, hasher, sm, crypto.NewSecretCipher(make([]byte, 32)), discardLogger())

	adminEmail := "approval.admin." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", adminEmail)
	})
	admin, err := repo.CreateRegisteredUser(ctx, adminEmail, "Vera Waltung", "Vera", "Waltung", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("creating approval admin failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET state = 'active' WHERE id = $1", admin.ID); err != nil {
		t.Fatalf("activating approval admin failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_permission_groups (user_id, permission_group_id)
		SELECT $1, g.id FROM permission_groups g WHERE g.name = 'admin'`, admin.ID); err != nil {
		t.Fatalf("granting admin role failed: %v", err)
	}
	admin.State = core.StateActive

	// A fresh pending volunteer with a REAL password hash so login-after-approve
	// exercises the actual Argon2id verify.
	volEmail := "approval.vol." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", volEmail)
	})
	volPassword := "freiwillig123"
	volHash, err := hasher.Hash(volPassword)
	if err != nil {
		t.Fatalf("hashing volunteer password failed: %v", err)
	}
	vol, err := repo.CreateRegisteredUser(ctx, volEmail, "Frei Willig", "Frei", "Willig", volHash)
	if err != nil {
		t.Fatalf("creating volunteer failed: %v", err)
	}

	approveRes, err := svc.ApproveUser(ctx, admin, vol.ID)
	if err != nil {
		t.Fatalf("svc.ApproveUser failed: %v", err)
	}
	if approveRes.Email != volEmail {
		t.Errorf("approve result email = %q, want %q", approveRes.Email, volEmail)
	}
	var approveAudits int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE operation = 'user.approve' AND operation_detail = 'target=' || $1`, volEmail).Scan(&approveAudits); err != nil {
		t.Fatalf("counting user.approve audit failed: %v", err)
	}
	if approveAudits != 1 {
		t.Errorf("user.approve audit rows = %d, want 1", approveAudits)
	}

	// LOGIN_AFTER_APPROVE: the approved volunteer logs in with resolved
	// permissions (the default helfende set, AD-2/AD-6).
	loginRes, err := svc.Login(ctx, core.LoginInput{Email: volEmail, Password: volPassword})
	if err != nil {
		t.Fatalf("login after approve failed: %v", err)
	}
	if loginRes.Token == "" {
		t.Errorf("login returned no session token")
	}

	// REJECT_VALID (core): audit row user.reject with the target email. A
	// fresh pending user is used — p3 was approved above.
	rejectEmail := "approval.rej." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", rejectEmail)
	})
	rejectTarget := createPending(rejectEmail, "Paul", "Abgelehnt")
	rejectRes, err := svc.RejectUser(ctx, admin, rejectTarget.ID)
	if err != nil {
		t.Fatalf("svc.RejectUser failed: %v", err)
	}
	if rejectRes.Email != rejectEmail {
		t.Errorf("reject result email = %q, want %q", rejectRes.Email, rejectEmail)
	}
	var rejectAudits int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE operation = 'user.reject' AND operation_detail = 'target=' || $1`, rejectEmail).Scan(&rejectAudits); err != nil {
		t.Fatalf("counting user.reject audit failed: %v", err)
	}
	if rejectAudits != 1 {
		t.Errorf("user.reject audit rows = %d, want 1", rejectAudits)
	}
}

// TestPostgresApproveUserRoleSeedRollback proves the all-or-nothing invariant
// of ApproveUser (Story 2.4, AD-2): when the role-seed step fails AFTER a valid
// state flip, the whole transaction rolls back — the user's state stays
// `pending_approval` and no membership row is committed. The seed failure is
// forced by renaming the 'helfende' group mid-test (an INSERT ... SELECT with a
// no-longer-matching group is a zero-row no-op, which the seed guard now
// detects and turns into a rollback). Renames are safe because this suite runs
// without t.Parallel and no other test asserts on the helfende membership count.
func TestPostgresApproveUserRoleSeedRollback(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	stamp := time.Now().Format("20060102150405.000000")
	email := "approval.rollback." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})
	u, err := repo.CreateRegisteredUser(ctx, email, "Roll Back", "Roll", "Back", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// Force the seed to fail: temporarily rename the 'helfende' group so the
	// INSERT ... SELECT resolves no row (a silent no-op). Restore it in ALL
	// paths (deferred) so the shared DB is left untouched.
	renameGroup := func(from, to string) {
		if _, err := pool.Exec(ctx, `UPDATE permission_groups SET name = $2 WHERE name = $1`, from, to); err != nil {
			t.Fatalf("renaming %q group failed: %v", from, err)
		}
	}
	const tmpName = "helfende_rollback_tmp"
	renameGroup(core.DefaultUserRoleGroup, tmpName)
	// Safety net: if a panic or Fatal interrupts before the immediate restore
	// below, this defer still restores the group. Registered AFTER defer
	// pool.Close(), so LIFO ordering runs it FIRST while the pool is open
	// (t.Cleanup would run after pool.Close and fail — see the shared-pool
	// cleanup pattern in this file).
	defer func() {
		if _, err := pool.Exec(context.Background(), `UPDATE permission_groups SET name = $2 WHERE name = $1`, tmpName, core.DefaultUserRoleGroup); err != nil {
			t.Errorf("restoring %q group failed: %v", core.DefaultUserRoleGroup, err)
		}
	}()

	_, err = repo.ApproveUser(ctx, u.ID)
	// Restore immediately (the deferred safety net is then a no-op) so a Fatal
	// in the assertions below cannot leave the shared DB renamed.
	renameGroup(tmpName, core.DefaultUserRoleGroup)

	if err == nil {
		t.Fatalf("ApproveUser with missing helfende group succeeded; want a seed-failure error")
	}

	// All-or-nothing: the state flip was NOT committed.
	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM users WHERE id = $1", u.ID).Scan(&state); err != nil {
		t.Fatalf("reading state after failed approve failed: %v", err)
	}
	if state != string(core.StatePendingApproval) {
		t.Errorf("state after failed approve = %q, want pending_approval (rollback)", state)
	}

	// And no membership row was committed (either under the tmp name or a
	// leftover under the real name — the rollback must have removed it).
	if groupMembershipCount(t, pool, u.ID, core.DefaultUserRoleGroup, 0) {
		if n := groupMembershipRawCount(t, pool, u.ID); n != 0 {
			t.Errorf("membership rows after failed approve = %d, want 0 (rollback)", n)
		}
	}
}

// groupMembershipRawCount counts ALL permission-group memberships of a user
// regardless of group name (used to prove the rollback removed every row).
func groupMembershipRawCount(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_permission_groups WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("counting raw memberships failed: %v", err)
	}
	return n
}

// groupMembershipCount reports whether the user holds EXACTLY want rows of the
// named permission group (used to assert the idempotent helfende seed).
func groupMembershipCount(t *testing.T, pool *pgxpool.Pool, userID, group string, want int) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM user_permission_groups upg
		JOIN permission_groups g ON g.id = upg.permission_group_id
		WHERE upg.user_id = $1 AND g.name = $2`, userID, group).Scan(&n); err != nil {
		t.Fatalf("counting group membership failed: %v", err)
	}
	return n == want
}
