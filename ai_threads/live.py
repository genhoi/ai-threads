"""Статусы запущенных агентов по реестрам, которые инструменты ведут сами.

Журналы сессий не читаются: опрос раз в несколько секунд смотрит только маленькие
файлы реестра и проверяет, жив ли процесс.
"""

import json
import os
from datetime import datetime
from pathlib import Path

from .model import Session

# Больше этого реестр уже не «маленький файл статуса».
_MAX_BYTES = 1_000_000
# Unix-время в миллисекундах больше этого порога, в секундах — нет.
_MILLISECOND_THRESHOLD = 10**11


def live_status(sessions: dict[str, Session]) -> dict[str, dict]:
    """key → {"state": "wait"|"work", "where": str, "since": number}.

    Реестр запущенных сессий ведут не все агенты: у Codex и Kimi его нет, их
    `session_index.jsonl` — каталог прошлых сессий, а не список живых процессов.
    """
    from .agents import active
    found: dict[str, dict] = {}
    stamps: dict[str, float] = {}
    for agent in active():
        for key, item, stamp in agent.live(sessions):
            if key in stamps and stamp < stamps[key]:
                continue
            found[key] = item
            stamps[key] = float(stamp) if isinstance(stamp, (int, float)) else 0.0
    return found


def process_alive(pid: object, proc_start: object) -> bool:
    """Процесс жив, если `os.kill(pid, 0)` без ошибки.

    Если в записи есть procStart, он должен совпасть с полем 22 `/proc/<pid>/stat`
    (время старта). Так запись не цепляется к новому процессу с тем же pid.
    pid <= 0 нельзя передавать в os.kill: 0 и -1 бьют по группе процессов.
    """
    if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    if proc_start is None or proc_start == "":
        return True
    actual = _proc_start_time(pid)
    if actual is None:
        return True
    return actual == str(proc_start)


def _proc_start_time(pid: int) -> str | None:
    try:
        text = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    except OSError:
        return None
    return _starttime(text)


def _starttime(text: str) -> str | None:
    # Поле comm в скобках может содержать пробелы и скобки, поэтому режется по последней «)».
    # Поле 22 (starttime) — 20-е после comm.
    end = text.rfind(")")
    if end < 0:
        return None
    fields = text[end + 1 :].split()
    if len(fields) <= 19:
        return None
    return fields[19]


def read_small_json(path: Path):
    try:
        if not path.is_file() or path.stat().st_size > _MAX_BYTES:
            return None
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return None


def unix_seconds(value: object) -> int | float | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return _from_number(float(value))
    if isinstance(value, str):
        text = value.strip()
        if not text:
            return None
        try:
            return _from_number(float(text))
        except ValueError:
            return _from_iso(text)
    return None


def _from_number(value: float) -> int | float:
    if value >= _MILLISECOND_THRESHOLD:
        value /= 1000.0
    rounded = int(value)
    if value == rounded:
        return rounded
    return value


def _from_iso(text: str) -> int | float | None:
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    return _from_number(parsed.timestamp())
