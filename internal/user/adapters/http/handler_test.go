package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
	"github.com/saskia-peters/gear/internal/user/ports"
)

type mockService struct {
	registerFunc         func(ctx context.Context, input core.RegisterInput) (*ports.RegisterResult, error)
	loginFunc            func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error)
	logoutFunc           func(ctx context.Context, rawToken string) error
	enrollFunc           func(ctx context.Context, user *core.User) (*core.MFAEnrollResult, error)
	confirmFunc          func(ctx context.Context, user *core.User, secret, code string) error
	disableFunc          func(ctx context.Context, user *core.User, code string) error
	changePasswordFunc   func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error)
	getProfileFunc       func(ctx context.Context, user *core.User) (*core.Profile, error)
	updateProfileFunc    func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error)
	stageEmailFunc       func(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error)
	requestResetFunc     func(ctx context.Context, email string) (*core.ResetRequestResult, error)
	completeResetFunc    func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error)
	requestAdminRecoveryFunc func(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error)
	approveAdminRecoveryFunc func(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error)
	denyAdminRecoveryFunc    func(ctx context.Context, approver *core.User, targetEmail, reason string) (*core.AdminRecoveryDenyResult, error)
	listAdminRecoveryFunc    func(ctx context.Context, caller *core.User) ([]*core.AdminRecoveryRequest, error)
	completeAdminRecoveryFunc func(ctx context.Context, rawToken, newPassword, confirm string) (*core.AdminRecoveryCompleteResult, error)
	revokeOtherCalls     *int
	revokeAllCalls       *int
}

func (m *mockService) Register(ctx context.Context, input core.RegisterInput) (*ports.RegisterResult, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, input)
	}
	return &ports.RegisterResult{
		Message: core.UniformSuccessMessage,
		Status:  "pending_approval",
	}, nil
}

func (m *mockService) Login(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, input)
	}
	return &ports.LoginResult{
		Token: "opaque-token",
		User: core.LoginUser{
			ID:          "u-1",
			Email:       "max@example.com",
			DisplayName: "Max Mustermann",
			FirstName:   "Max",
			LastName:    "Mustermann",
		},
	}, nil
}

func (m *mockService) Logout(ctx context.Context, rawToken string) error {
	if m.logoutFunc != nil {
		return m.logoutFunc(ctx, rawToken)
	}
	return nil
}

func (m *mockService) EnrollMFARequest(ctx context.Context, user *core.User) (*core.MFAEnrollResult, error) {
	if m.enrollFunc != nil {
		return m.enrollFunc(ctx, user)
	}
	return &core.MFAEnrollResult{Secret: "SECRETBASE32", URI: "otpauth://totp/G.E.A.R.:max@example.com?secret=SECRETBASE32&issuer=G.E.A.R."}, nil
}

func (m *mockService) ConfirmMFAEnable(ctx context.Context, user *core.User, secret, code string) error {
	if m.confirmFunc != nil {
		return m.confirmFunc(ctx, user, secret, code)
	}
	return nil
}

func (m *mockService) DisableMFA(ctx context.Context, user *core.User, code string) error {
	if m.disableFunc != nil {
		return m.disableFunc(ctx, user, code)
	}
	return nil
}

func (m *mockService) MFAStatus(ctx context.Context, user *core.User) (bool, error) {
	if user != nil {
		return user.IsMFAEnabled, nil
	}
	return false, nil
}

func (m *mockService) RevokeOtherSessions(ctx context.Context, userID, rawToken string) error {
	if m.revokeOtherCalls != nil {
		*m.revokeOtherCalls++
	}
	return nil
}

func (m *mockService) RevokeAllSessions(ctx context.Context, userID string) error {
	if m.revokeAllCalls != nil {
		*m.revokeAllCalls++
	}
	return nil
}

func (m *mockService) ChangePassword(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
	if m.changePasswordFunc != nil {
		return m.changePasswordFunc(ctx, user, input, rawToken)
	}
	return &core.ChangePasswordResult{Message: core.MsgPasswordChanged}, nil
}

func (m *mockService) GetProfile(ctx context.Context, user *core.User) (*core.Profile, error) {
	if m.getProfileFunc != nil {
		return m.getProfileFunc(ctx, user)
	}
	return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: "Max", LastName: "Mustermann", DisplayName: "Max Mustermann", Attributes: map[string]any{}}, nil
}

func (m *mockService) UpdateProfile(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
	if m.updateProfileFunc != nil {
		return m.updateProfileFunc(ctx, user, input)
	}
	return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: input.FirstName, LastName: input.LastName, DisplayName: input.DisplayName, Attributes: input.Attributes}, nil
}

func (m *mockService) StageEmailChange(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error) {
	if m.stageEmailFunc != nil {
		return m.stageEmailFunc(ctx, user, newEmail)
	}
	return &core.StageEmailResult{Message: core.MsgEmailChangeStaged, PendingEmail: newEmail}, nil
}

func (m *mockService) RequestPasswordReset(ctx context.Context, email string) (*core.ResetRequestResult, error) {
	if m.requestResetFunc != nil {
		return m.requestResetFunc(ctx, email)
	}
	return &core.ResetRequestResult{Message: core.MsgPasswordResetRequested}, nil
}

func (m *mockService) CompletePasswordReset(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
	if m.completeResetFunc != nil {
		return m.completeResetFunc(ctx, rawToken, newPassword, confirm)
	}
	return &core.ResetCompleteResult{Message: core.MsgPasswordResetComplete}, nil
}

func (m *mockService) RequestAdminRecovery(ctx context.Context, caller *core.User, targetEmail string) (*core.AdminRecoveryResult, error) {
	if m.requestAdminRecoveryFunc != nil {
		return m.requestAdminRecoveryFunc(ctx, caller, targetEmail)
	}
	return &core.AdminRecoveryResult{Message: core.MsgAdminRecoveryRequested, TargetEmail: targetEmail}, nil
}

func (m *mockService) ApproveAdminRecovery(ctx context.Context, approver *core.User, targetEmail, reason string, confirmed bool, totpCode string) (*core.AdminRecoveryApproveResult, error) {
	if m.approveAdminRecoveryFunc != nil {
		return m.approveAdminRecoveryFunc(ctx, approver, targetEmail, reason, confirmed, totpCode)
	}
	return &core.AdminRecoveryApproveResult{Message: core.MsgAdminRecoveryApproved, RecoveryToken: "raw-recovery-token"}, nil
}

func (m *mockService) DenyAdminRecovery(ctx context.Context, approver *core.User, targetEmail, reason string) (*core.AdminRecoveryDenyResult, error) {
	if m.denyAdminRecoveryFunc != nil {
		return m.denyAdminRecoveryFunc(ctx, approver, targetEmail, reason)
	}
	return &core.AdminRecoveryDenyResult{Message: core.MsgAdminRecoveryDenied}, nil
}

func (m *mockService) ListAdminRecoveryRequest(ctx context.Context, caller *core.User) ([]*core.AdminRecoveryRequest, error) {
	if m.listAdminRecoveryFunc != nil {
		return m.listAdminRecoveryFunc(ctx, caller)
	}
	return []*core.AdminRecoveryRequest{}, nil
}

func (m *mockService) CompleteAdminRecovery(ctx context.Context, rawToken, newPassword, confirm string) (*core.AdminRecoveryCompleteResult, error) {
	if m.completeAdminRecoveryFunc != nil {
		return m.completeAdminRecoveryFunc(ctx, rawToken, newPassword, confirm)
	}
	// Default: no valid token, so the reset-endpoint fallback must fail.
	return nil, core.ErrAdminRecoveryInvalid
}

// stubValidator always authenticates the caller as an active user. Used to
// exercise the MFA endpoints without a real session store.
type stubValidator struct {
	user *core.User
}

func (v *stubValidator) Validate(_ context.Context, _ string) (*core.Session, error) {
	if v == nil || v.user == nil {
		return &core.Session{User: &core.User{ID: "u-1", Email: "max@example.com", State: core.StateActive}}, nil
	}
	return &core.Session{User: v.user}, nil
}

// rejectingValidator always fails authentication.
type rejectingValidator struct{}

func (rejectingValidator) Validate(context.Context, string) (*core.Session, error) {
	return nil, core.ErrSessionNotFound
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestHandler(svc ports.Service, validator auth.SessionValidator) *Handler {
	return NewHandler(svc, discardLogger(), validator)
}

func TestHandlerRegisterHappyPath(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{
		"first_name":       "Max",
		"last_name":        "Mustermann",
		"email":            "max@example.com",
		"password":         "sicher123456",
		"password_confirm": "sicher123456",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var res ports.RegisterResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Message != core.UniformSuccessMessage {
		t.Errorf("got message %q, want %q", res.Message, core.UniformSuccessMessage)
	}
	if res.Status != "pending_approval" {
		t.Errorf("got status %q, want %q", res.Status, "pending_approval")
	}
}

func TestHandlerRegisterValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		serviceErr  error
		wantMessage string
	}{
		{
			name:        "short password",
			serviceErr:  core.ErrShortPassword,
			wantMessage: "Das Passwort muss mindestens 10 Zeichen lang sein.",
		},
		{
			name:        "password mismatch",
			serviceErr:  core.ErrPasswordMismatch,
			wantMessage: "Die Passwörter stimmen nicht überein.",
		},
		{
			name:        "invalid email",
			serviceErr:  core.ErrInvalidEmail,
			wantMessage: "Bitte gib eine gültige E-Mail-Adresse ein.",
		},
		{
			name:        "missing fields",
			serviceErr:  core.ErrMissingFields,
			wantMessage: "Alle Pflichtfelder müssen ausgefüllt sein.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				registerFunc: func(ctx context.Context, input core.RegisterInput) (*ports.RegisterResult, error) {
					return nil, tt.serviceErr
				},
			}
			h := newTestHandler(svc, &stubValidator{})

			payload := map[string]string{
				"first_name":       "Max",
				"last_name":        "Mustermann",
				"email":            "max@example.com",
				"password":         "pass",
				"password_confirm": "pass",
			}
			body, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			h.Register(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("failed to decode error envelope: %v", err)
			}

			if env.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want %q", env.Error.Code, "invalid_request")
			}
			if env.Error.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.wantMessage)
			}
		})
	}
}

func TestHandlerRegisterInvalidJSON(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader([]byte("{invalid-json")))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_request")
	}
}

func TestHandlerRegisterInternalError(t *testing.T) {
	svc := &mockService{
		registerFunc: func(ctx context.Context, input core.RegisterInput) (*ports.RegisterResult, error) {
			return nil, errors.New("db connection lost")
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{
		"first_name":       "Max",
		"last_name":        "Mustermann",
		"email":            "max@example.com",
		"password":         "1234567890",
		"password_confirm": "1234567890",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want %q", env.Error.Code, "internal_error")
	}
}

func TestHandlerLoginHappyPath(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var res ports.LoginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Token != "opaque-token" {
		t.Errorf("token = %q, want opaque-token", res.Token)
	}
	if res.User.Email != "max@example.com" {
		t.Errorf("user email = %q, want max@example.com", res.User.Email)
	}
}

func TestHandlerLoginInvalidCredentials(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrInvalidCredentials
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "nobody@example.com", "password": "falsch"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "invalid_credentials" {
		t.Errorf("code = %q, want invalid_credentials", env.Error.Code)
	}
	if env.Error.Message != "E-Mail oder Passwort ist falsch." {
		t.Errorf("message = %q, want anti-enumeration microcopy", env.Error.Message)
	}
}

func TestHandlerLoginLockedOutReturns429WithRetryAfter(t *testing.T) {
	tests := []struct {
		name         string
		retryAfter   time.Duration
		wantSeconds  string
		wantMessage  string
	}{
		{name: "30 second lockout", retryAfter: 30 * time.Second, wantSeconds: "30", wantMessage: "Zu viele Fehlversuche. Bitte warte 30 Sekunden."},
		{name: "60 second lockout", retryAfter: 60 * time.Second, wantSeconds: "60", wantMessage: "Zu viele Fehlversuche. Bitte warte 60 Sekunden."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
					return nil, core.NewLockoutError(tt.retryAfter, "active@example.com")
				},
			}
			h := newTestHandler(svc, &stubValidator{})

			payload := map[string]string{"email": "active@example.com", "password": "geheim123456"}
			body, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			h.Login(rec, req)

			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantSeconds {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantSeconds)
			}

			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("failed to decode error envelope: %v", err)
			}
			if env.Error.Code != "too_many_attempts" {
				t.Errorf("code = %q, want too_many_attempts", env.Error.Code)
			}
			if env.Error.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.wantMessage)
			}
			// The Retry-After header value must parse to the advertised seconds.
			parsed, err := strconv.Atoi(rec.Header().Get("Retry-After"))
			if err != nil || parsed <= 0 {
				t.Errorf("Retry-After = %q is not a positive integer", rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestHandlerLoginBareLockedOutFallsBackToSaneRetry(t *testing.T) {
	// A lockout error that is NOT a *LockoutError (bare sentinel) must still
	// produce a valid 429 with a sane Retry-After default, not seconds=1.
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrLockedOut
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "active@example.com", "password": "geheim123456"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want default 30", got)
	}

	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "too_many_attempts" {
		t.Errorf("code = %q, want too_many_attempts", env.Error.Code)
	}
	if env.Error.Message != "Zu viele Fehlversuche. Bitte warte 30 Sekunden." {
		t.Errorf("message = %q, want 30s fallback microcopy", env.Error.Message)
	}
}

func TestHandlerLoginInvalidJSON(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerLoginInternalError(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, errors.New("db down")
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "a@example.com", "password": "x"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandlerLoginInvalidInput(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrInvalidLoginInput
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "a@example.com", "password": "x"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if env.Error.Message != core.MsgInvalidLoginInput {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgInvalidLoginInput)
	}
}

func TestHandlerLogoutHappyPath(t *testing.T) {
	var gotToken string
	svc := &mockService{
		logoutFunc: func(ctx context.Context, rawToken string) error {
			gotToken = rawToken
			return nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Header.Set("Authorization", "Bearer sesstoken123")
	rec := httptest.NewRecorder()

	h.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if gotToken != "sesstoken123" {
		t.Errorf("logout token = %q, want sesstoken123", gotToken)
	}
}

func TestHandlerLogoutWithoutToken(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (idempotent)", rec.Code)
	}
}

func TestHandlerLoginMFAChallengeReturns200NoToken(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{MFARequired: true}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for mfa_required", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["mfa_required"] != true {
		t.Errorf("mfa_required = %v, want true", wire["mfa_required"])
	}
	if _, present := wire["token"]; present {
		t.Error("challenge response must not carry a token")
	}
}

func TestHandlerLoginMFAChallengeSuccessReturnsToken(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{
				Token: "opaque-token",
				User:  core.LoginUser{ID: "u-1", Email: "max@example.com", DisplayName: "Max", FirstName: "Max", LastName: "Mustermann"},
			}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456", "totp_code": "123456"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var res ports.LoginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Token != "opaque-token" {
		t.Errorf("token = %q, want opaque-token", res.Token)
	}
}

func TestHandlerLoginMFAChallengeFailureReturns401(t *testing.T) {
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrInvalidCredentials
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456", "totp_code": "000000"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_credentials" {
		t.Errorf("code = %q, want invalid_credentials", env.Error.Code)
	}
	// Anti-enumeration: identical microcopy as a wrong password (UX-DR7).
	if env.Error.Message != "E-Mail oder Passwort ist falsch." {
		t.Errorf("message = %q, want anti-enumeration microcopy", env.Error.Message)
	}
}

// authedMFARequest builds a request carrying an authenticated user in context,
// mirroring what the RequireAuth middleware injects.
func authedMFARequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	user := &core.User{ID: "u-1", Email: "max@example.com", State: core.StateActive}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

// authedPasswordRequest builds a change-password request carrying an
// authenticated user in context plus an optional bearer token (what the
// handler passes on for session revocation).
func authedPasswordRequest(body []byte, token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/password/change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	user := &core.User{ID: "u-1", Email: "max@example.com", State: core.StateActive}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

func TestHandlerMFAEnrollRequest(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := authedMFARequest(http.MethodPost, "/mfa/enroll", []byte("{}"))
	rec := httptest.NewRecorder()

	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var res core.MFAEnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Secret == "" {
		t.Error("expected a shared secret in the enroll response")
	}
	if res.URI == "" {
		t.Error("expected a provisioning URI in the enroll response")
	}
}

func TestHandlerMFAEnrollConfirmValid(t *testing.T) {
	var gotUser *core.User
	var gotSecret, gotCode string
	var revokeOtherCalls, revokeAllCalls int
	svc := &mockService{
		confirmFunc: func(ctx context.Context, user *core.User, secret, code string) error {
			gotUser = user
			gotSecret = secret
			gotCode = code
			return nil
		},
		revokeOtherCalls: &revokeOtherCalls,
		revokeAllCalls:   &revokeAllCalls,
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"secret": "SECRETBASE32", "code": "123456"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/enroll", body)
	rec := httptest.NewRecorder()

	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["enabled"] != true {
		t.Errorf("enabled = %v, want true", wire["enabled"])
	}
	if gotUser == nil || gotUser.Email != "max@example.com" {
		t.Errorf("user not passed through, got %+v", gotUser)
	}
	if gotSecret != "SECRETBASE32" || gotCode != "123456" {
		t.Errorf("secret/code not forwarded: %q / %q", gotSecret, gotCode)
	}
	// Enabling MFA revokes ALL sessions (including the current one) so the
	// caller must re-authenticate with the new TOTP code.
	if revokeAllCalls != 1 || revokeOtherCalls != 0 {
		t.Errorf("session revocation on MFA enable: revokeAll=%d (want 1), revokeOther=%d (want 0)",
			revokeAllCalls, revokeOtherCalls)
	}
}

func TestHandlerMFAEnrollConfirmInvalidCodeReturns400(t *testing.T) {
	svc := &mockService{
		confirmFunc: func(ctx context.Context, user *core.User, secret, code string) error {
			return core.ErrTOTPInvalid
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"secret": "SECRETBASE32", "code": "000000"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/enroll", body)
	rec := httptest.NewRecorder()

	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_totp" {
		t.Errorf("code = %q, want invalid_totp", env.Error.Code)
	}
}

func TestHandlerMFAEnrollAlreadyEnabled(t *testing.T) {
	svc := &mockService{
		enrollFunc: func(ctx context.Context, user *core.User) (*core.MFAEnrollResult, error) {
			return nil, core.ErrMFAAlreadyEnabled
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := authedMFARequest(http.MethodPost, "/mfa/enroll", []byte("{}"))
	rec := httptest.NewRecorder()

	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerMFADisableValid(t *testing.T) {
	var gotCode string
	svc := &mockService{
		disableFunc: func(ctx context.Context, user *core.User, code string) error {
			gotCode = code
			return nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"code": "123456"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/disable", body)
	rec := httptest.NewRecorder()

	h.MFADisable(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["enabled"] != false {
		t.Errorf("enabled = %v, want false", wire["enabled"])
	}
	if gotCode != "123456" {
		t.Errorf("code = %q, want 123456", gotCode)
	}
}

func TestHandlerMFADisableInvalidCodeReturns400(t *testing.T) {
	svc := &mockService{
		disableFunc: func(ctx context.Context, user *core.User, code string) error {
			return core.ErrTOTPInvalid
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"code": "000000"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/disable", body)
	rec := httptest.NewRecorder()

	h.MFADisable(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_totp" {
		t.Errorf("code = %q, want invalid_totp", env.Error.Code)
	}
}

func TestHandlerMFADisableNotEnabled(t *testing.T) {
	svc := &mockService{
		disableFunc: func(ctx context.Context, user *core.User, code string) error {
			return core.ErrMFANotEnabled
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"code": "123456"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/disable", body)
	rec := httptest.NewRecorder()

	h.MFADisable(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerMFAUnauthenticated(t *testing.T) {
	// The MFA routes are wrapped in the auth middleware: no valid bearer token
	// must yield a uniform 401.
	svc := &mockService{}
	h := NewHandler(svc, discardLogger(), &rejectingValidator{})

	req := httptest.NewRequest(http.MethodPost, "/mfa/enroll", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()

	r := h.Routes()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the auth middleware", rec.Code)
	}
}

func TestHandlerMFAStatusContract(t *testing.T) {
	// Review finding 1.6-10: GET /api/v1/auth/mfa/status returns
	// {"enabled":true} for an MFA-enabled user and {"enabled":false} for a
	// disabled one, via the real RequireAuth middleware + Routes().
	tests := []struct {
		name    string
		user    *core.User
		want    bool
	}{
		{name: "enabled", user: &core.User{ID: "u-1", Email: "max@example.com", State: core.StateActive, IsMFAEnabled: true}, want: true},
		{name: "disabled", user: &core.User{ID: "u-1", Email: "max@example.com", State: core.StateActive, IsMFAEnabled: false}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&mockService{}, discardLogger(), &stubValidator{user: tt.user})
			req := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			rec := httptest.NewRecorder()

			h.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var wire map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if wire["enabled"] != tt.want {
				t.Errorf("enabled = %v, want %v", wire["enabled"], tt.want)
			}
		})
	}
}

func TestHandlerMFAStatusUnauthenticated(t *testing.T) {
	h := NewHandler(&mockService{}, discardLogger(), &rejectingValidator{})
	req := httptest.NewRequest(http.MethodGet, "/mfa/status", nil)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandlerMFAUnavailableMapsTo503(t *testing.T) {
	// Review finding 1.6-3: encryption-key misconfiguration surfaces as a clear
	// 503 "MFA ist derzeit nicht verfügbar.", not a generic 500.
	svc := &mockService{
		enrollFunc: func(ctx context.Context, user *core.User) (*core.MFAEnrollResult, error) {
			return nil, core.ErrMFAUnavailable
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := authedMFARequest(http.MethodPost, "/mfa/enroll", []byte("{}"))
	rec := httptest.NewRecorder()
	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "mfa_unavailable" {
		t.Errorf("code = %q, want mfa_unavailable", env.Error.Code)
	}
	if env.Error.Message != "MFA ist derzeit nicht verfügbar." {
		t.Errorf("message = %q, want MFA-unavailable microcopy", env.Error.Message)
	}
}

func TestHandlerLoginMFAEnrollmentExpiredReturns400(t *testing.T) {
	svc := &mockService{
		confirmFunc: func(ctx context.Context, user *core.User, secret, code string) error {
			return core.ErrMFAEnrollmentExpired
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"secret": "SECRETBASE32", "code": "123456"}
	body, _ := json.Marshal(payload)
	req := authedMFARequest(http.MethodPost, "/mfa/enroll", body)
	rec := httptest.NewRecorder()

	h.MFAEnroll(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_totp" {
		t.Errorf("code = %q, want invalid_totp", env.Error.Code)
	}
}

func TestHandlerLoginChallengeSuccessLogRequiresMFA(t *testing.T) {
	// Review finding 1.6-11: the "mfa challenge success" log must only fire when
	// MFA was actually involved (user.IsMFAEnabled), not for a spurious
	// totp_code on an account without MFA.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	// No-MFA account sending a spurious totp_code.
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{
				Token: "opaque-token",
				User:  core.LoginUser{ID: "u-1", Email: "max@example.com", IsMFAEnabled: false},
			}, nil
		},
	}
	h := NewHandler(svc, logger, &stubValidator{})
	payload := map[string]string{"email": "max@example.com", "password": "geheim123456", "totp_code": "123456"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(buf.String(), "mfa challenge success") {
		t.Errorf("spurious totp_code must not log mfa challenge success, got %q", buf.String())
	}

	// MFA-enabled account with a valid flow.
	buf.Reset()
	svc2 := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{
				Token: "opaque-token",
				User:  core.LoginUser{ID: "u-1", Email: "max@example.com", IsMFAEnabled: true},
			}, nil
		},
	}
	h2 := NewHandler(svc2, logger, &stubValidator{})
	rec2 := httptest.NewRecorder()
	h2.Login(rec2, httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body)))
	if !strings.Contains(buf.String(), "mfa challenge success") {
		t.Errorf("MFA-enabled login must log mfa challenge success, got %q", buf.String())
	}
}

func TestHandlerLoginFailureLoggingIsUniform(t *testing.T) {
	// Review finding 1.6-4: a failed TOTP challenge logs the SAME uniform
	// "login failed" event as a wrong password — readers cannot distinguish the
	// failing stage.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrInvalidCredentials
		},
	}
	h := NewHandler(svc, logger, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456", "totp_code": "000000"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	logs := buf.String()
	if !strings.Contains(logs, "login failed") {
		t.Errorf("expected a uniform 'login failed' log, got %q", logs)
	}
	if strings.Contains(logs, "mfa challenge failed") {
		t.Errorf("must not log a stage-specific failure, got %q", logs)
	}
}

func TestHandlerLoginMFAUnavailableMapsTo503(t *testing.T) {
	// Review finding 1.6-3: a rotated/missing encryption key during the TOTP
	// step surfaces as a clear 503, not a misleading generic 500.
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return nil, core.ErrMFAUnavailable
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{"email": "max@example.com", "password": "geheim123456", "totp_code": "123456"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "mfa_unavailable" {
		t.Errorf("code = %q, want mfa_unavailable", env.Error.Code)
	}
	if env.Error.Message != "MFA ist derzeit nicht verfügbar." {
		t.Errorf("message = %q, want MFA-unavailable microcopy", env.Error.Message)
	}
}

func TestHandlerChangePasswordHappyPath(t *testing.T) {
	var gotUser *core.User
	var gotInput core.ChangePasswordInput
	var gotToken string
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			gotUser = user
			gotInput = input
			gotToken = rawToken
			return &core.ChangePasswordResult{Message: core.MsgPasswordChanged}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	payload := map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	}
	body, _ := json.Marshal(payload)
	req := authedPasswordRequest(body, "current-session-token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var res core.ChangePasswordResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgPasswordChanged {
		t.Errorf("message = %q, want %q", res.Message, core.MsgPasswordChanged)
	}
	// The authenticated user and the bearer token must be forwarded so the core
	// can revoke OTHER sessions while keeping the current one.
	if gotUser == nil || gotUser.Email != "max@example.com" {
		t.Errorf("user not passed through, got %+v", gotUser)
	}
	if gotInput.CurrentPassword != "geheim123456" || gotInput.NewPassword != "neuespasswort123" || gotInput.NewPasswordConfirm != "neuespasswort123" {
		t.Errorf("input not forwarded, got %+v", gotInput)
	}
	if gotToken != "current-session-token" {
		t.Errorf("raw token = %q, want current-session-token", gotToken)
	}
}

func TestHandlerChangePasswordWrongCurrentReturns400(t *testing.T) {
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrInvalidCurrentPassword
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "falsch",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_current_password" {
		t.Errorf("code = %q, want invalid_current_password", env.Error.Code)
	}
	if env.Error.Message != core.MsgInvalidCurrentPassword {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgInvalidCurrentPassword)
	}
}

func TestHandlerChangePasswordShortPasswordReturns400(t *testing.T) {
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrShortPassword
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "kurz",
		"new_password_confirm": "kurz",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if env.Error.Message != core.MsgShortPassword {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgShortPassword)
	}
}

func TestHandlerChangePasswordMismatchReturns400(t *testing.T) {
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrPasswordMismatch
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "anders123456",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if env.Error.Message != core.MsgPasswordMismatch {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgPasswordMismatch)
	}
}

func TestHandlerChangePasswordInvalidJSON(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := authedPasswordRequest([]byte("{bad"), "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestHandlerChangePasswordInternalErrorReturns500(t *testing.T) {
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, errors.New("db down")
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

func TestHandlerChangePasswordUnauthenticatedReturns401(t *testing.T) {
	// The change-password route is wrapped in the auth middleware: no valid
	// bearer token must yield a uniform 401.
	h := NewHandler(&mockService{}, discardLogger(), &rejectingValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the auth middleware", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestHandlerChangePasswordTooLongReturns400(t *testing.T) {
	// Review finding 1.7-3: an oversized new password (>1024 runes) maps to a
	// 400 invalid_request, not a generic 500.
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrPasswordTooLong
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "x",
		"new_password_confirm": "x",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if env.Error.Message != core.MsgPasswordTooLong {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgPasswordTooLong)
	}
}

func TestHandlerChangePasswordUserNotFoundReturns400(t *testing.T) {
	// Review finding 1.7-10: a referenced user that no longer exists maps to a
	// clear 400, never a generic 500.
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrUserNotFound
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	})
	req := authedPasswordRequest(body, "token")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestHandlerChangePasswordServiceUnauthorizedReturns401(t *testing.T) {
	// Review finding 1.7-2: the core rejects an empty/missing session token
	// with ErrInvalidCredentials; the handler maps it to a uniform 401 (it must
	// never fall through to a 500).
	svc := &mockService{
		changePasswordFunc: func(ctx context.Context, user *core.User, input core.ChangePasswordInput, rawToken string) (*core.ChangePasswordResult, error) {
			return nil, core.ErrInvalidCredentials
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"current_password":     "geheim123456",
		"new_password":         "neuespasswort123",
		"new_password_confirm": "neuespasswort123",
	})
	req := authedPasswordRequest(body, "")
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

// changePasswordRepo is a minimal core.Repository that records the password
// update and audit event without a live DB. It is used to drive the REAL user
// Service through the change-password flow (finding 1.7-12).
type changePasswordRepo struct {
	user    *core.User
	audit   []string
	updated int
	// freshUserOnProfile makes UpdateUserProfile/StagePendingEmail return a
	// FRESH user value (like the postgres adapter's userFromRow) instead of the
	// in-memory pointer, so tests can prove the session snapshot is refreshed.
	freshUserOnProfile bool
}

func (r *changePasswordRepo) CreateRegisteredUser(_ context.Context, email, _, _, _, _ string) (*core.User, error) {
	return nil, nil
}

func (r *changePasswordRepo) GetUserByEmail(_ context.Context, _ string) (*core.User, error) {
	return r.user, nil
}

func (r *changePasswordRepo) ListPermissionsByUser(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (r *changePasswordRepo) GetLoginAttempts(_ context.Context, _ string) (*core.LoginAttempts, error) {
	return nil, nil
}

func (r *changePasswordRepo) IncrementLoginAttempts(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) ClearLoginAttempts(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) SetUserTotpSecret(_ context.Context, _, _ string) error { return nil }

func (r *changePasswordRepo) ClearUserTotpSecret(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) SetUserPendingTotpSecret(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (r *changePasswordRepo) ClearUserPendingTotpSecret(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) UpdateUserPassword(_ context.Context, _ string, passwordHash string) (*core.User, error) {
	r.updated++
	r.user.PasswordHash = passwordHash
	return r.user, nil
}

func (r *changePasswordRepo) UpdateUserProfile(_ context.Context, userID, firstName, lastName, displayName string, attributes map[string]any) (*core.User, error) {
	r.user.FirstName = firstName
	r.user.LastName = lastName
	r.user.DisplayName = displayName
	r.user.Attributes = attributes
	if r.freshUserOnProfile {
		clone := *r.user
		return &clone, nil
	}
	return r.user, nil
}

func (r *changePasswordRepo) StagePendingEmail(_ context.Context, _ string, pendingEmail string) (*core.User, error) {
	r.user.PendingEmail = pendingEmail
	if r.freshUserOnProfile {
		clone := *r.user
		return &clone, nil
	}
	return r.user, nil
}

func (r *changePasswordRepo) ClearPendingEmail(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) CreatePasswordResetToken(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (r *changePasswordRepo) ConsumePasswordResetToken(_ context.Context, _ string) (*core.PasswordResetToken, error) {
	return nil, core.ErrResetTokenInvalid
}

func (r *changePasswordRepo) DeleteExpiredPasswordResetTokens(_ context.Context, _ string) error {
	return nil
}

func (r *changePasswordRepo) InsertAuditEventAnonymous(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) DeletePasswordResetToken(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) SetUserMustChangePassword(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) ClearUserMustChangePassword(_ context.Context, _ string) error { return nil }

func (r *changePasswordRepo) IsUserInPermissionGroup(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (r *changePasswordRepo) CountActiveAdmins(_ context.Context) (int, error) { return 0, nil }

func (r *changePasswordRepo) CreateAdminRecoveryRequest(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}

func (r *changePasswordRepo) ApproveAdminRecovery(_ context.Context, _, _, _ string) (string, error) {
	return "", core.ErrAdminRecoveryInvalid
}

func (r *changePasswordRepo) ConsumeAdminRecoveryToken(_ context.Context, _ string) (*core.AdminRecoveryToken, error) {
	return nil, core.ErrAdminRecoveryInvalid
}

func (r *changePasswordRepo) ListAdminRecoveryRequest(_ context.Context) ([]*core.AdminRecoveryRequest, error) {
	return nil, nil
}

func (r *changePasswordRepo) DenyAdminRecovery(_ context.Context, _ string) error {
	return nil
}

func (r *changePasswordRepo) InsertAuditEvent(_ context.Context, _ string, operation, _, _ string) error {
	r.audit = append(r.audit, operation)
	return nil
}

// changePasswordSessionStore is an in-memory SessionStore driving the real
// SessionManager (and thus the real RequireAuth resolution) without a DB.
type changePasswordSessionStore struct {
	sessions map[string]*core.Session
	users    map[string]*core.User
	nextID   int
}

func newChangePasswordSessionStore() *changePasswordSessionStore {
	return &changePasswordSessionStore{sessions: make(map[string]*core.Session)}
}

func (m *changePasswordSessionStore) CreateSession(_ context.Context, userID, tokenHash string, expiresAt time.Time) (*core.Session, error) {
	m.nextID++
	s := &core.Session{
		ID:        fmt.Sprintf("sess-%d", m.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
		User:      m.users[userID],
	}
	m.sessions[tokenHash] = s
	return s, nil
}

func (m *changePasswordSessionStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*core.Session, error) {
	s, ok := m.sessions[tokenHash]
	if !ok {
		return nil, core.ErrSessionNotFound
	}
	return s, nil
}

func (m *changePasswordSessionStore) DeleteSessionByTokenHash(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *changePasswordSessionStore) DeleteSessionsByUser(_ context.Context, userID string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

func (m *changePasswordSessionStore) DeleteSessionsByUserExcept(_ context.Context, userID, exceptTokenHash string) error {
	for tokenHash, s := range m.sessions {
		if s.UserID == userID && tokenHash != exceptTokenHash {
			delete(m.sessions, tokenHash)
		}
	}
	return nil
}

// RefreshSessionUser replaces the user snapshot on every session of the given
// user so a subsequent Validate returns the fresh profile (Story 2.1). This
// store caches the snapshot strictly (GetSessionByTokenHash returns it without
// re-reading), so the refresh is the ONLY way a profile edit lands in the
// session — letting the handler chain test prove the stale-snapshot fix.
func (m *changePasswordSessionStore) RefreshSessionUser(_ context.Context, user *core.User) error {
	for _, s := range m.sessions {
		if s.UserID == user.ID {
			s.User = user
		}
	}
	return nil
}

// TestHandlerChangePasswordRealSessionManagerChain pins the FR-25 current-password
// check through the REAL session-resolution path (finding 1.7-12): RequireAuth
// resolves the user via a real SessionManager over an in-memory SessionStore,
// the session user snapshot carries the PasswordHash, and the real user Service
// + real Argon2id hasher verify the current password — all without a live DB.
func TestHandlerChangePasswordRealSessionManagerChain(t *testing.T) {
	hash, err := crypto.HashPassword("geheim123456")
	if err != nil {
		t.Fatalf("hashing current password failed: %v", err)
	}
	user := &core.User{
		ID:           "u-real",
		Email:        "real@example.com",
		State:        core.StateActive,
		PasswordHash: hash,
	}

	store := newChangePasswordSessionStore()
	store.users = map[string]*core.User{user.ID: user}
	sm := core.NewSessionManager(store, time.Hour)
	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	repo := &changePasswordRepo{user: user}
	svc := core.NewService(repo, crypto.NewHasher(), sm, nil, discardLogger())
	h := NewHandler(svc, discardLogger(), sm)

	post := func(current string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{
			"current_password":     current,
			"new_password":         "neuespasswort123",
			"new_password_confirm": "neuespasswort123",
		})
		req := httptest.NewRequest(http.MethodPost, "/password/change", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)
		return rec
	}

	// Correct current password → 200 with the confirmation.
	rec := post("geheim123456")
	if rec.Code != http.StatusOK {
		t.Fatalf("correct current: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.ChangePasswordResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgPasswordChanged {
		t.Errorf("message = %q, want %q", res.Message, core.MsgPasswordChanged)
	}
	if !res.SessionsRevoked {
		t.Error("sessions_revoked must be true through the real chain")
	}
	if repo.updated != 1 {
		t.Errorf("UpdateUserPassword calls = %d, want 1", repo.updated)
	}
	if len(repo.audit) != 1 || repo.audit[0] != core.AuditOperationPasswordChange {
		t.Errorf("audit = %v, want [password.change]", repo.audit)
	}

	// Wrong current password → 400 invalid_current_password.
	rec = post("falsches-altes-passwort")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_current_password" {
		t.Errorf("code = %q, want invalid_current_password", env.Error.Code)
	}
	if repo.updated != 1 {
		t.Errorf("UpdateUserPassword calls = %d, want still 1 after a rejected change", repo.updated)
	}
}

// authedProfileRequest builds a profile request carrying an authenticated user
// in context, mirroring what the RequireAuth middleware injects (Story 2.1).
func authedProfileRequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	user := &core.User{ID: "u-1", Email: "max@example.com", FirstName: "Max", LastName: "Mustermann", DisplayName: "Max Mustermann", State: core.StateActive}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

func TestHandlerGetProfileHappyPath(t *testing.T) {
	// PROFILE_VIEW: an authenticated caller receives their base data incl.
	// pending_email with 200.
	svc := &mockService{
		getProfileFunc: func(ctx context.Context, user *core.User) (*core.Profile, error) {
			return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: "Max", LastName: "Mustermann", DisplayName: "Max Mustermann", PendingEmail: "neu@example.com"}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := authedProfileRequest(http.MethodGet, "/profile", nil)
	rec := httptest.NewRecorder()

	h.GetProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var res core.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ID != "u-1" || res.Email != "max@example.com" {
		t.Errorf("profile = %+v, want id/email (u-1, max@example.com)", res)
	}
	if res.FirstName != "Max" || res.LastName != "Mustermann" || res.DisplayName != "Max Mustermann" {
		t.Errorf("profile names = (%q,%q,%q)", res.FirstName, res.LastName, res.DisplayName)
	}
	if res.PendingEmail != "neu@example.com" {
		t.Errorf("pending_email = %q, want neu@example.com", res.PendingEmail)
	}
}

func TestHandlerGetProfileUnauthenticatedReturns401(t *testing.T) {
	// UNAUTHENTICATED: the profile route is wrapped in the auth middleware; a
	// missing bearer token yields a uniform 401.
	h := NewHandler(&mockService{}, discardLogger(), &rejectingValidator{})
	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestHandlerUpdateProfileHappyPath(t *testing.T) {
	// PROFILE_UPDATE: edits to Vorname/Nachname/Anzeigename are forwarded and
	// the updated profile is returned with 200.
	var gotUser *core.User
	var gotInput core.UpdateProfileInput
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			gotUser = user
			gotInput = input
			return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: input.FirstName, LastName: input.LastName, DisplayName: input.DisplayName}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"first_name":   "Erika",
		"last_name":    "Musterfrau",
		"display_name": "Erika",
	})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()

	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.FirstName != "Erika" || res.LastName != "Musterfrau" || res.DisplayName != "Erika" {
		t.Errorf("profile = %+v, want updated names", res)
	}
	if gotUser == nil || gotUser.Email != "max@example.com" {
		t.Errorf("authenticated user not forwarded, got %+v", gotUser)
	}
	if gotInput.FirstName != "Erika" || gotInput.LastName != "Musterfrau" || gotInput.DisplayName != "Erika" {
		t.Errorf("input not forwarded, got %+v", gotInput)
	}
}

func TestHandlerUpdateProfileValidationReturns400(t *testing.T) {
	// PROFILE_UPDATE validation: missing fields and over-long names map to a
	// uniform 400 invalid_request.
	tests := []struct {
		name        string
		serviceErr  error
		wantMessage string
	}{
		{name: "missing fields", serviceErr: core.ErrMissingFields, wantMessage: core.MsgMissingFields},
		{name: "name too long", serviceErr: core.ErrProfileNameTooLong, wantMessage: core.MsgProfileNameTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
					return nil, tt.serviceErr
				},
			}
			h := newTestHandler(svc, &stubValidator{})

			body, _ := json.Marshal(map[string]string{"first_name": "", "last_name": "", "display_name": ""})
			req := authedProfileRequest(http.MethodPost, "/profile", body)
			rec := httptest.NewRecorder()

			h.UpdateProfile(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", env.Error.Code)
			}
			if env.Error.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.wantMessage)
			}
		})
	}
}

func TestHandlerUpdateProfileForbiddenReturns403(t *testing.T) {
	// NOT_FOUND / self-ownership (AD-12): acting on another user's profile maps
	// to a uniform 403 forbidden.
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			return nil, core.ErrForbidden
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"first_name": "Erika", "last_name": "Musterfrau", "display_name": "Erika"})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()

	h.UpdateProfile(rec, req)

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

func TestHandlerUpdateProfileUnauthenticatedReturns401(t *testing.T) {
	// UNAUTHENTICATED: POST /api/v1/auth/profile is wrapped in the auth
	// middleware; a missing bearer token yields a uniform 401.
	h := NewHandler(&mockService{}, discardLogger(), &rejectingValidator{})
	body, _ := json.Marshal(map[string]string{"first_name": "Erika", "last_name": "Musterfrau", "display_name": "Erika"})
	req := httptest.NewRequest(http.MethodPost, "/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestHandlerStageEmailChangeHappyPath(t *testing.T) {
	// EMAIL_STAGE: a valid new email is staged and the German confirmation is
	// returned with 200.
	var gotUser *core.User
	var gotEmail string
	svc := &mockService{
		stageEmailFunc: func(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error) {
			gotUser = user
			gotEmail = newEmail
			return &core.StageEmailResult{Message: core.MsgEmailChangeStaged, PendingEmail: newEmail}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "neu@example.com"})
	req := authedProfileRequest(http.MethodPost, "/profile/email", body)
	rec := httptest.NewRecorder()

	h.StageEmailChange(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.StageEmailResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgEmailChangeStaged {
		t.Errorf("message = %q, want %q", res.Message, core.MsgEmailChangeStaged)
	}
	if res.PendingEmail != "neu@example.com" {
		t.Errorf("pending_email = %q, want neu@example.com", res.PendingEmail)
	}
	if gotUser == nil || gotUser.Email != "max@example.com" {
		t.Errorf("authenticated user not forwarded, got %+v", gotUser)
	}
	if gotEmail != "neu@example.com" {
		t.Errorf("email not forwarded, got %q", gotEmail)
	}
}

func TestHandlerStageEmailChangeErrorsReturn400(t *testing.T) {
	// EMAIL_STAGE_INVALID / EMAIL_STAGE_SAME / EMAIL_STAGE_DUPLICATE: all map
	// to a uniform 400 invalid_request with the matching German microcopy.
	tests := []struct {
		name        string
		serviceErr  error
		wantMessage string
	}{
		{name: "invalid email", serviceErr: core.ErrInvalidEmail, wantMessage: core.MsgInvalidEmail},
		{name: "same as current", serviceErr: core.ErrEmailUnchanged, wantMessage: core.MsgEmailUnchanged},
		{name: "already pending", serviceErr: core.ErrEmailAlreadyPending, wantMessage: core.MsgEmailAlreadyPending},
		{name: "duplicate email", serviceErr: core.ErrEmailInUse, wantMessage: core.MsgEmailInUse},
		{name: "user not found", serviceErr: core.ErrUserNotFound, wantMessage: "Das Konto wurde nicht gefunden."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				stageEmailFunc: func(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error) {
					return nil, tt.serviceErr
				},
			}
			h := newTestHandler(svc, &stubValidator{})

			body, _ := json.Marshal(map[string]string{"email": "neu@example.com"})
			req := authedProfileRequest(http.MethodPost, "/profile/email", body)
			rec := httptest.NewRecorder()

			h.StageEmailChange(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", env.Error.Code)
			}
			if env.Error.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.wantMessage)
			}
		})
	}
}

func TestHandlerStageEmailChangeForbiddenReturns403(t *testing.T) {
	// Self-ownership (AD-12): staging for another user maps to a uniform 403.
	svc := &mockService{
		stageEmailFunc: func(ctx context.Context, user *core.User, newEmail string) (*core.StageEmailResult, error) {
			return nil, core.ErrForbidden
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "neu@example.com"})
	req := authedProfileRequest(http.MethodPost, "/profile/email", body)
	rec := httptest.NewRecorder()

	h.StageEmailChange(rec, req)

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

func TestHandlerStageEmailChangeUnauthenticatedReturns401(t *testing.T) {
	// UNAUTHENTICATED: POST /api/v1/auth/profile/email is wrapped in the auth
	// middleware; a missing bearer token yields a uniform 401.
	h := NewHandler(&mockService{}, discardLogger(), &rejectingValidator{})
	body, _ := json.Marshal(map[string]string{"email": "neu@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/profile/email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestHandlerProfileInvalidJSONReturns400(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	for _, tc := range []struct {
		target string
		call   func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{target: "/profile", call: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.UpdateProfile(w, r) }},
		{target: "/profile/email", call: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.StageEmailChange(w, r) }},
	} {
		req := authedProfileRequest(http.MethodPost, tc.target, []byte("{bad"))
		rec := httptest.NewRecorder()
		tc.call(h, rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", tc.target, rec.Code)
		}
		var env httpapi.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if env.Error.Code != "invalid_request" {
			t.Errorf("%s: code = %q, want invalid_request", tc.target, env.Error.Code)
		}
	}
}

func TestHandlerProfileInternalErrorReturns500(t *testing.T) {
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			return nil, errors.New("db down")
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"first_name": "Erika", "last_name": "Musterfrau", "display_name": "Erika"})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()

	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

// TestHandlerProfileRealSessionManagerChain drives the REAL user Service over
// an in-memory SessionStore/repo through the profile routes (view → update →
// stage email) via the real RequireAuth middleware, asserting the audit rows
// are written end to end AND that a subsequent GET /profile reflects the edits
// (freshUserOnProfile makes the repo return fresh user values like the postgres
// adapter, and the changePasswordSessionStore caches strictly — so the session
// refresh is what lands the new values, proving the stale-snapshot fix).
func TestHandlerProfileRealSessionManagerChain(t *testing.T) {
	user := &core.User{
		ID:          "u-profile",
		Email:       "profile@example.com",
		FirstName:   "Profil",
		LastName:    "Test",
		DisplayName: "Profil Test",
		State:       core.StateActive,
	}

	store := newChangePasswordSessionStore()
	store.users = map[string]*core.User{user.ID: user}
	sm := core.NewSessionManager(store, time.Hour)
	raw, err := sm.Issue(context.Background(), user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	repo := &changePasswordRepo{user: user, freshUserOnProfile: true}
	svc := core.NewService(repo, crypto.NewHasher(), sm, nil, discardLogger())
	h := NewHandler(svc, discardLogger(), sm)

	do := func(method, target string, payload any) *httptest.ResponseRecorder {
		var body io.Reader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			body = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, target, body)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)
		return rec
	}

	// PROFILE_VIEW via the real chain.
	rec := do(http.MethodGet, "/profile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var profile core.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Email != "profile@example.com" || profile.FirstName != "Profil" {
		t.Errorf("profile = %+v, want base data of the session user", profile)
	}

	// PROFILE_UPDATE via the real chain: 200 + immediate persistence.
	rec = do(http.MethodPost, "/profile", map[string]string{"first_name": "Erika", "last_name": "Musterfrau", "display_name": "Erika"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /profile: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if user.FirstName != "Erika" || user.LastName != "Musterfrau" || user.DisplayName != "Erika" {
		t.Errorf("persisted user = %+v, want immediate base-data update", user)
	}

	// The SAME token now resolves the FRESH names (session snapshot refreshed).
	rec = do(http.MethodGet, "/profile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile after update: status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.FirstName != "Erika" || profile.LastName != "Musterfrau" || profile.DisplayName != "Erika" {
		t.Errorf("GET /profile after update = %+v, want refreshed names", profile)
	}

	// ATTRS via the real chain (Story 1.9): POST attributes, then the SAME
	// token resolves them on GET /profile (session snapshot refreshed with the
	// fresh attributes map).
	rec = do(http.MethodPost, "/profile", map[string]any{
		"first_name":   "Erika",
		"last_name":    "Musterfrau",
		"display_name": "Erika",
		"attributes":   map[string]any{"note": "Interne Notiz"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /profile (attrs): status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if user.Attributes == nil || user.Attributes["note"] != "Interne Notiz" {
		t.Errorf("persisted attributes = %+v, want note=Interne Notiz", user.Attributes)
	}
	rec = do(http.MethodGet, "/profile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile after attrs: status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Attributes == nil || profile.Attributes["note"] != "Interne Notiz" {
		t.Errorf("GET /profile after attrs = %+v, want note=Interne Notiz", profile.Attributes)
	}

	// EMAIL_STAGE via the real chain: 200 + staged pending_email, current email
	// untouched.
	rec = do(http.MethodPost, "/profile/email", map[string]string{"email": "neu@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /profile/email: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if user.PendingEmail != "neu@example.com" {
		t.Errorf("pending_email = %q, want neu@example.com", user.PendingEmail)
	}
	if user.Email != "profile@example.com" {
		t.Errorf("current email = %q, want unchanged profile@example.com", user.Email)
	}

	// The SAME token now resolves pending_email (session snapshot refreshed).
	rec = do(http.MethodGet, "/profile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profile after stage: status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.PendingEmail != "neu@example.com" {
		t.Errorf("GET /profile after stage pending_email = %q, want neu@example.com", profile.PendingEmail)
	}

	// EMAIL_STAGE_SAME via the real chain: 400 no-op.
	rec = do(http.MethodPost, "/profile/email", map[string]string{"email": "profile@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /profile/email (same): status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgEmailUnchanged {
		t.Errorf("same-email error = %+v, want invalid_request %q", env.Error, core.MsgEmailUnchanged)
	}

	// EMAIL_STAGE_ALREADY_PENDING via the real chain: re-staging the staged
	// address is a no-op (400) and writes NO extra audit row.
	rec = do(http.MethodPost, "/profile/email", map[string]string{"email": "neu@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /profile/email (already pending): status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgEmailAlreadyPending {
		t.Errorf("already-pending error = %+v, want invalid_request %q", env.Error, core.MsgEmailAlreadyPending)
	}

	// Audit rows written end to end (NFR-O1/NFR-O2): profile.update +
	// email.change.request must BOTH be present, in any order and any count
	// (set-based assertion, review finding: future audit writes must not break
	// this test). The re-stage no-op writes NO additional email.change.request.
	present := map[string]bool{}
	for _, op := range repo.audit {
		present[op] = true
	}
	for _, op := range []string{core.AuditOperationProfileUpdate, core.AuditOperationEmailChangeRequest} {
		if !present[op] {
			t.Errorf("audit missing %q, got %v", op, repo.audit)
		}
	}
}

func TestHandlerForgotPasswordUniformConfirmation(t *testing.T) {
	// FR-26: POST /api/v1/auth/password/forgot ALWAYS returns the uniform
	// anti-enumeration confirmation — the handler passes the service result
	// through unchanged (the service is responsible for the invariant).
	var gotEmail string
	svc := &mockService{
		requestResetFunc: func(ctx context.Context, email string) (*core.ResetRequestResult, error) {
			gotEmail = email
			return &core.ResetRequestResult{Message: core.MsgPasswordResetRequested}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "user@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/password/forgot", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ForgotPassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.ResetRequestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgPasswordResetRequested {
		t.Errorf("message = %q, want %q", res.Message, core.MsgPasswordResetRequested)
	}
	if gotEmail != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", gotEmail)
	}
}

func TestHandlerForgotPasswordInvalidJSONReturns400(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/password/forgot", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()
	h.ForgotPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestHandlerForgotPasswordThrottledReturns429(t *testing.T) {
	// Review finding 1.8-2: a throttled forgot request maps to a uniform 429
	// (too_many_attempts), never a 200/500.
	svc := &mockService{
		requestResetFunc: func(ctx context.Context, email string) (*core.ResetRequestResult, error) {
			return nil, core.ErrForgotThrottled
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "user@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/password/forgot", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ForgotPassword(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "too_many_attempts" {
		t.Errorf("code = %q, want too_many_attempts", env.Error.Code)
	}
}

func TestHandlerForgotPasswordInternalErrorReturns500(t *testing.T) {
	svc := &mockService{
		requestResetFunc: func(ctx context.Context, email string) (*core.ResetRequestResult, error) {
			return nil, errors.New("db down")
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "user@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/password/forgot", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ForgotPassword(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandlerResetPasswordHappyPath(t *testing.T) {
	var gotToken, gotNew, gotConfirm string
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			gotToken, gotNew, gotConfirm = rawToken, newPassword, confirm
			return &core.ResetCompleteResult{Message: core.MsgPasswordResetComplete}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "opaque-reset-token", "new_password": "neuespasswort123", "new_password_confirm": "neuespasswort123",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ResetPassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var res core.ResetCompleteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Message != core.MsgPasswordResetComplete {
		t.Errorf("message = %q, want %q", res.Message, core.MsgPasswordResetComplete)
	}
	if gotToken != "opaque-reset-token" || gotNew != "neuespasswort123" || gotConfirm != "neuespasswort123" {
		t.Errorf("payload not forwarded: (%q,%q,%q)", gotToken, gotNew, gotConfirm)
	}
}

func TestHandlerResetPasswordInvalidTokenReturns400(t *testing.T) {
	// RESET_EXPIRED / RESET_USED: 400 invalid_token with German microcopy.
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			return nil, core.ErrResetTokenInvalid
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "expired", "new_password": "neuespasswort123", "new_password_confirm": "neuespasswort123",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

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
	if env.Error.Message != core.MsgResetTokenInvalid {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgResetTokenInvalid)
	}
}

func TestHandlerResetPasswordShortPasswordReturns400(t *testing.T) {
	// RESET_SHORT_PW: 400 invalid_request with the FR-2 microcopy.
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			return nil, core.ErrShortPassword
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "opaque", "new_password": "kurz", "new_password_confirm": "kurz",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgShortPassword {
		t.Errorf("error = %+v, want invalid_request %q", env.Error, core.MsgShortPassword)
	}
}

func TestHandlerResetPasswordMismatchReturns400(t *testing.T) {
	// RESET_MISMATCH: 400 invalid_request with the mismatch microcopy.
	svc := &mockService{
		completeResetFunc: func(ctx context.Context, rawToken, newPassword, confirm string) (*core.ResetCompleteResult, error) {
			return nil, core.ErrPasswordMismatch
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"token": "opaque", "new_password": "neuespasswort123", "new_password_confirm": "anders123456",
	})
	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" || env.Error.Message != core.MsgPasswordMismatch {
		t.Errorf("error = %+v, want invalid_request %q", env.Error, core.MsgPasswordMismatch)
	}
}

func TestHandlerResetPasswordInvalidJSONReturns400(t *testing.T) {
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodPost, "/password/reset", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestHandlerLoginMustChangePasswordResponse(t *testing.T) {
	// LOGIN_MUST_CHANGE: no app session is issued; the response carries only the
	// must_change_password marker plus the single-use reset token that drives
	// the forced change flow, and a German note that the admins have been
	// notified (review finding 1.8-4).
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{MustChangePassword: true, ResetToken: "forced-change-raw-token"}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "active@example.com", "password": "geheim123456"})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["must_change_password"] != true {
		t.Errorf("must_change_password = %v, want true", wire["must_change_password"])
	}
	if wire["reset_token"] != "forced-change-raw-token" {
		t.Errorf("reset_token = %v, want forced-change-raw-token", wire["reset_token"])
	}
	if wire["message"] != core.MsgMustChangePassword {
		t.Errorf("message = %v, want %q", wire["message"], core.MsgMustChangePassword)
	}
	if _, present := wire["token"]; present {
		t.Error("forced-change login must not carry an app session token")
	}
	if _, present := wire["user"]; present {
		t.Error("forced-change login must not carry a user snapshot (no session issued)")
	}
}

func TestHandlerLoginIncludesIsAdmin(t *testing.T) {
	// Story 1.8: the login response carries is_admin from LoginUser.
	svc := &mockService{
		loginFunc: func(ctx context.Context, input core.LoginInput) (*ports.LoginResult, error) {
			return &ports.LoginResult{
				Token: "opaque-token",
				User:  core.LoginUser{ID: "u-1", Email: "admin@example.com", DisplayName: "Admin", IsAdmin: true},
			}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{"email": "admin@example.com", "password": "geheim123456"})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	user, ok := wire["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user object, got %T", wire["user"])
	}
	if user["is_admin"] != true {
		t.Errorf("user.is_admin = %v, want true", user["is_admin"])
	}
}

func TestHandlerGetProfileIncludesIsAdmin(t *testing.T) {
	// Story 1.8: GET /profile carries is_admin resolved server-side.
	svc := &mockService{
		getProfileFunc: func(ctx context.Context, user *core.User) (*core.Profile, error) {
			return &core.Profile{ID: "u-1", Email: "admin@example.com", FirstName: "Max", LastName: "Mustermann", DisplayName: "Max", IsAdmin: true}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := authedProfileRequest(http.MethodGet, "/profile", nil)
	rec := httptest.NewRecorder()
	h.GetProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["is_admin"] != true {
		t.Errorf("is_admin = %v, want true", wire["is_admin"])
	}
}

func TestHandlerGetProfileReturnsAttributes(t *testing.T) {
	// Story 1.9: GET /profile serves the custom attributes as valid JSON.
	svc := &mockService{
		getProfileFunc: func(ctx context.Context, user *core.User) (*core.Profile, error) {
			return &core.Profile{
				ID: "u-1", Email: "max@example.com", FirstName: "Max", LastName: "Mustermann",
				DisplayName: "Max Mustermann",
				Attributes:  map[string]any{"note": "Interne Notiz", "internal_tags": []string{"beta"}},
			}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	req := authedProfileRequest(http.MethodGet, "/profile", nil)
	rec := httptest.NewRecorder()
	h.GetProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	attrs, ok := wire["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %T, want an object", wire["attributes"])
	}
	if attrs["note"] != "Interne Notiz" {
		t.Errorf("attributes.note = %v, want Interne Notiz", attrs["note"])
	}
}

func TestHandlerUpdateProfileWithAttributes(t *testing.T) {
	// Story 1.9: POST /profile forwards the attributes payload and returns the
	// updated profile including them.
	var gotInput core.UpdateProfileInput
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			gotInput = input
			return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: input.FirstName, LastName: input.LastName, DisplayName: input.DisplayName, Attributes: input.Attributes}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{
		"first_name":   "Erika",
		"last_name":    "Musterfrau",
		"display_name": "Erika",
		"attributes":   map[string]any{"note": "Interne Notiz"},
	})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotInput.Attributes == nil || gotInput.Attributes["note"] != "Interne Notiz" {
		t.Errorf("input attributes = %+v, want note=Interne Notiz", gotInput.Attributes)
	}
	var res core.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Attributes == nil || res.Attributes["note"] != "Interne Notiz" {
		t.Errorf("response attributes = %+v, want note=Interne Notiz", res.Attributes)
	}
}

func TestHandlerUpdateProfileClearsAttributes(t *testing.T) {
	// Story 1.9: an explicit empty object clears the stored attributes.
	var gotInput core.UpdateProfileInput
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			gotInput = input
			return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: input.FirstName, LastName: input.LastName, DisplayName: input.DisplayName, Attributes: input.Attributes}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]any{
		"first_name":   "Erika",
		"last_name":    "Musterfrau",
		"display_name": "Erika",
		"attributes":   map[string]any{},
	})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotInput.Attributes == nil || len(gotInput.Attributes) != 0 {
		t.Errorf("input attributes = %+v, want empty map", gotInput.Attributes)
	}
}

func TestHandlerUpdateProfileInvalidAttributesReturns400(t *testing.T) {
	// ATTR_BAD_KEY / ATTR_TOO_LARGE / ATTR_INVALID_JSON (Story 1.9): the core
	// rejects the payload with a *AttributeError (unwrapping ErrInvalidAttributes);
	// the handler maps it to a uniform 400 invalid_request and carries the
	// machine-readable `details` (key + reason) in the envelope (review finding).
	tests := []struct {
		name         string
		serviceErr   error
		wantDetails  map[string]any
	}{
		{
			name:       "bad key details",
			serviceErr: &core.AttributeError{Key: "   ", Reason: "empty key"},
			wantDetails: map[string]any{"key": "   ", "reason": "empty key"},
		},
		{
			name:       "empty key omitted",
			serviceErr: &core.AttributeError{Key: "", Reason: "empty key"},
			wantDetails: map[string]any{"reason": "empty key"},
		},
		{
			name:       "oversized details",
			serviceErr: &core.AttributeError{Reason: "attributes too large"},
			wantDetails: map[string]any{"reason": "attributes too large"},
		},
		{
			name:       "bare sentinel fallback",
			serviceErr: core.ErrInvalidAttributes,
			wantDetails: map[string]any{"reason": "invalid attributes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
					return nil, tt.serviceErr
				},
			}
			h := newTestHandler(svc, &stubValidator{})

			body, _ := json.Marshal(map[string]any{
				"first_name":   "Erika",
				"last_name":    "Musterfrau",
				"display_name": "Erika",
				"attributes":   map[string]any{"": "wert"},
			})
			req := authedProfileRequest(http.MethodPost, "/profile", body)
			rec := httptest.NewRecorder()
			h.UpdateProfile(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", env.Error.Code)
			}
			if env.Error.Message != core.MsgInvalidAttributes {
				t.Errorf("message = %q, want %q", env.Error.Message, core.MsgInvalidAttributes)
			}
			details, ok := env.Error.Details.(map[string]any)
			if !ok {
				t.Fatalf("details = %T, want a details object", env.Error.Details)
			}
			for k, wantV := range tt.wantDetails {
				if gotV, ok := details[k]; !ok || gotV != wantV {
					t.Errorf("details[%q] = %v, want %v (details=%v)", k, gotV, wantV, details)
				}
			}
		})
	}
}

func TestHandlerUpdateProfileNameOnlySendsNilAttributes(t *testing.T) {
	// ATTR_ABSENT (Story 1.9, review finding): a name-only save (no attributes
	// field) must reach the service with a nil Attributes, so the core applies
	// "leave unchanged" instead of clearing stored custom attributes.
	var gotInput core.UpdateProfileInput
	svc := &mockService{
		updateProfileFunc: func(ctx context.Context, user *core.User, input core.UpdateProfileInput) (*core.Profile, error) {
			gotInput = input
			return &core.Profile{ID: "u-1", Email: "max@example.com", FirstName: input.FirstName, LastName: input.LastName, DisplayName: input.DisplayName}, nil
		},
	}
	h := newTestHandler(svc, &stubValidator{})

	body, _ := json.Marshal(map[string]string{
		"first_name":   "Erika",
		"last_name":    "Musterfrau",
		"display_name": "Erika",
	})
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotInput.Attributes != nil {
		t.Errorf("input attributes = %+v, want nil (leave-unchanged semantics)", gotInput.Attributes)
	}
}

func TestHandlerUpdateProfileNonObjectAttributesReturns400(t *testing.T) {
	// ATTR_NOT_OBJECT (Story 1.9): an array/scalar attributes payload cannot
	// decode into the typed map field, so the HTTP boundary rejects the body
	// with a uniform 400 invalid_request — no change is made.
	svc := &mockService{}
	h := newTestHandler(svc, &stubValidator{})

	body := []byte(`{"first_name":"Erika","last_name":"Musterfrau","display_name":"Erika","attributes":[1,2]}`)
	req := authedProfileRequest(http.MethodPost, "/profile", body)
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}
