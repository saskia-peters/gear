package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Configurable system settings (Story 5-2b): the Admin-owned typed store behind
// the Einstellungen → System surface. The 14 proposal defaults
// (configurable-settings-proposal-2026-09-13.md) are modeled as ATOMIC rows in
// app_settings — one value per row, one of three value columns set per row.
// This story ships the store + surface + read-only port only; consumer
// adoption (threading the values into the SMTP/backup/user/tools adapters) is
// deferred to the follow-up story (deferred-work.md). The typed AppSettings
// struct below is the single in-memory projection of all rows; consumers of the
// read-only AppSettingsPort read here, never a copy.

// ErrAppSettingUnknown is returned when an update references a key outside the
// shipped catalog (PUT_UNKNOWN). Handlers map it to a 400 invalid_request —
// the key catalog is fixed by the seed, so an unknown key is a client error,
// not a 404.
var ErrAppSettingUnknown = errors.New("admin core: unknown app setting")

// ErrAppSettingsInvalid is the sentinel wrapping a German validation message
// for a 400 invalid_request (PUT_TYPE_MISMATCH / PUT_NEGATIVE /
// PUT_WHOLE_NUMBER / PUT_TOO_LARGE / PUT_BELOW_MIN / PUT_EMPTY_TEXT / text too
// long / threshold-order).
var ErrAppSettingsInvalid = errors.New("admin core: invalid app setting value")

// InvalidAppSettingsError carries the German validation message for a 400
// invalid_request. It unwraps to ErrAppSettingsInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type InvalidAppSettingsError struct {
	Message string
}

func (e *InvalidAppSettingsError) Error() string { return e.Message }
func (e *InvalidAppSettingsError) Unwrap() error { return ErrAppSettingsInvalid }

// German microcopy for the system-settings surface (Story 5-2b, UX-DR8).
const (
	MsgAppSettingSaved = "System-Einstellung gespeichert."
	// MsgAppSettingUnknown rejects a PUT whose key is outside the shipped
	// catalog (PUT_UNKNOWN → 400 invalid_request).
	MsgAppSettingUnknown = "Unbekannte System-Einstellung."
	// MsgAppSettingTypeMismatch rejects a PUT whose value does not match the
	// setting's value_type (PUT_TYPE_MISMATCH → 400 invalid_request).
	MsgAppSettingTypeMismatch = "Der Wert passt nicht zum Typ dieser Einstellung."
	// MsgAppSettingNegative rejects a negative duration/integer (PUT_NEGATIVE →
	// 400 invalid_request, bounds).
	MsgAppSettingNegative = "Der Wert darf nicht negativ sein."
	// MsgAppSettingWholeNumber rejects a non-whole number for an
	// integer/duration setting (e.g. 10.5 — PUT_WHOLE_NUMBER → 400).
	MsgAppSettingWholeNumber = "Bitte gib eine ganze Zahl ein."
	// MsgAppSettingTooLarge rejects a value above the setting's upper bound
	// (overflow guard / generic cap → 400 invalid_request).
	MsgAppSettingTooLarge = "Der Wert ist zu groß."
	// MsgAppSettingBelowMin rejects a value below the setting's semantic
	// minimum (e.g. otp_length >= 1 → 400 invalid_request).
	MsgAppSettingBelowMin = "Der Wert ist zu klein."
	// MsgAppSettingThresholdOrder rejects a lockout-threshold pair that would
	// invert the progressive policy (short must stay < long → 400).
	MsgAppSettingThresholdOrder = "Die kurze Sperrschwelle muss kleiner sein als die lange Sperrschwelle."
	// MsgAppSettingDurationOrder rejects a lockout-duration pair that would
	// invert the progressive policy (short must stay < long → 400).
	MsgAppSettingDurationOrder = "Die kurze Sperrdauer muss kleiner sein als die lange Sperrdauer."
	// MsgAppSettingEmptyText rejects an empty/whitespace-only text value
	// (PUT_EMPTY_TEXT → 400 invalid_request).
	MsgAppSettingEmptyText = "Bitte gib einen Wert für diese Einstellung ein."
	// MsgAppSettingTextTooLong rejects an oversized text value (defensive cap).
	MsgAppSettingTextTooLong = "Der Wert ist zu lang."
)

// appSettingTextMaxRunes caps a text setting at 64 runes (defensive bound — the
// inventory prefix is the only text setting in V1 and is far smaller).
const appSettingTextMaxRunes = 64

// appSettingIntMax is the generic upper bound for plain integer settings
// (defensive: beyond any V1 need; per-key max would tighten it further).
const appSettingIntMax = 1_000_000_000

// appSettingDurationMaxSeconds is the duration overflow guard: the largest
// whole-second value whose time.Duration multiplication cannot overflow int64.
const appSettingDurationMaxSeconds = math.MaxInt64 / int64(time.Second)

// AppSettings is the typed in-memory projection of every seeded app_settings
// row — one field per atomic setting (durations as time.Duration, integers as
// int, text as string). It is what the read-only AppSettingsPort returns and
// what the follow-up consumer-adoption story reads.
//
// resolvedKeys records which catalog keys were actually read from the store by
// CurrentAppSettings (nil when the struct was constructed directly, e.g. a
// test fixture, meaning "treat all catalog keys as present"). AppSettingFor
// returns nil for a key absent from a non-nil resolvedKeys so the HTTP surface
// never invents a plausible zero value for a missing/drifted row.
type AppSettings struct {
	SmtpDialTimeout                 time.Duration
	SmtpProtocolTimeout             time.Duration
	BackupDialTimeout               time.Duration
	BackupProtocolTimeout           time.Duration
	PasswordResetTTL                time.Duration
	AdminRecoveryTTL                time.Duration
	ForgotThrottleInterval          time.Duration
	OtpTTL                          time.Duration
	OtpLength                       int
	MfaEnrollmentWindow             time.Duration
	LockoutThresholdShort           int
	LockoutThresholdLong            int
	LockoutDurationShort            time.Duration
	LockoutDurationLong             time.Duration
	LockoutMaxFailedCount           int
	AttributeKeyMaxRunes            int
	AttributesMaxSize               int
	InventoryPrefix                 string
	InventoryWidth                  int
	InspectionOrangeWindowDays      int
	QualificationExpiringSoonWindow time.Duration
	resolvedKeys                    map[string]struct{}
}

// AppSetting is the domain representation of one app_settings row (the typed
// store's value column). Exactly one of DurationValue / IntValue / TextValue is
// non-nil per row; durations are whole seconds. Unit is the catalog's display
// unit (e.g. "Sekunden", "Tage", "Zeichen") carried on rows the surface
// renders so admins can tell days from seconds.
type AppSetting struct {
	Key           string
	ValueType     string
	Unit          string
	DurationValue *time.Duration
	IntValue      *int64
	TextValue     *string
	UpdatedAt     time.Time
}

// Duration returns the duration value. Nil-safe: a drifted row with a NULL
// value column returns the zero duration instead of panicking.
func (a *AppSetting) Duration() time.Duration {
	if a == nil || a.DurationValue == nil {
		return 0
	}
	return *a.DurationValue
}

// Int returns the integer value. Nil-safe: a drifted row with a NULL value
// column returns 0 instead of panicking.
func (a *AppSetting) Int() int64 {
	if a == nil || a.IntValue == nil {
		return 0
	}
	return *a.IntValue
}

// Text returns the text value. Nil-safe: a drifted row with a NULL value
// column returns "" instead of panicking.
func (a *AppSetting) Text() string {
	if a == nil || a.TextValue == nil {
		return ""
	}
	return *a.TextValue
}

// ValidValue reports whether the row's value column is actually set AND matches
// its value_type. A CHECK-violating row (manually edited / drifted) with a NULL
// value column fails this and is skipped by the typed projection instead of
// shipping a zero/garbage value (finding 5).
func (a *AppSetting) ValidValue() bool {
	if a == nil {
		return false
	}
	switch a.ValueType {
	case ValueTypeDuration:
		return a.DurationValue != nil
	case ValueTypeInteger:
		return a.IntValue != nil
	case ValueTypeText:
		return a.TextValue != nil
	default:
		return false
	}
}

// Value returns the row's typed value as the scalar a client sees: durations
// as whole seconds (number), integers as number, text as string. Nil-safe: a
// row without a value column returns nil (never panics).
func (a *AppSetting) Value() any {
	if a == nil || !a.ValidValue() {
		return nil
	}
	switch a.ValueType {
	case ValueTypeDuration:
		return int64(a.Duration() / time.Second)
	case ValueTypeInteger:
		return a.Int()
	default:
		return a.Text()
	}
}

// appSettingDef is one entry of the server-authoritative catalog: the shipped
// key, its value type, its display unit (surfaced on the wire + in the SPA so
// days and seconds never look alike), the setter that projects a row onto the
// typed AppSettings struct (read direction), the getter that reads the typed
// field back out (projection direction), and optional semantic bounds (min in
// the setting's raw unit — seconds for durations, count for integers; the
// generic max comes from the value type). The catalog IS the seed contract —
// CurrentAppSettings, the update validation and the GET projection all derive
// from it, so the key set can never drift between the store and the typed
// struct.
type appSettingDef struct {
	key       string
	valueType string
	unit      string
	min       int64 // 0 = no per-key minimum (non-negative base applies)
	set       func(*AppSettings, *AppSetting)
	get       func(*AppSettings) any
}

// appSettingsCatalog is the 21 atomic rows of the 14 proposal defaults.
var appSettingsCatalog = []appSettingDef{
	// A1/A2 SMTP timeouts.
	{key: "smtp_dial_timeout", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.SmtpDialTimeout = r.Duration() },
		get: func(s *AppSettings) any { return s.SmtpDialTimeout }},
	{key: "smtp_protocol_timeout", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.SmtpProtocolTimeout = r.Duration() },
		get: func(s *AppSettings) any { return s.SmtpProtocolTimeout }},
	// A3 Backup test-connection timeouts.
	{key: "backup_dial_timeout", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.BackupDialTimeout = r.Duration() },
		get: func(s *AppSettings) any { return s.BackupDialTimeout }},
	{key: "backup_protocol_timeout", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.BackupProtocolTimeout = r.Duration() },
		get: func(s *AppSettings) any { return s.BackupProtocolTimeout }},
	// A4/A5/A6 Reset/recovery TTLs and the forgot-password throttle.
	{key: "password_reset_ttl", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.PasswordResetTTL = r.Duration() },
		get: func(s *AppSettings) any { return s.PasswordResetTTL }},
	{key: "admin_recovery_ttl", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.AdminRecoveryTTL = r.Duration() },
		get: func(s *AppSettings) any { return s.AdminRecoveryTTL }},
	{key: "forgot_throttle_interval", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.ForgotThrottleInterval = r.Duration() },
		get: func(s *AppSettings) any { return s.ForgotThrottleInterval }},
	// A7/A8/A9 OTP + MFA windows.
	{key: "otp_ttl", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.OtpTTL = r.Duration() },
		get: func(s *AppSettings) any { return s.OtpTTL }},
	{key: "otp_length", valueType: ValueTypeInteger, unit: "Zeichen", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.OtpLength = int(r.Int()) },
		get: func(s *AppSettings) any { return s.OtpLength }},
	{key: "mfa_enrollment_window", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.MfaEnrollmentWindow = r.Duration() },
		get: func(s *AppSettings) any { return s.MfaEnrollmentWindow }},
	// B1 Login lockout policy (short threshold must stay < long — enforced
	// cross-key on update).
	{key: "lockout_threshold_short", valueType: ValueTypeInteger, unit: "Fehlversuche", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.LockoutThresholdShort = int(r.Int()) },
		get: func(s *AppSettings) any { return s.LockoutThresholdShort }},
	{key: "lockout_threshold_long", valueType: ValueTypeInteger, unit: "Fehlversuche", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.LockoutThresholdLong = int(r.Int()) },
		get: func(s *AppSettings) any { return s.LockoutThresholdLong }},
	{key: "lockout_duration_short", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.LockoutDurationShort = r.Duration() },
		get: func(s *AppSettings) any { return s.LockoutDurationShort }},
	{key: "lockout_duration_long", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.LockoutDurationLong = r.Duration() },
		get: func(s *AppSettings) any { return s.LockoutDurationLong }},
	{key: "lockout_max_failed_count", valueType: ValueTypeInteger, unit: "Fehlversuche", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.LockoutMaxFailedCount = int(r.Int()) },
		get: func(s *AppSettings) any { return s.LockoutMaxFailedCount }},
	// C1 Attribute contract.
	{key: "attribute_key_max_runes", valueType: ValueTypeInteger, unit: "Zeichen", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.AttributeKeyMaxRunes = int(r.Int()) },
		get: func(s *AppSettings) any { return s.AttributeKeyMaxRunes }},
	{key: "attributes_max_size", valueType: ValueTypeInteger, unit: "Bytes", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.AttributesMaxSize = int(r.Int()) },
		get: func(s *AppSettings) any { return s.AttributesMaxSize }},
	// C2 Inventory-number format.
	{key: "inventory_prefix", valueType: ValueTypeText,
		set: func(s *AppSettings, r *AppSetting) { s.InventoryPrefix = r.Text() },
		get: func(s *AppSettings) any { return s.InventoryPrefix }},
	{key: "inventory_width", valueType: ValueTypeInteger, unit: "Ziffern", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.InventoryWidth = int(r.Int()) },
		get: func(s *AppSettings) any { return s.InventoryWidth }},
	// D1 Inspection orange-window threshold (days — a DAYS unit, unlike the
	// duration windows, so the unit is surfaced to distinguish them).
	{key: "inspection_orange_window_days", valueType: ValueTypeInteger, unit: "Tage", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.InspectionOrangeWindowDays = int(r.Int()) },
		get: func(s *AppSettings) any { return s.InspectionOrangeWindowDays }},
	// D2 Qualification "expiring soon" window.
	{key: "qualification_expiring_soon_window", valueType: ValueTypeDuration, unit: "Sekunden", min: 1,
		set: func(s *AppSettings, r *AppSetting) { s.QualificationExpiringSoonWindow = r.Duration() },
		get: func(s *AppSettings) any { return s.QualificationExpiringSoonWindow }},
}

// appSettingsCatalogByKey is the O(1) lookup for update validation and the
// CurrentAppSettings resolution.
var appSettingsCatalogByKey = func() map[string]appSettingDef {
	m := make(map[string]appSettingDef, len(appSettingsCatalog))
	for _, def := range appSettingsCatalog {
		m[def.key] = def
	}
	return m
}()

// AppSettingCatalogKeys returns the shipped setting keys in the catalog's
// canonical (seed) order. Used by the HTTP surface to render wire rows in a
// stable order.
func AppSettingCatalogKeys() []string {
	keys := make([]string, 0, len(appSettingsCatalog))
	for _, def := range appSettingsCatalog {
		keys = append(keys, def.key)
	}
	return keys
}

// AppSettingFor projects the typed AppSettings struct back onto a single row
// for the given key (the HTTP GET surface uses it to render wire rows from the
// typed resolution, so the value is always the server's live value). Returns
// nil for a key outside the catalog, OR for a catalog key that a non-nil
// resolvedKeys does not contain — so a missing/drifted DB row is never
// invented as a plausible zero on the surface.
func AppSettingFor(settings *AppSettings, key string) *AppSetting {
	def, ok := appSettingsCatalogByKey[key]
	if !ok || settings == nil {
		return nil
	}
	if settings.resolvedKeys != nil {
		if _, present := settings.resolvedKeys[key]; !present {
			return nil
		}
	}
	row := &AppSetting{Key: key, ValueType: def.valueType, Unit: def.unit}
	switch def.valueType {
	case ValueTypeDuration:
		d := def.get(settings).(time.Duration)
		row.DurationValue = &d
	case ValueTypeInteger:
		n := int64(def.get(settings).(int))
		row.IntValue = &n
	case ValueTypeText:
		s := def.get(settings).(string)
		row.TextValue = &s
	}
	return row
}

// AppSettingsStore is the outbound persistence port over the Admin-owned
// app_settings table (AD-11/Story 5-2b). ListAppSettings returns every seeded
// row; UpsertAppSetting writes one key's value (the core validates the value
// against the catalog before it lands here).
type AppSettingsStore interface {
	ListAppSettings(ctx context.Context) ([]*AppSetting, error)
	UpsertAppSetting(ctx context.Context, setting *AppSetting) error
}

// UpdateAppSettingInput is the PUT body for a per-setting update. Value is the
// decoded JSON scalar (number → float64, string → string, bool → bool,
// null/object/array → typed accordingly); the core validates it against the
// setting's value type.
type UpdateAppSettingInput struct {
	Value any `json:"value"`
}

// GetAppSettings returns every setting typed (GET_ALL). The caller is gated by
// admin.settings.system upstream; the permission is re-checked
// defense-in-depth (AD-6).
func (s *Service) GetAppSettings(ctx context.Context, actorID string) (*AppSettings, error) {
	if err := s.requireAppSettingsPermission(ctx, actorID); err != nil {
		return nil, err
	}
	return s.CurrentAppSettings(ctx)
}

// CurrentAppSettings implements the read-only AppSettingsPort (Story 5-2b) that
// the follow-up consumer-adoption story consumes — it reads the typed store
// here, never a copy. No actor, no permission re-check: this is the trusted
// internal read path (mirrors CurrentSchedules/SchedulesPort). A drifted row
// (outside the catalog, wrong value_type, or a NULL value column — CHECK
// violation) is logged and SKIPPED, never projected (finding 5).
func (s *Service) CurrentAppSettings(ctx context.Context) (*AppSettings, error) {
	rows, err := s.appSettingsStore.ListAppSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to list app settings: %w", err)
	}
	settings := &AppSettings{resolvedKeys: make(map[string]struct{}, len(rows))}
	for _, row := range rows {
		def, ok := appSettingsCatalogByKey[row.Key]
		if !ok {
			// A row outside the shipped catalog is a seeding/backfill drift —
			// log it and skip; the surface never invents values for unknown keys.
			s.log().Warn("app_settings row outside the shipped catalog", "key", row.Key)
			continue
		}
		if row.ValueType != def.valueType {
			s.log().Warn("app_settings row value_type drifted from the catalog", "key", row.Key, "value_type", row.ValueType)
			continue
		}
		if !row.ValidValue() {
			s.log().Warn("app_settings row has no value for its value_type (CHECK drift)", "key", row.Key, "value_type", row.ValueType)
			continue
		}
		settings.resolvedKeys[row.Key] = struct{}{}
		def.set(settings, row)
	}
	return settings, nil
}

// UpdateAppSettings persists one setting's value (PUT_SETTING): the key must be
// in the shipped catalog (PUT_UNKNOWN), the value must match the setting's
// value_type (PUT_TYPE_MISMATCH), be a whole number (PUT_WHOLE_NUMBER),
// non-negative (PUT_NEGATIVE), within the type + per-key bounds (PUT_TOO_LARGE /
// PUT_BELOW_MIN), and the lockout thresholds must keep short < long. Text must
// be non-empty and bounded (PUT_EMPTY_TEXT / too long). The update is audited
// (admin.settings.system.update, NFR-O1/NFR-O2) best-effort with an old→new
// detail and returns the freshly persisted row. If the read-back after the
// committed write fails, the written row is returned (never a misleading
// "unknown key" — finding 6).
func (s *Service) UpdateAppSettings(ctx context.Context, actorID, key string, input UpdateAppSettingInput) (*AppSetting, error) {
	if err := s.requireAppSettingsPermission(ctx, actorID); err != nil {
		return nil, err
	}
	def, ok := appSettingsCatalogByKey[key]
	if !ok {
		return nil, ErrAppSettingUnknown
	}

	// Current values: used for the cross-key lockout-threshold invariant and
	// the old→new audit detail. Best-effort — a failed read still lets the
	// write proceed (the audit detail then omits the old value).
	current, curErr := s.CurrentAppSettings(ctx)

	row, err := validateAppSettingValue(def, input.Value)
	if err != nil {
		return nil, err
	}
	row.Key = key
	row.ValueType = def.valueType
	row.Unit = def.unit

	if curErr != nil {
		// The cross-key lockout invariant needs the OTHER side's live value —
		// fail closed for lockout-pair keys rather than writing a policy that
		// could invert progressive locking (the audit detail may still omit the
		// old value for non-lockout keys).
		if lockoutPairKey(key) {
			return nil, fmt.Errorf("admin core: failed to read current app settings for lockout update: %w", curErr)
		}
	} else if err := validateAppSettingCrossKey(key, row, current); err != nil {
		return nil, err
	}

	if err := s.appSettingsStore.UpsertAppSetting(ctx, row); err != nil {
		return nil, fmt.Errorf("admin core: failed to persist app setting: %w", err)
	}

	// Read the persisted row back so the returned value carries the fresh
	// updated_at (the upsert is :exec — no RETURNING). A read-back failure is
	// NOT a write failure: log it and return the row we just persisted, never
	// a misleading "unknown key" 400 after a successful write.
	updated, err := s.findAppSetting(ctx, key)
	if err != nil {
		s.log().Warn("app setting read-back failed after upsert; returning the written value", "key", key, "error", err)
		updated = row
	}

	oldValue := ""
	if curErr == nil && current != nil {
		oldValue = appSettingAuditValue(AppSettingFor(current, key))
	}
	s.auditAppSettings(ctx, actorID, AuditOperationAppSettingsUpdate,
		fmt.Sprintf("key=%s old=%s new=%s", key, oldValue, appSettingAuditValue(row)))
	return updated, nil
}

// findAppSetting reads one setting back after an upsert, attaching the
// catalog's display unit so the surface row is complete.
func (s *Service) findAppSetting(ctx context.Context, key string) (*AppSetting, error) {
	rows, err := s.appSettingsStore.ListAppSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to read back app setting: %w", err)
	}
	for _, row := range rows {
		if row.Key == key {
			if def, ok := appSettingsCatalogByKey[key]; ok {
				row.Unit = def.unit
			}
			return row, nil
		}
	}
	return nil, ErrAppSettingUnknown
}

// validateAppSettingValue enforces the PUT invariants against the setting's
// value_type and the catalog's bounds (PUT_TYPE_MISMATCH / PUT_NEGATIVE /
// PUT_WHOLE_NUMBER / PUT_TOO_LARGE / PUT_BELOW_MIN / PUT_EMPTY_TEXT / too
// long). It returns the row with exactly the matching value column set.
func validateAppSettingValue(def appSettingDef, value any) (*AppSetting, error) {
	switch def.valueType {
	case ValueTypeDuration, ValueTypeInteger:
		f, ok := value.(float64) // a JSON number decodes to float64
		if !ok {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingTypeMismatch}
		}
		if f < 0 {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingNegative}
		}
		if math.Trunc(f) != f {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingWholeNumber}
		}
		if def.valueType == ValueTypeDuration {
			// Overflow guard: time.Duration(n) * time.Second must stay inside
			// int64. The bound is far above any real TTL; it exists so an
			// out-of-range float can never silently wrap the duration.
			if f > float64(appSettingDurationMaxSeconds) {
				return nil, &InvalidAppSettingsError{Message: MsgAppSettingTooLarge}
			}
			if def.min > 0 && f < float64(def.min) {
				return nil, &InvalidAppSettingsError{Message: MsgAppSettingBelowMin}
			}
			n := int64(f)
			d := time.Duration(n) * time.Second
			return &AppSetting{ValueType: def.valueType, Unit: def.unit, DurationValue: &d}, nil
		}
		if f > float64(appSettingIntMax) {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingTooLarge}
		}
		if def.min > 0 && f < float64(def.min) {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingBelowMin}
		}
		n := int64(f)
		return &AppSetting{ValueType: def.valueType, Unit: def.unit, IntValue: &n}, nil
	case ValueTypeText:
		str, ok := value.(string)
		if !ok {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingTypeMismatch}
		}
		trimmed := strings.TrimSpace(str)
		if trimmed == "" {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingEmptyText}
		}
		if utf8.RuneCountInString(trimmed) > appSettingTextMaxRunes {
			return nil, &InvalidAppSettingsError{Message: MsgAppSettingTextTooLong}
		}
		return &AppSetting{ValueType: def.valueType, Unit: def.unit, TextValue: &trimmed}, nil
	default:
		return nil, &InvalidAppSettingsError{Message: MsgAppSettingTypeMismatch}
	}
}

// lockoutPairKey reports whether the key participates in the progressive
// lockout short<long cross-key invariant (threshold OR duration pairs). The
// isolated lockout_max_failed_count does not.
func lockoutPairKey(key string) bool {
	switch key {
	case "lockout_threshold_short", "lockout_threshold_long",
		"lockout_duration_short", "lockout_duration_long":
		return true
	default:
		return false
	}
}

// validateAppSettingCrossKey enforces the cross-key invariants of the catalog:
// the progressive lockout policy keeps the SHORT side (threshold and duration)
// BELOW the LONG side (B1). Updating either side validates against the OTHER
// side's current value, so a user cannot silently invert the policy.
func validateAppSettingCrossKey(key string, row *AppSetting, current *AppSettings) error {
	switch key {
	case "lockout_threshold_short":
		if current != nil && current.LockoutThresholdLong > 0 && int(row.Int()) >= current.LockoutThresholdLong {
			return &InvalidAppSettingsError{Message: MsgAppSettingThresholdOrder}
		}
	case "lockout_threshold_long":
		if current != nil && current.LockoutThresholdShort > 0 && int(row.Int()) <= current.LockoutThresholdShort {
			return &InvalidAppSettingsError{Message: MsgAppSettingThresholdOrder}
		}
	case "lockout_duration_short":
		if current != nil && current.LockoutDurationLong > 0 && row.Duration() >= current.LockoutDurationLong {
			return &InvalidAppSettingsError{Message: MsgAppSettingDurationOrder}
		}
	case "lockout_duration_long":
		if current != nil && current.LockoutDurationShort > 0 && row.Duration() <= current.LockoutDurationShort {
			return &InvalidAppSettingsError{Message: MsgAppSettingDurationOrder}
		}
	}
	return nil
}

// appSettingAuditValue renders a row's value for the audit detail (nil-safe).
func appSettingAuditValue(row *AppSetting) string {
	if row == nil || !row.ValidValue() {
		return ""
	}
	switch row.ValueType {
	case ValueTypeDuration:
		return strconv.FormatInt(int64(row.Duration()/time.Second), 10)
	case ValueTypeInteger:
		return strconv.FormatInt(row.Int(), 10)
	default:
		return row.Text()
	}
}

// requireAppSettingsPermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds admin.settings.system. The route gateway
// already enforces it; the core re-checks so no future direct caller can skip
// it. An empty actor ID never passes.
func (s *Service) requireAppSettingsPermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("admin core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == AppSettingsPermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditAppSettings writes the audit row best-effort (NFR-O1): a failed audit
// write is logged, never rolled back into the triggering operation.
func (s *Service) auditAppSettings(ctx context.Context, actorID, operation, detail string) {
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("admin core: app settings audit write failed", "operation", operation, "error", err)
	}
}