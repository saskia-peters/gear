package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresDsgvoExportReads exercises the Story 3.3 DSGVO data-access reads
// over the dev database (migration 000029 applied): GetUserByIDFull returns the
// FULL row (attributes + created_at/updated_at AND the secret columns — the
// core strips them before assembly, REPORT_SECRETS) and ListSessionsByUser
// returns the user's sessions newest-first WITHOUT any token material.
func TestPostgresDsgvoExportReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := userTestPool(t)

	queries := New(pool)
	repo := NewRepository(queries)

	suffix := time.Now().Format("20060102150405.000000")
	testEmail := "dsgvo.test." + suffix + "@gear.local"
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Dsgvo Test", "Dsgvo", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", created.ID)
	})

	// GetUserByIDFull returns the FULL row: the secret columns AND the
	// attributes/timestamps are present (the export strips the secrets in the
	// core; this read is the profile source).
	full, err := repo.GetUserByIDFull(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByIDFull err = %v", err)
	}
	if full == nil {
		t.Fatal("GetUserByIDFull returned nil")
	}
	if full.Email != testEmail || full.FirstName != "Dsgvo" || full.LastName != "Test" {
		t.Errorf("full profile identity = %+v, want the created user", full)
	}
	if full.PasswordHash == "" {
		t.Error("full.PasswordHash empty — GetUserByIDFull must return the stored hash (the core strips it before assembly)")
	}
	if full.CreatedAt.IsZero() || full.UpdatedAt.IsZero() {
		t.Errorf("full timestamps = (%v, %v), want the stored values", full.CreatedAt, full.UpdatedAt)
	}
	if full.Attributes == nil {
		t.Error("full.Attributes nil, want the parsed JSONB object")
	}

	// An unknown id maps to ErrAdminUserNotFound.
	if _, err := repo.GetUserByIDFull(ctx, "00000000-0000-0000-0000-0000000000ff"); err != core.ErrAdminUserNotFound {
		t.Errorf("GetUserByIDFull(unknown) err = %v, want ErrAdminUserNotFound", err)
	}

	// ListSessionsByUser: two sessions (older first in creation, newer expiry)
	// → newest-first list, identity + timestamps only (no token material).
	olderHash := "hash-older-" + suffix
	newerHash := "hash-newer-" + suffix
	older, err := repo.CreateSession(ctx, created.ID, olderHash, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession(older) err = %v", err)
	}
	newer, err := repo.CreateSession(ctx, created.ID, newerHash, time.Now().UTC().Add(4*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession(newer) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM sessions WHERE id = ANY($1)", []string{older.ID, newer.ID})
	})

	sessions, err := repo.ListSessionsByUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListSessionsByUser err = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	// newest first by created_at DESC: newer has the later created_at.
	if sessions[0].ID != newer.ID || sessions[1].ID != older.ID {
		t.Errorf("sessions order = [%s, %s], want [%s, %s] newest first",
			sessions[0].ID, sessions[1].ID, newer.ID, older.ID)
	}
	if sessions[0].ExpiresAt.IsZero() || sessions[0].CreatedAt.IsZero() {
		t.Errorf("session timestamps missing: %+v", sessions[0])
	}

	// A user with no sessions answers an empty list, nil-safe.
	emptyUser, err := repo.CreateRegisteredUser(ctx, "dsgvo.empty."+suffix+"@gear.local", "Leer", "Leer", "Test", "$argon2id$v=19$dummy")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(empty) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", emptyUser.ID)
	})
	empty, err := repo.ListSessionsByUser(ctx, emptyUser.ID)
	if err != nil {
		t.Fatalf("ListSessionsByUser(empty) err = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty sessions = %+v, want an empty non-nil list", empty)
	}
}

// TestPostgresSoftDeleteAndArchivePurge exercises the Story 3.4 lifecycle over
// the dev database (migration 000030 applied): SoftDeleteAndArchive moves the
// personal snapshot into dsgvo_deleted_accounts (secrets excluded) and flips
// the users row to the scrubbed `deleted` tombstone in ONE transaction;
// ListDeletedAccounts returns it newest-first; PurgeDeletedAccount hard-deletes
// the archive row AND the users tombstone (the ONLY hard delete).
func TestPostgresSoftDeleteAndArchivePurge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := userTestPool(t)

	queries := New(pool)
	repo := NewRepository(queries)

	suffix := time.Now().Format("20060102150405.000000")
	testEmail := "dsgvo-del." + suffix + "@gear.local"
	created, err := repo.CreateRegisteredUser(ctx, testEmail, "Weg Archiv", "Weg", "Archiv", "$argon2id$v=19$dummy")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	// Give the account an MFA secret + a login attempt so the scrub clears both.
	if err := repo.SetUserTotpSecret(ctx, created.ID, "enc:secret"); err != nil {
		t.Fatalf("SetUserTotpSecret err = %v", err)
	}
	if err := repo.IncrementLoginAttempts(ctx, testEmail); err != nil {
		t.Fatalf("IncrementLoginAttempts err = %v", err)
	}

	// SoftDeleteAndArchive: one transaction moves the snapshot + scrubs.
	scrubbed, err := repo.SoftDeleteAndArchive(ctx, created.ID, "Auf Wunsch", created.ID)
	if err != nil {
		t.Fatalf("SoftDeleteAndArchive err = %v", err)
	}
	t.Cleanup(func() {
		// Belt-and-suspenders: the purge below removes the tombstone + archive;
		// a failed purge leaves rows the next run must not collide with. Uses
		// context.Background() — the test's `ctx` is canceled by `defer cancel()`
		// BEFORE the t.Cleanup callbacks run, so a canceled-context Exec would
		// silently leak the rows.
		_, _ = pool.Exec(context.Background(), "DELETE FROM dsgvo_deleted_accounts WHERE original_user_id = $1", created.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", created.ID)
	})

	if scrubbed.State != core.StateDeleted {
		t.Errorf("state = %q, want deleted", scrubbed.State)
	}
	if scrubbed.Email != "deleted."+created.ID+"@deleted.local" {
		t.Errorf("scrubbed email = %q, want the placeholder", scrubbed.Email)
	}
	if scrubbed.PasswordHash != "" || scrubbed.FirstName != "" || scrubbed.LastName != "" || scrubbed.DisplayName != "" {
		t.Errorf("scrubbed personal fields not cleared: %+v", scrubbed)
	}
	if scrubbed.TotpSecretEncrypted != "" || scrubbed.IsMFAEnabled {
		t.Errorf("scrubbed MFA not cleared: %+v", scrubbed)
	}

	// The archive holds the personal snapshot (no secrets) + reason + deleted_by.
	archived, err := repo.ListDeletedAccounts(ctx)
	if err != nil {
		t.Fatalf("ListDeletedAccounts err = %v", err)
	}
	firstArchiveID := archiveIDFor(t, archived, created.ID)
	if firstArchiveID == "" {
		t.Fatalf("archive row for %q not found: %+v", created.ID, archived)
	}
	a := firstArchive(t, archived, created.ID)
	if a.Email != testEmail || a.DisplayName != "Weg Archiv" ||
		a.FirstName != "Weg" || a.LastName != "Archiv" || a.Reason != "Auf Wunsch" || a.DeletedBy != created.ID {
		t.Errorf("archive row = %+v, want the full personal snapshot + reason + deleted_by", a)
	}
	if len(a.Attributes) != 0 {
		t.Errorf("archive attributes = %+v, want the stored (empty) object", a.Attributes)
	}

	// The tombstone is NON-EXISTENT to the admin user surface (Story 3.4 Never
	// rule): ListUsers(nil) excludes it, so the DSGVO delete picker / the admin
	// "Benutzer" list never offer a deleted account.
	listed, err := repo.ListUsers(ctx, nil)
	if err != nil {
		t.Fatalf("ListUsers(nil) err = %v", err)
	}
	for _, row := range listed {
		if row.ID == created.ID {
			t.Errorf("ListUsers(nil) exposes the deleted tombstone: %+v", row)
		}
		if row.Status == string(core.StateDeleted) {
			t.Errorf("ListUsers(nil) returns a deleted-status row: %+v", row)
		}
	}

	// The login-attempt row for the original email is gone (checked BEFORE a fresh
	// login attempt — a subsequent attempt re-tracks the freed email, which is
	// the correct anti-enumeration behavior, FR-3).
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM login_attempts WHERE email = $1", testEmail).Scan(&attempts); err != nil {
		t.Fatalf("counting login attempts err = %v", err)
	}
	if attempts != 0 {
		t.Errorf("login attempts for %q = %d, want 0 (scrubbed with the email)", testEmail, attempts)
	}

	// RE-LOGIN REJECTED (permanently blocked, not just state == deleted): the
	// scrubbed email no longer resolves (GetUserByEmail → nil) AND a FRESH login
	// attempt with the original email + correct password fails with
	// ErrInvalidCredentials (the account is gone from the auth path).
	byEmail, err := repo.GetUserByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail after deletion err = %v", err)
	}
	if byEmail != nil {
		t.Errorf("GetUserByEmail(%q) = %+v after deletion, want nil (address freed + unauthenticatable)", testEmail, byEmail)
	}
	loginSvc := core.NewService(repo, crypto.NewHasher(), core.NewSessionManager(repo, time.Hour), nil, nil)
	if _, err := loginSvc.Login(ctx, core.LoginInput{Email: testEmail, Password: "geheim123456"}); !errors.Is(err, core.ErrInvalidCredentials) {
		t.Errorf("login after deletion err = %v, want ErrInvalidCredentials (re-login permanently blocked)", err)
	}

	// ORDERING: soft-delete a SECOND user; ListDeletedAccounts must return the
	// two rows NEWEST-FIRST (deleted_at DESC, id DESC tiebreak — uuidv7 ids are
	// time-ordered, so even a same-microsecond deleted_at is deterministic).
	secondEmail := "dsgvo-del2." + suffix + "@gear.local"
	second, err := repo.CreateRegisteredUser(ctx, secondEmail, "Zweites Opfer", "Zweites", "Opfer", "$argon2id$v=19$dummy")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(second) err = %v", err)
	}
	if _, err := repo.SoftDeleteAndArchive(ctx, second.ID, "Auch auf Wunsch", created.ID); err != nil {
		t.Fatalf("SoftDeleteAndArchive(second) err = %v", err)
	}
	secondArchiveID := archiveIDFor(t, repoArchived(t, repo), second.ID)
	if secondArchiveID == "" {
		t.Fatalf("archive row for the second user not found")
	}
	order, err := repo.ListDeletedAccounts(ctx)
	if err != nil {
		t.Fatalf("ListDeletedAccounts(order) err = %v", err)
	}
	idxFirst, idxSecond := -1, -1
	for i, row := range order {
		if row.ID == firstArchiveID {
			idxFirst = i
		}
		if row.ID == secondArchiveID {
			idxSecond = i
		}
	}
	if idxFirst == -1 || idxSecond == -1 {
		t.Fatalf("order = %+v, missing the two archive rows (first=%d second=%d)", order, idxFirst, idxSecond)
	}
	if idxSecond >= idxFirst {
		t.Errorf("archive order = first@%d second@%d, want the SECOND (newest) first", idxFirst, idxSecond)
	}

	// Before the purge: a remaining session (the tombstone's session) AND an
	// audit row referencing the actor must both exist — the purge then CASCADEs
	// the session and auto-SET-NULLs the audit actor ref (000006/000009).
	session, err := repo.CreateSession(ctx, created.ID, "hash-purge-"+suffix, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sessions WHERE id = $1", session.ID)
	})
	if err := repo.InsertAuditEvent(ctx, created.ID, "test.audit", "", "normal"); err != nil {
		t.Fatalf("InsertAuditEvent err = %v", err)
	}
	var auditRowID string
	if err := pool.QueryRow(ctx,
		"SELECT id::text FROM audit_log WHERE actor_user_id = $1 AND operation = 'test.audit'", created.ID).Scan(&auditRowID); err != nil {
		t.Fatalf("reading pre-purge audit row err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_log WHERE id = $1", auditRowID)
	})

	// An already-deleted tombstone is non-existent to the surface (404).
	if _, err := repo.SoftDeleteAndArchive(ctx, created.ID, "Zweiter Grund", created.ID); err != core.ErrAdminUserNotFound {
		t.Errorf("re-delete err = %v, want ErrAdminUserNotFound", err)
	}
	// An unknown id → 404.
	if _, err := repo.SoftDeleteAndArchive(ctx, "00000000-0000-0000-0000-0000000000ff", "Grund", created.ID); err != core.ErrAdminUserNotFound {
		t.Errorf("unknown-target delete err = %v, want ErrAdminUserNotFound", err)
	}

	// PURGE: the FIRST archive row + its tombstone are hard-deleted in one
	// transaction.
	if err := repo.PurgeDeletedAccount(ctx, firstArchiveID); err != nil {
		t.Fatalf("PurgeDeletedAccount err = %v", err)
	}
	var archiveLeft, tombstoneLeft int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM dsgvo_deleted_accounts WHERE id = $1", firstArchiveID).Scan(&archiveLeft); err != nil {
		t.Fatalf("counting archive row err = %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", created.ID).Scan(&tombstoneLeft); err != nil {
		t.Fatalf("counting tombstone err = %v", err)
	}
	if archiveLeft != 0 || tombstoneLeft != 0 {
		t.Errorf("purge left archive=%d tombstone=%d, want both 0 (the ONLY hard delete)", archiveLeft, tombstoneLeft)
	}
	// The SECOND archive row (a different tombstone) is untouched.
	if id := archiveIDFor(t, repoArchived(t, repo), second.ID); id == "" {
		t.Errorf("the second archive row vanished — the purge must target exactly one account")
	}
	// The remaining session CASCADEd with the tombstone.
	var sessionLeft int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = $1", session.ID).Scan(&sessionLeft); err != nil {
		t.Fatalf("counting session err = %v", err)
	}
	if sessionLeft != 0 {
		t.Errorf("session after purge = %d, want 0 (CASCADEd with the tombstone)", sessionLeft)
	}
	// The prior audit row SURVIVED with its actor reference SET NULL (immutable
	// evidence, NFR-O2).
	var actorIsNull bool
	if err := pool.QueryRow(ctx,
		"SELECT actor_user_id IS NULL FROM audit_log WHERE id = $1", auditRowID).Scan(&actorIsNull); err != nil {
		t.Fatalf("reading post-purge audit row err = %v", err)
	}
	if !actorIsNull {
		t.Errorf("audit actor reference NOT set NULL after purge — evidence must survive")
	}

	// An already-purged archive id → 404.
	if err := repo.PurgeDeletedAccount(ctx, firstArchiveID); err != core.ErrDeletedAccountNotFound {
		t.Errorf("re-purge err = %v, want ErrDeletedAccountNotFound", err)
	}

	// Purging the SECOND account removes its tombstone too (the purge guard
	// hard-deletes only a `deleted` tombstone).
	if err := repo.PurgeDeletedAccount(ctx, secondArchiveID); err != nil {
		t.Fatalf("PurgeDeletedAccount(second) err = %v", err)
	}

	// PURGE GUARD (corrupted archive row): an archive row pointing at a
	// NON-deleted account must NEVER hard-delete a live user — the
	// `state = 'deleted'` guard + affected-row check map it to the 404 and the
	// account survives.
	liveEmail := "dsgvo-live." + suffix + "@gear.local"
	live, err := repo.CreateRegisteredUser(ctx, liveEmail, "Lebendig", "Le", "Bendig", "$argon2id$v=19$dummy")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(live) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", live.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM dsgvo_deleted_accounts WHERE original_user_id = $1", live.ID)
	})
	corruptArchive, err := queries.InsertDeletedAccount(ctx, InsertDeletedAccountParams{
		OriginalUserID: mustUUID(t, live.ID),
		Email:          liveEmail,
		DisplayName:    "Lebendig",
		Attributes:     []byte("{}"),
		Reason:         "korrupte Zeile",
	})
	if err != nil {
		t.Fatalf("InsertDeletedAccount(corrupt) err = %v", err)
	}
	if err := repo.PurgeDeletedAccount(ctx, uuidToString(corruptArchive.ID.Bytes)); err != core.ErrDeletedAccountNotFound {
		t.Errorf("purge of a corrupt archive row err = %v, want ErrDeletedAccountNotFound", err)
	}
	var liveStill int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", live.ID).Scan(&liveStill); err != nil {
		t.Fatalf("counting live user err = %v", err)
	}
	if liveStill != 1 {
		t.Errorf("live user after corrupt purge = %d, want 1 (the guard must never hard-delete a non-deleted account)", liveStill)
	}
	var liveState string
	if err := pool.QueryRow(ctx, "SELECT state FROM users WHERE id = $1", live.ID).Scan(&liveState); err != nil {
		t.Fatalf("reading live user state err = %v", err)
	}
	if liveState == string(core.StateDeleted) {
		t.Errorf("live user state = %q after corrupt purge — the guard flipped it to deleted", liveState)
	}
}

// repoArchived re-lists the archive (helper for the two-row ordering test).
func repoArchived(t *testing.T, repo *Repository) []*core.DeletedAccount {
	t.Helper()
	rows, err := repo.ListDeletedAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListDeletedAccounts err = %v", err)
	}
	return rows
}

// archiveIDFor returns the archive id of the row holding the given original
// user id, or "" when absent (scoped lookup — the archive is shared, so the
// assertions never assume a clean table).
func archiveIDFor(t *testing.T, rows []*core.DeletedAccount, originalUserID string) string {
	t.Helper()
	for _, row := range rows {
		if row.OriginalUserID == originalUserID {
			return row.ID
		}
	}
	return ""
}

// firstArchive returns the archived row of the given original user id (assumes
// present — the caller verified via archiveIDFor).
func firstArchive(t *testing.T, rows []*core.DeletedAccount, originalUserID string) *core.DeletedAccount {
	t.Helper()
	for _, row := range rows {
		if row.OriginalUserID == originalUserID {
			return row
		}
	}
	return nil
}
