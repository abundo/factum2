#!/usr/bin/env python3
"""Idempotent lab bootstrap: wait for compose services, overlay icinga API
config and Icinga DB, optionally load NetBox demo data (--demo), migrate
factum, create admin, seed Settings, LibreNMS token, NetBox webhook and
custom fields. If netbox-seed.yaml exists, load that inventory via the API.

`--icinga-db` only creates the Icinga MariaDB databases so icingadb /
icingaweb can start in parallel with NetBox (see `make dev-up`).
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
# LibreNMS 25.11+ Password::defaults(): min 8 chars and a symbol (not "admin").
LIBRENMS_ADMIN_USER = "admin"
LIBRENMS_ADMIN_PASS = "Admin-lab1!"

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

DNS_SEED_SQL = """\
INSERT INTO dns_soa_templates (name, mname, rname, refresh, retry, expire, ttl, created_at, updated_at)
SELECT 'default_soa', 'ns1.lab.example.', 'hostmaster.lab.example.', 36000, 3600, 604800, 900, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM dns_soa_templates WHERE name = 'default_soa');

INSERT INTO dns_templates (name, soa_template_id, default_ttl, created_at, updated_at)
SELECT 'default_dns', id, 900, NOW(), NOW() FROM dns_soa_templates WHERE name = 'default_soa'
 AND NOT EXISTS (SELECT 1 FROM dns_templates WHERE name = 'default_dns');

INSERT INTO dns_template_nameservers (dns_template_id, rank, hostname)
SELECT t.id, 0, 'ns1.lab.example.' FROM dns_templates t WHERE t.name = 'default_dns'
 AND NOT EXISTS (
   SELECT 1 FROM dns_template_nameservers n WHERE n.dns_template_id = t.id AND n.hostname = 'ns1.lab.example.'
 );

INSERT INTO dns_zones (name, type, dns_template_id, comment, created_at, updated_at)
SELECT 'lab.example', 'forward', t.id, 'lab zone', NOW(), NOW() FROM dns_templates t WHERE t.name = 'default_dns'
 AND NOT EXISTS (SELECT 1 FROM dns_zones WHERE name = 'lab.example');

INSERT INTO dns_zone_records (dns_zone_id, rank, name, record_type, value, description)
SELECT z.id, 0, 'ns1', 'A', '127.0.0.1', 'in-zone nameserver'
 FROM dns_zones z WHERE z.name = 'lab.example'
 AND NOT EXISTS (
   SELECT 1 FROM dns_zone_records r WHERE r.dns_zone_id = z.id AND r.name = 'ns1' AND r.record_type = 'A'
 );
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
  LibreNMS:       http://127.0.0.1:18001  admin / Admin-lab1!
  Icinga Web:     http://127.0.0.1:18002  admin / admin
  Grafana:        http://127.0.0.1:18003  admin / admin
  Icinga API:     https://127.0.0.1:15665  factum / factum
  Oxidized:       http://127.0.0.1:18888
  Prometheus:     http://127.0.0.1:19090
  Alertmanager:   http://127.0.0.1:19093
  snmp-exporter:  http://127.0.0.1:19116
  BIND:           127.0.0.1:18053          zone lab.example (factum2-dns)
  Worker hubs:    18443 factum-worker · 18444 dns · 18445 icinga · 18446 librenms · 18447 oxidized · 18448 prometheus
  Software:       http://127.0.0.1:18088  (SFTP 127.0.0.1:12222 factum / lab)
  Postgres:       127.0.0.1:15432          factum2 / factum2  (DBs: factum2, netbox)
  MariaDB:        127.0.0.1:13306          librenms / librenms

  Login: {admin_user} / {admin_pass}
"""


def _sql_lit(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def _worker_sql(name: str, address: str, token: str) -> str:
    return f"""\
INSERT INTO worker_nodes (name, address, token, enabled, tls_skip_verify, tls_ca, created_at, updated_at)
SELECT {_sql_lit(name)}, {_sql_lit(address)}, {_sql_lit(token)}, true, true, '', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM worker_nodes WHERE name = {_sql_lit(name)});
UPDATE worker_nodes SET
  address = {_sql_lit(address)},
  token = {_sql_lit(token)},
  enabled = true,
  tls_skip_verify = true
WHERE name = {_sql_lit(name)};
"""


WORKER_SQL = "".join(
    _worker_sql(name, address, token)
    for name, address, token in (
        ("lab", "factum-worker:8443", "lab-worker-token"),
        ("dns", "dns:8443", "lab-dns-worker-token"),
        ("icinga", "icinga:8443", "lab-icinga-worker-token"),
        ("librenms", "librenms:8443", "lab-librenms-worker-token"),
        ("oxidized", "oxidized:8443", "lab-oxidized-worker-token"),
        ("prometheus", "prometheus:8443", "lab-prometheus-worker-token"),
    )
)


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
    web_bin = REPO_ROOT / "build" / "factum2-web"
    extra_env = {**os.environ, "FACTUM_ADMIN_PASSWORD": admin_pass}
    if web_bin.is_file() and os.access(web_bin, os.X_OK):
        cmd = [str(web_bin), "createadmin", "-f", str(FACTUM_YAML), "-p", admin_pass]
        cwd = None
    else:
        cmd = [
            "go",
            "run",
            "./cmd/web",
            "createadmin",
            "-f",
            str(FACTUM_YAML),
            "-p",
            admin_pass,
        ]
        cwd = str(REPO_ROOT)
    result = run(
        cmd,
        check=False,
        capture_output=True,
        text=True,
        cwd=cwd,
        env=extra_env,
    )
    if result.returncode == 0:
        return
    combined = (result.stdout or "") + (result.stderr or "")
    if "unknown" in combined.lower() and "-p" in combined:
        log("createadmin -p not supported by this binary; using tmux prompt")
        _create_admin_tmux(admin_pass)
        return
    raise SystemExit(f"createadmin failed: {combined.strip() or 'no output'}")


def _create_admin_tmux(admin_pass: str) -> None:
    if not shutil.which("tmux"):
        raise SystemExit(
            f"tmux not found; rebuild factum2-web or run: "
            f"go run ./cmd/web createadmin -f {FACTUM_YAML} -p <password>"
        )
    web_bin = REPO_ROOT / "build" / "factum2-web"
    if web_bin.is_file() and os.access(web_bin, os.X_OK):
        create = f"'{web_bin}' createadmin -f '{FACTUM_YAML}'"
    else:
        create = f"cd '{REPO_ROOT}' && go run ./cmd/web createadmin -f '{FACTUM_YAML}'"
    run(["tmux", "kill-session", "-t", "factum2-dev-admin"], check=False, quiet=True)
    run(["tmux", "new-session", "-d", "-s", "factum2-dev-admin", "-x", "200", "-y", "50"])
    run(["tmux", "send-keys", "-t", "factum2-dev-admin", create, "Enter"])
    # createadmin uses term.ReadPassword (needs a TTY). The prebuilt binary
    # prompts immediately; `go run` compiles first. Wait for the prompt
    # instead of a fixed sleep that races the compiler.
    prompted = False
    for _ in range(60):
        pane = run(
            ["tmux", "capture-pane", "-t", "factum2-dev-admin", "-p"],
            check=False,
            capture_output=True,
            text=True,
        )
        if "password" in (pane.stdout or "").lower():
            prompted = True
            break
        time.sleep(0.5)
    if not prompted:
        run(["tmux", "kill-session", "-t", "factum2-dev-admin"], check=False, quiet=True)
        raise SystemExit("createadmin did not prompt for a password")
    run(["tmux", "send-keys", "-t", "factum2-dev-admin", admin_pass, "Enter"])
    time.sleep(0.4)
    run(["tmux", "send-keys", "-t", "factum2-dev-admin", admin_pass, "Enter"])
    time.sleep(1)
    run(["tmux", "kill-session", "-t", "factum2-dev-admin"], check=False, quiet=True)


def _librenms_user_add_output(text: str) -> str:
    return " ".join(text.split())


def _librenms_user_exists(text: str) -> bool:
    lower = text.lower()
    return "already been taken" in lower or "already exists" in lower


def _check_dns_tools() -> None:
    check = run(
        compose("exec", "-T", "dns", "sh", "-c", "command -v named-checkzone && command -v rndc"),
        check=False,
        capture_output=True,
        text=True,
    )
    if check.returncode != 0:
        raise SystemExit(
            "dns container is missing named-checkzone or rndc: "
            + ((check.stdout or "") + (check.stderr or "")).strip()
        )


def _disable_librenms_install_wizard() -> None:
    # Official image appends INSTALL=user,finish when the DB has no tables.
    # That keeps /login redirecting to the first-user wizard after user:add.
    script = """
set -e
found=0
for f in /opt/librenms/.env /data/.env; do
  [ -f "$f" ] || continue
  if grep -q '^INSTALL=' "$f"; then
    sed -i '/^INSTALL=/d' "$f"
    found=1
  fi
done
if [ "$found" = 1 ]; then
  artisan config:clear --no-interaction
  artisan config:cache --no-interaction
  s6-svc -r /var/run/s6/services/php-fpm 2>/dev/null || s6-svc -r /run/s6/services/php-fpm 2>/dev/null || true
fi
"""
    run(compose("exec", "-T", "librenms", "sh", "-c", script), quiet=True)


def _ensure_librenms_admin() -> None:
    attempts = (
        compose(
            "exec",
            "-T",
            "librenms",
            "lnms",
            "user:add",
            LIBRENMS_ADMIN_USER,
            f"--password={LIBRENMS_ADMIN_PASS}",
            "--role=admin",
            "--email=admin@lab.example",
        ),
        compose(
            "exec",
            "-T",
            "librenms",
            "php",
            "/opt/librenms/lnms",
            "user:add",
            LIBRENMS_ADMIN_USER,
            f"--password={LIBRENMS_ADMIN_PASS}",
            "--role=admin",
            "--email=admin@lab.example",
        ),
    )
    last = ""
    for cmd in attempts:
        result = run(cmd, check=False, capture_output=True, text=True)
        last = _librenms_user_add_output((result.stdout or "") + "\n" + (result.stderr or ""))
        if result.returncode == 0:
            _disable_librenms_install_wizard()
            return
        if _librenms_user_exists(last):
            log("LibreNMS admin user already exists")
            _disable_librenms_install_wizard()
            return
    raise SystemExit(f"LibreNMS user:add failed: {last or 'no output'}")


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
    # OxidizedApiURL is used by factum2-oxidized (reload) and by
    # factum-web's /oxidized browser. Compose DNS oxidized:8888 is
    # reachable from both; 127.0.0.1:8888 is only the oxidized container.
    return f"""
INSERT INTO settings (id, created_at, updated_at)
SELECT 1, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE id = 1);

UPDATE settings SET
  default_domain = 'lab.example',
  factum_api_token = {_sql_lit(factum_token)},
  organization_enabled = true,
  optical_enabled = true,
  ipam_enabled = true,
  dns_zones_enabled = true,
  device_sync_enabled = true,
  netbox_enabled = true,
  netbox_api_url = 'http://netbox:8080',
  netbox_api_token = {_sql_lit(netbox_token)},
  netbox_webhook_secret = {_sql_lit(webhook_secret)},
  public_base_url = {_sql_lit(public_base)},
  certs_enabled = true,
  certs_lego_yaml = {_sql_lit("/var/lib/lego/.lego.yaml")},
  certs_env_file = {_sql_lit("/var/lib/lego/.env")},
  certs_lego_bin = {_sql_lit("/usr/local/bin/lego")},
  certs_lego_storage = {_sql_lit("/var/lib/lego/storage")},
  certs_default_key_type = 'EC256',
  dns_enabled = true,
  dns_dest_file = {_sql_lit("/etc/dnsmgr2/records")},
  dns_zones_file = {_sql_lit("/etc/dnsmgr2/zones.yaml")},
  dhcp_prefixes_file = {_sql_lit("/etc/dnsmgr2/prefixes.yaml")},
  icinga_enabled = true,
  icinga_api_url = 'https://127.0.0.1:5665',
  icinga_api_user = 'factum',
  icinga_api_pass = 'factum',
  icinga_hosts_file = {_sql_lit("/factum/hosts.conf")},
  icinga_users_file = {_sql_lit("/factum/users.conf")},
  icinga_host_template = {_sql_lit(host_tmpl)},
  icinga_user_template = {_sql_lit(user_tmpl)},
  librenms_enabled = true,
  librenms_api_url = 'http://127.0.0.1:8000/api/v0',
  librenms_api_token = {_sql_lit(librenms_token)},
  librenms_snmp_version = 'v2c',
  librenms_snmp_communities = 'public',
  oxidized_enabled = true,
  oxidized_api_url = 'http://oxidized:8888',
  oxidized_dest_file = {_sql_lit("/home/oxidized/.config/oxidized/router.db")},
  prometheus_enabled = true,
  prometheus_dest_file = {_sql_lit("/etc/prometheus/targets.json")},
  prometheus_reload_url = 'http://127.0.0.1:9090/-/reload',
  storage_enabled = true,
  storage_root = {_sql_lit("/var/lib/factum2/storage")},
  storage_http_listen = ':8088',
  storage_http_url = {_sql_lit("http://factum-storage:8088")},
  storage_tftp_listen = '',
  storage_tftp_host = '',
  storage_sftp_listen = ':2222',
  storage_sftp_host = 'factum-storage:2222',
  storage_sftp_user = 'factum',
  storage_sftp_password = 'lab'
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
    _check_dns_tools()

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
    ensure_icinga_databases(mysql_root)

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
    wait_http("http://127.0.0.1:18003/login", 40, required=False)

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
    _ensure_librenms_admin()
    token_sql = f"""\
INSERT INTO librenms.api_tokens (user_id, token_hash, description, disabled)
 SELECT user_id, '{LIBRENMS_TOKEN}', 'factum lab', 0 FROM librenms.users WHERE username='{LIBRENMS_ADMIN_USER}'
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

    log("Registering lab worker nodes")
    _psql(pg_db, WORKER_SQL, user=pg_user)

    log("Seeding DNS SOA / template / lab.example zone")
    _psql(pg_db, DNS_SEED_SQL, user=pg_user)

    log("NetBox webhook and custom fields")
    netbox_bin = REPO_ROOT / "build" / "factum2-netbox"
    if not netbox_bin.is_file() or not os.access(netbox_bin, os.X_OK):
        raise SystemExit(f"missing {netbox_bin} (make build)")
    # check --update creates the factum-sync webhook, event rules, and custom
    # fields (including interface "role" with lab seed choices). Run inside
    # the compose network so Settings.NetboxApiURL (http://netbox:8080) resolves.
    run(
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
        )
    )

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


def ensure_icinga_databases(mysql_root: str | None = None) -> None:
    """Create icingadb/icingaweb on the shared MariaDB.

    docker-entrypoint-initdb.d only runs on an empty volume, so this is
    required on every subsequent `make dev-up` before icingadb can start.
    Retries while mysqld is still coming up.
    """
    load_env()
    if mysql_root is None:
        mysql_root = env("MYSQL_ROOT_PASSWORD", "lab")
    last = ""
    for _ in range(60):
        result = run(
            compose(
                "exec",
                "-T",
                "mysql",
                "mysql",
                "-uroot",
                f"-p{mysql_root}",
                "-e",
                ICINGA_DB_SQL,
            ),
            check=False,
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            return
        last = ((result.stdout or "") + (result.stderr or "")).strip()
        time.sleep(1)
    raise SystemExit(f"MariaDB not ready for Icinga databases: {last or 'no output'}")


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--demo",
        action="store_true",
        help="download and import netbox-community demo SQL if NetBox has no devices",
    )
    parser.add_argument(
        "--icinga-db",
        action="store_true",
        help="only create Icinga MariaDB databases, then exit",
    )
    args = parser.parse_args(argv)
    if args.icinga_db:
        log("Ensuring Icinga Web databases")
        ensure_icinga_databases()
        return
    seed(demo=args.demo)


if __name__ == "__main__":
    main()
