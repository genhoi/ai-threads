"""Общее для всех агентов: папка и программа, команда продолжения, чтение сессий, сводки.

Агент — объект `Agent`. Подкласс задаёт, где лежат сессии и как их читать. Агенты,
которые пишут переписку построчным JSON (JSONL), наследуют `JsonlAgent`: обход файлов,
кеш по размеру и времени изменения и разбор начала и конца журнала у них общие.
"""

import json
import os
import re
import shlex
import shutil
import subprocess
from dataclasses import asdict, fields
from datetime import datetime, timezone
from pathlib import Path

from .. import i18n, settings
from ..config import home
from ..model import Session
from ..sources.common import (HEAD_BYTES, TAIL_BYTES, clean_title, fingerprint, json_rows,
                              plain_line, timestamp, visible_text)

# Больше этого с конца журнала умный поиск не читает.
RAW_BYTES = 128 * 1024 * 1024
# Меняется вместе с правилами разбора: записи кеша со старой версией перечитываются.
PARSER_VERSION = 6
# Название, автор и проект без значения — пустые строки: подпись на нужном языке ставит страница.
NO_TITLE = ""

i18n.add({
    "agents.no_runner": {"ru": "{agent} не умеет составлять сводки", "en": "{agent} can't write summaries"},
})


class Agent:
    id = ""
    name = ""
    color = "#5a5e66"
    icon = ""                  # SVG 24×24 для списка и вкладок
    home_env = ""              # переменная окружения с папкой агента
    home_default = ""          # папка агента относительно домашней
    programs: tuple = ()       # имена программы в PATH
    program_fallbacks: tuple = ()  # пути к программе относительно папки агента
    resume = ""                # команда продолжения; {id} подставляется в кавычках
    resume_auto = ""           # команда для автоматических сессий, если отличается
    resume_hint_key = ""       # ключ i18n: подпись вместо команды, когда продолжить из терминала нельзя
    skip_flag = ""             # флаг «без подтверждений» — добавляется галочкой skip_approvals
    runner = False             # умеет составлять сводки (см. headless и stream_events)
    answer_file = False        # ответ модели приходит в файл, а не в поток

    # --- папка, программа, команда --------------------------------------

    def home(self) -> Path:
        value = os.environ.get(self.home_env, "") if self.home_env else ""
        if value:
            return Path(value)
        configured = settings.agent(self.id).get("home")
        if configured:
            return _expand(configured)
        return home() / self.home_default

    def home_source(self) -> str:
        if self.home_env and os.environ.get(self.home_env):
            return "env"
        return "settings" if settings.agent(self.id).get("home") else "default"

    def enabled(self) -> bool:
        return settings.agent(self.id).get("enabled", True)

    def program(self) -> str | None:
        configured = settings.agent(self.id).get("program")
        if configured:
            path = _expand(configured)
            return str(path) if path.is_file() and os.access(path, os.X_OK) else None
        for name in self.programs:
            found = shutil.which(name)
            if found:
                return found
        for relative in self.program_fallbacks:
            candidate = self.home() / relative
            if candidate.is_file():
                return str(candidate)
        return None

    def resume_hint(self) -> str:
        """Подпись вместо команды продолжения на языке запроса; пустая, если не задана."""
        return i18n.t(self.resume_hint_key) if self.resume_hint_key else ""

    def resume_template(self, session: Session) -> str:
        return self.resume_auto if session.auto and self.resume_auto else self.resume

    def resume_command(self, session: Session) -> str:
        """Команда продолжения: программа из настроек или из PATH, ID в кавычках и флаг
        «без подтверждений», если он включён."""
        template = self.resume_template(session)
        if not template:
            return ""
        own = settings.agent(self.id)
        command = template.replace("{id}", shlex.quote(session.id))
        name = next((name for name in self.programs if command.startswith(name + " ")), "")
        if own.get("program") and name:
            command = shlex.quote(str(_expand(own["program"]))) + command[len(name):]
        if own.get("skip_approvals") and self.skip_flag:
            command += " " + self.skip_flag
        return command

    def to_json(self, sessions: int = 0) -> dict:
        program = self.program()
        return {"id": self.id, "name": self.name, "color": self.color, "icon": self.icon,
                "home": str(self.home()), "home_env": self.home_env, "home_source": self.home_source(),
                "home_default": "~/" + self.home_default, "home_exists": self.home().is_dir(),
                "program": program or "", "program_names": list(self.programs),
                "needs_program": bool(self.programs or self.program_fallbacks), "enabled": self.enabled(),
                "sessions": sessions, "resume": self.resume, "resume_auto": self.resume_auto,
                "resume_hint": self.resume_hint(), "skip_flag": self.skip_flag,
                "skip_approvals": bool(settings.agent(self.id).get("skip_approvals")),
                "runner": self.runner, "can_run": bool(self.runner and program)}

    # --- каталог ----------------------------------------------------------

    def discover(self) -> list[Path]:
        """Единицы чтения: файлы, по которым находятся сессии."""
        return []

    def scan(self, cache: dict, units=None, on_unit=None) -> list[Session]:
        """Сессии из единиц чтения. `on_unit(sessions)` вызывается после каждой единицы."""
        return []

    def transcript(self, session: Session, limit_bytes: int, *, text_limit: int | None = 12000):
        """Последние сообщения и признак, что журнал прочитан не целиком."""
        return [], False

    def excerpt_messages(self, session: Session) -> list[dict]:
        """Сообщения начала и конца журнала — для выдержки в сводке за несколько дней."""
        return self.transcript(session, HEAD_BYTES)[0]

    def raw_text(self, session: Session) -> bytes:
        """Весь текст сессии для умного поиска: вместе с выводом команд и прочитанными файлами."""
        messages, _ = self.transcript(session, RAW_BYTES, text_limit=None)
        return "\n".join(m["text"] for m in messages).encode("utf-8")

    def plain_journal(self, session: Session) -> bool:
        """Журнал можно дать модели как есть. Иначе сводка выгружает переписку в текстовый файл."""
        return True

    # --- статусы ----------------------------------------------------------

    def live(self, sessions: dict) -> list[tuple[str, dict, float]]:
        """Запущенные сейчас сессии: (ключ, {"state", "where", "since"}, время отметки)."""
        return []

    # --- сводки -----------------------------------------------------------

    def headless(self, program: str, prompt: str, *, instructions: Path, add_dirs: list[str],
                 out_file: Path | None) -> tuple[list[str], str | None]:
        """Командная строка запуска без человека и текст для stdin (None — не нужен)."""
        raise ValueError(i18n.t("agents.no_runner", agent=self.name))

    def models(self) -> dict:
        """Модели и уровни рассуждений, которые знает сам CLI: {"default_model", "default_effort",
        "models": [{"id", "name", "efforts", "default_effort"}], "efforts"}. Пустые значения по
        умолчанию — «как настроено в CLI». Список берётся у CLI при каждом обращении (с кешем на
        минуту), поэтому новые модели появляются без правки настроек «Нити»."""
        return {"default_model": "", "default_effort": "", "models": [], "efforts": []}

    def choice_args(self, model: str, effort: str) -> list[str]:
        """Флаги выбора модели и уровня рассуждений; ставятся сразу после имени программы."""
        return []

    def session_of(self, row: dict) -> str:
        """ID сессии CLI из строки потока или пустая строка. По нему агенту можно ответить."""
        value = row.get("session_id")
        return value if isinstance(value, str) else ""

    def trace(self, row: dict) -> list[dict]:
        """Строки журнала для человека из строки потока: что модель думает, пишет, какие
        инструменты вызывает и что получает. {"kind": "model"|"thinking"|"text"|"tool"|"result"|
        "usage", "text": ..., "tool": имя инструмента}."""
        return []

    def reply(self, program: str, session_id: str, text: str, *, instructions: Path, add_dirs: list[str],
              out_file: Path | None) -> tuple[list[str], str | None]:
        """Командная строка, которая продолжает сессию без человека сообщением `text`."""
        raise ValueError(f"{self.name} не умеет продолжать сессию")

    def stream_events(self, row: dict) -> list[dict]:
        """События из строки потока: {"read": путь, "call": id}, {"done": id},
        {"text": текст}, {"answer": текст}, {"tokens": число, "final": bool}."""
        return []


def _expand(value: str) -> Path:
    if value == "~" or value.startswith("~/"):
        return home() / value[2:]
    return Path(value)


# Длиннее строка журнала обрезается: вывод команд и прочитанные файлы бывают огромными.
TRACE_MAX = 4000


def clip(text, limit: int = TRACE_MAX) -> str:
    text = text if isinstance(text, str) else json.dumps(text, ensure_ascii=False)
    return text if len(text) <= limit else text[:limit] + f" … (+{len(text) - limit})"


def toml_top_level(path: Path) -> dict:
    """Строковые значения верхнего уровня TOML (до первой секции). tomllib есть только с Python 3.11."""
    values = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return values
    for line in lines:
        line = line.strip()
        if line.startswith("["):
            break
        match = re.fullmatch(r'([A-Za-z0-9_]+)\s*=\s*"([^"]*)"', line)
        if match:
            values[match.group(1)] = match.group(2)
    return values


def run_quietly(argv: list[str], timeout: float = 15) -> str:
    """Вывод короткой служебной команды CLI или пустая строка, если не удалось."""
    try:
        return subprocess.run(argv, capture_output=True, text=True, timeout=timeout,
                              stdin=subprocess.DEVNULL).stdout
    except (OSError, subprocess.SubprocessError):
        return ""


def instructions_text(path: Path) -> str:
    """Инструкция агента без шапки YAML — для CLI, которым файл агента не передать."""
    text = path.read_text(encoding="utf-8")
    if text.startswith("---"):
        end = text.find("\n---", 3)
        if end >= 0:
            text = text[end + 4:]
    return text.strip()


def message(role: str, text: str, ts: float, text_limit: int | None) -> dict:
    return {"role": role, "text": text[:text_limit] if text_limit else text,
            "at": datetime.fromtimestamp(ts, timezone.utc).isoformat() if ts else None}


class JsonlAgent(Agent):
    """Агент, у которого каждая сессия — файл с перепиской в построчном JSON."""

    pattern = ""               # шаблон поиска файлов сессий от папки агента

    def journal_path(self, path: Path) -> Path:
        return path

    def discover(self) -> list[Path]:
        return sorted(self.home().glob(self.pattern))

    # Переопределяется агентом -------------------------------------------

    def message_of(self, row: dict):
        """(роль, содержимое, время) из строки журнала или None."""
        return None

    def read_info(self, path: Path, journal: Path):
        """Данные сессии до разбора переписки или None, если файл — не сессия."""
        return {}

    def describe(self, path: Path, info, head: list, rows: list, fields_: dict, extra: dict) -> None:
        """Заполнить id, cwd, ветку, признак автозапуска и прочее по данным агента."""

    def valid_id(self, sid: str) -> bool:
        return bool(sid)

    def indexes(self, cache: dict):
        """Индексы названий и истории ввода, нужные `finish`."""
        return None

    def finish(self, session: Session, entry: dict, context) -> None:
        """Последние правки сессии по индексам: название, папка, автозапуск."""

    def valid_extra(self, entry: dict) -> bool:
        return True

    # Общее ---------------------------------------------------------------

    def messages_from_rows(self, rows, *, text_limit: int | None = 12000) -> list[dict]:
        result = []
        for row in rows:
            found = self.message_of(row)
            if not found:
                continue
            role, value, at = found
            if role not in ("user", "assistant"):
                continue
            text = visible_text(value)
            if text:
                result.append(message(role, text, timestamp(at), text_limit))
        return result

    def transcript(self, session, limit_bytes, *, text_limit=12000):
        rows = self.messages_from_rows(json_rows(session.journal, limit_bytes, tail=True), text_limit=text_limit)
        return rows, fingerprint(session.journal)["size"] > limit_bytes

    def raw_text(self, session):
        try:
            with Path(session.journal).open("rb") as stream:
                size = stream.seek(0, 2)
                stream.seek(max(0, size - RAW_BYTES))
                return stream.read()
        except OSError:
            return b""

    def excerpt_messages(self, session):
        size = fingerprint(session.journal)["size"]
        rows = list(json_rows(session.journal, HEAD_BYTES))
        if size > HEAD_BYTES:
            rows += list(json_rows(session.journal, TAIL_BYTES, tail=True))
        return self.messages_from_rows(rows)

    def parse(self, path: Path, journal: Path, stat: dict) -> dict:
        info = self.read_info(path, journal)
        if info is None:
            return {"skip": True}
        head = list(json_rows(journal, HEAD_BYTES))
        tail = list(json_rows(journal, TAIL_BYTES, tail=True)) if stat["journal_size"] > HEAD_BYTES else []
        rows = head + tail
        transcript = self.messages_from_rows(rows)
        first = next((clean_title(m["text"]) for m in transcript if m["role"] == "user"), "")
        times = [t for t in (timestamp(r.get("timestamp") or r.get("time")) for r in rows) if t]
        updated = max(times, default=stat["mtime_ns"] / 10**9)
        created = min(times, default=updated)
        values = {"id": path.stem, "title": first, "cwd": "", "branch": "", "created": created,
                  "updated": updated, "auto": False, "by": ""}
        extra = {"first_title": first}
        self.describe(path, info, head, rows, values, extra)
        if not self.valid_id(values["id"]):
            return {"skip": True}
        last = next((plain_line(m["text"]) for m in reversed(transcript) if m["role"] == "assistant"), "")
        session = Session(tool=self.id, title=values.pop("title") or NO_TITLE, empty=not transcript,
                          last=last, journal=journal, meta_path=path, **values)
        return {**asdict(session), "journal": str(journal), "meta_path": str(path), **extra}

    def valid_record(self, entry) -> bool:
        if entry.get("parser") != PARSER_VERSION:
            return False
        if entry.get("skip") is True:
            return True
        strings = ("tool", "id", "title", "cwd", "branch", "by", "last", "journal", "meta_path", "first_title")
        if any(not isinstance(entry.get(k), str) for k in strings) or entry["tool"] != self.id:
            return False
        if any(type(entry.get(k)) is not bool for k in ("auto", "empty")):
            return False
        if any(type(entry.get(k)) not in (int, float) or entry[k] != entry[k] or abs(entry[k]) == float("inf")
               for k in ("created", "updated")):
            return False
        return self.valid_extra(entry)

    def scan(self, cache, units=None, on_unit=None):
        context = self.indexes(cache)
        result = []
        for path in self.discover() if units is None else units:
            journal = self.journal_path(path)
            stat = fingerprint(path)
            journal_stat = stat if journal == path else fingerprint(journal)
            stamp = {**stat, **{"journal_" + k: v for k, v in journal_stat.items()}}
            entry = cache.get(str(path), {})
            if not isinstance(entry, dict) or any(entry.get(k) != v for k, v in stamp.items()) \
                    or not self.valid_record(entry):
                entry = {}
            session = None
            if stat["size"] >= 0:
                if not entry:
                    entry = {**stamp, **self.parse(path, journal, stamp), "parser": PARSER_VERSION}
                if not entry.get("skip"):
                    session = Session(**{f.name: entry[f.name] for f in fields(Session)})
                    session.journal, session.meta_path = journal, path
                    if session.branch == "HEAD":
                        # Отсоединённый HEAD — не ветка; в строке сессии он только мешает.
                        session.branch = ""
                    self.finish(session, entry, context)
                    entry.update(asdict(session))
                    entry.update(journal=str(journal), meta_path=str(path))
                    result.append(session)
                cache[str(path)] = entry
            if on_unit:
                on_unit([session] if session else [])
        return result


def first_title(entry: dict) -> str:
    return entry.get("first_title") or NO_TITLE


def shell_prompt(instructions: Path, prompt: str) -> str:
    """Инструкция агента и запрос одним текстом — для CLI без файла агента."""
    return instructions_text(instructions) + "\n\n---\n\n" + prompt


def quote_list(values) -> str:
    return " ".join(shlex.quote(str(v)) for v in values)
