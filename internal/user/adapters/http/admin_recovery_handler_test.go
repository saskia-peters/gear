package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

func authedRecoveryRequest(body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/admin/recovery/request", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	user := &core.User{ID: "u-admina", Email: "admina@gear.local", State: core.StateActive}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

func authedApproveRequest(body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/admin/recovery/approve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	user := &core.User{ID: "u-adminb", Email: "adminb@gear.local", State: core.StateActive}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

func TestHandlerAdminRecoveryRequestHappyPath(t *testing.T) {
	var gotCaller *core.User
	var gotEmail string
	svc := &mockService{
		requestAdminRecoveryFunc: func(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error) {
			gotCaller = caller
			gotEmail = targetEmail
			return &core.AdminRecoveryResult{Message: core.MsgAdminRecoveryRequested, TargetEmail: targetEmail}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admina@gear.local"})
	req := authedRecoveryRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotCaller == nil || gotCaller.Email != "admina@gear.local" {
		t.Errorf("caller not forwarded, got %+v", gotCaller)
	}
	if gotEmail != "admina@gear.local" {
		t.Errorf("target email = %q, want admina@gear.local", gotEmail)
	}
	var res core.AdminRecoveryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgAdminRecoveryRequested {
		t.Errorf("message = %q, want %q", res.Message, core.MsgAdminRecoveryRequested)
	}
}

func TestHandlerAdminRecoveryRequestForbidden(t *testing.T) {
	svc := &mockService{
		requestAdminRecoveryFunc: func(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error) {
			return nil, core.ErrForbidden
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admina@gear.local"})
	req := authedRecoveryRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryRequest(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
}

func TestHandlerAdminRecoveryRequestLastAdminBlocked(t *testing.T) {
	svc := &mockService{
		requestAdminRecoveryFunc: func(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error) {
			return nil, core.ErrLastAdminRecoveryBlocked
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admina@gear.local"})
	req := authedRecoveryRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "last_admin_recovery_blocked" {
		t.Errorf("code = %q, want last_admin_recovery_blocked", env.Error.Code)
	}
}

func TestHandlerAdminRecoveryRequestMissingEmail(t *testing.T) {
	svc := &mockService{
		requestAdminRecoveryFunc: func(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error) {
			return nil, core.ErrMissingFields
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": ""})
	req := authedRecoveryRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerAdminRecoveryApproveHappyPath(t *testing.T) {
	var gotApprover *core.User
	var gotEmail, gotReason string
	var gotConfirmed bool
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			gotApprover = approver
			gotEmail, gotReason, gotConfirmed = targetEmail, reason, confirmed
			return &core.AdminRecoveryApproveResult{Message: core.MsgAdminRecoveryApproved, RecoveryToken: "raw-recovery-token"}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "admina@gear.local", "reason": "Ausgesperrt", "confirmed": true})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotApprover == nil || gotApprover.Email != "adminb@gear.local" {
		t.Errorf("approver not forwarded, got %+v", gotApprover)
	}
	if gotEmail != "admina@gear.local" || gotReason != "Ausgesperrt" || !gotConfirmed {
		t.Errorf("payload not forwarded: (%q,%q,%v)", gotEmail, gotReason, gotConfirmed)
	}
	var res core.AdminRecoveryApproveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.RecoveryToken != "raw-recovery-token" {
		t.Errorf("recovery_token = %q, want raw-recovery-token", res.RecoveryToken)
	}
}

func TestHandlerAdminRecoveryApproveMissingReason(t *testing.T) {
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			return nil, core.ErrRecoveryReasonRequired
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "admina@gear.local", "reason": "", "confirmed": true})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgRecoveryReasonRequired {
		t.Errorf("error = %+v, want invalid_request %q", env.Error, core.MsgRecoveryReasonRequired)
	}
}

func TestHandlerAdminRecoveryApproveNotConfirmed(t *testing.T) {
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			return nil, core.ErrRecoveryNotConfirmed
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "admina@gear.local", "reason": "Begründung", "confirmed": false})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgRecoveryNotConfirmed {
		t.Errorf("error = %+v, want invalid_request %q", env.Error, core.MsgRecoveryNotConfirmed)
	}
}

func TestHandlerAdminRecoveryApproveSelfApprovalForbidden(t *testing.T) {
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			return nil, core.ErrForbidden
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "adminb@gear.local", "reason": "selbst", "confirmed": true})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
}

func TestHandlerAdminRecoveryApproveInvalidToken(t *testing.T) {
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			return nil, core.ErrAdminRecoveryInvalid
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "admina@gear.local", "reason": "Begründung", "confirmed": true})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_token" {
		t.Errorf("code = %q, want invalid_token", env.Error.Code)
	}
}

func TestHandlerResetPasswordFallsBackToAdminRecovery(t *testing.T) {
	// The shared /password/reset endpoint completes an APPROVED admin-recovery
	// token via CompleteAdminRecovery when it is not a valid FR-26 forgot token.
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			return nil, core.ErrResetTokenInvalid
		},
		completeAdminRecoveryFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.AdminRecoveryCompleteResult, error) {
			return &core.AdminRecoveryCompleteResult{Message: core.MsgAdminRecoveryComplete}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "approved-admin-recovery-token", "new_password": "neuesadminpass123", "new_password_confirm": "neuesadminpass123",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.AdminRecoveryCompleteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgAdminRecoveryComplete {
		t.Errorf("message = %q, want %q", res.Message, core.MsgAdminRecoveryComplete)
	}
}

func TestHandlerAdminRecoveryApproveMFARequired(t *testing.T) {
	// MFA_STEP_UP (review finding 1.10): an approving admin with MFA enabled and
	// no valid TOTP code maps to 403 recovery_mfa_required.
	svc := &mockService{
		approveAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
			return nil, core.ErrRecoveryMFARequired
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{"email": "admina@gear.local", "reason": "Begründung", "confirmed": true, "totp_code": ""})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryApprove(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "recovery_mfa_required" {
		t.Errorf("code = %q, want recovery_mfa_required", env.Error.Code)
	}
}

func TestHandlerAdminRecoveryDenyHappyPath(t *testing.T) {
	var gotApprover *core.User
	var gotEmail, gotReason string
	svc := &mockService{
		denyAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string) (*core.AdminRecoveryDenyResult, error) {
			gotApprover = approver
			gotEmail, gotReason = targetEmail, reason
			return &core.AdminRecoveryDenyResult{Message: core.MsgAdminRecoveryDenied}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admina@gear.local", "reason": "unberechtigt"})
	req := authedApproveRequest(body)
	req.Method = http.MethodPost
	req.URL.Path = "/admin/recovery/deny"
	rec := httptest.NewRecorder()
	h.AdminRecoveryDeny(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotApprover == nil || gotApprover.Email != "adminb@gear.local" {
		t.Errorf("approver not forwarded, got %+v", gotApprover)
	}
	if gotEmail != "admina@gear.local" || gotReason != "unberechtigt" {
		t.Errorf("deny payload not forwarded: (%q,%q)", gotEmail, gotReason)
	}
	var res core.AdminRecoveryDenyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgAdminRecoveryDenied {
		t.Errorf("message = %q, want %q", res.Message, core.MsgAdminRecoveryDenied)
	}
}

func TestHandlerAdminRecoveryDenyMissingReason(t *testing.T) {
	svc := &mockService{
		denyAdminRecoveryFunc: func(ctx context.Context, approver *core.User, targetEmail, reason string) (*core.AdminRecoveryDenyResult, error) {
			return nil, core.ErrRecoveryDenyReasonRequired
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admina@gear.local", "reason": ""})
	req := authedApproveRequest(body)
	rec := httptest.NewRecorder()
	h.AdminRecoveryDeny(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgRecoveryDenyReasonRequired {
		t.Errorf("error = %+v, want invalid_request %q", env.Error, core.MsgRecoveryDenyReasonRequired)
	}
}

func TestHandlerAdminRecoveryPending(t *testing.T) {
	var gotCaller *core.User
	svc := &mockService{
		listAdminRecoveryFunc: func(ctx context.Context, caller *core.User) ([]*core.AdminRecoveryRequest, error) {
			gotCaller = caller
			return []*core.AdminRecoveryRequest{
				{ID: "tok-1", UserID: "u-admina", User: &core.User{ID: "u-admina", Email: "admina@gear.local"}},
			}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodGet, "/admin/recovery/pending", nil)
	user := &core.User{ID: "u-adminb", Email: "adminb@gear.local", State: core.StateActive}
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.AdminRecoveryPending(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotCaller == nil || gotCaller.Email != "adminb@gear.local" {
		t.Errorf("caller not forwarded, got %+v", gotCaller)
	}
	var wire struct {
		Requests []core.AdminRecoveryRequest `json:"requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Requests) != 1 || wire.Requests[0].UserID != "u-admina" {
		t.Errorf("requests = %+v, want 1 pending request for u-admina", wire.Requests)
	}
}

func TestHandlerResetPasswordFallbackMapsPolicyErrors(t *testing.T) {
	// RESET_POLICY_FALLBACK (review finding 1.10): when the admin-recovery
	// fallback rejects a SHORT password (validate-before-consume), the proper
	// 400 invalid_request short-password code/message is returned instead of
	// being swallowed into invalid_token.
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			return nil, core.ErrResetTokenInvalid
		},
		completeAdminRecoveryFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.AdminRecoveryCompleteResult, error) {
			return nil, core.ErrShortPassword
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "approved-admin-recovery-token", "new_password": "kurz", "new_password_confirm": "kurz",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request (not invalid_token)", env.Error.Code)
	}
	if env.Error.Message != core.MsgShortPassword {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgShortPassword)
	}
}
