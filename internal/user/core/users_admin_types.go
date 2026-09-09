package core

import "time"

// AdminUserSummary is one row of the admin "Benutzer" list (Story 2.6): id,
// names, email and state, server-authoritative. Status carries the raw
// account state (active / pending_approval / deactivated) — the client maps it
// to the German badges (aktiv / pending / deaktiviert). UserGroups lists the
// organisational team names the user belongs to (Effort 2): the SPA renders
// them as inline tags on the list row. No secret material.
type AdminUserSummary struct {
	ID       string `json:"id"`
	Vorname  string `json:"vorname"`
	Nachname string `json:"nachname"`
	Email    string `json:"email"`
	Status   string `json:"status"`
	// UserGroups holds the user-group (team) names the user belongs to, ordered
	// by name. Membership grants NO permission (AD-12); the resolution query
	// never joins user_groups. Empty when the user is in no team.
	UserGroups []string `json:"user_groups"`
}

// RoleGroupRef is a permission-group membership of a user, for the user detail
// surface (Story 2.6, AD-12). Roles ARE the access-granting groups; base roles
// stay flagged so the client can badge them.
type RoleGroupRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsBaseRole bool   `json:"is_base_role"`
}

// UserGroupRef is an organisational user-group (team) membership of a user
// (Story 2.6, AD-12). Membership grants NO permission by itself.
type UserGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DirectGrantRef is a direct one-off permission grant of a user (Story 2.6,
// additive AD-12). Separate from roles; joins the resolved set immediately.
type DirectGrantRef struct {
	PermissionID string    `json:"permission_id"`
	Code         string    `json:"code"`
	GrantedAt    time.Time `json:"granted_at"`
}

// QualificationAssignment is one qualification a user holds, for the user
// detail surface (Story 2.6, AD-7/FR-22). The status is derived from the
// qualification's expiry model and the current time (display only — editing
// assignments is Story 2.7).
type QualificationAssignment struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ExpiryKind  string     `json:"expiry_kind"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	AssignedAt  time.Time  `json:"assigned_at"`
	Status      string     `json:"status"`
}

// AdminUserDetail is the full user-detail payload (Story 2.6, Spec 2.9): the
// profile plus roles (permission groups), user groups (teams), direct
// permission grants, qualification assignments AND the resolved-permission
// provenance (each resolved code annotated with its source(s)). No secret
// material.
type AdminUserDetail struct {
	ID                  string                    `json:"id"`
	Vorname             string                    `json:"vorname"`
	Nachname            string                    `json:"nachname"`
	Email               string                    `json:"email"`
	Status              string                    `json:"status"`
	Roles               []RoleGroupRef            `json:"roles"`
	UserGroups          []UserGroupRef            `json:"user_groups"`
	DirectGrants        []DirectGrantRef          `json:"direct_grants"`
	Qualifications      []QualificationAssignment `json:"qualifications"`
	ResolvedPermissions []*PermissionProvenance   `json:"resolved_permissions"`
}

// PermissionProvenance annotates one resolved permission of a user with its
// source (Spec 2.9): SourceKind is "role" (individual permission-group
// membership), "group" (inherited via an organisational user-group) or
// "direct" (direct one-off grant); SourceName is the role name, the user-group
// name, or "direct" respectively. A permission may have MANY provenance rows
// (multi-source, e.g. `tools.manage` from both a role and a team); the core
// groups by code. Computed server-side so Effort 2's "Alle Berechtigungen"
// view is a pure render.
type PermissionProvenance struct {
	Code       string `json:"code"`
	SourceKind string `json:"source_kind"`
	SourceName string `json:"source_name"`
}

// AdminUserWriteResult is the payload returned by create/update (Story 2.6):
// the German confirmation message plus the resulting user detail, so the SPA
// never hardcodes its own success text.
type AdminUserWriteResult struct {
	Message string           `json:"message"`
	User    *AdminUserDetail `json:"user"`
}

// UserGroup is an organisational user group (team, AD-12) for the admin
// user-group surface and the user editor's assignment grid.
type UserGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateAdminUserInput is the POST /users body (Story 2.6): the profile
// fields, the initial state (active or pending_approval) and the optional
// assignment sets (roles via permission-group ids, user groups via
// user-group ids, direct grants via permission codes). Credentials are
// provisioned out-of-band, so no password is part of the input.
type CreateAdminUserInput struct {
	Vorname          string   `json:"vorname"`
	Nachname         string   `json:"nachname"`
	Email            string   `json:"email"`
	Status           string   `json:"status"`
	RoleIDs          []string `json:"role_ids"`
	UserGroupIDs     []string `json:"user_group_ids"`
	DirectGrantCodes []string `json:"direct_grant_codes"`
}

// UpdateAdminUserInput is the PUT /users/{id} body — the same shape as create;
// the profile fields, the state and ALL three assignment sets are replaced
// atomically (delete-then-insert in one transaction, Story 2.5 lesson).
type UpdateAdminUserInput struct {
	Vorname          string   `json:"vorname"`
	Nachname         string   `json:"nachname"`
	Email            string   `json:"email"`
	Status           string   `json:"status"`
	RoleIDs          []string `json:"role_ids"`
	UserGroupIDs     []string `json:"user_group_ids"`
	DirectGrantCodes []string `json:"direct_grant_codes"`
}

// CreateUserGroupInput is the POST /user-groups body (Story 2.6).
type CreateUserGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DeactivateUserResult is the confirmation returned by a deactivation
// (Story 2.6, FR-21): the target's identity so the client can show specific
// feedback. "→ Sofort kein Login" is enforced server-side by the state flip
// (session validation rejects non-active accounts immediately, Story 1.4).
type DeactivateUserResult struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
}