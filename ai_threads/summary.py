"""Сводка по одной сессии.

Запускает агента для сводок (по умолчанию Kimi, иначе первый установленный) с
инструкцией summary-agent.md, публикует события прогресса и сохраняет результат через Store.
"""

from __future__ import annotations

import json
import re
import time
from pathlib import Path

from . import i18n, runner, settings
from .agents import get
from .jobs import Job
from .model import Session
from .sources.common import MESSAGE_BYTES

TITLE_MAX = 90
SUMMARY_MAX = 12000
WHY_MAX = 40
RELATED_MAX = 3
MAX_CANDIDATES = 30
#: Имя файла с перепиской в рабочей папке, если журнал агента нельзя дать модели как есть.
EXPORT_FILE = "session.txt"

# Шаги прогресса — ключи сообщений; шаг чтения хранится номером фрагмента.
STEP_LAUNCH = "summary.launch"
STEP_ANALYZE = "summary.analyze"
STEP_WRITE = "summary.write"

i18n.add({
    "summary.launch": {"ru": "запускается", "en": "starting"},
    "summary.reading": {"ru": "читает фрагмент {n}", "en": "reading part {n}"},
    "summary.analyze": {"ru": "анализирует журнал", "en": "analyzing the log"},
    "summary.write": {"ru": "пишет сводку", "en": "writing the summary"},
    "summary.no_journal": {"ru": "Журнал сессии недоступен", "en": "The session log is unavailable"},
    "summary.bad_answer": {"ru": "{agent} вернул ответ без названия или сводки. Попробуй ещё раз.",
                           "en": "{agent} returned an answer without a title or summary. Try again."},
    "summary.not_object": {"ru": "ответ не JSON-объект", "en": "the answer is not a JSON object"},
    "summary.no_fields": {"ru": "нет строковых полей title и summary", "en": "no string fields title and summary"},
    "summary.empty_fields": {"ru": "пустые title или summary", "en": "title or summary is empty"},
})


def timeout() -> float:
    return settings.timeout("summary")


def export_transcript(session: Session, target: Path) -> Path:
    """Переписка текстом — для агентов, чей журнал лежит в базе SQLite."""
    messages, _ = get(session.tool).transcript(session, MESSAGE_BYTES, text_limit=None)
    lines = []
    for item in messages:
        lines.append(f"## {item['role']} · {item['at'] or ''}".rstrip(" ·"))
        lines.append(item["text"])
        lines.append("")
    target.write_text("\n".join(lines), encoding="utf-8")
    return target


class _Progress:
    """Шаги прогресса как в макете: запускается, читает фрагмент N,
    анализирует журнал, пишет сводку. Число фрагментов заранее неизвестно,
    список шагов растёт по ходу чтения."""

    def __init__(self, job: Job, agent: str):
        self._job = job
        self._agent = agent
        self.steps: list[str | int] = [STEP_LAUNCH]
        self.index = 0
        self._emit()

    def _emit(self) -> None:
        step = self.steps[self.index]
        text = i18n.t("summary.reading", n=step) if isinstance(step, int) else i18n.t(step)
        self._job.emit({"type": "progress", "step": self.index, "agent": self._agent,
                        "steps": len(self.steps), "text": text})

    def reading(self, fragment: int) -> None:
        if self.steps[self.index] in (STEP_ANALYZE, STEP_WRITE):
            self.steps = self.steps[:self.index]
        self.steps = self.steps[:fragment + 1]
        while len(self.steps) <= fragment:
            self.steps.append(len(self.steps))
        self.index = fragment
        self._emit()

    def analyzing(self) -> None:
        self.steps = self.steps[:self.index + 1] + [STEP_ANALYZE]
        self.index = len(self.steps) - 1
        self._emit()

    def writing(self) -> None:
        if self.steps[self.index] != STEP_WRITE:
            self.steps = self.steps[:self.index + 1] + [STEP_WRITE]
            self.index = len(self.steps) - 1
            self._emit()


def build_prompt(session: Session, current_title: str,
                 candidates: list[tuple[str, str]], journal: Path | None = None) -> str:
    lines = [
        f"Инструмент: {get(session.tool).name}",
        f"Проект: {session.project}",
        f"Текущее название: {current_title or session.title}",
        f"Исходное название: {session.title}",
        f"Путь к журналу: {journal or session.journal}",
    ]
    if candidates:
        lines.append("Сессии того же проекта за последние 30 дней "
                     "(для поля related — только из этого списка):")
        for key, title in candidates[:MAX_CANDIDATES]:
            lines.append(f"- {key} — {title}")
    lines.append("Прочитай этот журнал и верни сводку по инструкции агента.")
    lines.append(runner.language_line())
    return "\n".join(lines)


def loads_answer(text: str) -> dict:
    """Разобрать JSON из ответа модели: целиком, из блока ```json или
    из фрагмента от первой «{» до последней «}», если модель добавила прозу."""
    content = text.strip()
    fenced = re.search(r"```(?:json)?\s*\n(.*?)```", content,
                       flags=re.DOTALL | re.IGNORECASE)
    if fenced:
        content = fenced.group(1).strip()
    try:
        answer = json.loads(content)
    except ValueError:
        start, end = content.find("{"), content.rfind("}")
        if start < 0 or end <= start:
            raise
        answer = json.loads(content[start:end + 1])
    if not isinstance(answer, dict):
        raise ValueError(i18n.t("summary.not_object"))
    return answer


def parse_answer(text: str, valid_keys: set[str]) -> dict:
    """Разобрать ответ агента. Обязательны непустые строковые title и summary;
    related фильтруется по переданному списку ключей (до 3, why до 40 символов)."""
    answer = loads_answer(text)
    title = answer.get("title")
    summary = answer.get("summary")
    if not isinstance(title, str) or not isinstance(summary, str):
        raise ValueError(i18n.t("summary.no_fields"))
    title, summary = title.strip(), summary.strip()
    if not title or not summary:
        raise ValueError(i18n.t("summary.empty_fields"))

    next_step = answer.get("next_step", "")
    if not isinstance(next_step, str):
        next_step = str(next_step)
    related = []
    raw_related = answer.get("related")
    if isinstance(raw_related, list):
        for item in raw_related:
            if not isinstance(item, dict):
                continue
            key = item.get("key")
            if not isinstance(key, str) or key not in valid_keys:
                continue
            why = item.get("why", "")
            if not isinstance(why, str):
                why = str(why)
            related.append({"key": key, "why": why.strip()[:WHY_MAX]})
            if len(related) >= RELATED_MAX:
                break

    return {
        "title": title[:TITLE_MAX],
        "summary": summary[:SUMMARY_MAX],
        "next_step": next_step.strip(),
        "closed": bool(answer.get("closed")),
        "related": related,
    }


def run_summary(job: Job, session: Session, current_title: str,
                candidates: list[tuple[str, str]], store, agent_id: str | None = None,
                model: str = "", effort: str = "") -> None:
    """Собрать сводку по сессии и сохранить её в store."""
    journal = Path(session.journal)
    if not journal.is_file():
        job.emit({"type": "error", "message": i18n.t("summary.no_journal")})
        return
    agent, problem = runner.pick("summary", agent_id)
    if agent is None:
        job.emit({"type": "error", "message": problem})
        return

    valid_keys = {key for key, _ in candidates}
    progress = _Progress(job, agent.id)
    state = {"reads": 0}

    def on_event(event: dict) -> None:
        if "read" in event:
            state["reads"] += 1
            progress.reading(state["reads"])
        elif "done" in event:
            progress.analyzing()
        elif "answer" in event:
            progress.writing()

    limit = timeout()
    with runner.run_dir("summary") as workdir:
        plain = get(session.tool).plain_journal(session)
        source = journal if plain else export_transcript(session, Path(workdir) / EXPORT_FILE)
        prompt = build_prompt(session, current_title, candidates, source)
        code, output, answer = runner.run(job, agent, "summary", prompt,
                                          add_dirs=[str(journal.parent)] if plain else [],
                                          workdir=Path(workdir), timeout=limit, on_event=on_event,
                                          model=model, effort=effort)

    if job.cancelled:
        return
    if code != 0:
        job.emit({"type": "error", "message": runner.failure(agent, code, limit), "code": code, "output": output})
        return
    answer = answer.strip()
    if not answer:
        job.emit({"type": "error", "message": i18n.t("runner.empty_answer", agent=agent.name), "code": code,
                  "output": output})
        return
    try:
        parsed = parse_answer(answer, valid_keys)
    except (ValueError, KeyError, TypeError):
        job.emit({"type": "error", "message": i18n.t("summary.bad_answer", agent=agent.name), "code": code,
                  "output": output})
        return

    result = {**parsed, "at": time.time(), "model": agent.id}
    store.set_summary(session.key, result)
    job.emit({"type": "result", "result": result})
