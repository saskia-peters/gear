package pguuid

import (
	"errors"
	"testing"
)

func TestParseOptional(t *testing.T) {
	// EMPTY_ID: an empty (or whitespace-only) id is a legal "no FK" value → the
	// zero pgtype.UUID{} (SQL NULL), never an error. This is the ONE
	// empty-handling contract for the tools + admin repositories (Epic 4 retro
	// item D1).
	for _, empty := range []string{"", "   ", "\t\n"} {
		uid, err := ParseOptional(empty)
		if err != nil {
			t.Fatalf("ParseOptional(%q) err = %v, want nil", empty, err)
		}
		if uid.Valid {
			t.Errorf("ParseOptional(%q) Valid = true, want the zero (NULL) UUID", empty)
		}
	}

	// VALID_ID: a canonical UUID round-trips.
	canonical := "11111111-2222-3333-4444-555555555555"
	uid, err := ParseOptional(canonical)
	if err != nil {
		t.Fatalf("ParseOptional(%q) err = %v, want nil", canonical, err)
	}
	if !uid.Valid {
		t.Error("ParseOptional(canonical) Valid = false, want true")
	}
	if got := uid.String(); got != canonical {
		t.Errorf("ParseOptional(canonical) = %q, want %q", got, canonical)
	}

	// MALFORMED: a non-empty non-UUID id → the shared sentinel (callers map it
	// to their domain not-found sentinel).
	for _, bad := range []string{"abc", "123", "not-a-uuid", "11111111-2222-3333-4444-55555555555G"} {
		_, err := ParseOptional(bad)
		if !errors.Is(err, ErrInvalidUUID) {
			t.Errorf("ParseOptional(%q) err = %v, want ErrInvalidUUID", bad, err)
		}
	}
}