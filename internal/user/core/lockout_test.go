package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckNotLocked(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		attempts *LoginAttempts
	}{
		{name: "no attempts record", attempts: nil},
		{name: "no lockout window set", attempts: &LoginAttempts{FailedCount: 2}},
		{name: "lockout in the past (expired)", attempts: &LoginAttempts{FailedCount: 3, LockoutUntil: now.Add(-1 * time.Second)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if retryAfter, locked := Check(tt.attempts, now); locked {
				t.Fatalf("Check locked = true (retryAfter %v), want not locked", retryAfter)
			}
		})
	}
}

func TestCheckLockedWindow(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		lockoutTime time.Time
		wantSeconds int
	}{
		{name: "30s window remaining", lockoutTime: now.Add(30 * time.Second), wantSeconds: 30},
		{name: "60s window remaining", lockoutTime: now.Add(60 * time.Second), wantSeconds: 60},
		{name: "partial second rounds up", lockoutTime: now.Add(30*time.Second + 500*time.Millisecond), wantSeconds: 31},
		{name: "sub-second remaining floors to 1", lockoutTime: now.Add(100 * time.Millisecond), wantSeconds: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryAfter, locked := Check(&LoginAttempts{FailedCount: 3, LockoutUntil: tt.lockoutTime}, now)
			if !locked {
				t.Fatal("Check locked = false, want locked")
			}
			if int(retryAfter.Seconds()) != tt.wantSeconds {
				t.Fatalf("retryAfter = %v, want %ds", retryAfter, tt.wantSeconds)
			}
		})
	}
}

func TestLockoutRetryAfterHelper(t *testing.T) {
	err := NewLockoutError(30*time.Second, "active@example.com")
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("errors.Is(err, ErrLockedOut) = false, want true")
	}
	if retryAfter, ok := LockoutRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("LockoutRetryAfter = (%v, %v), want (30s, true)", retryAfter, ok)
	}
	if _, ok := LockoutRetryAfter(ErrInvalidCredentials); ok {
		t.Fatal("LockoutRetryAfter(non-lockout error) = true, want false")
	}
}

func TestServiceLoginLockoutAfterThreeFailures(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	// Three wrong-password logins increment the counter (401, FRESH_ACCOUNT +
	// NORMAL_401 rows). The third failure sets the 30s window.
	for i := 1; i <= 3; i++ {
		_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: Login error = %v, want ErrInvalidCredentials", i, err)
		}
		att := repo.attempts["active@example.com"]
		if att == nil {
			t.Fatalf("attempt %d: no attempts record persisted", i)
		}
		if att.FailedCount != i {
			t.Fatalf("attempt %d: FailedCount = %d, want %d", i, att.FailedCount, i)
		}
		if i < 3 && !att.LockoutUntil.IsZero() {
			t.Fatalf("attempt %d: LockoutUntil set below threshold: %v", i, att.LockoutUntil)
		}
	}
	if lu := repo.attempts["active@example.com"].LockoutUntil; !lu.After(time.Now()) {
		t.Fatalf("LockoutUntil = %v, want in the future after 3rd failure", lu)
	}

	// The 4th attempt (LOCKOUT_3_FAILS) is blocked for 30s — even with the
	// correct password, and no session is issued.
	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	retryAfter, ok := LockoutRetryAfter(err)
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("4th attempt error = %v, want ErrLockedOut", err)
	}
	if !ok || int(retryAfter.Seconds()) != 30 {
		t.Fatalf("4th attempt retryAfter = %v, want 30s", retryAfter)
	}
	if hasher.VerifyCalls() != 3 {
		t.Errorf("Verify calls = %d, want 3 (lockout gate must run before the verify)", hasher.VerifyCalls())
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session for a locked account, got %d", len(store.sessions))
	}
}

func TestServiceLoginLockoutFourPlusBlocksSixtySeconds(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	// Three failures lock the email for 30s.
	for i := 0; i < 3; i++ {
		if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); err != ErrInvalidCredentials {
			t.Fatalf("failed to record failure %d: %v", i, err)
		}
	}

	// Simulate the 30s window elapsing. The counter is NOT reset on expiry
	// (approved semantics: cleared only on a successful login).
	repo.attempts["active@example.com"].LockoutUntil = time.Now().Add(-1 * time.Second)
	if att := repo.attempts["active@example.com"]; att.FailedCount != 3 {
		t.Fatalf("FailedCount after expiry = %d, want 3 (counter persists across windows)", att.FailedCount)
	}

	// The 4th consecutive failure re-crosses a threshold: escalates to 60s
	// (LOCKOUT_4_PLUS). It is still a plain 401 at the moment of the failure.
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("4th failure error = %v, want ErrInvalidCredentials", err)
	}
	if att := repo.attempts["active@example.com"]; att.FailedCount != 4 {
		t.Fatalf("FailedCount = %d, want 4", att.FailedCount)
	}

	// The next attempt is blocked for 60s.
	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"})
	retryAfter, ok := LockoutRetryAfter(err)
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("blocked attempt error = %v, want ErrLockedOut", err)
	}
	if !ok || int(retryAfter.Seconds()) != 60 {
		t.Fatalf("blocked attempt retryAfter = %v, want 60s", retryAfter)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
}

func TestServiceLoginLockedAccountWithCorrectPasswordStillLocked(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	repo.attempts["active@example.com"] = &LoginAttempts{
		Email:        "active@example.com",
		FailedCount:  4,
		LockoutUntil: time.Now().Add(30 * time.Second),
	}

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("Login error = %v, want ErrLockedOut", err)
	}
	// Blocked regardless of password correctness — the counter is NOT reset and
	// no verify ran.
	if hasher.VerifyCalls() != 0 {
		t.Errorf("Verify calls = %d, want 0", hasher.VerifyCalls())
	}
	if repo.clearCalls != 0 {
		t.Errorf("ClearLoginAttempts calls = %d, want 0", repo.clearCalls)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
}

func TestServiceLoginLockoutExpiredLoginAcceptedCounterReset(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	// 3 failures -> locked for 30s.
	for i := 0; i < 3; i++ {
		if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); err != ErrInvalidCredentials {
			t.Fatalf("failed to record failure %d: %v", i, err)
		}
	}

	// The lockout window elapses.
	repo.attempts["active@example.com"].LockoutUntil = time.Now().Add(-1 * time.Second)

	// A correct login is accepted again (LOCKOUT_EXPIRED) and the counter
	// resets for a fresh cycle.
	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login after expiry failed: %v", err)
	}
	if res == nil || res.Token == "" {
		t.Fatal("expected a session token after expiry")
	}
	if repo.clearCalls != 1 {
		t.Errorf("ClearLoginAttempts calls = %d, want 1", repo.clearCalls)
	}
	// The row is kept, zeroed (matches the real repository's UPDATE reset).
	if att := repo.attempts["active@example.com"]; att == nil || att.FailedCount != 0 {
		t.Errorf("attempts after reset = %+v, want FailedCount 0", att)
	}
	if len(store.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(store.sessions))
	}
}

func TestServiceLoginSuccessResetsCounter(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	repo.attempts["active@example.com"] = &LoginAttempts{Email: "active@example.com", FailedCount: 2}

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if repo.clearCalls != 1 {
		t.Errorf("ClearLoginAttempts calls = %d, want 1", repo.clearCalls)
	}
	if att := repo.attempts["active@example.com"]; att == nil || att.FailedCount != 0 {
		t.Errorf("attempts not cleared after success: %+v", att)
	}
}

func TestServiceLoginPerEmailIndependence(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Push email A into lockout.
	for i := 0; i < 3; i++ {
		if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); err != ErrInvalidCredentials {
			t.Fatalf("failed to lock email A: %v", err)
		}
	}

	// Email B is unaffected: first wrong password is a plain 401 and its own
	// counter starts at 1 (NO_LOCKOUT_PER_ACCOUNT / FRESH_ACCOUNT).
	if _, err := svc.Login(context.Background(), LoginInput{Email: "pending@example.com", Password: "falsch"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("email B error = %v, want ErrInvalidCredentials", err)
	}
	if att := repo.attempts["pending@example.com"]; att == nil || att.FailedCount != 1 {
		t.Fatalf("email B attempts = %+v, want FailedCount 1", att)
	}
}

func TestServiceLoginNormal401BelowThreshold(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Fewer than 3 failures -> plain 401, counter increments, no lockout window.
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	att := repo.attempts["active@example.com"]
	if att == nil || att.FailedCount != 1 {
		t.Fatalf("attempts = %+v, want FailedCount 1", att)
	}
	if !att.LockoutUntil.IsZero() {
		t.Fatalf("LockoutUntil = %v, want zero below threshold", att.LockoutUntil)
	}
}

func TestServiceLoginUnknownEmailAlsoLockedOut(t *testing.T) {
	// Anti-enumeration: unknown emails accumulate failures identically and hit
	// 429 after 3 failures, so a 429 is not discriminating.
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	for i := 1; i <= 3; i++ {
		_, err := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "x"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: Login error = %v, want ErrInvalidCredentials", i, err)
		}
		if att := repo.attempts["nobody@example.com"]; att == nil || att.FailedCount != i {
			t.Fatalf("attempt %d: attempts = %+v, want FailedCount %d", i, att, i)
		}
	}

	_, err := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "x"})
	retryAfter, ok := LockoutRetryAfter(err)
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("unknown email error = %v, want ErrLockedOut", err)
	}
	if !ok || int(retryAfter.Seconds()) != 30 {
		t.Fatalf("unknown email retryAfter = %v, want 30s", retryAfter)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
}

func TestServiceLoginFailureCounterCapped(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Seed a long failure history at the cap with an expired window.
	repo.attempts["active@example.com"] = &LoginAttempts{
		Email:        "active@example.com",
		FailedCount:  LockoutMaxFailedCount,
		LockoutUntil: time.Now().Add(-1 * time.Second),
	}

	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if att := repo.attempts["active@example.com"]; att.FailedCount != LockoutMaxFailedCount {
		t.Fatalf("FailedCount = %d, want capped at %d", att.FailedCount, LockoutMaxFailedCount)
	}
}