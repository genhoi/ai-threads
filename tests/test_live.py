"""Реестр Claude во временной папке и разбор active_sessions.json Grok."""

import json
import os
from datetime import datetime
from pathlib import Path

import pytest

from ai_threads.live import _starttime, live_status
from ai_threads.model import Session


@pytest.fixture
def homes(tmp_path, monkeypatch):
    claude = tmp_path / "claude"
    grok = tmp_path / "grok"
    (claude / "sessions").mkdir(parents=True)
    grok.mkdir()
    monkeypatch.setenv("CLAUDE_HOME", str(claude))
    monkeypatch.setenv("GROK_HOME", str(grok))
    monkeypatch.setenv("CODEX_HOME", str(tmp_path / "codex"))
    monkeypatch.setenv("KIMI_CODE_HOME", str(tmp_path / "kimi"))
    return claude, grok


def session(tool, session_id, cwd="/tmp"):
    return Session(tool=tool, id=session_id, title="t", cwd=cwd, updated=0)


def proc_start(pid=None):
    pid = os.getpid() if pid is None else pid
    text = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    return _starttime(text)


def dead_pid():
    candidate = 2_100_000_000
    while candidate > 1:
        try:
            os.kill(candidate, 0)
        except ProcessLookupError:
            return candidate
        except PermissionError:
            candidate -= 1
            continue
        candidate -= 1
    raise RuntimeError("не нашёлся свободный pid")


def write_claude(home, pid, session_id, status, kind="interactive", name="", proc=None,
                 since=1_700_000_000_000, filename=None):
    record = {
        "pid": pid,
        "sessionId": session_id,
        "cwd": "/tmp/proj",
        "kind": kind,
        "entrypoint": "cli",
        "status": status,
        "updatedAt": since,
        "statusUpdatedAt": since,
        "name": name,
    }
    if proc is not None:
        record["procStart"] = proc
    path = home / "sessions" / (filename or f"{pid}-{session_id}.json")
    path.write_text(json.dumps(record), encoding="utf-8")
    return path


def test_starttime_ignores_spaces_and_parentheses_in_comm():
    text = "12 (we)ird proc) S " + " ".join(str(n) for n in range(1, 21))
    # после comm идут поля 3..; 20-е из них — starttime, здесь это «19» (нумерация с 1 в range)
    assert _starttime(text) == "19"


def test_claude_statuses_map_and_where(homes):
    claude, _grok = homes
    pid = os.getpid()
    start = proc_start(pid)
    since = 1_790_609_915_299
    write_claude(claude, pid, "idle-1", "idle", kind="interactive", name="имя", proc=start, since=since)
    write_claude(claude, pid, "wait-1", "waiting", kind="bg", name="", proc=start, since=since)
    write_claude(claude, pid, "busy-1", "busy", kind="interactive", name="  ", proc=start, since=since)
    write_claude(claude, pid, "shell-1", "shell", kind="bg", name="фон", proc=start, since=since)
    sessions = {
        "claude:idle-1": session("claude", "idle-1"),
        "claude:wait-1": session("claude", "wait-1"),
        "claude:busy-1": session("claude", "busy-1"),
        "claude:shell-1": session("claude", "shell-1"),
    }
    found = live_status(sessions)
    assert found["claude:idle-1"] == {
        "state": "wait",
        "where": "Claude Code в терминале · имя",
        "since": since / 1000,
    }
    assert found["claude:wait-1"]["state"] == "wait"
    assert found["claude:wait-1"]["where"] == "фоновая сессия Claude Code"
    assert found["claude:busy-1"] == {
        "state": "work",
        "where": "Claude Code в терминале",
        "since": since / 1000,
    }
    assert found["claude:shell-1"]["state"] == "work"
    assert found["claude:shell-1"]["where"] == "фоновая сессия Claude Code · фон"


def test_seconds_are_not_divided(homes):
    claude, _grok = homes
    pid = os.getpid()
    write_claude(claude, pid, "s", "idle", proc=proc_start(pid), since=1_700_000_000)
    found = live_status({"claude:s": session("claude", "s")})
    assert found["claude:s"]["since"] == 1_700_000_000


def test_dead_pid_procstart_mismatch_and_unknown_are_absent(homes):
    claude, _grok = homes
    pid = os.getpid()
    write_claude(claude, dead_pid(), "dead", "busy", proc="1", since=1_700_000_000_000)
    write_claude(claude, pid, "reused", "busy", proc="not-this-process", since=1_700_000_000_000)
    write_claude(claude, pid, "unknown", "thinking", proc=proc_start(pid), since=1_700_000_000_000)
    write_claude(claude, pid, "nokind", "idle", kind="other", proc=proc_start(pid), since=1_700_000_000_000)
    write_claude(claude, pid, "alive", "shell", proc=proc_start(pid), since=1_700_000_000_000)
    # procStart можно не передавать: тогда достаточно живого pid.
    write_claude(claude, pid, "nostart", "idle", proc=None, since=1_700_000_001_000)
    sessions = {f"claude:{name}": session("claude", name) for name in
                ("dead", "reused", "unknown", "nokind", "alive", "nostart")}
    found = live_status(sessions)
    assert set(found) == {"claude:alive", "claude:nostart"}
    assert found["claude:alive"]["state"] == "work"


def test_broken_json_and_key_files_do_not_hide_the_rest(homes):
    claude, _grok = homes
    pid = os.getpid()
    (claude / "sessions" / "broken.json").write_text("{", encoding="utf-8")
    (claude / "sessions" / "not-object.json").write_text("[]", encoding="utf-8")
    secret = {
        "pid": pid,
        "sessionId": "from-key",
        "kind": "interactive",
        "status": "busy",
        "statusUpdatedAt": 1_700_000_000_000,
        "procStart": proc_start(pid),
    }
    (claude / "sessions" / f"{pid}.key").write_text(json.dumps(secret), encoding="utf-8")
    write_claude(claude, pid, "ok", "idle", proc=proc_start(pid), since=1_700_000_000_000)
    found = live_status({
        "claude:ok": session("claude", "ok"),
        "claude:from-key": session("claude", "from-key"),
    })
    assert set(found) == {"claude:ok"}


def test_same_session_keeps_the_newer_record(homes):
    claude, _grok = homes
    pid = os.getpid()
    start = proc_start(pid)
    write_claude(claude, pid, "same", "idle", proc=start, since=1_700_000_000_000, filename="old.json")
    write_claude(claude, pid, "same", "busy", name="новее", proc=start, since=1_800_000_000_000, filename="new.json")
    found = live_status({"claude:same": session("claude", "same")})
    assert found["claude:same"]["state"] == "work"
    assert found["claude:same"]["since"] == 1_800_000_000
    assert "новее" in found["claude:same"]["where"]


def test_session_missing_from_catalog_is_not_reported(homes):
    claude, _grok = homes
    pid = os.getpid()
    write_claude(claude, pid, "orphan", "waiting", proc=proc_start(pid), since=1_700_000_000_000)
    assert live_status({}) == {}
    assert live_status({"claude:other": session("claude", "other")}) == {}


def test_grok_registry_alive_dead_and_broken(homes):
    _claude, grok = homes
    pid = os.getpid()
    opened = datetime.fromisoformat("2026-09-28T12:00:00+00:00")
    (grok / "active_sessions.json").write_text(json.dumps([
        {"session_id": "alive", "pid": pid, "cwd": "/tmp/g", "opened_at": 1_700_000_000},
        {"sessionId": "camel", "pid": pid, "cwd": "/tmp/g", "openedAt": opened.isoformat()},
        {"session_id": "dead", "pid": dead_pid(), "cwd": "/tmp/g", "opened_at": 1_700_000_000},
        {"session_id": "orphan", "pid": pid, "cwd": "/tmp/g", "opened_at": 1_700_000_000_000},
        "мусор",
    ]), encoding="utf-8")
    sessions = {
        "grok:alive": session("grok", "alive"),
        "grok:camel": session("grok", "camel"),
        "grok:dead": session("grok", "dead"),
        "claude:alive": session("claude", "alive"),
    }
    found = live_status(sessions)
    assert found["grok:alive"] == {"state": "work", "where": "Grok", "since": 1_700_000_000}
    assert found["grok:camel"]["state"] == "work"
    assert found["grok:camel"]["where"] == "Grok"
    assert found["grok:camel"]["since"] == int(opened.timestamp())
    assert "grok:dead" not in found
    assert "grok:orphan" not in found
    assert "claude:alive" not in found

    (grok / "active_sessions.json").write_text("{", encoding="utf-8")
    assert live_status({"grok:alive": session("grok", "alive")}) == {}
    (grok / "active_sessions.json").write_text("[]", encoding="utf-8")
    assert live_status({"grok:alive": session("grok", "alive")}) == {}


def test_codex_and_kimi_have_no_status(homes):
    assert live_status({
        "codex:1": session("codex", "1"),
        "kimi:session_1": session("kimi", "session_1"),
    }) == {}


def test_missing_registries_return_empty(tmp_path, monkeypatch):
    monkeypatch.setenv("CLAUDE_HOME", str(tmp_path / "no-claude"))
    monkeypatch.setenv("GROK_HOME", str(tmp_path / "no-grok"))
    assert live_status({"claude:x": session("claude", "x")}) == {}
