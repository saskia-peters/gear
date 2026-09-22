// Package pguuid is the single shared PostgreSQL UUID parser (Epic 4 retro
// item D1): every module's repository previously re-implemented the same
// string→pgtype.UUID scan with INCONSISTENT empty-handling (tools: empty→NULL;
// admin: empty→error). This package defines ONE contract and each module maps
// the shared sentinel to its own domain sentinel.
//
// Contract:
//   - an EMPTY (or whitespace-only) id is a legal "no FK" value → the zero
//     pgtype.UUID{} which encodes as SQL NULL — never an error.
//   - a NON-EMPTY id must be a canonical UUID; a malformed id returns
//     ErrInvalidUUID and the CALLER maps it to its domain not-found sentinel.
package pguuid

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// ErrInvalidUUID is the shared sentinel for a non-empty id that is not a
// canonical UUID. Callers translate it to their module's not-found sentinel
// (e.g. core.ErrToolNotFound) so the surface behavior is unchanged.
var ErrInvalidUUID = errors.New("pguuid: malformed UUID")

// ParseOptional parses a uuid FK that MAY be EMPTY (→ zero pgtype.UUID{} →
// SQL NULL), treating a malformed non-empty id as ErrInvalidUUID. This is the
// single empty-handling contract for the tools + admin repositories.
func ParseOptional(id string) (pgtype.UUID, error) {
	if strings.TrimSpace(id) == "" {
		return pgtype.UUID{}, nil
	}
	var uid pgtype.UUID
	if err := uid.Scan(id); err != nil {
		return pgtype.UUID{}, ErrInvalidUUID
	}
	return uid, nil
}