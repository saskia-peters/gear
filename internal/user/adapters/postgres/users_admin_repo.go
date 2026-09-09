package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/user/core"
)

// User & Group Administration persistence (Story 2.6, AD-12). ListUsers returns
// every user (id, names, email, state) ordered by name; GetUserDetail composes
// a user's profile + roles (permission groups) + user groups (teams) + direct
// grants + qualification assignments; CreateAdminUser/UpdateAdminUser persist
// the profile AND the three assignment sets atomically (delete-then-insert in
// one transaction, never a data-modifying CTE — Story 2.5 lesson);
// DeactivateUser flips an active user to deactivated and revokes their
// sessions in the same transaction; ListUserGroups/CreateUserGroup manage the
// organisational teams; AssignUserGroupMembers replaces a group's member set
// atomically. Bare membership of an organisational user group grants NO
// permission (AD-12) — access changes only through the permission-group/direct-
// grant writes and the roles a team holds (Spec 2.9 three-way resolution).
//
// Kept in its own file so repository.go does not grow into a god-class
// (standing convention).

// ListUsers returns the users matching the optional status filter (id, names,
// email, state) ordered by last name then first name (Story 2.6, Spec 2.9
// status filter). A nil status returns every user. No secret material is
// selected.
func (r *Repository) ListUsers(ctx context.Context, status *string) ([]*core.AdminUserSummary, error) {
	var statusParam pgtype.Text
	if status != nil {
		statusParam = pgtype.Text{String: *status, Valid: true}
	}
	rows, err := r.queries.ListUsers(ctx, statusParam)
	if err != nil {
		return nil, err
	}
	out := make([]*core.AdminUserSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.AdminUserSummary{
			ID:       uuidToString(row.ID.Bytes),
			Vorname:  row.FirstName,
			Nachname: row.LastName,
			Email:    row.Email,
			Status:   row.State,
		})
	}
	return out, nil
}

// ListUserGroupNamesByUsers returns the organisational user-group (team) names
// each listed user belongs to, keyed by user id (Effort 2): one query for the
// whole admin "Benutzer" list, so the SPA table renders inline group tags
// without a per-row lookup (no N+1). Names are ordered by name within each
// user. An empty input yields an empty map. Membership grants NO permission
// (AD-12) — this is display data only.
func (r *Repository) ListUserGroupNamesByUsers(ctx context.Context, userIDs []string) (map[string][]string, error) {
	if len(userIDs) == 0 {
		return map[string][]string{}, nil
	}
	uuids, err := uuidSlice(userIDs)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListUserGroupNamesByUsers(ctx, uuids)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(userIDs))
	for _, row := range rows {
		uid := uuidToString(row.UserID.Bytes)
		out[uid] = append(out[uid], row.Name)
	}
	return out, nil
}

// GetUserDetail composes a user's full admin detail — profile, roles
// (permission groups), user groups (teams), direct grants and qualification
// assignments (Story 2.6). An unknown id maps to core.ErrAdminUserNotFound.
// No secret material is selected; the qualification status is computed by the
// core (display only, Story 2.6).
func (r *Repository) GetUserDetail(ctx context.Context, userID string) (*core.AdminUserDetail, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, core.ErrAdminUserNotFound
	}
	profile, err := r.queries.GetUserByID(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrAdminUserNotFound
		}
		return nil, err
	}

	roles, err := r.queries.ListUserRoles(ctx, uid)
	if err != nil {
		return nil, err
	}
	groups, err := r.queries.ListUserGroupMemberships(ctx, uid)
	if err != nil {
		return nil, err
	}
	grants, err := r.queries.ListUserDirectGrants(ctx, uid)
	if err != nil {
		return nil, err
	}
	quals, err := r.queries.ListUserQualifications(ctx, uid)
	if err != nil {
		return nil, err
	}
	// Resolved-permission provenance (Spec 2.9): every resolved code annotated
	// with its source(s) — role name, user-group name, or 'direct'.
	provRows, err := r.queries.ListResolvedPermissionSources(ctx, uid)
	if err != nil {
		return nil, err
	}
	provenance := make([]*core.PermissionProvenance, 0, len(provRows))
	for _, row := range provRows {
		provenance = append(provenance, &core.PermissionProvenance{
			Code:       row.Code,
			SourceKind: row.SourceKind,
			SourceName: row.SourceName,
		})
	}

	detail := &core.AdminUserDetail{
		ID:       uuidToString(profile.ID.Bytes),
		Vorname:  profile.FirstName,
		Nachname: profile.LastName,
		Email:    profile.Email,
		Status:   profile.State,
		Roles:    make([]core.RoleGroupRef, 0, len(roles)),
		UserGroups: make([]core.UserGroupRef, 0, len(groups)),
		DirectGrants: make([]core.DirectGrantRef, 0, len(grants)),
		Qualifications: make([]core.QualificationAssignment, 0, len(quals)),
		ResolvedPermissions: provenance,
	}
	for _, row := range roles {
		detail.Roles = append(detail.Roles, core.RoleGroupRef{ID: uuidToString(row.ID.Bytes), Name: row.Name, IsBaseRole: row.IsBaseRole})
	}
	for _, row := range groups {
		detail.UserGroups = append(detail.UserGroups, core.UserGroupRef{ID: uuidToString(row.ID.Bytes), Name: row.Name})
	}
	for _, row := range grants {
		detail.DirectGrants = append(detail.DirectGrants, core.DirectGrantRef{PermissionID: uuidToString(row.ID.Bytes), Code: row.Code, GrantedAt: row.GrantedAt.Time})
	}
	for _, row := range quals {
		a := core.QualificationAssignment{
			ID:          uuidToString(row.ID.Bytes),
			Name:        row.Name,
			Description: row.Description,
			ExpiryKind:  row.ExpiryKind,
			AssignedAt:  row.AssignedAt.Time,
		}
		// Per-assignment valid-until (Spec 2.9): the per-assignment expires_at
		// IS the only valid-until (the vocabulary has no date, 2026-09-08
		// rework). NULL = an unlimited assignment, never expires. The core's
		// qualificationStatus derives the display status from ExpiryKind +
		// ExpiresAt.
		if row.AssignedExpiresAt.Valid {
			t := row.AssignedExpiresAt.Time
			a.ExpiresAt = &t
		}
		detail.Qualifications = append(detail.Qualifications, a)
	}
	return detail, nil
}
