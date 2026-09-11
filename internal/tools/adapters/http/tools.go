package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// toolDTO is the GET payload — the typed core fields plus the tool's type
// display name (JOIN) and the attributes jsonb passthrough. An EMPTY
// schedule_id means the tool inherits its type's default schedule (AD-5);
// archived tools never reach the active surface. ArchivedAt is null on the
// active surface and set (RFC3339) after a soft archive — it is the observable
// state-change signal of the POST /{id}/archive response.
type toolDTO struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	ToolTypeID   string         `json:"tool_type_id"`
	ToolTypeName string         `json:"tool_type_name"`
	ScheduleID   string         `json:"schedule_id"`
	Attributes   map[string]any `json:"attributes"`
	ArchivedAt   *string        `json:"archived_at"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

// toolWriteDTO adds the server-authoritative German confirmation.
type toolWriteDTO struct {
	toolDTO
	Message string `json:"message"`
}

// dashboardToolDTO is the minimal GET /api/v1/tools payload (Story 4-3b): the
// id, name and the tool type's display name (JOIN). Deliberately small — no
// schedule/attributes/audit data on this surface (the admin surface exposes
// the full DTO) and no status/due-date derivation (Story 6.1 owns it — the SPA
// marks every tool "verfügbar" statically).
type dashboardToolDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ToolTypeID   string `json:"tool_type_id"`
	ToolTypeName string `json:"tool_type_name"`
}

// ToolRoutes returns the Tool tool router (Story 4.3, FR-9/FR-10): GET/POST /
// and PUT /{id}, POST /{id}/archive — soft archive only, NO DELETE endpoint
// (archived rows keep FK history intact). The whole group is gated by
// `tools.manage` at the composition-root mount point — its OWN gate, one
// permission per surface (AD-6) — so this router carries no gateway itself;
// 404/405 answer with the uniform JSON envelope so no sub-path can emit a
// plain-text body.
func (h *Handler) ToolRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListTools)
	r.Post("/", h.CreateTool)
	r.Put("/{id}", h.UpdateTool)
	r.Post("/{id}/archive", h.ArchiveTool)
	return r
}

// DashboardToolsRoutes returns the GEAR-module (non-admin) tool router (Story
// 4-3b): GET / only, answering the minimal dashboard DTO (id, name, type
// name). The whole group is gated by `dashboard.view` at the composition-root
// mount point — its OWN gate, one permission per surface (AD-6) — so this
// router carries no gateway itself; 404/405 answer with the uniform JSON
// envelope so no sub-path can emit a plain-text body. No writes live here
// (admin-only, Story 4.3).
func (h *Handler) DashboardToolsRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListDashboardTools)
	return r
}

// ListTools handles GET /api/v1/admin/tools (GET_LIST_EMPTY / GET_LIST): it
// returns the ACTIVE tool catalog, oldest first, each with its type display
// name. Archived tools never appear.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks tools.manage (gateway or core
//     re-check; no tool data exposed)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListTools(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	tools, err := h.service.ListTools(r.Context(), user.ID)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	out := make([]toolDTO, 0, len(tools))
	for _, tool := range tools {
		out = append(out, toToolDTO(tool))
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// ListDashboardTools handles GET /api/v1/tools (GET_LIST_EMPTY / GET_LIST,
// Story 4-3b): it returns the ACTIVE tool catalog, oldest first, each with its
// type display name, as the minimal dashboard DTO. The `dashboard.view` gate
// lives at the composition-root mount (all base roles hold it) — the core read
// is ungated by design, so this handler never re-checks `tools.manage`.
// Archived tools never appear.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListDashboardTools(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	tools, err := h.service.ListToolsForDashboard(r.Context())
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	out := make([]dashboardToolDTO, 0, len(tools))
	for _, tool := range tools {
		out = append(out, dashboardToolDTO{
			ID:           tool.ID,
			Name:         tool.Name,
			ToolTypeID:   tool.ToolTypeID,
			ToolTypeName: tool.ToolTypeName,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// CreateTool handles POST /api/v1/admin/tools (CREATE_VALID / CREATE_OVERRIDE /
// CREATE_DUPLICATE / CREATE_INVALID / CREATE_BAD_TYPE / CREATE_BAD_OVERRIDE):
// it persists a new tool belonging to exactly one tool type, with the optional
// per-tool schedule override (empty → inherit the type default). Audited
// (tool.create).
func (h *Handler) CreateTool(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	var input toolscore.ToolInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	tool, err := h.service.CreateTool(r.Context(), user.ID, input)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	h.log().Info("tool created", "email", user.Email, "name", tool.Name, "type", tool.ToolTypeName)

	httpapi.WriteJSON(w, http.StatusCreated, toolWriteDTO{toolDTO: toToolDTO(tool), Message: toolscore.MsgToolSaved})
}

// UpdateTool handles PUT /api/v1/admin/tools/{id} (UPDATE_CLEAR_OVERRIDE): it
// persists the tool's name, type, override and attributes; an EMPTY
// schedule_id CLEARS the stored override (the tool inherits its type's default
// again, AD-5). Updating an already-archived tool answers the 404 sentinel
// (UPDATE_ARCHIVED). Audited (tool.update).
func (h *Handler) UpdateTool(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input toolscore.ToolInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	tool, err := h.service.UpdateTool(r.Context(), user.ID, id, input)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	h.log().Info("tool updated", "email", user.Email, "id", id, "name", tool.Name)

	httpapi.WriteJSON(w, http.StatusOK, toolWriteDTO{toolDTO: toToolDTO(tool), Message: toolscore.MsgToolSaved})
}

// ArchiveTool handles POST /api/v1/admin/tools/{id}/archive (ARCHIVE): it
// soft-archives the tool — archived_at is set and the row leaves the active
// list. Archiving an already-archived row answers the 404 sentinel. Audited
// (tool.archive).
func (h *Handler) ArchiveTool(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	tool, err := h.service.ArchiveTool(r.Context(), user.ID, id)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	h.log().Info("tool archived", "email", user.Email, "id", id, "name", tool.Name)

	httpapi.WriteJSON(w, http.StatusOK, toolWriteDTO{toolDTO: toToolDTO(tool), Message: toolscore.MsgToolArchived})
}

// toToolDTO maps the domain tool to the wire payload. The active surface never
// carries an archived row; an empty schedule_id (inherit) serializes as "".
// ArchivedAt is null for an active tool and a set RFC3339 string after a soft
// archive (the POST /{id}/archive state-change signal).
func toToolDTO(tool *toolscore.Tool) toolDTO {
	var archivedAt *string
	if tool.ArchivedAt != nil {
		s := tool.ArchivedAt.UTC().Format(time.RFC3339)
		archivedAt = &s
	}
	return toolDTO{
		ID:           tool.ID,
		Name:         tool.Name,
		ToolTypeID:   tool.ToolTypeID,
		ToolTypeName: tool.ToolTypeName,
		ScheduleID:   tool.ScheduleID,
		Attributes:   tool.Attributes,
		ArchivedAt:   archivedAt,
		CreatedAt:    tool.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    tool.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// mapToolError writes the uniform envelope for the tool service's errors.
func (h *Handler) mapToolError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *toolscore.InvalidToolError
	switch {
	case errors.Is(err, toolscore.ErrForbidden):
		h.log().Warn("tool access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, toolscore.ErrToolNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", toolscore.MsgToolNotFound)
	case errors.As(err, &inv):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("tool request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}