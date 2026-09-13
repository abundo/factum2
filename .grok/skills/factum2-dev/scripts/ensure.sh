#!/bin/sh
# Reuse a running factum-dev lab, start stopped containers, or create it.
set -eu
# shellcheck disable=SC1091
. "$(CDPATH= cd -- "$(dirname "$0")" && pwd)/lib.sh"

cd "$REPO_ROOT"

if [ ! -x "$COMPOSE" ]; then
	echo "missing $COMPOSE" >&2
	exit 1
fi

if service_running factum-web; then
	echo "==> reusing running factum-dev (factum-web is up)"
	echo "    index  http://127.0.0.1:18080"
	echo "    GUI    http://127.0.0.1:18091"
	echo "    logins $DEV_DIR/README.md"
	exit 0
fi

names=$(lab_container_names || true)
if [ -n "$names" ]; then
	echo "==> starting existing factum-dev containers"
	# up -d restarts stopped replicas and creates any missing services.
	# Volumes already hold seed data; do not make dev-up (that re-runs seed).
	"$COMPOSE" up -d
else
	echo "==> no factum-dev containers; creating lab (make dev-up)"
	make -C "$REPO_ROOT" dev-up
fi

echo "==> waiting for factum-web http://127.0.0.1:18091/api/version"
if ! wait_url "http://127.0.0.1:18091/api/version" 90; then
	echo "factum-web did not become ready. See: $COMPOSE ps" >&2
	exit 1
fi

echo "==> factum-dev ready"
echo "    index  http://127.0.0.1:18080"
echo "    GUI    http://127.0.0.1:18091"
echo "    logins $DEV_DIR/README.md"
