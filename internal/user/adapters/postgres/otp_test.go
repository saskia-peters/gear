package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresOneTimePasswordContract covers the Spec 2.8 persistence contract
// against the REAL postgres repository: the migration 000014 columns on users,
// the set/clear compare-and-swap (single-use under concurrency) and the
// GetUserByEmail shape carrying the OTP fields.
func TestPostgresOneTimePasswordContract(t *testing.T) {
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
	email := "otp." + stamp + "@gear.local"

	// Cleanup with an INDEPENDENT pool (t.Cleanup runs AFTER the test's deferred
	// pool.Close(); registered first so it closes LAST — LIFO).
	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email); err != nil {
			t.Errorf("cleaning up user %q failed: %v", email, err)
		}
	})

	// Seed an ACTIVE user (credentials provisioned out-of-band, admin surface).
	user, err := repo.CreateAdminUser(ctx, email, "OTP", "Test", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	userID := user.ID

	// MIGRATION COLUMNS: migration 000014 adds the two OTP columns on users.
	var col string
	if err := pool.QueryRow(ctx,
		"SELECT column_name FROM information_schema.columns WHERE table_name='users' AND column_name='one_time_password_hash'").Scan(&col); err != nil {
		t.Fatalf("one_time_password_hash column missing: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT column_name FROM information_schema.columns WHERE table_name='users' AND column_name='one_time_password_expires_at'").Scan(&col); err != nil {
		t.Fatalf("one_time_password_expires_at column missing: %v", err)
	}

	// SET: persist the OTP hash + expiry + must_change_password in one write.
	expiresAt := time.Now().UTC().Add(core.OneTimePasswordTTL)
	written, err := repo.SetUserOneTimePassword(ctx, userID, "argon2id:otp-hash", expiresAt)
	if err != nil {
		t.Fatalf("SetUserOneTimePassword failed: %v", err)
	}
	if !written {
		t.Fatal("SetUserOneTimePassword reported zero rows for an existing target")
	}

	// GETUSERBYEMAIL SHAPE: the resolved user carries the OTP fields + flag.
	byEmail, err := repo.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if byEmail == nil {
		t.Fatal("GetUserByEmail returned nil user")
	}
	if byEmail.OneTimePasswordHash != "argon2id:otp-hash" {
		t.Errorf("stored OTP hash = %q, want the argon2id hash (never plaintext)", byEmail.OneTimePasswordHash)
	}
	if byEmail.OneTimePasswordExpiresAt.IsZero() {
		t.Error("OTP expiry not returned by GetUserByEmail")
	}
	if !byEmail.MustChangePassword {
		t.Error("SetUserOneTimePassword must flag must_change_password")
	}

	// GETUSERBYID: the issuance target resolver (active-check).
	byID, err := repo.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if byID.ID != userID || byID.State != core.StateActive {
		t.Errorf("GetUserByID = %+v, want the active target", byID)
	}
	if _, err := repo.GetUserByID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("GetUserByID unknown err = %v, want ErrAdminUserNotFound", err)
	}
	if _, err := repo.GetUserByID(ctx, "not-a-uuid"); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("GetUserByID malformed err = %v, want ErrAdminUserNotFound", err)
	}

	// CAS CLEAR — wrong hash: zero rows affected, OTP survives.
	consumed, err := repo.ClearUserOneTimePassword(ctx, userID, "argon2id:wrong")
	if err != nil {
		t.Fatalf("ClearUserOneTimePassword (wrong hash) failed: %v", err)
	}
	if consumed {
		t.Error("clearing with a WRONG hash must affect zero rows (CAS)")
	}
	byEmail, _ = repo.GetUserByEmail(ctx, email)
	if byEmail.OneTimePasswordHash != "argon2id:otp-hash" {
		t.Error("OTP hash must survive a wrong-hash clear")
	}

	// CAS CLEAR — correct hash: consumed once.
	consumed, err = repo.ClearUserOneTimePassword(ctx, userID, "argon2id:otp-hash")
	if err != nil {
		t.Fatalf("ClearUserOneTimePassword failed: %v", err)
	}
	if !consumed {
		t.Error("clearing with the CORRECT hash must affect one row")
	}
	byEmail, _ = repo.GetUserByEmail(ctx, email)
	if byEmail.OneTimePasswordHash != "" {
		t.Error("OTP hash must be cleared after consumption")
	}
	if !byEmail.OneTimePasswordExpiresAt.IsZero() {
		t.Error("OTP expiry must be cleared after consumption")
	}

	// CAS CLEAR — reuse: the already-consumed OTP is not accepted again.
	consumed, err = repo.ClearUserOneTimePassword(ctx, userID, "argon2id:otp-hash")
	if err != nil {
		t.Fatalf("ClearUserOneTimePassword (reuse) failed: %v", err)
	}
	if consumed {
		t.Error("re-consuming a cleared OTP must affect zero rows (single-use)")
	}

	// CONCURRENT CAS: re-issue the OTP, then eight goroutines race the consume —
	// exactly ONE wins (single-use under concurrency, Spec 2.8 Design Notes).
	if _, err := repo.SetUserOneTimePassword(ctx, userID, "argon2id:race-hash", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("SetUserOneTimePassword (race) failed: %v", err)
	}
	var mu sync.Mutex
	wins := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.ClearUserOneTimePassword(context.Background(), userID, "argon2id:race-hash")
			if err != nil {
				t.Errorf("concurrent clear failed: %v", err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Errorf("concurrent CAS consume: %d winners, want exactly 1", wins)
	}
}