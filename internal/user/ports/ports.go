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
	// ResolvePermissionSet resolves a user's live permission set (AD-12, Story
	// 2.2): the additive union of permission-group memberships + direct grants,
	// resolved per request — never cached — so revocation is immediate
	// (AD-2/AD-6/FR-21/FR-22). Other modules (Tool, Admin) authorize against it
	// through this port; the gateway enforces the same live set per request.
	ResolvePermissionSet(ctx context.Context, user *core.User) ([]string, error)
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
	// User approval workflow (Story 2.4, FR-20): ListPending returns the
	// pending-approval users (oldest first) for the admin review surface;
	// ApproveUser activates a pending user and seeds the default 'helfende'
	// role (atomic); RejectUser moves a pending user to deactivated so the
	// pending record disappears and the account can neither log in nor
	// re-register. All three are gated by `users.approve` at the route mount
	// and re-verified in the core (AD-6).
	ListPending(ctx context.Context, actor *core.User) ([]*core.PendingUser, error)
	ApproveUser(ctx context.Context, actor *core.User, userID string) (*core.UserApprovalResult, error)
	RejectUser(ctx context.Context, actor *core.User, userID string) (*core.UserApprovalResult, error)
	// Role & Permission-Group Management (Story 2.5, AD-12/AD-6/FR-19):
	// ListRoles returns every permission group (base roles first) plus the
	// server-authoritative 21-code catalog with German labels; CreateRole
	// creates a named group with its additive permission set atomically;
	// UpdateRole replaces a group's name/description and permission set
	// atomically (base roles editable). The whole surface is gated by any of
	// the `roles.*` codes at the route mount; the core re-verifies the exact
	// code (create = roles.create, edit = roles.edit) defense-in-depth.
	ListRoles(ctx context.Context, actor *core.User) (*core.RoleListResult, error)
	CreateRole(ctx context.Context, actor *core.User, input core.CreateRoleInput) (*core.RoleGroup, error)
	UpdateRole(ctx context.Context, actor *core.User, id string, input core.UpdateRoleInput) (*core.RoleGroup, error)
	// User & Group Administration (Story 2.6, AD-12/AD-6/FR-19/FR-21/FR-22):
	// ListUsers returns every user (id, names, email, state) for the admin
	// "Benutzer" list; GetUserDetail composes a user's profile + roles +
	// user groups + direct grants + qualification assignments (with
	// per-assignment status); CreateAdminUser/UpdateAdminUser persist the
	// profile and all three assignment sets atomically; DeactivateUser flips
	// an active user to deactivated ("→ Sofort kein Login", audited) and
	// requires confirmation; ListUserGroups/CreateUserGroup manage the
	// organisational teams; AssignUserGroupMembers replaces a group's member
	// set atomically. The user surface is gated by any `users.*` code at the
	// route mount; the core re-verifies the exact code per action
	// (list/detail = any users.*, create/edit/deactivate = users.manage) and
	// the user-group surface by `user_groups.manage`, defense-in-depth.
	ListUsers(ctx context.Context, actor *core.User) ([]*core.AdminUserSummary, error)
	GetUserDetail(ctx context.Context, actor *core.User, userID string) (*core.AdminUserDetail, error)
	CreateAdminUser(ctx context.Context, actor *core.User, input core.CreateAdminUserInput) (*core.AdminUserWriteResult, error)
	UpdateAdminUser(ctx context.Context, actor *core.User, userID string, input core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error)
	DeactivateUser(ctx context.Context, actor *core.User, userID string, confirmed bool) (*core.DeactivateUserResult, error)
	ListUserGroups(ctx context.Context, actor *core.User) ([]*core.UserGroup, error)
	CreateUserGroup(ctx context.Context, actor *core.User, input core.CreateUserGroupInput) (*core.UserGroup, error)
	AssignUserGroupMembers(ctx context.Context, actor *core.User, groupID string, userIDs []string) (*core.UserGroup, error)
	ListUserGroupMembers(ctx context.Context, actor *core.User, groupID string) ([]string, error)
	DeleteUserGroup(ctx context.Context, actor *core.User, groupID string) error
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
	// User approval persistence (Story 2.4, FR-20).
	ListPendingUsers(ctx context.Context) ([]*core.PendingUser, error)
	ApproveUser(ctx context.Context, userID string) (*core.User, error)
	RejectUser(ctx context.Context, userID string) (*core.User, error)
	// Role & Permission-Group persistence (Story 2.5, AD-12).
	ListGroups(ctx context.Context) ([]*core.RoleGroup, error)
	CreateGroup(ctx context.Context, name, description string, permissionCodes []string) (*core.RoleGroup, error)
	UpdateGroup(ctx context.Context, id, name, description string, permissionCodes []string) (*core.RoleGroup, error)
	ListAllPermissions(ctx context.Context) ([]*core.PermissionCatalogEntry, error)
	// User & Group Administration persistence (Story 2.6, AD-12).
	ListUsers(ctx context.Context) ([]*core.AdminUserSummary, error)
	GetUserDetail(ctx context.Context, userID string) (*core.AdminUserDetail, error)
	CreateAdminUser(ctx context.Context, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error)
	UpdateAdminUser(ctx context.Context, userID, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error)
	DeactivateUser(ctx context.Context, userID string) (*core.User, error)
	ListUserGroups(ctx context.Context) ([]*core.UserGroup, error)
	CreateUserGroup(ctx context.Context, name, description string) (*core.UserGroup, error)
	AssignUserGroupMembers(ctx context.Context, groupID string, userIDs []string) (*core.UserGroup, error)
	ListUserGroupMembers(ctx context.Context, groupID string) ([]string, error)
	DeleteUserGroup(ctx context.Context, groupID string) error
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
