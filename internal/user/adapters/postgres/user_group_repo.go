package postgres

import (
	"context"
	"time"

	"github.com/saskia-peters/gear/internal/user/core"
)

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

// ReplaceUserGroupMemberships replaces the organisational user-group set of a
// user (Effort 2, user-detail assignment): delete-then-insert in one
// transaction (Story 2.5 lesson — separate statements, never a data-modifying
// CTE). An unknown user maps to core.ErrAdminUserNotFound → 404; an unknown
// group id maps to core.ErrAdminUserUnknownUserGroup → 400. An empty set
// removes every membership.
func (r *Repository) ReplaceUserGroupMemberships(ctx context.Context, userID string, groupIDs []string) (*core.User, error) {
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

	exists, err := q.UserExists(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrAdminUserNotFound
	}

	groupUUIDs, err := resolveExistingUserGroupIDs(ctx, q, groupIDs)
	if err != nil {
		return nil, err
	}

	if err := q.DeleteUserGroupMemberships(ctx, uid); err != nil {
		return nil, err
	}
	if len(groupUUIDs) > 0 {
		if err := q.InsertUserGroupMemberships(ctx, InsertUserGroupMembershipsParams{UserID: uid, Column2: groupUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	row, err := r.queries.GetUserByID(ctx, uid)
	if err != nil {
		return nil, core.ErrAdminUserNotFound
	}
	return &core.User{
		ID:        uuidToString(row.ID.Bytes),
		Email:     row.Email,
		FirstName: row.FirstName,
		LastName:  row.LastName,
		State:     core.UserState(row.State),
	}, nil
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
