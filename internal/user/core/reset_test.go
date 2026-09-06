package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockResetSender is an in-memory ResetEmailSender (FR-26/AD-14): it records
// the delivered emails (address + full reset link) and lets tests simulate a
// configured vs NOT-configured delivery path and a send failure.
type mockResetSender struct {
	configured bool
	err        error
	sends      []resetMail
}

type resetMail struct {
	email     string
	resetLink string
}

func (m *mockResetSender) SendPasswordResetEmail(_ context.Context, email, resetLink string) error {
	m.sends = append(m.sends, resetMail{email: email, resetLink: resetLink})
	return m.err
}

func (m *mockResetSender) Configured() bool { return m.configured }

// resetRepo seeds the standard reset I/O matrix accounts: an active account
// (with and without a must-change flag / admin membership) plus pending and
// deactivated accounts.
func resetRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["active@example.com"] = &User{
		ID:           "u-active",
		Email:        "active@example.com",
		DisplayName:  "Aktive Person",
		FirstName:    "Aktive",
		LastName:     "Person",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	repo.users["pending@example.com"] = &User{
		ID:           "u-pending",
		Email:        "pending@example.com",
		PasswordHash: "hashed:geheim123456",
		State:        StatePendingApproval,
	}
	repo.users["deactivated@example.com"] = &User{
		ID:           "u-deactivated",
		Email:        "deactivated@example.com",
		PasswordHash: "hashed:geheim123456",
		State:        StateDeactivated,
	}
	repo.users["volunteer@example.com"] = &User{
		ID:           "u-volunteer",
		Email:        "volunteer@example.com",
		DisplayName:  "Volunteer",
		FirstName:    "Frei",
		LastName:     "Willig",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	// admin@example.com is a DISTINCT active admin (FR-27/Story 1.8): it is
	// used by the IsAdmin-resolution tests. It is deliberately NOT the same as
	// active@example.com (u-active), which the FR-26 reset/must-change tests
	// treat as a regular active user — an admin must never self-reset via the
	// forgot flow.
	repo.users["admin@example.com"] = &User{
		ID:           "u-admin",
		Email:        "admin@example.com",
		DisplayName:  "Admin Person",
		FirstName:    "Admin",
		LastName:     "Person",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	repo.adminGroup["u-admin"] = true
	return repo
}

// resetService builds a Service over the seeded repo with the given sender
// (nil allowed for the no-sender default).
func resetService(t *testing.T, repo *mockRepo, sender ResetEmailSender) (*Service, *mockSessionStore) {
	t.Helper()
	svc, store := newTestService(repo, &mockHasher{})
	if sender != nil {
		svc.SetResetEmailSender(sender)
	}
	return svc, store
}

func TestRequestPasswordResetUnknownEmail(t *testing.T) {
	// FORGOT_UNKNOWN_EMAIL: any email without a matching account returns the
	// uniform confirmation — no token, no flag, no leak. The unknown path still
	// performs comparable-cost work (review findings 1.8-3 / 1.8-10): an
	// anonymous audit row is written (NFR-O1) so enumeration attempts leave a
	// trail.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)

	res, err := svc.RequestPasswordReset(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetRequested {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordResetRequested)
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("unknown email must not mint a token, got %d", len(repo.resetTokens))
	}
	if len(repo.mustChange) != 0 {
		t.Errorf("unknown email must not flag must_change, got %v", repo.mustChange)
	}
	if len(repo.audit["u-active"]) != 0 {
		t.Errorf("unknown email must not write a per-account audit row, got %v", repo.audit)
	}
	if got := repo.audit[""]; len(got) != 1 || got[0] != AuditOperationPasswordResetRequestUnknown {
		t.Errorf("unknown email must write one anonymous audit row, got %v", got)
	}
}

func TestRequestPasswordResetActiveNoSMTP(t *testing.T) {
	// FORGOT_ACTIVE_NO_SMTP: uniform confirmation + must_change_password=true,
	// no email sent (the stub reports NOT configured).
	repo := resetRepo()
	svc, _ := resetService(t, repo, &mockResetSender{configured: false})

	res, err := svc.RequestPasswordReset(context.Background(), "Active@Example.com ")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetRequested {
		t.Errorf("message = %q, want uniform %q", res.Message, MsgPasswordResetRequested)
	}
	if !repo.mustChange["u-active"] {
		t.Error("active account must be flagged must_change_password when SMTP is not configured")
	}
	if !repo.users["active@example.com"].MustChangePassword {
		t.Error("the user entity must carry must_change_password=true")
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("no-SMTP path must not mint a token, got %d", len(repo.resetTokens))
	}
	if got := repo.audit["u-active"]; len(got) != 1 || got[0] != AuditOperationPasswordResetRequest {
		t.Errorf("audit = %v, want [password.reset.request]", got)
	}
}

func TestRequestPasswordResetActiveWithSMTP(t *testing.T) {
	// FORGOT_ACTIVE_WITH_SMTP: uniform confirmation, a single-use hashed 30-min
	// token minted (invalidating earlier ones) and delivery requested via the
	// port with the RAW token exactly once.
	sender := &mockResetSender{configured: true}
	repo := resetRepo()
	svc, _ := resetService(t, repo, sender)

	res, err := svc.RequestPasswordReset(context.Background(), "active@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetRequested {
		t.Errorf("message = %q, want uniform %q", res.Message, MsgPasswordResetRequested)
	}
	if len(sender.sends) != 1 || sender.sends[0].email != "active@example.com" {
		t.Fatalf("sender deliveries = %+v, want one mail to active@example.com", sender.sends)
	}
	link := sender.sends[0].resetLink
	if !strings.Contains(link, "/reset-password/") {
		t.Fatalf("sender must receive a clickable reset link, got %q", link)
	}
	raw := strings.TrimPrefix(link, "/reset-password/")
	if raw == "" {
		t.Fatal("sender must receive a link carrying the raw token")
	}
	if len(repo.resetTokens) != 1 {
		t.Fatalf("expected exactly 1 stored token, got %d", len(repo.resetTokens))
	}
	token := repo.resetTokens[hashToken(raw)]
	if token == nil {
		t.Fatal("stored token hash must match SHA-256 of the delivered raw token")
	}
	if !token.ExpiresAt.After(time.Now().UTC().Add(29 * time.Minute)) {
		t.Errorf("token expiry = %v, want ~now+30min", token.ExpiresAt)
	}
	if repo.mustChange["u-active"] {
		t.Error("configured-SMTP path must NOT flag must_change_password")
	}
	if got := repo.audit["u-active"]; len(got) != 1 || got[0] != AuditOperationPasswordResetRequest {
		t.Errorf("audit = %v, want [password.reset.request]", got)
	}
}

func TestRequestPasswordResetWithSMTPInvalidatesEarlierToken(t *testing.T) {
	// A second request for the same active account invalidates the earlier
	// token (only the latest is valid, FR-26/AD-13).
	sender := &mockResetSender{configured: true}
	repo := resetRepo()
	svc, _ := resetService(t, repo, sender)

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	firstRaw := strings.TrimPrefix(sender.sends[0].resetLink, "/reset-password/")

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	if len(repo.resetTokens) != 1 {
		t.Fatalf("expected exactly 1 stored token after re-request, got %d", len(repo.resetTokens))
	}
	if _, ok := repo.resetTokens[hashToken(firstRaw)]; ok {
		t.Error("the earlier token must be invalidated by the newer request")
	}
	secondRaw := strings.TrimPrefix(sender.sends[1].resetLink, "/reset-password/")
	if secondRaw == firstRaw {
		t.Error("the second delivery must carry a fresh raw token")
	}
}

func TestRequestPasswordResetDeactivatedAndPending(t *testing.T) {
	// FORGOT_DEACTIVATED / FORGOT_PENDING: uniform confirmation, NO actionable
	// reset and no must-change flag — account state never leaks.
	sender := &mockResetSender{configured: true}
	for _, tt := range []struct {
		name  string
		email string
		user  string
	}{
		{name: "deactivated", email: "deactivated@example.com", user: "u-deactivated"},
		{name: "pending", email: "pending@example.com", user: "u-pending"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := resetRepo()
			svc, _ := resetService(t, repo, sender)

			res, err := svc.RequestPasswordReset(context.Background(), tt.email)
			if err != nil {
				t.Fatalf("RequestPasswordReset failed: %v", err)
			}
			if res.Message != MsgPasswordResetRequested {
				t.Errorf("message = %q, want uniform %q", res.Message, MsgPasswordResetRequested)
			}
			if len(repo.resetTokens) != 0 {
				t.Errorf("non-active account must not mint a token, got %d", len(repo.resetTokens))
			}
			if repo.mustChange[tt.user] {
				t.Error("non-active account must not be flagged must_change_password")
			}
			if len(sender.sends) != 0 {
				t.Errorf("no email may be sent for a non-active account, got %+v", sender.sends)
			}
			if got := repo.audit[tt.user]; len(got) != 1 || got[0] != AuditOperationPasswordResetRequest {
				t.Errorf("audit = %v, want [password.reset.request]", got)
			}
		})
	}
}

func TestRequestPasswordResetSendFailureStillUniform(t *testing.T) {
	// FORGOT_ACTIVE_WITH_SMTP send failure: logged (NFR-O1), the user still
	// receives the uniform confirmation. The token stays (it expires on its
	// own); the response never leaks the failure.
	sender := &mockResetSender{configured: true, err: errors.New("smtp timeout")}
	repo := resetRepo()
	svc, _ := resetService(t, repo, sender)

	res, err := svc.RequestPasswordReset(context.Background(), "active@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetRequested {
		t.Errorf("message = %q, want uniform %q", res.Message, MsgPasswordResetRequested)
	}
	if len(repo.resetTokens) != 1 {
		t.Errorf("token must still be minted on send failure, got %d", len(repo.resetTokens))
	}
}

func TestCompletePasswordResetValid(t *testing.T) {
	// RESET_VALID: new password set (Argon2id via hasher), token invalidated,
	// must_change cleared, ALL sessions revoked, completion audited.
	repo := resetRepo()
	svc, store := resetService(t, repo, nil)

	// Flag the account and mint a token exactly like the no-SMTP path + login.
	if err := repo.SetUserMustChangePassword(context.Background(), "u-active"); err != nil {
		t.Fatalf("SetUserMustChangePassword failed: %v", err)
	}
	raw, err := svc.mintResetToken(context.Background(), "u-active")
	if err != nil {
		t.Fatalf("mintResetToken failed: %v", err)
	}
	// Two pre-existing sessions that must be revoked (re-auth required).
	if _, err := store.CreateSession(context.Background(), "u-active", "hash-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(1) failed: %v", err)
	}
	if _, err := store.CreateSession(context.Background(), "u-active", "hash-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(2) failed: %v", err)
	}
	if len(store.sessions) != 2 {
		t.Fatalf("setup: want 2 sessions, got %d", len(store.sessions))
	}

	res, err := svc.CompletePasswordReset(context.Background(), raw, "neuespasswort123", "neuespasswort123")
	if err != nil {
		t.Fatalf("CompletePasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetComplete {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordResetComplete)
	}
	if repo.users["active@example.com"].PasswordHash != "hashed:neuespasswort123" {
		t.Errorf("stored hash = %q, want hashed:neuespasswort123", repo.users["active@example.com"].PasswordHash)
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("used token must be invalidated, got %d", len(repo.resetTokens))
	}
	if repo.users["active@example.com"].MustChangePassword {
		t.Error("must_change_password must be cleared after completion")
	}
	if len(store.sessions) != 0 {
		t.Errorf("all sessions must be revoked, got %d", len(store.sessions))
	}
	if got := repo.audit["u-active"]; len(got) != 1 || got[0] != AuditOperationPasswordResetComplete {
		t.Errorf("audit = %v, want [password.reset.complete]", got)
	}
}

func TestCompletePasswordResetExpired(t *testing.T) {
	// RESET_EXPIRED: a token past its 30-minute window is rejected with
	// ErrResetTokenInvalid and invalidated so it can never be used again.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)

	if err := repo.CreatePasswordResetToken(context.Background(), "u-active", hashToken("old-raw-token"), time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken failed: %v", err)
	}

	_, err := svc.CompletePasswordReset(context.Background(), "old-raw-token", "neuespasswort123", "neuespasswort123")
	if !errors.Is(err, ErrResetTokenInvalid) {
		t.Fatalf("expired token error = %v, want ErrResetTokenInvalid", err)
	}
	if len(repo.resetTokens) != 0 {
		t.Error("expired token must be deleted (invalidated on expiry)")
	}
}

func TestCompletePasswordResetUnknownOrMissingToken(t *testing.T) {
	// RESET_USED / unknown token: ErrResetTokenInvalid, no changes.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)

	if _, err := svc.CompletePasswordReset(context.Background(), "", "neuespasswort123", "neuespasswort123"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("empty token error = %v, want ErrResetTokenInvalid", err)
	}
	if _, err := svc.CompletePasswordReset(context.Background(), "no-such-token", "neuespasswort123", "neuespasswort123"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("unknown token error = %v, want ErrResetTokenInvalid", err)
	}
	if repo.users["active@example.com"].PasswordHash != "hashed:geheim123456" {
		t.Error("no password change may happen for an invalid token")
	}
}

func TestCompletePasswordResetShortPassword(t *testing.T) {
	// RESET_SHORT_PW: a new password under 10 characters is rejected with
	// ErrShortPassword (FR-2) and nothing changes.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)
	raw, err := svc.mintResetToken(context.Background(), "u-active")
	if err != nil {
		t.Fatalf("mintResetToken failed: %v", err)
	}

	_, err = svc.CompletePasswordReset(context.Background(), raw, "kurz", "kurz")
	if !errors.Is(err, ErrShortPassword) {
		t.Fatalf("short password error = %v, want ErrShortPassword", err)
	}
	if repo.users["active@example.com"].PasswordHash != "hashed:geheim123456" {
		t.Error("no password change may happen on policy violation")
	}
	// The token is consumed atomically BEFORE validation (review finding 1.8-5),
	// so a rejected policy violation does NOT leave a reusable token behind.
	if len(repo.resetTokens) != 0 {
		t.Errorf("consumed token must not survive a rejected policy violation, got %d", len(repo.resetTokens))
	}
}

func TestCompletePasswordResetMismatch(t *testing.T) {
	// RESET_MISMATCH: differing new password / repeat is rejected with
	// ErrPasswordMismatch and nothing changes.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)
	raw, err := svc.mintResetToken(context.Background(), "u-active")
	if err != nil {
		t.Fatalf("mintResetToken failed: %v", err)
	}

	_, err = svc.CompletePasswordReset(context.Background(), raw, "neuespasswort123", "anderspasswort456")
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("mismatch error = %v, want ErrPasswordMismatch", err)
	}
	if repo.users["active@example.com"].PasswordHash != "hashed:geheim123456" {
		t.Error("no password change may happen on mismatch")
	}
}

func TestLoginMustChangePasswordIssuesNoSession(t *testing.T) {
	// LOGIN_MUST_CHANGE: credentials validate but NO app session is issued; the
	// response signals must_change_password and carries a single-use reset token
	// the UI uses to force the change flow.
	repo := resetRepo()
	if err := repo.SetUserMustChangePassword(context.Background(), "u-active"); err != nil {
		t.Fatalf("SetUserMustChangePassword failed: %v", err)
	}
	svc, store := resetService(t, repo, nil)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !res.MustChangePassword {
		t.Fatal("login must signal must_change_password")
	}
	if res.Token != "" {
		t.Errorf("no app session may be issued, got token %q", res.Token)
	}
	if res.ResetToken == "" {
		t.Fatal("login must carry a reset token for the forced change flow")
	}
	if len(store.sessions) != 0 {
		t.Errorf("no session may be persisted, got %d", len(store.sessions))
	}
	// The minted token is usable by the forced change flow.
	if _, ok := repo.resetTokens[hashToken(res.ResetToken)]; !ok {
		t.Error("the returned reset token must be stored (hashed) server-side")
	}
}

func TestLoginMustChangePasswordWrongPasswordStillFails(t *testing.T) {
	// The must-change flow still requires valid credentials — a wrong password
	// is rejected identically to every other login failure.
	repo := resetRepo()
	if err := repo.SetUserMustChangePassword(context.Background(), "u-active"); err != nil {
		t.Fatalf("SetUserMustChangePassword failed: %v", err)
	}
	svc, _ := resetService(t, repo, nil)

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "falsch"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginIsAdminResolution(t *testing.T) {
	// Story 1.8: the login response carries is_admin resolved server-side from
	// admin-group membership — true for an admin-group member, false otherwise.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)

	adminRes, err := svc.Login(context.Background(), LoginInput{Email: "admin@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("admin Login failed: %v", err)
	}
	if !adminRes.User.IsAdmin {
		t.Error("admin-group member must resolve IsAdmin=true at login")
	}

	volunteerRes, err := svc.Login(context.Background(), LoginInput{Email: "volunteer@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("volunteer Login failed: %v", err)
	}
	if volunteerRes.User.IsAdmin {
		t.Error("non-admin user must resolve IsAdmin=false at login")
	}
}

func TestGetProfileIsAdmin(t *testing.T) {
	// Story 1.8: GET /profile carries is_admin resolved server-side.
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)

	user := repo.users["admin@example.com"]
	profile, err := svc.GetProfile(context.Background(), user)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if !profile.IsAdmin {
		t.Error("admin-group member must resolve Profile.IsAdmin=true")
	}

	nonAdmin := repo.users["pending@example.com"]
	profile, err = svc.GetProfile(context.Background(), nonAdmin)
	if err != nil {
		t.Fatalf("GetProfile(non-admin) failed: %v", err)
	}
	if profile.IsAdmin {
		t.Error("non-admin user must resolve Profile.IsAdmin=false")
	}
}
func TestResetLinkUsesConfiguredAppOrigin(t *testing.T) {
	// Review finding 1.8-6: the reset link delivered to the sender is built from
	// the configured app origin (GEAR_APP_ORIGIN), so a real SMTP sender can
	// hand the user a clickable link.
	sender := &mockResetSender{configured: true}
	repo := resetRepo()
	svc, _ := resetService(t, repo, sender)
	svc.SetAppOrigin("https://gear.example.com")

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if len(sender.sends) != 1 {
		t.Fatalf("sender deliveries = %d, want 1", len(sender.sends))
	}
	want := "https://gear.example.com/reset-password/"
	if !strings.HasPrefix(sender.sends[0].resetLink, want) {
		t.Errorf("reset link = %q, want prefix %q", sender.sends[0].resetLink, want)
	}
}

func TestRequestPasswordResetThrottled(t *testing.T) {
	// Review finding 1.8-2: the per-email rate gate answers a second forgot
	// request within the window with ErrForgotThrottled (uniform 429 upstream),
	// keyed by the email string REGARDLESS of whether the account exists.
	repo := resetRepo()
	svc, _ := resetService(t, repo, &mockResetSender{configured: false})
	svc.SetForgotThrottleInterval(time.Hour)

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); !errors.Is(err, ErrForgotThrottled) {
		t.Fatalf("second request error = %v, want ErrForgotThrottled", err)
	}
	// A different email is NOT throttled (per-email gate).
	if _, err := svc.RequestPasswordReset(context.Background(), "other@example.com"); err != nil {
		t.Fatalf("different email must not be throttled: %v", err)
	}
}

func TestForgotThrottleAllowBoundaries(t *testing.T) {
	// The gate honors the first request and rejects any request before minGap
	// has elapsed; a nil receiver or zero gap never gates.
	gate := newForgotThrottle(60 * time.Second)
	now := time.Now().UTC()
	if !gate.allow("a@example.com", now) {
		t.Fatal("first request must be allowed")
	}
	if gate.allow("a@example.com", now.Add(59*time.Second)) {
		t.Error("request inside the window must be rejected")
	}
	if !gate.allow("a@example.com", now.Add(60*time.Second)) {
		t.Error("request exactly at the window boundary must be allowed")
	}
	if !gate.allow("b@example.com", now.Add(time.Second)) {
		t.Error("different email must not be gated")
	}

	noGate := newForgotThrottle(0)
	if !noGate.allow("a@example.com", now) || !noGate.allow("a@example.com", now.Add(time.Nanosecond)) {
		t.Error("a zero interval must disable throttling")
	}
	if !(*forgotThrottle)(nil).allow("a@example.com", now) {
		t.Error("a nil throttle must not gate")
	}
}

func TestRequestPasswordResetExpiredTokensPurged(t *testing.T) {
	// Review finding 1.8-7: a reset request lazily purges the user's EXPIRED
	// tokens (fresh ones are invalidated by the mint anyway).
	repo := resetRepo()
	svc, _ := resetService(t, repo, &mockResetSender{configured: false})
	// Seed two EXPIRED tokens directly (the mock's CreatePasswordResetToken
	// invalidates earlier tokens, mirroring the real CTE).
	now := time.Now().UTC()
	repo.resetTokens["hash-expired-1"] = &PasswordResetToken{
		ID: "token-expired-1", UserID: "u-active", TokenHash: "hash-expired-1",
		ExpiresAt: now.Add(-time.Hour), CreatedAt: now.Add(-2 * time.Hour), User: repo.users["active@example.com"],
	}
	repo.resetTokens["hash-expired-2"] = &PasswordResetToken{
		ID: "token-expired-2", UserID: "u-active", TokenHash: "hash-expired-2",
		ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-2 * time.Hour), User: repo.users["active@example.com"],
	}
	if len(repo.resetTokens) != 2 {
		t.Fatalf("setup: want 2 expired tokens, got %d", len(repo.resetTokens))
	}

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("expired tokens must be purged on request, got %d", len(repo.resetTokens))
	}
}

func TestRequestPasswordResetMustChangeRevokesSessions(t *testing.T) {
	// Review finding 1.8-11: when the SMTP-not-configured fallback flags the
	// account, live sessions are revoked so the forced change is enforced
	// immediately (re-login required).
	repo := resetRepo()
	svc, store := resetService(t, repo, &mockResetSender{configured: false})
	if _, err := store.CreateSession(context.Background(), "u-active", "hash-live-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := store.CreateSession(context.Background(), "u-active", "hash-live-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if _, err := svc.RequestPasswordReset(context.Background(), "active@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if !repo.mustChange["u-active"] {
		t.Error("account must be flagged must_change_password")
	}
	if len(store.sessions) != 0 {
		t.Errorf("live sessions must be revoked, got %d", len(store.sessions))
	}
}

func TestCompletePasswordResetTokenSingleUseAtomic(t *testing.T) {
	// Review finding 1.8-5: consuming the token is atomic — the second
	// completion with the SAME raw token sees no row and is rejected (the
	// sequential case here; the postgres integration test covers true
	// concurrency).
	repo := resetRepo()
	svc, _ := resetService(t, repo, nil)
	raw, err := svc.mintResetToken(context.Background(), "u-active")
	if err != nil {
		t.Fatalf("mintResetToken failed: %v", err)
	}

	res, err := svc.CompletePasswordReset(context.Background(), raw, "neuespasswort123", "neuespasswort123")
	if err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if res.Message != MsgPasswordResetComplete {
		t.Errorf("message = %q, want %q", res.Message, MsgPasswordResetComplete)
	}

	if _, err := svc.CompletePasswordReset(context.Background(), raw, "anderespasswort456", "anderespasswort456"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Fatalf("second completion error = %v, want ErrResetTokenInvalid (single-use)", err)
	}
	// The first completion's password stays; the loser never overwrote it.
	if repo.users["active@example.com"].PasswordHash != "hashed:neuespasswort123" {
		t.Errorf("stored hash = %q, want the first completion's hash", repo.users["active@example.com"].PasswordHash)
	}
}
