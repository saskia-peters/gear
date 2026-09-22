package backupjob

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// fakeSettings returns a fixed typed AppSettings (caller may mutate the
// interval mid-test to prove per-tick re-resolution).
type fakeSettings struct {
	mu       sync.Mutex
	settings *admcore.AppSettings
	err      error
}

func (f *fakeSettings) CurrentAppSettings(context.Context) (*admcore.AppSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.settings == nil {
		return &admcore.AppSettings{BackupInterval: time.Hour}, nil
	}
	// Return a snapshot so a concurrent setInterval mutation never races the
	// readers (the test flips the interval mid-run to prove re-resolution).
	cp := *f.settings
	return &cp, nil
}

func (f *fakeSettings) setInterval(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings.BackupInterval = d
}

// fakeDestinations returns fixed destinations.
type fakeDestinations struct {
	dests []*admcore.BackupDestination
	err   error
}

func (f *fakeDestinations) CurrentBackupDestinations(context.Context) ([]*admcore.BackupDestination, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.dests, nil
}

// fakeCipher decrypts "enc:<pw>" to <pw>; anything else fails (a wrong/rotated
// key stands in for an unreadable ciphertext).
type fakeCipher struct {
	err error
}

func (f *fakeCipher) Decrypt(encoded string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if !strings.HasPrefix(encoded, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(encoded, "enc:"), nil
}

// fakeAudit records anonymous audit events.
type fakeAudit struct {
	mu     sync.Mutex
	events []auditEvent
}

type auditEvent struct {
	operation string
	detail    string
	severity  string
}

func (f *fakeAudit) InsertAuditEventAnonymous(_ context.Context, operation, detail, severity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, auditEvent{operation: operation, detail: detail, severity: severity})
	return nil
}

func (f *fakeAudit) details() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, e := range f.events {
		out[i] = e.detail
	}
	return out
}

func (f *fakeAudit) count(detailPrefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.operation == admcore.AuditOperationBackupRun && strings.HasPrefix(e.detail, detailPrefix) {
			n++
		}
	}
	return n
}

func (f *fakeAudit) severityFor(detailPrefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.operation == admcore.AuditOperationBackupRun && strings.HasPrefix(e.detail, detailPrefix) {
			return e.severity
		}
	}
	return ""
}

// fakeDumper writes fixed bytes or fails; callCount records invocations.
type fakeDumper struct {
	err       error
	callCount int
}

func (f *fakeDumper) Dump(_ context.Context, w io.Writer) error {
	f.callCount++
	if f.err != nil {
		return f.err
	}
	_, err := w.Write([]byte("GEAR custom-format dump"))
	return err
}

// slowDumper sleeps longer than a tiny interval so overlapping runs WOULD be
// observable; it tracks the max concurrent invocations.
type slowDumper struct {
	mu        sync.Mutex
	active    int
	maxActive int
}

func (d *slowDumper) Dump(_ context.Context, w io.Writer) error {
	d.mu.Lock()
	d.active++
	if d.active > d.maxActive {
		d.maxActive = d.active
	}
	d.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	d.mu.Lock()
	d.active--
	d.mu.Unlock()
	_, err := w.Write([]byte("GEAR custom-format dump"))
	return err
}

func (d *slowDumper) peak() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.maxActive
}

// panickingDumper panics mid-dump to prove runProtected recovers.
type panickingDumper struct{}

func (panickingDumper) Dump(context.Context, io.Writer) error {
	panic("boom: simulated dumper failure")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestJob wires the fakes around a job with the given destinations.
func newTestJob(dests []*admcore.BackupDestination, dumper Dumper, cipher CipherPort) (*Job, *fakeAudit) {
	if dumper == nil {
		dumper = &fakeDumper{}
	}
	if cipher == nil {
		cipher = &fakeCipher{}
	}
	audit := &fakeAudit{}
	j := New(Deps{
		Logger:       discardLogger(),
		Settings:     &fakeSettings{settings: &admcore.AppSettings{BackupInterval: time.Hour}},
		Destinations: &fakeDestinations{dests: dests},
		Cipher:       cipher,
		Audit:        audit,
		Dumper:       dumper,
	})
	return j, audit
}

func localDest(id, path string) *admcore.BackupDestination {
	return &admcore.BackupDestination{
		ID: id, Name: "local-" + id, Mechanism: admcore.BackupMechanismLocal,
		BucketOrPath: path, PasswordEncrypted: "enc:pw",
	}
}

func s3Dest(id, endpoint string) *admcore.BackupDestination {
	return &admcore.BackupDestination{
		ID: id, Name: "s3-" + id, Mechanism: admcore.BackupMechanismS3,
		Endpoint: endpoint, BucketOrPath: "bucket",
		Username: "svc", PasswordEncrypted: "enc:geheim",
	}
}

func ftpDest(id string) *admcore.BackupDestination {
	return &admcore.BackupDestination{
		ID: id, Name: "ftp-" + id, Mechanism: admcore.BackupMechanismFTP,
		Endpoint: "127.0.0.1", BucketOrPath: "/ftp/path",
		Username: "svc", PasswordEncrypted: "enc:pw",
	}
}

// startS3Recorder runs an in-process S3-compatible endpoint that records PUT
// paths and answers 2xx.
func startS3Recorder(t *testing.T) (string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	puts := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			puts = append(puts, r.URL.Path)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &puts
}

func TestRunOKShippedToEveryShippableDestination(t *testing.T) {
	// RUN_OK: a local + an s3 destination each receive the SAME dated artifact
	// and are audited result=ok; the run exits cleanly.
	localDir := t.TempDir()
	s3URL, s3Puts := startS3Recorder(t)
	j, audit := newTestJob([]*admcore.BackupDestination{
		localDest("dest-local", localDir),
		s3Dest("dest-s3", s3URL),
	}, nil, nil)

	j.RunBackup(context.Background())

	entries, err := os.ReadDir(localDir)
	if err != nil {
		t.Fatalf("ReadDir err = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("local artifacts = %+v, want exactly one dated dump per run", entries)
	}
	if !strings.HasPrefix(entries[0].Name(), "gear-") || !strings.HasSuffix(entries[0].Name(), ".dump") {
		t.Errorf("local artifact name = %q, want gear-<date>-<seq>.dump", entries[0].Name())
	}
	if got, err := os.ReadFile(filepath.Join(localDir, entries[0].Name())); err != nil || string(got) != "GEAR custom-format dump" {
		t.Errorf("local artifact content err=%v got=%q, want the dumped bytes", err, got)
	}
	if len(*s3Puts) != 1 || !strings.HasPrefix((*s3Puts)[0], "/bucket/gear-") || !strings.HasSuffix((*s3Puts)[0], ".dump") {
		t.Errorf("s3 PUTs = %+v, want one dated object under /bucket/gear-*.dump", *s3Puts)
	}
	for _, id := range []string{"dest-local", "dest-s3"} {
		if audit.count("destination="+id+" result=ok") != 1 {
			t.Errorf("audit missing destination=%s result=ok; got %v", id, audit.details())
		}
		if sev := audit.severityFor("destination=" + id + " result=ok"); sev != usercore.AuditSeverityNormal {
			t.Errorf("destination=%s ok severity = %q, want %q", id, sev, usercore.AuditSeverityNormal)
		}
	}
}

func TestOneArtifactNamePerRun(t *testing.T) {
	// PATCH 7b: a single run uses ONE artifact name for every destination —
	// the local file and the s3 object keys match, enabling run-level
	// correlation.
	localDir := t.TempDir()
	s3URL, s3Puts := startS3Recorder(t)
	j, _ := newTestJob([]*admcore.BackupDestination{
		localDest("dest-local", localDir),
		s3Dest("dest-s3", s3URL),
	}, nil, nil)

	j.RunBackup(context.Background())

	entries, _ := os.ReadDir(localDir)
	if len(entries) != 1 {
		t.Fatalf("local artifacts = %+v, want exactly one", entries)
	}
	localName := entries[0].Name()
	if len(*s3Puts) != 1 {
		t.Fatalf("s3 PUTs = %+v, want exactly one", *s3Puts)
	}
	s3Key := strings.TrimPrefix((*s3Puts)[0], "/bucket/")
	if s3Key != localName {
		t.Errorf("s3 object key = %q, local artifact = %q — want the SAME name per run", s3Key, localName)
	}
}

func TestPartialFailIsolatedRunContinues(t *testing.T) {
	// PARTIAL_FAIL: dest A (good local) ships, dest B (a local path whose parent
	// is a regular FILE) fails — logged + audited (high), the run continues (A is
	// NOT rolled back).
	goodDir := t.TempDir()
	// A file standing where a directory is required makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	j, audit := newTestJob([]*admcore.BackupDestination{
		localDest("dest-a", goodDir),
		localDest("dest-b", blocker),
	}, nil, nil)

	j.RunBackup(context.Background())

	good, err := os.ReadDir(goodDir)
	if err != nil || len(good) != 1 {
		t.Fatalf("good destination artifacts = %+v err=%v, want exactly one (A must still ship)", good, err)
	}
	if audit.count("destination=dest-a result=ok") != 1 {
		t.Errorf("audit missing destination=dest-a result=ok; got %v", audit.details())
	}
	if audit.count("destination=dest-b result=failed") != 1 {
		t.Errorf("audit missing destination=dest-b result=failed; got %v", audit.details())
	}
	if sev := audit.severityFor("destination=dest-b result=failed"); sev != usercore.AuditSeverityHigh {
		t.Errorf("failed destination severity = %q, want %q", sev, usercore.AuditSeverityHigh)
	}
}

func TestFtpSftpNotShippableNotFailure(t *testing.T) {
	// FTP_SFTP_DEST: ftp/sftp destinations are logged "not shippable in V1"
	// and the run continues — they are NOT failures. A local destination in the
	// same run still ships.
	localDir := t.TempDir()
	j, audit := newTestJob([]*admcore.BackupDestination{
		ftpDest("dest-ftp"),
		localDest("dest-local", localDir),
	}, nil, nil)

	j.RunBackup(context.Background())

	if audit.count("destination=dest-ftp result=skipped reason=not_shippable_in_v1") != 1 {
		t.Errorf("audit missing ftp not-shippable row; got %v", audit.details())
	}
	if audit.count("destination=dest-local result=ok") != 1 {
		t.Errorf("audit missing local ok row (run must continue past ftp); got %v", audit.details())
	}
	if audit.count("destination=dest-ftp result=failed") != 0 {
		t.Errorf("ftp marked as failed; got %v", audit.details())
	}
	if sev := audit.severityFor("destination=dest-ftp result=skipped"); sev != usercore.AuditSeverityNormal {
		t.Errorf("ftp skipped severity = %q, want %q", sev, usercore.AuditSeverityNormal)
	}
}

func TestOnlyFtpSftpWarnsAndSkipsWithoutDump(t *testing.T) {
	// A run with ONLY handshake-only destinations is NOT a failure — it logs a
	// warning (never silent), audits result=skipped and never runs pg_dump.
	d := &fakeDumper{}
	j, audit := newTestJob([]*admcore.BackupDestination{
		ftpDest("dest-ftp"), ftpDest("dest-sftp"),
	}, d, nil)

	j.RunBackup(context.Background())

	if audit.count("result=skipped reason=no_shippable_destinations") != 1 {
		t.Errorf("audit missing no_shippable skip; got %v", audit.details())
	}
	if d.callCount != 0 {
		t.Errorf("pg_dump invoked %d times for a zero-shippable run, want 0", d.callCount)
	}
}

func TestNoDestinationsWarnsNeverSilent(t *testing.T) {
	// NO_DEST: zero destinations → warn + audit result=skipped, no dump.
	d := &fakeDumper{}
	j, audit := newTestJob(nil, d, nil)

	j.RunBackup(context.Background())

	if audit.count("result=skipped reason=no_destinations") != 1 {
		t.Errorf("audit missing no_destinations skip; got %v", audit.details())
	}
	if d.callCount != 0 {
		t.Errorf("pg_dump invoked %d times with zero destinations, want 0", d.callCount)
	}
}

func TestDecryptFailSkipsS3DestinationRunContinues(t *testing.T) {
	// DECRYPT_FAIL: an unreadable stored credential skips an S3 destination
	// (logged + audited high) and the run continues.
	endpoint, s3Puts := startS3Recorder(t)
	bad := s3Dest("dest-bad", endpoint)
	bad.PasswordEncrypted = "not-ciphertext"
	j, audit := newTestJob([]*admcore.BackupDestination{bad}, nil, nil)

	j.RunBackup(context.Background())

	if audit.count("destination=dest-bad result=failed reason=decrypt") != 1 {
		t.Errorf("audit missing decrypt-fail row; got %v", audit.details())
	}
	if sev := audit.severityFor("destination=dest-bad result=failed reason=decrypt"); sev != usercore.AuditSeverityHigh {
		t.Errorf("decrypt-fail severity = %q, want %q", sev, usercore.AuditSeverityHigh)
	}
	if len(*s3Puts) != 0 {
		t.Errorf("decrypt-failed destination still PUT an object: %v", *s3Puts)
	}
}

func TestLocalShipsWithoutDecrypting(t *testing.T) {
	// PATCH 4: a local destination with a leftover/unreadable ciphertext still
	// ships — local shipping needs no credential, so a rotated encryption key
	// must never silently stop local backups.
	dir := t.TempDir()
	d := localDest("dest-local", dir)
	d.PasswordEncrypted = "not-ciphertext"
	j, audit := newTestJob([]*admcore.BackupDestination{d}, nil, nil)

	j.RunBackup(context.Background())

	if audit.count("destination=dest-local result=ok") != 1 {
		t.Errorf("local dest skipped on an unreadable (but unneeded) ciphertext; got %v", audit.details())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("local artifact = %+v, want exactly one", entries)
	}
}

func TestLocalEmptyPathFailsLoud(t *testing.T) {
	// PATCH 7a: an empty local BucketOrPath is a loud per-destination failure
	// (audited high), never a relative artifact in the container CWD.
	j, audit := newTestJob([]*admcore.BackupDestination{
		localDest("dest-empty", ""),
	}, nil, nil)

	j.RunBackup(context.Background())

	if audit.count("destination=dest-empty result=failed reason=no_path") != 1 {
		t.Errorf("audit missing no_path row; got %v", audit.details())
	}
	if sev := audit.severityFor("destination=dest-empty result=failed reason=no_path"); sev != usercore.AuditSeverityHigh {
		t.Errorf("no_path severity = %q, want %q", sev, usercore.AuditSeverityHigh)
	}
}

func TestPgDumpFailAbortsRunNoDestinationAttempted(t *testing.T) {
	// PG_DUMP_FAIL: a non-zero pg_dump aborts the whole run — no destination is
	// attempted, audited result=failed stage=dump (high).
	dir := t.TempDir()
	dumper := &fakeDumper{err: errors.New("pg_dump: connection refused")}
	j, audit := newTestJob([]*admcore.BackupDestination{localDest("dest-a", dir)}, dumper, nil)

	j.RunBackup(context.Background())

	if audit.count("result=failed stage=dump") != 1 {
		t.Errorf("audit missing dump-failure row; got %v", audit.details())
	}
	if sev := audit.severityFor("result=failed stage=dump"); sev != usercore.AuditSeverityHigh {
		t.Errorf("dump-fail severity = %q, want %q", sev, usercore.AuditSeverityHigh)
	}
	if audit.count("destination=dest-a result=") != 0 {
		t.Errorf("a destination was attempted after a failed dump; got %v", audit.details())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("artifact written despite dump failure: %+v", entries)
	}
}

func TestNilDestinationRowSkipped(t *testing.T) {
	// PATCH 2b: a nil element in the destinations slice never panics the job —
	// it is skipped, and the healthy sibling still ships.
	dir := t.TempDir()
	j, audit := newTestJob([]*admcore.BackupDestination{
		nil,
		localDest("dest-a", dir),
	}, nil, nil)

	j.RunBackup(context.Background())

	if audit.count("destination=dest-a result=ok") != 1 {
		t.Errorf("audit missing dest-a ok; got %v", audit.details())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("artifacts = %+v, want exactly one", entries)
	}
}

func TestRunProtectedRecoversPanic(t *testing.T) {
	// PATCH 2c: a panicking dumper is recovered by runProtected — the panic
	// never escapes into the caller (and would never kill the Start loop).
	j, _ := newTestJob([]*admcore.BackupDestination{localDest("dest-a", t.TempDir())}, &panickingDumper{}, nil)
	// Must NOT panic.
	j.runProtected(context.Background())
}

func TestNewPanicsOnNilDeps(t *testing.T) {
	// PATCH 2a: composition-root wiring defects fail loudly at New, not mid-run.
	cases := []struct {
		name string
		mut  func(*Deps)
	}{
		{"nil settings", func(d *Deps) { d.Settings = nil }},
		{"nil destinations", func(d *Deps) { d.Destinations = nil }},
		{"nil cipher", func(d *Deps) { d.Cipher = nil }},
		{"nil dumper", func(d *Deps) { d.Dumper = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("New with %s did not panic", tc.name)
				}
			}()
			deps := Deps{
				Logger:       discardLogger(),
				Settings:     &fakeSettings{},
				Destinations: &fakeDestinations{},
				Cipher:       &fakeCipher{},
				Audit:        &fakeAudit{},
				Dumper:       &fakeDumper{},
			}
			tc.mut(&deps)
			_ = New(deps)
		})
	}
}

func TestStartHonorsBackupInterval(t *testing.T) {
	// Pins the scheduling contract (spec INTERVAL): with StartupDelay 0 and a
	// small backup_interval, Start fires the startup run and then ticks on the
	// interval — two backup.run rows prove the interval setting (not the 24h
	// default) drives the timer.
	audit := &fakeAudit{}
	j := New(Deps{
		Logger:   discardLogger(),
		Settings: &fakeSettings{settings: &admcore.AppSettings{BackupInterval: 20 * time.Millisecond}},
		Destinations: &fakeDestinations{dests: []*admcore.BackupDestination{
			localDest("dest-local", t.TempDir()),
		}},
		Cipher: &fakeCipher{},
		Audit:  audit,
		Dumper: &fakeDumper{},
	}, WithStartupDelay(0))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if n := audit.count("destination=dest-local result=ok"); n < 2 {
		t.Errorf("backup.run ok rows = %d, want >= 2 (startup run + a tick on the 20ms interval)", n)
	}
}

func TestStartReResolvesIntervalPerTick(t *testing.T) {
	// PATCH 1: the interval is re-resolved on EVERY tick — after the interval
	// changes from 20ms to 1h, the loop stops ticking (a static ticker would
	// keep firing every 20ms forever).
	settings := &fakeSettings{settings: &admcore.AppSettings{BackupInterval: 20 * time.Millisecond}}
	audit := &fakeAudit{}
	j := New(Deps{
		Logger:   discardLogger(),
		Settings: settings,
		Destinations: &fakeDestinations{dests: []*admcore.BackupDestination{
			localDest("dest-local", t.TempDir()),
		}},
		Cipher: &fakeCipher{},
		Audit:  audit,
		Dumper: &fakeDumper{},
	}, WithStartupDelay(0))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.Start(ctx)
	}()
	// Wait for the startup run + at least one tick, then flip the interval.
	for audit.count("destination=dest-local result=ok") < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	settings.setInterval(time.Hour)
	time.Sleep(250 * time.Millisecond) // let any already-armed tick fire
	before := audit.count("destination=dest-local result=ok")
	time.Sleep(300 * time.Millisecond) // a 20ms tick would have fired here
	after := audit.count("destination=dest-local result=ok")
	cancel()
	<-done

	if after != before {
		t.Errorf("runs grew %d → %d after the interval changed to 1h; the loop did NOT re-resolve per tick", before, after)
	}
}

func TestStartNeverOverlapsRuns(t *testing.T) {
	// PATCH 1: a run longer than the interval never overlaps the next — the
	// dumper (80ms per run) reports a peak concurrency of exactly 1 under a
	// 20ms interval.
	slow := &slowDumper{}
	j := New(Deps{
		Logger:   discardLogger(),
		Settings: &fakeSettings{settings: &admcore.AppSettings{BackupInterval: 20 * time.Millisecond}},
		Destinations: &fakeDestinations{dests: []*admcore.BackupDestination{
			localDest("dest-local", t.TempDir()),
		}},
		Cipher: &fakeCipher{},
		Audit:  &fakeAudit{},
		Dumper: slow,
	}, WithStartupDelay(0))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.Start(ctx)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	if peak := slow.peak(); peak != 1 {
		t.Errorf("peak concurrent pg_dump runs = %d, want 1 (runs must never overlap)", peak)
	}
}

// TestIntervalFallback verifies the interval falls back to the seeded default
// when the settings read fails (never panic, never a zero ticker).
func TestIntervalFallback(t *testing.T) {
	j := &Job{settings: &fakeSettings{err: errors.New("db down")}}
	if got := j.interval(context.Background()); got != DefaultInterval {
		t.Errorf("interval = %v, want the %v default on settings failure", got, DefaultInterval)
	}
	j = &Job{settings: &fakeSettings{settings: &admcore.AppSettings{BackupInterval: 0}}}
	if got := j.interval(context.Background()); got != DefaultInterval {
		t.Errorf("interval = %v, want the default for a drifted zero row", got)
	}
}

func TestRedactDSN(t *testing.T) {
	// PATCH 8: the password never lands on the pg_dump command line — it is
	// stripped from the DSN and returned for PGPASSWORD.
	dsn, pw := redactDSN("postgres://gear:secretpw@localhost:5432/gear?sslmode=disable")
	if strings.Contains(dsn, "secretpw") {
		t.Errorf("redacted DSN still contains the password: %q", dsn)
	}
	if !strings.HasPrefix(dsn, "postgres://gear@localhost:5432/gear?sslmode=disable") {
		t.Errorf("redacted DSN = %q, want the host/port/db/sslmode preserved", dsn)
	}
	if pw != "secretpw" {
		t.Errorf("PGPASSWORD value = %q, want the stripped password", pw)
	}
	// No credentials → unchanged, empty password.
	plain, pw := redactDSN("postgres://localhost:5432/gear?sslmode=disable")
	if plain != "postgres://localhost:5432/gear?sslmode=disable" || pw != "" {
		t.Errorf("no-credential DSN = %q pw=%q, want unchanged + empty", plain, pw)
	}
}

func TestPgDumperSurfacesFailure(t *testing.T) {
	// The concrete dumper surfaces a failed pg_dump (unreachable DB or missing
	// binary) as an error — bounded by a short context.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := PgDumper{DSN: "postgres://gear:gear@no-such-host.invalid:5432/gear?sslmode=disable"}
	if err := d.Dump(ctx, io.Discard); err == nil {
		t.Fatal("PgDumper.Dump err = nil, want an error for an unreachable DB")
	}
}
