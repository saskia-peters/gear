package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Backup mechanisms (FR-29/AD-15). Stored verbatim in backup_destinations.
const (
	BackupMechanismS3    = "s3"
	BackupMechanismFTP   = "ftp"
	BackupMechanismSFTP  = "sftp"
	BackupMechanismLocal = "local"
)

// BackupDestination is the domain representation of one backup-destination row
// (FR-29/AD-15). PasswordEncrypted is the AES-256-GCM ciphertext stored at
// rest (NFR-S4) — it is carried only inside the module; the HTTP surface
// renders CredentialConfigured() instead and never serializes the ciphertext.
// Schedule is the optional scheduling hint ("" when unset/NULL).
// ClearCredential is a WRITE-ONLY directive honored only on the update path:
// when true it wipes the stored credential (finding: a blank password alone
// means keep-existing, so clearing must be explicit). It is never part of a
// read.
type BackupDestination struct {
	ID                string
	Name              string
	Mechanism         string
	Endpoint          string
	BucketOrPath      string
	Username          string
	PasswordEncrypted string
	Schedule          string
	ClearCredential   bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CredentialConfigured reports whether a credential is present: write-only
// semantics — the boolean is what a client may learn, never the credential
// itself (NFR-S4).
func (d *BackupDestination) CredentialConfigured() bool {
	return d != nil && d.PasswordEncrypted != ""
}

// BackupDestinationInput is the shared POST/PUT body. Password is a pointer so
// an absent field (nil) is distinct from an explicitly provided one: absent
// (or blank/whitespace-only) KEEPS the existing encrypted credential on update
// (write-only edit); on create it means "no credential" (only allowed for
// local destinations). ClearCredential is the explicit revoke signal on the
// update path only (finding): when true it wipes the stored credential. It is
// mutually exclusive with a provided password.
type BackupDestinationInput struct {
	Name            string  `json:"name"`
	Mechanism       string  `json:"mechanism"`
	Endpoint        string  `json:"endpoint"`
	BucketOrPath    string  `json:"bucket_or_path"`
	Username        string  `json:"username"`
	Password        *string `json:"password,omitempty"`
	ClearCredential bool    `json:"clear_credential"`
	Schedule        string  `json:"schedule"`
}

// BackupTestResult is the inline outcome of the "Verbindung testen" action: ok
// plus the server-authoritative German message. Failures are a 200-style
// result (never a generic 5xx), so the SPA renders the German error inline.
type BackupTestResult struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
}

// BackupTestParams is the live, already-decrypted input for one test
// connection (in-memory only — the plaintext credential never leaves it).
type BackupTestParams struct {
	Mechanism    string
	Endpoint     string
	BucketOrPath string
	Username     string
	Password     string
}

// BackupDestinationsStore is the outbound persistence port over the
// Admin-owned backup_destinations table (AD-11/AD-15). GetBackupDestination
// returns nil when no row matches; DeleteBackupDestination returns
// ErrBackupDestinationNotFound when the id does not exist.
type BackupDestinationsStore interface {
	ListBackupDestinations(ctx context.Context) ([]*BackupDestination, error)
	GetBackupDestination(ctx context.Context, id string) (*BackupDestination, error)
	CreateBackupDestination(ctx context.Context, dest *BackupDestination) (*BackupDestination, error)
	// UpdateBackupDestination writes the destination atomically and returns the
	// resulting row. An empty dest.PasswordEncrypted KEEPS the existing
	// ciphertext in the same statement (COALESCE), so keep-existing is atomic —
	// there is no read-then-write window a concurrent update could race.
	UpdateBackupDestination(ctx context.Context, dest *BackupDestination) (*BackupDestination, error)
	DeleteBackupDestination(ctx context.Context, id string) error
}

// BackupDestinationTester exercises one backup destination against its
// mechanism/endpoint — the same shape the future backup job's storage adapter
// will use (AD-15). It receives the decrypted-in-memory credential, never the
// ciphertext (NFR-S4). The concrete adapter is
// internal/admin/adapters/backup.Tester.
type BackupDestinationTester interface {
	Test(ctx context.Context, params BackupTestParams) error
}

// ListBackupDestinations returns every destination, oldest first (GET_LIST /
// GET_LIST_EMPTY). The HTTP surface maps these to DTOs carrying only
// credential_configured — the ciphertext stays inside the module (NFR-S4).
func (s *Service) ListBackupDestinations(ctx context.Context, actorID string) ([]*BackupDestination, error) {
	if err := s.requireBackupPermission(ctx, actorID); err != nil {
		return nil, err
	}
	return s.currentBackupDestinations(ctx)
}

// CurrentBackupDestinations implements the read-only BackupDestinationsPort
// (AD-15) that the future backup job will consume — it reads destinations
// here, never a copy. No actor, no permission re-check: this is the trusted
// internal read path.
func (s *Service) CurrentBackupDestinations(ctx context.Context) ([]*BackupDestination, error) {
	return s.currentBackupDestinations(ctx)
}

func (s *Service) currentBackupDestinations(ctx context.Context) ([]*BackupDestination, error) {
	dests, err := s.backupStore.ListBackupDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to list backup destinations: %w", err)
	}
	return dests, nil
}

// CreateBackupDestination persists a new destination (CREATE_VALID /
// CREATE_NO_CREDENTIAL / CREATE_INVALID): a present non-blank credential is
// encrypted at rest (NFR-S4); S3/FTP/SFTP destinations REQUIRE a credential
// (username + password), local ones make it optional; the name must be unique
// among destinations. Audited (admin.settings.backup.update).
func (s *Service) CreateBackupDestination(ctx context.Context, actorID string, input BackupDestinationInput) (*BackupDestination, error) {
	if err := s.requireBackupPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateBackupDestination(input); err != nil {
		return nil, err
	}

	dest := &BackupDestination{
		Name:         strings.TrimSpace(input.Name),
		Mechanism:    input.Mechanism,
		Endpoint:     strings.TrimSpace(input.Endpoint),
		BucketOrPath: strings.TrimSpace(input.BucketOrPath),
		Username:     strings.TrimSpace(input.Username),
		Schedule:     strings.TrimSpace(input.Schedule),
	}

	password := ""
	if input.Password != nil {
		password = strings.TrimSpace(*input.Password)
	}
	// CREATE_NO_CREDENTIAL: S3/FTP/SFTP need credentials, local does not.
	if dest.Mechanism != BackupMechanismLocal && (dest.Username == "" || password == "") {
		return nil, &InvalidBackupDestinationsError{Message: MsgBackupRequiresCredential}
	}
	if password != "" {
		enc, err := s.cipher.Encrypt(password)
		if err != nil {
			return nil, fmt.Errorf("admin core: failed to encrypt backup credential: %w", err)
		}
		dest.PasswordEncrypted = enc
	}

	if err := s.ensureUniqueBackupDestinationName(ctx, dest.Name, ""); err != nil {
		return nil, err
	}

	persisted, err := s.backupStore.CreateBackupDestination(ctx, dest)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to persist backup destination: %w", err)
	}

	s.auditBackup(ctx, actorID, AuditOperationBackupSettingsUpdate, "action=create target=backup_destination")
	return persisted, nil
}

// UpdateBackupDestination persists the destination atomically
// (UPDATE_KEEP_CREDENTIAL): a present non-blank credential is encrypted at
// rest and replaces the stored one; an absent one keeps the existing ciphertext
// (write-only edit); clear_credential: true explicitly wipes the stored
// credential (revoke — a blank password alone means keep-existing, so clearing
// must be explicit, finding). The keep/clear decision lives inside the store's
// single UPDATE statement, so it is atomic — a concurrent update cannot
// overwrite the credential with a stale value. The update is audited
// (admin.settings.backup.update).
func (s *Service) UpdateBackupDestination(ctx context.Context, actorID, id string, input BackupDestinationInput) (*BackupDestination, error) {
	if err := s.requireBackupPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateBackupDestination(input); err != nil {
		return nil, err
	}

	dest := &BackupDestination{
		ID:             id,
		Name:           strings.TrimSpace(input.Name),
		Mechanism:      input.Mechanism,
		Endpoint:       strings.TrimSpace(input.Endpoint),
		BucketOrPath:   strings.TrimSpace(input.BucketOrPath),
		Username:       strings.TrimSpace(input.Username),
		Schedule:       strings.TrimSpace(input.Schedule),
		ClearCredential: input.ClearCredential,
	}

	password := ""
	if input.Password != nil {
		// A blank/whitespace-only value is treated as absent (keep-existing).
		password = strings.TrimSpace(*input.Password)
	}
	// Clear and replace are mutually exclusive.
	if input.ClearCredential && password != "" {
		return nil, &InvalidBackupDestinationsError{Message: MsgBackupClearCredentialConflict}
	}
	if password != "" {
		enc, err := s.cipher.Encrypt(password)
		if err != nil {
			return nil, fmt.Errorf("admin core: failed to encrypt backup credential: %w", err)
		}
		dest.PasswordEncrypted = enc
	}
	// When absent/blank, PasswordEncrypted stays "" and ClearCredential is
	// false → the store atomically keeps the existing ciphertext (no
	// read-then-write race). When ClearCredential is true the store wipes it.

	if err := s.ensureUniqueBackupDestinationName(ctx, dest.Name, id); err != nil {
		return nil, err
	}

	persisted, err := s.backupStore.UpdateBackupDestination(ctx, dest)
	if err != nil {
		if errors.Is(err, ErrBackupDestinationNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("admin core: failed to persist backup destination: %w", err)
	}

	s.auditBackup(ctx, actorID, AuditOperationBackupSettingsUpdate, "action=update target=backup_destination id="+id)
	return persisted, nil
}

// DeleteBackupDestination removes one destination (DELETE). The deletion is
// audited (admin.settings.backup.delete).
func (s *Service) DeleteBackupDestination(ctx context.Context, actorID, id string) error {
	if err := s.requireBackupPermission(ctx, actorID); err != nil {
		return err
	}
	if err := s.backupStore.DeleteBackupDestination(ctx, id); err != nil {
		if errors.Is(err, ErrBackupDestinationNotFound) {
			return err
		}
		return fmt.Errorf("admin core: failed to delete backup destination: %w", err)
	}

	s.auditBackup(ctx, actorID, AuditOperationBackupSettingsDelete, "action=delete target=backup_destination id="+id)
	return nil
}

// TestBackupDestination exercises one destination's mechanism/endpoint
// (TEST_OK / TEST_FAIL / TEST_DECRYPT_FAIL). The stored ciphertext is
// decrypted only in memory for the tester (NFR-S4); failures return a
// 200-style result with a GENERIC German error — the detailed
// engine/TLS/auth error is logged structured (NFR-O1) and never surfaced
// inline. Every attempt is audited (admin.settings.backup.test).
func (s *Service) TestBackupDestination(ctx context.Context, actorID, id string) (*BackupTestResult, error) {
	if err := s.requireBackupPermission(ctx, actorID); err != nil {
		return nil, err
	}

	dest, err := s.backupStore.GetBackupDestination(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to read backup destination: %w", err)
	}
	if dest == nil {
		return nil, ErrBackupDestinationNotFound
	}

	password := ""
	if dest.CredentialConfigured() {
		password, err = s.cipher.Decrypt(dest.PasswordEncrypted)
		if err != nil {
			// TEST_DECRYPT_FAIL: stored ciphertext is unreadable (wrong/rotated
			// key or tampering). Clear German error, logged structured, audited.
			s.log().Warn("backup destination test failed: stored credential cannot be decrypted", "id", id, "error", err)
			s.auditBackup(ctx, actorID, AuditOperationBackupSettingsTest, "id="+id+" result=failed decrypt")
			return &BackupTestResult{Ok: false, Message: MsgBackupCredentialInvalid}, nil
		}
	}

	if s.tester == nil {
		return nil, fmt.Errorf("admin core: backup destination tester is not configured")
	}
	if err := s.tester.Test(ctx, BackupTestParams{
		Mechanism:    dest.Mechanism,
		Endpoint:     dest.Endpoint,
		BucketOrPath: dest.BucketOrPath,
		Username:     dest.Username,
		Password:     password,
	}); err != nil {
		// TEST_FAIL: unreachable / auth rejected / write failed. Generic inline
		// German message (no endpoint/TLS/auth detail leaks), full detail
		// logged structured (NFR-O1), audited — never a generic 5xx.
		s.log().Warn("backup destination test failed", "id", id, "name", dest.Name, "mechanism", dest.Mechanism, "error", err)
		s.auditBackup(ctx, actorID, AuditOperationBackupSettingsTest, "id="+id+" result=failed")
		return &BackupTestResult{Ok: false, Message: MsgBackupTestFailed}, nil
	}

	s.log().Info("backup destination test ok", "id", id, "name", dest.Name, "mechanism", dest.Mechanism)
	s.auditBackup(ctx, actorID, AuditOperationBackupSettingsTest, "id="+id+" result=ok")
	return &BackupTestResult{Ok: true, Message: MsgBackupTestOK}, nil
}

// requireBackupPermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds admin.settings.backup. The route gateway
// already enforces it; the core re-checks so no future direct caller can skip
// it. An empty actor ID never passes.
func (s *Service) requireBackupPermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("admin core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == BackupSettingsPermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditBackup writes the audit row best-effort (NFR-O1): a failed audit write
// is logged, never rolled back into the triggering operation.
func (s *Service) auditBackup(ctx context.Context, actorID, operation, detail string) {
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("admin core: backup settings audit write failed", "operation", operation, "error", err)
	}
}

// validateBackupDestination enforces the create/update invariants
// (CREATE_INVALID): non-empty name, valid mechanism, required endpoint for
// S3/FTP/SFTP, required bucket-or-path, bounded optional fields, no CR/LF/NUL
// (wire-injection guard: username and password are interpolated into raw FTP/
// SFTP protocol commands by the tester, so they must never carry a line
// break), and bounded provided credentials. A blank/whitespace-only provided
// password is NOT an error here — the caller treats it as absent (keep-existing
// on update; local destinations may be credential-less).
func validateBackupDestination(input BackupDestinationInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return &InvalidBackupDestinationsError{Message: "Bitte gib einen Namen für das Backup-Ziel an."}
	}
	if len(name) > 255 {
		return &InvalidBackupDestinationsError{Message: "Der Name ist zu lang."}
	}
	if containsWireUnsafe(name) {
		return &InvalidBackupDestinationsError{Message: "Der Name enthält ungültige Zeichen."}
	}
	switch input.Mechanism {
	case BackupMechanismS3, BackupMechanismFTP, BackupMechanismSFTP, BackupMechanismLocal:
	default:
		return &InvalidBackupDestinationsError{Message: "Bitte wähle einen gültigen Mechanismus (S3, FTP, SFTP oder Lokal)."}
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if len(endpoint) > 2048 {
		return &InvalidBackupDestinationsError{Message: "Der Endpunkt ist zu lang."}
	}
	if containsWireUnsafe(endpoint) {
		return &InvalidBackupDestinationsError{Message: "Der Endpunkt enthält ungültige Zeichen."}
	}
	if input.Mechanism != BackupMechanismLocal && endpoint == "" {
		return &InvalidBackupDestinationsError{Message: "Bitte gib einen Endpunkt für das Backup-Ziel an."}
	}
	bucket := strings.TrimSpace(input.BucketOrPath)
	if bucket == "" {
		return &InvalidBackupDestinationsError{Message: "Bitte gib einen Bucket oder Pfad an."}
	}
	if len(bucket) > 2048 {
		return &InvalidBackupDestinationsError{Message: "Der Bucket oder Pfad ist zu lang."}
	}
	if containsWireUnsafe(bucket) {
		return &InvalidBackupDestinationsError{Message: "Der Bucket oder Pfad enthält ungültige Zeichen."}
	}
	username := strings.TrimSpace(input.Username)
	if len(username) > 255 {
		return &InvalidBackupDestinationsError{Message: "Der Benutzername ist zu lang."}
	}
	if containsWireUnsafe(username) {
		return &InvalidBackupDestinationsError{Message: "Der Benutzername enthält ungültige Zeichen."}
	}
	if input.Password != nil {
		password := strings.TrimSpace(*input.Password)
		if len(password) > 1024 {
			return &InvalidBackupDestinationsError{Message: "Die Zugangsberechtigung ist zu lang."}
		}
		if containsWireUnsafe(password) {
			return &InvalidBackupDestinationsError{Message: "Die Zugangsberechtigung enthält ungültige Zeichen."}
		}
	}
	schedule := strings.TrimSpace(input.Schedule)
	if len(schedule) > 255 {
		return &InvalidBackupDestinationsError{Message: "Der Zeitplan ist zu lang."}
	}
	if containsWireUnsafe(schedule) {
		return &InvalidBackupDestinationsError{Message: "Der Zeitplan enthält ungültige Zeichen."}
	}
	return nil
}

// containsWireUnsafe reports whether s carries a byte that must never reach a
// wire-level protocol command: CR/LF would terminate an FTP/SSH line, NUL is
// rejected by most protocol parsers (finding: credential command injection).
func containsWireUnsafe(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}

// ensureUniqueBackupDestinationName rejects a name already held by another
// destination (case-insensitive, finding): two destinations must not share a
// name. exceptID excludes the destination being updated from the comparison.
func (s *Service) ensureUniqueBackupDestinationName(ctx context.Context, name, exceptID string) error {
	dests, err := s.backupStore.ListBackupDestinations(ctx)
	if err != nil {
		return fmt.Errorf("admin core: failed to check backup destination name uniqueness: %w", err)
	}
	for _, d := range dests {
		if d.ID == exceptID {
			continue
		}
		if strings.EqualFold(d.Name, name) {
			return &InvalidBackupDestinationsError{Message: MsgBackupDestinationNameTaken}
		}
	}
	return nil
}