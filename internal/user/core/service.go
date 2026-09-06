package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Repository defines the outbound persistence contract required by the User core.
type Repository interface {
	CreateRegisteredUser(ctx context.Context, email, displayName, firstName, lastName, passwordHash string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
	GetLoginAttempts(ctx context.Context, email string) (*LoginAttempts, error)
	IncrementLoginAttempts(ctx context.Context, email string) error
	ClearLoginAttempts(ctx context.Context, email string) error
	// TOTP MFA persistence (FR-4): the shared secret is stored encrypted at
	// rest (NFR-S4) and cleared on disable. The pending enrollment holds a
	// short-lived encrypted copy of the server-issued secret so the confirm
	// step validates the code against it (review finding 1.6-1).
	SetUserTotpSecret(ctx context.Context, userID, encryptedSecret string) error
	ClearUserTotpSecret(ctx context.Context, userID string) error
	SetUserPendingTotpSecret(ctx context.Context, userID, encryptedSecret string, expiresAt time.Time) error
	ClearUserPendingTotpSecret(ctx context.Context, userID string) error
	// Password change persistence (FR-25): UpdateUserPassword writes the new
	// Argon2id hash; InsertAuditEvent appends to the User-owned audit trail
	// (NFR-O1/NFR-O2, spine table 11).
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) (*User, error)
	InsertAuditEvent(ctx context.Context, userID, operation string) error
	// Profile base-data persistence (Story 2.1): UpdateUserProfile writes the
	// editable fields and the full custom-attribute set (Story 1.9);
	// StagePendingEmail stores a staged email awaiting admin approval (the user
	// stays active on the current email); ClearPendingEmail clears a staged
	// change (Epic 2 admin workflow).
	UpdateUserProfile(ctx context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*User, error)
	StagePendingEmail(ctx context.Context, userID, pendingEmail string) (*User, error)
	ClearPendingEmail(ctx context.Context, userID string) error
	// Password reset persistence (FR-26/AD-13): CreatePasswordResetToken stores
	// the SHA-256 hash of a single-use 30-min token (invalidating earlier ones);
	// ConsumePasswordResetToken atomically invalidates + returns a token (the
	// losing concurrent completion sees no row, review finding 1.8-5);
	// DeleteExpiredPasswordResetTokens lazily purges expired rows (review
	// finding 1.8-7); Set/ClearUserMustChangePassword flip the forced-change
	// flag (SMTP-not-configured fallback / Epic 2); InsertAuditEventAnonymous
	// writes an audit row without an actor (unknown-email reset requests).
	CreatePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*PasswordResetToken, error)
	DeleteExpiredPasswordResetTokens(ctx context.Context, userID string) error
	SetUserMustChangePassword(ctx context.Context, userID string) error
	ClearUserMustChangePassword(ctx context.Context, userID string) error
	InsertAuditEventAnonymous(ctx context.Context, operation string) error
	// IsUserInPermissionGroup reports whether the user is a member of the named
	// permission group (AD-12); the admin-group membership drives the
	// server-authoritative IsAdmin flag (Story 1.8).
	IsUserInPermissionGroup(ctx context.Context, userID, groupName string) (bool, error)
}

// SecretCipher encrypts/decrypts the TOTP shared secret at rest (NFR-S4). The
// concrete implementation is an outbound adapter (AES-256-GCM with the
// GEAR_ENCRYPTION_KEY) so the domain never touches key material directly.
type SecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// PasswordHasher defines the password hashing contract.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

// RegisterResult is the anti-enumeration confirmation returned on successful registration.
type RegisterResult struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Service provides User domain operations.
type Service struct {
	repo     Repository
	hasher   PasswordHasher
	sessions *SessionManager
	cipher   SecretCipher
	logger   *slog.Logger
	// resetSender is the reset-email delivery port (FR-26/AD-14). When nil or
	// reporting NOT-configured, RequestPasswordReset falls back to flagging the
	// account must_change_password (this story's default).
	resetSender ResetEmailSender
	// appOrigin is the public origin used to build reset links (GEAR_APP_ORIGIN,
	// review finding 1.8-6). Empty → relative links.
	appOrigin string
	// forgotThrottle is the per-email forgot-request rate gate (review finding
	// 1.8-2). Disabled when nil.
	forgotThrottle *forgotThrottle
}

// NewService constructs a User domain Service. logger is used for structured
// logging of best-effort operations inside the core (e.g. a failed audit write
// during a password change, NFR-O1); it may be nil, in which case the package
// falls back to slog.Default().
func NewService(repo Repository, hasher PasswordHasher, sessions *SessionManager, cipher SecretCipher, logger *slog.Logger) *Service {
	return &Service{
		repo:           repo,
		hasher:         hasher,
		sessions:       sessions,
		cipher:         cipher,
		logger:         logger,
		forgotThrottle: newForgotThrottle(ForgotPasswordMinInterval),
	}
}

// SetResetEmailSender configures the reset-email delivery port (FR-26). When
// unset, RequestPasswordReset falls back to the must_change_password flag (SMTP
// not configured — the active default until Story 3.1 wires a real sender).
func (s *Service) SetResetEmailSender(sender ResetEmailSender) {
	s.resetSender = sender
}

// SetAppOrigin configures the public origin used to build clickable reset links
// (GEAR_APP_ORIGIN, review finding 1.8-6). A trailing slash is trimmed.
func (s *Service) SetAppOrigin(origin string) {
	s.appOrigin = origin
}

// SetForgotThrottleInterval overrides the forgot-request rate-gate window
// (review finding 1.8-2). An interval <= 0 disables throttling (used by tests).
func (s *Service) SetForgotThrottleInterval(interval time.Duration) {
	s.forgotThrottle = newForgotThrottle(interval)
}

// UniformSuccessMessage is the German microcopy returned for anti-enumeration.
const UniformSuccessMessage = "Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung."

// Register executes volunteer self-registration with password policy enforcement and anti-enumeration protection.
func (s *Service) Register(ctx context.Context, input RegisterInput) (*RegisterResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	firstName := strings.TrimSpace(input.FirstName)
	lastName := strings.TrimSpace(input.LastName)
	displayName := fmt.Sprintf("%s %s", firstName, lastName)

	// Check if user already exists (anti-enumeration check)
	existing, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to check existing user: %w", err)
	}
	if existing != nil {
		// User already exists. Hash password to ensure uniform response timing
		// without creating a duplicate user or leaking account existence (UX-DR7).
		_, _ = s.hasher.Hash(input.Password)
		return &RegisterResult{
			Message: UniformSuccessMessage,
			Status:  string(StatePendingApproval),
		}, nil
	}

	// User does not exist, compute Argon2id hash and create record
	hash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to hash password: %w", err)
	}

	_, err = s.repo.CreateRegisteredUser(ctx, email, displayName, firstName, lastName, hash)
	if err != nil {
		// In case of duplicate key race condition, return uniform anti-enumeration response
		if errors.Is(err, ErrUserAlreadyExists) || isDuplicateKeyErr(err) {
			return &RegisterResult{
				Message: UniformSuccessMessage,
				Status:  string(StatePendingApproval),
			}, nil
		}
		return nil, fmt.Errorf("user core: failed to create user: %w", err)
	}

	return &RegisterResult{
		Message: UniformSuccessMessage,
		Status:  string(StatePendingApproval),
	}, nil
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") || strings.Contains(msg, "23505")
}
