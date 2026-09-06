// Package config loads runtime configuration from the environment using the
// GEAR_-prefixed variables:
//
//	GEAR_DATABASE_URL       PostgreSQL DSN for the pgx pool
//	GEAR_HTTP_ADDR          listen address for the HTTP server
//	GEAR_LOG_LEVEL          debug | info | warn | error
//	GEAR_SESSION_IDLE       server-side session idle lifetime (NFR-S2)
//	GEAR_ENCRYPTION_KEY     32-byte key (hex or base64) for at-rest encryption
//	                        of TOTP secrets (NFR-S4); generate with
//	                        `openssl rand -hex 32` or `openssl rand -base64 32`
//
// Local-development defaults match the root compose.yaml and justfile so a
// fresh checkout needs no configuration. Secrets (admin bootstrap credentials,
// DB passwords in production, the encryption key) are supplied at runtime via
// the environment and never committed (NFR-S4 / AD-13).
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/saskia-peters/gear/internal/platform/logger"
)

// Defaults for local development; they mirror compose.yaml and the justfile.
const (
	DefaultDatabaseURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	DefaultHTTPAddr    = ":8080"
	DefaultLogLevel    = "info"
	DefaultSessionIdle = "8h"
	// DefaultAppOrigin is the public origin of the SPA used to build clickable
	// password-reset links (review finding 1.8-6). Matches the Vite dev server.
	DefaultAppOrigin = "http://localhost:5173"
)

// ErrEncryptionKeyInvalid is returned when GEAR_ENCRYPTION_KEY is missing,
// not 32 bytes after decoding, or neither valid hex nor base64. MFA operations
// surface this clear error instead of panicking (NFR-S4).
var ErrEncryptionKeyInvalid = errors.New("GEAR_ENCRYPTION_KEY must be a 32-byte key encoded as hex or base64")

// Config holds the runtime configuration of the server.
type Config struct {
	DatabaseURL string
	HTTPAddr    string
	LogLevel    slog.Level
	SessionIdle time.Duration
	// AppOrigin is the public origin of the SPA used to build password-reset
	// links (GEAR_APP_ORIGIN, review finding 1.8-6).
	AppOrigin string
	// EncryptionKey is the raw GEAR_ENCRYPTION_KEY value (hex or base64) and
	// EncryptionKeyErr the parse result. When set, EncryptionKeyErr is nil.
	EncryptionKey    string
	EncryptionKeyErr error
}

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) Config {
	encKey := getenv("GEAR_ENCRYPTION_KEY")
	return Config{
		DatabaseURL:      envOr(getenv, "GEAR_DATABASE_URL", DefaultDatabaseURL),
		HTTPAddr:         envOr(getenv, "GEAR_HTTP_ADDR", DefaultHTTPAddr),
		LogLevel:         logger.ParseLevel(envOr(getenv, "GEAR_LOG_LEVEL", DefaultLogLevel)),
		SessionIdle:      durationOr(getenv, "GEAR_SESSION_IDLE", DefaultSessionIdle),
		AppOrigin:        envOr(getenv, "GEAR_APP_ORIGIN", DefaultAppOrigin),
		EncryptionKey:    encKey,
		EncryptionKeyErr: parseEncryptionKey(encKey),
	}
}

// EncryptionKeyBytes returns the 32 raw key bytes parsed from GEAR_ENCRYPTION_KEY.
// It returns ErrEncryptionKeyInvalid when the key is missing or malformed. The
// key is hex or base64 encoded at rest; the decoded 32 bytes feed AES-256-GCM.
func (c Config) EncryptionKeyBytes() ([]byte, error) {
	if c.EncryptionKeyErr != nil {
		return nil, c.EncryptionKeyErr
	}
	key, err := decodeEncryptionKey(c.EncryptionKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes", ErrEncryptionKeyInvalid, len(key))
	}
	return key, nil
}

// parseEncryptionKey validates the key value once at load time. An empty value
// (no env var) is treated as missing/invalid — MFA would fail clearly.
func parseEncryptionKey(value string) error {
	if value == "" {
		return ErrEncryptionKeyInvalid
	}
	key, err := decodeEncryptionKey(value)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEncryptionKeyInvalid, err)
	}
	if len(key) != 32 {
		return fmt.Errorf("%w: got %d bytes", ErrEncryptionKeyInvalid, len(key))
	}
	return nil
}

func decodeEncryptionKey(value string) ([]byte, error) {
	if key, err := hex.DecodeString(value); err == nil {
		return key, nil
	}
	if key, err := base64.StdEncoding.DecodeString(value); err == nil {
		return key, nil
	}
	if key, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return key, nil
	}
	return nil, errors.New("not valid hex or base64")
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationOr(getenv func(string) string, key, fallback string) time.Duration {
	v := envOr(getenv, key, fallback)
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	slog.Warn("invalid duration configured; falling back to default",
		"key", key, "value", v, "fallback", fallback)
	d, _ := time.ParseDuration(fallback)
	return d
}
