#!/usr/bin/env python3
"""Create dest-file dirs, hub TLS, Oxidized config. With --demo, fetch the
NetBox demo dump so postgres/init/02-netbox-demo.sh can load it on first init.
"""

from __future__ import annotations

import argparse
import os
import stat
from pathlib import Path

from lab import DIR, run

EXECUTABLES = (
    "container/dnsmgr2",
    "compose.sh",
    "netbox-seed.sh",
    "bin/dnsmgr2",
    "netbox-demo-fetch.sh",
    "postgres/init/02-netbox-demo.sh",
    "prepare.py",
    "seed.py",
    "netbox_seed.py",
    "lab.py",
)

SENTINEL = DIR / "data" / "netbox" / "load-demo"


def _chmod_exec(path: Path) -> None:
    try:
        mode = path.stat().st_mode
        path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    except OSError:
        pass


def _write_if_empty(path: Path, text: str) -> None:
    if not path.is_file() or path.stat().st_size == 0:
        path.write_text(text)


def prepare(*, demo: bool = False) -> None:
    for name in ("data/icinga", "data/oxidized", "data/dns", "data/netbox", "certs"):
        (DIR / name).mkdir(parents=True, exist_ok=True)
    for rel in EXECUTABLES:
        _chmod_exec(DIR / rel)

    if demo:
        run([str(DIR / "netbox-demo-fetch.sh")])
        SENTINEL.write_text("")
    elif SENTINEL.exists():
        SENTINEL.unlink()

    crt, key = DIR / "certs" / "hub.crt", DIR / "certs" / "hub.key"
    if not crt.is_file() or not key.is_file():
        run(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-sha256",
                "-days",
                "3650",
                "-nodes",
                "-keyout",
                str(key),
                "-out",
                str(crt),
                "-subj",
                "/CN=factum-worker",
                "-addext",
                "subjectAltName=DNS:factum-worker,DNS:localhost,IP:127.0.0.1",
            ],
            quiet=True,
        )
        crt.chmod(0o644)
        key.chmod(0o644)

    oxidized_src = DIR / "oxidized" / "config"
    oxidized_dst = DIR / "data" / "oxidized" / "config"
    oxidized_dst.write_bytes(oxidized_src.read_bytes())

    _write_if_empty(
        DIR / "data" / "icinga" / "hosts.conf",
        "// Written by factum2-icinga. Empty until the first sync.\n",
    )
    _write_if_empty(
        DIR / "data" / "icinga" / "users.conf",
        "// Written by factum2-icinga. Empty until the first sync.\n",
    )
    _write_if_empty(DIR / "data" / "oxidized" / "router.db", "lab-dummy:127.0.0.1:ios\n")
    _write_if_empty(
        DIR / "data" / "dns" / "records",
        "# Written by factum2-dns. Empty until the first sync.\n",
    )

    writable = (
        DIR / "data" / "icinga" / "hosts.conf",
        DIR / "data" / "icinga" / "users.conf",
        DIR / "data" / "oxidized" / "router.db",
        DIR / "data" / "oxidized" / "config",
    )
    for path in writable:
        try:
            path.chmod(0o666)
        except OSError:
            pass
    for name in ("data/icinga", "data/oxidized", "data/dns"):
        try:
            os.chmod(DIR / name, 0o777)
        except OSError:
            pass


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--demo",
        action="store_true",
        help="download NetBox demo SQL so first postgres init can load it",
    )
    args = parser.parse_args(argv)
    prepare(demo=args.demo)


if __name__ == "__main__":
    main()
