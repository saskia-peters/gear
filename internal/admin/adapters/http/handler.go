// Package http hosts the HTTP adapter of the Admin hexagon (Story 3.1 + 3.2):
// the SMTP-settings surface (GET/PUT /smtp, POST /smtp/test) and the
// backup-destination surface (GET/POST /backup, PUT/DELETE /{id}, POST
// /{id}/test) under /api/v1/admin/settings, each gated at the composition-root
// mount by RequireAnyPermission with its OWN permission code
// (admin.settings.email / admin.settings.backup — one permission per surface,
// AD-6). The core re-checks the permission defense-in-depth (AD-6). Later Epic
// 3 stories (DSGVO) add their surfaces here.
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/admin/ports"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

// Handler serves the Admin settings HTTP surface.
type Handler struct {
	service ports.Service
	logger  *slog.Logger
}

// NewHandler constructs the Admin settings HTTP handler. logger may be nil —
// the handlers never log through a nil logger (log() falls back to
// slog.Default()).
func NewHandler(service ports.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// log returns the configured logger or slog.Default() so a nil logger can
// never panic a handler.
func (h *Handler) log() *slog.Logger {
	if h.logger != nil {
		return h.logger
	}
	return slog.Default()
}

// Routes returns the Admin settings SMTP router (Story 3.1): GET/PUT /smtp
// and POST /smtp/test. The whole group is gated by `admin.settings.email` at
// the composition-root mount point (RequireAnyPermission), so this router
// carries no gateway itself; 404/405 answer with the uniform JSON envelope so
// no sub-path can emit a plain-text body.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/smtp", h.GetSmtpSettings)
	r.Put("/smtp", h.UpdateSmtpSettings)
	r.Post("/smtp/test", h.TestSmtpEmail)
	return r
}

// BackupRoutes returns the Admin settings backup-destination router (Story
// 3.2): GET/POST /, PUT/DELETE /{id} and POST /{id}/test. The whole group is
// gated by `admin.settings.backup` at the composition-root mount point — its
// OWN gate, one permission per surface (AD-6) — so this router carries no
// gateway itself; 404/405 answer with the uniform JSON envelope so no
// sub-path can emit a plain-text body.
func (h *Handler) BackupRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListBackupDestinations)
	r.Post("/", h.CreateBackupDestination)
	r.Put("/{id}", h.UpdateBackupDestination)
	r.Delete("/{id}", h.DeleteBackupDestination)
	r.Post("/{id}/test", h.TestBackupDestination)
	return r
}

// Compile-time check: the core Service satisfies the inbound port this adapter
// consumes.
var _ ports.Service = (*core.Service)(nil)