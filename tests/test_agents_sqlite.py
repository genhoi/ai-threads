"""ZCode и Cursor Agent: сессии в базах SQLite. Базы собираются в тесте из синтетических
данных: для ZCode — из SQL-текста в fixtures/zcode, для Cursor — генератором ниже."""

import hashlib
import json
import os
import shlex
import sqlite3
from contextlib import closing

import pytest

from ai_threads import sources
from ai_threads.agents import AGENTS, get
from ai_threads.agents import zcode as zcode_module
from ai_threads.agents.sqlite import MAX_MESSAGES, last_messages
from ai_threads.catalog import Catalog
from ai_threads.summary import export_transcript
from .a_support import FIXTURES, sid

CREATED, UPDATED = 1790582400, 1790589600
CWD = "/home/user/projects/sample app"
SECRET = "ключ-шифрования-не-показывать"


def zkey(n):
    return f"zcode:sess_{sid(n)}"


def ckey(n):
    return f"cursor:{sid(n)}"


def execute(db, sql, *params):
    with closing(sqlite3.connect(db)) as connection, connection:
        connection.execute(sql, params)


@pytest.fixture
def zcode_db(tmp_path, monkeypatch):
    home = tmp_path / "zcode"
    db = home / "cli" / "db" / "db.sqlite"
    db.parent.mkdir(parents=True)
    with closing(sqlite3.connect(db)) as connection:
        connection.executescript((FIXTURES / "zcode" / "db.sql").read_text(encoding="utf-8"))
    monkeypatch.setenv("ZCODE_HOME", str(home))
    return db


# --- ZCode -------------------------------------------------------------------

def test_zcode_sessions(zcode_db):
    calls = []
    sessions = {s.key: s for s in sources.scan("zcode", {}, on_read=calls.append)}
    assert sources.discover("zcode") == [zcode_db]
    assert len(calls) == 1 and sorted(s.key for s in calls[0]) == sorted(sessions)
    # Подагент 002 и прогон workflow 005 — не отдельные сессии.
    assert set(sessions) == {zkey(1), zkey(3), zkey(4)}
    manual = sessions[zkey(1)]
    assert manual.id == f"sess_{sid()}"
    assert manual.title == "Проверить каталог"
    assert manual.cwd == CWD and manual.project == "sample app" and manual.branch == ""
    assert manual.created == CREATED and manual.updated == UPDATED
    assert manual.auto is False and manual.by == ""
    assert not manual.empty and manual.last == "Запись найдена. Проверка завершена."
    assert manual.journal == zcode_db and manual.meta_path == zcode_db
    assert manual.command_no_cd == "" and manual.command == ""
    assert manual.resume_hint == "Продолжить можно только в приложении ZCode"
    assert manual.to_json()["resume_hint"] == manual.resume_hint
    empty = sessions[zkey(3)]
    assert empty.empty and empty.last == "" and empty.title == ""
    assert sources.messages(empty) == []
    # Название по умолчанию — заглушка с датой, вместо неё первый запрос.
    fork = sessions[zkey(4)]
    assert fork.title == "Продолжи проверку в ответвлении" and fork.last == "Ответвление проверено."


def test_zcode_transcript_has_only_visible_text_in_order(zcode_db, tmp_path):
    agent = get("zcode")
    session = next(s for s in sources.scan("zcode", {}) if s.key == zkey(1))
    result = sources.transcript(session)
    assert [(m["role"], m["text"]) for m in result["messages"]] == [
        ("user", "Найди запись в каталоге и проверь её название"),
        ("assistant", "Запись найдена.\nПроверка завершена.")]
    assert result["messages"][0]["at"] == "2026-09-28T08:01:00+00:00"
    assert result["truncated"] is False
    assert agent.excerpt_messages(session) == result["messages"]
    assert agent.plain_journal(session) is False
    text = export_transcript(session, tmp_path / "session.txt").read_text(encoding="utf-8")
    assert "Найди запись в каталоге" in text and "Проверка завершена." in text
    for hidden in ("Служебная вставка", "Напоминание", "Скрытый ответ", "Рассуждение", "Ответ инструмента", "Пересказ"):
        assert hidden not in text


@pytest.mark.parametrize("data, role", [
    ('{"role":"user"}', "user"),
    ('{"role":"assistant","semantics":{"uiVisibility":"visible"}}', "assistant"),
    ('{"role":"user","synthetic":true}', None),
    ('{"role":"assistant","visibility":"model-only"}', None),
    ('{"role":"user","semantics":{"uiVisibility":"hidden"}}', None),
    ('{"role":"system"}', None),
    ("не JSON", None),
])
def test_zcode_hidden_messages(data, role):
    assert zcode_module._visible_role(data) == role


def test_zcode_cache_rereads_only_changed_sessions(zcode_db, monkeypatch):
    cache = {}
    first = {s.key: s for s in sources.scan("zcode", cache)}
    zcode = type(get("zcode"))
    original = zcode._overview
    calls = []

    def counted(self, connection, session_id):
        calls.append(session_id)
        return original(self, connection, session_id)

    monkeypatch.setattr(zcode, "_overview", counted)
    assert {s.key: s for s in sources.scan("zcode", json.loads(json.dumps(cache)))} == first
    assert calls == []
    # Новое название без новых сообщений видно сразу, переписка не перечитывается.
    execute(zcode_db, "UPDATE session SET title = 'Проверить каталог еще раз' WHERE id = ?", f"sess_{sid()}")
    assert next(s for s in sources.scan("zcode", cache) if s.key == zkey(1)).title == "Проверить каталог еще раз"
    assert calls == []
    fork = f"sess_{sid(4)}"
    execute(zcode_db, "INSERT INTO message VALUES ('msg_d4', ?, 1790590000000, 1790590000000, ?, 4)",
            fork, '{"role":"assistant"}')
    execute(zcode_db, "INSERT INTO part VALUES ('prt_d4', 'msg_d4', ?, 1790590000000, 1790590000000, ?, 1)",
            fork, '{"type":"text","text":"Новый ответ"}')
    execute(zcode_db, "UPDATE session SET time_updated = 1790590000000 WHERE id = ?", fork)
    changed = {s.key: s for s in sources.scan("zcode", cache)}
    assert calls == [fork]
    assert changed[zkey(4)].last == "Новый ответ" and changed[zkey(4)].updated == 1790590000


def test_zcode_busy_or_broken_database(zcode_db, monkeypatch):
    agent = get("zcode")
    monkeypatch.setattr(agent, "busy_timeout", 0.05)
    cache = {}
    first = {s.key: s for s in sources.scan("zcode", cache)}
    saved = json.dumps(cache)
    locker = sqlite3.connect(zcode_db, isolation_level=None)
    locker.execute("BEGIN EXCLUSIVE")
    try:
        calls = []
        assert {s.key: s for s in sources.scan("zcode", cache, on_read=calls.append)} == first
        assert len(calls) == 1 and json.dumps(cache) == saved
        assert sources.scan("zcode", {}) == []
        assert agent.transcript(first[zkey(1)], 10**6) == ([], False)
        assert agent.excerpt_messages(first[zkey(1)]) == []
    finally:
        locker.execute("ROLLBACK")
        locker.close()
    zcode_db.write_bytes(b"not a database " * 1000)
    assert {s.key: s for s in sources.scan("zcode", cache)} == first
    assert sources.scan("zcode", {}) == []


def test_zcode_reads_wal_without_writing_next_to_database(zcode_db):
    with closing(sqlite3.connect(zcode_db)) as connection:
        connection.execute("PRAGMA journal_mode=WAL")
    folder = zcode_db.parent
    assert [p.name for p in folder.iterdir()] == ["db.sqlite"]
    assert len(sources.scan("zcode", {})) == 3
    assert [p.name for p in folder.iterdir()] == ["db.sqlite"]
    # Приложение запущено: свежая запись лежит в -wal и ещё не перенесена в основной файл.
    writer = sqlite3.connect(zcode_db)
    try:
        writer.execute("UPDATE session SET title = 'Название из журнала', time_updated = time_updated + 1 WHERE id = ?",
                       (f"sess_{sid()}",))
        writer.commit()
        assert (folder / "db.sqlite-wal").stat().st_size > 0
        session = next(s for s in sources.scan("zcode", {}) if s.key == zkey(1))
        assert session.title == "Название из журнала"
    finally:
        writer.close()


# --- Cursor Agent ------------------------------------------------------------

def _blob(row: dict) -> tuple[bytes, bytes]:
    data = json.dumps(row, ensure_ascii=False).encode()
    return hashlib.sha256(data).digest(), data


def build_store(path, name: str, rows: list, *, wal=False):
    """store.db как у cursor-agent: сообщения в JSON под своим sha256 и корневой protobuf,
    где поле 1 перечисляет ссылки на сообщения. Остальные поля разбор пропускает."""
    with closing(sqlite3.connect(path)) as connection:
        if wal:
            connection.execute("PRAGMA journal_mode=WAL")
        connection.executescript("CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);"
                                 "CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB);")
        # Сообщение, на которое ссылается не поле 1, в переписку попасть не должно.
        other, data = _blob(answer("Ответ из другой ветки"))
        connection.execute("INSERT INTO blobs VALUES (?, ?)", (other.hex(), data))
        # varint, 64 и 32 бита, ссылка длиной 32 байта в поле 8, вложенные данные в поле 18.
        noise = [b"\x10\x96\x01", b"\x19" + bytes(8), b"\x15" + bytes(4), b"\x42\x20" + other, b"\x92\x01\x05hello"]
        root = noise[0]
        for index, row in enumerate(rows):
            digest, data = _blob(row)
            connection.execute("INSERT OR IGNORE INTO blobs VALUES (?, ?)", (digest.hex(), data))
            root += b"\x0a\x20" + digest + noise[1 + index % 4]
        root_id = hashlib.sha256(root).hexdigest()
        connection.execute("INSERT INTO blobs VALUES (?, ?)", (root_id, root))
        connection.execute("INSERT INTO blobs VALUES (?, ?)", ("ab" * 32, b"\x0a\x03\x01\x02\x03"))
        meta = {"agentId": "agent", "latestRootBlobId": root_id, "name": name, "mode": "default",
                "createdAt": CREATED * 1000, "blobEncryptionKey": SECRET}
        connection.execute("INSERT INTO meta VALUES ('0', ?)", (json.dumps(meta, ensure_ascii=False).encode().hex(),))
        connection.commit()


def chat(home, n, meta=None, rows=None, *, name="", wal=False):
    folder = home / "chats" / hashlib.md5(CWD.encode()).hexdigest() / sid(n)
    folder.mkdir(parents=True)
    data = {"schemaVersion": 1, "createdAtMs": CREATED * 1000, "updatedAtMs": UPDATED * 1000,
            "hasConversation": rows is not None, **(meta or {})}
    (folder / "meta.json").write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    if rows is not None:
        build_store(folder / "store.db", name, rows, wal=wal)
    return folder


def query(text, at="Monday, Sep 28, 2026, 5:01 PM (UTC+9)"):
    text = f"<timestamp>{at}</timestamp>\n<user_query>\n{text}\n</user_query>"
    return {"role": "user", "content": [{"type": "text", "text": text}]}


def answer(text):
    return {"role": "assistant", "content": [{"type": "text", "text": text}]}


CHAT = [
    {"role": "system", "content": "Ты помощник для программирования."},
    {"role": "user", "content": f"<user_info>\nOS Version: linux\nWorkspace Path: {CWD}\n</user_info>\n"
                                "<git_status>Изменений нет</git_status>\n<rules>Правила проекта</rules>"},
    query("Найди запись в каталоге и проверь её название"),
    {"role": "assistant", "content": [{"type": "reasoning", "text": "Рассуждение модели"},
                                      {"type": "text", "text": "Ищу запись."},
                                      {"type": "tool-call", "toolCallId": "call-1", "toolName": "Read",
                                       "args": {"path": "catalog.json"}}]},
    {"role": "tool", "content": [{"type": "tool-result", "toolCallId": "call-1", "result": "Ответ инструмента"}]},
    {"role": "user", "content": [{"type": "text", "text": "<system_reminder>Служебное напоминание</system_reminder>"}]},
    answer("Запись найдена. Проверка завершена."),
]


@pytest.fixture
def cursor_home(tmp_path, monkeypatch):
    home = tmp_path / "cursor"
    monkeypatch.setenv("CURSOR_HOME", str(home))
    chat(home, 1, {}, CHAT, name="Проверить каталог")
    chat(home, 2, {"title": "Проверить изменения", "cwd": "/home/user/projects/other app"},
         [query("Проверь изменения", at=""), answer("Изменений нет.")], name="Название из базы", wal=True)
    chat(home, 3)
    chat(home, 4, {}, [query("Собери отчёт по каталогу"), answer("Отчёт готов.")])
    return home


def test_cursor_sessions(cursor_home):
    calls = []
    sessions = {s.key: s for s in sources.scan("cursor", {}, on_read=calls.append)}
    assert len(sources.discover("cursor")) == len(calls) == 4
    assert set(sessions) == {ckey(1), ckey(2), ckey(3), ckey(4)}
    manual = sessions[ckey(1)]
    folder = manual.meta_path.parent
    assert manual.id == sid() and manual.title == "Проверить каталог"
    assert manual.cwd == CWD and manual.branch == ""
    assert manual.created == CREATED and manual.updated == UPDATED
    assert manual.auto is False and manual.by == ""
    assert not manual.empty and manual.last == "Запись найдена. Проверка завершена."
    assert manual.journal == folder / "store.db" and manual.meta_path == folder / "meta.json"
    assert manual.command_no_cd == f"cursor-agent --resume {sid()}"
    assert manual.command == f"cd -- {shlex.quote(CWD)} && cursor-agent --resume {sid()}"
    assert manual.resume_hint == ""
    titled = sessions[ckey(2)]
    assert titled.title == "Проверить изменения" and titled.cwd == "/home/user/projects/other app"
    assert titled.last == "Изменений нет."
    # База в режиме WAL прочитана, рядом не появилось ни -wal, ни -shm.
    assert sorted(p.name for p in titled.meta_path.parent.iterdir()) == ["meta.json", "store.db"]
    empty = sessions[ckey(3)]
    assert empty.empty and empty.title == "" and empty.cwd == "" and empty.last == ""
    assert sources.messages(empty) == []
    assert sessions[ckey(4)].title == "Собери отчёт по каталогу"


def test_cursor_transcript_has_only_queries_and_answers(cursor_home):
    agent = get("cursor")
    session = next(s for s in sources.scan("cursor", {}) if s.key == ckey(1))
    result = sources.transcript(session)
    assert [(m["role"], m["text"]) for m in result["messages"]] == [
        ("user", "Найди запись в каталоге и проверь её название"),
        ("assistant", "Ищу запись."),
        ("assistant", "Запись найдена. Проверка завершена.")]
    assert result["messages"][0]["at"] == "2026-09-28T08:01:00+00:00" and result["messages"][1]["at"] is None
    assert result["truncated"] is False
    assert agent.excerpt_messages(session) == result["messages"]
    assert agent.plain_journal(session) is False
    second = next(s for s in sources.scan("cursor", {}) if s.key == ckey(2))
    assert sources.messages(second)[0] == {"role": "user", "text": "Проверь изменения", "at": None}


def test_cursor_cache_rereads_only_changed_chats(cursor_home, monkeypatch):
    cache = {}
    first = {s.key: s for s in sources.scan("cursor", cache)}
    cursor = type(get("cursor"))
    original = cursor._read_store
    calls = []

    def counted(self, store):
        calls.append(store.parent.name)
        return original(self, store)

    monkeypatch.setattr(cursor, "_read_store", counted)
    assert {s.key: s for s in sources.scan("cursor", json.loads(json.dumps(cache)))} == first
    assert calls == []
    store = first[ckey(4)].journal
    store.unlink()
    build_store(store, "", [query("Собери отчёт по каталогу"), answer("Отчёт готов."), answer("Новый ответ")])
    changed = {s.key: s for s in sources.scan("cursor", cache)}
    assert calls == [sid(4)] and changed[ckey(4)].last == "Новый ответ"
    dumped = json.dumps(cache, ensure_ascii=False)
    assert SECRET not in dumped and "blobEncryptionKey" not in dumped


def test_cursor_busy_or_broken_store(cursor_home, monkeypatch):
    agent = get("cursor")
    monkeypatch.setattr(agent, "busy_timeout", 0.05)
    cache = {}
    first = {s.key: s for s in sources.scan("cursor", cache)}
    session = first[ckey(1)]
    locker = sqlite3.connect(session.journal, isolation_level=None)
    locker.execute("BEGIN EXCLUSIVE")
    try:
        # Без кеша остаются данные из meta.json.
        alone = {s.key: s for s in sources.scan("cursor", {})}
        assert set(alone) == set(first) and alone[ckey(1)].updated == UPDATED
        # Чат изменился, пока база занята: остаётся прежняя запись кеша.
        stat = session.meta_path.stat()
        os.utime(session.meta_path, ns=(stat.st_atime_ns, stat.st_mtime_ns + 10**9))
        cached = {s.key: s for s in sources.scan("cursor", cache)}
        assert cached[ckey(1)].title == "Проверить каталог" and not cached[ckey(1)].empty
        assert agent.transcript(session, 10**6) == ([], False)
        assert agent.excerpt_messages(session) == []
    finally:
        locker.execute("ROLLBACK")
        locker.close()
    # Блокировка снята — база читается снова.
    assert next(s for s in sources.scan("cursor", cache) if s.key == ckey(1)).last == first[ckey(1)].last
    session.journal.write_bytes(b"not a database " * 1000)
    assert next(s for s in sources.scan("cursor", cache) if s.key == ckey(1)).title == "Проверить каталог"
    assert next(s for s in sources.scan("cursor", {}) if s.key == ckey(1)).empty


# --- Общее ---------------------------------------------------------------------

def test_last_messages_limits():
    items = [(n, "assistant", f"ответ {n}", 0.0) for n in range(MAX_MESSAGES + 50)]
    rows, truncated = last_messages(reversed(items), 10**6, None)
    assert truncated and len(rows) == MAX_MESSAGES
    assert rows[0]["text"] == "ответ 50" and rows[-1]["text"] == f"ответ {MAX_MESSAGES + 49}"
    rows, truncated = last_messages(reversed(items[:3]), 10**6, None)
    assert not truncated and [r["text"] for r in rows] == ["ответ 0", "ответ 1", "ответ 2"]
    rows, truncated = last_messages(reversed(items[:3]), len("ответ 2".encode()) * 2, None)
    assert truncated and [r["text"] for r in rows] == ["ответ 1", "ответ 2"]
    # Одно сообщение больше лимита всё равно показывается.
    rows, truncated = last_messages([(0, "user", "x" * 100, 0.0)], 10, 5)
    assert rows == [{"role": "user", "text": "xxxxx", "at": None}] and not truncated


def test_agents_read_only_without_runner():
    zcode, cursor = get("zcode"), get("cursor")
    assert list(AGENTS)[-2:] == ["zcode", "cursor"]
    assert not zcode.runner and not cursor.runner
    assert zcode.programs == () and zcode.resume == "" and zcode.to_json()["resume_hint"]
    assert cursor.programs == ("cursor-agent",) and cursor.name == "Cursor Agent"
    for agent in (zcode, cursor):
        assert agent.home_env and f'aria-label="{agent.name}"' in agent.icon and 'role="img"' in agent.icon
    assert len({agent.color for agent in AGENTS.values()}) == len(AGENTS)


def test_catalog_reads_and_caches_both_agents(zcode_db, cursor_home, tmp_path):
    path = tmp_path / "cache.json"
    catalog = Catalog(path)
    assert catalog.wait(10) and catalog.error is None
    keys = {s.key for s in catalog.sessions()}
    assert keys == {zkey(1), zkey(3), zkey(4), ckey(1), ckey(2), ckey(3), ckey(4)}
    progress = catalog.progress()
    assert progress["done"] == progress["total"] == 5
    text = path.read_text(encoding="utf-8")
    assert str(zcode_db) in json.loads(text) and SECRET not in text
    again = Catalog(path)
    assert again.wait(10) and again.error is None
    assert [s.to_json() for s in again.sessions()] == [s.to_json() for s in catalog.sessions()]
    found = {item["key"] for item in catalog.search("ответвлени", keys)}
    assert found == {zkey(4)}
