#!/usr/bin/env python3
"""Isolated SQLite and filesystem tests for the offline conversion tool."""
import fcntl
import importlib.util
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("migration", Path(__file__).with_name("migrate-multi-user.py"))
migration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(migration)


class ConversionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.data = self.root / "data"
        self.data.mkdir()
        self.db = self.data / "runtime.db"
        self.config = self.root / "server.json"
        self.config.write_text(json.dumps({"data_dir": str(self.data)}))
        self.backup = self.root / "backup"
        self.account = {"Account": {"ID": "original", "Username": "owner", "DisplayName": "My profile", "AvatarFileID": "avatar", "MFAEnabled": True}, "Version": 7, "PasswordHash": "preserved-hash", "EncryptedTOTP": "YWJj", "RecoveryDigests": ["digest"]}
        with sqlite3.connect(self.db) as c:
            c.execute("PRAGMA journal_mode=WAL")
            c.execute("CREATE TABLE records(bucket TEXT NOT NULL,key TEXT NOT NULL,value BLOB NOT NULL,PRIMARY KEY(bucket,key))")
            migration.put(c, "auth.accounts", "owner", self.account)
            for bucket in ["auth.sessions", "auth.tokens", "auth.login_links", "business.resources", "credentials", "im.messages", "agent.checkpoints"]:
                migration.put(c, bucket, "original/item", {"unchanged": bucket})
        (self.data / "keys").mkdir()
        (self.data / "keys/master.key").write_bytes(bytes(range(32)))
        (self.data / "files").mkdir()
        (self.data / "files/file").write_bytes(b"private attachment")
        (self.data / "files/link").symlink_to("file")

    def rows(self, path=None):
        with sqlite3.connect(path or self.db) as c:
            return c.execute("SELECT bucket,key,value FROM records ORDER BY bucket,key").fetchall()

    def test_conversion_preserves_identity_security_and_private_data_and_repeats_safely(self):
        before = self.rows()
        result = migration.migrate(self.config, self.backup)
        self.assertEqual(result["status"], "converted")
        self.assertEqual(self.rows(self.backup / "data/runtime.db"), before)
        for name in ["keys/master.key", "files/file"]:
            self.assertEqual((self.backup / "data" / name).read_bytes(), (self.data / name).read_bytes())
        self.assertTrue((self.backup / "data/files/link").is_symlink())
        with sqlite3.connect(self.db) as c:
            self.assertIsNone(migration.get(c, "auth.accounts", "owner"))
            a = migration.get(c, "auth.accounts", "original")
            self.assertEqual(a["Account"]["GroupID"], "administrators")
            self.assertEqual(a["Account"]["DisplayName"], "My profile")
            self.assertEqual(a["Account"]["Version"], 7)
            del a["Account"]["GroupID"], a["Account"]["Version"]
            self.assertEqual(a, self.account)
        private = lambda rows: [r for r in rows if r[0] not in ("auth.accounts", "auth.bootstrap", "auth.groups")]
        self.assertEqual(private(self.rows()), private(before))
        after = self.rows()
        self.assertEqual(migration.migrate(self.config, self.backup)["status"], "already_converted")
        self.assertEqual(self.rows(), after)

    def test_account_id_owner_is_converted_without_deleting_the_account(self):
        self.account["Account"]["ID"] = "owner"
        with sqlite3.connect(self.db) as c:
            c.execute("UPDATE records SET value=? WHERE bucket='auth.accounts' AND key='owner'", (json.dumps(self.account).encode(),))
        migration.migrate(self.config, self.backup)
        with sqlite3.connect(self.db) as c:
            self.assertEqual(migration.get(c, "auth.accounts", "owner")["Account"]["GroupID"], "administrators")
            self.assertEqual(migration.get(c, "auth.bootstrap", "owner"), {"AccountID": "owner"})
        self.assertEqual(migration.migrate(self.config)["status"], "already_converted")

    def test_check_does_not_modify_rows(self):
        before = self.rows()
        self.assertEqual(migration.migrate(self.config, check=True)["status"], "ready")
        self.assertEqual(before, self.rows())
        self.assertFalse(self.backup.exists())

    def test_running_instance_is_rejected(self):
        before = self.rows()
        with (self.data / "server.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(migration.MigrationError, "running"):
                migration.migrate(self.config, self.backup)
        self.assertEqual(before, self.rows())
        self.assertFalse(self.backup.exists())

    def test_backup_failure_leaves_original_database(self):
        before = self.rows()
        with patch.object(migration.shutil, "copytree", side_effect=OSError("disk full")):
            with self.assertRaises(OSError):
                migration.migrate(self.config, self.backup)
        self.assertEqual(before, self.rows())

    def test_transaction_failure_rolls_back_original_account(self):
        with sqlite3.connect(self.db) as c:
            c.execute("CREATE TRIGGER fail_group BEFORE INSERT ON records WHEN NEW.bucket='auth.groups' BEGIN SELECT RAISE(ABORT,'test failure'); END")
        before = self.rows()
        with self.assertRaises(sqlite3.Error):
            migration.migrate(self.config, self.backup)
        self.assertEqual(before, self.rows())
        self.assertEqual(before, self.rows(self.backup / "data/runtime.db"))

    def test_mixed_format_and_unsafe_backup_destinations_are_rejected(self):
        before = self.rows()
        with self.assertRaises(migration.MigrationError):
            migration.migrate(self.config, self.data / "backup")
        self.backup.mkdir()
        with self.assertRaises(FileExistsError):
            migration.migrate(self.config, self.backup)
        self.assertEqual(before, self.rows())
        with sqlite3.connect(self.db) as c:
            migration.put(c, "auth.accounts", "another", self.account)
        mixed = self.rows()
        with self.assertRaises(migration.MigrationError):
            migration.migrate(self.config, self.root / "other-backup")
        self.assertEqual(mixed, self.rows())

    def test_external_workspace_is_backed_up(self):
        work = self.root / "workspace"
        work.mkdir()
        (work / "result").write_text("assistant file")
        self.config.write_text(json.dumps({"data_dir": str(self.data), "sandbox": {"work_root": str(work)}}))
        migration.migrate(self.config, self.backup)
        self.assertEqual((self.backup / "workspace/result").read_text(), "assistant file")


if __name__ == "__main__":
    unittest.main()
