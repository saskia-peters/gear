package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
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

	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Auth Test", "Auth", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if created.State != core.StatePendingApproval {
		t.Fatalf("created.State = %q, want pending_approval", created.State)
	}

	// 1. CreateSession + GetSessionByTokenHash round-trip.
	expiry := time.Now().UTC().Add(time.Hour)
	sess, err := repo.CreateSession(ctx, created.ID, "hash-of-raw-token", expiry)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.UserID != created.ID {
		t.Errorf("session user id = %q, want %q", sess.UserID, created.ID)
	}

	fetched, err := repo.GetSessionByTokenHash(ctx, "hash-of-raw-token")
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
	if err := repo.DeleteSessionByTokenHash(ctx, "hash-of-raw-token"); err != nil {
		t.Fatalf("DeleteSessionByTokenHash failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, "hash-of-raw-token"); err != core.ErrSessionNotFound {
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
	sess, err := repo.CreateSession(ctx, created.ID, "hash-of-totp-session", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched, err := repo.GetSessionByTokenHash(ctx, "hash-of-totp-session")
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
	sess2, err := repo.CreateSession(ctx, created.ID, "hash-of-pending-session", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched2, err := repo.GetSessionByTokenHash(ctx, "hash-of-pending-session")
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
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Revoke Test", "Revoke", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// Create three sessions.
	expiry := time.Now().UTC().Add(time.Hour)
	for i := 1; i <= 3; i++ {
		hash := fmt.Sprintf("hash-revoke-%d", i)
		if _, err := repo.CreateSession(ctx, created.ID, hash, expiry); err != nil {
			t.Fatalf("CreateSession(%s) failed: %v", hash, err)
		}
	}

	// DeleteSessionsByUserExcept keeps the excepted session.
	if err := repo.DeleteSessionsByUserExcept(ctx, created.ID, "hash-revoke-2"); err != nil {
		t.Fatalf("DeleteSessionsByUserExcept failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, "hash-revoke-2"); err != nil {
		t.Errorf("excepted session must survive, got %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, "hash-revoke-1"); !errors.Is(err, core.ErrSessionNotFound) {
		t.Errorf("non-excepted session must be revoked, got %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, "hash-revoke-3"); !errors.Is(err, core.ErrSessionNotFound) {
		t.Errorf("non-excepted session must be revoked, got %v", err)
	}

	// DeleteSessionsByUser revokes everything left.
	if err := repo.DeleteSessionsByUser(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSessionsByUser failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, "hash-revoke-2"); !errors.Is(err, core.ErrSessionNotFound) {
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
	if _, err := repo.CreateSession(ctx, created.ID, "hash-of-changepw-session", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessFetched, err := repo.GetSessionByTokenHash(ctx, "hash-of-changepw-session")
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
