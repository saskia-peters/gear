package postgres

import (
	"context"
	"encoding/json"
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

// SoftDeleteAndArchive moves a user's personal data into the dsgvo_deleted_accounts
// archive AND flips the users row to the scrubbed `deleted` tombstone in ONE
// transaction (Story 3.4, FR-24/AD-8): (a) the full personal snapshot (email,
// names, attributes — the secret columns are NOT read) is copied into the
// archive with the deletion reason + deleting admin; (b) the user row is set
// state `deleted` and scrubbed (email → `deleted.<id>@deleted.local` freeing
// the UNIQUE key, password_hash → '', names/attributes → '', totp/otp/pending
// email/must-change cleared); (c) the email's login-attempt rows are deleted.
// NO hard delete at deletion time. An unknown id — or an already-deleted
// tombstone (the surface treats `deleted` as non-existent) — maps to
// core.ErrAdminUserNotFound (uniform 404); the `state <> 'deleted'` guard on
// the scrub is the TOCTOU backstop that rolls the archive insert back when a
// concurrent delete already flipped the row.
func (r *Repository) SoftDeleteAndArchive(ctx context.Context, targetUserID, reason, deletedBy string) (*core.User, error) {
	uid, err := uuidFromString(targetUserID)
	if err != nil {
		return nil, core.ErrAdminUserNotFound
	}
	actor, err := uuidFromString(deletedBy)
	if err != nil {
		return nil, err
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// (a) The archive snapshot source: the personal-data columns only (no
	// secrets), plus the live state so an already-deleted tombstone is rejected
	// BEFORE any archive write.
	snapshot, err := q.GetUserFullForArchive(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrAdminUserNotFound
		}
		return nil, err
	}
	if snapshot.State == string(core.StateDeleted) {
		return nil, core.ErrAdminUserNotFound
	}
	if _, err := q.InsertDeletedAccount(ctx, InsertDeletedAccountParams{
		OriginalUserID: uid,
		Email:          snapshot.Email,
		DisplayName:    snapshot.DisplayName,
		FirstName:      snapshot.FirstName,
		LastName:       snapshot.LastName,
		Attributes:     snapshot.Attributes,
		Reason:         reason,
		DeletedBy:      actor,
	}); err != nil {
		return nil, err
	}

	// (b) The tombstone flip + scrub. A zero-row update (unknown id or an
	// already-deleted row raced in) rolls the archive insert back and maps to
	// the uniform not-found.
	scrubbed, err := q.SetUserDeletedAndScrub(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrAdminUserNotFound
		}
		return nil, err
	}

	// (c) The scrubbed email is gone from the users row; the per-email
	// login-attempt state follows (the placeholder address is not a login
	// identity).
	if err := q.DeleteLoginAttemptsByEmail(ctx, snapshot.Email); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return userFromRow(scrubbed.ID, scrubbed.Email, scrubbed.DisplayName, scrubbed.FirstName, scrubbed.LastName,
		scrubbed.PasswordHash, scrubbed.State, scrubbed.IsMfaEnabled, scrubbed.MustChangePassword, scrubbed.TotpSecretEncrypted,
		scrubbed.PendingTotpSecretEncrypted, scrubbed.PendingTotpExpiresAt, scrubbed.Attributes, scrubbed.CreatedAt, scrubbed.UpdatedAt, scrubbed.PendingEmail, scrubbed.OneTimePasswordHash, scrubbed.OneTimePasswordExpiresAt)
}

// ListDeletedAccounts returns every archived (soft-deleted) account, newest
// first (deleted_at DESC, id DESC — the SQL ORDER BY guarantees it). A deleted
// history that is empty answers an empty list, nil-safe. No secret material is
// selected (the archive never held any — the snapshot excludes secrets).
func (r *Repository) ListDeletedAccounts(ctx context.Context) ([]*core.DeletedAccount, error) {
	rows, err := r.queries.ListDeletedAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*core.DeletedAccount, 0, len(rows))
	for _, row := range rows {
		deletedBy := ""
		if row.DeletedBy.Valid {
			deletedBy = uuidToString(row.DeletedBy.Bytes)
		}
		out = append(out, &core.DeletedAccount{
			ID:             uuidToString(row.ID.Bytes),
			OriginalUserID: uuidToString(row.OriginalUserID.Bytes),
			Email:          row.Email,
			DisplayName:    row.DisplayName,
			FirstName:      row.FirstName,
			LastName:       row.LastName,
			Attributes:     unmarshalArchiveAttributes(row.Attributes),
			Reason:         row.Reason,
			DeletedBy:      deletedBy,
			DeletedAt:      row.DeletedAt.Time,
		})
	}
	return out, nil
}

// PurgeDeletedAccount hard-deletes an archived account AND its (now-scrubbed)
// users tombstone in ONE transaction (Story 3.4, FR-24): this is the ONLY hard
// delete in the account lifecycle — admin-initiated on demand. The purge reads
// the archive row FIRST (inside the transaction) to resolve the original user
// id, then deletes the users tombstone (CASCADEs any remaining sessions/reset
// tokens/role memberships; the audit_log/admin_recovery references auto-SET-NULL
// — migrations 000006/000009) and finally the archive row — all-or-nothing. An
// unknown or already-purged archive id maps to core.ErrDeletedAccountNotFound
// (uniform 404).
func (r *Repository) PurgeDeletedAccount(ctx context.Context, archiveID string) error {
	aid, err := uuidFromString(archiveID)
	if err != nil {
		return core.ErrDeletedAccountNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)
	row, err := q.GetDeletedAccount(ctx, aid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.ErrDeletedAccountNotFound
		}
		return err
	}
	// Hard-delete the (scrubbed) users tombstone FIRST. The `state = 'deleted'`
	// guard + affected-row check mean a corrupted archive row pointing at a
	// NON-deleted account can NEVER hard-delete a live user — a zero-row delete
	// maps to the uniform 404.
	tombstoneAffected, err := q.DeleteUserByID(ctx, row.OriginalUserID)
	if err != nil {
		return err
	}
	if tombstoneAffected == 0 {
		return core.ErrDeletedAccountNotFound
	}
	// Then the archive row. A zero-row delete (a CONCURRENT purge removed it
	// between the GetDeletedAccount read and this delete) maps to the uniform
	// 404 — never a false success.
	archiveAffected, err := q.DeleteDeletedAccount(ctx, aid)
	if err != nil {
		return err
	}
	if archiveAffected == 0 {
		return core.ErrDeletedAccountNotFound
	}
	return tx.Commit(ctx)
}

// unmarshalArchiveAttributes parses an archived account's stored JSONB object
// into a map. A stored value that is not a JSON object (written out-of-band)
// surfaces as a clear error instead of being silently dropped (the Story 1.9
// boundary: reads never crash and never silently lose data).
func unmarshalArchiveAttributes(raw []byte) map[string]any {
	attrs := map[string]any{}
	if len(raw) == 0 {
		return attrs
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// The jsonb column rejects syntactically invalid JSON, so a "malformed"
		// value is a valid non-object shape written out-of-band.
		return map[string]any{"_unparseable": string(raw)}
	}
	if parsed != nil {
		attrs = parsed
	}
	return attrs
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
