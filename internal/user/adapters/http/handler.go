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
		r.Get("/me/permissions", h.MyPermissions)
		// The admin-recovery surface (request/approve/deny/pending, FR-27) is a
		// member of the isolated admin module group: it is mounted via
		// AdminRoutes at /api/v1/admin/recovery in the composition root, behind
		// RequireAdminPermission (review finding 2.1-1).
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
// returns a 200 with a deployment-wide anti-enumeration confirmation (UX-DR7):
// "Wenn deine E-Mail registriert ist, erhältst du einen Link." when SMTP is
// configured, or "Bitte kontaktiere deinen Administrator." when it is not. The
// message depends ONLY on SMTP config — never on the account — so account
// existence/state cannot be probed. The per-email rate gate (review finding
// 1.8-2) answers repeat requests for the same email with a uniform 429 that
// never depends on account existence. Only unparseable JSON is rejected with
// 400 invalid_request. Server-side: an active account gets a single-use hashed
// 30-min token (mailed via the sender port when configured) or is flagged
// must_change_password (SMTP not configured); every request is audited and
// logged (NFR-O1).
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

// MyPermissions handles GET /api/v1/auth/me/permissions (Story 2.2). It
// resolves the caller's live permission set server-side and returns it, so the
// SPA and other modules can inspect the server-authoritative set (AD-12). No
// caching — each request re-derives the set, so revocation is immediate
// (AD-2/AD-6/FR-21/FR-22). It is auth-gated via RequireAuth (any authenticated
// caller), never a client-supplied snapshot.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated (middleware) or
//     the request carries no usable session token
func (h *Handler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	perms, err := h.service.ResolvePermissionSet(r.Context(), user)
	if err != nil {
		// The client-abort guard: when the caller has gone away (request
		// context canceled) there is no one to answer — writing a spurious 500
		// and an error log on a canceled request is wrong.
		if r.Context().Err() != nil {
			return
		}
		h.logger.Error("permission resolution failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		return
	}
	if perms == nil {
		perms = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}