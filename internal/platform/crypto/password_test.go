package crypto

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "SecurePassword123!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash does not start with expected prefix, got: %s", hash)
	}

	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !match {
		t.Errorf("expected password to match hash")
	}

	wrongMatch, err := VerifyPassword("WrongPassword123!", hash)
	if err != nil {
		t.Fatalf("VerifyPassword with wrong password failed: %v", err)
	}
	if wrongMatch {
		t.Errorf("expected wrong password to not match hash")
	}
}

func TestSaltsAreRandom(t *testing.T) {
	password := "SamePassword123!"

	hash1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("first hash failed: %v", err)
	}

	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("second hash failed: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("expected different hashes due to random salting, but got identical: %s", hash1)
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"plain text", "notahash"},
		{"invalid prefix", "$bcrypt$v=19$m=65536,t=3,p=4$abc$def"},
		{"missing parts", "$argon2id$v=19$m=65536,t=3,p=4$abc"},
		{"invalid base64 salt", "$argon2id$v=19$m=65536,t=3,p=4$!!!$abc"},
		{"invalid base64 hash", "$argon2id$v=19$m=65536,t=3,p=4$YWJj$!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := VerifyPassword("password", tt.hash)
			if err == nil {
				t.Errorf("expected error for malformed hash %q, got match=%v", tt.hash, match)
			}
		})
	}
}
