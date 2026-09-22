package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestSoftDeleteAndArchiveLifecycle exercises the Story 3.4 User lifecycle port
// against the in-memory mock: the account becomes the scrubbed `deleted`
// tombstone (personal fields cleared, email placeholder frees the address), the
// personal snapshot moves into the archive (EXCLUDING secrets — the scrubbed
// tombstone carries none) and a FRESH login attempt with the ORIGINAL email is
// rejected (re-login permanently blocked — only `active` authenticates).
func TestSoftDeleteAndArchiveLifecycle(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Seed an ACTIVE user with a password + tracked login attempts.
	user, err := repo.CreateRegisteredUser(context.Background(), "del@gear.local", "Zu Loeschen", "Zu", "Loeschen", "hashed:geheim123456")
	if err != nil {
		t.Fatalf("CreateRegisteredUser err = %v", err)
	}
	user.State = StateActive
	if err := repo.IncrementLoginAttempts(context.Background(), "del@gear.local"); err != nil {
		t.Fatalf("IncrementLoginAttempts err = %v", err)
	}

	actor := &User{ID: "u-admin", Email: "admin@gear.local", State: StateActive}
	if err := svc.SoftDeleteAndArchive(context.Background(), actor, user.ID, "Auf Wunsch"); err != nil {
		t.Fatalf("SoftDeleteAndArchive err = %v", err)
	}

	// The tombstone: state deleted, personal fields scrubbed, email placeholder.
	tombstone := repo.userByID(user.ID)
	if tombstone == nil {
		t.Fatal("tombstone gone — SoftDeleteAndArchive must NEVER hard-delete")
	}
	if tombstone.State != StateDeleted {
		t.Errorf("state = %q, want deleted", tombstone.State)
	}
	if tombstone.Email != "deleted."+user.ID+"@deleted.local" {
		t.Errorf("email = %q, want the deleted.<id>@deleted.local placeholder", tombstone.Email)
	}
	if tombstone.PasswordHash != "" || tombstone.DisplayName != "" || tombstone.FirstName != "" || tombstone.LastName != "" {
		t.Errorf("tombstone personal fields not scrubbed: %+v", tombstone)
	}
	if len(tombstone.Attributes) != 0 {
		t.Errorf("tombstone attributes = %+v, want empty", tombstone.Attributes)
	}
	if tombstone.IsMFAEnabled || tombstone.MustChangePassword {
		t.Errorf("tombstone flags not cleared: %+v", tombstone)
	}

	// The archive snapshot holds the personal data (email, names) + reason +
	// deleted_by; NO secret material is present.
	archived, err := repo.ListDeletedAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListDeletedAccounts err = %v", err)
	}
	if len(archived) != 1 {
		t.Fatalf("archived rows = %d, want 1", len(archived))
	}
	a := archived[0]
	if a.OriginalUserID != user.ID || a.Email != "del@gear.local" || a.DisplayName != "Zu Loeschen" ||
		a.FirstName != "Zu" || a.LastName != "Loeschen" || a.Reason != "Auf Wunsch" || a.DeletedBy != "u-admin" {
		t.Errorf("archive row = %+v, want the full personal snapshot + reason + deleted_by", a)
	}
	if a.DeletedAt.IsZero() {
		t.Errorf("archive deleted_at zero, want the deletion timestamp")
	}

	// RE-LOGIN REJECTED: a fresh login attempt with the ORIGINAL email + correct
	// password fails (the account is gone from the auth path).
	_, err = svc.Login(context.Background(), LoginInput{Email: "del@gear.local", Password: "geheim123456"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login after deletion err = %v, want ErrInvalidCredentials (re-login permanently blocked)", err)
	}
}

// TestSoftDeleteAndArchiveUnknownAndDeletedTargets pins the 404 semantics: an
// unknown id OR an already-deleted tombstone maps to ErrAdminUserNotFound (the
// surface treats `deleted` as non-existent).
func TestSoftDeleteAndArchiveUnknownAndDeletedTargets(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})
	actor := &User{ID: "u-admin", State: StateActive}

	// Unknown id → ErrAdminUserNotFound, no archive row.
	if err := svc.SoftDeleteAndArchive(context.Background(), actor, "u-gibtsnicht", "Grund"); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("unknown target err = %v, want ErrAdminUserNotFound", err)
	}
	archived, _ := repo.ListDeletedAccounts(context.Background())
	if len(archived) != 0 {
		t.Errorf("archive rows after unknown target = %d, want 0", len(archived))
	}

	// Already-deleted tombstone → ErrAdminUserNotFound (no duplicate archive).
	user, err := repo.CreateRegisteredUser(context.Background(), "del2@gear.local", "Schon Weg", "Schon", "Weg", "hashed:x")
	if err != nil {
		t.Fatalf("CreateRegisteredUser err = %v", err)
	}
	user.State = StateActive
	if err := svc.SoftDeleteAndArchive(context.Background(), actor, user.ID, "Erster Grund"); err != nil {
		t.Fatalf("first delete err = %v", err)
	}
	if err := svc.SoftDeleteAndArchive(context.Background(), actor, user.ID, "Zweiter Grund"); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("second delete err = %v, want ErrAdminUserNotFound", err)
	}
	archived, _ = repo.ListDeletedAccounts(context.Background())
	if len(archived) != 1 {
		t.Errorf("archive rows after double delete = %d, want exactly 1 (no duplicate archive)", len(archived))
	}
}

// TestPurgeDeletedAccountHardDeletesTombstone pins the SOLE hard delete: the
// archive row AND the users tombstone are removed together; an unknown /
// already-purged archive id maps to ErrDeletedAccountNotFound.
func TestPurgeDeletedAccountHardDeletesTombstone(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	user, err := repo.CreateRegisteredUser(context.Background(), "purge@gear.local", "Weg Purgen", "Weg", "Purgen", "hashed:x")
	if err != nil {
		t.Fatalf("CreateRegisteredUser err = %v", err)
	}
	user.State = StateActive
	if err := svc.SoftDeleteAndArchive(context.Background(), &User{ID: "u-admin", State: StateActive}, user.ID, "Grund"); err != nil {
		t.Fatalf("SoftDeleteAndArchive err = %v", err)
	}

	archived, _ := repo.ListDeletedAccounts(context.Background())
	if err := svc.PurgeDeletedAccount(context.Background(), archived[0].ID); err != nil {
		t.Fatalf("PurgeDeletedAccount err = %v", err)
	}
	// The archive row is gone AND the tombstone is gone (the ONLY hard delete).
	left, _ := repo.ListDeletedAccounts(context.Background())
	if len(left) != 0 {
		t.Errorf("archive rows after purge = %d, want 0", len(left))
	}
	if repo.userByID(user.ID) != nil {
		t.Error("tombstone still present after purge — the purge must hard-delete the users row")
	}

	// An already-purged archive id → ErrDeletedAccountNotFound.
	if err := svc.PurgeDeletedAccount(context.Background(), archived[0].ID); !errors.Is(err, ErrDeletedAccountNotFound) {
		t.Fatalf("re-purge err = %v, want ErrDeletedAccountNotFound", err)
	}
	// A malformed id → ErrDeletedAccountNotFound.
	if err := svc.PurgeDeletedAccount(context.Background(), "nope"); !errors.Is(err, ErrDeletedAccountNotFound) {
		t.Fatalf("malformed id purge err = %v, want ErrDeletedAccountNotFound", err)
	}
}

// TestSoftDeleteAndArchiveRejectsEmptyActor guards the port contract: a nil
// actor (a wiring defect) surfaces a clear error, never a panic.
func TestSoftDeleteAndArchiveRejectsEmptyActor(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})
	err := svc.SoftDeleteAndArchive(context.Background(), nil, "u-target", "Grund")
	if err == nil || !strings.Contains(err.Error(), "nil actor") {
		t.Fatalf("err = %v, want a clear nil-actor error", err)
	}
}

// TestDeletedTombstoneNonExistentToAdminSurface pins the Story 3.4 Never rule
// on the admin surface: after a soft-delete, ListUsers(nil) EXCLUDES the
// tombstone and UpdateAdminUser on it is REFUSED (no re-activation path — an
// admin saving the user as `active` can never resurrect a deleted account).
func TestDeletedTombstoneNonExistentToAdminSurface(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	// Seed an active admin + a victim user. (The mock does not auto-assign ids,
	// so they are set explicitly — SoftDeleteAndArchive keyed by id needs them.)
	actor, err := repo.CreateRegisteredUser(context.Background(), "admin@gear.local", "Admin", "Ad", "Min", "hashed:x")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(admin) err = %v", err)
	}
	actor.ID = "u-admin-x"
	actor.State = StateActive
	repo.adminGroup[actor.ID] = true
	repo.perms[actor.ID] = []string{UserViewPermission, UserManagePermission}
	victim, err := repo.CreateRegisteredUser(context.Background(), "victim@gear.local", "Opfer", "Op", "Fer", "hashed:y")
	if err != nil {
		t.Fatalf("CreateRegisteredUser(victim) err = %v", err)
	}
	victim.ID = "u-victim"
	victim.State = StateActive

	// Before deletion the user is listed.
	before, err := svc.ListUsers(context.Background(), actor, nil)
	if err != nil {
		t.Fatalf("ListUsers err = %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("pre-delete users = %d, want 2", len(before))
	}

	if err := svc.SoftDeleteAndArchive(context.Background(), actor, victim.ID, "Auf Wunsch"); err != nil {
		t.Fatalf("SoftDeleteAndArchive err = %v", err)
	}

	// ListUsers(nil) excludes the tombstone (the unfiltered admin list).
	after, err := svc.ListUsers(context.Background(), actor, nil)
	if err != nil {
		t.Fatalf("ListUsers(after) err = %v", err)
	}
	if len(after) != 1 || after[0].ID != actor.ID {
		t.Errorf("post-delete users = %+v, want only the admin (tombstone excluded)", after)
	}

	// UpdateAdminUser on the tombstone → ErrAdminUserDeleted (409), even when
	// the admin submits status `active` (the re-activation attempt).
	_, err = svc.UpdateAdminUser(context.Background(), actor, victim.ID, UpdateAdminUserInput{
		Vorname: "Neu", Nachname: "Name", Email: "neue@example.com", Status: string(StateActive),
	})
	if !errors.Is(err, ErrAdminUserDeleted) {
		t.Fatalf("UpdateAdminUser on a tombstone err = %v, want ErrAdminUserDeleted", err)
	}
	// The tombstone is untouched (still deleted, still scrubbed).
	if repo.userByID(victim.ID) == nil || repo.userByID(victim.ID).State != StateDeleted {
		t.Errorf("tombstone state after refused update = %+v, want still deleted", repo.userByID(victim.ID))
	}
	// An UNKNOWN id still maps to the uniform 404 (not the deleted error).
	if _, err := svc.UpdateAdminUser(context.Background(), actor, "u-gibtsnicht", UpdateAdminUserInput{
		Vorname: "Neu", Nachname: "Name", Email: "neue@example.com", Status: string(StateActive),
	}); !errors.Is(err, ErrAdminUserNotFound) {
		t.Errorf("unknown-target update err = %v, want ErrAdminUserNotFound", err)
	}
}

// TestRegisterRejectsReservedDeletedLocalEmail pins the Story 3.4 reserved
// email guard: no real account may register on the `@deleted.local` tombstone
// domain (case-insensitive), so the placeholder addresses can never collide
// with a real registration.
func TestRegisterRejectsReservedDeletedLocalEmail(t *testing.T) {
	reserved := []string{
		"x@deleted.local",
		"deleted.00000000-0000-0000-0000-0000000000ff@deleted.local",
		"X@DELETED.LOCAL",
		"Foo.Bar@Deleted.Local",
	}
	for _, email := range reserved {
		input := RegisterInput{
			FirstName: "A", LastName: "B", Email: email,
			Password: "geheim123456", PasswordConfirm: "geheim123456",
		}
		if err := input.Validate(); !errors.Is(err, ErrEmailReserved) {
			t.Errorf("Validate(%q) err = %v, want ErrEmailReserved", email, err)
		}
		repo := newMockRepo()
		svc, _ := newTestService(repo, &mockHasher{})
		if _, err := svc.Register(context.Background(), input); !errors.Is(err, ErrEmailReserved) {
			t.Errorf("Register(%q) err = %v, want ErrEmailReserved", email, err)
		}
	}
	// A normal address is unaffected.
	normal := RegisterInput{
		FirstName: "A", LastName: "B", Email: "x@example.com",
		Password: "geheim123456", PasswordConfirm: "geheim123456",
	}
	if err := normal.Validate(); err != nil {
		t.Errorf("Validate(normal) err = %v, want nil", err)
	}
}
