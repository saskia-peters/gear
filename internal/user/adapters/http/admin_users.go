package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// This file holds the Story 2.4 user-approval handlers (FR-20): the live
// "Ausstehende Anträge" surface on the Verwaltung landing. All three routes
// live under /api/v1/admin/users and are gated by `users.approve` (AD-6/FR-19)
// — the gateway at the users sub-mount already rejects callers without it with
// the uniform hidden-existence 403, so these handlers only ever see authorized
// callers. The core re-verifies the permission defense-in-depth.

// ListPendingUsers handles GET /api/v1/admin/users/pending (Story 2.4, FR-20):
// it returns the pending-approval requests, oldest first, with the submitted
// profile details (Vorname, Nachname, E-Mail) plus id and created_at. The
// client only displays; the list is server-authoritative. An empty list is an
// empty array so the client can show the "Keine ausstehenden Anträge" empty
// state.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListPendingUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	pending, err := h.service.ListPending(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("pending users list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("pending users list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if pending == nil {
		pending = []*core.PendingUser{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"users": pending})
}

// ApproveUser handles POST /api/v1/admin/users/{userID}/approve (Story 2.4,
// FR-20): it activates the pending user and seeds the default `helfende` role
// (atomic, AD-2). The approval is audited (user.approve, actor + target email,
// NFR-O1).
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 404 not_found when the id is unknown or the user is no longer pending —
//     one uniform message for both, so nothing beyond what the admin already
//     sees is leaked (FR-19)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ApproveUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	res, err := h.service.ApproveUser(r.Context(), user, userID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("user approve forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserNotPending):
			h.logger.Warn("user approve rejected: not pending", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserNotPending)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user approve failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("user approved", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// RejectUser handles POST /api/v1/admin/users/{userID}/reject (Story 2.4,
// FR-20): it moves the pending user to `deactivated` so the pending record
// disappears and the account can neither log in nor re-register. The rejection
// is audited (user.reject, actor + target email, NFR-O1).
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 404 not_found when the id is unknown or the user is no longer pending —
//     one uniform message for both (FR-19)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) RejectUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	res, err := h.service.RejectUser(r.Context(), user, userID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("user reject forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserNotPending):
			h.logger.Warn("user reject rejected: not pending", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserNotPending)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user reject failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("user rejected", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}