# Shared by ensure.sh / ensure-browser.sh / run-browser.sh.
# shellcheck shell=sh

SKILL_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SKILL_DIR/../../.." && pwd)
DEV_DIR="$REPO_ROOT/dev"
COMPOSE="$DEV_DIR/compose.sh"
BROWSER_NAME="${FACTUM_BROWSER_CONTAINER:-factum-dev-browser}"
BROWSER_IMAGE="${FACTUM_BROWSER_IMAGE:-docker.io/library/node:20-bookworm-slim}"
PROJECT=factum-dev

engine() {
	if [ -n "${FACTUM_ENGINE:-}" ]; then
		echo "$FACTUM_ENGINE"
		return
	fi
	if [ -n "${FACTUM_COMPOSE:-}" ]; then
		echo "$FACTUM_COMPOSE" | awk '{ print $1 }'
		return
	fi
	if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
		echo docker
		return
	fi
	if command -v podman >/dev/null 2>&1; then
		echo podman
		return
	fi
	echo "Need docker compose or podman compose (or set FACTUM_COMPOSE)." >&2
	exit 1
}

ENGINE=$(engine)

# True if compose ps text contains a replica of service $1.
# Docker Compose v2: {project}-{service}-{n}; podman-compose: {project}_{service}_{n}.
service_in_ps() {
	svc=$1
	ps_text=$2
	echo "$ps_text" | grep -Eq "(^|[[:space:]/])([^[:space:]/]*[_-])?${svc}[_-][0-9]+([[:space:]]|$)"
}

compose_ps() {
	"$COMPOSE" ps "$@" 2>/dev/null || true
}

service_running() {
	service_in_ps "$1" "$(compose_ps)"
}

# Container names for this compose project (any state), excluding the browser sidecar.
lab_container_names() {
	{
		"$ENGINE" ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Names}}' 2>/dev/null || true
		"$ENGINE" ps -a --filter "label=io.podman.compose.project=$PROJECT" --format '{{.Names}}' 2>/dev/null || true
		"$ENGINE" ps -a --filter "name=$PROJECT" --format '{{.Names}}' 2>/dev/null || true
	} | grep -v "^${BROWSER_NAME}$" | grep -v '^$' | sort -u
}

web_container() {
	lab_container_names | grep -E '[_-]factum-web[_-]' | head -n 1
}

lab_network() {
	cid=$(web_container)
	if [ -z "$cid" ]; then
		echo "${PROJECT}_default"
		return
	fi
	nets=$("$ENGINE" inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$cid" 2>/dev/null || true)
	echo "$nets" | tr ' ' '\n' | grep -F "${PROJECT}" | head -n 1
}

wait_url() {
	url=$1
	tries=${2:-90}
	i=0
	while [ "$i" -lt "$tries" ]; do
		if command -v curl >/dev/null 2>&1; then
			curl -fsS -o /dev/null --max-time 3 "$url" 2>/dev/null && return 0
		elif command -v wget >/dev/null 2>&1; then
			wget -q -O /dev/null --timeout=3 "$url" 2>/dev/null && return 0
		fi
		i=$((i + 1))
		sleep 2
	done
	return 1
}
