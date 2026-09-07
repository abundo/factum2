#!/bin/sh
# Seed NetBox from YAML via the REST API. Copy netbox-seed.example.yaml to
# netbox-seed.yaml (gitignored) and edit; seed.py runs that file when present.
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
exec python3 "$DIR/netbox_seed.py" --wait "$@"
