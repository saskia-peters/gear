// Package auth provides the server-side authorization gateway (AD-2/AD-6):
// a chi middleware that validates a caller's bearer session token, resolves
// the caller's live permission set (AD-12) on every request — never a cached
// snapshot — and enforces a required permission code, answering with the
// uniform JSON error envelope (401 unauthorized / 403 forbidden).
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// SessionValidator validates a raw session token and returns the owning user.
type SessionValidator interface {
	Validate(ctx context.Context, rawToken string) (*core.Session, error)
}

// PermissionResolver resolves a user's live permission set (AD-12).
type PermissionResolver interface {
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
}

type userContextKey struct{}

// WithUser returns a context carrying the authenticated user.
func WithUser(ctx context.Context, user *core.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFrom returns the authenticated user stored in the request context, or
// nil when absent.
func UserFrom(ctx context.Context) *core.User {
	u, _ := ctx.Value(userContextKey{}).(*core.User)
	return u
}

// RequirePermission is the auth-gateway middleware (AD-2/AD-6). It validates
// the bearer token, re-derives the caller's live permission set and requires
// the given permission code. Missing/invalid/expired tokens return 401;
// authenticated callers lacking the permission return 403.
func RequirePermission(validator SessionValidator, resolver PermissionResolver, required string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := authenticate(w, r, validator)
			if !ok {
				return
			}
			perms, err := resolver.ListPermissionsByUser(r.Context(), user.ID)
			if err != nil {
				httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
				return
			}

			if !hasPermission(perms, required) {
				// FR-19 existence-hiding: no disclosure of what the caller lacks.
				httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// RequireAuth is an authentication-only gateway middleware (AD-2/AD-6): it
// validates the bearer token and stores the owning user in the context, but
// imposes no permission requirement (unlike RequirePermission). It is used for
// endpoints any authenticated user may call, such as MFA management (FR-4).
func RequireAuth(validator SessionValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := authenticate(w, r, validator)
			if !ok {
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// authenticate is the single shared token/session resolution for every gateway
// middleware (review findings 1.6-8 / 1.6-9): it extracts the bearer token,
// validates it and guards against a nil session (a validator returning a nil
// session with a nil error must not panic). On any failure it writes the
// uniform 401 envelope and reports ok=false.
func authenticate(w http.ResponseWriter, r *http.Request, validator SessionValidator) (*core.User, bool) {
	token := BearerToken(r)
	if token == "" {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return nil, false
	}
	sess, err := validator.Validate(r.Context(), token)
	if err != nil || sess == nil || sess.User == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return nil, false
	}
	return sess.User, true
}

// BearerToken extracts the bearer token from the Authorization header. It is
// the single shared parser for the auth gateway and the user HTTP handlers.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) <= len(bearerPrefix) || !strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(h[len(bearerPrefix):])
}

const bearerPrefix = "Bearer "

func hasPermission(perms []string, required string) bool {
	for _, p := range perms {
		if p == required {
			return true
		}
	}
	return false
}

// Route returns a small chi router used to demonstrate the gateway on a
// protected demo endpoint. It is mounted by the composition root.
func Route(validator SessionValidator, resolver PermissionResolver, required string) http.Handler {
	r := chi.NewRouter()
	r.Use(RequirePermission(validator, resolver, required))
	r.Get("/me", func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"first_name":   u.FirstName,
			"last_name":    u.LastName,
			"state":        string(u.State),
		})
	})
	return r
}

// RouteApproval returns a chi router gated by RequirePermission that POSTs the
// given handler at its root. It is mounted by the composition root at the
// admin-recovery approve endpoint (FR-27), so only a caller with the
// `admin.recovery.approve` permission can approve a recovery request.
func RouteApproval(validator SessionValidator, resolver PermissionResolver, required string, h http.HandlerFunc) http.Handler {
	r := chi.NewRouter()
	r.Use(RequirePermission(validator, resolver, required))
	r.Post("/", h)
	return r
}

// RouteAdminRecovery returns a chi router gated by RequirePermission that
// mounts the admin-recovery management surface (FR-27, review finding 1.10):
// POST /approve, POST /deny and GET /pending, each behind the
// `admin.recovery.approve` permission. It is mounted by the composition root.
func RouteAdminRecovery(validator SessionValidator, resolver PermissionResolver, required string, approve, deny http.HandlerFunc, pending http.HandlerFunc) http.Handler {
	r := chi.NewRouter()
	r.Use(RequirePermission(validator, resolver, required))
	r.Post("/approve", approve)
	r.Post("/deny", deny)
	r.Get("/pending", pending)
	return r
}
