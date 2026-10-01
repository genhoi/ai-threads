"""ZCode: сессии в базе SQLite `$ZCODE_HOME/cli/db/db.sqlite`.

Таблица `session` — сессии, `message` — сообщения с ролью в JSON-поле `data`, `part` —
части сообщений: текст, рассуждения, вызовы инструментов. База открывается только для
чтения; её может держать запущенное приложение. Продолжить сессию из терминала нельзя.
"""

import json
import sqlite3
from pathlib import Path

from ..sources.common import clean_title, plain_line, timestamp, visible_text
from .base import NO_TITLE, PARSER_VERSION
from .sqlite import SqliteAgent, edge_messages, last_messages, valid_record

# Подагенты и прогоны workflow записаны с parent_id — это не отдельные сессии. Ответвления
# и побочные чаты тоже ссылаются на родителя, но их начинает человек.
_SESSIONS = ("SELECT id, title, title_source, directory, time_created, time_updated FROM session "
             "WHERE parent_id IS NULL OR task_type IN ('fork', 'selection_side_chat')")
_MESSAGES = "SELECT id, time_created, data FROM message WHERE session_id = ? ORDER BY sequence, time_created, id"
_PARTS = "SELECT data FROM part WHERE message_id = ? ORDER BY sequence, time_created, id"


class ZCode(SqliteAgent):
    id = "zcode"
    name = "ZCode"
    color = "#1d7a52"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="ZCode"><rect x="0" y="0" width="24" height="24" rx="6" '
            'fill="#1d7a52"></rect><path d="M7.4 7.4h9.2l-9.2 9.2h9.2" stroke="#ffffff" stroke-width="2.2" '
            'stroke-linecap="round" stroke-linejoin="round" fill="none"></path></svg>')
    home_env = "ZCODE_HOME"
    home_default = ".zcode"
    resume_hint = "Продолжить можно только в приложении ZCode"

    def database(self) -> Path:
        return self.home() / "cli" / "db" / "db.sqlite"

    def discover(self):
        path = self.database()
        return [path] if path.is_file() else []

    def scan(self, cache, units=None, on_unit=None):
        result = []
        for path in self.discover() if units is None else units:
            sessions = self._read(path, cache)
            result += sessions
            if on_unit:
                on_unit(sessions)
        return result

    def _read(self, path: Path, cache: dict):
        """Сессии из базы. Переписку перечитывает только у сессий с новым time_updated;
        если база недоступна, отдаёт сессии из кеша."""
        entry = cache.get(str(path))
        known = entry["sessions"] if _valid_entry(entry, self.id) else {}
        records = {}
        try:
            with self.connect(path) as connection:
                for sid, title, source, cwd, created, updated in connection.execute(_SESSIONS).fetchall():
                    if not isinstance(sid, str) or not sid:
                        continue
                    old = known.get(sid)
                    if _fresh(old, updated):
                        overview = {k: old[k] for k in ("first_title", "last", "empty")}
                    else:
                        overview = self._overview(connection, sid)
                    # Название по умолчанию — заглушка с датой; вместо неё первый запрос.
                    own = "" if source == "default" else clean_title(title)
                    changed = timestamp(updated)
                    records[sid] = {"time_updated": updated, **overview,
                                    "title": own or overview["first_title"] or NO_TITLE,
                                    "cwd": cwd if isinstance(cwd, str) else "",
                                    "created": timestamp(created, changed), "updated": changed}
        except sqlite3.Error:
            records = {sid: record for sid, record in known.items() if valid_record(record)}
        else:
            cache[str(path)] = {"parser": PARSER_VERSION, "tool": self.id, "sessions": records}
        return [self.session_of(record, id=sid, journal=path, meta_path=path) for sid, record in records.items()]

    def _overview(self, connection, sid: str) -> dict:
        """Первый запрос, последний ответ и признак пустой переписки."""
        candidates = _candidates(connection, sid)
        first = next(_texts(connection, (c for c in candidates if c[1] == "user")), None)
        last = next(_texts(connection, (c for c in reversed(candidates) if c[1] == "assistant")), None)
        return {"first_title": clean_title(first[2]) if first else "",
                "last": plain_line(last[2]) if last else "", "empty": not (first or last)}

    def transcript(self, session, limit_bytes, *, text_limit=12000):
        try:
            with self.connect(session.journal) as connection:
                candidates = _candidates(connection, session.id)
                return last_messages(_texts(connection, reversed(candidates)), limit_bytes, text_limit)
        except sqlite3.Error:
            return [], False

    def excerpt_messages(self, session):
        try:
            with self.connect(session.journal) as connection:
                candidates = _candidates(connection, session.id)
                return edge_messages(_texts(connection, candidates), _texts(connection, reversed(candidates)))
        except sqlite3.Error:
            return []


def _valid_entry(entry, tool: str) -> bool:
    return (isinstance(entry, dict) and entry.get("parser") == PARSER_VERSION and entry.get("tool") == tool
            and isinstance(entry.get("sessions"), dict))


def _fresh(record, updated) -> bool:
    return valid_record(record) and isinstance(record.get("first_title"), str) \
        and record.get("time_updated") == updated


def _candidates(connection, sid: str) -> list[tuple]:
    """(id, роль, время) сообщений, которые ZCode показывает человеку, по порядку."""
    found = []
    for mid, created, data in connection.execute(_MESSAGES, (sid,)).fetchall():
        role = _visible_role(data)
        if role:
            found.append((mid, role, timestamp(created)))
    return found


def _texts(connection, candidates):
    """(id, роль, текст, время) сообщений с видимым текстом. Части читаются по одному сообщению."""
    for mid, role, ts in candidates:
        text = _message_text(connection, mid)
        if text:
            yield mid, role, text, ts


def _visible_role(data) -> str | None:
    info = _json(data)
    # Напоминания, уведомления о фоновых задачах, пересказ после сжатия контекста и копия
    # переписки в побочном чате помечены synthetic или скрыты из интерфейса.
    semantics = info.get("semantics")
    if info.get("synthetic") or info.get("visibility") == "model-only" \
            or (isinstance(semantics, dict) and semantics.get("uiVisibility") == "hidden"):
        return None
    role = info.get("role")
    return role if role in ("user", "assistant") else None


def _message_text(connection, mid: str) -> str:
    texts = []
    for (data,) in connection.execute(_PARTS, (mid,)):
        part = _json(data)
        if part.get("type") == "text" and not part.get("synthetic") and isinstance(part.get("text"), str):
            texts.append(part["text"])
    return visible_text("\n".join(texts))


def _json(data) -> dict:
    try:
        value = json.loads(data)
    except (TypeError, ValueError):
        return {}
    return value if isinstance(value, dict) else {}


AGENT = ZCode()
