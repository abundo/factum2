#!/usr/bin/env python3
"""Tests for install.py helpers (tag-pinned installer, archive layout)."""

from __future__ import annotations

import hashlib
import io
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

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
        self.assertGreaterEqual(install.installer_version_of(text), 11)
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


def _installer_script(version: int = 11) -> bytes:
    return (
        "#!/usr/bin/env python3\n"
        f"INSTALLER_VERSION = {version}\n"
        "def main():\n"
        "    return 'factum2'\n"
    ).encode()


def _tgz_bytes(files: dict[str, bytes]) -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name=name)
            info.size = len(data)
            tar.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def _write_tgz(path: Path, files: dict[str, bytes]) -> None:
    path.write_bytes(_tgz_bytes(files))


class _BlobClient:
    """GithubClient stand-in: download() writes named blobs; fetch_file is banned."""

    def __init__(self, blobs: dict[str, bytes]):
        self.blobs = blobs
        self.downloaded: list[str] = []

    def fetch_file(self, path: str, ref: str | None = None) -> bytes:
        raise AssertionError(
            f"self-update must not use Contents API ({path} ref={ref})"
        )

    def download(
        self, asset: install.Asset, dest: Path, progress: bool = True
    ) -> None:
        self.downloaded.append(asset.name)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(self.blobs[asset.name])


def _asset(name: str, blob: bytes, idn: int = 1) -> install.Asset:
    return install.Asset(
        name=name, size=len(blob), url=f"https://example/{name}", api_url="", id=idn
    )


def _release(tag: str, assets: list[install.Asset]) -> install.Release:
    return install.Release(
        tag=tag,
        name=tag,
        published_at="2026-01-01T00:00:00Z",
        prerelease=False,
        draft=False,
        html_url="",
        assets=assets,
    )


class InstallerBytesFromArchiveTests(unittest.TestCase):
    def test_nested_goreleaser_dir(self) -> None:
        payload = _installer_script(12)
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(
                archive,
                {
                    "factum2_1.0.0_linux_amd64/factum2": b"\x00",
                    "factum2_1.0.0_linux_amd64/install.py": payload,
                },
            )
            self.assertEqual(install.installer_bytes_from_archive(archive), payload)

    def test_flat_archive(self) -> None:
        payload = _installer_script(12)
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(archive, {"install.py": payload, "factum2": b"\x00"})
            self.assertEqual(install.installer_bytes_from_archive(archive), payload)

    def test_prefers_shallower_member(self) -> None:
        shallow = _installer_script(12)
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(
                archive,
                {
                    "install.py": shallow,
                    "examples/install.py": _installer_script(1),
                },
            )
            self.assertEqual(install.installer_bytes_from_archive(archive), shallow)

    def test_missing_file_errors(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(archive, {"factum2": b"\x00"})
            with self.assertRaises(install.InstallError) as ctx:
                install.installer_bytes_from_archive(archive)
            self.assertIn("has no install.py", str(ctx.exception))

    def test_rejects_garbage(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(archive, {"install.py": b"print('nope')\n"})
            with self.assertRaises(install.InstallError) as ctx:
                install.installer_bytes_from_archive(archive)
            self.assertIn("does not look like this installer", str(ctx.exception))

    def test_ignores_path_traversal_member(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            archive = Path(raw) / "rel.tar.gz"
            _write_tgz(archive, {"../install.py": _installer_script(12)})
            with self.assertRaises(install.InstallError) as ctx:
                install.installer_bytes_from_archive(archive)
            self.assertIn("has no install.py", str(ctx.exception))


class FetchVerifiedInstallerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.cache = Path(self.tmp.name)
        self.patcher = patch.object(install, "cache_dir", return_value=self.cache)
        self.patcher.start()

    def tearDown(self) -> None:
        self.patcher.stop()
        self.tmp.cleanup()

    def _blobs_for(self, payload: bytes, arch: str = "amd64") -> tuple[
        dict[str, bytes], install.Release, str, str
    ]:
        tgz = _tgz_bytes(
            {
                f"factum2_1.0.0_linux_{arch}/factum2": b"\x00",
                f"factum2_1.0.0_linux_{arch}/install.py": payload,
            }
        )
        tgz_name = f"factum2_1.0.0_linux_{arch}.tar.gz"
        csum_name = "factum2_1.0.0_checksums.txt"
        digest = hashlib.sha256(tgz).hexdigest()
        csum = f"{digest}  {tgz_name}\n".encode()
        blobs = {tgz_name: tgz, csum_name: csum}
        rel = _release(
            "v1.0.0",
            [_asset(tgz_name, tgz, 1), _asset(csum_name, csum, 2)],
        )
        return blobs, rel, tgz_name, csum_name

    def test_reads_install_py_from_checksummed_tarball(self) -> None:
        payload = _installer_script(12)
        blobs, rel, tgz_name, csum_name = self._blobs_for(payload)
        client = _BlobClient(blobs)
        got = install.fetch_verified_installer(client, rel, "amd64")
        self.assertEqual(got, payload)
        self.assertEqual(client.downloaded, [csum_name, tgz_name])

    def test_does_not_call_contents_api(self) -> None:
        payload = _installer_script(12)
        blobs, rel, _, _ = self._blobs_for(payload)
        client = _BlobClient(blobs)
        install.fetch_verified_installer(client, rel, "amd64")
        # _BlobClient.fetch_file raises; reaching here means it was not called.

    def test_reuses_cached_archive(self) -> None:
        payload = _installer_script(12)
        blobs, rel, tgz_name, csum_name = self._blobs_for(payload)
        (self.cache / tgz_name).write_bytes(blobs[tgz_name])
        client = _BlobClient(blobs)
        got = install.fetch_verified_installer(client, rel, "amd64")
        self.assertEqual(got, payload)
        self.assertEqual(client.downloaded, [csum_name])

    def test_mismatch_deletes_archive_and_errors(self) -> None:
        payload = _installer_script(12)
        blobs, rel, tgz_name, csum_name = self._blobs_for(payload)
        blobs[csum_name] = f"{'0' * 64}  {tgz_name}\n".encode()
        rel.assets[1] = _asset(csum_name, blobs[csum_name], 2)
        client = _BlobClient(blobs)
        with self.assertRaises(install.InstallError) as ctx:
            install.fetch_verified_installer(client, rel, "amd64")
        self.assertIn("Checksum mismatch", str(ctx.exception))
        self.assertFalse((self.cache / tgz_name).exists())

    def test_missing_checksums_asset_errors(self) -> None:
        payload = _installer_script(12)
        tgz = _tgz_bytes({"install.py": payload})
        tgz_name = "factum2_1.0.0_linux_amd64.tar.gz"
        rel = _release("v1.0.0", [_asset(tgz_name, tgz)])
        client = _BlobClient({tgz_name: tgz})
        with self.assertRaises(install.InstallError) as ctx:
            install.fetch_verified_installer(client, rel, "amd64")
        self.assertIn("checksums.txt", str(ctx.exception))
        self.assertEqual(client.downloaded, [])

    def test_checksums_missing_archive_entry_errors(self) -> None:
        payload = _installer_script(12)
        tgz = _tgz_bytes({"install.py": payload})
        tgz_name = "factum2_1.0.0_linux_amd64.tar.gz"
        csum_name = "factum2_1.0.0_checksums.txt"
        csum = b"deadbeef  other.tar.gz\n"
        rel = _release(
            "v1.0.0",
            [_asset(tgz_name, tgz, 1), _asset(csum_name, csum, 2)],
        )
        client = _BlobClient({tgz_name: tgz, csum_name: csum})
        with self.assertRaises(install.InstallError) as ctx:
            install.fetch_verified_installer(client, rel, "amd64")
        self.assertIn("has no SHA-256", str(ctx.exception))

    def test_tarball_without_installer_errors(self) -> None:
        tgz = _tgz_bytes({"factum2_1.0.0_linux_amd64/factum2": b"\x00"})
        tgz_name = "factum2_1.0.0_linux_amd64.tar.gz"
        csum_name = "factum2_1.0.0_checksums.txt"
        digest = hashlib.sha256(tgz).hexdigest()
        csum = f"{digest}  {tgz_name}\n".encode()
        rel = _release(
            "v1.0.0",
            [_asset(tgz_name, tgz, 1), _asset(csum_name, csum, 2)],
        )
        client = _BlobClient({tgz_name: tgz, csum_name: csum})
        with self.assertRaises(install.InstallError) as ctx:
            install.fetch_verified_installer(client, rel, "amd64")
        self.assertIn("has no install.py", str(ctx.exception))


class ComposeLabTests(unittest.TestCase):
    def test_compose_implies_source(self) -> None:
        args = install.parse_args(["--compose"])
        self.assertIsNotNone(args.compose)
        # main() fills this in; parse_args leaves source unset
        self.assertIsNone(args.source)

    def test_compose_default_dir(self) -> None:
        args = install.parse_args(["--source", "--compose"])
        self.assertEqual(Path(args.compose), install.COMPOSE_DIR_DEFAULT)

    def test_compose_custom_dir(self) -> None:
        args = install.parse_args(["--compose", "/tmp/lab"])
        self.assertEqual(args.compose, "/tmp/lab")

    def test_compose_argv_missing_script(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            with self.assertRaises(install.InstallError) as ctx:
                install.compose_argv(Path(raw))
            self.assertIn("compose lab not found", str(ctx.exception))

    def test_compose_argv_finds_script(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            script = Path(raw) / "compose.sh"
            script.write_text("#!/bin/sh\n")
            self.assertEqual(install.compose_argv(Path(raw)), [str(script)])

    def test_main_compose_remote_source_errors(self) -> None:
        with self.assertRaises(install.InstallError) as ctx:
            install.main(["--source", "other-host", "--compose"])
        self.assertIn("local only", str(ctx.exception))

    def test_main_compose_dry_run(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            compose_dir = Path(raw)
            (compose_dir / "compose.sh").write_text("#!/bin/sh\n")
            rc = install.main(
                ["--compose", str(compose_dir), "--skip-build", "--dry-run"]
            )
            self.assertEqual(rc, 0)


if __name__ == "__main__":
    unittest.main()
