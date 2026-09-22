package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionManagerIssueValidate(t *testing.T) {
	store := newMockSessionStore()
	sm := NewSessionManager(store, time.Hour)

	user := &User{ID: "u-1", State: StateActive, Email: "a@example.com"}
	store.withUsers(user)
	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if raw == "" {
		t.Fatal("expected non-empty opaque token")
	}

	// The raw token must never be stored; only its hash.
	for _, s := range store.sessions {
		if s.TokenHash == raw {
			t.Fatal("raw token was persisted — only the hash must be stored")
		}
	}

	sess, err := sm.Validate(context.Background(), raw)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if sess.UserID != "u-1" {
		t.Errorf("UserID = %q, want u-1", sess.UserID)
	}
	if sess.User == nil || sess.User.ID != "u-1" {
		t.Errorf("Validate did not attach user: %+v", sess.User)
	}
}

func TestSessionManagerValidateCases(t *testing.T) {
	deactivated := &User{ID: "u-deactivated", State: StateDeactivated, Email: "d@example.com"}

	tests := []struct {
		name    string
		setup   func(store *mockSessionStore) (rawToken string)
		wantErr error
	}{
		{
			name: "empty token",
			setup: func(_ *mockSessionStore) string {
				return ""
			},
			wantErr: ErrSessionNotFound,
		},
		{
			name: "unknown token",
			setup: func(_ *mockSessionStore) string {
				return "not-a-real-token"
			},
			wantErr: ErrSessionNotFound,
		},
		{
			name: "deactivated user session is rejected",
			setup: func(store *mockSessionStore) string {
				store.withUsers(deactivated)
				sm := NewSessionManager(store, time.Hour)
				raw, _ := sm.Issue(context.Background(), deactivated)
				return raw
			},
			wantErr: ErrSessionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockSessionStore()
			sm := NewSessionManager(store, time.Hour)
			raw := tt.setup(store)
			_, err := sm.Validate(context.Background(), raw)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSessionManagerValidateExpired(t *testing.T) {
	store := newMockSessionStore()
	sm := NewSessionManager(store, time.Hour)

	user := &User{ID: "u-1", State: StateActive, Email: "a@example.com"}
	store.withUsers(user)
	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Backdate the session so it is past idle expiry.
	for _, s := range store.sessions {
		s.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	}

	_, err = sm.Validate(context.Background(), raw)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Validate expired error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManagerInvalidate(t *testing.T) {
	store := newMockSessionStore()
	sm := NewSessionManager(store, time.Hour)

	user := &User{ID: "u-1", State: StateActive, Email: "a@example.com"}
	store.withUsers(user)
	raw, _ := sm.Issue(context.Background(), user)

	if err := sm.Invalidate(context.Background(), raw); err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}

	// Subsequent validate must fail.
	_, err := sm.Validate(context.Background(), raw)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Validate after logout error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManagerInvalidateUnknownTokenIsNoop(t *testing.T) {
	store := newMockSessionStore()
	sm := NewSessionManager(store, time.Hour)
	if err := sm.Invalidate(context.Background(), "unknown"); err != nil {
		t.Fatalf("Invalidate unknown token should be a no-op, got: %v", err)
	}
}

func TestNewSessionManagerDefaultIdle(t *testing.T) {
	sm := NewSessionManager(newMockSessionStore(), 0)
	if sm.idleTime != 8*time.Hour {
		t.Errorf("default idle = %v, want 8h", sm.idleTime)
	}
}
