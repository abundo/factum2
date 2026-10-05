#!/usr/bin/env python3
"""Install the pinned Wiregasm build into web/static/wiregasm.

Wiregasm is Wireshark compiled to WebAssembly (GPL-2.0), published as the
@goodtools/wiregasm npm package. factum2 serves the three dist files; it
does not vendor them. `make wiregasm` calls this script.
"""

import base64
import hashlib
import io
import socket
import sys
import tarfile
import urllib.request
from pathlib import Path

FILES = ("wiregasm.js", "wiregasm.wasm.gz", "wiregasm.data.gz")


def prefer_ipv4() -> None:
    """registry.npmjs.org publishes AAAA records that do not connect here.
    Sort A records first so urllib does not sit in SYN-SENT on IPv6.
    """
    orig = socket.getaddrinfo

    def getaddrinfo(host, port, family=0, typ=0, proto=0, flags=0):
        rows = orig(host, port, family, typ, proto, flags)
        rows.sort(key=lambda row: 0 if row[0] == socket.AF_INET else 1)
        return rows

    socket.getaddrinfo = getaddrinfo


def main() -> None:
    if len(sys.argv) != 5:
        raise SystemExit("usage: install_wiregasm.py VERSION URL SHA512 DEST")
    version, url, sha, dest_arg = sys.argv[1:]
    dest = Path(dest_arg)
    stamp = dest / "VERSION"
    if (
        stamp.is_file()
        and stamp.read_text().strip() == version
        and all((dest / name).is_file() for name in FILES)
    ):
        print(f"wiregasm {version} already in {dest}")
        return
    print(f"downloading {url}", flush=True)
    prefer_ipv4()
    req = urllib.request.Request(url, headers={"User-Agent": "factum2-make-wiregasm"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        blob = resp.read()
    got = base64.b64encode(hashlib.sha512(blob).digest()).decode()
    if got != sha:
        raise SystemExit(f"integrity mismatch for {url}")
    wanted = {f"package/dist/{name}": name for name in FILES}
    dest.mkdir(parents=True, exist_ok=True)
    with tarfile.open(fileobj=io.BytesIO(blob), mode="r:gz") as tar:
        for member in tar.getmembers():
            name = wanted.get(member.name)
            if not name or not member.isfile():
                continue
            src = tar.extractfile(member)
            if src is None:
                continue
            (dest / name).write_bytes(src.read())
    missing = [name for name in FILES if not (dest / name).is_file()]
    if missing:
        raise SystemExit("archive lacks " + ", ".join(missing))
    stamp.write_text(version + "\n")
    print(f"installed wiregasm {version} in {dest}")


if __name__ == "__main__":
    main()
