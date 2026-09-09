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
	// server-authoritative 23-code catalog with German labels; CreateRole
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
	ListUsers(ctx context.Context, actor *core.User, status *string) ([]*core.AdminUserSummary, error)
	GetUserDetail(ctx context.Context, actor *core.User, userID string) (*core.AdminUserDetail, error)
	CreateAdminUser(ctx context.Context, actor *core.User, input core.CreateAdminUserInput) (*core.AdminUserWriteResult, error)
	UpdateAdminUser(ctx context.Context, actor *core.User, userID string, input core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error)
	DeactivateUser(ctx context.Context, actor *core.User, userID string, confirmed bool) (*core.DeactivateUserResult, error)
	// IssueOneTimePassword (Spec 2.8, FR-26 Epic 2) generates a single-use,
	// hashed, expiring one-time password for an ACTIVE account and flags it
	// must_change_password, so the user's next login with the OTP runs the
	// forced-change flow (Story 1.8) instead of issuing an app session. The
	// plaintext OTP is returned exactly once (never stored/emailed/read back).
	// Gated by `users.manage` (defense-in-depth); a non-active target → 409.
	IssueOneTimePassword(ctx context.Context, actor *core.User, userID string, confirmed bool) (*core.OneTimePasswordResult, error)
	ListUserGroups(ctx context.Context, actor *core.User) ([]*core.UserGroup, error)
	CreateUserGroup(ctx context.Context, actor *core.User, input core.CreateUserGroupInput) (*core.UserGroup, error)
	AssignUserGroupMembers(ctx context.Context, actor *core.User, groupID string, userIDs []string) (*core.UserGroup, error)
	// AssignUserGroups replaces the organisational user-group set of a user
	// from the USER detail (Effort 2), returning the refreshed user detail.
	// Gated by `user_groups.manage` (defense-in-depth).
	AssignUserGroups(ctx context.Context, actor *core.User, userID string, groupIDs []string) (*core.AdminUserWriteResult, error)
	ListUserGroupMembers(ctx context.Context, actor *core.User, groupID string) ([]string, error)
	DeleteUserGroup(ctx context.Context, actor *core.User, groupID string) error
	// User-group ROLE assignment (Spec 2.9, AD-12): an organisational user group
	// can hold permission groups (roles), so a member inherits those roles'
	// permissions. ListUserGroupRoles returns the group's current roles;
	// AssignUserGroupRoles REPLACES the role set atomically. Both are gated by
	// `user_groups.manage` (defense-in-depth) and audited.
	ListUserGroupRoles(ctx context.Context, actor *core.User, groupID string) ([]*core.RoleGroupRef, error)
	AssignUserGroupRoles(ctx context.Context, actor *core.User, groupID string, roleIDs []string) ([]*core.RoleGroupRef, error)
	// Qualification Management (Story 2.7, AD-7/FR-22): ListQualifications
	// returns the full qualification vocabulary with server-derived status
	// indicators plus the user roster for the assignment editor;
	// CreateQualification/UpdateQualification persist the vocabulary rows
	// (duplicate name → 409, unknown id → 404); ListQualificationAssignees
	// returns the current assignees of a qualification;
	// AssignQualificationUsers replaces the assignee set atomically. The whole
	// surface is gated by `qualifications.manage` at the route mount and
	// re-verified in the core (defense-in-depth, AD-6).
	ListQualifications(ctx context.Context, actor *core.User) (*core.QualificationListResult, error)
	CreateQualification(ctx context.Context, actor *core.User, input core.CreateQualificationInput) (*core.QualificationWriteResult, error)
	UpdateQualification(ctx context.Context, actor *core.User, id string, input core.UpdateQualificationInput) (*core.QualificationWriteResult, error)
	ListQualificationAssignees(ctx context.Context, actor *core.User, id string) ([]*core.QualificationAssignee, error)
	AssignQualificationUsers(ctx context.Context, actor *core.User, id string, userIDs []string) (*core.QualificationAssignResult, error)
	// Per-user qualification assignment (Spec 2.9, `users.qualifications.manage`):
	// AssignUserQualification assigns a qualification to a user (a `fixed`
	// qualification REQUIRES a per-user expires_at); RevokeUserQualification
	// revokes it; UpdateUserQualificationExpiry edits the per-user valid-until.
	// All three are gated by `users.qualifications.manage` (defense-in-depth)
	// and audited; resolution is live per request.
	AssignUserQualification(ctx context.Context, actor *core.User, userID, qualificationID string, expiresAt *time.Time) (*core.UserQualificationAssignResult, error)
	RevokeUserQualification(ctx context.Context, actor *core.User, userID, qualificationID string) (*core.UserQualificationAssignResult, error)
	UpdateUserQualificationExpiry(ctx context.Context, actor *core.User, userID, qualificationID string, expiresAt *time.Time) (*core.UserQualificationAssignResult, error)
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
	DeletePasswordResetToken(ctx context.Context, tokenHash string) error
	SetUserMustChangePassword(ctx context.Context, userID string) error
	ClearUserMustChangePassword(ctx context.Context, userID string) error
	InsertAuditEventAnonymous(ctx context.Context, operation string) error
	// One-time-password persistence (Spec 2.8): SetUserOneTimePassword upserts
	// the Argon2id hash + expiry of an admin-issued OTP and flags
	// must_change_password, reporting whether a row was affected (false = the
	// target vanished); ClearUserOneTimePassword atomically consumes the OTP
	// via compare-and-swap on the stored hash (false = already consumed by a
	// racing login); GetUserByID resolves a user's profile + state (unknown id
	// → ErrAdminUserNotFound).
	SetUserOneTimePassword(ctx context.Context, userID, hash string, expiresAt time.Time) (bool, error)
	ClearUserOneTimePassword(ctx context.Context, userID, hash string) (bool, error)
	GetUserByID(ctx context.Context, userID string) (*core.User, error)
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
	ListUsers(ctx context.Context, status *string) ([]*core.AdminUserSummary, error)
	// ListUserGroupNamesByUsers returns the organisational user-group (team)
	// names each listed user belongs to (Effort 2), keyed by user id — one
	// lookup for the whole admin user list so the SPA table can render inline
	// group tags without a per-row query. Membership grants NO permission
	// (AD-12); this is display data only.
	ListUserGroupNamesByUsers(ctx context.Context, userIDs []string) (map[string][]string, error)
	GetUserDetail(ctx context.Context, userID string) (*core.AdminUserDetail, error)
	CreateAdminUser(ctx context.Context, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error)
	UpdateAdminUser(ctx context.Context, userID, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error)
	DeactivateUser(ctx context.Context, userID string) (*core.User, error)
	ListUserGroups(ctx context.Context) ([]*core.UserGroup, error)
	CreateUserGroup(ctx context.Context, name, description string) (*core.UserGroup, error)
	AssignUserGroupMembers(ctx context.Context, groupID string, userIDs []string) (*core.UserGroup, error)
	ReplaceUserGroupMemberships(ctx context.Context, userID string, groupIDs []string) (*core.User, error)
	ListUserGroupMembers(ctx context.Context, groupID string) ([]string, error)
	DeleteUserGroup(ctx context.Context, groupID string) error
	// User-group ROLE persistence (Spec 2.9): ListUserGroupRoles returns the
	// roles a group grants its members; ReplaceUserGroupRoles replaces the role
	// set atomically (delete-then-insert in one transaction).
	ListUserGroupRoles(ctx context.Context, groupID string) ([]*core.RoleGroupRef, error)
	ReplaceUserGroupRoles(ctx context.Context, groupID string, roleIDs []string) ([]*core.RoleGroupRef, error)
	// Qualification Management persistence (Story 2.7, AD-7/FR-22).
	ListQualificationVocabulary(ctx context.Context) ([]*core.Qualification, error)
	CreateQualification(ctx context.Context, name, description, expiryKind string) (*core.Qualification, error)
	UpdateQualification(ctx context.Context, id, name, description, expiryKind string) (*core.Qualification, error)
	ListQualificationAssignees(ctx context.Context, id string) ([]*core.QualificationAssignee, error)
	ReplaceQualificationAssignees(ctx context.Context, id string, userIDs []string) ([]*core.QualificationAssignee, error)
	// Per-user qualification persistence (Spec 2.9): AssignQualificationToUser
	// assigns a qualification to a user with an optional per-assignment
	// expires_at (enforcing the fixed-vs-unlimited rule);
	// RevokeQualificationFromUser revokes it; UpdateUserQualificationExpiry
	// edits the per-user valid-until.
	AssignQualificationToUser(ctx context.Context, userID, qualificationID string, expiresAt *time.Time) error
	RevokeQualificationFromUser(ctx context.Context, userID, qualificationID string) error
	UpdateUserQualificationExpiry(ctx context.Context, userID, qualificationID string, expiresAt *time.Time) error
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
