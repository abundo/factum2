#!/bin/bash
# Run Prometheus and factum2-worker in the same container so
# factum2-prometheus writes file_sd JSON and POSTs /-/reload locally.
set -euo pipefail

mkdir -p /run/factum2-worker /prometheus /etc/prometheus

/bin/prometheus \
	--config.file=/etc/prometheus/prometheus.yml \
	--storage.tsdb.path=/prometheus \
	--web.enable-lifecycle \
	--web.listen-address=0.0.0.0:9090 &
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
echo "prometheus or factum2-worker exited" >&2
term
wait || true
exit 1
