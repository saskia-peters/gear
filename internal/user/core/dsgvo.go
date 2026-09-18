package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DSGVO data-access export (Story 3.3, FR-24/AD-8): the read-only export port
// the DSGVO orchestrator consumes through the User module's seam
// (user/ports.DSGVOExportPort). The export aggregates the FULL profile (with
// every authenticator stripped — REPORT_SECRETS), the roles / user-group
// memberships / direct grants / qualification assignments (the GetUserDetail
// composition), the user's authentication sessions and the per-email
// login-attempt state. It is a pure read — no writes, no state mutation
// (DSGVO Never: "No hard-delete or user-state mutation here"). A missing user
// maps to ErrAdminUserNotFound (uniform 404).

// UserExportProfile is the report's profile section: the full user row
// (attributes + created_at/updated_at) with secrets excluded. PendingEmail is
// the staged email change awaiting admin approval (empty when none).
type UserExportProfile struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	PendingEmail string         `json:"pending_email,omitempty"`
	DisplayName  string         `json:"display_name"`
	FirstName    string         `json:"first_name"`
	LastName     string         `json:"last_name"`
	State        UserState      `json:"state"`
	IsMFAEnabled bool           `json:"is_mfa_enabled"`
	Attributes   map[string]any `json:"attributes"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// UserSessionExport is one authentication session of the report's auth-history
// section (FR-24). The token hash is deliberately absent — the report carries
// no authenticators (REPORT_SECRETS).
type UserSessionExport struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginAttemptsExport is the report's login-attempt state (FR-3/FR-24): the
// consecutive-failure counter + the active lockout window. LockoutUntil is nil
// while the email is not locked out. Absent entirely (LoginAttempts nil on the
// export) when the email has no tracked attempts — the SPA renders the German
// empty note.
type LoginAttemptsExport struct {
	Email        string     `json:"email"`
	FailedCount  int        `json:"failed_count"`
	LockoutUntil *time.Time `json:"lockout_until"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// UserDataExport is the User module's DSGVO data-access export (FR-24): the
// profile + roles + user groups + direct grants + qualification assignments
// (with the per-assignment display status) + the auth history (sessions +
// login-attempt state). No secret material is ever present.
type UserDataExport struct {
	Profile        UserExportProfile         `json:"profile"`
	Roles          []RoleGroupRef            `json:"roles"`
	UserGroups     []UserGroupRef            `json:"user_groups"`
	DirectGrants   []DirectGrantRef          `json:"direct_grants"`
	Qualifications []QualificationAssignment `json:"qualifications"`
	Sessions       []UserSessionExport       `json:"sessions"`
	LoginAttempts  *LoginAttemptsExport      `json:"login_attempts,omitempty"`
}

// ExportUserData returns the DSGVO data-access export of one user (Story 3.3,
// FR-24/AD-8): the full profile row (secret columns stripped before assembly —
// the report never carries a password hash, TOTP secret or one-time-password
// hash, REPORT_SECRETS), the GetUserDetail composition (roles, user groups,
// direct grants, qualification assignments), the user's sessions (newest
// first) and the per-email login-attempt state. An unknown id maps to
// ErrAdminUserNotFound (uniform 404). The export is UNGATED by design — the
// DSGVO orchestrator re-checks `dsgvo.access_report` defense-in-depth (AD-6);
// this method never touches the actor.
//
// REPORT_NO_AUTH: a user with no sessions/attempts gets an empty Sessions
// slice and a nil LoginAttempts — the SPA renders "Keine …", never an error.
func (s *Service) ExportUserData(ctx context.Context, userID string) (*UserDataExport, error) {
	user, err := s.repo.GetUserByIDFull(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to load user for DSGVO export: %w", err)
	}

	detail, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to load user detail for DSGVO export: %w", err)
	}

	sessions, err := s.repo.ListSessionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to list sessions for DSGVO export: %w", err)
	}

	attempts, err := s.repo.GetLoginAttempts(ctx, user.Email)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to read login attempts for DSGVO export: %w", err)
	}

	now := time.Now().UTC()
	export := &UserDataExport{
		Profile: UserExportProfile{
			ID:           user.ID,
			Email:        user.Email,
			PendingEmail: user.PendingEmail,
			DisplayName:  user.DisplayName,
			FirstName:    user.FirstName,
			LastName:     user.LastName,
			State:        user.State,
			IsMFAEnabled: user.IsMFAEnabled,
			Attributes:   user.Attributes,
			CreatedAt:    user.CreatedAt,
			UpdatedAt:    user.UpdatedAt,
		},
		Roles:          detail.Roles,
		UserGroups:     detail.UserGroups,
		DirectGrants:   detail.DirectGrants,
		Qualifications: detail.Qualifications,
		Sessions:       sessions,
	}
	// An absent attributes map must never serialize as JSON null — the report
	// profile carries a concrete `{}`.
	if export.Profile.Attributes == nil {
		export.Profile.Attributes = map[string]any{}
	}
	// Per-assignment display status is derived server-side (FR-22/AD-7), exactly
	// like the admin user detail.
	for i := range export.Qualifications {
		export.Qualifications[i].Status = qualificationStatus(export.Qualifications[i], now)
	}
	if attempts != nil {
		loginAttempts := &LoginAttemptsExport{
			Email:       attempts.Email,
			FailedCount: attempts.FailedCount,
			UpdatedAt:   attempts.UpdatedAt,
		}
		if !attempts.LockoutUntil.IsZero() {
			t := attempts.LockoutUntil
			loginAttempts.LockoutUntil = &t
		}
		export.LoginAttempts = loginAttempts
	}
	return export, nil
}