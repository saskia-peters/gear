package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeAppSettingsStore is an in-memory AppSettingsStore seeded with the 21
// canonical rows (mirrors the migration seed).
type fakeAppSettingsStore struct {
	rows    []*AppSetting
	listErr error
	upsert  []*AppSetting
	upsertErr error
}

func (f *fakeAppSettingsStore) ListAppSettings(context.Context) ([]*AppSetting, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

func (f *fakeAppSettingsStore) UpsertAppSetting(_ context.Context, setting *AppSetting) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upsert = append(f.upsert, setting)
	for i, row := range f.rows {
		if row.Key == setting.Key {
			persisted := *setting
			persisted.UpdatedAt = time.Now()
			f.rows[i] = &persisted
			return nil
		}
	}
	persisted := *setting
	persisted.UpdatedAt = time.Now()
	f.rows = append(f.rows, &persisted)
	return nil
}

// newAppSettingsService wires a service over the seeded fake store with the
// admin.settings.system holder by default.
func newAppSettingsService(perms ...string) (*Service, *fakeAppSettingsStore, *fakeAudit) {
	store := &fakeAppSettingsStore{rows: seededAppSettingRows()}
	audit := &fakeAudit{}
	if len(perms) == 0 {
		perms = []string{AppSettingsPermission}
	}
	svc := NewService(nil, nil, nil, store, &fakeCipher{}, &fakePerms{perms: perms}, audit, nil, nil, nil)
	return svc, store, audit
}

// seededAppSettingRows builds the 21 canonical rows exactly as the migration
// seeds them (durations in seconds).
func seededAppSettingRows() []*AppSetting {
	rows := make([]*AppSetting, 0, len(AppSettingCatalogKeys()))
	for _, key := range AppSettingCatalogKeys() {
		rows = append(rows, AppSettingFor(&AppSettings{
			SmtpDialTimeout:                 10 * time.Second,
			SmtpProtocolTimeout:             30 * time.Second,
			BackupDialTimeout:               10 * time.Second,
			BackupProtocolTimeout:           10 * time.Second,
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
		}, key))
	}
	return rows
}

func TestCurrentAppSettingsReadOnlyPort(t *testing.T) {
	// GET_ALL / read-only port: the 21 atomic rows resolve into the typed
	// struct with the proposal's default values — no permission check (the
	// trusted internal read path).
	svc, _, _ := newAppSettingsService("dashboard.view") // permission irrelevant here
	got, err := svc.CurrentAppSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentAppSettings err = %v", err)
	}
	if got.SmtpDialTimeout != 10*time.Second || got.SmtpProtocolTimeout != 30*time.Second {
		t.Errorf("smtp timeouts = %v/%v, want 10s/30s", got.SmtpDialTimeout, got.SmtpProtocolTimeout)
	}
	if got.BackupDialTimeout != 10*time.Second || got.BackupProtocolTimeout != 10*time.Second {
		t.Errorf("backup timeouts = %v/%v, want 10s/10s", got.BackupDialTimeout, got.BackupProtocolTimeout)
	}
	if got.PasswordResetTTL != 1800*time.Second || got.AdminRecoveryTTL != 1800*time.Second {
		t.Errorf("reset/recovery TTL = %v/%v, want 30min each", got.PasswordResetTTL, got.AdminRecoveryTTL)
	}
	if got.ForgotThrottleInterval != 60*time.Second {
		t.Errorf("forgot throttle = %v, want 60s", got.ForgotThrottleInterval)
	}
	if got.OtpTTL != 900*time.Second || got.OtpLength != 10 {
		t.Errorf("otp = %v/%d, want 15min/10", got.OtpTTL, got.OtpLength)
	}
	if got.MfaEnrollmentWindow != 600*time.Second {
		t.Errorf("mfa window = %v, want 10min", got.MfaEnrollmentWindow)
	}
	if got.LockoutThresholdShort != 3 || got.LockoutThresholdLong != 4 || got.LockoutMaxFailedCount != 10 {
		t.Errorf("lockout thresholds = %d/%d/%d, want 3/4/10", got.LockoutThresholdShort, got.LockoutThresholdLong, got.LockoutMaxFailedCount)
	}
	if got.LockoutDurationShort != 30*time.Second || got.LockoutDurationLong != 60*time.Second {
		t.Errorf("lockout durations = %v/%v, want 30s/60s", got.LockoutDurationShort, got.LockoutDurationLong)
	}
	if got.AttributeKeyMaxRunes != 64 || got.AttributesMaxSize != 16384 {
		t.Errorf("attribute caps = %d/%d, want 64/16384", got.AttributeKeyMaxRunes, got.AttributesMaxSize)
	}
	if got.InventoryPrefix != "GEAR" || got.InventoryWidth != 9 {
		t.Errorf("inventory = %q/%d, want GEAR/9", got.InventoryPrefix, got.InventoryWidth)
	}
	if got.InspectionOrangeWindowPercent != 25 {
		t.Errorf("orange window = %d, want 25", got.InspectionOrangeWindowPercent)
	}
	if got.QualificationExpiringSoonWindow != 2592000*time.Second {
		t.Errorf("qualification window = %v, want 30d", got.QualificationExpiringSoonWindow)
	}
}

func TestGetAppSettings(t *testing.T) {
	// GET_ALL via the permission-checked getter: holder resolves the typed
	// settings.
	svc, _, _ := newAppSettingsService()
	got, err := svc.GetAppSettings(context.Background(), actorID)
	if err != nil {
		t.Fatalf("GetAppSettings err = %v", err)
	}
	if got.OtpLength != 10 || got.InventoryPrefix != "GEAR" {
		t.Errorf("settings = %+v, want the seeded typed values", got)
	}
}

func TestGetAppSettingsForbidden(t *testing.T) {
	// FORBIDDEN: caller without admin.settings.system → ErrForbidden.
	svc, _, _ := newAppSettingsService("dashboard.view")
	if _, err := svc.GetAppSettings(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateAppSettingsValid(t *testing.T) {
	// PUT_SETTING: a valid value for a known key is persisted, audited, and the
	// returned row reflects the new value.
	svc, store, audit := newAppSettingsService()

	dur, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_ttl", UpdateAppSettingInput{Value: float64(1200)})
	if err != nil {
		t.Fatalf("UpdateAppSettings(duration) err = %v", err)
	}
	if dur.ValueType != ValueTypeDuration || dur.Duration() != 1200*time.Second {
		t.Errorf("duration row = %+v, want 1200s", dur)
	}

	integer, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_length", UpdateAppSettingInput{Value: float64(8)})
	if err != nil {
		t.Fatalf("UpdateAppSettings(integer) err = %v", err)
	}
	if integer.ValueType != ValueTypeInteger || integer.Int() != 8 {
		t.Errorf("integer row = %+v, want 8", integer)
	}

	text, err := svc.UpdateAppSettings(context.Background(), actorID, "inventory_prefix", UpdateAppSettingInput{Value: "  GKW  "})
	if err != nil {
		t.Fatalf("UpdateAppSettings(text) err = %v", err)
	}
	if text.ValueType != ValueTypeText || text.Text() != "GKW" {
		t.Errorf("text row = %+v, want trimmed GKW", text)
	}

	if len(store.upsert) != 3 {
		t.Fatalf("upserts = %d, want 3", len(store.upsert))
	}
	// Exactly one value column set per row (the typed-store invariant).
	for _, u := range store.upsert {
		set := 0
		if u.DurationValue != nil {
			set++
		}
		if u.IntValue != nil {
			set++
		}
		if u.TextValue != nil {
			set++
		}
		if set != 1 {
			t.Errorf("upsert row %s sets %d value columns, want exactly 1", u.Key, set)
		}
	}
	if len(audit.events) != 3 {
		t.Fatalf("audit events = %d, want 3", len(audit.events))
	}
	for _, e := range audit.events {
		if e.operation != AuditOperationAppSettingsUpdate || e.actorID != actorID || e.severity != AuditSeverityNormal {
			t.Errorf("audit event = %+v, want update op/actor/severity", e)
		}
	}
	if audit.events[0].detail != "key=otp_ttl old=900 new=1200" {
		t.Errorf("audit detail[0] = %q, want the old→new value", audit.events[0].detail)
	}
	if audit.events[1].detail != "key=otp_length old=10 new=8" {
		t.Errorf("audit detail[1] = %q, want the old→new value", audit.events[1].detail)
	}
	if audit.events[2].detail != "key=inventory_prefix old=GEAR new=GKW" {
		t.Errorf("audit detail[2] = %q, want the old→new value", audit.events[2].detail)
	}
}

func TestUpdateAppSettingsPersistsAndReadsBack(t *testing.T) {
	// PUT_SETTING round-trip: after the update the typed struct reads the new
	// value (the GET reflects the write).
	svc, _, _ := newAppSettingsService()
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "smtp_protocol_timeout", UpdateAppSettingInput{Value: float64(45)}); err != nil {
		t.Fatalf("update err = %v", err)
	}
	got, err := svc.GetAppSettings(context.Background(), actorID)
	if err != nil {
		t.Fatalf("GetAppSettings err = %v", err)
	}
	if got.SmtpProtocolTimeout != 45*time.Second {
		t.Errorf("smtp_protocol_timeout = %v, want 45s after update", got.SmtpProtocolTimeout)
	}
}

func TestUpdateAppSettingsUnknownKey(t *testing.T) {
	// PUT_UNKNOWN: a key outside the shipped catalog → ErrAppSettingUnknown,
	// nothing persisted, not audited.
	svc, store, audit := newAppSettingsService()
	_, err := svc.UpdateAppSettings(context.Background(), actorID, "smtp_timeout_typo", UpdateAppSettingInput{Value: float64(10)})
	if !errors.Is(err, ErrAppSettingUnknown) {
		t.Fatalf("err = %v, want ErrAppSettingUnknown", err)
	}
	if len(store.upsert) != 0 {
		t.Error("unknown-key update must not persist")
	}
	if len(audit.events) != 0 {
		t.Error("unknown-key update must not be audited")
	}
}

func TestUpdateAppSettingsValidation(t *testing.T) {
	// PUT_TYPE_MISMATCH / PUT_NEGATIVE / PUT_WHOLE_NUMBER / PUT_TOO_LARGE /
	// PUT_BELOW_MIN / threshold-order / PUT_EMPTY_TEXT / too long: every I/O
	// matrix row maps to the 400-class sentinel with German microcopy, nothing
	// persisted.
	cases := []struct {
		name    string
		key     string
		value   any
		wantMsg string
	}{
		{"duration gets string", "otp_ttl", "1200", MsgAppSettingTypeMismatch},
		{"duration gets bool", "otp_ttl", true, MsgAppSettingTypeMismatch},
		{"integer gets string", "otp_length", "10", MsgAppSettingTypeMismatch},
		{"integer gets float fraction", "otp_length", 10.5, MsgAppSettingWholeNumber},
		{"duration gets float fraction", "otp_ttl", 900.5, MsgAppSettingWholeNumber},
		{"text gets number", "inventory_prefix", float64(5), MsgAppSettingTypeMismatch},
		{"negative duration", "otp_ttl", float64(-1), MsgAppSettingNegative},
		{"negative integer", "otp_length", float64(-3), MsgAppSettingNegative},
		{"integer overflow cap", "attributes_max_size", float64(1_000_000_001), MsgAppSettingTooLarge},
		{"duration overflow guard", "otp_ttl", float64(appSettingDurationMaxSeconds) + 1, MsgAppSettingTooLarge},
		{"duration at overflow guard is allowed", "otp_ttl", float64(appSettingDurationMaxSeconds), ""},
		{"below min integer", "otp_length", float64(0), MsgAppSettingBelowMin},
		{"below min duration", "lockout_duration_short", float64(0), MsgAppSettingBelowMin},
		{"below min security duration", "otp_ttl", float64(0), MsgAppSettingBelowMin},
		{"below min reset ttl", "password_reset_ttl", float64(0), MsgAppSettingBelowMin},
		{"below min smtp timeout", "smtp_dial_timeout", float64(0), MsgAppSettingBelowMin},
		{"empty text", "inventory_prefix", "   ", MsgAppSettingEmptyText},
		{"null value", "inventory_prefix", nil, MsgAppSettingTypeMismatch},
		{"text too long", "inventory_prefix", strings.Repeat("G", 65), MsgAppSettingTextTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newAppSettingsService()
			_, err := svc.UpdateAppSettings(context.Background(), actorID, tc.key, UpdateAppSettingInput{Value: tc.value})
			if tc.wantMsg == "" {
				// "duration at overflow guard is allowed": the bound is
				// inclusive — the write succeeds.
				if err != nil {
					t.Fatalf("err = %v, want the boundary value accepted", err)
				}
				return
			}
			var inv *InvalidAppSettingsError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidAppSettingsError", err)
			}
			if inv.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrAppSettingsInvalid) {
				t.Errorf("err = %v, want unwraps to ErrAppSettingsInvalid", err)
			}
			if len(store.upsert) != 0 {
				t.Error("invalid value must not be persisted")
			}
		})
	}
}

func TestUpdateAppSettingsThresholdOrder(t *testing.T) {
	// B1 progressive-lockout invariant: short must stay < long (cross-key).
	svc, store, _ := newAppSettingsService()

	// Raising the short threshold onto/above the long one is rejected.
	_, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_threshold_short", UpdateAppSettingInput{Value: float64(4)})
	var inv *InvalidAppSettingsError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidAppSettingsError", err)
	}
	if inv.Message != MsgAppSettingThresholdOrder {
		t.Errorf("message = %q, want %q", inv.Message, MsgAppSettingThresholdOrder)
	}

	// Lowering the long threshold onto/below the short one is rejected.
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_threshold_long", UpdateAppSettingInput{Value: float64(3)}); !errors.Is(err, ErrAppSettingsInvalid) {
		t.Fatalf("err = %v, want threshold-order rejection", err)
	}

	// A valid short < long pair is accepted.
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_threshold_short", UpdateAppSettingInput{Value: float64(2)}); err != nil {
		t.Fatalf("valid short threshold err = %v", err)
	}
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_threshold_long", UpdateAppSettingInput{Value: float64(5)}); err != nil {
		t.Fatalf("valid long threshold err = %v", err)
	}
	if len(store.upsert) != 2 {
		t.Errorf("upserts = %d, want 2 (only the valid pair persisted)", len(store.upsert))
	}
}

func TestUpdateAppSettingsReadBackFailureReturnsWrittenValue(t *testing.T) {
	// Finding 6: if the write commits but the read-back fails, the client gets
	// the persisted row — never a misleading "unknown key" 400.
	svc, store, audit := newAppSettingsService()
	store.listErr = errors.New("db read back failed")
	got, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_ttl", UpdateAppSettingInput{Value: float64(1200)})
	if err != nil {
		t.Fatalf("update err = %v, want the write to succeed despite read-back failure", err)
	}
	if got == nil || got.Duration() != 1200*time.Second {
		t.Errorf("returned row = %+v, want the written otp_ttl=1200s", got)
	}
	if len(store.upsert) != 1 {
		t.Errorf("upserts = %d, want 1 (the write must have landed)", len(store.upsert))
	}
	if len(audit.events) != 1 {
		t.Errorf("audit events = %d, want 1", len(audit.events))
	}
}

func TestCurrentAppSettingsSkipsNilValueDrift(t *testing.T) {
	// Finding 5: a CHECK-violating row whose value column is NULL (manual
	// edit/drift) must be SKIPPED by the typed projection, not panicked and
	// not shipped as a zero value.
	svc, store, _ := newAppSettingsService()
	store.rows = append(store.rows,
		&AppSetting{Key: "otp_length", ValueType: ValueTypeInteger}, // IntValue nil
		&AppSetting{Key: "smtp_dial_timeout", ValueType: ValueTypeDuration}, // DurationValue nil
	)
	got, err := svc.CurrentAppSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentAppSettings err = %v", err)
	}
	// The drifted rows were skipped: the typed projection keeps the seed value.
	if got.OtpLength != 10 {
		t.Errorf("otp_length = %d, want the seeded 10 (drifted row skipped)", got.OtpLength)
	}
	if got.SmtpDialTimeout != 10*time.Second {
		t.Errorf("smtp_dial_timeout = %v, want the seeded 10s (drifted row skipped)", got.SmtpDialTimeout)
	}
	// Nil-safe accessors never panic.
	row := &AppSetting{Key: "x", ValueType: ValueTypeInteger}
	_ = row.Int()
	_ = row.Duration()
	_ = row.Text()
	_ = row.Value()
	if row.ValidValue() {
		t.Error("ValidValue = true for a row with no value column, want false")
	}
	if row.Value() != nil {
		t.Errorf("Value() = %v for a row with no value column, want nil", row.Value())
	}
}

func TestUpdateAppSettingsForbidden(t *testing.T) {
	// FORBIDDEN: caller without admin.settings.system → ErrForbidden, nothing
	// persisted, not audited.
	svc, store, audit := newAppSettingsService("dashboard.view")
	_, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_ttl", UpdateAppSettingInput{Value: float64(1200)})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(store.upsert) != 0 || len(audit.events) != 0 {
		t.Error("forbidden update must not persist or audit")
	}
}

func TestUpdateAppSettingsAuditWriteFailureIsBestEffort(t *testing.T) {
	// NFR-O1 best-effort audit: a failed audit write does NOT fail the update.
	svc, store, audit := newAppSettingsService()
	audit.err = errors.New("audit down")
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_ttl", UpdateAppSettingInput{Value: float64(1200)}); err != nil {
		t.Fatalf("update err = %v, want persisted despite audit failure", err)
	}
	if len(store.upsert) != 1 {
		t.Errorf("upserts = %d, want 1 (audit failure must not roll back)", len(store.upsert))
	}
}

func TestCurrentAppSettingsSkipsCatalogDrift(t *testing.T) {
	// A row outside the shipped catalog (or with a drifted value_type) is
	// skipped, not invented — the typed projection only ever carries the
	// catalog keys.
	svc, store, _ := newAppSettingsService()
	store.rows = append(store.rows, &AppSetting{Key: "bogus_key", ValueType: ValueTypeInteger, IntValue: int64Ptr(5)})
	got, err := svc.CurrentAppSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentAppSettings err = %v", err)
	}
	if got.OtpLength != 10 {
		t.Errorf("typed projection drifted: %+v", got)
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestCurrentAppSettingsSurfaceDoesNotFabricateMissingRows(t *testing.T) {
	// GET-surface integrity (review B4): a catalog key whose DB row is missing
	// must NOT project as a plausible zero. CurrentAppSettings resolves the
	// store's rows; AppSettingFor returns nil for a key the store did not
	// return, so the HTTP surface can skip it.
	svc, store, _ := newAppSettingsService()
	store.rows = store.rows[:1] // only smtp_dial_timeout survives
	got, err := svc.CurrentAppSettings(context.Background())
	if err != nil {
		t.Fatalf("CurrentAppSettings err = %v", err)
	}
	if row := AppSettingFor(got, "smtp_dial_timeout"); row == nil {
		t.Error("smtp_dial_timeout row = nil, want the resolved row")
	}
	if row := AppSettingFor(got, "otp_ttl"); row != nil {
		t.Errorf("otp_ttl row = %+v, want nil (row missing from the store)", row)
	}
}

func TestUpdateAppSettingsLockoutFailClosedOnReadError(t *testing.T) {
	// Cross-key invariant fail-closed (review B5): when the current-value read
	// fails, a lockout-pair update must be REJECTED (never written blind), while
	// a non-lockout key still proceeds (its read is only for the audit detail).
	svc, store, _ := newAppSettingsService()

	store.listErr = errors.New("db down")
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_threshold_short", UpdateAppSettingInput{Value: float64(2)}); err == nil {
		t.Fatal("lockout update err = nil, want fail-closed error")
	}
	if len(store.upsert) != 0 {
		t.Error("fail-closed lockout update must not persist")
	}

	// Non-lockout key: the write still lands (audit detail just omits the old
	// value).
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "otp_ttl", UpdateAppSettingInput{Value: float64(1200)}); err != nil {
		t.Fatalf("non-lockout update err = %v, want it to proceed despite read failure", err)
	}
	if len(store.upsert) != 1 {
		t.Errorf("upserts = %d, want 1", len(store.upsert))
	}
}

func TestUpdateAppSettingsDurationOrder(t *testing.T) {
	// B1 progressive-lockout invariant extended to the duration pair (review
	// B6): short duration must stay < long duration.
	svc, store, _ := newAppSettingsService()

	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_duration_short", UpdateAppSettingInput{Value: float64(60)}); !errors.Is(err, ErrAppSettingsInvalid) {
		t.Fatalf("err = %v, want duration-order rejection (short >= long)", err)
	}
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_duration_long", UpdateAppSettingInput{Value: float64(30)}); !errors.Is(err, ErrAppSettingsInvalid) {
		t.Fatalf("err = %v, want duration-order rejection (long <= short)", err)
	}
	if _, err := svc.UpdateAppSettings(context.Background(), actorID, "lockout_duration_short", UpdateAppSettingInput{Value: float64(45)}); err != nil {
		t.Fatalf("valid short duration err = %v", err)
	}
	if len(store.upsert) != 1 {
		t.Errorf("upserts = %d, want 1 (only the valid duration persisted)", len(store.upsert))
	}
}