import json
import threading
from pathlib import Path

import pytest

from ai_threads import sources
from ai_threads.catalog import Catalog
from .a_support import homes, key, sid


def load(path):
    catalog = Catalog(path)
    assert catalog.wait(10), catalog.error
    assert catalog.error is None
    return catalog


def test_persistent_cache_does_not_open_unchanged_files(homes, tmp_path, monkeypatch):
    path = tmp_path / "cache.json"
    first = load(path)
    assert len(first.sessions()) == 13
    original = Path.open

    def guarded(p, *args, **kwargs):
        assert not p.is_relative_to(homes), f"Повторное чтение {p}"
        return original(p, *args, **kwargs)

    monkeypatch.setattr(Path, "open", guarded)
    second = load(path)
    assert [s.to_json() for s in first.sessions()] == [s.to_json() for s in second.sessions()]


def test_changes_deletions_and_conversation_dependency(homes, tmp_path, monkeypatch):
    catalog = load(tmp_path / "cache.json")
    modified = catalog.get(key("grok"))
    deleted = catalog.get(key("claude"))
    with modified.journal.open("a") as stream:
        stream.write(json.dumps({"type": "assistant", "content": "Новый ответ"}) + "\n")
    deleted.meta_path.unlink()
    opened = []
    original = Path.open

    def track(p, *args, **kwargs):
        if p.is_relative_to(homes):
            opened.append(p)
        return original(p, *args, **kwargs)

    monkeypatch.setattr(Path, "open", track)
    catalog.refresh(force=True)
    assert catalog.wait(10)
    assert catalog.get(deleted.key) is None
    assert catalog.get(modified.key).last == "Новый ответ"
    assert set(opened) == {modified.meta_path, modified.journal}
    assert str(deleted.meta_path) not in json.loads(catalog.path.read_text())


def test_first_scan_progress(homes, tmp_path, monkeypatch):
    entered, release = threading.Event(), threading.Event()
    original = sources.scan

    def blocked(tool, *args, **kwargs):
        if tool == "claude":
            entered.set()
            assert release.wait(5)
        return original(tool, *args, **kwargs)

    monkeypatch.setattr(sources, "scan", blocked)
    catalog = Catalog(tmp_path / "cache.json")
    try:
        assert entered.wait(5)
        assert not catalog.ready
        progress = catalog.progress()
        assert 0 < progress["done"] < progress["total"]
        assert progress["tools"] == {"codex": "done", "claude": "reading", "grok": "waiting", "kimi": "waiting"}
        assert len(catalog.sessions()) == 3
    finally:
        release.set()
    assert catalog.wait(10)
    assert catalog.progress()["done"] == catalog.progress()["total"]
    assert all(status == "done" for status in catalog.progress()["tools"].values())


@pytest.mark.parametrize("content", ["{broken", "[]", '{"not-a-path": 42}'])
def test_bad_cache_is_rebuilt(homes, tmp_path, content):
    path = tmp_path / "cache.json"
    path.write_text(content)
    assert len(load(path).sessions()) == 13


@pytest.mark.parametrize("damage", ["index-values", "index-offset", "claude-cwd", "session-title", "kimi-prompts"])
def test_corrupt_cache_entries_are_rebuilt(homes, tmp_path, damage):
    path = tmp_path / "cache.json"
    load(path)
    cache = json.loads(path.read_text())
    if damage.startswith("index-"):
        entry = cache[str(homes / "codex/session_index.jsonl")]
        entry["values" if damage == "index-values" else "offset"] = 12 if damage == "index-values" else "bad"
    else:
        tool = "kimi" if damage == "kimi-prompts" else "claude"
        entry = next(e for e in cache.values() if e.get("tool") == tool)
        if damage == "claude-cwd":
            del entry["journal_cwd"]
        else:
            entry["prompts" if damage == "kimi-prompts" else "title"] = 12
    path.write_text(json.dumps(cache))
    assert len(load(path).sessions()) == 13


@pytest.mark.parametrize("tool,index,record", [
    ("codex", "session_index.jsonl", {"id": sid(), "thread_name": "Новое название"}),
    ("claude", "history.jsonl", {"sessionId": sid(3), "display": "Новая история", "project": "/home/user/new"}),
])
def test_append_index_offset_and_truncation(homes, tmp_path, monkeypatch, tool, index, record):
    path = tmp_path / "cache.json"
    catalog = load(path)
    index_path = homes / tool / index
    offset = index_path.stat().st_size
    with index_path.open("ab") as stream:
        stream.write(json.dumps(record, ensure_ascii=False).encode())
    catalog.refresh(force=True)
    assert catalog.wait(10)
    assert json.loads(path.read_text())[str(index_path)]["offset"] == offset
    with index_path.open("ab") as stream:
        stream.write(b"\n")
    catalog.refresh(force=True)
    assert catalog.wait(10)
    assert catalog.get(key(tool, 1 if tool == "codex" else 3)).title == record.get("thread_name", record.get("display"))
    assert json.loads(path.read_text())[str(index_path)]["offset"] == index_path.stat().st_size
    index_path.write_text("{}\n")
    rebuilt = load(path)
    assert rebuilt.get(key(tool, 1 if tool == "codex" else 3)).title != record.get("thread_name", record.get("display"))


def test_search_cache_and_invalidation(homes, tmp_path, monkeypatch):
    catalog = load(tmp_path / "cache.json")
    session = catalog.get(key())
    assert catalog.search("ЗАПИСЬ", [session.key])[0]["key"] == session.key
    original = Path.open

    def guarded(p, *args, **kwargs):
        assert p != session.journal
        return original(p, *args, **kwargs)

    with monkeypatch.context() as context:
        context.setattr(Path, "open", guarded)
        assert len(catalog.search("найдена", [session.key])[0]["snippet"]) <= 160
        assert catalog.search("Служебный", [session.key]) == []
    with session.journal.open("a") as stream:
        stream.write(json.dumps({"type": "response_item", "payload": {"type": "message", "role": "assistant", "content": "Обновление"}}) + "\n")
    assert catalog.search("обновление", [session.key])
    assert catalog.search("", [session.key]) == []


def test_search_reads_tail_and_does_not_cut_long_messages(homes, tmp_path):
    catalog = load(tmp_path / "cache.json")
    session = catalog.get(key("grok"))
    with session.journal.open("w") as stream:
        stream.write(json.dumps({"type": "user", "content": "older-marker"}) + "\n")
        stream.write('{"type":"ignored","text":"' + "x" * (600 * 1024) + '"}\n')
        stream.write(json.dumps({"type": "assistant", "content": "x" * 14000 + "recent-marker"}) + "\n")
    assert catalog.search("older-marker", [session.key]) == []
    assert catalog.search("recent-marker", [session.key])[0]["key"] == session.key


def test_forced_refresh_during_scan_runs_again(homes, tmp_path, monkeypatch):
    """Настройки сохранили посреди чтения: каталог дочитывает и читает всё ещё раз."""
    entered, release = threading.Event(), threading.Event()
    original = sources.scan
    calls = []

    def blocked(tool, *args, **kwargs):
        calls.append(tool)
        if tool == "codex" and len(calls) == 1:
            entered.set()
            assert release.wait(5)
        return original(tool, *args, **kwargs)

    monkeypatch.setattr(sources, "scan", blocked)
    catalog = Catalog(tmp_path / "cache.json")
    assert entered.wait(5)
    catalog.refresh(force=True)
    release.set()
    assert catalog.wait(10)
    assert calls.count("codex") == 2
