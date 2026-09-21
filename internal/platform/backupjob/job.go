// Package backupjob hosts the in-process backup job (Story 7.7, NFR-R3): a
// goroutine started at the cmd/server composition root that dumps the database
// via `pg_dump -Fc` and ships the artifact to every configured backup
// destination whose mechanism supports real transfer in V1 (local + s3;
// ftp/sftp stay handshake-only and are logged as "configured but not shippable
// in V1" — never silent, never a failure). Per the user's stdlib-partial depth
// decision it reuses the Story 3.2 tester's SigV4 PUT and the local round-trip
// via the admin backup adapter's store helpers (internal/admin/adapters/backup).
//
// The job runs once a few seconds after boot (so migrations/settings are warm)
// and then on a RE-ARMED TIMER whose interval is re-resolved from the
// `backup_interval` app setting on every tick (seeded 86400s = daily) — an
// admin change takes effect without a restart, a transient settings-read
// failure only affects that tick, and a run longer than the interval never
// overlaps the next one (the next timer is armed only after the run finishes;
// an in-flight guard is the defensive backstop). The dump is STREAMED to each
// destination from a single temp file — the job holds one payload hash + one
// file handle, never a full copy of the dump in RAM.
//
// Every per-run and per-destination outcome is logged structured (NFR-O1) and
// audited (`backup.run`, NFR-O1/NFR-O2) WITHOUT an actor (the job has no user
// session): failures (dump failed, destination failed, decrypt failed) are
// audited severity "high", ok/skipped "normal". A failing destination never
// aborts the run. The job never panics: New rejects nil dependency ports at the
// composition root, nil destination rows are skipped, and every run runs under
// a recover so a single-run panic cannot kill the process.
//
// All dependencies are small ports so tests drive the full matrix with fakes
// (fakeDestinations, fakeDumper, fakeCipher): RUN_OK, PARTIAL_FAIL,
// FTP_SFTP_DEST, NO_DEST, PG_DUMP_FAIL, DECRYPT_FAIL.
package backupjob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	admbck "github.com/saskia-peters/gear/internal/admin/adapters/backup"
	admcore "github.com/saskia-peters/gear/internal/admin/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// DefaultInterval is the fallback ticker interval when the backup_interval app
// setting cannot be resolved or drifts to zero — the seeded 86400s = daily.
const DefaultInterval = 24 * time.Hour

// defaultStartupDelay is the wait before the first (startup) run so
// migrations/settings are warm before the job reads them (spec scheduling).
const defaultStartupDelay = 5 * time.Second

// runSeq is the monotonic per-process counter that disambiguates artifact
// names created in the same UTC second (gear-<sec>-<seq>.dump).
var runSeq atomic.Uint64

// SettingsPort resolves the live typed app settings (backup_interval,
// backup_protocol_timeout).
type SettingsPort interface {
	CurrentAppSettings(ctx context.Context) (*admcore.AppSettings, error)
}

// DestinationsPort resolves the live backup destinations (AD-15).
type DestinationsPort interface {
	CurrentBackupDestinations(ctx context.Context) ([]*admcore.BackupDestination, error)
}

// CipherPort decrypts a stored destination credential in memory only (NFR-S4).
type CipherPort interface {
	Decrypt(encoded string) (string, error)
}

// AuditPort writes the job's audit rows (NFR-O1/NFR-O2). The job has no user
// session, so it always uses the anonymous path; detail carries the run's
// destination + outcome, severity distinguishes failures (high) from
// ok/skipped (normal).
type AuditPort interface {
	InsertAuditEventAnonymous(ctx context.Context, operation, detail, severity string) error
}

// Dumper produces the custom-format database dump (the pg_dump invocation).
type Dumper interface {
	Dump(ctx context.Context, w io.Writer) error
}

// PgDumper is the concrete Dumper that shells out to `pg_dump -Fc` against the
// configured DSN. The password is stripped from the command line and passed via
// PGPASSWORD instead (NFR-S4: never visible in /proc/<pid>/cmdline or ps). It
// streams the custom-format dump to w and surfaces a non-zero pg_dump exit
// (with its stderr) as an error.
type PgDumper struct{ DSN string }

func (d PgDumper) Dump(ctx context.Context, w io.Writer) error {
	dsn, password := redactDSN(d.DSN)
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "--no-owner", "--no-acl", dsn)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backupjob: pg_dump failed: %w: %s", err, stderr.String())
	}
	return nil
}

// redactDSN strips the password from a postgres:// DSN so it never lands on the
// pg_dump command line, returning the (still fully functional) DSN plus the
// password for PGPASSWORD. A non-URL DSN or one without credentials is returned
// unchanged.
func redactDSN(dsn string) (string, string) {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn, ""
	}
	username := u.User.Username()
	if username == "" {
		return dsn, ""
	}
	password, _ := u.User.Password()
	u.User = url.User(username)
	return u.String(), password
}

// Deps are the job's dependencies (all fake-friendly for tests).
type Deps struct {
	Logger       *slog.Logger
	Settings     SettingsPort
	Destinations DestinationsPort
	Cipher       CipherPort
	Audit        AuditPort
	Dumper       Dumper
}

// Job runs the backup loop.
type Job struct {
	logger       *slog.Logger
	settings     SettingsPort
	destinations DestinationsPort
	cipher       CipherPort
	auditWriter  AuditPort
	dumper       Dumper
	startupDelay time.Duration

	// mu guards `running` — the in-flight guard (defensive: the Start loop is
	// sequential, so a tick can never observe a concurrent run today, but the
	// guard keeps that invariant explicit).
	mu      sync.Mutex
	running bool
}

// Option configures a Job (test seams).
type Option func(*Job)

// WithStartupDelay overrides the delay before the first run.
func WithStartupDelay(d time.Duration) Option { return func(j *Job) { j.startupDelay = d } }

// New constructs the backup job. Logger and Audit may be nil (logger falls back
// to slog.Default(), audit is best-effort); Settings, Destinations, Cipher and
// Dumper are REQUIRED — a nil port is a composition-root wiring defect and
// panics loudly here instead of crashing the server mid-run.
func New(deps Deps, opts ...Option) *Job {
	if deps.Settings == nil {
		panic("backupjob: New: Settings port is required (composition-root wiring defect)")
	}
	if deps.Destinations == nil {
		panic("backupjob: New: Destinations port is required (composition-root wiring defect)")
	}
	if deps.Cipher == nil {
		panic("backupjob: New: Cipher port is required (composition-root wiring defect)")
	}
	if deps.Dumper == nil {
		panic("backupjob: New: Dumper is required (composition-root wiring defect)")
	}
	j := &Job{
		logger:       deps.Logger,
		settings:     deps.Settings,
		destinations: deps.Destinations,
		cipher:       deps.Cipher,
		auditWriter:  deps.Audit,
		dumper:       deps.Dumper,
		startupDelay: defaultStartupDelay,
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// log returns the configured logger or slog.Default().
func (j *Job) log() *slog.Logger {
	if j.logger != nil {
		return j.logger
	}
	return slog.Default()
}

// setRunning / isRunning are the in-flight guard.
func (j *Job) setRunning(v bool) {
	j.mu.Lock()
	j.running = v
	j.mu.Unlock()
}

func (j *Job) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// Start runs the job until ctx is canceled: one run shortly after boot, then
// on a re-armed timer whose interval is re-resolved from backup_interval on
// every tick. It never returns an error (failures are logged + audited inside
// RunBackup).
func (j *Job) Start(ctx context.Context) {
	// Wait the startup delay so migrations/settings are warm before the first
	// run resolves the interval and lists destinations.
	timer := time.NewTimer(j.startupDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	j.runProtected(ctx)

	// Re-armed timer (NOT a ticker): the interval is re-resolved every tick so
	// an admin change takes effect immediately and a transient settings-read
	// failure only affects that tick. The next timer is armed only after the
	// previous run finishes, so a run longer than the interval never overlaps
	// the next tick; the in-flight guard is the defensive backstop.
	for {
		timer.Reset(j.interval(ctx))
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if j.isRunning() {
			j.log().Warn("backup job: skipping tick, a run is still in flight (never overlapping)")
			continue
		}
		j.runProtected(ctx)
	}
}

// runProtected runs one backup run with a panic guard so a panic in a single
// run never kills the process — the loop continues on the next tick.
func (j *Job) runProtected(ctx context.Context) {
	j.setRunning(true)
	defer j.setRunning(false)
	defer func() {
		if r := recover(); r != nil {
			j.log().Error("backup job panicked; continuing next tick", "panic", r)
		}
	}()
	j.RunBackup(ctx)
}

// interval resolves the backup_interval app setting, falling back to the
// seeded default when the read fails or the row is absent/drifted (never
// panic, never silent).
func (j *Job) interval(ctx context.Context) time.Duration {
	settings, err := j.settings.CurrentAppSettings(ctx)
	if err != nil {
		j.log().Warn("backup job: failed to resolve backup_interval; using the default", "error", err, "interval", DefaultInterval)
		return DefaultInterval
	}
	if settings.BackupInterval <= 0 {
		j.log().Warn("backup job: backup_interval is unset or drifted; using the default", "interval", DefaultInterval)
		return DefaultInterval
	}
	return settings.BackupInterval
}

// protocolTimeout resolves the backup_protocol_timeout app setting ONCE per run
// for the s3 upload (zero → the store falls back to its own constant).
func (j *Job) protocolTimeout(ctx context.Context) time.Duration {
	settings, err := j.settings.CurrentAppSettings(ctx)
	if err != nil {
		j.log().Warn("backup job: failed to resolve backup_protocol_timeout; using the store default", "error", err)
		return 0
	}
	return settings.BackupProtocolTimeout
}

// RunBackup performs one backup run (spec RUN_OK / NO_DEST / PG_DUMP_FAIL /
// PARTIAL_FAIL / FTP_SFTP_DEST / DECRYPT_FAIL): dump the DB to a temp file,
// then stream it to every destination whose mechanism supports real transfer.
// A zero-shippable run (no destinations, or only ftp/sftp) logs a warning —
// never silent — and does NOT dump. A failing destination never aborts the run.
func (j *Job) RunBackup(ctx context.Context) {
	dests, err := j.destinations.CurrentBackupDestinations(ctx)
	if err != nil {
		j.log().Error("backup run failed: listing destinations", "error", err)
		j.audit(ctx, "result=failed stage=list_destinations", usercore.AuditSeverityHigh)
		return
	}
	if len(dests) == 0 {
		// NO_DEST: zero destinations configured — warn, never silent.
		j.log().Warn("backup run: no backup destinations configured; nothing dumped or shipped (never silent)")
		j.audit(ctx, "result=skipped reason=no_destinations", usercore.AuditSeverityNormal)
		return
	}
	if !hasShippable(dests) {
		// Only ftp/sftp destinations (handshake-only in V1): the run is NOT a
		// failure — the destinations are reachability-tested by the tester, real
		// transfer ships in a later story. Warn, never silent.
		j.log().Warn("backup run: no shippable destinations (local/s3) configured; ftp/sftp are handshake-only in V1 — nothing shipped", "destinations", len(dests))
		j.audit(ctx, "result=skipped reason=no_shippable_destinations", usercore.AuditSeverityNormal)
		return
	}

	dump, err := j.dumpToTemp(ctx)
	if err != nil {
		// PG_DUMP_FAIL: the whole run fails — no destination is attempted.
		j.log().Error("backup run failed: database dump", "error", err)
		j.audit(ctx, "result=failed stage=dump", usercore.AuditSeverityHigh)
		return
	}
	defer func() { _ = os.Remove(dump) }()

	protocolTimeout := j.protocolTimeout(ctx)

	// ONE artifact name per run: gear-<UTC-seconds>-<runSeq>.dump. Two runs in
	// the same second never overwrite, and every destination of this run shares
	// the key (run-level correlation across destinations).
	runKey := "gear-" + time.Now().UTC().Format("20060102150405") + "-" + strconv.FormatUint(runSeq.Add(1), 10) + ".dump"

	// Precompute the payload hash ONCE by streaming the temp file — the job
	// holds one hash + one file handle per destination, never a full copy of
	// the dump in RAM (PATCH 3).
	hash, err := fileSHA256(dump)
	if err != nil {
		j.log().Error("backup run failed: reading dump", "error", err)
		j.audit(ctx, "result=failed stage=read_dump", usercore.AuditSeverityHigh)
		return
	}

	for _, d := range dests {
		if d == nil {
			// A nil destination row must never panic the job.
			j.log().Warn("backup destination skipped: nil destination row")
			continue
		}
		f, err := os.Open(dump)
		if err != nil {
			j.log().Error("backup destination failed: cannot open dump", "destination", d.ID, "name", d.Name, "error", err)
			j.audit(ctx, fmt.Sprintf("destination=%s result=failed", d.ID), usercore.AuditSeverityHigh)
			continue
		}
		j.shipOne(ctx, d, f, hash, runKey, protocolTimeout)
		_ = f.Close()
	}
}

// hasShippable reports whether at least one destination supports real transfer
// in V1 (local/s3).
func hasShippable(dests []*admcore.BackupDestination) bool {
	for _, d := range dests {
		if d == nil {
			continue
		}
		switch d.Mechanism {
		case admcore.BackupMechanismLocal, admcore.BackupMechanismS3:
			return true
		}
	}
	return false
}

// dumpToTemp runs the dumper into a temp file and returns its path.
func (j *Job) dumpToTemp(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "gear-backup-*.dump")
	if err != nil {
		return "", fmt.Errorf("backupjob: cannot create temp dump file: %w", err)
	}
	name := f.Name()
	if err := j.dumper.Dump(ctx, f); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("backupjob: cannot close temp dump file: %w", err)
	}
	return name, nil
}

// fileSHA256 computes the SHA-256 of a file by streaming (bounded memory).
func fileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return admbck.SHA256(f)
}

// shipOne streams the dump to one destination, logging + auditing its outcome.
// FTP/SFTP are logged as "configured but not shippable in V1 (handshake-only)"
// and are NOT failures. Only s3 decrypts a stored credential (a local target
// ships without any credential — a leftover/unreadable ciphertext after an
// encryption-key rotation must not silently stop local backups). A
// per-destination failure (decrypt, empty local path, write, non-2xx) is
// logged structured + audited (high) and the run continues.
func (j *Job) shipOne(ctx context.Context, d *admcore.BackupDestination, body io.Reader, bodyHash []byte, key string, protocolTimeout time.Duration) {
	switch d.Mechanism {
	case admcore.BackupMechanismFTP, admcore.BackupMechanismSFTP:
		// FTP_SFTP_DEST: handshake-only in V1 (user decision) — NOT a failure,
		// never silent.
		j.log().Info("backup destination configured but not shippable in V1 (handshake-only)", "destination", d.ID, "name", d.Name, "mechanism", d.Mechanism)
		j.audit(ctx, fmt.Sprintf("destination=%s result=skipped reason=not_shippable_in_v1", d.ID), usercore.AuditSeverityNormal)
		return
	case admcore.BackupMechanismLocal, admcore.BackupMechanismS3:
	default:
		j.log().Warn("backup destination skipped: unsupported mechanism", "destination", d.ID, "name", d.Name, "mechanism", d.Mechanism)
		j.audit(ctx, fmt.Sprintf("destination=%s result=skipped reason=unsupported_mechanism", d.ID), usercore.AuditSeverityNormal)
		return
	}

	password := ""
	if d.Mechanism == admcore.BackupMechanismS3 {
		if d.CredentialConfigured() {
			var err error
			password, err = j.cipher.Decrypt(d.PasswordEncrypted)
			if err != nil {
				// DECRYPT_FAIL: the stored ciphertext is unreadable — dest skipped,
				// the run continues.
				j.log().Error("backup destination skipped: stored credential cannot be decrypted", "destination", d.ID, "name", d.Name, "error", err)
				j.audit(ctx, fmt.Sprintf("destination=%s result=failed reason=decrypt", d.ID), usercore.AuditSeverityHigh)
				return
			}
		}
	}

	var err error
	switch d.Mechanism {
	case admcore.BackupMechanismLocal:
		if d.BucketOrPath == "" {
			// An empty local path would resolve to a relative artifact in the
			// container CWD (likely unwritable) — fail loud + audit instead.
			j.log().Error("backup destination failed: no local path configured", "destination", d.ID, "name", d.Name)
			j.audit(ctx, fmt.Sprintf("destination=%s result=failed reason=no_path", d.ID), usercore.AuditSeverityHigh)
			return
		}
		err = admbck.StoreLocal(ctx, filepath.Join(d.BucketOrPath, key), body)
	case admcore.BackupMechanismS3:
		err = admbck.StoreS3(ctx, admcore.BackupTestParams{
			Mechanism:    d.Mechanism,
			Endpoint:     d.Endpoint,
			BucketOrPath: d.BucketOrPath,
			Username:     d.Username,
			Password:     password,
			Timeout:      protocolTimeout,
		}, key, body, bodyHash)
	}
	if err != nil {
		// PARTIAL_FAIL: this destination failed; the run continues to the next.
		j.log().Error("backup destination failed", "destination", d.ID, "name", d.Name, "mechanism", d.Mechanism, "error", err)
		j.audit(ctx, fmt.Sprintf("destination=%s result=failed", d.ID), usercore.AuditSeverityHigh)
		return
	}

	j.log().Info("backup shipped", "destination", d.ID, "name", d.Name, "mechanism", d.Mechanism, "artifact", key)
	j.audit(ctx, fmt.Sprintf("destination=%s result=ok", d.ID), usercore.AuditSeverityNormal)
}

// audit writes a backup.run audit row best-effort (NFR-O1): a failed audit
// write is logged, never surfaced as a job failure. severity distinguishes
// failed outcomes (high) from ok/skipped (normal) so NFR-O2 can filter runs
// without parsing the detail.
func (j *Job) audit(ctx context.Context, detail, severity string) {
	if j.auditWriter == nil {
		return
	}
	if err := j.auditWriter.InsertAuditEventAnonymous(ctx, admcore.AuditOperationBackupRun, detail, severity); err != nil {
		j.log().Warn("backup job audit write failed", "operation", admcore.AuditOperationBackupRun, "error", err)
	}
}
