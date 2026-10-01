"""Тесты ai_threads.summary: сводка Kimi по сессии на заглушках."""

from __future__ import annotations

import time
from pathlib import Path

import pytest

from ai_threads.jobs import Jobs
from ai_threads.model import Session
from ai_threads.summary import run_summary

from .conftest import all_events, wait_finished, wait_for


@pytest.fixture
def session(tmp_path) -> Session:
    journal = tmp_path / "wire.jsonl"
    journal.write_text('{"type":"metadata"}\n', encoding="utf-8")
    return Session(tool="kimi", id="session_test1", title="Исходное название",
                   cwd=str(tmp_path), updated=time.time(), journal=journal)


CANDIDATES = [("kimi:aaa", "Сессия A"), ("kimi:bbb", "Сессия B"),
              ("kimi:ccc", "Сессия C"), ("kimi:ddd", "Сессия D"),
              ("kimi:other", "Другая тема")]


def start_summary(jobs, session, store, candidates=CANDIDATES):
    return jobs.start(f"summary:{session.key}", "summary", session.key,
                      lambda job: run_summary(job, session, "Своё название",
                                              candidates, store))


def test_ok_saves_summary_and_emits_progress(stub_path, session, store):
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    events = all_events(job)

    saved = store.summaries.get(session.key)
    assert saved is not None, f"сводка не сохранена, события: {events}"
    assert saved["title"] == "Тест. Сводка по сессии"
    assert saved["model"] == "kimi"
    assert isinstance(saved["at"], float)
    assert saved["closed"] is True
    assert saved["next_step"] == "Проверить результат вручную"
    # related: только ключи из списка, до 3, why до 40 символов
    keys = [r["key"] for r in saved["related"]]
    assert keys == ["kimi:aaa", "kimi:bbb", "kimi:ccc"]
    assert all(len(r["why"]) <= 40 for r in saved["related"])

    progress = [e["text"] for e in events if e["type"] == "progress"]
    assert progress[0] == "запускается"
    assert "читает фрагмент 1" in progress
    assert "читает фрагмент 2" in progress
    assert progress[-1] == "пишет сводку"
    assert "анализирует журнал" in progress
    for prev, nxt in zip(events, events[1:]):
        assert nxt["n"] == prev["n"] + 1
        assert nxt["elapsed"] >= prev["elapsed"]

    result_events = [e for e in events if e["type"] == "result"]
    assert len(result_events) == 1
    assert result_events[0]["result"] == saved


def test_related_filters_unknown_keys(stub_path, session, store):
    jobs = Jobs()
    job = start_summary(jobs, session, store,
                        candidates=[("kimi:bbb", "Сессия B")])
    wait_finished(job)
    saved = store.summaries[session.key]
    assert [r["key"] for r in saved["related"]] == ["kimi:bbb"]


def test_error_mode_reports_code_and_stderr(stub_path, session, store,
                                            monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "error")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    events = all_events(job)
    errors = [e for e in events if e["type"] == "error"]
    assert len(errors) == 1
    assert errors[0]["code"] == 2
    assert "boom" in errors[0]["output"]
    assert session.key not in store.summaries
    assert not [e for e in events if e["type"] == "result"]


def test_garbage_mode_reports_invalid_answer(stub_path, session, store,
                                             monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "garbage")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    events = all_events(job)
    errors = [e for e in events if e["type"] == "error"]
    assert len(errors) == 1
    assert "без названия или сводки" in errors[0]["message"]
    assert session.key not in store.summaries


def test_hang_times_out(stub_path, session, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "hang")
    monkeypatch.setenv("AI_THREADS_SUMMARY_TIMEOUT", "1")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    events = all_events(job)
    errors = [e for e in events if e["type"] == "error"]
    assert len(errors) == 1
    assert "не ответил за 1 с" in errors[0]["message"]
    assert session.key not in store.summaries


def test_cancel_stops_without_result(stub_path, session, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "hang")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_for(job, lambda e: e["type"] == "progress" and e.get("step") == 0)
    job.cancel()
    wait_finished(job)
    events = all_events(job)
    assert session.key not in store.summaries
    assert not [e for e in events if e["type"] == "result"]
    assert not [e for e in events if e["type"] == "error"]


def test_slow_mode_streams_progress(stub_path, session, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "slow")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    # до паузы заглушки появляются события чтения
    wait_for(job, lambda e: e["type"] == "progress"
             and e.get("text", "").startswith("читает фрагмент"))
    wait_finished(job, timeout=15)
    assert session.key in store.summaries


def test_no_agent_for_summary(no_tools, session, store):
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert errors and "Не нашёл ни одного агента для сводок" in errors[0]["message"]
    assert session.key not in store.summaries


def test_journal_missing(stub_path, session, store):
    session.journal = Path("/nonexistent/wire.jsonl")
    jobs = Jobs()
    job = start_summary(jobs, session, store)
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert errors and "Журнал сессии недоступен" in errors[0]["message"]


def test_duplicate_run_does_not_start_twice(stub_path, session, store):
    jobs = Jobs()
    first = start_summary(jobs, session, store)
    second = start_summary(jobs, session, store)
    assert first is second
    wait_finished(first)
    assert len(store.summaries) == 1
