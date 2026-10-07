#!/usr/bin/env python3
"""Offline, one-time conversion of the pre-multi-user account layout.

This script is not imported or shipped inside molr-server. Stop the instance
before running it; a successful conversion requires no migration code at startup.
"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import stat
import sys
from datetime import datetime, timezone


class MigrationError(Exception):
    pass


def get(conn, bucket, key):
    row = conn.execute("SELECT value FROM records WHERE bucket=? AND key=?", (bucket, key)).fetchone()
    return json.loads(row[0]) if row else None


def put(conn, bucket, key, value):
    conn.execute("INSERT INTO records(bucket,key,value) VALUES(?,?,?)", (bucket, key, json.dumps(value, separators=(",", ":")).encode()))


def inspect(conn):
    rows = conn.execute("SELECT key,value FROM records WHERE bucket='auth.accounts' ORDER BY key").fetchall()
    if not rows:
        raise MigrationError("No initialized account; use the new Runtime's init command for a new installation.")
    bootstrap = get(conn, "auth.bootstrap", "owner")
    if bootstrap is not None:
        accounts = [json.loads(raw)["Account"] for _, raw in rows]
        if not any(a["ID"] == bootstrap.get("AccountID") for a in accounts):
            raise MigrationError("Incomplete account metadata; refusing to overwrite it.")
        for (key, _), a in zip(rows, accounts):
            group = get(conn, "auth.groups", a.get("GroupID", ""))
            if key != a["ID"] or not group or group.get("ID") != a.get("GroupID") or group.get("Administrator") != (a.get("GroupID") == "administrators"):
                raise MigrationError("Mixed account formats; refusing to overwrite them.")
        return None
    if len(rows) != 1 or rows[0][0] != "owner":
        raise MigrationError("Expected exactly one old owner account; refusing an unknown format.")
    legacy = json.loads(rows[0][1])
    a = legacy.get("Account", {})
    if not isinstance(a.get("ID"), str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", a["ID"]):
        raise MigrationError("Invalid original account ID.")
    if not a.get("Username") or not legacy.get("PasswordHash") or not isinstance(legacy.get("Version"), int) or legacy["Version"] < 1:
        raise MigrationError("Incomplete original account.")
    if a.get("GroupID"):
        raise MigrationError("Mixed account formats; refusing to overwrite them.")
    if conn.execute("SELECT 1 FROM records WHERE bucket='auth.groups' LIMIT 1").fetchone():
        raise MigrationError("Unexpected existing user groups; refusing to overwrite them.")
    return legacy


def migrate(config_path, backup_path=None, check=False):
    config_path = Path(config_path).resolve(strict=True)
    cfg = json.loads(config_path.read_text())
    data = Path(cfg["data_dir"])
    if not data.is_absolute() or not data.is_dir():
        raise MigrationError("data_dir must be an existing absolute directory.")
    data = data.resolve(strict=True)
    database = data / "runtime.db"
    if not database.is_file() or database.is_symlink():
        raise MigrationError("runtime.db must be an existing regular file.")
    lock_fd = os.open(data / "server.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    conn = None
    try:
        if not stat.S_ISREG(os.fstat(lock_fd).st_mode):
            raise MigrationError("Invalid server lock file.")
        try:
            fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise MigrationError("Runtime is running; stop this instance before conversion.") from None
        conn = sqlite3.connect(database.as_uri() + "?mode=rw", uri=True, timeout=0, isolation_level=None)
        # Also excludes any independent SQLite writer during snapshot and commit.
        conn.execute("BEGIN EXCLUSIVE")
        if conn.execute("PRAGMA quick_check").fetchall() != [("ok",)]:
            raise MigrationError("Database integrity check failed.")
        legacy = inspect(conn)
        if legacy is None:
            conn.rollback()
            return {"status": "already_converted"}
        if check:
            conn.rollback()
            return {"status": "ready", "action": "explicit offline conversion required"}
        if backup_path is None:
            raise MigrationError("--backup-dir is required for conversion.")
        backup = Path(backup_path).absolute()
        roots = [data]
        workspace = cfg.get("sandbox", {}).get("work_root")
        if workspace:
            work = Path(workspace).resolve(strict=True)
            if not work.is_dir():
                raise MigrationError("Configured work_root is not a directory.")
            if not work.is_relative_to(data):
                roots.append(work)
        resolved_backup = backup.resolve()
        if any(resolved_backup == root or resolved_backup.is_relative_to(root) for root in roots):
            raise MigrationError("Backup must be outside the data and workspace directories.")
        # Never reuse an old snapshot or follow workspace symlinks while copying.
        backup.mkdir(mode=0o700, parents=True, exist_ok=False)
        shutil.copytree(data, backup / "data", symlinks=True)
        if len(roots) > 1:
            shutil.copytree(roots[1], backup / "workspace", symlinks=True)
        shutil.copy2(config_path, backup / "server.json")
        os.chmod(backup / "server.json", 0o600)
        manifest = {"created_at": datetime.now(timezone.utc).isoformat(), "data_dir": str(data), "config": str(config_path), "work_root": workspace, "format": "pre-multi-user"}
        (backup / "manifest.json").write_text(json.dumps(manifest, indent=2))
        # Finish the backup before any database mutation. Failure keeps old rows.
        os.sync()
        account = legacy["Account"]
        account["GroupID"] = "administrators"
        account["DisplayName"] = account.get("DisplayName") or account["Username"]
        account["Version"] = legacy["Version"]
        if account["ID"] != "owner":
            put(conn, "auth.accounts", account["ID"], legacy)
            conn.execute("DELETE FROM records WHERE bucket='auth.accounts' AND key='owner'")
        else:
            conn.execute("UPDATE records SET value=? WHERE bucket='auth.accounts' AND key='owner'", (json.dumps(legacy).encode(),))
        put(conn, "auth.bootstrap", "owner", {"AccountID": account["ID"]})
        for group_id, name, admin in [("administrators", "Administrators", True), ("users", "Users", False)]:
            put(conn, "auth.groups", group_id, {"ID": group_id, "Name": name, "Administrator": admin, "Resources": [], "Version": 1})
        conn.commit()
        return {"status": "converted", "backup_dir": str(backup)}
    finally:
        if conn is not None:
            conn.close()  # rolls back every uncommitted change
        os.close(lock_fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--backup-dir")
    parser.add_argument("--check", action="store_true", help="check a stopped instance without converting it")
    args = parser.parse_args()
    try:
        print(json.dumps(migrate(args.config, args.backup_dir, args.check)))
    except (MigrationError, OSError, sqlite3.Error, ValueError, KeyError, TypeError) as error:
        print(f"Migration not completed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
