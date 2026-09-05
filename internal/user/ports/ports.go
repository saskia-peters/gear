// Package ports declares the inbound/outbound port interfaces of the User
// Directory & Auth hexagon (AD-1/AD-2).
package ports

import (
	"context"
	"time"

	"github.com/saskia-peters/gear/internal/user/core"
)

// RegisterResult is the anti-enumeration confirmation returned on registration.
type RegisterResult = core.RegisterResult

// LoginResult is the payload returned on a successful login.
type LoginResult = core.LoginResult

// Service is the User Directory & Auth inbound port (AD-2).
type Service interface {
	Register(ctx context.Context, input core.RegisterInput) (*core.RegisterResult, error)
	Login(ctx context.Context, input core.LoginInput) (*core.LoginResult, error)
	Logout(ctx context.Context, rawToken string) error
	// TOTP MFA (FR-4): enroll request/confirm and disable, all acting on the
	// authenticated user.
	EnrollMFARequest(ctx context.Context, user *core.User) (*core.MFAEnrollResult, error)
	ConfirmMFAEnable(ctx context.Context, user *core.User, secret, code string) error
	DisableMFA(ctx context.Context, user *core.User, code string) error
	MFAStatus(ctx context.Context, user *core.User) (bool, error)
	// RevokeOtherSessions/RevokeAllSessions invalidate the user's sessions when
	// MFA is enabled or disabled (review finding 1.6-2).
	RevokeOtherSessions(ctx context.Context, userID, rawToken string) error
	RevokeAllSessions(ctx context.Context, userID string) error
	// ChangePassword is the self-service password change use-case (FR-25):
	// confirm current password, validate + store new hash, revoke other
	// sessions, audit the change.
	ChangePassword(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error)
}

// Repository is the outbound persistence port for User data.
type Repository interface {
	CreateRegisteredUser(ctx context.Context, email, displayName, firstName, lastName, passwordHash string) (*core.User, error)
	GetUserByEmail(ctx context.Context, email string) (*core.User, error)
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
	GetLoginAttempts(ctx context.Context, email string) (*core.LoginAttempts, error)
	IncrementLoginAttempts(ctx context.Context, email string) error
	ClearLoginAttempts(ctx context.Context, email string) error
	SetUserTotpSecret(ctx context.Context, userID, encryptedSecret string) error
	ClearUserTotpSecret(ctx context.Context, userID string) error
	SetUserPendingTotpSecret(ctx context.Context, userID, encryptedSecret string, expiresAt time.Time) error
	ClearUserPendingTotpSecret(ctx context.Context, userID string) error
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) (*core.User, error)
	InsertAuditEvent(ctx context.Context, userID, operation string) error
}

// PasswordHasher is the outbound password hashing port (AD-13).
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

// SecretCipher is the outbound TOTP secret encryption port (NFR-S4).
type SecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}
