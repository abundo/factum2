#!/usr/bin/env python3
"""Idempotent NetBox inventory seed from YAML via the REST API.

Copy netbox-seed.example.yaml to netbox-seed.yaml (gitignored) and edit.
Objects are created if missing; existing rows are left in place. Interface
templates on a device type are added when absent, and missing interfaces are
created on devices we touch.
"""

from __future__ import annotations

import argparse
import json
import re
import ssl
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

from lab import DIR, ENV_FILE, env, load_env, log, wait_http

try:
    import yaml
except ImportError as exc:  # pragma: no cover
    raise SystemExit("PyYAML is required (python3 -m pip install pyyaml)") from exc

DEFAULT_URL = "http://127.0.0.1:18000"
DEFAULT_YAML = DIR / "netbox-seed.yaml"
EXAMPLE_YAML = DIR / "netbox-seed.example.yaml"

# YAML keys we resolve ourselves and must not send as raw strings.
_DEVICE_TYPE_SKIP = {"manufacturer", "interfaces", "interface_templates"}
_DEVICE_SKIP = {
    "manufacturer",
    "device_type",
    "platform",
    "site",
    "role",
    "device_role",
    "interfaces",
}
_PLATFORM_SKIP = {"manufacturer"}


class APIError(Exception):
    def __init__(self, method: str, path: str, code: int, body: str):
        self.method = method
        self.path = path
        self.code = code
        self.body = body
        super().__init__(f"{method} {path} -> {code}: {body}")


def slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.strip().lower())
    return slug.strip("-") or "item"


def as_items(value: Any, *, name_key: str = "name") -> list[dict[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, list):
        value = [value]
    items: list[dict[str, Any]] = []
    for entry in value:
        if isinstance(entry, str):
            items.append({name_key: entry})
        elif isinstance(entry, dict):
            items.append(dict(entry))
        else:
            raise SystemExit(f"expected string or mapping, got {type(entry).__name__}")
    return items


def extras(item: dict[str, Any], skip: set[str]) -> dict[str, Any]:
    return {k: v for k, v in item.items() if k not in skip and v is not None}


class NetBox:
    def __init__(self, url: str, token: str, *, dry_run: bool = False):
        self.url = url.rstrip("/")
        self.token = token
        self.dry_run = dry_run
        self._ctx = ssl._create_unverified_context()
        self._fake_id = 0

    def request(
        self,
        method: str,
        path: str,
        *,
        payload: dict[str, Any] | None = None,
        params: dict[str, Any] | None = None,
    ) -> Any:
        url = self.url + path
        if params:
            query = urllib.parse.urlencode(
                {k: v for k, v in params.items() if v is not None}, doseq=True
            )
            url = f"{url}?{query}"
        data = None
        headers = {
            "Authorization": f"Token {self.token}",
            "Accept": "application/json",
        }
        if payload is not None:
            data = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=60, context=self._ctx) as resp:
                body = resp.read()
                if not body:
                    return {}
                return json.loads(body)
        except urllib.error.HTTPError as exc:
            err = exc.read().decode("utf-8", "replace")
            raise APIError(method, path, exc.code, err) from exc

    def find(self, collection: str, **params: Any) -> dict[str, Any] | None:
        if self.dry_run:
            return None
        params = {**params, "limit": 1}
        data = self.request("GET", f"/api/{collection}/", params=params)
        results = data.get("results") or []
        return results[0] if results else None

    def post(self, collection: str, payload: dict[str, Any]) -> dict[str, Any]:
        return self.request("POST", f"/api/{collection}/", payload=payload)

    def ensure(
        self,
        collection: str,
        lookup: dict[str, Any],
        payload: dict[str, Any],
        label: str,
    ) -> dict[str, Any]:
        found = self.find(collection, **lookup)
        if found:
            log(f"exists {label} id={found['id']}")
            return found
        if self.dry_run:
            self._fake_id += 1
            log(f"would create {label}")
            return {"id": self._fake_id, **payload}
        try:
            created = self.post(collection, payload)
        except APIError as exc:
            if exc.code in {400, 409}:
                found = self.find(collection, **lookup)
                if found:
                    log(f"exists {label} id={found['id']} (after {exc.code})")
                    return found
            raise
        log(f"created {label} id={created.get('id')}")
        return created


class Seeder:
    def __init__(self, nb: NetBox, data: dict[str, Any]):
        self.nb = nb
        self.data = data
        self.defaults = data.get("defaults") or {}
        if not isinstance(self.defaults, dict):
            raise SystemExit("defaults: must be a mapping")
        self.manufacturers: dict[str, int] = {}
        self.sites: dict[str, int] = {}
        self.roles: dict[str, int] = {}
        self.platforms: dict[str, int] = {}
        self.device_types: dict[tuple[str, str], tuple[int, list[dict[str, Any]]]] = {}

    def run(self) -> None:
        self._manufacturers()
        self._sites()
        self._roles()
        self._platforms()
        self._device_types()
        self._devices()

    def _cache(self, cache: dict[str, int], name: str, obj_id: int) -> int:
        cache[name] = obj_id
        cache[name.lower()] = obj_id
        return obj_id

    def _lookup_id(
        self,
        cache: dict[str, int],
        collection: str,
        name: str,
        *,
        kind: str,
    ) -> int:
        if name in cache:
            return cache[name]
        if name.lower() in cache:
            return cache[name.lower()]
        found = self.nb.find(collection, name=name)
        if not found:
            found = self.nb.find(collection, slug=slugify(name))
        if not found:
            raise SystemExit(f"{kind} {name!r} not found (add it to the YAML or create it in NetBox)")
        return self._cache(cache, name, int(found["id"]))

    def _manufacturers(self) -> None:
        for item in as_items(self.data.get("manufacturers")):
            name = str(item["name"])
            slug = str(item.get("slug") or slugify(name))
            payload = {"name": name, "slug": slug, **extras(item, {"name", "slug"})}
            obj = self.nb.ensure(
                "dcim/manufacturers",
                {"slug": slug},
                payload,
                f"manufacturer {name}",
            )
            self._cache(self.manufacturers, name, int(obj["id"]))
            self._cache(self.manufacturers, slug, int(obj["id"]))

    def _sites(self) -> None:
        for item in as_items(self.data.get("sites")):
            name = str(item["name"])
            slug = str(item.get("slug") or slugify(name))
            payload = {
                "name": name,
                "slug": slug,
                "status": item.get("status") or "active",
                **extras(item, {"name", "slug", "status"}),
            }
            obj = self.nb.ensure("dcim/sites", {"slug": slug}, payload, f"site {name}")
            self._cache(self.sites, name, int(obj["id"]))
            self._cache(self.sites, slug, int(obj["id"]))

    def _roles(self) -> None:
        roles = self.data.get("device_roles") or self.data.get("roles")
        for item in as_items(roles):
            name = str(item["name"])
            slug = str(item.get("slug") or slugify(name))
            payload = {
                "name": name,
                "slug": slug,
                "color": str(item.get("color") or "9e9e9e").lstrip("#"),
                **extras(item, {"name", "slug", "color"}),
            }
            obj = self.nb.ensure(
                "dcim/device-roles",
                {"slug": slug},
                payload,
                f"device role {name}",
            )
            self._cache(self.roles, name, int(obj["id"]))
            self._cache(self.roles, slug, int(obj["id"]))

    def _manufacturer_id(self, name: str) -> int:
        return self._lookup_id(self.manufacturers, "dcim/manufacturers", name, kind="manufacturer")

    def _platforms(self) -> None:
        for item in as_items(self.data.get("platforms")):
            name = str(item["name"])
            slug = str(item.get("slug") or slugify(name))
            payload: dict[str, Any] = {
                "name": name,
                "slug": slug,
                **extras(item, _PLATFORM_SKIP | {"name", "slug"}),
            }
            if item.get("manufacturer"):
                payload["manufacturer"] = self._manufacturer_id(str(item["manufacturer"]))
            obj = self.nb.ensure(
                "dcim/platforms",
                {"slug": slug},
                payload,
                f"platform {name}",
            )
            self._cache(self.platforms, name, int(obj["id"]))
            self._cache(self.platforms, slug, int(obj["id"]))

    def _device_types(self) -> None:
        for item in as_items(self.data.get("device_types"), name_key="model"):
            model = str(item.get("model") or item.get("name") or "")
            if not model:
                raise SystemExit("device_types entry needs model")
            mfr_name = str(item.get("manufacturer") or "")
            if not mfr_name:
                raise SystemExit(f"device type {model!r} needs manufacturer")
            mfr_id = self._manufacturer_id(mfr_name)
            slug = str(item.get("slug") or slugify(model))
            payload = {
                "manufacturer": mfr_id,
                "model": model,
                "slug": slug,
                **extras(item, _DEVICE_TYPE_SKIP | {"model", "name", "slug"}),
            }
            obj = self.nb.ensure(
                "dcim/device-types",
                {"slug": slug, "manufacturer_id": mfr_id},
                payload,
                f"device type {mfr_name} {model}",
            )
            templates = as_items(
                item.get("interfaces") or item.get("interface_templates") or []
            )
            self._interface_templates(int(obj["id"]), f"{mfr_name} {model}", templates)
            key = (mfr_name.lower(), model.lower())
            self.device_types[key] = (int(obj["id"]), templates)
            self.device_types[("", model.lower())] = (int(obj["id"]), templates)

    def _interface_templates(
        self, device_type_id: int, label: str, templates: list[dict[str, Any]]
    ) -> None:
        for item in templates:
            name = str(item.get("name") or "")
            if not name:
                raise SystemExit(f"interface template on {label} needs name")
            if not item.get("type"):
                raise SystemExit(f"interface template {name} on {label} needs type")
            payload = {
                "device_type": device_type_id,
                "name": name,
                "type": item["type"],
                "mgmt_only": bool(item.get("mgmt_only", False)),
                **extras(item, {"name", "type", "mgmt_only", "device_type"}),
            }
            self.nb.ensure(
                "dcim/interface-templates",
                {"device_type_id": device_type_id, "name": name},
                payload,
                f"interface template {label} {name}",
            )

    def _device_type_ref(
        self, model: str, manufacturer: str | None
    ) -> tuple[int, list[dict[str, Any]]]:
        key = ((manufacturer or "").lower(), model.lower())
        if key in self.device_types:
            return self.device_types[key]
        fallback = ("", model.lower())
        if not manufacturer and fallback in self.device_types:
            return self.device_types[fallback]
        params: dict[str, Any] = {"model": model}
        if manufacturer:
            params["manufacturer_id"] = self._manufacturer_id(manufacturer)
        found = self.nb.find("dcim/device-types", **params)
        if not found:
            raise SystemExit(
                f"device type {model!r}"
                + (f" ({manufacturer})" if manufacturer else "")
                + " not found"
            )
        return int(found["id"]), []

    def _devices(self) -> None:
        for item in as_items(self.data.get("devices")):
            name = str(item.get("name") or "")
            if not name:
                raise SystemExit("devices entry needs name")
            model = str(item.get("device_type") or item.get("model") or "")
            if not model:
                raise SystemExit(f"device {name!r} needs device_type")
            mfr = item.get("manufacturer")
            mfr_name = str(mfr) if mfr else None
            type_id, templates = self._device_type_ref(model, mfr_name)

            site_name = str(item.get("site") or self.defaults.get("site") or "")
            role_name = str(
                item.get("role")
                or item.get("device_role")
                or self.defaults.get("role")
                or ""
            )
            if not site_name:
                raise SystemExit(f"device {name!r} needs site (or defaults.site)")
            if not role_name:
                raise SystemExit(f"device {name!r} needs role (or defaults.role)")

            payload: dict[str, Any] = {
                "name": name,
                "device_type": type_id,
                "site": self._lookup_id(self.sites, "dcim/sites", site_name, kind="site"),
                "role": self._lookup_id(
                    self.roles, "dcim/device-roles", role_name, kind="device role"
                ),
                "status": item.get("status") or self.defaults.get("status") or "active",
                **extras(item, _DEVICE_SKIP | {"name", "model", "status"}),
            }
            platform_name = item.get("platform") or self.defaults.get("platform")
            if platform_name:
                payload["platform"] = self._lookup_id(
                    self.platforms, "dcim/platforms", str(platform_name), kind="platform"
                )

            obj = self.nb.ensure(
                "dcim/devices",
                {"name": name, "site_id": payload["site"]},
                payload,
                f"device {name}",
            )
            wanted = templates + as_items(item.get("interfaces") or [])
            self._device_interfaces(int(obj["id"]), name, wanted)

    def _device_interfaces(
        self, device_id: int, device_name: str, interfaces: list[dict[str, Any]]
    ) -> None:
        seen: set[str] = set()
        for item in interfaces:
            ifname = str(item.get("name") or "")
            if not ifname or ifname in seen:
                continue
            seen.add(ifname)
            iftype = item.get("type")
            if not iftype:
                continue
            payload = {
                "device": device_id,
                "name": ifname,
                "type": iftype,
                "mgmt_only": bool(item.get("mgmt_only", False)),
                **extras(item, {"name", "type", "mgmt_only", "device"}),
            }
            self.nb.ensure(
                "dcim/interfaces",
                {"device_id": device_id, "name": ifname},
                payload,
                f"interface {device_name} {ifname}",
            )


def load_yaml(path: Path) -> dict[str, Any]:
    data = yaml.safe_load(path.read_text())
    if data is None:
        return {}
    if not isinstance(data, dict):
        raise SystemExit(f"{path}: top level must be a mapping")
    return data


def seed_inventory(
    path: Path,
    *,
    url: str,
    token: str,
    dry_run: bool = False,
    wait: bool = False,
) -> None:
    if not path.is_file():
        raise SystemExit(
            f"missing {path}\nCopy {EXAMPLE_YAML.name} to {DEFAULT_YAML.name} and edit."
        )
    data = load_yaml(path)
    if wait and not dry_run:
        wait_http(url.rstrip("/") + "/login/", 80)
    if not dry_run and not token:
        raise SystemExit("NetBox API token missing (--token or NETBOX_SUPERUSER_API_TOKEN)")
    log(f"NetBox seed {path}" + (" (dry-run)" if dry_run else f" @ {url}"))
    Seeder(NetBox(url, token, dry_run=dry_run), data).run()
    log("NetBox seed done")


def main(argv: list[str] | None = None) -> None:
    load_env(ENV_FILE)
    parser = argparse.ArgumentParser(
        description="Seed NetBox from YAML via the REST API.",
        epilog=f"Copy {EXAMPLE_YAML} to {DEFAULT_YAML} (gitignored) and edit.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "yaml",
        nargs="?",
        default=str(DEFAULT_YAML),
        help=f"YAML file (default: {DEFAULT_YAML})",
    )
    parser.add_argument("--url", default=env("NETBOX_URL", DEFAULT_URL), help="NetBox origin")
    parser.add_argument(
        "--token",
        default=env("NETBOX_TOKEN") or env("NETBOX_SUPERUSER_API_TOKEN"),
        help="API token (default: NETBOX_SUPERUSER_API_TOKEN from .env)",
    )
    parser.add_argument("--dry-run", action="store_true", help="parse YAML and print actions")
    parser.add_argument(
        "--wait",
        action="store_true",
        help="wait for NetBox HTTP before seeding (standalone use)",
    )
    args = parser.parse_args(argv)
    seed_inventory(
        Path(args.yaml),
        url=args.url,
        token=args.token,
        dry_run=args.dry_run,
        wait=args.wait,
    )


if __name__ == "__main__":
    main()
