#!/usr/bin/env bash
set -Eeuo pipefail

# Host-side Phase 6 promotion. This is intentionally separate from the normal
# release deployer: it changes the model policy and restarts only the
# prediction worker after an approved registry selection.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
RELEASE_SHA="${RELEASE_SHA:-}"
GHCR_IMAGE_PREFIX="${GHCR_IMAGE_PREFIX:-}"
ARTIFACT_ID="${ARTIFACT_ID:-}"
SELECTED_BY="${SELECTED_BY:-operator}"
REASON="${REASON:-Phase 6 acceptance gates reviewed}"
MODEL_NAME="${MODEL_NAME:-dynamic_bayesian_team_strength}"
MODEL_VERSION="${MODEL_VERSION:-phase6.v1}"
CONFIRM_PROMOTION="${CONFIRM_PROMOTION:-}"
EVALUATION_REPORT="${PHASE6_EVALUATION_REPORT:-$APP_DIR/phase6/phase6-replay-report.json}"
SHADOW_REPORT="${PHASE6_SHADOW_REPORT:-$APP_DIR/phase6/phase6-shadow-report.json}"

fail() {
  echo "phase6 promotion: $*" >&2
  exit 2
}

[[ "$CONFIRM_PROMOTION" == "YES" ]] || fail "set CONFIRM_PROMOTION=YES to authorize the production model switch"
[[ "$MODEL_NAME" == "dynamic_bayesian_team_strength" ]] || fail "MODEL_NAME must be dynamic_bayesian_team_strength"
[[ "$MODEL_VERSION" == "phase6.v1" ]] || fail "MODEL_VERSION must be phase6.v1"
[[ "$ARTIFACT_ID" =~ ^[0-9a-fA-F-]{36}$ ]] || fail "ARTIFACT_ID must be a model_artifact UUID"
[[ -d "$APP_DIR" && -f "$ENV_FILE" && -f "$COMPOSE_FILE" ]] || fail "production application files are missing"
[[ -s "$EVALUATION_REPORT" ]] || fail "Phase 6 evaluation report is missing: $EVALUATION_REPORT"
[[ -s "$SHADOW_REPORT" ]] || fail "Phase 6 shadow report is missing: $SHADOW_REPORT"

if [[ -z "$RELEASE_SHA" && -f "$APP_DIR/current-release" ]]; then
  RELEASE_SHA="$(tr -d '[:space:]' < "$APP_DIR/current-release")"
fi
[[ "$RELEASE_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]] || fail "RELEASE_SHA must identify the deployed Phase 6 image"
[[ -n "$GHCR_IMAGE_PREFIX" ]] || fail "GHCR_IMAGE_PREFIX is required for the deployed release"

export APP_DIR ENV_FILE COMPOSE_FILE RELEASE_SHA GHCR_IMAGE_PREFIX
export COMPOSE_PROJECT_NAME="bet9ja-vfl-production"

exec 9>"$APP_DIR/.model-promotion.lock"
flock -n 9 || fail "another model promotion owns the host lock"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

compose config --quiet

# The promotion binary is included in the deployed API image. It applies any
# pending migrations under the existing migration lock and refuses artifacts
# that are not already approved.
compose run --rm --no-deps --entrypoint /app/phase6-promote migrate \
  -artifact-id "$ARTIFACT_ID" \
  -selected-by "$SELECTED_BY" \
  -reason "$REASON"

# Keep a recoverable copy of the protected environment before changing only
# the model policy. No secret values are printed.
backup="$ENV_FILE.phase6-pre-promotion.bak"
cp --preserve=mode,ownership "$ENV_FILE" "$backup"
tmp="$(mktemp "$ENV_FILE.phase6.XXXXXX")"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT
awk -v model="$MODEL_NAME" '
  BEGIN { found = 0 }
  /^[[:space:]]*PREDICTION_MODEL=/ {
    if (!found) { print "PREDICTION_MODEL=" model; found = 1 }
    next
  }
  { print }
  END { if (!found) print "PREDICTION_MODEL=" model }
' "$ENV_FILE" > "$tmp"
chmod 600 "$tmp"
chown --reference="$ENV_FILE" "$tmp" 2>/dev/null || true
mv -f "$tmp" "$ENV_FILE"

# Only the prediction worker is restarted. Other services and the PostgreSQL
# volume are left untouched.
compose up -d --no-deps prediction-worker
worker_id="$(compose ps -q prediction-worker)"
[[ -n "$worker_id" ]] || fail "prediction worker has no container after promotion"
[[ "$(docker inspect -f '{{.State.Running}}' "$worker_id")" == "true" ]] || fail "prediction worker is not running after promotion"
actual_model="$(docker inspect "$worker_id" --format '{{range .Config.Env}}{{println .}}{{end}}' | awk -F= '$1 == "PREDICTION_MODEL" {print $2; exit}')"
[[ "$actual_model" == "$MODEL_NAME" ]] || fail "prediction worker model is '$actual_model', expected '$MODEL_NAME'"

echo "Phase 6 production model selected: $MODEL_NAME $MODEL_VERSION"
echo "artifact=$ARTIFACT_ID"
echo "worker=$worker_id"
echo "environment_backup=$backup"
