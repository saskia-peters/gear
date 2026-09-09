package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

type mockRepo struct {
	users       map[string]*User
	createCalls int
	getCalls    int
	createErr   error
	permsErr    error
	perms       map[string][]string
	attempts    map[string]*LoginAttempts
	attemptsErr error
	upsertCalls int
	clearCalls  int
	audit       map[string][]string
	auditErr    error
	// auditDetail and auditSeverity record the detail/severity passed to
	// InsertAuditEvent, keyed by user ID, for tests that assert reason and
	// severity persistence (review finding 1.10).
	auditDetail map[string][]string
	auditSeverity map[string][]string
	updateCalls int
	// resetTokens holds the minted reset tokens keyed by token_hash (FR-26).
	resetTokens map[string]*PasswordResetToken
	resetErr    error
	// mustChange users flagged for a forced password change, keyed by user ID.
	mustChange map[string]bool
	// consumeOtpForceLost makes ClearUserOneTimePassword report "already
	// consumed" (false), exercising the concurrent-consume CAS claim failure in
	// Login (Spec 2.8 Design Notes).
	consumeOtpForceLost bool
	// setOtpForceLost makes SetUserOneTimePassword report zero rows affected
	// (the target vanished between the eligibility read and the write), so
	// IssueOneTimePassword maps it to ErrAdminUserNotFound (Spec 2.8).
	setOtpForceLost bool
	// adminGroup is the set of user IDs considered members of the admin group
	// (Story 1.8); IsUserInPermissionGroup resolves against it.
	adminGroup map[string]bool
	// groupName is the only permission group name IsUserInPermissionGroup
	// resolves (tests only ever ask about the admin group).
	// mismatchProfileUser makes UpdateUserProfile/StagePendingEmail return a
	// DIFFERENT user than the caller, exercising the self-ownership
	// defense-in-depth guard (AD-12 → ErrForbidden).
	mismatchProfileUser bool
	// newObjectOnProfile makes UpdateUserProfile/StagePendingEmail return a
	// FRESH user value (like the postgres adapter's userFromRow) instead of the
	// mutated in-memory pointer, so tests can prove the session snapshot is
	// refreshed (review finding: stale session snapshot).
	newObjectOnProfile bool
	// activeAdmins is the count returned by CountActiveAdmins (FR-27 last-admin
	// guard); default derived from the admin group when 0 is not meaningful.
	activeAdmins int
	// adminRecovery holds the admin-recovery tokens keyed by token_hash (FR-27).
	// A nil ApprovedByUserID means the request is still pending.
	adminRecovery map[string]*AdminRecoveryToken
	// adminRecoveryErr makes Approve/ConsumeAdminRecovery surface a genuine
	// store error (review finding 1.10: real errors must propagate, only
	// ErrNoRows maps to ErrAdminRecoveryInvalid).
	adminRecoveryErr error
	// User approval (Story 2.4): listPendingUsers is returned by
	// ListPendingUsers; approveUserFunc/rejectUserFunc override the default
	// pending->active / pending->deactivated transition.
	listPendingUsers []*PendingUser
	listPendingErr   error
	approveErr       error
	rejectErr        error
	approveUserFunc  func(ctx context.Context, userID string) (*User, error)
	rejectUserFunc   func(ctx context.Context, userID string) (*User, error)
	// Role & Permission-Group support (Story 2.5): roleGroups holds the groups
	// keyed by ID; userRoleGroups maps a user ID to the group IDs they hold.
	// UpdateGroup recomputes the resolved permission set (m.perms) of every
	// member, mirroring the live per-request resolution (AD-2/FR-6). Only the
	// groups seeded through CreateGroup/seedRoleGroup participate.
	roleGroups       map[string]*RoleGroup
	roleGroupNextID  int
	userRoleGroups   map[string][]string
	listAllPermErr   error
	catalogLabels    map[string]string
	// User & Group Administration (Story 2.6): adminUsers holds the summary
	// rows returned by ListUsers (nil → derived live from users);
	// userGroups holds the organisational teams keyed by ID;
	// userGroupNextID assigns the next synthetic team ID;
	// userGroupMembers maps a user ID to the team IDs they belong to;
	// directGrants maps a user ID to the permission codes granted directly
	// (additive, AD-12) — they feed the resolved set alongside roles;
	// qualifications maps qualification IDs to their vocabulary row and
	// userQualifications maps a user ID to the qualification IDs they hold.
	adminUsers           []*AdminUserSummary
	adminUsersErr        error
	userGroups           map[string]*UserGroup
	userGroupNextID      int
	userGroupMembers     map[string][]string
	directGrants         map[string][]string
	qualifications       map[string]*QualificationAssignment
	qualificationNextID  int
	userQualifications   map[string][]string
	userDetailFunc       func(ctx context.Context, userID string) (*AdminUserDetail, error)
	createAdminUserFunc  func(ctx context.Context, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*User, error)
	updateAdminUserFunc  func(ctx context.Context, userID, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*User, error)
	deactivateUserFunc   func(ctx context.Context, userID string) (*User, error)
	deleteUserGroupFunc  func(ctx context.Context, groupID string) error
	// Admin Rework Effort 1 (Spec 2.9): userGroupRoles maps a user-group ID to
	// the permission-group (role) IDs it grants its members; the mock keeps the
	// resolved set (m.perms) consistent with the three-way union like the real
	// repository. qualificationExpiry maps a userID+qualificationID to the
	// per-assignment valid-until override.
	userGroupRoles       map[string][]string
	qualificationExpiry  map[string]*time.Time
	listUserGroupRolesErr error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		users:       make(map[string]*User),
		perms:       make(map[string][]string),
		attempts:    make(map[string]*LoginAttempts),
		audit:       make(map[string][]string),
		resetTokens: make(map[string]*PasswordResetToken),
		mustChange:  make(map[string]bool),
		adminGroup:  make(map[string]bool),
		adminRecovery: make(map[string]*AdminRecoveryToken),
		userGroups:     make(map[string]*UserGroup),
		userGroupMembers: make(map[string][]string),
		directGrants:     make(map[string][]string),
		qualifications:   make(map[string]*QualificationAssignment),
		userQualifications: make(map[string][]string),
		userGroupRoles:     make(map[string][]string),
		qualificationExpiry: make(map[string]*time.Time),
	}
}

func (m *mockRepo) CreateRegisteredUser(_ context.Context, email, displayName, firstName, lastName, passwordHash string) (*User, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	u := &User{
		Email:        email,
		DisplayName:  displayName,
		FirstName:    firstName,
		LastName:     lastName,
		PasswordHash: passwordHash,
		State:        StatePendingApproval,
	}
	m.users[email] = u
	return u, nil
}

func (m *mockRepo) GetUserByEmail(_ context.Context, email string) (*User, error) {
	m.getCalls++
	u, ok := m.users[email]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (m *mockRepo) ListPermissionsByUser(_ context.Context, userID string) ([]string, error) {
	if m.permsErr != nil {
		return nil, m.permsErr
	}
	return m.perms[userID], nil
}

func (m *mockRepo) GetLoginAttempts(_ context.Context, email string) (*LoginAttempts, error) {
	if m.attemptsErr != nil {
		return nil, m.attemptsErr
	}
	return m.attempts[email], nil
}

// IncrementLoginAttempts mirrors the postgres adapter's atomic upsert: it
// increments the per-email counter (capped), sets the lockout window when a
// threshold is crossed, and keeps any previously set window until the new count
// moves into a higher tier.
func (m *mockRepo) IncrementLoginAttempts(_ context.Context, email string) error {
	if m.attempts == nil {
		m.attempts = make(map[string]*LoginAttempts)
	}
	cur := 0
	if a := m.attempts[email]; a != nil {
		cur = a.FailedCount
	}
	newCount := cur + 1
	if newCount > LockoutMaxFailedCount {
		newCount = LockoutMaxFailedCount
	}
	now := time.Now().UTC()
	var lockoutUntil time.Time
	switch {
	case newCount >= LockoutThresholdLong:
		lockoutUntil = now.Add(LockoutDurationLong)
	case newCount == LockoutThresholdShort:
		lockoutUntil = now.Add(LockoutDurationShort)
	}
	m.attempts[email] = &LoginAttempts{
		Email:        email,
		FailedCount:  newCount,
		LockoutUntil: lockoutUntil,
		UpdatedAt:    now,
	}
	m.upsertCalls++
	return nil
}

// ClearLoginAttempts resets the email's counter to zero and clears the window,
// keeping the row — mirroring the real repository (UPDATE ... SET
// failed_count = 0).
func (m *mockRepo) ClearLoginAttempts(_ context.Context, email string) error {
	if m.attempts == nil {
		m.attempts = make(map[string]*LoginAttempts)
	}
	m.attempts[email] = &LoginAttempts{
		Email:       email,
		FailedCount: 0,
		UpdatedAt:   time.Now().UTC(),
	}
	m.clearCalls++
	return nil
}

// SetUserTotpSecret stores the encrypted secret and enables MFA on the in-memory
// user, mirroring the postgres adapter (FR-4/NFR-S4).
func (m *mockRepo) SetUserTotpSecret(_ context.Context, userID, encryptedSecret string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.TotpSecretEncrypted = encryptedSecret
			u.IsMFAEnabled = true
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// ClearUserTotpSecret disables MFA and clears the encrypted secret (FR-4).
func (m *mockRepo) ClearUserTotpSecret(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.TotpSecretEncrypted = ""
			u.IsMFAEnabled = false
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// SetUserPendingTotpSecret stores the short-lived pending enrollment (encrypted
// secret + expiry) on the in-memory user.
func (m *mockRepo) SetUserPendingTotpSecret(_ context.Context, userID, encryptedSecret string, expiresAt time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingTotpSecretEncrypted = encryptedSecret
			u.PendingTotpExpiresAt = expiresAt
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// ClearUserPendingTotpSecret clears the pending enrollment.
func (m *mockRepo) ClearUserPendingTotpSecret(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingTotpSecretEncrypted = ""
			u.PendingTotpExpiresAt = time.Time{}
			return nil
		}
	}
	return ErrUserAlreadyExists
}

// UpdateUserPassword replaces the user's stored password hash (FR-25). Only the
// hash is stored; the plaintext password is never kept. An unknown user ID maps
// to ErrUserNotFound (never a misleading "already exists" sentinel).
func (m *mockRepo) UpdateUserPassword(_ context.Context, userID, passwordHash string) (*User, error) {
	m.updateCalls++
	for _, u := range m.users {
		if u.ID == userID {
			u.PasswordHash = passwordHash
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

// InsertAuditEvent appends an audit row (actor_user_id -> operation) to the
// in-memory append-only trail (NFR-O1/NFR-O2). auditErr lets tests simulate an
// audit-write failure (best-effort path). The detail and severity are recorded
// for tests that assert reason/severity persistence (review finding 1.10).
func (m *mockRepo) InsertAuditEvent(_ context.Context, userID, operation, detail, severity string) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	if m.audit == nil {
		m.audit = make(map[string][]string)
	}
	m.audit[userID] = append(m.audit[userID], operation)
	if m.auditDetail == nil {
		m.auditDetail = make(map[string][]string)
	}
	m.auditDetail[userID] = append(m.auditDetail[userID], detail)
	if m.auditSeverity == nil {
		m.auditSeverity = make(map[string][]string)
	}
	m.auditSeverity[userID] = append(m.auditSeverity[userID], severity)
	return nil
}

// InsertAuditEventAnonymous appends an audit row without an actor (review
// findings 1.8-3 / 1.8-10): the row is keyed under the empty-string pseudo
// actor so tests can assert unknown-email enumeration attempts leave a trail.
func (m *mockRepo) InsertAuditEventAnonymous(_ context.Context, operation string) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	if m.audit == nil {
		m.audit = make(map[string][]string)
	}
	m.audit[""] = append(m.audit[""], operation)
	return nil
}

// UpdateUserProfile persists the user's editable base data (first/last/display
// name, Story 2.1) and the custom-attribute set (Story 1.9) and returns the
// updated user. An unknown user ID maps to ErrUserNotFound.
func (m *mockRepo) UpdateUserProfile(_ context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*User, error) {
	m.updateCalls++
	for _, u := range m.users {
		if u.ID == userID {
			u.FirstName = firstName
			u.LastName = lastName
			u.DisplayName = displayName
			u.Attributes = attributes
			if m.mismatchProfileUser {
				return &User{ID: "other-user", Email: "other@example.com"}, nil
			}
			if m.newObjectOnProfile {
				clone := *u
				return &clone, nil
			}
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

// StagePendingEmail stores a staged email change (Story 2.1) on the in-memory
// user. It mirrors the postgres adapter's conditional UPDATE: the staging is
// refused (ErrEmailInUse, "no row updated") while ANY other account holds the
// address as its current email or as an already-staged pending_email,
// compared case-insensitively. A missing target user also maps to
// ErrEmailInUse (the conditional UPDATE affects zero rows).
func (m *mockRepo) StagePendingEmail(_ context.Context, userID, pendingEmail string) (*User, error) {
	var target *User
	for _, u := range m.users {
		if u.ID == userID {
			target = u
			continue
		}
		if strings.EqualFold(u.Email, pendingEmail) || strings.EqualFold(u.PendingEmail, pendingEmail) {
			return nil, ErrEmailInUse
		}
	}
	if target == nil {
		return nil, ErrEmailInUse
	}
	if m.mismatchProfileUser {
		return &User{ID: "other-user", Email: "other@example.com"}, nil
	}
	target.PendingEmail = pendingEmail
	if m.newObjectOnProfile {
		clone := *target
		return &clone, nil
	}
	return target, nil
}

// ClearPendingEmail clears a staged email change (pending_email -> NULL).
func (m *mockRepo) ClearPendingEmail(_ context.Context, userID string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingEmail = ""
			return nil
		}
	}
	return ErrUserNotFound
}

// CreatePasswordResetToken stores the hash of a fresh reset token, invalidating
// earlier tokens of the user (only the latest stays valid, FR-26).
func (m *mockRepo) CreatePasswordResetToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	if m.resetErr != nil {
		return m.resetErr
	}
	for hash := range m.resetTokens {
		if m.resetTokens[hash].UserID == userID {
			delete(m.resetTokens, hash)
		}
	}
	m.resetTokens[tokenHash] = &PasswordResetToken{
		ID:        "token-" + tokenHash[:8],
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
		User:      m.userByID(userID),
	}
	return nil
}

// GetPasswordResetTokenByHash resolves a reset token by hash with its owner.
// Unknown hashes map to ErrResetTokenInvalid.
func (m *mockRepo) GetPasswordResetTokenByHash(_ context.Context, tokenHash string) (*PasswordResetToken, error) {
	t, ok := m.resetTokens[tokenHash]
	if !ok {
		return nil, ErrResetTokenInvalid
	}
	t.User = m.userByID(t.UserID)
	return t, nil
}

// ConsumePasswordResetToken atomically invalidates + returns a reset token
// (review finding 1.8-5): the delete happens with the read, so the losing
// concurrent completion sees no row and maps to ErrResetTokenInvalid.
func (m *mockRepo) ConsumePasswordResetToken(_ context.Context, tokenHash string) (*PasswordResetToken, error) {
	t, ok := m.resetTokens[tokenHash]
	if !ok {
		return nil, ErrResetTokenInvalid
	}
	delete(m.resetTokens, tokenHash)
	t.User = m.userByID(t.UserID)
	return t, nil
}

// DeleteExpiredPasswordResetTokens lazily purges a user's expired tokens
// (review finding 1.8-7).
func (m *mockRepo) DeleteExpiredPasswordResetTokens(_ context.Context, userID string) error {
	now := time.Now().UTC()
	for hash, t := range m.resetTokens {
		if t.UserID == userID && now.After(t.ExpiresAt) {
			delete(m.resetTokens, hash)
		}
	}
	return nil
}

// DeletePasswordResetToken invalidates a reset token after use (single-use).
func (m *mockRepo) DeletePasswordResetToken(_ context.Context, tokenHash string) error {
	delete(m.resetTokens, tokenHash)
	return nil
}

// SetUserMustChangePassword flags the user for a forced password change.
func (m *mockRepo) SetUserMustChangePassword(_ context.Context, userID string) error {
	m.mustChange[userID] = true
	for _, u := range m.users {
		if u.ID == userID {
			u.MustChangePassword = true
		}
	}
	return nil
}

// ClearUserMustChangePassword clears the forced-change flag.
func (m *mockRepo) ClearUserMustChangePassword(_ context.Context, userID string) error {
	delete(m.mustChange, userID)
	for _, u := range m.users {
		if u.ID == userID {
			u.MustChangePassword = false
		}
	}
	return nil
}

// GetUserByID returns a user's profile + state by ID (Spec 2.8 target check).
// An unknown id maps to ErrAdminUserNotFound.
func (m *mockRepo) GetUserByID(_ context.Context, userID string) (*User, error) {
	u := m.userByID(userID)
	if u == nil {
		return nil, ErrAdminUserNotFound
	}
	return u, nil
}

// SetUserOneTimePassword stores the OTP hash + expiry and flags
// must_change_password (Spec 2.8). Re-issuing replaces the previous values. It
// reports whether a row was affected (false = the target vanished).
func (m *mockRepo) SetUserOneTimePassword(_ context.Context, userID, hash string, expiresAt time.Time) (bool, error) {
	if m.setOtpForceLost {
		return false, nil
	}
	u := m.userByID(userID)
	if u == nil {
		return false, ErrAdminUserNotFound
	}
	u.OneTimePasswordHash = hash
	u.OneTimePasswordExpiresAt = expiresAt
	u.MustChangePassword = true
	m.mustChange[userID] = true
	return true, nil
}

// ClearUserOneTimePassword atomically consumes the OTP via compare-and-swap on
// the stored hash (Spec 2.8 Design Notes): the hash must still equal the
// presented one, otherwise the OTP was already consumed by a racing login and
// false is reported. consumeOtpForceLost lets tests force the "already
// consumed" outcome.
func (m *mockRepo) ClearUserOneTimePassword(_ context.Context, userID, hash string) (bool, error) {
	if m.consumeOtpForceLost {
		return false, nil
	}
	u := m.userByID(userID)
	if u == nil || u.OneTimePasswordHash != hash {
		return false, nil
	}
	u.OneTimePasswordHash = ""
	u.OneTimePasswordExpiresAt = time.Time{}
	return true, nil
}

// IsUserInPermissionGroup reports admin-group membership (Story 1.8). Tests
// only ever ask about the admin group, so any other name resolves false.
func (m *mockRepo) IsUserInPermissionGroup(_ context.Context, userID, groupName string) (bool, error) {
	if groupName != AdminGroupName {
		return false, nil
	}
	return m.adminGroup[userID], nil
}

// CountActiveAdmins reports the number of active admin-group members (FR-27
// last-admin guard). An explicit activeAdmins override wins; otherwise it counts
// admin-group members whose account state is active.
func (m *mockRepo) CountActiveAdmins(_ context.Context) (int, error) {
	if m.activeAdmins > 0 {
		return m.activeAdmins, nil
	}
	n := 0
	for uid := range m.adminGroup {
		u := m.userByID(uid)
		if u != nil && u.State == StateActive {
			n++
		}
	}
	return n, nil
}

// CreateAdminRecoveryRequest stores a recovery-marked single-use hashed 30-min
// token, invalidating any earlier recovery request for the user (only the
// latest stays valid, FR-27), stamped with the requesting admin so a requester
// can never approve their own request. The raw token is never stored.
func (m *mockRepo) CreateAdminRecoveryRequest(_ context.Context, userID, requestedByUserID, tokenHash string, expiresAt time.Time) error {
	for hash := range m.adminRecovery {
		if m.adminRecovery[hash].UserID == userID {
			delete(m.adminRecovery, hash)
		}
	}
	m.adminRecovery[tokenHash] = &AdminRecoveryToken{
		ID:                "recovery-" + tokenHash[:8],
		UserID:            userID,
		TokenHash:         tokenHash,
		ExpiresAt:         expiresAt,
		CreatedAt:         time.Now().UTC(),
		RequestedByUserID: requestedByUserID,
		User:              m.userByID(userID),
	}
	return nil
}

// ApproveAdminRecovery mints a fresh token hash onto the target's pending
// recovery request and stamps the approving admin, resetting the expiry to a
// FRESH 30 minutes (FR-27, review finding 1.10). A zero-row update (no pending
// request, already approved, or expired) maps to ErrAdminRecoveryInvalid,
// mirroring the postgres adapter.
func (m *mockRepo) ApproveAdminRecovery(_ context.Context, userID, approvedByUserID, tokenHash string) (string, error) {
	if m.adminRecoveryErr != nil {
		return "", m.adminRecoveryErr
	}
	for hash, t := range m.adminRecovery {
		if t.UserID == userID && t.ApprovedByUserID == "" && time.Now().UTC().Before(t.ExpiresAt) {
			t.ApprovedByUserID = approvedByUserID
			t.ExpiresAt = time.Now().UTC().Add(AdminRecoveryTokenTTL)
			delete(m.adminRecovery, hash)
			t.TokenHash = tokenHash
			t.ID = "recovery-approved-" + tokenHash[:8]
			m.adminRecovery[tokenHash] = t
			return t.ID, nil
		}
	}
	return "", ErrAdminRecoveryInvalid
}

// ConsumeAdminRecoveryToken atomically consumes an APPROVED admin-recovery token
// (FR-27): a missing, not-yet-approved, expired or already-used token maps to
// ErrAdminRecoveryInvalid.
func (m *mockRepo) ConsumeAdminRecoveryToken(_ context.Context, tokenHash string) (*AdminRecoveryToken, error) {
	if m.adminRecoveryErr != nil {
		return nil, m.adminRecoveryErr
	}
	t, ok := m.adminRecovery[tokenHash]
	if !ok || t.ApprovedByUserID == "" {
		return nil, ErrAdminRecoveryInvalid
	}
	delete(m.adminRecovery, tokenHash)
	t.User = m.userByID(t.UserID)
	return t, nil
}

// ListAdminRecoveryRequest returns the pending (not-yet-approved) recovery
// requests, newest first (FR-27). It never carries the password hash (the user
// snapshot is CLONED so clearing the hash never mutates the live user).
func (m *mockRepo) ListAdminRecoveryRequest(_ context.Context) ([]*AdminRecoveryRequest, error) {
	var out []*AdminRecoveryRequest
	for _, t := range m.adminRecovery {
		if t.ApprovedByUserID != "" {
			continue
		}
		req := &AdminRecoveryRequest{
			ID:                t.ID,
			UserID:            t.UserID,
			TokenHash:         t.TokenHash,
			ExpiresAt:         t.ExpiresAt,
			CreatedAt:         t.CreatedAt,
			RequestedByUserID: t.RequestedByUserID,
		}
		if u := m.userByID(t.UserID); u != nil {
			clone := *u
			clone.PasswordHash = ""
			req.User = &clone
		}
		out = append(out, req)
	}
	return out, nil
}

// DenyAdminRecovery invalidates the target's pending recovery request (review
// finding 1.10). A zero-row delete is a no-op.
func (m *mockRepo) DenyAdminRecovery(_ context.Context, userID string) error {
	for hash, t := range m.adminRecovery {
		if t.UserID == userID && t.ApprovedByUserID == "" {
			delete(m.adminRecovery, hash)
		}
	}
	return nil
}

// ListPendingUsers returns the configured pending list (Story 2.4).
func (m *mockRepo) ListPendingUsers(_ context.Context) ([]*PendingUser, error) {
	if m.listPendingErr != nil {
		return nil, m.listPendingErr
	}
	return m.listPendingUsers, nil
}

// ApproveUser activates the pending user and returns it (Story 2.4). The
// default implementation mirrors the postgres adapter's contract: an unknown
// or non-pending user maps to ErrUserNotPending.
func (m *mockRepo) ApproveUser(ctx context.Context, userID string) (*User, error) {
	if m.approveUserFunc != nil {
		return m.approveUserFunc(ctx, userID)
	}
	if m.approveErr != nil {
		return nil, m.approveErr
	}
	u := m.userByID(userID)
	if u == nil || u.State != StatePendingApproval {
		return nil, ErrUserNotPending
	}
	u.State = StateActive
	return u, nil
}

// RejectUser deactivates the pending user and returns it (Story 2.4).
func (m *mockRepo) RejectUser(ctx context.Context, userID string) (*User, error) {
	if m.rejectUserFunc != nil {
		return m.rejectUserFunc(ctx, userID)
	}
	if m.rejectErr != nil {
		return nil, m.rejectErr
	}
	u := m.userByID(userID)
	if u == nil || u.State != StatePendingApproval {
		return nil, ErrUserNotPending
	}
	u.State = StateDeactivated
	return u, nil
}

// seedRoleGroup registers a permission group (keyed by a synthetic id) in the
// mock, optionally attaching it to users (Story 2.5). It is the test-side
// counterpart of the seeded base roles.
func (m *mockRepo) seedRoleGroup(id, name, description string, isBaseRole bool, codes []string, memberIDs ...string) *RoleGroup {
	if m.roleGroups == nil {
		m.roleGroups = make(map[string]*RoleGroup)
	}
	if m.userRoleGroups == nil {
		m.userRoleGroups = make(map[string][]string)
	}
	g := &RoleGroup{ID: id, Name: name, Description: description, IsBaseRole: isBaseRole, Permissions: append([]string(nil), codes...)}
	m.roleGroups[id] = g
	for _, uid := range memberIDs {
		m.userRoleGroups[uid] = append(m.userRoleGroups[uid], id)
	}
	m.recomputeMemberPerms()
	return g
}

// recomputeMemberPerms re-derives the resolved permission set of every user who
// holds at least one tracked group (Story 2.5 immediate-effect semantics): the
// set is the sorted additive union of their groups' codes PLUS their direct
// grants (Story 2.6, AD-12), exactly what the postgres ListPermissionsByUser
// returns live.
func (m *mockRepo) recomputeMemberPerms() {
	if m.perms == nil {
		m.perms = make(map[string][]string)
	}
	union := func(gids []string, direct []string) []string {
		seen := make(map[string]bool)
		var out []string
		for _, gid := range gids {
			if g := m.roleGroups[gid]; g != nil {
				for _, c := range g.Permissions {
					if !seen[c] {
						seen[c] = true
						out = append(out, c)
					}
				}
			}
		}
		for _, c := range direct {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
		sort.Strings(out)
		return out
	}
	for uid, gids := range m.userRoleGroups {
		m.perms[uid] = union(gids, m.directGrants[uid])
	}
	// Users with ONLY direct grants (no role) must still resolve them.
	for uid := range m.directGrants {
		if _, ok := m.userRoleGroups[uid]; !ok {
			m.perms[uid] = union(nil, m.directGrants[uid])
		}
	}
}

// ListGroups returns every registered group, base-roles-first then by name
// (Story 2.5).
func (m *mockRepo) ListGroups(_ context.Context) ([]*RoleGroup, error) {
	out := make([]*RoleGroup, 0, len(m.roleGroups))
	for _, g := range m.roleGroups {
		copy := *g
		copy.Permissions = append([]string(nil), g.Permissions...)
		out = append(out, &copy)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsBaseRole != out[j].IsBaseRole {
			return out[i].IsBaseRole
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// CreateGroup registers a named group (is_base_role=false) with its additive
// permission set (Story 2.5). A case-insensitive duplicate name maps to
// ErrRoleNameTaken; an unknown code maps to ErrUnknownPermissionCode.
func (m *mockRepo) CreateGroup(_ context.Context, name, description string, codes []string) (*RoleGroup, error) {
	if m.roleGroups == nil {
		m.roleGroups = make(map[string]*RoleGroup)
	}
	for _, g := range m.roleGroups {
		if strings.EqualFold(g.Name, name) {
			return nil, ErrRoleNameTaken
		}
	}
	for _, c := range codes {
		if !basePermissionSet[c] {
			return nil, ErrUnknownPermissionCode
		}
	}
	m.roleGroupNextID++
	g := &RoleGroup{ID: fmt.Sprintf("g-%d", m.roleGroupNextID), Name: name, Description: description, IsBaseRole: false, Permissions: append([]string(nil), codes...)}
	m.roleGroups[g.ID] = g
	return g, nil
}

// UpdateGroup replaces a group's name/description and permission set, then
// re-derives the resolved set of every member (Story 2.5 immediate-effect). An
// unknown id maps to ErrRoleNotFound; a case-insensitive duplicate name held by
// ANOTHER group maps to ErrRoleNameTaken.
func (m *mockRepo) UpdateGroup(_ context.Context, id, name, description string, codes []string) (*RoleGroup, error) {
	g, ok := m.roleGroups[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	for oid, other := range m.roleGroups {
		if oid != id && strings.EqualFold(other.Name, name) {
			return nil, ErrRoleNameTaken
		}
	}
	for _, c := range codes {
		if !basePermissionSet[c] {
			return nil, ErrUnknownPermissionCode
		}
	}
	g.Name = name
	g.Description = description
	g.Permissions = append([]string(nil), codes...)
	m.recomputeMemberPerms()
	return g, nil
}

// ListAllPermissions returns the server-authoritative catalog (Story 2.5):
// every base code with its raw label.
func (m *mockRepo) ListAllPermissions(_ context.Context) ([]*PermissionCatalogEntry, error) {
	if m.listAllPermErr != nil {
		return nil, m.listAllPermErr
	}
	out := make([]*PermissionCatalogEntry, 0, len(BasePermissionCodes))
	for _, c := range BasePermissionCodes {
		label := "raw:" + c
		if m.catalogLabels != nil {
			if l, ok := m.catalogLabels[c]; ok {
				label = l
			}
		}
		out = append(out, &PermissionCatalogEntry{Code: c, Label: label})
	}
	return out, nil
}

// seedUserGroup registers an organisational user group (Story 2.6, AD-12) in
// the mock, optionally attaching it to users. It is the test-side counterpart
// of the admin CreateUserGroup path.
func (m *mockRepo) seedUserGroup(id, name, description string, memberIDs ...string) *UserGroup {
	if m.userGroups == nil {
		m.userGroups = make(map[string]*UserGroup)
	}
	g := &UserGroup{ID: id, Name: name, Description: description}
	m.userGroups[id] = g
	for _, uid := range memberIDs {
		m.userGroupMembers[uid] = append(m.userGroupMembers[uid], id)
	}
	return g
}

// ListUsers returns every user summary, ordered by last name then first name
// (Story 2.6). The optional status filter (Spec 2.9) narrows to a single state
// when set.
func (m *mockRepo) ListUsers(_ context.Context, status *string) ([]*AdminUserSummary, error) {
	if m.adminUsersErr != nil {
		return nil, m.adminUsersErr
	}
	if m.adminUsers != nil {
		return m.adminUsers, nil
	}
	out := make([]*AdminUserSummary, 0, len(m.users))
	for _, u := range m.users {
		if status != nil && string(u.State) != *status {
			continue
		}
		out = append(out, &AdminUserSummary{ID: u.ID, Vorname: u.FirstName, Nachname: u.LastName, Email: u.Email, Status: string(u.State)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Nachname != out[j].Nachname {
			return out[i].Nachname < out[j].Nachname
		}
		return out[i].Vorname < out[j].Vorname
	})
	return out, nil
}

// ListUserGroupNamesByUsers returns the organisational team names each listed
// user belongs to, keyed by user id (Effort 2): the mock-side counterpart of
// the repository's one-query-for-the-page lookup. Names are sorted by name.
func (m *mockRepo) ListUserGroupNamesByUsers(_ context.Context, userIDs []string) (map[string][]string, error) {
	want := make(map[string]bool, len(userIDs))
	for _, uid := range userIDs {
		want[uid] = true
	}
	out := make(map[string][]string, len(userIDs))
	for uid, groupIDs := range m.userGroupMembers {
		if !want[uid] {
			continue
		}
		names := make([]string, 0, len(groupIDs))
		for _, gid := range groupIDs {
			if g := m.userGroups[gid]; g != nil {
				names = append(names, g.Name)
			}
		}
		sort.Strings(names)
		out[uid] = names
	}
	return out, nil
}

// GetUserDetail composes a user's detail from the in-memory state (Story 2.6).
// An unknown id maps to ErrAdminUserNotFound.
func (m *mockRepo) GetUserDetail(_ context.Context, userID string) (*AdminUserDetail, error) {
	if m.userDetailFunc != nil {
		return m.userDetailFunc(context.Background(), userID)
	}
	u := m.userByID(userID)
	if u == nil {
		return nil, ErrAdminUserNotFound
	}
	detail := &AdminUserDetail{
		ID:       u.ID,
		Vorname:  u.FirstName,
		Nachname: u.LastName,
		Email:    u.Email,
		Status:   string(u.State),
		Roles:      []RoleGroupRef{},
		UserGroups: []UserGroupRef{},
		DirectGrants: []DirectGrantRef{},
		Qualifications: []QualificationAssignment{},
	}
	for _, gid := range m.userRoleGroups[userID] {
		if g := m.roleGroups[gid]; g != nil {
			detail.Roles = append(detail.Roles, RoleGroupRef{ID: g.ID, Name: g.Name, IsBaseRole: g.IsBaseRole})
		}
	}
	for _, gid := range m.userGroupMembers[userID] {
		if g := m.userGroups[gid]; g != nil {
			detail.UserGroups = append(detail.UserGroups, UserGroupRef{ID: g.ID, Name: g.Name})
		}
	}
	for _, code := range m.directGrants[userID] {
		detail.DirectGrants = append(detail.DirectGrants, DirectGrantRef{Code: code})
	}
	for _, qid := range m.userQualifications[userID] {
		if q := m.qualifications[qid]; q != nil {
			assignment := QualificationAssignment{
				ID: q.ID, Name: q.Name, Description: q.Description, ExpiryKind: q.ExpiryKind, ExpiresAt: q.ExpiresAt,
			}
			// Per-assignment valid-until override (Spec 2.9): a stored
			// per-user expires_at takes precedence over the vocabulary expiry.
			if perUser := m.qualificationExpiry[userID+"\x00"+qid]; perUser != nil {
				assignment.ExpiresAt = perUser
			}
			assignment.Status = qualificationStatus(assignment, time.Now().UTC())
			detail.Qualifications = append(detail.Qualifications, assignment)
		}
	}
	sort.SliceStable(detail.Roles, func(i, j int) bool { return detail.Roles[i].Name < detail.Roles[j].Name })
	sort.SliceStable(detail.UserGroups, func(i, j int) bool { return detail.UserGroups[i].Name < detail.UserGroups[j].Name })
	return detail, nil
}

// CreateAdminUser creates a user (Story 2.6): a case-insensitive duplicate
// email maps to ErrAdminUserEmailTaken; an unknown role/user-group id maps to
// ErrAdminUserUnknownRole/ErrAdminUserUnknownUserGroup; an unknown grant code
// maps to ErrUnknownPermissionCode. The assignments are stored and the
// resolved permission set recomputed (immediate-effect semantics, AD-2).
func (m *mockRepo) CreateAdminUser(_ context.Context, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*User, error) {
	if m.createAdminUserFunc != nil {
		return m.createAdminUserFunc(context.Background(), email, firstName, lastName, state, roleIDs, userGroupIDs, grantCodes)
	}
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return nil, ErrAdminUserEmailTaken
		}
	}
	for _, rid := range roleIDs {
		if m.roleGroups[rid] == nil {
			return nil, ErrAdminUserUnknownRole
		}
	}
	for _, gid := range userGroupIDs {
		if m.userGroups[gid] == nil {
			return nil, ErrAdminUserUnknownUserGroup
		}
	}
	for _, c := range grantCodes {
		if !basePermissionSet[c] {
			return nil, ErrUnknownPermissionCode
		}
	}
	u := &User{
		ID: fmt.Sprintf("u-%d", len(m.users)+1), Email: email,
		FirstName: firstName, LastName: lastName, DisplayName: firstName + " " + lastName,
		State: UserState(state),
	}
	m.users[email] = u
	m.userRoleGroups[u.ID] = append([]string(nil), roleIDs...)
	m.userGroupMembers[u.ID] = append([]string(nil), userGroupIDs...)
	m.directGrants[u.ID] = append([]string(nil), grantCodes...)
	m.recomputeMemberPerms()
	return u, nil
}

// UpdateAdminUser replaces a user's profile + assignment sets (Story 2.6). An
// unknown id maps to ErrAdminUserNotFound; an email held by ANOTHER account
// maps to ErrAdminUserEmailTaken; unknown role/group ids and out-of-series
// grant codes map to their 400 sentinels. The resolved permission set is
// recomputed (immediate effect).
func (m *mockRepo) UpdateAdminUser(_ context.Context, userID, email, firstName, lastName, state string, roleIDs, userGroupIDs, grantCodes []string) (*User, error) {
	if m.updateAdminUserFunc != nil {
		return m.updateAdminUserFunc(context.Background(), userID, email, firstName, lastName, state, roleIDs, userGroupIDs, grantCodes)
	}
	u := m.userByID(userID)
	if u == nil {
		return nil, ErrAdminUserNotFound
	}
	for _, other := range m.users {
		if other.ID != userID && strings.EqualFold(other.Email, email) {
			return nil, ErrAdminUserEmailTaken
		}
	}
	for _, rid := range roleIDs {
		if m.roleGroups[rid] == nil {
			return nil, ErrAdminUserUnknownRole
		}
	}
	for _, gid := range userGroupIDs {
		if m.userGroups[gid] == nil {
			return nil, ErrAdminUserUnknownUserGroup
		}
	}
	for _, c := range grantCodes {
		if !basePermissionSet[c] {
			return nil, ErrUnknownPermissionCode
		}
	}
	u.Email = email
	u.FirstName = firstName
	u.LastName = lastName
	u.DisplayName = firstName + " " + lastName
	u.State = UserState(state)
	m.userRoleGroups[userID] = append([]string(nil), roleIDs...)
	m.userGroupMembers[userID] = append([]string(nil), userGroupIDs...)
	m.directGrants[userID] = append([]string(nil), grantCodes...)
	m.recomputeMemberPerms()
	return u, nil
}

// DeactivateUser flips an ACTIVE user to deactivated (Story 2.6, FR-21). An
// unknown id maps to ErrAdminUserNotFound; a non-active user maps to
// ErrUserNotActiveForDeactivate.
func (m *mockRepo) DeactivateUser(_ context.Context, userID string) (*User, error) {
	if m.deactivateUserFunc != nil {
		return m.deactivateUserFunc(context.Background(), userID)
	}
	u := m.userByID(userID)
	if u == nil {
		return nil, ErrAdminUserNotFound
	}
	if u.State != StateActive {
		return nil, ErrUserNotActiveForDeactivate
	}
	u.State = StateDeactivated
	return u, nil
}

// ListUserGroups returns every organisational user group, ordered by name
// (Story 2.6).
func (m *mockRepo) ListUserGroups(_ context.Context) ([]*UserGroup, error) {
	out := make([]*UserGroup, 0, len(m.userGroups))
	for _, g := range m.userGroups {
		copy := *g
		out = append(out, &copy)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateUserGroup creates an organisational user group (Story 2.6). A
// case-insensitive duplicate name maps to ErrUserGroupNameTaken.
func (m *mockRepo) CreateUserGroup(_ context.Context, name, description string) (*UserGroup, error) {
	for _, g := range m.userGroups {
		if strings.EqualFold(g.Name, name) {
			return nil, ErrUserGroupNameTaken
		}
	}
	m.userGroupNextID++
	g := &UserGroup{ID: fmt.Sprintf("ug-%d", m.userGroupNextID), Name: name, Description: description}
	m.userGroups[g.ID] = g
	return g, nil
}

// AssignUserGroupMembers replaces a group's member set (Story 2.6). An unknown
// group maps to ErrUserGroupNotFound; an unknown member maps to
// ErrUserGroupMemberUnknown. Membership grants no permission (AD-12) — the
// resolved permission set is NOT touched.
func (m *mockRepo) AssignUserGroupMembers(_ context.Context, groupID string, userIDs []string) (*UserGroup, error) {
	g, ok := m.userGroups[groupID]
	if !ok {
		return nil, ErrUserGroupNotFound
	}
	for _, uid := range userIDs {
		if m.userByID(uid) == nil {
			return nil, ErrUserGroupMemberUnknown
		}
	}
	// Rebuild the membership map for the group.
	for uid := range m.userGroupMembers {
		kept := m.userGroupMembers[uid][:0]
		for _, gid := range m.userGroupMembers[uid] {
			if gid != groupID {
				kept = append(kept, gid)
			}
		}
		if len(kept) == 0 {
			delete(m.userGroupMembers, uid)
		} else {
			m.userGroupMembers[uid] = kept
		}
	}
	for _, uid := range userIDs {
		m.userGroupMembers[uid] = append(m.userGroupMembers[uid], groupID)
	}
	copy := *g
	return &copy, nil
}

// ListUserGroupMembers returns the current member user ids of a group (Story
// 2.6). An unknown group maps to ErrUserGroupNotFound.
func (m *mockRepo) ListUserGroupMembers(_ context.Context, groupID string) ([]string, error) {
	if _, ok := m.userGroups[groupID]; !ok {
		return nil, ErrUserGroupNotFound
	}
	var out []string
	for uid, gids := range m.userGroupMembers {
		for _, gid := range gids {
			if gid == groupID {
				out = append(out, uid)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// DeleteUserGroup removes an organisational user group (Story 2.6). An unknown
// group maps to ErrUserGroupNotFound.
func (m *mockRepo) DeleteUserGroup(_ context.Context, groupID string) error {
	if m.deleteUserGroupFunc != nil {
		return m.deleteUserGroupFunc(context.Background(), groupID)
	}
	if _, ok := m.userGroups[groupID]; !ok {
		return ErrUserGroupNotFound
	}
	delete(m.userGroups, groupID)
	for uid, gids := range m.userGroupMembers {
		kept := gids[:0]
		for _, gid := range gids {
			if gid != groupID {
				kept = append(kept, gid)
			}
		}
		if len(kept) == 0 {
			delete(m.userGroupMembers, uid)
		} else {
			m.userGroupMembers[uid] = kept
		}
	}
	return nil
}

// ReplaceUserGroupMemberships replaces the organisational user-group set of a
// user (Effort 2, user-detail assignment). Unknown user → ErrAdminUserNotFound;
// unknown group → ErrAdminUserUnknownUserGroup.
func (m *mockRepo) ReplaceUserGroupMemberships(_ context.Context, userID string, groupIDs []string) (*User, error) {
	u := m.userByID(userID)
	if u == nil {
		return nil, ErrAdminUserNotFound
	}
	for _, gid := range groupIDs {
		if m.userGroups[gid] == nil {
			return nil, ErrAdminUserUnknownUserGroup
		}
	}
	m.userGroupMembers[userID] = append([]string(nil), groupIDs...)
	return u, nil
}

// ListQualificationVocabulary returns every qualification, ordered by name
// (Story 2.7). The status indicator is derived by the core.
func (m *mockRepo) ListQualificationVocabulary(_ context.Context) ([]*Qualification, error) {
	out := make([]*Qualification, 0, len(m.qualifications))
	for _, q := range m.qualifications {
		out = append(out, &Qualification{ID: q.ID, Name: q.Name, Description: q.Description, ExpiryKind: q.ExpiryKind})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateQualification creates a qualification vocabulary row (Story 2.7). A
// case-insensitive duplicate name maps to ErrQualificationNameTaken.
func (m *mockRepo) CreateQualification(_ context.Context, name, description, expiryKind string) (*Qualification, error) {
	for _, q := range m.qualifications {
		if strings.EqualFold(q.Name, name) {
			return nil, ErrQualificationNameTaken
		}
	}
	m.qualificationNextID++
	id := fmt.Sprintf("q-%d", m.qualificationNextID)
	for m.qualifications[id] != nil {
		m.qualificationNextID++
		id = fmt.Sprintf("q-%d", m.qualificationNextID)
	}
	q := &QualificationAssignment{ID: id, Name: name, Description: description, ExpiryKind: expiryKind}
	m.qualifications[id] = q
	return &Qualification{ID: q.ID, Name: q.Name, Description: q.Description, ExpiryKind: q.ExpiryKind}, nil
}

// UpdateQualification replaces a qualification's name/description/expiry kind
// (Story 2.7). An unknown id maps to ErrQualificationNotFound; a name held by
// ANOTHER qualification maps to ErrQualificationNameTaken.
func (m *mockRepo) UpdateQualification(_ context.Context, id, name, description, expiryKind string) (*Qualification, error) {
	q := m.qualifications[id]
	if q == nil {
		return nil, ErrQualificationNotFound
	}
	for otherID, other := range m.qualifications {
		if otherID != id && strings.EqualFold(other.Name, name) {
			return nil, ErrQualificationNameTaken
		}
	}
	q.Name = name
	q.Description = description
	q.ExpiryKind = expiryKind
	return &Qualification{ID: q.ID, Name: q.Name, Description: q.Description, ExpiryKind: q.ExpiryKind}, nil
}

// ListQualificationAssignees returns the users currently assigned a
// qualification (Story 2.7). An unknown qualification maps to
// ErrQualificationNotFound.
func (m *mockRepo) ListQualificationAssignees(_ context.Context, id string) ([]*QualificationAssignee, error) {
	if m.qualifications[id] == nil {
		return nil, ErrQualificationNotFound
	}
	var out []*QualificationAssignee
	for uid, qids := range m.userQualifications {
		for _, qid := range qids {
			if qid == id {
				if u := m.userByID(uid); u != nil {
					out = append(out, &QualificationAssignee{ID: uid, Name: u.DisplayName})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReplaceQualificationAssignees replaces a qualification's assignee set (Story
// 2.7). An unknown qualification maps to ErrQualificationNotFound; an unknown
// user id maps to ErrQualificationAssigneeUnknown. Eligibility follows the
// live assignment rows, so removal is immediately visible on the next
// GetUserDetail (AD-7/FR-22).
func (m *mockRepo) ReplaceQualificationAssignees(_ context.Context, id string, userIDs []string) ([]*QualificationAssignee, error) {
	if m.qualifications[id] == nil {
		return nil, ErrQualificationNotFound
	}
	for _, uid := range userIDs {
		if m.userByID(uid) == nil {
			return nil, ErrQualificationAssigneeUnknown
		}
	}
	// Rebuild the assignment map: drop the qualification from every user, then
	// add it for each requested user.
	for uid, qids := range m.userQualifications {
		kept := qids[:0]
		for _, qid := range qids {
			if qid != id {
				kept = append(kept, qid)
			}
		}
		if len(kept) == 0 {
			delete(m.userQualifications, uid)
		} else {
			m.userQualifications[uid] = kept
		}
	}
	for _, uid := range userIDs {
		m.userQualifications[uid] = append(m.userQualifications[uid], id)
	}
	return m.ListQualificationAssignees(context.Background(), id)
}

// ListUserGroupRoles returns the permission groups (roles) an organisational
// user group grants its members (Spec 2.9). An unknown group maps to
// ErrUserGroupNotFound.
func (m *mockRepo) ListUserGroupRoles(_ context.Context, groupID string) ([]*RoleGroupRef, error) {
	if m.userGroups[groupID] == nil {
		return nil, ErrUserGroupNotFound
	}
	if m.listUserGroupRolesErr != nil {
		return nil, m.listUserGroupRolesErr
	}
	var out []*RoleGroupRef
	for _, roleID := range m.userGroupRoles[groupID] {
		if g := m.roleGroups[roleID]; g != nil {
			out = append(out, &RoleGroupRef{ID: g.ID, Name: g.Name, IsBaseRole: g.IsBaseRole})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReplaceUserGroupRoles replaces an organisational user group's role set
// atomically (Spec 2.9). An unknown group maps to ErrUserGroupNotFound; an
// unknown role id maps to ErrAdminUserUnknownRole. Members inherit the roles
// via the live resolved set, so removal is visible on the next resolution.
func (m *mockRepo) ReplaceUserGroupRoles(_ context.Context, groupID string, roleIDs []string) ([]*RoleGroupRef, error) {
	if m.userGroups[groupID] == nil {
		return nil, ErrUserGroupNotFound
	}
	for _, roleID := range roleIDs {
		if m.roleGroups[roleID] == nil {
			return nil, ErrAdminUserUnknownRole
		}
	}
	m.userGroupRoles[groupID] = roleIDs
	// Recompute the resolved set of every member (three-way union: individual
	// roles + team roles + direct grants) to mirror live per-request resolution.
	for _, u := range m.users {
		m.recomputePermissions(u.ID)
	}
	return m.ListUserGroupRoles(context.Background(), groupID)
}

// AssignQualificationToUser assigns a qualification to a user with an optional
// per-assignment valid-until (Spec 2.9). A `fixed` qualification REQUIRES an
// expires_at (ErrQualificationExpiryRequired); an `unlimited` one must NOT
// carry one (ErrQualificationInvalidExpiresAt). Unknown user → 404, unknown
// qualification → 404.
func (m *mockRepo) AssignQualificationToUser(_ context.Context, userID, qualificationID string, expiresAt *time.Time) error {
	if m.userByID(userID) == nil {
		return ErrAdminUserNotFound
	}
	q := m.qualifications[qualificationID]
	if q == nil {
		return ErrQualificationNotFound
	}
	switch q.ExpiryKind {
	case QualificationExpiryUnlimited:
		if expiresAt != nil {
			return ErrQualificationInvalidExpiresAt
		}
	case QualificationExpiryFixed:
		if expiresAt == nil {
			return ErrQualificationExpiryRequired
		}
	}
	if !containsString(m.userQualifications[userID], qualificationID) {
		m.userQualifications[userID] = append(m.userQualifications[userID], qualificationID)
	}
	if expiresAt != nil {
		m.qualificationExpiry[userID+"\x00"+qualificationID] = expiresAt
	}
	return nil
}

// RevokeQualificationFromUser revokes a qualification from a user (Spec 2.9).
// Unknown user → 404, unknown qualification → 404.
func (m *mockRepo) RevokeQualificationFromUser(_ context.Context, userID, qualificationID string) error {
	if m.userByID(userID) == nil {
		return ErrAdminUserNotFound
	}
	if m.qualifications[qualificationID] == nil {
		return ErrQualificationNotFound
	}
	if !containsString(m.userQualifications[userID], qualificationID) {
		// Unassigned pair maps to the uniform not-found (review finding 2.9).
		return ErrQualificationAssignmentNotFound
	}
	m.userQualifications[userID] = filterString(m.userQualifications[userID], qualificationID)
	delete(m.qualificationExpiry, userID+"\x00"+qualificationID)
	return nil
}

// UpdateUserQualificationExpiry edits a user's per-assignment valid-until
// (Spec 2.9). An unknown user/qualification pair (not assigned) maps to
// ErrQualificationAssignmentNotFound.
func (m *mockRepo) UpdateUserQualificationExpiry(_ context.Context, userID, qualificationID string, expiresAt *time.Time) error {
	if m.userByID(userID) == nil || m.qualifications[qualificationID] == nil {
		return ErrQualificationAssignmentNotFound
	}
	if !containsString(m.userQualifications[userID], qualificationID) {
		return ErrQualificationAssignmentNotFound
	}
	if q := m.qualifications[qualificationID]; q != nil && q.ExpiryKind == QualificationExpiryUnlimited && expiresAt != nil {
		// An unlimited qualification must never carry a per-assignment expiry.
		return ErrQualificationInvalidExpiresAt
	}
	if expiresAt == nil {
		delete(m.qualificationExpiry, userID+"\x00"+qualificationID)
	} else {
		m.qualificationExpiry[userID+"\x00"+qualificationID] = expiresAt
	}
	return nil
}

// recomputePermissions rebuilds a user's resolved permission set as the
// three-way additive union (Spec 2.9): individual roles + roles inherited via
// user-groups + direct grants. Users with NO group memberships/roles/grants
// (e.g. an admin whose perms were seeded directly via m.perms) are left
// untouched so manually-seeded sets survive.
func (m *mockRepo) recomputePermissions(userID string) {
	if len(m.userRoleGroups[userID]) == 0 && len(m.userGroupMembers[userID]) == 0 && len(m.directGrants[userID]) == 0 {
		return
	}
	set := map[string]bool{}
	for _, roleID := range m.userRoleGroups[userID] {
		if g := m.roleGroups[roleID]; g != nil {
			for _, c := range g.Permissions {
				set[c] = true
			}
		}
	}
	for _, groupID := range m.userGroupMembers[userID] {
		for _, roleID := range m.userGroupRoles[groupID] {
			if g := m.roleGroups[roleID]; g != nil {
				for _, c := range g.Permissions {
					set[c] = true
				}
			}
		}
	}
	for _, c := range m.directGrants[userID] {
		set[c] = true
	}
	var out []string
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	m.perms[userID] = out
}

// userByID finds a user by ID across the email-keyed map (the mock repository
// keys users by email; lookups by ID iterate the values).
func (m *mockRepo) userByID(userID string) *User {
	for _, u := range m.users {
		if u.ID == userID {
			return u
		}
	}
	return nil
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func filterString(xs []string, v string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

type mockHasher struct {
	hashCalls   int
	verifyCalls int
}

func (m *mockHasher) Hash(password string) (string, error) {
	m.hashCalls++
	return "hashed:" + password, nil
}

func (m *mockHasher) Verify(password, encodedHash string) (bool, error) {
	m.verifyCalls++
	return encodedHash == "hashed:"+password, nil
}

// VerifyCalls returns the number of Verify invocations (used to assert
// timing-normalization behaviour on login failures).
func (m *mockHasher) VerifyCalls() int {
	return m.verifyCalls
}

// mockSessionStore is an in-memory SessionStore for tests. revokeErr lets
// tests simulate a session-revocation failure (best-effort path in
// ChangePassword, FR-25/NFR-O1).
type mockSessionStore struct {
	sessions  map[string]*Session
	users     map[string]*User
	nextID    int
	revokeErr error
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: make(map[string]*Session)}
}

// withUsers registers users so GetSessionByTokenHash can attach the session
// owner, mirroring the repository's JOIN on users.
func (m *mockSessionStore) withUsers(users ...*User) *mockSessionStore {
	if m.users == nil {
		m.users = make(map[string]*User)
	}
	for _, u := range users {
		m.users[u.ID] = u
	}
	return m
}

func (m *mockSessionStore) CreateSession(_ context.Context, userID, tokenHash string, expiresAt time.Time) (*Session, error) {
	m.nextID++
	s := &Session{
		ID:        fmt.Sprintf("sess-%d", m.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}
	if m.users != nil {
		s.User = m.users[userID]
	}
	m.sessions[tokenHash] = s
	return s, nil
}

func (m *mockSessionStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*Session, error) {
	s, ok := m.sessions[tokenHash]
	if !ok {
		return nil, ErrSessionNotFound
	}
	if m.users != nil {
		s.User = m.users[s.UserID]
	}
	return s, nil
}

func (m *mockSessionStore) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *mockSessionStore) DeleteSessionsByUser(_ context.Context, userID string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

func (m *mockSessionStore) DeleteSessionsByUserExcept(_ context.Context, userID, exceptTokenHash string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	for tokenHash, s := range m.sessions {
		if s.UserID == userID && tokenHash != exceptTokenHash {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

// RefreshSessionUser replaces the user snapshot on every session of the given
// user so a subsequent Validate returns the fresh profile (Story 2.1). The
// mockSessionStore re-reads m.users on GetSessionByTokenHash anyway, but this
// keeps the store contract honest for session stores that cache strictly.
func (m *mockSessionStore) RefreshSessionUser(_ context.Context, user *User) error {
	for _, s := range m.sessions {
		if s.UserID == user.ID {
			s.User = user
		}
	}
	return nil
}

// mockCipher is a reversible SecretCipher used in tests: it shifts every byte
// and hex-encodes it so ciphertext never contains the plaintext secret (letting
// tests assert at-rest encryption) while staying invertible for TOTP checks.
type mockCipher struct{}

func (mockCipher) Encrypt(plaintext string) (string, error) {
	out := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i++ {
		out[i] = plaintext[i] + 1
	}
	return fmt.Sprintf("%x", out), nil
}

func (mockCipher) Decrypt(encoded string) (string, error) {
	if len(encoded)%2 != 0 {
		return "", errors.New("bad ciphertext")
	}
	out := make([]byte, len(encoded)/2)
	for i := 0; i < len(out); i++ {
		hi, lo := encoded[2*i], encoded[2*i+1]
		var b byte
		if _, err := fmt.Sscanf(string([]byte{hi, lo}), "%02x", &b); err != nil {
			return "", errors.New("bad ciphertext")
		}
		out[i] = b - 1
	}
	return string(out), nil
}

// newTestService builds a Service with in-memory repo/hasher/session store.
// The logger is nil (the core falls back to slog.Default()) and the forgot
// rate gate is DISABLED so tests can drive multiple reset requests per email
// (the throttle is exercised explicitly by the rate-limit tests).
func newTestService(repo *mockRepo, hasher *mockHasher) (*Service, *mockSessionStore) {
	store := newMockSessionStore()
	var users []*User
	for _, u := range repo.users {
		users = append(users, u)
	}
	store.withUsers(users...)
	sm := NewSessionManager(store, time.Hour)
	svc := NewService(repo, hasher, sm, mockCipher{}, nil)
	svc.SetForgotThrottleInterval(0)
	return svc, store
}

func TestServiceRegisterHappyPath(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Erika",
		LastName:        "Musterfrau",
		Email:           "erika@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}

	if repo.createCalls != 1 {
		t.Errorf("expected 1 CreateRegisteredUser call, got %d", repo.createCalls)
	}
	if hasher.hashCalls != 1 {
		t.Errorf("expected 1 Hash call, got %d", hasher.hashCalls)
	}

	createdUser := repo.users["erika@example.com"]
	if createdUser == nil {
		t.Fatal("user was not saved in repo")
	}
	if createdUser.DisplayName != "Erika Musterfrau" {
		t.Errorf("displayName = %q, want %q", createdUser.DisplayName, "Erika Musterfrau")
	}
	if createdUser.State != StatePendingApproval {
		t.Errorf("state = %q, want %q", createdUser.State, StatePendingApproval)
	}
}

func TestServiceRegisterDuplicateEmailAntiEnumeration(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	repo.users["existing@example.com"] = &User{
		Email: "existing@example.com",
		State: StateActive,
	}

	input := RegisterInput{
		FirstName:       "Max",
		LastName:        "Mustermann",
		Email:           "existing@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("Register failed for existing email: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}

	// Should not have called create on repo, but should have hashed password for constant time
	if repo.createCalls != 0 {
		t.Errorf("expected 0 CreateRegisteredUser calls, got %d", repo.createCalls)
	}
	if hasher.hashCalls != 1 {
		t.Errorf("expected 1 dummy Hash call for timing protection, got %d", hasher.hashCalls)
	}
}

func TestServiceRegisterValidationErrors(t *testing.T) {
	repo := newMockRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Max",
		LastName:        "Mustermann",
		Email:           "max@example.com",
		Password:        "short",
		PasswordConfirm: "short",
	}

	_, err := svc.Register(context.Background(), input)
	if !errors.Is(err, ErrShortPassword) {
		t.Errorf("expected ErrShortPassword, got: %v", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("expected 0 create calls on validation failure, got %d", repo.createCalls)
	}
}

func TestServiceRegisterDuplicateKeyRaceCondition(t *testing.T) {
	repo := newMockRepo()
	repo.createErr = errors.New("ERROR: duplicate key value violates unique constraint \"users_email_key\" (SQLSTATE 23505)")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	input := RegisterInput{
		FirstName:       "Hans",
		LastName:        "Müller",
		Email:           "hans@example.com",
		Password:        "geheimespasswort123",
		PasswordConfirm: "geheimespasswort123",
	}

	res, err := svc.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("expected duplicate key error to be swallowed into uniform confirmation, got: %v", err)
	}

	if res.Message != UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}
}
