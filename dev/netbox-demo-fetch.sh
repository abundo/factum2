#!/bin/sh
# Download the PostgreSQL dump from netbox-community/netbox-demo-data that
# matches this lab's NetBox image (compose.yml NETBOX_VERSION, default v4.3).
# Writes dev/data/netbox/netbox-demo.sql for postgres/init/02-netbox-demo.sh
# and seed.sh. Idempotent: skips when the cached file matches the minor version.
set -eu
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
if [ -f "$DIR/.env" ]; then
	set -a
	# shellcheck source=/dev/null
	. "$DIR/.env"
	set +a
fi

NETBOX_VERSION=${NETBOX_VERSION:-v4.3-3.3.0}
# docker tag v4.3-3.3.0 -> dump netbox-demo-v4.3.sql
NETBOX_DEMO_MINOR=${NETBOX_VERSION%%-*}
DUMP_DIR="$DIR/data/netbox"
DUMP="$DUMP_DIR/netbox-demo.sql"
STAMP="$DUMP_DIR/version"
URL="https://raw.githubusercontent.com/netbox-community/netbox-demo-data/master/sql/netbox-demo-${NETBOX_DEMO_MINOR}.sql"

mkdir -p "$DUMP_DIR"
if [ -s "$DUMP" ] && [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$NETBOX_DEMO_MINOR" ]; then
	exit 0
fi

printf '==> Downloading NetBox demo data (%s)\n' "$NETBOX_DEMO_MINOR"
curl -fL --retry 3 --retry-delay 2 -o "$DUMP.partial" "$URL"
sz=$(wc -c <"$DUMP.partial" | tr -d ' ')
if [ "$sz" -lt 1000000 ]; then
	rm -f "$DUMP.partial"
	echo "NetBox demo dump from $URL is too small (${sz} bytes)" >&2
	exit 1
fi
mv "$DUMP.partial" "$DUMP"
printf '%s\n' "$NETBOX_DEMO_MINOR" >"$STAMP"
