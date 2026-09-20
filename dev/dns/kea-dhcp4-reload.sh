#!/bin/bash
# Reload or probe kea-dhcp4. dnsmgr2 RunCommand does not invoke a shell,
# so cmd_restart / cmd_status must be this script (optional "status").
set -euo pipefail

PIDFILE=/run/kea/kea-dhcp4.pid
if [[ ! -f "$PIDFILE" ]]; then
	echo "kea-dhcp4 pid file missing: $PIDFILE" >&2
	exit 1
fi
pid=$(tr -d '[:space:]' <"$PIDFILE")
if [[ -z "$pid" ]]; then
	echo "kea-dhcp4 pid file is empty" >&2
	exit 1
fi
if [[ "${1:-}" == "status" ]]; then
	kill -0 "$pid"
	exit 0
fi
kill -HUP "$pid"
