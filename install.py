#!/usr/bin/env python3
"""Install factum2 on the primary and its remote workers.

Two sources, one install path (binaries to /opt/factum2, systemd units to
/etc/systemd/system, restart services, then the same binaries out to every
enabled worker_nodes row).

On the primary this installs factum2-web.service and factum2-worker.service;
on each worker, factum2-worker.service. If a unit already exists and differs
from this release, the installer shows a diff and asks before overwriting
(--yes overwrites without asking). Newly installed units are enabled.

  ./install.py                 GitHub release (production). TUI on a TTY,
                               or --list / --install TAG. A standalone copy
                               (no git checkout) is offered a self-update if
                               GitHub has a newer install.py.
  ./install.py --source [host] This source tree (development). Runs
                               `make release` and installs build/ onto host
                               (default localhost). Replaces install_prod.sh.
"""

from __future__ import annotations

import argparse
import base64
import curses
import difflib
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable, Sequence

REPO_DIR = Path(__file__).resolve().parent
REPO_DEFAULT = "abundo/factum2"
INSTALL_DIR_DEFAULT = "/opt/factum2"
CONFIG_PATH_DEFAULT = "/etc/factum2/factum2.yaml"
POSTGRES_COMPOSE_DIR = Path("/opt/postgresql")
POSTGRES_COMPOSE_NAMES = (
    "compose.yaml",
    "compose.yml",
    "docker-compose.yaml",
    "docker-compose.yml",
)
SYSTEMD_DIR = Path("/etc/systemd/system")
PRIMARY_UNITS = ("factum2-web.service", "factum2-worker.service")
WORKER_UNIT = "factum2-worker.service"
ARCHIVE_OS = "linux"
USER_AGENT = "factum2-install.py"
# Bump when the installer itself changes so production copies can detect
# a newer GitHub version. Missing/unparseable counts as 0.
INSTALLER_VERSION = 4
INSTALLER_FILENAME = "install.py"
SELF_UPDATED_ENV = "FACTUM2_INSTALL_SELF_UPDATED"

# Known binaries shipped in the GoReleaser tar.gz. Discovery also accepts
# any other top-level `factum*` file so a newly added cmd/ still installs.
KNOWN_BINARIES = (
    "factum",
    "factum-becs",
    "factum-device-sync",
    "factum-dns",
    "factum-driver",
    "factum-icinga",
    "factum-icinga-notifications",
    "factum-lime",
    "factum-librenms",
    "factum-netbox",
    "factum-oxidized",
    "factum-web",
    "factum-worker",
)


class InstallError(Exception):
    """Fatal, already-formatted error for the CLI."""


# ---------------------------------------------------------------------------
# small helpers
# ---------------------------------------------------------------------------


def log(msg: str) -> None:
    print(msg, flush=True)


def local_arch() -> str:
    machine = os.uname().machine
    return {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }.get(machine, machine)


def uname_to_goarch(machine: str) -> str:
    return {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }.get(machine.strip(), machine.strip())


def is_local_host(host: str) -> bool:
    return host in {"localhost", "127.0.0.1", "::1"}


def strip_v(tag: str) -> str:
    tag = tag.strip()
    return tag[1:] if tag.startswith("v") or tag.startswith("V") else tag


def parse_version(tag: str) -> tuple[int, int, int] | None:
    """Numeric X.Y.Z prefix. git-describe suffixes and 'dev' return None-able extras via is_dev_build."""
    m = re.match(r"v?(\d+)\.(\d+)\.(\d+)", tag.strip())
    if not m:
        return None
    return int(m.group(1)), int(m.group(2)), int(m.group(3))


def is_dev_build(tag: str) -> bool:
    s = tag.strip()
    if s in {"", "dev", "unknown", "none", "not installed"}:
        return True
    # git describe: v1.0.0-3-gdeadbee[-dirty]
    return bool(re.search(r"-\d+-g[0-9a-f]+", s)) or s.endswith("-dirty")


def version_newer(release_tag: str, installed: str) -> bool:
    rel = parse_version(release_tag)
    cur = parse_version(installed)
    if rel is None or cur is None:
        return False
    return rel > cur


def versions_equal(release_tag: str, installed: str) -> bool:
    if is_dev_build(installed):
        return False
    a, b = parse_version(release_tag), parse_version(installed)
    return a is not None and a == b


def same_base(release_tag: str, installed: str) -> bool:
    a, b = parse_version(release_tag), parse_version(installed)
    return a is not None and a == b


def format_date(iso: str) -> str:
    if not iso:
        return "-"
    try:
        dt = datetime.fromisoformat(iso.replace("Z", "+00:00"))
        return dt.date().isoformat()
    except ValueError:
        return iso[:10]


def cache_dir() -> Path:
    xdg = os.environ.get("XDG_CACHE_HOME")
    base = Path(xdg) if xdg else Path.home() / ".cache"
    return base / "factum2-releases"


def which(name: str) -> str | None:
    return shutil.which(name)


# ---------------------------------------------------------------------------
# GitHub
# ---------------------------------------------------------------------------


class StripAuthOnRedirect(urllib.request.HTTPRedirectHandler):
    """GitHub's asset API 302s to a signed URL that rejects Authorization."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        new = super().redirect_request(req, fp, code, msg, headers, newurl)
        if new is None:
            return None
        orig = urllib.parse.urlparse(req.full_url).netloc
        dest = urllib.parse.urlparse(new.full_url).netloc
        if orig != dest:
            for key in list(new.headers):
                if key.lower() == "authorization":
                    del new.headers[key]
        return new


def github_token() -> str | None:
    for key in ("GITHUB_TOKEN", "GH_TOKEN"):
        val = os.environ.get(key, "").strip()
        if val:
            return val
    gh = which("gh")
    if not gh:
        return None
    try:
        out = subprocess.run(
            [gh, "auth", "token"],
            check=False,
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    token = (out.stdout or "").strip()
    return token or None


def github_opener() -> urllib.request.OpenerDirector:
    return urllib.request.build_opener(StripAuthOnRedirect)


@dataclass
class Asset:
    name: str
    size: int
    url: str
    api_url: str
    id: int


@dataclass
class Release:
    tag: str
    name: str
    published_at: str
    prerelease: bool
    draft: bool
    html_url: str
    assets: list[Asset] = field(default_factory=list)

    @property
    def version(self) -> str:
        return strip_v(self.tag)

    def archive(self, goos: str, goarch: str) -> Asset | None:
        suffix = f"_{goos}_{goarch}.tar.gz"
        for asset in self.assets:
            if asset.name.endswith(suffix) and "checksums" not in asset.name:
                return asset
        return None

    def checksums(self) -> Asset | None:
        for asset in self.assets:
            if asset.name.endswith("_checksums.txt"):
                return asset
        return None


class GithubClient:
    def __init__(self, repo: str, token: str | None):
        self.repo = repo
        self.token = token
        self.opener = github_opener()

    def _headers(self, accept: str = "application/vnd.github+json") -> dict[str, str]:
        headers = {
            "Accept": accept,
            "User-Agent": USER_AGENT,
            "X-GitHub-Api-Version": "2022-11-28",
        }
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        return headers

    def _get(self, url: str, accept: str = "application/vnd.github+json") -> tuple[bytes, dict[str, str]]:
        req = urllib.request.Request(url, headers=self._headers(accept))
        try:
            with self.opener.open(req, timeout=60) as resp:
                body = resp.read()
                headers = {k.lower(): v for k, v in resp.headers.items()}
                return body, headers
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", "replace")[:300]
            if exc.code == 404:
                raise InstallError(
                    f"GitHub repo {self.repo} not found (404). "
                    "If it is private, set GITHUB_TOKEN or run `gh auth login`."
                ) from exc
            raise InstallError(f"GitHub request failed ({exc.code}): {url}\n{detail}") from exc
        except urllib.error.URLError as exc:
            raise InstallError(f"GitHub request failed: {exc}") from exc

    def list_releases(self, limit: int = 50) -> list[Release]:
        releases: list[Release] = []
        url: str | None = (
            f"https://api.github.com/repos/{self.repo}/releases?per_page=100"
        )
        while url and len(releases) < limit:
            body, headers = self._get(url)
            page = json.loads(body.decode())
            if not isinstance(page, list):
                raise InstallError(f"Unexpected GitHub releases payload: {type(page)}")
            for item in page:
                rel = _parse_release(item)
                if rel.draft:
                    continue
                releases.append(rel)
                if len(releases) >= limit:
                    break
            url = _next_link(headers.get("link", ""))
        return releases

    def fetch_file(self, path: str, ref: str | None = None) -> bytes:
        """Raw file from the repo (default branch if ref is omitted)."""
        quoted = urllib.parse.quote(path)
        url = f"https://api.github.com/repos/{self.repo}/contents/{quoted}"
        if ref:
            url += f"?ref={urllib.parse.quote(ref)}"
        body, _ = self._get(url, accept="application/vnd.github.raw")
        stripped = body.lstrip()
        if stripped.startswith(b"{") and b'"content"' in stripped[:800]:
            data = json.loads(body)
            if data.get("encoding") == "base64" and data.get("content"):
                return base64.b64decode(data["content"])
        return body

    def download(self, asset: Asset, dest: Path, progress: bool = True) -> None:
        dest.parent.mkdir(parents=True, exist_ok=True)
        tmp = dest.with_suffix(dest.suffix + ".partial")
        # Private-repo downloads must go through the asset API with
        # Accept: application/octet-stream; public ones can use browser_download_url.
        url = asset.api_url if (self.token and asset.api_url) else asset.url
        req = urllib.request.Request(url, headers=self._headers("application/octet-stream"))
        try:
            with self.opener.open(req, timeout=60) as resp, open(tmp, "wb") as out:
                total = resp.headers.get("Content-Length")
                total_n = int(total) if total and total.isdigit() else asset.size
                got = 0
                t0 = time.monotonic()
                while True:
                    chunk = resp.read(1024 * 256)
                    if not chunk:
                        break
                    out.write(chunk)
                    got += len(chunk)
                    if progress:
                        _download_progress(asset.name, got, total_n, t0)
            if progress:
                print(file=sys.stderr)
        except urllib.error.HTTPError as exc:
            tmp.unlink(missing_ok=True)
            raise InstallError(f"Download failed ({exc.code}): {asset.name}") from exc
        tmp.replace(dest)


def _parse_release(item: dict) -> Release:
    assets = [
        Asset(
            name=a["name"],
            size=int(a.get("size") or 0),
            url=a.get("browser_download_url") or "",
            api_url=a.get("url") or "",
            id=int(a.get("id") or 0),
        )
        for a in item.get("assets") or []
    ]
    return Release(
        tag=item.get("tag_name") or "",
        name=item.get("name") or item.get("tag_name") or "",
        published_at=item.get("published_at") or "",
        prerelease=bool(item.get("prerelease")),
        draft=bool(item.get("draft")),
        html_url=item.get("html_url") or "",
        assets=assets,
    )


def _next_link(link_header: str) -> str | None:
    # <url>; rel="next", <url>; rel="last"
    for part in link_header.split(","):
        part = part.strip()
        if 'rel="next"' not in part:
            continue
        m = re.search(r"<([^>]+)>", part)
        if m:
            return m.group(1)
    return None


def _download_progress(name: str, got: int, total: int, t0: float) -> None:
    elapsed = max(time.monotonic() - t0, 0.001)
    speed = got / elapsed
    if total:
        pct = min(got / total * 100.0, 100.0)
        bar_w = 24
        fill = int(bar_w * got / total) if total else 0
        bar = "#" * fill + "-" * (bar_w - fill)
        msg = f"\r    {name}  [{bar}] {pct:5.1f}%  {_fmt_bytes(got)}/{_fmt_bytes(total)}  {_fmt_bytes(speed)}/s"
    else:
        msg = f"\r    {name}  {_fmt_bytes(got)}  {_fmt_bytes(speed)}/s"
    print(msg, end="", file=sys.stderr, flush=True)


def _fmt_bytes(n: float) -> str:
    for unit in ("B", "KiB", "MiB", "GiB"):
        if abs(n) < 1024 or unit == "GiB":
            if unit == "B":
                return f"{int(n)} {unit}"
            return f"{n:.1f} {unit}"
        n /= 1024
    return f"{n:.1f} GiB"


# ---------------------------------------------------------------------------
# installed version, config, workers
# ---------------------------------------------------------------------------


def read_installed_version(install_dir: Path) -> str:
    version_file = install_dir / "VERSION"
    if version_file.is_file():
        text = version_file.read_text(encoding="utf-8", errors="replace").strip()
        if text:
            return text.splitlines()[0].strip()
    for name in ("factum-web", "factum", "factum-worker"):
        binary = install_dir / name
        if not binary.is_file():
            continue
        ver = _version_from_binary(binary)
        if ver:
            return ver
    if not (install_dir / "factum-web").exists() and not (install_dir / "factum").exists():
        return "not installed"
    return "unknown"


def _version_from_binary(binary: Path) -> str | None:
    """Best-effort: cobra --version, then a `version` subcommand."""
    for args in ([str(binary), "--version"], [str(binary), "version"]):
        try:
            proc = subprocess.run(
                args,
                check=False,
                capture_output=True,
                text=True,
                timeout=5,
            )
        except (OSError, subprocess.TimeoutExpired):
            continue
        text = (proc.stdout or "") + (proc.stderr or "")
        if proc.returncode != 0 and "unknown flag" in text:
            continue
        m = re.search(r"v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?", text)
        if m:
            return m.group(0)
    # Makefile -ldflags injects `git describe` (v1.0.0-3-gdeadbee[-dirty]);
    # that string is unique enough to grep out of a stripped Go binary.
    try:
        data = binary.read_bytes()
    except OSError:
        return None
    m = re.search(rb"v?\d+\.\d+\.\d+-\d+-g[0-9a-f]+(?:-dirty)?", data)
    if m:
        return m.group(0).decode("ascii")
    return None


def yaml_section(path: Path, section: str) -> dict[str, str]:
    """Tiny parser for this repo's flat `section:\\n  key: value` YAML."""
    if not path.is_file():
        raise InstallError(f"Config file not found: {path}")
    values: dict[str, str] = {}
    in_block = False
    header = f"{section}:"
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.rstrip()
        if line == header:
            in_block = True
            continue
        if in_block and line and not line[:1].isspace() and not line.lstrip().startswith("#"):
            in_block = False
        if not in_block:
            continue
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if ":" not in stripped:
            continue
        key, val = stripped.split(":", 1)
        values[key.strip()] = val.strip().strip("'\"")
    return values


def find_postgres_compose(root: Path = POSTGRES_COMPOSE_DIR) -> Path | None:
    for name in POSTGRES_COMPOSE_NAMES:
        path = root / name
        if path.is_file():
            return path
    return None


def _psql_direct(db: dict[str, str], sql: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["PGPASSWORD"] = db["pass"]
    return subprocess.run(
        [
            "psql",
            "-h",
            db["host"],
            "-p",
            db["port"],
            "-U",
            db["user"],
            "-d",
            db["database"],
            "-tAc",
            sql,
        ],
        check=False,
        capture_output=True,
        text=True,
        env=env,
        timeout=15,
    )


def _psql_via_compose(
    compose: Path, db: dict[str, str], sql: str
) -> subprocess.CompletedProcess[str]:
    # Connects to Postgres inside the compose `db` service, so this fails if
    # the server in factum2.yaml is a different host.
    return subprocess.run(
        [
            "docker",
            "compose",
            "-f",
            str(compose),
            "exec",
            "-T",
            "-e",
            f"PGPASSWORD={db['pass']}",
            "db",
            "psql",
            "-U",
            db["user"],
            "-d",
            db["database"],
            "-tAc",
            sql,
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=30,
    )


def _no_psql_fallback_error(compose: Path | None) -> str:
    names = ", ".join(POSTGRES_COMPOSE_NAMES)
    if compose is None:
        return (
            f"psql not found on PATH and no compose file in {POSTGRES_COMPOSE_DIR} "
            f"({names})"
        )
    return f"psql not found on PATH and docker is not available (compose file: {compose})"


def query_worker_addresses(
    config_path: Path,
    *,
    target_host: str = "localhost",
    ssh_user: str = "root",
) -> list[str]:
    db = yaml_section(config_path, "db")
    missing = [k for k in ("host", "port", "user", "pass", "database") if not db.get(k)]
    if missing:
        raise InstallError(f"db.{{{', '.join(missing)}}} missing from {config_path}")
    sql = "select address from worker_nodes where enabled = true;"
    if is_local_host(target_host):
        if which("psql"):
            proc = _psql_direct(db, sql)
        else:
            compose = find_postgres_compose()
            if compose is None or not which("docker"):
                raise InstallError(_no_psql_fallback_error(compose))
            log(
                f"==> psql not on PATH, using docker compose -f {compose} "
                "(fails if Postgres is on another host)"
            )
            proc = _psql_via_compose(compose, db, sql)
    else:
        # Credentials go as argv to bash -s, not interpolated into the remote script.
        names = " ".join(POSTGRES_COMPOSE_NAMES)
        remote = f"""\
set -euo pipefail
SQL='select address from worker_nodes where enabled = true;'
if command -v psql >/dev/null 2>&1; then
  export PGPASSWORD="$5"
  psql -h "$1" -p "$2" -U "$3" -d "$4" -tAc "$SQL"
  exit 0
fi
dir={POSTGRES_COMPOSE_DIR}
for name in {names}; do
  f="$dir/$name"
  if [ -f "$f" ]; then
    docker compose -f "$f" exec -T -e PGPASSWORD="$5" db \\
      psql -U "$3" -d "$4" -tAc "$SQL"
    exit 0
  fi
done
echo "psql not found and no compose file in $dir ({names})" >&2
exit 1
"""
        proc = subprocess.run(
            [
                "ssh",
                "-o",
                "BatchMode=yes",
                "-o",
                "ConnectTimeout=10",
                f"{ssh_user}@{target_host}",
                "bash",
                "-s",
                "--",
                db["host"],
                db["port"],
                db["user"],
                db["database"],
                db["pass"],
            ],
            check=False,
            capture_output=True,
            text=True,
            input=remote,
        )
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip()
        raise InstallError(f"worker_nodes lookup failed: {err}")
    return [line.strip() for line in proc.stdout.splitlines() if line.strip()]


def worker_hosts(addresses: Iterable[str], skip: Iterable[str] = ()) -> list[str]:
    skip_set = {h.lower() for h in skip}
    hosts: list[str] = []
    seen: set[str] = set()
    for addr in addresses:
        host = addr.rsplit("]", 1)[0] if addr.startswith("[") else addr.split(":", 1)[0]
        host = host.lstrip("[")
        if not host or is_local_host(host) or host.lower() in skip_set or host in seen:
            continue
        seen.add(host)
        hosts.append(host)
    return hosts


def remote_arch(host: str, ssh_user: str) -> str:
    proc = subprocess.run(
        ssh_cmd(ssh_user, host, "uname -m"),
        check=False,
        capture_output=True,
        text=True,
        timeout=15,
    )
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip()
        raise InstallError(f"{host}: uname -m failed: {err}")
    return uname_to_goarch(proc.stdout.strip())


def ssh_cmd(user: str, host: str, remote: str) -> list[str]:
    return [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "ConnectTimeout=10",
        f"{user}@{host}",
        remote,
    ]


# ---------------------------------------------------------------------------
# install
# ---------------------------------------------------------------------------


def need_sudo() -> bool:
    return os.geteuid() != 0


def sudo_prefix() -> list[str]:
    return ["sudo"] if need_sudo() else []


def ensure_sudo(dry_run: bool) -> None:
    if dry_run or not need_sudo():
        return
    log("==> Requesting sudo access")
    proc = subprocess.run(["sudo", "-v"], check=False)
    if proc.returncode != 0:
        raise InstallError("sudo is required to install into /opt/factum2 and restart systemd")


def run(
    cmd: list[str],
    dry_run: bool = False,
    check: bool = True,
    cwd: Path | None = None,
) -> subprocess.CompletedProcess[str]:
    printable = " ".join(_shell_quote(c) for c in cmd)
    if dry_run:
        log(f"    [dry-run] {printable}")
        return subprocess.CompletedProcess(cmd, 0, "", "")
    log(f"    $ {printable}")
    proc = subprocess.run(cmd, check=False, text=True, cwd=cwd)
    if check and proc.returncode != 0:
        raise InstallError(f"command failed ({proc.returncode}): {printable}")
    return proc


def _shell_quote(s: str) -> str:
    if re.fullmatch(r"[-A-Za-z0-9_./:=+@]+", s):
        return s
    return "'" + s.replace("'", "'\\''") + "'"


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def parse_checksums(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        digest, name = parts[0], parts[-1]
        name = name.lstrip("*")
        out[Path(name).name] = digest.lower()
    return out


def extract_archive(archive: Path, dest: Path) -> Path:
    dest.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:gz") as tar:
        try:
            tar.extractall(dest, filter="data")
        except TypeError:
            tar.extractall(dest)
    if _looks_like_release_root(dest):
        return dest
    subs = [p for p in dest.iterdir() if p.is_dir()]
    if len(subs) == 1 and _looks_like_release_root(subs[0]):
        return subs[0]
    raise InstallError(f"Could not find factum binaries in {archive.name}")


def _looks_like_release_root(path: Path) -> bool:
    return any(p.is_file() and p.name.startswith("factum") for p in path.iterdir())


def find_binaries(root: Path) -> list[Path]:
    found = []
    for p in sorted(root.iterdir()):
        if not p.is_file():
            continue
        if p.name.startswith("factum") and p.suffix == "":
            found.append(p)
    if not found:
        raise InstallError(f"No factum* binaries in {root}")
    return found


def write_version_file(
    install_dir: Path,
    tag: str,
    dry_run: bool,
    *,
    target_host: str = "localhost",
    ssh_user: str = "root",
) -> None:
    dest = install_dir / "VERSION"
    if dry_run:
        where = "this host" if is_local_host(target_host) else target_host
        log(f"    [dry-run] write {dest} <- {tag}  ({where})")
        return
    with tempfile.NamedTemporaryFile("w", delete=False, encoding="utf-8") as tmp:
        tmp.write(tag.strip() + "\n")
        tmp_path = tmp.name
    try:
        if is_local_host(target_host):
            run(
                sudo_prefix() + ["install", "-m", "644", tmp_path, str(dest)],
                dry_run=False,
            )
        else:
            run(
                [
                    "scp",
                    "-o",
                    "BatchMode=yes",
                    "-o",
                    "ConnectTimeout=10",
                    tmp_path,
                    f"{ssh_user}@{target_host}:{dest}",
                ],
                dry_run=False,
            )
    finally:
        os.unlink(tmp_path)


def _stage_binaries(binaries_dir: Path) -> Path:
    staging = Path(tempfile.mkdtemp(prefix="factum2-bins-"))
    for b in find_binaries(binaries_dir):
        dest = staging / b.name
        shutil.copy2(b, dest)
        dest.chmod(dest.stat().st_mode | 0o111)
    return staging


def normalize_unit_text(text: str) -> str:
    """Compare unit files ignoring trailing whitespace and a missing final newline."""
    lines = [line.rstrip() for line in text.replace("\r\n", "\n").split("\n")]
    return "\n".join(lines).strip() + "\n"


def read_installed_unit(
    unit: str,
    *,
    target_host: str,
    ssh_user: str,
) -> str | None:
    dest = SYSTEMD_DIR / unit
    if is_local_host(target_host):
        if dest.is_file():
            try:
                return dest.read_text(encoding="utf-8", errors="replace")
            except OSError:
                pass
        proc = subprocess.run(
            sudo_prefix() + ["cat", str(dest)],
            check=False,
            capture_output=True,
            text=True,
        )
        if proc.returncode != 0:
            return None
        return proc.stdout
    proc = subprocess.run(
        ssh_cmd(ssh_user, target_host, f"cat {_shell_quote(str(dest))}"),
        check=False,
        capture_output=True,
        text=True,
        timeout=15,
    )
    if proc.returncode != 0:
        return None
    return proc.stdout


def format_unit_diff(installed: str, packaged: str, unit: str) -> str:
    return "\n".join(
        difflib.unified_diff(
            installed.splitlines(),
            packaged.splitlines(),
            fromfile=f"installed {unit}",
            tofile=f"packaged {unit}",
            lineterm="",
        )
    )


def confirm_overwrite_unit(
    unit: str,
    where: str,
    diff: str,
    assume_yes: bool,
) -> bool:
    log(f"==> {unit} on {where} differs from this release:")
    if diff:
        for line in diff.splitlines():
            log(f"    {line}")
    else:
        log("    (no textual diff; whitespace-only difference)")
    if assume_yes:
        log(f"    --yes: overwriting {unit} on {where}")
        return True
    if not sys.stdin.isatty():
        log(f"!!  leaving {unit} on {where} unchanged (no TTY; pass --yes to overwrite)")
        return False
    try:
        ans = input(f"Overwrite {unit} on {where}? [y/N] ").strip().lower()
    except EOFError:
        return False
    return ans in {"y", "yes"}


def copy_unit_file(
    src: Path,
    unit: str,
    *,
    target_host: str,
    ssh_user: str,
    dry_run: bool,
) -> None:
    dest = SYSTEMD_DIR / unit
    if is_local_host(target_host):
        run(sudo_prefix() + ["cp", str(src), str(dest)], dry_run=dry_run)
        return
    run(
        [
            "scp",
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=10",
            str(src),
            f"{ssh_user}@{target_host}:{dest}",
        ],
        dry_run=dry_run,
    )


def install_unit(
    src: Path,
    unit: str,
    *,
    target_host: str,
    ssh_user: str,
    dry_run: bool,
    assume_yes: bool,
) -> str:
    """Install or update a systemd unit.

    Returns one of: "installed", "updated", "unchanged", "kept".
    """
    if not src.is_file():
        raise InstallError(f"Missing systemd unit {src}")
    where = "this host" if is_local_host(target_host) else target_host
    packaged = src.read_text(encoding="utf-8", errors="replace")
    installed = read_installed_unit(unit, target_host=target_host, ssh_user=ssh_user)
    if installed is None:
        log(f"==> Installing {unit} on {where}")
        copy_unit_file(
            src, unit, target_host=target_host, ssh_user=ssh_user, dry_run=dry_run
        )
        return "installed"
    if normalize_unit_text(installed) == normalize_unit_text(packaged):
        log(f"    {unit} on {where} already matches this release")
        return "unchanged"
    diff = format_unit_diff(installed, packaged, unit)
    if dry_run:
        log(f"==> [dry-run] {unit} on {where} differs from this release")
        for line in diff.splitlines():
            log(f"    {line}")
        log(f"    [dry-run] would prompt to overwrite {unit} on {where}")
        return "updated"
    if not confirm_overwrite_unit(unit, where, diff, assume_yes):
        log(f"    keeping existing {unit} on {where}")
        return "kept"
    log(f"==> Overwriting {unit} on {where}")
    copy_unit_file(
        src, unit, target_host=target_host, ssh_user=ssh_user, dry_run=False
    )
    return "updated"


def systemd_reload_enable_restart(
    units: Sequence[str],
    *,
    enable_units: Sequence[str] = (),
    target_host: str,
    ssh_user: str,
    dry_run: bool,
) -> None:
    where = "this host" if is_local_host(target_host) else target_host
    if is_local_host(target_host):
        run(sudo_prefix() + ["systemctl", "daemon-reload"], dry_run=dry_run)
        for unit in enable_units:
            log(f"==> Enabling {unit}")
            run(sudo_prefix() + ["systemctl", "enable", "--now", unit], dry_run=dry_run)
        for unit in units:
            if unit in enable_units:
                continue
            log(f"==> Restarting {unit}")
            run(sudo_prefix() + ["systemctl", "restart", unit], dry_run=dry_run)
        return
    parts = ["systemctl daemon-reload"]
    for unit in enable_units:
        parts.append(f"systemctl enable --now {unit}")
    for unit in units:
        if unit in enable_units:
            continue
        parts.append(f"systemctl restart {unit}")
    log(f"==> Reloading systemd on {where} ({', '.join(units)})")
    run(ssh_cmd(ssh_user, target_host, " && ".join(parts)), dry_run=dry_run)


def install_primary(
    binaries_dir: Path,
    examples_dir: Path,
    install_dir: Path,
    tag: str,
    dry_run: bool,
    *,
    target_host: str = "localhost",
    ssh_user: str = "root",
    assume_yes: bool = False,
) -> None:
    binaries = find_binaries(binaries_dir)
    where = "this host" if is_local_host(target_host) else target_host
    log(f"==> Installing {len(binaries)} binaries to {install_dir} on {where}")
    if dry_run:
        for b in binaries:
            log(f"    [dry-run] {b.name} -> {install_dir / b.name}")
        write_version_file(
            install_dir, tag, dry_run=True, target_host=target_host, ssh_user=ssh_user
        )
    elif is_local_host(target_host):
        run(sudo_prefix() + ["mkdir", "-p", str(install_dir)], dry_run=False)
        staging = _stage_binaries(binaries_dir)
        try:
            run(
                sudo_prefix()
                + ["rsync", "-a", "-c", "--", str(staging) + "/", str(install_dir) + "/"],
                dry_run=False,
            )
        finally:
            shutil.rmtree(staging, ignore_errors=True)
        write_version_file(install_dir, tag, dry_run=False)
    else:
        run(ssh_cmd(ssh_user, target_host, f"mkdir -p {install_dir}"), dry_run=False)
        staging = _stage_binaries(binaries_dir)
        try:
            run(
                [
                    "rsync",
                    "-a",
                    "-c",
                    "-e",
                    "ssh -o BatchMode=yes -o ConnectTimeout=10",
                    "--",
                    str(staging) + "/",
                    f"{ssh_user}@{target_host}:{install_dir}/",
                ],
                dry_run=False,
            )
        finally:
            shutil.rmtree(staging, ignore_errors=True)
        write_version_file(
            install_dir, tag, dry_run=False, target_host=target_host, ssh_user=ssh_user
        )

    log("==> Installing systemd units")
    newly: list[str] = []
    for unit in PRIMARY_UNITS:
        action = install_unit(
            examples_dir / unit,
            unit,
            target_host=target_host,
            ssh_user=ssh_user,
            dry_run=dry_run,
            assume_yes=assume_yes,
        )
        if action == "installed":
            newly.append(unit)
    systemd_reload_enable_restart(
        PRIMARY_UNITS,
        enable_units=newly,
        target_host=target_host,
        ssh_user=ssh_user,
        dry_run=dry_run,
    )


def install_worker(
    host: str,
    ssh_user: str,
    binaries_dir: Path,
    examples_dir: Path,
    install_dir: Path,
    dry_run: bool,
    *,
    assume_yes: bool = False,
) -> None:
    binaries = find_binaries(binaries_dir)
    log(f"==> Updating remote worker {host} ({len(binaries)} binaries)")
    run(ssh_cmd(ssh_user, host, f"mkdir -p {install_dir}"), dry_run=dry_run)

    staging = _stage_binaries(binaries_dir) if not dry_run else None
    try:
        src = (str(staging) + "/") if staging else str(binaries_dir) + "/"
        run(
            [
                "rsync",
                "-a",
                "-c",
                "-e",
                "ssh -o BatchMode=yes -o ConnectTimeout=10",
                "--",
                src,
                f"{ssh_user}@{host}:{install_dir}/",
            ],
            dry_run=dry_run,
        )
    finally:
        if staging is not None:
            shutil.rmtree(staging, ignore_errors=True)

    action = install_unit(
        examples_dir / WORKER_UNIT,
        WORKER_UNIT,
        target_host=host,
        ssh_user=ssh_user,
        dry_run=dry_run,
        assume_yes=assume_yes,
    )

    if "icinga" in host:
        tpl = examples_dir / "icinga-notification-email.tpl"
        if tpl.is_file():
            log(f"    copying icinga-notification-email.tpl example to {host}")
            run(
                [
                    "scp",
                    "-o",
                    "BatchMode=yes",
                    str(tpl),
                    f"{ssh_user}@{host}:/etc/factum2/icinga-notification-email-example.tpl",
                ],
                dry_run=dry_run,
            )

    systemd_reload_enable_restart(
        (WORKER_UNIT,),
        enable_units=(WORKER_UNIT,) if action == "installed" else (),
        target_host=host,
        ssh_user=ssh_user,
        dry_run=dry_run,
    )


def prepare_roots(
    client: GithubClient,
    release: Release,
    archs: set[str],
) -> tuple[Path, dict[str, Path]]:
    """Download, verify, and extract one archive per arch. Caller deletes work."""
    checksum_asset = release.checksums()
    checksums: dict[str, str] = {}
    work = Path(tempfile.mkdtemp(prefix="factum2-rel-"))
    cache = cache_dir()
    cache.mkdir(parents=True, exist_ok=True)

    if checksum_asset:
        csum_path = cache / checksum_asset.name
        log(f"==> Downloading {checksum_asset.name}")
        client.download(checksum_asset, csum_path)
        checksums = parse_checksums(csum_path.read_text(encoding="utf-8", errors="replace"))

    roots: dict[str, Path] = {}
    for arch in sorted(archs):
        asset = release.archive(ARCHIVE_OS, arch)
        if asset is None:
            raise InstallError(
                f"Release {release.tag} has no {ARCHIVE_OS}/{arch} tar.gz asset"
            )
        archive = cache / asset.name
        need = True
        if archive.is_file() and checksums.get(asset.name):
            if sha256_file(archive) == checksums[asset.name]:
                log(f"==> Using cached {asset.name}")
                need = False
        if need:
            log(f"==> Downloading {asset.name} ({_fmt_bytes(asset.size)})")
            client.download(asset, archive)
        expected = checksums.get(asset.name)
        if expected:
            got = sha256_file(archive)
            if got != expected:
                archive.unlink(missing_ok=True)
                raise InstallError(
                    f"Checksum mismatch for {asset.name}: expected {expected}, got {got}"
                )
            log(f"    checksum ok ({got[:12]}…)")
        extract_to = work / arch
        log(f"==> Extracting {asset.name}")
        roots[arch] = extract_archive(archive, extract_to)
        missing = [name for name in KNOWN_BINARIES if not (roots[arch] / name).is_file()]
        if missing:
            log(f"    warning: {asset.name} is missing {', '.join(missing)}")
    return work, roots


# ---------------------------------------------------------------------------
# TUI / menus
# ---------------------------------------------------------------------------


def release_status(rel: Release, installed: str, latest_tag: str | None) -> str:
    bits: list[str] = []
    if latest_tag and rel.tag == latest_tag:
        bits.append("latest")
    if versions_equal(rel.tag, installed):
        bits.append("current")
    elif same_base(rel.tag, installed) and is_dev_build(installed):
        bits.append("local")
    elif version_newer(rel.tag, installed):
        bits.append("newer")
    elif parse_version(installed) is not None:
        bits.append("older")
    if rel.prerelease:
        bits.append("pre")
    return "  ".join(bits)


def latest_stable(releases: list[Release], include_pre: bool = False) -> Release | None:
    for rel in releases:
        if include_pre or not rel.prerelease:
            return rel
    return releases[0] if releases else None


def default_index(releases: list[Release], installed: str) -> int:
    for i, rel in enumerate(releases):
        if version_newer(rel.tag, installed) and not rel.prerelease:
            return i
    for i, rel in enumerate(releases):
        if versions_equal(rel.tag, installed):
            return i
    return 0


def print_release_table(
    releases: list[Release],
    installed: str,
    goarch: str,
    selected: int | None = None,
) -> None:
    latest = latest_stable(releases)
    latest_tag = latest.tag if latest else None
    log(f"Installed : {installed}")
    log(f"Latest    : {latest_tag or '(none)'}")
    log(f"Arch      : {ARCHIVE_OS}/{goarch}")
    log("")
    header = f"{'#':>3} {'TAG':<16} {'DATE':<12} {'ARCH':<10} NOTES"
    log(header)
    log("-" * len(header))
    for i, rel in enumerate(releases):
        mark = ">" if selected == i else f"{i + 1}"
        asset = rel.archive(ARCHIVE_OS, goarch)
        arch_ok = "yes" if asset else "MISSING"
        log(
            f"{mark:>3} {rel.tag:<16} {format_date(rel.published_at):<12} {arch_ok:<10} "
            f"{release_status(rel, installed, latest_tag)}"
        )


def numbered_select(releases: list[Release], installed: str, goarch: str) -> Release | None:
    print_release_table(releases, installed, goarch)
    log("")
    log("Enter a number (1 is newest), tag, or q to quit.")
    while True:
        try:
            raw = input("Select: ").strip()
        except EOFError:
            return None
        if not raw or raw.lower() in {"q", "quit"}:
            return None
        if raw.isdigit():
            n = int(raw)
            if 1 <= n <= len(releases):
                return releases[n - 1]
            log(f"  pick 1–{len(releases)}")
            continue
        for rel in releases:
            if raw in {rel.tag, strip_v(rel.tag), f"v{strip_v(rel.tag)}"}:
                return rel
        log("  unknown selection")


def curses_select(
    releases: list[Release],
    installed: str,
    goarch: str,
    workers: list[str],
    worker_err: str | None = None,
) -> Release | None:
    def _draw(stdscr):
        curses.curs_set(0)
        curses.use_default_colors()
        if curses.has_colors():
            curses.init_pair(1, curses.COLOR_CYAN, -1)
            curses.init_pair(2, curses.COLOR_GREEN, -1)
            curses.init_pair(3, curses.COLOR_YELLOW, -1)
            curses.init_pair(4, curses.COLOR_MAGENTA, -1)
            curses.init_pair(5, curses.COLOR_BLACK, curses.COLOR_CYAN)
            curses.init_pair(6, curses.COLOR_RED, -1)
        latest = latest_stable(releases)
        latest_tag = latest.tag if latest else None
        newer_n = sum(1 for r in releases if version_newer(r.tag, installed) and not r.prerelease)
        idx = default_index(releases, installed)
        offset = 0

        while True:
            stdscr.clear()
            h, w = stdscr.getmaxyx()
            title = " factum2 installer "
            _add(stdscr, 0, 0, title.center(max(w - 1, 0)), curses.A_BOLD | _pair(1))
            installed_line = f"  Installed {installed}   Latest {latest_tag or '-'}   {ARCHIVE_OS}/{goarch}"
            _add(stdscr, 1, 0, installed_line[: w - 1])
            if newer_n:
                note = f"  {newer_n} newer release(s) available — select one to install."
                _add(stdscr, 2, 0, note[: w - 1], _pair(3))
            elif versions_equal(latest_tag or "", installed):
                _add(stdscr, 2, 0, "  Up to date. Select a release to reinstall or roll back.", _pair(2))
            elif latest_tag and same_base(latest_tag, installed) and is_dev_build(installed):
                _add(
                    stdscr,
                    2,
                    0,
                    f"  Local build of {latest_tag}. Select a release to replace it.",
                    _pair(3),
                )
            else:
                _add(stdscr, 2, 0, "  Select a release to install.", _pair(2))

            if worker_err:
                worker_note = f"  Remote workers: unavailable ({worker_err})"
                _add(stdscr, 3, 0, worker_note[: w - 1], _pair(6))
            elif workers:
                _add(stdscr, 3, 0, f"  Remote workers: {', '.join(workers)}"[: w - 1])
            else:
                _add(stdscr, 3, 0, "  Remote workers: (none)")

            header = f"  {'TAG':<16} {'DATE':<12} {'NOTES'}"
            _add(stdscr, 5, 0, header[: w - 1], curses.A_UNDERLINE)
            list_top = 6
            list_h = max(1, h - list_top - 3)
            if idx < offset:
                offset = idx
            if idx >= offset + list_h:
                offset = idx - list_h + 1

            for row, rel in enumerate(releases[offset : offset + list_h]):
                real = offset + row
                status = release_status(rel, installed, latest_tag)
                missing = rel.archive(ARCHIVE_OS, goarch) is None
                line = f"  {rel.tag:<16} {format_date(rel.published_at):<12} {status}"
                if missing:
                    line += "  (no asset)"
                attr = curses.A_NORMAL
                if missing:
                    attr |= _pair(6)
                elif versions_equal(rel.tag, installed):
                    attr |= _pair(2)
                elif version_newer(rel.tag, installed):
                    attr |= _pair(3)
                if rel.prerelease:
                    attr |= _pair(4)
                if real == idx:
                    attr = curses.A_REVERSE | curses.A_BOLD
                    if curses.has_colors():
                        attr = curses.color_pair(5) | curses.A_BOLD
                    line = ">" + line[1:]
                _add(stdscr, list_top + row, 0, line[: w - 1], attr)

            _add(
                stdscr,
                h - 2,
                0,
                "  ↑/↓ move   Enter install   q quit"[: w - 1],
                curses.A_DIM,
            )
            stdscr.refresh()

            key = stdscr.getch()
            if key in (ord("q"), ord("Q"), 27):
                return None
            if key in (curses.KEY_UP, ord("k")):
                idx = max(0, idx - 1)
            elif key in (curses.KEY_DOWN, ord("j")):
                idx = min(len(releases) - 1, idx + 1)
            elif key in (curses.KEY_PPAGE,):
                idx = max(0, idx - list_h)
            elif key in (curses.KEY_NPAGE,):
                idx = min(len(releases) - 1, idx + list_h)
            elif key in (curses.KEY_HOME, ord("g")):
                idx = 0
            elif key in (curses.KEY_END, ord("G")):
                idx = len(releases) - 1
            elif key in (curses.KEY_ENTER, 10, 13):
                chosen = releases[idx]
                if chosen.archive(ARCHIVE_OS, goarch) is None:
                    curses.flash()
                    continue
                if not _curses_confirm(stdscr, chosen, installed, workers):
                    continue
                return chosen

    return curses.wrapper(_draw)


def _pair(n: int) -> int:
    return curses.color_pair(n) if curses.has_colors() else 0


def _add(win, y: int, x: int, text: str, attr: int = 0) -> None:
    h, w = win.getmaxyx()
    if y < 0 or y >= h or x >= w:
        return
    try:
        win.addnstr(y, x, text, max(0, w - x - 1), attr)
    except curses.error:
        pass


def _curses_confirm(
    stdscr,
    rel: Release,
    installed: str,
    workers: list[str],
) -> bool:
    h, w = stdscr.getmaxyx()
    lines = [
        f"Install {rel.tag}  ({format_date(rel.published_at)})",
        f"currently {installed}",
        f"primary  {os.uname().nodename}  ({', '.join(PRIMARY_UNITS)})",
        f"workers  {', '.join(workers) if workers else '(none)'}",
        "",
        "Enter confirm    Esc cancel",
    ]
    box_w = min(w - 4, max(len(s) for s in lines) + 4)
    box_h = len(lines) + 2
    y0 = max(0, (h - box_h) // 2)
    x0 = max(0, (w - box_w) // 2)
    try:
        panel = stdscr.derwin(box_h, box_w, y0, x0)
    except curses.error:
        return True
    panel.clear()
    panel.box()
    for i, line in enumerate(lines):
        attr = curses.A_BOLD if i == 0 else 0
        _add(panel, i + 1, 2, line[: box_w - 3], attr)
    panel.refresh()
    while True:
        key = panel.getch()
        if key in (curses.KEY_ENTER, 10, 13, ord("y"), ord("Y")):
            return True
        if key in (27, ord("n"), ord("N"), ord("q"), ord("Q")):
            return False


def confirm_text(rel: Release, installed: str, workers: list[str], assume_yes: bool) -> bool:
    if assume_yes:
        return True
    if not sys.stdin.isatty():
        raise InstallError("Refusing to install without a TTY; pass --yes")
    log("")
    log(f"Install {rel.tag} ({format_date(rel.published_at)})")
    log(f"  currently : {installed}")
    log(f"  primary   : this host ({', '.join(PRIMARY_UNITS)})")
    log(f"  workers   : {', '.join(workers) if workers else '(none)'}")
    try:
        ans = input("Proceed? [y/N] ").strip().lower()
    except EOFError:
        return False
    return ans in {"y", "yes"}


# ---------------------------------------------------------------------------
# self-update (install.py from GitHub)
# ---------------------------------------------------------------------------


_INSTALLER_VERSION_RE = re.compile(r"^INSTALLER_VERSION\s*=\s*(\d+)\s*$", re.M)


def installer_version_of(text: str) -> int:
    m = _INSTALLER_VERSION_RE.search(text)
    return int(m.group(1)) if m else 0


def looks_like_installer(text: str) -> bool:
    if "def main(" not in text or "factum2" not in text:
        return False
    try:
        compile(text, INSTALLER_FILENAME, "exec")
    except SyntaxError:
        return False
    return True


def in_git_checkout() -> bool:
    return (REPO_DIR / ".git").exists()


def confirm_self_update(local_ver: int, remote_ver: int, repo: str, assume_yes: bool) -> bool:
    if assume_yes:
        return True
    if not sys.stdin.isatty():
        log(
            f"==> A newer {INSTALLER_FILENAME} is on GitHub "
            f"({repo}: {remote_ver}, this copy: {local_ver}). "
            "Re-run on a TTY, or pass --self-update / --yes."
        )
        return False
    log("")
    log(f"A newer {INSTALLER_FILENAME} is available on GitHub ({repo}).")
    log(f"  this copy : {local_ver}")
    log(f"  GitHub    : {remote_ver}")
    try:
        ans = input("Update this installer and re-run? [Y/n] ").strip().lower()
    except EOFError:
        return False
    return ans in {"", "y", "yes"}


def replace_installer(new_bytes: bytes) -> None:
    path = Path(__file__).resolve()
    mode = path.stat().st_mode
    fd, tmp_name = tempfile.mkstemp(prefix=".install.py.", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as tmp:
            tmp.write(new_bytes)
            tmp.flush()
            os.fsync(tmp.fileno())
        os.chmod(tmp_name, mode)
        os.replace(tmp_name, path)
    except Exception:
        try:
            os.unlink(tmp_name)
        except OSError:
            pass
        raise


def reexec_self() -> None:
    env = os.environ.copy()
    env[SELF_UPDATED_ENV] = "1"
    os.execve(sys.executable, [sys.executable, *sys.argv], env)


def maybe_self_update(args: argparse.Namespace) -> None:
    """Replace this script from GitHub if a newer copy exists. May os.execve."""
    if args.skip_self_update:
        return
    if os.environ.get(SELF_UPDATED_ENV):
        return
    if in_git_checkout() and not args.self_update:
        return

    token = github_token()
    client = GithubClient(args.repo, token)
    try:
        remote = client.fetch_file(INSTALLER_FILENAME)
    except (InstallError, OSError, json.JSONDecodeError, ValueError) as exc:
        log(f"!!  Could not check for a newer {INSTALLER_FILENAME}: {exc}")
        return

    try:
        remote_text = remote.decode("utf-8")
    except UnicodeDecodeError:
        log(f"!!  GitHub {INSTALLER_FILENAME} is not valid UTF-8; leaving this copy")
        return
    if not looks_like_installer(remote_text):
        log(f"!!  GitHub {INSTALLER_FILENAME} does not look like this installer; leaving this copy")
        return

    local_path = Path(__file__).resolve()
    try:
        local = local_path.read_bytes()
    except OSError as exc:
        log(f"!!  Could not read {local_path}: {exc}")
        return
    local_text = local.decode("utf-8", errors="replace")
    local_ver = installer_version_of(local_text)
    remote_ver = installer_version_of(remote_text)
    if remote_ver < local_ver:
        if args.self_update:
            log(
                f"==> This {INSTALLER_FILENAME} (version {local_ver}) is newer than "
                f"GitHub ({remote_ver}); not downgrading"
            )
        return
    if remote == local:
        if args.self_update:
            log(f"==> {INSTALLER_FILENAME} is already the GitHub copy (version {local_ver})")
        return

    if remote_ver > local_ver:
        log(
            f"==> Newer {INSTALLER_FILENAME} on GitHub "
            f"(this copy {local_ver}, GitHub {remote_ver})"
        )
    else:
        log(
            f"==> GitHub has an updated {INSTALLER_FILENAME} "
            f"(version {remote_ver}, content differs from this copy)"
        )
    if args.dry_run:
        log(f"==> Dry run: would replace {local_path} and re-run")
        return
    if not confirm_self_update(local_ver, remote_ver, args.repo, assume_yes=args.yes or args.self_update):
        return
    try:
        replace_installer(remote)
    except OSError as exc:
        log(f"!!  Could not replace {local_path}: {exc}")
        return
    log(f"==> Updated {local_path} ({local_ver} -> {remote_ver})")
    standalone = args.self_update and not args.list and args.install is None
    if standalone:
        return
    log("==> Re-running with the new installer")
    reexec_self()
    raise InstallError("failed to re-exec updated installer")


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def pick_release(releases: list[Release], spec: str, include_pre: bool = False) -> Release:
    if spec in {"latest", "stable"}:
        rel = latest_stable(releases, include_pre=include_pre)
        if rel is None:
            raise InstallError("No stable releases found")
        return rel
    for rel in releases:
        if spec in {rel.tag, strip_v(rel.tag), f"v{strip_v(rel.tag)}"}:
            return rel
    raise InstallError(f"Release {spec} not in the fetched list (try --list)")


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Install factum2 on this primary (or --source HOST) and its remote workers.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Modes:
  (default)   GitHub release — production. TUI, --list, or --install TAG.
  --source    This source tree — development. make release, then install
              build/ (replaces install_prod.sh).

Environment:
  GITHUB_TOKEN / GH_TOKEN   token for private repos (else `gh auth token`)
  SSH_USER                  ssh user for a remote primary and workers (default: root)

Examples:
  /etc/factum2/install.py
  /etc/factum2/install.py --list
  /etc/factum2/install.py --install latest --yes
  ./install.py --install v1.0.0 --dry-run
  ./install.py --self-update
  ./install.py --source
  ./install.py --source lab-primary --dry-run
  ./install.py --source --skip-build --primary-only
""",
    )
    p.add_argument("--repo", default=os.environ.get("GITHUB_REPO", REPO_DEFAULT), help=f"owner/name (default {REPO_DEFAULT})")
    p.add_argument("--install-dir", default=INSTALL_DIR_DEFAULT, type=Path)
    p.add_argument("--config", default=CONFIG_PATH_DEFAULT, type=Path, help="factum2.yaml (db credentials for worker_nodes lookup)")
    p.add_argument("--ssh-user", default=os.environ.get("SSH_USER", "root"))
    p.add_argument("--list", action="store_true", help="print GitHub releases and exit")
    p.add_argument("--install", metavar="TAG", help="GitHub tag to install, or 'latest'")
    p.add_argument("--pre", action="store_true", help="include prereleases when resolving 'latest'")
    p.add_argument(
        "--yes",
        "-y",
        action="store_true",
        help="do not prompt for confirmation (including overwriting modified systemd units)",
    )
    p.add_argument("--dry-run", action="store_true", help="print what would happen, change nothing")
    p.add_argument("--primary-only", action="store_true", help="do not update remote workers")
    p.add_argument("--limit", type=int, default=50, help="max GitHub releases to fetch")
    p.add_argument(
        "--source",
        nargs="?",
        const="localhost",
        metavar="HOST",
        help="install from this source tree onto HOST (default localhost)",
    )
    p.add_argument(
        "--skip-build",
        action="store_true",
        help="with --source, use existing build/ instead of running make release",
    )
    p.add_argument(
        "--self-update",
        action="store_true",
        help="replace this script from GitHub if a newer copy exists, then exit "
        "(or continue when combined with --list / --install)",
    )
    p.add_argument(
        "--skip-self-update",
        action="store_true",
        help="do not check GitHub for a newer install.py",
    )
    return p.parse_args(argv)


def git_describe(repo_dir: Path) -> str:
    proc = subprocess.run(
        ["git", "describe", "--tags", "--always", "--dirty"],
        cwd=repo_dir,
        check=False,
        capture_output=True,
        text=True,
    )
    text = (proc.stdout or "").strip()
    return text if proc.returncode == 0 and text else "dev"


def fetch_config(config_path: Path, target_host: str, ssh_user: str) -> tuple[Path, Path | None]:
    """Return (local yaml path, tempfile to delete)."""
    if is_local_host(target_host):
        if not config_path.is_file():
            raise InstallError(f"Config file not found: {config_path}")
        return config_path, None
    probe = subprocess.run(
        ssh_cmd(ssh_user, target_host, f"test -f {config_path}"),
        check=False,
        capture_output=True,
        text=True,
    )
    if probe.returncode != 0:
        raise InstallError(f"Config file not found on {target_host}: {config_path}")
    fd, name = tempfile.mkstemp(prefix="factum2-yaml-")
    os.close(fd)
    tmp = Path(name)
    run(
        [
            "scp",
            "-o",
            "BatchMode=yes",
            "-q",
            f"{ssh_user}@{target_host}:{config_path}",
            str(tmp),
        ],
        dry_run=False,
    )
    return tmp, tmp


def main_source(args: argparse.Namespace) -> int:
    target_host: str = args.source
    install_dir: Path = args.install_dir
    repo_dir = REPO_DIR
    build_dir = repo_dir / "build"
    examples_dir = repo_dir / "examples"
    version = git_describe(repo_dir)

    log(f"==> factum2  source-tree install  version={version}")
    log(f"    REPO_DIR    = {repo_dir}")
    log(f"    BUILD_DIR   = {build_dir}")
    log(f"    INSTALL_DIR = {install_dir}")
    log(f"    CONFIG      = {args.config}")
    log(f"    TARGET_HOST = {target_host}")
    log(f"    SSH_USER    = {args.ssh_user}")

    config_file, tmp_config = fetch_config(args.config, target_host, args.ssh_user)
    workers: list[str] = []
    worker_err: str | None = None
    try:
        db = yaml_section(config_file, "db")
        log(f"    db          = {db.get('user')}@{db.get('host')}/{db.get('database')}")
        if not args.primary_only:
            log(f"==> Looking up enabled remote worker nodes ({db.get('database')}@{db.get('host')})")
            try:
                addresses = query_worker_addresses(
                    config_file,
                    target_host=target_host,
                    ssh_user=args.ssh_user,
                )
                workers = worker_hosts(addresses, skip=[target_host])
            except InstallError as exc:
                worker_err = str(exc)
                log(f"!!  Could not list worker nodes: {exc}")
    finally:
        if tmp_config is not None:
            tmp_config.unlink(missing_ok=True)

    if workers:
        log("==> Enabled remote workers: " + ", ".join(workers))
    elif args.primary_only:
        log("==> Skipping remote workers (--primary-only)")
    elif worker_err:
        log("==> Continuing without remote workers")
    else:
        log("==> Enabled remote workers: (none)")

    if args.skip_build:
        if not build_dir.is_dir() and not args.dry_run:
            raise InstallError(f"--skip-build given but {build_dir} does not exist")
        log(f"==> Skipping build, using {build_dir}")
    else:
        log("==> Building release (make release)")
        run(["make", "release"], dry_run=args.dry_run, cwd=repo_dir)

    if not args.dry_run:
        missing = [name for name in KNOWN_BINARIES if not (build_dir / name).is_file()]
        if missing:
            raise InstallError(f"{build_dir} is missing {', '.join(missing)}")

    if is_local_host(target_host):
        ensure_sudo(args.dry_run)

    if args.dry_run:
        log(f"==> Would install {version} on {target_host} and restart {', '.join(PRIMARY_UNITS)}")
        for host in workers:
            log(f"==> Would update {host} and restart {WORKER_UNIT}")
        return 0

    install_primary(
        build_dir,
        examples_dir,
        install_dir,
        version,
        dry_run=False,
        target_host=target_host,
        ssh_user=args.ssh_user,
        assume_yes=args.yes,
    )
    failures: list[str] = []
    for host in workers:
        try:
            install_worker(
                host,
                args.ssh_user,
                build_dir,
                examples_dir,
                install_dir,
                dry_run=False,
                assume_yes=args.yes,
            )
        except InstallError as exc:
            log(f"!!  {host}: {exc}")
            failures.append(host)
    log("==> Done")
    if is_local_host(target_host):
        log(f"    primary now at {read_installed_version(install_dir)}")
    else:
        log(f"    primary {target_host} now at {version}")
    if failures:
        raise InstallError("Failed updating workers: " + ", ".join(failures))
    return 0


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv if argv is not None else sys.argv[1:])
    if args.skip_build and args.source is None:
        raise InstallError("--skip-build requires --source")
    if args.self_update and args.skip_self_update:
        raise InstallError("--self-update cannot be combined with --skip-self-update")
    if args.source is not None:
        if args.list or args.install:
            raise InstallError("--source cannot be combined with --list / --install")
        if args.self_update:
            raise InstallError("--source cannot be combined with --self-update")
        return main_source(args)
    maybe_self_update(args)
    if args.self_update and not args.list and args.install is None:
        return 0
    return main_release(args)


def main_release(args: argparse.Namespace) -> int:
    goarch = local_arch()
    install_dir: Path = args.install_dir
    installed = read_installed_version(install_dir)

    log(f"==> factum2  installed={installed}  arch={ARCHIVE_OS}/{goarch}  repo={args.repo}")
    token = github_token()
    if not token:
        log("==> No GitHub token (GITHUB_TOKEN / gh auth); public download only")
    client = GithubClient(args.repo, token)

    log("==> Fetching releases")
    releases = client.list_releases(limit=args.limit)
    if not releases:
        raise InstallError("No GitHub releases found")

    newer = [
        r
        for r in releases
        if version_newer(r.tag, installed) and (args.pre or not r.prerelease)
    ]

    workers: list[str] = []
    worker_err: str | None = None
    if not args.primary_only:
        try:
            addresses = query_worker_addresses(args.config)
            workers = worker_hosts(addresses)
        except InstallError as exc:
            worker_err = str(exc)
            log(f"!!  Could not list worker nodes: {exc}")

    if args.list:
        print_release_table(releases, installed, goarch)
        if workers:
            log("")
            log("Enabled remote workers: " + ", ".join(workers))
        elif worker_err:
            log("")
            log(f"Workers unavailable: {worker_err}")
        if newer:
            log("")
            log(f"{len(newer)} newer release(s) than {installed}.")
            return 2
        return 0

    selected: Release | None
    if args.install:
        selected = pick_release(releases, args.install, include_pre=args.pre)
        if selected.prerelease and args.install in {"latest", "stable"} and not args.pre:
            raise InstallError("Refusing to auto-pick a prerelease; pass --pre")
        print_release_table(releases, installed, goarch, selected=releases.index(selected))
        if not confirm_text(selected, installed, workers, assume_yes=args.yes or args.dry_run):
            log("Aborted.")
            return 1
    elif sys.stdin.isatty() and sys.stdout.isatty():
        try:
            selected = curses_select(releases, installed, goarch, workers, worker_err)
        except curses.error:
            selected = numbered_select(releases, installed, goarch)
        if selected is None:
            log("Aborted.")
            return 1
        if not sys.stdout.isatty():
            pass
    else:
        print_release_table(releases, installed, goarch)
        if newer:
            log("")
            log("Newer releases available. Re-run on a TTY, or pass --install TAG.")
            return 2
        log("Already up to date.")
        return 0

    asset = selected.archive(ARCHIVE_OS, goarch)
    if asset is None:
        raise InstallError(f"{selected.tag} has no {ARCHIVE_OS}/{goarch} tar.gz")

    log(f"==> Selected {selected.tag} ({format_date(selected.published_at)})")
    if workers:
        log("==> Enabled remote workers: " + ", ".join(workers))
    elif args.primary_only:
        log("==> Skipping remote workers (--primary-only)")
    elif worker_err:
        log("==> Continuing without remote workers")

    ensure_sudo(args.dry_run)

    archs = {goarch}
    remote_archs: dict[str, str] = {}
    if workers and not args.primary_only:
        for host in workers:
            try:
                arch = goarch if args.dry_run else remote_arch(host, args.ssh_user)
            except InstallError as exc:
                log(f"!!  {exc}")
                arch = goarch
            remote_archs[host] = arch
            archs.add(arch)
            if arch != goarch:
                log(f"    {host} is {arch} (primary is {goarch})")

    if args.dry_run:
        log("==> Dry run: would download " + ", ".join(sorted(archs)))
        log(f"==> Would install {selected.tag} on this host and restart {', '.join(PRIMARY_UNITS)}")
        for host in workers:
            log(f"==> Would update {host} ({remote_archs.get(host, goarch)}) and restart {WORKER_UNIT}")
        return 0

    work, roots = prepare_roots(client, selected, archs)
    try:
        primary_root = roots[goarch]
        install_primary(
            primary_root,
            primary_root / "examples",
            install_dir,
            selected.tag,
            dry_run=False,
            assume_yes=args.yes,
        )
        failures: list[str] = []
        for host in workers:
            arch = remote_archs.get(host, goarch)
            try:
                install_worker(
                    host,
                    args.ssh_user,
                    roots[arch],
                    roots[arch] / "examples",
                    install_dir,
                    dry_run=False,
                    assume_yes=args.yes,
                )
            except InstallError as exc:
                log(f"!!  {host}: {exc}")
                failures.append(host)
        new_ver = read_installed_version(install_dir)
        log("==> Done")
        log(f"    primary now at {new_ver}")
        if failures:
            raise InstallError("Failed updating workers: " + ", ".join(failures))
    finally:
        shutil.rmtree(work, ignore_errors=True)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("\nAborted.", file=sys.stderr)
        sys.exit(130)
    except InstallError as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
