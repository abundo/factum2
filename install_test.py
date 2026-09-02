#!/usr/bin/env python3
"""Tests for install.py helpers (tag-pinned installer, archive layout)."""

from __future__ import annotations

import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import install


class PinnedChildArgvTests(unittest.TestCase):
    def test_adds_install_yes_and_skip_self_update(self) -> None:
        self.assertEqual(
            install.pinned_child_argv([], "v1.0.1"),
            ["--skip-self-update", "--yes", "--install", "v1.0.1"],
        )

    def test_replaces_install_latest_with_resolved_tag(self) -> None:
        self.assertEqual(
            install.pinned_child_argv(["--install", "latest", "--dry-run"], "v1.0.2"),
            ["--dry-run", "--skip-self-update", "--yes", "--install", "v1.0.2"],
        )

    def test_drops_self_update_keeps_other_flags(self) -> None:
        self.assertEqual(
            install.pinned_child_argv(
                ["--repo", "abundo/factum2", "--self-update", "-y", "--pre"],
                "v1.0.1",
            ),
            [
                "--repo",
                "abundo/factum2",
                "-y",
                "--pre",
                "--skip-self-update",
                "--install",
                "v1.0.1",
            ],
        )

    def test_install_equals_form(self) -> None:
        self.assertEqual(
            install.pinned_child_argv(["--install=v9.9.9"], "v1.0.1"),
            ["--skip-self-update", "--yes", "--install", "v1.0.1"],
        )


class RootsFromWorkTests(unittest.TestCase):
    def test_flat_arch_dir(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            work = Path(raw)
            arch = work / "amd64"
            arch.mkdir()
            (arch / "factum2-web").write_bytes(b"\x00")
            self.assertEqual(install.roots_from_work(work), {"amd64": arch})

    def test_nested_goreleaser_dir(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            work = Path(raw)
            nested = work / "amd64" / "factum2_1.0.3_linux_amd64"
            nested.mkdir(parents=True)
            (nested / "factum2").write_bytes(b"\x00")
            self.assertEqual(install.roots_from_work(work), {"amd64": nested})

    def test_missing_binaries_errors(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            work = Path(raw)
            (work / "amd64").mkdir()
            with self.assertRaises(install.InstallError):
                install.roots_from_work(work)


class ReleaseInstallerLoadTests(unittest.TestCase):
    def test_bundled_file_wins(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            body = Path("install.py").read_bytes()
            (root / "install.py").write_bytes(body)

            class _Client:
                def fetch_file(self, path: str, ref: str | None = None) -> bytes:
                    raise AssertionError(
                        "must not hit GitHub when tarball has install.py"
                    )

            got = install.load_release_installer(root, _Client(), "v1.0.3")  # type: ignore[arg-type]
            self.assertEqual(got, body)

    def test_rejects_garbage_file(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            (root / "install.py").write_text("print('nope')\n", encoding="utf-8")

            class _Client:
                def fetch_file(self, path: str, ref: str | None = None) -> bytes:
                    raise install.InstallError("404")

            self.assertIsNone(
                install.load_release_installer(root, _Client(), "v1.0.0")  # type: ignore[arg-type]
            )


class InstallerVersionTests(unittest.TestCase):
    def test_current_file_parses(self) -> None:
        text = Path("install.py").read_text(encoding="utf-8")
        self.assertGreaterEqual(install.installer_version_of(text), 10)
        self.assertTrue(install.looks_like_installer(text))


class AddressHostTests(unittest.TestCase):
    def test_hostname_port(self) -> None:
        self.assertEqual(install.address_host("dns1.example.com:8443"), "dns1.example.com")

    def test_ipv4_port(self) -> None:
        self.assertEqual(install.address_host("192.0.2.10:8443"), "192.0.2.10")

    def test_ipv6_port(self) -> None:
        self.assertEqual(install.address_host("[2001:db8::1]:8443"), "2001:db8::1")

    def test_hostname_only(self) -> None:
        self.assertEqual(install.address_host("icinga"), "icinga")


class WorkerHostsTests(unittest.TestCase):
    def test_skips_local_and_duplicates(self) -> None:
        self.assertEqual(
            install.worker_hosts(
                [
                    "dns1.example.com:8443",
                    "127.0.0.1:8443",
                    "dns1.example.com:9443",
                    "icinga.example.com:8443",
                    "localhost:8443",
                ],
                skip=["icinga.example.com"],
            ),
            ["dns1.example.com"],
        )

    def test_ipv6(self) -> None:
        self.assertEqual(
            install.worker_hosts(["[2001:db8::10]:8443"]),
            ["2001:db8::10"],
        )


class SanEntryTests(unittest.TestCase):
    def test_dns_and_ip(self) -> None:
        self.assertEqual(
            install.san_entries(["dns1.example.com", "192.0.2.10", "dns1.example.com"]),
            ["DNS:dns1.example.com", "IP:192.0.2.10"],
        )

    def test_ipv6_strips_brackets(self) -> None:
        self.assertEqual(install.san_entries(["[2001:db8::1]"]), ["IP:2001:db8::1"])


class StoreHubCATests(unittest.TestCase):
    def test_full_install_stores(self) -> None:
        self.assertTrue(
            install.should_store_hub_ca(primary_only=False, worker_err=None)
        )

    def test_primary_only_skips(self) -> None:
        self.assertFalse(
            install.should_store_hub_ca(primary_only=True, worker_err=None)
        )

    def test_lookup_error_skips(self) -> None:
        self.assertFalse(
            install.should_store_hub_ca(primary_only=False, worker_err="db down")
        )


class DollarQuoteTests(unittest.TestCase):
    def test_wraps_pem(self) -> None:
        pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
        self.assertEqual(install.dollar_quote(pem), f"$hubca${pem}$hubca$")

    def test_rotates_tag_when_present(self) -> None:
        value = "has $hubca$ inside"
        quoted = install.dollar_quote(value)
        self.assertTrue(quoted.startswith("$hubca"))
        self.assertNotEqual(quoted, f"$hubca${value}$hubca$")
        self.assertIn(value, quoted)


class HubCertTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("openssl"), "openssl not on PATH")
    def test_ca_signs_leaf_with_san(self) -> None:
        ca_pem, ca_key = install.generate_hub_ca(days=1)
        cert_pem, key_pem = install.issue_hub_leaf(
            ca_pem, ca_key, ["worker.example.com", "192.0.2.10"], days=1
        )
        self.assertIn(b"BEGIN CERTIFICATE", ca_pem)
        self.assertIn(b"BEGIN CERTIFICATE", cert_pem)
        self.assertIn(b"PRIVATE KEY", ca_key)
        self.assertIn(b"PRIVATE KEY", key_pem)
        with tempfile.TemporaryDirectory() as raw:
            work = Path(raw)
            (work / "ca.crt").write_bytes(ca_pem)
            (work / "leaf.crt").write_bytes(cert_pem)
            proc = subprocess.run(
                [
                    "openssl",
                    "verify",
                    "-CAfile",
                    str(work / "ca.crt"),
                    str(work / "leaf.crt"),
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(proc.returncode, 0, proc.stderr)
            text = subprocess.run(
                ["openssl", "x509", "-in", str(work / "leaf.crt"), "-noout", "-text"],
                check=True,
                capture_output=True,
                text=True,
            ).stdout
            self.assertIn("DNS:worker.example.com", text)
            self.assertIn("192.0.2.10", text)


if __name__ == "__main__":
    unittest.main()
