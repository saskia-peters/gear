package core

import (
	"bytes"
	"context"
	"errors"
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

func TestGenerateAccessReportComposesBothPorts(t *testing.T) {
	// REPORT_OK: a `dsgvo.access_report` holder → the assembled report carries
	// the User export + the Tool export + a generation timestamp, BOTH ports
	// are called with the target id, and the generation is audited (actor,
	// operation, target, NFR-O1/NFR-O2).
	userPort := &fakeUserPort{export: sampleExport()}
	toolPort := &fakeToolPort{export: sampleInspectionExport()}
	audit := &fakeAudit{}
	svc := NewService(userPort, toolPort, &fakePerms{perms: []string{AccessReportPermission}}, audit, nil)

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
	svc := NewService(userPort, toolPort, &fakePerms{perms: []string{"dashboard.view"}}, audit, nil)

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
	svc := NewService(&fakeUserPort{}, &fakeToolPort{}, &fakePerms{perms: []string{AccessReportPermission}}, &fakeAudit{}, nil)
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
	svc := NewService(userPort, toolPort, &fakePerms{perms: []string{AccessReportPermission}}, audit, nil)

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
	svc := NewService(userPort, toolPort, &fakePerms{perms: []string{AccessReportPermission}}, &fakeAudit{}, nil)

	if _, err := svc.GenerateAccessReport(context.Background(), "u-admin", "u-target"); err == nil {
		t.Fatal("err = nil, want a nil user-export error")
	}
	if userPort.calls != 1 || toolPort.calls != 0 {
		t.Errorf("port calls = user(%d) tool(%d), want user once and tool never after the nil user export",
			userPort.calls, toolPort.calls)
	}

	// A nil TOOL export (the user export succeeded) is equally guarded.
	toolPort2 := &fakeToolPort{export: nil}
	svc2 := NewService(&fakeUserPort{export: sampleExport()}, toolPort2, &fakePerms{perms: []string{AccessReportPermission}}, &fakeAudit{}, nil)
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
	svc := NewService(userPort, toolPort, &fakePerms{perms: []string{AccessReportPermission}}, audit, logger)

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