"""Журнал работы агента и ответ в его сессию: события trace, agent_session, runner.reply."""

from __future__ import annotations

import os
import time
from pathlib import Path

import pytest

from ai_threads import runner
from ai_threads.jobs import Jobs
from ai_threads.model import Session
from ai_threads.search import run_search
from ai_threads.summary import run_summary

from .a_support import homes
from .conftest import all_events, wait_finished


@pytest.fixture
def session(tmp_path) -> Session:
    journal = tmp_path / "wire.jsonl"
    journal.write_text('{"type":"metadata"}\n', encoding="utf-8")
    return Session(tool="kimi", id="session_test1", title="Исходное название", cwd=str(tmp_path),
                   updated=time.time(), journal=journal)


def summarize(session, store, agent_id=None):
    jobs = Jobs()
    job = jobs.start("summary:x", "summary", session.key,
                     lambda job: run_summary(job, session, "t", [], store, agent_id))
    wait_finished(job)
    return all_events(job)


@pytest.mark.parametrize("agent_id,session_id,tool", [
    ("kimi", "session_stub", "Read"),
    ("claude", "stub-session", "Read"),
    ("codex", "stub-thread", "shell"),
])
def test_trace_and_agent_session(stub_path, session, store, agent_id, session_id, tool):
    events = summarize(session, store, agent_id)
    traces = [e for e in events if e["type"] == "trace"]
    assert traces[0]["kind"] == "prompt" and "Путь к журналу" in traces[0]["text"]
    kinds = {t["kind"] for t in traces}
    assert {"tool", "result", "text"} <= kinds, kinds
    assert any(t["kind"] == "tool" and t["tool"] == tool for t in traces)
    started = [e for e in events if e["type"] == "agent_session"]
    assert len(started) == 1 and started[0]["agent"] == agent_id and started[0]["session"] == session_id
    assert started[0]["command"].startswith("cd -- ") and session_id in started[0]["command"]
    workdir = Path(runner.SESSIONS[session_id]["workdir"])
    assert workdir.is_dir() and workdir.parent.name == "runs"
    assert any(e["type"] == "result" for e in events)


def test_reply_continues_the_session(stub_path, session, store):
    summarize(session, store, "claude")
    jobs = Jobs()
    job = jobs.start("reply:1", "reply", None, lambda job: runner.reply(job, "claude", "stub-session", "А что дальше?"))
    wait_finished(job)
    events = all_events(job)
    prompts = [e["text"] for e in events if e["type"] == "trace" and e["kind"] == "prompt"]
    assert prompts == ["А что дальше?"]
    result = [e for e in events if e["type"] == "result"]
    assert result and result[0]["result"]["answer"]


def test_reply_to_unknown_session(stub_path):
    jobs = Jobs()
    job = jobs.start("reply:2", "reply", None, lambda job: runner.reply(job, "kimi", "session_nope", "текст"))
    wait_finished(job)
    errors = [e["message"] for e in all_events(job) if e["type"] == "error"]
    assert errors and "не помнит" in errors[0]


def test_old_run_dirs_are_removed():
    first = runner.workdir("summary")
    old = first.parent / "20200101-000000-summary-old"
    old.mkdir()
    stamp = time.time() - (runner.RUNS_KEEP_DAYS + 1) * 86400
    os.utime(old, (stamp, stamp))
    second = runner.workdir("search")
    assert first.is_dir() and second.is_dir() and not old.exists()


def test_one_word_query_skips_the_plan(stub_path, homes, tmp_path):
    from ai_threads.catalog import Catalog
    catalog = Catalog(tmp_path / "cache.json")
    assert catalog.wait(10)
    jobs = Jobs()
    job = jobs.start("search:1", "search", None,
                     lambda job: run_search(job, catalog.sessions(), "каталог", "mine", [], {}, {}))
    wait_finished(job)
    events = all_events(job)
    texts = [e["text"] for e in events if e["type"] == "progress"]
    assert "понимает запрос" not in texts and texts[-1] == "отбирает сессии"
    assert next(e for e in events if e["type"] == "plan")["terms"] == ["каталог"]
    nit = [e["text"] for e in events if e["type"] == "trace" and e["agent"] == "nit"]
    assert nit[0].startswith("Запрос из одного слова") and any("агенту ушли" in t for t in nit)
    order = [e["type"] for e in events if e["type"] in ("candidates", "result")]
    assert order == ["candidates", "result"]
    candidates = next(e for e in events if e["type"] == "candidates")["results"]
    assert candidates and all(c["matched"] == ["каталог"] for c in candidates)
