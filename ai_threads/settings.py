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

from . import i18n
from .config import home

LANGUAGES = ("auto", "ru", "en")
OPEN_WITH = ("auto", "wt", "tmux", "plain")
TMUX_NAME = re.compile(r"^[A-Za-z0-9_.-]{1,40}$")

DEFAULTS = {
    "agents": {},
    "open_with": "auto",
    "tmux_session": "nit",
    "summary": {"agent": "auto", "timeout": 300},
    "digest": {"agent": "auto", "timeout": 900},
    "search": {"agent": "auto", "timeout": 300},
    "language": "auto",
    "temp_dirs": ["/tmp", "/var/tmp", "~/.local"],
}
AGENT_FIELDS = ("enabled", "home", "program", "skip_approvals")

_lock = threading.Lock()
_cache = {"stamp": None, "data": {}, "error": None, "effective": DEFAULTS}

i18n.add({
    "settings.need_string": {"ru": "{name}: нужна строка до {limit} символов",
                             "en": "{name}: must be a string of up to {limit} characters"},
    "settings.need_seconds": {"ru": "{name}: нужно число секунд от 10 до 7200",
                              "en": "{name}: must be a number of seconds from 10 to 7200"},
    "settings.need_object": {"ru": "Настройки должны быть JSON-объектом", "en": "Settings must be a JSON object"},
    "settings.unknown": {"ru": "Неизвестные настройки: {names}", "en": "Unknown settings: {names}"},
    "settings.agents_object": {"ru": "agents: нужен объект", "en": "agents: must be an object"},
    "settings.unknown_agent": {"ru": "agents: неизвестный агент {agent}", "en": "agents: unknown agent {agent}"},
    "settings.agent_fields": {"ru": "agents.{agent}: допустимы поля {fields}",
                              "en": "agents.{agent}: allowed fields are {fields}"},
    "settings.need_bool": {"ru": "{name}: нужно true или false", "en": "{name}: must be true or false"},
    "settings.one_of": {"ru": "{name}: одно из {values}", "en": "{name}: one of {values}"},
    "settings.tmux_name": {"ru": "tmux_session: латинские буквы, цифры, точка, дефис и подчёркивание",
                           "en": "tmux_session: Latin letters, digits, dot, hyphen and underscore only"},
    "settings.kind_fields": {"ru": "{kind}: допустимы поля agent и timeout",
                             "en": "{kind}: allowed fields are agent and timeout"},
    "settings.kind_agent": {"ru": "{kind}.agent: auto или один из {agents}",
                            "en": "{kind}.agent: auto or one of {agents}"},
    "settings.temp_list": {"ru": "temp_dirs: нужен список путей", "en": "temp_dirs: must be a list of paths"},
    "settings.temp_path": {"ru": "temp_dirs: путь {path} должен начинаться с / или ~/",
                           "en": "temp_dirs: path {path} must start with / or ~/"},
})


class Invalid(ValueError):
    """Неверное значение настройки. Текст собирается при показе, а не при проверке: файл
    проверяется и внутри `_load`, где язык ещё не узнать (он сам берётся из настроек). Так и
    причина из `error()` видна на языке того, кто её читает."""

    def __init__(self, key: str, **params):
        super().__init__(key)
        self.key, self.params = key, params

    def __str__(self) -> str:
        return i18n.t(self.key, **self.params)


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
                    data, error = {}, (file, exc)
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
    problem = _load()["error"]
    return f"{problem[0]}: {problem[1]}" if problem else None


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
        raise Invalid("settings.need_string", name=name, limit=limit)
    return value.strip()


def _timeout(value, name) -> int | float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not 10 <= value <= 7200:
        raise Invalid("settings.need_seconds", name=name)
    return value


def validate(data) -> dict:
    """Нормализованные значения или ValueError с понятной причиной."""
    from .agents import AGENTS, RUNNERS

    if not isinstance(data, dict):
        raise Invalid("settings.need_object")
    unknown = set(data) - set(DEFAULTS)
    if unknown:
        raise Invalid("settings.unknown", names=", ".join(sorted(unknown)))
    result = {}
    if "agents" in data:
        agents = data["agents"]
        if not isinstance(agents, dict):
            raise Invalid("settings.agents_object")
        result["agents"] = {}
        for agent_id, values in agents.items():
            if agent_id not in AGENTS:
                raise Invalid("settings.unknown_agent", agent=agent_id)
            if not isinstance(values, dict) or set(values) - set(AGENT_FIELDS):
                raise Invalid("settings.agent_fields", agent=agent_id, fields=", ".join(AGENT_FIELDS))
            clean = {}
            for field, value in values.items():
                name = f"agents.{agent_id}.{field}"
                if field in ("enabled", "skip_approvals"):
                    if not isinstance(value, bool):
                        raise Invalid("settings.need_bool", name=name)
                    clean[field] = value
                    continue
                clean[field] = _string(value, name)
            result["agents"][agent_id] = clean
    if "open_with" in data:
        if data["open_with"] not in OPEN_WITH:
            raise Invalid("settings.one_of", name="open_with", values=", ".join(OPEN_WITH))
        result["open_with"] = data["open_with"]
    if "tmux_session" in data:
        if not isinstance(data["tmux_session"], str) or not TMUX_NAME.fullmatch(data["tmux_session"]):
            raise Invalid("settings.tmux_name")
        result["tmux_session"] = data["tmux_session"]
    for kind in ("summary", "digest", "search"):
        if kind not in data:
            continue
        values = data[kind]
        if not isinstance(values, dict) or set(values) - {"agent", "timeout"}:
            raise Invalid("settings.kind_fields", kind=kind)
        clean = {}
        if "agent" in values:
            if values["agent"] != "auto" and values["agent"] not in RUNNERS:
                raise Invalid("settings.kind_agent", kind=kind, agents=", ".join(RUNNERS))
            clean["agent"] = values["agent"]
        if "timeout" in values:
            clean["timeout"] = _timeout(values["timeout"], f"{kind}.timeout")
        result[kind] = clean
    if "language" in data:
        if data["language"] not in LANGUAGES:
            raise Invalid("settings.one_of", name="language", values=", ".join(LANGUAGES))
        result["language"] = data["language"]
    if "temp_dirs" in data:
        dirs = data["temp_dirs"]
        if not isinstance(dirs, list) or len(dirs) > 50:
            raise Invalid("settings.temp_list")
        clean_dirs = []
        for value in dirs:
            value = _string(value, "temp_dirs", empty=False, limit=500)
            if not (value.startswith("/") or value == "~" or value.startswith("~/")):
                raise Invalid("settings.temp_path", path=value)
            clean_dirs.append(value)
        result["temp_dirs"] = list(dict.fromkeys(clean_dirs))
    return result
