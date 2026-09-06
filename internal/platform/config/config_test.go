package config

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg := Load(mapEnv(nil))

	if cfg.DatabaseURL != DefaultDatabaseURL {
		t.Errorf("DatabaseURL = %q, want default %q", cfg.DatabaseURL, DefaultDatabaseURL)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want default %q", cfg.HTTPAddr, DefaultHTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.SessionIdle != 8*time.Hour {
		t.Errorf("SessionIdle = %v, want 8h default", cfg.SessionIdle)
	}
	if cfg.AppOrigin != DefaultAppOrigin {
		t.Errorf("AppOrigin = %q, want default %q", cfg.AppOrigin, DefaultAppOrigin)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := map[string]string{
		"GEAR_DATABASE_URL": "postgres://u:p@db.example:5432/gear?sslmode=require",
		"GEAR_HTTP_ADDR":    "127.0.0.1:9999",
		"GEAR_LOG_LEVEL":    "debug",
		"GEAR_SESSION_IDLE": "30m",
		"GEAR_APP_ORIGIN":   "https://gear.example.com",
	}
	cfg := Load(mapEnv(env))

	if cfg.DatabaseURL != env["GEAR_DATABASE_URL"] {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, env["GEAR_DATABASE_URL"])
	}
	if cfg.HTTPAddr != env["GEAR_HTTP_ADDR"] {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, env["GEAR_HTTP_ADDR"])
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
	if cfg.SessionIdle != 30*time.Minute {
		t.Errorf("SessionIdle = %v, want 30m", cfg.SessionIdle)
	}
	if cfg.AppOrigin != env["GEAR_APP_ORIGIN"] {
		t.Errorf("AppOrigin = %q, want %q", cfg.AppOrigin, env["GEAR_APP_ORIGIN"])
	}
}

func TestLoadSessionIdleInvalidFallsBack(t *testing.T) {
	cfg := Load(mapEnv(map[string]string{"GEAR_SESSION_IDLE": "not-a-duration"}))
	if cfg.SessionIdle != 8*time.Hour {
		t.Errorf("SessionIdle = %v, want 8h fallback", cfg.SessionIdle)
	}
}

func TestLoadSessionIdleInvalidWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(&buf), &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	_ = Load(mapEnv(map[string]string{"GEAR_SESSION_IDLE": "not-a-duration"}))

	if !strings.Contains(buf.String(), "invalid duration configured") {
		t.Errorf("expected a warning for invalid GEAR_SESSION_IDLE, got output %q", buf.String())
	}
}

func TestLoadEncryptionKeyHex(t *testing.T) {
	hexKey := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	cfg := Load(mapEnv(map[string]string{"GEAR_ENCRYPTION_KEY": hexKey}))
	if cfg.EncryptionKeyErr != nil {
		t.Fatalf("EncryptionKeyErr = %v, want nil", cfg.EncryptionKeyErr)
	}
	key, err := cfg.EncryptionKeyBytes()
	if err != nil {
		t.Fatalf("EncryptionKeyBytes failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
}

func TestLoadEncryptionKeyBase64(t *testing.T) {
	b64 := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	cfg := Load(mapEnv(map[string]string{"GEAR_ENCRYPTION_KEY": b64}))
	if cfg.EncryptionKeyErr != nil {
		t.Fatalf("EncryptionKeyErr = %v, want nil", cfg.EncryptionKeyErr)
	}
	key, err := cfg.EncryptionKeyBytes()
	if err != nil {
		t.Fatalf("EncryptionKeyBytes failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
}

func TestLoadEncryptionKeyMissingIsError(t *testing.T) {
	cfg := Load(mapEnv(nil))
	if cfg.EncryptionKeyErr == nil {
		t.Fatal("expected EncryptionKeyErr for a missing GEAR_ENCRYPTION_KEY")
	}
	if _, err := cfg.EncryptionKeyBytes(); err == nil {
		t.Fatal("expected EncryptionKeyBytes to fail for a missing key")
	}
}

func TestLoadEncryptionKeyInvalid(t *testing.T) {
	cfg := Load(mapEnv(map[string]string{"GEAR_ENCRYPTION_KEY": "not-a-valid-key"}))
	if cfg.EncryptionKeyErr == nil {
		t.Fatal("expected EncryptionKeyErr for an invalid key")
	}
}

func TestLoadEncryptionKeyWrongLength(t *testing.T) {
	// 16 bytes encoded as hex — must be rejected (AES-256 requires 32 bytes).
	shortHex := "00112233445566778899aabbccddeeff"
	cfg := Load(mapEnv(map[string]string{"GEAR_ENCRYPTION_KEY": shortHex}))
	if cfg.EncryptionKeyErr == nil {
		t.Fatal("expected EncryptionKeyErr for a 16-byte key")
	}
}
