#!/bin/sh
# Create dest-file dirs, hub TLS, copy Oxidized config, fetch NetBox demo dump
# before compose starts (postgres/init/02-netbox-demo.sh loads it on first init).
set -eu
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
mkdir -p "$DIR/data/icinga" "$DIR/data/oxidized" "$DIR/data/dns" "$DIR/data/netbox" "$DIR/certs"
chmod +x "$DIR/container/dnsmgr2" "$DIR/compose.sh" "$DIR/seed.sh" "$DIR/bin/dnsmgr2" \
	"$DIR/netbox-demo-fetch.sh" "$DIR/postgres/init/02-netbox-demo.sh" 2>/dev/null || true
"$DIR/netbox-demo-fetch.sh"

if [ ! -f "$DIR/certs/hub.crt" ] || [ ! -f "$DIR/certs/hub.key" ]; then
	openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
		-keyout "$DIR/certs/hub.key" -out "$DIR/certs/hub.crt" \
		-subj "/CN=factum-worker" \
		-addext "subjectAltName=DNS:factum-worker,DNS:localhost,IP:127.0.0.1" \
		>/dev/null 2>&1
	chmod 644 "$DIR/certs/hub.crt" "$DIR/certs/hub.key"
fi
cp "$DIR/oxidized/config" "$DIR/data/oxidized/config"
if [ ! -s "$DIR/data/icinga/hosts.conf" ]; then
	printf '%s\n' '// Written by factum2-icinga. Empty until the first sync.' >"$DIR/data/icinga/hosts.conf"
fi
if [ ! -s "$DIR/data/icinga/users.conf" ]; then
	printf '%s\n' '// Written by factum2-icinga. Empty until the first sync.' >"$DIR/data/icinga/users.conf"
fi
if [ ! -s "$DIR/data/oxidized/router.db" ]; then
	printf '%s\n' 'lab-dummy:127.0.0.1:ios' >"$DIR/data/oxidized/router.db"
fi
if [ ! -s "$DIR/data/dns/records" ]; then
	printf '%s\n' '# Written by factum2-dns. Empty until the first sync.' >"$DIR/data/dns/records"
fi
chmod 666 "$DIR/data/icinga/hosts.conf" "$DIR/data/icinga/users.conf" \
	"$DIR/data/oxidized/router.db" "$DIR/data/oxidized/config" 2>/dev/null || true
chmod 777 "$DIR/data/icinga" "$DIR/data/oxidized" "$DIR/data/dns" 2>/dev/null || true
