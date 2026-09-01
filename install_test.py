#!/usr/bin/env python3
"""Tests for install.py helpers (tag-pinned installer, archive layout)."""

from __future__ import annotations

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
        self.assertGreaterEqual(install.installer_version_of(text), 9)
        self.assertTrue(install.looks_like_installer(text))


if __name__ == "__main__":
    unittest.main()
