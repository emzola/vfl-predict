#!/usr/bin/env bash
set -Eeuo pipefail

# Read-only operational check for a deployed host. It emits machine-readable
# key/value lines, exits 1 for a warning and 2 for a critical disk condition,
# and never prunes Docker objects or changes PostgreSQL data.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
warn_free="${STORAGE_WARN_FREE_PERCENT:-25}"
critical_free="${STORAGE_CRITICAL_FREE_PERCENT:-15}"
exit_code=0

read -r used_percent mount < <(df -P -k / | awk 'NR==2 {gsub(/%/, "", $5); print $5, $6}')
free_percent=$((100 - used_percent))
echo "filesystem_mount=$mount filesystem_free_percent=$free_percent"
if (( free_percent < critical_free )); then
  echo "storage_status=critical"
  exit_code=2
elif (( free_percent < warn_free )); then
  echo "storage_status=warning"
  exit_code=1
else
  echo "storage_status=ok"
fi

if command -v docker >/dev/null 2>&1; then
  docker info --format 'docker_server={{.ServerVersion}} docker_root={{.DockerRootDir}}'
  docker system df
  docker ps --format 'container={{.Names}} status={{.Status}}'
  docker stats --no-stream --format 'container={{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}} memory_percent={{.MemPerc}}' || true
  if [[ -f "$ENV_FILE" && -f "$COMPOSE_FILE" ]]; then
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps
    postgres_id="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps -q postgres 2>/dev/null || true)"
    if [[ -n "$postgres_id" ]]; then
      echo "postgres_health=$(docker inspect -f '{{.State.Health.Status}}' "$postgres_id" 2>/dev/null || echo unknown)"
    fi
  fi
else
  echo "docker_status=unavailable"
  exit_code=1
fi

exit "$exit_code"
