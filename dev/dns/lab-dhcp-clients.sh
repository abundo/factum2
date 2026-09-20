#!/bin/bash
# Lab-only: a bridge with 192.0.2.1/24 and two veth DHCP clients talking
# to kea-dhcp4 on that bridge. Idempotent; safe to re-run.
set -euo pipefail

BR=br-lab-dhcp
GW=192.0.2.1/24

log() { echo "lab-dhcp-clients: $*" >&2; }

need_ip() {
	if ! command -v ip >/dev/null 2>&1; then
		log "iproute2 is missing; cannot simulate DHCP clients"
		return 1
	fi
}

setup_bridge() {
	if ! ip link show "$BR" >/dev/null 2>&1; then
		ip link add "$BR" type bridge
	fi
	ip addr replace "$GW" dev "$BR"
	ip link set "$BR" up
}

setup_veth() {
	local host=$1 mac=$2
	local peer="${host}-br"
	if ! ip link show "$host" >/dev/null 2>&1; then
		ip link add "$host" type veth peer name "$peer"
		ip link set "$peer" master "$BR"
	fi
	ip link set "$host" address "$mac"
	ip link set "$peer" up
	ip link set "$host" up
}

wait_kea() {
	local i
	for i in $(seq 1 50); do
		if [[ -S /run/kea/kea4-ctrl-socket ]]; then
			return 0
		fi
		sleep 0.2
	done
	log "kea-dhcp4 control socket never appeared"
	return 1
}

run_dhclient() {
	local ifname=$1
	local lease="/var/lib/dhcp/dhclient-${ifname}.leases"
	local pidf="/run/dhclient-${ifname}.pid"
	mkdir -p /var/lib/dhcp
	if [[ -f "$pidf" ]]; then
		local old
		old=$(tr -d '[:space:]' <"$pidf" || true)
		if [[ -n "${old:-}" ]] && kill -0 "$old" 2>/dev/null; then
			log "$ifname already has dhclient pid $old"
			return 0
		fi
		rm -f "$pidf"
	fi
	# Foreground until the first ACK, then daemonize so leases renew.
	if ! dhclient -4 -v -lf "$lease" -pf "$pidf" "$ifname"; then
		log "dhclient failed on $ifname"
		return 1
	fi
	return 0
}

if ! need_ip; then
	exit 0
fi
if ! setup_bridge; then
	log "could not create $BR (need CAP_NET_ADMIN); skipping clients"
	exit 0
fi
setup_veth veth-dhcp1 02:00:00:00:00:01
setup_veth veth-dhcp2 02:00:00:00:00:02
wait_kea || exit 0

ok=0
if run_dhclient veth-dhcp1; then
	ok=$((ok + 1))
fi
if run_dhclient veth-dhcp2; then
	ok=$((ok + 1))
fi
ip -4 addr show veth-dhcp1 >&2 || true
ip -4 addr show veth-dhcp2 >&2 || true
log "got leases on $ok/2 interfaces"
exit 0
