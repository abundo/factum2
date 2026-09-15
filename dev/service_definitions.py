#!/usr/bin/env python3
"""Lab-only sample service definitions (ELINE, ELAN, POLARIX).

Factum does not seed products. The laptop lab posts these so Config →
Catalog → Service types is not empty. Idempotent: existing names are left
alone. Safe to re-run against a running factum-web.
"""

from __future__ import annotations

import json
import os
import ssl
import urllib.error
import urllib.request
from typing import Any

from lab import env, load_env, log
from prepare import eline_templates

TOKEN = "lab-factum-api-token-not-for-production"
DEFAULT_URL = "http://127.0.0.1:18091"

BW = {
    "name": "bandwidth_mbps",
    "type": "int",
    "required": True,
    "description": "Service bandwidth",
    "min": 1,
    "max": 1000,
    "unit": "Mbps",
}
BW_OPTIONAL = {**BW, "required": False, "description": "Override default bandwidth"}
VLAN = {
    "name": "vlan",
    "type": "vlan",
    "required": True,
    "description": "Customer VLAN / SAP tag",
}
SERVICE_ID = {
    "name": "service_id",
    "type": "service_id",
    "required": True,
    "description": "Commercial service",
}

DEFINITIONS: list[dict[str, Any]] = [
    {
        "name": "ELINE",
        "description": "L2VPN point to point",
        "schema": [
            SERVICE_ID,
            BW,
            {
                "name": "mtu",
                "type": "int",
                "required": False,
                "description": "Pseudowire MTU",
                "default_value": 9100,
            },
            {
                "name": "control_word",
                "type": "bool",
                "required": False,
                "description": "EOS pseudowire control-word",
                "default_value": True,
            },
        ],
        "interfaces": {
            "min": 2,
            "max": 2,
            "unique": True,
            "fields": [VLAN],
        },
        "sync_source": "eline",
        "netbox_type": "evpl",
        "connection_types": [
            {"name": "Direct PE"},
            {"name": "NNI VLAN"},
        ],
    },
    {
        "name": "ELAN",
        "description": "L2VPN multipoint",
        "schema": [
            BW,
            {
                "name": "max_mac_addresses",
                "type": "int",
                "required": True,
                "description": "MAC table limit",
                "min": 100,
            },
        ],
        "interfaces": {
            "min": 0,
            "max": 0,
            "unique": True,
            "fields": [SERVICE_ID, VLAN, BW_OPTIONAL],
        },
        "sync_source": "elan",
        "netbox_type": "vpls",
        "connection_types": [],
    },
    {
        "name": "POLARIX",
        "description": "Internet access",
        "schema": [
            BW,
            {
                "name": "max_prefixes",
                "type": "int",
                "required": True,
                "description": "Prefix limit",
                "min": 100,
                "max": 1000,
            },
            {
                "name": "routing",
                "type": "enum",
                "required": True,
                "description": "Customer routing",
                "enum": [
                    {"label": "Static", "value": "static"},
                    {"label": "BGP", "value": "bgp"},
                ],
            },
            {
                "name": "ipv4_prefixes",
                "type": "list",
                "description": "IPv4 prefixes for prefix-lists or static routes",
                "items": {"type": "ipv4_prefix"},
            },
            {
                "name": "ipv6_prefixes",
                "type": "list",
                "description": "IPv6 prefixes for prefix-lists or static routes",
                "items": {"type": "ipv6_prefix"},
            },
        ],
        "interfaces": {
            "min": 0,
            "max": 0,
            "unique": True,
            "fields": [
                SERVICE_ID,
                BW_OPTIONAL,
                {
                    "name": "ipv4_prefix",
                    "type": "ipv4_prefix",
                    "resource": "polarix-v4",
                },
                {
                    "name": "ipv6_prefix",
                    "type": "ipv6_prefix",
                    "resource": "polarix-v6",
                },
            ],
        },
        "sync_source": "",
        "netbox_type": "",
        "connection_types": [],
    },
]




class FactumClient:
    def __init__(self, base: str, token: str) -> None:
        self.base = base.rstrip("/")
        self.token = token
        self._ctx = ssl.create_default_context()

    def request(self, method: str, path: str, body: Any | None = None) -> Any:
        data = None
        headers = {
            "Authorization": f"Bearer {self.token}",
            "Accept": "application/json",
        }
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(
            self.base + path, data=data, headers=headers, method=method
        )
        try:
            with urllib.request.urlopen(req, context=self._ctx, timeout=30) as resp:
                raw = resp.read()
                if not raw:
                    return None
                return json.loads(raw.decode())
        except urllib.error.HTTPError as e:
            err = e.read().decode(errors="replace")
            raise SystemExit(f"{method} {path} -> {e.code}: {err}") from e

    def get(self, path: str) -> Any:
        return self.request("GET", path)

    def post(self, path: str, body: Any) -> Any:
        return self.request("POST", path, body)

    def put(self, path: str, body: Any) -> Any:
        return self.request("PUT", path, body)

    def delete(self, path: str) -> Any:
        return self.request("DELETE", path)


def _scopes_by_parent(scopes: list[dict[str, Any]], parent_id: int | None) -> list[dict[str, Any]]:
    return [s for s in scopes if s.get("parent_id") == parent_id]


def _named(scopes: list[dict[str, Any]], name: str, kind: str | None = None) -> dict[str, Any] | None:
    for s in scopes:
        if s.get("name") != name:
            continue
        if kind and s.get("kind") != kind:
            continue
        return s
    return None


def _remove_stray_eline_cli(client: FactumClient, scopes: list[dict[str, Any]]) -> None:
    """Goose wipe left a baseline CLI named ELINE under _catalog/cli."""
    global_ = _named(scopes, "global", "folder")
    if not global_:
        return
    catalog = _named(_scopes_by_parent(scopes, global_["id"]), "_catalog", "folder")
    if not catalog:
        return
    cli_folder = _named(_scopes_by_parent(scopes, catalog["id"]), "cli", "folder")
    if not cli_folder:
        return
    stray = _named(_scopes_by_parent(scopes, cli_folder["id"]), "ELINE", "cli")
    if stray and stray.get("service_type_id") in (None, 0):
        log(f"Removing leftover baseline CLI object ELINE (id={stray['id']})")
        client.delete(f"/api/config/scopes/{stray['id']}")


def _feature_body(add: str, remove: str) -> dict[str, Any]:
    return {"name": "apply", "add_commands": add, "remove_commands": remove}


def _ensure_eline_cli(client: FactumClient, type_id: int) -> None:
    scopes = client.get("/api/config/scopes") or []
    global_ = _named(scopes, "global", "folder")
    catalog = _named(_scopes_by_parent(scopes, global_["id"]), "_catalog", "folder") if global_ else None
    cli_folder = _named(_scopes_by_parent(scopes, catalog["id"]), "cli", "folder") if catalog else None
    type_folder = (
        _named(_scopes_by_parent(scopes, cli_folder["id"]), "ELINE", "folder") if cli_folder else None
    )
    if not type_folder:
        return
    kids = _scopes_by_parent(scopes, type_folder["id"])
    for plat in ("eos", "ios-xr", "sros"):
        add, remove = eline_templates(plat)
        existing = _named(kids, plat, "cli")
        if existing:
            feats = client.get(f"/api/config/scopes/{existing['id']}/features") or []
            apply_feat = next((f for f in feats if f.get("name") == "apply"), feats[0] if feats else None)
            body = (apply_feat or {}).get("add_commands") or ""
            stub = plat == "eos" and "pseudowire ldp" not in body
            empty = not body.strip()
            if apply_feat and (empty or stub):
                client.put(f"/api/config/features/{apply_feat['id']}", _feature_body(add, remove))
                log(f"Updated ELINE CLI features for {plat}")
                continue
            if not apply_feat:
                client.post(
                    f"/api/config/scopes/{existing['id']}/features",
                    _feature_body(add, remove),
                )
                log(f"Filled empty ELINE CLI features for {plat}")
            continue
        obj = client.post(
            "/api/config/scopes",
            {
                "parent_id": type_folder["id"],
                "name": plat,
                "kind": "cli",
                "platform": plat,
                "service_type_id": type_id,
                "payload_kind": "cli",
                "enabled": True,
            },
        )
        client.post(
            f"/api/config/scopes/{obj['id']}/features",
            _feature_body(add, remove),
        )
        log(f"Seeded ELINE CLI for {plat}")


def seed_service_definitions(base_url: str | None = None, token: str | None = None) -> None:
    load_env()
    url = (base_url or env("FACTUM_PUBLIC_BASE_URL") or DEFAULT_URL).rstrip("/")
    if "factum-web:" in url:
        url = DEFAULT_URL
    tok = token or env("FACTUM_API_TOKEN") or TOKEN
    client = FactumClient(url, tok)

    existing = {t["name"]: t for t in (client.get("/api/config/service-types") or [])}
    scopes = client.get("/api/config/scopes") or []
    _remove_stray_eline_cli(client, scopes)

    for spec in DEFINITIONS:
        name = spec["name"]
        if name in existing:
            log(f"Service definition {name} already exists")
            if name == "ELINE":
                _ensure_eline_cli(client, existing[name]["id"])
            continue
        created = client.post("/api/config/service-types", spec)
        log(f"Created service definition {name} (id={created.get('id')})")
        if name == "ELINE":
            _ensure_eline_cli(client, created["id"])


def main() -> None:
    seed_service_definitions()


if __name__ == "__main__":
    main()
