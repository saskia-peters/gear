package core

import (
	"context"
	"errors"
	"testing"
)

// basePermissionCodes is the AD-12 base series the seed migration installs:
// admin = all 21, the base roles = the matrix subsets (Story 2.2).
func basePermissionCodes() []string {
	return []string{
		"admin.recovery.approve",
		"admin.settings.backup",
		"admin.settings.email",
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
		"tool.reinstate",
		"tool_types.manage",
		"tools.manage",
		"user_groups.manage",
		"users.approve",
		"users.manage",
		"users.view",
	}
}

// helfendeSet is the helfende role matrix (AD-12): dashboard.view +
// inspection.submit.
var helfendeSet = []string{"dashboard.view", "inspection.submit"}

func resolveSet(t *testing.T, svc *Service, repo *mockRepo, userID string) []string {
	t.Helper()
	perms, err := svc.ResolvePermissionSet(context.Background(), &User{ID: userID})
	if err != nil {
		t.Fatalf("ResolvePermissionSet(%q) failed: %v", userID, err)
	}
	return perms
}

func assertSameCodes(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("resolved %d codes %v, want %d codes %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved codes %v, want %v (mismatch at index %d)", got, want, i)
		}
	}
}

// RESOLVE_ADMIN (I/O matrix): an admin-group user resolves all 22 base codes.
func TestResolvePermissionSetAdminResolvesAll21(t *testing.T) {
	repo := newMockRepo()
	repo.perms["admin-1"] = basePermissionCodes()
	svc, _ := newTestService(repo, &mockHasher{})

	assertSameCodes(t, resolveSet(t, svc, repo, "admin-1"), basePermissionCodes())
}

// RESOLVE_HELFENDE (I/O matrix): a helfende-role user resolves exactly
// dashboard.view + inspection.submit.
func TestResolvePermissionSetHelfendeSubset(t *testing.T) {
	repo := newMockRepo()
	repo.perms["helfende-1"] = helfendeSet
	svc, _ := newTestService(repo, &mockHasher{})

	assertSameCodes(t, resolveSet(t, svc, repo, "helfende-1"), helfendeSet)
}

// RESOLVE_MULTI_ROLE (I/O matrix): membership in helfende + schirrmeister
// resolves the additive union of both matrices (no precedence, AD-12).
func TestResolvePermissionSetMultiRoleUnion(t *testing.T) {
	repo := newMockRepo()
	repo.perms["multi-1"] = []string{
		"dashboard.view",
		"inspection.submit",
		"tool_types.manage",
		"tools.manage",
	}
	svc, _ := newTestService(repo, &mockHasher{})

	assertSameCodes(t, resolveSet(t, svc, repo, "multi-1"), []string{
		"dashboard.view",
		"inspection.submit",
		"tool_types.manage",
		"tools.manage",
	})
}

// RESOLVE_DIRECT_GRANT (I/O matrix): a direct grant (user_permissions) is
// added to the union of the role membership.
func TestResolvePermissionSetDirectGrantAddedToUnion(t *testing.T) {
	repo := newMockRepo()
	repo.perms["direct-1"] = []string{"dashboard.view", "inspection.submit", "report.export"}
	svc, _ := newTestService(repo, &mockHasher{})

	assertSameCodes(t, resolveSet(t, svc, repo, "direct-1"), []string{
		"dashboard.view",
		"inspection.submit",
		"report.export",
	})
}

// RESOLVE_NO_PERM (I/O matrix): a user with no roles or grants resolves the
// empty set.
func TestResolvePermissionSetEmpty(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	perms, err := svc.ResolvePermissionSet(context.Background(), &User{ID: "nobody-1"})
	if err != nil {
		t.Fatalf("ResolvePermissionSet(nobody) failed: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("resolved %d codes %v, want the empty set", len(perms), perms)
	}
}

// REVOCATION_IMMEDIATE (I/O matrix): a permission removed from the backing
// store is reflected on the very NEXT resolution — the service never caches
// across requests (AD-2/AD-6/FR-21/FR-22).
func TestResolvePermissionSetRevocationImmediate(t *testing.T) {
	repo := newMockRepo()
	repo.perms["revoke-1"] = []string{"dashboard.view", "inspection.submit", "report.export"}
	svc, _ := newTestService(repo, &mockHasher{})

	assertSameCodes(t, resolveSet(t, svc, repo, "revoke-1"), []string{
		"dashboard.view",
		"inspection.submit",
		"report.export",
	})

	// Remove a code from the backing store: the next resolution reflects it.
	repo.perms["revoke-1"] = []string{"dashboard.view", "inspection.submit"}
	assertSameCodes(t, resolveSet(t, svc, repo, "revoke-1"), []string{
		"dashboard.view",
		"inspection.submit",
	})
}

// A nil user is rejected up front (defense-in-depth; the handler guards the
// same boundary).
func TestResolvePermissionSetNilUserRejected(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	if _, err := svc.ResolvePermissionSet(context.Background(), nil); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// A user with an EMPTY ID is rejected too: it must never query the repo with
// "" and silently resolve an empty set.
func TestResolvePermissionSetEmptyIDRejected(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	if _, err := svc.ResolvePermissionSet(context.Background(), &User{Email: "a@example.com"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for an empty user ID", err)
	}
}

// A backing-store failure propagates as a wrapped error (never a silent empty
// set).
func TestResolvePermissionSetStoreErrorPropagates(t *testing.T) {
	repo := newMockRepo()
	repo.permsErr = errors.New("db down")
	svc, _ := newTestService(repo, &mockHasher{})

	if _, err := svc.ResolvePermissionSet(context.Background(), &User{ID: "u-1"}); err == nil {
		t.Fatal("expected a wrapped store error, got nil")
	}
}