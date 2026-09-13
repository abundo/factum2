#!/bin/sh
# Sidecar Chromium on the lab network. factum-web is capped at 128MiB, so
# the browser cannot live in that container.
set -eu
# shellcheck disable=SC1091
. "$(CDPATH= cd -- "$(dirname "$0")" && pwd)/lib.sh"

"$SKILL_DIR/scripts/ensure.sh"

net=$(lab_network)
if [ -z "$net" ]; then
	echo "could not find compose network for $PROJECT" >&2
	exit 1
fi

create_browser() {
	echo "==> creating $BROWSER_NAME on $net"
	"$ENGINE" run -d \
		--name "$BROWSER_NAME" \
		--label factum.dev.browser=1 \
		--network "$net" \
		--memory 1g \
		--shm-size 1g \
		--init \
		-v "$SKILL_DIR:/opt/factum-browser:z" \
		"$BROWSER_IMAGE" \
		sleep infinity >/dev/null
}

need_recreate=0
if "$ENGINE" inspect "$BROWSER_NAME" >/dev/null 2>&1; then
	status=$("$ENGINE" inspect -f '{{.State.Status}}' "$BROWSER_NAME")
	if [ "$status" != running ]; then
		echo "==> starting stopped $BROWSER_NAME"
		"$ENGINE" start "$BROWSER_NAME" >/dev/null
	else
		echo "==> reusing running $BROWSER_NAME"
	fi
	if ! "$ENGINE" inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$BROWSER_NAME" | grep -qw "$net"; then
		echo "==> connecting $BROWSER_NAME to $net"
		if ! "$ENGINE" network connect "$net" "$BROWSER_NAME"; then
			need_recreate=1
		fi
	fi
	# playwright-core >= 1.50 needs Node 20; debian bookworm's nodejs is 18.
	if ! "$ENGINE" exec "$BROWSER_NAME" node -e 'process.exit(Number(process.versions.node.split(".")[0]) >= 20 ? 0 : 1)' 2>/dev/null; then
		echo "==> $BROWSER_NAME has Node < 20; recreating from $BROWSER_IMAGE"
		need_recreate=1
	fi
else
	need_recreate=1
fi
if [ "$need_recreate" -eq 1 ]; then
	"$ENGINE" rm -f "$BROWSER_NAME" >/dev/null 2>&1 || true
	create_browser
fi

echo "==> ensuring Chromium + playwright-core in $BROWSER_NAME"
"$ENGINE" exec "$BROWSER_NAME" sh -c '
set -eu
if ! command -v chromium >/dev/null 2>&1; then
	echo "==> installing chromium (first run)"
	apt-get update
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		ca-certificates chromium chromium-sandbox fonts-liberation
	rm -rf /var/lib/apt/lists/*
fi
if [ ! -d /opt/factum-browser/node_modules/playwright-core ]; then
	echo "==> npm install playwright-core"
	cd /opt/factum-browser
	npm install --omit=dev
fi
command -v chromium
command -v node
'

echo "==> browser ready in $BROWSER_NAME"
echo "    drive with $SKILL_DIR/scripts/run-browser.sh"
echo "    in-network Factum GUI: http://factum-web:8091"
