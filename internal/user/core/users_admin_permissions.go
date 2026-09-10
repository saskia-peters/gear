package core

import (
	"context"
	"fmt"
)

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
	// UsersQualificationsManagePermission gates assigning/revoking a
	// qualification on a user AND editing a user's per-qualification
	// valid-until (Spec 2.9). Granted to fuehrende, schirrmeister and admin.
	UsersQualificationsManagePermission = "users.qualifications.manage"
	// UserAccountApprovePermission gates approving or denying user-account
	// / recovery actions (2026-09-09, retro finding F11): the admin-recovery
	// DENY surface must not be reachable by every admin-module-code holder.
	// Granted to the admin base role.
	UserAccountApprovePermission = "user.account.approve"
)

// AdminModuleAccessCodes returns the server-authoritative set of permission
// codes that open the ADMIN module (Spec 2.9 / Effort 2): the union of every
// admin sub-surface's gating codes — the same set the SPA nav model uses for
// `hasAnyAdminCode`. A caller holding ANY of these may enter the module (the
// per-surface sub-mounts then apply their own tighter gates). This replaces
// the old `admin.recovery.approve`-only outer gate so fuehrende/schirrmeister
// (who hold `users.view` + `users.qualifications.manage`) can reach the user
// directory and qualification assignment, while recovery stays admin-only.
// It returns a FRESH slice on every call so the value handed to the auth
// gateway can never be mutated by a caller (the slice is used directly as the
// authorization gate input).
func AdminModuleAccessCodes() []string {
	return []string{
		"users.view",
		"users.approve",
		"users.manage",
		"users.qualifications.manage",
		"user_groups.manage",
		"roles.create",
		"roles.edit",
		"roles.assign",
		"qualifications.manage",
		"tools.manage",
		"tool_types.manage",
		"admin.settings.email",
		"admin.settings.backup",
		"schedules.manage",
		"dsgvo.access_report",
		"dsgvo.delete",
		"admin.recovery.approve",
		"user.account.approve",
	}
}

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
	AuditOperationUserGroupRolesSet = "user_group.roles.assign"
)

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