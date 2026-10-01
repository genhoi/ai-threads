"""Ограниченное чтение журналов и извлечение видимого текста."""

import json
import math
import re
from datetime import datetime, timezone
from pathlib import Path

HEAD_BYTES = 1024 * 1024
TAIL_BYTES = 256 * 1024
MESSAGE_BYTES = 8 * 1024 * 1024
SEARCH_BYTES = 512 * 1024
UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", re.I)


def json_rows(path: Path, limit: int, *, tail: bool = False):
    try:
        with path.open("rb") as stream:
            size = stream.seek(0, 2)
            start = max(0, size - limit) if tail else 0
            # Один байт перед срезом отличает начало строки от её середины.
            stream.seek(max(0, start - 1))
            boundary = stream.read(1) if start else b"\n"
            raw = stream.read(limit)
        if start and boundary != b"\n":
            raw = raw.partition(b"\n")[2]
        if not tail and size > limit:
            raw = raw.rpartition(b"\n")[0]
        for line in raw.splitlines():
            try:
                row = json.loads(line)
            except (ValueError, UnicodeDecodeError):
                continue
            if isinstance(row, dict):
                yield row
    except OSError:
        return


def read_json(path: Path) -> dict:
    try:
        with path.open("rb") as stream:
            data = json.loads(stream.read(HEAD_BYTES))
        return data if isinstance(data, dict) else {}
    except (OSError, ValueError, UnicodeDecodeError):
        return {}


def content_text(value) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return "\n".join(filter(None, map(content_text, value)))
    if isinstance(value, dict) and value.get("type") in ("text", "input_text", "output_text"):
        return value.get("text", "") if isinstance(value.get("text"), str) else ""
    return ""


# Вставки, которые CLI добавляет в сообщение пользователя; человек их не писал.
SERVICE_TAGS = "INSTRUCTIONS|environment_context|recommended_plugins|user_instructions|system-reminder"
# Сообщения Claude Code от имени пользователя, которые на деле — вывод команд и служебные строки.
CLAUDE_SERVICE_PREFIXES = ("<local-command-caveat>", "<local-command-stdout>", "<local-command-stderr>",
                           "<command-name>", "<command-message>", "<bash-stdout>", "<bash-stderr>")


def visible_text(value) -> str:
    text = content_text(value)
    text = re.sub(rf"<({SERVICE_TAGS})>.*?</\1>", "", text, flags=re.S)
    text = re.sub(r"^\s*#*\s*AGENTS\.md instructions[^\n]*\n?", "", text)
    text = re.sub(r"</?image\b[^>]*>", "", text)
    # Незакрытая служебная вставка не должна становиться сообщением или названием.
    text = re.split(rf"<(?:{SERVICE_TAGS})>", text, maxsplit=1)[0]
    return text.strip()


def claude_user_text(row: dict, value):
    """Текст, который человек набрал в Claude Code, или None для служебной записи."""
    if row.get("isMeta") or row.get("isCompactSummary") or (row.get("origin") or {}).get("kind") not in (None, "human"):
        return None
    text = content_text(value).strip()
    if text.startswith(CLAUDE_SERVICE_PREFIXES):
        return None
    bash = re.fullmatch(r"<bash-input>(.*?)</bash-input>", text, flags=re.S)
    if bash:
        return "! " + bash.group(1).strip()
    return re.sub(r'</?pasted_content\b[^>]*>', "", text)


def clean_title(value) -> str:
    return re.sub(r"\s+", " ", visible_text(value)).lstrip("#>*- ")[:150].strip()


def plain_line(text: str, limit: int = 300) -> str:
    """Одна строка без разметки Markdown — для строки сессии в ленте."""
    text = re.sub(r"^\s{0,3}(?:#{1,6}\s+|[-*+]\s+|>\s?)", "", text, flags=re.M)
    text = text.replace("**", "").replace("__", "").replace("`", "")
    return re.sub(r"\s+", " ", text).strip()[:limit]


def prefix(value) -> str:
    return " ".join(content_text(value).split())[:50]


def timestamp(value, fallback=0.0) -> float:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return (value / 1000 if value > 10**11 else float(value)) if math.isfinite(value) else fallback
    if isinstance(value, str):
        try:
            dt = datetime.fromisoformat(value.replace("Z", "+00:00"))
            return dt.replace(tzinfo=timezone.utc).timestamp() if dt.tzinfo is None else dt.timestamp()
        except (ValueError, OverflowError):
            pass
    return fallback


def messages_from_rows(tool: str, rows, *, text_limit: int | None = 12000) -> list[dict]:
    """Сообщения из строк журнала агента с построчным JSON."""
    from ..agents import get
    return get(tool).messages_from_rows(rows, text_limit=text_limit)


def fingerprint(path: Path) -> dict:
    try:
        stat = path.stat()
        return {"size": stat.st_size, "mtime_ns": stat.st_mtime_ns}
    except OSError:
        return {"size": -1, "mtime_ns": -1}


def update_index(path: Path, cache: dict, kind: str, apply, check) -> dict:
    """Продолжает чтение дописываемого индекса с последней полной строки.

    `apply(values, row)` добавляет строку индекса в словарь, `check(value)` проверяет
    значение из кеша: кеш другой версии читается заново.
    """
    key = str(path)
    stamp = fingerprint(path)
    old = cache.get(key, {})
    if not _valid_index(old, kind, check):
        old = {}
    if all(old.get(k) == v for k, v in stamp.items()) and isinstance(old.get("values"), dict):
        return old["values"]
    offset = old.get("offset", 0)
    if not isinstance(offset, int) or stamp["size"] < old.get("size", 0):
        offset = 0
    values = dict(old.get("values", {})) if offset else {}
    if stamp["size"] >= 0:
        try:
            with path.open("rb") as stream:
                stream.seek(offset)
                while raw := stream.readline():
                    if not raw.endswith(b"\n"):
                        break
                    offset = stream.tell()
                    try:
                        row = json.loads(raw)
                    except (ValueError, UnicodeDecodeError):
                        continue
                    if isinstance(row, dict):
                        apply(values, row)
        except OSError:
            return values
    cache[key] = {**stamp, "kind": kind, "offset": offset, "values": values}
    return values


def _valid_index(entry, kind, check):
    if not isinstance(entry, dict) or entry.get("kind") != kind:
        return False
    if any(type(entry.get(k)) is not int for k in ("size", "mtime_ns", "offset")):
        return False
    if not 0 <= entry["offset"] <= max(0, entry["size"]):
        return False
    values = entry.get("values")
    return isinstance(values, dict) and all(check(value) for value in values.values())
