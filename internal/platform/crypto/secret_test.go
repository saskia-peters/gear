package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	// A fixed 32-byte key for deterministic tests.
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestSecretEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := "JBSWY3DPEHPK3PXP"

	enc, err := EncryptSecret(key, plaintext)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}
	if enc == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}
	if strings.Contains(enc, plaintext) {
		t.Fatal("ciphertext must not embed the plaintext secret (NFR-S4)")
	}

	dec, err := DecryptSecret(key, enc)
	if err != nil {
		t.Fatalf("DecryptSecret failed: %v", err)
	}
	if dec != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", dec, plaintext)
	}
}

func TestSecretEncryptProducesUniqueCiphertext(t *testing.T) {
	key := testKey(t)
	a, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	// A random nonce must yield distinct ciphertexts for the same plaintext.
	if a == b {
		t.Error("same plaintext should encrypt to distinct ciphertexts (random nonce)")
	}
}

func TestSecretDecryptTamperedFails(t *testing.T) {
	key := testKey(t)
	enc, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	// Flip one base64 character.
	flipped := enc[:len(enc)-1]
	flipped += string([]byte{enc[len(enc)-1] ^ 0x01})

	if _, err := DecryptSecret(key, flipped); err == nil {
		t.Fatal("expected decryption of tampered ciphertext to fail")
	}
}

func TestSecretDecryptWrongKeyFails(t *testing.T) {
	key := testKey(t)
	other := make([]byte, 32)
	for i := range other {
		other[i] = 0x42
	}
	enc, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSecret(other, enc); err == nil {
		t.Fatal("expected decryption with a different key to fail")
	}
}

func TestSecretCipherAdapterRejectsShortKey(t *testing.T) {
	c := NewSecretCipher([]byte("too-short"))
	if _, err := c.Encrypt("JBSWY3DPEHPK3PXP"); err == nil {
		t.Fatal("expected Encrypt to fail with a short key")
	}
	if _, err := c.Decrypt("aaaa"); err == nil {
		t.Fatal("expected Decrypt to fail with a short key")
	}
}

func TestSecretDecryptEmptyCiphertextMapsToInvalidCiphertext(t *testing.T) {
	// Review finding 1.6-13: an empty/truncated ciphertext (shorter than the
	// GCM nonce) must map to ErrInvalidCiphertext, never a panic.
	key := testKey(t)

	if _, err := DecryptSecret(key, ""); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("empty ciphertext err = %v, want ErrInvalidCiphertext", err)
	}
	if _, err := DecryptSecret(key, "AAAA"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("truncated ciphertext err = %v, want ErrInvalidCiphertext", err)
	}
	// A nonce-only ciphertext (no payload) also fails authentication.
	enc, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	nonceOnly := base64.StdEncoding.EncodeToString(decoded[:12])
	if _, err := DecryptSecret(key, nonceOnly); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("nonce-only ciphertext err = %v, want ErrInvalidCiphertext", err)
	}
}

func TestSecretDecryptWrongKeyMapsToInvalidCiphertext(t *testing.T) {
	// Review finding 1.6-13: decryption with a different key (rotated or
	// misconfigured GEAR_ENCRYPTION_KEY) must map to ErrInvalidCiphertext so
	// callers can surface a clear MFA-unavailable error.
	key := testKey(t)
	other := make([]byte, 32)
	for i := range other {
		other[i] = 0x42
	}
	enc, err := EncryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSecret(other, enc); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("wrong-key err = %v, want ErrInvalidCiphertext", err)
	}
}

func TestSecretDecryptMalformedBase64MapsToInvalidCiphertext(t *testing.T) {
	key := testKey(t)
	if _, err := DecryptSecret(key, "!!!not-base64!!!"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("malformed base64 err = %v, want ErrInvalidCiphertext", err)
	}
}
