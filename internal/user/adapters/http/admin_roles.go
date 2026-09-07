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

// This file holds the Story 2.5 Role & Permission-Group Management handlers
// (AD-12/AD-6/FR-19): GET /groups (list + catalog), POST /groups (create),
// PUT /groups/{id} (update). All routes live under /api/v1/admin/groups and
// are gated at the sub-mount by ANY of the three `roles.*` codes (roles.create/
// roles.edit/roles.assign — the same codes the SPA nav uses for the Rollen
// entry), so a caller without any of them gets the uniform hidden-existence 403.
// The core re-verifies the exact code per action defense-in-depth (create =
// roles.create, edit = roles.edit), so an assign-only holder can list but never
// create/edit.

// ListRoles handles GET /api/v1/admin/groups (Story 2.5): it returns every
// permission group (the four base roles + any custom named groups), sorted
// base-roles-first then by name, each with {id, name, description, is_base_role,
// permissions:[code...]}, plus the server-authoritative 21-code permission
// catalog with German labels (available_permissions) so the SPA editor never
// hardcodes a stale code list.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller holds no `roles.*` code (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	res, err := h.service.ListRoles(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("roles list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("roles list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if res.Groups == nil {
		res.Groups = []*core.RoleGroup{}
	}
	if res.AvailablePermissions == nil {
		res.AvailablePermissions = []*core.PermissionCatalogEntry{}
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// CreateRole handles POST /api/v1/admin/groups (Story 2.5): it creates a named
// permission group (is_base_role=false) with the additive permission set,
// atomically (group row + permission rows in one transaction). The name is
// unique case-insensitively. Gated by `roles.create` at the gateway and
// re-verified in the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `roles.create`
//   - 409 conflict when the name is already taken (case-insensitive)
//   - 400 invalid when a permission code is not one of the 21 base codes, or
//     invalid_request when the name is empty/too long or the JSON is malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	var input core.CreateRoleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	group, err := h.service.CreateRole(r.Context(), user, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("role create forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrRoleNameTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgRoleNameTaken)
		case errors.Is(err, core.ErrRoleInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRoleNameRequired)
		case errors.Is(err, core.ErrRoleInvalidDescription):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRoleDescriptionTooLong)
		case errors.Is(err, core.ErrUnknownPermissionCode):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgRoleUnknownPermission)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("role create failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("role created", "email", user.Email, "group", group.Name)
	httpapi.WriteJSON(w, http.StatusCreated, group)
}

// UpdateRole handles PUT /api/v1/admin/groups/{groupID} (Story 2.5): it
// replaces the group's name/description and its permission set atomically
// (delete-then-insert in one transaction). Base roles are editable (they remain
// the named matrix starting point). Because permission resolution is live per
// request (AD-2/AD-6/FR-6), the change takes effect for every affected user on
// their very next request. Gated by `roles.edit` at the gateway and re-verified
// in the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `roles.edit`
//   - 404 not_found when the id is unknown or malformed
//   - 409 conflict when renaming onto a name another group already holds
//   - 400 invalid when a permission code is not one of the 21 base codes, or
//     invalid_request when the name is empty/too long or the JSON is malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	groupID := chi.URLParam(r, "groupID")
	var input core.UpdateRoleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	group, err := h.service.UpdateRole(r.Context(), user, groupID, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("role update forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrRoleNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgRoleNotFound)
		case errors.Is(err, core.ErrRoleNameTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgRoleNameTaken)
		case errors.Is(err, core.ErrRoleInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRoleNameRequired)
		case errors.Is(err, core.ErrRoleInvalidDescription):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgRoleDescriptionTooLong)
		case errors.Is(err, core.ErrUnknownPermissionCode):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgRoleUnknownPermission)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("role update failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("role updated", "email", user.Email, "group", group.Name)
	httpapi.WriteJSON(w, http.StatusOK, group)
}