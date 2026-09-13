package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// inspectionStore seeds the Story 5.1 I/O matrix: one ACTIVE tool whose type
// REQUIRES qualification id-q1 (checklist mode). Tests mutate the type's
// RequiredQualificationID to cover the no-qualification rows.
func inspectionStore() *fakeToolStore {
	return &fakeToolStore{
		types: []*ToolType{{
			ID:                      "id-t1",
			Name:                    "Bohrmaschine",
			RequiredQualificationID: "id-q1",
			InspectionMode:          InspectionModeChecklist,
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