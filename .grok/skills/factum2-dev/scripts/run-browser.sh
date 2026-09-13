#!/bin/sh
# Drive lab UIs from the Chromium sidecar. Commands on stdin, one per line
# (same REPL as browser.mjs). Optional first arg: app alias or absolute URL.
set -eu
# shellcheck disable=SC1091
. "$(CDPATH= cd -- "$(dirname "$0")" && pwd)/lib.sh"

"$SKILL_DIR/scripts/ensure-browser.sh"

target=${1:-factum}
case $target in
http://*|https://*) BASE_URL=$target ;;
factum|gui|web) BASE_URL=http://factum-web:8091 ;;
netbox) BASE_URL=http://netbox:8080 ;;
librenms) BASE_URL=http://librenms:8000 ;;
icinga|icingaweb) BASE_URL=http://icingaweb:8080 ;;
grafana) BASE_URL=http://grafana:3000 ;;
oxidized) BASE_URL=http://oxidized:8888 ;;
prometheus) BASE_URL=http://prometheus:9090 ;;
portal|index) BASE_URL=http://portal ;;
*)
	echo "unknown target '$target' (use factum|netbox|librenms|icinga|grafana|oxidized|prometheus|portal or a URL)" >&2
	exit 1
	;;
esac
shift $(( $# > 0 ? 1 : 0 )) || true

ADMIN_USER=${ADMIN_USER:-${FACTUM_ADMIN_USER:-admin}}
ADMIN_PASS=${ADMIN_PASS:-${FACTUM_ADMIN_PASSWORD:-admin}}
CHROME_PATH=${CHROME_PATH:-/usr/bin/chromium}

echo "==> browser $BROWSER_NAME  BASE_URL=$BASE_URL"
exec "$ENGINE" exec -i \
	-e "BASE_URL=$BASE_URL" \
	-e "ADMIN_USER=$ADMIN_USER" \
	-e "ADMIN_PASS=$ADMIN_PASS" \
	-e "CHROME_PATH=$CHROME_PATH" \
	"$BROWSER_NAME" \
	node /opt/factum-browser/browser.mjs
