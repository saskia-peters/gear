package core

import (
	"context"
	"fmt"
)

// DSGVO account-deletion anonymization (Story 3.4, FR-24/AD-8): the write seam
// the DSGVO orchestrator consumes through the Tool module's deletion port
// (tools/ports.DSGVODeletionPort). Given the erased user id it rewrites EVERY
// inspection/reinstatement reference to the canonical DeletedUserID sentinel in
// ONE transaction (the plain FK-less inspector_id/actor_id columns, AD-8/3.4).
// The rewrite is EXPLICIT + AUDITABLE (the orchestrator audits `dsgvo.delete`);
// idempotent (no matching rows → no-op); and the sentinel is absent from users,
// so the DisplayNameResolver seam renders the literal "Deleted User" — the
// inspection history/status report keep every timestamp/result/item/OOS state
// intact (FR-24/FR-18). The method is UNGATED by design — the orchestrator
// re-checks `dsgvo.delete` defense-in-depth (AD-6); the actor id is carried for
// audit/log context only.

// AnonymizeUserReferences rewrites the target user's inspector/actor references
// to DeletedUserID (Story 3.4, FR-24/AD-8): `inspections.inspector_id` and
// `reinstatements.actor_id` both flip to the canonical sentinel in ONE
// transaction (a half-rewrite never leaves mixed references). A user with no
// inspection/reinstatement rows is a no-op (IDEMPOTENT). No schema change, no
// user-table writes — the Tool module owns only its own references (AD-8/AD-11).
// The method is UNGATED and carries NO actor id: the orchestrator re-checks
// `dsgvo.delete` defense-in-depth and audits the operation (the rewrite itself
// only needs the target id).
//
// I/O matrix:
//   - ANON_OK: the target's inspection/reinstatement references are rewritten
//     to DeletedUserID; the history/status reads then render "Deleted User".
//   - ANON_EMPTY: no matching rows → a no-op, never an error.
//   - ANON_STORE_ERR: a storage failure surfaces wrapped (the orchestrator
//     answers the uniform 500).
func (s *Service) AnonymizeUserReferences(ctx context.Context, userID string) error {
	if err := s.store.AnonymizeUserReferences(ctx, userID); err != nil {
		return fmt.Errorf("tools core: failed to anonymize user references: %w", err)
	}
	s.log().Info("dsgvo deletion: user references anonymized",
		"target", userID, "sentinel", DeletedUserID)
	return nil
}
