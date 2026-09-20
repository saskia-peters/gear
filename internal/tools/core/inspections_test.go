package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// testIdempotencyKey is the fixed client-supplied key the inspection/
// reinstatement tests reuse (Story 7.5): a stable per-intent UUID value so
// a retried submit/reinstate (same key) replays instead of inserting.
const testIdempotencyKey = "11111111-1111-1111-1111-111111111111"

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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{"dashboard.view"}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
		nil,
		&fakePerms{perms: []string{InspectionSubmitPermission}},
		nil,
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
	}, testIdempotencyKey)
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
	// derived status reads `oos` with NextDue nil (AD-4 — never stored). The
	// tool was NOT OOS before the submit (the fake's post-commit status read
	// reflects the just-committed fail).
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultFail,
	}, testIdempotencyKey)
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

func TestSubmitInspectionResolvesAppSettingsOnce(t *testing.T) {
	// Story 5-2c (D1): SubmitInspection resolves CurrentAppSettings ONCE per
	// request — the fake port counts the resolutions, so a per-derivation (N+1)
	// settings read would trip this.
	svc, _, _ := submitInspectionService()
	port := &fakeAppSettingsPort{settings: adoptedSettings()}
	svc.appSettings = port

	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput(), testIdempotencyKey); err != nil {
		t.Fatalf("SubmitInspection err = %v", err)
	}
	if port.calls != 1 {
		t.Errorf("CurrentAppSettings resolutions = %d, want exactly 1 (resolve once per submit)", port.calls)
	}
}

func TestReinstateToolResolvesAppSettingsOnce(t *testing.T) {
	// Story 5-2c (D1): ReinstateTool resolves CurrentAppSettings ONCE per
	// request — the post-commit derivation shares a single settings read.
	svc, store, _ := submitInspectionService()
	svc.perms = &fakePerms{perms: []string{ToolReinstatePermission}}
	port := &fakeAppSettingsPort{settings: adoptedSettings()}
	svc.appSettings = port
	// Make the tool OOS so the reinstatement is meaningful (REINSTATE_OK).
	now := time.Now()
	store.status = &ToolInspectionStatus{LatestFailAt: &now}

	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "wiederhergestellt", testIdempotencyKey); err != nil {
		t.Fatalf("ReinstateTool err = %v", err)
	}
	if port.calls != 1 {
		t.Errorf("CurrentAppSettings resolutions = %d, want exactly 1 (resolve once per reinstate)", port.calls)
	}
}

func TestSubmitInspectionChecklistWithFailures(t *testing.T) {
	// SUBMIT_CHECKLIST: every item answered, ≥1 failed → the record persists
	// (fail) WITH the per-item snapshot (label + position from the type), and
	// the derived status reads `oos` (the fake's post-commit read reflects the
	// just-committed fail).
	svc, store, _ := submitInspectionService()

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode:   InspectionModeChecklist,
		Result: InspectionResultFail,
		Items: []InspectionItemInput{
			{ItemID: "item-1", Result: InspectionResultPass},
			{ItemID: "item-2", Result: InspectionResultFail},
		},
	}, testIdempotencyKey)
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
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput(), testIdempotencyKey); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(store.inspections) != 0 {
		t.Error("a gated caller must not persist an inspection")
	}

	// SUBMIT_GATED (qualification): an inspection.submit holder WITHOUT the
	// tool's required qualification → ErrToolQualificationMissing, no record.
	svc.perms = &fakePerms{perms: []string{InspectionSubmitPermission}}
	svc.qualifications = holdsPort(actorID)
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput(), testIdempotencyKey); !errors.Is(err, ErrToolQualificationMissing) {
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
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-missing", checklistSubmitInput(), testIdempotencyKey); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id err = %v, want ErrToolNotFound", err)
	}
	archived := *inspectionStore()
	now := time.Now()
	archived.tools[0].ArchivedAt = &now
	svc.store = &archived
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput(), testIdempotencyKey); !errors.Is(err, ErrToolNotFound) {
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
			_, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", input, testIdempotencyKey)
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
	// (GetToolInspectionStatus): a committed pass answers green with a
	// next_due = submitted_at + interval (1 year) — the fake's read-after-write
	// mirrors the repository.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("SubmitInspection err = %v", err)
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Fatalf("status = %q, want green", result.Status.Status)
	}
	if result.Status.NextDue == nil {
		t.Fatal("next_due = nil, want the pass + 1-year interval")
	}
	want := result.Inspection.SubmittedAt.Add(365 * 24 * time.Hour)
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

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("SubmitInspection(override) err = %v", err)
	}
	if result.Status.Status != ToolStatusCodeGreen {
		t.Fatalf("status = %q, want green", result.Status.Status)
	}
	want := result.Inspection.SubmittedAt.Add(30 * 24 * time.Hour) // the 1-month OVERRIDE interval
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
	}, testIdempotencyKey)
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
	// Patch 8 + Story 5.6: a FAILED status read AFTER the record committed must
	// NOT surface as an error (a client retry would duplicate the record) — a
	// warning is logged and the status is derived from the persisted record
	// alone. The OOS gate reads the status BEFORE the write (read 1 → succeeds,
	// not OOS); the post-commit read (read 2) fails → the best-effort path.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	store.statusErr = errors.New("boom")
	store.statusErrFromRead = 2

	result, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
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
		}, testIdempotencyKey); err == nil {
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
		}, testIdempotencyKey); err == nil {
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
		}, testIdempotencyKey); err == nil {
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
		}, testIdempotencyKey); err == nil {
			t.Fatal("invalid-interval err = nil, want a loud internal error")
		}
		if len(store.inspections) != 0 {
			t.Error("invalid-interval submit must not persist")
		}
	})
}

// ============================================================================
// Story 5.6 — OOS not-inspectable block + reinstatement (FR-14/FR-15/AD-4/AD-9)
// ============================================================================

func TestStartInspectionOOSBlocked(t *testing.T) {
	// START_OOS: the tool's latest FAILED inspection is not since the latest
	// reinstatement (none here) → the start is blocked with ErrToolOutOfService
	// (403, German) BEFORE the qualification gate — an OOS tool is NOT
	// inspectable (FR-14/AD-4).
	svc, store, _ := submitInspectionService()
	failAt := time.Now()
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	_, err := svc.StartInspection(context.Background(), actorID, "id-tool")
	if !errors.Is(err, ErrToolOutOfService) {
		t.Fatalf("err = %v, want ErrToolOutOfService", err)
	}
	if MsgToolOutOfService != "Das Gerät ist außer Betrieb und kann nicht geprüft werden." {
		t.Errorf("MsgToolOutOfService = %q, want the spec German text", MsgToolOutOfService)
	}
}

func TestStartInspectionOOSTieBoundary(t *testing.T) {
	// START_OOS tie boundary: a fail AT the EXACT reinstatement timestamp is
	// OOS (the equal-timestamp boundary favors safety) → the start is blocked.
	svc, store, _ := submitInspectionService()
	failAt := time.Now()
	reinstatedAt := failAt
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt, LastReinstatedAt: &reinstatedAt}

	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); !errors.Is(err, ErrToolOutOfService) {
		t.Fatalf("tie-boundary err = %v, want ErrToolOutOfService", err)
	}
}

func TestStartInspectionReinstatedNotBlocked(t *testing.T) {
	// A fail STRICTLY BEFORE the latest reinstatement is NOT OOS → the start is
	// eligible again (the reinstate clock reset, AD-5).
	svc, store, _ := submitInspectionService()
	failAt := time.Now().Add(-48 * time.Hour)
	reinstatedAt := time.Now()
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt, LastReinstatedAt: &reinstatedAt}

	if _, err := svc.StartInspection(context.Background(), actorID, "id-tool"); err != nil {
		t.Fatalf("reinstated start err = %v, want eligible", err)
	}
}

func TestSubmitInspectionOOSBlocked(t *testing.T) {
	// SUBMIT_OOS: submitting on an OOS tool → ErrToolOutOfService and NOTHING
	// persisted (FR-14/AD-4). The OOS check runs BEFORE the qualification gate.
	svc, store, _ := submitInspectionService()
	failAt := time.Now()
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	_, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", checklistSubmitInput(), testIdempotencyKey)
	if !errors.Is(err, ErrToolOutOfService) {
		t.Fatalf("err = %v, want ErrToolOutOfService", err)
	}
	if len(store.inspections) != 0 {
		t.Error("an OOS tool must not persist an inspection")
	}
}

// reinstateService wires the reinstatement I/O matrix around a Service: the
// checklist-mode fixture, one ACTIVE schedule (id-s1, 1 year) and a
// tool.reinstate holder.
func reinstateService() (*Service, *fakeToolStore, *fakeAudit) {
	store := inspectionStore()
	audit := &fakeAudit{}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-s1", Name: "1 Jahr", IntervalUnit: admcore.IntervalUnitYear, IntervalMagnitude: 1,
		}}},
		holdsPort(actorID, "id-q1"),
		nil,
		&fakePerms{perms: []string{ToolReinstatePermission}},
		nil,
		audit,
		nil,
	)
	return svc, store, audit
}

func TestReinstateToolOK(t *testing.T) {
	// REINSTATE_OK: an OOS tool (a latest fail, NO prior reinstatement) is
	// reinstated by a tool.reinstate holder with a valid reason → the row
	// persists (actor + trimmed reason), the reinstatement is audited and the
	// derived status is NOT OOS with next_due = reinstatement + interval (the
	// clock reset, AD-5). The fake's InsertReinstatement upgrades the status
	// fixture's LastReinstatedAt (read-after-write), so the not-OOS response only
	// holds if the service genuinely re-reads AFTER the write — never a
	// pre-seeded anchor.
	svc, store, audit := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	result, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "  Ersatzteil eingetroffen  ", testIdempotencyKey)
	if err != nil {
		t.Fatalf("ReinstateTool err = %v", err)
	}
	if result.Status.Status == ToolStatusCodeOOS {
		t.Fatalf("status = %q, want NOT oos after reinstatement", result.Status.Status)
	}
	if len(store.reinstatements) != 1 {
		t.Fatalf("persisted = %+v, want one reinstatement", store.reinstatements)
	}
	if result.Status.NextDue == nil {
		t.Fatal("next_due = nil, want the persisted reinstatement + 1-year interval")
	}
	want := store.reinstatements[0].CreatedAt.Add(365 * 24 * time.Hour)
	if !result.Status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (the PERSISTED reinstatement's created_at + 1 year)", result.Status.NextDue, want)
	}
	if store.reinstatements[0].Reason != "Ersatzteil eingetroffen" || store.reinstatements[0].ActorID != actorID || store.reinstatements[0].ToolID != "id-tool" {
		t.Errorf("persisted = %+v, want the trimmed reason + actor + tool", store.reinstatements[0])
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolReinstate {
		t.Fatalf("audit events = %+v, want one tool.reinstate audit", audit.events)
	}
}

func TestReinstateToolNotOOS(t *testing.T) {
	// REINSTATE_NOT_OOS: a reinstatement on a tool that is NOT out of service —
	// never failed, OR already reinstated after the last fail — answers the 400
	// sentinel (MsgToolNotOutOfService) and NOTHING is written (FR-15: a
	// reinstatement is only meaningful as the SOLE exit from OOS; a serviceable
	// tool must never advance the clock or add a ledger row).
	svc, store, _ := reinstateService()

	// Never failed → serviceable.
	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil", testIdempotencyKey); !errors.Is(err, ErrInspectionInvalid) {
		t.Fatalf("never-failed err = %v, want ErrInspectionInvalid", err)
	} else {
		var inv *InvalidInspectionError
		if errors.As(err, &inv) && inv.Message != MsgToolNotOutOfService {
			t.Errorf("message = %q, want %q", inv.Message, MsgToolNotOutOfService)
		}
	}
	if len(store.reinstatements) != 0 {
		t.Error("a non-OOS tool must not persist a reinstatement")
	}
}

func TestReinstateToolDuplicateRejected(t *testing.T) {
	// REINSTATE_DUPLICATE: after a successful reinstatement the tool is
	// serviceable again (the persisted row flipped the derivation) — a SECOND
	// reinstate with a FRESH key (a genuinely new submit intent, Story 7.5)
	// answers the not-OOS 400 with NOTHING more written. (A second reinstate
	// with the SAME key is a REPLAY and returns 200 — pinned by
	// TestReinstateToolReplayAfterCommit.)
	svc, store, _ := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Erstes Ersatzteil", testIdempotencyKey); err != nil {
		t.Fatalf("first reinstatement err = %v", err)
	}
	if len(store.reinstatements) != 1 {
		t.Fatalf("persisted after first = %d, want 1", len(store.reinstatements))
	}

	// A fresh key = a new intent on a now-serviceable tool → the not-OOS 400.
	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Doppelt", "22222222-2222-2222-2222-222222222222"); !errors.Is(err, ErrInspectionInvalid) {
		t.Fatalf("duplicate reinstatement err = %v, want ErrInspectionInvalid (not-OOS)", err)
	} else {
		var inv *InvalidInspectionError
		if errors.As(err, &inv) && inv.Message != MsgToolNotOutOfService {
			t.Errorf("duplicate message = %q, want %q", inv.Message, MsgToolNotOutOfService)
		}
	}
	if len(store.reinstatements) != 1 {
		t.Errorf("persisted after duplicate = %d, want still 1 (nothing more written)", len(store.reinstatements))
	}
}

func TestReinstateToolPostCommitScheduleFailure(t *testing.T) {
	// REINSTATE_POST_COMMIT_SCHEDULE: the reinstatement row commits, then the
	// schedule resolution fails — the committed write must be reported as a
	// SUCCESS with a conservative non-OOS status (green, nil next_due), never an
	// error (a client retry would DUPLICATE the row). The row + audit are
	// written regardless.
	svc, store, audit := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}
	svc.schedules = &fakeSchedulesPort{err: errors.New("boom")}

	result, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil", testIdempotencyKey)
	if err != nil {
		t.Fatalf("ReinstateTool(schedule fail) err = %v, want a best-effort success", err)
	}
	if len(store.reinstatements) != 1 {
		t.Fatalf("persisted = %d, want the row committed despite the schedule failure", len(store.reinstatements))
	}
	if result.Status.Status == ToolStatusCodeOOS {
		t.Errorf("status = %q, want a conservative NON-OOS status", result.Status.Status)
	}
	if result.Status.NextDue != nil {
		t.Errorf("next_due = %v, want nil for the conservative fallback", result.Status.NextDue)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolReinstate {
		t.Fatalf("audit events = %+v, want the tool.reinstate audit written", audit.events)
	}
}

func TestReinstateToolPostCommitReadFailure(t *testing.T) {
	// REINSTATE_POST_COMMIT_READ: the reinstatement row commits, then the
	// post-write status read fails — the committed write must be reported as a
	// SUCCESS with a conservative non-OOS status, never an error (a retry would
	// duplicate the row). The OOS precondition read (read 1) succeeds; the
	// post-write read (read 2) fails via statusErrFromRead.
	svc, store, _ := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}
	store.statusErr = errors.New("boom")
	store.statusErrFromRead = 2

	result, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil", testIdempotencyKey)
	if err != nil {
		t.Fatalf("ReinstateTool(read fail) err = %v, want a best-effort success", err)
	}
	if len(store.reinstatements) != 1 {
		t.Fatalf("persisted = %d, want the row committed despite the status-read failure", len(store.reinstatements))
	}
	if result.Status.Status == ToolStatusCodeOOS {
		t.Errorf("status = %q, want a conservative NON-OOS status", result.Status.Status)
	}
}

func TestReinstateToolValidation(t *testing.T) {
	// REINSTATE_EMPTY / REINSTATE_LONG: an empty/whitespace reason or a reason
	// > 2000 runes answers the 400 sentinel (ErrInspectionInvalid) with the
	// German message and NOTHING is persisted. The tool is OOS (a latest fail,
	// no reinstatement) so the reason validation is what rejects the write.
	svc, store, _ := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "   ", testIdempotencyKey); !errors.Is(err, ErrInspectionInvalid) {
		t.Fatalf("empty reason err = %v, want ErrInspectionInvalid", err)
	}
	if MsgReinstatementReasonRequired != "Bitte gib einen Grund für die Wiederherstellung an." {
		t.Errorf("MsgReinstatementReasonRequired = %q, want the spec German text", MsgReinstatementReasonRequired)
	}
	long := strings.Repeat("ä", MaxReinstatementReasonRunes+1)
	_, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", long, testIdempotencyKey)
	var inv *InvalidInspectionError
	if !errors.As(err, &inv) {
		t.Fatalf("long reason err = %v, want *InvalidInspectionError", err)
	}
	if inv.Message != MsgReinstatementReasonTooLong {
		t.Errorf("message = %q, want %q", inv.Message, MsgReinstatementReasonTooLong)
	}
	if len(store.reinstatements) != 0 {
		t.Error("a validation failure must not persist a reinstatement")
	}
}

func TestReinstateToolGated(t *testing.T) {
	// REINSTATE_GATED: a caller without tool.reinstate → ErrForbidden, NO write
	// (AD-6). An empty actor id never passes.
	svc, store, _ := reinstateService()
	svc.perms = &fakePerms{perms: []string{InspectionSubmitPermission}}
	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil", testIdempotencyKey); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if _, err := svc.ReinstateTool(context.Background(), "", "id-tool", "Ersatzteil", testIdempotencyKey); !errors.Is(err, ErrForbidden) {
		t.Fatalf("empty actor err = %v, want ErrForbidden", err)
	}
	if len(store.reinstatements) != 0 {
		t.Error("a gated caller must not persist a reinstatement")
	}
}

func TestReinstateToolToolNotFound(t *testing.T) {
	// REINSTATE_ARCHIVED / REINSTATE_UNKNOWN: an unknown or archived tool → the
	// 404 sentinel, no write.
	svc, store, _ := reinstateService()
	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-missing", "Ersatzteil", testIdempotencyKey); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id err = %v, want ErrToolNotFound", err)
	}
	archived := *inspectionStore()
	now := time.Now()
	archived.tools[0].ArchivedAt = &now
	svc.store = &archived
	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil", testIdempotencyKey); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("archived id err = %v, want ErrToolNotFound", err)
	}
	if len(store.reinstatements) != 0 {
		t.Error("an archived/unknown tool must not persist a reinstatement")
	}
}

// ============================================================================
// Story 6.3 — per-tool inspection + reinstatement history (FR-18/AD-6/AD-8)
// ============================================================================

// fakeDisplayNames is a DisplayNameResolver returning a fixed id → display_name
// map (absent ids are MISSING keys — the core maps them to "Deleted User").
// err lets tests simulate a resolution failure (a 500-style internal error);
// calls counts the invocations so tests can pin the ONE-bulk-call invariant.
type fakeDisplayNames struct {
	names map[string]string
	err   error
	calls int
}

func (f *fakeDisplayNames) ResolveDisplayNames(_ context.Context, userIDs []string) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, id := range userIDs {
		if name, ok := f.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

// historyService wires the history I/O matrix around a Service: the checklist
// fixture (id-tool / id-t1), a display-name resolver seeded with the known
// inspector (actorID → "Anna Muster") and an inspection.history.view holder.
func historyService() (*Service, *fakeToolStore, *fakeDisplayNames) {
	store := inspectionStore()
	names := &fakeDisplayNames{names: map[string]string{actorID: "Anna Muster"}}
	svc := NewService(
		store,
		nil,
		nil,
		nil,
		&fakePerms{perms: []string{InspectionHistoryViewPermission}},
		names,
		&fakeAudit{},
		nil,
	)
	return svc, store, names
}

// historyInspections seeds the tool's inspection history in REVERSE-CHRONO order
// (newest first — the order the store returns, pinned by the postgres suite):
// a NEWER checklist inspection (with its per-item snapshot) and an OLDER
// pass_fail inspection, both by the known inspector.
func historyInspections() []*Inspection {
	newer := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	older := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	return []*Inspection{
		{
			ID: "insp-new", ToolID: "id-tool", InspectorID: actorID,
			Mode: InspectionModeChecklist, OverallResult: InspectionResultFail, Notes: "Bohrfutter locker",
			SubmittedAt: newer,
			Items: []InspectionItem{
				{ID: "item-row-1", InspectionID: "insp-new", ItemID: "item-1", Label: "Kabel", Position: 0, Result: InspectionResultPass},
				{ID: "item-row-2", InspectionID: "insp-new", ItemID: "item-2", Label: "Bohrfutter", Position: 1, Result: InspectionResultFail},
			},
		},
		{
			ID: "insp-old", ToolID: "id-tool", InspectorID: actorID,
			Mode: InspectionModePassFail, OverallResult: InspectionResultPass, Notes: "Alles ok",
			SubmittedAt: older,
			Items:       []InspectionItem{},
		},
	}
}

func TestListInspectionHistoryOK(t *testing.T) {
	// HIST_OK: an inspection.history.view holder reads the tool's full history —
	// inspections newest-first (each naming the inspector + timestamp + outcome +
	// notes + mode + the snapshotted per-checklist-item results) and
	// reinstatements newest-first (actor + reason). The display names resolve
	// through the seam in ONE call.
	svc, store, names := historyService()
	store.inspections = historyInspections()
	store.reinstatements = []reinstatementRecord{
		{ToolID: "id-tool", ActorID: actorID, Reason: "Ersatzteil eingetroffen", CreatedAt: time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)},
	}

	got, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("ListInspectionHistory err = %v", err)
	}
	if len(got.Inspections) != 2 || len(got.Reinstatements) != 1 {
		t.Fatalf("history = %d inspections / %d reinstatements, want 2 / 1", len(got.Inspections), len(got.Reinstatements))
	}
	// Newest first (the store order is preserved).
	if got.Inspections[0].ID != "insp-new" || got.Inspections[1].ID != "insp-old" {
		t.Errorf("inspections order = [%s, %s], want [insp-new, insp-old]", got.Inspections[0].ID, got.Inspections[1].ID)
	}
	newest := got.Inspections[0]
	if newest.InspectorName != "Anna Muster" || newest.InspectorID != actorID {
		t.Errorf("newest inspector = %+v, want Anna Muster", newest)
	}
	if newest.Mode != InspectionModeChecklist || newest.OverallResult != InspectionResultFail || newest.Notes != "Bohrfutter locker" {
		t.Errorf("newest record = %+v, want the checklist fail + notes", newest)
	}
	if !newest.SubmittedAt.Equal(time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("newest submitted_at = %v, want the newer timestamp", newest.SubmittedAt)
	}
	// The per-checklist-item snapshot round-trips (label + position + result).
	if len(newest.Items) != 2 {
		t.Fatalf("newest items = %+v, want the two snapshot items", newest.Items)
	}
	if newest.Items[0].Label != "Kabel" || newest.Items[0].Result != InspectionResultPass {
		t.Errorf("items[0] = %+v, want the Kabel pass snapshot", newest.Items[0])
	}
	if newest.Items[1].Label != "Bohrfutter" || newest.Items[1].Result != InspectionResultFail {
		t.Errorf("items[1] = %+v, want the Bohrfutter fail snapshot", newest.Items[1])
	}
	// The older pass_fail inspection carries an EMPTY item list.
	if len(got.Inspections[1].Items) != 0 {
		t.Errorf("older items = %+v, want none for a pass_fail inspection", got.Inspections[1].Items)
	}
	if got.Inspections[1].InspectorName != "Anna Muster" {
		t.Errorf("older inspector = %q, want Anna Muster", got.Inspections[1].InspectorName)
	}
	// The reinstatement names the actor + reason.
	rein := got.Reinstatements[0]
	if rein.ActorName != "Anna Muster" || rein.Reason != "Ersatzteil eingetroffen" || rein.ActorID != actorID {
		t.Errorf("reinstatement = %+v, want Anna Muster + the reason", rein)
	}
	if !rein.CreatedAt.Equal(time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("reinstatement created_at = %v, want the seeded timestamp", rein.CreatedAt)
	}
	// ONE bulk name-resolution call (no N+1 user reads).
	if names.calls != 1 {
		t.Errorf("resolver calls = %d, want exactly 1 (one bulk resolution)", names.calls)
	}
}

func TestListInspectionHistoryOrdering(t *testing.T) {
	// HIST_ORDERING: the core preserves the store's reverse-chronological order
	// (the SQL ORDER BY submitted_at DESC / created_at DESC is pinned by the
	// postgres suite; the core never reorders — it names and passes through).
	svc, store, _ := historyService()
	store.inspections = historyInspections()
	store.reinstatements = []reinstatementRecord{
		{ToolID: "id-tool", ActorID: actorID, Reason: "zuerst", CreatedAt: time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)},
		{ToolID: "id-tool", ActorID: actorID, Reason: "spaeter", CreatedAt: time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)},
	}

	got, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("ListInspectionHistory err = %v", err)
	}
	if got.Reinstatements[0].Reason != "spaeter" || got.Reinstatements[1].Reason != "zuerst" {
		t.Errorf("reinstatements order = [%s, %s], want [spaeter, zuerst] (newest first)", got.Reinstatements[0].Reason, got.Reinstatements[1].Reason)
	}
}

func TestListInspectionHistoryEmpty(t *testing.T) {
	// HIST_EMPTY: a tool with no records answers EMPTY arrays (nil-safe) and the
	// display-name resolver is NEVER called — there are no user ids to resolve.
	svc, _, names := historyService()
	names.calls = 0
	got, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("ListInspectionHistory(empty) err = %v", err)
	}
	if got.Inspections == nil || len(got.Inspections) != 0 {
		t.Errorf("inspections = %+v, want an empty (non-nil) list", got.Inspections)
	}
	if got.Reinstatements == nil || len(got.Reinstatements) != 0 {
		t.Errorf("reinstatements = %+v, want an empty (non-nil) list", got.Reinstatements)
	}
	if names.calls != 0 {
		t.Errorf("resolver calls = %d, want 0 (empty history needs no names)", names.calls)
	}
}

func TestListInspectionHistoryGated(t *testing.T) {
	// HIST_GATED: a caller without inspection.history.view → ErrForbidden (403,
	// no data exposed, AD-6). An empty actor id never passes.
	svc, store, _ := historyService()
	svc.perms = &fakePerms{perms: []string{InspectionSubmitPermission}}
	store.inspections = historyInspections()
	if _, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if _, err := svc.ListInspectionHistory(context.Background(), "", "id-tool"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("empty actor err = %v, want ErrForbidden", err)
	}
}

func TestListInspectionHistoryToolNotFound(t *testing.T) {
	// HIST_UNKNOWN / HIST_ARCHIVED: an unknown or archived tool id → the 404
	// sentinel (German), even for a permission holder.
	svc, _, _ := historyService()
	if _, err := svc.ListInspectionHistory(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id err = %v, want ErrToolNotFound", err)
	}
	archived := *inspectionStore()
	now := time.Now()
	archived.tools[0].ArchivedAt = &now
	svc.store = &archived
	if _, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("archived id err = %v, want ErrToolNotFound", err)
	}
}

func TestListInspectionHistoryDeletedUser(t *testing.T) {
	// HIST_DELETED_USER: an inspector/actor id with NO user row (a deleted
	// account, Story 3.4 not yet built) renders the literal "Deleted User" —
	// never a 404, never an empty string. This applies to BOTH the inspection
	// inspector AND the reinstatement actor.
	svc, store, _ := historyService()
	store.inspections = historyInspections()
	store.inspections[0].InspectorID = "u-deleted"
	store.reinstatements = []reinstatementRecord{
		{ToolID: "id-tool", ActorID: "u-deleted", Reason: "Ersatzteil", CreatedAt: time.Now()},
	}

	got, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool")
	if err != nil {
		t.Fatalf("ListInspectionHistory(deleted user) err = %v", err)
	}
	if got.Inspections[0].InspectorName != DeletedUserDisplayName {
		t.Errorf("deleted inspector = %q, want %q", got.Inspections[0].InspectorName, DeletedUserDisplayName)
	}
	if got.Reinstatements[0].ActorName != DeletedUserDisplayName {
		t.Errorf("deleted actor = %q, want %q", got.Reinstatements[0].ActorName, DeletedUserDisplayName)
	}
	// The KNOWN inspector's name still resolves (never a blanket "Deleted User").
	if got.Inspections[1].InspectorName != "Anna Muster" {
		t.Errorf("known inspector = %q, want Anna Muster", got.Inspections[1].InspectorName)
	}
	if DeletedUserDisplayName != "Deleted User" {
		t.Errorf("DeletedUserDisplayName = %q, want the spec literal", DeletedUserDisplayName)
	}
}

func TestListInspectionHistoryNilResolverFailsLoudly(t *testing.T) {
	// A NIL DisplayNameResolver is a composition-root wiring defect: with
	// HISTORY to name the history path must FAIL LOUDLY (a 500-style internal
	// error, never a silent all-"Deleted User" list). (An empty history skips
	// the seam and succeeds — pinned by TestListInspectionHistoryEmpty.)
	svc, store, _ := historyService()
	store.inspections = historyInspections()
	svc.displayNames = nil
	if _, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool"); err == nil {
		t.Fatal("ListInspectionHistory(nil resolver, history present) err = nil, want internal error")
	} else {
		if errors.Is(err, ErrForbidden) {
			t.Fatalf("err = %v, want a 500-style internal error, not ErrForbidden", err)
		}
	}
}

func TestListInspectionHistoryResolverErrorPropagates(t *testing.T) {
	// A resolver failure surfaces as an internal error (500-style), never a
	// partial/silent history — the names are unknown, so the history must not
	// proceed with guessed names.
	svc, store, names := historyService()
	store.inspections = historyInspections()
	names.err = errors.New("boom")
	if _, err := svc.ListInspectionHistory(context.Background(), actorID, "id-tool"); err == nil {
		t.Fatal("ListInspectionHistory(resolver error) err = nil, want internal error")
	}
}

// ============================================================================
// Story 7.5 — idempotency replay (NFR-R1/FR-12/FR-15): a retried submit /
// reinstate with the SAME tool + client-supplied key replays the committed
// result instead of erroring or inserting a second row. The replay check runs
// BEFORE the OOS gate, so a retried FAIL submit on a now-OOS tool replays 200.
// ============================================================================

func TestSubmitInspectionReplayAfterCommit(t *testing.T) {
	// RETRY_AFTER_COMMIT: a second submit with the same tool + key (the first
	// committed) returns the EXISTING record + derived status, does NOT insert a
	// second row and does NOT re-audit.
	svc, store, audit := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	first, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("first submit err = %v", err)
	}
	if len(store.inspections) != 1 {
		t.Fatalf("persisted after first = %d, want 1", len(store.inspections))
	}
	auditEventsAfterFirst := len(audit.events)

	replay, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("replayed submit err = %v, want 200-style success", err)
	}
	if replay.Inspection.ID != first.Inspection.ID {
		t.Errorf("replayed record id = %q, want the FIRST record %q (no second row)", replay.Inspection.ID, first.Inspection.ID)
	}
	if replay.Inspection.OverallResult != InspectionResultPass {
		t.Errorf("replayed record = %+v, want the committed pass", replay.Inspection)
	}
	if len(store.inspections) != 1 {
		t.Errorf("persisted after replay = %d, want still 1 (no second row)", len(store.inspections))
	}
	// Replay NEVER re-audits.
	if len(audit.events) != auditEventsAfterFirst {
		t.Errorf("audit events = %d, want still %d (a replay never re-audits)", len(audit.events), auditEventsAfterFirst)
	}
}

func TestSubmitInspectionReplayScheduleFailureFallsBack(t *testing.T) {
	// REPLAY_SCHEDULE_FAIL (Story 7.5): the replay path must NEVER surface an
	// error when the schedule port fails (e.g. the schedule was archived
	// between commit and retry) — it logs and derives a conservative status from
	// the record alone (interval 0), so a retried submit always answers 200
	// with a derived status. No test reaches that fallback with a healthy port.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey); err != nil {
		t.Fatalf("first submit err = %v", err)
	}

	// The schedule catalog now fails — the replay of the committed record must
	// still answer a derived status, never an error.
	svc.schedules = &fakeSchedulesPort{err: errors.New("boom")}

	replay, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("replayed submit (schedule port down) err = %v, want a derived status, never an error", err)
	}
	if replay == nil || replay.Inspection == nil || replay.Status.Status == "" {
		t.Fatalf("replay = %+v, want a record + a derived status", replay)
	}
	if len(store.inspections) != 1 {
		t.Errorf("persisted = %d, want still 1 (no second row)", len(store.inspections))
	}
}

func TestSubmitInspectionReplayBeforeOOSGate(t *testing.T) {
	// RETRY_FAIL_TURNS_OOS: the FIRST submit was a FAIL → the tool is now OOS; a
	// retried submit with the SAME key must REPLAY 200 (the replay check runs
	// BEFORE the OOS gate), NOT hit the OOS 403.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	first, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultFail,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("first FAIL submit err = %v", err)
	}
	if first.Status.Status != ToolStatusCodeOOS {
		t.Fatalf("first status = %q, want oos after the committed fail", first.Status.Status)
	}

	replay, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultFail,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("retried FAIL submit err = %v, want a 200 replay (NOT the OOS 403)", err)
	}
	if replay.Inspection.ID != first.Inspection.ID {
		t.Errorf("replayed record id = %q, want the FIRST record %q", replay.Inspection.ID, first.Inspection.ID)
	}
	if replay.Status.Status != ToolStatusCodeOOS {
		t.Errorf("replayed status = %q, want oos (the committed fail derives OOS)", replay.Status.Status)
	}
	if len(store.inspections) != 1 {
		t.Errorf("persisted after replay = %d, want still 1 (no second row)", len(store.inspections))
	}
}

func TestSubmitInspectionReplayArchivedTool(t *testing.T) {
	// REPLAY_ARCHIVED (Story 7.5): a tool ARCHIVED between the commit and the
	// retry must NOT block the replay — the replay check runs BEFORE the tool
	// load, so the committed record is returned (200), never the 404.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail

	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey); err != nil {
		t.Fatalf("first submit err = %v", err)
	}

	// Archive the tool between the commit and the retry.
	now := time.Now()
	store.tools[0].ArchivedAt = &now

	replay, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey)
	if err != nil {
		t.Fatalf("replayed submit on an ARCHIVED tool err = %v, want a 200 replay, never the 404", err)
	}
	if replay == nil || replay.Inspection == nil || replay.Inspection.OverallResult != InspectionResultPass {
		t.Fatalf("replay = %+v, want the committed pass record", replay)
	}
	if len(store.inspections) != 1 {
		t.Errorf("persisted = %d, want still 1 (no second row)", len(store.inspections))
	}
}

func TestSubmitInspectionKeyReusedOtherTool(t *testing.T) {
	// KEY_REUSED_OTHER_TOOL: the key is scoped per tool (the UNIQUE is
	// (tool_id, idempotency_key)) — the SAME key on a DIFFERENT tool is a fresh
	// submit for that tool and inserts its own row.
	svc, store, _ := submitInspectionService()
	store.types[0].InspectionMode = InspectionModePassFail
	store.tools = append(store.tools, &Tool{
		ID: "id-tool-2", Name: "Schleifmaschine-01", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine",
	})

	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey); err != nil {
		t.Fatalf("tool-1 submit err = %v", err)
	}
	if _, err := svc.SubmitInspection(context.Background(), actorID, "id-tool-2", InspectionInput{
		Mode: InspectionModePassFail, Result: InspectionResultPass,
	}, testIdempotencyKey); err != nil {
		t.Fatalf("tool-2 submit (same key) err = %v, want success (scope is per tool)", err)
	}
	if len(store.inspections) != 2 {
		t.Errorf("persisted = %d, want 2 (the same key is legal across two tools)", len(store.inspections))
	}
}

func TestReinstateToolReplayAfterCommit(t *testing.T) {
	// REINSTATE_REPLAY: a second reinstate with the same tool + key (the first
	// committed — the tool is now serviceable) replays 200 with the same
	// not-OOS status, does NOT insert a second row and does NOT re-audit — the
	// NOT-OOS 400 would otherwise reject the retry.
	svc, store, audit := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	first, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil eingetroffen", testIdempotencyKey)
	if err != nil {
		t.Fatalf("first reinstatement err = %v", err)
	}
	if len(store.reinstatements) != 1 {
		t.Fatalf("persisted after first = %d, want 1", len(store.reinstatements))
	}
	auditEventsAfterFirst := len(audit.events)

	replay, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil eingetroffen", testIdempotencyKey)
	if err != nil {
		t.Fatalf("replayed reinstatement err = %v, want 200-style success (NOT the not-OOS 400)", err)
	}
	if replay.Status.Status != first.Status.Status {
		t.Errorf("replayed status = %q, want the first %q", replay.Status.Status, first.Status.Status)
	}
	if len(store.reinstatements) != 1 {
		t.Errorf("persisted after replay = %d, want still 1 (no second row)", len(store.reinstatements))
	}
	// Replay NEVER re-audits.
	if len(audit.events) != auditEventsAfterFirst {
		t.Errorf("audit events = %d, want still %d (a replay never re-audits)", len(audit.events), auditEventsAfterFirst)
	}
}

func TestReinstateToolReplayArchivedTool(t *testing.T) {
	// REPLAY_ARCHIVED (Story 7.5): a tool ARCHIVED between the commit and the
	// retry must NOT block the reinstatement replay — the replay check runs
	// BEFORE the tool load, so the committed row replays 200, never the 404.
	svc, store, _ := reinstateService()
	failAt := time.Now().Add(-48 * time.Hour)
	store.status = &ToolInspectionStatus{LatestFailAt: &failAt}

	if _, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil eingetroffen", testIdempotencyKey); err != nil {
		t.Fatalf("first reinstatement err = %v", err)
	}

	// Archive the tool between the commit and the retry.
	now := time.Now()
	store.tools[0].ArchivedAt = &now

	replay, err := svc.ReinstateTool(context.Background(), actorID, "id-tool", "Ersatzteil eingetroffen", testIdempotencyKey)
	if err != nil {
		t.Fatalf("replayed reinstatement on an ARCHIVED tool err = %v, want a 200 replay, never the 404", err)
	}
	if replay == nil || replay.Status.Status == "" {
		t.Fatalf("replay = %+v, want a derived status", replay)
	}
	if len(store.reinstatements) != 1 {
		t.Errorf("persisted = %d, want still 1 (no second row)", len(store.reinstatements))
	}
}
