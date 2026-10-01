"""Общее для агентов, которые хранят переписку в базе SQLite: открытие базы только для
чтения, последние сообщения в пределах объёма, выдержка из начала и конца, запись кеша.

Базу может держать запущенный агент. Ошибки чтения и блокировки — `sqlite3.Error`;
агент ловит их сам и отдаёт то, что есть в кеше, чтобы каталог не падал.
"""

import math
import sqlite3
from contextlib import contextmanager
from itertools import islice
from pathlib import Path
from urllib.parse import quote

from ..model import Session
from ..sources.common import fingerprint
from .base import Agent, message

# Больше сообщений экран сессии и выгрузка для сводки не показывают.
MAX_MESSAGES = 200
# Сообщений из начала и из конца переписки в выдержке для сводки за несколько дней.
EXCERPT_MESSAGES = 5


class SqliteAgent(Agent):
    busy_timeout = 1.0         # секунд ждать, пока база занята записью

    @contextmanager
    def connect(self, path: Path):
        connection = sqlite3.connect(_readonly_uri(Path(path)), uri=True, timeout=self.busy_timeout)
        try:
            yield connection
        finally:
            connection.close()

    def plain_journal(self, session):
        # Базу SQLite модели не дать: сводка выгружает переписку в текстовый файл.
        return False

    def session_of(self, record: dict, **values) -> Session:
        return Session(tool=self.id, title=record["title"], cwd=record["cwd"], created=record["created"],
                       updated=record["updated"], last=record["last"], empty=record["empty"], **values)


def valid_record(record) -> bool:
    """Запись кеша с полями сессии, которые агент считает сам."""
    if not isinstance(record, dict) or any(not isinstance(record.get(k), str) for k in ("title", "cwd", "last")):
        return False
    if type(record.get("empty")) is not bool:
        return False
    return all(type(record.get(k)) in (int, float) and math.isfinite(record[k]) for k in ("created", "updated"))


def last_messages(found, limit_bytes: int, text_limit: int | None):
    """Сообщения из `found` — (ключ, роль, текст, время) от конца к началу — не больше
    MAX_MESSAGES и около `limit_bytes` текста. Второй элемент — признак, что показаны не все."""
    result, used = [], 0
    for _, role, text, ts in found:
        used += len(text.encode())
        if result and (len(result) >= MAX_MESSAGES or used > limit_bytes):
            return result[::-1], True
        result.append(message(role, text, ts, text_limit))
    return result[::-1], False


def edge_messages(forward, backward, count: int = EXCERPT_MESSAGES) -> list[dict]:
    """Первые и последние `count` сообщений: `forward` идёт от начала, `backward` — от конца."""
    head = list(islice(forward, count))
    seen = {item[0] for item in head}
    tail = [item for item in islice(backward, count) if item[0] not in seen][::-1]
    return [message(role, text, ts, 12000) for _, role, text, ts in head + tail]


def _readonly_uri(path: Path) -> str:
    uri = f"file:{quote(str(path))}?mode=ro"
    # Даже с mode=ro SQLite у базы в режиме WAL пишет в индекс -shm, а если файла -wal
    # нет — создаёт пустые -wal и -shm. Когда -wal нет или он пуст, вся база лежит в
    # основном файле: её читаем как неизменяемую и рядом ничего не трогаем.
    if _wal_mode(path) and fingerprint(path.with_name(path.name + "-wal"))["size"] <= 0:
        uri += "&immutable=1"
    return uri


def _wal_mode(path: Path) -> bool:
    try:
        with path.open("rb") as stream:
            header = stream.read(20)
    except OSError:
        return False
    return header[:16] == b"SQLite format 3\x00" and header[18:20] == b"\x02\x02"
