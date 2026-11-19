#!/usr/bin/env bash
# stop.sh — tear down the GL app's local infra (Postgres).
#
#   ./.mycel.infra/stop.sh        stop + remove the containers (keeps the data volume)
#   ./.mycel.infra/stop.sh -v     also drop the volume (wipes the database)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
COMPOSE="$HERE/docker-compose.yml"

DOWN_ARGS=()
if [ "${1:-}" = "-v" ]; then
  DOWN_ARGS+=(-v)
  echo "▶ docker compose down -v — removing containers AND the data volume"
else
  echo "▶ docker compose down — removing containers (data volume kept)"
fi

docker compose -f "$COMPOSE" down "${DOWN_ARGS[@]}"
echo "✓ down."
