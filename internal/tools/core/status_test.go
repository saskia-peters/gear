package core

import (
	"testing"
	"time"
)

// fixedNow is the shared clock input for the derivation tests.
func fixedNow() time.Time {
	return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
}

func TestScheduleInterval(t *testing.T) {
	// scheduleInterval: the documented approximations (year=365d, quarter=91d,
	// month=30d, week=7d, day=1d) times the magnitude; an unknown unit resolves
	// to 0 (the caller's loud-failure path surfaces it).
	day := 24 * time.Hour
	cases := []struct {
		unit      string
		magnitude int
		want      time.Duration
	}{
		{"year", 1, 365 * day},
		{"quarter", 1, 91 * day},
		{"month", 1, 30 * day},
		{"week", 2, 14 * day},
		{"day", 3, 3 * day},
		{"fortnight", 1, 0},
	}
	for _, tc := range cases {
		if got := scheduleInterval(tc.unit, tc.magnitude); got != tc.want {
			t.Errorf("scheduleInterval(%q, %d) = %v, want %v", tc.unit, tc.magnitude, got, tc.want)
		}
	}
}

func TestDeriveToolStatusOOS(t *testing.T) {
	// DERIVE_OOS: the latest FAILED inspection is at-or-after the latest
	// reinstatement (or none exists) → `oos`, NextDue nil (Red — OOS outranks
	// the clock).
	latestFailAt := fixedNow()
	status := deriveToolStatus(&latestFailAt, nil, nil, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", status.NextDue)
	}
}

func TestDeriveToolStatusPassAfterFailStillOOS(t *testing.T) {
	// FR-15/AD-4: OOS is derived from the LATEST FAILED inspection not since
	// reinstated. A PASSING inspection (t2) after an earlier fail (t1) does NOT
	// clear OOS — reinstatement is the SOLE exit. The pass only becomes the
	// clock's last-success anchor AFTER a reinstatement releases OOS.
	failAt := fixedNow().Add(-10 * 24 * time.Hour)
	passAt := fixedNow().Add(-5 * 24 * time.Hour)
	status := deriveToolStatus(&failAt, &passAt, nil, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos (a passing inspection must NOT clear OOS)", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", status.NextDue)
	}
}

func TestDeriveToolStatusPassAfterFailThenReinstatementNotOOS(t *testing.T) {
	// A reinstatement (t3) AFTER the latest fail (t1) releases OOS — the sole
	// exit (FR-15). The base is max(last success t2, reinstatement t3) = t3, so
	// next_due = t3 + interval.
	failAt := fixedNow().Add(-20 * 24 * time.Hour)
	passAt := fixedNow().Add(-15 * 24 * time.Hour)
	reinstatedAt := fixedNow().Add(-5 * 24 * time.Hour)
	status := deriveToolStatus(&failAt, &passAt, &reinstatedAt, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("status = %q, want green (reinstated since the fail — the clock reset)", status.Status)
	}
	if status.NextDue == nil {
		t.Fatal("next_due = nil, want the reinstatement + interval")
	}
	want := reinstatedAt.Add(30 * 24 * time.Hour)
	if !status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (reinstatement + interval)", status.NextDue, want)
	}
}

func TestDeriveToolStatusFailBeforeReinstatementNotOOS(t *testing.T) {
	// A fail STRICTLY BEFORE the latest reinstatement is NOT OOS (the
	// reinstatement reset the clock, Story 5.6): base = the reinstatement,
	// next_due = it + interval.
	failAt := fixedNow().Add(-10 * 24 * time.Hour)
	reinstatedAt := fixedNow().Add(-5 * 24 * time.Hour)
	status := deriveToolStatus(&failAt, nil, &reinstatedAt, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("status = %q, want green (reinstated since the fail — the clock reset)", status.Status)
	}
	if status.NextDue == nil {
		t.Fatal("next_due = nil, want the reinstatement + interval")
	}
	want := reinstatedAt.Add(30 * 24 * time.Hour)
	if !status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (reinstatement + interval)", status.NextDue, want)
	}
}

func TestDeriveToolStatusFailAtReinstatementIsOOS(t *testing.T) {
	// OOS tie boundary (patch 5): a FAILED inspection whose submitted_at EQUALS
	// the latest reinstatement's created_at is OOS (at-or-after favors safety —
	// only a fail STRICTLY BEFORE a reinstatement is cleared).
	at := fixedNow()
	latestFailAt := at
	reinstatedAt := at
	status := deriveToolStatus(&latestFailAt, nil, &reinstatedAt, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos (fail at the exact reinstatement timestamp)", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", status.NextDue)
	}
}

func TestDeriveToolStatusNever(t *testing.T) {
	// DERIVE_NEVER: no inspections, no reinstatement → `red`, NextDue nil (AD-5).
	status := deriveToolStatus(nil, nil, nil, 30*24*time.Hour, 25, fixedNow())
	if status.Status != ToolStatusCodeRed {
		t.Errorf("status = %q, want red", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for a never-inspected tool", status.NextDue)
	}
}

func TestDeriveToolStatusDue(t *testing.T) {
	// DERIVE_DUE: base + interval vs now — past due → red, within one QUARTER
	// of the tool's own cycle → orange, beyond it → green. The base is max(last
	// success, latest reinstatement). The orange window is interval/4 (user
	// decision 2026-09-17): proportional to each tool's schedule, so a FRESH
	// inspection reads green even on a short (2-week) cycle.
	interval := 30 * 24 * time.Hour
	window := interval / 4 // 7.5 days

	// Past due: last success 40d ago → next_due 10d ago → red (NextDue set).
	old := fixedNow().Add(-40 * 24 * time.Hour)
	status := deriveToolStatus(nil, &old, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeRed || status.NextDue == nil {
		t.Errorf("past due: status = %+v, want red + a next_due", status)
	}

	// Orange: last success 25d ago → next_due 5d from now (within the quarter
	// window).
	orange := fixedNow().Add(-25 * 24 * time.Hour)
	status = deriveToolStatus(nil, &orange, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeOrange {
		t.Errorf("within quarter window: status = %q, want orange", status.Status)
	}

	// Green: last success 10d ago → next_due 20d from now (beyond the quarter
	// window).
	green := fixedNow().Add(-10 * 24 * time.Hour)
	status = deriveToolStatus(nil, &green, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("beyond quarter window: status = %q, want green", status.Status)
	}

	// Orange boundary: next_due EXACTLY now + interval/4 is still orange (≤).
	atBoundary := fixedNow().Add(-(interval - window))
	status = deriveToolStatus(nil, &atBoundary, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeOrange {
		t.Errorf("boundary = %q, want orange (≤ now + interval/4)", status.Status)
	}

	// The reinstatement anchor wins over an older success: a reinstatement 10d
	// ago with a success 40d ago → next_due 20d from now → green (the clock
	// reset to the reinstatement, AD-5).
	reinstated := fixedNow().Add(-10 * 24 * time.Hour)
	status = deriveToolStatus(nil, &old, &reinstated, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("reinstatement anchor: status = %q, want green (max anchor)", status.Status)
	}
}

func TestDeriveToolStatusOrangeWindowPercent(t *testing.T) {
	// Story 5-2c (D1, ORANGE_PERCENT_CHANGED): the orange window is a CONSUMED
	// percentage of the tool's own interval — 25% = interval/4 (the old
	// behavior), 50% doubles it, 100% swallows the whole cycle. A last-success
	// 20d ago on a 30d cycle → next_due 10d out → green at 25% (window 7.5d),
	// orange at 50% (window 15d).
	interval := 30 * 24 * time.Hour
	base := fixedNow().Add(-20 * 24 * time.Hour)
	if s := deriveToolStatus(nil, &base, nil, interval, 25, fixedNow()); s.Status != ToolStatusCodeGreen {
		t.Errorf("percent 25: status = %q, want green (window 7.5d, next_due 10d out)", s.Status)
	}
	if s := deriveToolStatus(nil, &base, nil, interval, 50, fixedNow()); s.Status != ToolStatusCodeOrange {
		t.Errorf("percent 50: status = %q, want orange (window 15d, next_due 10d out)", s.Status)
	}
	// 100%: the whole interval is the window — a FRESH pass 1d ago (next_due
	// 29d out) still reads orange.
	fresh := fixedNow().Add(-1 * 24 * time.Hour)
	if s := deriveToolStatus(nil, &fresh, nil, interval, 100, fixedNow()); s.Status != ToolStatusCodeOrange {
		t.Errorf("percent 100: status = %q, want orange (window = the whole interval)", s.Status)
	}
}

func TestDeriveToolStatusShortCycleFreshIsGreen(t *testing.T) {
	// User bug report (2026-09-17): a tool on a 2-WEEK (14-day) inspection cycle
	// read "Ausstehend" after a passing inspection, because the OLD fixed 14-day
	// orange window exactly swallowed the whole cycle. With the proportional
	// window (interval/4 = 3.5 days), a FRESH pass reads green/Einsatzbereit.
	interval := 14 * 24 * time.Hour
	// Fresh pass: last success ~now → next_due = now + 14d, far beyond the
	// 3.5-day quarter window → green.
	fresh := fixedNow()
	status := deriveToolStatus(nil, &fresh, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("fresh 2-week-cycle pass: status = %q, want green (Einsatzbereit)", status.Status)
	}
	// Approaching due: last success 11d ago → next_due 3d from now, within the
	// quarter window → orange.
	due := fixedNow().Add(-11 * 24 * time.Hour)
	status = deriveToolStatus(nil, &due, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeOrange {
		t.Errorf("due 2-week-cycle tool: status = %q, want orange", status.Status)
	}
	// Past due: last success 15d ago → next_due 1d past → red.
	past := fixedNow().Add(-15 * 24 * time.Hour)
	status = deriveToolStatus(nil, &past, nil, interval, 25, fixedNow())
	if status.Status != ToolStatusCodeRed {
		t.Errorf("past-due 2-week-cycle tool: status = %q, want red", status.Status)
	}
}
