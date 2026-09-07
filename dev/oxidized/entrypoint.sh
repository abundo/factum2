#!/bin/bash
# Restore a dummy router.db if sync (or a wipe) left it empty. Oxidized 0.37
# exits with NoNodesFound otherwise, and oxidized-web dies with it.
set -euo pipefail
HOME_DIR="${OXIDIZED_HOME:-/home/oxidized/.config/oxidized}"
ROUTER_DB="$HOME_DIR/router.db"
mkdir -p "$HOME_DIR"
if [ ! -s "$ROUTER_DB" ]; then
	printf 'lab-dummy:127.0.0.1:ios\n' >"$ROUTER_DB"
	chmod 666 "$ROUTER_DB" || true
fi
exec runsvdir -P /etc/service
