#!/bin/bash
# Run ISC BIND, ISC Kea DHCPv4, and factum2-worker (dns + certs) in the
# same container so dnsmgr2 can write zone/Kea files and reload locally,
# and lego can RFC2136-update the same named.
set -euo pipefail

mkdir -p /etc/bind /var/cache/bind /var/lib/bind /var/lib/dnsmgr2 /var/lib/lego \
	/var/lib/kea /var/lib/dhcp /run/named /run/kea /run/factum2-worker /etc/dnsmgr2 /etc/kea
chown bind:bind /var/cache/bind /var/lib/bind /run/named
rm -f /run/kea/kea4-ctrl-socket /run/kea/kea4-ctrl-socket.lock

if [[ ! -f /etc/bind/rndc.key ]]; then
	rndc-confgen -a -c /etc/bind/rndc.key >/dev/null
fi
chmod 640 /etc/bind/rndc.key
chown root:bind /etc/bind/rndc.key
if [[ ! -f /etc/bind/named.conf.dnsmgr2 ]]; then
	printf '// Written by dnsmgr2. Empty until the first sync.\n' >/etc/bind/named.conf.dnsmgr2
fi
if [[ ! -s /etc/kea/kea-dhcp4.dnsmgr2.json ]]; then
	printf '[]\n' >/etc/kea/kea-dhcp4.dnsmgr2.json
fi

# Isolated TEST-NET-1 for Kea + simulated clients (CAP_NET_ADMIN).
if command -v ip >/dev/null 2>&1 && ip link add br-lab-dhcp type bridge 2>/dev/null; then
	:
fi
if command -v ip >/dev/null 2>&1 && ip link show br-lab-dhcp >/dev/null 2>&1; then
	ip addr replace 192.0.2.1/24 dev br-lab-dhcp
	ip link set br-lab-dhcp up
else
	echo "lab DHCP bridge br-lab-dhcp not available (need CAP_NET_ADMIN)" >&2
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

kea-dhcp4 -c /etc/kea/kea-dhcp4.conf &
KEA4_PID=$!
echo "$KEA4_PID" >/run/kea/kea-dhcp4.pid
for _ in $(seq 1 50); do
	if [[ -S /run/kea/kea4-ctrl-socket ]] && kill -0 "$KEA4_PID" 2>/dev/null; then
		break
	fi
	if ! kill -0 "$KEA4_PID" 2>/dev/null; then
		echo "kea-dhcp4 failed to start" >&2
		wait "$KEA4_PID" || true
		exit 1
	fi
	sleep 0.2
done

/opt/factum2/factum2-worker -f /etc/factum2/factum2-worker.yaml start &
WORKER_PID=$!

# Two veth dhclients on br-lab-dhcp; do not take the container down if
# they cannot get a lease (Kea include may still be empty).
/usr/local/sbin/lab-dhcp-clients &

term() {
	kill "$NAMED_PID" "$KEA4_PID" "$WORKER_PID" 2>/dev/null || true
}
trap term INT TERM

while kill -0 "$NAMED_PID" 2>/dev/null && kill -0 "$KEA4_PID" 2>/dev/null && kill -0 "$WORKER_PID" 2>/dev/null; do
	sleep 1
done
echo "named, kea-dhcp4, or factum2-worker exited" >&2
term
wait || true
exit 1
