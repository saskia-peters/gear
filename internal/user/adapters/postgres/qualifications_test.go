package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresQualificationManagement covers the Story 2.7 qualification
// persistence contract (AD-7/FR-22): vocabulary CRUD (case-insensitive
// duplicate name → 409, unknown id → 404), the atomic assignee replacement
// (delete-then-insert in one transaction), the immediate effect of
// assign/remove on the user detail (AD-7/FR-22), and the server-derived status
// indicators through the REAL core service — against the REAL postgres
// repository.
func TestPostgresQualificationManagement(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	stamp := time.Now().Format("20060102150405.000000")
	qualName := "qual." + stamp
	volunteerEmail := "qualvol." + stamp + "@gear.local"

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", volunteerEmail); err != nil {
			t.Errorf("cleaning up user %q failed: %v", volunteerEmail, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE lower(name) = lower($1)", qualName); err != nil {
			t.Errorf("cleaning up qualification %q failed: %v", qualName, err)
		}
	})

	volunteer, err := repo.CreateAdminUser(ctx, volunteerEmail, "Frei", "Willig", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}

	// CREATE_UNLIMITED: an unlimited qualification (never expires).
	created, err := repo.CreateQualification(ctx, qualName, "Unbegrenzt gültig", core.QualificationExpiryUnlimited)
	if err != nil {
		t.Fatalf("CreateQualification failed: %v", err)
	}
	if created.ExpiryKind != core.QualificationExpiryUnlimited {
		t.Errorf("created = %+v, want unlimited", created)
	}

	// CREATE_DUP_NAME: the same name in a DIFFERENT case is a uniform 409.
	if _, err := repo.CreateQualification(ctx, strings.ToUpper(qualName), "", core.QualificationExpiryUnlimited); !errors.Is(err, core.ErrQualificationNameTaken) {
		t.Errorf("CreateQualification(dup, case variant) err = %v, want ErrQualificationNameTaken", err)
	}

	// CREATE_FIXED: a fixed qualification persists WITHOUT any date — the
	// vocabulary has no valid-until (2026-09-08 rework); a per-user valid-until
	// is set at assignment (Spec 2.9).
	fixedName := qualName + ".fixed"
	fixed, err := repo.CreateQualification(ctx, fixedName, "Befristet", core.QualificationExpiryFixed)
	if err != nil {
		t.Fatalf("CreateQualification(fixed) failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE id = $1", fixed.ID); err != nil {
			t.Errorf("cleaning up fixed qualification failed: %v", err)
		}
	})
	if fixed.ExpiryKind != core.QualificationExpiryFixed {
		t.Errorf("fixed qualification expiry kind = %q, want fixed", fixed.ExpiryKind)
	}

	// UPDATE_VALID: replace name/description/expiry kind atomically.
	renamedName := qualName + ".renamed"
	updated, err := repo.UpdateQualification(ctx, fixed.ID, renamedName, "Neu beschrieben", core.QualificationExpiryUnlimited)
	if err != nil {
		t.Fatalf("UpdateQualification failed: %v", err)
	}
	if updated.Name != renamedName || updated.Description != "Neu beschrieben" {
		t.Errorf("updated = %+v, want the new name/description", updated)
	}
	if updated.ExpiryKind != core.QualificationExpiryUnlimited {
		t.Errorf("updated expiry kind = %q, want unlimited", updated.ExpiryKind)
	}

	// UPDATE_UNKNOWN: a nonexistent id maps to the uniform 404 sentinel.
	if _, err := repo.UpdateQualification(ctx, "00000000-0000-0000-0000-000000000001", "X", "", core.QualificationExpiryUnlimited); !errors.Is(err, core.ErrQualificationNotFound) {
		t.Errorf("UpdateQualification(unknown) err = %v, want ErrQualificationNotFound", err)
	}

	// ASSIGN_VALID + IMMEDIATE_EFFECT: assigning a volunteer is reflected
	// immediately in the user detail (AD-7/FR-22).
	assignees, err := repo.ReplaceQualificationAssignees(ctx, created.ID, []string{volunteer.ID})
	if err != nil {
		t.Fatalf("ReplaceQualificationAssignees failed: %v", err)
	}
	if len(assignees) != 1 || assignees[0].ID != volunteer.ID {
		t.Fatalf("assignees = %+v, want the volunteer", assignees)
	}
	detail, err := repo.GetUserDetail(ctx, volunteer.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 || detail.Qualifications[0].Name != qualName {
		t.Errorf("user detail qualifications = %+v, want the assignment on the next request", detail.Qualifications)
	}

	// ASSIGN_REMOVE: removing the volunteer revokes eligibility immediately —
	// the next user detail no longer carries the assignment.
	assignees, err = repo.ReplaceQualificationAssignees(ctx, created.ID, nil)
	if err != nil {
		t.Fatalf("ReplaceQualificationAssignees(remove) failed: %v", err)
	}
	if len(assignees) != 0 {
		t.Errorf("assignees = %+v, want empty after removal", assignees)
	}
	detail, err = repo.GetUserDetail(ctx, volunteer.ID)
	if err != nil {
		t.Fatalf("GetUserDetail after removal failed: %v", err)
	}
	if len(detail.Qualifications) != 0 {
		t.Errorf("user detail qualifications = %+v, want [] (revoked immediately)", detail.Qualifications)
	}

	// ASSIGN_UNKNOWN: an unknown qualification → 404, an unknown user id → 400.
	if _, err := repo.ReplaceQualificationAssignees(ctx, "00000000-0000-0000-0000-000000000002", []string{volunteer.ID}); !errors.Is(err, core.ErrQualificationNotFound) {
		t.Errorf("ReplaceQualificationAssignees(unknown qual) err = %v, want ErrQualificationNotFound", err)
	}
	if _, err := repo.ReplaceQualificationAssignees(ctx, created.ID, []string{"00000000-0000-0000-0000-000000000003"}); !errors.Is(err, core.ErrQualificationAssigneeUnknown) {
		t.Errorf("ReplaceQualificationAssignees(unknown user) err = %v, want ErrQualificationAssigneeUnknown", err)
	}

	// LIST_ASSIGNEES: the current assignee set is returned (id + display name).
	got, err := repo.ListQualificationAssignees(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListQualificationAssignees failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListQualificationAssignees = %+v, want []", got)
	}

	// STATUS_* via the REAL core service (FR-22/AD-7): the vocabulary list
	// derives the badge from the expiry kind only (2026-09-08 rework) —
	// unlimited → Unbegrenzt, fixed → Befristet. The seeded admin group
	// carries `qualifications.manage` (000010), so admin.1 can list.
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil || admin == nil {
		t.Skip("seeded admin.1 not present — skipping status derivation assertion")
	}
	hasher := crypto.NewHasher()
	sm := core.NewSessionManager(repo, time.Hour)
	svc := core.NewService(repo, hasher, sm, nil, discardLogger())

	// A fixed qualification kept fixed, to pin the "Befristet" vocabulary badge
	// (the `fixed` row above was renamed to unlimited).
	badgeFixedName := qualName + ".badge-fixed"
	badgeFixed, err := repo.CreateQualification(ctx, badgeFixedName, "Befristet", core.QualificationExpiryFixed)
	if err != nil {
		t.Fatalf("CreateQualification(badge-fixed) failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE id = $1", badgeFixed.ID); err != nil {
			t.Errorf("cleaning up badge-fixed qualification failed: %v", err)
		}
	})

	res, err := svc.ListQualifications(ctx, admin)
	if err != nil {
		t.Fatalf("ListQualifications failed: %v", err)
	}
	statusByName := map[string]string{}
	for _, q := range res.Qualifications {
		statusByName[q.Name] = q.Status
	}
	if statusByName[created.Name] != core.QualificationStatusUnlimited {
		t.Errorf("unlimited status = %q, want %q", statusByName[created.Name], core.QualificationStatusUnlimited)
	}
	if statusByName[badgeFixedName] != core.QualificationStatusFixed {
		t.Errorf("fixed status = %q, want %q (vocabulary badge, no date)", statusByName[badgeFixedName], core.QualificationStatusFixed)
	}
	// The roster comes back so the assignment editor needs one round-trip.
	if len(res.Users) == 0 {
		t.Error("roster users is empty, want the user roster")
	}

	// UPDATE_DUP_NAME (finding 8): renaming onto a name held by ANOTHER
	// qualification maps to the uniform 409 conflict; renaming to the
	// qualification's OWN name (including a case variant) stays legal. The
	// `created` row still holds `qualName`; `fixed` was renamed to renamedName.
	otherName := qualName + ".other"
	if _, err := repo.CreateQualification(ctx, otherName, "Andere", core.QualificationExpiryUnlimited); err != nil {
		t.Fatalf("CreateQualification(other) failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE lower(name) = lower($1)", otherName); err != nil {
			t.Errorf("cleaning up other qualification failed: %v", err)
		}
	})
	if _, err := repo.UpdateQualification(ctx, created.ID, otherName, "", core.QualificationExpiryUnlimited); !errors.Is(err, core.ErrQualificationNameTaken) {
		t.Errorf("UpdateQualification(onto other's name) err = %v, want ErrQualificationNameTaken", err)
	}
	if _, err := repo.UpdateQualification(ctx, created.ID, strings.ToUpper(qualName), "", core.QualificationExpiryUnlimited); err != nil {
		t.Errorf("UpdateQualification(own name, case variant) failed: %v", err)
	}

	// UPDATE_UNKNOWN_TAKEN_NAME (finding 6): an unknown id whose requested name
	// is held by another qualification maps to the uniform 404 — never 409
	// (existence checked first inside the transaction).
	if _, err := repo.UpdateQualification(ctx, "00000000-0000-0000-0000-000000000004", otherName, "", core.QualificationExpiryUnlimited); !errors.Is(err, core.ErrQualificationNotFound) {
		t.Errorf("UpdateQualification(unknown id, taken name) err = %v, want ErrQualificationNotFound (never 409)", err)
	}
}