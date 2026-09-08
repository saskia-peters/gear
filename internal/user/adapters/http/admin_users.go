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

// This file holds the Story 2.4 user-approval handlers (FR-20) and the Story
// 2.6 User & Group Administration handlers (FR-19/FR-21/FR-22, AD-12). All
// routes live under /api/v1/admin/users (approval + user management) and
// /api/v1/admin/user-groups (organisational teams).
//
// The users sub-mount is gated by ANY of the `users.*` codes (users.view/
// users.approve/users.manage — AD-6/FR-19); the approval endpoints STILL
// require `users.approve` and create/edit/deactivate `users.manage`, both
// re-verified in the core defense-in-depth (Design Notes spec 2.6). The
// user-groups sub-mount is gated by `user_groups.manage`. Callers without the
// relevant code get the uniform hidden-existence 403, so these handlers only
// ever see authorized callers.

// ListPendingUsers handles GET /api/v1/admin/users/pending (Story 2.4, FR-20):
// it returns the pending-approval requests, oldest first, with the submitted
// profile details (Vorname, Nachname, E-Mail) plus id and created_at. The
// client only displays; the list is server-authoritative. An empty list is an
// empty array so the client can show the "Keine ausstehenden Anträge" empty
// state.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListPendingUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	pending, err := h.service.ListPending(r.Context(), user)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("pending users list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("pending users list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if pending == nil {
		pending = []*core.PendingUser{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"users": pending})
}

// ApproveUser handles POST /api/v1/admin/users/{userID}/approve (Story 2.4,
// FR-20): it activates the pending user and seeds the default `helfende` role
// (atomic, AD-2). The approval is audited (user.approve, actor + target email,
// NFR-O1).
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 404 not_found when the id is unknown or the user is no longer pending —
//     one uniform message for both, so nothing beyond what the admin already
//     sees is leaked (FR-19)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ApproveUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	res, err := h.service.ApproveUser(r.Context(), user, userID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("user approve forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserNotPending):
			h.logger.Warn("user approve rejected: not pending", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserNotPending)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user approve failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("user approved", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// RejectUser handles POST /api/v1/admin/users/{userID}/reject (Story 2.4,
// FR-20): it moves the pending user to `deactivated` so the pending record
// disappears and the account can neither log in nor re-register. The rejection
// is audited (user.reject, actor + target email, NFR-O1).
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.approve` (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 404 not_found when the id is unknown or the user is no longer pending —
//     one uniform message for both (FR-19)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) RejectUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	res, err := h.service.RejectUser(r.Context(), user, userID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("user reject forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrUserNotPending):
			h.logger.Warn("user reject rejected: not pending", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgUserNotPending)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("user reject failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("user rejected", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// ListAdminUsers handles GET /api/v1/admin/users (Story 2.6): it returns every
// user with {id, vorname, nachname, email, status}, ordered by name, for the
// admin "Benutzer" list surface. The list is server-authoritative; an empty
// list is an empty array so the client can show the empty state. No secret
// material is returned (NFR-O1).
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller holds no `users.*` code (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	// Optional ?status= filter (Spec 2.9): active | pending_approval |
	// deactivated. Absent or empty = all users. An unknown value is passed
	// through; the core maps it to the uniform 400.
	var status *string
	if raw := r.URL.Query().Get("status"); raw != "" {
		status = &raw
	}

	users, err := h.service.ListUsers(r.Context(), user, status)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin users list forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		case errors.Is(err, core.ErrAdminUserInvalidStatus):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidStatus)
		default:
			h.logger.Error("admin users list failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	if users == nil {
		users = []*core.AdminUserSummary{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

// GetAdminUserDetail handles GET /api/v1/admin/users/{userID} (Story 2.6): it
// returns the user's profile + roles (permission groups) + user groups (teams)
// + direct permission grants + qualification assignments with per-assignment
// status. No secret material is returned.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller holds no `users.*` code (gateway, hidden
//     existence) or is no longer an active authorized holder
//   - 404 not_found when the id is unknown or malformed — one uniform message
//     (no existence leak beyond what the admin already sees, FR-19)
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) GetAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")

	detail, err := h.service.GetUserDetail(r.Context(), user, userID)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user detail forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			h.logger.Warn("admin user detail rejected: unknown id", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user detail failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, detail)
}

// CreateAdminUser handles POST /api/v1/admin/users (Story 2.6): it creates a
// user with the submitted profile fields, an explicit initial state (active or
// pending_approval) and the optional assignment sets (roles, user groups,
// direct grants), atomically. New accounts carry no credentials (provisioned
// out-of-band). Gated by `users.manage` at the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.manage`
//   - 409 conflict when the email is already taken (case-insensitive)
//   - 400 invalid when a role/user-group id is unknown or a direct grant is not
//     one of the 22 base codes, or invalid_request when the JSON is malformed,
//     a name is empty/too long, the email is invalid, or the status is invalid
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) CreateAdminUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	var input core.CreateAdminUserInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.CreateAdminUser(r.Context(), user, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user create forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserEmailTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgAdminUserEmailTaken)
		case errors.Is(err, core.ErrAdminUserInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidName)
		case errors.Is(err, core.ErrAdminUserInvalidEmail):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidEmail)
		case errors.Is(err, core.ErrAdminUserInvalidStatus):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidStatus)
		case errors.Is(err, core.ErrAdminUserUnknownRole):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgAdminUserUnknownRole)
		case errors.Is(err, core.ErrAdminUserUnknownUserGroup):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgAdminUserUnknownUserGroup)
		case errors.Is(err, core.ErrUnknownPermissionCode):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgRoleUnknownPermission)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user create failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user created", "email", user.Email, "target", res.User.Email)
	httpapi.WriteJSON(w, http.StatusCreated, res)
}

// UpdateAdminUser handles PUT /api/v1/admin/users/{userID} (Story 2.6): it
// replaces the user's profile fields, state AND all three assignment sets
// (roles, user groups, direct grants) atomically (delete-then-insert in one
// transaction). Because permission resolution is live per request (AD-2/FR-21),
// the change takes effect on the user's very next request. Gated by
// `users.manage` at the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.manage`
//   - 404 not_found when the id is unknown or malformed
//   - 409 conflict when the email is already taken by ANOTHER account
//   - 400 invalid when a role/user-group id is unknown or a direct grant is not
//     one of the 22 base codes, or invalid_request when the JSON is malformed,
//     a name is empty/too long, the email is invalid, or the status is invalid
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) UpdateAdminUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	var input core.UpdateAdminUserInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.UpdateAdminUser(r.Context(), user, userID, input)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user update forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			h.logger.Warn("admin user update rejected: unknown id", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrAdminUserEmailTaken):
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgAdminUserEmailTaken)
		case errors.Is(err, core.ErrAdminUserInvalidName):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidName)
		case errors.Is(err, core.ErrAdminUserInvalidEmail):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidEmail)
		case errors.Is(err, core.ErrAdminUserInvalidStatus):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgAdminUserInvalidStatus)
		case errors.Is(err, core.ErrAdminUserUnknownRole):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgAdminUserUnknownRole)
		case errors.Is(err, core.ErrAdminUserUnknownUserGroup):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgAdminUserUnknownUserGroup)
		case errors.Is(err, core.ErrUnknownPermissionCode):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid", core.MsgRoleUnknownPermission)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user update failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user updated", "email", user.Email, "target", res.User.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// deactivateAdminUserRequest is the body of POST /users/{userID}/deactivate
// (Story 2.6): the mandatory confirmation checkbox — deactivation is
// destructive ("→ Sofort kein Login") and must be explicit.
type deactivateAdminUserRequest struct {
	Confirmed bool `json:"confirmed"`
}

// DeactivateAdminUser handles POST /api/v1/admin/users/{userID}/deactivate
// (Story 2.6, FR-21): it flips an active user's state to `deactivated` and
// revokes the user's sessions, so they cannot authenticate at all ("→ Sofort
// kein Login"). Requires client confirmation (a missing/false confirmation maps
// to 400). Audited (NFR-O1). Gated by `users.manage` at the core.
//
// Error mapping (uniform envelope):
//   - 403 forbidden when the caller lacks `users.manage`
//   - 404 not_found when the id is unknown or malformed
//   - 409 conflict when the user is not active (already deactivated or pending)
//     — one uniform conflict, no existence leak (FR-19)
//   - 400 invalid_request when the confirmation is missing or the JSON is
//     malformed
//   - 401 unauthorized when the caller is not authenticated
func (h *Handler) DeactivateAdminUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}
	userID := chi.URLParam(r, "userID")
	var input deactivateAdminUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiges JSON-Format.")
		return
	}

	res, err := h.service.DeactivateUser(r.Context(), user, userID, input.Confirmed)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrForbidden):
			h.logger.Warn("admin user deactivate forbidden", "email", user.Email)
			httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
		case errors.Is(err, core.ErrAdminUserNotFound):
			h.logger.Warn("admin user deactivate rejected: unknown id", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusNotFound, "not_found", core.MsgAdminUserNotFound)
		case errors.Is(err, core.ErrUserNotActiveForDeactivate):
			h.logger.Warn("admin user deactivate rejected: not active", "email", user.Email, "target", userID)
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgUserNotActiveForDeactivate)
		case errors.Is(err, core.ErrSelfDeactivation):
			h.logger.Warn("admin user deactivate rejected: self-deactivation", "email", user.Email)
			httpapi.WriteError(w, http.StatusConflict, "conflict", core.MsgSelfDeactivation)
		case errors.Is(err, core.ErrDeactivationNotConfirmed):
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", core.MsgDeactivationConfirmationRequired)
		case errors.Is(err, core.ErrInvalidCredentials):
			httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		default:
			h.logger.Error("admin user deactivate failed unexpectedly", "error", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		}
		return
	}

	h.logger.Info("admin user deactivated", "email", user.Email, "target", res.Email)
	httpapi.WriteJSON(w, http.StatusOK, res)
}

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