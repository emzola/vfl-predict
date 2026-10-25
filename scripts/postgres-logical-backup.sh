#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   DATABASE_URL='postgres://...' BACKUP_DIR=/var/backups/bet9ja \
#     ./scripts/postgres-logical-backup.sh
#
# Prefer .pgpass/PGPASSFILE or a secret-injected DATABASE_URL. The URL is never
# written to the backup metadata or echoed by this script.

: "${DATABASE_URL:?DATABASE_URL must be supplied through the environment}"
backup_dir="${BACKUP_DIR:-./backups/postgres}"
mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_file="$backup_dir/vfl-${timestamp}.dump"

umask 077
pg_dump --format=custom --no-owner --no-privileges --dbname="$DATABASE_URL" --file="$backup_file"
pg_restore --list "$backup_file" >/dev/null
printf 'logical backup verified: %s\n' "$backup_file"
