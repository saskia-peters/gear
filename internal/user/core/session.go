package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Session is the domain representation of a server-side authentication session
// (NFR-S2). Only the SHA-256 hash of the opaque token is persisted; the raw
// token is never stored server-side (defense-in-depth).
type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	User      *User
}

// ErrSessionNotFound is returned when a session token is unknown, malformed,
// expired or its account is no longer active.
var ErrSessionNotFound = errors.New("session not found")

// SessionStore is the outbound persistence contract for server-side sessions.
type SessionStore interface {
	CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error
	// DeleteSessionsByUser revokes every session of a user; used when MFA is
	// disabled so pre-existing sessions must re-authenticate (review finding
	// 1.6-2).
	DeleteSessionsByUser(ctx context.Context, userID string) error
	// DeleteSessionsByUserExcept revokes all of a user's sessions except the
	// one identified by the given token hash; used when MFA is enabled so
	// pre-enrollment sessions cannot bypass the second factor (review finding
	// 1.6-2).
	DeleteSessionsByUserExcept(ctx context.Context, userID, exceptTokenHash string) error
	// RefreshSessionUser replaces the user snapshot on every session of the
	// given user so the session reflects profile edits immediately (Story 2.1).
	// Adapters whose session user is re-derived live on every Validate (the
	// postgres JOIN) implement this as a no-op; adapters that cache the
	// snapshot must update it.
	RefreshSessionUser(ctx context.Context, user *User) error
}

// SessionManager issues, validates and invalidates opaque session tokens
// (NFR-S2). Idle expiry is re-checked against the stored expires_at on every
// Validate call — never a client-trusted or cached snapshot (AD-2).
type SessionManager struct {
	store    SessionStore
	idleTime time.Duration
}

// NewSessionManager constructs a SessionManager with the given idle lifetime
// (defaults to 8h per NFR-S2 when idle <= 0).
func NewSessionManager(store SessionStore, idle time.Duration) *SessionManager {
	if idle <= 0 {
		idle = 8 * time.Hour
	}
	return &SessionManager{store: store, idleTime: idle}
}

// newOpaqueToken returns 32 cryptographically random bytes URL-safe encoded.
func newOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("core: failed to generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken computes the SHA-256 hash of a raw token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Issue creates a new session for the user and returns the opaque token. The
// raw token is returned exactly once; only its hash is stored server-side.
func (m *SessionManager) Issue(ctx context.Context, user *User) (string, error) {
	raw, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().UTC().Add(m.idleTime)
	if _, err := m.store.CreateSession(ctx, user.ID, hashToken(raw), expiresAt); err != nil {
		return "", fmt.Errorf("core: failed to persist session: %w", err)
	}
	return raw, nil
}

// Validate looks up a session by its raw token and re-checks idle expiry
// against the stored expires_at (AD-2). Returns the session and its user.
// Any failure (unknown token, expired, deactivated account) maps to
// ErrSessionNotFound so callers can respond with a single unauthorized code.
func (m *SessionManager) Validate(ctx context.Context, rawToken string) (*Session, error) {
	if rawToken == "" {
		return nil, ErrSessionNotFound
	}
	sess, err := m.store.GetSessionByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if sess.User == nil || sess.User.State != StateActive {
		return nil, ErrSessionNotFound
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// Invalidate deletes the session identified by the raw token server-side
// (NFR-S2). Deletion is atomic (by the stored token hash), so there is no
// Get-then-Delete TOCTOU window. Unknown tokens are a no-op.
func (m *SessionManager) Invalidate(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return m.store.DeleteSessionByTokenHash(ctx, hashToken(rawToken))
}

// RevokeOtherSessions invalidates every session of the user except the one
// identified by rawToken (NFR-S2). An empty rawToken (e.g. the caller is not
// acting through a specific session) revokes all sessions. Used after enabling
// MFA so sessions issued before enrollment cannot bypass the second factor
// (review finding 1.6-2).
func (m *SessionManager) RevokeOtherSessions(ctx context.Context, userID, rawToken string) error {
	if rawToken == "" {
		return m.store.DeleteSessionsByUser(ctx, userID)
	}
	return m.store.DeleteSessionsByUserExcept(ctx, userID, hashToken(rawToken))
}

// RevokeAllSessions invalidates every session of the user (NFR-S2). Used after
// disabling MFA so all pre-existing sessions must re-authenticate (review
// finding 1.6-2).
func (m *SessionManager) RevokeAllSessions(ctx context.Context, userID string) error {
	return m.store.DeleteSessionsByUser(ctx, userID)
}

// RefreshSessionUser replaces the user snapshot on every session of the given
// user (Story 2.1). The Service calls it after a profile edit so a subsequent
// session resolution — and thus GetProfile and the header greeting — reflects
// the fresh names/pending_email immediately, even for session stores that
// cache the snapshot.
func (m *SessionManager) RefreshSessionUser(ctx context.Context, user *User) error {
	if user == nil {
		return nil
	}
	return m.store.RefreshSessionUser(ctx, user)
}
