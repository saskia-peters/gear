package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// This file holds the Story 3.1 SMTP-settings handlers (FR-28/AD-6): GET/PUT
// /api/v1/admin/settings/smtp and POST /api/v1/admin/settings/smtp/test. The
// group is gated by `admin.settings.email` at the composition-root mount; the
// core re-checks the code defense-in-depth. The password is WRITE-ONLY: GET
// returns password_configured: bool, never the plaintext or ciphertext
// (NFR-S4).

// smtpSettingsDTO is the GET payload — the password is never serialized.
type smtpSettingsDTO struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Security           string `json:"security"`
	SenderAddress      string `json:"sender_address"`
	SenderName         string `json:"sender_name"`
	Username           string `json:"username"`
	PasswordConfigured bool   `json:"password_configured"`
}

// smtpSettingsWriteDTO adds the server-authoritative German confirmation.
type smtpSettingsWriteDTO struct {
	smtpSettingsDTO
	Message string `json:"message"`
}

// GetSmtpSettings handles GET /api/v1/admin/settings/smtp (GET_INITIAL /
// PUT_FORBIDDEN): it returns the current settings (zero defaults +
// password_configured: false when no row exists yet). The stored ciphertext
// is never part of the response.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks admin.settings.email (gateway or
//     core re-check; no settings data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) GetSmtpSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	settings, err := h.service.GetSmtpSettings(r.Context(), user.ID)
	if err != nil {
		h.mapSettingsError(w, r, err, user)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, toSmtpSettingsDTO(settings))
}

// UpdateSmtpSettings handles PUT /api/v1/admin/settings/smtp (PUT_VALID /
// PUT_NO_PASSWORD / PUT_INVALID / PUT_FORBIDDEN): it persists the settings
// atomically. A present password is encrypted at rest and replaces the stored
// one; an absent password keeps the existing ciphertext (write-only edit).
// Changes apply to subsequent sends immediately — no redeploy (FR-28). Audited
// (admin.settings.email.update).
func (h *Handler) UpdateSmtpSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	var input core.UpdateSmtpSettingsInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	settings, err := h.service.UpdateSmtpSettings(r.Context(), user.ID, input)
	if err != nil {
		h.mapSettingsError(w, r, err, user)
		return
	}
	h.log().Info("smtp settings updated", "email", user.Email, "host", settings.Host)

	dto := toSmtpSettingsDTO(settings)
	httpapi.WriteJSON(w, http.StatusOK, smtpSettingsWriteDTO{smtpSettingsDTO: dto, Message: core.MsgSmtpSettingsSaved})
}

// TestSmtpEmail handles POST /api/v1/admin/settings/smtp/test (TEST_OK /
// TEST_FAIL / DECRYPT_FAIL): it sends a test email to the acting admin's
// address through the configured server and returns the inline German result.
// SMTP failures answer a 200-style result {ok:false, message} (never a generic
// 5xx), are logged structured (NFR-O1) and audited
// (admin.settings.email.test).
func (h *Handler) TestSmtpEmail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.TestSmtpSettings(r.Context(), user.ID, user.Email)
	if err != nil {
		h.mapSettingsError(w, r, err, user)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// toSmtpSettingsDTO maps the domain settings to the wire payload, exposing
// only password_configured — never the ciphertext (NFR-S4).
func toSmtpSettingsDTO(s *core.SmtpSettings) smtpSettingsDTO {
	return smtpSettingsDTO{
		Host:               s.Host,
		Port:               s.Port,
		Security:           s.Security,
		SenderAddress:      s.SenderAddress,
		SenderName:         s.SenderName,
		Username:           s.Username,
		PasswordConfigured: s.PasswordConfigured(),
	}
}

// mapSettingsError writes the uniform envelope for the settings service's
// errors.
func (h *Handler) mapSettingsError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *core.InvalidSmtpSettingsError
	switch {
	case errors.Is(err, core.ErrForbidden):
		h.log().Warn("smtp settings access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("smtp settings request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}