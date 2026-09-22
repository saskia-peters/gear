package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

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
// AssignUserGroups replaces the organisational user-group set of a user from
// the USER detail (Effort 2): delete-then-insert, atomic, so the user's group
// memberships match the checkboxes exactly. An unknown user maps to
// ErrAdminUserNotFound → 404; an unknown group id maps to
// ErrAdminUserUnknownUserGroup → 400. Gated by `user_groups.manage`
// (defense-in-depth). Audited (NFR-O1). Membership grants no permission by
// itself (AD-12), but a team may hold roles (Spec 2.9) whose permissions the
// user then inherits — so the change can affect access on the next request.
func (s *Service) AssignUserGroups(ctx context.Context, actor *User, userID string, groupIDs []string) (*AdminUserWriteResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	groupIDs = dedupeStrings(groupIDs)

	user, err := s.repo.ReplaceUserGroupMemberships(ctx, userID, groupIDs)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		if errors.Is(err, ErrAdminUserUnknownUserGroup) {
			return nil, ErrAdminUserUnknownUserGroup
		}
		return nil, fmt.Errorf("user core: failed to replace user group memberships: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserGroupAssign, fmt.Sprintf("target=%s groups=%d", user.Email, len(groupIDs)), AuditSeverityNormal); err != nil {
		s.log().Warn("user group memberships assign audit write failed", "error", err)
	}
	s.log().Info("user group memberships assigned", "actor", actor.Email, "target", user.Email, "groups", len(groupIDs))

	detail, err := s.GetUserDetail(ctx, actor, user.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to reload user detail after group change: %w", err)
	}
	return &AdminUserWriteResult{Message: MsgUserGroupsUpdated, User: detail}, nil
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
	// 23 base codes — an unknown code is a uniform 400 (nothing is stored).
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