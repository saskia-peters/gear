package core

import "time"

// Qualification expiry kinds (spine table 9, Story 2.6). 'unlimited' never
// expires (Unbegrenzt); 'fixed' carries an expires_at and the assignment
// status derives from it (Gültig / Bald ablaufend / Abgelaufen).
const (
	QualificationExpiryUnlimited = "unlimited"
	QualificationExpiryFixed     = "fixed"
)

// Qualification display statuses (FR-22/AD-7, UX-DR8). The WIRE values are
// stable English codes so the SPA can key its German label map and badge
// classes on them (the client owns the German display strings).
const (
	QualificationStatusValid        = "valid"
	QualificationStatusExpiringSoon = "expiring_soon"
	QualificationStatusExpired      = "expired"
	QualificationStatusUnlimited    = "unlimited"
	// QualificationStatusFixed is the VOCABULARY display status of a fixed
	// qualification (2026-09-08 rework): a fixed qualification has no date of
	// its own, so the vocabulary badge reads "Befristet"; the per-user
	// valid-until is set at assignment and drives the per-assignment status.
	QualificationStatusFixed = "fixed"
)

// qualificationExpiringSoonWindow is how close to the expiry date an assignment
// turns "Bald ablaufend" (30 days).
const qualificationExpiringSoonWindow = 30 * 24 * time.Hour

// qualificationStatus derives the display status of a qualification assignment
// from its expiry model (Story 2.6, FR-22/AD-7): 'unlimited' never expires;
// a 'fixed' assignment is valid while far from the date, expiring_soon within
// the 30-day window, and expired once the date is reached or passed (a
// qualification expiring exactly NOW counts as expired).
func qualificationStatus(a QualificationAssignment, now time.Time) string {
	switch a.ExpiryKind {
	case QualificationExpiryUnlimited:
		return QualificationStatusUnlimited
	case QualificationExpiryFixed:
		if a.ExpiresAt == nil {
			// Defensive: a 'fixed' qualification without a stored date has
			// nothing to expire against — treat as valid rather than crashing.
			return QualificationStatusValid
		}
		if !now.Before(*a.ExpiresAt) {
			return QualificationStatusExpired
		}
		if now.Add(qualificationExpiringSoonWindow).After(*a.ExpiresAt) {
			return QualificationStatusExpiringSoon
		}
		return QualificationStatusValid
	default:
		// Unknown expiry kind (data written out-of-band): treat as valid.
		return QualificationStatusValid
	}
}