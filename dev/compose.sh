#!/bin/sh
# Run docker compose or podman compose against this directory's compose.yml.
set -eu
DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
cd "$DIR"

if [ -n "${FACTUM_COMPOSE:-}" ]; then
	# shellcheck disable=SC2086
	exec $FACTUM_COMPOSE -f compose.yml "$@"
fi

# `docker compose version` succeeds when the CLI is installed but the daemon
# is stopped or this user cannot reach its socket. Only prefer Docker when
# it actually answers; otherwise Podman (the laptop lab) is the engine.
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
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
