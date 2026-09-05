package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// passwordChangeUser returns a repo with an active user and the user itself.
func passwordChangeUser() (*mockRepo, *User) {
	repo := newLoginRepo()
	return repo, repo.users["active@example.com"]
}

func TestServiceChangePasswordSuccess(t *testing.T) {
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	// Two active sessions: the second must be revoked, the first (current) stays
	// logged in (FR-25).
	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(store.sessions))
	}

	res, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, t1.Token)
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
	if res.Message != MsgPasswordChanged {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordChanged)
	}
	if !res.SessionsRevoked {
		t.Error("sessions_revoked must be true when revocation succeeded")
	}

	// New password persisted as a hash (Argon2id in production, AD-13); the
	// plaintext is never stored.
	if user.PasswordHash != "hashed:neuespasswort123" {
		t.Errorf("password hash = %q, want hashed:neuespasswort123", user.PasswordHash)
	}

	// Current session survives; all other sessions revoked (FR-25).
	if len(store.sessions) != 1 {
		t.Fatalf("expected 1 remaining session, got %d", len(store.sessions))
	}
	if _, ok := store.sessions[hashOfToken(t1.Token)]; !ok {
		t.Error("current session must survive the password change")
	}
	if _, ok := store.sessions[hashOfToken(t2.Token)]; ok {
		t.Error("other sessions must be revoked after a password change")
	}

	// Audit row written with actor + operation (NFR-O1/NFR-O2).
	events := repo.audit[user.ID]
	if len(events) != 1 || events[0] != AuditOperationPasswordChange {
		t.Errorf("audit events = %v, want [%s]", events, AuditOperationPasswordChange)
	}
}

func TestServiceChangePasswordWrongCurrent(t *testing.T) {
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "falsches-altes-passwort",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, t1.Token)
	if !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("err = %v, want ErrInvalidCurrentPassword", err)
	}

	// Password unchanged (FR-25), no session revocation, no audit row.
	if user.PasswordHash != "hashed:geheim123456" {
		t.Errorf("password hash = %q, want unchanged hashed:geheim123456", user.PasswordHash)
	}
	if len(store.sessions) != 2 {
		t.Errorf("expected both sessions to survive a rejected change, got %d", len(store.sessions))
	}
	if _, ok := store.sessions[hashOfToken(t2.Token)]; !ok {
		t.Error("session must not be revoked when the change is rejected")
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none on a rejected change", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordShortNewPassword(t *testing.T) {
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "kurz",
		NewPasswordConfirm: "kurz",
	}, t1.Token)
	if !errors.Is(err, ErrShortPassword) {
		t.Fatalf("err = %v, want ErrShortPassword (FR-2)", err)
	}
	if user.PasswordHash != "hashed:geheim123456" {
		t.Error("password must be unchanged when the new password is too short")
	}
	if len(store.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (no revocation on validation failure)", len(store.sessions))
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none on a validation failure", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordMismatch(t *testing.T) {
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "anderspasswort123",
	}, t1.Token)
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("err = %v, want ErrPasswordMismatch", err)
	}
	if user.PasswordHash != "hashed:geheim123456" {
		t.Error("password must be unchanged on a mismatch")
	}
	if len(store.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (no revocation on mismatch)", len(store.sessions))
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none on a mismatch", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordRequiresAuthenticatedUser(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	_, err := svc.ChangePassword(context.Background(), nil, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, "some-token")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for a nil user", err)
	}
	if len(store.sessions) != 0 {
		t.Error("no session may be created for an unauthenticated change")
	}
}

func TestServiceChangePasswordAuditWriteFailureIsBestEffort(t *testing.T) {
	// AUDIT_WRITE_FAIL (NFR-O1): a failed audit insert must NOT roll back the
	// password change (availability); the failure is logged server-side.
	repo, user := passwordChangeUser()
	repo.auditErr = errors.New("audit table unavailable")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	store := newMockSessionStore()
	store.withUsers(user)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, &mockHasher{}, sm, mockCipher{}, logger)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, t1.Token)
	if err != nil {
		t.Fatalf("ChangePassword must succeed despite an audit failure, got: %v", err)
	}
	if res.Message != MsgPasswordChanged {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordChanged)
	}
	if !res.SessionsRevoked {
		t.Error("sessions_revoked must remain true when only the audit write fails")
	}
	if user.PasswordHash != "hashed:neuespasswort123" {
		t.Error("password change must be persisted even when the audit write fails")
	}
	// Session revocation still ran.
	if len(store.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (current survives, others revoked)", len(store.sessions))
	}
	// The audit failure is logged (NFR-O1).
	if !strings.Contains(buf.String(), "password change audit write failed") {
		t.Errorf("expected the audit failure to be logged, got %q", buf.String())
	}
}

func TestServiceChangePasswordEmptyTokenIsRejectedBeforeRevocation(t *testing.T) {
	// FR-25 / review finding 1.7-2: an empty raw token must NOT reach
	// RevokeOtherSessions — SessionManager.RevokeOtherSessions("") falls back to
	// revoking ALL sessions, which would kill the current session too. The
	// empty token is rejected up front with ErrInvalidCredentials.
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"}); err != nil {
		t.Fatal(err)
	}
	if len(store.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(store.sessions))
	}

	_, err = svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for an empty token", err)
	}

	// No revocation happened: both sessions survive (the current one included).
	if len(store.sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (empty token must not revoke anything)", len(store.sessions))
	}
	if _, ok := store.sessions[hashOfToken(t1.Token)]; !ok {
		t.Error("current session must survive an empty-token attempt")
	}
	// Password unchanged and no audit row.
	if user.PasswordHash != "hashed:geheim123456" {
		t.Error("password must be unchanged when the change is rejected")
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordEmptyTokenEvenWhenCurrentValid(t *testing.T) {
	// The empty-token guard runs before the password verify: even with a fully
	// valid current password the request is rejected as unauthenticated.
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if len(store.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (no revocation on empty token)", len(store.sessions))
	}
	if _, ok := store.sessions[hashOfToken(t1.Token)]; !ok {
		t.Error("current session must survive")
	}
}

func TestServiceChangePasswordTooLongNewPassword(t *testing.T) {
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	tooLong := strings.Repeat("x", 1025)
	_, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        tooLong,
		NewPasswordConfirm: tooLong,
	}, "some-token")
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("err = %v, want ErrPasswordTooLong", err)
	}
	if user.PasswordHash != "hashed:geheim123456" {
		t.Error("password must be unchanged when the new password is too long")
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordWrongCurrentBeatsShortNew(t *testing.T) {
	// FR-25 ordering (review finding 1.7-4): the current password is verified
	// BEFORE the new-password policy. A wrong current password with a short new
	// password must surface as ErrInvalidCurrentPassword, not a policy error,
	// so an unauthenticated credential cannot probe the policy.
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "falsches-altes-passwort",
		NewPassword:        "kurz",
		NewPasswordConfirm: "kurz",
	}, "some-token")
	if !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("err = %v, want ErrInvalidCurrentPassword (wrong current beats short new)", err)
	}
}

func TestServiceChangePasswordRevokeFailureReportsSessionsRevokedFalse(t *testing.T) {
	// Review finding 1.7-8: when session revocation fails, the change still
	// succeeds (availability) but the result reports sessions_revoked=false so
	// the SPA can warn the user instead of claiming "→ Andere Sitzungen beendet".
	repo, user := passwordChangeUser()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"}); err != nil {
		t.Fatal(err)
	}

	store.revokeErr = errors.New("revocation backend unavailable")

	res, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, t1.Token)
	if err != nil {
		t.Fatalf("ChangePassword must succeed despite a revocation failure, got: %v", err)
	}
	if res.Message != MsgPasswordChanged {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordChanged)
	}
	if res.SessionsRevoked {
		t.Error("sessions_revoked must be false when revocation failed")
	}
	// The password change itself still landed (availability, NFR-O1).
	if user.PasswordHash != "hashed:neuespasswort123" {
		t.Error("password change must be persisted even when revocation fails")
	}
	// The audit row is still written.
	if len(repo.audit[user.ID]) != 1 {
		t.Errorf("audit events = %v, want 1 audit row", repo.audit[user.ID])
	}
}

func TestServiceChangePasswordMissingUserReturnsClearError(t *testing.T) {
	// Review finding 1.7-10: persisting a change for a user ID that has no row
	// maps to ErrUserNotFound (a clear 4xx at the handler), never a generic 500.
	repo := newMockRepo()
	user := &User{
		ID:           "ghost-user",
		Email:        "ghost@example.com",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.ChangePassword(context.Background(), user, ChangePasswordInput{
		CurrentPassword:    "geheim123456",
		NewPassword:        "neuespasswort123",
		NewPasswordConfirm: "neuespasswort123",
	}, "some-token")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestSessionUserJSONNeverSerializesPasswordHash(t *testing.T) {
	// Review finding 1.7-13: the user snapshot carried on a session must never
	// serialize the password hash (core.User.PasswordHash is json:"-"). This
	// pins that the session-resolution path cannot leak credentials.
	user := &User{
		ID:           "u-1",
		Email:        "a@example.com",
		PasswordHash: "super-secret-argon2id-hash",
		State:        StateActive,
	}
	sess := &Session{User: user}

	raw, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if strings.Contains(wire, "super-secret-argon2id-hash") {
		t.Errorf("session JSON must never carry the password hash, got %s", wire)
	}
	if strings.Contains(wire, "password_hash") {
		t.Errorf("session JSON must not contain a password_hash field, got %s", wire)
	}
}

func TestUserJSONNeverSerializesPasswordHash(t *testing.T) {
	// The same invariant holds for the user DTO directly (json:"-").
	user := &User{
		ID:           "u-1",
		Email:        "a@example.com",
		PasswordHash: "super-secret-argon2id-hash",
		State:        StateActive,
	}
	raw, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if strings.Contains(wire, "super-secret-argon2id-hash") || strings.Contains(wire, "password_hash") {
		t.Errorf("user JSON must never carry the password hash, got %s", wire)
	}
}
