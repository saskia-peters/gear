package http

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// toolDTO is the GET payload — the typed core fields plus the tool's type
// display name (JOIN), the inventory number and the attributes jsonb
// passthrough. An EMPTY schedule_id means the tool inherits its type's default
// schedule (AD-5); archived tools never reach the active surface. ArchivedAt
// is null on the active surface and set (RFC3339) after a soft archive — it is
// the observable state-change signal of the POST /{id}/archive response.
type toolDTO struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ToolTypeID      string         `json:"tool_type_id"`
	ToolTypeName    string         `json:"tool_type_name"`
	ScheduleID      string         `json:"schedule_id"`
	InventoryNumber string         `json:"inventory_number"`
	Attributes      map[string]any `json:"attributes"`
	ArchivedAt      *string        `json:"archived_at"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

// toolWriteDTO adds the server-authoritative German confirmation.
type toolWriteDTO struct {
	toolDTO
	Message string `json:"message"`
}

// toolImportErrorDTO is one per-row import error (Story 4.5, FR-9): the 1-based
// file line + the German reason.
type toolImportErrorDTO struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// toolImportResultDTO is the POST /import payload (Story 4.5): the number of
// created/updated tools + the per-row errors. An EMPTY errors array means every
// row succeeded (never null).
type toolImportResultDTO struct {
	Imported int                  `json:"imported"`
	Errors   []toolImportErrorDTO `json:"errors"`
}

// dashboardToolDTO is the minimal GET /api/v1/tools payload (Story 4-3b +
// 6.1): the id, name, the tool type's display name (JOIN), the inventory
// number (row meta) and the DERIVED status (FR-16/AD-4/AD-5 — computed on
// read, never stored). Deliberately small — no schedule/attributes/audit data
// on this surface (the admin surface exposes the full DTO).
type dashboardToolDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ToolTypeID      string    `json:"tool_type_id"`
	ToolTypeName    string    `json:"tool_type_name"`
	InventoryNumber string    `json:"inventory_number"`
	Status          statusDTO `json:"status"`
}

// inspectionStartDTO is the eligible POST /api/v1/tools/{id}/inspection/start
// payload (Story 5.1, FR-11): the tool plus its type's inspection_mode and —
// for checklist-mode types — the type's ordered checklist items (Story 5.2
// mode-aware start). The SPA renders the mode-appropriate surface from this.
// No inspection record is created here.
type inspectionStartDTO struct {
	ToolID         string                     `json:"tool_id"`
	ToolName       string                     `json:"tool_name"`
	ToolTypeID     string                     `json:"tool_type_id"`
	ToolTypeName   string                     `json:"tool_type_name"`
	InspectionMode string                     `json:"inspection_mode"`
	ChecklistItems []toolTypeChecklistItemDTO `json:"checklist_items"`
}

// inspectionItemDTO is one persisted snapshotted checklist result in the
// submit response (Story 5.3, FR-12).
type inspectionItemDTO struct {
	ID       string `json:"id"`
	ItemID   string `json:"item_id"`
	Label    string `json:"label"`
	Position int    `json:"position"`
	Result   string `json:"result"`
}

// inspectionDTO is the persisted inspection record in the submit response
// (Story 5.3, FR-12/FR-13): identity + timestamp + mode + overall result +
// notes + the ordered snapshot items (empty for pass_fail).
type inspectionDTO struct {
	ID            string              `json:"id"`
	ToolID        string              `json:"tool_id"`
	InspectorID   string              `json:"inspector_id"`
	Mode          string              `json:"mode"`
	OverallResult string              `json:"overall_result"`
	Notes         string              `json:"notes"`
	SubmittedAt   string              `json:"submitted_at"`
	Items         []inspectionItemDTO `json:"items"`
}

// statusDTO is the derived status in the submit response (Story 5.3, AD-4/AD-5):
// `oos|red|orange|green` plus the next-due timestamp (null for `oos` and the
// never-inspected `red`).
type statusDTO struct {
	Status  string  `json:"status"`
	NextDue *string `json:"next_due"`
}

// inspectionSubmitResponseDTO is the POST /api/v1/tools/{id}/inspection
// payload (Story 5.3): the persisted inspection record + the derived status.
type inspectionSubmitResponseDTO struct {
	Inspection inspectionDTO `json:"inspection"`
	Status     statusDTO     `json:"status"`
}

// reinstatementRequestDTO is the POST /api/v1/tools/{id}/reinstatement body
// (Story 5.6, FR-15/AD-9): the MANDATORY reason. The decoder rejects unknown
// fields + trailing JSON like the submit handler.
type reinstatementRequestDTO struct {
	Reason string `json:"reason"`
}

// reinstatementResponseDTO is the POST /api/v1/tools/{id}/reinstatement
// payload (Story 5.6): the newly derived status (not-OOS, clock reset) + the
// German confirmation.
type reinstatementResponseDTO struct {
	Status  statusDTO `json:"status"`
	Message string    `json:"message"`
}

// toolHistoryInspectionDTO is one inspection row of the history payload (Story
// 6.3, FR-18): the inspector display name (resolved through the User-module
// seam, AD-8) + the record's fields + the snapshotted ordered checklist items
// (reusing inspectionItemDTO; empty for pass_fail). "Deleted User" renders for
// an inspector account that no longer exists (Story 3.4 forward-compat).
type toolHistoryInspectionDTO struct {
	ID            string              `json:"id"`
	InspectorID   string              `json:"inspector_id"`
	InspectorName string              `json:"inspector_name"`
	Mode          string              `json:"mode"`
	OverallResult string              `json:"overall_result"`
	Notes         string              `json:"notes"`
	SubmittedAt   string              `json:"submitted_at"`
	Items         []inspectionItemDTO `json:"items"`
}

// toolHistoryReinstatementDTO is one reinstatement row of the history payload
// (Story 6.3, FR-18): the actor display name (resolved through the seam) + the
// record's fields.
type toolHistoryReinstatementDTO struct {
	ID        string `json:"id"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

// toolHistoryDTO is the GET /api/v1/tools/{id}/history payload (Story 6.3,
// FR-18): the two newest-first lists — inspections (each with per-checklist-item
// results) and reinstatements. Empty lists serialize as `[]`, never null.
type toolHistoryDTO struct {
	Inspections    []toolHistoryInspectionDTO    `json:"inspections"`
	Reinstatements []toolHistoryReinstatementDTO `json:"reinstatements"`
}

// ToolRoutes returns the Tool tool router (Story 4.3 + 4-3b, FR-9/FR-10):
// GET/POST / and PUT /{id}, POST /{id}/archive — soft archive only, NO DELETE
// endpoint (archived rows keep FK history intact). The outer mount gate is
// any-of [tools.manage, tool.edit] (Spec 4-3b): a tool.edit-only holder can
// GET (list) + PUT (edit, incl. the inventory number) but NOT create/archive.
// The writes POST / (create), POST /{id}/archive (archive) and POST /import
// (bulk CSV import, Story 4.5) are wrapped in a chi GROUP that
// re-applies a tools.manage-ONLY RequireAnyPermission (the real auth
// middleware, mirroring the admin sub-surface precedent in
// internal/user/adapters/http/admin.go) — the tighter write-only gate — while
// GET / and PUT /{id} stay at the mount level. The core re-checks the same
// split defense-in-depth (AD-6). 404/405 answer with the uniform JSON envelope
// so no sub-path can emit a plain-text body.
func (h *Handler) ToolRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListTools)
	r.Put("/{id}", h.UpdateTool)

	// Write-only sub-gate (Spec 4-3b + 4.5): POST / (create), POST /{id}/archive
	// and POST /import re-apply a tools.manage-only RequireAnyPermission. The
	// outer any-of gate already authenticated + resolved the caller's permission
	// set; this group re-runs the REAL auth middleware so a tool.edit-only
	// holder is denied the writes with the uniform 403 (no tool data, FR-19).
	r.Group(func(writes chi.Router) {
		writes.Use(auth.RequireAnyPermission(h.sessionValidator, h.permissionResolver,
			[]string{toolscore.ToolsManagePermission}, "tools.manage write access denied", h.logger))
		writes.Post("/", h.CreateTool)
		writes.Post("/{id}/archive", h.ArchiveTool)
		writes.Post("/import", h.ImportTools)
	})

	return r
}

// DashboardToolsRoutes returns the GEAR-module (non-admin) tool router (Story
// 4-3b): GET / only, answering the minimal dashboard DTO (id, name, type
// name, inventory number). The whole group is gated by `dashboard.view` at the
// composition-root mount point — its OWN gate, one permission per surface
// (AD-6) — so this router carries no gateway itself; 404/405 answer with the
// uniform JSON envelope so no sub-path can emit a plain-text body. No writes
// live here (admin-only, Story 4.3).
func (h *Handler) DashboardToolsRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListDashboardTools)
	return r
}

// InspectionRoutes returns the inspection router (Story 5.1 + 5.3, FR-11/AD-7):
// the POST inspection-start handler at /start and the POST inspection-submit
// handler at the router ROOT. The composition root mounts this router at the
// full path prefix (/api/v1/tools/{id}/inspection via chi Mount, which strips
// the prefix and preserves the {id} param), so the route patterns are defined
// ONCE here and never duplicated at the mount site. The whole router is gated
// by `inspection.submit` at the composition-root mount point — its OWN gate,
// one permission per surface (AD-6) — so this router carries no gateway itself;
// 404/405 answer with the uniform JSON envelope so no sub-path can emit a
// plain-text body.
func (h *Handler) InspectionRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Post("/start", h.StartInspection)
	r.Post("/", h.SubmitInspection)
	return r
}

// ReinstateRoutes returns the reinstatement router (Story 5.6, FR-15/AD-9):
// the POST reinstate handler at the router ROOT. The composition root mounts
// this router at the full path prefix (/api/v1/tools/{id}/reinstatement via
// chi Mount, which strips the prefix and preserves the {id} param), so the
// route pattern is defined ONCE here and never duplicated at the mount site.
// The whole router is gated by `tool.reinstate` at the composition-root mount
// point — its OWN gate, one permission per surface (AD-6) — so this router
// carries no gateway itself; 404/405 answer with the uniform JSON envelope so
// no sub-path can emit a plain-text body.
func (h *Handler) ReinstateRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Post("/", h.ReinstateTool)
	return r
}

// HistoryRoutes returns the history router (Story 6.3, FR-18/AD-6): the GET
// history handler at the router ROOT. The composition root mounts this router
// at the full path prefix (/api/v1/tools/{id}/history via chi Mount, which
// strips the prefix and preserves the {id} param), so the route pattern is
// defined ONCE here and never duplicated at the mount site. The whole router is
// gated by `inspection.history.view` at the composition-root mount point — its
// OWN gate, one permission per surface (AD-6) — so this router carries no
// gateway itself; 404/405 answer with the uniform JSON envelope so no sub-path
// can emit a plain-text body. Read-only — no write path lives here.
func (h *Handler) HistoryRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ListInspectionHistory)
	return r
}

// StartInspection handles POST /api/v1/tools/{id}/inspection/start
// (START_ELIGIBLE / START_QUALIFIED / START_MISSING_QUAL / START_EXPIRED_QUAL /
// START_NO_QUAL_TYPE / START_TOOL_NOT_FOUND, Story 5.1, FR-11/AD-7): it
// resolves the tool + its type's required qualification and checks the caller's
// granted qualifications through the User module's port (expiry-aware). An
// eligible caller gets 200 with the tool + its type's inspection_mode (the SPA
// navigates to the inspection screen). Gated `inspection.submit` at the mount;
// the core re-checks the code defense-in-depth (AD-6).
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden with the German reason when the caller lacks the required
//     qualification (missing OR expired) — or lacks inspection.submit (no tool
//     data exposed)
//   - 404 not_found for an unknown / archived tool id
//   - 500 internal_error on an unexpected failure
func (h *Handler) StartInspection(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	result, err := h.service.StartInspection(r.Context(), user.ID, id)
	if err != nil {
		h.mapInspectionError(w, r, err, user)
		return
	}
	h.log().Info("inspection started", "email", user.Email, "id", id, "name", result.ToolName, "mode", result.InspectionMode)

	httpapi.WriteJSON(w, http.StatusOK, inspectionStartDTO{
		ToolID:         result.ToolID,
		ToolName:       result.ToolName,
		ToolTypeID:     result.ToolTypeID,
		ToolTypeName:   result.ToolTypeName,
		InspectionMode: result.InspectionMode,
		ChecklistItems: toChecklistItemDTOs(result.ChecklistItems),
	})
}

// SubmitInspection handles POST /api/v1/tools/{id}/inspection
// (SUBMIT_PASSFAIL / SUBMIT_CHECKLIST / SUBMIT_INVALID / SUBMIT_GATED /
// SUBMIT_ARCHIVED / SUBMIT_UNKNOWN, Story 5.3, FR-12/FR-13/FR-14/AD-4/AD-5):
// it re-checks `inspection.submit` AND the tool-type qualification on submit
// (never trusts the client, FR-11), validates the mode/result/notes/items
// contract, persists the inspection + its snapshot items and returns the
// record + the shared derived status (OOS on a failed inspection). Gated
// `inspection.submit` at the mount; the core re-checks defense-in-depth (AD-6).
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks inspection.submit (no tool data
//     exposed) or the tool's required qualification (German reason)
//   - 404 not_found for an unknown / archived tool id
//   - 400 invalid_request with a German message for a validation failure
//     (bad mode/result, over-long notes, checklist mismatch, items on a
//     pass_fail mode)
//   - 500 internal_error on an unexpected failure
func (h *Handler) SubmitInspection(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input toolscore.InspectionInput
	// Buffered decoder (same pattern as the admin settings handlers): reject
	// UNKNOWN fields (DisallowUnknownFields) and trailing content after the
	// JSON object — both answer the uniform 400, never a partial parse.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	result, err := h.service.SubmitInspection(r.Context(), user.ID, id, input)
	if err != nil {
		h.mapInspectionError(w, r, err, user)
		return
	}
	// Defensive nil-guard: a nil-returning service path (a wiring defect) must
	// not panic — answer the clean 500 via the error mapper's default branch.
	if result == nil || result.Inspection == nil {
		h.mapInspectionError(w, r, errors.New("tools http: nil inspection result from service"), user)
		return
	}
	h.log().Info("inspection submitted", "email", user.Email, "id", id, "result", result.Inspection.OverallResult, "status", result.Status.Status)

	httpapi.WriteJSON(w, http.StatusOK, toInspectionSubmitResponse(result))
}

// ReinstateTool handles POST /api/v1/tools/{id}/reinstatement
// (REINSTATE_OK / REINSTATE_EMPTY / REINSTATE_LONG / REINSTATE_GATED /
// REINSTATE_ARCHIVED / REINSTATE_UNKNOWN, Story 5.6, FR-15/AD-9): it re-checks
// `tool.reinstate` defense-in-depth (AD-6), loads the tool, validates the
// MANDATORY reason, persists the reinstatement, audits it and returns the newly
// derived not-OOS status (next_due = reinstatement + interval, AD-5). Gated
// `tool.reinstate` at the mount; the core re-checks defense-in-depth.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks tool.reinstate (no tool data exposed)
//   - 404 not_found for an unknown / archived tool id
//   - 400 invalid_request with a German message for an empty / over-long reason
//   - 500 internal_error on an unexpected failure
func (h *Handler) ReinstateTool(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	var input reinstatementRequestDTO
	// Buffered decoder (same hardened pattern as the submit handler): reject
	// UNKNOWN fields and trailing content after the JSON object — both answer
	// the uniform 400, never a partial parse.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	result, err := h.service.ReinstateTool(r.Context(), user.ID, id, input.Reason)
	if err != nil {
		h.mapReinstatementError(w, r, err, user)
		return
	}
	// Defensive nil-guard: a nil-returning service path (a wiring defect) must
	// not panic — answer the clean 500 via the error mapper's default branch.
	if result == nil {
		h.mapReinstatementError(w, r, errors.New("tools http: nil reinstatement result from service"), user)
		return
	}
	h.log().Info("tool reinstated", "email", user.Email, "id", id, "status", result.Status.Status)

	httpapi.WriteJSON(w, http.StatusOK, reinstatementResponseDTO{
		Status:  toStatusDTO(result.Status),
		Message: toolscore.MsgToolReinstated,
	})
}

// ListInspectionHistory handles GET /api/v1/tools/{id}/history
// (HIST_OK / HIST_EMPTY / HIST_GATED / HIST_UNKNOWN / HIST_ARCHIVED /
// HIST_DELETED_USER, Story 6.3, FR-18): it returns the tool's full inspection
// history (newest first, each naming the inspector + timestamp + outcome +
// notes + mode + the per-checklist-item results) and its reinstatement ledger
// (newest first, actor + reason). Gated `inspection.history.view` at the
// mount; the core re-checks defense-in-depth (AD-6). Read-only.
//
// Error mapping (uniform envelope, via mapInspectionError):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks inspection.history.view (no history
//     data exposed, AD-6)
//   - 404 not_found for an unknown / archived tool id (German)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ListInspectionHistory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	history, err := h.service.ListInspectionHistory(r.Context(), user.ID, id)
	if err != nil {
		h.mapInspectionError(w, r, err, user)
		return
	}
	// Defensive nil-guard: a nil-returning service path (a wiring defect) must
	// not panic — answer the clean 500 via the error mapper's default branch.
	if history == nil {
		h.mapInspectionError(w, r, errors.New("tools http: nil history result from service"), user)
		return
	}
	h.log().Info("inspection history read", "email", user.Email, "id", id,
		"inspections", len(history.Inspections), "reinstatements", len(history.Reinstatements))

	httpapi.WriteJSON(w, http.StatusOK, toHistoryDTO(history))
}

// ListTools handles GET /api/v1/admin/tools (GET_LIST_EMPTY / GET_LIST): it
// returns the ACTIVE tool catalog, oldest first, each with its type display
// name and inventory number. Archived tools never appear.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks BOTH tools.manage and tool.edit
//     (gateway or core re-check; no tool data exposed)
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
// Story 4-3b + 6.1): it returns the ACTIVE tool catalog, oldest first, each
// with its type display name AND its derived status (FR-16/AD-4/AD-5), as the
// minimal dashboard DTO. The `dashboard.view` gate lives at the
// composition-root mount (all base roles hold it) — the core read is ungated
// by design, so this handler never re-checks `tools.manage`. Archived tools
// never appear.
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
			ID:              tool.ID,
			Name:            tool.Name,
			ToolTypeID:      tool.ToolTypeID,
			ToolTypeName:    tool.ToolTypeName,
			InventoryNumber: tool.InventoryNumber,
			Status:          toStatusDTO(tool.Status),
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// CreateTool handles POST /api/v1/admin/tools (CREATE_VALID / CREATE_OVERRIDE /
// CREATE_DUPLICATE / CREATE_INVALID / CREATE_BAD_TYPE / CREATE_BAD_OVERRIDE):
// it persists a new tool belonging to exactly one tool type, with the optional
// per-tool schedule override (empty → inherit the type default). The inventory
// number is AUTO-ASSIGNED by the server (a client-sent value is ignored) and
// returned in the response. Gated `tools.manage`-only by the write-only
// sub-router (a tool.edit-only holder is denied). Audited (tool.create).
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

// UpdateTool handles PUT /api/v1/admin/tools/{id} (UPDATE_CLEAR_OVERRIDE /
// UPDATE_INVENTORY): it persists the tool's name, type, override, inventory
// number and attributes; an EMPTY schedule_id CLEARS the stored override (the
// tool inherits its type's default again, AD-5). A non-empty inventory_number
// edits the stored number (uniquely enforced); an empty one is rejected 400
// (a tool always has one). Updating an already-archived tool answers the 404
// sentinel (UPDATE_ARCHIVED). Any-of [tools.manage, tool.edit] holder may call
// it. Audited (tool.update).
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
// list. Archiving an already-archived row answers the 404 sentinel. Gated
// `tools.manage`-only by the write-only sub-router (a tool.edit-only holder is
// denied). Audited (tool.archive).
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

// importBodyLimit caps the CSV import body at 5 MB (IMPORT_TOO_LARGE → German
// 400). The multipart overhead is part of the cap, so a "5 MB CSV" plus form
// framing still fits comfortably.
const importBodyLimit = 5 << 20

// importParseSentinels distinguish the CSV parse failures so the handler can
// surface the German 400 microcopy (the parse layer returns sentinels, the
// message stays at the HTTP boundary — matching the codebase convention).
var (
	errImportCSVUnreadable  = errors.New("tools http: unreadable csv")
	errImportCSVEmpty       = errors.New("tools http: empty csv")
	errImportCSVNotUTF8     = errors.New("tools http: csv is not utf-8")
	errImportCSVMissingCols = errors.New("tools http: missing required csv columns")
	errImportCSVDupCols     = errors.New("tools http: duplicate csv columns")
	errImportCSVNoRows      = errors.New("tools http: csv has no data rows")
)

// ImportTools handles POST /api/v1/admin/tools/import (IMPORT_HAPPY /
// IMPORT_MIXED / IMPORT_MISSING_HEADER / IMPORT_MALFORMED / IMPORT_EMPTY /
// IMPORT_TOO_LARGE / IMPORT_FORBIDDEN, Story 4.5, FR-9/FR-23): it parses the
// uploaded CSV (multipart field `file`, UTF-8, encoding/csv with trimmed
// leading space) into structured core rows — the hexagon boundary: NO CSV
// parsing happens in core — maps the header columns case-insensitively by
// name (`name`/`tool_type` required, `schedule`/`inventory_number` optional,
// extra columns ignored) and delegates to the service, which validates +
// partitions + batch-persists. Gated `tools.manage`-ONLY by the write-only
// sub-router; the core re-checks defense-in-depth (AD-6). Audited once per call.
//
// Error mapping (uniform envelope):
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks tools.manage (no tool data, AD-6)
//   - 400 invalid_request (German) for a missing `file` field, a body over
//     5 MB, an unparseable CSV, non-UTF-8 bytes, duplicate header columns, a
//     missing `name`/`tool_type` header column, an empty file or a header-only
//     file — BEFORE any row is processed
//   - 200 { imported, errors[] } when the import ran (per-row errors for the
//     invalid rows; the SPA renders them + offers the error report download)
func (h *Handler) ImportTools(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, importBodyLimit)
	if err := r.ParseMultipartForm(importBodyLimit); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Die Datei ist zu groß (maximal 5 MB).")
			return
		}
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Die CSV-Datei konnte nicht gelesen werden.")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Bitte wähle eine CSV-Datei aus.")
		return
	}
	defer func() { _ = file.Close() }()

	rows, parseErr := parseToolImportCSV(file)
	if parseErr != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", importParseErrorMessage(parseErr))
		return
	}

	result, err := h.service.ImportTools(r.Context(), user.ID, rows)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	if result == nil {
		h.mapToolError(w, r, errors.New("tools http: nil import result from service"), user)
		return
	}
	h.log().Info("tools imported", "email", user.Email, "imported", result.Imported, "errors", len(result.Errors))

	httpapi.WriteJSON(w, http.StatusOK, toImportResultDTO(result))
}

// parseToolImportCSV decodes the uploaded CSV into structured core rows (the
// hexagon: core receives rows, never bytes). It reads the whole file, REJECTS
// non-UTF-8 bytes, then uses encoding/csv (comma, trimmed leading space,
// variable field count so short rows read as empty cells); the first row is
// the HEADER, columns matched case-insensitively by name (a NORMALIZED header
// name appearing twice rejects the file). Missing `name`/`tool_type` columns,
// an empty file or a header-only file answer a German 400 BEFORE any row is
// processed. The row Line numbers are the PHYSICAL file positions (header = 1,
// first data row = 2) computed from the raw bytes — so a quoted cell with an
// embedded newline still makes the following record point at its true line.
func parseToolImportCSV(r io.Reader) ([]toolscore.ToolImportRow, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, errImportCSVUnreadable
	}
	// UTF-8 is a hard contract: reject invalid bytes before any CSV decoding.
	if !utf8.Valid(raw) {
		return nil, errImportCSVNotUTF8
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, errImportCSVUnreadable
	}
	if len(records) == 0 {
		return nil, errImportCSVEmpty
	}
	header := records[0]
	cols := map[string]int{}
	for i, name := range header {
		// Normalize the header cell: lowercase + strip the UTF-8 BOM
		// (Excel-exported CSVs) + trim. A duplicate normalized name is a 400.
		norm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "\ufeff"))
		if _, dup := cols[norm]; dup {
			return nil, errImportCSVDupCols
		}
		cols[norm] = i
	}
	nameIdx, nameOK := cols["name"]
	typeIdx, typeOK := cols["tool_type"]
	if !nameOK || !typeOK {
		return nil, errImportCSVMissingCols
	}
	// An absent schedule/inventory column (index -1) reads as "" via
	// toolCSVCell — treated exactly like an empty cell (NOT provided).
	scheduleIdx := -1
	if idx, ok := cols["schedule"]; ok {
		scheduleIdx = idx
	}
	invIdx := -1
	if idx, ok := cols["inventory_number"]; ok {
		invIdx = idx
	}

	// Physical file-line tracking: csv.Reader buffers ahead, so the record
	// index cannot express the true file line when a quoted cell contains an
	// embedded newline — compute each record's START line from the raw bytes.
	startLines := csvRecordStartLines(raw)

	rows := make([]toolscore.ToolImportRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		line := i + 1
		if i < len(startLines) {
			line = startLines[i]
		}
		record := records[i]
		rows = append(rows, toolscore.ToolImportRow{
			Line:            line,
			Name:            toolCSVCell(record, nameIdx),
			ToolTypeName:    toolCSVCell(record, typeIdx),
			ScheduleName:    toolCSVCell(record, scheduleIdx),
			InventoryNumber: toolCSVCell(record, invIdx),
		})
	}
	if len(rows) == 0 {
		return nil, errImportCSVNoRows
	}
	return rows, nil
}

// csvRecordStartLines returns the 1-based PHYSICAL line on which each logical
// CSV record STARTS, computed from the raw bytes with a quote-aware scan. A
// newline INSIDE a quoted cell does not end the record but still advances the
// physical line counter; only an unquoted `\n` is a record boundary. This is
// what makes error rows point at the true file line even when a cell contains
// an embedded newline (csv.Reader buffers ahead, so its record index cannot
// express this). The scan mirrors encoding/csv's quoting rules (`""` escape),
// so the boundaries it reports match the records csv.Reader returns.
func csvRecordStartLines(raw []byte) []int {
	lines := []int{1}
	physicalLine := 1
	inQuote := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == '\n' {
			// Every newline advances the physical line — whether or not it is a
			// record boundary (a quoted cell may contain a newline).
			physicalLine++
			if !inQuote {
				// An unquoted newline ends the logical record → the NEXT record
				// starts on the following physical line.
				lines = append(lines, physicalLine)
			}
			continue
		}
		if inQuote {
			if b == '"' {
				if i+1 < len(raw) && raw[i+1] == '"' {
					i++ // escaped quote inside a quoted cell
					continue
				}
				inQuote = false
			}
			continue
		}
		if b == '"' {
			inQuote = true
		}
	}
	return lines
}

// importParseErrorMessage maps the CSV parse sentinels to the German 400
// microcopy (the uniform-envelope message, surfaced verbatim in the SPA).
func importParseErrorMessage(err error) string {
	switch {
	case errors.Is(err, errImportCSVUnreadable):
		return "Die CSV-Datei konnte nicht gelesen werden."
	case errors.Is(err, errImportCSVEmpty):
		return "Die CSV-Datei ist leer."
	case errors.Is(err, errImportCSVNotUTF8):
		return "Die CSV-Datei ist nicht UTF-8-kodiert."
	case errors.Is(err, errImportCSVMissingCols):
		return "Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten."
	case errors.Is(err, errImportCSVDupCols):
		return "Die CSV enthält doppelte Spalten."
	case errors.Is(err, errImportCSVNoRows):
		return "Die CSV-Datei enthält keine Datenzeilen."
	default:
		return "Die CSV-Datei konnte nicht gelesen werden."
	}
}

// toolCSVCell reads one column of a record, trimming whitespace and treating a
// missing column (a short row) or an empty cell as "" (= NOT provided).
func toolCSVCell(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// toImportResultDTO maps the domain import result to the wire payload: the
// imported count + the per-row errors (an empty slice serializes as `[]`,
// never null).
func toImportResultDTO(result *toolscore.ToolImportResult) toolImportResultDTO {
	out := toolImportResultDTO{Errors: []toolImportErrorDTO{}}
	if result == nil {
		return out
	}
	out.Imported = result.Imported
	for _, e := range result.Errors {
		out.Errors = append(out.Errors, toolImportErrorDTO{Row: e.Row, Reason: e.Reason})
	}
	return out
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
		ID:              tool.ID,
		Name:            tool.Name,
		ToolTypeID:      tool.ToolTypeID,
		ToolTypeName:    tool.ToolTypeName,
		ScheduleID:      tool.ScheduleID,
		InventoryNumber: tool.InventoryNumber,
		Attributes:      attributesOrEmpty(tool.Attributes),
		ArchivedAt:      archivedAt,
		CreatedAt:       tool.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       tool.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// mapInspectionError writes the uniform envelope for the inspection service
// errors (Story 5.1 + 5.3). The qualification-gate denial is its OWN 403
// with the German reason (MsgToolQualificationMissing) — distinct from the
// generic forbidden; an inspection.submit-less caller gets the generic no-hint
// 403; a validation failure (ErrInspectionInvalid, Story 5.3) maps to the 400
// with the field-specific German message; an unknown/archived tool → 404.
func (h *Handler) mapInspectionError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *toolscore.InvalidInspectionError
	switch {
	case errors.Is(err, toolscore.ErrForbidden):
		h.log().Warn("inspection access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, toolscore.ErrToolOutOfService):
		h.log().Warn("inspection denied: tool out of service", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", toolscore.MsgToolOutOfService)
	case errors.Is(err, toolscore.ErrToolQualificationMissing):
		h.log().Warn("inspection denied: required qualification missing", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", toolscore.MsgToolQualificationMissing)
	case errors.Is(err, toolscore.ErrToolNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", toolscore.MsgToolNotFound)
	case errors.As(err, &inv):
		h.log().Warn("inspection submit invalid", "email", user.Email)
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("inspection request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}

// mapReinstatementError writes the uniform envelope for the reinstatement
// service errors (Story 5.6, FR-15/AD-9). A validation failure
// (ErrInspectionInvalid, the reason sentinels) maps to the 400 with the German
// message; an unknown/archived tool → 404; a tool.reinstate-less caller → the
// generic no-hint 403 (AD-6).
func (h *Handler) mapReinstatementError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	var inv *toolscore.InvalidInspectionError
	switch {
	case errors.Is(err, toolscore.ErrForbidden):
		h.log().Warn("reinstatement access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, toolscore.ErrToolNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", toolscore.MsgToolNotFound)
	case errors.As(err, &inv):
		h.log().Warn("reinstatement invalid", "email", user.Email)
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", inv.Message)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("reinstatement request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
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
	case errors.Is(err, toolscore.ErrInvalidAttributes):
		mapInvalidAttributesError(w, err)
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

// toChecklistItemDTOs maps the ordered domain checklist items to the wire shape
// (Story 5.2 mode-aware start / Story 4.2 type surface). Shared by the
// tool-type and inspection-start DTOs so the two surfaces serialize items
// identically.
func toChecklistItemDTOs(items []toolscore.ToolTypeChecklistItem) []toolTypeChecklistItemDTO {
	out := make([]toolTypeChecklistItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toolTypeChecklistItemDTO{
			ID:       item.ID,
			Position: item.Position,
			Label:    item.Label,
		})
	}
	return out
}

// toStatusDTO maps the domain derived status to the wire shape (Story 5.3 +
// 6.1, AD-4/AD-5): `oos|red|orange|green` plus the next-due timestamp (null
// for `oos` and the never-inspected `red`). Shared by the dashboard list and
// the inspection submit response so the two surfaces serialize the status
// identically.
func toStatusDTO(status toolscore.ToolStatus) statusDTO {
	var nextDue *string
	if status.NextDue != nil {
		s := status.NextDue.UTC().Format(time.RFC3339)
		nextDue = &s
	}
	return statusDTO{Status: string(status.Status), NextDue: nextDue}
}

// toHistoryDTO maps the domain tool history to the wire payload (Story 6.3,
// FR-18): the two newest-first lists, timestamps in RFC3339 UTC, empty lists
// serialized as `[]` (never null). The per-checklist-item results reuse the
// inspectionItemDTO shape so the item vocabulary never drifts.
func toHistoryDTO(history *toolscore.ToolHistory) toolHistoryDTO {
	if history == nil {
		return toolHistoryDTO{Inspections: []toolHistoryInspectionDTO{}, Reinstatements: []toolHistoryReinstatementDTO{}}
	}
	inspections := make([]toolHistoryInspectionDTO, 0, len(history.Inspections))
	for _, insp := range history.Inspections {
		items := make([]inspectionItemDTO, 0, len(insp.Items))
		for _, item := range insp.Items {
			items = append(items, inspectionItemDTO{
				ID:       item.ID,
				ItemID:   item.ItemID,
				Label:    item.Label,
				Position: item.Position,
				Result:   item.Result,
			})
		}
		inspections = append(inspections, toolHistoryInspectionDTO{
			ID:            insp.ID,
			InspectorID:   insp.InspectorID,
			InspectorName: insp.InspectorName,
			Mode:          insp.Mode,
			OverallResult: insp.OverallResult,
			Notes:         insp.Notes,
			SubmittedAt:   insp.SubmittedAt.UTC().Format(time.RFC3339),
			Items:         items,
		})
	}
	reinstatements := make([]toolHistoryReinstatementDTO, 0, len(history.Reinstatements))
	for _, r := range history.Reinstatements {
		reinstatements = append(reinstatements, toolHistoryReinstatementDTO{
			ID:        r.ID,
			ActorID:   r.ActorID,
			ActorName: r.ActorName,
			Reason:    r.Reason,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return toolHistoryDTO{Inspections: inspections, Reinstatements: reinstatements}
}

// toInspectionSubmitResponse maps the domain submit result to the wire payload
// (Story 5.3): the persisted record + the derived status. An empty notes column
// serializes as ""; NextDue serializes as null for `oos` and the
// never-inspected `red`.
func toInspectionSubmitResponse(result *toolscore.SubmitInspectionResult) inspectionSubmitResponseDTO {
	if result == nil || result.Inspection == nil {
		// Defensive: the handler guards nil before this is called (a nil
		// service result answers 500 there); a nil here serializes an empty
		// record rather than panicking.
		return inspectionSubmitResponseDTO{}
	}
	insp := result.Inspection
	items := make([]inspectionItemDTO, 0, len(insp.Items))
	for _, item := range insp.Items {
		items = append(items, inspectionItemDTO{
			ID:       item.ID,
			ItemID:   item.ItemID,
			Label:    item.Label,
			Position: item.Position,
			Result:   item.Result,
		})
	}
	return inspectionSubmitResponseDTO{
		Inspection: inspectionDTO{
			ID:            insp.ID,
			ToolID:        insp.ToolID,
			InspectorID:   insp.InspectorID,
			Mode:          insp.Mode,
			OverallResult: insp.OverallResult,
			Notes:         insp.Notes,
			SubmittedAt:   insp.SubmittedAt.UTC().Format(time.RFC3339),
			Items:         items,
		},
		Status: toStatusDTO(result.Status),
	}
}
