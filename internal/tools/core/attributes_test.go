package core

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// Shared attributes validation (Story 4.4, FR-10/AD-3): the JSONB extension
// surface rules mirror the User-module profile precedent — keys trimmed/
// non-empty/≤ MaxAttributeKeyRunes, values JSON-serializable, whole map ≤
// MaxAttributesSize, nil valid ("absent"), empty map valid ("clear").

func TestValidateAttributesNil(t *testing.T) {
	// A nil (absent) field is valid and reports the leave-unchanged signal.
	got, err := validateAttributes(nil)
	if err != nil {
		t.Fatalf("validateAttributes(nil) err = %v", err)
	}
	if got != nil {
		t.Errorf("validateAttributes(nil) = %v, want nil (leave unchanged)", got)
	}
}

func TestValidateAttributesEmptyClears(t *testing.T) {
	// An EXPLICIT empty map is valid and reports the clear signal.
	got, err := validateAttributes(map[string]any{})
	if err != nil {
		t.Fatalf("validateAttributes({}) err = %v", err)
	}
	if got == nil {
		t.Fatal("validateAttributes({}) = nil, want a non-nil empty map (explicit clear)")
	}
	if len(got) != 0 {
		t.Errorf("validateAttributes({}) = %v, want empty", got)
	}
}

func TestValidateAttributesNormalizesKeys(t *testing.T) {
	// Keys are trimmed on the way in (a `" note "` key becomes `"note"`).
	got, err := validateAttributes(map[string]any{" note ": "Interne Notiz", "standort": "Werkstatt"})
	if err != nil {
		t.Fatalf("validateAttributes err = %v", err)
	}
	if got["note"] != "Interne Notiz" {
		t.Errorf("normalized = %+v, want trimmed key note=Interne Notiz", got)
	}
	if _, padded := got[" note "]; padded {
		t.Errorf("normalized must not keep the padded key, got %+v", got)
	}
	if got["standort"] != "Werkstatt" {
		t.Errorf("normalized = %+v, want standort preserved", got)
	}
}

func TestValidateAttributesRoundTrip(t *testing.T) {
	// ROUND_TRIP: an object with mixed JSON-serializable values passes through
	// unchanged (validated, not rewritten).
	attrs := map[string]any{
		"standort": "Werkstatt",
		"leistung": float64(1200),
		"tags":     []string{"bohren", "schleifen"},
		"aktiv":    true,
		"konfig":   map[string]any{"modus": "auto"},
	}
	got, err := validateAttributes(attrs)
	if err != nil {
		t.Fatalf("validateAttributes err = %v", err)
	}
	if len(got) != len(attrs) {
		t.Fatalf("normalized = %+v, want %d entries", got, len(attrs))
	}
	if got["leistung"] != float64(1200) {
		t.Errorf("leistung = %v, want 1200", got["leistung"])
	}
	tags, ok := got["tags"].([]string)
	if !ok || len(tags) != 2 || tags[0] != "bohren" {
		t.Errorf("tags = %+v, want the []string round-tripped", got["tags"])
	}
}

func TestValidateAttributesRejectsBadKeys(t *testing.T) {
	// VALID_INVALID_KEY: an empty/whitespace-only or over-long key is rejected
	// with a *AttributeError (unwrapping ErrInvalidAttributes).
	cases := []struct {
		name       string
		attrs      map[string]any
		wantKey    string
		wantReason string
	}{
		{"empty key", map[string]any{"": "wert"}, "", "empty key"},
		{"whitespace-only key", map[string]any{"   ": "wert"}, "   ", "empty key"},
		{"over-long key", map[string]any{strings.Repeat("k", MaxAttributeKeyRunes+1): "wert"}, strings.Repeat("k", MaxAttributeKeyRunes+1), "key too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateAttributes(tc.attrs)
			if !errors.Is(err, ErrInvalidAttributes) {
				t.Fatalf("err = %v, want ErrInvalidAttributes", err)
			}
			var attrErr *AttributeError
			if !errors.As(err, &attrErr) {
				t.Fatalf("err = %v, want a *AttributeError with details", err)
			}
			if attrErr.Key != tc.wantKey || attrErr.Reason != tc.wantReason {
				t.Errorf("details = (%q, %q), want (%q, %q)", attrErr.Key, attrErr.Reason, tc.wantKey, tc.wantReason)
			}
		})
	}
}

func TestValidateAttributesRejectsTrimmedDuplicateKeys(t *testing.T) {
	// VALID_DUPLICATE_TRIMMED: two keys that TRIM to the same string
	// (`"a"` and `" a "`) silently collapse last-wins in a map — the server must
	// reject them (the client editor does), never normalize silently. The Go map
	// iteration order is random, so the duplicate is rejected regardless of
	// which key is processed first.
	_, err := validateAttributes(map[string]any{"a": 1, " a ": 2})
	if !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("err = %v, want ErrInvalidAttributes", err)
	}
	var attrErr *AttributeError
	if !errors.As(err, &attrErr) {
		t.Fatalf("err = %v, want a *AttributeError with details", err)
	}
	if attrErr.Reason != "duplicate key" {
		t.Errorf("details = (%q, %q), want reason=duplicate key", attrErr.Key, attrErr.Reason)
	}
}

func TestValidateAttributesAllowsCaseVariantKeys(t *testing.T) {
	// Two keys that differ only in case are DISTINCT (the server never
	// case-folds keys) — only EXACT trimmed duplicates are rejected.
	got, err := validateAttributes(map[string]any{"a": 1, "A": 2})
	if err != nil {
		t.Fatalf("validateAttributes(a, A) err = %v, want accepted (distinct keys)", err)
	}
	if len(got) != 2 || got["a"] != 1 || got["A"] != 2 {
		t.Errorf("normalized = %+v, want both case-variant keys kept", got)
	}
}

func TestValidateAttributesRejectsBadValue(t *testing.T) {
	// VALID_BAD_VALUE: a value that cannot be JSON-serialized (a NaN float) is
	// rejected with the offending key identified.
	_, err := validateAttributes(map[string]any{"wert": math.NaN()})
	if !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("err = %v, want ErrInvalidAttributes", err)
	}
	var attrErr *AttributeError
	if !errors.As(err, &attrErr) {
		t.Fatalf("err = %v, want a *AttributeError with details", err)
	}
	if attrErr.Key != "wert" || attrErr.Reason != "value not JSON-serializable" {
		t.Errorf("details = (%q, %q), want (wert, value not JSON-serializable)", attrErr.Key, attrErr.Reason)
	}
}

func TestValidateAttributesRejectsTooLarge(t *testing.T) {
	// VALID_TOO_LARGE: a serialized map over MaxAttributesSize is rejected with
	// a whole-map *AttributeError.
	big := strings.Repeat("x", MaxAttributesSize)
	_, err := validateAttributes(map[string]any{"note": big})
	if !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("err = %v, want ErrInvalidAttributes", err)
	}
	var attrErr *AttributeError
	if !errors.As(err, &attrErr) {
		t.Fatalf("err = %v, want a *AttributeError with details", err)
	}
	if attrErr.Key != "" || attrErr.Reason != "attributes too large" {
		t.Errorf("details = (%q, %q), want (, attributes too large)", attrErr.Key, attrErr.Reason)
	}
}

func TestAttributesContractHelpers(t *testing.T) {
	// The absent/clear/replace signals (Story 4.4 update contract):
	// attributesUnchanged = nil, attributesCleared = explicit empty object,
	// anything else replaces.
	if !attributesUnchanged(nil) {
		t.Error("attributesUnchanged(nil) = false, want true (absent = unchanged)")
	}
	if attributesUnchanged(map[string]any{}) {
		t.Error("attributesUnchanged({}) = true, want false")
	}
	if attributesUnchanged(map[string]any{"a": 1}) {
		t.Error("attributesUnchanged({a:1}) = true, want false")
	}
	if !attributesCleared(map[string]any{}) {
		t.Error("attributesCleared({}) = false, want true ({} = clear)")
	}
	if attributesCleared(nil) {
		t.Error("attributesCleared(nil) = true, want false")
	}
	if attributesCleared(map[string]any{"a": 1}) {
		t.Error("attributesCleared({a:1}) = true, want false")
	}
}
