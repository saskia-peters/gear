#!/usr/bin/env bash
# G.E.A.R. restore procedure (Story 7.7, NFR-R3): restore a backup dump
# (pg_dump -Fc custom format) into a target database with pg_restore.
#
# Usage:
#   deploy/restore.sh <DUMP_FILE> <TARGET_DATABASE_URL>
#
# Idempotent by design: `--clean --if-exists` drops the restored objects before
# recreating them, so re-running against the same target is safe. `--no-owner`
# keeps ownership out of the restore (the dump is taken with --no-owner too),
# which is what a target DB whose roles differ from the source requires.
#
# The password is never exposed: it is stripped from the URL passed to
# pg_restore (so /proc/<pid>/cmdline / ps never show it) and passed via
# PGPASSWORD instead, and any URL echoed to the terminal is masked
# (user:***@).
#
# The two seeded admin accounts (admin.1@gear.local / admin.2@gear.local,
# AD-13) are part of the dump and are restored as-is; the documented,
# TESTED-from-deployment proof is `just backup-restore-proof`.
set -euo pipefail

DUMP="${1:-}"
DB_URL="${2:-}"
if [ -z "$DUMP" ] || [ -z "$DB_URL" ]; then
  echo "usage: deploy/restore.sh <DUMP_FILE> <TARGET_DATABASE_URL>" >&2
  exit 1
fi
if [ ! -r "$DUMP" ]; then
  echo "restore: dump file not readable: $DUMP" >&2
  exit 1
fi

# Masked URL for any operator-facing output (user:***@) — never the password.
DISPLAY_URL=$(printf '%s' "$DB_URL" | sed -E 's#(://[^:/@]+):[^@]*@#\1:***@#')
# Password-free URL for the pg_restore command line (password via PGPASSWORD).
RESTORE_URL=$(printf '%s' "$DB_URL" | sed -E 's#(://[^:/@]+):[^@]*@#\1@#')
PGPASSWORD_VALUE=$(printf '%s' "$DB_URL" | sed -nE 's#.*://([^:/@]+):([^@]*)@.*#\2#p')

export PGPASSWORD="${PGPASSWORD_VALUE:-${PGPASSWORD:-}}"

pg_restore --clean --if-exists --no-owner -d "$RESTORE_URL" "$DUMP"
echo "restore: OK — $DUMP restored into ${DISPLAY_URL}"