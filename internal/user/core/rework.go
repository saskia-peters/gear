package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Admin Rework Effort 1 — Model & Backend (Spec 2.9). This file holds the
// domain methods that "glue" the isolated admin surfaces together:
//
//   - user-group ROLE assignment (ListUserGroupRoles / AssignUserGroupRoles):
//     an organisational user group can now hold permission groups (roles), so a
//     team member INHERITS those roles' permissions (three-way additive
//     resolution, AD-12). Gated by `user_groups.manage` (defense-in-depth).
//   - per-user qualification assignment with a per-assignment valid-until
//     (AssignUserQualification / RevokeUserQualification /
//     UpdateUserQualificationExpiry): a `fixed` qualification REQUIRES a
//     per-user expires_at at assignment; `unlimited` ones never expire. Gated
//     by `users.qualifications.manage` (defense-in-depth).
//
// Every change is live per request (FR-21/FR-22) — nothing here caches.
// Qualification and group-role writes are audited (NFR-O1).

// UserQualificationAssignResult is the confirmation returned by a per-user
// qualification assignment/revoke/expiry edit (Spec 2.9): the server-
// authoritative German message plus the target user's id so the client can
// scope feedback.
type UserQualificationAssignResult struct {
	Message          string `json:"message"`
	UserID           string `json:"user_id"`
	QualificationID  string `json:"qualification_id"`
}

// German microcopy for the user-group role and per-user qualification surfaces
// (Spec 2.9, UX-DR6/UX-DR8).
const (
	// MsgUserGroupRolesRequired is the uniform 400 message when the role_ids
	// field is missing from a group-role assignment (a nil set must not clear
	// every role — review finding 2.9).
	MsgUserGroupRolesRequired = "Bitte wähle mindestens eine Rolle für die Benutzergruppe aus."
)

// ListUserGroupRoles returns the permission groups (roles) an organisational
// user group grants its members (Spec 2.9), ordered by name. An unknown group
// maps to ErrUserGroupNotFound → 404. Gated by `user_groups.manage`
// (defense-in-depth).
func (s *Service) ListUserGroupRoles(ctx context.Context, actor *User, groupID string) ([]*RoleGroupRef, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	roles, err := s.repo.ListUserGroupRoles(ctx, groupID)
	if err != nil {
		if errors.Is(err, ErrUserGroupNotFound) {
			return nil, ErrUserGroupNotFound
		}
		return nil, fmt.Errorf("user core: failed to list user group roles: %w", err)
	}
	return roles, nil
}

// AssignUserGroupRoles REPLACES the role set of an organisational user group
// atomically (delete-then-insert in one transaction, Spec 2.9 — separate
// statements, never a data-modifying CTE, Story 2.5 lesson). An unknown group
// maps to ErrUserGroupNotFound → 404; an unknown role id maps to
// ErrAdminUserUnknownRole → 400. Because permission resolution is live per
// request (AD-2/FR-21), members inherit the group's roles on the very next
// request; removing a role revokes it from every member next request (nothing
// cached). Gated by `user_groups.manage` (defense-in-depth). The change is
// audited (NFR-O1).
func (s *Service) AssignUserGroupRoles(ctx context.Context, actor *User, groupID string, roleIDs []string) ([]*RoleGroupRef, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserGroupsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	roleIDs = dedupeStrings(roleIDs)

	roles, err := s.repo.ReplaceUserGroupRoles(ctx, groupID, roleIDs)
	if err != nil {
		if errors.Is(err, ErrUserGroupNotFound) {
			return nil, ErrUserGroupNotFound
		}
		if errors.Is(err, ErrAdminUserUnknownRole) {
			return nil, ErrAdminUserUnknownRole
		}
		return nil, fmt.Errorf("user core: failed to assign user group roles: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserGroupRolesSet, fmt.Sprintf("group=%s roles=%d", groupID, len(roleIDs)), AuditSeverityNormal); err != nil {
		s.log().Warn("user group roles assign audit write failed", "error", err)
	}
	s.log().Info("user group roles assigned", "actor", actor.Email, "group", groupID, "roles", len(roleIDs))

	return roles, nil
}

// AssignUserQualification assigns a qualification to a user (Spec 2.9) with an
// optional per-assignment valid-until. A `fixed` qualification REQUIRES a
// per-user expires_at (ErrQualificationExpiryRequired → 400); an `unlimited`
// one must NOT carry one and never expires. An unknown user maps to
// ErrAdminUserNotFound → 404; an unknown qualification maps to
// ErrQualificationNotFound → 404. Gated by `users.qualifications.manage`
// (defense-in-depth). The assignment is audited (NFR-O1).
func (s *Service) AssignUserQualification(ctx context.Context, actor *User, userID, qualificationID string, expiresAt *time.Time) (*UserQualificationAssignResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUsersQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	// Normalize: trim/validate a future date is NOT required here (an already
	// past date simply shows as expired), but the fixed-vs-unlimited rule is
	// enforced in the repository against the qualification's expiry_kind.
	if err := s.repo.AssignQualificationToUser(ctx, userID, qualificationID, expiresAt); err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		if errors.Is(err, ErrQualificationNotFound) {
			return nil, ErrQualificationNotFound
		}
		if errors.Is(err, ErrQualificationExpiryRequired) {
			return nil, ErrQualificationExpiryRequired
		}
		if errors.Is(err, ErrQualificationInvalidExpiresAt) {
			return nil, ErrQualificationInvalidExpiresAt
		}
		return nil, fmt.Errorf("user core: failed to assign qualification to user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationAssign, fmt.Sprintf("target=%s qualification=%s", userID, qualificationID), AuditSeverityNormal); err != nil {
		s.log().Warn("qualification assign audit write failed", "error", err)
	}
	s.log().Info("qualification assigned to user", "actor", actor.Email, "target", userID, "qualification", qualificationID)

	return &UserQualificationAssignResult{Message: MsgQualificationAssignedToUser, UserID: userID, QualificationID: qualificationID}, nil
}

// RevokeUserQualification revokes a qualification from a user (Spec 2.9). An
// unknown user maps to ErrAdminUserNotFound → 404; an unknown qualification
// maps to ErrQualificationNotFound → 404. Revocation is immediate (AD-7/FR-22)
// because resolution is live per request. Gated by
// `users.qualifications.manage` (defense-in-depth). The revocation is audited
// (NFR-O1).
func (s *Service) RevokeUserQualification(ctx context.Context, actor *User, userID, qualificationID string) (*UserQualificationAssignResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUsersQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	if err := s.repo.RevokeQualificationFromUser(ctx, userID, qualificationID); err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		if errors.Is(err, ErrQualificationNotFound) {
			return nil, ErrQualificationNotFound
		}
		if errors.Is(err, ErrQualificationAssignmentNotFound) {
			return nil, ErrQualificationAssignmentNotFound
		}
		return nil, fmt.Errorf("user core: failed to revoke qualification from user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationRevoke, fmt.Sprintf("target=%s qualification=%s", userID, qualificationID), AuditSeverityNormal); err != nil {
		s.log().Warn("qualification revoke audit write failed", "error", err)
	}
	s.log().Info("qualification revoked from user", "actor", actor.Email, "target", userID, "qualification", qualificationID)

	return &UserQualificationAssignResult{Message: MsgQualificationRevokedFromUser, UserID: userID, QualificationID: qualificationID}, nil
}

// UpdateUserQualificationExpiry edits a user's per-assignment valid-until
// (Spec 2.9): a nil expiresAt clears the override (reverting to the vocabulary
// expiry); a value overrides it. An unknown user/qualification pair (not
// assigned) maps to ErrQualificationAssignmentNotFound → 404. Gated by
// `users.qualifications.manage` (defense-in-depth). The change is audited
// (NFR-O1).
func (s *Service) UpdateUserQualificationExpiry(ctx context.Context, actor *User, userID, qualificationID string, expiresAt *time.Time) (*UserQualificationAssignResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUsersQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateUserQualificationExpiry(ctx, userID, qualificationID, expiresAt); err != nil {
		if errors.Is(err, ErrQualificationAssignmentNotFound) {
			return nil, ErrQualificationAssignmentNotFound
		}
		if errors.Is(err, ErrQualificationInvalidExpiresAt) {
			return nil, ErrQualificationInvalidExpiresAt
		}
		return nil, fmt.Errorf("user core: failed to update qualification valid-until: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationValidUntilUpd, fmt.Sprintf("target=%s qualification=%s", userID, qualificationID), AuditSeverityNormal); err != nil {
		s.log().Warn("qualification valid-until update audit write failed", "error", err)
	}
	s.log().Info("qualification valid-until updated", "actor", actor.Email, "target", userID, "qualification", qualificationID)

	return &UserQualificationAssignResult{Message: MsgQualificationValidUntilUpdated, UserID: userID, QualificationID: qualificationID}, nil
}

// requireUsersQualificationsManagePermission re-verifies (defense-in-depth)
// that the actor's LIVE permission set holds `users.qualifications.manage`
// (Spec 2.9): grants assign/revoke of a qualification on a user AND editing a
// user's per-qualification valid-until.
func (s *Service) requireUsersQualificationsManagePermission(ctx context.Context, actor *User) error {
	return s.requireAnyPermission(ctx, actor, []string{UsersQualificationsManagePermission})
}
