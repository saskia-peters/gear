// Package http hosts the HTTP adapter for the User Directory & Auth hexagon.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
	"github.com/saskia-peters/gear/internal/user/ports"
)

// Handler serves HTTP requests for authentication and user registration.
type Handler struct {
	service   ports.Service
	logger    *slog.Logger
	validator auth.SessionValidator
}

// NewHandler constructs a User HTTP handler. validator is used to authenticate
// the MFA management endpoints (which require an authenticated user).
func NewHandler(service ports.Service, logger *slog.Logger, validator auth.SessionValidator) *Handler {
	return &Handler{
		service:   service,
		logger:    logger,
		validator: validator,
	}
}

// Routes returns a chi.Router with all auth routes mounted. The MFA management
// routes require an authenticated bearer session.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Post("/password/forgot", h.ForgotPassword)
	r.Post("/password/reset", h.ResetPassword)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.validator))
		r.Get("/mfa/status", h.MFAStatus)
		r.Post("/mfa/enroll", h.MFAEnroll)
		r.Post("/mfa/disable", h.MFADisable)
		r.Post("/password/change", h.ChangePassword)
		r.Get("/profile", h.GetProfile)
		r.Post("/profile", h.UpdateProfile)
		r.Post("/profile/email", h.StageEmailChange)
		// Dual-admin credential recovery request (FR-27): auth-gated; the
		// approve route is mounted separately behind
		// RequirePermission("admin.recovery.approve") in the composition root.
		r.Post("/admin/recovery/request", h.AdminRecoveryRequest)
		// The deny and pending-list recovery routes are gated by
		// RequirePermission("admin.recovery.approve") at the mount level in the
		// composition root; here they are auth-gated only.
		r.Post("/admin/recovery/deny", h.AdminRecoveryDeny)
		r.Get("/admin/recovery/pending", h.AdminRecoveryPending)
	})
	return r
}

// Register handles POST /api/v1/auth/register.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var input core.RegisterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.Register(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrMissingFields):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgMissingFields)
		case errors.Is(err, core.ErrInvalidEmail):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgInvalidEmail)
		case errors.Is(err, core.ErrShortPassword):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgShortPassword)
		case errors.Is(err, core.ErrPasswordMismatch):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordMismatch)
		default:
			h.logger.Error("registration failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, res)
}

// Login handles POST /api/v1/auth/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var input core.LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.Login(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrLockedOut):
			// Progressive lockout (FR-3): the email is temporarily blocked. The
			// message stays generic (never reveal why), Retry-After lets the
			// client count down, and the trigger is emitted to structured
			// logging (NFR-O1). A bare ErrLockedOut without a *LockoutError
			// falls back to a sane 30s window.
			retryAfter, ok := core.LockoutRetryAfter(err)
			seconds := core.LockoutDefaultRetrySeconds
			if ok && retryAfter > 0 {
				seconds = int(retryAfter.Seconds())
				if seconds < 1 {
					seconds = 1
				}
			} else {
				h.logger.Warn("login lockout triggered without retry details", "error", err)
			}
			var lockoutErr *core.LockoutError
			email := ""
			if errors.As(err, &lockoutErr) {
				email = lockoutErr.Email
			}
			h.logger.Warn("login lockout triggered", "email", email, "retry_after", seconds)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			httpapi.WriteError(w, http.StatusTooManyRequests, "too_many_attempts",
				fmt.Sprintf("Zu viele Fehlversuche. Bitte warte %d Sekunden.", seconds))
		case errors.Is(err, core.ErrInvalidCredentials):
			// Anti-enumeration: identical response for every failure (UX-DR7).
			// Failed-login logging is UNIFORM (review finding 1.6-4): the same
			// event is emitted for a wrong password, an unknown email, a
			// non-active account and a failed TOTP challenge, so log readers
			// cannot tell which stage failed (NFR-O1).
			h.logger.Warn("login failed", "email", input.Email)
			httpapi.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "E-Mail oder Passwort ist falsch.")
		case errors.Is(err, core.ErrMFAUnavailable):
			// Encryption key missing/invalid/rotated during the TOTP step
			// (NFR-S4): a clear, distinct message is returned while the real
			// cause is logged (review finding 1.6-3).
			h.logger.Error("mfa unavailable during login", "error", err)
			httpapi.WriteError(w, http.StatusServiceUnavailable, "mfa_unavailable", "MFA ist derzeit nicht verfügbar.")
		case errors.Is(err, core.ErrInvalidLoginInput):
			// Oversized email/password rejected before the Argon2id verify.
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgInvalidLoginInput)
		default:
			h.logger.Error("login failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	if res.MFARequired {
		// Two-step login (FR-4): valid password but MFA is enabled and no TOTP
		// code yet — issue NO session, signal the challenge.
		h.logger.Info("mfa challenge issued", "email", input.Email)
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"mfa_required": true})
		return
	}

	if res.MustChangePassword {
		// LOGIN_MUST_CHANGE (FR-26): valid credentials but the account is
		// flagged for a mandatory password change (SMTP-not-configured fallback
		// / Epic 2 one-time password). NO app session is issued; the response
		// carries the single-use reset token that drives the forced change flow
		// (/reset-password/<token>) and a German note telling the user the
		// admins have been notified (review finding 1.8-4).
		h.logger.Info("must-change password signalled", "email", input.Email)
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"must_change_password": true,
			"reset_token":          res.ResetToken,
			"message":              core.MsgMustChangePassword,
		})
		return
	}

	// The challenge-success event only fires when MFA was actually involved
	// (review finding 1.6-11): a spurious totp_code on an account without MFA
	// must not be logged as an MFA challenge success.
	if input.TotpCode != "" && res.User.IsMFAEnabled {
		h.logger.Info("mfa challenge success", "email", input.Email)
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// Logout handles POST /api/v1/auth/logout. It invalidates the caller's
// session token server-side (NFR-S2). Logout is idempotent: it always returns
// 204, even for unknown/absent tokens.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := auth.BearerToken(r)
	if err := h.service.Logout(r.Context(), token); err != nil {
		h.logger.Error("logout failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// forgotPasswordRequest is the body of POST /api/v1/auth/password/forgot
// (FR-26): the account email.
type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword handles POST /api/v1/auth/password/forgot (FR-26). It ALWAYS
// returns the uniform 200 anti-enumeration confirmation
// "Wenn deine E-Mail registriert ist, erhältst du einen Link." (UX-DR7) — the
// body is identical whether the account exists, its state, and whether SMTP is
// configured — so account existence cannot be probed. The per-email rate gate
// (review finding 1.8-2) answers repeat requests for the same email with a
// uniform 429 that never depends on account existence. Only unparseable JSON is
// rejected with 400 invalid_request. Server-side: an active account gets a
// single-use hashed 30-min token (mailed via the sender port when configured)
// or is flagged must_change_password (SMTP not configured); every request is
// audited and logged (NFR-O1).
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.RequestPasswordReset(r.Context(), input.Email)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForgotThrottled):
			// Rate limited (review finding 1.8-2): a uniform 429 for the email
			// string regardless of account existence — like the login lockout, a
			// 429 is not discriminating (anti-enumeration).
			h.logger.Warn("password reset request throttled")
			httpapi.WriteError(w, http.StatusTooManyRequests, "too_many_attempts", "Zu viele Anfragen. Bitte warte einen Moment und versuche es erneut.")
		default:
			h.logger.Error("password reset request failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("password reset requested")
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// resetPasswordRequest is the body of POST /api/v1/auth/password/reset
// (FR-26): the single-use token plus the new password and its confirmation.
type resetPasswordRequest struct {
	Token              string `json:"token"`
	NewPassword        string `json:"new_password"`
	NewPasswordConfirm string `json:"new_password_confirm"`
}

// ResetPassword handles POST /api/v1/auth/password/reset (FR-26). A valid
// single-use token sets the new password (Argon2id, ≥10 chars FR-2), invalidates
// the token, clears must_change_password and revokes all sessions. Error mapping
// (uniform envelope): 400 invalid_token for an expired/used/unknown token;
// 400 invalid_request for a short/oversized/mismatched new password.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.CompletePasswordReset(r.Context(), input.Token, input.NewPassword, input.NewPasswordConfirm)
	if err != nil {
		// The completion endpoint is shared by the FR-26 forgot flow and the
		// FR-27 admin-recovery flow (review finding 1.8-13). A token that is not
		// a valid FR-26 forgot token may still be an APPROVED admin-recovery
		// token — the raw token admin B handed to admin A out-of-band. Try the
		// recovery completion before giving up.
		if errors.Is(err, core.ErrResetTokenInvalid) {
			if res2, err2 := h.service.CompleteAdminRecovery(r.Context(), input.Token, input.NewPassword, input.NewPasswordConfirm); err2 == nil {
				h.logger.Info("admin recovery completed via reset endpoint")
				httpapi.WriteJSON(w, http.StatusOK, res2)
				return
			} else if errors.Is(err2, core.ErrShortPassword) ||
				errors.Is(err2, core.ErrPasswordTooLong) ||
				errors.Is(err2, core.ErrPasswordMismatch) {
				// A policy-violating password from the recovery fallback must
				// surface its proper 400 (review finding 1.10), not be swallowed
				// into invalid_token. This only happens when the token was a
				// VALID approved recovery token (validation runs before
				// consumption), so mapping it here does not leak token validity.
				h.mapPasswordPolicyError(w, err2)
				return
			}
			h.logger.Warn("password reset rejected: invalid token")
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_token", core.MsgResetTokenInvalid)
			return
		}
		switch {
		case errors.Is(err, core.ErrShortPassword):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgShortPassword)
		case errors.Is(err, core.ErrPasswordTooLong):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordTooLong)
		case errors.Is(err, core.ErrPasswordMismatch):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordMismatch)
		default:
			h.logger.Error("password reset failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("password reset completed")
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// mapPasswordPolicyError writes the uniform 400 invalid_request envelope for a
// password-policy failure (short / too long / mismatched).
func (h *Handler) mapPasswordPolicyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrShortPassword):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgShortPassword)
	case errors.Is(err, core.ErrPasswordTooLong):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordTooLong)
	case errors.Is(err, core.ErrPasswordMismatch):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordMismatch)
	default:
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges Passwort.")
	}
}

// adminRecoveryRequestRequest is the body of POST
// /api/v1/auth/admin/recovery/request (FR-27): the target admin's email.
type adminRecoveryRequestRequest struct {
	Email string `json:"email"`
}

// AdminRecoveryRequest handles POST /api/v1/auth/admin/recovery/request
// (FR-27). It is auth-gated (RequireAuth): the caller must be an authenticated
// admin-group member (the requesting admin A, or any authenticated admin
// requesting on another admin's behalf). It creates a recovery-marked
// single-use hashed 30-min token for the target admin and returns a
// confirmation — the raw token is never returned to the requester; only the
// OTHER admin (B) can approve the request and obtain the deliverable token.
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
// /api/v1/auth/admin/recovery/approve (FR-27): the target admin's email, the
// mandatory Begründung (reason), the confirmation checkbox and — when the
// approving admin has MFA enabled — a current TOTP code (MFA step-up, review
// finding 1.10).
type adminRecoveryApproveRequest struct {
	Email     string `json:"email"`
	Reason    string `json:"reason"`
	Confirmed bool   `json:"confirmed"`
	TotpCode  string `json:"totp_code"`
}

// AdminRecoveryApprove handles POST /api/v1/auth/admin/recovery/approve
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
// /api/v1/auth/admin/recovery/deny (FR-27, review finding 1.10): the target
// admin's email and the mandatory Begründung.
type adminRecoveryDenyRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// AdminRecoveryDeny handles POST /api/v1/auth/admin/recovery/deny (FR-27,
// review finding 1.10). It is gated by RequirePermission("admin.recovery.approve")
// so the caller must be an authenticated admin with the recovery-approve
// permission. Denying invalidates the target's pending request and audits the
// deny with the reason in the operation detail.
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

// AdminRecoveryPending handles GET /api/v1/auth/admin/recovery/pending (FR-27,
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

// mfaEnrollRequest is the body of POST /api/v1/auth/mfa/enroll.
// With no code it is an enrollment REQUEST (returns secret + URI); with a code
// (plus the server-issued secret) it CONFIRMS and enables MFA (FR-4). The
// confirm step validates the code against the SERVER-persisted pending secret;
// a client-supplied secret that does not match it is rejected (review finding
// 1.6-1).
type mfaEnrollRequest struct {
	Code   string `json:"code,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// MFAStatus handles GET /api/v1/auth/mfa/status for an authenticated user. It
// reports whether MFA is currently enabled so the SPA can branch the settings
// surface and show the "MFA aktiv" indicator (UX-DR6).
func (h *Handler) MFAStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	enabled, err := h.service.MFAStatus(r.Context(), user)
	if err != nil {
		h.logger.Error("mfa status failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

// MFAEnroll handles POST /api/v1/auth/mfa/enroll for an authenticated user.
// - request (no code): returns a fresh shared secret + otpauth provisioning URI
// - confirm (code + secret): validates the 6-digit code against the server's
//   pending enrollment, then promotes the encrypted secret, enabling MFA
// The secret is shown once at request and stored encrypted at rest (NFR-S4).
func (h *Handler) MFAEnroll(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input mfaEnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	// Confirm step.
	if input.Code != "" {
		if err := h.service.ConfirmMFAEnable(r.Context(), user, input.Secret, input.Code); err != nil {
			switch {
			case errors.Is(err, core.ErrTOTPInvalid):
				h.logger.Warn("mfa enroll confirm failed", "email", user.Email)
				httpapi.WriteError(w, http.StatusBadRequest, "invalid_totp", "Der Bestätigungscode ist ungültig oder abgelaufen.")
			case errors.Is(err, core.ErrMFAEnrollmentExpired):
				h.logger.Warn("mfa enroll confirm expired", "email", user.Email)
				httpapi.WriteError(w, http.StatusBadRequest, "invalid_totp", "Die Aktivierung ist abgelaufen. Bitte starte die Aktivierung erneut.")
			case errors.Is(err, core.ErrMFAAlreadyEnabled):
				httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Zwei-Faktor-Authentifizierung ist bereits aktiviert.")
			case errors.Is(err, core.ErrMFAUnavailable):
				// Encryption key missing/invalid/rotated (NFR-S4): a clear,
				// distinct message is returned while the real cause is logged.
				h.logger.Error("mfa unavailable during enroll confirm", "error", err)
				httpapi.WriteError(w, http.StatusServiceUnavailable, "mfa_unavailable", "MFA ist derzeit nicht verfügbar.")
			default:
				h.logger.Error("mfa enroll confirm failed unexpectedly", "error", err)
				httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
			}
			return
		}
		h.logger.Info("mfa enroll confirmed", "email", user.Email)
		// Sessions issued before enrollment must not bypass the second factor
		// (review finding 1.6-2): the caller must re-authenticate with the new
		// TOTP code, so ALL sessions (including the current one) are revoked.
		if err := h.service.RevokeAllSessions(r.Context(), user.ID); err != nil {
			h.logger.Error("mfa enroll session revocation failed", "error", err)
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true})
		return
	}

	// Request step: generate a fresh secret + provisioning URI.
	res, err := h.service.EnrollMFARequest(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrMFAAlreadyEnabled):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Zwei-Faktor-Authentifizierung ist bereits aktiviert.")
		case errors.Is(err, core.ErrMFAUnavailable):
			h.logger.Error("mfa unavailable during enroll request", "error", err)
			httpapi.WriteError(w, http.StatusServiceUnavailable, "mfa_unavailable", "MFA ist derzeit nicht verfügbar.")
		default:
			h.logger.Error("mfa enroll request failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	h.logger.Info("mfa enroll requested", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// mfaDisableRequest is the body of POST /api/v1/auth/mfa/disable: the caller's
// current 6-digit TOTP code (FR-4).
type mfaDisableRequest struct {
	Code string `json:"code"`
}

// MFADisable handles POST /api/v1/auth/mfa/disable for an authenticated user.
// It requires a valid current TOTP code before clearing the stored secret.
func (h *Handler) MFADisable(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input mfaDisableRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	if err := h.service.DisableMFA(r.Context(), user, input.Code); err != nil {
		switch {
		case errors.Is(err, core.ErrTOTPInvalid):
			h.logger.Warn("mfa disable failed", "email", user.Email)
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_totp", "Der Bestätigungscode ist ungültig oder abgelaufen.")
		case errors.Is(err, core.ErrMFANotEnabled):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Zwei-Faktor-Authentifizierung ist nicht aktiviert.")
		case errors.Is(err, core.ErrMFAUnavailable):
			h.logger.Error("mfa unavailable during disable", "error", err)
			httpapi.WriteError(w, http.StatusServiceUnavailable, "mfa_unavailable", "MFA ist derzeit nicht verfügbar.")
		default:
			h.logger.Error("mfa disable failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	h.logger.Info("mfa disable succeeded", "email", user.Email)
	// After disabling MFA all pre-existing sessions must re-authenticate
	// (review finding 1.6-2): revoke every session of the user.
	if err := h.service.RevokeAllSessions(r.Context(), user.ID); err != nil {
		h.logger.Error("mfa disable session revocation failed", "error", err)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

// changePasswordRequest is the body of POST /api/v1/auth/password/change
// (FR-25): the current password plus the new password and its confirmation.
type changePasswordRequest struct {
	CurrentPassword    string `json:"current_password"`
	NewPassword        string `json:"new_password"`
	NewPasswordConfirm string `json:"new_password_confirm"`
}

// ChangePassword handles POST /api/v1/auth/password/change for an authenticated
// user (auth-gated via RequireAuth). It confirms the current password, stores
// the new Argon2id hash, revokes all OTHER sessions (the current session stays
// logged in, FR-25) and audits the change (NFR-O1/NFR-O2).
//
// Error mapping (uniform envelope):
//   - 400 invalid_request for a short (<10 chars, FR-2), oversized (>1024) or
//     mismatched new password
//   - 400 invalid_current_password for a wrong current password
//   - 400 invalid_request for a referenced user that no longer exists
//   - 401 unauthorized when the caller is not authenticated (middleware) or
//     the request carries no usable session token
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.ChangePassword(r.Context(), user, core.ChangePasswordInput{
		CurrentPassword:    input.CurrentPassword,
		NewPassword:        input.NewPassword,
		NewPasswordConfirm: input.NewPasswordConfirm,
	}, auth.BearerToken(r))
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidCredentials):
			// Missing/invalid session token or a nil user: never revoke ALL
			// sessions on an empty token (FR-25 current session stays logged in).
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		case errors.Is(err, core.ErrInvalidCurrentPassword):
			h.logger.Warn("password change rejected: wrong current password", "email", user.Email)
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_current_password", core.MsgInvalidCurrentPassword)
		case errors.Is(err, core.ErrShortPassword):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgShortPassword)
		case errors.Is(err, core.ErrPasswordTooLong):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordTooLong)
		case errors.Is(err, core.ErrPasswordMismatch):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgPasswordMismatch)
		case errors.Is(err, core.ErrUserNotFound):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Das Konto wurde nicht gefunden.")
		default:
			h.logger.Error("password change failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("password changed", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// GetProfile handles GET /api/v1/auth/profile for an authenticated user (Story
// 2.1). It returns the caller's base data (Vorname, Nachname, Anzeigename,
// E-Mail plus any staged pending_email) built from the authenticated session
// user — no DB round-trip needed.
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	profile, err := h.service.GetProfile(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("profile read failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, profile)
}

// UpdateProfile handles POST /api/v1/auth/profile for an authenticated user
// (Story 2.1). It validates and persists the caller's editable base data
// (Vorname, Nachname, Anzeigename) and returns the updated profile. Error
// mapping (uniform envelope): 400 invalid_request for missing/over-long
// fields or a vanished account; 401 unauthorized when the caller is not
// authenticated; 403 forbidden when an operation would target another user
// (defense-in-depth, self-ownership AD-12).
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input core.UpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	profile, err := h.service.UpdateProfile(r.Context(), user, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		case errors.Is(err, core.ErrMissingFields):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgMissingFields)
		case errors.Is(err, core.ErrProfileNameTooLong):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgProfileNameTooLong)
		case errors.Is(err, core.ErrInvalidAttributes):
			// ATTR_NOT_OBJECT / ATTR_BAD_KEY / ATTR_TOO_LARGE /
			// ATTR_INVALID_JSON (Story 1.9): invalid custom attributes map to a
			// uniform 400 invalid_request. The envelope carries machine-readable
			// `details` (the offending key + reason) so the client can act on
			// the specific failure.
			details := map[string]any{"reason": "invalid attributes"}
			var attrErr *core.AttributeError
			if errors.As(err, &attrErr) {
				details = map[string]any{"reason": attrErr.Reason}
				if attrErr.Key != "" {
					details["key"] = attrErr.Key
				}
			}
			httpapi.WriteErrorDetail(w, http.StatusBadRequest, "invalid_request", core.MsgInvalidAttributes, details)
		case errors.Is(err, core.ErrUserNotFound):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Das Konto wurde nicht gefunden.")
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		default:
			h.logger.Error("profile update failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("profile updated", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, profile)
}

// StageEmailChange handles POST /api/v1/auth/profile/email for an authenticated
// user (Story 2.1). It stages the submitted email as pending_email — the
// account stays ACTIVE on the current email until an admin approves the change
// (Epic 2 admin workflow) — and returns a German confirmation. Error mapping
// (uniform envelope): 400 invalid_request for a malformed email, a no-op
// (same as current) or an email already in use by another account; 401
// unauthorized when the caller is not authenticated; 403 forbidden when an
// operation would target another user (defense-in-depth, self-ownership
// AD-12).
func (h *Handler) StageEmailChange(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input core.StageEmailInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.StageEmailChange(r.Context(), user, input.Email)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		case errors.Is(err, core.ErrInvalidEmail):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgInvalidEmail)
		case errors.Is(err, core.ErrEmailUnchanged):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgEmailUnchanged)
		case errors.Is(err, core.ErrEmailAlreadyPending):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgEmailAlreadyPending)
		case errors.Is(err, core.ErrEmailInUse):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgEmailInUse)
		case errors.Is(err, core.ErrUserNotFound):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Das Konto wurde nicht gefunden.")
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		default:
			h.logger.Error("email change staging failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("email change staged", "email", user.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}
