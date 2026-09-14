package core

import "time"

// Status derivation (Story 5.3, FR-14/AD-4/AD-5): the shared derived-status /
// inspection-clock function. OOS (Out of Service) is DERIVED on read from the
// persisted inspection + reinstatement records — NEVER stored as a flag. The
// tool's effective schedule interval (per-tool override else type default,
// resolved through the Admin SchedulesPort, AD-16) is passed in as a plain
// duration; the pure function renders the color + the next-due anchor that
// Stories 5.4/5.5 feed and 6.1 renders.

// ToolStatusCode is the derived status of a tool (AD-4/AD-5): `oos` (Out of
// Service — a failed inspection not since reinstated), `red` (past due or
// never inspected), `orange` (due within the static window) or `green`
// (fresh). It is NEVER persisted — derived on read.
type ToolStatusCode string

const (
	ToolStatusCodeOOS    ToolStatusCode = "oos"
	ToolStatusCodeRed    ToolStatusCode = "red"
	ToolStatusCodeOrange ToolStatusCode = "orange"
	ToolStatusCodeGreen  ToolStatusCode = "green"
)

// ToolStatus is the derived status of one tool (AD-4/AD-5): the color plus the
// next-due timestamp. NextDue is nil for `oos` (only a reinstatement resets
// it, Story 5.6) and for the never-inspected `red` (there is no base to add
// the interval to).
type ToolStatus struct {
	Status  ToolStatusCode
	NextDue *time.Time
}

// OrangeWindowDays is the static orange window: a tool is `orange` when its
// next due lies within this many days of `now`. The consumer adoption of the
// `inspection_orange_window_days` app_setting is deferred (deferred-work.md);
// the follow-up story threads the value into this constant.
const OrangeWindowDays = 14

// scheduleInterval converts a schedule's interval unit + magnitude to a
// time.Duration (AD-16). Calendar math is a DOCUMENTED approximation — a year
// is 365 days, a quarter 91, a month 30 (the seeds 000019 use 1 year /
// 1 quarter / 1 month / 2 weeks / 3 days); an unknown unit resolves to 0 so
// the caller's loud-failure path can surface the config defect.
func scheduleInterval(unit string, magnitude int) time.Duration {
	var base time.Duration
	switch unit {
	case "year":
		base = 365 * 24 * time.Hour
	case "quarter":
		base = 91 * 24 * time.Hour
	case "month":
		base = 30 * 24 * time.Hour
	case "week":
		base = 7 * 24 * time.Hour
	case "day":
		base = 24 * time.Hour
	default:
		return 0
	}
	return base * time.Duration(magnitude)
}

// deriveToolStatus is the pure derived-status/clock function (AD-4/AD-5):
//
//   - OOS: the LATEST FAILED inspection's submitted_at is at-or-after the
//     latest reinstatement (or none exists) → `oos`, NextDue nil. A PASSING
//     inspection does NOT clear OOS — reinstatement is the SOLE exit (FR-15);
//     OOS is derived from the latest FAILED inspection not since reinstated.
//     A fail STRICTLY BEFORE the latest reinstatement is NOT OOS; a fail at
//     the EXACT reinstatement timestamp is OOS (the equal-timestamp boundary
//     favors safety).
//   - Otherwise `base = max(last successful inspection, latest reinstatement)`
//     (both nil → never-inspected → `red`, NextDue nil, AD-5).
//   - `next_due = base + interval`; `red` when next_due < now, `orange` when
//     next_due <= now + orangeWindowDays, else `green`.
//
// The inputs are read-only pointers; a nil pointer means "no such record".
func deriveToolStatus(latestFailAt, lastSuccessAt, lastReinstatedAt *time.Time, interval time.Duration, now time.Time, orangeWindowDays int) ToolStatus {
	if latestFailAt != nil && !latestFailAt.IsZero() {
		if lastReinstatedAt == nil || !latestFailAt.Before(*lastReinstatedAt) {
			return ToolStatus{Status: ToolStatusCodeOOS}
		}
	}

	var base *time.Time
	if lastSuccessAt != nil && !lastSuccessAt.IsZero() {
		t := *lastSuccessAt
		base = &t
	}
	if lastReinstatedAt != nil && !lastReinstatedAt.IsZero() {
		t := *lastReinstatedAt
		if base == nil || t.After(*base) {
			base = &t
		}
	}
	if base == nil {
		return ToolStatus{Status: ToolStatusCodeRed}
	}

	nextDue := base.Add(interval)
	window := time.Duration(orangeWindowDays) * 24 * time.Hour
	switch {
	case nextDue.Before(now):
		return ToolStatus{Status: ToolStatusCodeRed, NextDue: &nextDue}
	case !nextDue.After(now.Add(window)):
		return ToolStatus{Status: ToolStatusCodeOrange, NextDue: &nextDue}
	default:
		return ToolStatus{Status: ToolStatusCodeGreen, NextDue: &nextDue}
	}
}
