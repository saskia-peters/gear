package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// inspectionStore seeds the Story 5.1/5.3 I/O matrix: one ACTIVE tool whose
// type REQUIRES qualification id-q1 (checklist mode) with the type's ordered
// checklist items and the AD-5 schedule-resolution inputs (ScheduleID empty →
// inherit the type default id-s1). Tests mutate the type's
// RequiredQualificationID to cover the no-qualification rows.
func inspectionStore() *fakeToolStore {
	return &fakeToolStore{
		types: []*ToolType{{
			ID:                      "id-t1",
			Name:                    "Bohrmaschine",
			DefaultScheduleID:       "id-s1",
			RequiredQualificationID: "id-q1",
			InspectionMode:          InspectionModeChecklist,
			Items: []ToolTypeChecklistItem{
				{ID: "item-1", Position: 0, Label: "Kabel"},
				{ID: "item-2", Position: 1, Label: "Bohrfutter"},
			},
		}},
		tools: []*Tool{{
			ID:           "id-tool",
			Name:         "Bohrmaschine-01",
			ToolTypeID:   "id-t1",
			ToolTypeName: "Bohrmaschine",
		}},
	}
}

// holdsPort builds a qualification port where actorID holds the given
// qualification ids.
func holdsPort(userID string, ids ...string) *fakeQualificationPort {
	return &fakeQualificationPort{
		qualificationIDs:   []string{"id-q1"},
		heldQualifications: map[string][]string{userID: ids},
	}
}

func TestStartInspectionEligibleNoQualType(t *testing.T) {
	// START_ELIGIBLE / START_NO_QUAL_TYPE: the tool's type has NO required
	// qualification → any inspection.submit holder is eligible: 200 with the
	// tool + its type's inspection_mode, and the qualification port is NEVER
	// called (an unwired/erroring port must not fail the start — nothing to
	// resolve).
	store := inspectionStore()
	store.types[0].RequiredQualificationID = ""
	svc := NewService(
		store,
		nil,
		nil, // nil qualification port: must NOT be reached
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	got, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("StartInspection(no qual type) err = %v, want success", err)
	}
	if got.ToolID != "id-tool" || got.ToolName != "Bohrmaschine-01" {
		t.Errorf("tool = %+v, want id-tool / Bohrmaschine-01", got)
	}
	if got.ToolTypeID != "id-t1" || got.ToolTypeName != "Bohrmaschine" {
		t.Errorf("type = %+v, want id-t1 / Bohrmaschine", got)
	}
	if got.InspectionMode != InspectionModeChecklist {
		t.Errorf("mode = %q, want %q", got.InspectionMode, InspectionModeChecklist)
	}
}

func TestStartInspectionQualified(t *testing.T) {
	// START_QUALIFIED: the type requires qualification id-q1 and the caller
	// HOLDS it (active) → 200 with the tool + mode.
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		holdsPort(actorID, "id-q1"),
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	got, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("StartInspection(qualified) err = %v, want success", err)
	}
	if got.ToolID != "id-tool" || got.InspectionMode != InspectionModeChecklist {
		t.Errorf("result = %+v, want the tool + mode", got)
	}
}

func TestStartInspectionMissingQual(t *testing.T) {
	// START_MISSING_QUAL: the type requires id-q1 and the caller LACKS it → the
	// 403 sentinel ErrToolQualificationMissing (the caller is an
	// inspection.submit holder, so this is the tool-specific gate, not
	// ErrForbidden).
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		holdsPort(actorID), // holds nothing
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	_, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if !errors.Is(err, ErrToolQualificationMissing) {
		t.Fatalf("err = %v, want ErrToolQualificationMissing", err)
	}
	if MsgToolQualificationMissing != "Erforderliche Qualifikation fehlt." {
		t.Errorf("MsgToolQualificationMissing = %q, want the spec German text", MsgToolQualificationMissing)
	}
}

func TestStartInspectionExpiredQual(t *testing.T) {
	// START_EXPIRED_QUAL: the type requires id-q1 and the caller's assignment is
	// EXPIRED — the UserHoldsQualification port answers false (expired = not
	// held, live resolution, AD-7/FR-22) → the same 403 sentinel. The
	// expiry-aware decision itself is a USER-core concern (pinned there); the
	// tools core treats "not held" uniformly.
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		holdsPort(actorID), // the user-core port already resolved the expired assignment
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	_, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if !errors.Is(err, ErrToolQualificationMissing) {
		t.Fatalf("err = %v, want ErrToolQualificationMissing (expired = not held)", err)
	}
}

func TestStartInspectionToolNotFound(t *testing.T) {
	// START_TOOL_NOT_FOUND: unknown tool id AND already-archived tool → the 404
	// sentinel.
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		holdsPort(actorID, "id-q1"),
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	if _, err := svc.StartInspection(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id err = %v, want ErrToolNotFound", err)
	}

	archived := *inspectionStore()
	now := time.Now()
	archived.tools[0].ArchivedAt = &now
	svc.store = &archived
	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("archived id err = %v, want ErrToolNotFound", err)
	}
}

func TestStartInspectionForbidden(t *testing.T) {
	// START_FORBIDDEN: the caller lacks `inspection.submit` → ErrForbidden
	// (defense-in-depth; no tool data). An empty actor id never passes.
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		holdsPort(actorID, "id-q1"),
		&fakePerms{perms: []string{"dashboard.view"}},
		&fakeAudit{},
		nil,
	)
	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if _, err := svc.StartInspection(context.Background(), "", "id-tool"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("empty actor err = %v, want ErrForbidden", err)
	}
}

func TestStartInspectionNilQualPortFailsLoudly(t *testing.T) {
	// A NIL QualificationCatalogPort is a composition-root wiring defect. With a
	// type that REQUIRES a qualification the start path must FAIL LOUDLY (a
	// 500-style internal error, never a silent skip that would bypass the
	// AD-7 gate). (An empty required qualification skips the port and succeeds —
	// pinned by TestStartInspectionEligibleNoQualType.)
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); err == nil {
		t.Fatal("StartInspection(nil port, required qual) err = nil, want internal error")
	} else {
		if errors.Is(err, ErrToolQualificationMissing) {
			t.Fatalf("err = %v, want a 500-style internal error, not the qualification sentinel", err)
		}
		if errors.Is(err, ErrForbidden) {
			t.Fatalf("err = %v, want internal error, not ErrForbidden", err)
		}
	}
}

func TestStartInspectionPortErrorPropagates(t *testing.T) {
	// A port resolution failure surfaces as an internal error (500-style), never
	// the qualification sentinel — the eligibility is unknown, so the start
	// must not proceed.
	svc := NewService(
		inspectionStore(),
		&fakeSchedulesPort{},
		&fakeQualificationPort{qualificationIDs: []string{"id-q1"}, holdErr: errors.New("boom")},
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		&fakeAudit{},
		nil,
	)
	_, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if err == nil {
		t.Fatal("StartInspection(port error) err = nil, want internal error")
	}
	if errors.Is(err, ErrToolQualificationMissing) {
		t.Fatalf("err = %v, want a 500-style internal error, NOT the qualification sentinel (the eligibility is unknown, not missing)", err)
	}
}

func TestStartInspectionAuditsEligibleStart(t *testing.T) {
	// NFR-O1: an ELIGIBLE start is audited best-effort (inspection.start).
	store := inspectionStore()
	store.types[0].RequiredQualificationID = ""
	audit := &fakeAudit{}
	svc := NewService(
		store,
		&fakeSchedulesPort{},
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		audit,
		nil,
	)
	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); err != nil {
		t.Fatalf("StartInspection err = %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationInspectionStart {
		t.Fatalf("audit events = %+v, want one inspection.start audit", audit.events)
	}
	if audit.events[0].actorID != actorID {
		t.Errorf("audit actor = %q, want %q", audit.events[0].actorID, actorID)
	}
}

// submitInspectionService wires the submit I/O matrix around a Service: the
// checklist-mode fixture (id-t1 / id-tool), one ACTIVE schedule (id-s1, 1 year)
// for the interval resolution, a qualification port where actorID HOLDS id-q1,
// and an inspection.submit holder.
func submitInspectionService() (*Service, *fakeToolStore, *fakeAudit) {
	store := inspectionStore()
	audit := &fakeAudit{}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-s1", Name: "1 Jahr", IntervalUnit: admcore.IntervalUnitYear, IntervalMagnitude: 1,
		}}},
		holdsPort(actorID, "id-q1"),
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		audit,
		nil,
	)
	return svc, store, audit
}

// checklistSubmitInput is a fully-answered checklist inspection (SUBMIT_CHECKLIST).
func checklistSubmitInput() InspectionInput {
	return InspectionInput{
		Mode:   InspectionModeChecklist,
		Result: InspectionResultPass,
		Items: []InspectionItemInput{
			{ItemID: "item-1", Result: InspectionResultPass},
			{ItemID: "item-2", Result: InspectionResultPass},
		},
	}
}

func TestSubmitInspectionPassFailPass(t *testing.T) {
	// SUBMIT_PASSFAIL (pass): a pass_fail-mode tool, result pass, NO items →
	// the record persists (pass, no items), the derived status reads green when
	// the tool is fresh, and the submit is audited (inspection.submit).
	svc, store, audit := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	now := time.Now()
	store.status = &ToolInspectionStatus{LastSuccessAt: &now}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass, Notes: "Alles ok",
	})
	if err != nil {
		t.Fatalf("SubmitInspection(pass) err = %v", err)
	}
	insp := result.Inspection
	if insp.OverallResult != InspectionResultPass || insp.Mode != InspectionModePassFail {
		t.Errorf("record = %+v, want pass/pass_fail", insp)
	}
	if insp.Notes != "Alles ok" || insp.InspectorID != actorID || insp.ToolID != "id-tool" {
		t.Errorf("record = %+v, want the actor + notes snapshot", insp)
	}
	if len(insp.Items) != 0 {
		t.Errorf("items = %+v, want none for a pass_fail inspection", insp.Items)
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Errorf("status = %q, want green for a fresh pass", result.Status.Status)
	}
	if len(store.inspections) != 1 || store.inspections[0].OverallResult != InspectionResultPass {
		t.Fatalf("persisted = %+v, want one pass inspection", store.inspections)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationInspectionSubmit {
		t.Fatalf("audit events = %+v, want one inspection.submit audit", audit.events)
	}
}

func TestSubmitInspectionPassFailFail(t *testing.T) {
	// SUBMIT_FAIL: a pass_fail result fail → the record persists (fail) and the
	// derived status reads `oos` with NextDue nil (AD-4 — never stored).
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	latest := &Inspection{ToolID: "id-tool", Mode: InspectionModePassFail, OverallResult: InspectionResultFail, SubmittedAt: time.Now()}
	store.status = &ToolInspectionStatus{Latest: latest}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultFail,
	})
	if err != nil {
		t.Fatalf("SubmitInspection(fail) err = %v", err)
	}
	if result.Inspection.OverallResult != InspectionResultFail {
		t.Errorf("record = %+v, want fail", result.Inspection)
	}
	if result.Status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos on a failed inspection", result.Status.Status)
	}
	if result.Status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for oos", result.Status.NextDue)
	}
}

func TestSubmitInspectionChecklistWithFailures(t *testing.T) {
	// SUBMIT_CHECKLIST: every item answered, ≥1 failed → the record persists
	// (fail) WITH the per-item snapshot (label + position from the type), and
	// the derived status reads `oos`.
	svc, store, _ := submitInspectionService()
	failedAt := time.Now()
	store.status = &ToolInspectionStatus{Latest: &Inspection{
		ToolID: "id-tool", Mode: InspectionModeChecklist, OverallResult: InspectionResultFail, SubmittedAt: failedAt,
	}}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode:   InspectionModeChecklist,
		Result: InspectionResultFail,
		Items: []InspectionItemInput{
			{ItemID: "item-1", Result: InspectionResultPass},
			{ItemID: "item-2", Result: InspectionResultFail},
		},
	})
	if err != nil {
		t.Fatalf("SubmitInspection(checklist) err = %v", err)
	}
	if result.Inspection.OverallResult != InspectionResultFail {
		t.Errorf("record = %+v, want fail", result.Inspection)
	}
	if result.Status.Status != ToolStatusCodeOOS {
		t.Errorf("status = %q, want oos when a checklist item fails", result.Status.Status)
	}
	items := result.Inspection.Items
	if len(items) != 2 {
		t.Fatalf("items = %+v, want the two snapshotted items", items)
	}
	// The LABEL + POSITION snapshot comes from the TYPE (server-side), never
	// the client body.
	if items[0].Label != "Kabel" || items[0].Position != 0 || items[0].Result != InspectionResultPass {
		t.Errorf("items[0] = %+v, want the Kabel snapshot", items[0])
	}
	if items[1].Label != "Bohrfutter" || items[1].Position != 1 || items[1].Result != InspectionResultFail {
		t.Errorf("items[1] = %+v, want the Bohrfutter snapshot", items[1])
	}
	if len(store.inspections) != 1 || len(store.inspections[0].Items) != 2 {
		t.Fatalf("persisted = %+v, want one inspection with 2 snapshot items", store.inspections)
	}
}

func TestSubmitInspectionGated(t *testing.T) {
	// SUBMIT_GATED: a caller without inspection.submit → ErrForbidden, NO record
	// persisted (defense-in-depth, AD-6).
	svc, store, _ := submitInspectionService()
	svc.perms = &fakePerms{perms: []string{"dashboard.view"}}
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(store.inspections) != 0 {
		t.Error("a gated caller must not persist an inspection")
	}

	// SUBMIT_GATED (qualification): an inspection.submit holder WITHOUT the
	// tool's required qualification → ErrToolQualificationMissing, no record.
	svc.perms = &fakePerms{perms: []string{InspectionSubmitPermission}}
	svc.qualifications = holdsPort(actorID)
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput()); !errors.Is(err, ErrToolQualificationMissing) {
		t.Fatalf("err = %v, want ErrToolQualificationMissing", err)
	}
	if len(store.inspections) != 0 {
		t.Error("a qualification-less caller must not persist an inspection")
	}
}

func TestSubmitInspectionToolNotFound(t *testing.T) {
	// SUBMIT_ARCHIVED / SUBMIT_UNKNOWN: archived or unknown tool → the 404
	// sentinel, no record.
	svc, store, _ := submitInspectionService()
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-missing", checklistSubmitInput()); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id err = %v, want ErrToolNotFound", err)
	}
	archived := *inspectionStore()
	now := time.Now()
	archived.tools[0].ArchivedAt = &now
	svc.store = &archived
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput()); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("archived id err = %v, want ErrToolNotFound", err)
	}
	if len(store.inspections) != 0 {
		t.Error("an archived/unknown tool must not persist an inspection")
	}
}

func TestSubmitInspectionInvalid(t *testing.T) {
	// SUBMIT_INVALID / SUBMIT_NOTES / SUBMIT_CHECKLIST mismatch /
	// SUBMIT_PASSFAIL-items: every bad body answers ErrInspectionInvalid (400)
	// with the field-specific German message and nothing is persisted.
	cases := []struct {
		name       string
		mode       string
		mutate     func(*InspectionInput)
		setupStore func(*fakeToolStore)
		wantMsg    string
	}{
		{"mode mismatch", InspectionModeChecklist, func(in *InspectionInput) { in.Mode = "matrix" }, nil, MsgInspectionModeMismatch},
		{"bad result", InspectionModeChecklist, func(in *InspectionInput) { in.Result = "maybe" }, nil, MsgInspectionResultInvalid},
		{"notes too long", InspectionModePassFail, func(in *InspectionInput) {
			in.Result = InspectionResultPass
			in.Notes = strings.Repeat("ä", MaxInspectionNotesRunes+1)
		}, nil, MsgInspectionNotesTooLong},
		{"checklist unanswered", InspectionModeChecklist, func(in *InspectionInput) {
			in.Items = in.Items[:1]
		}, nil, MsgInspectionItemsMismatch},
		{"checklist extra", InspectionModeChecklist, func(in *InspectionInput) {
			in.Items = append(in.Items, InspectionItemInput{ItemID: "item-9", Result: InspectionResultPass})
		}, nil, MsgInspectionItemsMismatch},
		// Patch 9: an EMPTY type checklist in checklist mode must reject the
		// submit — an empty-snapshot inspection must not bypass the contract.
		{"checklist empty template", InspectionModeChecklist, func(in *InspectionInput) {}, func(store *fakeToolStore) {
			store.types[0].Items = nil
		}, MsgInspectionItemsMismatch},
		{"pass_fail with items", InspectionModePassFail, func(in *InspectionInput) {
			in.Result = InspectionResultPass
			in.Items = []InspectionItemInput{{ItemID: "item-1", Result: InspectionResultPass}}
		}, nil, MsgInspectionItemsUnexpected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := submitInspectionService()
			store.types[0].InspectionMode = tc.mode
			if tc.setupStore != nil {
				tc.setupStore(store)
			}
			input := checklistSubmitInput()
			input.Mode = tc.mode
			tc.mutate(&input)
			_, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", input)
			var inv *InvalidInspectionError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidInspectionError", err)
			}
			if inv.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrInspectionInvalid) {
				t.Errorf("err = %v, want unwraps to ErrInspectionInvalid", err)
			}
			if len(store.inspections) != 0 {
				t.Error("an invalid inspection must not persist")
			}
		})
	}
}

func TestSubmitInspectionRoundTripStatus(t *testing.T) {
	// The submit response's derived status reflects the ACTUAL status read
	// (GetToolInspectionStatus): a tool with a recent pass answers green with a
	// next_due = pass + interval (1 year). The fake store is seeded as if the
	// pass just landed.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	now := time.Now()
	store.status = &ToolInspectionStatus{LastSuccessAt: &now}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("SubmitInspection err = %v", err)
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Fatalf("status = %q, want green", result.Status.Status)
	}
	if result.Status.NextDue == nil {
		t.Fatal("next_due = nil, want the pass + 1-year interval")
	}
	want := now.Add(365 * 24 * time.Hour)
	if !result.Status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (last pass + 1 year)", result.Status.NextDue, want)
	}
}

func TestSubmitInspectionScheduleOverride(t *testing.T) {
	// AD-5 schedule resolution: a per-tool OVERRIDE (id-s2, 1 month) beats the
	// tool type's default (id-s1, 1 year) — the derived next_due uses the
	// override interval, never the type default.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{
		{ID: "id-s1", Name: "1 Jahr", IntervalUnit: admcore.IntervalUnitYear, IntervalMagnitude: 1},
		{ID: "id-s2", Name: "1 Monat", IntervalUnit: admcore.IntervalUnitMonth, IntervalMagnitude: 1},
	}}
	store.tools[0].ScheduleID = "id-s2" // the per-tool override
	now := time.Now()
	store.status = &ToolInspectionStatus{LastSuccessAt: &now}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("SubmitInspection(override) err = %v", err)
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Fatalf("status = %q, want green", result.Status.Status)
	}
	want := now.Add(30 * 24 * time.Hour) // the 1-month OVERRIDE interval
	if result.Status.NextDue == nil || !result.Status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (the override interval, not the type default 1 year)", result.Status.NextDue, want)
	}
}

func TestSubmitInspectionTrimBeforeValidate(t *testing.T) {
	// Trim-before-validate: `" pass "` is a VALID pass and the notes are stored
	// trimmed (the rune-count measures the stored value, not the raw body).
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	now := time.Now()
	store.status = &ToolInspectionStatus{LastSuccessAt: &now}

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: "  pass  ", Notes: "  alles ok  ",
	})
	if err != nil {
		t.Fatalf("SubmitInspection(trimmed) err = %v", err)
	}
	if result.Inspection.OverallResult != InspectionResultPass {
		t.Errorf("overall_result = %q, want pass (trimmed before validation)", result.Inspection.OverallResult)
	}
	if result.Inspection.Notes != "alles ok" {
		t.Errorf("notes = %q, want trimmed", result.Inspection.Notes)
	}
	if len(store.inspections) != 1 || store.inspections[0].Notes != "alles ok" {
		t.Fatalf("persisted = %+v, want the trimmed note stored", store.inspections)
	}
}

func TestSubmitInspectionStatusReadFailsAfterCommit(t *testing.T) {
	// Patch 8: a FAILED status read AFTER the record committed must NOT surface
	// as an error (a client retry would duplicate the record) — a warning is
	// logged and the status is derived from the persisted record alone. A pass
	// reads as green-from-submittedAt; a fail as `oos`.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	store.statusErr = errors.New("boom")

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("SubmitInspection(status read fail) err = %v, want a best-effort success", err)
	}
	if len(store.inspections) != 1 {
		t.Fatalf("persisted = %d, want the record committed despite the status-read failure", len(store.inspections))
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Errorf("status = %q, want green (derived from the persisted pass alone)", result.Status.Status)
	}
	if result.Status.NextDue == nil || !result.Status.NextDue.Equal(result.Inspection.SubmittedAt.Add(365*24*time.Hour)) {
		t.Errorf("next_due = %v, want the persisted submittedAt + 1 year", result.Status.NextDue)
	}
}

func TestSubmitInspectionScheduleLoudFailures(t *testing.T) {
	// Patch 4/16: every interval-resolution defect FAILS LOUDLY (a 500-style
	// internal error) with NOTHING persisted — the clock must never silently
	// collapse to 0.
	base := func() (*Service, *fakeToolStore) {
		svc, store, _ := submitInspectionService()
		store.types[0].InspectionMode = InspectionModePassFail
		return svc, store
	}

	t.Run("nil schedules port", func(t *testing.T) {
		svc, store := base()
		svc.schedules = nil
		if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
			Mode: InspectionModePassFail, Result: InspectionResultPass,
		}); err == nil {
			t.Fatal("nil port err = nil, want a loud internal error")
		}
		if len(store.inspections) != 0 {
			t.Error("nil-port submit must not persist")
		}
	})

	t.Run("port error", func(t *testing.T) {
		svc, store := base()
		svc.schedules = &fakeSchedulesPort{err: errors.New("boom")}
		if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
			Mode: InspectionModePassFail, Result: InspectionResultPass,
		}); err == nil {
			t.Fatal("port error err = nil, want a loud internal error")
		}
		if len(store.inspections) != 0 {
			t.Error("port-error submit must not persist")
		}
	})

	t.Run("effective schedule absent", func(t *testing.T) {
		svc, store := base()
		svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-other", Name: "Anderer", IntervalUnit: admcore.IntervalUnitMonth, IntervalMagnitude: 1,
		}}}
		if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
			Mode: InspectionModePassFail, Result: InspectionResultPass,
		}); err == nil {
			t.Fatal("absent effective schedule err = nil, want a loud internal error")
		}
		if len(store.inspections) != 0 {
			t.Error("absent-schedule submit must not persist")
		}
	})

	t.Run("invalid interval", func(t *testing.T) {
		svc, store := base()
		svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-s1", Name: "Unbekannt", IntervalUnit: "fortnight", IntervalMagnitude: 1,
		}}}
		if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
			Mode: InspectionModePassFail, Result: InspectionResultPass,
		}); err == nil {
			t.Fatal("invalid-interval err = nil, want a loud internal error")
		}
		if len(store.inspections) != 0 {
			t.Error("invalid-interval submit must not persist")
		}
	})
}
