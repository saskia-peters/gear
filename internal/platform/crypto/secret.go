package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidCiphertext is returned when an encrypted secret cannot be
	// decrypted (malformed encoding, wrong key, or tampered data).
	ErrInvalidCiphertext = errors.New("crypto: invalid ciphertext")
)

// SecretCipher is an adapter implementing the User core's TOTP secret
// encryption port (NFR-S4). It seals/unseals the shared secret with AES-256-GCM
// using the app-level 32-byte key from GEAR_ENCRYPTION_KEY.
type SecretCipher struct {
	key []byte
}

// NewSecretCipher returns a SecretCipher bound to the given 32-byte key. If the
// key is missing or not 32 bytes, Encrypt/Decrypt return a clear error — never
// a panic (NFR-S4).
func NewSecretCipher(key []byte) *SecretCipher {
	return &SecretCipher{key: key}
}

// Encrypt seals plaintext with AES-256-GCM (NFR-S4).
func (c *SecretCipher) Encrypt(plaintext string) (string, error) {
	if len(c.key) != 32 {
		return "", fmt.Errorf("crypto: AES-256-GCM requires a 32-byte key, got %d bytes", len(c.key))
	}
	return EncryptSecret(c.key, plaintext)
}

// Decrypt unseals ciphertext produced by Encrypt (NFR-S4).
func (c *SecretCipher) Decrypt(encoded string) (string, error) {
	if len(c.key) != 32 {
		return "", fmt.Errorf("crypto: AES-256-GCM requires a 32-byte key, got %d bytes", len(c.key))
	}
	return DecryptSecret(c.key, encoded)
}

// EncryptSecret seals plaintext with AES-256-GCM using the given 32-byte key
// (NFR-S4). The returned string is base64( nonce || ciphertext ) so it can be
// stored in a text column and round-trips losslessly.
func EncryptSecret(key []byte, plaintext string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: AES-256-GCM requires a 32-byte key, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret reverses EncryptSecret: it decodes base64, splits the nonce and
// authenticates/decrypts the payload with AES-256-GCM (NFR-S4). Any tampering,
// wrong key or malformed input returns ErrInvalidCiphertext.
func DecryptSecret(key []byte, encoded string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: AES-256-GCM requires a 32-byte key, got %d", len(key))
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64: %v", ErrInvalidCiphertext, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: failed to create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("%w: data too short", ErrInvalidCiphertext)
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCiphertext, err)
	}
	return string(plain), nil
}
