package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeService is an in-memory ports.Service for the handler tests.
type fakeService struct {
	settings *core.SmtpSettings
	getErr   error
	putErr   error
	testRes  *core.SmtpTestResult
	testErr  error

	backupDests      []*core.BackupDestination
	backupListErr    error
	backupWriteErr   error
	backupDeleteErr  error
	backupTestRes    *core.BackupTestResult
	backupTestErr    error
	lastBackupInput  core.BackupDestinationInput
}

func (f *fakeService) GetSmtpSettings(_ context.Context, _ string) (*core.SmtpSettings, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.settings == nil {
		return &core.SmtpSettings{}, nil
	}
	return f.settings, nil
}

func (f *fakeService) UpdateSmtpSettings(_ context.Context, _ string, input core.UpdateSmtpSettingsInput) (*core.SmtpSettings, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	s := &core.SmtpSettings{
		Host: input.Host, Port: input.Port, Security: input.Security,
		SenderAddress: input.SenderAddress, SenderName: input.SenderName, Username: input.Username,
	}
	if input.Password != nil {
		s.PasswordEncrypted = "enc:" + *input.Password
	}
	return s, nil
}

func (f *fakeService) TestSmtpSettings(_ context.Context, _, _ string) (*core.SmtpTestResult, error) {
	if f.testErr != nil {
		return nil, f.testErr
	}
	if f.testRes == nil {
		return &core.SmtpTestResult{Ok: true, Message: core.MsgSmtpTestSent}, nil
	}
	return f.testRes, nil
}

// gateway wraps the REAL Routes() behind the same RequireAnyPermission gate the
// composition root uses (admin.settings.email), with a fake session validator +
// permission resolver.
func gateway(perms []string, session *usercore.Session, svc *fakeService) http.Handler {
	h := NewHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{core.SmtpSettingsPermission},
		"admin.settings.email access denied", discardLogger(),
	)(h.Routes())
}

type gateValidator struct {
	session *usercore.Session
}

func (v *gateValidator) Validate(context.Context, string) (*usercore.Session, error) {
	return v.session, nil
}

type gateResolver struct {
	perms []string
}

func (r *gateResolver) ListPermissionsByUser(context.Context, string) ([]string, error) {
	return r.perms, nil
}

func activeAdmin() *usercore.Session {
	return &usercore.Session{User: &usercore.User{ID: "u-admin", Email: "admin@gear.local", State: usercore.StateActive}}
}

func doRequest(h http.Handler, method, path, token string, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSmtpSettingsGetInitial(t *testing.T) {
	// GET_INITIAL: 200 with zero defaults, password_configured=false, and NO
	// password/ciphertext field in the payload (NFR-S4).
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/smtp", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body err = %v", err)
	}
	if body["password_configured"] != false {
		t.Errorf("password_configured = %v, want false", body["password_configured"])
	}
	if _, present := body["password"]; present {
		t.Error("payload contains a password field — never expose it")
	}
	if _, present := body["password_encrypted"]; present {
		t.Error("payload contains password_encrypted — never expose the ciphertext")
	}
}

func TestSmtpSettingsGetAsHolder(t *testing.T) {
	svc := &fakeService{settings: &core.SmtpSettings{
		Host: "smtp.example.com", Port: 587, Security: core.SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", SenderName: "G.E.A.R.",
		Username: "smtpuser", PasswordEncrypted: "enc:secret",
	}}
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/smtp", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Host               string `json:"host"`
		Port               int    `json:"port"`
		Security           string `json:"security"`
		SenderAddress      string `json:"sender_address"`
		SenderName         string `json:"sender_name"`
		Username           string `json:"username"`
		PasswordConfigured bool   `json:"password_configured"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body.Host != "smtp.example.com" || body.Port != 587 || body.Security != "starttls" {
		t.Errorf("payload = %+v, want stored values", body)
	}
	if body.SenderName != "G.E.A.R." || body.Username != "smtpuser" {
		t.Errorf("payload = %+v, want sender/username", body)
	}
	if !body.PasswordConfigured {
		t.Error("password_configured = false, want true (ciphertext present)")
	}
	if strings.Contains(rec.Body.String(), "enc:secret") {
		t.Error("response leaks the stored ciphertext")
	}
}

func TestSmtpSettingsGetForbidden(t *testing.T) {
	// PUT_FORBIDDEN: authenticated caller without admin.settings.email → uniform
	// 403 with no settings data and no hint of what is missing.
	svc := &fakeService{settings: &core.SmtpSettings{Host: "smtp.example.com", PasswordEncrypted: "enc:secret"}}
	surface := gateway([]string{"dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/smtp", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "smtp") || strings.Contains(rec.Body.String(), "enc:secret") {
		t.Errorf("403 body leaks settings data: %s", rec.Body.String())
	}
}

func TestSmtpSettingsGetUnauthenticated(t *testing.T) {
	surface := gateway([]string{core.SmtpSettingsPermission}, nil, &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/smtp", "", "")
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

func TestSmtpSettingsPutValid(t *testing.T) {
	// PUT_VALID: 200 with the saved values + German confirmation; the response
	// still never contains a password.
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPut, "/smtp", "tok",
		`{"host":"smtp.example.com","port":587,"security":"starttls","sender_address":"noreply@example.com","sender_name":"G.E.A.R.","username":"u","password":"geheim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgSmtpSettingsSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgSmtpSettingsSaved)
	}
	if body["password_configured"] != true {
		t.Errorf("password_configured = %v, want true", body["password_configured"])
	}
	if _, present := body["password"]; present {
		t.Error("PUT response leaks a password field")
	}
	if strings.Contains(rec.Body.String(), "geheim") {
		t.Error("PUT response leaks the plaintext password")
	}
}

func TestSmtpSettingsPutInvalid(t *testing.T) {
	// PUT_INVALID: empty host → 400 invalid_request with German message.
	svc := &fakeService{putErr: &core.InvalidSmtpSettingsError{Message: "Bitte gib einen SMTP-Host an."}}
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/smtp", "tok",
		`{"host":"","port":587,"security":"starttls","sender_address":"noreply@example.com"}`)
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
	if !strings.Contains(env.Error.Message, "SMTP-Host") {
		t.Errorf("message = %q, want host validation", env.Error.Message)
	}
}

func TestSmtpSettingsPutForbidden(t *testing.T) {
	surface := gateway([]string{"dashboard.view"}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPut, "/smtp", "tok",
		`{"host":"smtp.example.com","port":587,"security":"none","sender_address":"noreply@example.com"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSmtpSettingsPostTestOK(t *testing.T) {
	// TEST_OK: 200-style {ok:true} result with German message.
	svc := &fakeService{testRes: &core.SmtpTestResult{Ok: true, Message: core.MsgSmtpTestSent}}
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/smtp/test", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.SmtpTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !res.Ok || res.Message != core.MsgSmtpTestSent {
		t.Errorf("result = %+v, want ok + %q", res, core.MsgSmtpTestSent)
	}
}

func TestSmtpSettingsPostTestFail(t *testing.T) {
	// TEST_FAIL: 200-style {ok:false} with the GENERIC inline German message
	// (never a generic 5xx, never the raw engine detail — finding 5).
	svc := &fakeService{testRes: &core.SmtpTestResult{Ok: false, Message: core.MsgSmtpTestFailed}}
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/smtp/test", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200-style result (body %s)", rec.Code, rec.Body.String())
	}
	var res core.SmtpTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if res.Ok {
		t.Error("result.Ok = true, want false")
	}
	if res.Message != core.MsgSmtpTestFailed {
		t.Errorf("message = %q, want generic %q", res.Message, core.MsgSmtpTestFailed)
	}
}

func TestSmtpSettingsNotFoundEnvelope(t *testing.T) {
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/unknown", "tok", "")
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

func TestSmtpSettingsMethodNotAllowedEnvelope(t *testing.T) {
	surface := gateway([]string{core.SmtpSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodDelete, "/smtp", "tok", "")
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