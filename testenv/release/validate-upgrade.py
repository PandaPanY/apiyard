#!/usr/bin/env python3
"""Verify an offline database copy with old/new binaries, never a live database."""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request


class Client:
    def __init__(self, port):
        self.base = f"http://127.0.0.1:{port}"
        self.http = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base + path, data=data,
                                     headers={"Content-Type": "application/json"})
        with self.http.open(req, timeout=15) as response:
            return json.load(response)

    def ok(self, path, body=None):
        result = self.call(path, body)
        if result.get("errcode") != 0:
            raise AssertionError(f"{path.split('?')[0]}: {result.get('errmsg')}")
        return result["data"]


def launch(binary, port, database, directory, name):
    log = (directory / f"{name}.log").open("w")
    proc = subprocess.Popen([str(binary), "-addr", f"127.0.0.1:{port}",
                             "-db", str(database), "-token-salt", "abcde"],
                            stdout=log, stderr=log, start_new_session=True)
    log.close()
    for _ in range(100):
        if proc.poll() is not None:
            raise RuntimeError(f"{name} exited; see {name}.log")
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=1):
                return proc
        except (OSError, urllib.error.URLError):
            time.sleep(0.1)
    stop(proc)
    raise TimeoutError(name)


def stop(proc):
    if proc is not None and proc.poll() is None:
        proc.terminate()
        proc.wait(timeout=15)


def db_state(database):
    with sqlite3.connect(database) as db:
        assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
        assert db.execute("PRAGMA foreign_key_check").fetchall() == []
        names = [r[0] for r in db.execute(
            "SELECT name FROM sqlite_master WHERE type='table'")]
        counts = {name: db.execute(f'SELECT count(*) FROM "{name}"').fetchone()[0]
                  for name in names}
        return db.execute("PRAGMA user_version").fetchone()[0], counts


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old", required=True, type=Path)
    parser.add_argument("--new", required=True, type=Path)
    parser.add_argument("--database", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--old-port", type=int, default=31340)
    parser.add_argument("--new-port", type=int, default=31341)
    parser.add_argument("--email", default="admin@admin.com")
    parser.add_argument("--password-env", default="APIYARD_VALIDATION_PASSWORD")
    parser.add_argument("--keep-running", action="store_true")
    args = parser.parse_args()
    password = os.environ[args.password_env]
    args.output_dir.mkdir(parents=True, exist_ok=True)
    backup = args.output_dir / "before-upgrade.db"
    if backup.exists():
        raise FileExistsError("validation output must be a fresh directory")
    old = new = None
    passed = False
    try:
        old = launch(args.old, args.old_port, args.database, args.output_dir, "v010")
        client = Client(args.old_port)
        login = {"email": args.email, "password": password}
        client.ok("/api/user/login", login)
        snapshots = {}
        groups = client.ok("/api/group/list")
        pids = set()
        for group in groups:
            gid = group["_id"]
            paths = [f"/api/group/get?id={gid}",
                     f"/api/group/get_member_list?id={gid}"]
            for path in paths:
                snapshots[path] = client.call(path)
            pids.update(p["_id"] for p in client.ok(
                f"/api/project/list?group_id={gid}")["list"])
        tokens = {}
        interfaces = 0
        for pid in sorted(pids):
            for path in [f"/api/project/get?id={pid}",
                         f"/api/project/get_member_list?id={pid}",
                         f"/api/interface/list_menu?project_id={pid}"]:
                snapshots[path] = client.call(path)
            listing = client.ok(f"/api/interface/list?project_id={pid}&limit=all")["list"]
            interfaces += len(listing)
            for item in listing:
                path = f"/api/interface/get?id={item['_id']}"
                snapshots[path] = client.call(path)
            tokens[pid] = client.ok(f"/api/project/token?project_id={pid}")
        assert pids and interfaces, "expected real migrated project/interface data"
        stop(old)
        version, counts = db_state(args.database)
        assert version == 1, version
        with sqlite3.connect(args.database) as source, sqlite3.connect(backup) as target:
            source.backup(target)
        assert db_state(backup) == (version, counts)

        new = launch(args.new, args.new_port, args.database, args.output_dir, "v011")
        client = Client(args.new_port)
        client.ok("/api/user/login", login)
        version, upgraded_counts = db_state(args.database)
        assert version == 2, version
        assert all(upgraded_counts[k] == v for k, v in counts.items()), "row count changed"
        for path, expected in snapshots.items():
            assert client.call(path) == expected, f"upgrade changed response: {path}"
        anonymous = Client(args.new_port)
        fresh = {}
        for pid, token in tokens.items():
            query = urllib.parse.urlencode({"token": token})
            anonymous.ok(f"/api/interface/list_menu?{query}")
            rejected = anonymous.call(f"/api/project/up?{query}", {"id": pid, "desc": "must reject"})
            assert rejected["errcode"] == 405, "legacy token retained administration"
            fresh[pid] = client.ok(f"/api/project/token?project_id={pid}")
            assert fresh[pid] != token
            anonymous.ok("/api/interface/list_menu?" + urllib.parse.urlencode({"token": fresh[pid]}))
        try:
            client.call("/mcp", {})
            raise AssertionError("MCP must default to closed")
        except urllib.error.HTTPError as error:
            assert error.code == 404
        stop(new)
        new = launch(args.new, args.new_port, args.database, args.output_dir, "v011-restart")
        anonymous = Client(args.new_port)
        for token in list(tokens.values()) + list(fresh.values()):
            anonymous.ok("/api/interface/list_menu?" + urllib.parse.urlencode({"token": token}))
        summary = {"schema_before": 1, "schema_after": 2,
                   "projects": len(pids), "interfaces": interfaces,
                   "responses_compared": len(snapshots), "legacy_tokens": len(tokens),
                   "new_tokens_persisted": len(fresh), "backup_integrity": "ok",
                   "mcp_default": 404, "result": "passed"}
        (args.output_dir / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        if args.keep_running:
            (args.output_dir / "server.pid").write_text(str(new.pid))
        print(json.dumps(summary))
        passed = True
    finally:
        stop(old)
        if not (passed and args.keep_running):
            stop(new)


if __name__ == "__main__":
    main()
