package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

// AdminRoutes returns the isolated admin-module router (Story 2.1, review
// finding 2.1-1): the admin-status root plus the admin-recovery surface (FR-27)
// mounted at /recovery — the whole admin module lives in ONE URL space
// (/api/v1/admin). The gateway itself is applied at the mount point in the
// composition root (RequireAdminPermission with an admin-only permission), so
// this router only carries admin surfaces. The 404/405 responders answer with
// the uniform JSON envelope so no admin sub-path can ever emit a plain-text
// body.
func (h *Handler) AdminRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.adminStatus)
	r.Post("/recovery/request", h.AdminRecoveryRequest)
	r.Post("/recovery/approve", h.AdminRecoveryApprove)
	r.Post("/recovery/deny", h.AdminRecoveryDeny)
	r.Get("/recovery/pending", h.AdminRecoveryPending)
	return r
}

// adminStatus is the minimal admin root handler proving the /api/v1/admin
// gateway isolation (Story 2.1): a health-style admin-status payload, reachable
// only by an admin.
func (h *Handler) adminStatus(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"module": "admin", "status": "ok"})
}