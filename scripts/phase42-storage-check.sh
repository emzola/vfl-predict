#!/usr/bin/env bash
set -euo pipefail

# Read-only VPS check. Provide DATABASE_URL through the environment or a
# process/service secret, never by committing it to this repository.
if [[ -z "${DATABASE_URL:-}" ]]; then
  echo "DATABASE_URL is required for PostgreSQL storage checks" >&2
  exit 2
fi

warn_free_percent="${STORAGE_WARN_FREE_PERCENT:-25}"
critical_free_percent="${STORAGE_CRITICAL_FREE_PERCENT:-15}"
root_mount="${STORAGE_ROOT_MOUNT:-/}"
read -r filesystem_percent filesystem_mount < <(df -P -k "$root_mount" | awk 'NR==2 {gsub(/%/, "", $5); print $5, $6}')
free_percent=$((100 - filesystem_percent))
echo "filesystem mount=${filesystem_mount} free_percent=${free_percent}"
if (( free_percent < critical_free_percent )); then
  echo "CRITICAL: filesystem free space is below ${critical_free_percent}%" >&2
  exit 2
elif (( free_percent < warn_free_percent )); then
  echo "WARNING: filesystem free space is below ${warn_free_percent}%" >&2
fi

if command -v docker >/dev/null 2>&1; then
  docker system df
  docker info --format 'docker_server={{.ServerVersion}} root={{.DockerRootDir}}'
  docker ps -a --format 'container={{.Names}} status={{.Status}} size={{.Size}}'
  docker stats --no-stream --format 'container={{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}} memory_percent={{.MemPerc}}' || true
fi

# Prefer a compiled command on the VPS; fall back to go run for an operator
# checkout. Both paths are read-only with respect to PostgreSQL.
if command -v phase42-storage-audit >/dev/null 2>&1; then
  phase42-storage-audit -database-url "$DATABASE_URL" -include-docker -max-wal-bytes "${STORAGE_MAX_WAL_BYTES:-2147483648}"
elif command -v go >/dev/null 2>&1 && [[ -f go.mod ]]; then
  go run ./cmd/phase42-storage-audit -database-url "$DATABASE_URL" -include-docker -max-wal-bytes "${STORAGE_MAX_WAL_BYTES:-2147483648}"
else
  echo "phase42-storage-audit binary or Go checkout is required" >&2
  exit 2
fi
