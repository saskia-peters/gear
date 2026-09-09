package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// adminRecoveryRequestRequest is the body of POST
// /api/v1/admin/recovery/request (FR-27): the target admin's email.
type adminRecoveryRequestRequest struct {
	Email string `json:"email"`
}

// AdminRecoveryRequest handles POST /api/v1/admin/recovery/request
// (FR-27). It is admin-gated via the isolated admin module group
// (RequireAdminPermission): the caller must be an authenticated admin-group
// member (the requesting admin A, or any authenticated admin requesting on
// another admin's behalf). It creates a recovery-marked single-use hashed
// 30-min token for the target admin and returns a confirmation — the raw token
// is never returned to the requester; only the OTHER admin (B) can approve the
// request and obtain the deliverable token.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated (middleware)
//   - 403 forbidden when the caller is not an admin-group member
//   - 400 invalid_request for a missing target email
//   - 400 last_admin_recovery_blocked when the target is the last active admin
func (h *Handler) AdminRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input adminRecoveryRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.RequestAdminRecovery(r.Context(), user, input.Email)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin recovery request forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrMissingFields):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgMissingFields)
		case errors.Is(err, core.ErrLastAdminRecoveryBlocked):
			h.logger.Warn("admin recovery request blocked: last admin", "email", user.Email)
			httpapi.WriteError(w, http.StatusBadRequest, "last_admin_recovery_blocked", core.MsgLastAdminRecoveryBlocked)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin recovery request failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin recovery requested", "email", user.Email, "target", res.TargetEmail)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// adminRecoveryApproveRequest is the body of POST
// /api/v1/admin/recovery/approve (FR-27): the target admin's email, the
// mandatory Begründung (reason), the confirmation checkbox and — when the
// approving admin has MFA enabled — a current TOTP code (MFA step-up, review
// finding 1.10).
type adminRecoveryApproveRequest struct {
	Email     string `json:"email"`
	Reason    string `json:"reason"`
	Confirmed bool   `json:"confirmed"`
	TotpCode  string `json:"totp_code"`
}

// AdminRecoveryApprove handles POST /api/v1/admin/recovery/approve
// (FR-27). It is gated by RequirePermission("admin.recovery.approve") so the
// caller must be an authenticated admin with the recovery-approve permission
// (the seeded admin group carries it). The approving admin (B) must be a
// DIFFERENT admin than the target AND than the requester, must supply a
// mandatory Begründung and must confirm via the checkbox; when B has MFA
// enabled a valid TOTP code is required (step-up). On approval the raw
// single-use token is returned to B to hand to the recovered admin (A)
// out-of-band.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks the permission (middleware), attempts
//     a self-approval, or the caller is no longer an active authorized admin
//   - 403 recovery_mfa_required when the approver has MFA enabled and did not
//     supply a valid TOTP code
//   - 400 invalid_request for a missing Begründung or an unchecked confirmation
//   - 400 invalid_token when the target has no approvable pending request
//   - 400 last_admin_recovery_blocked when the target is the last active admin
func (h *Handler) AdminRecoveryApprove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input adminRecoveryApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.ApproveAdminRecovery(r.Context(), user, input.Email, input.Reason, input.Confirmed, input.TotpCode)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin recovery approve forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrRecoveryMFARequired):
			h.logger.Warn("admin recovery approve requires MFA code", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "recovery_mfa_required", core.MsgRecoveryMFARequired)
		case errors.Is(err, core.ErrRecoveryReasonRequired):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRecoveryReasonRequired)
		case errors.Is(err, core.ErrRecoveryNotConfirmed):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRecoveryNotConfirmed)
		case errors.Is(err, core.ErrMissingFields):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgMissingFields)
		case errors.Is(err, core.ErrAdminRecoveryInvalid):
			h.logger.Warn("admin recovery approval rejected: invalid target")
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_token", core.MsgAdminRecoveryInvalid)
		case errors.Is(err, core.ErrLastAdminRecoveryBlocked):
			h.logger.Warn("admin recovery approve blocked: last admin", "email", user.Email)
			httpapi.WriteError(w, http.StatusBadRequest, "last_admin_recovery_blocked", core.MsgLastAdminRecoveryBlocked)
		default:
			h.logger.Error("admin recovery approval failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin recovery approved", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// adminRecoveryDenyRequest is the body of POST
// /api/v1/admin/recovery/deny (FR-27, review finding 1.10): the target
// admin's email and the mandatory Begründung.
type adminRecoveryDenyRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// AdminRecoveryDeny handles POST /api/v1/admin/recovery/deny (FR-27,
// review finding 1.10). The core `DenyAdminRecovery` re-verifies the
// `user.account.approve` permission defense-in-depth (retro finding F11) — a
// caller admitted by the outer admin-module gate who lacks it is denied, so a
// fuehrende/schirrmeister with only `users.view` cannot invalidate a pending
// recovery request. Denying invalidates the target's pending request and
// audits the deny with the reason in the operation detail.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks the permission (middleware) or
//     attempts a self-deny
//   - 400 invalid_request for a missing Begründung
//   - 400 invalid_token when the target has no pending request
func (h *Handler) AdminRecoveryDeny(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input adminRecoveryDenyRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.DenyAdminRecovery(r.Context(), user, input.Email, input.Reason)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin recovery deny forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrRecoveryDenyReasonRequired):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRecoveryDenyReasonRequired)
		case errors.Is(err, core.ErrMissingFields):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgMissingFields)
		case errors.Is(err, core.ErrAdminRecoveryInvalid):
			h.logger.Warn("admin recovery deny rejected: invalid target")
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_token", core.MsgAdminRecoveryInvalid)
		default:
			h.logger.Error("admin recovery deny failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin recovery denied", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// AdminRecoveryPending handles GET /api/v1/admin/recovery/pending (FR-27,
// review finding 1.10): it lists the pending (not-yet-approved) recovery
// requests for the admin-B review surface. It is gated by
// RequirePermission("admin.recovery.approve").
func (h *Handler) AdminRecoveryPending(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.ListAdminRecoveryRequest(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin recovery list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin recovery list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if res == nil {
		res = []*core.AdminRecoveryRequest{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"requests": res})
}