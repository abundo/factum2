#!/bin/sh
# Load netbox-community/netbox-demo-data into the empty netbox DB.
# docker-entrypoint-initdb.d only runs this on first volume init.
# Must be executable (otherwise the entrypoint sources us and set -e leaks).
set -eu
DUMP=/netbox-demo/netbox-demo.sql
if [ ! -s "$DUMP" ]; then
	echo "NetBox demo dump not mounted at $DUMP; leaving netbox empty"
	exit 0
fi
echo "Loading NetBox demo data from $DUMP"
psql -q -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname netbox -f "$DUMP"
echo "NetBox demo data loaded"
