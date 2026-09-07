#!/usr/bin/env python3
"""Create dest-file dirs, hub TLS, Oxidized config, BIND layout, and the
dnsmgr2 binary for the dns container. With --demo, fetch the NetBox demo
dump so postgres/init/02-netbox-demo.sh can load it on first init.

Feature flags (DNS, IPAM, …) live in Settings and are turned on by seed.py.
This script only prepares the files those features write.

--wipe removes bind-mounted dest data (used by `make dev-reset`); it does
not start anything.
"""

from __future__ import annotations

import argparse
import os
import shutil
import stat
from pathlib import Path

from lab import DIR, log, run

EXECUTABLES = (
    "compose.sh",
    "netbox-seed.sh",
    "bin/dnsmgr2",
    "dns/entrypoint.sh",
    "icinga/entrypoint.sh",
    "librenms/98-lab-tune.sh",
    "librenms/99-factum-worker.sh",
    "oxidized/factum-worker/run",
    "prometheus/entrypoint.sh",
    "netbox-demo-fetch.sh",
    "postgres/init/02-netbox-demo.sh",
    "prepare.py",
    "seed.py",
    "netbox_seed.py",
    "lab.py",
)

HUB_CERT_DNS = (
    "factum-worker",
    "dns",
    "icinga",
    "librenms",
    "oxidized",
    "prometheus",
)

SENTINEL = DIR / "data" / "netbox" / "load-demo"
DNSMGR2_REPO = "https://github.com/abundo/dnsmgr2"
DNSMGR2_SRC = DIR / ".dnsmgr2-src"
DNSMGR2_BIN = DIR / "bin" / "dnsmgr2"

DATA_DIRS = (
    "data/icinga",
    "data/oxidized",
    "data/dns",
    "data/bind",
    "data/bind-zones",
    "data/dnsmgr2",
    "data/prometheus",
    "data/netbox",
    "certs",
)

WIPE_DIRS = DATA_DIRS


def _chmod_exec(path: Path) -> None:
    try:
        mode = path.stat().st_mode
        path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    except OSError:
        pass


def _write_if_empty(path: Path, text: str) -> None:
    if not path.is_file() or path.stat().st_size == 0:
        path.write_text(text)


def _rmtree(path: Path) -> None:
    if not path.exists():
        return
    try:
        shutil.rmtree(path)
        return
    except OSError:
        pass
    uid, gid = os.getuid(), os.getgid()
    run(
        [
            "docker",
            "run",
            "--rm",
            "-v",
            f"{path}:/fix",
            "docker.io/alpine",
            "sh",
            "-c",
            f"chown -R {uid}:{gid} /fix && rm -rf /fix/..?* /fix/.[!.]* /fix/*",
        ],
        check=False,
        quiet=True,
    )
    shutil.rmtree(path, ignore_errors=True)


def wipe() -> None:
    log("Wiping lab dest-file data")
    for name in WIPE_DIRS:
        _rmtree(DIR / name)


def _hub_cert_has_dns_san(crt: Path) -> bool:
    if not crt.is_file():
        return False
    result = run(
        ["openssl", "x509", "-in", str(crt), "-noout", "-ext", "subjectAltName"],
        check=False,
        capture_output=True,
        text=True,
    )
    text = result.stdout or ""
    return all(f"DNS:{name}" in text for name in HUB_CERT_DNS)


def _ensure_hub_certs() -> None:
    crt, key = DIR / "certs" / "hub.crt", DIR / "certs" / "hub.key"
    if crt.is_file() and key.is_file() and _hub_cert_has_dns_san(crt):
        return
    sans = ",".join(f"DNS:{name}" for name in HUB_CERT_DNS) + ",DNS:localhost,IP:127.0.0.1"
    log(f"Writing hub TLS cert (SAN {', '.join(HUB_CERT_DNS)})")
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
            f"subjectAltName={sans}",
        ],
        quiet=True,
    )
    crt.chmod(0o644)
    key.chmod(0o644)


def _ensure_dnsmgr2() -> None:
    DNSMGR2_BIN.parent.mkdir(parents=True, exist_ok=True)
    if DNSMGR2_BIN.is_file() and os.access(DNSMGR2_BIN, os.X_OK):
        return
    log(f"Building dnsmgr2 from {DNSMGR2_REPO}")
    if not (DNSMGR2_SRC / ".git").is_dir():
        if DNSMGR2_SRC.exists():
            shutil.rmtree(DNSMGR2_SRC)
        run(["git", "clone", "--depth", "1", DNSMGR2_REPO, str(DNSMGR2_SRC)])
    else:
        run(["git", "-C", str(DNSMGR2_SRC), "pull", "--ff-only"], check=False)
    run(["make"], cwd=str(DNSMGR2_SRC))
    built = DNSMGR2_SRC / "bin" / "dnsmgr2"
    if not built.is_file():
        raise SystemExit(f"dnsmgr2 build produced no binary at {built}")
    shutil.copy2(built, DNSMGR2_BIN)
    _chmod_exec(DNSMGR2_BIN)


def prepare(*, demo: bool = False) -> None:
    for name in DATA_DIRS:
        (DIR / name).mkdir(parents=True, exist_ok=True)
    for rel in EXECUTABLES:
        _chmod_exec(DIR / rel)

    if demo:
        run([str(DIR / "netbox-demo-fetch.sh")])
        SENTINEL.write_text("")
    elif SENTINEL.exists():
        SENTINEL.unlink()

    _ensure_hub_certs()
    _ensure_dnsmgr2()

    oxidized_src = DIR / "oxidized" / "config"
    oxidized_dst = DIR / "data" / "oxidized" / "config"
    oxidized_dst.write_bytes(oxidized_src.read_bytes())

    shutil.copyfile(DIR / "dns" / "named.conf", DIR / "data" / "bind" / "named.conf")
    dst_yaml = DIR / "data" / "dns" / "dnsmgr2.yaml"
    if not dst_yaml.is_file() or dst_yaml.stat().st_size == 0:
        shutil.copyfile(DIR / "dns" / "dnsmgr2.yaml", dst_yaml)
    _write_if_empty(
        DIR / "data" / "bind" / "named.conf.dnsmgr2",
        "// Written by dnsmgr2. Empty until the first sync.\n",
    )
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
        "# Written by factum2-dns. ns1 is required so named-checkzone accepts the zone.\n"
        "$DOMAIN lab.example\n"
        "ns1                                     A       127.0.0.1\n",
    )
    _write_if_empty(DIR / "data" / "prometheus" / "targets.json", "[]\n")

    writable = (
        DIR / "data" / "icinga" / "hosts.conf",
        DIR / "data" / "icinga" / "users.conf",
        DIR / "data" / "oxidized" / "router.db",
        DIR / "data" / "oxidized" / "config",
        DIR / "data" / "dns" / "records",
        DIR / "data" / "dns" / "dnsmgr2.yaml",
        DIR / "data" / "bind" / "named.conf",
        DIR / "data" / "bind" / "named.conf.dnsmgr2",
        DIR / "data" / "prometheus" / "targets.json",
    )
    for path in writable:
        try:
            path.chmod(0o666)
        except OSError:
            pass
    for name in (
        "data/icinga",
        "data/oxidized",
        "data/dns",
        "data/bind",
        "data/bind-zones",
        "data/dnsmgr2",
        "data/prometheus",
    ):
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
    parser.add_argument(
        "--wipe",
        action="store_true",
        help="remove bind-mounted dest data; do not start the lab",
    )
    args = parser.parse_args(argv)
    if args.wipe:
        wipe()
        return
    prepare(demo=args.demo)


if __name__ == "__main__":
    main()
