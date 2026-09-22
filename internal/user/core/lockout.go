package core

import (
	"errors"
	"time"
)

// LoginAttempts is the per-email record of consecutive failed logins and the
// active lockout window (FR-3). It is keyed by the normalized email — not the
// user_id — so unknown emails also accumulate failures and hit HTTP 429
// identically (anti-enumeration: a 429 is not discriminating). Lockout for one
// email never affects another (AD-2/AD-11).
type LoginAttempts struct {
	Email        string
	FailedCount  int
	LockoutUntil time.Time
	UpdatedAt    time.Time
}

// ErrLockedOut is the sentinel (matched via errors.Is) returned when an email
// is inside a lockout window. It maps to HTTP 429. Wrap it in a *LockoutError
// to carry the Retry-After seconds and the affected email.
var ErrLockedOut = errors.New("account locked out")

// LockoutError wraps ErrLockedOut with the Retry-After duration and the
// affected email (used for structured logging, NFR-O1).
type LockoutError struct {
	RetryAfter time.Duration
	Email      string
}

func (e *LockoutError) Error() string { return ErrLockedOut.Error() }

// Unwrap lets errors.Is(err, ErrLockedOut) match a *LockoutError.
func (e *LockoutError) Unwrap() error { return ErrLockedOut }

// NewLockoutError constructs a lockout error for an email.
func NewLockoutError(retryAfter time.Duration, email string) *LockoutError {
	return &LockoutError{RetryAfter: retryAfter, Email: email}
}

// LockoutRetryAfter returns the Retry-After duration carried by a lockout error
// and whether err represents a lockout.
func LockoutRetryAfter(err error) (time.Duration, bool) {
	var le *LockoutError
	if errors.As(err, &le) {
		return le.RetryAfter, true
	}
	return 0, false
}

// FR-3 progressive thresholds: 3 consecutive failures block for 30s, 4 or more
// for 60s. The counter is not reset on expiry so the escalation to 60s is
// reachable across a repeated failure sequence; a successful login clears it
// (no permanent lockout — the windows are bounded and always expire). The
// atomic write in the postgres adapter mirrors these values; the counter is
// capped so it cannot grow unbounded.
const (
	LockoutThresholdShort = 3
	LockoutThresholdLong  = 4
	LockoutDurationShort  = 30 * time.Second
	LockoutDurationLong   = 60 * time.Second
	// LockoutMaxFailedCount caps the per-email failure counter.
	LockoutMaxFailedCount = 10
	// LockoutDefaultRetrySeconds is the sane Retry-After fallback a caller
	// should use when a lockout error carries no retry details.
	LockoutDefaultRetrySeconds = 30
)

// Check reports whether the email is currently locked out at time now. When
// locked, it returns the Retry-After duration (seconds, rounded up) the caller
// should wait. A nil or non-window attempts value is never locked.
func Check(attempts *LoginAttempts, now time.Time) (retryAfter time.Duration, locked bool) {
	if attempts == nil || attempts.LockoutUntil.IsZero() || !now.Before(attempts.LockoutUntil) {
		return 0, false
	}
	remaining := attempts.LockoutUntil.Sub(now)
	if remaining < time.Second {
		remaining = time.Second
	}
	return roundUpDuration(remaining), true
}

func roundUpDuration(d time.Duration) time.Duration {
	secs := int(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return time.Duration(secs) * time.Second
}