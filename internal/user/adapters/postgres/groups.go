package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/user/core"
)

// Role & Permission-Group persistence (Story 2.5, AD-12). ListGroups returns
// every permission group (base roles first, then name) each with its granted
// codes; CreateGroup inserts a named group (is_base_role=false) AND its
// permission rows atomically; UpdateGroup replaces the group's name/description
// AND its permission set atomically (delete-then-insert in one transaction);
// ListAllPermissions returns the server-authoritative 21-code catalog.
//
// Kept in its own file so repository.go does not grow into a god-class
// (standing convention).

// ListGroups returns every permission group with its granted permission codes,
// base-roles-first then by name (Story 2.5). No secret material is selected.
// The permission sets are loaded in ONE grouped query (no per-group N+1).
func (r *Repository) ListGroups(ctx context.Context) ([]*core.RoleGroup, error) {
	rows, err := r.queries.ListPermissionGroups(ctx)
	if err != nil {
		return nil, err
	}
	permRows, err := r.queries.ListGroupPermissionsByGroupIDs(ctx)
	if err != nil {
		return nil, err
	}
	// Assemble every group's codes in a single pass, ordered by code within each
	// group (the grouped query orders by permission_group_id, then code).
	byGroup := make(map[pgtype.UUID][]string, len(rows))
	for _, p := range permRows {
		byGroup[p.PermissionGroupID] = append(byGroup[p.PermissionGroupID], p.Code)
	}
	out := make([]*core.RoleGroup, 0, len(rows))
	for _, row := range rows {
		perms := byGroup[row.ID]
		if perms == nil {
			perms = []string{}
		}
		out = append(out, &core.RoleGroup{
			ID:          uuidToString(row.ID.Bytes),
			Name:        row.Name,
			Description: row.Description,
			IsBaseRole:  row.IsBaseRole,
			Permissions: perms,
		})
	}
	return out, nil
}

// groupPermissions builds a RoleGroup from a group row, loading its granted
// codes (Story 2.5).
func (r *Repository) groupPermissions(ctx context.Context, id pgtype.UUID, name, description string, isBaseRole bool) (*core.RoleGroup, error) {
	codes, err := r.queries.ListPermissionsByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	perms := make([]string, 0, len(codes))
	for _, c := range codes {
		perms = append(perms, c.Code)
	}
	return &core.RoleGroup{
		ID:          uuidToString(id.Bytes),
		Name:        name,
		Description: description,
		IsBaseRole:  isBaseRole,
		Permissions: perms,
	}, nil
}

// CreateGroup atomically creates a named permission group (is_base_role=false)
// and its permission rows in ONE transaction (Story 2.5, AD-12): the group row
// and the membership rows are all-or-nothing, so a failed half-write never
// leaves a group with a partial permission set. The name is unique
// case-insensitively (a duplicate maps to core.ErrRoleNameTaken → 409); an
// unknown permission code maps to core.ErrUnknownPermissionCode → 400.
func (r *Repository) CreateGroup(ctx context.Context, name, description string, permissionCodes []string) (*core.RoleGroup, error) {
	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// Case-insensitive duplicate-name guard inside the transaction (Story 2.5).
	taken, err := q.PermissionGroupNameExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrRoleNameTaken
	}

	permIDs, err := q.ListGroupPermissionIdsByCodes(ctx, permissionCodes)
	if err != nil {
		return nil, err
	}
	// Additive-only (FR-6): a requested code with no row is rejected — a group
	// may only ever grant codes that exist in the 21-code base series. The
	// count comparison catches any unknown code (an empty input is valid).
	if len(permIDs) != len(permissionCodes) {
		return nil, core.ErrUnknownPermissionCode
	}

	row, err := q.CreatePermissionGroup(ctx, CreatePermissionGroupParams{
		Name:        name,
		Description: description,
	})
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, core.ErrRoleNameTaken
		}
		return nil, err
	}
	if err := q.InsertGroupPermissions(ctx, InsertGroupPermissionsParams{
		PermissionGroupID: row.ID,
		Column2:           permIDs,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.groupPermissions(ctx, row.ID, row.Name, row.Description, row.IsBaseRole)
}

// UpdateGroup atomically replaces a permission group's name/description AND its
// permission set in ONE transaction (Story 2.5, AD-12): the delete-then-insert
// of the membership rows happens in the same transaction as the group-row
// update, so a failed half-write never leaves a group with a partial permission
// set. Base roles are editable (they remain the named matrix starting point).
// An unknown id maps to core.ErrRoleNotFound → 404; renaming onto a taken name
// maps to core.ErrRoleNameTaken → 409. Because permission resolution is live
// per request (AD-2/FR-6), the change reaches every affected user on the next
// request — nothing is cached here.
func (r *Repository) UpdateGroup(ctx context.Context, id, name, description string, permissionCodes []string) (*core.RoleGroup, error) {
	uid, err := uuidFromString(id)
	if err != nil {
		return nil, core.ErrRoleNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// Existence FIRST (review finding): an unknown id must always map to the
	// uniform 404 not-found — never a 409 "name taken", even when the requested
	// name is held by another group. The zero-row UpdatePermissionGroup below
	// remains the TOCTOU backstop for a concurrent delete.
	exists, err := q.PermissionGroupExists(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrRoleNotFound
	}

	// Case-insensitive duplicate-name guard inside the transaction, EXCLUDING
	// the target itself (renaming a group to its own name stays legal).
	taken, err := q.PermissionGroupNameExistsExcept(ctx, PermissionGroupNameExistsExceptParams{
		Lower: name,
		ID:    uid,
	})
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrRoleNameTaken
	}

	permIDs, err := q.ListGroupPermissionIdsByCodes(ctx, permissionCodes)
	if err != nil {
		return nil, err
	}
	if len(permIDs) != len(permissionCodes) {
		return nil, core.ErrUnknownPermissionCode
	}

	row, err := q.UpdatePermissionGroup(ctx, UpdatePermissionGroupParams{
		ID:          uid,
		Name:        name,
		Description: description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrRoleNotFound
		}
		if isPgUniqueViolation(err) {
			return nil, core.ErrRoleNameTaken
		}
		return nil, err
	}
	if err := q.DeleteGroupPermissions(ctx, row.ID); err != nil {
		return nil, err
	}
	if err := q.InsertGroupPermissions(ctx, InsertGroupPermissionsParams{
		PermissionGroupID: row.ID,
		Column2:           permIDs,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.groupPermissions(ctx, row.ID, row.Name, row.Description, row.IsBaseRole)
}

// ListAllPermissions returns the server-authoritative permission catalog
// (Story 2.5): every base code with its stored description as the raw label.
// The core derives the German display label from the code and falls back to
// this description only when a code has no label.
func (r *Repository) ListAllPermissions(ctx context.Context) ([]*core.PermissionCatalogEntry, error) {
	rows, err := r.queries.ListAllPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*core.PermissionCatalogEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.PermissionCatalogEntry{Code: row.Code, Label: row.Description})
	}
	return out, nil
}