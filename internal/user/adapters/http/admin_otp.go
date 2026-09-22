package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// This file holds the admin one-time-password issuance handler (Spec 2.8,
// FR-26 Epic 2). It lives under /api/v1/admin/users/{userID}/otp on the users
// sub-mount (gated by any `users.*` code); the action itself requires
// `users.manage`, re-verified in the core defense-in-depth (AD-2/AD-6).

// issueOneTimePasswordRequest is the body of POST /users/{userID}/otp (Spec
// 2.8): the mandatory confirmation checkbox — issuing a recovery credential is
// sensitive and must be explicit.
type issueOneTimePasswordRequest struct {
	Confirmed bool `json:"confirmed"`
}

// IssueOneTimePasswordHandler handles POST /api/v1/admin/users/{userID}/otp
// (Spec 2.8): it generates a single-use, hashed, expiring one-time password for
// an ACTIVE account and returns the plaintext value EXACTLY once (there is no
// readback/recovery endpoint). The account is flagged must_change_password, so
// the next login with the OTP runs the forced-change flow (Story 1.8) instead
// of issuing an app session. Gated by `users.manage` at the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.manage`
//   - 404 not_found when the id is unknown or malformed
//   - 409 conflict when the target is not active (deactivated or
//     pending_approval) — one uniform conflict message, OTPs are active-only
//   - 400 invalid_request when the confirmation is missing or the JSON is
//     malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) IssueOneTimePasswordHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	var input issueOneTimePasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.IssueOneTimePassword(r.Context(), user, userID, input.Confirmed)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("user one-time-password issue forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			h.logger.Warn("user one-time-password issue rejected: unknown id", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrOneTimePasswordTargetNotEligible):
			h.logger.Warn("user one-time-password issue rejected: not active", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgOneTimePasswordTargetNotEligible)
		case errors.Is(err, core.ErrOneTimePasswordNotConfirmed):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgOneTimePasswordNotConfirmed)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user one-time-password issue failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("user one-time-password issued", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}