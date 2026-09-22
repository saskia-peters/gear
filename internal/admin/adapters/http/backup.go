package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// This file holds the Story 3.2 backup-destination handlers (FR-29/AD-6):
// GET/POST /api/v1/admin/settings/backup, PUT/DELETE/POST /{id}/test. The
// group is gated by `admin.settings.backup` at the composition-root mount; the
// core re-checks the code defense-in-depth. The credential is WRITE-ONLY: GET
// returns credential_configured: bool per row, never the plaintext or
// ciphertext (NFR-S4).

// backupDestinationDTO is the GET payload — the credential is never serialized.
type backupDestinationDTO struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Mechanism            string `json:"mechanism"`
	Endpoint             string `json:"endpoint"`
	BucketOrPath         string `json:"bucket_or_path"`
	Username             string `json:"username"`
	CredentialConfigured bool   `json:"credential_configured"`
	Schedule             string `json:"schedule,omitempty"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// backupDestinationWriteDTO adds the server-authoritative German confirmation.
type backupDestinationWriteDTO struct {
	backupDestinationDTO
	Message string `json:"message"`
}

// ListBackupDestinations handles GET /api/v1/admin/settings/backup
// (GET_LIST_EMPTY / GET_LIST): it returns every destination, oldest first,
// with credential_configured per row. The stored ciphertext is never part of
// the response (NFR-S4).
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks admin.settings.backup (gateway or
//     core re-check; no destination data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListBackupDestinations(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	dests, err := h.service.ListBackupDestinations(r.Context(), user.ID)
	if err != nil {
		h.mapBackupError(w, r, err, user)
		return
	}
	out := make([]backupDestinationDTO, 0, len(dests))
	for _, d := range dests {
		out = append(out, toBackupDestinationDTO(d))
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// CreateBackupDestination handles POST /api/v1/admin/settings/backup
// (CREATE_VALID / CREATE_NO_CREDENTIAL / CREATE_INVALID): it persists a new
// destination. A present credential is encrypted at rest; S3/FTP/SFTP require
// a credential, local ones make it optional. Audited
// (admin.settings.backup.update).
func (h *Handler) CreateBackupDestination(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	var input core.BackupDestinationInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	dest, err := h.service.CreateBackupDestination(r.Context(), user.ID, input)
	if err != nil {
		h.mapBackupError(w, r, err, user)
		return
	}
	h.log().Info("backup destination created", "email", user.Email, "name", dest.Name, "mechanism", dest.Mechanism)

	httpapi.WriteJSON(w, http.StatusCreated, backupDestinationWriteDTO{backupDestinationDTO: toBackupDestinationDTO(dest), Message: core.MsgBackupDestinationSaved})
}

// UpdateBackupDestination handles PUT /api/v1/admin/settings/backup/{id}
// (UPDATE_KEEP_CREDENTIAL): it persists the destination atomically. A present
// credential is encrypted at rest and replaces the stored one; an absent one
// keeps the existing ciphertext (write-only edit, atomic COALESCE in the
// store's statement). Audited (admin.settings.backup.update).
func (h *Handler) UpdateBackupDestination(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input core.BackupDestinationInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	dest, err := h.service.UpdateBackupDestination(r.Context(), user.ID, id, input)
	if err != nil {
		h.mapBackupError(w, r, err, user)
		return
	}
	h.log().Info("backup destination updated", "email", user.Email, "id", id, "name", dest.Name, "mechanism", dest.Mechanism)

	httpapi.WriteJSON(w, http.StatusOK, backupDestinationWriteDTO{backupDestinationDTO: toBackupDestinationDTO(dest), Message: core.MsgBackupDestinationSaved})
}

// DeleteBackupDestination handles DELETE /api/v1/admin/settings/backup/{id}
// (DELETE): it removes one destination. Audited
// (admin.settings.backup.delete).
func (h *Handler) DeleteBackupDestination(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.service.DeleteBackupDestination(r.Context(), user.ID, id); err != nil {
		h.mapBackupError(w, r, err, user)
		return
	}
	h.log().Info("backup destination deleted", "email", user.Email, "id", id)

	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": core.MsgBackupDestinationDeleted})
}

// TestBackupDestination handles POST /api/v1/admin/settings/backup/{id}/test
// (TEST_OK / TEST_FAIL / TEST_DECRYPT_FAIL): it exercises the destination's
// mechanism/endpoint and returns the inline German result. Failures answer a
// 200-style result {ok:false, message} (never a generic 5xx), are logged
// structured (NFR-O1) and audited (admin.settings.backup.test).
func (h *Handler) TestBackupDestination(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	res, err := h.service.TestBackupDestination(r.Context(), user.ID, id)
	if err != nil {
		h.mapBackupError(w, r, err, user)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// toBackupDestinationDTO maps the domain destination to the wire payload,
// exposing only credential_configured — never the ciphertext (NFR-S4).
func toBackupDestinationDTO(d *core.BackupDestination) backupDestinationDTO {
	return backupDestinationDTO{
		ID:                   d.ID,
		Name:                 d.Name,
		Mechanism:            d.Mechanism,
		Endpoint:             d.Endpoint,
		BucketOrPath:         d.BucketOrPath,
		Username:             d.Username,
		CredentialConfigured: d.CredentialConfigured(),
		Schedule:             d.Schedule,
		CreatedAt:            d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            d.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// mapBackupError writes the uniform envelope for the backup service's errors.
func (h *Handler) mapBackupError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *core.InvalidBackupDestinationsError
	switch {
	case errors.Is(err, core.ErrForbidden):
		h.log().Warn("backup settings access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, core.ErrBackupDestinationNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", "Backup-Ziel wurde nicht gefunden.")
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("backup settings request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}