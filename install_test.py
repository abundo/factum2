#!/usr/bin/env python3
"""Tests for install.py helpers (tag-pinned installer, archive layout)."""

from __future__ import annotations

import base64
import hashlib
import io
import re
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import warnings
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
        self.assertGreaterEqual(install.installer_version_of(text), 16)
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


class SameStampTests(unittest.TestCase):
    def test_v_prefix(self) -> None:
        self.assertTrue(install.same_stamp("v1.0.6", "1.0.6"))
        self.assertTrue(install.same_stamp("1.0.6", "v1.0.6"))
        self.assertTrue(install.same_stamp("v1.0.6", "v1.0.6"))
        self.assertTrue(install.same_stamp("1.0.6", "1.0.6"))
        self.assertTrue(install.same_stamp("v1.0.6-3-gdeadbee", "1.0.6-3-gdeadbee"))

    def test_git_describe_still_distinct(self) -> None:
        self.assertFalse(install.same_stamp("v1.0.6", "v1.0.6-3-gdeadbee"))
        self.assertFalse(install.same_stamp("1.0.6", "v1.0.7"))

    def test_empty(self) -> None:
        self.assertFalse(install.same_stamp("", "1.0.6"))
        self.assertFalse(install.same_stamp("v1.0.6", ""))


class ShouldRunPinnedInstallerTests(unittest.TestCase):
    def test_identical_stays_in_process(self) -> None:
        body = _installer_script(16)
        self.assertFalse(install.should_run_pinned_installer(body, body))

    def test_newer_local_stays_in_process(self) -> None:
        self.assertFalse(
            install.should_run_pinned_installer(
                _installer_script(16), _installer_script(15)
            )
        )

    def test_older_local_yields_to_tarball(self) -> None:
        self.assertTrue(
            install.should_run_pinned_installer(
                _installer_script(15), _installer_script(16)
            )
        )

    def test_same_version_different_content_yields(self) -> None:
        a = _installer_script(15)
        b = _installer_script(15) + b"# different\n"
        self.assertTrue(install.should_run_pinned_installer(a, b))


class ExtractStampedVersionTests(unittest.TestCase):
    def test_cobra_git_describe(self) -> None:
        self.assertEqual(
            install.extract_stamped_version(
                "factum2-worker version v1.2.3-4-gabcdef"
            ),
            "v1.2.3-4-gabcdef",
        )

    def test_cobra_dirty(self) -> None:
        self.assertEqual(
            install.extract_stamped_version(
                "factum2-worker version v1.2.3-4-gabcdef-dirty\n"
            ),
            "v1.2.3-4-gabcdef-dirty",
        )

    def test_clean_tag(self) -> None:
        self.assertEqual(
            install.extract_stamped_version("factum2-web version v1.0.0"),
            "v1.0.0",
        )

    def test_goreleaser_omits_v(self) -> None:
        self.assertEqual(
            install.extract_stamped_version("factum2-worker version 1.0.6"),
            "1.0.6",
        )

    def test_version_file_line(self) -> None:
        self.assertEqual(
            install.extract_stamped_version("v1.0.0-3-gdeadbee\n"),
            "v1.0.0-3-gdeadbee",
        )

    def test_dev_token(self) -> None:
        self.assertEqual(install.extract_stamped_version("dev\n"), "dev")

    def test_empty(self) -> None:
        self.assertIsNone(install.extract_stamped_version(""))
        self.assertIsNone(install.extract_stamped_version("   "))


class ScpUrlTests(unittest.TestCase):
    def test_hostname(self) -> None:
        self.assertEqual(
            install.scp_url("root", "dns1.example.com", "/opt/factum2/"),
            "root@dns1.example.com:/opt/factum2/",
        )

    def test_ipv6_is_bracketed(self) -> None:
        self.assertEqual(
            install.scp_url("root", "2001:db8::10", "/opt/factum2/VERSION"),
            "root@[2001:db8::10]:/opt/factum2/VERSION",
        )

    def test_already_bracketed(self) -> None:
        self.assertEqual(
            install.scp_url("root", "[2001:db8::10]", "/x"),
            "root@[2001:db8::10]:/x",
        )


class RefuseInstallWithoutWorkersTests(unittest.TestCase):
    def test_ok_when_lookup_succeeded(self) -> None:
        install.refuse_install_without_workers(None, primary_only=False)

    def test_ok_when_primary_only(self) -> None:
        install.refuse_install_without_workers("db down", primary_only=True)

    def test_raises_on_lookup_failure(self) -> None:
        with self.assertRaises(install.InstallError) as ctx:
            install.refuse_install_without_workers(
                "worker_nodes lookup failed: connection refused",
                primary_only=False,
            )
        self.assertIn("--primary-only", str(ctx.exception))
        self.assertIn("connection refused", str(ctx.exception))


class VerifyHostVersionTests(unittest.TestCase):
    def test_remote_match(self) -> None:
        def fake_run(cmd, **kwargs):
            return subprocess.CompletedProcess(
                cmd, 0, stdout="factum2-worker version v1.2.3-4-gabcdef\n", stderr=""
            )

        with patch.object(install.subprocess, "run", fake_run):
            install.verify_host_version(
                "v1.2.3-4-gabcdef",
                target_host="icinga.example.com",
                ssh_user="root",
                install_dir=Path("/opt/factum2"),
            )

    def test_remote_mismatch(self) -> None:
        def fake_run(cmd, **kwargs):
            return subprocess.CompletedProcess(
                cmd, 0, stdout="factum2-worker version v0.9.0\n", stderr=""
            )

        with patch.object(install.subprocess, "run", fake_run):
            with self.assertRaises(install.InstallError) as ctx:
                install.verify_host_version(
                    "v1.2.3-4-gabcdef",
                    target_host="icinga.example.com",
                    ssh_user="root",
                    install_dir=Path("/opt/factum2"),
                )
        self.assertIn("v0.9.0", str(ctx.exception))
        self.assertIn("v1.2.3-4-gabcdef", str(ctx.exception))

    def test_remote_v_prefix_vs_goreleaser(self) -> None:
        def fake_run(cmd, **kwargs):
            return subprocess.CompletedProcess(
                cmd, 0, stdout="factum2-worker version 1.0.6\n", stderr=""
            )

        with patch.object(install.subprocess, "run", fake_run):
            install.verify_host_version(
                "v1.0.6",
                target_host="librenms.example.com",
                ssh_user="root",
                install_dir=Path("/opt/factum2"),
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


class PsqlPasswordArgvTests(unittest.TestCase):
    """DB password must not appear in subprocess argv (visible to `ps`)."""

    _DB = {
        "host": "127.0.0.1",
        "port": "5432",
        "user": "factum2",
        "pass": "s3cr$et; rm -rf / && echo 'pwned'",
        "database": "factum2",
    }

    def test_remote_password_not_in_ssh_argv(self) -> None:
        captured: dict[str, object] = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            captured["kwargs"] = kwargs
            return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

        with patch.object(install.subprocess, "run", fake_run):
            install._psql_remote(
                self._DB,
                "select 1;",
                target_host="primary.example",
                ssh_user="root",
            )

        cmd = captured["cmd"]
        assert isinstance(cmd, list)
        self.assertEqual(cmd[-2:], ["bash", "-s"])
        self.assertNotIn("--", cmd)
        joined = " ".join(str(part) for part in cmd)
        self.assertNotIn(self._DB["pass"], joined)

        kwargs = captured["kwargs"]
        assert isinstance(kwargs, dict)
        script = kwargs["input"]
        assert isinstance(script, str)
        self.assertNotIn(self._DB["pass"], script)
        self.assertIn("-e PGPASSWORD db", script)
        self.assertNotIn("-e PGPASSWORD=", script)
        blobs = re.findall(r"printf '%s' '([A-Za-z0-9+/=]+)'", script)
        decoded = [base64.b64decode(b).decode() for b in blobs]
        self.assertIn(self._DB["pass"], decoded)
        self.assertIn("select 1;", decoded)

    def test_compose_password_not_in_docker_argv(self) -> None:
        captured: dict[str, object] = {}

        def fake_run(cmd, **kwargs):
            captured["cmd"] = cmd
            captured["kwargs"] = kwargs
            return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

        with patch.object(install.subprocess, "run", fake_run):
            install._psql_via_compose(Path("/opt/postgresql/compose.yaml"), self._DB, "select 1;")

        cmd = captured["cmd"]
        assert isinstance(cmd, list)
        joined = " ".join(str(part) for part in cmd)
        self.assertNotIn(self._DB["pass"], joined)
        self.assertIn("-e", cmd)
        self.assertIn("PGPASSWORD", cmd)
        self.assertNotIn(f"PGPASSWORD={self._DB['pass']}", cmd)

        kwargs = captured["kwargs"]
        assert isinstance(kwargs, dict)
        env = kwargs["env"]
        assert isinstance(env, dict)
        self.assertEqual(env["PGPASSWORD"], self._DB["pass"])


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


def _tgz_from_infos(entries: list[tuple[tarfile.TarInfo, bytes | None]]) -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for info, payload in entries:
            if payload is None:
                tar.addfile(info)
            else:
                info.size = len(payload)
                tar.addfile(info, io.BytesIO(payload))
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


class ExtractArchiveTests(unittest.TestCase):
    def test_flat_root(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            _write_tgz(archive, {"factum2": b"\x00", "install.py": b"x"})
            root = install.extract_archive(archive, dest)
            self.assertEqual(root, dest)
            self.assertTrue((dest / "factum2").is_file())

    def test_nested_goreleaser_dir(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            _write_tgz(
                archive,
                {"factum2_1.0.0_linux_amd64/factum2": b"\x00"},
            )
            root = install.extract_archive(archive, dest)
            self.assertEqual(root, dest / "factum2_1.0.0_linux_amd64")
            self.assertTrue((root / "factum2").is_file())

    def test_missing_binaries_errors(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            _write_tgz(archive, {"README.md": b"hi"})
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Could not find factum2 binaries", str(ctx.exception))

    def test_rejects_parent_path(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            outside = base / "pwned"
            info = tarfile.TarInfo(name="../pwned")
            archive.write_bytes(_tgz_from_infos([(info, b"evil")]))
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Unsafe path", str(ctx.exception))
            self.assertFalse(outside.exists())
            self.assertFalse(dest.exists() and any(dest.iterdir()))

    def test_rejects_absolute_path(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            outside = base / "abs_pwned"
            info = tarfile.TarInfo(name=str(outside))
            archive.write_bytes(_tgz_from_infos([(info, b"evil")]))
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Unsafe path", str(ctx.exception))
            self.assertFalse(outside.exists())

    def test_rejects_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            info = tarfile.TarInfo(name="link")
            info.type = tarfile.SYMTYPE
            info.linkname = "../pwned"
            archive.write_bytes(_tgz_from_infos([(info, None)]))
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Refusing link", str(ctx.exception))
            self.assertFalse((base / "pwned").exists())

    def test_rejects_hardlink(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            info = tarfile.TarInfo(name="hlink")
            info.type = tarfile.LNKTYPE
            info.linkname = "../pwned"
            archive.write_bytes(_tgz_from_infos([(info, None)]))
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Refusing link", str(ctx.exception))

    def test_rejects_fifo(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            base = Path(raw)
            archive = base / "rel.tar.gz"
            dest = base / "dest"
            info = tarfile.TarInfo(name="pipe")
            info.type = tarfile.FIFOTYPE
            archive.write_bytes(_tgz_from_infos([(info, None)]))
            with self.assertRaises(install.InstallError) as ctx:
                install.extract_archive(archive, dest)
            self.assertIn("Refusing special file", str(ctx.exception))

    def test_parent_path_without_data_filter(self) -> None:
        with patch.object(tarfile, "data_filter", None), warnings.catch_warnings():
            warnings.simplefilter("ignore", DeprecationWarning)
            self.test_rejects_parent_path()
            self.test_rejects_absolute_path()
            self.test_rejects_symlink()

    def test_extracts_without_data_filter(self) -> None:
        with patch.object(tarfile, "data_filter", None), warnings.catch_warnings():
            warnings.simplefilter("ignore", DeprecationWarning)
            self.test_flat_root()
            self.test_nested_goreleaser_dir()


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

    def test_compose_lab_restarts_running_dest_workers(self) -> None:
        """up -d does not re-exec bind-mounted binaries; dests must restart."""
        with tempfile.TemporaryDirectory() as raw:
            repo = Path(raw) / "repo"
            compose_dir = Path(raw) / "dev"
            repo.mkdir()
            compose_dir.mkdir()
            (compose_dir / "compose.sh").write_text("#!/bin/sh\n")
            (compose_dir / "factum2.yaml").write_text("db: {}\n")
            build = repo / "build"
            build.mkdir()
            for name in install.KNOWN_BINARIES:
                (build / name).write_bytes(b"x")

            calls: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                calls.append(list(cmd))
                return subprocess.CompletedProcess(cmd, 0, "", "")

            running = {
                "postgres",
                "factum-web",
                "factum-worker",
                "dns",
                "icinga",
                "librenms",
            }

            def fake_running(_compose_dir: Path, service: str) -> bool:
                return service in running

            with (
                patch.object(install, "run", fake_run),
                patch.object(install, "compose_service_running", fake_running),
                patch.object(
                    install, "git_describe", return_value="v1.0.5-22-g40dc744"
                ),
            ):
                rc = install.install_compose_lab(
                    repo, compose_dir, skip_build=True, dry_run=False
                )
            self.assertEqual(rc, 0)

            stop = next(c for c in calls if "stop" in c)
            self.assertEqual(stop[-2:], ["stop", "factum-web"])

            restart = next(c for c in calls if "restart" in c)
            self.assertEqual(
                restart[restart.index("restart") + 1 :],
                ["dns", "icinga", "librenms"],
            )
            self.assertNotIn("factum-web", restart)
            self.assertNotIn("factum-worker", restart)
            self.assertNotIn("oxidized", restart)
            self.assertNotIn("prometheus", restart)

            recreate = next(c for c in calls if "--force-recreate" in c)
            self.assertEqual(
                recreate[recreate.index("up") :],
                ["up", "-d", "--no-deps", "--force-recreate", "factum-worker"],
            )

            up = next(
                c
                for c in calls
                if "up" in c and "--force-recreate" not in c
            )
            self.assertEqual(
                up[-len(install.COMPOSE_FACTUM_SERVICES) :],
                list(install.COMPOSE_FACTUM_SERVICES),
            )
            self.assertLess(calls.index(restart), calls.index(recreate))
            self.assertLess(calls.index(recreate), calls.index(up))

    def test_compose_lab_starts_without_restart_when_dests_down(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            repo = Path(raw) / "repo"
            compose_dir = Path(raw) / "dev"
            repo.mkdir()
            compose_dir.mkdir()
            (compose_dir / "compose.sh").write_text("#!/bin/sh\n")
            (compose_dir / "factum2.yaml").write_text("db: {}\n")
            build = repo / "build"
            build.mkdir()
            for name in install.KNOWN_BINARIES:
                (build / name).write_bytes(b"x")

            calls: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                calls.append(list(cmd))
                return subprocess.CompletedProcess(cmd, 0, "", "")

            with (
                patch.object(install, "run", fake_run),
                patch.object(
                    install,
                    "compose_service_running",
                    side_effect=lambda _d, s: s == "postgres",
                ),
                patch.object(install, "git_describe", return_value="v1.0.0"),
            ):
                rc = install.install_compose_lab(
                    repo, compose_dir, skip_build=True, dry_run=False
                )
            self.assertEqual(rc, 0)
            self.assertFalse(any("restart" in c for c in calls))
            recreate = next(c for c in calls if "--force-recreate" in c)
            self.assertEqual(
                recreate[recreate.index("up") :],
                ["up", "-d", "--no-deps", "--force-recreate", "factum-worker"],
            )
            self.assertTrue(any("up" in c for c in calls))

    _PODMAN_PS = """\
CONTAINER ID  IMAGE                                     COMMAND               CREATED      STATUS                PORTS                                                                                 NAMES
93e291465cdb  docker.io/library/postgres:18-alpine      postgres              2 hours ago  Up 2 hours (healthy)  0.0.0.0:15432->5432/tcp                                                               factum-dev_postgres_1
eddfdc49f628  docker.io/icinga/icinga2:2.16.5                                 2 hours ago  Up 2 hours (healthy)  0.0.0.0:15665->5665/tcp, 0.0.0.0:18445->8443/tcp                                      factum-dev_icinga_1
00c9c669709c  docker.io/icinga/icingadb:1.5.1           icingadb --databa...  2 hours ago  Up 2 hours                                                                                                  factum-dev_icingadb_1
dbed178ee341  docker.io/icinga/icingaweb2:2.14.0        bash -eo pipefail...  2 hours ago  Up 2 hours (healthy)  0.0.0.0:18002->8080/tcp                                                               factum-dev_icingaweb_1
b90c76d2b92e  localhost/factum-dev:local                /opt/factum2/fact...  2 hours ago  Up 2 hours (healthy)  0.0.0.0:18091->8091/tcp                                                               factum-dev_factum-web_1
73d225bab149  localhost/factum-dev:local                /opt/factum2/fact...  2 hours ago  Up 2 hours (healthy)  0.0.0.0:18443->8443/tcp                                                               factum-dev_factum-worker_1
"""

    _DOCKER_PS = """\
NAME                           IMAGE                               COMMAND                  SERVICE             CREATED        STATUS                  PORTS
factum-dev-postgres-1          postgres:18-alpine                  "docker-entrypoint.s…"   postgres            2 hours ago    Up 2 hours (healthy)    0.0.0.0:15432->5432/tcp
factum-dev-icinga-1            icinga/icinga2:2.16.5               "/entrypoint.sh"         icinga              2 hours ago    Up 2 hours (healthy)    0.0.0.0:15665->5665/tcp
factum-dev-icingadb-1          icinga/icingadb:1.5.1               "icingadb"               icingadb            2 hours ago    Up 2 hours
factum-dev-factum-web-1        factum-dev:local                    "/opt/factum2/factum…"   factum-web          2 hours ago    Up 2 hours (healthy)    0.0.0.0:18091->8091/tcp
factum-dev-factum-worker-1     factum-dev:local                    "/opt/factum2/factum…"   factum-worker       2 hours ago    Up 2 hours (healthy)    0.0.0.0:18443->8443/tcp
"""

    def test_compose_ps_has_service_podman_names(self) -> None:
        self.assertTrue(install.compose_ps_has_service(self._PODMAN_PS, "postgres"))
        self.assertTrue(install.compose_ps_has_service(self._PODMAN_PS, "factum-web"))
        self.assertTrue(install.compose_ps_has_service(self._PODMAN_PS, "factum-worker"))
        self.assertTrue(install.compose_ps_has_service(self._PODMAN_PS, "icinga"))
        self.assertFalse(install.compose_ps_has_service(self._PODMAN_PS, "missing"))
        self.assertFalse(
            install.compose_ps_has_service(
                self._PODMAN_PS.replace("factum-dev_icinga_1", "gone"), "icinga"
            )
        )

    def test_compose_ps_has_service_docker_names(self) -> None:
        self.assertTrue(install.compose_ps_has_service(self._DOCKER_PS, "postgres"))
        self.assertTrue(install.compose_ps_has_service(self._DOCKER_PS, "factum-web"))
        self.assertTrue(install.compose_ps_has_service(self._DOCKER_PS, "factum-worker"))
        self.assertTrue(install.compose_ps_has_service(self._DOCKER_PS, "icinga"))
        self.assertFalse(install.compose_ps_has_service(self._DOCKER_PS, "missing"))

    def test_compose_service_running_docker_ps_q(self) -> None:
        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(cmd)
            return subprocess.CompletedProcess(cmd, 0, stdout="abc123\n", stderr="")

        with tempfile.TemporaryDirectory() as raw:
            (Path(raw) / "compose.sh").write_text("#!/bin/sh\n")
            with patch.object(install.subprocess, "run", fake_run):
                self.assertTrue(
                    install.compose_service_running(Path(raw), "postgres")
                )
        self.assertEqual(calls[0][-3:], ["ps", "-q", "postgres"])
        self.assertEqual(len(calls), 1)

    def test_compose_service_running_podman_fallback(self) -> None:
        def fake_run(cmd, **kwargs):
            if cmd[-3:] == ["ps", "-q", "postgres"]:
                return subprocess.CompletedProcess(
                    cmd,
                    2,
                    stdout="",
                    stderr="podman-compose: error: unrecognized arguments: postgres\n",
                )
            if cmd[-1] == "ps":
                return subprocess.CompletedProcess(
                    cmd, 0, stdout=self._PODMAN_PS, stderr=""
                )
            raise AssertionError(cmd)

        with tempfile.TemporaryDirectory() as raw:
            (Path(raw) / "compose.sh").write_text("#!/bin/sh\n")
            with patch.object(install.subprocess, "run", fake_run):
                self.assertTrue(
                    install.compose_service_running(Path(raw), "postgres")
                )

    def test_compose_service_running_podman_down(self) -> None:
        def fake_run(cmd, **kwargs):
            if cmd[-3:] == ["ps", "-q", "postgres"]:
                return subprocess.CompletedProcess(cmd, 2, stdout="", stderr="err\n")
            if cmd[-1] == "ps":
                return subprocess.CompletedProcess(
                    cmd, 0, stdout="CONTAINER ID  NAMES\n", stderr=""
                )
            raise AssertionError(cmd)

        with tempfile.TemporaryDirectory() as raw:
            (Path(raw) / "compose.sh").write_text("#!/bin/sh\n")
            with patch.object(install.subprocess, "run", fake_run):
                self.assertFalse(
                    install.compose_service_running(Path(raw), "postgres")
                )


if __name__ == "__main__":
    unittest.main()
