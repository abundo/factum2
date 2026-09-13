#!/bin/sh
# Bring up the laptop lab after prepare.py / binaries / frontend.
# Invoked by `make dev-up`. Prints elapsed seconds per step.
set -eu
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
cd "$DIR"

start=${FACTUM_DEV_UP_START:-$(date +%s)}
step() { echo "==> $1 +$(($(date +%s)-start))s"; }

COMPOSE="./compose.sh"
LAB_DBS="${LAB_DBS:-postgres mysql redis icingadb-redis}"
# Sidecars (netbox-worker, librenms-dispatcher) start with LAB_CORE but are
# not in the --wait set: they cannot become healthy until a parent is, so
# waiting on them serializes extra healthchecks after NetBox/LibreNMS.
LAB_APPS="${LAB_APPS:-netbox librenms icinga oxidized prometheus snmp-exporter alertmanager grafana dns portal}"
LAB_SIDECARS="${LAB_SIDECARS:-netbox-worker librenms-dispatcher}"
LAB_ICINGA_WEB="${LAB_ICINGA_WEB:-icingadb icingaweb}"
LAB_CORE="${LAB_CORE:-$LAB_DBS $LAB_APPS $LAB_SIDECARS}"

step compose
# Start apps first so NetBox/LibreNMS initialize while we wait only for DBs,
# create Icinga MariaDB DBs, and build the factum image.
# shellcheck disable=SC2086
$COMPOSE up -d $LAB_CORE
# shellcheck disable=SC2086
$COMPOSE up -d --wait --wait-timeout 90 $LAB_DBS
step icinga-db
./seed.py --icinga-db
# shellcheck disable=SC2086
$COMPOSE up -d $LAB_ICINGA_WEB
$COMPOSE build factum-web
step wait-apps
# shellcheck disable=SC2086
$COMPOSE up -d --wait --wait-timeout 300 $LAB_APPS $LAB_ICINGA_WEB
step seed
./seed.py "$@"
step factum
$COMPOSE up -d --wait --wait-timeout 120 factum-web factum-worker
step service-definitions
./service_definitions.py
step done
