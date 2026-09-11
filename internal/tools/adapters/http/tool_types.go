// Package http hosts the HTTP adapter of the Tool hexagon (Story 4.2): the
// tool-type surface (GET/POST /, PUT /{id}, POST /{id}/archive) under
// /api/v1/admin/tool-types, gated at the composition-root mount by
// RequireAnyPermission with ITS OWN permission code (tool_types.manage — one
// permission per surface, AD-6). The core re-checks the permission
// defense-in-depth (AD-6). Later Epic 4 stories (tools, attributes) add their
// surfaces here.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// toolTypeChecklistItemDTO is one ordered checklist item of the GET payload.
type toolTypeChecklistItemDTO struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Label    string `json:"label"`
}

// toolTypeDTO is the GET payload — the core typed fields plus the ordered
// checklist items. The attributes jsonb extension surface is NOT exposed in V1
// (Story 4.4 owns it); archived types never reach the active surface.
type toolTypeDTO struct {
	ID                      string                     `json:"id"`
	Name                    string                     `json:"name"`
	DefaultScheduleID       string                     `json:"default_schedule_id"`
	RequiredQualificationID string                     `json:"required_qualification_id"`
	InspectionMode          string                     `json:"inspection_mode"`
	ChecklistItems          []toolTypeChecklistItemDTO `json:"checklist_items"`
	CreatedAt               string                     `json:"created_at"`
	UpdatedAt               string                     `json:"updated_at"`
}

// toolTypeWriteDTO adds the server-authoritative German confirmation.
type toolTypeWriteDTO struct {
	toolTypeDTO
	Message string `json:"message"`
}

// Handler serves the Tool HTTP surface.
type Handler struct {
	service toolports.Service
	logger  *slog.Logger
}

// NewHandler constructs the Tool HTTP handler. logger may be nil — the handlers
// never log through a nil logger (log() falls back to slog.Default()).
func NewHandler(service toolports.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// log returns the configured logger or slog.Default() so a nil logger can never
// panic a handler.
func (h *Handler) log() *slog.Logger {
	if h.logger != nil {
		return h.logger
	}
	return slog.Default()
}

// ToolTypeRoutes returns the Tool tool-type router (Story 4.2, FR-8/FR-23):
// GET/POST / and PUT /{id}, POST /{id}/archive — soft archive only, NO DELETE
// endpoint (archived rows keep FK history intact). The whole group is gated by
// `tool_types.manage` at the composition-root mount point — its OWN gate, one
// permission per surface (AD-6) — so this router carries no gateway itself;
// 404/405 answer with the uniform JSON envelope so no sub-path can emit a
// plain-text body.
func (h *Handler) ToolTypeRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListToolTypes)
	r.Post("/", h.CreateToolType)
	r.Put("/{id}", h.UpdateToolType)
	r.Post("/{id}/archive", h.ArchiveToolType)
	return r
}

// ListToolTypes handles GET /api/v1/admin/tool-types (GET_LIST_EMPTY /
// GET_LIST): it returns the ACTIVE tool-type catalog, oldest first, each with
// its ordered checklist items. Archived types never appear.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks tool_types.manage (gateway or core
//     re-check; no tool-type data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListToolTypes(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	types, err := h.service.ListToolTypes(r.Context(), user.ID)
	if err != nil {
		h.mapToolTypeError(w, r, err, user)
		return
	}
	out := make([]toolTypeDTO, 0, len(types))
	for _, tt := range types {
		out = append(out, toToolTypeDTO(tt))
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// CreateToolType handles POST /api/v1/admin/tool-types (CREATE_VALID /
// CREATE_DUPLICATE / CREATE_INVALID / CREATE_BAD_SCHEDULE /
// CREATE_BAD_QUALIFICATION): it persists a new tool type with its ordered
// checklist items. Audited (tool_type.create).
func (h *Handler) CreateToolType(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	var input toolscore.ToolTypeInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	tt, err := h.service.CreateToolType(r.Context(), user.ID, input)
	if err != nil {
		h.mapToolTypeError(w, r, err, user)
		return
	}
	h.log().Info("tool type created", "email", user.Email, "name", tt.Name, "mode", tt.InspectionMode)

	httpapi.WriteJSON(w, http.StatusCreated, toolTypeWriteDTO{toolTypeDTO: toToolTypeDTO(tt), Message: toolscore.MsgToolTypeSaved})
}

// UpdateToolType handles PUT /api/v1/admin/tool-types/{id}
// (UPDATE_REPLACE_ITEMS): it persists the type's core fields and REPLACES its
// checklist items fully (the surface always submits the whole ordered list).
// Updating an already-archived type answers the 404 sentinel (UPDATE_ARCHIVED).
// Audited (tool_type.update).
func (h *Handler) UpdateToolType(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input toolscore.ToolTypeInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	tt, err := h.service.UpdateToolType(r.Context(), user.ID, id, input)
	if err != nil {
		h.mapToolTypeError(w, r, err, user)
		return
	}
	h.log().Info("tool type updated", "email", user.Email, "id", id, "name", tt.Name, "mode", tt.InspectionMode)

	httpapi.WriteJSON(w, http.StatusOK, toolTypeWriteDTO{toolTypeDTO: toToolTypeDTO(tt), Message: toolscore.MsgToolTypeSaved})
}

// ArchiveToolType handles POST /api/v1/admin/tool-types/{id}/archive (ARCHIVE):
// it soft-archives the type — archived_at is set and the row leaves the active
// list. Archiving an already-archived row answers the 404 sentinel. Audited
// (tool_type.archive).
func (h *Handler) ArchiveToolType(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	tt, err := h.service.ArchiveToolType(r.Context(), user.ID, id)
	if err != nil {
		h.mapToolTypeError(w, r, err, user)
		return
	}
	h.log().Info("tool type archived", "email", user.Email, "id", id, "name", tt.Name)

	httpapi.WriteJSON(w, http.StatusOK, toolTypeWriteDTO{toolTypeDTO: toToolTypeDTO(tt), Message: toolscore.MsgToolTypeArchived})
}

// toToolTypeDTO maps the domain tool type to the wire payload. The attributes
// jsonb extension surface is never exposed in V1 (Story 4.4 owns it), and the
// active surface never carries an archived row.
func toToolTypeDTO(tt *toolscore.ToolType) toolTypeDTO {
	items := make([]toolTypeChecklistItemDTO, 0, len(tt.Items))
	for _, item := range tt.Items {
		items = append(items, toolTypeChecklistItemDTO{
			ID:       item.ID,
			Position: item.Position,
			Label:    item.Label,
		})
	}
	return toolTypeDTO{
		ID:                      tt.ID,
		Name:                    tt.Name,
		DefaultScheduleID:       tt.DefaultScheduleID,
		RequiredQualificationID: tt.RequiredQualificationID,
		InspectionMode:          tt.InspectionMode,
		ChecklistItems:          items,
		CreatedAt:               tt.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:               tt.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// mapToolTypeError writes the uniform envelope for the tool-type service's
// errors.
func (h *Handler) mapToolTypeError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *toolscore.InvalidToolTypeError
	switch {
	case errors.Is(err, toolscore.ErrForbidden):
		h.log().Warn("tool type access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, toolscore.ErrToolTypeNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", toolscore.MsgToolTypeNotFound)
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("tool type request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}