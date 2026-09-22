// Package crypto provides cryptographic utilities including Argon2id password
// hashing and constant-time verification according to AD-13.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	// ErrInvalidHash is returned when an encoded hash is malformed or uses an unsupported format.
	ErrInvalidHash = errors.New("crypto: invalid encoded hash format")

	// ErrIncompatibleVersion is returned when the hash version is not supported.
	ErrIncompatibleVersion = errors.New("crypto: incompatible argon2 version")
)

// DefaultArgon2idParams represents the standard Argon2id configuration (AD-13).
// Memory: 64 MB (65536 KiB), Iterations: 3, Parallelism: 4 threads, Salt: 16 bytes, KeyLength: 32 bytes.
type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultParams = Argon2idParams{
	Memory:      64 * 1024, // 64 MB (65536 KiB)
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Hasher provides an instance-based interface for password hashing.
type Hasher struct{}

// NewHasher returns a new Argon2id Hasher.
func NewHasher() *Hasher {
	return &Hasher{}
}

func (h *Hasher) Hash(password string) (string, error) {
	return HashPassword(password)
}

func (h *Hasher) Verify(password, encodedHash string) (bool, error) {
	return VerifyPassword(password, encodedHash)
}

// HashPassword hashes a plaintext password using Argon2id with default parameters.
func HashPassword(password string) (string, error) {
	return HashPasswordCustom(password, DefaultParams)
}

// HashPasswordCustom hashes a plaintext password using Argon2id with custom parameters.
func HashPasswordCustom(password string, params Argon2idParams) (string, error) {
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("crypto: failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.Memory, params.Iterations, params.Parallelism, b64Salt, b64Hash)

	return encoded, nil
}

// VerifyPassword checks if a plaintext password matches an Argon2id encoded hash in constant time.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	// Format: "" , "argon2id", "v=19", "m=65536,t=3,p=4", "<salt>", "<hash>"
	if len(parts) != 6 {
		return false, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, ErrInvalidHash
	}

	salt, err := decodeBase64(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}

	expectedHash, err := decodeBase64(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	if len(salt) == 0 || len(expectedHash) == 0 {
		return false, ErrInvalidHash
	}

	computedHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(expectedHash, computedHash) == 1 {
		return true, nil
	}

	return false, nil
}

func decodeBase64(s string) ([]byte, error) {
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
