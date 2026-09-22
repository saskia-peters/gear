package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DSGVO account deletion (Story 3.4, FR-24/AD-8): the lifecycle port the DSGVO
// orchestrator consumes through the User module's seam
// (user/ports.DSGVODeletionPort). Deletion NEVER hard-deletes (user decision
// 2026-09-18) — SoftDeleteAndArchive moves the target's personal data into the
// `dsgvo_deleted_accounts` ARCHIVE and flips the users row to a scrubbed
// `deleted` tombstone (re-login permanently blocked — only `active`
// authenticates, auth.go / session.go). The archive + tombstone are
// HARD-DELETED only when an admin invokes the on-demand purge — the sole hard
// delete. The methods are UNGATED by design: the orchestrator re-checks
// `dsgvo.delete` defense-in-depth (AD-6).

// DeletedAccount is one archived (soft-deleted) account row: the personal-data
// snapshot moved into dsgvo_deleted_accounts at deletion time (original user
// id, email, names, attributes, the deletion reason, the deleting admin id and
// the deletion timestamp). No secret material (password hash / TOTP / OTP) is
// ever present (the archive snapshot excludes secrets).
type DeletedAccount struct {
	ID             string
	OriginalUserID string
	Email          string
	DisplayName    string
	FirstName      string
	LastName       string
	Attributes     map[string]any
	Reason         string
	DeletedBy      string
	DeletedAt      time.Time
}

// ErrDeletedAccountNotFound is returned when a purge references an archive id
// that does not exist (or is not a valid uuidv7) — INCLUDING an already-purged
// row (PURGE_MISSING). Handlers map it to the uniform 404 German.
var ErrDeletedAccountNotFound = errors.New("user core: deleted account not found")

// SoftDeleteAndArchive is the DSGVO account-deletion lifecycle step (Story 3.4,
// FR-24/AD-8): in ONE transaction the repo (a) copies the target's personal
// data into dsgvo_deleted_accounts (the snapshot EXCLUDES secrets), (b) flips
// the users row state → `deleted` and scrubs the live personal fields (email →
// `deleted.<id>@deleted.local` placeholder freeing the UNIQUE key,
// password_hash → '', display/first/last → '', attributes → '{}',
// totp/otp/pending_email/must_change_password cleared) and (c) deletes the
// email's login-attempt rows. NO hard delete at deletion time. An unknown id —
// OR an already-deleted tombstone (the surface treats `deleted` as
// non-existent) — maps to ErrAdminUserNotFound (uniform 404).
func (s *Service) SoftDeleteAndArchive(ctx context.Context, actor *User, targetUserID, reason string) error {
	if actor == nil {
		return fmt.Errorf("user core: nil actor for soft-delete")
	}
	scrubbed, err := s.repo.SoftDeleteAndArchive(ctx, targetUserID, reason, actor.ID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return ErrAdminUserNotFound
		}
		return fmt.Errorf("user core: failed to soft-delete and archive user: %w", err)
	}
	s.log().Info("dsgvo deletion: account soft-deleted and archived",
		"actor", actor.ID, "target", scrubbed.ID)
	return nil
}

// ListDeletedAccounts returns every archived (soft-deleted) account, newest
// first (deleted_at DESC, id DESC — the SQL ORDER BY guarantees it). A deleted
// history that is empty answers an empty list, nil-safe.
func (s *Service) ListDeletedAccounts(ctx context.Context) ([]*DeletedAccount, error) {
	accounts, err := s.repo.ListDeletedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list deleted accounts: %w", err)
	}
	return accounts, nil
}

// PurgeDeletedAccount hard-deletes an archived account AND its (now-scrubbed)
// users tombstone in ONE transaction (Story 3.4, FR-24): this is the ONLY hard
// delete in the deletion lifecycle — admin-initiated on demand. The users
// delete CASCADEs any remaining sessions/reset tokens and auto-SET-NULLs the
// audit_log / admin_recovery references (migrations 000006/000009). An unknown
// or already-purged archive id maps to ErrDeletedAccountNotFound (uniform 404).
func (s *Service) PurgeDeletedAccount(ctx context.Context, archiveID string) error {
	if err := s.repo.PurgeDeletedAccount(ctx, archiveID); err != nil {
		if errors.Is(err, ErrDeletedAccountNotFound) {
			return ErrDeletedAccountNotFound
		}
		return fmt.Errorf("user core: failed to purge deleted account: %w", err)
	}
	s.log().Info("dsgvo deletion: deleted account purged", "archive", archiveID)
	return nil
}
