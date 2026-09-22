package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func newLoginRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["active@example.com"] = &User{
		ID:           "u-active",
		Email:        "active@example.com",
		DisplayName:  "Aktive Person",
		FirstName:    "Aktive",
		LastName:     "Person",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	repo.users["pending@example.com"] = &User{
		ID:           "u-pending",
		Email:        "pending@example.com",
		PasswordHash: "hashed:geheim123456",
		State:        StatePendingApproval,
	}
	repo.users["deactivated@example.com"] = &User{
		ID:           "u-deactivated",
		Email:        "deactivated@example.com",
		PasswordHash: "hashed:geheim123456",
		State:        StateDeactivated,
	}
	repo.perms["u-active"] = []string{"inspect.tool.read"}
	return repo
}

func TestServiceLoginSuccess(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "Active@Example.com ", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if res.Token == "" {
		t.Fatal("expected a non-empty opaque token on successful login")
	}
	if len(store.sessions) != 1 {
		t.Fatalf("expected 1 persisted session, got %d", len(store.sessions))
	}
	if res.User.ID != "u-active" {
		t.Errorf("user id = %q, want u-active", res.User.ID)
	}
	// The login response must NOT carry the resolved permission set (AD-2/AD-6):
	// permissions are re-derived server-side per request, never client-trusted.
	var wire map[string]any
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal LoginResult: %v", err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("failed to unmarshal LoginResult: %v", err)
	}
	userObj, ok := wire["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user object in response, got %T", wire["user"])
	}
	if _, present := userObj["permissions"]; present {
		t.Error("login response must not expose a permissions field to the client")
	}
}

func TestServiceLoginRejectsOversizedInput(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	tooLongPassword := strings.Repeat("x", 1025)
	longEmail := strings.Repeat("a", 255) + "@example.com"

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{name: "password over 1024 runes", email: "active@example.com", password: tooLongPassword},
		{name: "email over 254 runes", email: longEmail, password: "geheim123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Login(context.Background(), LoginInput{Email: tt.email, Password: tt.password})
			if !errors.Is(err, ErrInvalidLoginInput) {
				t.Fatalf("Login error = %v, want ErrInvalidLoginInput", err)
			}
			if hasher.VerifyCalls() != 0 {
				t.Errorf("oversized input must be rejected before the Argon2id verify, got %d verify calls", hasher.VerifyCalls())
			}
			if len(store.sessions) != 0 {
				t.Errorf("expected no session created on invalid input, got %d", len(store.sessions))
			}
		})
	}
}

func TestServiceLoginPermissionFailureDoesNotOrphanSession(t *testing.T) {
	repo := newLoginRepo()
	repo.permsErr = errors.New("permission lookup failed")
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err == nil {
		t.Fatal("expected Login to fail when permission resolution fails")
	}
	// Permissions are resolved before the session is issued (finding: a failure
	// must not leave an orphaned session row behind).
	if len(store.sessions) != 0 {
		t.Errorf("expected no session issued when permission resolution fails, got %d", len(store.sessions))
	}
}

func TestServiceLoginRejectsActiveAccountWithoutHash(t *testing.T) {
	// An active account WITHOUT a stored password hash (e.g. a seeded admin)
	// must never authenticate — even if the attacker supplies the dummy
	// placeholder password used for timing normalization.
	repo := newMockRepo()
	repo.users["nohash@example.com"] = &User{
		ID:    "u-nohash",
		Email: "nohash@example.com",
		State: StateActive,
	}
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{Email: "nohash@example.com", Password: "dummy-password-for-timing"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials for hash-less active account", err)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session for hash-less account, got %d", len(store.sessions))
	}
}

func TestServiceLoginFailureMatrix(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
	}{
		{name: "wrong password", email: "active@example.com", password: "falsches-passwort"},
		{name: "unknown email", email: "nobody@example.com", password: "geheim123456"},
		{name: "pending account", email: "pending@example.com", password: "geheim123456"},
		{name: "deactivated account", email: "deactivated@example.com", password: "geheim123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newLoginRepo()
			hasher := &mockHasher{}
			svc, store := newTestService(repo, hasher)

			res, err := svc.Login(context.Background(), LoginInput{Email: tt.email, Password: tt.password})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
			}
			if res != nil {
				t.Fatalf("expected nil result on failure, got %+v", res)
			}
			if len(store.sessions) != 0 {
				t.Fatalf("expected no session created on failure, got %d", len(store.sessions))
			}
		})
	}
}

func TestServiceLoginAntiEnumerationUniform(t *testing.T) {
	// Unknown email and wrong password must both result in the SAME error so
	// the handler returns identical microcopy (UX-DR7).
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, errUnknown := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "x"})
	_, errWrong := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "x"})
	if !errors.Is(errUnknown, ErrInvalidCredentials) || !errors.Is(errWrong, ErrInvalidCredentials) {
		t.Fatalf("both paths must map to ErrInvalidCredentials, got %v / %v", errUnknown, errWrong)
	}
}

func TestServiceLoginWrongPasswordRunsDummyVerify(t *testing.T) {
	// For unknown emails the dummy hash verify must be exercised to normalize
	// timing (UX-DR7). The mock hasher counts Verify calls.
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, _ = svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "x"})
	if hasher.VerifyCalls() != 1 {
		t.Errorf("expected 1 Verify call for timing normalization, got %d", hasher.VerifyCalls())
	}
}

func TestServiceLogout(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(store.sessions))
	}

	if err := svc.Logout(context.Background(), res.Token); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("expected session deleted after logout, got %d", len(store.sessions))
	}
}

func TestServiceLogoutEmptyTokenIsNoop(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout empty token should be a no-op, got: %v", err)
	}
}
