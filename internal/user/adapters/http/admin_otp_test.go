package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/saskia-peters/gear/internal/user/core"
)

// otpSvc returns a mock service whose caller resolves all `users.*` codes (so
// the users sub-mount gate passes) with the given issuance behaviour.
func otpSvc(issueFunc func(ctx context.Context, userID string, confirmed bool) (*core.OneTimePasswordResult, error)) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{
			core.UserViewPermission, core.UserApprovePermission, core.UserManagePermission,
		}, nil
	}
	svc.issueOneTimePasswordFunc = func(_ context.Context, _ *core.User, userID string, confirmed bool) (*core.OneTimePasswordResult, error) {
		return issueFunc(context.Background(), userID, confirmed)
	}
	return svc
}

func TestIssueOneTimePasswordOK(t *testing.T) {
	// ISSUE_OK_ACTIVE: a confirmed issuance returns 200 with the plaintext OTP
	// ONCE plus the server-authoritative German message, the target identity and
	// the expiry.
	svc := otpSvc(func(_ context.Context, userID string, _ bool) (*core.OneTimePasswordResult, error) {
		return &core.OneTimePasswordResult{
			Message:         core.MsgOneTimePasswordIssued,
			UserID:          userID,
			Email:           "tim@gear.local",
			OneTimePassword: "ABC2345678",
			ExpiresAt:       core.OneTimePasswordResult{}.ExpiresAt,
		}, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message         string `json:"message"`
		UserID          string `json:"user_id"`
		Email           string `json:"email"`
		OneTimePassword string `json:"one_time_password"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding response failed: %v", err)
	}
	if wire.Message != core.MsgOneTimePasswordIssued {
		t.Errorf("message = %q, want the server message", wire.Message)
	}
	if wire.UserID != "u-tim" || wire.Email != "tim@gear.local" {
		t.Errorf("identity = %q/%q, want u-tim/tim@gear.local", wire.UserID, wire.Email)
	}
	if wire.OneTimePassword != "ABC2345678" {
		t.Errorf("one_time_password = %q, want the plaintext OTP exactly once", wire.OneTimePassword)
	}
}

func TestIssueOneTimePasswordNotConfirmed(t *testing.T) {
	// ISSUE_NOT_CONFIRMED: a missing/false confirmation maps to the uniform 400.
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		return nil, core.ErrOneTimePasswordNotConfirmed
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{"confirmed":false}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIssueOneTimePasswordTargetNotEligible(t *testing.T) {
	// ISSUE_DEACTIVATED / ISSUE_PENDING: a non-active target maps to the uniform
	// 409 conflict (OTPs are active-only).
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		return nil, core.ErrOneTimePasswordTargetNotEligible
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIssueOneTimePasswordNotFound(t *testing.T) {
	// ISSUE_UNKNOWN: an unknown/malformed id maps to the uniform 404.
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		return nil, core.ErrAdminUserNotFound
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-nope/otp", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIssueOneTimePasswordForbidden(t *testing.T) {
	// ISSUE_FORBIDDEN: the core re-verifies users.manage → uniform 403 for a
	// holder without it.
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		return nil, core.ErrForbidden
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIssueOneTimePasswordMalformedJSON(t *testing.T) {
	// Malformed JSON maps to the uniform 400 invalid_request and the service is
	// never consulted.
	var called bool
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		called = true
		return &core.OneTimePasswordResult{}, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{not json`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("service must not be called for malformed JSON")
	}
}

func TestIssueOneTimePasswordEmptyBody(t *testing.T) {
	// ISSUE_NOT_CONFIRMED boundary: an empty `{}` body decodes with confirmed
	// missing (false) — the service is called with confirmed=false and answers
	// the uniform 400, never a credential.
	var gotConfirmed *bool
	svc := otpSvc(func(_ context.Context, _ string, confirmed bool) (*core.OneTimePasswordResult, error) {
		gotConfirmed = &confirmed
		return nil, core.ErrOneTimePasswordNotConfirmed
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "token", []byte(`{}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if gotConfirmed == nil {
		t.Fatal("service was not called for an empty body")
	}
	if *gotConfirmed {
		t.Error("a missing confirmation field must arrive as false (never a silent success)")
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 envelope failed: %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestIssueOneTimePasswordUnauthorized(t *testing.T) {
	// A missing token is rejected by the users sub-mount gate with the uniform
	// 401 before the service is consulted.
	svc := otpSvc(func(_ context.Context, _ string, _ bool) (*core.OneTimePasswordResult, error) {
		return &core.OneTimePasswordResult{}, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/otp", "", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}