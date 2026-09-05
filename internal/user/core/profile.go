package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Profile base-data use-cases (Story 2.1): an authenticated user views and
// edits their OWN base data (Vorname, Nachname, Anzeigename) and stages an
// email change. Self-ownership is the capability (AD-12): no permission code
// is required beyond authentication, and every operation acts exclusively on
// the authenticated user resolved by the RequireAuth gateway.

// AuditOperationProfileUpdate is the operation tag recorded in the User-owned
// audit trail when a user saves base-data edits (NFR-O1/NFR-O2).
const AuditOperationProfileUpdate = "profile.update"

// AuditOperationEmailChangeRequest is the operation tag recorded when a user
// stages a new email awaiting admin approval (NFR-O1/NFR-O2).
const AuditOperationEmailChangeRequest = "email.change.request"

// MsgEmailChangeStaged is the German confirmation returned when an email
// change is staged and awaits admin approval (Story 2.1).
const MsgEmailChangeStaged = "E-Mail-Änderung wartet auf Admin-Freigabe."

// MsgEmailUnchanged is the German microcopy for staging the user's current
// email (a no-op, rejected with 400 invalid_request).
const MsgEmailUnchanged = "Diese E-Mail-Adresse ist bereits die aktuelle Adresse."

// MsgEmailAlreadyPending is the German microcopy for re-staging the address
// the user has already staged (a no-op, rejected with 400 invalid_request).
const MsgEmailAlreadyPending = "Diese E-Mail-Adresse wartet bereits auf eine Freigabe."

// MsgEmailInUse is the German microcopy for an email already registered to
// another account or already staged by another account (400 invalid_request).
const MsgEmailInUse = "Diese E-Mail-Adresse wird bereits verwendet."

// MsgProfileNameTooLong is the German microcopy for an over-long name field.
const MsgProfileNameTooLong = "Der Name ist zu lang."

var (
	// ErrEmailUnchanged is returned when the staged email equals the user's
	// current email — a no-op rejected with 400 invalid_request.
	ErrEmailUnchanged = errors.New("email unchanged")

	// ErrEmailAlreadyPending is returned when the staged email equals the
	// address the user has already staged — a no-op rejected with 400
	// invalid_request (no duplicate audit row is written).
	ErrEmailAlreadyPending = errors.New("email already pending approval")

	// ErrEmailInUse is returned when the staged email already belongs to
	// another account (as email or pending_email, enforced by the DB-level
	// conditional UPDATE and UNIQUE constraints) — 400 invalid_request.
	ErrEmailInUse = errors.New("email already in use")

	// ErrProfileNameTooLong is returned when a name field exceeds the upper
	// bound (100 runes, matching registration). Handlers map it to a 400.
	ErrProfileNameTooLong = errors.New("profile name too long")

	// ErrForbidden is returned when an operation would act on a user that is
	// not the authenticated caller (self-ownership invariant, AD-12). The
	// RequireAuth gateway already restricts every request to the session user;
	// this is a defense-in-depth guard. Handlers map it to 403 forbidden.
	ErrForbidden = errors.New("forbidden: cannot act on another user's profile")
)

// Profile is the base-data payload of the authenticated user (Story 2.1):
// the editable fields plus the current email and any staged email awaiting
// admin approval. It is built from the authenticated session user, so it
// never carries password hashes or TOTP secrets.
type Profile struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	DisplayName  string `json:"display_name"`
	PendingEmail string `json:"pending_email,omitempty"`
}

// UpdateProfileInput captures the editable base-data payload (Story 2.1):
// Vorname, Nachname and Anzeigename. Email and state are never editable here.
type UpdateProfileInput struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
}

// Validate enforces the base-data rules: every field must be non-empty after
// trimming, and no name may exceed 100 runes (matching registration bounds).
func (in *UpdateProfileInput) Validate() error {
	if strings.TrimSpace(in.FirstName) == "" ||
		strings.TrimSpace(in.LastName) == "" ||
		strings.TrimSpace(in.DisplayName) == "" {
		return ErrMissingFields
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.FirstName)) > 100 ||
		utf8.RuneCountInString(strings.TrimSpace(in.LastName)) > 100 ||
		utf8.RuneCountInString(strings.TrimSpace(in.DisplayName)) > 100 {
		return ErrProfileNameTooLong
	}
	return nil
}

// StageEmailInput captures the email-staging payload (Story 2.1).
type StageEmailInput struct {
	Email string `json:"email"`
}

// Validate enforces the email-staging rules: a present, well-formed address.
func (in *StageEmailInput) Validate() error {
	if strings.TrimSpace(in.Email) == "" {
		return ErrInvalidEmail
	}
	if !isValidEmail(strings.ToLower(strings.TrimSpace(in.Email))) {
		return ErrInvalidEmail
	}
	return nil
}

// StageEmailResult is the confirmation returned after an email change is
// staged (Story 2.1): the account stays active on the current email and the
// new address awaits admin approval.
type StageEmailResult struct {
	Message      string `json:"message"`
	PendingEmail string `json:"pending_email"`
}

// GetProfile returns the authenticated user's base data (Story 2.1). It is
// read from the authenticated session user resolved by the RequireAuth gateway
// — no DB round-trip is needed because the session snapshot already carries
// the profile fields (including any staged pending_email). The snapshot is
// always fresh: the postgres adapter re-derives it live from users on every
// session Validate (JOIN), and the Service refreshes it after every profile
// write (RefreshSessionUser), so the endpoint and the header both reflect
// edits immediately.
func (s *Service) GetProfile(_ context.Context, user *User) (*Profile, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	return profileFromUser(user), nil
}

// UpdateProfile validates and persists the user's editable base data
// (Vorname, Nachname, Anzeigename) and writes a `profile.update` audit event
// (NFR-O1/NFR-O2). Changes take effect immediately for the authenticated
// user. Only the authenticated caller's OWN profile can be updated
// (self-ownership, AD-12). Non-active accounts are rejected with ErrForbidden
// as defense-in-depth (the gateway already only resolves active sessions).
func (s *Service) UpdateProfile(ctx context.Context, user *User, input UpdateProfileInput) (*Profile, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	// Defense-in-depth: sessions should only exist for active users; guard the
	// write anyway (the RequireAuth gateway already rejects non-active users).
	if user.State != StateActive {
		return nil, ErrForbidden
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	firstName := strings.TrimSpace(input.FirstName)
	lastName := strings.TrimSpace(input.LastName)
	displayName := strings.TrimSpace(input.DisplayName)

	updated, err := s.repo.UpdateUserProfile(ctx, user.ID, firstName, lastName, displayName)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to update profile: %w", err)
	}
	// Defense-in-depth: the persistence must never have acted on a different
	// user than the authenticated caller (self-ownership, AD-12).
	if updated == nil || updated.ID != user.ID {
		return nil, ErrForbidden
	}

	// Refresh the session snapshot so the current session (and the header,
	// which reads the cached display name) reflects the edits immediately
	// (review finding: stale session snapshot). Best-effort: a failure is
	// logged, not rolled back (availability, NFR-O1).
	if err := s.sessions.RefreshSessionUser(ctx, updated); err != nil {
		s.log().Warn("profile update session refresh failed", "error", err)
	}

	// Audit event (NFR-O1/NFR-O2): best-effort — a failed audit write must not
	// roll back the successful profile update (availability); the failure is
	// logged server-side.
	if err := s.repo.InsertAuditEvent(ctx, user.ID, AuditOperationProfileUpdate); err != nil {
		s.log().Warn("profile update audit write failed", "error", err)
	}

	return profileFromUser(updated), nil
}

// StageEmailChange validates a new email, rejects no-ops (same as current, or
// already staged) and duplicates (already registered/staged by another
// account), persists it as pending_email and writes an `email.change.request`
// audit event (NFR-O1/NFR-O2). The user stays ACTIVE on the current email
// until an admin approves the change (Epic 2 admin workflow) — until then
// login keeps using the current email.
//
// The uniqueness checks span two mechanisms. An address that is another
// account's CURRENT email is rejected up front by a lookup against the (still
// unique) users.email column. The final authority is the DB: the
// StagePendingEmail query is a conditional UPDATE that refuses to touch a row
// while any OTHER account holds the address as email OR pending_email
// (case-insensitive, lower()) — closing the TOCTOU window where a registration
// between the pre-check and the UPDATE could leave a mixed collision. The
// pending_email UNIQUE constraint backstops exact pending_email-vs-pending_email
// duplicates. "No row updated" maps to ErrEmailInUse.
func (s *Service) StageEmailChange(ctx context.Context, user *User, newEmail string) (*StageEmailResult, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	// Defense-in-depth: sessions should only exist for active users; guard the
	// write anyway (the RequireAuth gateway already rejects non-active users).
	if user.State != StateActive {
		return nil, ErrForbidden
	}
	input := StageEmailInput{Email: newEmail}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(newEmail))
	// No-op: staging the address the user already signs in with is rejected
	// (400 invalid_request) — there is nothing to change.
	if strings.EqualFold(email, strings.TrimSpace(user.Email)) {
		return nil, ErrEmailUnchanged
	}
	// No-op: re-staging the address already staged is rejected (400
	// invalid_request) and must NOT write a duplicate audit row.
	if user.PendingEmail != "" && strings.EqualFold(email, user.PendingEmail) {
		return nil, ErrEmailAlreadyPending
	}

	// EMAIL_STAGE_DUPLICATE (already registered to another user): the unique
	// constraint lives on users.email, so a staged value equal to another
	// account's real email must be rejected up front. The DB-level conditional
	// UPDATE below is the race-safe backstop (case-insensitive).
	owner, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to check existing email: %w", err)
	}
	if owner != nil && owner.ID != user.ID {
		return nil, ErrEmailInUse
	}

	updated, err := s.repo.StagePendingEmail(ctx, user.ID, email)
	if err != nil {
		if errors.Is(err, ErrEmailInUse) {
			return nil, ErrEmailInUse
		}
		if isDuplicateKeyErr(err) {
			return nil, ErrEmailInUse
		}
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to stage email change: %w", err)
	}
	// Defense-in-depth: the persistence must never have acted on a different
	// user than the authenticated caller (self-ownership, AD-12).
	if updated == nil || updated.ID != user.ID {
		return nil, ErrForbidden
	}

	// Refresh the session snapshot so a subsequent GET /profile shows the
	// staged pending_email immediately (review finding: stale session
	// snapshot). Best-effort: a failure is logged, not rolled back
	// (availability, NFR-O1).
	if err := s.sessions.RefreshSessionUser(ctx, updated); err != nil {
		s.log().Warn("email change session refresh failed", "error", err)
	}

	// Audit event (NFR-O1/NFR-O2): best-effort — a failed audit write must not
	// roll back the staged email change (availability); the failure is logged.
	if err := s.repo.InsertAuditEvent(ctx, user.ID, AuditOperationEmailChangeRequest); err != nil {
		s.log().Warn("email change request audit write failed", "error", err)
	}

	return &StageEmailResult{Message: MsgEmailChangeStaged, PendingEmail: updated.PendingEmail}, nil
}

// profileFromUser maps a domain User to the safe Profile payload.
func profileFromUser(u *User) *Profile {
	if u == nil {
		return nil
	}
	return &Profile{
		ID:           u.ID,
		Email:        u.Email,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		DisplayName:  u.DisplayName,
		PendingEmail: u.PendingEmail,
	}
}