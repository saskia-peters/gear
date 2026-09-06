package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/platform/router"
	userpostgres "github.com/saskia-peters/gear/internal/user/adapters/postgres"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

const argon2DummyHash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM"

// newComposedAdminRouter builds the REAL composition-root wiring for the admin
// module (AD-1, review finding 2.1-4): postgres Repository + SessionManager +
// Service + the real AdminRoutes() mounted at /api/v1/admin behind
// RequireAdminPermission (admin-only permission), exactly as cmd/server/main.go
// does. A generic protected demo route is mounted too so hidden-existence tests
// can compare admin 403s byte-for-byte against a generic forbidden response.
func newComposedAdminRouter(t *testing.T, log *slog.Logger) (http.Handler, *userpostgres.Repository, *usercore.SessionManager, *pgxpool.Pool) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := userpostgres.NewRepository(userpostgres.New(pool))
	sm := usercore.NewSessionManager(repo, time.Hour)
	svc := usercore.NewService(repo, crypto.NewHasher(), sm, crypto.NewSecretCipher(make([]byte, 32)), log)
	h := NewHandler(svc, log, sm)
	adminSurface := auth.RequireAdminPermission(sm, repo, adminModulePermission, log)(h.AdminRoutes())
	r := router.New(pool, log,
		router.WithProtected(auth.Route(sm, repo, adminModulePermission)),
		router.WithMount("/api/v1/admin", adminSurface),
	)
	return r, repo, sm, pool
}

func doComposedAdminGET(h http.Handler, token, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doComposedAdminPOST(h http.Handler, token, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// createActiveUser registers a FRESH user, activates it and returns the user
// plus a live session token. Cleanup is registered IMMEDIATELY (review finding
// 2.1-7) — before any t.Skip or later failure — so the row never leaks into the
// shared dev DB. The cleanup deletes ONLY this user (sessions and recovery
// tokens cascade on user delete), so it can never touch rows owned by OTHER
// test binaries that share the database concurrently (the postgres adapter
// suite). grantAdmin additionally attaches the admin role.
func createActiveUser(t *testing.T, pool *pgxpool.Pool, repo *userpostgres.Repository, sm *usercore.SessionManager, email, display, first, last string, grantAdmin bool) (*usercore.User, string) {
	t.Helper()
	ctx := context.Background()
	user, err := repo.CreateRegisteredUser(ctx, email, display, first, last, argon2DummyHash)
	if err != nil {
		t.Fatalf("CreateRegisteredUser(%s) failed: %v", email, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})
	if _, err := pool.Exec(ctx, "UPDATE users SET state = 'active' WHERE id = $1", user.ID); err != nil {
		t.Fatalf("activating %s failed: %v", email, err)
	}
	user.State = usercore.StateActive
	if grantAdmin {
		if _, err := pool.Exec(ctx, "INSERT INTO user_permission_groups (user_id, permission_group_id) SELECT $1, g.id FROM permission_groups g WHERE g.name = 'admin'", user.ID); err != nil {
			t.Fatalf("granting admin role to %s failed: %v", email, err)
		}
	}
	token, err := sm.Issue(ctx, user)
	if err != nil {
		t.Fatalf("Issue session for %s failed: %v", email, err)
	}
	return user, token
}

func TestComposedAdminRouteGroup(t *testing.T) {
	r, repo, sm, pool := newComposedAdminRouter(t, discardLogger())

	// 401: no token.
	if rec := doComposedAdminGET(r, "", "/api/v1/admin"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	volunteerEmail := fmt.Sprintf("admcomp.vol.%s@gear.local", time.Now().Format("20060102150405.000000"))
	_, volunteerToken := createActiveUser(t, pool, repo, sm, volunteerEmail, "Comp Vol", "Comp", "Vol", false)

	// 403 + hidden existence on the root: the uniform envelope, byte-identical
	// to a generic forbidden route and carrying no admin hint (FR-19, review
	// finding 2.1-8c).
	rec := doComposedAdminGET(r, volunteerToken, "/api/v1/admin")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("volunteer: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding forbidden envelope failed: %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("forbidden code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
	genericRec := doComposedAdminGET(r, volunteerToken, "/api/v1/protected/me")
	if genericRec.Code != http.StatusForbidden {
		t.Fatalf("generic protected route status = %d, want 403", genericRec.Code)
	}
	if genericRec.Body.String() != rec.Body.String() {
		t.Errorf("admin 403 body differs from the generic forbidden body\nadmin:   %s\ngeneric: %s",
			rec.Body.String(), genericRec.Body.String())
	}

	// 403 + hidden existence on a NESTED admin sub-path (review finding 2.1-8b):
	// the gateway runs before routing, so a non-admin never reaches the admin
	// surface or a 404 that would hint at it.
	nestedRec := doComposedAdminGET(r, volunteerToken, "/api/v1/admin/recovery/pending")
	if nestedRec.Code != http.StatusForbidden {
		t.Errorf("nested sub-path: status = %d, want 403 (body %s)", nestedRec.Code, nestedRec.Body.String())
	}
	if strings.Contains(strings.ToLower(nestedRec.Body.String()), "admin") {
		t.Errorf("nested 403 body hints at the admin module: %s", nestedRec.Body.String())
	}

	// 200: a genuine admin (FRESH user — the seeded admins are concurrently used
	// by the postgres adapter suite) resolves the admin permission via the live
	// permission set (AD-12) and reaches the real admin-status surface.
	adminEmail := fmt.Sprintf("admcomp.admin.%s@gear.local", time.Now().Format("20060102150405.000000"))
	admin, adminToken := createActiveUser(t, pool, repo, sm, adminEmail, "Comp Admin", "Comp", "Admin", true)
	rec = doComposedAdminGET(r, adminToken, "/api/v1/admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire["module"] != "admin" || wire["status"] != "ok" {
		t.Errorf("admin status payload = %v, want module=admin status=ok", wire)
	}

	// Revocation is IMMEDIATE (AD-2/FR-21): after removing the admin role, the
	// SAME session token is denied 403 on the next request. The fresh admin's
	// role revocation never touches the seeded admins.
	if _, err := pool.Exec(context.Background(), "DELETE FROM user_permission_groups upg USING permission_groups g WHERE upg.permission_group_id = g.id AND g.name = 'admin' AND upg.user_id = $1", admin.ID); err != nil {
		t.Fatalf("revoking admin role failed: %v", err)
	}
	if rec := doComposedAdminGET(r, adminToken, "/api/v1/admin"); rec.Code != http.StatusForbidden {
		t.Errorf("revoked admin: status = %d, want 403", rec.Code)
	}
}

func TestComposedAdminForbiddenStructuredLogged(t *testing.T) {
	// NFR-O1 (review finding 2.1-5): an admin-route denial emits BOTH the
	// router-level request log line AND a denial-specific structured line
	// (message, caller email, path, required permission) — distinct from the
	// generic request log.
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	r, repo, sm, pool := newComposedAdminRouter(t, log)

	volunteerEmail := fmt.Sprintf("admlog.vol.%s@gear.local", time.Now().Format("20060102150405.000000"))
	_, volunteerToken := createActiveUser(t, pool, repo, sm, volunteerEmail, "Log Vol", "Log", "Vol", false)

	if rec := doComposedAdminGET(r, volunteerToken, "/api/v1/admin"); rec.Code != http.StatusForbidden {
		t.Fatalf("volunteer: status = %d, want 403", rec.Code)
	}

	out := buf.String()
	if !strings.Contains(out, "status=403") {
		t.Errorf("structured request log missing the 403 line: %s", out)
	}
	if !strings.Contains(out, "admin access denied") {
		t.Errorf("denial-specific log missing 'admin access denied': %s", out)
	}
	if !strings.Contains(out, "path=/api/v1/admin") {
		t.Errorf("denial log missing the admin path: %s", out)
	}
	if !strings.Contains(out, "permission_required=admin.recovery.approve") {
		t.Errorf("denial log missing the required permission: %s", out)
	}
	if !strings.Contains(out, volunteerEmail) {
		t.Errorf("denial log missing the caller email: %s", out)
	}
}

func TestComposedAdminRecoveryRoutesReachable(t *testing.T) {
	// Review finding 2.1-1: the admin-recovery surface is a member of the
	// isolated /api/v1/admin group. Through the REAL handler + REAL wiring the
	// request/approve/deny/pending flows are reachable at the new URLs and still
	// work (FR-27), while a non-admin is denied 403 at the group gateway. All
	// participants are FRESH admins so the seeded admins (concurrently used by
	// the postgres adapter suite) are never touched.
	r, repo, sm, pool := newComposedAdminRouter(t, discardLogger())
	stamp := time.Now().Format("20060102150405.000000")

	requesterEmail := fmt.Sprintf("admcomp.req.%s@gear.local", stamp)
	_, requesterToken := createActiveUser(t, pool, repo, sm, requesterEmail, "Requester", "Req", "Admin", true)
	targetEmail := fmt.Sprintf("admcomp.target.%s@gear.local", stamp)
	_, _ = createActiveUser(t, pool, repo, sm, targetEmail, "Target", "Tgt", "Admin", true)
	approverEmail := fmt.Sprintf("admcomp.appr.%s@gear.local", stamp)
	_, approverToken := createActiveUser(t, pool, repo, sm, approverEmail, "Approver", "Appr", "Admin", true)
	volunteerEmail := fmt.Sprintf("admcomp.reqvol.%s@gear.local", stamp)
	_, volunteerToken := createActiveUser(t, pool, repo, sm, volunteerEmail, "Req Vol", "Req", "Vol", false)

	// pending: the route is reachable for an admin (200). The pending list is a
	// shared, global surface (concurrent postgres-suite tests may hold pending
	// requests for the seeded admins), so only the HTTP contract is asserted
	// here; the target-specific assertions below are what prove this test's own
	// request shows up.
	rec := doComposedAdminGET(r, requesterToken, "/api/v1/admin/recovery/pending")
	if rec.Code != http.StatusOK {
		t.Fatalf("pending: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// request: the requester (A) requests recovery for the target (T).
	requestBody := []byte(fmt.Sprintf(`{"email":%q}`, targetEmail))
	rec = doComposedAdminPOST(r, requesterToken, "/api/v1/admin/recovery/request", requestBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("request: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// The target now appears in the pending list (this test's own request).
	rec = doComposedAdminGET(r, requesterToken, "/api/v1/admin/recovery/pending")
	if rec.Code != http.StatusOK {
		t.Fatalf("pending after request: status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), targetEmail) {
		t.Errorf("pending list missing the target %s: %s", targetEmail, rec.Body.String())
	}

	// deny: the independent approver B (neither requester nor target) denies T.
	denyBody := []byte(fmt.Sprintf(`{"email":%q,"reason":"unberechtigt"}`, targetEmail))
	rec = doComposedAdminPOST(r, approverToken, "/api/v1/admin/recovery/deny", denyBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("deny: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// request again, then approve: B approves T's fresh request and receives the
	// single-use token (the ONLY caller who may see it).
	rec = doComposedAdminPOST(r, requesterToken, "/api/v1/admin/recovery/request", requestBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("request (2nd): status = %d, want 200", rec.Code)
	}
	approveBody := []byte(fmt.Sprintf(`{"email":%q,"reason":"Ausgesperrt","confirmed":true}`, targetEmail))
	rec = doComposedAdminPOST(r, approverToken, "/api/v1/admin/recovery/approve", approveBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var approveRes struct {
		RecoveryToken string `json:"recovery_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &approveRes); err != nil {
		t.Fatalf("decoding approve response failed: %v", err)
	}
	if approveRes.RecoveryToken == "" {
		t.Errorf("approve returned an empty recovery token")
	}

	// A non-admin never reaches any recovery route: 403 with no admin hint.
	rec = doComposedAdminPOST(r, volunteerToken, "/api/v1/admin/recovery/request", requestBody)
	if rec.Code != http.StatusForbidden {
		t.Errorf("volunteer request: status = %d, want 403", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("volunteer 403 body hints at the admin module: %s", rec.Body.String())
	}
}