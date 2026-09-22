package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// This file holds the Story 5-2b configurable system-settings handlers
// (AD-6/AD-11): GET /api/v1/admin/settings/system (all typed settings) and
// PUT /{key} (per-setting update). The group is gated by admin.settings.system
// at the composition-root mount; the core re-checks the code defense-in-depth.
// The server is authoritative for the typed values AND the German microcopy;
// unknown key / type mismatch / negative / empty-text answer the 400-class
// uniform envelope.

// systemSettingDTO is one GET/PUT payload row: the typed scalar `value`
// (duration → whole seconds as number, integer → number, text → string) plus
// the setting's display unit ("Sekunden", "Tage", "Zeichen", …) so the SPA can
// tell days from seconds (finding 11).
type systemSettingDTO struct {
	Key       string `json:"key"`
	ValueType string `json:"value_type"`
	Unit      string `json:"unit"`
	Value     any    `json:"value"`
}

// systemSettingWriteDTO adds the server-authoritative German confirmation.
type systemSettingWriteDTO struct {
	systemSettingDTO
	Message string `json:"message"`
}

// SystemRoutes returns the Admin settings system-settings router (Story 5-2b):
// GET / and PUT /{key}. The whole group is gated by `admin.settings.system` at
// the composition-root mount point — its OWN gate, one permission per surface
// (AD-6) — so this router carries no gateway itself; 404/405 answer with the
// uniform JSON envelope so no sub-path can emit a plain-text body.
func (h *Handler) SystemRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.GetSystemSettings)
	r.Put("/{key}", h.UpdateSystemSetting)
	return r
}

// GetSystemSettings handles GET /api/v1/admin/settings/system (GET_ALL): it
// returns every seeded setting typed (duration → seconds, integer → number,
// text → string).
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks admin.settings.system (gateway or
//     core re-check; no setting data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	settings, err := h.service.GetAppSettings(r.Context(), user.ID)
	if err != nil {
		h.mapSystemSettingError(w, r, err, user)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, appSettingsRows(settings))
}

// UpdateSystemSetting handles PUT /api/v1/admin/settings/system/{key}
// (PUT_SETTING / PUT_UNKNOWN / PUT_TYPE_MISMATCH / PUT_NEGATIVE / PUT_EMPTY_TEXT):
// it persists one setting's value and answers the updated row + German
// confirmation. Audited (admin.settings.system.update).
func (h *Handler) UpdateSystemSetting(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	key := chi.URLParam(r, "key")
	var input core.UpdateAppSettingInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	// Reject trailing content after the JSON object (e.g. `{"value":5} x`). The
	// check reuses the same buffered decoder — a fresh one over r.Body would
	// skip bytes the first decode already buffered.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	setting, err := h.service.UpdateAppSettings(r.Context(), user.ID, key, input)
	if err != nil {
		h.mapSystemSettingError(w, r, err, user)
		return
	}
	h.log().Info("system setting updated", "email", user.Email, "key", key)

	httpapi.WriteJSON(w, http.StatusOK, systemSettingWriteDTO{systemSettingDTO: toSystemSettingDTO(setting), Message: core.MsgAppSettingSaved})
}

// appSettingsRows projects the typed AppSettings struct onto wire rows in the
// catalog's canonical order (mirrors CurrentAppSettings' typed resolution).
// Keys the store did NOT resolve (missing/drifted rows) are skipped — the
// surface never invents a plausible zero for them.
func appSettingsRows(settings *core.AppSettings) []systemSettingDTO {
	rows := make([]systemSettingDTO, 0, len(core.AppSettingCatalogKeys()))
	for _, key := range core.AppSettingCatalogKeys() {
		row := core.AppSettingFor(settings, key)
		if row == nil {
			continue
		}
		rows = append(rows, toSystemSettingDTO(row))
	}
	return rows
}

// toSystemSettingDTO maps one domain setting row to the wire payload. A nil
// row (unresolved/unknown key) maps to an empty DTO so a service path that
// ever returns nil cannot panic the handler.
func toSystemSettingDTO(s *core.AppSetting) systemSettingDTO {
	if s == nil {
		return systemSettingDTO{}
	}
	return systemSettingDTO{
		Key:       s.Key,
		ValueType: s.ValueType,
		Unit:      s.Unit,
		Value:     s.Value(),
	}
}

// mapSystemSettingError writes the uniform envelope for the system-settings
// service's errors.
func (h *Handler) mapSystemSettingError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *core.InvalidAppSettingsError
	switch {
	case errors.Is(err, core.ErrForbidden):
		h.log().Warn("system settings access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, core.ErrAppSettingUnknown):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAppSettingUnknown)
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("system settings request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}