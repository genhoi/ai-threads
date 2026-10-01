"""Запуск агента без человека для сводок: выбор агента, команда, поток событий, ответ.

Сводка по сессии и сводка за несколько дней запускают CLI одинаково: агенту дают
инструкцию (`summary-agent.md` или `digest-agent.md`), короткий запрос и папки с
журналами только для чтения. Строки потока агент переводит в общие события.
"""

from __future__ import annotations

import json
from pathlib import Path

from . import settings
from .agents import AGENTS, RUNNERS
from .agents.base import Agent
from .config import ROOT
from .jobs import Job

INSTRUCTIONS = {"summary": ROOT / "summary-agent.md", "digest": ROOT / "digest-agent.md",
                "search-plan": ROOT / "search-plan-agent.md", "search-rank": ROOT / "search-rank-agent.md"}


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
            return None, f"{wanted} не умеет составлять сводки"
        if not agent.program():
            return None, f"{agent.name} не установлен"
        return agent, ""
    ready = available()
    if not ready:
        names = ", ".join(AGENTS[agent_id].name for agent_id in RUNNERS)
        return None, f"Не нашёл ни одного агента для сводок. Установите один из: {names}"
    return AGENTS[ready[0]], ""


def language_line() -> str:
    if settings.effective()["language"] == "en":
        return "Write every text field of the JSON answer in English."
    return "Все текстовые поля ответа пиши на русском языке."


def run(job: Job, agent: Agent, kind: str, prompt: str, *, add_dirs: list[str], workdir: Path,
        timeout: float, on_event) -> tuple[int, str, str]:
    """Запустить агента в рабочей папке и вернуть (код, хвост вывода, ответ).
    `kind` выбирает инструкцию из INSTRUCTIONS."""
    out_file = Path(workdir) / "answer.json" if agent.answer_file else None
    argv, stdin = agent.headless(agent.program(), prompt, instructions=INSTRUCTIONS[kind],
                                 add_dirs=add_dirs, out_file=out_file)
    state = {"answer": ""}

    def on_line(line: str) -> None:
        try:
            row = json.loads(line)
        except ValueError:
            return
        if not isinstance(row, dict):
            return
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


def failure(agent: Agent, code: int, timeout: float) -> str:
    if code == -9:
        return f"{agent.name} не ответил за {int(timeout)} с"
    return f"{agent.name} завершился с кодом {code}"
