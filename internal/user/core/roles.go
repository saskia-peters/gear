package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Role & Permission-Group Management (Story 2.5, AD-12/AD-6/FR-19): named
// permission groups (roles) can be listed, created and edited through the admin
// "Rollen" surface — no longer only migration-seeded. Groups stay flat (no
// groups-in-groups in V1, AD-12); base roles remain editable as the named
// starting point of the matrix. Editing a role takes effect immediately on the
// next request because permission resolution is live per request (AD-2/AD-6,
// FR-6) — nothing here caches.
//
// The whole surface is gated by any of the three `roles.*` codes at the HTTP
// sub-mount (AD-6/FR-19); the core re-verifies the exact code for each action
// defense-in-depth (mirroring the approval path): listing needs any of the
// three, creating needs `roles.create`, editing needs `roles.edit` — so a
// `roles.assign`-only holder can list but never create/edit.

// Role-management permission codes (AD-12). These are the SAME codes the SPA
// nav uses for the Rollen entry (Story 2.3), so the server gate and the client
// visibility never drift.
const (
	RoleCreatePermission = "roles.create"
	RoleEditPermission   = "roles.edit"
	RoleAssignPermission = "roles.assign"
)

// Role-management audit operation codes (NFR-O1/NFR-O2). Distinct from the
// approval/recovery codes so role/permission-grant changes are separately
// auditable — they are at least as sensitive as user approvals.
const (
	AuditOperationRoleCreate = "role.create"
	AuditOperationRoleUpdate = "role.update"
)

// RoleGroup is the domain representation of a permission group (AD-12). A
// custom named group has is_base_role=false; the four seeded base roles
// (helfende, schirrmeister, fuehrende, admin) are editable like any group but
// stay flagged so the client can badge them. Permissions is the additive
// granted set (checked = granted; no denies ever, FR-6).
type RoleGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsBaseRole  bool     `json:"is_base_role"`
	Permissions []string `json:"permissions"`
}

// PermissionCatalogEntry is one row of the server-authoritative permission
// catalog (Story 2.5): a 23-code base series entry with its German display
// label. Served to the SPA so the editor's checkbox grid never hardcodes a
// stale code list.
type PermissionCatalogEntry struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// RoleListResult is the GET /groups payload: every permission group plus the
// full catalog, so one round-trip renders the whole Rollen surface.
type RoleListResult struct {
	Groups              []*RoleGroup             `json:"groups"`
	AvailablePermissions []*PermissionCatalogEntry `json:"available_permissions"`
}

// CreateRoleInput is the POST /groups body (Story 2.5). Permissions is the
// additive granted set (may be empty — a valid additive empty set).
type CreateRoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// UpdateRoleInput is the PUT /groups/{id} body — the same shape as create; the
// group's name/description AND its permission set are replaced atomically.
type UpdateRoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// Role-management sentinel errors. Handlers map them to the uniform envelope
// (FR-19): ErrRoleNameTaken → 409 conflict, ErrUnknownPermissionCode → 400
// invalid, ErrRoleNotFound → 404 not_found.
var (
	// ErrRoleNameTaken is returned when a create/update uses a name already held
	// by another group, compared case-insensitively.
	ErrRoleNameTaken = errors.New("permission group name is already taken")
	// ErrRoleInvalidName is returned when the name is empty or exceeds the
	// 120-rune cap (400 invalid_request, distinct from the 409 duplicate).
	ErrRoleInvalidName = errors.New("permission group name is invalid")
	// ErrUnknownPermissionCode is returned when the input grants a code outside
	// the 23-code base series (additive-only, FR-6).
	ErrUnknownPermissionCode = errors.New("unknown permission code")
	// ErrRoleNotFound is returned when an update targets an unknown group id.
	ErrRoleNotFound = errors.New("permission group not found")
	// ErrRoleInvalidDescription is returned when the description exceeds the
	// rune cap (400 invalid_request).
	ErrRoleInvalidDescription = errors.New("permission group description is invalid")
)

// German user-facing microcopy for the Rollen surface (UX-DR6/UX-DR8).
const (
	// MsgRoleCreated confirms a successful group creation.
	MsgRoleCreated = "Rolle erstellt. Sie ist jetzt verfügbar zum Zuweisen."
	// MsgRoleUpdated confirms a successful group update.
	MsgRoleUpdated = "Rolle gespeichert. Änderungen gelten ab der nächsten Anfrage."
	// MsgRoleNameTaken is the uniform 409 conflict message.
	MsgRoleNameTaken = "Es gibt bereits eine Rolle mit diesem Namen."
	// MsgRoleUnknownPermission is the uniform 400 invalid message for a code
	// outside the 23 base codes.
	MsgRoleUnknownPermission = "Eine ausgewählte Berechtigung ist ungültig."
	// MsgRoleNotFound is the uniform 404 not-found message for an unknown group.
	MsgRoleNotFound = "Die Rolle wurde nicht gefunden."
	// MsgRoleNameRequired is the uniform 400 message for an empty/too-long name.
	MsgRoleNameRequired = "Bitte gib einen Namen für die Rolle an (maximal 120 Zeichen)."
	// MsgRoleDescriptionTooLong is the uniform 400 message for an over-long
	// description.
	MsgRoleDescriptionTooLong = "Die Beschreibung ist zu lang (maximal 500 Zeichen)."
)

// RoleNameMaxLength caps a permission-group name at 120 runes (Story 2.5).
const RoleNameMaxLength = 120

// RoleDescriptionMaxLength caps a permission-group description at 500 runes
// (Story 2.5). An empty description is fine (it is stored as '').
const RoleDescriptionMaxLength = 500

// BasePermissionCodes is the full AD-12 base series (Story 2.2/2.5, Spec 2.9,
// 000016): the only codes the role editor may grant. The list is the
// server-authoritative source the catalog and the create/update validation
// draw from — it never drifts from the seed.
var BasePermissionCodes = []string{
	"dashboard.view",
	"inspection.submit",
	"inspection.history.view",
	"report.export",
	"tool.reinstate",
	"tools.manage",
	"tool_types.manage",
	"users.view",
	"users.approve",
	"users.manage",
	"users.qualifications.manage",
	"user_groups.manage",
	"roles.create",
	"roles.edit",
	"roles.assign",
	"qualifications.manage",
	"dsgvo.access_report",
	"dsgvo.delete",
	"admin.recovery.approve",
	"user.account.approve",
	"admin.settings.email",
	"admin.settings.backup",
	"schedules.manage",
}

// basePermissionSet is the O(1) membership check for validation.
var basePermissionSet = func() map[string]bool {
	m := make(map[string]bool, len(BasePermissionCodes))
	for _, c := range BasePermissionCodes {
		m[c] = true
	}
	return m
}()

// permissionLabels maps every base code to its German display label (UX-DR4/
// DR8, Story 2.5). Server-authoritative: the SPA editor consumes the catalog
// built from this map, so labels never drift. The raw DB description is the
// fallback for a code without a label.
var permissionLabels = map[string]string{
	"dashboard.view":          "Dashboard ansehen",
	"inspection.submit":       "Prüfung einreichen",
	"inspection.history.view": "Prüfungshistorie ansehen",
	"report.export":           "Berichte exportieren",
	"tool.reinstate":          "Gerät wiederherstellen",
	"tools.manage":            "Geräte verwalten",
	"tool_types.manage":       "Gerätetypen verwalten",
	"users.view":              "Benutzer ansehen",
	"users.approve":           "Benutzerfreigaben erteilen",
	"users.manage":            "Benutzer verwalten",
	"users.qualifications.manage": "Qualifikationen an Benutzer vergeben",
	"user_groups.manage":      "Benutzergruppen verwalten",
	"roles.create":            "Rollen erstellen",
	"roles.edit":              "Rollen bearbeiten",
	"roles.assign":            "Rollen zuweisen",
	"qualifications.manage":   "Qualifikationen verwalten",
	"dsgvo.access_report":     "DSGVO-Auskünfte erteilen",
	"dsgvo.delete":            "Daten löschen (DSGVO)",
	"admin.recovery.approve":  "Kontowiederherstellung freigeben",
	"admin.settings.email":    "E-Mail-Einstellungen verwalten",
	"admin.settings.backup":   "Sicherungen verwalten",
	"schedules.manage":        "Zeitpläne verwalten",
}

// permissionLabel returns the German label for a code, falling back to the
// raw stored description when the map has no entry (a future code must still
// render, never blank).
func permissionLabel(code, fallback string) string {
	if l, ok := permissionLabels[code]; ok {
		return l
	}
	return fallback
}

// ListRoles returns every permission group (base roles first, then name) plus
// the full 23-code permission catalog with German labels (Story 2.5). The
// caller is gated by any of the `roles.*` codes upstream; here it is
// re-verified defense-in-depth (AD-2/AD-6).
func (s *Service) ListRoles(ctx context.Context, actor *User) (*RoleListResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireAnyRolePermission(ctx, actor, []string{RoleCreatePermission, RoleEditPermission, RoleAssignPermission}); err != nil {
		return nil, err
	}

	groups, err := s.repo.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list permission groups: %w", err)
	}
	rawCatalog, err := s.repo.ListAllPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list permission catalog: %w", err)
	}
	catalog := make([]*PermissionCatalogEntry, 0, len(rawCatalog))
	for _, e := range rawCatalog {
		catalog = append(catalog, &PermissionCatalogEntry{Code: e.Code, Label: permissionLabel(e.Code, e.Label)})
	}
	return &RoleListResult{Groups: groups, AvailablePermissions: catalog}, nil
}

// CreateRole creates a named permission group (is_base_role=false) and its
// permission rows atomically (Story 2.5, AD-12). The name is unique
// case-insensitively (a duplicate maps to ErrRoleNameTaken → 409). Only the 22
// base codes are accepted (additive, no denies — any unknown code maps to
// ErrUnknownPermissionCode → 400). The caller must hold `roles.create`.
func (s *Service) CreateRole(ctx context.Context, actor *User, input CreateRoleInput) (*RoleGroup, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireRoleCreatePermission(ctx, actor); err != nil {
		return nil, err
	}

	name, description, codes, err := validateRoleInput(input.Name, input.Description, input.Permissions)
	if err != nil {
		return nil, err
	}

	group, err := s.repo.CreateGroup(ctx, name, description, codes)
	if err != nil {
		if errors.Is(err, ErrRoleNameTaken) {
			return nil, ErrRoleNameTaken
		}
		return nil, fmt.Errorf("user core: failed to create permission group: %w", err)
	}
	// Best-effort audit (NFR-O1, like the approval path): a failed audit write is
	// logged, never rolled back into the creation. Detail is the group name and
	// permission count — never sensitive.
	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationRoleCreate, fmt.Sprintf("group=%s permissions=%d", group.Name, len(group.Permissions)), AuditSeverityNormal); err != nil {
		s.log().Warn("role create audit write failed", "error", err)
	}
	s.log().Info("role created", "actor", actor.Email, "group", group.Name, "permissions", len(group.Permissions))
	return group, nil
}

// UpdateRole replaces a permission group's name/description AND its permission
// set atomically (delete-then-insert in one transaction, Story 2.5). Base
// roles are editable (they remain the named matrix starting point, AD-12). An
// unknown id maps to ErrRoleNotFound → 404; renaming onto a taken name maps to
// ErrRoleNameTaken → 409. The caller must hold `roles.edit`. Because
// permission resolution is live per request (AD-2/AD-6/FR-6), the change
// reaches every affected user on their very next request — verified by test,
// never cached.
func (s *Service) UpdateRole(ctx context.Context, actor *User, id string, input UpdateRoleInput) (*RoleGroup, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireRoleEditPermission(ctx, actor); err != nil {
		return nil, err
	}

	name, description, codes, err := validateRoleInput(input.Name, input.Description, input.Permissions)
	if err != nil {
		return nil, err
	}

	group, err := s.repo.UpdateGroup(ctx, id, name, description, codes)
	if err != nil {
		if errors.Is(err, ErrRoleNameTaken) {
			return nil, ErrRoleNameTaken
		}
		if errors.Is(err, ErrRoleNotFound) {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("user core: failed to update permission group: %w", err)
	}
	// Best-effort audit (NFR-O1): a failed audit write is logged, never rolled
	// back into the update. Detail is the group name and permission count —
	// never sensitive.
	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationRoleUpdate, fmt.Sprintf("group=%s permissions=%d", group.Name, len(group.Permissions)), AuditSeverityNormal); err != nil {
		s.log().Warn("role update audit write failed", "error", err)
	}
	s.log().Info("role updated", "actor", actor.Email, "group", group.Name, "permissions", len(group.Permissions))
	return group, nil
}

// validateRoleInput trims the name AND description, enforces their length caps
// and verifies every granted code is one of the 23 base codes (additive-only,
// FR-6). It returns the trimmed description, the deduplicated, order-preserved
// code set. Empty/too-long names map to ErrRoleInvalidName (400); an over-long
// description maps to ErrRoleInvalidDescription (400); an unknown code maps to
// ErrUnknownPermissionCode (400).
func validateRoleInput(name, description string, codes []string) (string, string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > RoleNameMaxLength {
		return "", "", nil, ErrRoleInvalidName
	}
	description = strings.TrimSpace(description)
	if len([]rune(description)) > RoleDescriptionMaxLength {
		return "", "", nil, ErrRoleInvalidDescription
	}
	seen := make(map[string]bool, len(codes))
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if !basePermissionSet[c] {
			return "", "", nil, ErrUnknownPermissionCode
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return name, description, out, nil
}

// requireAnyRolePermission re-verifies (defense-in-depth) that the actor's LIVE
// permission set holds ANY of the given `roles.*` codes (AD-12/AD-6).
func (s *Service) requireAnyRolePermission(ctx context.Context, actor *User, required []string) error {
	perms, err := s.repo.ListPermissionsByUser(ctx, actor.ID)
	if err != nil {
		return fmt.Errorf("user core: failed to resolve actor permissions: %w", err)
	}
	for _, want := range required {
		for _, p := range perms {
			if p == want {
				return nil
			}
		}
	}
	return ErrForbidden
}

// requireRoleCreatePermission guards create (defense-in-depth): the actor must
// hold `roles.create` — an assign-only holder may list but never create.
func (s *Service) requireRoleCreatePermission(ctx context.Context, actor *User) error {
	return s.requireAnyRolePermission(ctx, actor, []string{RoleCreatePermission})
}

// requireRoleEditPermission guards update (defense-in-depth): the actor must
// hold `roles.edit`.
func (s *Service) requireRoleEditPermission(ctx context.Context, actor *User) error {
	return s.requireAnyRolePermission(ctx, actor, []string{RoleEditPermission})
}