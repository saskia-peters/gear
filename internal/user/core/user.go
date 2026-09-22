// Package core is the domain core of the User Directory & Auth hexagon (AD-2).
// It owns the business rules for users, credentials, and registration.
package core

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// German user-facing validation messages
	MsgMissingFields    = "Alle Pflichtfelder müssen ausgefüllt sein."
	MsgInvalidEmail     = "Bitte gib eine gültige E-Mail-Adresse ein."
	MsgShortPassword    = "Das Passwort muss mindestens 10 Zeichen lang sein."
	MsgPasswordMismatch = "Die Passwörter stimmen nicht überein."
	MsgPasswordTooLong  = "Das Passwort ist zu lang."
	// MsgEmailReserved rejects a RESERVED email (Story 3.4): the
	// `deleted.<id>@deleted.local` tombstone domain of DSGVO-deleted accounts
	// must stay unclaimable.
	MsgEmailReserved = "Diese E-Mail-Adresse kann nicht verwendet werden."
)

// ReservedEmailDomain is the TLD+label the DSGVO account-deletion tombstones
// use (Story 3.4, migration 000030): every scrubbed account's email becomes
// `deleted.<id>@deleted.local`. The register validation rejects any
// registration on this domain so the placeholder addresses can never collide
// with a real account (case-insensitive — `@DELETED.LOCAL` is equally rejected).
const ReservedEmailDomain = "@deleted.local"

var (
	// ErrMissingFields is returned when one or more required registration fields are empty.
	ErrMissingFields = errors.New("missing required fields")

	// ErrInvalidEmail is returned when an email address does not have a valid syntax.
	ErrInvalidEmail = errors.New("invalid email address")

	// ErrEmailReserved is returned when a registration email is a RESERVED
	// address (Story 3.4): the `deleted.<id>@deleted.local` tombstones of
	// DSGVO-deleted accounts live on that domain, so no real account may ever
	// register one — the placeholder addresses must stay unclaimable.
	ErrEmailReserved = errors.New("email address is reserved")

	// ErrShortPassword is returned when a password has fewer than 10 characters (FR-2).
	ErrShortPassword = errors.New("password too short")

	// ErrPasswordMismatch is returned when password and confirmation do not match.
	ErrPasswordMismatch = errors.New("passwords do not match")

	// ErrPasswordTooLong is returned when a password exceeds the upper bound
	// (1024 runes). A sentinel so handlers can map it to a 400 instead of a 500.
	ErrPasswordTooLong = errors.New("password too long")

	// ErrUserAlreadyExists is returned when attempting to register a user with an existing email.
	ErrUserAlreadyExists = errors.New("user already exists")

	// ErrUserNotFound is returned when an operation references a user ID with no
	// matching row (e.g. the account was deleted mid-session). Handlers map it
	// to a clear 4xx rather than a generic 500.
	ErrUserNotFound = errors.New("user not found")
)

type UserState string

const (
	StatePendingApproval UserState = "pending_approval"
	StateActive          UserState = "active"
	StateDeactivated     UserState = "deactivated"
	// StateDeleted is the scrubbed tombstone of a DSGVO-deleted account (Story
	// 3.4, FR-24/AD-8): re-login is permanently blocked (only StateActive
	// authenticates — auth.go / session.go) and the personal data lives in the
	// dsgvo_deleted_accounts archive. The tombstone + archive are hard-purged
	// only on admin demand (the sole hard delete).
	StateDeleted UserState = "deleted"
)

// User is the domain entity for a system user.
type User struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	// PendingEmail holds a staged email change awaiting admin approval (Story
	// 2.1). Until approval the account stays active on Email, which remains the
	// login identifier. Empty means no staged change.
	PendingEmail string         `json:"pending_email,omitempty"`
	DisplayName  string         `json:"display_name"`
	FirstName    string         `json:"first_name"`
	LastName     string         `json:"last_name"`
	PasswordHash string         `json:"-"`
	State        UserState      `json:"state"`
	IsMFAEnabled bool           `json:"is_mfa_enabled"`
	// TotpSecretEncrypted holds the AES-256-GCM ciphertext of the TOTP shared
	// secret (NFR-S4). Never serialized to clients (json:"-") and never the
	// plaintext secret.
	TotpSecretEncrypted string `json:"-"`
	// PendingTotpSecretEncrypted and PendingTotpExpiresAt hold the short-lived
	// enrollment secret (encrypted at rest) and its expiry (FR-4). The confirm
	// step validates a code against this server-issued secret. Never serialized
	// to clients (json:"-") and never the plaintext secret.
	PendingTotpSecretEncrypted string    `json:"-"`
	PendingTotpExpiresAt       time.Time `json:"-"`
	// MustChangePassword flags an account whose next login must force a
	// mandatory password change before app access (FR-26, SMTP-not-configured
	// fallback / Epic 2 one-time password). It is cleared once the user
	// completes a change via a reset link. Never serialized to clients.
	MustChangePassword bool `json:"-"`
	// OneTimePasswordHash holds the Argon2id hash of a currently valid
	// single-use one-time password (Spec 2.8). Empty means no OTP is issued.
	// The plaintext OTP is shown once in the issuance response and never
	// stored, emailed or read back (NFR-S4). Never serialized to clients.
	OneTimePasswordHash string `json:"-"`
	// OneTimePasswordExpiresAt is the TTL of the issued OTP (Spec 2.8). A zero
	// value means no OTP is issued. Never serialized to clients.
	OneTimePasswordExpiresAt time.Time `json:"-"`
	Attributes                 map[string]any `json:"attributes,omitempty"`
	CreatedAt                  time.Time      `json:"created_at"`
	UpdatedAt                  time.Time      `json:"updated_at"`
}

// RegisterInput captures the user self-registration payload.
type RegisterInput struct {
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

// Validate checks domain constraints on registration input.
func (in *RegisterInput) Validate() error {
	if strings.TrimSpace(in.FirstName) == "" ||
		strings.TrimSpace(in.LastName) == "" ||
		strings.TrimSpace(in.Email) == "" ||
		in.Password == "" ||
		in.PasswordConfirm == "" {
		return ErrMissingFields
	}

	email := strings.TrimSpace(in.Email)
	if !isValidEmail(email) {
		return ErrInvalidEmail
	}
	// Story 3.4 reserved-domain guard: `deleted.<id>@deleted.local` tombstones
	// live on this domain — no real registration may claim it (case-insensitive,
	// so `@DELETED.LOCAL` is equally rejected).
	if strings.HasSuffix(strings.ToLower(email), ReservedEmailDomain) {
		return ErrEmailReserved
	}

	if utf8.RuneCountInString(in.FirstName) > 100 || utf8.RuneCountInString(in.LastName) > 100 {
		return errors.New("name too long")
	}

	if utf8.RuneCountInString(in.Password) < 10 {
		return ErrShortPassword
	}

	if utf8.RuneCountInString(in.Password) > 1024 {
		return errors.New("password too long")
	}

	if in.Password != in.PasswordConfirm {
		return ErrPasswordMismatch
	}

	return nil
}

func isValidEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := parts[1]
	if !strings.Contains(domain, ".") {
		return false
	}
	domainParts := strings.Split(domain, ".")
	for _, part := range domainParts {
		if part == "" {
			return false
		}
	}
	return true
}
