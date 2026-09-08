package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

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
// atomically. Membership of an organisational user group grants NO permission
// (AD-12) — the resolution query never joins user_groups, so nothing here
// changes access; only the permission-group/direct-grant writes do.
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
		// OVERRIDES the vocabulary expiry for the display status. The core's
		// qualificationStatus reads ExpiresAt first, then falls back to the
		// vocabulary expiry — so here we prefer the assignment override and
		// carry the vocabulary date as the fallback.
		effective := row.VocabExpiresAt
		if row.AssignedExpiresAt.Valid {
			effective = row.AssignedExpiresAt
		}
		if effective.Valid {
			t := effective.Time
			a.ExpiresAt = &t
		}
		detail.Qualifications = append(detail.Qualifications, a)
	}
	return detail, nil
}

// CreateAdminUser atomically creates a user AND its optional assignment sets
// (roles, user groups, direct grants) in ONE transaction (Story 2.6): the user
// row and the three assignment sets are all-or-nothing. The email is unique
// case-insensitively (a duplicate maps to core.ErrAdminUserEmailTaken → 409); a
// role/user-group id that does not exist maps to core.ErrAdminUserUnknownRole /
// ErrAdminUserUnknownUserGroup → 400; a direct-grant code outside the 21 base
// series maps to core.ErrUnknownPermissionCode → 400. The state is written
// verbatim (active or pending_approval) and the password hash stays empty
// (credentials are provisioned out-of-band).
func (r *Repository) CreateAdminUser(ctx context.Context, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error) {
	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	taken, err := q.UserEmailExists(ctx, email)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrAdminUserEmailTaken
	}

	roleUUIDs, err := resolveExistingPermissionGroupIDs(ctx, q, roleIDs)
	if err != nil {
		return nil, err
	}
	groupUUIDs, err := resolveExistingUserGroupIDs(ctx, q, userGroupIDs)
	if err != nil {
		return nil, err
	}
	grantUUIDs, err := resolvePermissionIDsByCodes(ctx, q, grantCodes)
	if err != nil {
		return nil, err
	}

	row, err := q.CreateAdminUser(ctx, CreateAdminUserParams{
		Email:       email,
		DisplayName: firstName + " " + lastName,
		FirstName:   firstName,
		LastName:    lastName,
		State:       state,
	})
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, core.ErrAdminUserEmailTaken
		}
		return nil, err
	}
	if len(roleUUIDs) > 0 {
		if err := q.InsertUserRoles(ctx, InsertUserRolesParams{UserID: row.ID, Column2: roleUUIDs}); err != nil {
			return nil, err
		}
	}
	if len(groupUUIDs) > 0 {
		if err := q.InsertUserGroupMemberships(ctx, InsertUserGroupMembershipsParams{UserID: row.ID, Column2: groupUUIDs}); err != nil {
			return nil, err
		}
	}
	if len(grantUUIDs) > 0 {
		if err := q.InsertUserDirectGrants(ctx, InsertUserDirectGrantsParams{UserID: row.ID, Column2: grantUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// UpdateAdminUser atomically replaces a user's profile fields, state AND all
// three assignment sets (roles, user groups, direct grants) in ONE transaction
// (Story 2.6): the delete-then-insert of each assignment set happens in the
// same transaction as the profile update, so a failed half-write never leaves a
// mixed assignment state. An unknown id maps to core.ErrAdminUserNotFound →
// 404 (existence checked FIRST, so it never answers 409 "email taken"); an
// email held by ANOTHER account maps to core.ErrAdminUserEmailTaken → 409; a
// nonexistent role/user-group id → 400; an out-of-series grant code → 400.
// Because permission resolution is live per request (AD-2/FR-21), the change
// reaches the user's resolved set on the very next request — nothing is cached.
func (r *Repository) UpdateAdminUser(ctx context.Context, userID, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*core.User, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, core.ErrAdminUserNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// Existence FIRST (review finding): an unknown id must always map to the
	// uniform 404 not-found — never a 409 "email taken", even when the requested
	// email is held by another user. The zero-row UpdateUserProfileAdmin below
	// remains the TOCTOU backstop for a concurrent delete.
	exists, err := q.UserExists(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrAdminUserNotFound
	}

	// Case-insensitive duplicate-email guard inside the transaction, EXCLUDING
	// the target itself (keeping the user's own email stays legal).
	taken, err := q.UserEmailExistsExcept(ctx, UserEmailExistsExceptParams{Lower: email, ID: uid})
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrAdminUserEmailTaken
	}

	roleUUIDs, err := resolveExistingPermissionGroupIDs(ctx, q, roleIDs)
	if err != nil {
		return nil, err
	}
	groupUUIDs, err := resolveExistingUserGroupIDs(ctx, q, userGroupIDs)
	if err != nil {
		return nil, err
	}
	grantUUIDs, err := resolvePermissionIDsByCodes(ctx, q, grantCodes)
	if err != nil {
		return nil, err
	}

	row, err := q.UpdateUserProfileAdmin(ctx, UpdateUserProfileAdminParams{
		ID:          uid,
		Email:       email,
		DisplayName: firstName + " " + lastName,
		FirstName:   firstName,
		LastName:    lastName,
		State:       state,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrAdminUserNotFound
		}
		if isPgUniqueViolation(err) {
			return nil, core.ErrAdminUserEmailTaken
		}
		return nil, err
	}

	// Replace each assignment set atomically: separate DELETE + INSERT
	// statements in the same transaction (Story 2.5 lesson — a data-modifying
	// CTE would silently drop rows under PostgreSQL's same-statement
	// unique-index behaviour).
	if err := q.DeleteUserRoles(ctx, row.ID); err != nil {
		return nil, err
	}
	if len(roleUUIDs) > 0 {
		if err := q.InsertUserRoles(ctx, InsertUserRolesParams{UserID: row.ID, Column2: roleUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := q.DeleteUserGroupMemberships(ctx, row.ID); err != nil {
		return nil, err
	}
	if len(groupUUIDs) > 0 {
		if err := q.InsertUserGroupMemberships(ctx, InsertUserGroupMembershipsParams{UserID: row.ID, Column2: groupUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := q.DeleteUserDirectGrants(ctx, row.ID); err != nil {
		return nil, err
	}
	if len(grantUUIDs) > 0 {
		if err := q.InsertUserDirectGrants(ctx, InsertUserDirectGrantsParams{UserID: row.ID, Column2: grantUUIDs}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// DeactivateUser flips an ACTIVE user to `deactivated` AND revokes the user's
// sessions in ONE transaction (Story 2.6, FR-21): the user cannot authenticate
// at all ("→ Sofort kein Login") — session validation rejects non-active
// accounts immediately (Story 1.4) and the session rows are deleted as a
// belt-and-suspenders measure. An unknown id maps to core.ErrAdminUserNotFound
// → 404; a pending/deactivated user maps to core.ErrUserNotActiveForDeactivate
// → 409 (no existence leak beyond what the admin already sees, FR-19).
func (r *Repository) DeactivateUser(ctx context.Context, userID string) (*core.User, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, core.ErrAdminUserNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// Existence FIRST so an unknown id maps to the uniform 404, never the 409
	// "not active" — same reasoning as UpdateAdminUser.
	exists, err := q.UserExists(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrAdminUserNotFound
	}

	row, err := q.SetUserState(ctx, SetUserStateParams{
		StateNew:     string(core.StateDeactivated),
		StateCurrent: string(core.StateActive),
		ID:           uid,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrUserNotActiveForDeactivate
		}
		return nil, err
	}
	// Sofort kein Login: also revoke every existing session in the same
	// transaction (belt-and-suspenders on top of the live state check).
	if err := q.DeleteSessionsByUser(ctx, uid); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// ListUserGroups returns every organisational user group (Story 2.6, AD-12),
// ordered by name.
func (r *Repository) ListUserGroups(ctx context.Context) ([]*core.UserGroup, error) {
	rows, err := r.queries.ListUserGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*core.UserGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.UserGroup{
			ID:          uuidToString(row.ID.Bytes),
			Name:        row.Name,
			Description: row.Description,
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return out, nil
}

// CreateUserGroup creates an organisational user group (Story 2.6, AD-12). The
// name is unique case-insensitively (a duplicate maps to
// core.ErrUserGroupNameTaken → 409). A duplicate-name insert is guarded inside
// the transaction via UserGroupNameExists, with the UNIQUE constraint as the
// belt-and-suspenders backstop.
func (r *Repository) CreateUserGroup(ctx context.Context, name, description string) (*core.UserGroup, error) {
	taken, err := r.queries.UserGroupNameExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrUserGroupNameTaken
	}
	row, err := r.queries.CreateUserGroup(ctx, CreateUserGroupParams{Name: name, Description: description})
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, core.ErrUserGroupNameTaken
		}
		return nil, err
	}
	return &core.UserGroup{
		ID:          uuidToString(row.ID.Bytes),
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}

// AssignUserGroupMembers replaces the member set of an organisational user
// group atomically (delete-then-insert in ONE transaction, Story 2.6). An
// unknown group maps to core.ErrUserGroupNotFound → 404 (checked FIRST); an
// unknown member maps to core.ErrUserGroupMemberUnknown → 400. Membership
// grants NO permission (AD-12) — this only changes team composition, never
// access. The updated group is returned so the caller can echo it.
func (r *Repository) AssignUserGroupMembers(ctx context.Context, groupID string, userIDs []string) (*core.UserGroup, error) {
	gid, err := uuidFromString(groupID)
	if err != nil {
		return nil, core.ErrUserGroupNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	exists, err := q.UserGroupExists(ctx, gid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrUserGroupNotFound
	}

	memberUUIDs, err := resolveExistingUserIDs(ctx, q, userIDs)
	if err != nil {
		return nil, err
	}

	if err := q.DeleteGroupMembers(ctx, gid); err != nil {
		return nil, err
	}
	if len(memberUUIDs) > 0 {
		if err := q.InsertGroupMembers(ctx, InsertGroupMembersParams{UserGroupID: gid, Column2: memberUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Re-fetch the group so the caller gets the current row (the commit already
	// happened; a single-row read by name is over the base pool).
	group := &core.UserGroup{ID: uuidToString(gid.Bytes), Name: "", Description: "", CreatedAt: time.Time{}}
	rows, err := r.queries.ListUserGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ID == gid {
			group.Name = row.Name
			group.Description = row.Description
			group.CreatedAt = row.CreatedAt.Time
			return group, nil
		}
	}
	// The group existed at transaction time; if the concurrent delete removed
	// it, report the uniform not-found.
	return nil, core.ErrUserGroupNotFound
}

// ListUserGroupMembers returns the current member user ids of an organisational
// user group, ordered by id (Story 2.6). An unknown group maps to
// core.ErrUserGroupNotFound → 404 (checked first, uniform).
func (r *Repository) ListUserGroupMembers(ctx context.Context, groupID string) ([]string, error) {
	gid, err := uuidFromString(groupID)
	if err != nil {
		return nil, core.ErrUserGroupNotFound
	}
	exists, err := r.queries.UserGroupExists(ctx, gid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrUserGroupNotFound
	}
	rows, err := r.queries.ListUserGroupMemberIDs(ctx, gid)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, id := range rows {
		out = append(out, uuidToString(id.Bytes))
	}
	return out, nil
}

// ListUserGroupRoles returns the permission groups (roles) an organisational
// user group grants its members (Spec 2.9, AD-12): id, name and is_base_role,
// ordered by name. An unknown group maps to core.ErrUserGroupNotFound → 404.
func (r *Repository) ListUserGroupRoles(ctx context.Context, groupID string) ([]*core.RoleGroupRef, error) {
	gid, err := uuidFromString(groupID)
	if err != nil {
		return nil, core.ErrUserGroupNotFound
	}
	exists, err := r.queries.UserGroupExists(ctx, gid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrUserGroupNotFound
	}
	rows, err := r.queries.ListUserGroupRoles(ctx, gid)
	if err != nil {
		return nil, err
	}
	out := make([]*core.RoleGroupRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.RoleGroupRef{
			ID:         uuidToString(row.ID.Bytes),
			Name:       row.Name,
			IsBaseRole: row.IsBaseRole,
		})
	}
	return out, nil
}

// ReplaceUserGroupRoles REPLACES the role set of an organisational user group
// atomically (delete-then-insert in ONE transaction, Spec 2.9 — separate
// statements, never a data-modifying CTE, Story 2.5 lesson). An unknown group
// maps to core.ErrUserGroupNotFound → 404 (checked FIRST); an unknown role id
// maps to core.ErrAdminUserUnknownRole → 400. Because permission resolution is
// live per request (AD-2/FR-21), a member inherits the group's roles on the
// very next request; removing a role revokes it from every member next request
// (nothing cached). The updated role set is returned so the caller can echo it.
func (r *Repository) ReplaceUserGroupRoles(ctx context.Context, groupID string, roleIDs []string) ([]*core.RoleGroupRef, error) {
	gid, err := uuidFromString(groupID)
	if err != nil {
		return nil, core.ErrUserGroupNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	exists, err := q.UserGroupExists(ctx, gid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrUserGroupNotFound
	}

	roleUUIDs, err := resolveExistingPermissionGroupIDs(ctx, q, roleIDs)
	if err != nil {
		return nil, err
	}

	if err := q.DeleteUserGroupRoles(ctx, gid); err != nil {
		return nil, err
	}
	if len(roleUUIDs) > 0 {
		if err := q.InsertUserGroupRoles(ctx, InsertUserGroupRolesParams{UserGroupID: gid, Column2: roleUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Re-fetch the role set so the caller gets the committed rows.
	rows, err := r.queries.ListUserGroupRoles(ctx, gid)
	if err != nil {
		return nil, err
	}
	out := make([]*core.RoleGroupRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.RoleGroupRef{
			ID:         uuidToString(row.ID.Bytes),
			Name:       row.Name,
			IsBaseRole: row.IsBaseRole,
		})
	}
	return out, nil
}

// DeleteUserGroup removes an organisational user group (Story 2.6). Member rows
// cascade (ON DELETE CASCADE). An unknown group maps to core.ErrUserGroupNotFound
// → 404. Membership grants no permission by itself (AD-12), but a team can hold
// roles via `user_group_permission_groups` (Spec 2.9); deleting the team
// cascades those away, so members immediately lose any access inherited
// through it (live revocation, FR-21).
func (r *Repository) DeleteUserGroup(ctx context.Context, groupID string) error {
	gid, err := uuidFromString(groupID)
	if err != nil {
		return core.ErrUserGroupNotFound
	}
	exists, err := r.queries.UserGroupExists(ctx, gid)
	if err != nil {
		return err
	}
	if !exists {
		return core.ErrUserGroupNotFound
	}
	return r.queries.DeleteUserGroup(ctx, gid)
}

// resolveExistingPermissionGroupIDs validates that every requested role id
// exists and returns the id set as pgtype.UUID. A missing id maps to
// core.ErrAdminUserUnknownRole (400). An empty input yields an empty set.
func resolveExistingPermissionGroupIDs(ctx context.Context, q *Queries, ids []string) ([]pgtype.UUID, error) {
	uuids, err := uuidSlice(ids)
	if err != nil {
		return nil, core.ErrAdminUserUnknownRole
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	found, err := q.PermissionGroupsExistByIDs(ctx, uuids)
	if err != nil {
		return nil, err
	}
	if len(found) != len(uuids) {
		return nil, core.ErrAdminUserUnknownRole
	}
	return uuids, nil
}

// resolveExistingUserGroupIDs validates that every requested user-group id
// exists and returns the id set as pgtype.UUID. A missing id maps to
// core.ErrAdminUserUnknownUserGroup (400). An empty input yields an empty set.
func resolveExistingUserGroupIDs(ctx context.Context, q *Queries, ids []string) ([]pgtype.UUID, error) {
	uuids, err := uuidSlice(ids)
	if err != nil {
		return nil, core.ErrAdminUserUnknownUserGroup
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	found, err := q.UserGroupsExistByIDs(ctx, uuids)
	if err != nil {
		return nil, err
	}
	if len(found) != len(uuids) {
		return nil, core.ErrAdminUserUnknownUserGroup
	}
	return uuids, nil
}

// resolveExistingUserIDs validates that every requested member user id exists
// and returns the id set as pgtype.UUID. A missing id maps to
// core.ErrUserGroupMemberUnknown (400). An empty input yields an empty set.
func resolveExistingUserIDs(ctx context.Context, q *Queries, ids []string) ([]pgtype.UUID, error) {
	uuids, err := uuidSlice(ids)
	if err != nil {
		return nil, core.ErrUserGroupMemberUnknown
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	found, err := q.UsersExistByIDs(ctx, uuids)
	if err != nil {
		return nil, err
	}
	if len(found) != len(uuids) {
		return nil, core.ErrUserGroupMemberUnknown
	}
	return uuids, nil
}

// resolvePermissionIDsByCodes resolves direct-grant permission codes → row ids.
// A code with no row maps to core.ErrUnknownPermissionCode (400); the caller
// (core) has already verified every code is one of the 21 base codes, so a
// mismatch can only mean an out-of-band drift (belt-and-suspenders). An empty
// input yields an empty set.
func resolvePermissionIDsByCodes(ctx context.Context, q *Queries, codes []string) ([]pgtype.UUID, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	ids, err := q.ListGroupPermissionIdsByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if len(ids) != len(codes) {
		return nil, core.ErrUnknownPermissionCode
	}
	return ids, nil
}

// uuidSlice parses a list of canonical UUID strings into pgtype.UUID values. A
// malformed id returns an error (the caller maps it to its 400 sentinel).
func uuidSlice(ids []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		u, err := uuidFromString(id)
		if err != nil {
			return nil, fmt.Errorf("user postgres: malformed uuid: %w", err)
		}
		out = append(out, u)
	}
	return out, nil
}