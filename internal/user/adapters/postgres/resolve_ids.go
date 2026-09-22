package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/user/core"
)

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
// (core) has already verified every code is one of the 23 base codes, so a
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