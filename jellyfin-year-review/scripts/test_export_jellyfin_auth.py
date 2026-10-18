"""Synthetic Jellyfin 12.2 SQLite fixtures; never access a real media database."""

import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("export_auth", Path(__file__).with_name("export-jellyfin-auth.py"))
exporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(exporter)
FIXTURE = json.loads((Path(__file__).parents[1] / "testdata/offline-auth.json").read_text())


class ExportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.database = self.root / "jellyfin.db"
        self.output = self.root / "auth/users.json"
        self.db = sqlite3.connect(self.database)
        self.addCleanup(self.db.close)
        self.db.executescript('''
            PRAGMA journal_mode=WAL;
            PRAGMA wal_autocheckpoint=0;
            CREATE TABLE Users (Id TEXT, Username TEXT, Password TEXT, AuthenticationProviderId TEXT, MustUpdatePassword INTEGER);
            CREATE TABLE Permissions (UserId TEXT, Kind INTEGER, Value INTEGER);
            CREATE TABLE AccessSchedules (UserId TEXT);
        ''')
        self.add_user("a", "Alice")

    def add_user(self, char, name, provider=exporter.PROVIDER, password="fixture", disabled=0, remote=1, must_update=0):
        user_id = char * 32
        verifier = FIXTURE["users"][0]["passwordHash"] if password == "fixture" else password
        self.db.execute("INSERT INTO Users VALUES (?,?,?,?,?)", (user_id, name, verifier, provider, must_update))
        self.db.executemany("INSERT INTO Permissions VALUES (?,?,?)", [(user_id, 2, disabled), (user_id, 4, remote)])
        self.db.commit()
        return user_id

    def export(self):
        return exporter.export(self.database, self.output, "2026-01-01T00:00:00Z")

    def test_export_sees_committed_wal_and_no_plaintext(self):
        # Keep the writer open, forcing the exporter to read committed WAL pages.
        self.add_user("c", "Legacy", password=FIXTURE["users"][1]["passwordHash"])
        before = self.db.total_changes
        enabled, _ = self.export()
        doc = json.loads(self.output.read_text())
        self.assertEqual(enabled, 2)
        self.assertEqual(doc["users"][0]["id"], "a" * 32)
        self.assertEqual(doc["users"][0]["passwordHash"], FIXTURE["users"][0]["passwordHash"])
        self.assertNotIn("fixture-only", self.output.read_text())
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.db.total_changes, before)
        self.assertEqual(self.db.execute("SELECT count(*) FROM Users").fetchone()[0], 2)

    def test_blocked_external_passwordless_and_restricted_users(self):
        self.add_user("b", "Disabled", disabled=1)
        self.add_user("c", "LDAP", provider="External.Provider")
        self.add_user("d", "NoPassword", password=None)
        self.add_user("e", "LocalOnly", remote=0)
        self.add_user("f", "MustChange", must_update=1)
        uid = self.add_user("1", "Scheduled")
        self.db.execute("INSERT INTO AccessSchedules VALUES (?)", (uid,))
        self.db.commit()
        enabled, counts = self.export()
        self.assertEqual(enabled, 1)
        self.assertEqual(counts, {"external": 1, "passwordless": 1, "disabled": 1, "restricted": 3})
        doc = json.loads(self.output.read_text())
        for user in doc["users"]:
            if user["disabled"]:
                self.assertEqual(user["passwordHash"], "")

    def test_failed_exports_preserve_previous_and_success_creates_backup(self):
        self.export()
        previous = self.output.read_bytes()
        self.db.execute("UPDATE Users SET Password='not-a-hash'")
        self.db.commit()
        with self.assertRaises(ValueError):
            self.export()
        self.assertEqual(self.output.read_bytes(), previous)
        self.db.execute("UPDATE Users SET Password=?", (FIXTURE["users"][0]["passwordHash"],))
        self.db.commit()
        self.export()
        self.assertEqual(Path(str(self.output) + ".previous").read_bytes(), previous)
        self.assertEqual(Path(str(self.output) + ".previous").stat().st_mode & 0o777, 0o600)
        with patch.object(exporter.os, "replace", side_effect=OSError("synthetic failure")):
            with self.assertRaises(OSError):
                self.export()
        self.assertEqual(self.output.read_bytes(), previous)

    def test_missing_permissions_schema_and_unsafe_destinations(self):
        self.db.execute("DELETE FROM Permissions WHERE Kind=2")
        self.db.commit()
        with self.assertRaises(ValueError):
            self.export()
        self.db.execute("DROP TABLE Permissions")
        self.db.commit()
        with self.assertRaises(ValueError):
            self.export()
        with self.assertRaises(ValueError):
            exporter.export(self.database, self.database)

    def test_private_file_and_symlink_checks(self):
        self.export()
        self.output.chmod(0o644)
        with self.assertRaises(ValueError):
            self.export()
        self.output.chmod(0o600)
        link = self.root / "linked.json"
        link.symlink_to(self.output)
        with self.assertRaises(ValueError):
            exporter.export(self.database, link)

    def test_zero_accounts_is_valid_and_bounds_reject_dangerous_hashes(self):
        self.db.execute("DELETE FROM Users")
        self.db.commit()
        enabled, _ = self.export()
        self.assertEqual(enabled, 0)
        self.assertEqual(json.loads(self.output.read_text())["users"], [])
        for encoded in ["plaintext", "$PBKDF2-SHA512$iterations=9999999$0011223344556677$" + "00" * 64]:
            with self.assertRaises(ValueError):
                exporter.validate_hash(encoded)


if __name__ == "__main__":
    unittest.main()
