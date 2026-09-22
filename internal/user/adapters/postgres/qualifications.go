package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/user/core"
)

// Qualification Management persistence (Story 2.7, AD-6/FR-19/FR-22/AD-7).
// ListQualificationVocabulary returns the full vocabulary (reusing the Story
// 2.6 ListQualifications query); CreateQualification/UpdateQualification
// persist the vocabulary rows; ListQualificationAssignees / ReplaceQualificationAssignees
// manage the per-qualification assignee set (delete-then-insert in one
// transaction — Story 2.5 lesson, never a data-modifying CTE).
//
// Kept in its own file so repository.go does not grow into a god-class
// (standing convention).

// ListQualificationVocabulary returns every qualification (id, name,
// description, expiry model), ordered by name (Story 2.7). The status
// indicator is derived by the core from the expiry model and now — this
// adapter only returns the stored row.
func (r *Repository) ListQualificationVocabulary(ctx context.Context) ([]*core.Qualification, error) {
	rows, err := r.queries.ListQualifications(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*core.Qualification, 0, len(rows))
	for _, row := range rows {
		out = append(out, qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind))
	}
	return out, nil
}

// CreateQualification creates a qualification vocabulary row (Story 2.7). The
// name is unique case-insensitively (a duplicate maps to
// core.ErrQualificationNameTaken → 409). The expiry model is only the
// expiry_kind (unlimited/fixed) — a qualification itself has NO valid-until
// date (2026-09-08 rework); per-user valid-until lives on assignments.
func (r *Repository) CreateQualification(ctx context.Context, name, description, expiryKind string) (*core.Qualification, error) {
	taken, err := r.queries.QualificationNameExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrQualificationNameTaken
	}
	row, err := r.queries.CreateQualification(ctx, CreateQualificationParams{
		Name:        name,
		Description: description,
		ExpiryKind:  expiryKind,
	})
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, core.ErrQualificationNameTaken
		}
		return nil, err
	}
	return qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind), nil
}

// UpdateQualification replaces a qualification's name/description/expiry_kind
// atomically (Story 2.7). Editing the expiry model never rewrites existing
// assignments — each assignment keeps its per-user valid-until (Spec 2.9). An
// unknown id maps to core.ErrQualificationNotFound → 404 (checked FIRST, so it
// never answers 409 "name taken"); renaming onto a name held by ANOTHER
// qualification maps to core.ErrQualificationNameTaken → 409.
func (r *Repository) UpdateQualification(ctx context.Context, id, name, description, expiryKind string) (*core.Qualification, error) {
	uid, err := uuidFromString(id)
	if err != nil {
		return nil, core.ErrQualificationNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	// Existence FIRST (review finding, Story 2.5 lesson): an unknown id must
	// always map to the uniform 404 not-found — never a 409 "name taken", even
	// when the requested name is held by another qualification. The zero-row
	// UpdateQualification below remains the TOCTOU backstop for a concurrent
	// delete.
	exists, err := q.QualificationExists(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrQualificationNotFound
	}

	// Case-insensitive duplicate-name guard inside the transaction, EXCLUDING
	// the target itself (renaming a qualification to its own name stays legal).
	taken, err := q.QualificationNameExistsExcept(ctx, QualificationNameExistsExceptParams{Lower: name, ID: uid})
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, core.ErrQualificationNameTaken
	}

	row, err := q.UpdateQualification(ctx, UpdateQualificationParams{
		ID:          uid,
		Name:        name,
		Description: description,
		ExpiryKind:  expiryKind,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrQualificationNotFound
		}
		if isPgUniqueViolation(err) {
			return nil, core.ErrQualificationNameTaken
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind), nil
}

// ListQualificationAssignees returns the users currently assigned a
// qualification (Story 2.7), with id + display name, ordered by name. An
// unknown qualification maps to core.ErrQualificationNotFound → 404.
func (r *Repository) ListQualificationAssignees(ctx context.Context, qualificationID string) ([]*core.QualificationAssignee, error) {
	qid, err := uuidFromString(qualificationID)
	if err != nil {
		return nil, core.ErrQualificationNotFound
	}
	exists, err := r.queries.QualificationExists(ctx, qid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrQualificationNotFound
	}
	rows, err := r.queries.ListQualificationAssignees(ctx, qid)
	if err != nil {
		return nil, err
	}
	out := make([]*core.QualificationAssignee, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.QualificationAssignee{
			ID:   uuidToString(row.ID.Bytes),
			Name: row.DisplayName,
		})
	}
	return out, nil
}

// ReplaceQualificationAssignees REPLACES the assignee set of a qualification
// atomically (delete-then-insert in ONE transaction, Story 2.7): the current
// assignment rows are deleted and the new set inserted in the same transaction,
// so a failed half-write never leaves a mixed assignment state. An unknown
// qualification maps to core.ErrQualificationNotFound → 404 (checked FIRST); an
// unknown user id maps to core.ErrQualificationAssigneeUnknown → 400. Removing
// a volunteer from the set revokes eligibility immediately (AD-7/FR-22) because
// resolution is live per request — nothing is cached. The updated assignee set
// is returned so the caller can echo it.
func (r *Repository) ReplaceQualificationAssignees(ctx context.Context, qualificationID string, userIDs []string) ([]*core.QualificationAssignee, error) {
	qid, err := uuidFromString(qualificationID)
	if err != nil {
		return nil, core.ErrQualificationNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	exists, err := q.QualificationExists(ctx, qid)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrQualificationNotFound
	}

	assigneeUUIDs, err := resolveQualificationAssigneeIDs(ctx, q, userIDs)
	if err != nil {
		return nil, err
	}

	if err := q.DeleteQualificationAssignees(ctx, qid); err != nil {
		return nil, err
	}
	if len(assigneeUUIDs) > 0 {
		if err := q.InsertQualificationAssignees(ctx, InsertQualificationAssigneesParams{QualificationID: qid, Column2: assigneeUUIDs}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Re-fetch the assignee set so the caller gets the committed rows.
	rows, err := r.queries.ListQualificationAssignees(ctx, qid)
	if err != nil {
		return nil, err
	}
	out := make([]*core.QualificationAssignee, 0, len(rows))
	for _, row := range rows {
		out = append(out, &core.QualificationAssignee{
			ID:   uuidToString(row.ID.Bytes),
			Name: row.DisplayName,
		})
	}
	return out, nil
}

// resolveQualificationAssigneeIDs validates that every requested assignee user
// id exists and returns the id set as pgtype.UUID. A missing id maps to
// core.ErrQualificationAssigneeUnknown (400). An empty input yields an empty
// set.
func resolveQualificationAssigneeIDs(ctx context.Context, q *Queries, ids []string) ([]pgtype.UUID, error) {
	uuids, err := uuidSlice(ids)
	if err != nil {
		return nil, core.ErrQualificationAssigneeUnknown
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	found, err := q.UsersExistByIDs(ctx, uuids)
	if err != nil {
		return nil, err
	}
	if len(found) != len(uuids) {
		return nil, core.ErrQualificationAssigneeUnknown
	}
	return uuids, nil
}

// AssignQualificationToUser assigns a qualification to a user (Spec 2.9) with
// an optional per-assignment expires_at. A `fixed` qualification REQUIRES a
// per-assignment expires_at; an `unlimited` one must NOT carry one (the core
// enforces this rule). An unknown user maps to core.ErrAdminUserNotFound → 404;
// an unknown qualification maps to core.ErrQualificationNotFound → 404. The
// qualification's expiry_kind is read so the fixed-vs-unlimited rule can be
// enforced before the insert.
func (r *Repository) AssignQualificationToUser(ctx context.Context, userID, qualificationID string, expiresAt *time.Time) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return core.ErrAdminUserNotFound
	}
	qid, err := uuidFromString(qualificationID)
	if err != nil {
		return core.ErrQualificationNotFound
	}

	tx, err := r.beginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	q := r.queries.WithTx(tx)

	userExists, err := q.UserExists(ctx, uid)
	if err != nil {
		return err
	}
	if !userExists {
		return core.ErrAdminUserNotFound
	}

	exists, err := q.QualificationExists(ctx, qid)
	if err != nil {
		return err
	}
	if !exists {
		return core.ErrQualificationNotFound
	}

	// Enforce the fixed-vs-unlimited assignment rule (Spec 2.9 human decision A)
	// at the persistence seam as a belt-and-suspenders check on top of the core.
	// A targeted expiry-kind lookup (review finding 2.9) instead of scanning the
	// whole vocabulary.
	expiryRow, err := q.GetQualificationExpiryKindByID(ctx, qid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.ErrQualificationNotFound
		}
		return err
	}
	switch expiryRow.ExpiryKind {
	case core.QualificationExpiryUnlimited:
		if expiresAt != nil {
			return core.ErrQualificationInvalidExpiresAt
		}
	case core.QualificationExpiryFixed:
		if expiresAt == nil {
			return core.ErrQualificationExpiryRequired
		}
	default:
		return core.ErrQualificationNotFound
	}

	if err := q.AddQualificationToUser(ctx, AddQualificationToUserParams{
		UserID:          uid,
		QualificationID: qid,
		ExpiresAt:       timestamptzFromPtr(expiresAt),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateUserQualificationExpiry updates a user's per-assignment valid-until
// (Spec 2.9): a nil expiresAt clears the override (reverting to the vocabulary
// expiry); a value overrides it. An unknown user/qualification pair maps to
// core.ErrQualificationAssignmentNotFound → 404.
func (r *Repository) UpdateUserQualificationExpiry(ctx context.Context, userID, qualificationID string, expiresAt *time.Time) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return core.ErrQualificationAssignmentNotFound
	}
	qid, err := uuidFromString(qualificationID)
	if err != nil {
		return core.ErrQualificationAssignmentNotFound
	}
	// The fixed-vs-unlimited rule holds on UPDATE too, not just assign (retro
	// finding F13): read the qualification's expiry kind so a `fixed`
	// assignment can never be silently cleared to permanent-"valid", and an
	// `unlimited` one can never carry a date.
	expiryRow, err := r.queries.GetQualificationExpiryKindByID(ctx, qid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return core.ErrQualificationAssignmentNotFound
		}
		return err
	}
	switch expiryRow.ExpiryKind {
	case core.QualificationExpiryUnlimited:
		if expiresAt != nil {
			// An `unlimited` qualification must NEVER carry a per-assignment
			// expires_at (Spec 2.9: "Unbegrenzt" never expires).
			return core.ErrQualificationInvalidExpiresAt
		}
	case core.QualificationExpiryFixed:
		if expiresAt == nil {
			// A `fixed` qualification REQUIRES a per-assignment expires_at
			// (Spec 2.9 human decision A) — clearing it here would make the
			// assignment never expire (retro finding F13).
			return core.ErrQualificationExpiryRequired
		}
	default:
		return core.ErrQualificationAssignmentNotFound
	}
	rowsAffected, err := r.queries.UpdateUserQualificationExpiry(ctx, UpdateUserQualificationExpiryParams{
		UserID:          uid,
		QualificationID: qid,
		ExpiresAt:       timestamptzFromPtr(expiresAt),
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return core.ErrQualificationAssignmentNotFound
	}
	return nil
}

// RevokeQualificationFromUser revokes a qualification from a user (Spec 2.9).
// An unknown user maps to core.ErrAdminUserNotFound → 404; an unknown
// qualification maps to core.ErrQualificationNotFound → 404. Revocation is
// immediate (AD-7/FR-22) because resolution is live per request.
func (r *Repository) RevokeQualificationFromUser(ctx context.Context, userID, qualificationID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return core.ErrAdminUserNotFound
	}
	qid, err := uuidFromString(qualificationID)
	if err != nil {
		return core.ErrQualificationNotFound
	}
	userExists, err := r.queries.UserExists(ctx, uid)
	if err != nil {
		return err
	}
	if !userExists {
		return core.ErrAdminUserNotFound
	}
	qualExists, err := r.queries.QualificationExists(ctx, qid)
	if err != nil {
		return err
	}
	if !qualExists {
		return core.ErrQualificationNotFound
	}
	rows, err := r.queries.RemoveQualificationFromUser(ctx, RemoveQualificationFromUserParams{
		UserID:          uid,
		QualificationID: qid,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		// Unassigned pair: consistent with UpdateUserQualificationExpiry, an
		// unassigned assignment maps to the uniform not-found (review finding
		// 2.9).
		return core.ErrQualificationAssignmentNotFound
	}
	return nil
}

// qualificationFromRow maps an sqlc qualification row to the core domain value.
func qualificationFromRow(id pgtype.UUID, name, description, expiryKind string) *core.Qualification {
	return &core.Qualification{
		ID:          uuidToString(id.Bytes),
		Name:        name,
		Description: description,
		ExpiryKind:  expiryKind,
	}
}

// timestamptzFromPtr converts an optional time to a pgtype.Timestamptz (nil →
// NULL, used to clear expires_at when switching a qualification to unlimited).
func timestamptzFromPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}