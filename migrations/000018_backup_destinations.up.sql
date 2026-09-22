-- G.E.A.R. Admin-owned backup destinations (Story 3.2, FR-29/AD-15).
--
-- The runtime-configurable list of external backup targets (S3-compatible /
-- FTP / SFTP / local) edited from the Admin panel by holders of
-- `admin.settings.backup`. The future backup job consumes this table READ-ONLY
-- through the Admin settings port (AD-15) — this table is authored exclusively
-- by the Admin module (AD-11).
--
-- MULTI-ROW by design (NFR-R3 requires ≥1 destination; there is NO single-row
-- guard — unlike smtp_settings). The credential is stored ENCRYPTED at rest
-- (AES-256-GCM with the app-level key from GEAR_ENCRYPTION_KEY, NFR-S4) and is
-- write-only/masked: never returned by GET and never logged. A missing/empty
-- value means "no credential configured" (local destinations may be
-- credential-less; S3/FTP/SFTP require one). `schedule` is an optional,
-- nullable free-form scheduling hint.
CREATE TABLE backup_destinations (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    name               text NOT NULL,
    mechanism          text NOT NULL
                       CHECK (mechanism IN ('s3', 'ftp', 'sftp', 'local')),
    endpoint           text NOT NULL DEFAULT '',
    bucket_or_path     text NOT NULL DEFAULT '',
    username           text NOT NULL DEFAULT '',
    password_encrypted text NOT NULL DEFAULT '',
    schedule           text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);