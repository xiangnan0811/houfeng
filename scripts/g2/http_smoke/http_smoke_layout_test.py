"""Layout and preflight checks for the isolated HTTP smoke runner."""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock


HERE = Path(__file__).resolve().parent
CHECKOUT = HERE.parents[2]
sys.path.insert(0, str(HERE))
import bootstrap
import runtime


class SmokeLayoutTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.checkout = Path(self.temp.name) / "checkout"
        self.agent = self.checkout / "tmp" / "agent"
        self.root = self.agent / "owned"
        self.root.mkdir(parents=True, mode=0o700)
        self.scripts = self.checkout / "scripts"
        self.scripts.mkdir()
        self.binaries = Path(self.temp.name) / "bin"
        self.binaries.mkdir()
        for name in bootstrap.BINARY_NAMES:
            (self.binaries / name).write_bytes(b"\x7fELFfixture-" + name.encode())
        self.env = {
            "G2_SMOKE_RUN_ROOT": str(self.root),
            "G2_SMOKE_BIN_DIR": str(self.binaries),
            "HOUFENG_POSTGRES_INTEGRATION": "1",
            "HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER": "fixture",
            "HOUFENG_RECORDS_RUN_ID": "fixture",
            "HOUFENG_DATABASE_URL": "postgres://postgres:password@127.0.0.1:5432/postgres?sslmode=disable",
        }

    def rejected(self, value):
        env = dict(self.env)
        if value is None:
            del env["G2_SMOKE_RUN_ROOT"]
        else:
            env["G2_SMOKE_RUN_ROOT"] = str(value)
        with mock.patch.object(bootstrap, "CHECKOUT", self.checkout), \
             mock.patch.dict(os.environ, env, clear=True), \
             mock.patch.object(sys, "argv", ["bootstrap.py"]), \
             mock.patch.object(bootstrap.subprocess, "run") as invoke, \
             mock.patch.object(bootstrap, "btrfs") as filesystem:
            with self.assertRaises(bootstrap.Failure):
                bootstrap.run()
            invoke.assert_not_called()
            filesystem.assert_not_called()

    def test_missing_root(self):
        self.rejected(None)

    def test_scripts_root(self):
        target = self.scripts / "owned"
        target.mkdir(mode=0o700)
        self.rejected(target)

    def test_outside_agent(self):
        self.rejected(self.checkout)

    def test_symlink_root_and_ancestor(self):
        link = self.agent / "link"
        link.symlink_to(self.root, target_is_directory=True)
        self.rejected(link)
        (self.root / "nested").mkdir(mode=0o700)
        self.rejected(link / "nested")

    def test_wrong_mode(self):
        self.root.chmod(0o750)
        self.rejected(self.root)

    def test_relative_parent_traversal_and_missing_rejected(self):
        self.rejected("relative")
        self.rejected(str(self.agent / "owned" / ".." / "owned"))
        self.rejected(self.agent / "missing")

    def test_wrong_owner(self):
        with mock.patch.object(bootstrap.os, "getuid", return_value=os.getuid() + 1):
            self.rejected(self.root)

    def test_valid_root_confines_output(self):
        before = {p.name: p.read_bytes() for p in HERE.iterdir() if p.is_file()}
        with mock.patch.object(bootstrap, "CHECKOUT", self.checkout), \
             mock.patch.dict(os.environ, self.env, clear=True), \
             mock.patch.object(sys, "argv", ["bootstrap.py"]), \
             mock.patch.object(bootstrap, "btrfs", return_value=True) as filesystem, \
             mock.patch.object(bootstrap, "Proxy", side_effect=bootstrap.Failure("fixture stop")), \
             mock.patch.object(bootstrap.subprocess, "run", return_value=mock.Mock(returncode=0)):
            with self.assertRaisesRegex(bootstrap.Failure, "fixture stop"):
                bootstrap.run()
        filesystem.assert_called_once_with(self.root)
        runs = list(self.root.iterdir())
        self.assertEqual(len(runs), 1)
        run = runs[0]
        self.assertTrue(run.is_dir())
        self.assertTrue(run.name.startswith("run-"))
        self.assertEqual(json.loads((run / "result.json").read_text())["run_dir"], str(run))
        self.assertTrue((run / "private" / "pg.env").is_file())
        self.assertEqual(list(self.scripts.iterdir()), [])
        self.assertEqual({p.name: p.read_bytes() for p in HERE.iterdir() if p.is_file()}, before)

    def test_clamdb_fixed_historical_path_and_missing_failure(self):
        self.assertEqual(runtime.CLAMDB, CHECKOUT / "tmp/agent/g2arc-5u6fzq04/main-http-smoke/clamdb")
        with mock.patch.object(runtime, "CLAMDB", self.agent / "missing-clamdb"), \
             mock.patch.object(runtime.subprocess, "run") as invoke:
            with self.assertRaises(runtime.RuntimeFailure):
                runtime.run(None, None, self.root, 0, "", "", {})
            invoke.assert_not_called()


if __name__ == "__main__":
    unittest.main()
