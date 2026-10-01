"""Запуск агента без человека для сводок: выбор агента, команда, поток событий, ответ.

Сводка по сессии и сводка за несколько дней запускают CLI одинаково: агенту дают
инструкцию (`summary-agent.md` или `digest-agent.md`), короткий запрос и папки с
журналами только для чтения. Строки потока агент переводит в общие события.
"""

from __future__ import annotations

import contextlib
import json
import re
import shutil
import tempfile
import threading
import time
from pathlib import Path

from . import config, i18n, settings
from .agents import AGENTS, RUNNERS
from .agents.base import Agent, clip
from .config import ROOT
from .jobs import Job
from .model import Session

INSTRUCTIONS = {"summary": ROOT / "summary-agent.md", "digest": ROOT / "digest-agent.md",
                "search-plan": ROOT / "search-plan-agent.md", "search-rank": ROOT / "search-rank-agent.md"}

i18n.add({
    "runner.bad_choice": {"ru": "Неверная модель или уровень рассуждений", "en": "Invalid model or reasoning effort"},
    "runner.session_unknown": {"ru": "Эту сессию агента сервер не помнит: она начата до перезапуска. Продолжите её в терминале.",
                               "en": "The server does not know this agent session: it started before a restart. Continue it in a terminal."},
    "runner.not_installed": {"ru": "{agent} не установлен", "en": "{agent} is not installed"},
    "runner.no_agents": {"ru": "Не нашёл ни одного агента для сводок. Установите один из: {names}",
                         "en": "No agent for summaries found. Install one of: {names}"},
    "runner.timeout": {"ru": "{agent} не ответил за {seconds} с", "en": "{agent} didn't answer in {seconds} s"},
    "runner.exit_code": {"ru": "{agent} завершился с кодом {code}", "en": "{agent} exited with code {code}"},
    "runner.empty_answer": {"ru": "{agent} вернул пустой ответ", "en": "{agent} returned an empty answer"},
})


def available() -> list[str]:
    return [agent_id for agent_id in RUNNERS if AGENTS[agent_id].enabled() and AGENTS[agent_id].program()]


def pick(kind: str, requested: str | None = None) -> tuple[Agent | None, str]:
    """Агент для сводки и сообщение об ошибке, если запустить некого.

    Порядок: агент из запроса, из настроек, первый установленный из RUNNERS.
    """
    wanted = requested or settings.effective()[kind]["agent"]
    if wanted and wanted != "auto":
        agent = AGENTS.get(wanted)
        if agent is None or not agent.runner:
            return None, i18n.t("agents.no_runner", agent=wanted)
        if not agent.program():
            return None, i18n.t("runner.not_installed", agent=agent.name)
        return agent, ""
    ready = available()
    if not ready:
        names = ", ".join(AGENTS[agent_id].name for agent_id in RUNNERS)
        return None, i18n.t("runner.no_agents", names=names)
    return AGENTS[ready[0]], ""


def language_line() -> str:
    if i18n.current() == "en":
        return "Write every text field of the JSON answer in English."
    return "Все текстовые поля ответа пиши на русском языке."


# Рабочие папки запусков. Они остаются после работы: Claude Code и Kimi находят сессию по
# папке, в которой она шла, и без неё агенту нельзя ответить. Папки старше недели удаляются.
RUNS_KEEP_DAYS = 7
# Сессии агентов, начатые этим сервером: ID → агент, инструкция и рабочая папка.
SESSIONS: dict[str, dict] = {}
_sessions_lock = threading.Lock()


def workdir(kind: str) -> Path:
    runs = config.data_dir() / "runs"
    runs.mkdir(parents=True, exist_ok=True)
    limit = time.time() - RUNS_KEEP_DAYS * 86400
    for old in runs.iterdir():
        try:
            if old.is_dir() and old.stat().st_mtime < limit:
                shutil.rmtree(old, ignore_errors=True)
        except OSError:
            pass
    return Path(tempfile.mkdtemp(prefix=time.strftime("%Y%m%d-%H%M%S-") + kind + "-", dir=runs))


@contextlib.contextmanager
def run_dir(kind: str):
    """Рабочая папка запуска, которая остаётся после него (см. workdir)."""
    yield str(workdir(kind))


def nit(job: Job, text: str) -> None:
    """Строка журнала от самой «Нити»: что она делает между шагами агента."""
    job.emit({"type": "trace", "agent": "nit", "kind": "nit", "text": text})


CHOICE = re.compile(r"^[A-Za-z0-9._:/@+-]{1,100}$")
_models_cache: dict[str, tuple[float, dict]] = {}


def models(agent: Agent) -> dict:
    """Модели и уровни рассуждений агента из его CLI, с кешем на минуту."""
    cached = _models_cache.get(agent.id)
    if cached and time.monotonic() - cached[0] < 60:
        return cached[1]
    value = agent.models()
    _models_cache[agent.id] = (time.monotonic(), value)
    return value


def choice(model, effort) -> tuple[str, str]:
    """Проверить модель и уровень рассуждений из запроса; пустые — как настроено в CLI."""
    model = model or ""
    effort = effort or ""
    for value in (model, effort):
        if not isinstance(value, str) or (value and not CHOICE.fullmatch(value)):
            raise ValueError(i18n.t("runner.bad_choice"))
    return model, effort


def run(job: Job, agent: Agent, kind: str, prompt: str, *, add_dirs: list[str], workdir: Path,
        timeout: float, on_event, session: str = "", model: str = "", effort: str = "") -> tuple[int, str, str]:
    """Запустить агента в рабочей папке и вернуть (код, хвост вывода, ответ).

    `kind` выбирает инструкцию из INSTRUCTIONS. С `session` продолжает уже начатую сессию
    агента сообщением `prompt`. По ходу работы публикует события `trace` — что модель думает,
    пишет, какие инструменты вызывает — и `agent_session` с ID сессии, чтобы ей можно было ответить.
    """
    out_file = Path(workdir) / "answer.json" if agent.answer_file else None
    if session:
        argv, stdin = agent.reply(agent.program(), session, prompt, instructions=INSTRUCTIONS[kind],
                                  add_dirs=add_dirs, out_file=out_file)
    else:
        argv, stdin = agent.headless(agent.program(), prompt, instructions=INSTRUCTIONS[kind],
                                     add_dirs=add_dirs, out_file=out_file)
    # Модель и уровень рассуждений: сразу после имени программы, их понимают все четыре CLI.
    argv = argv[:1] + agent.choice_args(model, effort) + argv[1:]
    state = {"answer": "", "session": session}
    job.emit({"type": "trace", "agent": agent.id, "kind": "prompt", "text": clip(prompt, 20000)})

    def on_line(line: str) -> None:
        try:
            row = json.loads(line)
        except ValueError:
            return
        if not isinstance(row, dict):
            return
        found = agent.session_of(row)
        if found and found != state["session"]:
            state["session"] = found
            with _sessions_lock:
                SESSIONS[found] = {"agent": agent.id, "kind": kind, "workdir": str(workdir), "add_dirs": add_dirs}
            resume = Session(tool=agent.id, id=found, title="", cwd=str(workdir), updated=0)
            job.emit({"type": "agent_session", "agent": agent.id, "session": found, "command": resume.command})
        for item in agent.trace(row):
            job.emit({"type": "trace", "agent": agent.id, **item})
        for event in agent.stream_events(row):
            if "answer" in event:
                state["answer"] = event["answer"]
            on_event(event)

    code, output = job.run_process(argv, cwd=workdir, timeout=timeout, on_line=on_line, input_text=stdin)
    if out_file is not None and out_file.is_file():
        try:
            answer = out_file.read_text(encoding="utf-8", errors="replace")
            if answer.strip():
                state["answer"] = answer
        except OSError:
            pass
    return code, output, state["answer"]


def reply(job: Job, agent_id: str, session: str, text: str) -> None:
    """Ответ человека в сессию агента, начатую сводкой или умным поиском."""
    with _sessions_lock:
        known = SESSIONS.get(session)
    agent = AGENTS.get(agent_id)
    if not known or agent is None or known["agent"] != agent_id:
        job.emit({"type": "error", "message": i18n.t("runner.session_unknown")})
        return
    if not agent.program():
        job.emit({"type": "error", "message": i18n.t("runner.not_installed", agent=agent.name)})
        return
    folder = Path(known["workdir"])
    folder.mkdir(parents=True, exist_ok=True)
    limit = settings.timeout("search")
    code, output, answer = run(job, agent, known["kind"], text, add_dirs=known["add_dirs"], workdir=folder,
                               timeout=limit, on_event=lambda event: None, session=session)
    if job.cancelled:
        return
    if code != 0:
        job.emit({"type": "error", "message": failure(agent, code, limit), "code": code, "output": output})
        return
    job.emit({"type": "result", "result": {"answer": answer.strip()}})


def failure(agent: Agent, code: int, timeout: float) -> str:
    if code == -9:
        return i18n.t("runner.timeout", agent=agent.name, seconds=int(timeout))
    return i18n.t("runner.exit_code", agent=agent.name, code=code)
