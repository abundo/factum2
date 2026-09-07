#!/usr/bin/env python3
"""Tests for NetBox YAML seed IP assignment."""

from __future__ import annotations

import unittest
from typing import Any

import netbox_seed
from netbox_seed import (
    Seeder,
    interface_ip_specs,
    merge_interfaces,
    nested_id,
    normalize_ip_address,
)


class HelperTests(unittest.TestCase):
    def test_normalize_adds_host_mask(self) -> None:
        self.assertEqual(normalize_ip_address("192.0.2.1"), "192.0.2.1/32")
        self.assertEqual(normalize_ip_address("2001:db8::1"), "2001:db8::1/128")
        self.assertEqual(normalize_ip_address("192.0.2.10/24"), "192.0.2.10/24")

    def test_normalize_rejects_empty_and_junk(self) -> None:
        with self.assertRaises(SystemExit):
            normalize_ip_address("")
        with self.assertRaises(SystemExit):
            normalize_ip_address("not-an-ip")

    def test_nested_id(self) -> None:
        self.assertEqual(nested_id({"id": 7}), 7)
        self.assertEqual(nested_id(7), 7)
        self.assertIsNone(nested_id(None))
        self.assertIsNone(nested_id({"address": "1.2.3.4/32"}))

    def test_merge_overlays_device_ips_on_template(self) -> None:
        templates = [
            {"name": "Loopback0", "type": "virtual"},
            {"name": "Ethernet1", "type": "10gbase-x-sfpp"},
        ]
        extra = [
            {
                "name": "Loopback0",
                "ip_addresses": [{"address": "192.0.2.1/32", "primary": True}],
            },
            {"name": "Ethernet99", "type": "1000base-t"},
        ]
        merged = merge_interfaces(templates, extra)
        by_name = {row["name"]: row for row in merged}
        self.assertEqual(by_name["Loopback0"]["type"], "virtual")
        self.assertEqual(
            by_name["Loopback0"]["ip_addresses"][0]["address"], "192.0.2.1/32"
        )
        self.assertIn("Ethernet99", by_name)
        self.assertEqual([row["name"] for row in merged], ["Loopback0", "Ethernet1", "Ethernet99"])

    def test_interface_ip_specs_forms(self) -> None:
        mapped = interface_ip_specs(
            {
                "name": "Loopback0",
                "ip_addresses": [
                    {"address": "192.0.2.1/32", "primary": True},
                    "2001:db8::1/128",
                ],
            }
        )
        self.assertEqual(mapped[0]["address"], "192.0.2.1/32")
        self.assertTrue(mapped[0]["primary"])
        self.assertEqual(mapped[1]["address"], "2001:db8::1/128")

        shorthand = interface_ip_specs({"name": "Management1", "ip": "192.0.2.10/24"})
        self.assertEqual(shorthand[0]["address"], "192.0.2.10/24")
        self.assertNotIn("primary", shorthand[0])

        port_primary = interface_ip_specs(
            {"name": "Loopback0", "ip": ["192.0.2.1/32", "2001:db8::1/128"], "primary": True}
        )
        self.assertTrue(all(row.get("primary") for row in port_primary))
        self.assertEqual(len(port_primary), 2)


class RecordingNB:
    def __init__(self) -> None:
        self.dry_run = False
        self.ops: list[tuple[Any, ...]] = []
        self.objects: dict[str, list[dict[str, Any]]] = {}
        self._id = 0

    def _next(self) -> int:
        self._id += 1
        return self._id

    def _match(self, obj: dict[str, Any], params: dict[str, Any]) -> bool:
        aliases = {
            "device_id": "device",
            "site_id": "site",
            "device_type_id": "device_type",
            "manufacturer_id": "manufacturer",
        }
        for key, value in params.items():
            if key == "limit":
                continue
            field = aliases.get(key, key)
            if obj.get(field) != value:
                return False
        return True

    def find(self, collection: str, **params: Any) -> dict[str, Any] | None:
        for obj in self.objects.get(collection, []):
            if self._match(obj, params):
                return obj
        return None

    def post(self, collection: str, payload: dict[str, Any]) -> dict[str, Any]:
        obj = {"id": self._next(), **payload}
        self.objects.setdefault(collection, []).append(obj)
        self.ops.append(("POST", collection, dict(payload)))
        return obj

    def patch(
        self, collection: str, obj_id: int, payload: dict[str, Any], label: str
    ) -> dict[str, Any]:
        self.ops.append(("PATCH", collection, obj_id, dict(payload), label))
        for obj in self.objects.get(collection, []):
            if obj.get("id") == obj_id:
                merged = dict(payload)
                if isinstance(payload.get("custom_fields"), dict) and isinstance(
                    obj.get("custom_fields"), dict
                ):
                    merged["custom_fields"] = {
                        **obj["custom_fields"],
                        **payload["custom_fields"],
                    }
                obj.update(merged)
                return obj
        return {"id": obj_id, **payload}

    def ensure(
        self,
        collection: str,
        lookup: dict[str, Any],
        payload: dict[str, Any],
        label: str,
    ) -> dict[str, Any]:
        found = self.find(collection, **lookup)
        if found:
            return found
        return self.post(collection, payload)


def _inventory(**device_extra: Any) -> dict[str, Any]:
    device = {
        "name": "r0",
        "device_type": "X",
        "manufacturer": "Arista",
        **device_extra,
    }
    return {
        "defaults": {"site": "Lab", "role": "Router", "status": "active"},
        "sites": [{"name": "Lab", "slug": "lab"}],
        "device_roles": [{"name": "Router", "slug": "router", "color": "9e9e9e"}],
        "manufacturers": ["Arista"],
        "device_types": [
            {
                "model": "X",
                "manufacturer": "Arista",
                "interfaces": [
                    {"name": "Loopback0", "type": "virtual"},
                    {"name": "Management1", "type": "1000base-t", "mgmt_only": True},
                ],
            }
        ],
        "devices": [device],
    }


class SeedIPTests(unittest.TestCase):
    def test_assigns_ips_and_primary(self) -> None:
        nb = RecordingNB()
        Seeder(
            nb,  # type: ignore[arg-type]
            _inventory(
                interfaces=[
                    {
                        "name": "Loopback0",
                        "ip_addresses": [
                            {"address": "192.0.2.1/32", "primary": True},
                            {"address": "2001:db8::1/128", "primary": True},
                        ],
                    },
                    {"name": "Management1", "ip": "192.0.2.10/24"},
                ]
            ),
        ).run()

        ips = nb.objects["ipam/ip-addresses"]
        addresses = {row["address"]: row for row in ips}
        self.assertEqual(set(addresses), {"192.0.2.1/32", "2001:db8::1/128", "192.0.2.10/24"})
        lo = next(
            row
            for row in nb.objects["dcim/interfaces"]
            if row["name"] == "Loopback0"
        )
        self.assertEqual(addresses["192.0.2.1/32"]["assigned_object_type"], "dcim.interface")
        self.assertEqual(addresses["192.0.2.1/32"]["assigned_object_id"], lo["id"])
        self.assertNotIn("ip_addresses", lo)
        self.assertNotIn("primary", lo)

        device = nb.objects["dcim/devices"][0]
        self.assertEqual(device["primary_ip4"], addresses["192.0.2.1/32"]["id"])
        self.assertEqual(device["primary_ip6"], addresses["2001:db8::1/128"]["id"])
        patches = [op for op in nb.ops if op[0] == "PATCH"]
        self.assertTrue(any(op[1] == "dcim/devices" for op in patches))

    def test_port_primary_marks_both_families(self) -> None:
        nb = RecordingNB()
        Seeder(
            nb,  # type: ignore[arg-type]
            _inventory(
                interfaces=[
                    {
                        "name": "Loopback0",
                        "primary": True,
                        "ip": ["192.0.2.1", "2001:db8::1"],
                    }
                ]
            ),
        ).run()
        device = nb.objects["dcim/devices"][0]
        self.assertIn("primary_ip4", device)
        self.assertIn("primary_ip6", device)

    def test_duplicate_primary_v4_errors(self) -> None:
        nb = RecordingNB()
        with self.assertRaises(SystemExit) as ctx:
            Seeder(
                nb,  # type: ignore[arg-type]
                _inventory(
                    interfaces=[
                        {
                            "name": "Loopback0",
                            "ip_addresses": [{"address": "192.0.2.1/32", "primary": True}],
                        },
                        {
                            "name": "Management1",
                            "ip_addresses": [{"address": "192.0.2.10/24", "primary": True}],
                        },
                    ]
                ),
            ).run()
        self.assertIn("multiple primary IPv4", str(ctx.exception))

    def test_ip_on_device_type_errors(self) -> None:
        data = _inventory()
        data["device_types"][0]["interfaces"][0]["ip"] = "192.0.2.1/32"
        with self.assertRaises(SystemExit) as ctx:
            Seeder(RecordingNB(), data).run()  # type: ignore[arg-type]
        self.assertIn("belongs on the device", str(ctx.exception))

    def test_existing_unassigned_ip_is_attached(self) -> None:
        nb = RecordingNB()
        nb.objects["ipam/ip-addresses"] = [
            {
                "id": 99,
                "address": "192.0.2.1/32",
                "assigned_object_type": None,
                "assigned_object_id": None,
            }
        ]
        nb._id = 99
        Seeder(
            nb,  # type: ignore[arg-type]
            _inventory(
                interfaces=[
                    {
                        "name": "Loopback0",
                        "ip_addresses": [{"address": "192.0.2.1/32", "primary": True}],
                    }
                ]
            ),
        ).run()
        patches = [op for op in nb.ops if op[0] == "PATCH" and op[1] == "ipam/ip-addresses"]
        self.assertEqual(len(patches), 1)
        self.assertEqual(patches[0][2], 99)
        self.assertEqual(patches[0][3]["assigned_object_type"], "dcim.interface")
        device = nb.objects["dcim/devices"][0]
        self.assertEqual(device["primary_ip4"], 99)

    def test_ip_assigned_elsewhere_errors(self) -> None:
        nb = RecordingNB()
        nb.objects["ipam/ip-addresses"] = [
            {
                "id": 5,
                "address": "192.0.2.1/32",
                "assigned_object_type": "dcim.interface",
                "assigned_object_id": 12345,
            }
        ]
        with self.assertRaises(SystemExit) as ctx:
            Seeder(
                nb,  # type: ignore[arg-type]
                _inventory(
                    interfaces=[
                        {
                            "name": "Loopback0",
                            "ip_addresses": [{"address": "192.0.2.1/32", "primary": True}],
                        }
                    ]
                ),
            ).run()
        self.assertIn("already assigned", str(ctx.exception))

    def test_example_yaml_parses(self) -> None:
        data = netbox_seed.load_yaml(netbox_seed.EXAMPLE_YAML)
        r0 = next(d for d in data["devices"] if d["name"] == "lu17-lab-r0")
        lo = next(i for i in r0["interfaces"] if i["name"] == "Loopback0")
        self.assertTrue(any(a.get("primary") for a in lo["ip_addresses"]))


class SeedCustomFieldTests(unittest.TestCase):
    def test_create_sets_dest_sync_flags(self) -> None:
        nb = RecordingNB()
        Seeder(nb, _inventory()).run()  # type: ignore[arg-type]
        device = nb.objects["dcim/devices"][0]
        self.assertEqual(
            device["custom_fields"],
            {
                "backup_oxidized": True,
                "monitor_grafana": True,
                "monitor_icinga": True,
                "monitor_librenms": True,
            },
        )
        posts = [op for op in nb.ops if op[0] == "POST" and op[1] == "dcim/devices"]
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0][2]["custom_fields"]["backup_oxidized"], True)
        patches = [
            op
            for op in nb.ops
            if op[0] == "PATCH" and op[1] == "dcim/devices" and "custom fields" in op[4]
        ]
        self.assertEqual(patches, [])

    def test_yaml_overrides_sync_flag(self) -> None:
        nb = RecordingNB()
        Seeder(
            nb,  # type: ignore[arg-type]
            _inventory(custom_fields={"monitor_icinga": False, "location": "lab"}),
        ).run()
        cf = nb.objects["dcim/devices"][0]["custom_fields"]
        self.assertFalse(cf["monitor_icinga"])
        self.assertTrue(cf["backup_oxidized"])
        self.assertEqual(cf["location"], "lab")

    def test_existing_device_gets_missing_sync_flags(self) -> None:
        nb = RecordingNB()
        data = _inventory()
        Seeder(nb, data).run()  # type: ignore[arg-type]
        device = nb.objects["dcim/devices"][0]
        device["custom_fields"] = {"backup_oxidized": False, "location": "rack-1"}
        Seeder(nb, data).run()  # type: ignore[arg-type]
        patches = [
            op
            for op in nb.ops
            if op[0] == "PATCH" and op[1] == "dcim/devices" and "custom fields" in op[4]
        ]
        self.assertEqual(len(patches), 1)
        self.assertEqual(
            patches[0][3]["custom_fields"],
            {
                "backup_oxidized": True,
                "monitor_grafana": True,
                "monitor_icinga": True,
                "monitor_librenms": True,
            },
        )
        self.assertEqual(device["custom_fields"]["location"], "rack-1")
        self.assertTrue(device["custom_fields"]["backup_oxidized"])


if __name__ == "__main__":
    unittest.main()
