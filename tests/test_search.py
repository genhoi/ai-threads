"""Умный поиск: разбор запроса агентом, поиск слов в журналах, отбор агентом."""

from __future__ import annotations

import json

import pytest

from ai_threads.catalog import Catalog
from ai_threads.jobs import Jobs
from ai_threads.search import (fallback_plan, needles, parse_plan, parse_rank, rank_prompt, run_search,
                               score, select)

from .a_support import homes, key
from .conftest import all_events, wait_finished


@pytest.fixture
def sessions(homes, tmp_path):
    catalog = Catalog(tmp_path / "cache.json")
    assert catalog.wait(10)
    return catalog.sessions()


def search(sessions, query="найди сессии про каталог", scope="mine"):
    jobs = Jobs()
    job = jobs.start("search:1", "search", None,
                     lambda job: run_search(job, sessions, query, scope, [], {}, {}))
    wait_finished(job)
    return all_events(job)


def result_of(events):
    results = [e for e in events if e["type"] == "result"]
    assert len(results) == 1, events
    return results[0]["result"]


def test_plan_scan_and_rank(stub_path, sessions):
    events = search(sessions)
    plan = next(e for e in events if e["type"] == "plan")
    assert plan["terms"] == ["каталог"]
    progress = [e["text"] for e in events if e["type"] == "progress"]
    assert progress[0] == "понимает запрос" and progress[-1] == "отбирает сессии"
    assert any(text.startswith("ищет в журналах:") for text in progress)
    assert all(e["agent"] == "kimi" for e in events if e["type"] == "progress")
    result = result_of(events)
    assert result["ranked"] is True
    # «Мои»: без автоматических и пустых; заглушка возвращает два первых ключа в обратном порядке
    assert result["scanned"] == 5
    assert len(result["results"]) == 2
    assert all(r["why"] == f"нашлось: {r['key']}" and r["score"] > 0 for r in result["results"])
    assert not [e for e in events if e["type"] in ("warning", "error")]


def test_agents_from_plan_limit_the_search(stub_path, sessions, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_AGENTS", "claude,codex,ghost")
    events = search(sessions)
    plan = next(e for e in events if e["type"] == "plan")
    assert plan["agents"] == ["claude", "codex"]
    result = result_of(events)
    assert result["scanned"] == 2
    assert {r["key"].split(":")[0] for r in result["results"]} <= {"claude", "codex"}


def test_all_scope_includes_automatic_sessions(stub_path, sessions):
    assert result_of(search(sessions, scope="all"))["scanned"] > result_of(search(sessions))["scanned"]


def test_no_matches_skips_ranking(stub_path, sessions, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_TERMS", "message_profile")
    events = search(sessions)
    result = result_of(events)
    assert result["results"] == [] and result["ranked"] is False
    assert [e["text"] for e in events if e["type"] == "progress"][-1] != "отбирает сессии"


def test_garbage_answers_fall_back_to_words(stub_path, sessions, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "garbage")
    events = search(sessions, "каталог записей")
    warnings = [e["message"] for e in events if e["type"] == "warning"]
    assert len(warnings) == 2 and "не разобрал запрос" in warnings[0] and "не отобрал" in warnings[1]
    plan = next(e for e in events if e["type"] == "plan")
    assert plan["terms"] == ["каталог", "записей"]
    result = result_of(events)
    assert result["ranked"] is False and result["results"]
    assert all(r["why"] for r in result["results"])


def test_failed_cli_reports_error_then_falls_back(stub_path, sessions, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "error")
    events = search(sessions, "каталог")
    assert any("завершился с кодом 2" in e["message"] for e in events if e["type"] == "warning")
    assert result_of(events)["ranked"] is False


def test_no_runner(no_tools, sessions):
    events = search(sessions)
    errors = [e for e in events if e["type"] == "error"]
    assert errors and "Не нашёл ни одного агента" in errors[0]["message"]


def test_parse_plan_filters_values():
    text = json.dumps({"terms": ["message_profile", "Партиц", "партиц", "x", 5],
                       "agents": ["claude", "ghost"], "projects": ["SHOP-API", "нет такого"], "days": 7})
    plan = parse_plan(text, ["shop-api", "infra"])
    assert plan == {"terms": ["message_profile", "Партиц"], "agents": ["claude"], "projects": ["shop-api"], "days": 7}
    assert parse_plan(json.dumps({"terms": ["a1"], "days": True}), [])["days"] is None
    with pytest.raises(ValueError):
        parse_plan(json.dumps({"terms": []}), [])


def test_fallback_plan_drops_service_words():
    assert fallback_plan("Найди сессии по переходу на партиции message_profile")["terms"] == [
        "переходу", "партиции", "message_profile"]


def test_parse_rank_keeps_only_known_keys():
    text = json.dumps({"results": [{"key": "codex:a", "why": "x" * 300}, {"key": "ghost:b", "why": "y"},
                                   {"key": "codex:a", "why": "повтор"}, {"key": "claude:c", "why": 5}]})
    assert parse_rank(text, {"codex:a", "claude:c"}) == [{"key": "codex:a", "why": "x" * 160},
                                                         {"key": "claude:c", "why": "5"}]


def test_escaped_cyrillic_and_rare_terms_score_higher(tmp_path):
    from ai_threads.model import Session

    def session(n, text):
        journal = tmp_path / f"{n}.jsonl"
        journal.write_text(json.dumps({"type": "response_item", "payload": {
            "type": "message", "role": "user", "content": [{"type": "input_text", "text": text}]}}) + "\n",
            encoding="utf-8")
        return Session(tool="codex", id=f"00000000-0000-4000-8000-00000000000{n}", title="t", cwd="",
                       updated=n, journal=journal)

    # json.dumps экранирует кириллицу, как делают некоторые журналы
    rare = session(1, "Переход на партиции message_profile")
    common = session(2, "просто партиции")
    other = session(3, "ничего общего")
    assert needles("Партиц")[1] == b"\\u043f\\u0430\\u0440\\u0442\\u0438\\u0446"

    class Job:
        cancelled = False

    ranked = score(Job(), [rare, common, other], ["message_profile", "партиц"], {}, {}, lambda done, total: None)
    assert [s.key for _, s, _ in ranked] == [rare.key, common.key]
    assert ranked[0][2] == ["message_profile", "партиц"]


def test_rank_prompt_respects_size_limit():
    entry = {"key": "codex:a", "agent": "Codex", "project": "p", "date": "2026-10-01", "title": "t",
             "summary": "", "first": "", "matched": ["x"], "snippets": ["я: " + "ф" * 200]}
    prompt = rank_prompt("запрос", [dict(entry, key=f"codex:{i}") for i in range(1000)])
    assert len(prompt) <= 60_000 and "codex:0" in prompt and "codex:999" not in prompt


def test_select_respects_scope_hidden_and_days(sessions):
    plan = {"terms": ["x"], "agents": [], "projects": [], "days": None}
    mine = select(sessions, plan, "mine", [key("codex")])
    assert key("codex") not in {s.key for s in mine}
    assert all(not s.auto and not s.empty for s in mine)
    assert select(sessions, dict(plan, days=1), "all", []) == []


def test_uppercase_cyrillic_is_found(tmp_path):
    from ai_threads.model import Session

    journal = tmp_path / "1.jsonl"
    journal.write_text(json.dumps({"type": "response_item", "payload": {"type": "message", "role": "user",
                       "content": [{"type": "input_text", "text": "ПАРТИЦИОНИРОВАНИЕ и Партиции"}]}},
                                  ensure_ascii=False) + "\n", encoding="utf-8")
    session = Session(tool="codex", id="00000000-0000-4000-8000-000000000001", title="t", cwd="", updated=1,
                      journal=journal)

    class Job:
        cancelled = False

    ranked = score(Job(), [session], ["партиц"], {}, {}, lambda done, total: None)
    assert ranked and ranked[0][2] == ["партиц"]


def test_tool_output_is_not_sent_to_the_model(tmp_path):
    from ai_threads.model import Session
    from ai_threads.search import snippets

    rows = [{"type": "response_item", "payload": {"type": "message", "role": "user",
             "content": [{"type": "input_text", "text": "Проверь конфиг"}]}},
            {"type": "response_item", "payload": {"type": "function_call_output", "output": "SECRET_TOKEN=abc123"}}]
    journal = tmp_path / "1.jsonl"
    journal.write_text("".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8")
    session = Session(tool="codex", id="00000000-0000-4000-8000-000000000001", title="t", cwd="", updated=1,
                      journal=journal)
    assert snippets(session, ["secret_token"]) == []
    entry = {"key": "codex:a", "agent": "Codex", "project": "p", "date": "2026-10-01", "title": "t",
             "summary": "", "first": "Проверь конфиг", "matched": ["secret_token"], "snippets": []}
    prompt = rank_prompt("токен", [entry])
    assert "abc123" not in prompt and "только в выводе команд" in prompt


def test_title_case_cyrillic_is_found():
    assert "База Данных".lower().encode() in [n for n in needles("база данных")] or \
        "База Данных".encode() in needles("база данных")


def test_full_search_finds_words_in_tool_output(sessions, tmp_path):
    from ai_threads.search import run_full_search
    jobs = Jobs()
    job = jobs.start("fullsearch:1", "fullsearch", None,
                     lambda job: run_full_search(job, sessions, "каталоге", "all", [], {}, {}))
    wait_finished(job)
    events = all_events(job)
    progress = [e for e in events if e["type"] == "progress"]
    assert progress and progress[-1]["done"] == progress[-1]["total"]
    assert progress[-1]["text"].startswith("ищет в журналах")
    result = result_of(events)
    assert result["terms"] == ["каталоге"] and result["found"] == len(result["results"]) > 0
    assert all(r["snippet"] and "каталог" in r["snippet"].lower() for r in result["results"])


def test_full_search_needs_every_word(sessions):
    from ai_threads.search import run_full_search
    jobs = Jobs()
    job = jobs.start("fullsearch:2", "fullsearch", None,
                     lambda job: run_full_search(job, sessions, "каталоге message_profile", "all", [], {}, {}))
    wait_finished(job)
    assert result_of(all_events(job))["results"] == []


def test_raw_snippet_shows_tool_output_locally(tmp_path):
    from ai_threads.model import Session
    from ai_threads.search import raw_snippet
    rows = [{"type": "response_item", "payload": {"type": "function_call_output", "output": "ticket SHOP-1042 closed"}}]
    journal = tmp_path / "1.jsonl"
    journal.write_text("".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8")
    session = Session(tool="codex", id="00000000-0000-4000-8000-000000000001", title="t", cwd="", updated=1,
                      journal=journal)
    assert "SHOP-1042 closed" in raw_snippet(session, ["shop-1042"])
