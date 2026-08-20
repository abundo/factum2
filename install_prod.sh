#!/bin/bash
#
# Build and install all files on the production server
# Restarts services
#
# Usage: ./install_prod.sh [host]
#
#   host defaults to localhost. When given, the primary install (binaries +
#   factum2-gui.service + factum2-worker.service) is done on that host over
#   ssh instead of locally. Config and the worker_nodes lookup also run on
#   that host.
#
#   1. Read db.* from the primary's factum2.yaml and query worker_nodes for
#      every enabled remote node (models.WorkerNode - the "Worker nodes"
#      admin page).
#   2. make release, install the binaries + systemd units to /opt/factum2,
#      restart factum2-gui.service and factum2-worker.service.
#   3. rsync the same binaries + factum2-worker.service out to each enabled
#      remote node over ssh and restart factum-worker.service there.
#
# Assumes passwordless (key-based) ssh/scp access to the target host (when
# not localhost) and every remote node as $SSH_USER, and that the local user
# can sudo systemctl/cp into /etc/systemd/system when installing locally.

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    echo "Usage: $0 [host]" >&2
    exit 0
fi
if [[ $# -gt 1 ]]; then
    echo "Usage: $0 [host]" >&2
    exit 1
fi

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$REPO_DIR/build"
INSTALL_DIR="/opt/factum2"
CONFIG_PATH="/etc/factum2/factum2.yaml"
SSH_USER="${SSH_USER:-root}"
TARGET_HOST="${1:-localhost}"

is_local_target() {
    [[ "$TARGET_HOST" == "localhost" || "$TARGET_HOST" == "127.0.0.1" || "$TARGET_HOST" == "::1" ]]
}

tmp_config=
cleanup() {
    [[ -n "$tmp_config" && -f "$tmp_config" ]] && rm -f "$tmp_config"
}
trap cleanup EXIT

if is_local_target; then
    CONFIG_FILE="$CONFIG_PATH"
    if [[ ! -f "$CONFIG_FILE" ]]; then
        echo "Config file not found: $CONFIG_FILE" >&2
        exit 1
    fi
else
    if ! ssh -o BatchMode=yes "$SSH_USER@$TARGET_HOST" "test -f $CONFIG_PATH"; then
        echo "Config file not found on $TARGET_HOST: $CONFIG_PATH" >&2
        exit 1
    fi
    tmp_config="$(mktemp)"
    scp -q "$SSH_USER@$TARGET_HOST:$CONFIG_PATH" "$tmp_config"
    CONFIG_FILE="$tmp_config"
fi

echo "REPO_DIR    = $REPO_DIR"
echo "BUILD_DIR   = $BUILD_DIR"
echo "INSTALL_DIR = $INSTALL_DIR"
echo "CONFIG_FILE = $CONFIG_PATH"
echo "TARGET_HOST = $TARGET_HOST"
echo "SSH_USER    = $SSH_USER"

# Minimal scraper for this file's flat "section:\n  key: value" shape -
# good enough for the db: block, not a general YAML parser.
yaml_get() {
    local section="$1" key="$2"
    awk -v section="$section:" -v key="$key" '
        $0 == section { in_block=1; next }
        in_block && /^[^ #]/ { in_block=0 }
        in_block {
            line=$0
            sub(/^[ \t]+/, "", line)
            n=split(line, parts, /:[ \t]*/)
            if (parts[1] == key) { print parts[2]; exit }
        }
    ' "$CONFIG_FILE"
}

DB_HOST=$(yaml_get db host)
DB_PORT=$(yaml_get db port)
DB_USER=$(yaml_get db user)
DB_PASS=$(yaml_get db pass)
DB_NAME=$(yaml_get db database)
echo "$DB_HOST $DB_PORT $DB_USER $DB_NAME"

echo "----------------------------------------------------------------------"
echo " Looking up enabled remote worker nodes ($DB_NAME@$DB_HOST)"
echo "----------------------------------------------------------------------"
query_worker_nodes() {
    local sql="select address from worker_nodes where enabled = true;"
    if is_local_target; then
        docker compose -f /opt/postgresql/compose.yaml exec -T -e PGPASSWORD="$DB_PASS" db \
            psql -U "$DB_USER" -d "$DB_NAME" -tAc "$sql"
    else
        ssh "$SSH_USER@$TARGET_HOST" bash -s -- "$DB_USER" "$DB_NAME" "$DB_PASS" <<'EOF'
set -euo pipefail
docker compose -f /opt/postgresql/compose.yaml exec -T -e PGPASSWORD="$3" db \
    psql -U "$1" -d "$2" -tAc "select address from worker_nodes where enabled = true;"
EOF
    fi
}
mapfile -t REMOTE_NODES < <(query_worker_nodes)
echo "Enabled remote nodes: ${REMOTE_NODES[*]:-(none)}"

if is_local_target; then
    echo "----------------------------------------------------------------------"
    echo " Requesting sudo access (needed for the local install step below)"
    echo "----------------------------------------------------------------------"
    sudo -v
fi

echo "----------------------------------------------------------------------"
echo " Building release"
echo "----------------------------------------------------------------------"
cd "$REPO_DIR"
make release

echo "----------------------------------------------------------------------"
if is_local_target; then
    echo " Installing locally to $INSTALL_DIR"
    echo "----------------------------------------------------------------------"
    sudo mkdir -p "$INSTALL_DIR"
    sudo rsync -a -c "$BUILD_DIR"/ "$INSTALL_DIR/"
    sudo cp examples/factum2-gui.service /etc/systemd/system
    sudo cp examples/factum2-worker.service /etc/systemd/system
    sudo systemctl daemon-reload
    sudo systemctl restart factum2-gui.service
    sudo systemctl restart factum2-worker.service
else
    echo " Installing on $TARGET_HOST to $INSTALL_DIR"
    echo "----------------------------------------------------------------------"
    echo "-> copying binaries"
    ssh "$SSH_USER@$TARGET_HOST" "mkdir -p $INSTALL_DIR"
    rsync -a -c "$BUILD_DIR"/ "$SSH_USER@$TARGET_HOST:$INSTALL_DIR/"

    echo "-> copying systemd units"
    scp examples/factum2-gui.service "$SSH_USER@$TARGET_HOST:/etc/systemd/system/factum2-gui.service"
    scp examples/factum2-worker.service "$SSH_USER@$TARGET_HOST:/etc/systemd/system/factum2-worker.service"

    echo "-> reloading systemd and restarting factum2-gui.service factum2-worker.service"
    ssh "$SSH_USER@$TARGET_HOST" 'systemctl daemon-reload && systemctl restart factum2-gui.service && systemctl restart factum2-worker.service'
fi

for node in "${REMOTE_NODES[@]:-}"; do
    [[ -z "$node" ]] && continue
    host="${node%%:*}" # Address is host:port (worker's listen port) - strip it, ssh uses port 22
    [[ "$host" == "localhost" || "$host" == "127.0.0.1" || "$host" == "::1" ]] && continue
    [[ "$host" == "$TARGET_HOST" ]] && continue

    echo "----------------------------------------------------------------------"
    echo " Updating remote node $host"
    echo "----------------------------------------------------------------------"

    echo "-> copying binaries"
    rsync -a -c "$BUILD_DIR"/ "$SSH_USER@$host:$INSTALL_DIR/"

    echo "-> copying factum2-worker.service"
    scp examples/factum2-worker.service "$SSH_USER@$host:/etc/systemd/system/factum2-worker.service"

    if [[ "$host" == *icinga* ]]; then
        echo "-> copying icinga-notification-email.tpl"
        scp examples/icinga-notification-email.tpl "$SSH_USER@$host:/etc/factum2/icinga-notification-email-example.tpl"
    fi

    echo "-> reloading systemd and restarting factum-worker.service"
    ssh "$SSH_USER@$host" 'systemctl daemon-reload && systemctl restart factum2-worker.service'
done

echo "----------------------------------------------------------------------"
echo " Done"
echo "----------------------------------------------------------------------"
