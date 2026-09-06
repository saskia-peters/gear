package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/router"
	userpostgres "github.com/saskia-peters/gear/internal/user/adapters/postgres"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newCompositionRouter builds the REAL composition-root wiring (AD-1): postgres
// Repository + SessionManager + the auth gateway mounted via router.WithProtected
// requiring admin.recovery.approve. This pins the wiring that is otherwise only
// exercised manually via curl. The admin module's composition wiring is covered
// on the REAL handler in internal/user/adapters/http (this package cannot
// import it — import cycle).
func newCompositionRouter(t *testing.T) (http.Handler, *userpostgres.Repository, *usercore.SessionManager, *pgxpool.Pool) {
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
	protected := Route(sm, repo, protectedCode)
	r := router.New(pool, discardLogger(),
		router.WithProtected(protected),
	)

	return r, repo, sm, pool
}

func doComposedRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestComposedAuthFlow(t *testing.T) {
	r, repo, sm, pool := newCompositionRouter(t)
	ctx := context.Background()

	// 401: no token.
	if rec := doComposedRequest(r, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 401: invalid/unknown token.
	if rec := doComposedRequest(r, "garbage-token"); rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: status = %d, want 401", rec.Code)
	}

	// 403: an authenticated active caller WITHOUT admin.recovery.approve.
	email := fmt.Sprintf("authflow.%s@gear.local", time.Now().Format("20060102150405.000000"))
	volunteer, err := repo.CreateRegisteredUser(ctx, email, "Flow Test", "Flow", "Test", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$8U3f5yO8JUpfGT5WmljHhL8n2nWlVEhL2fj7EXpS9gM")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET state = 'active' WHERE id = $1", volunteer.ID); err != nil {
		t.Fatalf("activating volunteer failed: %v", err)
	}
	volunteer.State = usercore.StateActive

	volunteerToken, err := sm.Issue(ctx, volunteer)
	if err != nil {
		t.Fatalf("Issue volunteer session failed: %v", err)
	}
	if rec := doComposedRequest(r, volunteerToken); rec.Code != http.StatusForbidden {
		t.Errorf("volunteer without permission: status = %d, want 403", rec.Code)
	}

	// 200: an authorized caller — the seeded admin holds admin.recovery.approve.
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err != nil {
		t.Fatalf("GetUserByEmail(admin) failed: %v", err)
	}
	if admin == nil {
		t.Skip("seeded admin not present — skipping authorized-caller assertion")
	}
	adminToken, err := sm.Issue(ctx, admin)
	if err != nil {
		t.Fatalf("Issue admin session failed: %v", err)
	}
	if rec := doComposedRequest(r, adminToken); rec.Code != http.StatusOK {
		t.Errorf("admin with permission: status = %d, want 200", rec.Code)
	}

	// 401: expired token re-checked against the stored expires_at (AD-2).
	expiredToken, err := sm.Issue(ctx, admin)
	if err != nil {
		t.Fatalf("Issue session for expiry test failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE user_id = $1", admin.ID); err != nil {
		t.Fatalf("backdating session failed: %v", err)
	}
	if rec := doComposedRequest(r, expiredToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token: status = %d, want 401", rec.Code)
	}

	// Cleanup: drop the volunteer (sessions cascade) and any session rows the
	// test created so the dev DB is not polluted.
	start := time.Now().UTC().Add(-time.Minute)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM sessions WHERE created_at >= $1", start)
	})
}
