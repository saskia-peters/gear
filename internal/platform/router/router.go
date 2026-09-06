// Package router assembles the HTTP routing surface shared by the server
// composition root and its tests: the middleware chain, the JSON 404/405
// handlers, and the /healthz mount. The chi router emits plain-text bodies for
// unmatched routes and panics by default, so this package overrides them with
// the uniform JSON error envelope.
package router

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/health"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	appmw "github.com/saskia-peters/gear/internal/platform/middleware"
)

// Option allows customizing the mounted router (e.g. mounting module routes).
type Option func(*chi.Mux)

// WithAuth mounts the authentication and user directory routes at /api/v1/auth.
func WithAuth(h http.Handler) Option {
	return func(r *chi.Mux) {
		if h != nil {
			r.Mount("/api/v1/auth", h)
		}
	}
}

// WithProtected mounts a gateway-protected demo route at /api/v1/protected to
// prove the auth-gateway middleware (AD-2/AD-6) rejects unauthenticated and
// unauthorized callers with the uniform envelope.
func WithProtected(h http.Handler) Option {
	return func(r *chi.Mux) {
		if h != nil {
			r.Mount("/api/v1/protected", h)
		}
	}
}

// WithMount mounts an arbitrary http.Handler at the given path pattern. Used by
// the composition root for permission-gated sub-routes (e.g. the FR-27
// admin-recovery approve endpoint mounted behind RequirePermission).
func WithMount(pattern string, h http.Handler) Option {
	return func(r *chi.Mux) {
		if h != nil {
			r.Mount(pattern, h)
		}
	}
}

// New returns the mounted chi router. The panic-recovery middleware and the
// 404/405 handlers answer with the uniform JSON envelope (not plain text), and
// /healthz is wired to the given Pinger with the structured request logger.
func New(p health.Pinger, log *slog.Logger, opts ...Option) *chi.Mux {
	r := chi.NewRouter()
	r.Use(appmw.Recovery(log))
	r.Use(appmw.RequestLogger(log))
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/healthz", health.New(p, log))

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// compile-time check: *chi.Mux satisfies http.Handler.
var _ http.Handler = (*chi.Mux)(nil)
