-- G.E.A.R. Admin-owned configurable system settings (Story 5-2b, AD-11): the
-- generic typed key/value store behind the Einstellungen → System surface. The
-- 14 proposal defaults (configurable-settings-proposal-2026-09-13.md) are
-- modeled as ATOMIC rows — one value per row, exactly one of the three value
-- columns set per row. Durations are stored as whole SECONDS (bigint), integers
-- as bigint, text as text. Consumers (SMTP/backup/user/tools) still read their
-- current constants; this migration only ships the store + seeded defaults, and
-- the follow-up story threads the values into the consumers (deferred-work.md).
--
-- The `admin.settings.system` permission seed follows the 000016 pattern:
-- granted to the admin base role only, idempotent (ON CONFLICT DO NOTHING). It
-- is the code that gates the whole surface (AD-6).

CREATE TABLE app_settings (
    key            text PRIMARY KEY,
    value_type     text NOT NULL CHECK (value_type IN ('duration', 'integer', 'text')),
    duration_value bigint,
    int_value      bigint,
    text_value     text,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    -- Exactly one value column set per row (the typed store invariant): the
    -- column that matches value_type must be set, the other two must be NULL.
    CHECK (
        (value_type = 'duration' AND duration_value IS NOT NULL AND int_value IS NULL AND text_value IS NULL)
        OR (value_type = 'integer' AND duration_value IS NULL AND int_value IS NOT NULL AND text_value IS NULL)
        OR (value_type = 'text' AND duration_value IS NULL AND int_value IS NULL AND text_value IS NOT NULL)
    )
);

-- Canonical seed of the 14 proposal defaults (configurable-settings-proposal
-- 2026-09-13.md), one atomic row per value. Durations are SECONDS.
INSERT INTO app_settings (key, value_type, duration_value, int_value, text_value) VALUES
    -- A1/A2 SMTP timeouts.
    ('smtp_dial_timeout',        'duration', 10,      NULL, NULL),
    ('smtp_protocol_timeout',    'duration', 30,      NULL, NULL),
    -- A3 Backup test-connection timeouts (dial + protocol).
    ('backup_dial_timeout',      'duration', 10,      NULL, NULL),
    ('backup_protocol_timeout',  'duration', 10,      NULL, NULL),
    -- A4/A5/A6 Reset + recovery TTLs and the forgot-password throttle.
    ('password_reset_ttl',       'duration', 1800,    NULL, NULL),
    ('admin_recovery_ttl',       'duration', 1800,    NULL, NULL),
    ('forgot_throttle_interval', 'duration', 60,      NULL, NULL),
    -- A7/A8/A9 OTP + MFA windows.
    ('otp_ttl',                  'duration', 900,     NULL, NULL),
    ('otp_length',               'integer',  NULL,    10,   NULL),
    ('mfa_enrollment_window',    'duration', 600,     NULL, NULL),
    -- B1 Login lockout policy (thresholds, durations, failure cap).
    ('lockout_threshold_short',  'integer',  NULL,    3,    NULL),
    ('lockout_threshold_long',   'integer',  NULL,    4,    NULL),
    ('lockout_duration_short',   'duration', 30,      NULL, NULL),
    ('lockout_duration_long',    'duration', 60,      NULL, NULL),
    ('lockout_max_failed_count', 'integer',  NULL,    10,   NULL),
    -- C1 Attribute contract (key rune cap + total map size in bytes).
    ('attribute_key_max_runes',  'integer',  NULL,    64,   NULL),
    ('attributes_max_size',      'integer',  NULL,    16384, NULL),
    -- C2 Inventory-number format.
    ('inventory_prefix',         'text',     NULL,    NULL, 'GEAR'),
    ('inventory_width',          'integer',  NULL,    6,    NULL),
    -- D1 Inspection orange-window threshold (days, Story 6.1 dashboard).
    ('inspection_orange_window_days', 'integer', NULL, 14, NULL),
    -- D2 Qualification "expiring soon" window (30 days in seconds).
    ('qualification_expiring_soon_window', 'duration', 2592000, NULL, NULL);

-- Seed the `admin.settings.system` permission (Story 5-2b, AD-6): the code that
-- gates the Einstellungen → System surface. Granted to the admin base role,
-- following the 000016 pattern (idempotent).
INSERT INTO permissions (code, description)
VALUES ('admin.settings.system', 'Manage system settings (Einstellungen → System)')
ON CONFLICT (code) DO NOTHING;

INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES ('admin', 'admin.settings.system')) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;