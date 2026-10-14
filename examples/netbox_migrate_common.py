# Shared NetBox REST client and factum settings lookup for the migrate scripts.
# NetBox URL and token come from settings.netbox_api_url / netbox_api_token.
# Postgres is opened with the db: section of factum2.yaml.

import json
import os
import ssl
import subprocess
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_CONFIG = "/etc/factum2/factum2.yaml"

# object type -> REST collection. Unknown types are reported and skipped.
OBJECT_PATHS = {
    "dcim.device": "/api/dcim/devices/",
    "dcim.interface": "/api/dcim/interfaces/",
    "ipam.ipaddress": "/api/ipam/ip-addresses/",
    "virtualization.virtualmachine": "/api/virtualization/virtual-machines/",
    "virtualization.vminterface": "/api/virtualization/interfaces/",
    "tenancy.tenant": "/api/tenancy/tenants/",
    "tenancy.contact": "/api/tenancy/contacts/",
}


class APIError(Exception):
    pass


def _yaml_scalar(value):
    """One unquoted or single-quoted/double-quoted YAML scalar. No escapes."""
    value = value.strip()
    if value == "":
        return ""
    if value[0] in "\"'":
        quote = value[0]
        end = value.find(quote, 1)
        if end > 0:
            return value[1:end]
    comment = value.find(" #")
    if comment >= 0:
        value = value[:comment].rstrip()
    return value


def parse_factum_db(text):
    """Return the db: mapping from a factum2.yaml document.

    Only host, port, user, pass, and database are kept. Other sections are ignored.
    """
    db = {}
    in_db = False
    db_indent = 0
    for raw in text.splitlines():
        stripped = raw.strip()
        if stripped == "" or stripped.startswith("#"):
            continue
        indent = len(raw) - len(raw.lstrip(" "))
        if not in_db:
            if stripped == "db:" or stripped.startswith("db:"):
                in_db = True
                db_indent = indent
            continue
        if indent <= db_indent:
            break
        key, sep, value = stripped.partition(":")
        if sep == "":
            continue
        key = key.strip()
        if key in ("host", "port", "user", "pass", "database"):
            db[key] = _yaml_scalar(value)
    return db


def _settings_row_psycopg(db):
    params = {
        "host": db["host"],
        "port": db.get("port") or "5432",
        "user": db["user"],
        "password": db.get("pass") or "",
        "dbname": db["database"],
    }
    sql = "SELECT netbox_api_url, netbox_api_token FROM settings ORDER BY id LIMIT 1"
    try:
        import psycopg
    except ImportError:
        psycopg = None
    if psycopg is not None:
        with psycopg.connect(**params) as conn:
            row = conn.execute(sql).fetchone()
        return row
    import psycopg2
    conn = psycopg2.connect(**params)
    try:
        cur = conn.cursor()
        cur.execute(sql)
        return cur.fetchone()
    finally:
        conn.close()


def _settings_row_psql(db):
    env = os.environ.copy()
    env["PGPASSWORD"] = db.get("pass") or ""
    cmd = [
        "psql",
        "-h", db["host"],
        "-p", str(db.get("port") or "5432"),
        "-U", db["user"],
        "-d", db["database"],
        "-v", "ON_ERROR_STOP=1",
        "-At",
        "-F", "\t",
        "-c", "SELECT netbox_api_url, netbox_api_token FROM settings ORDER BY id LIMIT 1",
    ]
    try:
        proc = subprocess.run(cmd, env=env, capture_output=True, text=True, check=False)
    except FileNotFoundError as exc:
        raise RuntimeError("psql is not installed and neither psycopg nor psycopg2 is available") from exc
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip()
        raise RuntimeError("psql failed: %s" % detail)
    line = (proc.stdout or "").strip()
    if line == "":
        return None
    url, _sep, token = line.partition("\t")
    return (url, token)


def load_netbox_settings(config_path):
    """Read settings.netbox_api_url and netbox_api_token using db: from factum2.yaml."""
    try:
        with open(config_path, encoding="utf-8") as fh:
            text = fh.read()
    except OSError as exc:
        raise RuntimeError("cannot read %s: %s" % (config_path, exc.strerror or exc)) from exc
    db = parse_factum_db(text)
    missing = [key for key in ("host", "user", "database") if not db.get(key)]
    if missing:
        raise RuntimeError("%s db section is missing %s" % (config_path, ", ".join(missing)))
    try:
        row = _settings_row_psycopg(db)
    except ImportError:
        row = _settings_row_psql(db)
    except RuntimeError:
        raise
    except Exception as exc:
        raise RuntimeError("cannot read settings: %s" % exc) from exc
    if not row:
        raise RuntimeError("settings has no row")
    url = (row[0] or "").strip()
    token = (row[1] or "").strip()
    if url == "" or token == "":
        raise RuntimeError("settings.netbox_api_url or settings.netbox_api_token is empty")
    return url, token


class NetBox:
    def __init__(self, base_url, token, timeout):
        self.base = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout
        self.ctx = ssl.create_default_context()

    def get(self, path, query=None):
        return self._json("GET", self._url(path, query), None)

    def patch(self, path, body):
        return self._json("PATCH", self._url(path, None), body)

    def _url(self, path, query):
        if path.startswith("http://") or path.startswith("https://"):
            url = path
        else:
            if not path.startswith("/"):
                path = "/" + path
            url = self.base + path
        if query:
            join = "&" if "?" in url else "?"
            url = url + join + urllib.parse.urlencode(query)
        return url

    def _json(self, method, url, body):
        data = None
        headers = {
            "Authorization": "Token " + self.token,
            "Accept": "application/json",
            "User-Agent": "factum2-netbox-migrate",
        }
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self.ctx) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", "replace")[:500]
            raise APIError("%s %s -> HTTP %s %s" % (method, url, exc.code, detail)) from exc
        except urllib.error.URLError as exc:
            raise APIError("%s %s -> %s" % (method, url, exc.reason)) from exc
        if not raw:
            return None
        return json.loads(raw.decode("utf-8"))


def results(page):
    if isinstance(page, list):
        return page, None
    if not isinstance(page, dict):
        raise APIError("unexpected list response")
    return page.get("results") or [], page.get("next")


def object_types(field, resolve_content_type=None):
    types = []
    raw = field.get("object_types")
    if isinstance(raw, list):
        for item in raw:
            if isinstance(item, str) and item:
                types.append(item)
    if not types:
        raw = field.get("content_types")
        if isinstance(raw, list):
            for item in raw:
                if isinstance(item, str) and "." in item:
                    types.append(item)
                elif isinstance(item, dict):
                    app = item.get("app_label") or ""
                    model = item.get("model") or ""
                    if app and model:
                        types.append(app + "." + model)
                elif isinstance(item, int) and resolve_content_type is not None:
                    resolved = resolve_content_type(item)
                    if resolved:
                        types.append(resolved)
    seen = []
    for item in types:
        if item not in seen:
            seen.append(item)
    return seen


def content_type_resolver(nb):
    def resolve(content_type_id):
        row = nb.get("/api/extras/content-types/%s/" % content_type_id)
        if not isinstance(row, dict):
            return ""
        app = row.get("app_label") or ""
        model = row.get("model") or ""
        if app and model:
            return app + "." + model
        return ""

    return resolve


def field_type(field):
    """Custom-field type slug. NetBox may return "text" or {"value": "text", "label": "Text"}."""
    raw = field.get("type") if isinstance(field, dict) else None
    if isinstance(raw, dict):
        raw = raw.get("value")
    if raw is None:
        return ""
    return str(raw).strip().lower()


def find_field(nb, name):
    page = nb.get("/api/extras/custom-fields/", {"name": name, "limit": 50})
    rows, _next = results(page)
    for field in rows:
        if field.get("name") == name:
            return field
    return None


def iter_query(nb, path, query):
    """Yield every object for one list query, following next."""
    page = nb.get(path, query)
    while True:
        rows, nxt = results(page)
        for row in rows:
            yield row
        if not nxt:
            return
        page = nb.get(nxt)


def iter_filtered(nb, path, field_name, lookup):
    """Yield objects matching one custom-field lookup. On HTTP 400, scan the collection once.

    lookup is "gt" (cf_<field>__gt=0) or "empty" (cf_<field>__empty=false).
    """
    if lookup == "gt":
        query = {"cf_%s__gt" % field_name: "0", "limit": "100"}
    elif lookup == "empty":
        query = {"cf_%s__empty" % field_name: "false", "limit": "100"}
    else:
        raise ValueError("unknown lookup %r" % lookup)
    try:
        yield from iter_query(nb, path, query)
        return
    except APIError as exc:
        if "HTTP 400" not in str(exc):
            raise
    print("note: %s rejected the %s filter; scanning every object" % (path, field_name))
    yield from iter_query(nb, path, {"limit": "100"})


def row_label(row):
    name = row.get("name") or row.get("display") or row.get("address") or ""
    return "pk=%s%s" % (row.get("id"), (" " + name) if name else "")


def patch_custom_field(nb, path, row, field, value):
    nb.patch(path.rstrip("/") + "/" + str(row.get("id")) + "/", {
        "custom_fields": {field: value},
    })


def add_common_args(parser):
    parser.add_argument("--url", default=os.environ.get("NETBOX_URL", ""), help="NetBox base URL (or NETBOX_URL). Overrides settings.")
    parser.add_argument("--token", default=os.environ.get("NETBOX_TOKEN", ""), help="NetBox API token (or NETBOX_TOKEN). Overrides settings.")
    parser.add_argument("--config", default=os.environ.get("FACTUM_CONFIG", DEFAULT_CONFIG),
                        help="factum2.yaml whose db: section opens Postgres (default /etc/factum2/factum2.yaml)")
    parser.add_argument("--timeout", type=float, default=60, help="HTTP timeout in seconds")
    parser.add_argument("--dry-run", action="store_true", help="print the writes without PATCHing")


def open_client(args):
    url, token = args.url, args.token
    if not url or not token:
        loaded_url, loaded_token = load_netbox_settings(args.config)
        url = url or loaded_url
        token = token or loaded_token
    return NetBox(url, token, args.timeout)
