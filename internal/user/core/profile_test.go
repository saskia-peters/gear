package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// profileUser builds a repo with an active user carrying a staged email.
func profileUser() (*mockRepo, *User) {
	repo := newMockRepo()
	user := &User{
		ID:           "u-active",
		Email:        "active@example.com",
		DisplayName:  "Aktive Person",
		FirstName:    "Aktive",
		LastName:     "Person",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
		PendingEmail: "",
	}
	repo.users[user.Email] = user
	return repo, user
}

func TestServiceGetProfileFromSessionUser(t *testing.T) {
	// PROFILE_VIEW: the base data is built from the authenticated session user
	// (including a staged pending_email), with no DB round-trip.
	repo, user := profileUser()
	user.PendingEmail = "neu@example.com"
	svc, _ := newTestService(repo, &mockHasher{})

	profile, err := svc.GetProfile(context.Background(), user)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if profile.ID != "u-active" {
		t.Errorf("id = %q, want u-active", profile.ID)
	}
	if profile.Email != "active@example.com" {
		t.Errorf("email = %q, want active@example.com", profile.Email)
	}
	if profile.FirstName != "Aktive" || profile.LastName != "Person" || profile.DisplayName != "Aktive Person" {
		t.Errorf("names = (%q,%q,%q), want (Aktive, Person, Aktive Person)", profile.FirstName, profile.LastName, profile.DisplayName)
	}
	if profile.PendingEmail != "neu@example.com" {
		t.Errorf("pending_email = %q, want neu@example.com", profile.PendingEmail)
	}
	// No DB access for a read of the session user.
	if repo.getCalls != 0 {
		t.Errorf("GetProfile must not hit the repository, got %d calls", repo.getCalls)
	}
}

func TestServiceGetProfileNilUser(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.GetProfile(context.Background(), nil)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceUpdateProfileHappy(t *testing.T) {
	// PROFILE_UPDATE: base data edits take effect immediately, the updated
	// profile is returned and an audit event profile.update is written.
	repo, user := profileUser()
	svc, _ := newTestService(repo, &mockHasher{})

	profile, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "  Erika  ",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}
	if profile.FirstName != "Erika" || profile.LastName != "Musterfrau" || profile.DisplayName != "Erika" {
		t.Errorf("profile = %+v, want trimmed (Erika, Musterfrau, Erika)", profile)
	}
	if user.FirstName != "Erika" || user.LastName != "Musterfrau" || user.DisplayName != "Erika" {
		t.Error("persisted user must reflect the saved base data immediately")
	}
	if user.Email != "active@example.com" {
		t.Error("email must be untouched by a base-data update")
	}
	events := repo.audit[user.ID]
	if len(events) != 1 || events[0] != AuditOperationProfileUpdate {
		t.Errorf("audit events = %v, want [profile.update]", events)
	}
}

func TestServiceUpdateProfileMissingFields(t *testing.T) {
	repo, user := profileUser()
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "   ",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if !errors.Is(err, ErrMissingFields) {
		t.Fatalf("err = %v, want ErrMissingFields", err)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none on a validation failure", repo.audit[user.ID])
	}
}

func TestServiceUpdateProfileNameTooLong(t *testing.T) {
	repo, user := profileUser()
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   strings.Repeat("x", 101),
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if !errors.Is(err, ErrProfileNameTooLong) {
		t.Fatalf("err = %v, want ErrProfileNameTooLong", err)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceUpdateProfileUserNotFound(t *testing.T) {
	repo := newMockRepo()
	user := &User{ID: "ghost-user", Email: "ghost@example.com", State: StateActive}
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "Erika",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestServiceUpdateProfileForbiddenForAnotherUser(t *testing.T) {
	// NOT_FOUND / self-ownership (AD-12): a persistence result that belongs to
	// a different user than the authenticated caller must surface as
	// ErrForbidden, never a success.
	repo, user := profileUser()
	repo.mismatchProfileUser = true
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "Erika",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestServiceUpdateProfileAuditWriteFailureIsBestEffort(t *testing.T) {
	// AUDIT_WRITE_FAIL (NFR-O1): a failed audit insert must NOT roll back the
	// profile update (availability); the failure is logged server-side.
	repo, user := profileUser()
	repo.auditErr = errors.New("audit table unavailable")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	store := newMockSessionStore()
	store.withUsers(user)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, &mockHasher{}, sm, mockCipher{}, logger)

	profile, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "Erika",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if err != nil {
		t.Fatalf("UpdateProfile must succeed despite an audit failure, got: %v", err)
	}
	if profile.FirstName != "Erika" {
		t.Error("profile update must be persisted even when the audit write fails")
	}
	if !strings.Contains(buf.String(), "profile update audit write failed") {
		t.Errorf("expected the audit failure to be logged, got %q", buf.String())
	}
}

func TestServiceStageEmailChangeHappy(t *testing.T) {
	// EMAIL_STAGE: the new email is stored as pending_email, the account stays
	// on the current email, an email.change.request audit event is written and
	// a German confirmation is returned.
	repo, user := profileUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res, err := svc.StageEmailChange(context.Background(), user, "  Neue.Adresse@Example.com ")
	if err != nil {
		t.Fatalf("StageEmailChange failed: %v", err)
	}
	if res.Message != MsgEmailChangeStaged {
		t.Errorf("message = %q, want %q", res.Message, MsgEmailChangeStaged)
	}
	// Normalized (trimmed + lowercased) and persisted; the current email is
	// untouched and login keeps using it.
	if res.PendingEmail != "neue.adresse@example.com" {
		t.Errorf("pending_email = %q, want neue.adresse@example.com", res.PendingEmail)
	}
	if user.PendingEmail != "neue.adresse@example.com" {
		t.Errorf("staged email = %q, want neue.adresse@example.com", user.PendingEmail)
	}
	if user.Email != "active@example.com" {
		t.Errorf("current email = %q, want unchanged active@example.com", user.Email)
	}
	events := repo.audit[user.ID]
	if len(events) != 1 || events[0] != AuditOperationEmailChangeRequest {
		t.Errorf("audit events = %v, want [email.change.request]", events)
	}
}

func TestServiceStageEmailChangeInvalid(t *testing.T) {
	// EMAIL_STAGE_INVALID: a malformed address is rejected before any write.
	tests := []struct {
		name  string
		email string
	}{
		{name: "empty", email: ""},
		{name: "whitespace", email: "   "},
		{name: "missing at", email: "keine-mail"},
		{name: "missing domain", email: "a@b"},
		{name: "oversized", email: strings.Repeat("x", 250) + "@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, user := profileUser()
			svc, _ := newTestService(repo, &mockHasher{})

			_, err := svc.StageEmailChange(context.Background(), user, tt.email)
			if !errors.Is(err, ErrInvalidEmail) {
				t.Fatalf("err = %v, want ErrInvalidEmail", err)
			}
			if user.PendingEmail != "" {
				t.Errorf("pending_email = %q, want unset", user.PendingEmail)
			}
			if len(repo.audit[user.ID]) != 0 {
				t.Errorf("audit events = %v, want none", repo.audit[user.ID])
			}
		})
	}
}

func TestServiceStageEmailChangeSameAsCurrent(t *testing.T) {
	// EMAIL_STAGE_SAME: staging the address the user already signs in with is a
	// no-op rejected with ErrEmailUnchanged (400 invalid_request). The check is
	// case-insensitive.
	tests := []struct {
		name  string
		email string
	}{
		{name: "exact", email: "active@example.com"},
		{name: "case-different", email: "ACTIVE@example.com"},
		{name: "surrounded-by-whitespace", email: "  active@example.com  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, user := profileUser()
			svc, _ := newTestService(repo, &mockHasher{})

			_, err := svc.StageEmailChange(context.Background(), user, tt.email)
			if !errors.Is(err, ErrEmailUnchanged) {
				t.Fatalf("err = %v, want ErrEmailUnchanged", err)
			}
			if user.PendingEmail != "" {
				t.Errorf("pending_email = %q, want unset", user.PendingEmail)
			}
			if len(repo.audit[user.ID]) != 0 {
				t.Errorf("audit events = %v, want none", repo.audit[user.ID])
			}
		})
	}
}

func TestServiceStageEmailChangeDuplicateEmail(t *testing.T) {
	// EMAIL_STAGE_DUPLICATE: an address already registered to another user is
	// rejected (the email UNIQUE constraint) with ErrEmailInUse.
	repo, user := profileUser()
	repo.users["taken@example.com"] = &User{ID: "u-other", Email: "taken@example.com", State: StateActive}
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "taken@example.com")
	if !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("err = %v, want ErrEmailInUse", err)
	}
	if user.PendingEmail != "" {
		t.Errorf("pending_email = %q, want unset", user.PendingEmail)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceStageEmailChangeDuplicatePending(t *testing.T) {
	// EMAIL_STAGE_DUPLICATE: an address already STAGED by another user is
	// rejected (the pending_email UNIQUE constraint) with ErrEmailInUse.
	repo, user := profileUser()
	repo.users["other@example.com"] = &User{ID: "u-other", Email: "other@example.com", PendingEmail: "shared@example.com", State: StateActive}
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "shared@example.com")
	if !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("err = %v, want ErrEmailInUse", err)
	}
	if user.PendingEmail != "" {
		t.Errorf("pending_email = %q, want unset", user.PendingEmail)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceStageEmailChangeUserGoneMapsToInUse(t *testing.T) {
	// A user that vanished between session resolution and the write makes the
	// conditional UPDATE affect zero rows, which the adapter maps to
	// ErrEmailInUse (review finding: "no row updated" == the in-use case).
	repo := newMockRepo()
	user := &User{ID: "ghost-user", Email: "ghost@example.com", State: StateActive}
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "neu@example.com")
	if !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("err = %v, want ErrEmailInUse (no row updated)", err)
	}
}

func TestServiceStageEmailChangeForbiddenForAnotherUser(t *testing.T) {
	// Self-ownership (AD-12): a persistence result belonging to a different
	// user than the authenticated caller must surface as ErrForbidden.
	repo, user := profileUser()
	repo.mismatchProfileUser = true
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "neu@example.com")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestServiceStageEmailChangeAuditWriteFailureIsBestEffort(t *testing.T) {
	// AUDIT_WRITE_FAIL (NFR-O1): a failed audit insert must NOT roll back the
	// staged email change (availability); the failure is logged server-side.
	repo, user := profileUser()
	repo.auditErr = errors.New("audit table unavailable")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	store := newMockSessionStore()
	store.withUsers(user)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, &mockHasher{}, sm, mockCipher{}, logger)

	res, err := svc.StageEmailChange(context.Background(), user, "neu@example.com")
	if err != nil {
		t.Fatalf("StageEmailChange must succeed despite an audit failure, got: %v", err)
	}
	if res.PendingEmail != "neu@example.com" {
		t.Error("the staged email must be persisted even when the audit write fails")
	}
	if user.PendingEmail != "neu@example.com" {
		t.Error("the staged email must be persisted even when the audit write fails")
	}
	if !strings.Contains(buf.String(), "email change request audit write failed") {
		t.Errorf("expected the audit failure to be logged, got %q", buf.String())
	}
}

func TestServiceStageEmailChangeNilUser(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), nil, "neu@example.com")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// cachingSessionStore is a SessionStore that caches the user snapshot STRICTLY:
// GetSessionByTokenHash returns the cached session without re-reading the
// users map, so the ONLY way a profile edit reaches the session is
// RefreshSessionUser. It mirrors adapters that cache the snapshot (review
// finding: stale session snapshot) and lets tests prove the refresh.
type cachingSessionStore struct {
	sessions map[string]*Session
	users    map[string]*User
	nextID   int
}

func newCachingSessionStore() *cachingSessionStore {
	return &cachingSessionStore{sessions: make(map[string]*Session)}
}

func (m *cachingSessionStore) withUsers(users ...*User) *cachingSessionStore {
	if m.users == nil {
		m.users = make(map[string]*User)
	}
	for _, u := range users {
		m.users[u.ID] = u
	}
	return m
}

func (m *cachingSessionStore) CreateSession(_ context.Context, userID, tokenHash string, expiresAt time.Time) (*Session, error) {
	m.nextID++
	s := &Session{
		ID:        fmt.Sprintf("cached-sess-%d", m.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
		User:      m.users[userID],
	}
	m.sessions[tokenHash] = s
	return s, nil
}

func (m *cachingSessionStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*Session, error) {
	s, ok := m.sessions[tokenHash]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *cachingSessionStore) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *cachingSessionStore) DeleteSessionsByUser(_ context.Context, userID string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

func (m *cachingSessionStore) DeleteSessionsByUserExcept(_ context.Context, userID, exceptTokenHash string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID && tokenHash != exceptTokenHash {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

// RefreshSessionUser replaces the user snapshot on every session of the given
// user (Story 2.1).
func (m *cachingSessionStore) RefreshSessionUser(_ context.Context, user *User) error {
	for _, s := range m.sessions {
		if s.UserID == user.ID {
			s.User = user
		}
	}
	return nil
}

func TestServiceUpdateProfileRefreshesSessionSnapshot(t *testing.T) {
	// Review finding: stale session snapshot. The repository returns a FRESH
	// user value (like the postgres adapter's userFromRow); the session store
	// caches strictly, so without RefreshSessionUser a subsequent session
	// resolution would still carry the PRE-EDIT names. GetProfile reads the
	// session user, so this is what the endpoint returns.
	repo, user := profileUser()
	repo.newObjectOnProfile = true
	store := newCachingSessionStore()
	store.withUsers(user)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, &mockHasher{}, sm, mockCipher{}, nil)

	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "Erika",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	}); err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	// The cached snapshot must now hold the fresh values.
	sess, err := sm.Validate(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if sess.User == nil || sess.User.FirstName != "Erika" || sess.User.LastName != "Musterfrau" || sess.User.DisplayName != "Erika" {
		t.Fatalf("session snapshot not refreshed after UpdateProfile: %+v", sess.User)
	}
	// And the endpoint reads it fresh.
	profile, err := svc.GetProfile(context.Background(), sess.User)
	if err != nil {
		t.Fatal(err)
	}
	if profile.FirstName != "Erika" || profile.LastName != "Musterfrau" {
		t.Errorf("GetProfile after save = %+v, want refreshed names", profile)
	}
}

func TestServiceStageEmailChangeRefreshesSessionSnapshot(t *testing.T) {
	// Review finding: stale session snapshot — pending_email must appear in a
	// subsequent session resolution (and thus GET /profile) after staging.
	repo, user := profileUser()
	repo.newObjectOnProfile = true
	store := newCachingSessionStore()
	store.withUsers(user)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, &mockHasher{}, sm, mockCipher{}, nil)

	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.StageEmailChange(context.Background(), user, "neu@example.com"); err != nil {
		t.Fatalf("StageEmailChange failed: %v", err)
	}

	sess, err := sm.Validate(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if sess.User == nil || sess.User.PendingEmail != "neu@example.com" {
		t.Fatalf("session snapshot not refreshed after StageEmailChange: %+v", sess.User)
	}
	profile, err := svc.GetProfile(context.Background(), sess.User)
	if err != nil {
		t.Fatal(err)
	}
	if profile.PendingEmail != "neu@example.com" {
		t.Errorf("GetProfile after stage = %+v, want pending_email neu@example.com", profile)
	}
}

func TestServiceStageEmailChangeDuplicateCaseVariant(t *testing.T) {
	// EMAIL_STAGE_DUPLICATE with a case-variant of another account's CURRENT
	// email: the core pre-check (GetUserByEmail, case-sensitive WHERE email = $1)
	// cannot see it, so the DB-level conditional UPDATE (lower() on both sides)
	// must reject it — no mixed pending_email/email collision (TOCTOU fix).
	repo, user := profileUser()
	repo.users["Mixed@Case.com"] = &User{ID: "u-other", Email: "Mixed@Case.com", State: StateActive}
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "mixed@case.com")
	if !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("err = %v, want ErrEmailInUse (case-variant of another account's email)", err)
	}
	if user.PendingEmail != "" {
		t.Errorf("pending_email = %q, want unset", user.PendingEmail)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceStageEmailChangeAlreadyPending(t *testing.T) {
	// Review finding: re-staging the address already staged is a no-op —
	// rejected with ErrEmailAlreadyPending and NO additional audit row.
	repo, user := profileUser()
	user.PendingEmail = "neu@example.com"
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "NEU@example.com")
	if !errors.Is(err, ErrEmailAlreadyPending) {
		t.Fatalf("err = %v, want ErrEmailAlreadyPending", err)
	}
	if user.PendingEmail != "neu@example.com" {
		t.Errorf("pending_email = %q, want unchanged neu@example.com", user.PendingEmail)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none on a re-stage no-op", repo.audit[user.ID])
	}
}

func TestServiceUpdateProfileRejectsNonActiveUser(t *testing.T) {
	// Review finding: active-state guard on profile writes. A non-active user
	// must never edit their profile (defense-in-depth; the gateway already
	// rejects such sessions).
	repo, user := profileUser()
	user.State = StatePendingApproval
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.UpdateProfile(context.Background(), user, UpdateProfileInput{
		FirstName:   "Erika",
		LastName:    "Musterfrau",
		DisplayName: "Erika",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for a non-active user", err)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}

func TestServiceStageEmailChangeRejectsNonActiveUser(t *testing.T) {
	repo, user := profileUser()
	user.State = StateDeactivated
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.StageEmailChange(context.Background(), user, "neu@example.com")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for a non-active user", err)
	}
	if user.PendingEmail != "" {
		t.Errorf("pending_email = %q, want unset", user.PendingEmail)
	}
	if len(repo.audit[user.ID]) != 0 {
		t.Errorf("audit events = %v, want none", repo.audit[user.ID])
	}
}