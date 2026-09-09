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

// ListUsers returns the users (id, names, email, state) ordered by name, for
// the admin "Benutzer" list surface (Story 2.6, Spec 2.9). An optional status
// filter (active/pending_approval/deactivated) narrows the set; an invalid
// status maps to ErrAdminUserInvalidStatus → 400. Each summary also carries
// the user-group (team) names the user belongs to (Effort 2) so the SPA table
// can render inline group tags without a per-row lookup. The caller is gated by
// ANY of the `users.*` codes at the sub-mount; here it is re-verified
// defense-in-depth (AD-2/AD-6).
func (s *Service) ListUsers(ctx context.Context, actor *User, status *string) ([]*AdminUserSummary, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireAnyUserPermission(ctx, actor); err != nil {
		return nil, err
	}
	var normalized *string
	if status != nil {
		st := strings.TrimSpace(*status)
		switch st {
		case string(StateActive), string(StatePendingApproval), string(StateDeactivated):
			normalized = &st
		default:
			return nil, ErrAdminUserInvalidStatus
		}
	}
	users, err := s.repo.ListUsers(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list users: %w", err)
	}
	// Attach the organisational user-group names (Effort 2): one lightweight
	// lookup for the whole page, not a query per row. Membership grants no
	// permission (AD-12) — this only feeds the inline tags.
	if len(users) > 0 {
		ids := make([]string, 0, len(users))
		for _, u := range users {
			if u != nil {
				ids = append(ids, u.ID)
			}
		}
		groupNames, err := s.repo.ListUserGroupNamesByUsers(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("user core: failed to load user group names: %w", err)
		}
		for _, u := range users {
			if u == nil {
				continue
			}
			names := groupNames[u.ID]
			if names == nil {
				// Never leave UserGroups nil: an ungrouped user serializes as an
				// empty array (not JSON null) so the SPA table can rely on it.
				names = []string{}
			}
			u.UserGroups = names
		}
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
	// Assemble the resolved-permission provenance (Spec 2.9): the repository
	// returns one row per source; group by code so each resolved permission
	// lists ALL of its sources (role names, user-group names, 'direct'),
	// preserving first-seen order and deduplicating duplicate source rows.
	detail.ResolvedPermissions = assembleProvenance(detail.ResolvedPermissions)
	return detail, nil
}

// assembleProvenance groups a flat provenance list (one row per source) by
// code, deduplicating duplicate source rows and preserving first-seen order.
// Each code maps to all its sources (multi-source supported, e.g.
// `tools.manage` from both a role and a team).
func assembleProvenance(rows []*PermissionProvenance) []*PermissionProvenance {
	type source struct {
		kind string
		name string
	}
	ordered := make([]string, 0, len(rows))
	byCode := make(map[string][]source, len(rows))
	seen := make(map[string]map[string]bool, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		key := r.Code + "\x00" + r.SourceKind + "\x00" + r.SourceName
		if seen[r.Code] == nil {
			seen[r.Code] = map[string]bool{}
		}
		if seen[r.Code][key] {
			continue
		}
		seen[r.Code][key] = true
		if _, ok := byCode[r.Code]; !ok {
			ordered = append(ordered, r.Code)
		}
		byCode[r.Code] = append(byCode[r.Code], source{kind: r.SourceKind, name: r.SourceName})
	}
	out := make([]*PermissionProvenance, 0, len(ordered))
	for _, code := range ordered {
		for _, s := range byCode[code] {
			out = append(out, &PermissionProvenance{Code: code, SourceKind: s.kind, SourceName: s.name})
		}
	}
	if out == nil {
		out = []*PermissionProvenance{}
	}
	return out
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