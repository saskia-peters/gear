package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)// inspectionStartDTO is the eligible POST /api/v1/tools/{id}/inspection/start
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