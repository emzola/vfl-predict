#!/usr/bin/env bash
set -Eeuo pipefail

# Host-side release deployer. The GitHub workflow uploads a release bundle to
# APP_DIR/releases/RELEASE_SHA and invokes this script over SSH. It never owns
# application secrets; .env.production is created and protected on the VPS.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
RELEASE_SHA="${RELEASE_SHA:?RELEASE_SHA is required}"
BUNDLE_DIR="${BUNDLE_DIR:-$APP_DIR/releases/$RELEASE_SHA}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
DEPLOY_SERVICES="${DEPLOY_SERVICES:-api web caddy live-monitor prediction-worker settlement-worker}"
DRY_RUN="${DRY_RUN:-0}"

if [[ ! "$RELEASE_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]]; then
  echo "RELEASE_SHA must be a Git commit SHA" >&2
  exit 2
fi
if [[ ! "$APP_DIR" = /* || ! "$BUNDLE_DIR" = /* ]]; then
  echo "APP_DIR and BUNDLE_DIR must be absolute paths" >&2
  exit 2
fi
if [[ ! -d "$APP_DIR" || ! -d "$BUNDLE_DIR" ]]; then
  echo "deployment directory is missing" >&2
  exit 2
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing protected production environment: $ENV_FILE" >&2
  exit 2
fi
if [[ -z "${GHCR_IMAGE_PREFIX:-}" ]]; then
  GHCR_IMAGE_PREFIX="$(awk -F= '$1 == "GHCR_IMAGE_PREFIX" {print substr($0, index($0, "=") + 1); exit}' "$ENV_FILE")"
fi
export GHCR_IMAGE_PREFIX
if [[ -z "$GHCR_IMAGE_PREFIX" ]]; then
  echo "GHCR_IMAGE_PREFIX is missing; it must be supplied by the deployment workflow or $ENV_FILE" >&2
  exit 2
fi
if [[ ! -f "$BUNDLE_DIR/docker-compose.production.yml" || ! -f "$BUNDLE_DIR/Caddyfile.production" ]]; then
  echo "release bundle is incomplete" >&2
  exit 2
fi

exec 9>"$APP_DIR/.deployment.lock"
if ! flock -n 9; then
  echo "another production deployment owns the host lock" >&2
  exit 3
fi

run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '+ '
    printf '%q ' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

if [[ "$DRY_RUN" != "1" ]]; then
  install -m 0644 "$BUNDLE_DIR/docker-compose.production.yml" "$COMPOSE_FILE"
  install -m 0644 "$BUNDLE_DIR/Caddyfile.production" "$APP_DIR/Caddyfile.production"
fi

export COMPOSE_FILE
export RELEASE_SHA
export APP_DIR
export COMPOSE_PROJECT_NAME="bet9ja-vfl-production"

compose() {
  run docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

compose config --quiet

if [[ "$DRY_RUN" == "1" ]]; then
  echo "dry-run validation passed for release $RELEASE_SHA"
  exit 0
fi

previous=""
if [[ -f "$APP_DIR/current-release" ]]; then
  previous="$(tr -d '[:space:]' < "$APP_DIR/current-release")"
fi
printf '%s\n' "$previous" > "$APP_DIR/previous-release"

# Pull first. The configured GHCR credential is expected to be a restricted
# read-only credential already available to the deployment user on the VPS.
compose pull migrate api web live-monitor prediction-worker settlement-worker caddy

# The release artifact contains image|digest lines produced by CI. Refuse a
# deployment if a pulled tag does not resolve to the expected immutable digest.
if [[ -f "$BUNDLE_DIR/image-digests.txt" ]]; then
  while IFS='|' read -r image expected || [[ -n "$image" ]]; do
    [[ -z "${image//[[:space:]]/}" ]] && continue
    [[ "$image" == \#* ]] && continue
    full_image="${GHCR_IMAGE_PREFIX:?GHCR_IMAGE_PREFIX is required}/${image}:${RELEASE_SHA}"
    actual="$(docker image inspect "$full_image" --format '{{range .RepoDigests}}{{println .}}{{end}}' | sed -n 's/.*@\(sha256:[0-9a-f]*\).*/\1/p' | head -n 1)"
    if [[ -z "$actual" || "$actual" != "$expected" ]]; then
      echo "digest mismatch for $image: expected $expected, got ${actual:-none}" >&2
      exit 4
    fi
  done < "$BUNDLE_DIR/image-digests.txt"
fi

# Never recreate or remove the database volume. Start/reuse PostgreSQL, wait
# for its healthcheck, and run exactly one migration container.
compose up -d --no-deps postgres
for attempt in $(seq 1 60); do
  postgres_id="$(compose ps -q postgres)"
  if [[ -n "$postgres_id" ]] && [[ "$(docker inspect -f '{{.State.Health.Status}}' "$postgres_id" 2>/dev/null || true)" == "healthy" ]]; then
    break
  fi
  if [[ "$attempt" == "60" ]]; then
    echo "PostgreSQL did not become healthy" >&2
    exit 5
  fi
  sleep 2
done
compose run --rm --no-deps migrate

# Compose sends the configured stop grace period before replacing a service,
# allowing workers to release their leases. The service list is intentionally
# overridable so an operator can deploy only a frontend/API change.
compose up -d --no-deps $DEPLOY_SERVICES

for service in api web caddy; do
  for attempt in $(seq 1 45); do
    service_id="$(compose ps -q "$service")"
    status=""
    if [[ -n "$service_id" ]]; then
      status="$(docker inspect -f '{{.State.Health.Status}}' "$service_id" 2>/dev/null || true)"
    fi
    if [[ "$status" == "healthy" ]]; then
      break
    fi
    if [[ "$attempt" == "45" ]]; then
      echo "$service did not become healthy" >&2
      compose ps
      exit 6
    fi
    sleep 2
  done
done

for service in $DEPLOY_SERVICES; do
  service_id="$(compose ps -q "$service")"
  [[ -n "$service_id" ]] || { echo "service $service has no container" >&2; exit 7; }
  release_label="$(docker inspect -f '{{index .Config.Labels "com.bet9ja-vfl.release"}}' "$service_id" 2>/dev/null || true)"
  [[ "$release_label" == "$RELEASE_SHA" ]] || { echo "release label mismatch for $service" >&2; exit 7; }
done

printf '%s\n' "$RELEASE_SHA" > "$APP_DIR/current-release"
cat > "$APP_DIR/last-deploy" <<EOF
release=$RELEASE_SHA
previous=${previous:-none}
completed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
chmod 600 "$APP_DIR/last-deploy" "$APP_DIR/previous-release" "$APP_DIR/current-release"
echo "production release $RELEASE_SHA deployed"
