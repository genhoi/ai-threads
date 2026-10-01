"""Сводка за 3–5 дней по журналам сессий.

Модель сама читает журналы (только чтение) и возвращает JSON;
модуль разбирает её поток, публикует события этапов, чтения, заметок
и расхода контекста, а результат сохраняет через Store.
"""

from __future__ import annotations

import datetime
import json
import re
import tempfile
import time
from pathlib import Path

from . import runner, settings
from .agents import AGENTS, RUNNERS, get
from .jobs import Job
from .model import Session
from .sources.common import plain_line
from .summary import export_transcript, loads_answer

STAGES = ["Сбор", "Чтение журналов", "Разбор", "Текст"]

#: Предел контекста для полосы «контекст N из 128 тыс.» в интерфейсе.
TOKEN_LIMIT = 128000

NOTE_RE = re.compile(r"^ЗАМЕТКА\s+(\S+):\s*(.+)$")
LOG_MAX = 60
LOG_TEXT_MAX = 240

#: Предел выдержки из журнала, которая кладётся в запрос вместо чтения моделью.
EXCERPT_BUDGET = 900
#: Выдержка для автоматических запусков: название и последний ответ агента.
AUTO_TEXT_MAX = 160

#: Имя файла в рабочей папке модели со списком сессий и выдержками.
#: Промпт в argv у всех CLI ограничен (у Kimi — 128 КБ на аргумент),
#: поэтому модель получает короткий промпт и читает список сама.
INPUT_FILE = "digest-input.txt"
#: Папка в рабочей папке модели с перепиской сессий, чей журнал лежит в базе SQLite.
EXPORT_DIR = "journals"


def timeout() -> float:
    return settings.timeout("digest")


def models() -> list[dict]:
    """Агенты для сводки и есть ли их программа."""
    out = []
    for agent_id in RUNNERS:
        agent = AGENTS[agent_id]
        available = agent.enabled() and agent.program() is not None
        hint = f"Составить сводку через {agent.name}" if available else f"{agent.name} не установлен"
        out.append({"id": agent_id, "name": agent.name, "available": available, "hint": hint})
    return out


def _summaries_text(summary: dict) -> str:
    parts = []
    title = summary.get("title")
    if title:
        parts.append(str(title).rstrip("."))
    body = summary.get("summary")
    if body:
        parts.append(str(body))
    next_step = summary.get("next_step")
    if next_step:
        parts.append(f"Следующий шаг: {next_step}")
    return " ".join(parts).strip()[:600] or "есть, но пустая"


def _journal_size(session: Session):
    return _size(Path(session.journal))


def _size(path: Path):
    try:
        return path.stat().st_size
    except OSError:
        return "файла нет"


def _excerpt(session: Session, budget: int = EXCERPT_BUDGET) -> str:
    """Выдержка из переписки для запроса: первое сообщение пользователя
    и последние сообщения, коротко. Читаются начало и конец журнала."""
    try:
        messages = get(session.tool).excerpt_messages(session)
    except (OSError, ValueError):
        return ""
    if not messages:
        return ""
    parts = []
    first = next((m for m in messages if m["role"] == "user"), None)
    if first is not None:
        text = plain_line(first["text"], 500)
        if text:
            parts.append(f"начало: {text}")
    if len(messages) > 1:
        ending = " | ".join(f"{m['role']}: {plain_line(m['text'], 300)}"
                            for m in messages[-3:])
        if ending.strip(" |:"):
            parts.append(f"конец: {ending}")
    excerpt = " … ".join(parts)
    if len(excerpt) > budget:
        excerpt = excerpt[:budget].rstrip() + "…"
    return excerpt


def select_for_digest(sessions: list[Session], days: int,
                      hidden=None, *, today: datetime.date | None = None):
    """Отбор сессий периода для сводки.

    Ручные — не auto, не temp, не empty, не скрытые; автоматические —
    auto за тот же период. Период — с начала дня `сегодня − (days − 1)`
    по полю updated. Возвращает пару списков: (ручные, автоматические);
    в списке ручных пустые сессии есть, но в запрос не идут.
    """
    if today is None:
        today = datetime.date.today()
    start = datetime.datetime.combine(
        today - datetime.timedelta(days=days - 1),
        datetime.time.min).timestamp()
    hidden = set(hidden or ())
    period = [s for s in sessions if s.updated >= start]
    manual = [s for s in period
              if not s.auto and not s.temp and s.key not in hidden]
    autos = [s for s in period if s.auto]
    return manual, autos


def build_prompt(manual: list[Session], autos: list[Session], days: int,
                 titles: dict[str, str], summaries: dict,
                 excerpts: dict[str, str] | None = None,
                 journals: dict[str, Path] | None = None) -> str:
    """Текст запроса к модели: ручные сессии с готовыми сводками и
    выдержками и отдельно автоматические запуски. `journals` подменяет
    путь к журналу выгрузкой переписки, если журнал лежит в базе SQLite."""
    excerpts = excerpts or {}
    journals = journals or {}
    today = datetime.date.today()
    start = today - datetime.timedelta(days=days - 1)
    lines = [f"Составь сводку за {days} дня ({start.isoformat()} — {today.isoformat()}).",
             "", f"Ручные сессии ({len(manual)}):"]
    for s in manual:
        lines.append(f"- ключ: {s.key}")
        lines.append(f"  инструмент: {get(s.tool).name}")
        lines.append(f"  проект: {s.project}")
        lines.append(f"  название: {titles.get(s.key) or s.title}")
        ready = summaries.get(s.key)
        lines.append(f"  готовая сводка: {_summaries_text(ready) if ready else 'нет'}")
        excerpt = excerpts.get(s.key)
        lines.append(f"  выдержка: {excerpt if excerpt else 'нет'}")
        journal = journals.get(s.key)
        if journal:
            lines.append(f"  журнал: {journal} ({_size(journal)} байт)")
        else:
            lines.append(f"  журнал: {s.journal} ({_journal_size(s)} байт)")
    lines.append("")
    lines.append(f"Автоматические запуски ({len(autos)}): разбирай по названию и последнему "
                 "ответу; журналы их читать не нужно.")
    for s in autos:
        last = plain_line(s.last, AUTO_TEXT_MAX) or "нет"
        lines.append(f"- ключ: {s.key}")
        lines.append(f"  проект: {s.project}")
        lines.append(f"  название: {titles.get(s.key) or s.title}")
        lines.append(f"  последний ответ: {last}")
    lines.append("")
    lines.append("Опирайся на готовые сводки и выдержки — читай журнал сам "
                 "инструментом Read (только чтение) только если выдержки "
                 "явно не хватило. За весь прогон достаточно прочитать "
                 "не больше 10 журналов. "
                 "По ходу печатай строки «ЗАМЕТКА <проект>: …» — одно "
                 "предложение до 120 символов, по ходу чтения, не откладывая "
                 "на конец. Пиши кратко: bullets — 1–2 предложения. "
                 "Итог — только JSON по схеме из инструкции агента.")
    return "\n".join(lines)


def short_prompt(days: int) -> str:
    """Короткий промпт для CLI: список сессий модель читает из файла."""
    today = datetime.date.today()
    start = today - datetime.timedelta(days=days - 1)
    return (f"Составь сводку за {days} дня ({start.isoformat()} — "
            f"{today.isoformat()}). Полный список ручных сессий и "
            f"автоматических запусков с готовыми сводками и выдержками "
            f"лежит в файле {INPUT_FILE} в текущей папке — прочитай его "
            "первым инструментом Read и действуй по инструкции агента. "
            + runner.language_line())


class _Tracker:
    """Состояние прогона: этапы, статусы сессий, заметки, ответ."""

    def __init__(self, job: Job, manual: list[Session], autos: list[Session],
                 summaries: dict, excerpts: dict[str, str] | None = None,
                 journals: dict[str, Path] | None = None, agent: str = ""):
        self.job = job
        self.agent = agent
        self.by_path: dict[str, Session] = {}
        for s in [*manual, *autos]:
            if s.journal:
                self.by_path[str(Path(s.journal))] = s
        for s in manual:
            if journals and s.key in journals:
                self.by_path[str(Path(journals[s.key]))] = s
        self.status: dict[str, str] = {}
        self.pending: dict[str, list[str]] = {}
        self.read_done: set[str] = set()
        self._reading: set[str] = set()
        self.stage_index = -1
        self.answer = ""
        self.answer_ready = False
        self.logs = 0
        self._last_tokens = 0
        self._notes: set[tuple[str, str]] = set()
        excerpts = excerpts or {}
        # Хватает сводки или выдержки — ok; пустые — skip; остальные
        # модели придётся читать самой — queued, пока очередь не дошла.
        self._needed = {s.key for s in manual
                        if not s.empty and s.key not in summaries
                        and s.key not in excerpts}
        with_ready, with_excerpt = 0, 0
        for s in manual:
            if s.empty:
                status = "skip"
            elif s.key in summaries:
                status, with_ready = "ok", with_ready + 1
            elif s.key in excerpts:
                status, with_excerpt = "ok", with_excerpt + 1
            else:
                status = "queued"
            self.status[s.key] = status
            job.emit({"type": "session", "key": s.key, "status": status})
        self.stage(0)
        job.emit({"type": "log", "text":
                  f"Передано модели: {len(manual)} сессий "
                  f"({with_ready} с готовыми сводками, "
                  f"{with_excerpt} с выдержками, "
                  f"{len(self._needed)} прочитает сама), "
                  f"{len(autos)} автоматических запусков"})

    # --- события ---------------------------------------------------------

    def stage(self, index: int) -> None:
        while self.stage_index < index and self.stage_index < len(STAGES) - 1:
            self.stage_index += 1
            event = {"type": "stage", "index": self.stage_index, "label": STAGES[self.stage_index]}
            if self.agent:
                # Страница, открытая заново посреди прогона, узнаёт, какой агент работает.
                event["agent"] = self.agent
            self.job.emit(event)

    def _session_key(self, path: str | None) -> str | None:
        if not path:
            return None
        session = self.by_path.get(str(Path(path)))
        return session.key if session else None

    def read_start(self, path: str | None) -> None:
        key = self._session_key(path)
        if not key:
            return
        current = self.status.get(key)
        # повторное чтение не считаем; готовая сводка/выдержка не мешают
        # модели перепроверить журнал — статус честно меняется на now/read
        if current in ("read", "skip", "now"):
            return
        self.status[key] = "now"
        self._reading.add(key)
        self.job.emit({"type": "session", "key": key, "status": "now"})
        self.stage(1)

    def read_end(self, path: str | None) -> None:
        key = self._session_key(path)
        if not key or key in self.read_done or key not in self._reading:
            return
        self._reading.discard(key)
        self.read_done.add(key)
        self.status[key] = "read"
        self.job.emit({"type": "session", "key": key, "status": "read"})
        if self._needed and self._needed <= self.read_done:
            self.stage(2)

    def note(self, project: str, text: str) -> None:
        text = text.strip()[:300]
        project = project.strip()[:40]
        if not text or (project, text) in self._notes:
            return
        self._notes.add((project, text))
        self.job.emit({"type": "note", "project": project, "text": text})

    def log(self, text: str) -> None:
        text = " ".join(str(text).split())[:LOG_TEXT_MAX]
        if text and self.logs < LOG_MAX:
            self.logs += 1
            self.job.emit({"type": "log", "text": text})

    def tokens(self, used: int, force: bool = False) -> None:
        # поток claude шлёт thinking_tokens очень часто — ужимаем до заметных
        # шагов; итоговый расход из usage публикуем всегда
        if force or used - self._last_tokens >= 1000:
            self._last_tokens = used
            self.job.emit({"type": "tokens", "used": int(used),
                           "limit": TOKEN_LIMIT})

    def _text(self, chunk: str) -> None:
        for line in chunk.splitlines():
            stripped = line.strip()
            match = NOTE_RE.match(stripped)
            if match:
                self.note(match.group(1), match.group(2))
            elif len(stripped) >= 12 and stripped[0] not in '{[`"':
                self.log(line)

    # --- разбор потоков ---------------------------------------------------

    def feed(self, model: str, line: str) -> None:
        try:
            row = json.loads(line)
        except ValueError:
            return
        if isinstance(row, dict):
            for event in get(model).stream_events(row):
                self.apply(event)

    def apply(self, event: dict) -> None:
        """Общее событие потока агента (см. Agent.stream_events)."""
        if "read" in event:
            call, path = event.get("call"), event["read"]
            if call:
                self.pending.setdefault(call, [])
                if path:
                    self.pending[call].append(path)
            self.read_start(path)
        elif "done" in event:
            for path in self.pending.pop(event["done"], event.get("paths", [])):
                self.read_end(path)
        elif "text" in event:
            self._text(event["text"])
        elif "answer" in event:
            self.answer = event["answer"]
        elif "tokens" in event:
            self.tokens(event["tokens"], force=event.get("final", False))
        elif event.get("finished") and self.answer:
            self.stage(3)


def parse_digest(text: str, manual_keys: set[str], auto_keys: set[str]) -> dict:
    """Разобрать и проверить итоговый JSON модели."""
    data = loads_answer(text)
    lead = data.get("lead")
    if not isinstance(lead, str) or not lead.strip():
        raise ValueError("нет строкового поля lead")
    result = {"lead": lead.strip(), "projects": [], "tails": [], "autos": []}

    projects = data.get("projects")
    if not isinstance(projects, list):
        raise ValueError("projects не список")
    for project in projects[:30]:
        if not isinstance(project, dict):
            continue
        name = project.get("name")
        bullets = project.get("bullets")
        if not isinstance(name, str) or not isinstance(bullets, list):
            continue
        clean_bullets = []
        for bullet in bullets[:50]:
            if not isinstance(bullet, dict):
                continue
            text_b = bullet.get("text")
            if not isinstance(text_b, str) or not text_b.strip():
                continue
            keys = bullet.get("keys")
            clean_keys = [k for k in keys if isinstance(k, str) and k in manual_keys] \
                if isinstance(keys, list) else []
            clean_bullets.append({"text": text_b.strip(), "keys": clean_keys})
        if clean_bullets:
            result["projects"].append({"name": name.strip(),
                                       "bullets": clean_bullets})

    tails = data.get("tails")
    if not isinstance(tails, list):
        raise ValueError("tails не список")
    seen_ids: set[str] = set()
    for i, tail in enumerate(tails[:100]):
        if not isinstance(tail, dict):
            continue
        text_t = tail.get("text")
        key = tail.get("key")
        if not isinstance(text_t, str) or not text_t.strip():
            continue
        if not isinstance(key, str) or key not in manual_keys:
            continue
        tail_id = tail.get("id")
        if not isinstance(tail_id, str) or not tail_id.strip():
            tail_id = f"t{i + 1}"
        tail_id = tail_id.strip()
        while tail_id in seen_ids:
            tail_id += "-2"
        seen_ids.add(tail_id)
        result["tails"].append({"id": tail_id, "text": text_t.strip(),
                                "key": key})

    autos = data.get("autos")
    if not isinstance(autos, list):
        raise ValueError("autos не список")
    for auto in autos[:50]:
        if not isinstance(auto, dict):
            continue
        key = auto.get("key")
        if not isinstance(key, str) or key not in auto_keys:
            continue
        text_a = auto.get("text")
        if not isinstance(text_a, str) or not text_a.strip():
            continue
        project = auto.get("project")
        result["autos"].append({"key": key,
                                "project": project.strip() if isinstance(project, str) else "",
                                "text": text_a.strip()})
    return result


def run_digest(job: Job, sessions: list[Session], days: int, model: str,
               titles: dict[str, str], summaries: dict, hidden, store,
               today: datetime.date | None = None) -> None:
    """Собрать сводку за `days` дней через `model` и сохранить её в store.

    Из переданного каталога отбираются сессии периода: ручные (без auto,
    temp, empty и скрытых) и автоматические запуски. Отмена не сохраняет
    новую сводку — прошлая остаётся как была.
    """
    agent, problem = runner.pick("digest", model)
    if agent is None:
        job.emit({"type": "error", "message": problem})
        return
    if days not in (3, 4, 5):
        job.emit({"type": "error",
                  "message": f"Неверный период: {days} (нужно 3, 4 или 5)"})
        return

    manual, autos = select_for_digest(sessions, days, hidden, today=today)
    digest_manual = [s for s in manual if not s.empty]
    excerpts = {}
    for s in digest_manual:
        excerpt = _excerpt(s)
        if excerpt:
            excerpts[s.key] = excerpt
    prompt = short_prompt(days)
    limit = timeout()

    with tempfile.TemporaryDirectory(prefix="ai-threads-digest-") as workdir:
        journals = {}
        for s in digest_manual:
            if not get(s.tool).plain_journal(s):
                folder = Path(workdir, EXPORT_DIR)
                folder.mkdir(exist_ok=True)
                journals[s.key] = export_transcript(s, folder / (s.key.replace(":", "-") + ".txt"))
        listing = build_prompt(digest_manual, autos, days, titles, summaries, excerpts, journals)
        Path(workdir, INPUT_FILE).write_text(listing, encoding="utf-8")
        add_dirs = sorted({str(Path(s.journal).parent) for s in digest_manual
                           if s.journal and s.key not in journals})
        tracker = _Tracker(job, manual, autos, summaries, excerpts, journals, agent.id)
        code, output, answer = runner.run(job, agent, "digest", prompt, add_dirs=add_dirs,
                                          workdir=Path(workdir), timeout=limit, on_event=tracker.apply)

    if job.cancelled:
        return
    if code != 0:
        job.emit({"type": "error", "message": runner.failure(agent, code, limit), "code": code, "output": output})
        return
    answer = answer.strip()
    if not answer:
        job.emit({"type": "error", "message": f"{agent.name} вернул пустой ответ", "code": code, "output": output})
        return
    try:
        parsed = parse_digest(answer, {s.key for s in digest_manual}, {s.key for s in autos})
    except (ValueError, KeyError, TypeError):
        job.emit({"type": "error", "message": f"{agent.name} вернул неверный ответ. Попробуй ещё раз.",
                  "code": code, "output": output})
        return

    store.set_digest(days, {"at": time.time(), "model": agent.id, "result": parsed})
    job.emit({"type": "result", "result": parsed})
