package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeBackupStore is an in-memory BackupDestinationsStore emulating the
// repository's atomic COALESCE keep-existing semantics.
type fakeBackupStore struct {
	dests      []*BackupDestination
	listErr    error
	getErr     error
	createErr  error
	updateErr  error
	deleteErr  error
	created    []*BackupDestination
	updated    []*BackupDestination
	deleted    []string
}

func (f *fakeBackupStore) ListBackupDestinations(context.Context) ([]*BackupDestination, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.dests, nil
}

func (f *fakeBackupStore) GetBackupDestination(_ context.Context, id string) (*BackupDestination, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, d := range f.dests {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, nil
}

func (f *fakeBackupStore) CreateBackupDestination(_ context.Context, dest *BackupDestination) (*BackupDestination, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	persisted := *dest
	persisted.ID = "id-" + dest.Name
	f.dests = append(f.dests, &persisted)
	f.created = append(f.created, &persisted)
	return &persisted, nil
}

// UpdateBackupDestination emulates the store's atomic credential semantics: an
// empty incoming credential KEEPS the existing ciphertext (COALESCE), a
// non-empty one replaces it, and dest.ClearCredential WIPES it.
func (f *fakeBackupStore) UpdateBackupDestination(_ context.Context, dest *BackupDestination) (*BackupDestination, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	for i, d := range f.dests {
		if d.ID == dest.ID {
			persisted := *dest
			switch {
			case dest.ClearCredential:
				persisted.PasswordEncrypted = ""
			case dest.PasswordEncrypted == "":
				persisted.PasswordEncrypted = d.PasswordEncrypted
			}
			f.dests[i] = &persisted
			f.updated = append(f.updated, &persisted)
			return &persisted, nil
		}
	}
	return nil, ErrBackupDestinationNotFound
}

func (f *fakeBackupStore) DeleteBackupDestination(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	for i, d := range f.dests {
		if d.ID == id {
			f.dests = append(f.dests[:i], f.dests[i+1:]...)
			f.deleted = append(f.deleted, id)
			return nil
		}
	}
	return ErrBackupDestinationNotFound
}

// fakeTester records BackupTestParams and can fail.
type fakeTester struct {
	err    error
	params []BackupTestParams
}

func (f *fakeTester) Test(_ context.Context, params BackupTestParams) error {
	if f.err != nil {
		return f.err
	}
	f.params = append(f.params, params)
	return nil
}

// newBackupService wires the fakes around a Service with the backup store and
// tester populated. perms defaults to the backup permission holder.
func newBackupService(perms ...string) (*Service, *fakeBackupStore, *fakeAudit, *fakeTester) {
	store := &fakeBackupStore{}
	audit := &fakeAudit{}
	tester := &fakeTester{}
	if len(perms) == 0 {
		perms = []string{BackupSettingsPermission}
	}
	svc := NewService(nil, store, &fakeCipher{}, &fakePerms{perms: perms}, audit, nil, tester, nil)
	return svc, store, audit, tester
}

func destFixture(id, name, mechanism string) *BackupDestination {
	return &BackupDestination{
		ID: id, Name: name, Mechanism: mechanism,
		Endpoint: "127.0.0.1", BucketOrPath: "/srv/backup",
		Username: "u", PasswordEncrypted: "enc:pw",
	}
}

func TestListBackupDestinationsEmpty(t *testing.T) {
	// GET_LIST_EMPTY: no destinations → empty list, no error.
	svc, _, _, _ := newBackupService()
	got, err := svc.ListBackupDestinations(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListBackupDestinations err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("dests = %d, want 0", len(got))
	}
}

func TestListBackupDestinations(t *testing.T) {
	// GET_LIST: two destinations returned, ciphertext carried internally only.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal), destFixture("id-b", "S3", BackupMechanismS3)}
	got, err := svc.ListBackupDestinations(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListBackupDestinations err = %v", err)
	}
	if len(got) != 2 || got[0].ID != "id-a" || got[1].ID != "id-b" {
		t.Fatalf("dests = %+v, want both in order", got)
	}
	if !got[0].CredentialConfigured() {
		t.Error("credential_configured = false, want true")
	}
}

func TestListBackupDestinationsForbidden(t *testing.T) {
	// TEST_FORBIDDEN-style: caller without admin.settings.backup → ErrForbidden.
	svc, store, _, _ := newBackupService("dashboard.view")
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	if _, err := svc.ListBackupDestinations(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreateBackupDestinationValid(t *testing.T) {
	// CREATE_VALID: local with path + credential → persisted, credential
	// encrypted at rest, audited.
	svc, store, audit, _ := newBackupService()
	cred := "geheim123"
	input := BackupDestinationInput{
		Name: "  Lokal ", Mechanism: BackupMechanismLocal,
		Endpoint: "", BucketOrPath: "/srv/backup", Username: "u",
		Password: &cred, Schedule: "0 2 * * *",
	}
	got, err := svc.CreateBackupDestination(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateBackupDestination err = %v", err)
	}
	if got.Name != "Lokal" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.PasswordEncrypted != "enc:"+cred {
		t.Errorf("password_encrypted = %q, want encrypted ciphertext", got.PasswordEncrypted)
	}
	if !got.CredentialConfigured() {
		t.Error("credential_configured = false, want true")
	}
	if got.Schedule != "0 2 * * *" {
		t.Errorf("schedule = %q", got.Schedule)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationBackupSettingsUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestCreateBackupDestinationLocalWithoutCredential(t *testing.T) {
	// Local destinations may be credential-less.
	svc, store, _, _ := newBackupService()
	input := BackupDestinationInput{Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/srv/backup"}
	got, err := svc.CreateBackupDestination(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateBackupDestination err = %v", err)
	}
	if got.CredentialConfigured() {
		t.Error("credential_configured = true, want false (no credential)")
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
}

func TestCreateBackupDestinationNoCredential(t *testing.T) {
	// CREATE_NO_CREDENTIAL: s3 without credential → 400-class German error, not
	// persisted.
	svc, store, _, _ := newBackupService()
	for _, mechanism := range []string{BackupMechanismS3, BackupMechanismFTP, BackupMechanismSFTP} {
		t.Run(mechanism, func(t *testing.T) {
			input := BackupDestinationInput{Name: "S3", Mechanism: mechanism, Endpoint: "s3.example.com", BucketOrPath: "bucket"}
			_, err := svc.CreateBackupDestination(context.Background(), actorID, input)
			var inv *InvalidBackupDestinationsError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidBackupDestinationsError", err)
			}
			if !strings.Contains(inv.Message, MsgBackupRequiresCredential) {
				t.Errorf("message = %q, want %q", inv.Message, MsgBackupRequiresCredential)
			}
			if len(store.created) != 0 {
				t.Error("destination must not be persisted on invalid input")
			}
		})
	}
}

func TestCreateBackupDestinationInvalid(t *testing.T) {
	// CREATE_INVALID: bad mechanism / empty name → 400-class German error.
	cases := []struct {
		name    string
		input   BackupDestinationInput
		wantMsg string
	}{
		{"empty name", BackupDestinationInput{Name: "  ", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}, "Namen"},
		{"bad mechanism", BackupDestinationInput{Name: "x", Mechanism: "nfs", BucketOrPath: "/x"}, "Mechanismus"},
		{"missing bucket/path", BackupDestinationInput{Name: "x", Mechanism: BackupMechanismLocal}, "Bucket oder Pfad"},
		{"missing endpoint for s3", BackupDestinationInput{Name: "x", Mechanism: BackupMechanismS3, BucketOrPath: "b"}, "Endpunkt"},
		{"name newline", BackupDestinationInput{Name: "x\r\nY", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}, "ungültige Zeichen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _ := newBackupService()
			_, err := svc.CreateBackupDestination(context.Background(), actorID, tc.input)
			var inv *InvalidBackupDestinationsError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidBackupDestinationsError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrBackupDestinationsInvalid) {
				t.Errorf("err = %v, want unwraps to ErrBackupDestinationsInvalid", err)
			}
		})
	}
}

func TestCreateBackupDestinationForbidden(t *testing.T) {
	svc, _, _, _ := newBackupService("dashboard.view")
	input := BackupDestinationInput{Name: "x", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}
	if _, err := svc.CreateBackupDestination(context.Background(), actorID, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateBackupDestinationKeepCredential(t *testing.T) {
	// UPDATE_KEEP_CREDENTIAL: edit without credential field → existing
	// encrypted credential kept (atomic COALESCE in the store), other fields
	// updated, audited.
	svc, store, audit, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	existing := store.dests[0].PasswordEncrypted

	input := BackupDestinationInput{Name: "Lokal v2", Mechanism: BackupMechanismLocal, BucketOrPath: "/srv/backup2"}
	got, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateBackupDestination err = %v", err)
	}
	if got.PasswordEncrypted != existing {
		t.Errorf("password_encrypted = %q, want kept %q", got.PasswordEncrypted, existing)
	}
	if !got.CredentialConfigured() {
		t.Error("credential_configured = false, want true (existing credential kept)")
	}
	if got.Name != "Lokal v2" || got.BucketOrPath != "/srv/backup2" {
		t.Errorf("updated fields = %+v", got)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationBackupSettingsUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateBackupDestinationReplacesCredential(t *testing.T) {
	// A present credential is encrypted at rest and replaces the stored one.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	cred := "neu-geheim"
	input := BackupDestinationInput{
		Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/srv/backup",
		Password: &cred,
	}
	got, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateBackupDestination err = %v", err)
	}
	if got.PasswordEncrypted != "enc:"+cred {
		t.Errorf("password_encrypted = %q, want replaced ciphertext", got.PasswordEncrypted)
	}
}

func TestUpdateBackupDestinationNotFound(t *testing.T) {
	svc, _, _, _ := newBackupService()
	input := BackupDestinationInput{Name: "x", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}
	if _, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-missing", input); !errors.Is(err, ErrBackupDestinationNotFound) {
		t.Fatalf("err = %v, want ErrBackupDestinationNotFound", err)
	}
}

func TestDeleteBackupDestination(t *testing.T) {
	// DELETE: destination removed, audited (admin.settings.backup.delete).
	svc, store, audit, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	if err := svc.DeleteBackupDestination(context.Background(), actorID, "id-a"); err != nil {
		t.Fatalf("DeleteBackupDestination err = %v", err)
	}
	if len(store.dests) != 0 {
		t.Errorf("dests = %d, want 0 after delete", len(store.dests))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationBackupSettingsDelete {
		t.Fatalf("audit events = %+v, want one delete audit", audit.events)
	}
}

func TestDeleteBackupDestinationNotFound(t *testing.T) {
	svc, _, _, _ := newBackupService()
	if err := svc.DeleteBackupDestination(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrBackupDestinationNotFound) {
		t.Fatalf("err = %v, want ErrBackupDestinationNotFound", err)
	}
}

func TestTestBackupDestinationOK(t *testing.T) {
	// TEST_OK: credential decrypted in memory, tester exercised with the
	// plaintext, inline success, audited result=ok.
	svc, store, audit, tester := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "S3", BackupMechanismS3)}
	res, err := svc.TestBackupDestination(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("TestBackupDestination err = %v", err)
	}
	if !res.Ok || res.Message != MsgBackupTestOK {
		t.Errorf("result = %+v, want ok + %q", res, MsgBackupTestOK)
	}
	if len(tester.params) != 1 {
		t.Fatalf("tester calls = %d, want 1", len(tester.params))
	}
	if tester.params[0].Password != "pw" {
		t.Errorf("plaintext credential = %q, want decrypted in-memory value", tester.params[0].Password)
	}
	if tester.params[0].Mechanism != BackupMechanismS3 || tester.params[0].Endpoint != "127.0.0.1" {
		t.Errorf("tester params = %+v", tester.params[0])
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "result=ok") {
		t.Errorf("audit = %+v, want result=ok", audit.events)
	}
}

func TestTestBackupDestinationLocalNoCredential(t *testing.T) {
	// A local destination without a credential still tests (empty plaintext
	// passed to the tester).
	svc, store, _, tester := newBackupService()
	store.dests = []*BackupDestination{{ID: "id-a", Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/srv/backup"}}
	res, err := svc.TestBackupDestination(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("TestBackupDestination err = %v", err)
	}
	if !res.Ok {
		t.Errorf("result = %+v, want ok", res)
	}
	if len(tester.params) != 1 || tester.params[0].Password != "" {
		t.Errorf("tester params = %+v, want empty credential", tester.params)
	}
}

func TestTestBackupDestinationFail(t *testing.T) {
	// TEST_FAIL: unreachable / bad auth → GENERIC inline German error (the
	// detailed engine error is logged structured, never surfaced), audited
	// result=failed.
	svc, store, audit, tester := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "S3", BackupMechanismS3)}
	tester.err = errors.New("connection refused")
	res, err := svc.TestBackupDestination(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("TestBackupDestination err = %v", err)
	}
	if res.Ok {
		t.Error("result.Ok = true, want false")
	}
	if res.Message != MsgBackupTestFailed {
		t.Errorf("message = %q, want generic %q", res.Message, MsgBackupTestFailed)
	}
	if strings.Contains(res.Message, "connection refused") || strings.Contains(res.Message, "127.0.0.1") {
		t.Errorf("message leaks engine detail: %q", res.Message)
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "result=failed") {
		t.Errorf("audit = %+v, want result=failed", audit.events)
	}
}

func TestTestBackupDestinationDecryptFail(t *testing.T) {
	// TEST_DECRYPT_FAIL: stored ciphertext unreadable → German error, logged,
	// audited; the tester is never invoked.
	svc, store, audit, tester := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "S3", BackupMechanismS3)}
	store.dests[0].PasswordEncrypted = "kaputt"
	svc.cipher = &fakeCipher{decryptErr: ErrInvalidCiphertext}
	res, err := svc.TestBackupDestination(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("TestBackupDestination err = %v", err)
	}
	if res.Ok || res.Message != MsgBackupCredentialInvalid {
		t.Errorf("result = %+v, want %q", res, MsgBackupCredentialInvalid)
	}
	if len(tester.params) != 0 {
		t.Error("tester must not run when decryption fails")
	}
	if len(audit.events) != 1 || !strings.Contains(audit.events[0].detail, "decrypt") {
		t.Errorf("audit = %+v, want result=failed decrypt", audit.events)
	}
}

func TestTestBackupDestinationNotFound(t *testing.T) {
	svc, _, _, _ := newBackupService()
	if _, err := svc.TestBackupDestination(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrBackupDestinationNotFound) {
		t.Fatalf("err = %v, want ErrBackupDestinationNotFound", err)
	}
}

func TestBackupForbidden(t *testing.T) {
	// TEST_FORBIDDEN: every backup method re-checks the live permission.
	svc, store, _, _ := newBackupService("dashboard.view")
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	input := BackupDestinationInput{Name: "x", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}
	if _, err := svc.CreateBackupDestination(context.Background(), actorID, input); !errors.Is(err, ErrForbidden) {
		t.Errorf("create err = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input); !errors.Is(err, ErrForbidden) {
		t.Errorf("update err = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteBackupDestination(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete err = %v, want ErrForbidden", err)
	}
	if _, err := svc.TestBackupDestination(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("test err = %v, want ErrForbidden", err)
	}
}

func TestCurrentBackupDestinationsReadOnlyPort(t *testing.T) {
	// The read-only consumer port (AD-15): returns live destinations incl.
	// ciphertext for the future backup job, no permission check.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "S3", BackupMechanismS3)}
	got, err := svc.CurrentBackupDestinations(context.Background())
	if err != nil {
		t.Fatalf("CurrentBackupDestinations err = %v", err)
	}
	if len(got) != 1 || got[0].PasswordEncrypted != "enc:pw" {
		t.Errorf("dests = %+v, want ciphertext carried for in-memory decrypt", got)
	}

	store.dests = nil
	got, err = svc.CurrentBackupDestinations(context.Background())
	if err != nil {
		t.Fatalf("CurrentBackupDestinations err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty list = %d, want 0", len(got))
	}
}

func TestCredentialConfigured(t *testing.T) {
	if (*BackupDestination)(nil).CredentialConfigured() {
		t.Error("CredentialConfigured(nil) = true, want false")
	}
	if d := (&BackupDestination{}).CredentialConfigured(); d {
		t.Error("CredentialConfigured(empty) = true, want false")
	}
	if d := (&BackupDestination{PasswordEncrypted: "enc:x"}).CredentialConfigured(); !d {
		t.Error("CredentialConfigured(ciphertext) = false, want true")
	}
}

func TestCreateBackupDestinationDuplicateName(t *testing.T) {
	// Finding: a destination's name must be unique (case-insensitive) — a
	// create with a name another destination already holds is rejected.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "S3 Ziel", BackupMechanismS3)}

	cred := "geheim"
	input := BackupDestinationInput{
		Name: "s3 ziel", Mechanism: BackupMechanismS3,
		Endpoint: "s3.example.com", BucketOrPath: "bucket", Username: "u", Password: &cred,
	}
	_, err := svc.CreateBackupDestination(context.Background(), actorID, input)
	var inv *InvalidBackupDestinationsError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidBackupDestinationsError", err)
	}
	if !strings.Contains(inv.Message, MsgBackupDestinationNameTaken) {
		t.Errorf("message = %q, want %q", inv.Message, MsgBackupDestinationNameTaken)
	}
	if len(store.created) != 0 {
		t.Error("duplicate-name destination must not be persisted")
	}
}

func TestUpdateBackupDestinationDuplicateName(t *testing.T) {
	// Finding: an update that takes another destination's name is rejected; an
	// update keeping its own name is fine.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{
		destFixture("id-a", "Lokal", BackupMechanismLocal),
		destFixture("id-b", "S3", BackupMechanismS3),
	}

	// Taking id-b's name → invalid.
	input := BackupDestinationInput{Name: "S3", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}
	_, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
	var inv *InvalidBackupDestinationsError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidBackupDestinationsError", err)
	}
	if !strings.Contains(inv.Message, MsgBackupDestinationNameTaken) {
		t.Errorf("message = %q, want %q", inv.Message, MsgBackupDestinationNameTaken)
	}

	// Keeping its own name (case variant) → OK.
	input = BackupDestinationInput{Name: "lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/x"}
	if _, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input); err != nil {
		t.Fatalf("UpdateBackupDestination(own name) err = %v", err)
	}
}

func TestCreateBackupDestinationInjection(t *testing.T) {
	// Finding: CR/LF/NUL in the username or a provided password must be
	// rejected on BOTH the create and update paths (they are interpolated into
	// raw FTP/SFTP wire commands).
	cases := []struct {
		name    string
		change  func(*BackupDestinationInput)
		wantMsg string
	}{
		{"username CRLF", func(i *BackupDestinationInput) { i.Username = "svc\r\nQUIT" }, "Benutzername"},
		{"username LF", func(i *BackupDestinationInput) { i.Username = "svc\nQUIT" }, "Benutzername"},
		{"username NUL", func(i *BackupDestinationInput) { i.Username = "svc\x00x" }, "Benutzername"},
		{"password CRLF", func(i *BackupDestinationInput) { p := "geheim\r\nPASS x"; i.Password = &p }, "Zugangsberechtigung"},
		{"password NUL", func(i *BackupDestinationInput) { p := "geheim\x00x"; i.Password = &p }, "Zugangsberechtigung"},
	}
	for _, tc := range cases {
		t.Run("create_"+tc.name, func(t *testing.T) {
			svc, store, _, _ := newBackupService()
			cred := "geheim"
			input := BackupDestinationInput{
				Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/x",
				Username: "svc", Password: &cred,
			}
			tc.change(&input)
			_, err := svc.CreateBackupDestination(context.Background(), actorID, input)
			var inv *InvalidBackupDestinationsError
			if !errors.As(err, &inv) {
				t.Fatalf("create err = %v, want *InvalidBackupDestinationsError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("create message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if len(store.created) != 0 {
				t.Error("injected destination must not be persisted")
			}
		})
		t.Run("update_"+tc.name, func(t *testing.T) {
			svc, store, _, _ := newBackupService()
			store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
			input := BackupDestinationInput{
				Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/x",
				Username: "svc",
			}
			tc.change(&input)
			_, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
			var inv *InvalidBackupDestinationsError
			if !errors.As(err, &inv) {
				t.Fatalf("update err = %v, want *InvalidBackupDestinationsError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("update message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
		})
	}
}

func TestUpdateBackupDestinationClearCredential(t *testing.T) {
	// Finding: a stored credential can be revoked via clear_credential:true —
	// the persisted row ends up credential-less, the write is audited.
	svc, store, audit, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}

	input := BackupDestinationInput{
		Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/x",
		ClearCredential: true,
	}
	got, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateBackupDestination(clear) err = %v", err)
	}
	if got.PasswordEncrypted != "" {
		t.Errorf("password_encrypted = %q, want cleared", got.PasswordEncrypted)
	}
	if got.CredentialConfigured() {
		t.Error("credential_configured = true after clear, want false")
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationBackupSettingsUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateBackupDestinationClearAndReplaceConflict(t *testing.T) {
	// Finding: clear_credential and a provided password are mutually exclusive.
	svc, store, _, _ := newBackupService()
	store.dests = []*BackupDestination{destFixture("id-a", "Lokal", BackupMechanismLocal)}
	cred := "neu"
	input := BackupDestinationInput{
		Name: "Lokal", Mechanism: BackupMechanismLocal, BucketOrPath: "/x",
		Password: &cred, ClearCredential: true,
	}
	_, err := svc.UpdateBackupDestination(context.Background(), actorID, "id-a", input)
	var inv *InvalidBackupDestinationsError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidBackupDestinationsError", err)
	}
	if !strings.Contains(inv.Message, MsgBackupClearCredentialConflict) {
		t.Errorf("message = %q, want %q", inv.Message, MsgBackupClearCredentialConflict)
	}
}