package core

import "errors"

// User-group administration sentinel errors. Handlers map them to the uniform
// envelope (FR-19): ErrAdminUserEmailTaken → 409 conflict, ErrAdminUserNotFound
// → 404 not_found, ErrUserNotActiveForDeactivate → 409/404 (never an existence
// hint), ErrUserGroupNameTaken → 409, ErrUserGroupNotFound → 404, the
// validation errors → 400 invalid/invalid_request.
var (
	// ErrAdminUserEmailTaken is returned when a create/update uses an email
	// already held by another account, compared case-insensitively.
	ErrAdminUserEmailTaken = errors.New("admin user email is already taken")
	// ErrAdminUserNotFound is returned when a detail/edit/deactivate targets an
	// unknown user id (uniform 404, no existence leak beyond what the admin
	// already sees, FR-19).
	ErrAdminUserNotFound = errors.New("admin user not found")
	// ErrAdminUserInvalidName is returned when Vorname/Nachname is empty or
	// exceeds the rune cap (400 invalid_request).
	ErrAdminUserInvalidName = errors.New("admin user name is invalid")
	// ErrAdminUserInvalidEmail is returned when the email is syntactically
	// invalid (400 invalid_request, distinct from the 409 duplicate).
	ErrAdminUserInvalidEmail = errors.New("admin user email is invalid")
	// ErrAdminUserInvalidStatus is returned when the requested status is not one
	// of active/pending_approval (400 invalid_request — deactivation is its own
	// confirmed flow, never a status field).
	ErrAdminUserInvalidStatus = errors.New("admin user status is invalid")
	// ErrAdminUserUnknownRole is returned when a role id does not exist
	// (400 invalid, additive-only).
	ErrAdminUserUnknownRole = errors.New("admin user role is unknown")
	// ErrAdminUserUnknownUserGroup is returned when a user-group id does not
	// exist (400 invalid).
	ErrAdminUserUnknownUserGroup = errors.New("admin user user-group is unknown")
	// ErrUserNotActiveForDeactivate is returned when deactivation targets a
	// user that is not currently active (already deactivated or pending) — the
	// uniform conflict (no existence leak).
	ErrUserNotActiveForDeactivate = errors.New("user is not active, cannot be deactivated")
	// ErrDeactivationNotConfirmed is returned when the client did not confirm
	// the deactivation (400 invalid_request).
	ErrDeactivationNotConfirmed = errors.New("deactivation requires confirmation")
	// ErrSelfDeactivation is returned when an admin tries to deactivate their
	// OWN account (409 conflict with a clear German message) — an admin must
	// never be able to lock themselves out of the module they manage.
	ErrSelfDeactivation = errors.New("cannot deactivate own account")
	// ErrUserGroupNameTaken is returned when a user-group create uses a name
	// already held by another group, compared case-insensitively.
	ErrUserGroupNameTaken = errors.New("user group name is already taken")
	// ErrUserGroupInvalidName is returned when the name is empty or exceeds the
	// 120-rune cap (400 invalid_request, distinct from the 409 duplicate).
	ErrUserGroupInvalidName = errors.New("user group name is invalid")
	// ErrUserGroupNotFound is returned when a group-member assignment targets
	// an unknown user-group id.
	ErrUserGroupNotFound = errors.New("user group not found")
	// ErrUserGroupMemberUnknown is returned when an assigned member user id does
	// not exist (400 invalid).
	ErrUserGroupMemberUnknown = errors.New("user group member is unknown")
)

// German user-facing microcopy for the user-administration surface
// (UX-DR6/UX-DR8).
const (
	// MsgUserCreated confirms a successful account creation.
	MsgUserCreated = "Benutzer angelegt. Zugangsdaten werden separat vergeben."
	// MsgUserUpdated confirms a successful profile/assignment update.
	MsgUserUpdated = "Benutzer gespeichert. Änderungen gelten ab der nächsten Anfrage."
	// MsgUserDeactivated confirms the deactivation ("→ Sofort kein Login").
	MsgUserDeactivated = "Benutzer deaktiviert. Ein Login ist ab sofort nicht mehr möglich."
	// MsgAdminUserEmailTaken is the uniform 409 conflict message.
	MsgAdminUserEmailTaken = "Es gibt bereits ein Konto mit dieser E-Mail-Adresse."
	// MsgAdminUserNotFound is the uniform 404 not-found message.
	MsgAdminUserNotFound = "Der Benutzer wurde nicht gefunden."
	// MsgUserNotActiveForDeactivate is the uniform conflict message for
	// deactivating a non-active user (no existence leak, FR-19).
	MsgUserNotActiveForDeactivate = "Nur aktive Benutzer können deaktiviert werden."
	// MsgDeactivationConfirmationRequired tells the client to confirm the
	// destructive deactivation step.
	MsgDeactivationConfirmationRequired = "Bitte bestätige die Deaktivierung."
	// MsgSelfDeactivation is the uniform 409 message when an admin tries to
	// deactivate their own account.
	MsgSelfDeactivation = "Du kannst dein eigenes Konto nicht deaktivieren."
	// MsgAdminUserInvalidName is the uniform 400 message for a missing/too-long
	// name.
	MsgAdminUserInvalidName = "Bitte gib Vor- und Nachnamen an (maximal 100 Zeichen)."
	// MsgAdminUserInvalidEmail is the uniform 400 message for a malformed email.
	MsgAdminUserInvalidEmail = "Bitte gib eine gültige E-Mail-Adresse ein."
	// MsgAdminUserInvalidStatus is the uniform 400 message for a bad status.
	MsgAdminUserInvalidStatus = "Der Status ist ungültig."
	// MsgAdminUserUnknownRole is the uniform 400 message for an unknown role id.
	MsgAdminUserUnknownRole = "Eine ausgewählte Rolle ist ungültig."
	// MsgAdminUserUnknownUserGroup is the uniform 400 message for an unknown
	// user-group id.
	MsgAdminUserUnknownUserGroup = "Eine ausgewählte Benutzergruppe ist ungültig."
	// MsgUserGroupCreated confirms a successful user-group creation.
	MsgUserGroupCreated = "Benutzergruppe erstellt."
	// MsgUserGroupNameTaken is the uniform 409 conflict message.
	MsgUserGroupNameTaken = "Es gibt bereits eine Benutzergruppe mit diesem Namen."
	// MsgUserGroupNameRequired is the uniform 400 message for an empty/too-long
	// name.
	MsgUserGroupNameRequired = "Bitte gib einen Namen für die Benutzergruppe an (maximal 120 Zeichen)."
	// MsgUserGroupNotFound is the uniform 404 not-found message.
	MsgUserGroupNotFound = "Die Benutzergruppe wurde nicht gefunden."
	// MsgUserGroupMembersUpdated confirms a successful member-set replacement.
	MsgUserGroupMembersUpdated = "Mitglieder der Benutzergruppe aktualisiert."
	// MsgUserGroupDeleted confirms a successful user-group deletion.
	MsgUserGroupDeleted = "Benutzergruppe gelöscht."
	// MsgUserGroupsUpdated confirms a successful user↔user-group membership
	// change on the user detail (Effort 2).
	MsgUserGroupsUpdated = "Benutzergruppen aktualisiert. Änderungen gelten ab sofort."
	// MsgUserGroupMemberUnknown is the uniform 400 message for an unknown
	// assigned member.
	MsgUserGroupMemberUnknown = "Eine ausgewählte Person ist ungültig."
	// MsgUserGroupMembersRequired is the uniform 400 message when the
	// `user_ids` field is missing from a member-set replacement — a nil set
	// must never silently clear every member (retro finding F12).
	MsgUserGroupMembersRequired = "Bitte wähle mindestens eine Person aus."
	// MsgUserGroupsRequired is the uniform 400 message when the
	// `user_group_ids` field is missing from a user↔user-group replacement — a
	// nil set must never silently clear every membership (retro finding F12).
	MsgUserGroupsRequired = "Bitte wähle mindestens eine Benutzergruppe aus."
)

// UserNameMaxLength caps a user's Vorname/Nachname at 100 runes (matches the
// registration bound, Story 1.3).
const UserNameMaxLength = 100

// UserGroupNameMaxLength caps a user-group name at 120 runes (Story 2.6).
const UserGroupNameMaxLength = 120