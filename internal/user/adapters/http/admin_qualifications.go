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

// This file holds the Story 2.7 Qualification Management handlers (FR-19/
// FR-22/AD-7): the vocabulary list/create/update and the per-qualification
// assignee surface. All routes live under /api/v1/admin/qualifications.
//
// The qualifications sub-mount is gated by `qualifications.manage`
// (AD-6/FR-19) — a caller without it gets the uniform hidden-existence 403, so
// these handlers only ever see authorized callers.

// ListAdminQualifications handles GET /api/v1/admin/qualifications (Story 2.7):
// it returns every qualification with its server-derived status indicator
// (Unbegrenzt / Gültig / Bald ablaufend / Abgelaufen, FR-22/AD-7) plus the full
// user roster (id, display name) so the assignment editor can pick assignees in
// one round-trip. An empty vocabulary is an empty array.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `qualifications.manage` (gateway,
//     hidden existence)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListAdminQualifications(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.ListQualifications(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin qualifications list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin qualifications list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if res.Qualifications == nil {
		res.Qualifications = []*core.QualificationWithStatus{}
	}
	if res.Users == nil {
		res.Users = []*core.QualificationRosterUser{}
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// CreateAdminQualification handles POST /api/v1/admin/qualifications (Story
// 2.7): it creates a qualification with a unique (case-insensitive) name and
// either no expiry (unbegrenzt gültig) or a fixed validity period. The response
// carries the server-authoritative German confirmation plus the created
// qualification with its derived status.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `qualifications.manage`
//   - 409 conflict when the name is already taken (case-insensitive)
//   - 400 invalid_request when the name is empty/too long, the description is
//     too long, the expiry kind is invalid, a fixed qualification lacks a
//     future expires_at, or the JSON is malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) CreateAdminQualification(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	var input core.CreateQualificationInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	q, err := h.service.CreateQualification(r.Context(), user, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin qualification create forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrQualificationNameTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgQualificationNameTaken)
		case errors.Is(err, core.ErrQualificationInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationNameRequired)
		case errors.Is(err, core.ErrQualificationDescriptionTooLong):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationDescriptionTooLong)
		case errors.Is(err, core.ErrQualificationInvalidExpiryKind), errors.Is(err, core.ErrQualificationInvalidExpiresAt):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationInvalidExpiry)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin qualification create failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin qualification created", "email", user.Email, "qualification", q.Qualification.Name)
	httpapi.WriteJSON(w, http.StatusCreated, q)
}

// UpdateAdminQualification handles PUT /api/v1/admin/qualifications/{id}
// (Story 2.7): it replaces the qualification's name/description/expiry model
// atomically. Editing the expiry model never rewrites existing assignments —
// each assignment inherits the qualification's current expiry model on read, so
// every assignment's status changes immediately (live resolution, AD-7/FR-22).
// The response carries the server-authoritative German confirmation plus the
// replaced qualification.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `qualifications.manage`
//   - 404 not_found when the id is unknown or malformed
//   - 409 conflict when the name is already taken by ANOTHER qualification
//   - 400 invalid_request when the name is empty/too long, the description is
//     too long, the expiry kind is invalid, a fixed qualification lacks a
//     future expires_at, or the JSON is malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) UpdateAdminQualification(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	id := chi.URLParam(r, "id")
	var input core.UpdateQualificationInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	q, err := h.service.UpdateQualification(r.Context(), user, id, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin qualification update forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrQualificationNotFound):
			h.logger.Warn("admin qualification update rejected: unknown id", "email", user.Email, "target", id)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationNotFound)
		case errors.Is(err, core.ErrQualificationNameTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgQualificationNameTaken)
		case errors.Is(err, core.ErrQualificationInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationNameRequired)
		case errors.Is(err, core.ErrQualificationDescriptionTooLong):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationDescriptionTooLong)
		case errors.Is(err, core.ErrQualificationInvalidExpiryKind), errors.Is(err, core.ErrQualificationInvalidExpiresAt):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationInvalidExpiry)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin qualification update failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin qualification updated", "email", user.Email, "qualification", q.Qualification.Name)
	httpapi.WriteJSON(w, http.StatusOK, q)
}

// ListAdminQualificationAssignees handles GET
// /api/v1/admin/qualifications/{id}/assignees (Story 2.7): it returns the
// current assignees (id + display name) of a qualification, so the assignment
// editor can pre-check the set. An empty set is an empty array.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `qualifications.manage`
//   - 404 not_found when the id is unknown or malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListAdminQualificationAssignees(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	id := chi.URLParam(r, "id")

	assignees, err := h.service.ListQualificationAssignees(r.Context(), user, id)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin qualification assignees list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrQualificationNotFound):
			h.logger.Warn("admin qualification assignees list rejected: unknown id", "email", user.Email, "target", id)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin qualification assignees list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if assignees == nil {
		assignees = []*core.QualificationAssignee{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"assignees": assignees})
}

// assignAdminQualificationUsersRequest is the body of POST
// /qualifications/{id}/assignees (Story 2.7): the full replacement assignee set.
// A MISSING `user_ids` field (nil) is a malformed request — only an explicit
// empty array means "clear everyone".
type assignAdminQualificationUsersRequest struct {
	UserIDs []string `json:"user_ids"`
}

// AssignAdminQualificationUsers handles POST
// /api/v1/admin/qualifications/{id}/assignees (Story 2.7): it REPLACES the
// qualification's assignee set atomically (delete-then-insert in one
// transaction). An explicit empty set clears every assignee; a MISSING
// `user_ids` field is rejected (a malformed request must never silently revoke
// every volunteer). Removing a volunteer revokes eligibility immediately
// (AD-7/FR-22) because resolution is live per request.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `qualifications.manage`
//   - 404 not_found when the qualification id is unknown or malformed
//   - 400 invalid when an assignee user id is unknown
//   - 400 invalid_request when the JSON is malformed or `user_ids` is missing
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) AssignAdminQualificationUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	id := chi.URLParam(r, "id")
	var input assignAdminQualificationUsersRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	if input.UserIDs == nil {
		// Finding (review): a missing `user_ids` field must not be treated as
		// "clear everyone" — only an explicit empty array is a valid clear-all.
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationAssigneeRequired)
		return
	}

	res, err := h.service.AssignQualificationUsers(r.Context(), user, id, input.UserIDs)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin qualification assign forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrQualificationNotFound):
			h.logger.Warn("admin qualification assign rejected: unknown id", "email", user.Email, "target", id)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationNotFound)
		case errors.Is(err, core.ErrQualificationAssigneeUnknown):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgQualificationAssigneeUnknown)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin qualification assign failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin qualification assignees assigned", "email", user.Email, "qualification", id, "assignees", len(input.UserIDs))
	httpapi.WriteJSON(w, http.StatusOK, res)
}