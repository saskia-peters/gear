package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

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