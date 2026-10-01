import json
import shlex
import time
from pathlib import Path

import pytest

from ai_threads import sources
from ai_threads.agents.base import PARSER_VERSION
from ai_threads.sources.common import HEAD_BYTES, TAIL_BYTES, json_rows, messages_from_rows
from .a_support import TOOLS, homes, key, sid


@pytest.mark.parametrize("tool", TOOLS)
def test_manual_and_automatic(homes, tool):
    sessions = {s.key: s for s in sources.scan(tool, {})}
    manual, auto, empty = (sessions[key(tool, n)] for n in (1, 2, 3))
    assert manual.id == ("session_" if tool == "kimi" else "") + sid()
    assert manual.title == "Проверить каталог"
    assert manual.cwd == "/home/user/projects/sample app"
    assert manual.project == "sample app"
    assert manual.branch == ("" if tool == "kimi" else "feature/catalog")
    assert manual.created == 1790582400 and manual.updated == 1790589600
    assert manual.auto is False and manual.by == ""
    assert not manual.empty and manual.last == "Запись найдена. Проверка завершена."
    assert manual.journal.is_file() and manual.meta_path.is_file()
    assert auto.auto is True
    assert auto.by == {"codex": "codex exec", "claude": "claude -p", "grok": "grok headless", "kimi": "kimi -p"}[tool]
    command = {"codex": "codex resume", "claude": "claude --resume", "grok": "grok --resume", "kimi": "kimi --session"}[tool]
    assert manual.command_no_cd == f"{command} {manual.id}"
    assert manual.command == f"cd -- {shlex.quote(manual.cwd)} && {manual.command_no_cd}"
    if tool == "codex":
        assert auto.command_no_cd == f"codex exec resume {auto.id}"
    assert empty.empty and not empty.last and sources.messages(empty) == []
    assert key(tool, 9) not in sessions


@pytest.mark.parametrize("tool", TOOLS)
def test_messages_contain_only_conversation(homes, tool):
    session = next(s for s in sources.scan(tool, {}) if s.key == key(tool))
    messages = sources.messages(session)
    assert [(m["role"], m["text"]) for m in messages] == [
        ("user", "Найди запись в каталоге и проверь её название"),
        ("assistant", "Запись найдена. Проверка завершена.")]
    assert all(m["at"] is None or m["at"].startswith("2026-09-28T") for m in messages)


def test_kimi_old_format(homes):
    session = next(s for s in sources.scan("kimi", {}) if s.key == key("kimi", 4))
    assert session.id == "session_" + sid(4)
    assert session.cwd == "/home/user/projects/sample app"
    assert session.created == 1790582400 and not session.auto


def test_kimi_context_format_excludes_injections_and_tools(homes):
    session = next(s for s in sources.scan("kimi", {}) if s.key == key("kimi", 2))
    session.journal.write_bytes((homes / "kimi/formats/context-wire.jsonl").read_bytes())
    session = next(s for s in sources.scan("kimi", {}) if s.key == key("kimi", 2))
    assert session.auto and not session.empty
    assert session.last == "Запись найдена. Проверка завершена."
    assert [(m["role"], m["text"]) for m in sources.messages(session)] == [
        ("user", "Проверь изменения без ручного запуска"), ("assistant", "Запись найдена. Проверка завершена.")]


def test_codex_uses_first_source(homes):
    path = sources.discover("codex")[0]
    with path.open("a") as stream:
        stream.write(json.dumps({"type": "session_meta", "payload": {"source": "exec"}}) + "\n")
    session = next(s for s in sources.scan("codex", {}) if s.key == key())
    assert not session.auto and session.by == ""


def test_title_precedence_and_kimi_prompt_matching(homes):
    cache = {}
    claude = {s.id: s for s in sources.scan("claude", cache)}
    assert claude[sid()].title == "Проверить каталог"
    assert claude[sid(2)].title == "Проверить изменения"
    with (homes / "kimi/user-history/history.jsonl").open("a") as stream:
        stream.write(json.dumps({"content": "Проверь  изменения\nбез ручного запуска"}) + "\n")
    assert not next(s for s in sources.scan("kimi", cache) if s.key == key("kimi", 2)).auto


@pytest.mark.parametrize("tool", TOOLS)
def test_large_journal_reads_only_head_and_tail(homes, monkeypatch, tool):
    path = next(p for p in sources.discover(tool) if sid() in str(p))
    journal = sources.journal_path(tool, path)
    original = journal.read_bytes()
    with journal.open("wb") as stream:
        stream.write(original)
        padding = b'{"type":"ignored","text":"' + b'x' * 1000 + b'"}\n'
        for _ in range((50 * 1024 * 1024) // len(padding)):
            stream.write(padding)
        stream.write(original)
    assert journal.stat().st_size >= 50 * 1024 * 1024
    reads = []
    real_open = Path.open

    class Reader:
        def __init__(self, stream):
            self.stream = stream

        def __enter__(self):
            self.stream.__enter__()
            return self

        def __exit__(self, *args):
            return self.stream.__exit__(*args)

        def seek(self, *args):
            return self.stream.seek(*args)

        def read(self, size=-1):
            assert 0 <= size <= HEAD_BYTES
            result = self.stream.read(size)
            reads.append(len(result))
            return result

    def tracked_open(p, *args, **kwargs):
        stream = real_open(p, *args, **kwargs)
        return Reader(stream) if p == journal else stream

    monkeypatch.setattr(Path, "open", tracked_open)
    start = time.perf_counter()
    session = next(s for s in sources.scan(tool, {}) if s.key == key(tool))
    assert time.perf_counter() - start < 2
    assert sum(reads) <= 2 * HEAD_BYTES + TAIL_BYTES + 2
    assert session.title == "Проверить каталог" and session.last == "Запись найдена. Проверка завершена."


def test_incomplete_boundaries_and_service_text(tmp_path):
    path = tmp_path / "journal.jsonl"
    path.write_bytes(b'{"type":"ignored"}\n' + b'x' * 5000 + b'\n{"type":"user","content":"hello"}\n')
    assert list(json_rows(path, 100, tail=True)) == [{"type": "user", "content": "hello"}]
    assert list(json_rows(path, 25)) == [{"type": "ignored"}]
    rows = [{"type": "user", "content": "<environment_context>Служебный текст</environment_context>\nВопрос"},
            {"type": "user", "content": "<INSTRUCTIONS>Служебный текст"},
            {"type": "assistant", "content": [{"type": "thinking", "text": "Скрыто"}, {"type": "text", "text": "Ответ"}]}]
    assert [m["text"] for m in messages_from_rows("grok", rows)] == ["Вопрос", "Ответ"]


def test_transcript_limit_and_truncation(homes):
    session = next(s for s in sources.scan("grok", {}) if s.key == key("grok"))
    session.journal.write_text("".join(json.dumps({"type": "user", "content": str(n)}) + "\n" for n in range(80)))
    result = sources.transcript(session)
    assert result["truncated"] and len(result["messages"]) == 60
    assert result["messages"][0]["text"] == "20" and result["messages"][-1]["text"] == "79"


def test_claude_service_records_are_not_user_messages():
    def user(content, **extra):
        return {"type": "user", "message": {"role": "user", "content": content}, **extra}

    rows = [
        user("Первый вопрос", origin={"kind": "human"}, promptSource="typed"),
        user("<task-notification><task-id>1</task-id></task-notification>", origin={"kind": "task-notification"}),
        user("Another Claude session sent a message", origin={"kind": "peer"}, isMeta=True),
        user("Base directory for this skill: /x", isMeta=True),
        user("This session is being continued from a previous conversation", isCompactSummary=True,
             isVisibleInTranscriptOnly=True),
        user("<local-command-stdout>Set model</local-command-stdout>"),
        user("<command-name>/model</command-name>"),
        user("<bash-stdout>ok</bash-stdout>"),
        user("<bash-input>docker ps</bash-input>"),
        user('<pasted_content id="1a2b">Вставленный текст</pasted_content> и вопрос', origin={"kind": "human"}),
        {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "Ответ"}]}},
    ]
    texts = [(m["role"], m["text"]) for m in messages_from_rows("claude", rows)]
    assert texts == [("user", "Первый вопрос"), ("user", "! docker ps"), ("user", "Вставленный текст и вопрос"),
                     ("assistant", "Ответ")]


def test_codex_service_blocks_are_removed():
    part = {"type": "input_text", "text": "<recommended_plugins>список</recommended_plugins>\n"
                                          '<image name=[Image #1] path="/tmp/a.png">\n</image>\nПочини тест'}
    rows = [{"type": "response_item", "payload": {"type": "message", "role": "user", "content": [part]}}]
    assert [m["text"] for m in messages_from_rows("codex", rows)] == ["Почини тест"]


def test_cache_from_older_parser_is_reparsed(homes):
    cache = {}
    first = {s.key: s for s in sources.scan("claude", cache)}
    for entry in cache.values():
        if entry.get("tool") == "claude":
            entry["parser"] = 1
            entry["first_title"] = "Устаревшее название"
    again = {s.key: s for s in sources.scan("claude", cache)}
    assert again == first
    entries = [e for e in cache.values() if e.get("tool") == "claude"]
    assert entries and all(e["parser"] == PARSER_VERSION and e["first_title"] != "Устаревшее название" for e in entries)


def test_last_answer_is_plain_line():
    from ai_threads.sources.common import plain_line
    text = "## Итог\n**Готово:** `make test` прошёл.\n- пункт session_id"
    assert plain_line(text) == "Итог Готово: make test прошёл. пункт session_id"
