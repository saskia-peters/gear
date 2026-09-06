package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/user/core"
)

// Repository wraps the sqlc-generated Queries to implement core.Repository.
type Repository struct {
	queries *Queries
}

// NewRepository creates a new postgres user repository.
func NewRepository(queries *Queries) *Repository {
	return &Repository{queries: queries}
}

// CreateRegisteredUser inserts a new user record in pending_approval state.
func (r *Repository) CreateRegisteredUser(ctx context.Context, email, displayName, firstName, lastName, passwordHash string) (*core.User, error) {
	row, err := r.queries.CreateRegisteredUser(ctx, CreateRegisteredUserParams{
		Email:        email,
		DisplayName:  displayName,
		FirstName:    firstName,
		LastName:     lastName,
		PasswordHash: passwordHash,
	})
	if err != nil {
		// Reliable duplicate-key detection (review finding): match the Postgres
		// error code directly instead of fragile string matching. The core's
		// isDuplicateKeyErr remains only as a belt-and-suspenders fallback.
		if isPgUniqueViolation(err) {
			return nil, core.ErrUserAlreadyExists
		}
		return nil, err
	}

	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// GetUserByEmail queries a user by their email address. If not found, returns nil, nil.
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*core.User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// ListPermissionsByUser resolves the user's live permission set (AD-12):
// the additive union of permission-group memberships and direct grants.
func (r *Repository) ListPermissionsByUser(ctx context.Context, userID string) ([]string, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListPermissionsByUser(ctx, uid)
}

// CreateSession persists a new server-side session row and returns its domain
// representation (NFR-S2).
func (r *Repository) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*core.Session, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.CreateSession(ctx, CreateSessionParams{
		UserID:    uid,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return &core.Session{
		ID:        uuidToString(row.ID.Bytes),
		UserID:    uuidToString(row.UserID.Bytes),
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// GetSessionByTokenHash looks up a session by its hashed token, returning the
// associated user. Not found maps to core.ErrSessionNotFound.
func (r *Repository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*core.Session, error) {
	row, err := r.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrSessionNotFound
		}
		return nil, err
	}

	var attrs map[string]any
	if len(row.Attributes) > 0 {
		if err := json.Unmarshal(row.Attributes, &attrs); err != nil {
			// A stored attributes value that is not a JSON object (written
			// out-of-band) must surface as a clear error, never a crash or a
			// silent data-loss read (Story 1.9 boundary). The auth gateway maps
			// this session-resolution failure to the uniform 401 envelope.
			return nil, fmt.Errorf("user postgres: invalid stored attributes jsonb: %w", err)
		}
	}

	secret := ""
	if row.TotpSecretEncrypted.Valid {
		secret = row.TotpSecretEncrypted.String
	}
	pendingSecret := ""
	if row.PendingTotpSecretEncrypted.Valid {
		pendingSecret = row.PendingTotpSecretEncrypted.String
	}
	var pendingExpiry time.Time
	if row.PendingTotpExpiresAt.Valid {
		pendingExpiry = row.PendingTotpExpiresAt.Time
	}
	pendingEmail := ""
	if row.PendingEmail.Valid {
		pendingEmail = row.PendingEmail.String
	}

return &core.Session{
			ID:        uuidToString(row.ID.Bytes),
			UserID:    uuidToString(row.UserID.Bytes),
			TokenHash: row.TokenHash,
			ExpiresAt: row.ExpiresAt.Time,
			CreatedAt: row.CreatedAt.Time,
			User: &core.User{
				ID:                         uuidToString(row.UserID.Bytes),
				Email:                      row.Email,
				PendingEmail:               pendingEmail,
				DisplayName:                row.DisplayName,
				FirstName:                  row.FirstName,
				LastName:                   row.LastName,
				State:                      core.UserState(row.State),
				IsMFAEnabled:               row.IsMfaEnabled,
				MustChangePassword:         row.MustChangePassword,
				PasswordHash:               row.PasswordHash,
				TotpSecretEncrypted:        secret,
				PendingTotpSecretEncrypted: pendingSecret,
				PendingTotpExpiresAt:       pendingExpiry,
				Attributes:                 attrs,
			},
		}, nil
}

// DeleteSessionByTokenHash removes a session row server-side by its hashed
// token (NFR-S2). Atomic: no Get-then-Delete window. Unknown hashes are a no-op.
func (r *Repository) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	return r.queries.DeleteSessionByTokenHash(ctx, tokenHash)
}

// GetLoginAttempts reads the email's login attempt record (FR-3). Returns nil,
// nil when the email has no tracked attempts yet.
func (r *Repository) GetLoginAttempts(ctx context.Context, email string) (*core.LoginAttempts, error) {
	row, err := r.queries.GetLoginAttempts(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &core.LoginAttempts{
		Email:        row.Email,
		FailedCount:  int(row.FailedCount),
		LockoutUntil: row.LockoutUntil.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}

// IncrementLoginAttempts atomically records a failed login for the email: it
// increments the failure counter and sets the progressive lockout window when a
// threshold is crossed, in a single statement (FR-3) so concurrent attempts for
// the same email cannot lose updates.
func (r *Repository) IncrementLoginAttempts(ctx context.Context, email string) error {
	return r.queries.IncrementLoginAttempts(ctx, email)
}

// ClearLoginAttempts resets the email's failure counter and lockout window
// after a successful login (fresh cycle).
func (r *Repository) ClearLoginAttempts(ctx context.Context, email string) error {
	return r.queries.ClearLoginAttempts(ctx, email)
}

// SetUserTotpSecret persists the AES-256-GCM encrypted TOTP secret and enables
// MFA for the user (FR-4/NFR-S4). The plaintext secret is never stored.
func (r *Repository) SetUserTotpSecret(ctx context.Context, userID, encryptedSecret string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.SetUserTotpSecret(ctx, SetUserTotpSecretParams{
		ID:                  uid,
		TotpSecretEncrypted: pgtype.Text{String: encryptedSecret, Valid: true},
	})
}

// ClearUserTotpSecret disables MFA and clears the stored encrypted secret
// (FR-4).
func (r *Repository) ClearUserTotpSecret(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.ClearUserTotpSecret(ctx, uid)
}

// SetUserPendingTotpSecret persists a short-lived pending TOTP enrollment: the
// freshly generated secret is stored ENCRYPTED at rest (NFR-S4) with an expiry
// (FR-4 / review finding 1.6-1).
func (r *Repository) SetUserPendingTotpSecret(ctx context.Context, userID, encryptedSecret string, expiresAt time.Time) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.SetUserPendingTotpSecret(ctx, SetUserPendingTotpSecretParams{
		ID:                       uid,
		PendingTotpSecretEncrypted: pgtype.Text{String: encryptedSecret, Valid: true},
		PendingTotpExpiresAt:     pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
}

// ClearUserPendingTotpSecret clears a pending TOTP enrollment after the confirm
// step (success or failure).
func (r *Repository) ClearUserPendingTotpSecret(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.ClearUserPendingTotpSecret(ctx, uid)
}

// DeleteSessionsByUser revokes every session of a user (NFR-S2).
func (r *Repository) DeleteSessionsByUser(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.DeleteSessionsByUser(ctx, uid)
}

// DeleteSessionsByUserExcept revokes all of a user's sessions except the one
// identified by the given token hash (NFR-S2 / review finding 1.6-2).
func (r *Repository) DeleteSessionsByUserExcept(ctx context.Context, userID, exceptTokenHash string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.DeleteSessionsByUserExcept(ctx, DeleteSessionsByUserExceptParams{
		UserID:     uid,
		TokenHash: exceptTokenHash,
	})
}

// UpdateUserPassword persists a new Argon2id password hash for the user and
// returns the updated user (FR-25/AD-13). Only the hash is stored; the
// plaintext password is never written.
func (r *Repository) UpdateUserPassword(ctx context.Context, userID, passwordHash string) (*core.User, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.UpdateUserPassword(ctx, UpdateUserPasswordParams{
		ID:           uid,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// UpdateUserProfile persists the user's editable base data (first/last/display
// name, Story 2.1) and the full custom-attribute set (Story 1.9) and returns
// the updated user. Email and state are never touched. The attributes map is
// marshalled to JSONB for storage (a nil map becomes `{}`). Absent-vs-clear is
// decided by the core (review finding): the core passes the current value
// through for "leave unchanged", so the repository always receives a concrete
// map to write. An unknown user ID maps to core.ErrUserNotFound.
func (r *Repository) UpdateUserProfile(ctx context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*core.User, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	attrsJSON := []byte("{}")
	if attributes != nil {
		raw, err := json.Marshal(attributes)
		if err != nil {
			return nil, fmt.Errorf("user postgres: failed to marshal attributes: %w", err)
		}
		attrsJSON = raw
	}
	row, err := r.queries.UpdateUserProfile(ctx, UpdateUserProfileParams{
		ID:          uid,
		FirstName:   firstName,
		LastName:    lastName,
		DisplayName: displayName,
		Attributes:  attrsJSON,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrUserNotFound
		}
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// StagePendingEmail stores a staged email change (Story 2.1) in a single
// conditional UPDATE: the address is persisted only while NO OTHER account
// holds it as its current email or as an already-staged pending_email
// (case-insensitive). This is the DB-level TOCTOU guard that keeps a racing
// registration from leaving a mixed collision. Both "no row updated" (the
// address is taken) and a duplicate-key violation (23505, the pending_email
// UNIQUE backstop) map to core.ErrEmailInUse.
func (r *Repository) StagePendingEmail(ctx context.Context, userID, pendingEmail string) (*core.User, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.StagePendingEmail(ctx, StagePendingEmailParams{
		ID:           uid,
		PendingEmail: pgtype.Text{String: pendingEmail, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrEmailInUse
		}
		if isPgUniqueViolation(err) {
			return nil, core.ErrEmailInUse
		}
		return nil, err
	}
	return userFromRow(row.ID, row.Email, row.DisplayName, row.FirstName, row.LastName,
		row.PasswordHash, row.State, row.IsMfaEnabled, row.MustChangePassword, row.TotpSecretEncrypted,
		row.PendingTotpSecretEncrypted, row.PendingTotpExpiresAt, row.Attributes, row.CreatedAt, row.UpdatedAt, row.PendingEmail)
}

// ClearPendingEmail clears a staged email change (pending_email -> NULL) for
// the user. Used by the Epic 2 admin workflow when the staged address becomes
// the real email or the change is cancelled. Unknown user IDs are a no-op.
func (r *Repository) ClearPendingEmail(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.ClearPendingEmail(ctx, uid)
}

// RefreshSessionUser is a no-op: the postgres session store never caches a
// user snapshot — GetSessionByTokenHash re-derives it live from users via the
// JOIN on every Validate — so profile edits are always reflected immediately
// (review finding: stale session snapshot).
func (r *Repository) RefreshSessionUser(_ context.Context, _ *core.User) error {
	return nil
}

// InsertAuditEvent appends a row to the User-owned append-only audit trail
// (NFR-O1/NFR-O2, spine table 11). It records only actor_user_id, operation
// and created_at — never password values or other sensitive payloads.
func (r *Repository) InsertAuditEvent(ctx context.Context, userID, operation string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.InsertAuditEvent(ctx, InsertAuditEventParams{
		ActorUserID: uid,
		Operation:   operation,
	})
}

// InsertAuditEventAnonymous appends an audit row WITHOUT an actor (actor_user_id
// stays NULL). Used for anti-enumeration paths with no authenticated user, e.g.
// a forgot-password request for an unknown email (review findings 1.8-3/1.8-10):
// enumeration attempts leave a trail (NFR-O1) and the path performs
// comparable-cost work.
func (r *Repository) InsertAuditEventAnonymous(ctx context.Context, operation string) error {
	return r.queries.InsertAuditEventAnonymous(ctx, operation)
}

// CreatePasswordResetToken stores the SHA-256 hash of a fresh single-use reset
// token, atomically invalidating every earlier token of the user (only the
// latest request stays valid, FR-26/AD-13). The raw token is never persisted.
func (r *Repository) CreatePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.CreatePasswordResetToken(ctx, CreatePasswordResetTokenParams{
		UserID:    uid,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
}

// GetPasswordResetTokenByHash resolves a reset token by its stored hash with
// its owning user (JOIN on users), so the completion step can verify the
// account is still active. An unknown hash maps to core.ErrResetTokenInvalid.
func (r *Repository) GetPasswordResetTokenByHash(ctx context.Context, tokenHash string) (*core.PasswordResetToken, error) {
	row, err := r.queries.GetPasswordResetTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrResetTokenInvalid
		}
		return nil, err
	}
	return &core.PasswordResetToken{
		ID:        uuidToString(row.ID.Bytes),
		UserID:    uuidToString(row.UserID.Bytes),
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
		User: &core.User{
			ID:                 uuidToString(row.UserID.Bytes),
			Email:              row.Email,
			DisplayName:        row.DisplayName,
			FirstName:          row.FirstName,
			LastName:           row.LastName,
			State:              core.UserState(row.State),
			IsMFAEnabled:       row.IsMfaEnabled,
			MustChangePassword: row.MustChangePassword,
			PasswordHash:       row.PasswordHash,
		},
	}, nil
}

// ConsumePasswordResetToken atomically invalidates a reset token and returns it
// with its owning user (review finding 1.8-5): the DELETE and the read happen in
// one statement, so two concurrent completions with the same token cannot both
// succeed — the loser sees no row and this maps to core.ErrResetTokenInvalid.
func (r *Repository) ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*core.PasswordResetToken, error) {
	row, err := r.queries.ConsumePasswordResetToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, core.ErrResetTokenInvalid
		}
		return nil, err
	}
	return &core.PasswordResetToken{
		ID:        uuidToString(row.ID.Bytes),
		UserID:    uuidToString(row.UserID.Bytes),
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
		User: &core.User{
			ID:                 uuidToString(row.UserID.Bytes),
			Email:              row.Email,
			DisplayName:        row.DisplayName,
			FirstName:          row.FirstName,
			LastName:           row.LastName,
			State:              core.UserState(row.State),
			IsMFAEnabled:       row.IsMfaEnabled,
			MustChangePassword: row.MustChangePassword,
			PasswordHash:       row.PasswordHash,
		},
	}, nil
}

// DeletePasswordResetToken invalidates a single reset token after use
// (single-use, FR-26). Unknown hashes are a no-op.
func (r *Repository) DeletePasswordResetToken(ctx context.Context, tokenHash string) error {
	return r.queries.DeletePasswordResetToken(ctx, tokenHash)
}

// DeleteExpiredPasswordResetTokens lazily purges a user's expired reset tokens
// (review finding 1.8-7): run on each reset request so expired rows do not
// accumulate indefinitely. Unknown users are a no-op.
func (r *Repository) DeleteExpiredPasswordResetTokens(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.DeleteExpiredPasswordResetTokens(ctx, uid)
}

// SetUserMustChangePassword flags an active account so the next login forces a
// mandatory password change (FR-26, SMTP-not-configured fallback / Epic 2
// one-time password).
func (r *Repository) SetUserMustChangePassword(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.SetUserMustChangePassword(ctx, uid)
}

// ClearUserMustChangePassword clears the mandatory-change flag once the user
// completes a password change via the forced flow or a reset link (FR-26).
func (r *Repository) ClearUserMustChangePassword(ctx context.Context, userID string) error {
	uid, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	return r.queries.ClearUserMustChangePassword(ctx, uid)
}

// IsUserInPermissionGroup reports whether the user is a member of the named
// permission group (AD-12), e.g. the 'admin' group. It drives the
// server-authoritative IsAdmin flag for ADMIN module visibility (Story 1.8).
func (r *Repository) IsUserInPermissionGroup(ctx context.Context, userID, groupName string) (bool, error) {
	uid, err := uuidFromString(userID)
	if err != nil {
		return false, err
	}
	return r.queries.IsUserInPermissionGroup(ctx, IsUserInPermissionGroupParams{
		UserID: uid,
		Name:   groupName,
	})
}

func uuidToString(b [16]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3],
		b[4], b[5],
		b[6], b[7],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15],
	)
}

// userFromRow maps an sqlc user row to the core.User domain entity. A stored
// `attributes` value that cannot be parsed into a JSON object (e.g. an array or
// scalar written out-of-band — the jsonb column rejects syntactically invalid
// JSON, so a "malformed" value is a valid non-object shape) surfaces as a clear
// error instead of silently dropping it (Story 1.9 boundary: reads never crash
// and never silently lose data).
func userFromRow(id pgtype.UUID, email, displayName, firstName, lastName, passwordHash, state string, isMfa, mustChange bool, totpSecret pgtype.Text, pendingSecret pgtype.Text, pendingExpiry pgtype.Timestamptz, attributes []byte, createdAt, updatedAt pgtype.Timestamptz, pendingEmail pgtype.Text) (*core.User, error) {
	var attrs map[string]any
	if len(attributes) > 0 {
		if err := json.Unmarshal(attributes, &attrs); err != nil {
			return nil, fmt.Errorf("user postgres: invalid stored attributes jsonb: %w", err)
		}
	}
	secret := ""
	if totpSecret.Valid {
		secret = totpSecret.String
	}
	pending := ""
	if pendingSecret.Valid {
		pending = pendingSecret.String
	}
	var expiry time.Time
	if pendingExpiry.Valid {
		expiry = pendingExpiry.Time
	}
	staged := ""
	if pendingEmail.Valid {
		staged = pendingEmail.String
	}
	return &core.User{
		ID:                         uuidToString(id.Bytes),
		Email:                      email,
		PendingEmail:               staged,
		DisplayName:                displayName,
		FirstName:                  firstName,
		LastName:                   lastName,
		PasswordHash:               passwordHash,
		State:                      core.UserState(state),
		IsMFAEnabled:               isMfa,
		MustChangePassword:         mustChange,
		TotpSecretEncrypted:        secret,
		PendingTotpSecretEncrypted: pending,
		PendingTotpExpiresAt:       expiry,
		Attributes:                 attrs,
		CreatedAt:                  createdAt.Time,
		UpdatedAt:                  updatedAt.Time,
	}, nil
}

// uuidFromString parses a canonical UUID string into a pgtype.UUID.
func uuidFromString(s string) (pgtype.UUID, error) {
	var b [16]byte
	_, err := fmt.Sscanf(s, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		&b[0], &b[1], &b[2], &b[3],
		&b[4], &b[5],
		&b[6], &b[7],
		&b[8], &b[9],
		&b[10], &b[11], &b[12], &b[13], &b[14], &b[15],
	)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}

// isPgUniqueViolation reports whether err is a Postgres unique-violation
// (SQLSTATE 23505). Matching the structured pgconn.PgError code is reliable —
// unlike matching the human-readable error string (review finding: pgx 23505
// mapping).
func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
