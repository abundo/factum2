#!/bin/bash
# Run Icinga 2 and factum2-worker in the same container so factum2-icinga
# writes /factum/*.conf locally and reloads icinga2 on this host.
set -euo pipefail

mkdir -p /run/factum2-worker /factum
chown icinga:icinga /run/factum2-worker /factum || true

runuser -u icinga -- /usr/local/bin/entrypoint.sh icinga2 daemon &
DEST_PID=$!

/opt/factum2/factum2-worker -f /etc/factum2/factum2-worker.yaml start &
WORKER_PID=$!

term() {
	kill "$DEST_PID" "$WORKER_PID" 2>/dev/null || true
}
trap term INT TERM

while kill -0 "$DEST_PID" 2>/dev/null && kill -0 "$WORKER_PID" 2>/dev/null; do
	sleep 1
done
echo "icinga2 or factum2-worker exited" >&2
term
wait || true
exit 1
