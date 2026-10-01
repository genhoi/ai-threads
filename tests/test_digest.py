"""Тесты ai_threads.digest: сводка за период через четыре модели-заглушки."""

from __future__ import annotations

import datetime
import json
import time
from pathlib import Path

import pytest

from ai_threads.digest import (_Tracker, _excerpt, build_prompt, models,
                               parse_digest, run_digest, select_for_digest)
from ai_threads.jobs import Job, Jobs
from ai_threads.model import Session

from .conftest import all_events, wait_finished, wait_for


@pytest.fixture
def sessions(tmp_path):
    """Две ручные сессии (у одной готовая сводка) и один автозапуск."""
    journal_a = tmp_path / "a" / "wire.jsonl"
    journal_b = tmp_path / "b" / "wire.jsonl"
    journal_auto = tmp_path / "auto" / "rollout.jsonl"
    for path in (journal_a, journal_b, journal_auto):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('{"type":"row"}\n', encoding="utf-8")
    # cwd вне /tmp: сессии с временной папкой не попадают в сводку
    manual_ready = Session(tool="kimi", id="aaa", title="Сессия A",
                           cwd="/home/user/projects/sample-a",
                           updated=time.time(), journal=journal_a)
    manual_open = Session(tool="claude", id="bbb", title="Сессия B",
                          cwd="/home/user/projects/sample-b",
                          updated=time.time(), journal=journal_b)
    auto = Session(tool="codex", id="auto1", title="Автоматическое ревью",
                   cwd="/home/user/projects/sample-auto",
                   updated=time.time(),
                   auto=True, by="codex exec", journal=journal_auto)
    return [manual_ready, manual_open, auto]


SUMMARIES = {"kimi:aaa": {"title": "Сводка A", "summary": "Сделано то-то",
                          "next_step": "", "closed": False,
                          "related": [], "at": 1.0, "model": "kimi"}}


def start_digest(jobs, sessions, store, model="kimi", days=3, hidden=()):
    return jobs.start("digest", "digest", None,
                      lambda job: run_digest(job, sessions, days, model,
                                             {}, SUMMARIES, hidden, store))


def assert_digest_result(events, store, model):
    saved = store.digests.get(3)
    assert saved is not None, f"сводка не сохранена, события: {events}"
    assert saved["model"] == model
    assert isinstance(saved["at"], float)
    result = saved["result"]
    assert result["lead"].startswith("Главное за период")
    # ключи в bullets отфильтрованы по списку сессий
    bullet = result["projects"][0]["bullets"][0]
    assert bullet["text"] == "Сделано важное в проекте A"
    assert bullet["keys"] == ["kimi:aaa"]
    # хвост с чужим ключом отброшен, id уникальны
    assert [t["id"] for t in result["tails"]] == ["t1"]
    assert result["tails"][0]["key"] == "claude:bbb"
    # автосессия с чужим ключом отброшена
    assert [a["key"] for a in result["autos"]] == ["codex:auto1"]
    assert result["autos"][0]["text"].startswith("Важное замечание")

    stages = [(e["index"], e["label"]) for e in events if e["type"] == "stage"]
    assert stages == [(0, "Сбор"), (1, "Чтение журналов"),
                      (2, "Разбор"), (3, "Текст")]
    first_status, last_status = {}, {}
    for e in events:
        if e["type"] == "session":
            first_status.setdefault(e["key"], e["status"])
            last_status[e["key"]] = e["status"]
    # готовой сводки хватило для стартового статуса ok, но заглушка всё
    # равно «читает» журнал — статус честно уходит в now и заканчивается read
    assert first_status["kimi:aaa"] == "ok"
    assert last_status["kimi:aaa"] == "read"
    assert last_status["claude:bbb"] == "read"  # журнал прочитан
    notes = [e for e in events if e["type"] == "note"]
    assert notes and notes[0]["project"] == "proj-a"
    if model == "kimi":
        # kimi stream-json не сообщает расход контекста
        assert not [e for e in events if e["type"] == "tokens"]
    else:
        assert any(e["type"] == "tokens" for e in events)
    assert any(e["type"] == "log" for e in events)
    result_events = [e for e in events if e["type"] == "result"]
    assert len(result_events) == 1
    assert result_events[0]["result"] == result


@pytest.mark.parametrize("model", ["kimi", "claude", "codex", "grok"])
def test_ok_all_models(stub_path, sessions, store, model):
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model=model)
    wait_finished(job)
    assert_digest_result(all_events(job), store, model)


def test_models_availability(stub_path):
    available = {m["id"]: m["available"] for m in models()}
    assert available == {"kimi": True, "claude": True,
                         "codex": True, "grok": True}
    by_id = {m["id"]: m for m in models()}
    assert by_id["kimi"]["name"] == "Kimi"
    assert "Kimi" in by_id["kimi"]["hint"]


def test_models_unavailable(no_tools):
    available = {m["id"]: m["available"] for m in models()}
    assert available == {"kimi": False, "claude": False,
                         "codex": False, "grok": False}
    assert "не установлен" in models()[0]["hint"]


def test_run_with_missing_model(no_tools, sessions, store):
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="kimi")
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert errors and "Kimi не установлен" in errors[0]["message"]


def test_error_mode(stub_path, sessions, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "error")
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="claude")
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert len(errors) == 1
    assert errors[0]["code"] == 2
    assert "boom" in errors[0]["output"]
    assert 3 not in store.digests


def test_garbage_mode(stub_path, sessions, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "garbage")
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="grok")
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert len(errors) == 1
    assert "неверный ответ" in errors[0]["message"]
    assert 3 not in store.digests


def test_hang_times_out(stub_path, sessions, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "hang")
    monkeypatch.setenv("AI_THREADS_DIGEST_TIMEOUT", "1")
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="kimi")
    wait_finished(job)
    errors = [e for e in all_events(job) if e["type"] == "error"]
    assert len(errors) == 1
    assert "Kimi не ответил за 1 с" in errors[0]["message"]
    assert 3 not in store.digests


def test_cancel_keeps_previous_digest(stub_path, sessions, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "hang")
    store.set_digest(3, {"at": 1.0, "model": "kimi", "result": {"lead": "старое"}})
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="codex")
    wait_for(job, lambda e: e["type"] == "stage")
    job.cancel()
    wait_finished(job)
    assert store.digest(3)["result"] == {"lead": "старое"}


def test_slow_mode_streams_events(stub_path, sessions, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "slow")
    jobs = Jobs()
    job = start_digest(jobs, sessions, store, model="kimi")
    wait_for(job, lambda e: e["type"] == "session"
             and e.get("status") == "now")
    wait_finished(job, timeout=15)
    assert 3 in store.digests


def test_parse_digest_validates_structure():
    with pytest.raises(ValueError):
        parse_digest("не json", set(), set())
    with pytest.raises(ValueError):
        parse_digest('{"projects": [], "tails": [], "autos": []}',
                     set(), set())  # нет lead
    with pytest.raises(ValueError):
        parse_digest('{"lead": "x", "projects": "nope", "tails": [], '
                     '"autos": []}', set(), set())
    parsed = parse_digest(
        '{"lead": "x", "projects": [{"name": "p", "bullets": '
        '[{"text": "t", "keys": ["a", "b"]}]}], '
        '"tails": [{"text": "h", "key": "a"}], "autos": []}',
        {"a"}, {"c"})
    assert parsed["projects"][0]["bullets"][0]["keys"] == ["a"]
    assert parsed["tails"][0]["id"] == "t1"
    assert parsed["autos"] == []


def _session(sid: str, **kwargs) -> Session:
    kwargs.setdefault("tool", "claude")
    kwargs.setdefault("title", sid)
    kwargs.setdefault("cwd", "/home/user/projects/sample")
    kwargs.setdefault("updated", time.time())
    kwargs.setdefault("journal", Path("/nonexistent/journal.jsonl"))
    return Session(id=sid, **kwargs)


def test_select_for_digest_filters_period_and_flags():
    today = datetime.date(2026, 9, 28)
    start = datetime.datetime.combine(
        datetime.date(2026, 9, 26), datetime.time.min).timestamp()
    inside = _session("in", updated=start + 60)
    at_boundary = _session("edge", updated=start)
    before = _session("old", updated=start - 1)
    auto = _session("au", updated=start + 60, auto=True)
    temp = _session("tm", updated=start + 60, cwd="/tmp/scratch/x")
    empty = _session("em", updated=start + 60, empty=True)
    hidden = _session("hid", updated=start + 60)
    manual, autos = select_for_digest(
        [inside, at_boundary, before, auto, temp, empty, hidden], 3,
        {"claude:hid"}, today=today)
    assert [s.key for s in manual] == ["claude:in", "claude:edge",
                                       "claude:em"]
    assert [s.key for s in autos] == ["claude:au"]


def test_excerpt_takes_first_user_message_and_ending(tmp_path):
    journal = tmp_path / "journal.jsonl"
    rows = [
        {"type": "user", "timestamp": "2026-09-28T08:00:00Z",
         "message": {"role": "user", "content": "Первое сообщение"}},
        {"type": "assistant", "timestamp": "2026-09-28T08:05:00Z",
         "message": {"role": "assistant",
                     "content": [{"type": "text", "text": "Первый ответ"}]}},
        {"type": "user", "timestamp": "2026-09-28T09:00:00Z",
         "message": {"role": "user", "content": "Второй вопрос"}},
        {"type": "assistant", "timestamp": "2026-09-28T09:05:00Z",
         "message": {"role": "assistant",
                     "content": [{"type": "text", "text": "Итоговый ответ"}]}},
    ]
    journal.write_text("\n".join(json.dumps(r) for r in rows) + "\n",
                       encoding="utf-8")
    session = _session("ex", journal=journal)
    excerpt = _excerpt(session)
    assert excerpt.startswith("начало: Первое сообщение")
    assert "конец:" in excerpt
    # в конец попадают последние три сообщения
    assert "assistant: Первый ответ" in excerpt
    assert "assistant: Итоговый ответ" in excerpt
    # больше бюджета не бывает
    long_text = "слово " * 2000
    rows[0]["message"]["content"] = long_text
    rows[-1]["message"]["content"] = [{"type": "text", "text": long_text}]
    journal.write_text("\n".join(json.dumps(r) for r in rows) + "\n",
                       encoding="utf-8")
    assert len(_excerpt(session)) <= 900 + 1  # «…» на месте обрезки


def test_tracker_marks_summary_and_excerpt_as_ok(tmp_path):
    job = Job("digest", "digest")
    ready = _session("ready", journal=tmp_path / "r.jsonl")
    excerpted = _session("exc", journal=tmp_path / "e.jsonl")
    bare = _session("bare", journal=tmp_path / "b.jsonl")
    empty = _session("em", journal=tmp_path / "m.jsonl", empty=True)
    auto = _session("au", journal=tmp_path / "a.jsonl", auto=True)
    tracker = _Tracker(job, [ready, excerpted, bare, empty], [auto],
                       {"claude:ready": {"title": "готово"}},
                       {"claude:exc": "начало: выдержка"})
    events, _ = job.events(0, 0)
    statuses = {e["key"]: e["status"] for e in events if e["type"] == "session"}
    assert statuses == {"claude:ready": "ok", "claude:exc": "ok",
                        "claude:bare": "queued", "claude:em": "skip"}
    # модель может перепроверить журнал даже с готовой сводкой —
    # статус честно уходит в now, после чтения — в read
    tracker.read_start(str(tmp_path / "r.jsonl"))
    assert tracker.status["claude:ready"] == "now"
    tracker.read_end(str(tmp_path / "r.jsonl"))
    assert tracker.status["claude:ready"] == "read"
    events, _ = job.events(0, 0)
    last = {e["key"]: e["status"] for e in events if e["type"] == "session"}
    assert last["claude:ready"] == "read"


def test_build_prompt_includes_excerpts_and_auto_answers(tmp_path):
    journal = tmp_path / "wire.jsonl"
    journal.write_text('{"type":"user","message":{"role":"user","content":"'
                       'Начало работы"}}\n', encoding="utf-8")
    manual = [_session("m1", journal=journal)]
    auto = _session("a1", auto=True, journal=journal)
    auto.last = "Последний ответ агента"
    prompt = build_prompt(manual, [auto], 3, {}, {},
                          {"claude:m1": "начало: Начало работы"})
    assert "выдержка: начало: Начало работы" in prompt
    assert "последний ответ: Последний ответ агента" in prompt
    assert "журнал: " in prompt  # пути журналов — для чтения моделью


def test_short_prompt_carries_language(monkeypatch, tmp_path):
    from ai_threads.digest import short_prompt
    import os
    assert "на русском языке" in short_prompt(3)
    Path(os.environ["AI_THREADS_CONFIG"]).write_text('{"language": "en"}', encoding="utf-8")
    assert "in English" in short_prompt(3)
