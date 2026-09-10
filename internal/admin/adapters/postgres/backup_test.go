package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// TestPostgresBackupDestinationsStore exercises the CRUD + atomic COALESCE
// keep-existing round-trip over the dev database (migration 000018 applied),
// mirroring the smtp_settings store test. It covers GET_LIST_EMPTY, GET_LIST,
// CREATE_VALID, CREATE_NO_CREDENTIAL-path (local without credential),
// UPDATE_KEEP_CREDENTIAL and DELETE.
func TestPostgresBackupDestinationsStore(t *testing.T) {
	pool := adminTestPool(t)
	ctx := context.Background()
	// Close the pool AFTER the row-cleanup DELETE below runs (t.Cleanup runs in
	// LIFO order: registering the close first means the DELETE runs before it).
	t.Cleanup(func() { pool.Close() })
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM backup_destinations") })

	queries := New(pool)
	repo := NewRepository(queries)

	if _, err := pool.Exec(ctx, "DELETE FROM backup_destinations"); err != nil {
		t.Fatalf("cleanup err = %v", err)
	}

	// GET_LIST_EMPTY: no rows yet → empty list.
	got, err := repo.ListBackupDestinations(ctx)
	if err != nil {
		t.Fatalf("ListBackupDestinations(fresh) err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh list = %d, want 0", len(got))
	}

	// CREATE_VALID: local with path + credential → persisted with the generated
	// uuidv7 id and the ciphertext verbatim.
	first, err := repo.CreateBackupDestination(ctx, &core.BackupDestination{
		Name: "Lokales Ziel", Mechanism: core.BackupMechanismLocal,
		BucketOrPath: "/srv/backup", Username: "svc",
		PasswordEncrypted: "enc:geheim", Schedule: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("CreateBackupDestination(first) err = %v", err)
	}
	if first.ID == "" || len(first.ID) != 36 {
		t.Errorf("id = %q, want a generated uuid", first.ID)
	}
	if first.Name != "Lokales Ziel" || first.Mechanism != core.BackupMechanismLocal {
		t.Errorf("first = %+v, want persisted values", first)
	}
	if first.PasswordEncrypted != "enc:geheim" {
		t.Errorf("password_encrypted = %q, want ciphertext verbatim", first.PasswordEncrypted)
	}
	if first.Schedule != "0 2 * * *" {
		t.Errorf("schedule = %q", first.Schedule)
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Errorf("timestamps missing: %+v", first)
	}
	createdAt := first.CreatedAt
	firstUpdatedAt := first.UpdatedAt

	// CREATE without a credential (local optional) → credential_configured=false.
	second, err := repo.CreateBackupDestination(ctx, &core.BackupDestination{
		Name: "Lokal ohne Zugangsdaten", Mechanism: core.BackupMechanismLocal,
		BucketOrPath: "/tmp/backup",
	})
	if err != nil {
		t.Fatalf("CreateBackupDestination(second) err = %v", err)
	}
	if second.CredentialConfigured() {
		t.Error("credential_configured = true for a credential-less local dest, want false")
	}

	// GET_LIST: both destinations, oldest first.
	list, err := repo.ListBackupDestinations(ctx)
	if err != nil {
		t.Fatalf("ListBackupDestinations err = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %d, want 2 (multi-row)", len(list))
	}
	if list[0].ID != first.ID || list[1].ID != second.ID {
		t.Errorf("order = %q,%q want first,second", list[0].ID, list[1].ID)
	}

	// GET by id.
	fetched, err := repo.GetBackupDestination(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetBackupDestination err = %v", err)
	}
	if fetched == nil || fetched.PasswordEncrypted != "enc:geheim" {
		t.Fatalf("fetched = %+v, want the first destination", fetched)
	}

	// GET missing → nil, nil.
	missing, err := repo.GetBackupDestination(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil || missing != nil {
		t.Fatalf("GetBackupDestination(missing) = %+v, %v; want nil, nil", missing, err)
	}

	// UPDATE_KEEP_CREDENTIAL: edit without credential → existing ciphertext
	// kept atomically (COALESCE in one statement), other fields updated, the
	// schedule cleared by an empty value.
	updated, err := repo.UpdateBackupDestination(ctx, &core.BackupDestination{
		ID: first.ID, Name: "Lokales Ziel v2", Mechanism: core.BackupMechanismLocal,
		BucketOrPath: "/srv/backup2", Username: "svc", Schedule: "",
	})
	if err != nil {
		t.Fatalf("UpdateBackupDestination(keep) err = %v", err)
	}
	if updated.PasswordEncrypted != "enc:geheim" {
		t.Errorf("password after keep-existing = %q, want unchanged enc:geheim", updated.PasswordEncrypted)
	}
	if !updated.CredentialConfigured() {
		t.Error("credential_configured = false after keep-existing, want true")
	}
	if updated.Name != "Lokales Ziel v2" || updated.BucketOrPath != "/srv/backup2" {
		t.Errorf("updated fields = %+v", updated)
	}
	if updated.Schedule != "" {
		t.Errorf("schedule = %q, want cleared (NULL)", updated.Schedule)
	}
	// Finding: updated_at is refreshed on every edit (the UPDATE statement must
	// set it, not rely on the DEFAULT).
	if !updated.UpdatedAt.After(firstUpdatedAt) {
		t.Errorf("updated_at = %v, want after the create's %v", updated.UpdatedAt, firstUpdatedAt)
	}
	if !updated.UpdatedAt.After(createdAt) {
		t.Errorf("updated_at = %v, want after created_at %v", updated.UpdatedAt, createdAt)
	}

	// UPDATE with a NEW credential → replaced.
	cred := "neu-geheim"
	replaced, err := repo.UpdateBackupDestination(ctx, &core.BackupDestination{
		ID: first.ID, Name: "Lokales Ziel v2", Mechanism: core.BackupMechanismLocal,
		BucketOrPath: "/srv/backup2", Username: "svc", PasswordEncrypted: "enc:" + cred,
	})
	if err != nil {
		t.Fatalf("UpdateBackupDestination(replace) err = %v", err)
	}
	if replaced.PasswordEncrypted != "enc:"+cred {
		t.Errorf("replaced credential = %q, want enc:neu-geheim", replaced.PasswordEncrypted)
	}

	// Finding: clear_credential:true WIPES the stored credential (explicit
	// revoke — a blank password alone would keep it).
	cleared, err := repo.UpdateBackupDestination(ctx, &core.BackupDestination{
		ID: first.ID, Name: "Lokales Ziel v2", Mechanism: core.BackupMechanismLocal,
		BucketOrPath: "/srv/backup2", ClearCredential: true,
	})
	if err != nil {
		t.Fatalf("UpdateBackupDestination(clear) err = %v", err)
	}
	if cleared.PasswordEncrypted != "" {
		t.Errorf("password_encrypted = %q, want cleared", cleared.PasswordEncrypted)
	}
	if cleared.CredentialConfigured() {
		t.Error("credential_configured = true after clear, want false")
	}

	// UPDATE missing id → ErrBackupDestinationNotFound.
	if _, err := repo.UpdateBackupDestination(ctx, &core.BackupDestination{ID: "00000000-0000-0000-0000-000000000000", Name: "x"}); !errors.Is(err, core.ErrBackupDestinationNotFound) {
		t.Fatalf("UpdateBackupDestination(missing) err = %v, want ErrBackupDestinationNotFound", err)
	}

	// DELETE: the destination is removed; a second delete reports not-found.
	if err := repo.DeleteBackupDestination(ctx, second.ID); err != nil {
		t.Fatalf("DeleteBackupDestination err = %v", err)
	}
	if err := repo.DeleteBackupDestination(ctx, second.ID); !errors.Is(err, core.ErrBackupDestinationNotFound) {
		t.Fatalf("second delete err = %v, want ErrBackupDestinationNotFound", err)
	}

	// DELETE with a malformed id is treated as not-found (no existence hint).
	if err := repo.DeleteBackupDestination(ctx, "not-a-uuid"); !errors.Is(err, core.ErrBackupDestinationNotFound) {
		t.Fatalf("delete malformed id err = %v, want ErrBackupDestinationNotFound", err)
	}
}