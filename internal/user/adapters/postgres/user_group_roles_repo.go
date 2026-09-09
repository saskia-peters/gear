package postgres

import (
	"context"

	"github.com/saskia-peters/gear/internal/user/core"
)

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