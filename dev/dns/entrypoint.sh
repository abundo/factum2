#!/bin/bash
# Run ISC BIND and factum2-worker (factum-dns) in the same container so
# dnsmgr2 can write zone files and rndc-reload named locally.
set -euo pipefail

mkdir -p /etc/bind /var/cache/bind /var/lib/bind /var/lib/dnsmgr2 \
	/run/named /run/factum2-worker /etc/dnsmgr2
chown bind:bind /var/cache/bind /var/lib/bind /run/named

if [[ ! -f /etc/bind/rndc.key ]]; then
	rndc-confgen -a -c /etc/bind/rndc.key >/dev/null
	chmod 640 /etc/bind/rndc.key
	chown root:bind /etc/bind/rndc.key
fi
if [[ ! -f /etc/bind/named.conf.dnsmgr2 ]]; then
	printf '// Written by dnsmgr2. Empty until the first sync.\n' >/etc/bind/named.conf.dnsmgr2
fi

named -g -u bind -c /etc/bind/named.conf &
NAMED_PID=$!

for _ in $(seq 1 50); do
	if rndc -k /etc/bind/rndc.key status >/dev/null 2>&1; then
		break
	fi
	if ! kill -0 "$NAMED_PID" 2>/dev/null; then
		echo "named failed to start" >&2
		wait "$NAMED_PID" || true
		exit 1
	fi
	sleep 0.2
done

/opt/factum2/factum2-worker -f /etc/factum2/factum2-dns-worker.yaml start &
WORKER_PID=$!

term() {
	kill "$NAMED_PID" "$WORKER_PID" 2>/dev/null || true
}
trap term INT TERM

while kill -0 "$NAMED_PID" 2>/dev/null && kill -0 "$WORKER_PID" 2>/dev/null; do
	sleep 1
done
echo "named or factum2-worker exited" >&2
term
wait || true
exit 1
