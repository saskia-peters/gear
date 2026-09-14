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
	// DERIVE_OOS: the latest inspection is a fail and there is NO reinstatement
	// after it → `oos`, NextDue nil (Red — OOS outranks the clock).
	latest := &Inspection{OverallResult: InspectionResultFail, SubmittedAt: fixedNow()}
	status := deriveToolStatus(latest, nil, nil, 30*24*time.Hour, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", status.NextDue)
	}
}

func TestDeriveToolStatusFailBeforeReinstatementNotOOS(t *testing.T) {
	// A fail STRICTLY BEFORE the latest reinstatement is NOT OOS (the
	// reinstatement reset the clock, Story 5.6): base = the reinstatement,
	// next_due = it + interval.
	failAt := fixedNow().Add(-10 * 24 * time.Hour)
	reinstatedAt := fixedNow().Add(-5 * 24 * time.Hour)
	latest := &Inspection{OverallResult: InspectionResultFail, SubmittedAt: failAt}
	status := deriveToolStatus(latest, nil, &reinstatedAt, 30*24*time.Hour, fixedNow(), OrangeWindowDays)
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
	// OOS tie boundary (patch 5): a failed inspection whose submitted_at EQUALS
	// the latest reinstatement's created_at is OOS (at-or-after favors safety —
	// only a fail STRICTLY BEFORE a reinstatement is cleared).
	at := fixedNow()
	latest := &Inspection{OverallResult: InspectionResultFail, SubmittedAt: at}
	reinstatedAt := at
	status := deriveToolStatus(latest, nil, &reinstatedAt, 30*24*time.Hour, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos (fail at the exact reinstatement timestamp)", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", status.NextDue)
	}
}

func TestDeriveToolStatusNever(t *testing.T) {
	// DERIVE_NEVER: no inspections, no reinstatement → `red`, NextDue nil (AD-5).
	status := deriveToolStatus(nil, nil, nil, 30*24*time.Hour, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeRed {
		t.Errorf("status = %q, want red", status.Status)
	}
	if status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for a never-inspected tool", status.NextDue)
	}
}

func TestDeriveToolStatusDue(t *testing.T) {
	// DERIVE_DUE: base + interval vs now — past due → red, ≤14d → orange,
	// >14d → green. The base is max(last success, latest reinstatement).
	interval := 30 * 24 * time.Hour

	// Past due: last success 40d ago → next_due 10d ago → red (NextDue set).
	old := fixedNow().Add(-40 * 24 * time.Hour)
	status := deriveToolStatus(nil, &old, nil, interval, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeRed || status.NextDue == nil {
		t.Errorf("past due: status = %+v, want red + a next_due", status)
	}

	// Orange: last success 20d ago → next_due 10d from now (≤14d).
	orange := fixedNow().Add(-20 * 24 * time.Hour)
	status = deriveToolStatus(nil, &orange, nil, interval, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeOrange {
		t.Errorf("≤14d: status = %q, want orange", status.Status)
	}

	// Green: last success 10d ago → next_due 20d from now (>14d).
	green := fixedNow().Add(-10 * 24 * time.Hour)
	status = deriveToolStatus(nil, &green, nil, interval, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeGreen {
		t.Errorf(">14d: status = %q, want green", status.Status)
	}

	// Orange boundary: next_due EXACTLY now + 14d is still orange (≤).
	atBoundary := fixedNow().Add(-(interval - 14*24*time.Hour))
	status = deriveToolStatus(nil, &atBoundary, nil, interval, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeOrange {
		t.Errorf("boundary = %q, want orange (≤ now + 14d)", status.Status)
	}

	// The reinstatement anchor wins over an older success: a reinstatement 10d
	// ago with a success 40d ago → next_due 20d from now → green (the clock
	// reset to the reinstatement, AD-5).
	reinstated := fixedNow().Add(-10 * 24 * time.Hour)
	status = deriveToolStatus(nil, &old, &reinstated, interval, fixedNow(), OrangeWindowDays)
	if status.Status != ToolStatusCodeGreen {
		t.Errorf("reinstatement anchor: status = %q, want green (max anchor)", status.Status)
	}
}
