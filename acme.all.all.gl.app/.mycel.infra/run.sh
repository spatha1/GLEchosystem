#!/usr/bin/env bash
# run.sh — bring the GL app up locally.
#
#   1) Start Postgres via .mycel.infra/docker-compose.yml (generated and owned by mycel).
#   2) Wait for it to be healthy.
#   3) Run the app natively with `mycel app run` (the core + each module server).
#
# Requires docker (compose plugin) and a configured `mycel` CLI. Without the CLI, run a
# module through its development adapter instead, e.g. for gl:
#   DATABASE_URL=postgres://<ordinary role>@localhost:<port>/<db> go run ./modules/gl/module/service/cmd/devhttp
# Tear down with ./.mycel.infra/stop.sh.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
APP="$(cd "$HERE/.." && pwd)"
COMPOSE="$HERE/docker-compose.yml"

if ! command -v mycel >/dev/null 2>&1; then
  echo "✗ the mycel CLI is not installed. Use a module's development adapter (see the header of this script)." >&2
  exit 1
fi

# DATABASE_URL must name an ORDINARY role (not superuser, not BYPASSRLS): tenant isolation
# relies on row-level security.
export DATABASE_URL="${DATABASE_URL:?set DATABASE_URL to an ordinary (non-superuser) role}"

echo "▶ docker compose up — Postgres for acme.all.all.gl.app"
docker compose -f "$COMPOSE" up -d db

echo "▶ waiting for Postgres to be healthy…"
for _ in $(seq 1 60); do
  status="$(docker compose -f "$COMPOSE" ps --format '{{.Health}}' db 2>/dev/null || true)"
  [ "$status" = "healthy" ] && { echo "  ✓ Postgres healthy"; break; }
  sleep 2
done

echo "▶ mycel app run"
cd "$APP"
exec mycel app run
