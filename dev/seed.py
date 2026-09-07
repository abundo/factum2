#!/usr/bin/env python3
"""Idempotent lab bootstrap: wait for compose services, overlay icinga API
config and Icinga DB, optionally load NetBox demo data (--demo), migrate
factum, create admin, seed Settings, LibreNMS token, NetBox webhook and
custom fields. If netbox-seed.yaml exists, load that inventory via the API.
"""

from __future__ import annotations

import argparse
import os
import shutil
import time
from pathlib import Path

from lab import DIR, REPO_ROOT, compose, env, load_env, log, run, wait_http
from netbox_seed import DEFAULT_URL, DEFAULT_YAML, seed_inventory
from prepare import prepare

FACTUM_YAML = DIR / "factum2.yaml"
LIBRENMS_TOKEN = "0123456789abcdef0123456789abcdef"

NETBOX_TOKEN_PY = """\
import os
from users.choices import TokenVersionChoices
from users.models import Token, User
key = os.environ.get("FACTUM_LAB_NETBOX_TOKEN", "")
u = User.objects.filter(username="admin").first()
if not u or not key:
    raise SystemExit("netbox admin user or lab token missing")
if not Token.objects.filter(plaintext=key).exists():
    Token.objects.create(
        user=u,
        token=key,
        version=TokenVersionChoices.V1,
        write_enabled=True,
        description="factum lab",
    )
print(key)
"""

WORKER_SQL = """\
INSERT INTO worker_nodes (name, address, token, enabled, tls_skip_verify, tls_ca, created_at, updated_at)
SELECT 'lab', 'factum-worker:8443', 'lab-worker-token', true, true, '', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM worker_nodes WHERE name = 'lab');
UPDATE worker_nodes SET
  address = 'factum-worker:8443',
  token = 'lab-worker-token',
  enabled = true,
  tls_skip_verify = true
WHERE name = 'lab';
"""

ICINGA_DB_SQL = """\
CREATE DATABASE IF NOT EXISTS icingadb;
CREATE DATABASE IF NOT EXISTS icingaweb;
CREATE USER IF NOT EXISTS 'icingadb'@'%' IDENTIFIED BY 'icingadb';
CREATE USER IF NOT EXISTS 'icingaweb'@'%' IDENTIFIED BY 'icingaweb';
GRANT ALL PRIVILEGES ON icingadb.* TO 'icingadb'@'%';
GRANT ALL PRIVILEGES ON icingaweb.* TO 'icingaweb'@'%';
FLUSH PRIVILEGES;
"""

NETBOX_RECREATE_SQL = """\
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname = 'netbox' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS netbox WITH (FORCE);
CREATE DATABASE netbox OWNER netbox;
"""

READY = """
  Factum config:  {factum_yaml}
  Source env:     . dev/env.sh

  Lab index:      http://127.0.0.1:18080
  Factum GUI:     http://127.0.0.1:18091   admin / admin
  Rebuild:        ./install.py --source --compose
  NetBox:         http://127.0.0.1:18000  admin / admin
  LibreNMS:       http://127.0.0.1:18001  admin / admin
  Icinga Web:     http://127.0.0.1:18002  admin / admin
  Icinga API:     https://127.0.0.1:15665  factum / factum
  Oxidized:       http://127.0.0.1:18888
  BIND:           127.0.0.1:18053          zone lab.example
  Postgres:       127.0.0.1:15432          factum2 / factum2  (DBs: factum2, netbox)
  MariaDB:        127.0.0.1:13306          librenms / librenms

  Login: {admin_user} / {admin_pass}
"""


def _sql_lit(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def _psql(database: str, sql: str, *, user: str, quiet: bool = False) -> None:
    run(
        compose(
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            user,
            "-d",
            database,
            "-v",
            "ON_ERROR_STOP=1",
        ),
        input=sql,
        quiet=quiet,
    )


def _tee_icinga(dest: str, src: Path) -> None:
    run(
        compose("exec", "-T", "-u", "root", "icinga", "tee", dest),
        input=src.read_bytes(),
        quiet=True,
    )


def _netbox_demo(*, pg_user: str) -> None:
    log("NetBox demo data")
    dump = DIR / "data" / "netbox" / "netbox-demo.sql"
    if not dump.is_file() or dump.stat().st_size == 0:
        run([str(DIR / "netbox-demo-fetch.sh")])
    count_run = run(
        compose(
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            pg_user,
            "-d",
            "netbox",
            "-tAc",
            "SELECT COUNT(*) FROM dcim_device;",
        ),
        check=False,
        capture_output=True,
        text=True,
    )
    raw = (count_run.stdout or "0").strip() if count_run.returncode == 0 else "0"
    try:
        device_count = int(raw or "0")
    except ValueError:
        device_count = 0
    if device_count > 0:
        log(f"NetBox already has {device_count} devices; skipping demo dump")
        return
    log("Loading NetBox demo dump into empty database (about a minute)")
    run(compose("stop", "netbox", "netbox-worker"))
    _psql("postgres", NETBOX_RECREATE_SQL, user=pg_user)
    run(
        compose(
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            pg_user,
            "-d",
            "netbox",
            "-q",
            "-v",
            "ON_ERROR_STOP=1",
        ),
        input=dump.read_bytes(),
        quiet=True,
    )
    run(compose("up", "-d", "--wait", "--wait-timeout", "300", "netbox", "netbox-worker"))
    wait_http("http://127.0.0.1:18000/login/", 80)


def _create_admin(admin_user: str, admin_pass: str) -> None:
    log(f"Creating admin user ({admin_user})")
    if not shutil.which("tmux"):
        log(f"tmux not found; run: go run ./cmd/web createadmin -f {FACTUM_YAML}")
        return
    run(["tmux", "kill-session", "-t", "factum2-dev-admin"], check=False, quiet=True)
    run(["tmux", "new-session", "-d", "-s", "factum2-dev-admin", "-x", "200", "-y", "50"])
    run(
        [
            "tmux",
            "send-keys",
            "-t",
            "factum2-dev-admin",
            f"cd '{REPO_ROOT}' && go run ./cmd/web createadmin -f '{FACTUM_YAML}'",
            "Enter",
        ]
    )
    time.sleep(4)
    run(["tmux", "send-keys", "-t", "factum2-dev-admin", admin_pass, "Enter"])
    time.sleep(1)
    run(["tmux", "send-keys", "-t", "factum2-dev-admin", admin_pass, "Enter"])
    time.sleep(2)
    run(["tmux", "kill-session", "-t", "factum2-dev-admin"], check=False, quiet=True)


def _settings_sql(
    *,
    netbox_token: str,
    librenms_token: str,
    factum_token: str,
    webhook_secret: str,
    public_base: str,
) -> str:
    host_tmpl = (DIR / "templates" / "icinga-host.tmpl").read_text()
    user_tmpl = (DIR / "templates" / "icinga-user.tmpl").read_text()
    return f"""
INSERT INTO settings (id, created_at, updated_at)
SELECT 1, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE id = 1);

UPDATE settings SET
  default_domain = 'lab.example',
  factum_api_token = {_sql_lit(factum_token)},
  netbox_enabled = true,
  netbox_api_url = 'http://netbox:8080',
  netbox_api_token = {_sql_lit(netbox_token)},
  netbox_webhook_secret = {_sql_lit(webhook_secret)},
  public_base_url = {_sql_lit(public_base)},
  dns_enabled = true,
  dns_dest_file = {_sql_lit("/data/dns/records")},
  icinga_enabled = true,
  icinga_api_url = 'https://icinga:5665',
  icinga_api_user = 'factum',
  icinga_api_pass = 'factum',
  icinga_hosts_file = {_sql_lit("/data/icinga/hosts.conf")},
  icinga_users_file = {_sql_lit("/data/icinga/users.conf")},
  icinga_host_template = {_sql_lit(host_tmpl)},
  icinga_user_template = {_sql_lit(user_tmpl)},
  librenms_enabled = true,
  librenms_api_url = 'http://librenms:8000',
  librenms_api_token = {_sql_lit(librenms_token)},
  librenms_snmp_version = 'v2c',
  librenms_snmp_communities = 'public',
  oxidized_enabled = true,
  oxidized_api_url = 'http://oxidized:8888',
  oxidized_dest_file = {_sql_lit("/data/oxidized/router.db")}
WHERE id = 1;
"""


def seed(*, demo: bool = False) -> None:
    load_env()
    admin_user = env("FACTUM_ADMIN_USER", "admin")
    admin_pass = env("FACTUM_ADMIN_PASSWORD", "admin")
    netbox_token = env(
        "NETBOX_SUPERUSER_API_TOKEN", "0123456789abcdef0123456789abcdef01234567"
    )
    public_base = env("FACTUM_PUBLIC_BASE_URL", "http://factum-web:8091")
    webhook_secret = env("NETBOX_WEBHOOK_SECRET", "lab-netbox-webhook-secret")
    pg_user = env("POSTGRES_USER", "factum2")
    pg_db = env("POSTGRES_DB", "factum2")
    mysql_root = env("MYSQL_ROOT_PASSWORD", "lab")
    factum_token = env("FACTUM_API_TOKEN")

    log("Preparing dest files")
    prepare(demo=demo)
    # Oxidized runs as uid 30000 and may chown router.db; make dest files
    # writable by the host user and the container.
    run(
        compose("exec", "-T", "-u", "root", "oxidized", "chmod", "-R", "a+rwX", "/home/oxidized/.config/oxidized"),
        check=False,
        quiet=True,
    )

    log("Waiting for NetBox")
    wait_http("http://127.0.0.1:18000/login/", 80)
    if demo:
        _netbox_demo(pg_user=pg_user)
    else:
        log("NetBox demo data skipped (pass --demo to download and import)")

    log("Waiting for LibreNMS")
    wait_http("http://127.0.0.1:18001/login", 80)
    log("Waiting for Icinga API")
    wait_http("https://127.0.0.1:15665/v1/status", 40, required=False)

    log("Ensuring Icinga Web databases")
    run(
        compose(
            "exec",
            "-T",
            "mysql",
            "mysql",
            "-uroot",
            f"-p{mysql_root}",
            "-e",
            ICINGA_DB_SQL,
        )
    )

    log("Installing Icinga API user, factum includes, and Icinga DB")
    for _ in range(40):
        probe = run(
            compose("exec", "-T", "icinga", "test", "-d", "/data/etc/icinga2/conf.d"),
            check=False,
            quiet=True,
        )
        if probe.returncode == 0:
            break
        time.sleep(2)
    _tee_icinga("/data/etc/icinga2/conf.d/api-users.conf", DIR / "icinga" / "api-users.conf")
    _tee_icinga("/data/etc/icinga2/conf.d/factum.conf", DIR / "icinga" / "factum.conf")
    run(compose("exec", "-T", "-u", "root", "icinga", "sh", "-c", "mkdir -p /data/etc/icinga2/features-enabled"))
    _tee_icinga(
        "/data/etc/icinga2/features-enabled/icingadb.conf",
        DIR / "icinga" / "icingadb.conf",
    )
    reload = run(
        compose("exec", "-T", "icinga", "icinga2", "daemon", "--reload"),
        check=False,
        quiet=True,
    )
    if reload.returncode != 0:
        run(
            compose(
                "exec",
                "-T",
                "-u",
                "root",
                "icinga",
                "sh",
                "-c",
                "kill -HUP $(pidof icinga2) 2>/dev/null || true",
            ),
            check=False,
        )

    log("Starting Icinga Web")
    run(compose("up", "-d", "--wait", "--wait-timeout", "180", "icingadb", "icingaweb"))
    wait_http("http://127.0.0.1:18002", 40, required=False)

    log("NetBox API token")
    # Demo dump has admin/admin but no API tokens; first-boot SUPERUSER_API_TOKEN
    # is skipped once that user exists. Always ensure the known lab key.
    #
    # NetBox 4.5+ split Token.key (varchar(12), v2 lookup prefix) from
    # Token.plaintext (varchar(40), v1 secret). Passing the 40-char lab token
    # as key raises StringDataRightTruncation. netboxtool still sends
    # "Authorization: Token <plaintext>", so create a v1 token.
    run(
        compose(
            "exec",
            "-T",
            "netbox",
            "env",
            f"FACTUM_LAB_NETBOX_TOKEN={netbox_token}",
            "/opt/netbox/venv/bin/python",
            "/opt/netbox/netbox/manage.py",
            "shell",
            "--interface",
            "python",
        ),
        input=NETBOX_TOKEN_PY,
    )
    log(f"NetBox token: {netbox_token[:8]}…")

    log("LibreNMS admin user + API token")
    add_user = run(
        compose(
            "exec",
            "-T",
            "librenms",
            "lnms",
            "user:add",
            "admin",
            "--password=admin",
            "--role=admin",
            "--email=admin@lab.example",
        ),
        check=False,
        quiet=True,
    )
    if add_user.returncode != 0:
        run(
            compose(
                "exec",
                "-T",
                "librenms",
                "php",
                "/opt/librenms/lnms",
                "user:add",
                "admin",
                "--password=admin",
                "--role=admin",
                "--email=admin@lab.example",
            ),
            check=False,
            quiet=True,
        )
    token_sql = f"""\
INSERT INTO librenms.api_tokens (user_id, token_hash, description, disabled)
 SELECT user_id, '{LIBRENMS_TOKEN}', 'factum lab', 0 FROM librenms.users WHERE username='admin'
 AND NOT EXISTS (SELECT 1 FROM librenms.api_tokens WHERE token_hash='{LIBRENMS_TOKEN}');
"""
    mariadb = run(
        compose(
            "exec",
            "-T",
            "mysql",
            "mariadb",
            "-u",
            "root",
            f"-p{mysql_root}",
            "-N",
            "-e",
            token_sql,
        ),
        check=False,
        quiet=True,
    )
    if mariadb.returncode != 0:
        run(
            compose(
                "exec",
                "-T",
                "mysql",
                "mysql",
                "-u",
                "root",
                f"-p{mysql_root}",
                "-N",
                "-e",
                token_sql,
            ),
            check=False,
            quiet=True,
        )

    (DIR / "container" / "librenms.env").write_text(
        "\n".join(
            [
                f"MYSQL_DATABASE={env('MYSQL_DATABASE', 'librenms')}",
                f"MYSQL_USER={env('MYSQL_USER', 'librenms')}",
                f"MYSQL_PASSWORD={env('MYSQL_PASSWORD', 'librenms')}",
                "MYSQL_HOST=mysql",
                "MYSQL_PORT=3306",
                "",
            ]
        )
    )

    log("Migrating factum schema")
    web_bin = REPO_ROOT / "build" / "factum2-web"
    if web_bin.is_file() and os.access(web_bin, os.X_OK):
        run([str(web_bin), "migrate", "-f", str(FACTUM_YAML)])
    else:
        run(["go", "run", "./cmd/web", "migrate", "-f", str(FACTUM_YAML)], cwd=str(REPO_ROOT))

    _create_admin(admin_user, admin_pass)

    log("Seeding Settings")
    _psql(
        pg_db,
        _settings_sql(
            netbox_token=netbox_token,
            librenms_token=LIBRENMS_TOKEN,
            factum_token=factum_token,
            webhook_secret=webhook_secret,
            public_base=public_base,
        ),
        user=pg_user,
    )

    log("Registering lab worker node")
    _psql(pg_db, WORKER_SQL, user=pg_user)

    log("NetBox webhook and custom fields")
    netbox_bin = REPO_ROOT / "build" / "factum2-netbox"
    if not netbox_bin.is_file() or not os.access(netbox_bin, os.X_OK):
        raise SystemExit(f"missing {netbox_bin} (make build)")
    # check --update creates the factum-sync webhook, event rules, and custom
    # fields. The interface "role" select cannot be created without operator
    # choices; that one problem is expected on a fresh demo dump.
    check = run(
        compose(
            "run",
            "--rm",
            "--no-deps",
            "-T",
            "--entrypoint",
            "/opt/factum2/factum2-netbox",
            "factum-web",
            "check",
            "--update",
            "-f",
            "/etc/factum2/factum2.yaml",
        ),
        check=False,
    )
    if check.returncode != 0:
        log("NetBox check reported problems (interface custom field 'role' needs operator-defined choices)")

    if DEFAULT_YAML.is_file():
        log(f"Seeding NetBox from {DEFAULT_YAML.name}")
        seed_inventory(
            DEFAULT_YAML,
            url=env("NETBOX_URL", DEFAULT_URL),
            token=netbox_token,
        )
    else:
        log(f"NetBox YAML seed skipped (copy netbox-seed.example.yaml to {DEFAULT_YAML.name})")

    (DIR / "env.sh").write_text(
        "\n".join(
            [
                "# shellcheck disable=SC2148",
                "# Source from the repo root:  . dev/env.sh",
                f'export LIBRENMS_ENV_FILE="{DIR / "data" / "librenms.env"}"',
                f'export PATH="{DIR / "bin"}:$PATH"',
                f'export FACTUM_DEV_CONFIG="{FACTUM_YAML}"',
                "",
            ]
        )
    )

    log("Lab is ready")
    print(
        READY.format(
            factum_yaml=FACTUM_YAML,
            admin_user=admin_user,
            admin_pass=admin_pass,
        )
    )


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--demo",
        action="store_true",
        help="download and import netbox-community demo SQL if NetBox has no devices",
    )
    args = parser.parse_args(argv)
    seed(demo=args.demo)


if __name__ == "__main__":
    main()
