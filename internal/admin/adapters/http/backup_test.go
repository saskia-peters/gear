package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/admin/ports"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// Backup fakeService methods (defined here, same package): the Story 3.2
// backup surface consumed by the handler tests.

func (f *fakeService) ListBackupDestinations(_ context.Context, _ string) ([]*core.BackupDestination, error) {
	if f.backupListErr != nil {
		return nil, f.backupListErr
	}
	if f.backupDests == nil {
		return []*core.BackupDestination{}, nil
	}
	return f.backupDests, nil
}

func (f *fakeService) CreateBackupDestination(_ context.Context, _ string, input core.BackupDestinationInput) (*core.BackupDestination, error) {
	if f.backupWriteErr != nil {
		return nil, f.backupWriteErr
	}
	f.lastBackupInput = input
	d := &core.BackupDestination{
		ID: "id-new", Name: input.Name, Mechanism: input.Mechanism,
		Endpoint: input.Endpoint, BucketOrPath: input.BucketOrPath,
		Username: input.Username, Schedule: input.Schedule,
	}
	if input.Password != nil {
		d.PasswordEncrypted = "enc:" + *input.Password
	}
	return d, nil
}

func (f *fakeService) UpdateBackupDestination(_ context.Context, _, id string, input core.BackupDestinationInput) (*core.BackupDestination, error) {
	if f.backupWriteErr != nil {
		return nil, f.backupWriteErr
	}
	f.lastBackupInput = input
	d := &core.BackupDestination{
		ID: id, Name: input.Name, Mechanism: input.Mechanism,
		Endpoint: input.Endpoint, BucketOrPath: input.BucketOrPath,
		Username: input.Username, Schedule: input.Schedule,
	}
	if input.ClearCredential {
		d.PasswordEncrypted = ""
	} else if input.Password != nil {
		d.PasswordEncrypted = "enc:" + *input.Password
	}
	return d, nil
}

func (f *fakeService) DeleteBackupDestination(_ context.Context, _, _ string) error {
	return f.backupDeleteErr
}

func (f *fakeService) TestBackupDestination(_ context.Context, _, _ string) (*core.BackupTestResult, error) {
	if f.backupTestErr != nil {
		return nil, f.backupTestErr
	}
	if f.backupTestRes == nil {
		return &core.BackupTestResult{Ok: true, Message: core.MsgBackupTestOK}, nil
	}
	return f.backupTestRes, nil
}

// backupGateway wraps the REAL BackupRoutes() behind the same
// RequireAnyPermission gate the composition root uses (admin.settings.backup),
// with a fake session validator + permission resolver.
func backupGateway(perms []string, session *usercore.Session, svc ports.Service) http.Handler {
	h := NewHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{core.BackupSettingsPermission},
		"admin.settings.backup access denied", discardLogger(),
	)(h.BackupRoutes())
}

func backupDestFixture(id, name, mechanism string) *core.BackupDestination {
	return &core.BackupDestination{
		ID: id, Name: name, Mechanism: mechanism,
		Endpoint: "s3.example.com", BucketOrPath: "bucket",
		Username: "svc", PasswordEncrypted: "enc:secret",
	}
}

func TestBackupGetListEmpty(t *testing.T) {
	// GET_LIST_EMPTY: 200 `[]` (never null), no credential field in the rows.
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want JSON empty array", rec.Body.String())
	}
}

func TestBackupGetList(t *testing.T) {
	// GET_LIST: 200 list, credential_configured per row, NO ciphertext/credential
	// field in the payload (NFR-S4).
	svc := &fakeService{backupDests: []*core.BackupDestination{
		backupDestFixture("id-a", "S3", core.BackupMechanismS3),
		backupDestFixture("id-b", "Lokal", core.BackupMechanismLocal),
	}}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("rows = %d, want 2", len(body))
	}
	if body[0]["id"] != "id-a" || body[0]["name"] != "S3" {
		t.Errorf("row 0 = %+v", body[0])
	}
	if body[0]["credential_configured"] != true {
		t.Errorf("row 0 credential_configured = %v, want true", body[0]["credential_configured"])
	}
	if strings.Contains(rec.Body.String(), "enc:secret") {
		t.Error("response leaks the stored ciphertext")
	}
	if _, present := body[0]["password_encrypted"]; present {
		t.Error("response contains a password_encrypted field — never expose it")
	}
	if _, present := body[0]["password"]; present {
		t.Error("response contains a password field — never expose it")
	}
}

func TestBackupGetForbidden(t *testing.T) {
	// TEST_FORBIDDEN: authenticated caller without admin.settings.backup →
	// uniform 403 with no destination data and no hint of what is missing.
	svc := &fakeService{backupDests: []*core.BackupDestination{backupDestFixture("id-a", "S3", core.BackupMechanismS3)}}
	surface := backupGateway([]string{core.SmtpSettingsPermission, "dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "backup") || strings.Contains(rec.Body.String(), "enc:secret") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks destination data: %s", rec.Body.String())
	}
}

func TestBackupGetUnauthenticated(t *testing.T) {
	surface := backupGateway([]string{core.BackupSettingsPermission}, nil, &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestBackupCreateValid(t *testing.T) {
	// CREATE_VALID: 201 with the saved destination + German confirmation; the
	// response still never contains a credential.
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"S3","mechanism":"s3","endpoint":"s3.example.com","bucket_or_path":"bucket","username":"svc","password":"geheim","schedule":""}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgBackupDestinationSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgBackupDestinationSaved)
	}
	if body["credential_configured"] != true {
		t.Errorf("credential_configured = %v, want true", body["credential_configured"])
	}
	if strings.Contains(rec.Body.String(), "geheim") {
		t.Error("POST response leaks the plaintext credential")
	}
	if _, present := body["password"]; present {
		t.Error("POST response leaks a password field")
	}
}

func TestBackupCreateNoCredential(t *testing.T) {
	// CREATE_NO_CREDENTIAL: s3 without credential → 400 invalid_request with the
	// German message.
	svc := &fakeService{backupWriteErr: &core.InvalidBackupDestinationsError{Message: core.MsgBackupRequiresCredential}}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"S3","mechanism":"s3","endpoint":"s3.example.com","bucket_or_path":"bucket"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "Zugangsberechtigung") {
		t.Errorf("message = %q, want credential microcopy", env.Error.Message)
	}
}

func TestBackupCreateInvalid(t *testing.T) {
	// CREATE_INVALID: bad mechanism → 400 invalid_request with German message.
	svc := &fakeService{backupWriteErr: &core.InvalidBackupDestinationsError{Message: "Bitte wähle einen gültigen Mechanismus (S3, FTP, SFTP oder Lokal)."}}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"x","mechanism":"nfs","endpoint":"h","bucket_or_path":"b"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "Mechanismus") {
		t.Errorf("message = %q, want mechanism microcopy", env.Error.Message)
	}
}

func TestBackupUpdate(t *testing.T) {
	// UPDATE_KEEP_CREDENTIAL: PUT returns 200 with the saved destination +
	// German confirmation; the credential is never in the response.
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok",
		`{"name":"S3 v2","mechanism":"s3","endpoint":"s3.example.com","bucket_or_path":"bucket","username":"svc"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["id"] != "id-a" || body["name"] != "S3 v2" {
		t.Errorf("body = %+v, want updated destination", body)
	}
	if body["message"] != core.MsgBackupDestinationSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgBackupDestinationSaved)
	}
	if _, present := body["password"]; present {
		t.Error("PUT response leaks a password field")
	}
}

func TestBackupDelete(t *testing.T) {
	// DELETE: 200 with the German confirmation, audited at the core.
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodDelete, "/id-a", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgBackupDestinationDeleted {
		t.Errorf("message = %q, want %q", body["message"], core.MsgBackupDestinationDeleted)
	}
}

func TestBackupNotFound(t *testing.T) {
	// A missing id on update/delete/test → uniform 404 envelope.
	svc := &fakeService{backupWriteErr: core.ErrBackupDestinationNotFound}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-missing", "tok", `{"name":"x","mechanism":"local","bucket_or_path":"/x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestBackupTestOK(t *testing.T) {
	// TEST_OK: 200-style {ok:true} result with the German success message.
	svc := &fakeService{backupTestRes: &core.BackupTestResult{Ok: true, Message: core.MsgBackupTestOK}}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/test", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.BackupTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !res.Ok || res.Message != core.MsgBackupTestOK {
		t.Errorf("result = %+v, want ok + %q", res, core.MsgBackupTestOK)
	}
}

func TestBackupTestFail(t *testing.T) {
	// TEST_FAIL: 200-style {ok:false} with the GENERIC inline German message
	// (never a generic 5xx, never the raw engine detail).
	svc := &fakeService{backupTestRes: &core.BackupTestResult{Ok: false, Message: core.MsgBackupTestFailed}}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/test", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200-style result (body %s)", rec.Code, rec.Body.String())
	}
	var res core.BackupTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if res.Ok {
		t.Error("result.Ok = true, want false")
	}
	if res.Message != core.MsgBackupTestFailed {
		t.Errorf("message = %q, want generic %q", res.Message, core.MsgBackupTestFailed)
	}
}

func TestBackupTestNotFoundEnvelope(t *testing.T) {
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/a/b/c", "tok", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestBackupMethodNotAllowedEnvelope(t *testing.T) {
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPatch, "/", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 err = %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}

func TestBackupUpdateClearCredential(t *testing.T) {
	// Finding: clear_credential:true is threaded through the PUT body — the
	// fake receives the signal with no password and answers a credential-less
	// destination.
	svc := &fakeService{}
	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok",
		`{"name":"S3 v2","mechanism":"s3","endpoint":"s3.example.com","bucket_or_path":"bucket","username":"svc","clear_credential":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !svc.lastBackupInput.ClearCredential {
		t.Error("clear_credential was not threaded through to the service")
	}
	if svc.lastBackupInput.Password != nil {
		t.Errorf("password = %v, want nil when clearing", *svc.lastBackupInput.Password)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["credential_configured"] != false {
		t.Errorf("credential_configured = %v, want false after clear", body["credential_configured"])
	}
}

// handlerBackupStore is an in-memory core.BackupDestinationsStore used to wire
// the REAL core service behind the REAL HTTP handlers for the leak test.
type handlerBackupStore struct {
	dests []*core.BackupDestination
}

func (s *handlerBackupStore) ListBackupDestinations(context.Context) ([]*core.BackupDestination, error) {
	return s.dests, nil
}
func (s *handlerBackupStore) GetBackupDestination(_ context.Context, id string) (*core.BackupDestination, error) {
	for _, d := range s.dests {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, nil
}
func (s *handlerBackupStore) CreateBackupDestination(context.Context, *core.BackupDestination) (*core.BackupDestination, error) {
	return nil, errors.New("not implemented")
}
func (s *handlerBackupStore) UpdateBackupDestination(context.Context, *core.BackupDestination) (*core.BackupDestination, error) {
	return nil, errors.New("not implemented")
}
func (s *handlerBackupStore) DeleteBackupDestination(context.Context, string) error {
	return errors.New("not implemented")
}

type handlerCipher struct{}

func (handlerCipher) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (handlerCipher) Decrypt(e string) (string, error) {
	if !strings.HasPrefix(e, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(e, "enc:"), nil
}

type handlerAudit struct{}

func (handlerAudit) InsertAuditEvent(context.Context, string, string, string, string) error { return nil }

type handlerTester struct{ err error }

func (t handlerTester) Test(context.Context, core.BackupTestParams) error { return t.err }

// TestBackupTestEngineErrorNotLeaked wires the REAL core service (with a
// failing tester that surfaces a raw engine-level error) behind the REAL HTTP
// handlers and proves the wire body is only the generic German message — the
// engine detail (e.g. "PUT answered HTTP 403") never reaches the client
// (finding).
func TestBackupTestEngineErrorNotLeaked(t *testing.T) {
	store := &handlerBackupStore{dests: []*core.BackupDestination{{
		ID: "id-a", Name: "S3", Mechanism: core.BackupMechanismS3,
		Endpoint: "s3.example.com", BucketOrPath: "bucket", Username: "svc", PasswordEncrypted: "enc:pw",
	}}}
	engineErr := errors.New("backup tester: s3: PUT answered HTTP 403")
	svc := core.NewService(nil, store, nil, handlerCipher{},
		&gateResolver{perms: []string{core.BackupSettingsPermission}}, handlerAudit{}, nil,
		handlerTester{err: engineErr}, discardLogger())

	surface := backupGateway([]string{core.BackupSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/test", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200-style result (body %s)", rec.Code, rec.Body.String())
	}
	var res core.BackupTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if res.Ok {
		t.Error("result.Ok = true, want false")
	}
	if res.Message != core.MsgBackupTestFailed {
		t.Errorf("message = %q, want generic %q", res.Message, core.MsgBackupTestFailed)
	}
	if strings.Contains(rec.Body.String(), "403") || strings.Contains(rec.Body.String(), "PUT") || strings.Contains(rec.Body.String(), "s3.example.com") {
		t.Errorf("response leaks engine detail: %s", rec.Body.String())
	}
}