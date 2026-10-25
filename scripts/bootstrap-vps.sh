#!/usr/bin/env bash
set -Eeuo pipefail

# Idempotent host preparation. It intentionally does not install credentials,
# change DNS, create firewall rules, or run application containers. Run it as a
# reviewed operator action on a fresh Debian/Ubuntu Contabo VPS.

APP_DIR="${APP_DIR:-/opt/bet9ja-vfl}"
DEPLOY_USER="${DEPLOY_USER:-vfl-deploy}"
APPLY_FIREWALL="${APPLY_FIREWALL:-NO}"

if [[ "$(id -u)" != "0" ]]; then
  echo "run bootstrap as root on the VPS" >&2
  exit 2
fi
if [[ ! "$APP_DIR" = /* || "$APP_DIR" == "/" ]]; then
  echo "APP_DIR must be a specific absolute directory" >&2
  exit 2
fi
command -v docker >/dev/null || { echo "install Docker Engine and the Compose plugin first" >&2; exit 3; }
docker compose version >/dev/null || { echo "Docker Compose plugin is required" >&2; exit 3; }

if ! id "$DEPLOY_USER" >/dev/null 2>&1; then
  useradd --create-home --shell /usr/sbin/nologin "$DEPLOY_USER"
fi
usermod -aG docker "$DEPLOY_USER"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0750 "$APP_DIR"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0750 "$APP_DIR/releases"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0750 "$APP_DIR/backups"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0750 "$APP_DIR/deploy"

if [[ ! -f "$APP_DIR/.env.production" ]]; then
  install -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0600 /dev/null "$APP_DIR/.env.production"
  echo "created empty $APP_DIR/.env.production; populate it from the reviewed example before deployment"
fi

if [[ "$APPLY_FIREWALL" == "YES" ]]; then
  command -v ufw >/dev/null || { echo "ufw is required for APPLY_FIREWALL=YES" >&2; exit 4; }
  echo "Firewall changes are intentionally not automated by this script. Review the runbook and allow SSH, TCP 80, and TCP 443 before applying them." >&2
  exit 4
fi

echo "VPS bootstrap checks passed for $APP_DIR"
echo "Next: install the reviewed Compose/Caddy bundle, configure .env.production, authenticate the deploy user to GHCR with a read-only token, and verify SSH host keys."
