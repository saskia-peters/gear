//go:build integration

package postgres

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/dbtest"
	"github.com/saskia-peters/gear/internal/user/core"
)

// This file pins the COLD-START SEED (NFR-R2, AD-12/AD-13): the 4 base roles,
// the full permission catalog, the role→permission base matrix, and the two
// seeded admin accounts. It runs against a FRESH migrated schema via dbtest
// (Story 7.4) — never the pre-seeded dev DB — and asserts through the REAL
// repository read path a fresh deployment would exercise. Gated behind
// `//go:build integration`; run with `just test-integration` (and in CI).

// baseRoleMatrix is the documented AD-12 base-role series (architecture-spine
// "Base role matrix" + the later additions tool.edit / user.account.approve /
// admin.settings.system). A base role's EXACT permission set is asserted, so a
// missing OR an unexpected grant both fail.
var baseRoleMatrix = map[string][]string{
	"helfende": {"dashboard.view", "inspection.submit"},
	"schirrmeister": {
		"dashboard.view",
		"inspection.submit",
		"tools.manage",
		"tool_types.manage",
		"tool.edit",
		"inspection.history.view",
		"users.view",
		"users.qualifications.manage",
	},
	"fuehrende": {
		"dashboard.view",
		"inspection.submit",
		"tools.manage",
		"tool_types.manage",
		"tool.edit",
		"inspection.history.view",
		"users.view",
		"users.qualifications.manage",
		"report.export",
		"tool.reinstate",
	},
	"admin": {
		"dashboard.view",
		"inspection.submit",
		"tools.manage",
		"tool_types.manage",
		"tool.edit",
		"inspection.history.view",
		"report.export",
		"tool.reinstate",
		"users.view",
		"users.qualifications.manage",
		"users.approve",
		"users.manage",
		"user_groups.manage",
		"roles.create",
		"roles.edit",
		"roles.assign",
		"qualifications.manage",
		"dsgvo.access_report",
		"dsgvo.delete",
		"admin.recovery.approve",
		"admin.settings.email",
		"admin.settings.backup",
		"schedules.manage",
		"user.account.approve",
		"admin.settings.system",
	},
}

// expectedPermissionCodes is the INDEPENDENT full catalog the seed must
// contain (NOT derived from baseRoleMatrix, so a permission dropped from BOTH
// the seed and the matrix cannot pass silently). Derived from the migrations'
// permission INSERTs (000001/000010/000013/000016/000025/000026/000028).
var expectedPermissionCodes = []string{
	"admin.recovery.approve",
	"admin.settings.backup",
	"admin.settings.email",
	"admin.settings.system",
	"dashboard.view",
	"dsgvo.access_report",
	"dsgvo.delete",
	"inspection.history.view",
	"inspection.submit",
	"qualifications.manage",
	"report.export",
	"roles.assign",
	"roles.create",
	"roles.edit",
	"schedules.manage",
	"tool.edit",
	"tool.reinstate",
	"tool_types.manage",
	"tools.manage",
	"user.account.approve",
	"user_groups.manage",
	"users.approve",
	"users.manage",
	"users.qualifications.manage",
	"users.view",
}

// adminEmails are the two seeded admin accounts (FR-27/AD-13).
var adminEmails = []string{"admin.1@gear.local", "admin.2@gear.local"}

func TestIntegrationSeedBaseRoles(t *testing.T) {
	pool := dbtest.Open(t, "gear_test_seed")
	ctx := context.Background()
	repo := NewRepository(New(pool))

	groups, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}

	// Exactly 4 base roles, each matching its documented name, no duplicates.
	base := map[string]*core.RoleGroup{}
	for _, g := range groups {
		if !g.IsBaseRole {
			continue
		}
		if _, dup := base[g.Name]; dup {
			t.Errorf("duplicate base role %q", g.Name)
		}
		base[g.Name] = g
	}
	for _, want := range []string{"helfende", "schirrmeister", "fuehrende", "admin"} {
		if _, ok := base[want]; !ok {
			t.Errorf("base role %q missing from the seed (found: %v)", want, slices.Sorted(maps.Keys(base)))
		}
	}
	if len(base) != 4 {
		t.Errorf("base roles = %d, want exactly 4 (found: %v)", len(base), slices.Sorted(maps.Keys(base)))
	}

	// Each base role's EXACT permission set matches the AD-12 matrix.
	for name, want := range baseRoleMatrix {
		g, ok := base[name]
		if !ok {
			continue // already reported above
		}
		got := slices.Sorted(slices.Values(g.Permissions))
		wantSorted := slices.Sorted(slices.Values(want))
		if !slices.Equal(got, wantSorted) {
			t.Errorf("base role %q permissions = %v, want %v (AD-12 matrix)", name, got, wantSorted)
		}
	}
}

func TestIntegrationSeedPermissionCatalog(t *testing.T) {
	pool := dbtest.Open(t, "gear_test_seed")
	ctx := context.Background()
	repo := NewRepository(New(pool))

	catalog, err := repo.ListAllPermissions(ctx)
	if err != nil {
		t.Fatalf("ListAllPermissions: %v", err)
	}

	// The catalog must contain EXACTLY the independent expected set — no more,
	// no fewer — so a dropped or an unexpected permission both fail.
	got := map[string]bool{}
	for _, p := range catalog {
		got[p.Code] = true
		if strings.TrimSpace(p.Label) == "" {
			t.Errorf("permission %q has a blank label", p.Code)
		}
	}
	want := map[string]bool{}
	for _, code := range expectedPermissionCodes {
		want[code] = true
		if !got[code] {
			t.Errorf("permission %q missing from the catalog", code)
		}
	}
	for code := range got {
		if !want[code] {
			t.Errorf("permission %q present but not in the expected catalog (seed drift)", code)
		}
	}
	if len(got) != len(want) {
		t.Errorf("permission catalog = %d entries, want %d", len(got), len(want))
	}
}

func TestIntegrationSeedAdminAccounts(t *testing.T) {
	pool := dbtest.Open(t, "gear_test_seed")
	ctx := context.Background()
	repo := NewRepository(New(pool))

	users, err := repo.ListUsers(ctx, nil)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	byEmail := map[string]*core.AdminUserSummary{}
	for _, u := range users {
		byEmail[u.Email] = u
	}
	for _, want := range adminEmails {
		u, ok := byEmail[want]
		if !ok {
			t.Errorf("seeded admin %q missing", want)
			continue
		}
		if u.Status != string(core.StateActive) {
			t.Errorf("admin %q status = %q, want active (AD-13)", want, u.Status)
		}
		// Both admins hold the admin base role (FR-27/AD-13).
		detail, err := repo.GetUserDetail(ctx, u.ID)
		if err != nil {
			t.Fatalf("GetUserDetail(%q): %v", want, err)
		}
		hasAdmin := false
		for _, r := range detail.Roles {
			if r.Name == "admin" && r.IsBaseRole {
				hasAdmin = true
			}
		}
		if !hasAdmin {
			t.Errorf("admin %q does not hold the admin base role (roles: %+v)", want, detail.Roles)
		}
	}

	// EXACTLY 2 admins: on the fresh seed schema the account list must be the
	// two seeded admins and no more (FR-27 dual-admin, AD-13). An extra seeded
	// admin would otherwise ship undetected.
	adminHolders := 0
	for _, u := range users {
		detail, err := repo.GetUserDetail(ctx, u.ID)
		if err != nil {
			t.Fatalf("GetUserDetail(%q): %v", u.Email, err)
		}
		for _, r := range detail.Roles {
			if r.Name == "admin" && r.IsBaseRole {
				adminHolders++
			}
		}
	}
	if adminHolders != len(adminEmails) {
		t.Errorf("admin-role holders = %d, want exactly %d (FR-27 dual-admin)", adminHolders, len(adminEmails))
	}
}