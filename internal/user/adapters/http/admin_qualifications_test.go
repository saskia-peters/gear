package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
	"github.com/saskia-peters/gear/internal/user/ports"
)

// newAdminQualificationsSurface mounts the REAL AdminRoutes() WITHOUT the outer
// composition-root gateway — the qualifications sub-mount carries its OWN
// `qualifications.manage` RequireAdminPermission gate (AD-6/FR-19), which is
// exactly what this suite exercises. The mock service resolves permissions via
// the resolver adapter the same way the real service would.
func newAdminQualificationsSurface(t *testing.T, svc ports.Service, validator auth.SessionValidator) http.Handler {
	t.Helper()
	h := newTestHandler(svc, validator)
	return h.AdminRoutes()
}

func doAdminQualifications(method, path, token string, body []byte, h http.Handler) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// qualificationsSvc returns a mock service that resolves `qualifications.manage`
// for the caller (so the inner gate passes) and wires the given handlers.
func qualificationsSvc(
	listRes *core.QualificationListResult,
	createFunc func(context.Context, core.CreateQualificationInput) (*core.QualificationWriteResult, error),
	updateFunc func(context.Context, string, core.UpdateQualificationInput) (*core.QualificationWriteResult, error),
	assigneesFunc func(context.Context, string) ([]*core.QualificationAssignee, error),
	assignFunc func(context.Context, string, []string) (*core.QualificationAssignResult, error),
) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.QualificationsManagePermission}, nil
	}
	svc.listQualificationsFunc = func(_ context.Context, _ *core.User) (*core.QualificationListResult, error) {
		return listRes, nil
	}
	if createFunc != nil {
		svc.createQualificationFunc = func(_ context.Context, _ *core.User, in core.CreateQualificationInput) (*core.QualificationWriteResult, error) {
			return createFunc(context.Background(), in)
		}
	}
	if updateFunc != nil {
		svc.updateQualificationFunc = func(_ context.Context, _ *core.User, id string, in core.UpdateQualificationInput) (*core.QualificationWriteResult, error) {
			return updateFunc(context.Background(), id, in)
		}
	}
	if assigneesFunc != nil {
		svc.listQualificationAssigneesFunc = func(_ context.Context, _ *core.User, id string) ([]*core.QualificationAssignee, error) {
			return assigneesFunc(context.Background(), id)
		}
	}
	if assignFunc != nil {
		svc.assignQualificationUsersFunc = func(_ context.Context, _ *core.User, id string, userIDs []string) (*core.QualificationAssignResult, error) {
			return assignFunc(context.Background(), id, userIDs)
		}
	}
	return svc
}

func TestListAdminQualificationsOK(t *testing.T) {
	// LIST_QUALS: an admin holding qualifications.manage gets the vocabulary
	// with status indicators plus the user roster (CLIENT_LIST round-trip).
	svc := qualificationsSvc(&core.QualificationListResult{
		Qualifications: []*core.QualificationWithStatus{
			{ID: "q-1", Name: "Kettensäge", Description: "Führerschein", ExpiryKind: "fixed", Status: core.QualificationStatusExpiringSoon},
			{ID: "q-2", Name: "Erste Hilfe", ExpiryKind: "unlimited", Status: core.QualificationStatusUnlimited},
		},
		Users: []*core.QualificationRosterUser{{ID: "u-1", Name: "Frei Willig"}},
	}, nil, nil, nil, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodGet, "/qualifications", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Qualifications []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			ExpiryKind string `json:"expiry_kind"`
			Status     string `json:"status"`
		} `json:"qualifications"`
		Users []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding list response failed: %v", err)
	}
	if len(raw.Qualifications) != 2 {
		t.Fatalf("qualification count = %d, want 2", len(raw.Qualifications))
	}
	if raw.Qualifications[0].Status != core.QualificationStatusExpiringSoon || raw.Qualifications[1].Status != core.QualificationStatusUnlimited {
		t.Errorf("statuses = %s, want the server-derived indicators", rec.Body.String())
	}
	if len(raw.Users) != 1 || raw.Users[0].Name != "Frei Willig" {
		t.Errorf("roster = %+v, want the user roster", raw.Users)
	}
}

func TestListAdminQualificationsForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller without qualifications.manage is denied by the
	// sub-mount's own gate with the uniform hidden-existence 403 (FR-19).
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"tools.manage", "tool_types.manage"}, nil
	}
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodGet, "/qualifications", "token", nil, h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestCreateAdminQualificationOK(t *testing.T) {
	// CREATE_FIXED: a valid create answers 201 with the server-authoritative
	// confirmation plus the created qualification (finding: the SPA never
	// hardcodes its own success text).
	svc := qualificationsSvc(nil, func(_ context.Context, in core.CreateQualificationInput) (*core.QualificationWriteResult, error) {
		return &core.QualificationWriteResult{Message: core.MsgQualificationCreated, Qualification: &core.QualificationWithStatus{ID: "q-9", Name: in.Name, Description: in.Description, ExpiryKind: in.ExpiryKind, Status: core.QualificationStatusValid}}, nil
	}, nil, nil, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"Seilwinde","description":"Führerschein","expiry_kind":"fixed","expires_at":"2099-01-01T00:00:00Z"}`), h)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Message       string `json:"message"`
		Qualification struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"qualification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding create response failed: %v", err)
	}
	if raw.Message != core.MsgQualificationCreated {
		t.Errorf("message = %q, want %q", raw.Message, core.MsgQualificationCreated)
	}
	if raw.Qualification.Name != "Seilwinde" {
		t.Errorf("created = %s, want the submitted name", rec.Body.String())
	}
}

func TestCreateAdminQualificationErrors(t *testing.T) {
	// CREATE_DUP_NAME / CREATE_INVALID map to the uniform 409/400 envelope.
	svc := qualificationsSvc(nil, func(_ context.Context, in core.CreateQualificationInput) (*core.QualificationWriteResult, error) {
		switch in.Name {
		case "Kettensäge":
			return nil, core.ErrQualificationNameTaken
		case "":
			return nil, core.ErrQualificationInvalidName
		case "BadKind":
			return nil, core.ErrQualificationInvalidExpiryKind
		case "BadDate":
			return nil, core.ErrQualificationInvalidExpiresAt
		}
		return nil, core.ErrQualificationDescriptionTooLong
	}, nil, nil, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	dup := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"Kettensäge","expiry_kind":"unlimited"}`), h)
	if dup.Code != http.StatusConflict {
		t.Fatalf("dup status = %d, want 409", dup.Code)
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(dup.Body.Bytes(), &env)
	if env.Error.Code != "conflict" {
		t.Errorf("dup code = %q, want conflict", env.Error.Code)
	}

	empty := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"","expiry_kind":"unlimited"}`), h)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d, want 400", empty.Code)
	}
	badKind := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"BadKind","expiry_kind":"sometimes"}`), h)
	if badKind.Code != http.StatusBadRequest {
		t.Fatalf("bad kind status = %d, want 400", badKind.Code)
	}
	badDate := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"BadDate","expiry_kind":"fixed"}`), h)
	if badDate.Code != http.StatusBadRequest {
		t.Fatalf("bad date status = %d, want 400", badDate.Code)
	}
	// Over-long description → 400 invalid_request with the German message.
	longDesc := doAdminQualifications(http.MethodPost, "/qualifications", "token",
		[]byte(`{"name":"Seilwinde","description":"`+strings.Repeat("x", 501)+`","expiry_kind":"unlimited"}`), h)
	if longDesc.Code != http.StatusBadRequest {
		t.Fatalf("long description status = %d, want 400", longDesc.Code)
	}
	if !strings.Contains(longDesc.Body.String(), core.MsgQualificationDescriptionTooLong) {
		t.Errorf("long description body = %s, want the German too-long message", longDesc.Body.String())
	}
	// Malformed JSON → 400 invalid_request.
	badJSON := doAdminQualifications(http.MethodPost, "/qualifications", "token", []byte(`{`), h)
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d, want 400", badJSON.Code)
	}
}

func TestUpdateAdminQualificationOK(t *testing.T) {
	// UPDATE_VALID: a valid update answers 200 with the server confirmation
	// plus the replaced row.
	svc := qualificationsSvc(nil, nil, func(_ context.Context, id string, in core.UpdateQualificationInput) (*core.QualificationWriteResult, error) {
		return &core.QualificationWriteResult{Message: core.MsgQualificationUpdated, Qualification: &core.QualificationWithStatus{ID: id, Name: in.Name, Description: in.Description, ExpiryKind: in.ExpiryKind, Status: core.QualificationStatusUnlimited}}, nil
	}, nil, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodPut, "/qualifications/q-1", "token",
		[]byte(`{"name":"Erste Hilfe neu","expiry_kind":"unlimited"}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Message       string `json:"message"`
		Qualification struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"qualification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding update response failed: %v", err)
	}
	if raw.Message != core.MsgQualificationUpdated {
		t.Errorf("message = %q, want %q", raw.Message, core.MsgQualificationUpdated)
	}
	if raw.Qualification.ID != "q-1" || raw.Qualification.Name != "Erste Hilfe neu" {
		t.Errorf("updated = %s, want the replaced row", rec.Body.String())
	}
}

func TestUpdateAdminQualificationErrors(t *testing.T) {
	// UPDATE_UNKNOWN / UPDATE_DUP_NAME map to the uniform 404/409 envelope.
	svc := qualificationsSvc(nil, nil, func(_ context.Context, id string, in core.UpdateQualificationInput) (*core.QualificationWriteResult, error) {
		switch in.Name {
		case "Kettensäge":
			return nil, core.ErrQualificationNameTaken
		default:
			return nil, core.ErrQualificationNotFound
		}
	}, nil, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	unknown := doAdminQualifications(http.MethodPut, "/qualifications/q-nope", "token",
		[]byte(`{"name":"X","expiry_kind":"unlimited"}`), h)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, want 404", unknown.Code)
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(unknown.Body.Bytes(), &env)
	if env.Error.Code != "not_found" {
		t.Errorf("unknown code = %q, want not_found", env.Error.Code)
	}

	dup := doAdminQualifications(http.MethodPut, "/qualifications/q-1", "token",
		[]byte(`{"name":"Kettensäge","expiry_kind":"unlimited"}`), h)
	if dup.Code != http.StatusConflict {
		t.Fatalf("dup status = %d, want 409", dup.Code)
	}
}

func TestListAdminQualificationAssigneesOK(t *testing.T) {
	// ASSIGNEE_LIST: the current assignees (id + display name) come back for
	// the editor's pre-checked set.
	svc := qualificationsSvc(nil, nil, nil, func(_ context.Context, id string) ([]*core.QualificationAssignee, error) {
		if id == "q-nope" {
			return nil, core.ErrQualificationNotFound
		}
		return []*core.QualificationAssignee{{ID: "u-1", Name: "Frei Willig"}}, nil
	}, nil)
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodGet, "/qualifications/q-1/assignees", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Assignees []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"assignees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding assignees response failed: %v", err)
	}
	if len(raw.Assignees) != 1 || raw.Assignees[0].ID != "u-1" {
		t.Errorf("assignees = %+v, want the volunteer", raw.Assignees)
	}

	unknown := doAdminQualifications(http.MethodGet, "/qualifications/q-nope/assignees", "token", nil, h)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown assignees status = %d, want 404", unknown.Code)
	}
}

func TestAssignAdminQualificationUsersOK(t *testing.T) {
	// ASSIGN_VALID: replacing the assignee set answers 200 with the German
	// confirmation and the resulting set.
	svc := qualificationsSvc(nil, nil, nil, nil, func(_ context.Context, id string, userIDs []string) (*core.QualificationAssignResult, error) {
		if id == "q-nope" {
			return nil, core.ErrQualificationNotFound
		}
		if len(userIDs) == 1 && userIDs[0] == "u-ghost" {
			return nil, core.ErrQualificationAssigneeUnknown
		}
		assignees := []*core.QualificationAssignee{}
		for _, uid := range userIDs {
			assignees = append(assignees, &core.QualificationAssignee{ID: uid, Name: "Frei Willig"})
		}
		return &core.QualificationAssignResult{Message: core.MsgQualificationAssigneesUpdated, Assignees: assignees}, nil
	})
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodPost, "/qualifications/q-1/assignees", "token",
		[]byte(`{"user_ids":["u-1"]}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Message   string `json:"message"`
		Assignees []struct {
			ID string `json:"id"`
		} `json:"assignees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding assign response failed: %v", err)
	}
	if raw.Message != core.MsgQualificationAssigneesUpdated || len(raw.Assignees) != 1 || raw.Assignees[0].ID != "u-1" {
		t.Errorf("assign = %s, want confirmation + the new set", rec.Body.String())
	}
}

func TestAssignAdminQualificationUsersRemove(t *testing.T) {
	// ASSIGN_REMOVE: an empty set answers 200 and revokes eligibility.
	svc := qualificationsSvc(nil, nil, nil, nil, func(_ context.Context, _ string, _ []string) (*core.QualificationAssignResult, error) {
		return &core.QualificationAssignResult{Message: core.MsgQualificationAssigneesUpdated, Assignees: []*core.QualificationAssignee{}}, nil
	})
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	rec := doAdminQualifications(http.MethodPost, "/qualifications/q-1/assignees", "token",
		[]byte(`{"user_ids":[]}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		Assignees []json.RawMessage `json:"assignees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding assign response failed: %v", err)
	}
	if len(raw.Assignees) != 0 {
		t.Errorf("assignees = %+v, want [] after removal", raw.Assignees)
	}
}

func TestAssignAdminQualificationUsersErrors(t *testing.T) {
	// ASSIGN_UNKNOWN: an unknown qualification → 404, an unknown user → 400.
	svc := qualificationsSvc(nil, nil, nil, nil, func(_ context.Context, id string, userIDs []string) (*core.QualificationAssignResult, error) {
		if id == "q-nope" {
			return nil, core.ErrQualificationNotFound
		}
		return nil, core.ErrQualificationAssigneeUnknown
	})
	h := newAdminQualificationsSurface(t, svc, &stubValidator{})

	unknown := doAdminQualifications(http.MethodPost, "/qualifications/q-nope/assignees", "token",
		[]byte(`{"user_ids":["u-1"]}`), h)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown qual status = %d, want 404", unknown.Code)
	}
	badUser := doAdminQualifications(http.MethodPost, "/qualifications/q-1/assignees", "token",
		[]byte(`{"user_ids":["u-ghost"]}`), h)
	if badUser.Code != http.StatusBadRequest {
		t.Fatalf("unknown user status = %d, want 400", badUser.Code)
	}
	// MISSING_FIELD (finding): a request WITHOUT `user_ids` must never silently
	// revoke everyone — 400 invalid_request with a German message, distinct
	// from an explicit empty array (which is a valid clear-all).
	missing := doAdminQualifications(http.MethodPost, "/qualifications/q-1/assignees", "token", []byte(`{}`), h)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing user_ids status = %d, want 400", missing.Code)
	}
	var missingEnv httpapi.ErrorEnvelope
	_ = json.Unmarshal(missing.Body.Bytes(), &missingEnv)
	if missingEnv.Error.Code != "invalid_request" {
		t.Errorf("missing user_ids code = %q, want invalid_request", missingEnv.Error.Code)
	}
	if !strings.Contains(missing.Body.String(), core.MsgQualificationAssigneeRequired) {
		t.Errorf("missing user_ids body = %s, want the German required message", missing.Body.String())
	}
	// Malformed JSON → 400 invalid_request.
	badJSON := doAdminQualifications(http.MethodPost, "/qualifications/q-1/assignees", "token", []byte(`{`), h)
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d, want 400", badJSON.Code)
	}
}