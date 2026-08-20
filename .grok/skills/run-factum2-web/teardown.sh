#!/usr/bin/env bash
# Stops the isolated factum2-web instance started by setup.sh. Leaves the
# factum2_skilltest DB/role in place (setup.sh reuses them - drop them
# manually if you want a completely clean slate, see SKILL.md).
#
# Kills by port, not by the "go run" pid: "go run" execs a separate
# compiled binary as a child, so the wrapper's pid alone doesn't reliably
# reach the actual server process.
set -euo pipefail

BIND_PORT=18090

PIDS="$(lsof -ti:"$BIND_PORT" -sTCP:LISTEN 2>/dev/null || true)"
if [ -n "$PIDS" ]; then
  echo "$PIDS" | xargs -r kill
  echo "==> Stopped process(es) on port $BIND_PORT: $PIDS"
else
  echo "==> Nothing listening on port $BIND_PORT"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
rm -f "$SCRIPT_DIR/.state/backend.pid"
