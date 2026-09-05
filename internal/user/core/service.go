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
}

// NewService constructs a User domain Service. logger is used for structured
// logging of best-effort operations inside the core (e.g. a failed audit write
// during a password change, NFR-O1); it may be nil, in which case the package
// falls back to slog.Default().
func NewService(repo Repository, hasher PasswordHasher, sessions *SessionManager, cipher SecretCipher, logger *slog.Logger) *Service {
	return &Service{
		repo:     repo,
		hasher:   hasher,
		sessions: sessions,
		cipher:   cipher,
		logger:   logger,
	}
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
