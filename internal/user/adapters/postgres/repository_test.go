package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/user/core"
)

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
	if err := repo.InsertAuditEvent(ctx, created.ID, op); err != nil {
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
