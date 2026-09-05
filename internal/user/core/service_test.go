package core

import (
	"context"
	"errors"
	"fmt"
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
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		users:    make(map[string]*User),
		perms:    make(map[string][]string),
		attempts: make(map[string]*LoginAttempts),
		audit:    make(map[string][]string),
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
// The logger is nil (the core falls back to slog.Default()).
func newTestService(repo *mockRepo, hasher *mockHasher) (*Service, *mockSessionStore) {
	store := newMockSessionStore()
	var users []*User
	for _, u := range repo.users {
		users = append(users, u)
	}
	store.withUsers(users...)
	sm := NewSessionManager(store, time.Hour)
	return NewService(repo, hasher, sm, mockCipher{}, nil), store
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
