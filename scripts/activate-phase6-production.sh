#!/usr/bin/env bash
set -Eeuo pipefail

# Direct Phase 6 activation requested by the operator. This deliberately skips
# historical replay and shadow evaluation, but keeps the activation auditable,
# locked, digest-pinned, and limited to the prediction worker.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
RELEASE_SHA="${RELEASE_SHA:-}"
GHCR_IMAGE_PREFIX="${GHCR_IMAGE_PREFIX:-}"
SELECTED_BY="${SELECTED_BY:-github-actions-direct}"
REASON="${REASON:-Direct Phase 6 activation without replay or shadow evaluation}"
CONFIRM_ACTIVATION="${CONFIRM_ACTIVATION:-}"

fail() {
  echo "phase6 direct activation: $*" >&2
  exit 2
}

[[ "$CONFIRM_ACTIVATION" == "YES" ]] || fail "CONFIRM_ACTIVATION=YES is required"
[[ "$RELEASE_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]] || fail "RELEASE_SHA must identify the deployed release"
[[ -n "$GHCR_IMAGE_PREFIX" ]] || fail "GHCR_IMAGE_PREFIX is required"
[[ -f "$ENV_FILE" && -f "$COMPOSE_FILE" ]] || fail "production application files are missing"
[[ -f "$APP_DIR/releases/$RELEASE_SHA/image-digests.txt" ]] || fail "release digest manifest is missing"

export APP_DIR ENV_FILE COMPOSE_FILE RELEASE_SHA GHCR_IMAGE_PREFIX
export COMPOSE_PROJECT_NAME="bet9ja-vfl-production"

exec 9>"$APP_DIR/.model-promotion.lock"
flock -n 9 || fail "another model promotion owns the host lock"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

compose config --quiet

api_digest_ref="$(awk -F'|' '$1 == "vfl-api" { print $2; exit }' "$APP_DIR/releases/$RELEASE_SHA/image-digests.txt")"
api_digest="${api_digest_ref#sha256:}"
[[ "$api_digest" =~ ^[0-9a-fA-F]{64}$ ]] || fail "API image digest is missing or invalid"

echo "activating the dynamic Bayesian model directly; replay and shadow evaluation are intentionally skipped"
compose run --rm --no-deps --entrypoint /app/phase6-activate migrate \
  -artifact-path "$GHCR_IMAGE_PREFIX/vfl-api@sha256:$api_digest" \
  -artifact-sha256 "$api_digest" \
  -selected-by "$SELECTED_BY" \
  -reason "$REASON" \
  -confirm YES

backup="$ENV_FILE.phase6-direct-activation.bak"
cp --preserve=mode,ownership "$ENV_FILE" "$backup"
tmp="$(mktemp "$ENV_FILE.phase6-direct.XXXXXX")"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT
awk '
  BEGIN { found = 0 }
  /^[[:space:]]*PREDICTION_MODEL=/ {
    if (!found) { print "PREDICTION_MODEL=dynamic_bayesian_team_strength"; found = 1 }
    next
  }
  { print }
  END { if (!found) print "PREDICTION_MODEL=dynamic_bayesian_team_strength" }
' "$ENV_FILE" > "$tmp"
chmod 600 "$tmp"
chown --reference="$ENV_FILE" "$tmp" 2>/dev/null || true
mv -f "$tmp" "$ENV_FILE"

# Only the prediction worker is restarted. PostgreSQL, the database volume,
# the API, frontend, and provider workers remain untouched.
compose up -d --no-deps prediction-worker
worker_id="$(compose ps -q prediction-worker)"
[[ -n "$worker_id" ]] || fail "prediction worker has no container after activation"
[[ "$(docker inspect -f '{{.State.Running}}' "$worker_id")" == "true" ]] || fail "prediction worker is not running after activation"
actual_model="$(docker inspect "$worker_id" --format '{{range .Config.Env}}{{println .}}{{end}}' | awk -F= '$1 == "PREDICTION_MODEL" {print $2; exit}')"
[[ "$actual_model" == "dynamic_bayesian_team_strength" ]] || fail "prediction worker model is '$actual_model', expected dynamic_bayesian_team_strength"

echo "Phase 6 production model directly activated: dynamic_bayesian_team_strength phase6.v1"
echo "backup=$backup"
