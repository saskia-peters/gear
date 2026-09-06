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

// ResetRequestResult is the uniform confirmation returned by the forgot
// endpoint (FR-26).
type ResetRequestResult = core.ResetRequestResult

// ResetCompleteResult is the confirmation returned when a reset is completed.
type ResetCompleteResult = core.ResetCompleteResult

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
	// Profile base-data (Story 2.1): view own base data, edit Vorname/Nachname/
	// Anzeigename immediately, and stage an email change awaiting admin
	// approval. All act exclusively on the authenticated user (self-ownership).
	GetProfile(ctx context.Context, user *core.User) (*core.Profile, error)
	UpdateProfile(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error)
	StageEmailChange(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error)
	// Password reset (FR-26): RequestPasswordReset returns the uniform
	// anti-enumeration confirmation; CompletePasswordReset sets a new password
	// via a valid single-use token.
	RequestPasswordReset(ctx context.Context, email string) (*core.ResetRequestResult, error)
	CompletePasswordReset(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error)
	// Dual-admin credential recovery (FR-27): RequestAdminRecovery creates a
	// recovery request for a target admin (actor = caller);
	// ApproveAdminRecovery approves it with a mandatory Begründung + confirmation
	// and returns the single-use token to the approving admin (B), requiring a
	// TOTP code when B has MFA enabled (step-up);
	// DenyAdminRecovery denies a pending request with a Begründung;
	// ListAdminRecoveryRequest returns the pending requests for the admin-B
	// review surface; CompleteAdminRecovery consumes an approved token to set a
	// new password.
	RequestAdminRecovery(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error)
	ApproveAdminRecovery(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error)
	DenyAdminRecovery(ctx context.Context, approver *core.User, targetEmail, reason string) (*core.AdminRecoveryDenyResult, error)
	ListAdminRecoveryRequest(ctx context.Context, caller *core.User) ([]*core.AdminRecoveryRequest, error)
	CompleteAdminRecovery(ctx context.Context, rawToken, newPassword, confirm string) (*core.AdminRecoveryCompleteResult, error)
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
	InsertAuditEvent(ctx context.Context, userID, operation, detail, severity string) error
	UpdateUserProfile(ctx context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*core.User, error)
	StagePendingEmail(ctx context.Context, userID, pendingEmail string) (*core.User, error)
	ClearPendingEmail(ctx context.Context, userID string) error
	CreatePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*core.PasswordResetToken, error)
	DeleteExpiredPasswordResetTokens(ctx context.Context, userID string) error
	SetUserMustChangePassword(ctx context.Context, userID string) error
	ClearUserMustChangePassword(ctx context.Context, userID string) error
	InsertAuditEventAnonymous(ctx context.Context, operation string) error
	IsUserInPermissionGroup(ctx context.Context, userID, groupName string) (bool, error)
	// Dual-admin recovery persistence (FR-27).
	CountActiveAdmins(ctx context.Context) (int, error)
	CreateAdminRecoveryRequest(ctx context.Context, userID, requestedByUserID, tokenHash string, expiresAt time.Time) error
	ApproveAdminRecovery(ctx context.Context, userID, approvedByUserID, tokenHash string) (string, error)
	ConsumeAdminRecoveryToken(ctx context.Context, tokenHash string) (*core.AdminRecoveryToken, error)
	ListAdminRecoveryRequest(ctx context.Context) ([]*core.AdminRecoveryRequest, error)
	DenyAdminRecovery(ctx context.Context, userID string) error
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

// ResetEmailSender is the outbound reset-email delivery port (FR-26/AD-14):
// the User module never owns SMTP. This story ships a stub that reports
// NOT-configured (so the must-change-password fallback is the active default);
// Story 3.1 supplies the real SMTP sender. The sender receives the FULL
// clickable reset link (built from GEAR_APP_ORIGIN, review finding 1.8-6).
type ResetEmailSender interface {
	SendPasswordResetEmail(ctx context.Context, email, resetLink string) error
	Configured() bool
}
