package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User & Group Administration (Story 2.6, AD-2/AD-6/FR-19/FR-21/FR-22, AD-12):
// the admin "Benutzer" surface — list/create/edit/deactivate users, manage
// organisational user groups (teams) and their members, and view a user's
// roles, user groups, direct permission grants and qualification assignments.
//
// The whole surface is gated by ANY of the three `users.*` codes at the HTTP
// sub-mount (users.view/users.manage/users.approve, the same codes the SPA nav
// uses for the Benutzer entry); the core re-verifies the exact code per action
// defense-in-depth: listing/detail need any `users.*` code, create/edit/
// deactivate need `users.manage` (Design Notes spec 2.6). The user-group
// endpoints are gated by `user_groups.manage`.
//
// Organisational user groups grant NO permission (AD-12) — they are teams, not
// roles. Only the permission-group/direct-grant path affects access, and the
// permission-resolution query never joins user_groups (already true). Every
// change takes effect immediately on the next request because resolution is
// live per request (AD-2/FR-21/FR-22) — nothing here caches.

// User-administration permission codes (AD-12). Same codes the SPA nav uses
// for the Benutzer entry (Story 2.3), so server gate and client visibility
// never drift. UserApprovePermission is declared in approval.go.
const (
	// UserViewPermission gates the read-only user-management surfaces.
	UserViewPermission = "users.view"
	// UserManagePermission gates create/edit/deactivate of users.
	UserManagePermission = "users.manage"
	// UserGroupsManagePermission gates the organisational user-group endpoints.
	UserGroupsManagePermission = "user_groups.manage"
)

// User-administration audit operation codes (NFR-O1/NFR-O2). Distinct from the
// approval/recovery/role codes so user-management actions are separately
// auditable (they are at least as sensitive as user approvals).
const (
	AuditOperationUserCreate        = "user.create"
	AuditOperationUserUpdate        = "user.update"
	AuditOperationUserDeactivate    = "user.deactivate"
	AuditOperationUserGroupCreate   = "user_group.create"
	AuditOperationUserGroupAssign   = "user_group.assign"
	AuditOperationUserGroupDelete   = "user_group.delete"
)

// AdminUserSummary is one row of the admin "Benutzer" list (Story 2.6): id,
// names, email and state, server-authoritative. Status carries the raw
// account state (active / pending_approval / deactivated) — the client maps it
// to the German badges (aktiv / pending / deaktiviert). No secret material.
type AdminUserSummary struct {
	ID       string `json:"id"`
	Vorname  string `json:"vorname"`
	Nachname string `json:"nachname"`
	Email    string `json:"email"`
	Status   string `json:"status"`
}

// RoleGroupRef is a permission-group membership of a user, for the user detail
// surface (Story 2.6, AD-12). Roles ARE the access-granting groups; base roles
// stay flagged so the client can badge them.
type RoleGroupRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsBaseRole bool   `json:"is_base_role"`
}

// UserGroupRef is an organisational user-group (team) membership of a user
// (Story 2.6, AD-12). Membership grants NO permission by itself.
type UserGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DirectGrantRef is a direct one-off permission grant of a user (Story 2.6,
// additive AD-12). Separate from roles; joins the resolved set immediately.
type DirectGrantRef struct {
	PermissionID string    `json:"permission_id"`
	Code         string    `json:"code"`
	GrantedAt    time.Time `json:"granted_at"`
}

// Qualification expiry kinds (spine table 9, Story 2.6). 'unlimited' never
// expires (Unbegrenzt); 'fixed' carries an expires_at and the assignment
// status derives from it (Gültig / Bald ablaufend / Abgelaufen).
const (
	QualificationExpiryUnlimited = "unlimited"
	QualificationExpiryFixed     = "fixed"
)

// Qualification display statuses (FR-22/AD-7, UX-DR8). The WIRE values are
// stable English codes so the SPA can key its German label map and badge
// classes on them (the client owns the German display strings).
const (
	QualificationStatusValid        = "valid"
	QualificationStatusExpiringSoon = "expiring_soon"
	QualificationStatusExpired      = "expired"
	QualificationStatusUnlimited    = "unlimited"
)

// qualificationExpiringSoonWindow is how close to the expiry date an assignment
// turns "Bald ablaufend" (30 days).
const qualificationExpiringSoonWindow = 30 * 24 * time.Hour

// QualificationAssignment is one qualification a user holds, for the user
// detail surface (Story 2.6, AD-7/FR-22). The status is derived from the
// qualification's expiry model and the current time (display only — editing
// assignments is Story 2.7).
type QualificationAssignment struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ExpiryKind  string     `json:"expiry_kind"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	AssignedAt  time.Time  `json:"assigned_at"`
	Status      string     `json:"status"`
}

// AdminUserDetail is the full user-detail payload (Story 2.6): the profile
// plus roles (permission groups), user groups (teams), direct permission
// grants and qualification assignments. No secret material.
type AdminUserDetail struct {
	ID             string                    `json:"id"`
	Vorname        string                    `json:"vorname"`
	Nachname       string                    `json:"nachname"`
	Email          string                    `json:"email"`
	Status         string                    `json:"status"`
	Roles          []RoleGroupRef            `json:"roles"`
	UserGroups     []UserGroupRef            `json:"user_groups"`
	DirectGrants   []DirectGrantRef          `json:"direct_grants"`
	Qualifications []QualificationAssignment `json:"qualifications"`
}

// AdminUserWriteResult is the payload returned by create/update (Story 2.6):
// the German confirmation message plus the resulting user detail, so the SPA
// never hardcodes its own success text.
type AdminUserWriteResult struct {
	Message string           `json:"message"`
	User    *AdminUserDetail `json:"user"`
}

// UserGroup is an organisational user group (team, AD-12) for the admin
// user-group surface and the user editor's assignment grid.
type UserGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateAdminUserInput is the POST /users body (Story 2.6): the profile
// fields, the initial state (active or pending_approval) and the optional
// assignment sets (roles via permission-group ids, user groups via
// user-group ids, direct grants via permission codes). Credentials are
// provisioned out-of-band, so no password is part of the input.
type CreateAdminUserInput struct {
	Vorname          string   `json:"vorname"`
	Nachname         string   `json:"nachname"`
	Email            string   `json:"email"`
	Status           string   `json:"status"`
	RoleIDs          []string `json:"role_ids"`
	UserGroupIDs     []string `json:"user_group_ids"`
	DirectGrantCodes []string `json:"direct_grant_codes"`
}

// UpdateAdminUserInput is the PUT /users/{id} body — the same shape as create;
// the profile fields, the state and ALL three assignment sets are replaced
// atomically (delete-then-insert in one transaction, Story 2.5 lesson).
type UpdateAdminUserInput struct {
	Vorname          string   `json:"vorname"`
	Nachname         string   `json:"nachname"`
	Email            string   `json:"email"`
	Status           string   `json:"status"`
	RoleIDs          []string `json:"role_ids"`
	UserGroupIDs     []string `json:"user_group_ids"`
	DirectGrantCodes []string `json:"direct_grant_codes"`
}

// CreateUserGroupInput is the POST /user-groups body (Story 2.6).
type CreateUserGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DeactivateUserResult is the confirmation returned by a deactivation
// (Story 2.6, FR-21): the target's identity so the client can show specific
// feedback. "→ Sofort kein Login" is enforced server-side by the state flip
// (session validation rejects non-active accounts immediately, Story 1.4).
type DeactivateUserResult struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
}

// User-group administration sentinel errors. Handlers map them to the uniform
// envelope (FR-19): ErrAdminUserEmailTaken → 409 conflict, ErrAdminUserNotFound
// → 404 not_found, ErrUserNotActiveForDeactivate → 409/404 (never an existence
// hint), ErrUserGroupNameTaken → 409, ErrUserGroupNotFound → 404, the
// validation errors → 400 invalid/invalid_request.
var (
	// ErrAdminUserEmailTaken is returned when a create/update uses an email
	// already held by another account, compared case-insensitively.
	ErrAdminUserEmailTaken = errors.New("admin user email is already taken")
	// ErrAdminUserNotFound is returned when a detail/edit/deactivate targets an
	// unknown user id (uniform 404, no existence leak beyond what the admin
	// already sees, FR-19).
	ErrAdminUserNotFound = errors.New("admin user not found")
	// ErrAdminUserInvalidName is returned when Vorname/Nachname is empty or
	// exceeds the rune cap (400 invalid_request).
	ErrAdminUserInvalidName = errors.New("admin user name is invalid")
	// ErrAdminUserInvalidEmail is returned when the email is syntactically
	// invalid (400 invalid_request, distinct from the 409 duplicate).
	ErrAdminUserInvalidEmail = errors.New("admin user email is invalid")
	// ErrAdminUserInvalidStatus is returned when the requested status is not one
	// of active/pending_approval (400 invalid_request — deactivation is its own
	// confirmed flow, never a status field).
	ErrAdminUserInvalidStatus = errors.New("admin user status is invalid")
	// ErrAdminUserUnknownRole is returned when a role id does not exist
	// (400 invalid, additive-only).
	ErrAdminUserUnknownRole = errors.New("admin user role is unknown")
	// ErrAdminUserUnknownUserGroup is returned when a user-group id does not
	// exist (400 invalid).
	ErrAdminUserUnknownUserGroup = errors.New("admin user user-group is unknown")
	// ErrUserNotActiveForDeactivate is returned when deactivation targets a
	// user that is not currently active (already deactivated or pending) — the
	// uniform conflict (no existence leak).
	ErrUserNotActiveForDeactivate = errors.New("user is not active, cannot be deactivated")
	// ErrDeactivationNotConfirmed is returned when the client did not confirm
	// the deactivation (400 invalid_request).
	ErrDeactivationNotConfirmed = errors.New("deactivation requires confirmation")
	// ErrSelfDeactivation is returned when an admin tries to deactivate their
	// OWN account (409 conflict with a clear German message) — an admin must
	// never be able to lock themselves out of the module they manage.
	ErrSelfDeactivation = errors.New("cannot deactivate own account")
	// ErrUserGroupNameTaken is returned when a user-group create uses a name
	// already held by another group, compared case-insensitively.
	ErrUserGroupNameTaken = errors.New("user group name is already taken")
	// ErrUserGroupInvalidName is returned when the name is empty or exceeds the
	// 120-rune cap (400 invalid_request, distinct from the 409 duplicate).
	ErrUserGroupInvalidName = errors.New("user group name is invalid")
	// ErrUserGroupNotFound is returned when a group-member assignment targets
	// an unknown user-group id.
	ErrUserGroupNotFound = errors.New("user group not found")
	// ErrUserGroupMemberUnknown is returned when an assigned member user id does
	// not exist (400 invalid).
	ErrUserGroupMemberUnknown = errors.New("user group member is unknown")
)

// German user-facing microcopy for the user-administration surface
// (UX-DR6/UX-DR8).
const (
	// MsgUserCreated confirms a successful account creation.
	MsgUserCreated = "Benutzer angelegt. Zugangsdaten werden separat vergeben."
	// MsgUserUpdated confirms a successful profile/assignment update.
	MsgUserUpdated = "Benutzer gespeichert. Änderungen gelten ab der nächsten Anfrage."
	// MsgUserDeactivated confirms the deactivation ("→ Sofort kein Login").
	MsgUserDeactivated = "Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich."
	// MsgAdminUserEmailTaken is the uniform 409 conflict message.
	MsgAdminUserEmailTaken = "Es gibt bereits ein Konto mit dieser E-Mail-Adresse."
	// MsgAdminUserNotFound is the uniform 404 not-found message.
	MsgAdminUserNotFound = "Der Benutzer wurde nicht gefunden."
	// MsgUserNotActiveForDeactivate is the uniform conflict message for
	// deactivating a non-active user (no existence leak, FR-19).
	MsgUserNotActiveForDeactivate = "Nur aktive Benutzer können deaktiviert werden."
	// MsgDeactivationConfirmationRequired tells the client to confirm the
	// destructive deactivation step.
	MsgDeactivationConfirmationRequired = "Bitte bestätige die Deaktivierung."
	// MsgSelfDeactivation is the uniform 409 message when an admin tries to
	// deactivate their own account.
	MsgSelfDeactivation = "Du kannst dein eigenes Konto nicht deaktivieren."
	// MsgAdminUserInvalidName is the uniform 400 message for a missing/too-long
	// name.
	MsgAdminUserInvalidName = "Bitte gib Vor- und Nachnamen an (maximal 100 Zeichen)."
	// MsgAdminUserInvalidEmail is the uniform 400 message for a malformed email.
	MsgAdminUserInvalidEmail = "Bitte gib eine gültige E-Mail-Adresse ein."
	// MsgAdminUserInvalidStatus is the uniform 400 message for a bad status.
	MsgAdminUserInvalidStatus = "Der Status ist ungültig."
	// MsgAdminUserUnknownRole is the uniform 400 message for an unknown role id.
	MsgAdminUserUnknownRole = "Eine ausgewählte Rolle ist ungültig."
	// MsgAdminUserUnknownUserGroup is the uniform 400 message for an unknown
	// user-group id.
	MsgAdminUserUnknownUserGroup = "Eine ausgewählte Benutzergruppe ist ungültig."
	// MsgUserGroupCreated confirms a successful user-group creation.
	MsgUserGroupCreated = "Benutzergruppe erstellt."
	// MsgUserGroupNameTaken is the uniform 409 conflict message.
	MsgUserGroupNameTaken = "Es gibt bereits eine Benutzergruppe mit diesem Namen."
	// MsgUserGroupNameRequired is the uniform 400 message for an empty/too-long
	// name.
	MsgUserGroupNameRequired = "Bitte gib einen Namen für die Benutzergruppe an (maximal 120 Zeichen)."
	// MsgUserGroupNotFound is the uniform 404 not-found message.
	MsgUserGroupNotFound = "Die Benutzergruppe wurde nicht gefunden."
	// MsgUserGroupMembersUpdated confirms a successful member-set replacement.
	MsgUserGroupMembersUpdated = "Mitglieder der Benutzergruppe aktualisiert."
	// MsgUserGroupDeleted confirms a successful user-group deletion.
	MsgUserGroupDeleted = "Benutzergruppe gelöscht."
	// MsgUserGroupMemberUnknown is the uniform 400 message for an unknown
	// assigned member.
	MsgUserGroupMemberUnknown = "Eine ausgewählte Person ist ungültig."
)

// UserNameMaxLength caps a user's Vorname/Nachname at 100 runes (matches the
// registration bound, Story 1.3).
const UserNameMaxLength = 100

// UserGroupNameMaxLength caps a user-group name at 120 runes (Story 2.6).
const UserGroupNameMaxLength = 120

// ListUsers returns every user (id, names, email, state) ordered by name, for
// the admin "Benutzer" list surface (Story 2.6). The caller is gated by ANY of
// the `users.*` codes at the sub-mount; here it is re-verified
// defense-in-depth (AD-2/AD-6).
func (s *Service) ListUsers(ctx context.Context, actor *User) ([]*AdminUserSummary, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireAnyUserPermission(ctx, actor); err != nil {
		return nil, err
	}
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list users: %w", err)
	}
	return users, nil
}

// GetUserDetail returns the full user detail — profile, roles (permission
// groups), user groups (teams), direct grants and qualification assignments
// with per-assignment status (Story 2.6). An unknown id maps to
// ErrAdminUserNotFound → uniform 404. Gated by `users.view` (or any users.*
// code) at the core; the caller must be an active authorized holder.
func (s *Service) GetUserDetail(ctx context.Context, actor *User, userID string) (*AdminUserDetail, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireAnyUserPermission(ctx, actor); err != nil {
		return nil, err
	}
	detail, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to load user detail: %w", err)
	}
	// Compute the per-assignment display status server-side (FR-22/AD-7): the
	// expiry model lives on the qualification; the core derives Gültig /
	// Bald ablaufend / Abgelaufen / Unbegrenzt from now.
	now := time.Now().UTC()
	for i := range detail.Qualifications {
		detail.Qualifications[i].Status = qualificationStatus(detail.Qualifications[i], now)
	}
	return detail, nil
}

// CreateAdminUser creates a user from the admin surface (Story 2.6): the
// profile fields, an explicit initial state (active or pending_approval) and
// the optional assignment sets (roles, user groups, direct grants) are
// persisted atomically. The email is unique case-insensitively (a duplicate
// maps to ErrAdminUserEmailTaken → 409). New accounts carry no credentials
// (provisioned out-of-band); admin-created users are typically active. Gated
// by `users.manage` (defense-in-depth). The creation is audited (NFR-O1) and
// the result carries the server-authoritative confirmation (MsgUserCreated).
func (s *Service) CreateAdminUser(ctx context.Context, actor *User, input CreateAdminUserInput) (*AdminUserWriteResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	vorname, nachname, email, state, roleIDs, groupIDs, grantCodes, err := validateAdminUserInput(input.Vorname, input.Nachname, input.Email, input.Status, input.RoleIDs, input.UserGroupIDs, input.DirectGrantCodes)
	if err != nil {
		return nil, err
	}

	user, err := s.repo.CreateAdminUser(ctx, email, vorname, nachname, state, roleIDs, groupIDs, grantCodes)
	if err != nil {
		if errors.Is(err, ErrAdminUserEmailTaken) {
			return nil, ErrAdminUserEmailTaken
		}
		if errors.Is(err, ErrAdminUserUnknownRole) {
			return nil, ErrAdminUserUnknownRole
		}
		if errors.Is(err, ErrAdminUserUnknownUserGroup) {
			return nil, ErrAdminUserUnknownUserGroup
		}
		if errors.Is(err, ErrUnknownPermissionCode) {
			return nil, ErrUnknownPermissionCode
		}
		return nil, fmt.Errorf("user core: failed to create admin user: %w", err)
	}

	// Best-effort audit (NFR-O1): a failed audit write is logged, never rolled
	// back into the creation. Detail is the target email — never sensitive.
	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserCreate, "target="+user.Email, AuditSeverityNormal); err != nil {
		s.log().Warn("admin user create audit write failed", "error", err)
	}
	s.log().Info("admin user created", "actor", actor.Email, "target", user.Email, "state", state)

	detail, err := s.repo.GetUserDetail(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to load created user detail: %w", err)
	}
	now := time.Now().UTC()
	for i := range detail.Qualifications {
		detail.Qualifications[i].Status = qualificationStatus(detail.Qualifications[i], now)
	}
	return &AdminUserWriteResult{Message: MsgUserCreated, User: detail}, nil
}

// UpdateAdminUser replaces a user's profile fields, state AND all three
// assignment sets (roles, user groups, direct grants) atomically
// (delete-then-insert in one transaction, Story 2.5 lesson). An unknown id
// maps to ErrAdminUserNotFound → 404; an email held by another account maps to
// ErrAdminUserEmailTaken → 409. Because permission resolution is live per
// request (AD-2/FR-21), the change reaches the user's resolved set on the very
// next request — verified by test, never cached. Gated by `users.manage`
// (defense-in-depth). The update is audited (NFR-O1) and the result carries
// the server-authoritative confirmation (MsgUserUpdated).
func (s *Service) UpdateAdminUser(ctx context.Context, actor *User, userID string, input UpdateAdminUserInput) (*AdminUserWriteResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	vorname, nachname, email, state, roleIDs, groupIDs, grantCodes, err := validateAdminUserInput(input.Vorname, input.Nachname, input.Email, input.Status, input.RoleIDs, input.UserGroupIDs, input.DirectGrantCodes)
	if err != nil {
		return nil, err
	}

	user, err := s.repo.UpdateAdminUser(ctx, userID, email, vorname, nachname, state, roleIDs, groupIDs, grantCodes)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		if errors.Is(err, ErrAdminUserEmailTaken) {
			return nil, ErrAdminUserEmailTaken
		}
		if errors.Is(err, ErrAdminUserUnknownRole) {
			return nil, ErrAdminUserUnknownRole
		}
		if errors.Is(err, ErrAdminUserUnknownUserGroup) {
			return nil, ErrAdminUserUnknownUserGroup
		}
		if errors.Is(err, ErrUnknownPermissionCode) {
			return nil, ErrUnknownPermissionCode
		}
		return nil, fmt.Errorf("user core: failed to update admin user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserUpdate, "target="+user.Email, AuditSeverityNormal); err != nil {
		s.log().Warn("admin user update audit write failed", "error", err)
	}
	s.log().Info("admin user updated", "actor", actor.Email, "target", user.Email)

	detail, err := s.repo.GetUserDetail(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to load updated user detail: %w", err)
	}
	now := time.Now().UTC()
	for i := range detail.Qualifications {
		detail.Qualifications[i].Status = qualificationStatus(detail.Qualifications[i], now)
	}
	return &AdminUserWriteResult{Message: MsgUserUpdated, User: detail}, nil
}

// DeactivateUser sets an ACTIVE user's state to `deactivated` (Story 2.6,
// FR-21): the user cannot authenticate at all ("→ Sofort kein Login") because
// session validation rejects non-active accounts immediately (Story 1.4) and
// the repository also revokes the user's sessions in the same transaction. The
// action requires client confirmation (ErrDeactivationNotConfirmed → 400
// without it) and is audited with the actor + target (NFR-O1). Only active
// users can be deactivated: an unknown id maps to ErrAdminUserNotFound → 404;
// a pending/deactivated user maps to ErrUserNotActiveForDeactivate → 409 (no
// existence leak, FR-19). Gated by `users.manage` (defense-in-depth).
func (s *Service) DeactivateUser(ctx context.Context, actor *User, userID string, confirmed bool) (*DeactivateUserResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	if !confirmed {
		return nil, ErrDeactivationNotConfirmed
	}
	// An admin must never be able to deactivate their OWN account and lock
	// themselves out of the module they manage (finding 7): the request maps to
	// a uniform 409 with a clear German message.
	if userID == actor.ID {
		return nil, ErrSelfDeactivation
	}

	user, err := s.repo.DeactivateUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		if errors.Is(err, ErrUserNotActiveForDeactivate) {
			return nil, ErrUserNotActiveForDeactivate
		}
		return nil, fmt.Errorf("user core: failed to deactivate user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserDeactivate, "target="+user.Email, AuditSeverityHigh); err != nil {
		s.log().Warn("user deactivate audit write failed", "error", err)
	}
	s.log().Info("user deactivated", "actor", actor.Email, "target", user.Email)

	return &DeactivateUserResult{Message: MsgUserDeactivated, UserID: user.ID, Email: user.Email}, nil
}

// ListUserGroups returns every organisational user group, ordered by name
// (Story 2.6, AD-12). Gated by `user_groups.manage` at the sub-mount and
// re-verified defense-in-depth in the core.
func (s *Service) ListUserGroups(ctx context.Context, actor *User) ([]*UserGroup, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	groups, err := s.repo.ListUserGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list user groups: %w", err)
	}
	return groups, nil
}

// CreateUserGroup creates an organisational user group (Story 2.6, AD-12). The
// name is unique case-insensitively (a duplicate maps to ErrUserGroupNameTaken
// → 409). Gated by `user_groups.manage` (defense-in-depth). The creation is
// audited (NFR-O1).
func (s *Service) CreateUserGroup(ctx context.Context, actor *User, input CreateUserGroupInput) (*UserGroup, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	name, description, err := validateUserGroupInput(input.Name, input.Description)
	if err != nil {
		return nil, err
	}

	group, err := s.repo.CreateUserGroup(ctx, name, description)
	if err != nil {
		if errors.Is(err, ErrUserGroupNameTaken) {
			return nil, ErrUserGroupNameTaken
		}
		return nil, fmt.Errorf("user core: failed to create user group: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserGroupCreate, "group="+group.Name, AuditSeverityNormal); err != nil {
		s.log().Warn("user group create audit write failed", "error", err)
	}
	s.log().Info("user group created", "actor", actor.Email, "group", group.Name)

	return group, nil
}

// AssignUserGroupMembers replaces the member set of an organisational user
// group atomically (delete-then-insert in one transaction, Story 2.6). An
// unknown group maps to ErrUserGroupNotFound → 404; an unknown member maps to
// ErrUserGroupMemberUnknown → 400. Membership grants NO permission (AD-12) —
// this only changes team composition, never access. Gated by
// `user_groups.manage` (defense-in-depth). The assignment is audited (NFR-O1).
func (s *Service) AssignUserGroupMembers(ctx context.Context, actor *User, groupID string, userIDs []string) (*UserGroup, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	// Deduplicate the member set before validation (finding 10): a client
	// sending duplicate user ids must not trip the existence-count check and
	// produce a spurious ErrUserGroupMemberUnknown.
	userIDs = dedupeStrings(userIDs)

	group, err := s.repo.AssignUserGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		if errors.Is(err, ErrUserGroupNotFound) {
			return nil, ErrUserGroupNotFound
		}
		if errors.Is(err, ErrUserGroupMemberUnknown) {
			return nil, ErrUserGroupMemberUnknown
		}
		return nil, fmt.Errorf("user core: failed to assign user group members: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserGroupAssign, fmt.Sprintf("group=%s members=%d", group.Name, len(userIDs)), AuditSeverityNormal); err != nil {
		s.log().Warn("user group assign audit write failed", "error", err)
	}
	s.log().Info("user group members assigned", "actor", actor.Email, "group", group.Name, "members", len(userIDs))

	return group, nil
}

// ListUserGroupMembers returns the current member user ids of an organisational
// user group, ordered by id (Story 2.6). Drives the group-member editor's
// pre-checked set; the assignment endpoint replaces this set atomically. An
// unknown group maps to ErrUserGroupNotFound → 404. Gated by
// `user_groups.manage` (defense-in-depth).
func (s *Service) ListUserGroupMembers(ctx context.Context, actor *User, groupID string) ([]string, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	members, err := s.repo.ListUserGroupMembers(ctx, groupID)
	if err != nil {
		if errors.Is(err, ErrUserGroupNotFound) {
			return nil, ErrUserGroupNotFound
		}
		return nil, fmt.Errorf("user core: failed to list user group members: %w", err)
	}
	return members, nil
}

// DeleteUserGroup removes an organisational user group (Story 2.6). Member rows
// cascade; membership grants NO permission (AD-12), so deleting a team never
// changes anyone's access. An unknown group maps to ErrUserGroupNotFound → 404.
// Gated by `user_groups.manage` (defense-in-depth). The deletion is audited
// (NFR-O1).
func (s *Service) DeleteUserGroup(ctx context.Context, actor *User, groupID string) error {
	if actor == nil {
		return ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return err
	}
	if err := s.repo.DeleteUserGroup(ctx, groupID); err != nil {
		if errors.Is(err, ErrUserGroupNotFound) {
			return ErrUserGroupNotFound
		}
		return fmt.Errorf("user core: failed to delete user group: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserGroupDelete, "group="+groupID, AuditSeverityNormal); err != nil {
		s.log().Warn("user group delete audit write failed", "error", err)
	}
	s.log().Info("user group deleted", "actor", actor.Email, "group", groupID)

	return nil
}

// validateAdminUserInput trims and validates the admin create/edit payload: it
// enforces the name length caps, the email syntax and the allowed statuses
// (active/pending_approval — deactivation is a separate confirmed flow), and
// deduplicates the assignment sets while preserving order. It returns the
// normalized fields. Validation errors are the uniform 400 sentinels.
func validateAdminUserInput(vorname, nachname, email, status string, roleIDs, groupIDs, grantCodes []string) (string, string, string, string, []string, []string, []string, error) {
	vorname = strings.TrimSpace(vorname)
	nachname = strings.TrimSpace(nachname)
	email = strings.ToLower(strings.TrimSpace(email))

	if vorname == "" || nachname == "" || len([]rune(vorname)) > UserNameMaxLength || len([]rune(nachname)) > UserNameMaxLength {
		return "", "", "", "", nil, nil, nil, ErrAdminUserInvalidName
	}
	if !isValidEmail(email) {
		return "", "", "", "", nil, nil, nil, ErrAdminUserInvalidEmail
	}
	switch status {
	case string(StateActive), string(StatePendingApproval):
	default:
		return "", "", "", "", nil, nil, nil, ErrAdminUserInvalidStatus
	}
	roleIDs = dedupeStrings(roleIDs)
	groupIDs = dedupeStrings(groupIDs)
	grantCodes = dedupeStrings(grantCodes)
	// Additive-only (FR-6/AD-12): a direct grant may only ever carry one of the
	// 21 base codes — an unknown code is a uniform 400 (nothing is stored).
	for _, c := range grantCodes {
		if !basePermissionSet[c] {
			return "", "", "", "", nil, nil, nil, ErrUnknownPermissionCode
		}
	}
	return vorname, nachname, email, status, roleIDs, groupIDs, grantCodes, nil
}

// validateUserGroupInput trims the user-group name/description and enforces
// the name length cap. An empty/too-long name maps to ErrUserGroupInvalidName
// (400 invalid_request, distinct from the 409 duplicate).
func validateUserGroupInput(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > UserGroupNameMaxLength {
		return "", "", ErrUserGroupInvalidName
	}
	return name, strings.TrimSpace(description), nil
}

// dedupeStrings removes duplicate entries while preserving first-occurrence
// order (used for the additive assignment sets).
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// qualificationStatus derives the display status of a qualification assignment
// from its expiry model (Story 2.6, FR-22/AD-7): 'unlimited' never expires;
// a 'fixed' assignment is valid while far from the date, expiring_soon within
// the 30-day window, and expired once the date is reached or passed (a
// qualification expiring exactly NOW counts as expired).
func qualificationStatus(a QualificationAssignment, now time.Time) string {
	switch a.ExpiryKind {
	case QualificationExpiryUnlimited:
		return QualificationStatusUnlimited
	case QualificationExpiryFixed:
		if a.ExpiresAt == nil {
			// Defensive: a 'fixed' qualification without a stored date has
			// nothing to expire against — treat as valid rather than crashing.
			return QualificationStatusValid
		}
		if !now.Before(*a.ExpiresAt) {
			return QualificationStatusExpired
		}
		if now.Add(qualificationExpiringSoonWindow).After(*a.ExpiresAt) {
			return QualificationStatusExpiringSoon
		}
		return QualificationStatusValid
	default:
		// Unknown expiry kind (data written out-of-band): treat as valid.
		return QualificationStatusValid
	}
}

// requireAnyUserPermission re-verifies (defense-in-depth) that the actor's
// LIVE permission set holds ANY of the `users.*` codes (users.view/users.approve/
// users.manage — the same codes the SPA nav uses for the Benutzer entry).
func (s *Service) requireAnyUserPermission(ctx context.Context, actor *User) error {
	return s.requireAnyPermission(ctx, actor, []string{UserViewPermission, UserApprovePermission, UserManagePermission})
}

// requireUserManagePermission guards create/edit/deactivate (defense-in-depth):
// the actor must hold `users.manage` — a view/approve-only holder may list and
// view detail but never create/edit/deactivate.
func (s *Service) requireUserManagePermission(ctx context.Context, actor *User) error {
	return s.requireAnyPermission(ctx, actor, []string{UserManagePermission})
}

// requireUserGroupsManagePermission guards the user-group endpoints
// (defense-in-depth): the actor must hold `user_groups.manage`.
func (s *Service) requireUserGroupsManagePermission(ctx context.Context, actor *User) error {
	return s.requireAnyPermission(ctx, actor, []string{UserGroupsManagePermission})
}

// requireAnyPermission is the shared any-of permission re-check used across the
// admin surfaces (approval, roles, users, user groups). It resolves the
// actor's LIVE permission set and requires AT LEAST ONE of the given codes.
func (s *Service) requireAnyPermission(ctx context.Context, actor *User, required []string) error {
	perms, err := s.repo.ListPermissionsByUser(ctx, actor.ID)
	if err != nil {
		return fmt.Errorf("user core: failed to resolve actor permissions: %w", err)
	}
	for _, want := range required {
		for _, p := range perms {
			if p == want {
				return nil
			}
		}
	}
	return ErrForbidden
}