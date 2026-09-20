#!/usr/bin/env python3
"""Create dest-file dirs, hub TLS, Oxidized/Grafana config, BIND layout,
and lab ELINE CLI templates.

With --demo, fetch the NetBox demo dump so postgres/init/02-netbox-demo.sh
can load it on first init.

Feature flags (DNS, IPAM, …) live in Settings and are turned on by seed.py.
This script only prepares the files those features write. Catalog rows
(including ELINE CLI objects) are posted later by service_definitions.py,
which reads the templates written here.

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
    "up.sh",
    "netbox-seed.sh",
    "dns/entrypoint.sh",
    "icinga/entrypoint.sh",
    "librenms/98-lab-tune.sh",
    "librenms/99-factum-worker.sh",
    "oxidized/entrypoint.sh",
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

# GenericRenderData bodies for Catalog CLI under _catalog/cli/ELINE/<platform>.
# Rendered per UNI. No .Remote / .LocalVLAN — VLAN is Current.Fields.vlan,
# far-end loopback is .Others[0].NeighborIP (empty on same-device).
ELINE_EOS_ADD = """\
interface {{.LocalIface}}
 no switchport
interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
 description {{.Name}}
 encapsulation vlan
  client dot1q {{ .Current.Fields.vlan }}
  exit
exit
{{ if len(.Others) }}{{ peer := .Others[0] }}{{ if peer.NeighborIP }}
mpls ldp
 pseudowires
  pseudowire {{.Name}}
   neighbor {{ peer.NeighborIP }}
   pseudowire-id {{.ServiceNumericID}}
   mtu {{ .Vars.mtu ? .Vars.mtu : (.Fields.mtu ? .Fields.mtu : 9100) }}
   control-word
  exit
 exit
exit
patch panel
 patch {{.Name}}
  connector 1 interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
  connector 2 pseudowire ldp {{.Name}}
 exit
exit
{{ else }}
interface {{ peer.LocalIface }}
 no switchport
interface {{ peer.LocalIface }}.{{ peer.Fields.vlan }}
 description {{.Name}}
 encapsulation vlan
  client dot1q {{ peer.Fields.vlan }}
  exit
exit
patch panel
 patch {{.Name}}
  connector 1 interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
  connector 2 interface {{ peer.LocalIface }}.{{ peer.Fields.vlan }}
 exit
exit
{{ end }}{{ end }}
"""

ELINE_EOS_REMOVE = """\
mpls ldp
 pseudowires
  no pseudowire {{.Name}}
  exit
exit
patch panel
 no patch {{.Name}}
 exit
no interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
"""

# Fallback for ios-xr / sros until those packs are filled in.
ELINE_ADD = """\
interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
 description {{.Name}}
{{ range .Others }}{{ if .NeighborIP }} neighbor {{.NeighborIP}}
{{ end }}{{ end }}
"""

ELINE_REMOVE = """\
no interface {{.LocalIface}}.{{ .Current.Fields.vlan }}
"""

ELINE_ADD_PATH = DIR / "templates" / "eline-add.tmpl"
ELINE_REMOVE_PATH = DIR / "templates" / "eline-remove.tmpl"
ELINE_EOS_ADD_PATH = DIR / "templates" / "eline-eos-add.tmpl"
ELINE_EOS_REMOVE_PATH = DIR / "templates" / "eline-eos-remove.tmpl"

DATA_DIRS = (
    "data/icinga",
    "data/oxidized",
    "data/dns",
    "data/bind",
    "data/bind-zones",
    "data/dnsmgr2",
    "data/lego",
    "data/prometheus",
    "data/grafana",
    "data/netbox",
    "data/storage",
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


def _read_tmpl(path: Path, fallback: str) -> str:
    if path.is_file() and path.stat().st_size:
        return path.read_text()
    return fallback


def eline_templates(platform: str = "") -> tuple[str, str]:
    """Return (add, remove) bodies for a platform, preferring prepare() files."""
    if platform == "eos":
        return (
            _read_tmpl(ELINE_EOS_ADD_PATH, ELINE_EOS_ADD),
            _read_tmpl(ELINE_EOS_REMOVE_PATH, ELINE_EOS_REMOVE),
        )
    return (
        _read_tmpl(ELINE_ADD_PATH, ELINE_ADD),
        _read_tmpl(ELINE_REMOVE_PATH, ELINE_REMOVE),
    )


def _preload_eline_templates() -> None:
    (DIR / "templates").mkdir(parents=True, exist_ok=True)
    log("Writing lab ELINE CLI templates")
    ELINE_ADD_PATH.write_text(ELINE_ADD)
    ELINE_REMOVE_PATH.write_text(ELINE_REMOVE)
    ELINE_EOS_ADD_PATH.write_text(ELINE_EOS_ADD)
    ELINE_EOS_REMOVE_PATH.write_text(ELINE_EOS_REMOVE)


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
    _preload_eline_templates()

    oxidized_src = DIR / "oxidized" / "config"
    oxidized_dst = DIR / "data" / "oxidized" / "config"
    oxidized_dst.write_bytes(oxidized_src.read_bytes())

    grafana_src = DIR / "grafana" / "grafana.ini"
    grafana_dst = DIR / "data" / "grafana" / "grafana.ini"
    grafana_dst.write_bytes(grafana_src.read_bytes())

    shutil.copyfile(DIR / "dns" / "named.conf", DIR / "data" / "bind" / "named.conf")
    shutil.copyfile(DIR / "dns" / "dnsmgr2.yaml", DIR / "data" / "dns" / "dnsmgr2.yaml")
    _write_if_empty(
        DIR / "data" / "dns" / "zones.yaml",
        "# Written by factum2-dns. Empty until the first DNS sync.\n",
    )
    _write_if_empty(
        DIR / "data" / "dns" / "prefixes.yaml",
        "# Written by factum2-dns. Empty until the first DHCP sync.\n",
    )
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
        """{
  "version": 1,
  "domains": [
    {
      "name": "lab.example",
      "records": [
        {"name": "ns1", "type": "A", "value": "127.0.0.1"}
      ]
    }
  ]
}
""",
    )
    _write_if_empty(DIR / "data" / "prometheus" / "targets.json", "[]\n")

    writable = (
        DIR / "data" / "icinga" / "hosts.conf",
        DIR / "data" / "icinga" / "users.conf",
        DIR / "data" / "oxidized" / "router.db",
        DIR / "data" / "oxidized" / "config",
        DIR / "data" / "dns" / "records",
        DIR / "data" / "dns" / "dnsmgr2.yaml",
        DIR / "data" / "dns" / "zones.yaml",
        DIR / "data" / "dns" / "prefixes.yaml",
        DIR / "data" / "bind" / "named.conf",
        DIR / "data" / "bind" / "named.conf.dnsmgr2",
        DIR / "data" / "prometheus" / "targets.json",
        DIR / "data" / "grafana" / "grafana.ini",
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
        "data/lego",
        "data/prometheus",
        "data/grafana",
        "data/storage",
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
