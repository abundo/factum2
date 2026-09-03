#!/usr/bin/env bash
# Idempotent lab bootstrap: wait for compose services, overlay icinga API
# config, migrate factum, create admin, seed Settings, LibreNMS token.
set -euo pipefail

DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$DIR/.." && pwd)
COMPOSE=("$DIR/compose.sh")
# shellcheck disable=SC1091
set -a
# Values from compose .env; seed uses the same file.
# shellcheck source=/dev/null
. "$DIR/.env"
set +a

FACTUM_YAML="$DIR/factum2.yaml"
ADMIN_USER="${FACTUM_ADMIN_USER:-admin}"
ADMIN_PASS="${FACTUM_ADMIN_PASSWORD:-admin}"
LIBRENMS_TOKEN="0123456789abcdef0123456789abcdef"
NETBOX_TOKEN="${NETBOX_SUPERUSER_API_TOKEN:-0123456789abcdef0123456789abcdef01234567}"

log() { printf '==> %s\n' "$*"; }

wait_http() {
	local url=$1
	local tries=${2:-60}
	local i code
	for i in $(seq 1 "$tries"); do
		code=$(curl -sS -k -o /dev/null --connect-timeout 2 -w '%{http_code}' "$url" || true)
		# 401 means the listener is up (Icinga API without credentials).
		case $code in
		200|204|301|302|401|403) return 0 ;;
		esac
		sleep 3
	done
	echo "timed out waiting for $url (last HTTP $code)" >&2
	return 1
}

log "Preparing dest files"
"$DIR/prepare.sh"
# Oxidized runs as uid 30000 and may chown router.db; make dest files
# writable by the host user and the container.
"${COMPOSE[@]}" exec -T -u root oxidized chmod -R a+rwX /home/oxidized/.config/oxidized 2>/dev/null || true

log "Waiting for NetBox"
wait_http "http://127.0.0.1:18000/login/" 80

log "Waiting for LibreNMS"
wait_http "http://127.0.0.1:18001/login" 80

log "Waiting for Icinga API"
wait_http "https://127.0.0.1:15665/v1/status" 40 || true

log "Installing Icinga API user and factum includes"
for i in $(seq 1 40); do
	if "${COMPOSE[@]}" exec -T icinga test -d /data/etc/icinga2/conf.d; then
		break
	fi
	sleep 2
done
"${COMPOSE[@]}" exec -T -u root icinga tee /data/etc/icinga2/conf.d/api-users.conf >/dev/null <"$DIR/icinga/api-users.conf"
"${COMPOSE[@]}" exec -T -u root icinga tee /data/etc/icinga2/conf.d/factum.conf >/dev/null <"$DIR/icinga/factum.conf"
"${COMPOSE[@]}" exec -T icinga icinga2 daemon --reload >/dev/null 2>&1 || \
	"${COMPOSE[@]}" exec -T -u root icinga sh -c 'kill -HUP $(pidof icinga2) 2>/dev/null || true'

log "NetBox API token"
token=$(
	"${COMPOSE[@]}" exec -T netbox /opt/netbox/venv/bin/python /opt/netbox/netbox/manage.py shell --interface python <<'PY' || true
from users.models import Token, User
u = User.objects.filter(username="admin").first()
if not u:
    print("")
else:
    t = Token.objects.filter(user=u).first()
    if not t:
        print("")
    else:
        key = getattr(t, "key", None)
        print(key if key else str(t))
PY
)
token=$(printf '%s' "$token" | tr -d '\r' | tail -n 1)
if [ -n "$token" ]; then
	NETBOX_TOKEN=$token
fi
log "NetBox token: ${NETBOX_TOKEN:0:8}…"

log "LibreNMS admin user + API token"
"${COMPOSE[@]}" exec -T librenms lnms user:add admin --password=admin --role=admin --email=admin@lab.example >/dev/null 2>&1 || \
	"${COMPOSE[@]}" exec -T librenms php /opt/librenms/lnms user:add admin --password=admin --role=admin --email=admin@lab.example >/dev/null 2>&1 || \
	true
"${COMPOSE[@]}" exec -T mysql mariadb -u root -p"${MYSQL_ROOT_PASSWORD}" -N -e \
	"INSERT INTO librenms.api_tokens (user_id, token_hash, description, disabled)
	 SELECT user_id, '${LIBRENMS_TOKEN}', 'factum lab', 0 FROM librenms.users WHERE username='admin'
	 AND NOT EXISTS (SELECT 1 FROM librenms.api_tokens WHERE token_hash='${LIBRENMS_TOKEN}');" \
	>/dev/null 2>&1 || \
"${COMPOSE[@]}" exec -T mysql mysql -u root -p"${MYSQL_ROOT_PASSWORD}" -N -e \
	"INSERT INTO librenms.api_tokens (user_id, token_hash, description, disabled)
	 SELECT user_id, '${LIBRENMS_TOKEN}', 'factum lab', 0 FROM librenms.users WHERE username='admin'
	 AND NOT EXISTS (SELECT 1 FROM librenms.api_tokens WHERE token_hash='${LIBRENMS_TOKEN}');" \
	>/dev/null 2>&1 || true

cat >"$DIR/container/librenms.env" <<EOF
MYSQL_DATABASE=${MYSQL_DATABASE:-librenms}
MYSQL_USER=${MYSQL_USER:-librenms}
MYSQL_PASSWORD=${MYSQL_PASSWORD:-librenms}
MYSQL_HOST=mysql
MYSQL_PORT=3306
EOF

log "Migrating factum schema"
if [ -x "$REPO_ROOT/build/factum2-web" ]; then
	"$REPO_ROOT/build/factum2-web" migrate -f "$FACTUM_YAML"
else
	( cd "$REPO_ROOT" && go run ./cmd/web migrate -f "$FACTUM_YAML" )
fi

log "Creating admin user ($ADMIN_USER)"
if command -v tmux >/dev/null 2>&1; then
	tmux kill-session -t factum2-dev-admin 2>/dev/null || true
	tmux new-session -d -s factum2-dev-admin -x 200 -y 50
	tmux send-keys -t factum2-dev-admin \
		"cd '$REPO_ROOT' && go run ./cmd/web createadmin -f '$FACTUM_YAML'" Enter
	sleep 4
	tmux send-keys -t factum2-dev-admin "$ADMIN_PASS" Enter
	sleep 1
	tmux send-keys -t factum2-dev-admin "$ADMIN_PASS" Enter
	sleep 2
	tmux kill-session -t factum2-dev-admin 2>/dev/null || true
else
	log "tmux not found; run: go run ./cmd/web createadmin -f $FACTUM_YAML"
fi

log "Seeding Settings"
SQL_FILE=$(mktemp)
python3 - "$SQL_FILE" "$NETBOX_TOKEN" "$LIBRENMS_TOKEN" "${FACTUM_API_TOKEN}" \
	"/data/dns/records" "/data/icinga/hosts.conf" "/data/icinga/users.conf" \
	"/data/oxidized/router.db" "$DIR/templates/icinga-host.tmpl" "$DIR/templates/icinga-user.tmpl" <<'PY'
import sys
from pathlib import Path

out, netbox_token, librenms_token, factum_token, dns_file, icinga_hosts, icinga_users, ox_file, host_tmpl_path, user_tmpl_path = sys.argv[1:]

def lit(s: str) -> str:
    return "'" + s.replace("'", "''") + "'"

host_tmpl = Path(host_tmpl_path).read_text()
user_tmpl = Path(user_tmpl_path).read_text()
sql = f"""
INSERT INTO settings (id, created_at, updated_at)
SELECT 1, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE id = 1);

UPDATE settings SET
  default_domain = 'lab.example',
  factum_api_token = {lit(factum_token)},
  netbox_enabled = true,
  netbox_api_url = 'http://netbox:8080',
  netbox_api_token = {lit(netbox_token)},
  dns_enabled = true,
  dns_dest_file = {lit(dns_file)},
  icinga_enabled = true,
  icinga_api_url = 'https://icinga:5665',
  icinga_api_user = 'factum',
  icinga_api_pass = 'factum',
  icinga_hosts_file = {lit(icinga_hosts)},
  icinga_users_file = {lit(icinga_users)},
  icinga_host_template = {lit(host_tmpl)},
  icinga_user_template = {lit(user_tmpl)},
  librenms_enabled = true,
  librenms_api_url = 'http://librenms:8000',
  librenms_api_token = {lit(librenms_token)},
  librenms_snmp_version = 'v2c',
  librenms_snmp_communities = 'public',
  oxidized_enabled = true,
  oxidized_api_url = 'http://oxidized:8888',
  oxidized_dest_file = {lit(ox_file)}
WHERE id = 1;
"""
Path(out).write_text(sql)
PY
# docker exec -i is required when piping SQL (AGENTS.md).
"${COMPOSE[@]}" exec -T postgres psql -U "${POSTGRES_USER:-factum2}" -d "${POSTGRES_DB:-factum2}" \
	-v ON_ERROR_STOP=1 <"$SQL_FILE"
rm -f "$SQL_FILE"

log "Registering lab worker node"
"${COMPOSE[@]}" exec -T postgres psql -U "${POSTGRES_USER:-factum2}" -d "${POSTGRES_DB:-factum2}" \
	-v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO worker_nodes (name, address, token, enabled, tls_skip_verify, tls_ca, created_at, updated_at)
SELECT 'lab', 'factum-worker:8443', 'lab-worker-token', true, true, '', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM worker_nodes WHERE name = 'lab');
UPDATE worker_nodes SET
  address = 'factum-worker:8443',
  token = 'lab-worker-token',
  enabled = true,
  tls_skip_verify = true
WHERE name = 'lab';
SQL

cat >"$DIR/env.sh" <<EOF
# shellcheck disable=SC2148
# Source from the repo root:  . dev/env.sh
export LIBRENMS_ENV_FILE="$DIR/data/librenms.env"
export PATH="$DIR/bin:\$PATH"
export FACTUM_DEV_CONFIG="$FACTUM_YAML"
EOF

log "Lab is ready"
cat <<EOF

  Factum config:  $FACTUM_YAML
  Source env:     . dev/env.sh

  Factum GUI:     http://127.0.0.1:8091   admin / admin
  Rebuild:        ./install.py --source --compose
  NetBox:         http://127.0.0.1:18000  admin / admin
  LibreNMS:       http://127.0.0.1:18001  admin / admin
  Icinga API:     https://127.0.0.1:15665  factum / factum
  Oxidized:       http://127.0.0.1:18888
  BIND:           127.0.0.1:18053          zone lab.example
  Postgres:       127.0.0.1:15432          factum2 / factum2  (DBs: factum2, netbox)
  MariaDB:        127.0.0.1:13306          librenms / librenms

  Login: $ADMIN_USER / $ADMIN_PASS

EOF
