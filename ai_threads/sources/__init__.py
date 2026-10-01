"""Чтение сессий через реестр агентов. Файлы агентов открываются только для чтения."""

from ..model import Session
from .common import MESSAGE_BYTES

# Последние сообщения, которые видны на экране сессии.
SHOWN_MESSAGES = 60


def _agent(tool: str):
    from ..agents import get
    return get(tool)


def discover(tool: str):
    return _agent(tool).discover()


def journal_path(tool: str, path):
    return _agent(tool).journal_path(path)


def scan(tool: str, cache: dict, *, paths=None, on_read=None) -> list[Session]:
    """Сессии агента. `on_read(sessions)` вызывается после каждого прочитанного файла."""
    return _agent(tool).scan(cache, paths, on_read)


def transcript(session: Session) -> dict:
    rows, truncated = _agent(session.tool).transcript(session, MESSAGE_BYTES)
    return {"messages": rows[-SHOWN_MESSAGES:], "truncated": truncated or len(rows) > SHOWN_MESSAGES}


def messages(session: Session) -> list[dict]:
    return transcript(session)["messages"]
