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
		out = append(out, qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind, row.ExpiresAt))
	}
	return out, nil
}

// CreateQualification creates a qualification vocabulary row (Story 2.7). The
// name is unique case-insensitively (a duplicate maps to
// core.ErrQualificationNameTaken → 409). `unlimited` qualifications store a
// NULL expires_at; `fixed` carry one.
func (r *Repository) CreateQualification(ctx context.Context, name, description, expiryKind string, expiresAt *time.Time) (*core.Qualification, error) {
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
		ExpiresAt:   timestamptzFromPtr(expiresAt),
	})
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, core.ErrQualificationNameTaken
		}
		return nil, err
	}
	return qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind, row.ExpiresAt), nil
}

// UpdateQualification replaces a qualification's name/description/expiry model
// atomically (Story 2.7). Editing the expiry model never rewrites existing
// assignments — each assignment inherits the qualification's current expiry
// model on read. An unknown id maps to core.ErrQualificationNotFound → 404
// (checked FIRST, so it never answers 409 "name taken"); renaming onto a name
// held by ANOTHER qualification maps to core.ErrQualificationNameTaken → 409.
func (r *Repository) UpdateQualification(ctx context.Context, id, name, description, expiryKind string, expiresAt *time.Time) (*core.Qualification, error) {
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
		ExpiresAt:   timestamptzFromPtr(expiresAt),
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
	return qualificationFromRow(row.ID, row.Name, row.Description, row.ExpiryKind, row.ExpiresAt), nil
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

// qualificationFromRow maps an sqlc qualification row to the core domain value.
func qualificationFromRow(id pgtype.UUID, name, description, expiryKind string, expiresAt pgtype.Timestamptz) *core.Qualification {
	q := &core.Qualification{
		ID:          uuidToString(id.Bytes),
		Name:        name,
		Description: description,
		ExpiryKind:  expiryKind,
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		q.ExpiresAt = &t
	}
	return q
}

// timestamptzFromPtr converts an optional time to a pgtype.Timestamptz (nil →
// NULL, used to clear expires_at when switching a qualification to unlimited).
func timestamptzFromPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}