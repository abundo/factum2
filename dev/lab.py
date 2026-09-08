"""Shared helpers for the compose lab scripts."""

from __future__ import annotations

import os
import ssl
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

DIR = Path(__file__).resolve().parent
REPO_ROOT = DIR.parent
ENV_FILE = DIR / ".env"

OK_HTTP = {200, 204, 301, 302, 401, 403}

_T0 = time.monotonic()


def log(msg: str) -> None:
    print(f"==> {msg}  [{time.monotonic() - _T0:.0f}s]", flush=True)


def load_env(path: Path = ENV_FILE) -> dict[str, str]:
    """Load KEY=VAL lines into os.environ (setdefault) and return them."""
    found: dict[str, str] = {}
    if not path.is_file():
        return found
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        key, value = key.strip(), value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        found[key] = value
        os.environ.setdefault(key, value)
    return found


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default)


def run(
    cmd: list[str] | tuple[str, ...],
    *,
    check: bool = True,
    input: str | bytes | None = None,
    quiet: bool = False,
    **kwargs,
) -> subprocess.CompletedProcess:
    cmd_s = [str(c) for c in cmd]
    kw = dict(kwargs)
    if input is not None and not isinstance(input, (bytes, bytearray)):
        input = str(input).encode()
    if input is not None:
        kw["input"] = input
    if quiet:
        kw.setdefault("stdout", subprocess.DEVNULL)
        kw.setdefault("stderr", subprocess.DEVNULL)
    result = subprocess.run(cmd_s, check=False, **kw)
    if check and result.returncode != 0:
        raise SystemExit(f"command failed ({result.returncode}): {' '.join(cmd_s)}")
    return result


def compose(*args: str) -> list[str]:
    return [str(DIR / "compose.sh"), *args]


def wait_http(url: str, tries: int = 60, *, required: bool = True) -> bool:
    ctx = ssl._create_unverified_context()
    last = "no response"
    for _ in range(tries):
        try:
            req = urllib.request.Request(url, method="GET")
            with urllib.request.urlopen(req, timeout=2, context=ctx) as resp:
                last = str(resp.status)
                if resp.status in OK_HTTP:
                    return True
        except urllib.error.HTTPError as exc:
            last = str(exc.code)
            if exc.code in OK_HTTP:
                return True
        except OSError as exc:
            last = str(exc)
        time.sleep(1)
    msg = f"timed out waiting for {url} (last HTTP {last})"
    if required:
        raise SystemExit(msg)
    print(msg, flush=True)
    return False
