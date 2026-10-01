"""Cursor Agent (`cursor-agent`): чаты в `$CURSOR_HOME/chats/<md5 от папки>/<id чата>/`.

В папке чата `meta.json` с названием и временем и база `store.db`. В её таблице `meta`
под ключом '0' лежит JSON в шестнадцатеричной записи со ссылкой на корневой blob, в
таблице `blobs` — сообщения в JSON и служебные записи в protobuf. Корневой blob
перечисляет сообщения по порядку: каждое поле 1 длиной 32 байта — sha256 сообщения,
он же id в `blobs`. У пустого чата базы может не быть.
"""

import json
import re
import sqlite3
from datetime import datetime, timedelta, timezone

from ..sources.common import (UUID, clean_title, content_text, fingerprint, plain_line, read_json, timestamp,
                              visible_text)
from .base import NO_TITLE, PARSER_VERSION
from .sqlite import SqliteAgent, edge_messages, last_messages, valid_record

_QUERY = re.compile(r"<user_query>(.*?)</user_query>", re.S)
_SENT = re.compile(r"<timestamp>(.*?)</timestamp>", re.S)
_WORKSPACE = re.compile(r"^\s*Workspace Path:\s*(\S.*?)\s*$", re.M)
# «Monday, Sep 28, 2026, 5:00 PM (UTC+9)» — так cursor-agent помечает время запроса.
_HUMAN_TIME = re.compile(r"\b([A-Za-z]{3})[A-Za-z]*\.?\s+(\d{1,2}),?\s+(\d{4}),?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?"
                         r"\s*([AaPp][Mm])?(\s*\(UTC(?:([+-])(\d{1,2})(?::?(\d{2}))?)?\))?")
_MONTHS = ("jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec")
# Больше ссылок за раз в запрос не передаётся: старые SQLite принимают до 999 параметров.
_CHUNK = 500


class Cursor(SqliteAgent):
    id = "cursor"
    name = "Cursor Agent"
    color = "#1b1c20"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="Cursor Agent"><rect x="0" y="0" width="24" height="24" '
            'rx="6" fill="#1b1c20"></rect><path d="M8 5.6l9.6 6.2-4.4 1.2-2.4 4.6z" fill="#ffffff" stroke="#ffffff" '
            'stroke-width="1.4" stroke-linejoin="round"></path></svg>')
    home_env = "CURSOR_HOME"
    home_default = ".cursor"
    programs = ("cursor-agent",)
    resume = "cursor-agent --resume {id}"
    skip_flag = "--force"

    def discover(self):
        return sorted(self.home().glob("chats/*/*/meta.json"))

    def scan(self, cache, units=None, on_unit=None):
        result = []
        for path in self.discover() if units is None else units:
            session = self._session(path, cache)
            if session:
                result.append(session)
            if on_unit:
                on_unit([session] if session else [])
        return result

    def _session(self, path, cache):
        chat = path.parent.name
        store = path.with_name("store.db")
        stamp = {**fingerprint(path), **_prefixed("store_", fingerprint(store)),
                 **_prefixed("wal_", fingerprint(store.with_name("store.db-wal")))}
        if not UUID.fullmatch(chat) or stamp["size"] < 0:
            return None
        entry = cache.get(str(path))
        if not self._valid_entry(entry) or any(entry.get(k) != v for k, v in stamp.items()):
            try:
                name, messages, workspace = self._read_store(store) if store.is_file() else ("", [], "")
            except sqlite3.Error:
                # База занята или испорчена: прежняя запись или данные из meta.json. В кеш
                # не пишем, чтобы прочитать базу ещё раз при следующем обходе.
                if not self._valid_entry(entry):
                    entry = _record(read_json(path), "", [], "", stamp)
            else:
                entry = {**stamp, **_record(read_json(path), name, messages, workspace, stamp),
                         "parser": PARSER_VERSION, "tool": self.id}
                cache[str(path)] = entry
        return self.session_of(entry, id=chat, journal=store, meta_path=path)

    def _valid_entry(self, entry) -> bool:
        return valid_record(entry) and entry.get("parser") == PARSER_VERSION and entry.get("tool") == self.id

    def _read_store(self, store):
        """Название чата, сообщения (номер, роль, текст, время) и папка из служебного сообщения."""
        with self.connect(store) as connection:
            row = connection.execute("SELECT value FROM meta WHERE key = '0'").fetchone()
            name, root_id = _store_meta(row[0] if row else None)
            root = connection.execute("SELECT data FROM blobs WHERE id = ?", (root_id,)).fetchone() if root_id else None
            refs = _message_refs(root[0]) if root and isinstance(root[0], bytes) else []
            blobs = {}
            unique = list(dict.fromkeys(refs))
            for start in range(0, len(unique), _CHUNK):
                chunk = unique[start:start + _CHUNK]
                blobs.update(connection.execute(
                    f"SELECT id, data FROM blobs WHERE id IN ({', '.join('?' * len(chunk))})", chunk))
        messages, workspace = _conversation(blobs[ref] for ref in refs if ref in blobs)
        return name, messages, workspace

    def _messages(self, session) -> list[tuple]:
        if not session.journal.is_file():
            return []
        try:
            return self._read_store(session.journal)[1]
        except sqlite3.Error:
            return []

    def transcript(self, session, limit_bytes, *, text_limit=12000):
        return last_messages(reversed(self._messages(session)), limit_bytes, text_limit)

    def excerpt_messages(self, session):
        messages = self._messages(session)
        return edge_messages(messages, reversed(messages))


def _prefixed(prefix: str, values: dict) -> dict:
    return {prefix + k: v for k, v in values.items()}


def _record(meta: dict, name: str, messages: list, workspace: str, stamp: dict) -> dict:
    first = next((clean_title(text) for _, role, text, _ in messages if role == "user"), "")
    last = next((plain_line(text) for _, role, text, _ in reversed(messages) if role == "assistant"), "")
    updated = timestamp(meta.get("updatedAtMs"), stamp["mtime_ns"] / 10**9)
    cwd = meta.get("cwd")
    return {"title": clean_title(meta.get("title")) or clean_title(name) or first or NO_TITLE,
            "cwd": cwd if isinstance(cwd, str) and cwd else workspace,
            "created": timestamp(meta.get("createdAtMs"), updated), "updated": updated,
            "last": last, "empty": not messages}


def _store_meta(value) -> tuple[str, str]:
    """Название чата и id корневого blob. Остальные поля (среди них бывает ключ
    шифрования) не сохраняются."""
    try:
        info = json.loads(bytes.fromhex(value))
    except (TypeError, ValueError):
        return "", ""
    if not isinstance(info, dict):
        return "", ""
    name, root = info.get("name"), info.get("latestRootBlobId")
    return (name if isinstance(name, str) else ""), (root if isinstance(root, str) else "")


def _message_refs(data: bytes) -> list[str]:
    """id сообщений из корневого blob: поля 1 длиной 32 байта по порядку. Разбор protobuf
    по типу поля: 0 — varint, 1 — 8 байт, 2 — длина и данные, 5 — 4 байта."""
    refs, index = [], 0
    try:
        while index < len(data):
            key, index = _varint(data, index)
            field, kind = key >> 3, key & 7
            if kind == 0:
                _, index = _varint(data, index)
            elif kind == 1:
                index += 8
            elif kind == 2:
                size, index = _varint(data, index)
                value = data[index:index + size]
                index += size
                if field == 1 and len(value) == 32:
                    refs.append(value.hex())
            elif kind == 5:
                index += 4
            else:
                break
    except IndexError:
        pass
    return refs


def _varint(data: bytes, index: int) -> tuple[int, int]:
    value = shift = 0
    while True:
        byte = data[index]
        index += 1
        value |= (byte & 0x7F) << shift
        shift += 7
        if byte < 0x80:
            return value, index


def _conversation(blobs) -> tuple[list[tuple], str]:
    """Запросы человека и ответы агента. Первое сообщение пользователя — служебный контекст
    без <user_query>: из него берётся только папка."""
    messages, workspace = [], ""
    for index, data in enumerate(blobs):
        try:
            row = json.loads(data)
        except (TypeError, ValueError):
            continue
        if not isinstance(row, dict):
            continue
        role, content = row.get("role"), row.get("content")
        if role == "user":
            raw = content_text(content)
            queries = _QUERY.findall(raw)
            if not queries:
                found = _WORKSPACE.search(raw)
                workspace = workspace or (found.group(1) if found else "")
                continue
            text = visible_text("\n\n".join(query.strip() for query in queries))
            sent = _SENT.search(raw)
            at = _sent_at(sent.group(1)) if sent else 0.0
        elif role == "assistant":
            text, at = visible_text(content), 0.0
        else:
            continue
        if text:
            messages.append((index, role, text, at))
    return messages, workspace


def _sent_at(value: str) -> float:
    """Время из <timestamp>: «Monday, Sep 28, 2026, 5:00 PM (UTC+9)» или ISO 8601."""
    found = _HUMAN_TIME.search(value)
    if not found:
        return timestamp(value.strip())
    month, day, year, hour, minute, second, half, zone, sign, hours, minutes = found.groups()
    if month.lower() not in _MONTHS:
        return 0.0
    hour = int(hour)
    if half:
        hour = hour % 12 + (12 if half.lower() == "pm" else 0)
    try:
        offset = timedelta(hours=int(hours or 0), minutes=int(minutes or 0))
        tz = timezone(-offset if sign == "-" else offset) if zone else None
        moment = datetime(int(year), _MONTHS.index(month.lower()) + 1, int(day), hour, int(minute), int(second or 0),
                          tzinfo=tz)
    except ValueError:
        return 0.0
    # Без пояса время считается местным.
    return moment.timestamp()


AGENT = Cursor()
