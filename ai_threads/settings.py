"""Настройки пользователя: файл `~/.config/ai-threads/settings.json`.

В файле лежат только значения, которые человек поменял; остальное берётся из умолчаний.
Файл читается заново, когда меняются его размер или время изменения. Переменные окружения
(`CODEX_HOME`, `AI_THREADS_SUMMARY_TIMEOUT` и другие) важнее файла.
"""

import copy
import json
import os
import re
import tempfile
import threading
from pathlib import Path

from .config import home

LANGUAGES = ("ru", "en")
OPEN_WITH = ("auto", "wt", "tmux", "plain")
TMUX_NAME = re.compile(r"^[A-Za-z0-9_.-]{1,40}$")

DEFAULTS = {
    "agents": {},
    "open_with": "auto",
    "tmux_session": "nit",
    "summary": {"agent": "auto", "timeout": 300},
    "digest": {"agent": "auto", "timeout": 900},
    "search": {"agent": "auto", "timeout": 300},
    "language": "ru",
    "kimi_model": "kimi-code/kimi-for-coding",
    "temp_dirs": ["/tmp", "/var/tmp", "~/.local"],
}
AGENT_FIELDS = ("enabled", "home", "program", "skip_approvals")

_lock = threading.Lock()
_cache = {"stamp": None, "data": {}, "error": None, "effective": DEFAULTS}


def path() -> Path:
    explicit = os.environ.get("AI_THREADS_CONFIG")
    if explicit:
        return Path(explicit)
    base = os.environ.get("XDG_CONFIG_HOME") or home() / ".config"
    return Path(base) / "ai-threads" / "settings.json"


def _stamp(file: Path):
    try:
        stat = file.stat()
        return (str(file), stat.st_size, stat.st_mtime_ns)
    except OSError:
        return (str(file), -1, -1)


def _load() -> dict:
    """Кеш: значения из файла и итоговые настройки. Повреждённый файл не роняет приложение:
    берутся умолчания, а причина видна в `error()` и в ответе `/api/config`."""
    file = path()
    stamp = _stamp(file)
    with _lock:
        if stamp != _cache["stamp"]:
            data, error = {}, None
            if stamp[1] >= 0:
                try:
                    data = validate(json.loads(file.read_text(encoding="utf-8")))
                except (OSError, ValueError) as exc:
                    data, error = {}, f"{file}: {exc}"
            merged = copy.deepcopy(DEFAULTS)
            for name, value in data.items():
                if isinstance(value, dict) and isinstance(merged.get(name), dict):
                    merged[name].update(value)
                else:
                    merged[name] = value
            _cache.update(stamp=stamp, data=data, error=error, effective=merged)
        return _cache


def stored() -> dict:
    """Значения, которые записаны в файле."""
    return copy.deepcopy(_load()["data"])


def error() -> str | None:
    return _load()["error"]


def effective() -> dict:
    """Умолчания, поверх которых наложены значения из файла. Изменять результат нельзя."""
    return _load()["effective"]


def agent(agent_id: str) -> dict:
    return effective()["agents"].get(agent_id, {})


def temp_dirs() -> tuple[str, ...]:
    dirs = []
    for value in effective()["temp_dirs"]:
        expanded = str(home()) + value[1:] if value == "~" or value.startswith("~/") else value
        dirs.append(expanded.rstrip("/") or "/")
    return tuple(dirs)


def timeout(kind: str) -> float:
    """Предел ожидания сводки: переменная окружения, потом настройка."""
    raw = os.environ.get(f"AI_THREADS_{kind.upper()}_TIMEOUT")
    if raw:
        try:
            value = float(raw)
            if value > 0:
                return value
        except ValueError:
            pass
    return float(effective()[kind]["timeout"])


def save(data) -> dict:
    """Проверить и записать значения целиком. Возвращает то, что записано."""
    clean = validate(data)
    file = path()
    file.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".settings-", suffix=".tmp", dir=file.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(clean, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.replace(name, file)
    finally:
        if os.path.exists(name):
            os.unlink(name)
    with _lock:
        _cache["stamp"] = None
    return clean


def _string(value, name, *, empty=True, limit=1000) -> str:
    if not isinstance(value, str) or len(value) > limit or (not empty and not value.strip()):
        raise ValueError(f"{name}: нужна строка до {limit} символов")
    return value.strip()


def _timeout(value, name) -> int | float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not 10 <= value <= 7200:
        raise ValueError(f"{name}: нужно число секунд от 10 до 7200")
    return value


def validate(data) -> dict:
    """Нормализованные значения или ValueError с понятной причиной."""
    from .agents import AGENTS, RUNNERS

    if not isinstance(data, dict):
        raise ValueError("Настройки должны быть JSON-объектом")
    unknown = set(data) - set(DEFAULTS)
    if unknown:
        raise ValueError(f"Неизвестные настройки: {', '.join(sorted(unknown))}")
    result = {}
    if "agents" in data:
        agents = data["agents"]
        if not isinstance(agents, dict):
            raise ValueError("agents: нужен объект")
        result["agents"] = {}
        for agent_id, values in agents.items():
            if agent_id not in AGENTS:
                raise ValueError(f"agents: неизвестный агент {agent_id}")
            if not isinstance(values, dict) or set(values) - set(AGENT_FIELDS):
                raise ValueError(f"agents.{agent_id}: допустимы поля {', '.join(AGENT_FIELDS)}")
            clean = {}
            for field, value in values.items():
                name = f"agents.{agent_id}.{field}"
                if field in ("enabled", "skip_approvals"):
                    if not isinstance(value, bool):
                        raise ValueError(f"{name}: нужно true или false")
                    clean[field] = value
                    continue
                clean[field] = _string(value, name)
            result["agents"][agent_id] = clean
    if "open_with" in data:
        if data["open_with"] not in OPEN_WITH:
            raise ValueError(f"open_with: одно из {', '.join(OPEN_WITH)}")
        result["open_with"] = data["open_with"]
    if "tmux_session" in data:
        if not isinstance(data["tmux_session"], str) or not TMUX_NAME.fullmatch(data["tmux_session"]):
            raise ValueError("tmux_session: латинские буквы, цифры, точка, дефис и подчёркивание")
        result["tmux_session"] = data["tmux_session"]
    for kind in ("summary", "digest", "search"):
        if kind not in data:
            continue
        values = data[kind]
        if not isinstance(values, dict) or set(values) - {"agent", "timeout"}:
            raise ValueError(f"{kind}: допустимы поля agent и timeout")
        clean = {}
        if "agent" in values:
            if values["agent"] != "auto" and values["agent"] not in RUNNERS:
                raise ValueError(f"{kind}.agent: auto или один из {', '.join(RUNNERS)}")
            clean["agent"] = values["agent"]
        if "timeout" in values:
            clean["timeout"] = _timeout(values["timeout"], f"{kind}.timeout")
        result[kind] = clean
    if "language" in data:
        if data["language"] not in LANGUAGES:
            raise ValueError(f"language: одно из {', '.join(LANGUAGES)}")
        result["language"] = data["language"]
    if "kimi_model" in data:
        result["kimi_model"] = _string(data["kimi_model"], "kimi_model", empty=False, limit=200)
    if "temp_dirs" in data:
        dirs = data["temp_dirs"]
        if not isinstance(dirs, list) or len(dirs) > 50:
            raise ValueError("temp_dirs: нужен список путей")
        clean_dirs = []
        for value in dirs:
            value = _string(value, "temp_dirs", empty=False, limit=500)
            if not (value.startswith("/") or value == "~" or value.startswith("~/")):
                raise ValueError(f"temp_dirs: путь {value} должен начинаться с / или ~/")
            clean_dirs.append(value)
        result["temp_dirs"] = list(dict.fromkeys(clean_dirs))
    return result
