#!/usr/bin/env bash
set -euo pipefail

# Restore is intentionally guarded because it overwrites database objects.
# Run only against an explicitly selected disposable/recovery database:
#   CONFIRM_RESTORE=YES DATABASE_URL='postgres://...' BACKUP_FILE=... \
#     ./scripts/postgres-logical-restore.sh

: "${DATABASE_URL:?DATABASE_URL must be supplied through the environment}"
: "${BACKUP_FILE:?BACKUP_FILE must point to an existing custom-format dump}"
if [[ "${CONFIRM_RESTORE:-}" != "YES" ]]; then
  echo 'Refusing restore: set CONFIRM_RESTORE=YES after verifying the target database.' >&2
  exit 2
fi
if [[ ! -f "$BACKUP_FILE" ]]; then
  echo "Backup file does not exist: $BACKUP_FILE" >&2
  exit 2
fi

pg_restore --list "$BACKUP_FILE" >/dev/null
pg_restore --clean --if-exists --no-owner --no-privileges --dbname="$DATABASE_URL" "$BACKUP_FILE"
printf 'logical restore completed from: %s\n' "$BACKUP_FILE"
