#!/usr/bin/env bash
# Provisions an isolated factum2-web instance for an agent to drive:
# a throwaway Postgres DB (separate from the real "factum2" DB), a fresh
# admin user, a built frontend, and a running backend on a non-default
# port. Safe to re-run - every step is idempotent.
#
# Run from the repo root: .grok/skills/run-factum2-web/setup.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
STATE_DIR="$SCRIPT_DIR/.state"
mkdir -p "$STATE_DIR"

PG_CONTAINER="${FACTUM_PG_CONTAINER:-postgresql-db-1}"
PG_SUPERUSER="${FACTUM_PG_SUPERUSER:-factum2_user}"
DB_NAME="factum2_skilltest"
DB_USER="factum2_skilltest_user"
DB_PASS="skilltest_pass"
BIND="127.0.0.1:18090"
ADMIN_USER="admin@local"
ADMIN_PASS="skilltest123"

echo "==> Checking Postgres container ($PG_CONTAINER)"
if ! docker ps --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
  echo "Postgres container '$PG_CONTAINER' is not running. This project's DB" >&2
  echo "runs in docker (see /opt/postgres per project convention) - start it first." >&2
  exit 1
fi

echo "==> Ensuring isolated DB/role ($DB_NAME), separate from the real 'factum2' DB"
docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -d postgres -v ON_ERROR_STOP=0 -c \
  "CREATE ROLE ${DB_USER} WITH LOGIN PASSWORD '${DB_PASS}';" >/dev/null 2>&1 || true
docker exec "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -d postgres -v ON_ERROR_STOP=0 -c \
  "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" >/dev/null 2>&1 || true

CONFIG_FILE="$STATE_DIR/config.yaml"
cat > "$CONFIG_FILE" <<YAML
db:
  host: localhost
  port: "5432"
  user: ${DB_USER}
  pass: ${DB_PASS}
  database: ${DB_NAME}
factum:
  url: ""
  token: ""
web:
  bind: "${BIND}"
  jwtsecret: "skilltest-insecure-secret-do-not-use-in-prod"
worker:
  commands: {}
YAML
echo "==> Wrote $CONFIG_FILE"

echo "==> Building frontend (web/static/vue is gitignored - safe to rebuild)"
( cd "$REPO_ROOT/web/frontend" && npm run build >/dev/null )

echo "==> Applying database schema migrations"
( cd "$REPO_ROOT" && go run ./cmd/web migrate -f "$CONFIG_FILE" )

echo "==> Creating/updating admin user ($ADMIN_USER) - needs a real TTY, so this runs under tmux"
tmux kill-session -t factum2-skill-admin 2>/dev/null || true
tmux new-session -d -s factum2-skill-admin -x 200 -y 50
tmux send-keys -t factum2-skill-admin \
  "cd '$REPO_ROOT' && go run ./cmd/web createadmin -f '$CONFIG_FILE'" Enter
sleep 4
tmux send-keys -t factum2-skill-admin "$ADMIN_PASS" Enter
sleep 1
tmux send-keys -t factum2-skill-admin "$ADMIN_PASS" Enter
sleep 2
tmux capture-pane -t factum2-skill-admin -p | tail -5
tmux kill-session -t factum2-skill-admin 2>/dev/null || true

BIND_PORT="${BIND##*:}"
EXISTING_PIDS="$(lsof -ti:"$BIND_PORT" -sTCP:LISTEN 2>/dev/null || true)"
if [ -n "$EXISTING_PIDS" ]; then
  echo "==> Killing existing process(es) on port $BIND_PORT before restart: $EXISTING_PIDS"
  echo "$EXISTING_PIDS" | xargs -r kill
  sleep 1
fi

echo "==> Starting backend on $BIND"
# -b overrides web.bind and defaults to ":8090" if omitted - always pass it
# explicitly or this collides with the real instance's port. "go run" execs
# a separate compiled binary as a child, so killing by port (not by this
# nohup pid) is what teardown.sh does too - see its comment.
#
# The "exec" here matters: without it, "( cd ... && nohup ... & )" is a
# background job started *inside* a subshell, and that subshell can end up
# blocking in wait() for its own background job before it exits - which
# then blocks this script (hung ~19 minutes on do_wait before this fix,
# even though the server itself came up fine within seconds). "exec"
# replaces the subshell process with nohup/go instead of forking yet
# another job inside it, and "disown" drops it from this script's own job
# table so there is nothing left for anything to wait on.
( cd "$REPO_ROOT" && exec nohup go run ./cmd/web start -f "$CONFIG_FILE" -b "$BIND" \
    > "$STATE_DIR/backend.log" 2>&1 ) &
echo $! > "$STATE_DIR/backend.pid"
disown

echo -n "==> Waiting for http://$BIND/ "
for _ in $(seq 1 30); do
  if curl -sf "http://$BIND/" >/dev/null 2>&1; then
    echo "UP"
    break
  fi
  echo -n "."
  sleep 1
done
curl -sf "http://$BIND/" >/dev/null || { echo "FAILED"; tail -40 "$STATE_DIR/backend.log"; exit 1; }

echo "==> Seeding a demo job if none exist (dns: many events incl. warning/error, icinga: one event)"
docker exec -i "$PG_CONTAINER" psql -U "$PG_SUPERUSER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -q <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM jobs) THEN
    INSERT INTO jobs (created_at, updated_at, type, triggered_by, started_at, finished_at, expected_tasks)
    VALUES (now(), now(), 'sync', 'admin@local', now() - interval '2 minutes', now(), 2);

    INSERT INTO job_tasks (created_at, updated_at, job_id, task_id, target, started_at, finished_at, exit_code, err, error_count, warning_count)
    VALUES
      (now(), now(), (SELECT max(id) FROM jobs), 'task-dns', 'dns', now() - interval '2 minutes', now() - interval '1 minute', 0, '', 1, 3),
      (now(), now(), (SELECT max(id) FROM jobs), 'task-icinga', 'icinga', now() - interval '1 minute', now(), 0, '', 0, 0);

    INSERT INTO job_task_events (created_at, updated_at, job_task_id, task_id, target, level, message, at)
    SELECT now(), now(), (SELECT id FROM job_tasks WHERE task_id = 'task-dns'), 'task-dns', 'dns',
           (ARRAY['info','info','warning','info','error','info','info','info','info','info'])[i],
           'DNS sync event number ' || i, now() - (interval '1 second' * i)
    FROM generate_series(1, 40) AS i;

    INSERT INTO job_task_events (created_at, updated_at, job_task_id, task_id, target, level, message, at)
    VALUES (now(), now(), (SELECT id FROM job_tasks WHERE task_id = 'task-icinga'), 'task-icinga', 'icinga', 'info', 'Icinga sync completed, nothing to do', now());
  END IF;
END $$;
SQL

cat <<EOF

==> Ready.
    URL:      http://$BIND/
    Login:    $ADMIN_USER / $ADMIN_PASS
    Backend log: $STATE_DIR/backend.log
    Backend pid: $(cat "$STATE_DIR/backend.pid")

Drive it with the browser REPL:
    node .grok/skills/run-factum2-web/browser.mjs

Tear down with:
    .grok/skills/run-factum2-web/teardown.sh
EOF
