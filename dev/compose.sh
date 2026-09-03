#!/bin/sh
# Run docker compose or podman compose against this directory's compose.yml.
set -eu
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
cd "$DIR"

if [ -n "${FACTUM_COMPOSE:-}" ]; then
	# shellcheck disable=SC2086
	exec $FACTUM_COMPOSE -f compose.yml "$@"
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
	exec docker compose -f compose.yml "$@"
fi

if command -v podman >/dev/null 2>&1 && podman compose version >/dev/null 2>&1; then
	exec podman compose -f compose.yml "$@"
fi

if command -v podman-compose >/dev/null 2>&1; then
	exec podman-compose -f compose.yml "$@"
fi

echo "Need docker compose or podman compose (or set FACTUM_COMPOSE)." >&2
exit 1
