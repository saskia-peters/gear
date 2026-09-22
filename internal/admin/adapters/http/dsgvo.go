package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	dsgvocore "github.com/saskia-peters/gear/internal/dsgvo/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// This file hosts the DSGVO admin surface (Story 3.3, FR-24/AD-6): the
// data-access report endpoint GET /reports/{userId} under /api/v1/admin/dsgvo.
// The group is gated at the composition-root mount by RequireAnyPermission with
// the DSGVO codes [dsgvo.access_report, dsgvo.delete] (one permission per
// surface, AD-6); the DSGVO orchestrator re-checks `dsgvo.access_report`
// defense-in-depth (AD-6) before ANY personal data is assembled. The report is
// returned as structured JSON (application/json) with an attachment-style
// Content-Disposition so a direct browser visit downloads the artifact, while
// the SPA's fetch still parses and renders it. Every generation is audited by
// the orchestrator (NFR-O1/NFR-O2).

// DsgvoService is the narrow inbound seam the handler consumes: the DSGVO
// orchestrator's report generation and the Story 3.4 account-deletion lifecycle
// (delete / list deleted / purge). Implemented by *dsgvocore.Service.
type DsgvoService interface {
	GenerateAccessReport(ctx context.Context, actorID, targetUserID string) (*dsgvocore.AccessReport, error)
	DeleteAccount(ctx context.Context, actorID, targetUserID, reason string) (*dsgvocore.DeleteAccountResult, error)
	ListDeletedAccounts(ctx context.Context, actorID string) ([]*dsgvocore.DeletedAccountRow, error)
	PurgeDeletedAccount(ctx context.Context, actorID, archiveID string) error
}

// MsgDsgvoReportTitle is the German microcopy shown as the report file name
// (attachment-style download).
const MsgDsgvoReportTitle = "dsgvo-datenauskunft.json"

// DsgvoHandler serves the DSGVO admin HTTP surface.
type DsgvoHandler struct {
	service DsgvoService
	logger  *slog.Logger
}

// NewDsgvoHandler constructs the DSGVO HTTP handler. logger may be nil — the
// handlers never log through a nil logger (log() falls back to slog.Default()).
func NewDsgvoHandler(service DsgvoService, logger *slog.Logger) *DsgvoHandler {
	if service == nil {
		panic("admin http: dsgvo service is not wired")
	}
	return &DsgvoHandler{service: service, logger: logger}
}

// log returns the configured logger or slog.Default() so a nil logger can never
// panic a handler.
func (h *DsgvoHandler) log() *slog.Logger {
	if h.logger != nil {
		return h.logger
	}
	return slog.Default()
}

// DsgvoRoutes returns the DSGVO admin router (Story 3.3 + 3.4, FR-24/AD-6): the
// GET report handler at /reports/{userId}, the account deletion at
// POST /users/{id}/delete (body {reason}), the archived-account list at
// GET /users/deleted and the on-demand purge at DELETE /users/deleted/{archiveId}.
// The composition root mounts this router at /api/v1/admin/dsgvo behind the
// DSGVO any-of gate; the router carries no gateway itself. 404/405 answer with
// the uniform JSON envelope so no sub-path can emit a plain-text body.
//
// The delete verb is POST (not DELETE): it carries the reason in the body, and
// proxies strip DELETE bodies — the repo's POST /{id}/archive-style convention.
func (h *DsgvoHandler) DsgvoRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/reports/{userId}", h.GetAccessReport)
	r.Post("/users/{id}/delete", h.DeleteUserAccount)
	r.Get("/users/deleted", h.ListDeletedAccounts)
	r.Delete("/users/deleted/{archiveId}", h.PurgeDeletedAccount)
	return r
}

// GetAccessReport handles GET /api/v1/admin/dsgvo/reports/{userId}
// (REPORT_OK / REPORT_GATED / REPORT_UNKNOWN / REPORT_401, Story 3.3, FR-24/
// AD-6): it asks the DSGVO orchestrator to assemble the target user's
// data-access report and answers the structured JSON with an attachment-style
// Content-Disposition (the direct-browser download + the SPA's fetch render
// source). The orchestrator re-checks `dsgvo.access_report` defense-in-depth
// and audits the generation (NFR-O1/NFR-O2). No personal data is ever exposed
// to a non-holder (AD-6).
//
// Error mapping (uniform envelope, via mapDsgvoError):
//   - 401 unauthorized when the caller is not authenticated
//   - 400 invalid_request (German) for an empty target user id
//   - 403 forbidden when the caller lacks dsgvo.access_report (no personal
//     data exposed, AD-6)
//   - 404 not_found (German) for an unknown target user id
//   - 500 internal_error on an unexpected failure
func (h *DsgvoHandler) GetAccessReport(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	userID := strings.TrimSpace(chi.URLParam(r, "userId"))
	if userID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Bitte wähle einen Benutzer aus.")
		return
	}

	report, err := h.service.GenerateAccessReport(r.Context(), user.ID, userID)
	if err != nil {
		h.mapDsgvoError(w, r, err, user)
		return
	}
	// Defensive nil-guard: a nil-returning service path (a wiring defect) must
	// not panic — answer the clean 500 via the error mapper's default branch.
	if report == nil {
		h.mapDsgvoError(w, r, errors.New("admin http: nil dsgvo report from service"), user)
		return
	}

	h.log().Info("dsgvo data-access report served", "email", user.Email, "target", userID)
	// The report is a GATED artifact (dsgvo.access_report, AD-6): personal data
	// must never be cached by a proxy or the browser.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", MsgDsgvoReportTitle))
	httpapi.WriteJSON(w, http.StatusOK, report)
}

// DeleteAccountRequest is the POST /users/{id}/delete body (Story 3.4): the
// MANDATORY Begründung. The server trims + bounds it (≤ 2000 runes); an empty
// or over-long reason answers the German 400.
type DeleteAccountRequest struct {
	Reason string `json:"reason"`
}

// DeleteUserAccount handles POST /api/v1/admin/dsgvo/users/{id}/delete
// (DELETE_OK / DELETE_SELF / DELETE_EMPTY_REASON / DELETE_LONG_REASON /
// DELETE_UNKNOWN / DELETE_GATED, Story 3.4, FR-24/AD-6): it asks the DSGVO
// orchestrator to soft-delete + archive the target user's account (the Tool
// rewrite + User lifecycle in order) and answers the German confirmation. The
// orchestrator re-checks `dsgvo.delete` defense-in-depth and audits the
// operation (NFR-O1/NFR-O2). NO hard delete happens here — the account becomes
// a scrubbed tombstone and its personal data moves into the archive (the purge
// is a separate admin action).
//
// Error mapping (uniform envelope, via mapDsgvoError):
//   - 401 unauthorized when the caller is not authenticated
//   - 400 invalid_request (German) for a self-deletion or an empty/over-long
//     reason
//   - 403 forbidden when the caller lacks dsgvo.delete (no data exposed, AD-6)
//   - 404 not_found (German) for an unknown / already-deleted target user
//   - 500 internal_error on an unexpected failure
func (h *DsgvoHandler) DeleteUserAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	userID := strings.TrimSpace(chi.URLParam(r, "id"))
	if userID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Bitte wähle einen Benutzer aus.")
		return
	}

	var body DeleteAccountRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	result, err := h.service.DeleteAccount(r.Context(), user.ID, userID, body.Reason)
	if err != nil {
		h.mapDsgvoError(w, r, err, user)
		return
	}
	if result == nil {
		h.mapDsgvoError(w, r, errors.New("admin http: nil dsgvo delete result from service"), user)
		return
	}

	h.log().Info("dsgvo account deleted", "email", user.Email, "target", userID)
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, result)
}

// ListDeletedAccounts handles GET /api/v1/admin/dsgvo/users/deleted
// (LIST_DELETED, Story 3.4): the archived (soft-deleted) accounts newest-first
// for the "Gelöschte Konten" surface. The orchestrator re-checks `dsgvo.delete`
// defense-in-depth (AD-6); a non-holder answers 403 with no data.
func (h *DsgvoHandler) ListDeletedAccounts(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	rows, err := h.service.ListDeletedAccounts(r.Context(), user.ID)
	if err != nil {
		h.mapDsgvoError(w, r, err, user)
		return
	}
	if rows == nil {
		rows = []*dsgvocore.DeletedAccountRow{}
	}

	h.log().Info("dsgvo deleted accounts listed", "email", user.Email, "count", len(rows))
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"accounts": rows})
}

// PurgeDeletedAccount handles DELETE /api/v1/admin/dsgvo/users/deleted/{archiveId}
// (PURGE_OK / PURGE_MISSING, Story 3.4, FR-24): the admin-initiated on-demand
// hard purge — the archive row AND the users tombstone are hard-deleted in ONE
// transaction (the ONLY hard delete in the account lifecycle). The orchestrator
// re-checks `dsgvo.delete`, audits `dsgvo.purge` and answers the German
// confirmation.
//
// Error mapping:
//   - 401 unauthorized when the caller is not authenticated
//   - 403 forbidden when the caller lacks dsgvo.delete
//   - 404 not_found (German) for an unknown / already-purged archive id
//   - 500 internal_error on an unexpected failure
func (h *DsgvoHandler) PurgeDeletedAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	archiveID := strings.TrimSpace(chi.URLParam(r, "archiveId"))
	if archiveID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Bitte wähle einen gelöschten Account aus.")
		return
	}

	if err := h.service.PurgeDeletedAccount(r.Context(), user.ID, archiveID); err != nil {
		h.mapDsgvoError(w, r, err, user)
		return
	}

	h.log().Info("dsgvo deleted account purged", "email", user.Email, "archive", archiveID)
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"message": dsgvocore.MsgPurgeConfirmation})
}

// mapDsgvoError writes the uniform envelope for the DSGVO service errors (Story
// 3.3 + 3.4, AD-6). A non-holder → the generic no-hint 403 (no personal data,
// AD-6); a self-deletion or a bad reason → the German 400; an unknown target
// user / archive id → the German 404; the default branch → the clean 500 (a
// canceled request is not answered).
func (h *DsgvoHandler) mapDsgvoError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	switch {
	case errors.Is(err, dsgvocore.ErrForbidden):
		h.log().Warn("dsgvo access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	case errors.Is(err, dsgvocore.ErrDsgvoDeleteSelf):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", dsgvocore.MsgDsgvoDeleteSelf)
	case errors.Is(err, dsgvocore.ErrDsgvoReasonInvalid):
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, usercore.ErrDeletedAccountNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", dsgvocore.MsgDeletedAccountNotFound)
	case errors.Is(err, usercore.ErrAdminUserNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "not_found", dsgvocore.MsgUserNotFound)
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("dsgvo request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}
