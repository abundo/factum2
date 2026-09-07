#!/bin/sh
# Load netbox-community/netbox-demo-data into the empty netbox DB when
# prepare.py --demo / seed.py --demo requested it (sentinel file).
# docker-entrypoint-initdb.d only runs this on first volume init.
# Must be executable (otherwise the entrypoint sources us and set -e leaks).
set -eu
DUMP=/netbox-demo/netbox-demo.sql
SENTINEL=/netbox-demo/load-demo
if [ ! -f "$SENTINEL" ] || [ ! -s "$DUMP" ]; then
	echo "NetBox demo dump not requested; leaving netbox empty"
	exit 0
fi
echo "Loading NetBox demo data from $DUMP"
psql -q -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname netbox -f "$DUMP"
echo "NetBox demo data loaded"
