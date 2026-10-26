#!/usr/bin/env bash
set -Eeuo pipefail

# Workflow-owned Phase 6 promotion. It runs the required production-side
# replay and shadow gates, registers and approves the exact deployed API
# image, then delegates the atomic model switch to the audited promotion
# script. If any gate fails, the existing worker policy remains in place.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/docker-compose.production.yml}"
RELEASE_SHA="${RELEASE_SHA:-}"
GHCR_IMAGE_PREFIX="${GHCR_IMAGE_PREFIX:-}"
PHASE6_DIR="${PHASE6_DIR:-$APP_DIR/phase6}"
SELECTED_BY="${SELECTED_BY:-github-actions}"
APPROVED_BY="${APPROVED_BY:-github-actions}"

fail() {
  echo "phase6 automatic promotion: $*" >&2
  exit 2
}

[[ "$RELEASE_SHA" =~ ^[0-9a-fA-F]{7,64}$ ]] || fail "RELEASE_SHA must identify the deployed release"
[[ -n "$GHCR_IMAGE_PREFIX" ]] || fail "GHCR_IMAGE_PREFIX is required"
[[ -f "$ENV_FILE" && -f "$COMPOSE_FILE" ]] || fail "production application files are missing"
[[ -f "$APP_DIR/releases/$RELEASE_SHA/image-digests.txt" ]] || fail "release digest manifest is missing"
[[ -f "$APP_DIR/releases/$RELEASE_SHA/promote-phase6-production.sh" ]] || fail "promotion script is missing"

export APP_DIR ENV_FILE COMPOSE_FILE RELEASE_SHA GHCR_IMAGE_PREFIX
export COMPOSE_PROJECT_NAME="bet9ja-vfl-production"

exec 9>"$APP_DIR/.model-auto-promotion.lock"
flock -n 9 || fail "another model promotion owns the host lock"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

compose config --quiet
mkdir -p "$PHASE6_DIR"
chmod 0750 "$PHASE6_DIR"

api_digest_ref="$(awk -F'|' '$1 == "vfl-api" { print $2; exit }' "$APP_DIR/releases/$RELEASE_SHA/image-digests.txt")"
api_digest="${api_digest_ref#sha256:}"
[[ "$api_digest" =~ ^[0-9a-fA-F]{64}$ ]] || fail "API image digest is missing or invalid"

evaluation_report="$PHASE6_DIR/phase6-replay-report.json"
shadow_report="$PHASE6_DIR/phase6-shadow-report.json"
run_user="$(id -u):$(id -g)"

echo "running Phase 6 chronological replay against the deployed database"
compose run --rm -T --no-deps --user "$run_user" \
  -v "$PHASE6_DIR:/var/phase6" \
  --entrypoint /app/phase6-evaluate phase6-research \
  -report /var/phase6/phase6-replay-report.json \
  -bootstrap-replicates 100
[[ -s "$evaluation_report" ]] || fail "Phase 6 replay report was not created"

echo "running a pre-cutoff shadow prediction for the next eligible round"
compose run --rm -T --no-deps --user "$run_user" \
  --entrypoint /app/phase6-shadow phase6-research \
  > "$shadow_report"
[[ -s "$shadow_report" ]] || fail "Phase 6 shadow report was not created"
grep -Eq '"status"[[:space:]]*:[[:space:]]*"generated"' "$shadow_report" || fail "shadow gate did not generate a prediction run"
grep -Eq '"generated_count"[[:space:]]*:[[:space:]]*8' "$shadow_report" || fail "shadow gate did not produce all eight fixtures"

echo "registering the exact deployed API image as the Phase 6 candidate"
registration_output="$(compose run --rm -T --no-deps --user "$run_user" \
  -v "$PHASE6_DIR:/var/phase6:ro" \
  --entrypoint /app/phase6-register phase6-research \
  -report /var/phase6/phase6-replay-report.json \
  -artifact-path "$GHCR_IMAGE_PREFIX/vfl-api@sha256:$api_digest" \
  -artifact-sha256 "$api_digest")"
printf '%s\n' "$registration_output"
artifact_id="$(printf '%s\n' "$registration_output" | sed -n 's/^registered Phase 6 candidate artifact //p' | tail -n 1)"
[[ "$artifact_id" =~ ^[0-9a-fA-F-]{36}$ ]] || fail "candidate artifact ID was not returned"

echo "approving the candidate after replay and shadow gates passed"
compose run --rm -T --no-deps --user "$run_user" \
  --entrypoint /app/phase6-approve phase6-research \
  -artifact-id "$artifact_id" \
  -approved-by "$APPROVED_BY" \
  -confirm YES \
  -reason "Automated main-branch deployment replay and pre-cutoff shadow gates passed"

CONFIRM_PROMOTION=YES \
ARTIFACT_ID="$artifact_id" \
SELECTED_BY="$SELECTED_BY" \
REASON="Automated main-branch deployment after Phase 6 replay and shadow gates" \
PHASE6_EVALUATION_REPORT="$evaluation_report" \
PHASE6_SHADOW_REPORT="$shadow_report" \
bash "$APP_DIR/releases/$RELEASE_SHA/promote-phase6-production.sh"

echo "Phase 6 automatic production promotion completed for release $RELEASE_SHA"
