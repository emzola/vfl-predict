#!/usr/bin/env bash
set -Eeuo pipefail

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
TARGET_SHA="${TARGET_SHA:-}"
CONFIRM_ROLLBACK="${CONFIRM_ROLLBACK:-}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
SERVICES="${ROLLBACK_SERVICES:-api web caddy live-monitor prediction-worker settlement-worker}"

if [[ "$CONFIRM_ROLLBACK" != "YES" ]]; then
  echo "set CONFIRM_ROLLBACK=YES to authorize an image rollback" >&2
  exit 2
fi
if [[ -z "$TARGET_SHA" && -f "$APP_DIR/previous-release" ]]; then
  TARGET_SHA="$(tr -d '[:space:]' < "$APP_DIR/previous-release")"
fi
if [[ ! "$TARGET_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]]; then
  echo "TARGET_SHA must identify a retained release" >&2
  exit 2
fi
BUNDLE_DIR="$APP_DIR/releases/$TARGET_SHA"
if [[ ! -f "$BUNDLE_DIR/docker-compose.production.yml" || ! -f "$BUNDLE_DIR/Caddyfile.production" ]]; then
  echo "retained release bundle not found: $BUNDLE_DIR" >&2
  exit 2
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing protected production environment: $ENV_FILE" >&2
  exit 2
fi

exec 9>"$APP_DIR/.deployment.lock"
flock -n 9 || { echo "another deployment owns the host lock" >&2; exit 3; }
install -m 0644 "$BUNDLE_DIR/docker-compose.production.yml" "$COMPOSE_FILE"
install -m 0644 "$BUNDLE_DIR/Caddyfile.production" "$APP_DIR/Caddyfile.production"
export COMPOSE_FILE RELEASE_SHA="$TARGET_SHA" COMPOSE_PROJECT_NAME="bet9ja-vfl-production"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" pull $SERVICES
# This is an image rollback only. It deliberately does not run a reverse
# migration; schema compatibility must be confirmed before this command.
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps $SERVICES
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps
printf '%s\n' "$TARGET_SHA" > "$APP_DIR/current-release"
echo "image rollback requested for $TARGET_SHA; database schema was not rolled back"
