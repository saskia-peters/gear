package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Flexible attributes on tools & tool types (Story 4.4, FR-10/AD-3): the
// shared validation + update-contract helpers for the `attributes JSONB`
// extension surface on BOTH entities. The rules mirror the User-module profile
// attributes precedent (internal/user/core/profile.go, Story 1.9) so the
// modules agree on the JSONB extension contract: attribute KEYS are non-empty,
// trimmed and bounded (MaxAttributeKeyRunes), values must be JSON-serializable,
// and the whole map must fit in MaxAttributesSize once serialized. A non-object
// shape cannot reach this package — the typed `map[string]any` input fields
// reject arrays/scalars at the HTTP decode boundary (matching the user
// precedent); out-of-band non-object stored values still degrade to the
// existing `_unparseable` read fallback.

// Attribute-validation caps (Story 4.4 / FR-10 / AD-3).
const (
	// MaxAttributeKeyRunes caps the length of a single custom attribute key.
	MaxAttributeKeyRunes = 64
	// MaxAttributesSize caps the JSON-serialized size of the attributes map.
	MaxAttributesSize = 16 * 1024
)

// MsgInvalidAttributes is the German 400 message for an invalid `attributes`
// payload (bad key, an unserializable value or an over-size map). The handler
// also reports machine-readable `details` (the offending key and reason)
// alongside this message, mirroring the user module.
const MsgInvalidAttributes = "Die benutzerdefinierten Attribute sind ungültig."

// ErrInvalidAttributes is returned when the `attributes` map fails validation:
// an empty or over-long key, a value that cannot be JSON-serialized, or a
// serialized map exceeding the 16 KB cap. Handlers map it to 400
// invalid_request. The concrete failure is carried by a wrapping
// *AttributeError (Key + Reason) so handlers can surface machine-readable
// details; errors.Is(err, ErrInvalidAttributes) matches both.
var ErrInvalidAttributes = errors.New("tools core: invalid custom attributes")

// AttributeError is the detailed variant of ErrInvalidAttributes: it
// identifies the offending attribute key and a machine-readable reason so the
// 400 invalid_request envelope can carry `details` (e.g.
// {"key":"note","reason":"empty key"}). It unwraps to ErrInvalidAttributes.
type AttributeError struct {
	// Key is the offending attribute key (empty for whole-map failures such as
	// an over-size attributes object).
	Key string
	// Reason is a stable machine-readable failure reason.
	Reason string
}

// Error implements the error interface.
func (e *AttributeError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("invalid attribute %q: %s", e.Key, e.Reason)
	}
	return fmt.Sprintf("invalid attributes: %s", e.Reason)
}

// Unwrap reports ErrInvalidAttributes so errors.Is matches the sentinel.
func (e *AttributeError) Unwrap() error { return ErrInvalidAttributes }

// validateAttributes validates the extensible-attribute set (Story 4.4) and
// returns the NORMALIZED map for storage: keys are trimmed (a `" note "` key
// becomes `"note"`), must be non-empty after trimming, ≤ MaxAttributeKeyRunes
// runes and UNIQUE after trimming (two keys that trim to the same string are
// rejected — no silent last-wins collapse), values must be JSON-serializable,
// and the serialized map must fit in MaxAttributesSize. A nil input returns
// (nil, nil) — "leave unchanged". Because the Go json decoder maps an explicit
// JSON `null` to a nil map, a `"attributes": null` body is treated as ABSENT
// (unchanged) too — consistent with nil-as-absent, never a clear. The empty
// map `{}` is valid and returned as a non-nil empty map (the explicit clear).
// Failures carry a *AttributeError with the offending key and a machine-
// readable reason.
func validateAttributes(attrs map[string]any) (map[string]any, error) {
	if attrs == nil {
		return nil, nil
	}
	normalized := make(map[string]any, len(attrs))
	seen := make(map[string]struct{}, len(attrs))
	for key, val := range attrs {
		k := strings.TrimSpace(key)
		if k == "" {
			return nil, &AttributeError{Key: key, Reason: "empty key"}
		}
		if utf8.RuneCountInString(k) > MaxAttributeKeyRunes {
			return nil, &AttributeError{Key: key, Reason: "key too long"}
		}
		// Two keys that TRIM to the same string would silently collide in a
		// map (last-wins); reject them like the client editor does, so the
		// server is never more permissive than the SPA.
		if _, dup := seen[k]; dup {
			return nil, &AttributeError{Key: key, Reason: "duplicate key"}
		}
		seen[k] = struct{}{}
		// Reject values that cannot be JSON-serialized (e.g. a NaN float):
		// marshalling each value up front surfaces the offending key's failure
		// deterministically.
		if _, err := json.Marshal(val); err != nil {
			return nil, &AttributeError{Key: key, Reason: "value not JSON-serializable"}
		}
		normalized[k] = val
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, &AttributeError{Reason: "attributes not JSON-serializable"}
	}
	if len(data) > MaxAttributesSize {
		return nil, &AttributeError{Reason: "attributes too large"}
	}
	return normalized, nil
}

// attributesUnchanged reports whether the submitted attributes field was
// ABSENT (nil) — the update-contract signal "absent = unchanged": the stored
// JSONB is left as-is. Mirrors the user-module profile precedent (a nil field
// leaves the stored attributes unchanged).
func attributesUnchanged(attrs map[string]any) bool {
	return attrs == nil
}

// attributesCleared reports whether the submitted attributes field is an
// EXPLICIT empty object — the update-contract signal "{} = clear": the stored
// JSONB is replaced with the empty object.
func attributesCleared(attrs map[string]any) bool {
	return attrs != nil && len(attrs) == 0
}
