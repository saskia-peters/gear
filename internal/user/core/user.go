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
)

var (
	// ErrMissingFields is returned when one or more required registration fields are empty.
	ErrMissingFields = errors.New("missing required fields")

	// ErrInvalidEmail is returned when an email address does not have a valid syntax.
	ErrInvalidEmail = errors.New("invalid email address")

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
)

// User is the domain entity for a system user.
type User struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
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
