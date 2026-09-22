package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

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