// Package http hosts the HTTP adapter of the Admin hexagon (Story 3.1): the
// SMTP-settings surface (GET/PUT /smtp, POST /smtp/test) under
// /api/v1/admin/settings, gated at the composition-root mount by
// RequireAnyPermission with `admin.settings.email`. The core re-checks the
// permission defense-in-depth (AD-6). Later Epic 3 stories (backup, DSGVO)
// add their surfaces here.
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

// Routes returns the Admin settings router (Story 3.1): GET/PUT /smtp and
// POST /smtp/test. The whole group is gated by `admin.settings.email` at the
// composition-root mount point (RequireAnyPermission), so this router carries
// no gateway itself; 404/405 answer with the uniform JSON envelope so no
// sub-path can emit a plain-text body.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/smtp", h.GetSmtpSettings)
	r.Put("/smtp", h.UpdateSmtpSettings)
	r.Post("/smtp/test", h.TestSmtpEmail)
	return r
}

// Compile-time check: the core Service satisfies the inbound port this adapter
// consumes.
var _ ports.Service = (*core.Service)(nil)