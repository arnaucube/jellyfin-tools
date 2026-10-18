#!/usr/bin/env python3
"""Export local Jellyfin 12.2 verifiers without passwords or database writes."""

import argparse
import contextlib
import datetime
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import sys
import tempfile
import time
import uuid

PROVIDER = "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider"
MAX_USERS = 1000
MAX_BYTES = 1 << 20


def identity(value):
    """Match Jellyfin's immutable Guid identity across its two string forms."""
    if not isinstance(value, str):
        raise ValueError("Expected text UUIDs in the Jellyfin 12.2 schema")
    try:
        return uuid.UUID(value).hex
    except ValueError:
        raise ValueError("Invalid account UUID") from None


def login_key(name):
    """Use ASCII case folding and exact spelling for Unicode account names."""
    return ("ascii:" + name.lower()) if name.isascii() else ("unicode:" + name)


def validate_hash(encoded):
    """Accept the two bounded, salted PBKDF2 encodings supported by Go serving."""
    if not isinstance(encoded, str):
        raise ValueError("Unsupported password verifier; sign in to Jellyfin to migrate it")
    match = re.fullmatch(
        r"\$(PBKDF2-SHA512|PBKDF2)\$iterations=([0-9]{1,7})\$([0-9a-fA-F]+)\$([0-9a-fA-F]+)",
        encoded,
    )
    if not match:
        raise ValueError("Unsupported password verifier; sign in to Jellyfin to migrate it")
    algorithm, iterations, salt_hex, hash_hex = match.groups()
    size = 64 if algorithm == "PBKDF2-SHA512" else 32
    if not 1 <= int(iterations) <= 1_000_000 or len(salt_hex) % 2 or not 16 <= len(salt_hex) <= 256 or len(hash_hex) != size * 2:
        raise ValueError("Password verifier parameters are outside supported bounds")


def read_accounts(database):
    """Pin one read-only SQLite transaction, including committed WAL contents."""
    counts = {"external": 0, "passwordless": 0, "disabled": 0, "restricted": 0}
    deadline = time.monotonic() + 60
    with contextlib.closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True, timeout=5)) as db:
        db.execute("PRAGMA query_only=ON")
        db.set_progress_handler(lambda: int(time.monotonic() > deadline), 1000)
        db.execute("BEGIN")
        required = {
            "Users": {"Id", "Username", "Password", "AuthenticationProviderId", "MustUpdatePassword"},
            "Permissions": {"UserId", "Kind", "Value"},
        }
        for table, columns in required.items():
            actual = {row[1] for row in db.execute(f'PRAGMA table_info("{table}")')}
            if not columns <= actual:
                raise ValueError("Unsupported database schema; expected Jellyfin 12.2 Users and Permissions")
        rows = db.execute('SELECT Id, Username, Password, AuthenticationProviderId, MustUpdatePassword FROM Users LIMIT 1001').fetchall()
        if len(rows) > MAX_USERS:
            raise ValueError("Account count exceeds the supported limit")
        permissions = {}
        for user_id, kind, value in db.execute('SELECT UserId, Kind, Value FROM Permissions WHERE Kind IN (2, 4)'):
            key = (identity(user_id), kind)
            if key in permissions or value not in (0, 1):
                raise ValueError("Invalid or duplicate account status permissions")
            permissions[key] = bool(value)
        scheduled = set()
        if db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='AccessSchedules'").fetchone():
            scheduled = {identity(row[0]) for row in db.execute('SELECT DISTINCT UserId FROM AccessSchedules')}
        users, ids, names = [], set(), set()
        for user_id, name, password_hash, provider, must_update in rows:
            user_id = identity(user_id)
            if not isinstance(name, str) or not name or len(name.encode("utf-8")) > 256 or any(c in name for c in "\x00\r\n"):
                raise ValueError("Invalid account username")
            name_key = login_key(name)
            if user_id in ids or name_key in names:
                raise ValueError("Duplicate account identity or login name")
            ids.add(user_id)
            names.add(name_key)
            if not isinstance(provider, str) or provider.lower() != PROVIDER.lower():
                counts["external"] += 1
                continue
            if (user_id, 2) not in permissions or (user_id, 4) not in permissions:
                raise ValueError("Missing explicit disabled or remote-access account permission")
            disabled = permissions[user_id, 2]
            restricted = not permissions[user_id, 4] or bool(must_update) or user_id in scheduled
            if disabled or restricted:
                counts["disabled" if disabled else "restricted"] += 1
                users.append({"id": user_id, "username": name, "passwordHash": "", "disabled": True})
                continue
            if password_hash is None or password_hash == "":
                counts["passwordless"] += 1
                continue
            validate_hash(password_hash)
            users.append({"id": user_id, "username": name, "passwordHash": password_hash, "disabled": False})
        db.rollback()
    return sorted(users, key=lambda user: user["id"]), counts


def private_existing(path):
    """Refuse accidental symlinks, oversized files or permissive secret copies."""
    try:
        info = path.lstat()
    except FileNotFoundError:
        return None
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) & 0o077 or info.st_size > MAX_BYTES:
        raise ValueError("Existing credential file must be regular, private (0600/0400) and within 1 MiB")
    return path.read_bytes()


def atomic_write(path, content):
    """Publish a private synced file with atomic rename on the same filesystem."""
    descriptor, temporary = tempfile.mkstemp(prefix=".auth-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as file:
            file.write(content)
            file.flush()
            os.fsync(file.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def export(database, output, exported_at=None):
    """Keep the last successful credential export intact on source failures."""
    database = Path(database).resolve(strict=True)
    output = Path(output).absolute()
    backup = output.with_name(output.name + ".previous")
    if database in (output.resolve(), backup.resolve()):
        raise ValueError("Credential output cannot replace the source database")
    users, counts = read_accounts(database)
    document = {
        "schemaVersion": 1,
        "sourceVersion": "12.2",
        "exportedAt": exported_at or datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "users": users,
    }
    payload = (json.dumps(document, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    if len(payload) > MAX_BYTES:
        raise ValueError("Credential export exceeds 1 MiB")
    output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    previous = private_existing(output)
    private_existing(backup)
    if previous is not None:
        atomic_write(backup, previous)
    atomic_write(output, payload)
    return len([user for user in users if not user["disabled"]]), counts


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database", required=True, help="readable live Jellyfin 12.2 SQLite DB or safe backup")
    parser.add_argument("--output", required=True, help="private auth/users.json, outside web and review data")
    args = parser.parse_args()
    try:
        enabled, counts = export(args.database, args.output)
    except ValueError as error:
        print(f"export-auth: {error}", file=sys.stderr)
        return 1
    except (sqlite3.Error, OSError):
        print("export-auth: cannot read the database or publish the private export; check schema, paths and permissions", file=sys.stderr)
        return 1
    print(f"Exported {enabled} enabled local accounts. Excluded: {counts['external']} external, {counts['passwordless']} passwordless; blocked: {counts['disabled']} disabled, {counts['restricted']} restricted.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
