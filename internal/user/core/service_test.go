package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type mockRepo struct {
	users       map[string]*User
	createCalls int
	getCalls    int
	createErr   error
	permsErr    error
	perms       map[string][]string
	attempts    map[string]*LoginAttempts
	attemptsErr error
	upsertCalls int
	clearCalls  int
	audit       map[string][]string
	auditErr    error
	updateCalls int
	// resetTokens holds the minted reset tokens keyed by token_hash (FR-26).
	resetTokens map[string]*PasswordResetToken
	resetErr    error
	// mustChange users flagged for a forced password change, keyed by user ID.
	mustChange map[string]bool
	// adminGroup is the set of user IDs considered members of the admin group
	// (Story 1.8); IsUserInPermissionGroup resolves against it.
	adminGroup map[string]bool
	// groupName is the only permission group name IsUserInPermissionGroup
	// resolves (tests only ever ask about the admin group).
	// mismatchProfileUser makes UpdateUserProfile/StagePendingEmail return a
	// DIFFERENT user than the caller, exercising the self-ownership
	// defense-in-depth guard (AD-12 → ErrForbidden).
	mismatchProfileUser bool
	// newObjectOnProfile makes UpdateUserProfile/StagePendingEmail return a
	// FRESH user value (like the postgres adapter's userFromRow) instead of the
	// mutated in-memory pointer, so tests can prove the session snapshot is
	// refreshed (review finding: stale session snapshot).
	newObjectOnProfile bool
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		users:       make(map[string]*User),
		perms:       make(map[string][]string),
		attempts:    make(map[string]*LoginAttempts),
		audit:       make(map[string][]string),
		resetTokens: make(map[string]*PasswordResetToken),
		mustChange:  make(map[string]bool),
		adminGroup:  make(map[string]bool),
	}
}

func (m *mockRepo) CreateRegisteredUser(_ context.Context, email, displayName, firstName, lastName, passwordHash string) (*User, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	u := &User{
		Email:        email,
		DisplayName:  displayName,
		FirstName:    firstName,
		LastName:     lastName,
		PasswordHash: passwordHash,
		State:        StatePendingApproval,
	}
	m.users[email] = u
	return u, nil
}

func (m *mockRepo) GetUserByEmail(_ context.Context, email string) (*User, error) {
	m.getCalls++
	u, ok := m.users[email]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (m *mockRepo) ListPermissionsByUser(_ context.Context, userID string) ([]string, error) {
	if m.permsErr != nil {
		return nil, m.permsErr
	}
	return m.perms[userID], nil
}

func (m *mockRepo) GetLoginAttempts(_ context.Context, email string) (*LoginAttempts, error) {
	if m.attemptsErr != nil {
		return nil, m.attemptsErr
	}
	return m.attempts[email], nil
}

// IncrementLoginAttempts mirrors the postgres adapter's atomic upsert: it
// increments the per-email counter (capped), sets the lockout window when a
// threshold is crossed, and keeps any previously set window until the new count
// moves into a higher tier.
func (m *mockRepo) IncrementLoginAttempts(_ context.Context, email string) error {
	if m.attempts == nil {
		m.attempts = make(map[string]*LoginAttempts)
	}
	cur := 0
	if a := m.attempts[email]; a != nil {
		cur = a.FailedCount
	}
	newCount := cur + 1
	if newCount > LockoutMaxFailedCount {
		newCount = LockoutMaxFailedCount
	}
	now := time.Now().UTC()
	var lockoutUntil time.Time
	switch {
	case newCount >= LockoutThresholdLong:
		lockoutUntil = now.Add(LockoutDurationLong)
	case newCount == LockoutThresholdShort:
		lockoutUntil = now.Add(LockoutDurationShort)
	}
	m.attempts[email] = &LoginAttempts{
		Email:        email,
		FailedCount:  newCount,
		LockoutUntil: lockoutUntil,
		UpdatedAt:    now,
	}
	m.upsertCalls++
	return nil
}

// ClearLoginAttempts resets the email's counter to zero and clears the window,
// keeping the row — mirroring the real repository (UPDATE ... SET
// failed_count = 0).
func (m *mockRepo) ClearLoginAttempts(_ context.Context, email string) error {
	if m.attempts == nil {
		m.attempts = make(map[string]*LoginAttempts)
	}
	m.attempts[email] = &LoginAttempts{
		Email:       email,
		FailedCount: 0,
		UpdatedAt:   time.Now().UTC(),
	}
	m.clearCalls++
	return nil
}

// SetUserTotpSecret stores the encrypted secret and enables MFA on the in-memory
// user, mirroring the postgres adapter (FR-4/NFR-S4).
func (m *mockRepo) SetUserTotpSecret(_ context.Context, userID, encryptedSecret string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.TotpSecretEncrypted = encryptedSecret
			u.IsMFAEnabled = true
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// ClearUserTotpSecret disables MFA and clears the encrypted secret (FR-4).
func (m *mockRepo) ClearUserTotpSecret(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.TotpSecretEncrypted = ""
			u.IsMFAEnabled = false
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// SetUserPendingTotpSecret stores the short-lived pending enrollment (encrypted
// secret + expiry) on the in-memory user.
func (m *mockRepo) SetUserPendingTotpSecret(_ context.Context, userID, encryptedSecret string, expiresAt time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingTotpSecretEncrypted = encryptedSecret
			u.PendingTotpExpiresAt = expiresAt
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// ClearUserPendingTotpSecret clears the pending enrollment.
func (m *mockRepo) ClearUserPendingTotpSecret(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// UpdateUserPassword replaces the user's stored password hash (FR-25). Only the
// hash is stored; the plaintext password is never kept. An unknown user ID maps
// to ErrUserNotFound (never a misleading "already exists" sentinel).
func (m *mockRepo) UpdateUserPassword(_ context.Context, userID, passwordHash string) (*User, error) {
	m.updateCalls++
	for _, u := range m.users {
		if u.ID == userID {
			u.PasswordHash = passwordHash
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

// InsertAuditEvent appends an audit row (actor_user_id -> operation) to the
// in-memory append-only trail (NFR-O1/NFR-O2). auditErr lets tests simulate an
// audit-write failure (best-effort path).
func (m *mockRepo) InsertAuditEvent(_ context.Context, userID, operation string) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	if m.audit == nil {
		m.audit = make(map[string][]string)
	}
	m.audit[userID] = append(m.audit[userID], operation)
	return nil
}

// InsertAuditEventAnonymous appends an audit row without an actor (review
// findings 1.8-3 / 1.8-10): the row is keyed under the empty-string pseudo
// actor so tests can assert unknown-email enumeration attempts leave a trail.
func (m *mockRepo) InsertAuditEventAnonymous(_ context.Context, operation string) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	if m.audit == nil {
		m.audit = make(map[string][]string)
	}
	m.audit[""] = append(m.audit[""], operation)
	return nil
}

// UpdateUserProfile persists the user's editable base data (first/last/display
// name, Story 2.1) and the custom-attribute set (Story 1.9) and returns the
// updated user. An unknown user ID maps to ErrUserNotFound.
func (m *mockRepo) UpdateUserProfile(_ context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*User, error) {
	m.updateCalls++
	for _, u := range m.users {
		if u.ID == userID {
			u.FirstName = firstName
			u.LastName = lastName
			u.DisplayName = displayName
			u.Attributes = attributes
			if m.mismatchProfileUser {
				return &User{ID: "other-user", Email: "other@example.com"}, nil
			}
			if m.newObjectOnProfile {
				clone := *u
				return &clone, nil
			}
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

// StagePendingEmail stores a staged email change (Story 2.1) on the in-memory
// user. It mirrors the postgres adapter's conditional UPDATE: the staging is
// refused (ErrEmailInUse, "no row updated") while ANY other account holds the
// address as its current email or as an already-staged pending_email,
// compared case-insensitively. A missing target user also maps to
// ErrEmailInUse (the conditional UPDATE affects zero rows).
func (m *mockRepo) StagePendingEmail(_ context.Context, userID, pendingEmail string) (*User, error) {
	var target *User
	for _, u := range m.users {
		if u.ID == userID {
			target = u
			continue
		}
		if strings.EqualFold(u.Email, pendingEmail) || strings.EqualFold(u.PendingEmail, pendingEmail) {
			return nil, ErrEmailInUse
		}
	}
	if target == nil {
		return nil, ErrEmailInUse
	}
	if m.mismatchProfileUser {
		return &User{ID: "other-user", Email: "other@example.com"}, nil
	}
	target.PendingEmail = pendingEmail
	if m.newObjectOnProfile {
		clone := *target
		return &clone, nil
	}
	return target, nil
}

// ClearPendingEmail clears a staged email change (pending_email -> NULL).
func (m *mockRepo) ClearPendingEmail(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingEmail = ""
			return nil
		}
	}
	return ErrUserNotFound
}

// CreatePasswordResetToken stores the hash of a fresh reset token, invalidating
// earlier tokens of the user (only the latest stays valid, FR-26).
func (m *mockRepo) CreatePasswordResetToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	if m.resetErr != nil {
		return m.resetErr
	}
	for hash := range m.resetTokens {
		if m.resetTokens[hash].UserID == userID {
			delete(m.resetTokens, hash)
		}
	}
	m.resetTokens[tokenHash] = &PasswordResetToken{
		ID:        "token-" + tokenHash[:8],
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
		User:      m.userByID(userID),
	}
	return nil
}

// GetPasswordResetTokenByHash resolves a reset token by hash with its owner.
// Unknown hashes map to ErrResetTokenInvalid.
func (m *mockRepo) GetPasswordResetTokenByHash(_ context.Context, tokenHash string) (*PasswordResetToken, error) {
	t, ok := m.resetTokens[tokenHash]
	if !ok {
		return nil, ErrResetTokenInvalid
	}
	t.User = m.userByID(t.UserID)
	return t, nil
}

// ConsumePasswordResetToken atomically invalidates + returns a reset token
// (review finding 1.8-5): the delete happens with the read, so the losing
// concurrent completion sees no row and maps to ErrResetTokenInvalid.
func (m *mockRepo) ConsumePasswordResetToken(_ context.Context, tokenHash string) (*PasswordResetToken, error) {
	t, ok := m.resetTokens[tokenHash]
	if !ok {
		return nil, ErrResetTokenInvalid
	}
	delete(m.resetTokens, tokenHash)
	t.User = m.userByID(t.UserID)
	return t, nil
}

// DeleteExpiredPasswordResetTokens lazily purges a user's expired tokens
// (review finding 1.8-7).
func (m *mockRepo) DeleteExpiredPasswordResetTokens(_ context.Context, userID string) error {
	now := time.Now().UTC()
	for hash, t := range m.resetTokens {
		if t.UserID == userID && now.After(t.ExpiresAt) {
			delete(m.resetTokens, hash)
		}
	}
	return nil
}

// DeletePasswordResetToken invalidates a reset token after use (single-use).
func (m *mockRepo) DeletePasswordResetToken(_ context.Context, tokenHash string) error {
	delete(m.resetTokens, tokenHash)
	return nil
}

// SetUserMustChangePassword flags the user for a forced password change.
func (m *mockRepo) SetUserMustChangePassword(_ context.Context, userID string) error {
	m.mustChange[userID] = true
	for _, u := range m.users {
		if u.ID == userID {
			u.MustChangePassword = true
		}
	}
	return nil
}

// ClearUserMustChangePassword clears the forced-change flag.
func (m *mockRepo) ClearUserMustChangePassword(_ context.Context, userID string) error {
	delete(m.mustChange, userID)
	for _, u := range m.users {
		if u.ID == userID {
			u.MustChangePassword = false
		}
	}
	return nil
}

// IsUserInPermissionGroup reports admin-group membership (Story 1.8). Tests
// only ever ask about the admin group, so any other name resolves false.
func (m *mockRepo) IsUserInPermissionGroup(_ context.Context, userID, groupName string) (bool, error) {
	if groupName != AdminGroupName {
		return false, nil
	}
	return m.adminGroup[userID], nil
}

// userByID finds a user by ID across the email-keyed map (the mock repository
// keys users by email; lookups by ID iterate the values).
func (m *mockRepo) userByID(userID string) *User {
	for _, u := range m.users {
		if u.ID == userID {
			return u
		}
	}
	return nil
}

type mockHasher struct {
	hashCalls   int
	verifyCalls int
}

func (m *mockHasher) Hash(password string) (string, error) {
	m.hashCalls++
	return "hashed:" + password, nil
}

func (m *mockHasher) Verify(password, encodedHash string) (bool, error) {
	m.verifyCalls++
	return encodedHash == "hashed:"+password, nil
}

// VerifyCalls returns the number of Verify invocations (used to assert
// timing-normalization behaviour on login failures).
func (m *mockHasher) VerifyCalls() int {
	return m.verifyCalls
}

// mockSessionStore is an in-memory SessionStore for tests. revokeErr lets
// tests simulate a session-revocation failure (best-effort path in
// ChangePassword, FR-25/NFR-O1).
type mockSessionStore struct {
	sessions  map[string]*Session
	users     map[string]*User
	nextID    int
	revokeErr error
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: make(map[string]*Session)}
}

// withUsers registers users so GetSessionByTokenHash can attach the session
// owner, mirroring the repository's JOIN on users.
func (m *mockSessionStore) withUsers(users ...*User) *mockSessionStore {
	if m.users == nil {
		m.users = make(map[string]*User)
	}
	for _, u := range users {
		m.users[u.ID] = u
	}
	return m
}

func (m *mockSessionStore) CreateSession(_ context.Context, userID, tokenHash string, expiresAt time.Time) (*Session, error) {
	m.nextID++
	s := &Session{
		ID:        fmt.Sprintf("sess-%d", m.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}
	if m.users != nil {
		s.User = m.users[userID]
	}
	m.sessions[tokenHash] = s
	return s, nil
}

func (m *mockSessionStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*Session, error) {
	s, ok := m.sessions[tokenHash]
	if !ok {
		return nil, ErrSessionNotFound
	}
	if m.users != nil {
		s.User = m.users[s.UserID]
	}
	return s, nil
}

func (m *mockSessionStore) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *mockSessionStore) DeleteSessionsByUser(_ context.Context, userID string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

func (m *mockSessionStore) DeleteSessionsByUserExcept(_ context.Context, userID, exceptTokenHash string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	for tokenHash, s := range m.sessions {
		if s.UserID == userID && tokenHash != exceptTokenHash {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

// RefreshSessionUser replaces the user snapshot on every session of the given
// user so a subsequent Validate returns the fresh profile (Story 2.1). The
// mockSessionStore re-reads m.users on GetSessionByTokenHash anyway, but this
// keeps the store contract honest for session stores that cache strictly.
func (m *mockSessionStore) RefreshSessionUser(_ context.Context, user *User) error {
	for _, s := range m.sessions {
		if s.UserID == user.ID {
			s.User = user
		}
	}
	return nil
}

// mockCipher is a reversible SecretCipher used in tests: it shifts every byte
// and hex-encodes it so ciphertext never contains the plaintext secret (letting
// tests assert at-rest encryption) while staying invertible for TOTP checks.
type mockCipher struct{}

func (mockCipher) Encrypt(plaintext string) (string, error) {
	out := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i++ {
		out[i] = plaintext[i] + 1
	}
	return fmt.Sprintf("%x", out), nil
}

func (mockCipher) Decrypt(encoded string) (string, error) {
	if len(encoded)%2 != 0 {
		return "", errors.New("bad ciphertext")
	}
	out := make([]byte, len(encoded)/2)
	for i := 0; i < len(out); i++ {
		hi, lo := encoded[2*i], encoded[2*i+1]
		var b byte
		if _, err := fmt.Sscanf(string([]byte{hi, lo}), "%02x", &b); err != nil {
			return "", errors.New("bad ciphertext")
		}
		out[i] = b - 1
	}
	return string(out), nil
}

// newTestService builds a Service with in-memory repo/hasher/session store.
// The logger is nil (the core falls back to slog.Default()) and the forgot
// rate gate is DISABLED so tests can drive multiple reset requests per email
// (the throttle is exercised explicitly by the rate-limit tests).
func newTestService(repo *mockRepo, hasher *mockHasher) (*Service, *mockSessionStore) {
	store := newMockSessionStore()
	var users []*User
	for _, u := range repo.users {
		users = append(users, u)
	}
	store.withUsers(users...)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, hasher, sm, mockCipher{}, nil)
	svc.SetForgotThrottleInterval(0)
	return svc, store
}

func TestServiceRegisterHappyPath(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Erika",
		LastName:        "Musterfrau",
		Email:           "erika@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}

	if repo.createCalls != 1 {
		t.Errorf("expected 1 CreateRegisteredUser call, got %d", repo.createCalls)
	}
	if hasher.hashCalls != 1 {
		t.Errorf("expected 1 Hash call, got %d", hasher.hashCalls)
	}

	createdUser := repo.users["erika@example.com"]
	if createdUser == nil {
		t.Fatal("user was not saved in repo")
	}
	if createdUser.DisplayName != "Erika Musterfrau" {
		t.Errorf("displayName = %q, want %q", createdUser.DisplayName, "Erika Musterfrau")
	}
	if createdUser.State != StatePendingApproval {
		t.Errorf("state = %q, want %q", createdUser.State, StatePendingApproval)
	}
}

func TestServiceRegisterDuplicateEmailAntiEnumeration(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	repo.users["existing@example.com"] = &User{
		Email: "existing@example.com",
		State: StateActive,
	}

	input := RegisterInput{
		FirstName:       "Max",
		LastName:        "Mustermann",
		Email:           "existing@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("Register failed for existing email: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}

	// Should not have called create on repo, but should have hashed password for constant time
	if repo.createCalls != 0 {
		t.Errorf("expected 0 CreateRegisteredUser calls, got %d", repo.createCalls)
	}
	if hasher.hashCalls != 1 {
		t.Errorf("expected 1 dummy Hash call for timing protection, got %d", hasher.hashCalls)
	}
}

func TestServiceRegisterValidationErrors(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Max",
		LastName:        "Mustermann",
		Email:           "max@example.com",
		Password:        "short",
		PasswordConfirm: "short",
	}

	_, err := svc.Register(context.Background(), input)
	if !errors.Is(err, ErrShortPassword) {
		t.Errorf("expected ErrShortPassword, got: %v", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("expected 0 create calls on validation failure, got %d", repo.createCalls)
	}
}

func TestServiceRegisterDuplicateKeyRaceCondition(t *testing.T) {
	repo := newMockRepo()
	repo.createErr = errors.New("ERROR: duplicate key value violates unique constraint \"users_email_key\" (SQLSTATE 23505)")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Hans",
		LastName:        "Müller",
		Email:           "hans@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("expected duplicate key error to be swallowed into uniform confirmation, got: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}
}
