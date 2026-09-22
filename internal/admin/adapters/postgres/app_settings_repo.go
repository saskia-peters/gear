package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// App settings store implementation (Story 5-2b, AD-11): list-all + per-key
// upsert over the Admin-owned typed app_settings table. The core validates
// values against its catalog before they land here, so exactly one value
// column is set per row (the DB CHECK is the final backstop).

// ListAppSettings reads the full typed setting list (GET_ALL), deterministic
// order (key ASC — the seed's insert order is not stable across re-applies).
func (r *Repository) ListAppSettings(ctx context.Context) ([]*core.AppSetting, error) {
	rows, err := r.queries.ListAppSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*core.AppSetting, 0, len(rows))
	for i := range rows {
		out = append(out, appSettingFromRow(rows[i]))
	}
	return out, nil
}

// UpsertAppSetting writes one key's value (PUT_SETTING): INSERT with an
// ON CONFLICT update, refreshing updated_at. The value column matching the
// setting's type is set, the other two are NULL.
func (r *Repository) UpsertAppSetting(ctx context.Context, setting *core.AppSetting) error {
	return r.queries.UpsertAppSetting(ctx, appSettingToParams(setting))
}

// appSettingFromRow maps an sqlc app_settings row to the domain setting.
// Durations are stored as whole seconds and mapped to time.Duration; the two
// non-matching value columns are always NULL (the DB CHECK guarantees it) and
// map to nil pointers.
func appSettingFromRow(row AppSetting) *core.AppSetting {
	out := &core.AppSetting{
		Key:       row.Key,
		ValueType: row.ValueType,
		UpdatedAt: row.UpdatedAt.Time,
	}
	switch row.ValueType {
	case core.ValueTypeDuration:
		if row.DurationValue.Valid {
			d := time.Duration(row.DurationValue.Int64) * time.Second
			out.DurationValue = &d
		}
	case core.ValueTypeInteger:
		if row.IntValue.Valid {
			n := row.IntValue.Int64
			out.IntValue = &n
		}
	case core.ValueTypeText:
		if row.TextValue.Valid {
			s := row.TextValue.String
			out.TextValue = &s
		}
	}
	return out
}

// appSettingToParams maps the domain setting to the sqlc upsert params. Exactly
// one value param is set; the other two are NULL.
func appSettingToParams(setting *core.AppSetting) UpsertAppSettingParams {
	params := UpsertAppSettingParams{
		Key:       setting.Key,
		ValueType: setting.ValueType,
	}
	switch setting.ValueType {
	case core.ValueTypeDuration:
		if setting.DurationValue != nil {
			params.DurationValue = pgtype.Int8{Int64: int64(*setting.DurationValue / time.Second), Valid: true}
		}
	case core.ValueTypeInteger:
		if setting.IntValue != nil {
			params.IntValue = pgtype.Int8{Int64: *setting.IntValue, Valid: true}
		}
	case core.ValueTypeText:
		if setting.TextValue != nil {
			params.TextValue = pgtype.Text{String: *setting.TextValue, Valid: true}
		}
	}
	return params
}