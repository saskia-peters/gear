package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/saskia-peters/gear/internal/user/core"
)

// CreateAdminUser atomically creates a user AND its optional assignment sets
// (roles, user groups, direct grants) in ONE transaction (Story 2.6): the user
// row and the three assignment sets are all-or-nothing. The email is unique
// case-insensitively (a duplicate maps to core.ErrAdminUserEmailTaken → 409); a
// role/user-group id that does not exist maps to core.ErrAdminUserUnknownRole /
// ErrAdminUserUnknownUserGroup → 400; a direct-grant code outside the 22 base
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
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail, row.OneTimePasswordHash, row.OneTimePasswordExpiresAt)
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
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail, row.OneTimePasswordHash, row.OneTimePasswordExpiresAt)
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
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail, row.OneTimePasswordHash, row.OneTimePasswordExpiresAt)
}
