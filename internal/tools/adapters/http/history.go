package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
)// toolHistoryInspectionDTO is one inspection row of the history payload (Story
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