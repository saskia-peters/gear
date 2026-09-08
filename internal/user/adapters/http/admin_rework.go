package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// nullableTime accepts a JSON null (→ nil), a full RFC3339 timestamp, OR a
// date-only "YYYY-MM-DD" (→ that date at midnight UTC) for the per-user
// valid-until fields (review finding 2.9 — a date-only value must not fail
// with a generic JSON error).
type nullableTime struct {
	Value *time.Time
}

func (n *nullableTime) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		n.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			n.Value = &t
			return nil
		}
	}
	return errors.New("expires_at must be an RFC3339 timestamp or YYYY-MM-DD")
}

// This file holds the Spec 2.9 (Admin Rework Effort 1) HTTP handlers: the
// user-group ROLE assignment surface (GET/POST /user-groups/{id}/roles) and the
// per-user qualification assignment surface (POST/DELETE
// /users/{userID}/qualifications/{qualificationID} and PUT
// /users/{userID}/qualifications/{qualificationID}/expiry).
//
// The userGroups sub-mount is gated by `user_groups.manage`; the users
// sub-mount by ANY `users.*` code including `users.qualifications.manage`. The
// core re-verifies the exact code per action defense-in-depth, so a
// fuehrende/schirrmeister holding `users.view` + `users.qualifications.manage`
// can assign/revoke/edit qualifications but cannot edit users or deactivate.

// AssignUserGroupsHandler handles PUT /api/v1/admin/users/{userID}/groups
// (Effort 2): it REPLACES the user's organisational user-group set atomically
// (delete-then-insert, user-detail assignment). Gated by `user_groups.manage`
// (defense-in-depth). Returns the refreshed user detail + German confirmation.
func (h *Handler) AssignUserGroupsHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		UserGroupIDs []string `json:"user_group_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.AssignUserGroups(r.Context(), user, userID, input.UserGroupIDs)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrAdminUserUnknownUserGroup):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserUnknownUserGroup)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user group memberships assign failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// ListUserGroupRolesHandler handles GET /api/v1/admin/user-groups/{groupID}/roles
// (Spec 2.9): the permission groups (roles) an organisational user group grants
// its members. Gated by `user_groups.manage` (sub-mount) + defense-in-depth.
func (h *Handler) ListUserGroupRolesHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")

	roles, err := h.service.ListUserGroupRoles(r.Context(), user, groupID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserGroupNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user group roles list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if roles == nil {
		roles = []*core.RoleGroupRef{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// AssignUserGroupRolesHandler handles POST /api/v1/admin/user-groups/{groupID}/roles
// (Spec 2.9): it REPLACES the group's role set atomically. Gated by
// `user_groups.manage` (sub-mount) + defense-in-depth. Members inherit the new
// roles on the very next resolution.
func (h *Handler) AssignUserGroupRolesHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		RoleIDs []string `json:"role_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	// A missing role_ids field must not silently clear every role a group
	// grants (review finding 2.9 — consistent with the guarded assignee path).
	if input.RoleIDs == nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgUserGroupRolesRequired)
		return
	}

	roles, err := h.service.AssignUserGroupRoles(r.Context(), user, groupID, input.RoleIDs)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserGroupNotFound)
		case errors.Is(err, core.ErrAdminUserUnknownRole):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRoleNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user group roles assign failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if roles == nil {
		roles = []*core.RoleGroupRef{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// AssignUserQualificationHandler handles
// POST /api/v1/admin/users/{userID}/qualifications/{qualificationID}
// (Spec 2.9): it assigns a qualification to a user with an optional per-user
// valid-until. A `fixed` qualification REQUIRES expires_at; an `unlimited` one
// must not carry it. Gated by `users.qualifications.manage` (defense-in-depth).
func (h *Handler) AssignUserQualificationHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	qualificationID := chi.URLParam(r, "qualificationID")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		ExpiresAt nullableTime `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.AssignUserQualification(r.Context(), user, userID, qualificationID, input.ExpiresAt.Value)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrQualificationNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationNotFound)
		case errors.Is(err, core.ErrQualificationExpiryRequired):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationExpiryRequired)
		case errors.Is(err, core.ErrQualificationInvalidExpiresAt):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationInvalidExpiry)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user qualification assign failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// RevokeUserQualificationHandler handles
// DELETE /api/v1/admin/users/{userID}/qualifications/{qualificationID}
// (Spec 2.9): it revokes a qualification from a user immediately. Gated by
// `users.qualifications.manage` (defense-in-depth).
func (h *Handler) RevokeUserQualificationHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	qualificationID := chi.URLParam(r, "qualificationID")

	res, err := h.service.RevokeUserQualification(r.Context(), user, userID, qualificationID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrQualificationNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationNotFound)
		case errors.Is(err, core.ErrQualificationAssignmentNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationAssignmentNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user qualification revoke failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// UpdateUserQualificationExpiryHandler handles
// PUT /api/v1/admin/users/{userID}/qualifications/{qualificationID}/expiry
// (Spec 2.9): it edits a user's per-assignment valid-until. A NULL clears the
// override. Gated by `users.qualifications.manage` (defense-in-depth) — the
// "valid-until can only be updated by fuehrende/schirrmeister/admin" rule.
func (h *Handler) UpdateUserQualificationExpiryHandler(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	qualificationID := chi.URLParam(r, "qualificationID")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		ExpiresAt nullableTime `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.UpdateUserQualificationExpiry(r.Context(), user, userID, qualificationID, input.ExpiresAt.Value)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrQualificationAssignmentNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgQualificationAssignmentNotFound)
		case errors.Is(err, core.ErrQualificationInvalidExpiresAt):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgQualificationInvalidExpiry)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user qualification expiry update failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}