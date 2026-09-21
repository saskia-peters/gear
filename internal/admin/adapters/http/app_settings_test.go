package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/admin/ports"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// systemGateway wraps the REAL SystemRoutes() behind the same RequireAnyPermission
// gate the composition root uses (admin.settings.system), with a fake session
// validator + permission resolver.
func systemGateway(perms []string, session *usercore.Session, svc ports.Service) http.Handler {
	h := NewHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{core.AppSettingsPermission},
		"admin.settings.system access denied", discardLogger(),
	)(h.SystemRoutes())
}

// fullAppSettings is the 22-row typed projection for the GET/PUT fixtures.
func fullAppSettings() *core.AppSettings {
	return &core.AppSettings{
		SmtpDialTimeout:                 10 * time.Second,
		SmtpProtocolTimeout:             30 * time.Second,
		BackupDialTimeout:               10 * time.Second,
		BackupProtocolTimeout:           10 * time.Second,
		BackupInterval:                  86400 * time.Second,
		PasswordResetTTL:                1800 * time.Second,
		AdminRecoveryTTL:                1800 * time.Second,
		ForgotThrottleInterval:          60 * time.Second,
		OtpTTL:                          900 * time.Second,
		OtpLength:                       10,
		MfaEnrollmentWindow:             600 * time.Second,
		LockoutThresholdShort:           3,
		LockoutThresholdLong:            4,
		LockoutDurationShort:            30 * time.Second,
		LockoutDurationLong:             60 * time.Second,
		LockoutMaxFailedCount:           10,
		AttributeKeyMaxRunes:            64,
		AttributesMaxSize:               16384,
		InventoryPrefix:                 "GEAR",
		InventoryWidth:                  9,
		InspectionOrangeWindowPercent:   25,
		QualificationExpiringSoonWindow: 2592000 * time.Second,
	}
}

func TestSystemSettingsGetAll(t *testing.T) {
	// GET_ALL: 200 with every seeded setting typed — durations as whole seconds,
	// integers as numbers, text as string.
	svc := &fakeService{appSettings: fullAppSettings()}
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if len(body) != 22 {
		t.Fatalf("rows = %d, want 22", len(body))
	}
	byKey := make(map[string]map[string]any, len(body))
	for _, row := range body {
		byKey[row["key"].(string)] = row
	}
	if v, ok := byKey["backup_interval"]; !ok || v["value_type"] != "duration" || v["value"] != float64(86400) {
		t.Errorf("backup_interval row = %v, want duration 86400", v)
	}
	if v, ok := byKey["smtp_dial_timeout"]; !ok || v["value_type"] != "duration" || v["value"] != float64(10) {
		t.Errorf("smtp_dial_timeout row = %v, want duration 10", v)
	}
	// The unit metadata is surfaced so the SPA can tell days from seconds
	// (finding 11).
	if v, ok := byKey["smtp_dial_timeout"]; !ok || v["unit"] != "Sekunden" {
		t.Errorf("smtp_dial_timeout unit = %v, want Sekunden", v)
	}
	if v, ok := byKey["inspection_orange_window_percent"]; !ok || v["unit"] != "Prozent" {
		t.Errorf("inspection_orange_window_percent unit = %v, want Prozent", v)
	}
	if v, ok := byKey["inspection_orange_window_percent"]; !ok || v["value"] != float64(25) {
		t.Errorf("inspection_orange_window_percent value = %v, want 25", v)
	}
	if v, ok := byKey["otp_length"]; !ok || v["unit"] != "Zeichen" {
		t.Errorf("otp_length unit = %v, want Zeichen", v)
	}
	if v, ok := byKey["otp_length"]; !ok || v["value_type"] != "integer" || v["value"] != float64(10) {
		t.Errorf("otp_length row = %v, want integer 10", v)
	}
	if v, ok := byKey["inventory_prefix"]; !ok || v["value_type"] != "text" || v["value"] != "GEAR" {
		t.Errorf("inventory_prefix row = %v, want text GEAR", v)
	}
	if v, ok := byKey["qualification_expiring_soon_window"]; !ok || v["value"] != float64(2592000) {
		t.Errorf("qualification window row = %v, want 2592000 seconds", v)
	}
	if v, ok := byKey["attributes_max_size"]; !ok || v["value"] != float64(16384) {
		t.Errorf("attributes_max_size row = %v, want 16384", v)
	}
}

func TestSystemSettingsGetForbidden(t *testing.T) {
	// FORBIDDEN: authenticated caller without admin.settings.system → uniform
	// 403 with no setting data and no hint of what is missing (AD-6).
	svc := &fakeService{appSettings: fullAppSettings()}
	surface := systemGateway([]string{core.SmtpSettingsPermission, "dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "einstellung") || strings.Contains(rec.Body.String(), "GEAR") || strings.Contains(rec.Body.String(), "otp") {
		t.Errorf("403 body leaks setting data: %s", rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" || env.Error.Message != "Keine Berechtigung." {
		t.Errorf("403 envelope = %+v, want uniform forbidden", env.Error)
	}
}

func TestSystemSettingsGetUnauthenticated(t *testing.T) {
	surface := systemGateway([]string{core.AppSettingsPermission}, nil, &fakeService{})
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

func TestSystemSettingsPutValid(t *testing.T) {
	// PUT_SETTING: 200 with the updated row + German confirmation; the value
	// really reached the service (the fake records the input).
	svc := &fakeService{appSettings: fullAppSettings()}
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/smtp_protocol_timeout", "tok", `{"value":45}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgAppSettingSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgAppSettingSaved)
	}
	if body["key"] != "smtp_protocol_timeout" || body["value_type"] != "duration" || body["value"] != float64(45) {
		t.Errorf("body = %+v, want the updated setting row", body)
	}
	if svc.lastAppSettingKey != "smtp_protocol_timeout" {
		t.Errorf("service received key = %q, want smtp_protocol_timeout", svc.lastAppSettingKey)
	}
	if v, ok := svc.lastAppSettingVal.Value.(float64); !ok || v != 45 {
		t.Errorf("service received value = %v, want 45", svc.lastAppSettingVal.Value)
	}
}

func TestSystemSettingsPutText(t *testing.T) {
	// PUT_SETTING (text): a text value round-trips.
	svc := &fakeService{appSettings: fullAppSettings()}
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/inventory_prefix", "tok", `{"value":"GKW"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastAppSettingKey != "inventory_prefix" {
		t.Errorf("service received key = %q, want inventory_prefix", svc.lastAppSettingKey)
	}
	if v, ok := svc.lastAppSettingVal.Value.(string); !ok || v != "GKW" {
		t.Errorf("service received value = %v, want GKW", svc.lastAppSettingVal.Value)
	}
}

func TestSystemSettingsPutUnknownKey(t *testing.T) {
	// PUT_UNKNOWN: a key outside the catalog → 400 invalid_request German, no
	// data hint.
	svc := &fakeService{appSettingsPutErr: core.ErrAppSettingUnknown}
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/smtp_timeout", "tok", `{"value":45}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if env.Error.Message != core.MsgAppSettingUnknown {
		t.Errorf("message = %q, want %q", env.Error.Message, core.MsgAppSettingUnknown)
	}
}

func TestSystemSettingsPutInvalid(t *testing.T) {
	// PUT_TYPE_MISMATCH / PUT_NEGATIVE / PUT_EMPTY_TEXT: each 400-class German
	// message surfaces through the uniform envelope.
	cases := []struct {
		name       string
		key        string
		body       string
		wantErr    error
		wantMsgSub string
	}{
		{"type mismatch", "otp_ttl", `{"value":"nope"}`, &core.InvalidAppSettingsError{Message: core.MsgAppSettingTypeMismatch}, "Typ"},
		{"negative", "otp_length", `{"value":-1}`, &core.InvalidAppSettingsError{Message: core.MsgAppSettingNegative}, "negativ"},
		{"empty text", "inventory_prefix", `{"value":"  "}`, &core.InvalidAppSettingsError{Message: core.MsgAppSettingEmptyText}, "Wert"},
		{"malformed json", "otp_ttl", `{value:`, &core.InvalidAppSettingsError{Message: ""}, "JSON"},
		{"trailing content", "otp_ttl", `{"value":1200} x`, &core.InvalidAppSettingsError{Message: ""}, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{appSettings: fullAppSettings()}
			if tc.name != "malformed json" && tc.name != "trailing content" {
				// The JSON decode fails before the service is reached — the 400
				// is the handler's, not the service's.
				svc.appSettingsPutErr = tc.wantErr
			}
			surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
			rec := doRequest(surface, http.MethodPut, "/"+tc.key, "tok", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			var env httpapi.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decoding 400 err = %v", err)
			}
			if env.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", env.Error.Code)
			}
			if !strings.Contains(env.Error.Message, tc.wantMsgSub) {
				t.Errorf("message = %q, want contains %q", env.Error.Message, tc.wantMsgSub)
			}
		})
	}
}

func TestSystemSettingsPutForbidden(t *testing.T) {
	surface := systemGateway([]string{core.SmtpSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPut, "/otp_ttl", "tok", `{"value":1200}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSystemSettingsPutUnseededFixtureNoPanic(t *testing.T) {
	// Finding 10: a PUT against a service whose typed fixture is unset must
	// answer a clean 500 envelope (the fake's error), never a nil-deref panic.
	svc := &fakeService{} // appSettings nil
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/otp_ttl", "tok", `{"value":1200}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (clean error, not a panic; body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

func TestSystemSettingsPutRoundTripsToGet(t *testing.T) {
	// PUT→GET consistency through the REAL handler chain: after a per-setting
	// PUT the same surface's GET reflects the new value, and untouched rows keep
	// their values (the fake service persists the edit).
	svc := &fakeService{appSettings: fullAppSettings()}
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodPut, "/smtp_protocol_timeout", "tok", `{"value":45}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding GET err = %v", err)
	}
	byKey := make(map[string]map[string]any, len(body))
	for _, row := range body {
		byKey[row["key"].(string)] = row
	}
	if v, ok := byKey["smtp_protocol_timeout"]; !ok || v["value"] != float64(45) {
		t.Errorf("smtp_protocol_timeout after PUT = %v, want 45", v)
	}
	// An untouched row keeps its seeded value (a per-key update does not clobber).
	if v, ok := byKey["otp_ttl"]; !ok || v["value"] != float64(900) {
		t.Errorf("otp_ttl after unrelated PUT = %v, want 900", v)
	}
}

func TestSystemSettingsNotFoundEnvelope(t *testing.T) {
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), &fakeService{})
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

func TestSystemSettingsMethodNotAllowedEnvelope(t *testing.T) {
	surface := systemGateway([]string{core.AppSettingsPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodDelete, "/", "tok", "")
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