package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
	userports "github.com/saskia-peters/gear/internal/user/ports"
)

// fakeUserPort is a userports.DSGVOExportPort over a fixed export value.
type fakeUserPort struct {
	export    *userports.UserDataExport
	err       error
	calls     int
	lastUser  string
}

func (f *fakeUserPort) ExportUserData(_ context.Context, userID string) (*userports.UserDataExport, error) {
	f.calls++
	f.lastUser = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.export, nil
}

// fakeToolPort is a toolports.DSGVOInspectionExportPort over a fixed export.
type fakeToolPort struct {
	export    *toolports.UserInspectionDataExport
	err       error
	calls     int
	lastUser  string
}

func (f *fakeToolPort) ExportUserInspectionData(_ context.Context, userID string) (*toolports.UserInspectionDataExport, error) {
	f.calls++
	f.lastUser = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.export, nil
}

// fakePerms is a PermissionResolver with a fixed set.
type fakePerms struct {
	perms []string
	err   error
}

func (f *fakePerms) ListPermissionsByUser(context.Context, string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.perms, nil
}

// fakeAudit records InsertAuditEvent calls. err lets tests exercise the
// best-effort contract (NFR-O1): a failed audit write is logged, never rolled
// back into the report.
type fakeAudit struct {
	events []auditRecord
	err    error
}

type auditRecord struct {
	actorID   string
	operation string
	detail    string
	severity  string
}

func (f *fakeAudit) InsertAuditEvent(_ context.Context, actorID, operation, detail, severity string) error {
	f.events = append(f.events, auditRecord{actorID: actorID, operation: operation, detail: detail, severity: severity})
	return f.err
}

func sampleExport() *userports.UserDataExport {
	return &userports.UserDataExport{
		Profile: usercore.UserExportProfile{
			ID: "u-target", Email: "target@gear.local", DisplayName: "Ziel Person",
			State: usercore.StateActive, Attributes: map[string]any{},
		},
		Sessions: []usercore.UserSessionExport{{ID: "sess-1"}},
	}
}

func sampleInspectionExport() *toolports.UserInspectionDataExport {
	return &toolports.UserInspectionDataExport{
		Inspections: []toolscore.InspectionExport{{ID: "insp-1", ToolID: "id-tool-a", ToolName: "Bohrmaschine-01"}},
		Summary:     []toolscore.ToolExportSummary{},
	}
}

// fakeUserDeletion is a userports.DSGVODeletionPort fake for the Story 3.4
// lifecycle: it records SoftDeleteAndArchive calls, serves canned archived rows
// and canned purge errors, and remembers purged archive ids.
type fakeUserDeletion struct {
	softCalls  int
	softTarget string
	softReason string
	softActor  string
	softErr    error
	rows       []*userports.DeletedAccount
	listErr    error
	purged     []string
	purgeErr   error
}

func (f *fakeUserDeletion) SoftDeleteAndArchive(_ context.Context, actor *usercore.User, targetUserID, reason string) error {
	f.softCalls++
	f.softTarget = targetUserID
	f.softReason = reason
	if actor != nil {
		f.softActor = actor.ID
	}
	return f.softErr
}

func (f *fakeUserDeletion) ListDeletedAccounts(_ context.Context) ([]*userports.DeletedAccount, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

func (f *fakeUserDeletion) PurgeDeletedAccount(_ context.Context, archiveID string) error {
	f.purged = append(f.purged, archiveID)
	return f.purgeErr
}

// fakeToolDeletion is a toolports.DSGVODeletionPort fake: it records the
// AnonymizeUserReferences calls (target only — the actor id is NOT threaded,
// the orchestrator audits) and can fail the rewrite.
type fakeToolDeletion struct {
	calls  int
	target string
	err    error
}

func (f *fakeToolDeletion) AnonymizeUserReferences(_ context.Context, userID string) error {
	f.calls++
	f.target = userID
	return f.err
}

// fakeActor is an ActorResolver with a fixed user + optional error. A nil user
// AND nil error answers (nil, nil) — the wiring-defect case the orchestrator
// must guard (500, never a panic). byID lets tests resolve per-id (an id absent
// from the map → ErrAdminUserNotFound, the DELETE_UNKNOWN source).
type fakeActor struct {
	user *usercore.User
	err  error
	byID map[string]*usercore.User
}

func (f *fakeActor) GetUserByID(_ context.Context, userID string) (*usercore.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.byID != nil {
		u, ok := f.byID[userID]
		if !ok {
			return nil, usercore.ErrAdminUserNotFound
		}
		return u, nil
	}
	return f.user, nil
}

// deletionService is the wired Story 3.4 orchestrator used by the delete tests:
// the export ports are canned, the deletion ports are the fakes above, and the
// actor seam resolves "u-admin" to a real actor row.
func deletionService(userDel *fakeUserDeletion, toolDel *fakeToolDeletion, perms []string, audit *fakeAudit) *Service {
	return NewService(
		&fakeUserPort{export: sampleExport()},
		&fakeToolPort{export: sampleInspectionExport()},
		userDel,
		toolDel,
		&fakePerms{perms: perms},
		&fakeActor{user: &usercore.User{ID: "u-admin", Email: "admin@gear.local", State: usercore.StateActive}},
		audit,
		nil,
	)
}

// reportingService is the wired Story 3.3 orchestrator used by the report tests
// (deletion ports are inert fakes — the report path never touches them).
func reportingService(userPort userports.DSGVOExportPort, toolPort toolports.DSGVOInspectionExportPort, perms []string, audit *fakeAudit) *Service {
	return NewService(
		userPort,
		toolPort,
		&fakeUserDeletion{},
		&fakeToolDeletion{},
		&fakePerms{perms: perms},
		&fakeActor{},
		audit,
		nil,
	)
}

func TestGenerateAccessReportComposesBothPorts(t *testing.T) {
	// REPORT_OK: a `dsgvo.access_report` holder → the assembled report carries
	// the User export + the Tool export + a generation timestamp, BOTH ports
	// are called with the target id, and the generation is audited (actor,
	// operation, target, NFR-O1/NFR-O2).
	userPort := &fakeUserPort{export: sampleExport()}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	audit := &fakeAudit{}
	svc := reportingService(userPort, toolPort, []string{AccessReportPermission}, audit)

	report, err := svc.GenerateAccessReport(context.Background(), "u-admin", "u-target")
	if err != nil {
		t.Fatalf("GenerateAccessReport err = %v, want success", err)
	}
	if report == nil {
		t.Fatal("report = nil")
	}
	if report.User == nil || report.User.Profile.Email != "target@gear.local" {
		t.Errorf("report.user = %+v, want the user export", report.User)
	}
	if report.Tools == nil || len(report.Tools.Inspections) != 1 {
		t.Errorf("report.tools = %+v, want the inspection export", report.Tools)
	}
	if report.GeneratedAt.IsZero() || report.GeneratedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("generated_at = %v, want a sane UTC timestamp", report.GeneratedAt)
	}
	if userPort.calls != 1 || userPort.lastUser != "u-target" || toolPort.calls != 1 || toolPort.lastUser != "u-target" {
		t.Errorf("port calls = user(%d,%s) tool(%d,%s), want one each with u-target",
			userPort.calls, userPort.lastUser, toolPort.calls, toolPort.lastUser)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want exactly one", len(audit.events))
	}
	ev := audit.events[0]
	if ev.actorID != "u-admin" || ev.operation != AuditOperationAccessReport || ev.severity != AuditSeverityNormal {
		t.Errorf("audit event = %+v, want actor/operation/severity", ev)
	}
	if !strings.Contains(ev.detail, "u-target") {
		t.Errorf("audit detail = %q, want the target user id", ev.detail)
	}
}

func TestGenerateAccessReportForbidden(t *testing.T) {
	// REPORT_GATED: a caller lacking `dsgvo.access_report` → ErrForbidden, and
	// NEITHER port is called nor an audit written — no personal data is
	// assembled (AD-6).
	userPort := &fakeUserPort{export: sampleExport()}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	audit := &fakeAudit{}
	svc := reportingService(userPort, toolPort, []string{"dashboard.view"}, audit)

	_, err := svc.GenerateAccessReport(context.Background(), "u-vol", "u-target")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if userPort.calls != 0 || toolPort.calls != 0 {
		t.Errorf("ports called despite the gate: user=%d tool=%d", userPort.calls, toolPort.calls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit written for a denied report: %+v", audit.events)
	}
}

func TestGenerateAccessReportEmptyActor(t *testing.T) {
	// A missing actor (no authenticated session) maps to ErrForbidden.
	svc := reportingService(&fakeUserPort{}, &fakeToolPort{}, []string{AccessReportPermission}, &fakeAudit{})
	_, err := svc.GenerateAccessReport(context.Background(), "", "u-target")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for an empty actor", err)
	}
}

func TestGenerateAccessReportUnknownUser(t *testing.T) {
	// REPORT_UNKNOWN: the user export port's ErrAdminUserNotFound propagates —
	// the Tool export is never reached and no audit is written for a report
	// that never assembled.
	userPort := &fakeUserPort{err: usercore.ErrAdminUserNotFound}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	audit := &fakeAudit{}
	svc := reportingService(userPort, toolPort, []string{AccessReportPermission}, audit)

	_, err := svc.GenerateAccessReport(context.Background(), "u-admin", "u-gibtsnicht")
	if !errors.Is(err, usercore.ErrAdminUserNotFound) {
		t.Fatalf("err = %v, want ErrAdminUserNotFound", err)
	}
	if toolPort.calls != 0 {
		t.Errorf("tool port reached for an unknown user: calls=%d", toolPort.calls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit written for an unknown user: %+v", audit.events)
	}
}

func TestGenerateAccessReportNilExportGuard(t *testing.T) {
	// REPORT_NIL_EXPORT: a (nil, nil) port return is a wiring defect → a
	// 500-class error, never a `null` section in the report. The USER export
	// runs first; a nil user export short-circuits BEFORE the tool port.
	userPort := &fakeUserPort{export: nil}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	svc := reportingService(userPort, toolPort, []string{AccessReportPermission}, &fakeAudit{})

	if _, err := svc.GenerateAccessReport(context.Background(), "u-admin", "u-target"); err == nil {
		t.Fatal("err = nil, want a nil user-export error")
	}
	if userPort.calls != 1 || toolPort.calls != 0 {
		t.Errorf("port calls = user(%d) tool(%d), want user once and tool never after the nil user export",
			userPort.calls, toolPort.calls)
	}

	// A nil TOOL export (the user export succeeded) is equally guarded.
	toolPort2 := &fakeToolPort{export: nil}
	svc2 := reportingService(&fakeUserPort{export: sampleExport()}, toolPort2, []string{AccessReportPermission}, &fakeAudit{})
	if _, err := svc2.GenerateAccessReport(context.Background(), "u-admin", "u-target"); err == nil {
		t.Fatal("err = nil, want a nil tool-export error")
	}
	if toolPort2.calls != 1 {
		t.Errorf("tool port calls = %d, want one (the user export succeeded)", toolPort2.calls)
	}
}

func TestGenerateAccessReportAuditWriteFailureIsBestEffort(t *testing.T) {
	// NFR-O1: a failed audit write must NOT fail the report — the assembled
	// report is still returned with a nil error and the failure is structured-
	// logged (mirroring the profile audit best-effort contract).
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	audit := &fakeAudit{err: errors.New("audit table unavailable")}
	userPort := &fakeUserPort{export: sampleExport()}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	svc := NewService(userPort, toolPort, &fakeUserDeletion{}, &fakeToolDeletion{}, &fakePerms{perms: []string{AccessReportPermission}}, &fakeActor{}, audit, logger)

	report, err := svc.GenerateAccessReport(context.Background(), "u-admin", "u-target")
	if err != nil {
		t.Fatalf("GenerateAccessReport must succeed despite an audit failure, got: %v", err)
	}
	if report == nil || report.User == nil || report.Tools == nil {
		t.Errorf("report = %+v, want the fully assembled report", report)
	}
	if !strings.Contains(buf.String(), "access-report audit write failed") {
		t.Errorf("expected the audit failure to be logged, got %q", buf.String())
	}
}

// --- Story 3.4: DeleteAccount / ListDeletedAccounts / PurgeDeletedAccount ----

func TestDeleteAccountHappyPath(t *testing.T) {
	// DELETE_OK: a `dsgvo.delete` holder deletes a non-self user with a valid
	// reason → the German confirmation; the Tool rewrite runs FIRST (with the
	// actor + target), then the User soft-delete + archive (with the resolved
	// actor), and the deletion is audited (actor, target, reason, NFR-O2).
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	audit := &fakeAudit{}
	svc := deletionService(userDel, toolDel, []string{DeletePermission}, audit)

	result, err := svc.DeleteAccount(context.Background(), "u-admin", "u-target", "  Auf Wunsch  ")
	if err != nil {
		t.Fatalf("DeleteAccount err = %v, want success", err)
	}
	if result == nil || result.Message != fmt.Sprintf(MsgDeleteConfirmation, "u-target") {
		t.Errorf("result = %+v, want the German confirmation", result)
	}
	// Ordered: target verified + tool rewrite first, user soft-delete second.
	if toolDel.calls != 1 || toolDel.target != "u-target" {
		t.Errorf("tool rewrite = %+v, want one call with target u-target (no actor id threaded)", toolDel)
	}
	if userDel.softCalls != 1 || userDel.softTarget != "u-target" || userDel.softActor != "u-admin" {
		t.Errorf("user soft-delete = %+v, want actor u-admin / target u-target", userDel)
	}
	// The reason is TRIM-med once before validation and audit.
	if userDel.softReason != "Auf Wunsch" {
		t.Errorf("soft-delete reason = %q, want the trimmed reason", userDel.softReason)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want exactly one", len(audit.events))
	}
	ev := audit.events[0]
	if ev.actorID != "u-admin" || ev.operation != AuditOperationDelete || ev.severity != AuditSeverityNormal {
		t.Errorf("audit event = %+v, want actor u-admin / operation dsgvo.delete / severity normal", ev)
	}
	if !strings.Contains(ev.detail, "u-target") || !strings.Contains(ev.detail, "Auf Wunsch") {
		t.Errorf("audit detail = %q, want the target id + reason", ev.detail)
	}
}

func TestDeleteAccountSelfDeletion(t *testing.T) {
	// DELETE_SELF: target == actor → ErrDsgvoDeleteSelf (German 400), and NO
	// port is reached nor an audit written.
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	audit := &fakeAudit{}
	svc := deletionService(userDel, toolDel, []string{DeletePermission}, audit)

	_, err := svc.DeleteAccount(context.Background(), "u-admin", "u-admin", "Grund")
	if !errors.Is(err, ErrDsgvoDeleteSelf) {
		t.Fatalf("err = %v, want ErrDsgvoDeleteSelf", err)
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("ports reached for a self-deletion: tool=%d user=%d", toolDel.calls, userDel.softCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit written for a denied self-deletion: %+v", audit.events)
	}
}

func TestDeleteAccountReasonValidation(t *testing.T) {
	// DELETE_EMPTY_REASON / DELETE_LONG_REASON: an empty or > 2000-rune reason
	// → *DeleteReasonError (German 400), and NO port is reached.
	svc := deletionService(&fakeUserDeletion{}, &fakeToolDeletion{}, []string{DeletePermission}, &fakeAudit{})

	_, err := svc.DeleteAccount(context.Background(), "u-admin", "u-target", "   ")
	if !errors.Is(err, ErrDsgvoReasonInvalid) {
		t.Fatalf("empty reason err = %v, want ErrDsgvoReasonInvalid", err)
	}
	if err.Error() != MsgDsgvoReasonRequired {
		t.Errorf("empty reason message = %q, want %q", err.Error(), MsgDsgvoReasonRequired)
	}

	long := strings.Repeat("x", MaxDeleteReasonRunes+1)
	_, err = svc.DeleteAccount(context.Background(), "u-admin", "u-target", long)
	if !errors.Is(err, ErrDsgvoReasonInvalid) {
		t.Fatalf("long reason err = %v, want ErrDsgvoReasonInvalid", err)
	}
	if err.Error() != MsgDsgvoReasonTooLong {
		t.Errorf("long reason message = %q, want %q", err.Error(), MsgDsgvoReasonTooLong)
	}
}

func TestDeleteAccountUnknownTarget(t *testing.T) {
	// DELETE_UNKNOWN: the target verification (via the actor seam) answers
	// ErrAdminUserNotFound BEFORE the Tool rewrite — NO mutation runs (no
	// partial apply) and no audit is written for a deletion that never happened.
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	audit := &fakeAudit{}
	svc := NewService(
		&fakeUserPort{export: sampleExport()},
		&fakeToolPort{export: sampleInspectionExport()},
		userDel,
		toolDel,
		&fakePerms{perms: []string{DeletePermission}},
		&fakeActor{byID: map[string]*usercore.User{"u-admin": {ID: "u-admin", State: usercore.StateActive}}},
		audit,
		nil,
	)

	_, err := svc.DeleteAccount(context.Background(), "u-admin", "u-gibtsnicht", "Grund")
	if !errors.Is(err, usercore.ErrAdminUserNotFound) {
		t.Fatalf("err = %v, want ErrAdminUserNotFound", err)
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("mutation ports reached for an unknown target: tool=%d user=%d (no partial apply)",
			toolDel.calls, userDel.softCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit written for an unknown target: %+v", audit.events)
	}
}

func TestDeleteAccountAlreadyDeletedTarget(t *testing.T) {
	// An already-deleted tombstone is non-existent to the surface → 404, and NO
	// mutation runs (the tombstone can never be re-erased nor its refs re-rewritten).
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	svc := NewService(
		&fakeUserPort{export: sampleExport()},
		&fakeToolPort{export: sampleInspectionExport()},
		userDel,
		toolDel,
		&fakePerms{perms: []string{DeletePermission}},
		&fakeActor{byID: map[string]*usercore.User{
			"u-admin": {ID: "u-admin", State: usercore.StateActive},
			"u-dead":  {ID: "u-dead", State: usercore.StateDeleted},
		}},
		&fakeAudit{},
		nil,
	)
	_, err := svc.DeleteAccount(context.Background(), "u-admin", "u-dead", "Grund")
	if !errors.Is(err, usercore.ErrAdminUserNotFound) {
		t.Fatalf("err = %v, want ErrAdminUserNotFound for an already-deleted tombstone", err)
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("mutation ports reached for a deleted tombstone: tool=%d user=%d", toolDel.calls, userDel.softCalls)
	}
}

func TestDeleteAccountNilActorGuard(t *testing.T) {
	// NIL_ACTOR: a (nil, nil) actor-resolver return is a wiring defect → a
	// 500-class error, NEVER a panic and NEVER a mutation (no partial apply).
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	svc := NewService(
		&fakeUserPort{export: sampleExport()},
		&fakeToolPort{export: sampleInspectionExport()},
		userDel,
		toolDel,
		&fakePerms{perms: []string{DeletePermission}},
		&fakeActor{}, // user nil, err nil → (nil, nil)
		&fakeAudit{},
		nil,
	)
	_, err := svc.DeleteAccount(context.Background(), "u-admin", "u-target", "Grund")
	if err == nil {
		t.Fatal("err = nil, want a nil-actor 500-class error")
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("mutation ports reached despite a nil actor: tool=%d user=%d", toolDel.calls, userDel.softCalls)
	}
}

func TestDeleteAccountSelfDeletionCaseInsensitive(t *testing.T) {
	// DELETE_SELF must be case-INSENSITIVE: a case-variant UUID of the actor
	// cannot bypass the guard.
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	svc := deletionService(userDel, toolDel, []string{DeletePermission}, &fakeAudit{})
	_, err := svc.DeleteAccount(context.Background(), "U-ADMIN", "u-admin", "Grund")
	if !errors.Is(err, ErrDsgvoDeleteSelf) {
		t.Fatalf("err = %v, want ErrDsgvoDeleteSelf for a case-variant self-id", err)
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("ports reached despite the case-insensitive self guard: tool=%d user=%d", toolDel.calls, userDel.softCalls)
	}
}

func TestDeleteAccountForbidden(t *testing.T) {
	// DELETE_GATED: a caller lacking dsgvo.delete → ErrForbidden, and NO port is
	// reached nor an audit written — no data exposed (AD-6).
	userDel := &fakeUserDeletion{}
	toolDel := &fakeToolDeletion{}
	audit := &fakeAudit{}
	svc := deletionService(userDel, toolDel, []string{"dashboard.view"}, audit)

	_, err := svc.DeleteAccount(context.Background(), "u-vol", "u-target", "Grund")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if toolDel.calls != 0 || userDel.softCalls != 0 {
		t.Errorf("ports reached for a non-holder: tool=%d user=%d", toolDel.calls, userDel.softCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit written for a denied deletion: %+v", audit.events)
	}
}

func TestDeleteAccountAuditWriteFailureIsBestEffort(t *testing.T) {
	// NFR-O1: a failed audit write must NOT fail the deletion — the confirmation
	// is still returned and the failure is structured-logged.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	audit := &fakeAudit{err: errors.New("audit table unavailable")}
	svc := NewService(
		&fakeUserPort{export: sampleExport()},
		&fakeToolPort{export: sampleInspectionExport()},
		&fakeUserDeletion{},
		&fakeToolDeletion{},
		&fakePerms{perms: []string{DeletePermission}},
		&fakeActor{user: &usercore.User{ID: "u-admin", State: usercore.StateActive}},
		audit,
		logger,
	)

	result, err := svc.DeleteAccount(context.Background(), "u-admin", "u-target", "Grund")
	if err != nil {
		t.Fatalf("DeleteAccount must succeed despite an audit failure, got: %v", err)
	}
	if result == nil || result.Message == "" {
		t.Errorf("result = %+v, want the confirmation despite the audit failure", result)
	}
	if !strings.Contains(buf.String(), "delete audit write failed") {
		t.Errorf("expected the audit failure to be logged, got %q", buf.String())
	}
}

func TestListDeletedAccounts(t *testing.T) {
	// LIST_DELETED: a `dsgvo.delete` holder → the archived rows newest-first
	// (the user port's order is passed through); a non-holder → ErrForbidden.
	rows := []*userports.DeletedAccount{
		{ID: "a-1", Email: "x@gear.local", DisplayName: "X Y", DeletedAt: time.Now(), Reason: "Grund"},
	}
	svc := deletionService(&fakeUserDeletion{rows: rows}, &fakeToolDeletion{}, []string{DeletePermission}, &fakeAudit{})
	out, err := svc.ListDeletedAccounts(context.Background(), "u-admin")
	if err != nil {
		t.Fatalf("ListDeletedAccounts err = %v", err)
	}
	if len(out) != 1 || out[0].ID != "a-1" || out[0].Reason != "Grund" {
		t.Errorf("rows = %+v, want the archived row mapped to the wire shape", out)
	}

	// Empty history → an empty list, nil-safe.
	svcEmpty := deletionService(&fakeUserDeletion{rows: nil}, &fakeToolDeletion{}, []string{DeletePermission}, &fakeAudit{})
	outEmpty, err := svcEmpty.ListDeletedAccounts(context.Background(), "u-admin")
	if err != nil {
		t.Fatalf("ListDeletedAccounts(empty) err = %v", err)
	}
	if len(outEmpty) != 0 {
		t.Errorf("empty rows = %+v, want an empty list", outEmpty)
	}

	// Non-holder → 403.
	svcForbidden := deletionService(&fakeUserDeletion{rows: rows}, &fakeToolDeletion{}, []string{"dashboard.view"}, &fakeAudit{})
	if _, err := svcForbidden.ListDeletedAccounts(context.Background(), "u-vol"); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-holder err = %v, want ErrForbidden", err)
	}
}

func TestPurgeDeletedAccount(t *testing.T) {
	// PURGE_OK: a `dsgvo.delete` holder purges an archive id → the user port's
	// transaction runs and the purge is audited (actor, archive id).
	userDel := &fakeUserDeletion{}
	audit := &fakeAudit{}
	svc := deletionService(userDel, &fakeToolDeletion{}, []string{DeletePermission}, audit)

	if err := svc.PurgeDeletedAccount(context.Background(), "u-admin", "a-1"); err != nil {
		t.Fatalf("PurgeDeletedAccount err = %v, want success", err)
	}
	if len(userDel.purged) != 1 || userDel.purged[0] != "a-1" {
		t.Errorf("purged = %+v, want a-1", userDel.purged)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want exactly one", len(audit.events))
	}
	ev := audit.events[0]
	if ev.actorID != "u-admin" || ev.operation != AuditOperationPurge {
		t.Errorf("audit event = %+v, want actor u-admin / operation dsgvo.purge", ev)
	}
	if !strings.Contains(ev.detail, "a-1") {
		t.Errorf("audit detail = %q, want the archive id", ev.detail)
	}

	// PURGE_MISSING: an already-purged archive id → ErrDeletedAccountNotFound.
	userDelMissing := &fakeUserDeletion{purgeErr: usercore.ErrDeletedAccountNotFound}
	svcMissing := deletionService(userDelMissing, &fakeToolDeletion{}, []string{DeletePermission}, &fakeAudit{})
	if err := svcMissing.PurgeDeletedAccount(context.Background(), "u-admin", "a-gone"); !errors.Is(err, usercore.ErrDeletedAccountNotFound) {
		t.Errorf("missing purge err = %v, want ErrDeletedAccountNotFound", err)
	}

	// Non-holder → 403.
	svcForbidden := deletionService(&fakeUserDeletion{}, &fakeToolDeletion{}, []string{"dashboard.view"}, &fakeAudit{})
	if err := svcForbidden.PurgeDeletedAccount(context.Background(), "u-vol", "a-1"); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-holder purge err = %v, want ErrForbidden", err)
	}
}
