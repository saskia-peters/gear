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

// ListAdminUserGroups handles GET /api/v1/admin/user-groups (Story 2.6): it
// returns every organisational user group (team), ordered by name, for the
// admin user-group surface and the user editor's assignment grid. An empty
// list is an empty array so the client can show the empty state.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `user_groups.manage` (gateway,
//     hidden existence)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListAdminUserGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	groups, err := h.service.ListUserGroups(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user groups list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user groups list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if groups == nil {
		groups = []*core.UserGroup{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"user_groups": groups})
}

// CreateAdminUserGroup handles POST /api/v1/admin/user-groups (Story 2.6): it
// creates an organisational user group with a unique (case-insensitive) name.
// Organisational groups grant NO permission (AD-12) — only the
// permission-group/direct-grant path affects access.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `user_groups.manage`
//   - 409 conflict when the name is already taken (case-insensitive)
//   - 400 invalid_request when the name is empty/too long or the JSON is
//     malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) CreateAdminUserGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	var input core.CreateUserGroupInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	group, err := h.service.CreateUserGroup(r.Context(), user, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user group create forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNameTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgUserGroupNameTaken)
		case errors.Is(err, core.ErrUserGroupInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgUserGroupNameRequired)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user group create failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user group created", "email", user.Email, "group", group.Name)
	httpapi.WriteJSON(w, http.StatusCreated, group)
}

// assignAdminUserGroupMembersRequest is the body of POST
// /user-groups/{groupID}/members (Story 2.6): the full replacement member set.
type assignAdminUserGroupMembersRequest struct {
	UserIDs []string `json:"user_ids"`
}

// AssignAdminUserGroupMembers handles POST /api/v1/admin/user-groups/{groupID}/members
// (Story 2.6): it REPLACES the group's member set atomically (delete-then-insert
// in one transaction). Membership grants NO permission (AD-12) — this only
// changes team composition, never access. Gated by `user_groups.manage`.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `user_groups.manage`
//   - 404 not_found when the group id is unknown or malformed
//   - 400 invalid when a member user id is unknown
//   - 400 invalid_request when the JSON is malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) AssignAdminUserGroupMembers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")
	var input assignAdminUserGroupMembersRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}
	// A missing user_ids field must not silently clear every member of the
	// group (retro finding F12 — consistent with the guarded roles/assignee
	// endpoints); only an explicit empty array clears.
	if input.UserIDs == nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgUserGroupMembersRequired)
		return
	}

	group, err := h.service.AssignUserGroupMembers(r.Context(), user, groupID, input.UserIDs)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user group assign forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNotFound):
			h.logger.Warn("admin user group assign rejected: unknown group", "email", user.Email, "target", groupID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserGroupNotFound)
		case errors.Is(err, core.ErrUserGroupMemberUnknown):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgUserGroupMemberUnknown)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user group assign failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user group members assigned", "email", user.Email, "group", group.Name, "members", len(input.UserIDs))
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"message": core.MsgUserGroupMembersUpdated,
		"group":   group,
	})
}

// ListAdminUserGroupMembers handles GET /api/v1/admin/user-groups/{groupID}/members
// (Story 2.6): it returns the current member user ids of an organisational
// user group, ordered by id, so the member editor can pre-check the set. An
// empty set is an empty array.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `user_groups.manage`
//   - 404 not_found when the group id is unknown or malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListAdminUserGroupMembers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")

	members, err := h.service.ListUserGroupMembers(r.Context(), user, groupID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user group members list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNotFound):
			h.logger.Warn("admin user group members list rejected: unknown group", "email", user.Email, "target", groupID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserGroupNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user group members list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if members == nil {
		members = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"user_ids": members})
}

// DeleteAdminUserGroup handles DELETE /api/v1/admin/user-groups/{groupID}
// (Story 2.6): it removes an organisational user group. Member rows cascade;
// membership grants NO permission (AD-12), so deleting a team never changes
// anyone's access. Gated by `user_groups.manage`.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `user_groups.manage`
//   - 404 not_found when the group id is unknown or malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) DeleteAdminUserGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")

	if err := h.service.DeleteUserGroup(r.Context(), user, groupID); err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user group delete forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserGroupNotFound):
			h.logger.Warn("admin user group delete rejected: unknown group", "email", user.Email, "target", groupID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserGroupNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user group delete failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user group deleted", "email", user.Email, "group", groupID)
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"message": core.MsgUserGroupDeleted})
}