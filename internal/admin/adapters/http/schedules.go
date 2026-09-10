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

// This file holds the Story 4.1 schedule-catalog handlers (FR-30/AD-6): GET/
// POST /api/v1/admin/settings/schedules, PUT /{id} and POST /{id}/archive. The
// group is gated by `schedules.manage` at the composition-root mount; the core
// re-checks the code defense-in-depth. Archive is SOFT (archived_at set) — no
// DELETE endpoint, and editing/archiving an already-archived row answers the
// uniform 404 sentinel.

// scheduleDTO is the GET payload — archived schedules are never returned by
// the active surface, and the reserved weekday/time fields stay server-side.
type scheduleDTO struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	IntervalUnit      string `json:"interval_unit"`
	IntervalMagnitude int    `json:"interval_magnitude"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// scheduleWriteDTO adds the server-authoritative German confirmation.
type scheduleWriteDTO struct {
	scheduleDTO
	Message string `json:"message"`
}

// ListSchedules handles GET /api/v1/admin/settings/schedules
// (GET_LIST_EMPTY / GET_LIST): it returns the ACTIVE catalog, oldest first.
// Archived rows never appear.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks schedules.manage (gateway or core
//     re-check; no schedule data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	schedules, err := h.service.ListSchedules(r.Context(), user.ID)
	if err != nil {
		h.mapScheduleError(w, r, err, user)
		return
	}
	out := make([]scheduleDTO, 0, len(schedules))
	for _, s := range schedules {
		out = append(out, toScheduleDTO(s))
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// CreateSchedule handles POST /api/v1/admin/settings/schedules
// (CREATE_VALID / CREATE_DUPLICATE / CREATE_INVALID): it persists a new
// schedule (name + interval unit/magnitude). Audited (schedule.create).
func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	var input core.ScheduleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	schedule, err := h.service.CreateSchedule(r.Context(), user.ID, input)
	if err != nil {
		h.mapScheduleError(w, r, err, user)
		return
	}
	h.log().Info("schedule created", "email", user.Email, "name", schedule.Name, "interval", schedule.IntervalUnit)

	httpapi.WriteJSON(w, http.StatusCreated, scheduleWriteDTO{scheduleDTO: toScheduleDTO(schedule), Message: core.MsgScheduleSaved})
}

// UpdateSchedule handles PUT /api/v1/admin/settings/schedules/{id}
// (UPDATE_VALID): it persists the schedule's name/interval. Updating an
// already-archived schedule answers the 404 sentinel (UPDATE_ARCHIVED).
// Audited (schedule.update).
func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input core.ScheduleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	schedule, err := h.service.UpdateSchedule(r.Context(), user.ID, id, input)
	if err != nil {
		h.mapScheduleError(w, r, err, user)
		return
	}
	h.log().Info("schedule updated", "email", user.Email, "id", id, "name", schedule.Name, "interval", schedule.IntervalUnit)

	httpapi.WriteJSON(w, http.StatusOK, scheduleWriteDTO{scheduleDTO: toScheduleDTO(schedule), Message: core.MsgScheduleSaved})
}

// ArchiveSchedule handles POST /api/v1/admin/settings/schedules/{id}/archive
// (ARCHIVE / ARCHIVE_ARCHIVED): it soft-archives the schedule — archived_at is
// set and the row leaves the active list. Archiving an already-archived row
// answers the 404 sentinel. Audited (schedule.archive).
func (h *Handler) ArchiveSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	schedule, err := h.service.ArchiveSchedule(r.Context(), user.ID, id)
	if err != nil {
		h.mapScheduleError(w, r, err, user)
		return
	}
	h.log().Info("schedule archived", "email", user.Email, "id", id, "name", schedule.Name)

	httpapi.WriteJSON(w, http.StatusOK, scheduleWriteDTO{scheduleDTO: toScheduleDTO(schedule), Message: core.MsgScheduleArchived})
}

// toScheduleDTO maps the domain schedule to the wire payload. The reserved
// weekday_set/time_of_day fields are never exposed (unused in V1), and the
// active surface never carries an archived row.
func toScheduleDTO(s *core.Schedule) scheduleDTO {
	return scheduleDTO{
		ID:                s.ID,
		Name:              s.Name,
		IntervalUnit:      s.IntervalUnit,
		IntervalMagnitude: s.IntervalMagnitude,
		CreatedAt:         s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// mapScheduleError writes the uniform envelope for the schedule service's
// errors.
func (h *Handler) mapScheduleError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *core.InvalidSchedulesError
	switch {
	case errors.Is(err, core.ErrForbidden):
		h.log().Warn("schedule access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, core.ErrScheduleNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", "Zeitplan wurde nicht gefunden.")
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("schedule request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}
