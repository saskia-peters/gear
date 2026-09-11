package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Qualification Management (Story 2.7, AD-6/FR-19/FR-22/AD-7): the admin
// "Qualifikationen" surface — create/edit the qualification vocabulary and
// assign/remove volunteers per qualification. The whole surface is gated by
// `qualifications.manage` at the HTTP sub-mount; the core re-verifies the exact
// code defense-in-depth (AD-2/AD-6). The status indicator is derived server-side
// from the qualification's expiry model (reusing the Story 2.6
// `qualificationStatus` derivation — never duplicated). Assigning/removing
// takes effect immediately on the next check because qualification resolution
// is live per request (AD-7/FR-22); nothing here caches. Never-expiring
// qualifications (`unlimited`) never enter the `Bald ablaufend`/`Abgelaufen`
// states (FR-22).

// QualificationsManagePermission gates the whole qualification-management
// surface (AD-6/FR-19). The same code the SPA nav uses for the Qualifikationen
// entry (Story 2.3), so server gate and client visibility never drift.
const QualificationsManagePermission = "qualifications.manage"

// Qualification-management audit operation codes (NFR-O1/NFR-O2). Distinct
// from the user/role/group codes so qualification actions are separately
// auditable.
const (
	AuditOperationQualificationCreate        = "qualification.create"
	AuditOperationQualificationUpdate        = "qualification.update"
	AuditOperationQualificationAssign        = "qualification.assign"
	AuditOperationQualificationRevoke        = "qualification.revoke"
	AuditOperationQualificationValidUntilUpd = "qualification.valid_until.update"
)

// Qualification is one vocabulary entry (Story 2.7, AD-7/FR-22): the name,
// description and the expiry KIND (unlimited/fixed). A qualification itself has
// NO valid-until date (2026-09-08 rework) — a per-user valid-until exists only
// on an assignment (user_qualifications.expires_at, Spec 2.9). The status
// indicator for the vocabulary is derived from the expiry kind; per-assignment
// status derives from the per-user date — see QualificationWithStatus and
// qualificationStatus.
type Qualification struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ExpiryKind  string `json:"expiry_kind"`
}

// QualificationWithStatus is a vocabulary entry with its server-derived status
// indicator (FR-22/AD-7): Unbegrenzt for never-expiring qualifications, and
// Befristet for fixed ones (a fixed qualification has no date of its own — the
// per-user valid-until is set at assignment). The wire value is the stable
// English code; the SPA owns the German display strings.
type QualificationWithStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ExpiryKind  string `json:"expiry_kind"`
	Status      string `json:"status"`
}

// QualificationWriteResult is the payload returned by create/update (Story 2.7,
// finding: server-authoritative success text): the German confirmation message
// plus the resulting qualification with its derived status, so the SPA never
// hardcodes its own success text.
type QualificationWriteResult struct {
	Message       string                   `json:"message"`
	Qualification *QualificationWithStatus `json:"qualification"`
}

// QualificationRosterUser is one entry of the user roster returned alongside
// the qualification list (Story 2.7): id + display name, so the assignment
// editor can pick assignees in one round-trip.
type QualificationRosterUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// QualificationListResult is the GET /qualifications payload (Story 2.7): the
// full vocabulary with per-qualification status indicators, plus the user
// roster for the assignment editor.
type QualificationListResult struct {
	Qualifications []*QualificationWithStatus `json:"qualifications"`
	Users          []*QualificationRosterUser `json:"users"`
}

// QualificationAssignee is one user assigned a qualification (Story 2.7).
type QualificationAssignee struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// QualificationAssignResult is the POST /qualifications/{id}/assignees payload:
// the server-authoritative German confirmation plus the resulting assignee set.
type QualificationAssignResult struct {
	Message   string                    `json:"message"`
	Assignees []*QualificationAssignee  `json:"assignees"`
}

// CreateQualificationInput is the POST /qualifications body (Story 2.7):
// name, description and the expiry kind. `unlimited` never expires; `fixed`
// means a per-user valid-until is required at assignment (no date here).
type CreateQualificationInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ExpiryKind  string `json:"expiry_kind"`
}

// UpdateQualificationInput is the PUT /qualifications/{id} body — the same
// shape as create; the name/description/expiry kind are replaced atomically.
// Editing the expiry kind never rewrites existing assignments (each assignment
// keeps its per-user expires_at).
type UpdateQualificationInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ExpiryKind  string `json:"expiry_kind"`
}

// Qualification-management sentinel errors. Handlers map them to the uniform
// envelope (FR-19): ErrQualificationNameTaken → 409 conflict,
// ErrQualificationNotFound → 404 not_found, the validation errors → 400
// invalid/invalid_request, ErrQualificationAssigneeUnknown → 400 invalid.
var (
	// ErrQualificationNameTaken is returned when a create/update uses a name
	// already held by another qualification, compared case-insensitively.
	ErrQualificationNameTaken = errors.New("qualification name is already taken")
	// ErrQualificationNotFound is returned when an update/assign targets an
	// unknown qualification id (uniform 404, no existence leak beyond what the
	// admin already sees, FR-19).
	ErrQualificationNotFound = errors.New("qualification not found")
	// ErrQualificationInvalidName is returned when the name is empty or exceeds
	// the 120-rune cap (400 invalid_request, distinct from the 409 duplicate).
	ErrQualificationInvalidName = errors.New("qualification name is invalid")
	// ErrQualificationDescriptionTooLong is returned when the description
	// exceeds the 500-rune cap (400 invalid_request).
	ErrQualificationDescriptionTooLong = errors.New("qualification description is too long")
	// ErrQualificationInvalidExpiryKind is returned when expiry_kind is not one
	// of unlimited/fixed (400 invalid_request).
	ErrQualificationInvalidExpiryKind = errors.New("qualification expiry kind is invalid")
	// ErrQualificationInvalidExpiresAt is returned on a per-user assignment when
	// an `unlimited` qualification is assigned WITH a per-user expires_at (400
	// invalid — an unlimited assignment never expires, Spec 2.9).
	ErrQualificationInvalidExpiresAt = errors.New("qualification expires_at is invalid")
	// ErrQualificationAssigneeUnknown is returned when an assigned user id does
	// not exist (400 invalid).
	ErrQualificationAssigneeUnknown = errors.New("qualification assignee is unknown")
	// ErrQualificationExpiryRequired is returned when assigning a `fixed`
	// qualification to a user WITHOUT a per-user expires_at (400 invalid,
	// Spec 2.9 human decision A).
	ErrQualificationExpiryRequired = errors.New("per-user qualification valid-until is required")
	// ErrQualificationAssignmentNotFound is returned when updating a per-user
	// valid-until for a user/qualification pair that is not assigned (404).
	ErrQualificationAssignmentNotFound = errors.New("qualification assignment not found")
)

// German user-facing microcopy for the qualification-management surface
// (UX-DR6/UX-DR8).
const (
	// MsgQualificationCreated confirms a successful vocabulary creation.
	MsgQualificationCreated = "Qualifikation erstellt."
	// MsgQualificationUpdated confirms a successful vocabulary update.
	MsgQualificationUpdated = "Qualifikation gespeichert. Änderungen gelten ab sofort."
	// MsgQualificationNameTaken is the uniform 409 conflict message.
	MsgQualificationNameTaken = "Es gibt bereits eine Qualifikation mit diesem Namen."
	// MsgQualificationNotFound is the uniform 404 not-found message.
	MsgQualificationNotFound = "Die Qualifikation wurde nicht gefunden."
	// MsgQualificationNameRequired is the uniform 400 message for an
	// empty/too-long name.
	MsgQualificationNameRequired = "Bitte gib einen Namen für die Qualifikation an (maximal 120 Zeichen)."
	// MsgQualificationDescriptionTooLong is the uniform 400 message for an
	// over-long description.
	MsgQualificationDescriptionTooLong = "Bitte gib eine kürzere Beschreibung an (maximal 500 Zeichen)."
	// MsgQualificationInvalidExpiry is the uniform 400 message for a bad expiry
	// kind on the vocabulary.
	MsgQualificationInvalidExpiry = "Bitte wähle eine gültige Gültigkeitsdauer aus."
	// MsgQualificationAssigneesUpdated confirms a successful assignee-set
	// replacement.
	MsgQualificationAssigneesUpdated = "Zugewiesene Personen aktualisiert. Änderungen gelten ab sofort."
	// MsgQualificationAssigneeUnknown is the uniform 400 message for an unknown
	// assigned person.
	MsgQualificationAssigneeUnknown = "Eine ausgewählte Person ist ungültig."
	// MsgQualificationExpiryRequired is the uniform 400 message when assigning
	// a fixed qualification without a per-user valid-until (Spec 2.9).
	MsgQualificationExpiryRequired = "Bitte gib ein Ablaufdatum für die Zuweisung an (diese Qualifikation ist nicht unbegrenzt gültig)."
	// MsgQualificationAssignmentNotFound is the uniform 404 message when
	// updating a per-user valid-until for an unassigned qualification.
	MsgQualificationAssignmentNotFound = "Die Qualifikationszuweisung wurde nicht gefunden."
	// MsgQualificationAssignedToUser confirms a successful per-user assignment.
	MsgQualificationAssignedToUser = "Qualifikation zugewiesen. Die Änderung gilt ab sofort."
	// MsgQualificationRevokedFromUser confirms a successful per-user revocation.
	MsgQualificationRevokedFromUser = "Qualifikation entzogen. Die Änderung gilt ab sofort."
	// MsgQualificationValidUntilUpdated confirms a successful valid-until edit.
	MsgQualificationValidUntilUpdated = "Ablaufdatum aktualisiert. Die Änderung gilt ab sofort."
	// MsgQualificationAssigneeRequired is the uniform 400 message when the
	// assignee request omits the `user_ids` field (a malformed request must
	// never silently revoke everyone — only an explicit empty array clears).
	MsgQualificationAssigneeRequired = "Bitte wähle mindestens eine Person aus."
)

// QualificationNameMaxLength caps a qualification name at 120 runes (matches
// the user-group bound, Story 2.6).
const QualificationNameMaxLength = 120

// QualificationDescriptionMaxLength caps a qualification description at 500
// runes (finding: the description length was previously unbounded).
const QualificationDescriptionMaxLength = 500

// ListQualifications returns every qualification with its server-derived
// status indicator plus the full user roster (id, display name) for the
// assignment editor, in one round-trip (Story 2.7). Ordered by name. Gated by
// `qualifications.manage` (defense-in-depth).
func (s *Service) ListQualifications(ctx context.Context, actor *User) (*QualificationListResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	// The vocabulary LIST opens to any caller who can assign qualifications
	// (users.qualifications.manage) as well as vocabulary managers
	// (qualifications.manage) — Effort 2: fuehrende/schirrmeister need the
	// vocabulary to ADD a qualification on the user detail. Create/update/
	// assignees still require `qualifications.manage` (admin-only).
	if err := s.requireAnyPermission(ctx, actor, []string{QualificationsManagePermission, UsersQualificationsManagePermission}); err != nil {
		return nil, err
	}

	quals, err := s.repo.ListQualificationVocabulary(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list qualification vocabulary: %w", err)
	}
	users, err := s.repo.ListUsers(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list roster users: %w", err)
	}

	out := make([]*QualificationWithStatus, 0, len(quals))
	for _, q := range quals {
		out = append(out, &QualificationWithStatus{
			ID:          q.ID,
			Name:        q.Name,
			Description: q.Description,
			ExpiryKind:  q.ExpiryKind,
			Status:      qualificationStatusFor(q),
		})
	}
	roster := make([]*QualificationRosterUser, 0, len(users))
	for _, u := range users {
		name := strings.TrimSpace(u.Vorname + " " + u.Nachname)
		if name == "" {
			// Seed/out-of-band users may lack first/last names; fall back to a
			// neutral German label so the assignment editor never renders an
			// empty checkbox — never the raw email (the roster has no need for
			// it).
			name = "Unbekannt"
		}
		roster = append(roster, &QualificationRosterUser{ID: u.ID, Name: name})
	}
	return &QualificationListResult{Qualifications: out, Users: roster}, nil
}

// CreateQualification creates a qualification vocabulary entry (Story 2.7,
// FR-22/AD-7). The name is unique case-insensitively (a duplicate maps to
// ErrQualificationNameTaken → 409); an empty/too-long name, an over-long
// description or a bad expiry_kind map to the uniform 400s. A qualification
// itself has NO valid-until date (2026-09-08 rework) — `unlimited` never
// expires; `fixed` requires a per-user valid-until at assignment (Spec 2.9).
// Gated by `qualifications.manage` (defense-in-depth). The creation is audited
// (NFR-O1). The result carries the server-authoritative confirmation plus the
// derived status.
func (s *Service) CreateQualification(ctx context.Context, actor *User, input CreateQualificationInput) (*QualificationWriteResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	name, description, expiryKind, err := validateQualificationInput(input.Name, input.Description, input.ExpiryKind)
	if err != nil {
		return nil, err
	}

	q, err := s.repo.CreateQualification(ctx, name, description, expiryKind)
	if err != nil {
		if errors.Is(err, ErrQualificationNameTaken) {
			return nil, ErrQualificationNameTaken
		}
		return nil, fmt.Errorf("user core: failed to create qualification: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationCreate, "qualification="+q.Name, AuditSeverityNormal); err != nil {
		s.log().Warn("qualification create audit write failed", "error", err)
	}
	s.log().Info("qualification created", "actor", actor.Email, "qualification", q.Name, "expiry_kind", expiryKind)

	return &QualificationWriteResult{Message: MsgQualificationCreated, Qualification: &QualificationWithStatus{
		ID:          q.ID,
		Name:        q.Name,
		Description: q.Description,
		ExpiryKind:  q.ExpiryKind,
		Status:      qualificationStatusFor(q),
	}}, nil
}

// UpdateQualification replaces a qualification's name/description/expiry kind
// atomically (Story 2.7). An unknown id maps to ErrQualificationNotFound →
// 404 (checked BEFORE the duplicate-name guard, so updating an unknown id whose
// requested name is held by another qualification never answers 409); a name
// held by ANOTHER qualification maps to ErrQualificationNameTaken → 409.
// Editing the expiry kind does not rewrite existing assignments — each
// assignment keeps its per-user expires_at (Spec 2.9). Gated by
// `qualifications.manage` (defense-in-depth). The update is audited (NFR-O1).
// The result carries the server-authoritative confirmation plus the derived
// status.
func (s *Service) UpdateQualification(ctx context.Context, actor *User, id string, input UpdateQualificationInput) (*QualificationWriteResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	name, description, expiryKind, err := validateQualificationInput(input.Name, input.Description, input.ExpiryKind)
	if err != nil {
		return nil, err
	}

	q, err := s.repo.UpdateQualification(ctx, id, name, description, expiryKind)
	if err != nil {
		if errors.Is(err, ErrQualificationNotFound) {
			return nil, ErrQualificationNotFound
		}
		if errors.Is(err, ErrQualificationNameTaken) {
			return nil, ErrQualificationNameTaken
		}
		return nil, fmt.Errorf("user core: failed to update qualification: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationUpdate, "qualification="+q.Name, AuditSeverityNormal); err != nil {
		s.log().Warn("qualification update audit write failed", "error", err)
	}
	s.log().Info("qualification updated", "actor", actor.Email, "qualification", q.Name, "expiry_kind", expiryKind)

	return &QualificationWriteResult{Message: MsgQualificationUpdated, Qualification: &QualificationWithStatus{
		ID:          q.ID,
		Name:        q.Name,
		Description: q.Description,
		ExpiryKind:  q.ExpiryKind,
		Status:      qualificationStatusFor(q),
	}}, nil
}

// QualificationExists implements the read-only QualificationCatalogPort
// (AD-7/AD-11): an ungated existence check over the qualification vocabulary,
// consumed by the Tool module (Story 4.2) to validate its
// required_qualification_id FK — the Tool write path never joins user tables.
// No actor, no permission re-check: this is the trusted internal read path.
func (s *Service) QualificationExists(ctx context.Context, id string) (bool, error) {
	quals, err := s.repo.ListQualificationVocabulary(ctx)
	if err != nil {
		return false, fmt.Errorf("user core: failed to check qualification existence: %w", err)
	}
	for _, q := range quals {
		if q.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// ListQualificationAssignees returns the users currently assigned a
// qualification (id + display name), ordered by name (Story 2.7). An unknown
// id maps to ErrQualificationNotFound → 404. Gated by `qualifications.manage`
// (defense-in-depth).
func (s *Service) ListQualificationAssignees(ctx context.Context, actor *User, id string) ([]*QualificationAssignee, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	assignees, err := s.repo.ListQualificationAssignees(ctx, id)
	if err != nil {
		if errors.Is(err, ErrQualificationNotFound) {
			return nil, ErrQualificationNotFound
		}
		return nil, fmt.Errorf("user core: failed to list qualification assignees: %w", err)
	}
	return assignees, nil
}

// AssignQualificationUsers REPLACES the assignee set of a qualification
// atomically (delete-then-insert in one transaction, Story 2.7). An unknown
// qualification maps to ErrQualificationNotFound → 404; an unknown user id maps
// to ErrQualificationAssigneeUnknown → 400. Removing a volunteer from the set
// revokes eligibility immediately (AD-7/FR-22) because resolution is live per
// request — verified by test, never cached. Gated by `qualifications.manage`
// (defense-in-depth). The assignment is audited (NFR-O1) and the result carries
// the server-authoritative confirmation plus the resulting assignee set.
func (s *Service) AssignQualificationUsers(ctx context.Context, actor *User, id string, userIDs []string) (*QualificationAssignResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireQualificationsManagePermission(ctx, actor); err != nil {
		return nil, err
	}

	// Deduplicate the assignee set before validation (Story 2.6 finding 10): a
	// client sending duplicate user ids must not trip the existence-count check
	// and produce a spurious ErrQualificationAssigneeUnknown.
	userIDs = dedupeStrings(userIDs)

	assignees, err := s.repo.ReplaceQualificationAssignees(ctx, id, userIDs)
	if err != nil {
		if errors.Is(err, ErrQualificationNotFound) {
			return nil, ErrQualificationNotFound
		}
		if errors.Is(err, ErrQualificationAssigneeUnknown) {
			return nil, ErrQualificationAssigneeUnknown
		}
		return nil, fmt.Errorf("user core: failed to assign qualification users: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationQualificationAssign, fmt.Sprintf("qualification=%s assignees=%d", id, len(userIDs)), AuditSeverityNormal); err != nil {
		s.log().Warn("qualification assign audit write failed", "error", err)
	}
	s.log().Info("qualification assignees assigned", "actor", actor.Email, "qualification", id, "assignees", len(userIDs))

	return &QualificationAssignResult{Message: MsgQualificationAssigneesUpdated, Assignees: assignees}, nil
}

// validateQualificationInput trims the name/description and enforces the
// expiry-kind rule (Story 2.7): the name must be non-empty and ≤120 runes, the
// description ≤500 runes; expiry_kind must be one of unlimited/fixed. There is
// NO vocabulary-level valid-until date (2026-09-08 rework) — a per-user
// valid-until exists only on assignments (Spec 2.9). Returns the normalized
// fields. Validation errors are the uniform 400 sentinels.
func validateQualificationInput(name, description, expiryKind string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len([]rune(name)) > QualificationNameMaxLength {
		return "", "", "", ErrQualificationInvalidName
	}
	if len([]rune(description)) > QualificationDescriptionMaxLength {
		return "", "", "", ErrQualificationDescriptionTooLong
	}
	switch expiryKind {
	case QualificationExpiryUnlimited, QualificationExpiryFixed:
		return name, description, expiryKind, nil
	default:
		return "", "", "", ErrQualificationInvalidExpiryKind
	}
}

// qualificationStatusFor derives the vocabulary display status of a
// qualification from its expiry kind (Story 2.7, FR-22/AD-7): 'unlimited' is
// always Unbegrenzt; 'fixed' is Befristet (a fixed qualification has no date of
// its own — the per-user valid-until is set at assignment, Spec 2.9).
func qualificationStatusFor(q *Qualification) string {
	if q.ExpiryKind == QualificationExpiryUnlimited {
		return QualificationStatusUnlimited
	}
	return QualificationStatusFixed
}

// requireQualificationsManagePermission re-verifies (defense-in-depth) that the
// actor's LIVE permission set holds `qualifications.manage`.
func (s *Service) requireQualificationsManagePermission(ctx context.Context, actor *User) error {
	return s.requireAnyPermission(ctx, actor, []string{QualificationsManagePermission})
}