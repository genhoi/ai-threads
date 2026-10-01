"""Умный поиск по сессиям.

Агент разбирает запрос человека на слова для поиска, агентов, проекты и период. Приложение
ищет эти слова во всех журналах подходящих сессий — вместе с выводом команд и прочитанными
файлами — и оценивает совпадения: редкие слова весят больше частых. Лучших кандидатов с
фрагментами переписки агент отбирает и объясняет, чем каждая сессия отвечает запросу.

Если агент не разобрал запрос, поиск идёт по словам самого запроса. Если не удался отбор,
показываются кандидаты по оценке.
"""

from __future__ import annotations

import datetime
import json
import math
import re
import tempfile
import time
from pathlib import Path

from . import runner, settings
from .agents import AGENTS, get
from .jobs import Job
from .model import Session
from .sources.common import SEARCH_BYTES, plain_line
from .summary import loads_answer

QUERY_MAX = 500
MAX_TERMS = 12
CANDIDATES = 30
RESULTS = 15
SNIPPETS = 3
SNIPPET_CHARS = 220
WHY_MAX = 160
#: Предел запроса на отбор: у CLI ограничена длина аргумента (у Kimi — 128 КБ).
PROMPT_MAX = 60_000

STEP_PLAN = "понимает запрос"
STEP_SCAN = "ищет в журналах"
STEP_RANK = "отбирает сессии"

# Слова, которые не помогают искать: их нет смысла искать в журналах.
_STOP = {"найди", "найти", "покажи", "сессии", "сессию", "сессия", "сессий", "где", "как", "что", "про",
         "для", "это", "был", "были", "было", "все", "мои", "мой", "которые", "который", "когда",
         "find", "show", "session", "sessions", "where", "what", "about", "with", "from", "that"}


def timeout() -> float:
    return settings.timeout("search")


class _Progress:
    def __init__(self, job: Job, agent: str):
        self.job, self.agent = job, agent

    def emit(self, step: int, text: str) -> None:
        self.job.emit({"type": "progress", "step": step, "steps": 3, "text": text, "agent": self.agent})


def plan_prompt(query: str, projects: list[str]) -> str:
    lines = ["Разбери запрос поиска по инструкции агента.", f"Запрос: {query}",
             f"Сегодня: {datetime.date.today().isoformat()}", "Агенты (id — название):"]
    lines += [f"- {agent.id} — {agent.name}" for agent in AGENTS.values()]
    lines.append("Проекты: " + ", ".join(projects[:300]))
    return "\n".join(lines)


def parse_plan(text: str, projects: list[str]) -> dict:
    data = loads_answer(text)
    terms = data.get("terms")
    if not isinstance(terms, list):
        raise ValueError("нет списка terms")
    clean = _dedupe(t.strip() for t in terms if isinstance(t, str) and 2 <= len(t.strip()) <= 80)[:MAX_TERMS]
    if not clean:
        raise ValueError("пустой список terms")
    agents = data.get("agents") if isinstance(data.get("agents"), list) else []
    by_name = {p.lower(): p for p in projects}
    wanted = data.get("projects") if isinstance(data.get("projects"), list) else []
    days = data.get("days")
    return {"terms": clean,
            "agents": list(dict.fromkeys(a for a in agents if a in AGENTS)),
            "projects": list(dict.fromkeys(by_name[p.lower()] for p in wanted
                                           if isinstance(p, str) and p.lower() in by_name)),
            "days": days if isinstance(days, int) and not isinstance(days, bool) and 1 <= days <= 3650 else None}


def fallback_plan(query: str) -> dict:
    """Слова запроса без служебных: если агент не разобрал запрос."""
    words = re.findall(r"[\w.-]{3,}", query.lower())
    terms = _dedupe(w.strip(".-") for w in words if w.strip(".-") not in _STOP and len(w.strip(".-")) >= 3)
    return {"terms": terms[:MAX_TERMS], "agents": [], "projects": [], "days": None}


def _dedupe(values) -> list[str]:
    seen, out = set(), []
    for value in values:
        if value and value.lower() not in seen:
            seen.add(value.lower())
            out.append(value)
    return out


def needles(term: str) -> list[bytes]:
    """Как слово выглядит в журнале, который приведён к нижнему регистру через bytes.lower().

    bytes.lower() меняет регистр только у латиницы, поэтому у слов с кириллицей ищутся
    написания «база данных», «База данных», «База Данных», «БАЗА ДАННЫХ» — как есть и в
    экранированном виде JSON (\\u0441). Смешанный регистр вроде «бАза» не найдётся.
    """
    low = term.lower()
    if low.isascii():
        return [low.encode("utf-8")]
    variants = []
    for form in dict.fromkeys((low, low[:1].upper() + low[1:], low.title(), low.upper())):
        variants.append(form.encode("utf-8"))
        variants.append(json.dumps(form)[1:-1].lower().encode("ascii"))
    return list(dict.fromkeys(variants))


def select(sessions: list[Session], plan: dict, scope: str, hidden) -> list[Session]:
    hidden = set(hidden or ())
    since = time.time() - plan["days"] * 86400 if plan["days"] else None
    projects = {p.lower() for p in plan["projects"]}
    pool = []
    for s in sessions:
        if s.empty or (scope == "mine" and (s.auto or s.temp or s.key in hidden)):
            continue
        if plan["agents"] and s.tool not in plan["agents"]:
            continue
        if projects and s.project.lower() not in projects:
            continue
        if since and s.updated < since:
            continue
        pool.append(s)
    return pool


def score(job: Job, pool: list[Session], terms: list[str], names: dict, summaries: dict, on_progress) -> list[tuple]:
    """(оценка, сессия, совпавшие слова) по убыванию оценки, только с совпадениями."""
    found: list[tuple[Session, dict, set]] = []
    variants = {term: needles(term) for term in terms}
    last = 0.0
    for index, session in enumerate(pool):
        if job.cancelled:
            return []
        if time.monotonic() - last > 0.4:
            last = time.monotonic()
            on_progress(index, len(pool))
        data = get(session.tool).raw_text(session).lower()
        summary = summaries.get(session.key) or {}
        meta = " ".join(str(v) for v in (session.title, names.get(session.key, ""), summary.get("title", ""),
                                          summary.get("summary", ""), session.project, session.branch)).lower()
        counts = {term: sum(data.count(v) for v in variants[term]) for term in terms}
        in_meta = {term for term in terms if term.lower() in meta}
        if any(counts.values()) or in_meta:
            found.append((session, counts, in_meta))
    on_progress(len(pool), len(pool))
    total = max(1, len(pool))
    freq = {term: sum(1 for _, counts, meta in found if counts[term] or term in meta) for term in terms}
    ranked = []
    for session, counts, in_meta in found:
        value = 0.0
        for term in terms:
            if not freq[term]:
                continue
            weight = math.log(1 + total / freq[term])
            if counts[term]:
                value += weight * (1 + math.log(counts[term]))
            if term in in_meta:
                value += 2 * weight
        matched = [term for term in terms if counts[term] or term in in_meta]
        ranked.append((round(value, 3), session, matched))
    ranked.sort(key=lambda item: (item[0], item[1].updated), reverse=True)
    return ranked


def snippets(session: Session, terms: list[str]) -> list[str]:
    """Фрагменты видимой переписки с совпадениями. Вывод команд и прочитанные файлы модели
    не отправляются: там бывают токены и пароли. По ним приложение только ищет."""
    lowered = [t.lower() for t in terms]
    found = []
    try:
        messages, _ = get(session.tool).transcript(session, SEARCH_BYTES * 4, text_limit=None)
    except (OSError, ValueError):
        messages = []
    for item in messages:
        text = item["text"]
        low = text.lower()
        hits = [low.find(t) for t in lowered if t in low]
        if hits:
            at = min(hits)
            found.append(("я" if item["role"] == "user" else "агент") + ": "
                         + plain_line(text[max(0, at - 80):at + SNIPPET_CHARS - 80], SNIPPET_CHARS))
            if len(found) >= SNIPPETS:
                return found
    return found


def rank_prompt(query: str, entries: list[dict]) -> str:
    head = ["Отбери сессии по запросу по инструкции агента.", f"Запрос: {query}", runner.language_line(), "",
            "Кандидаты:"]
    text = "\n".join(head)
    for entry in entries:
        block = [f"- ключ: {entry['key']}", f"  агент: {entry['agent']}", f"  проект: {entry['project']}",
                 f"  дата: {entry['date']}", f"  название: {entry['title']}"]
        if entry["summary"]:
            block.append(f"  сводка: {entry['summary']}")
        if entry["first"]:
            block.append(f"  первый запрос: {entry['first']}")
        block.append(f"  совпали слова: {', '.join(entry['matched'])}")
        block += [f"  фрагмент: {snippet}" for snippet in entry["snippets"]]
        if not entry["snippets"]:
            block.append("  фрагменты: слова нашлись только в выводе команд и прочитанных файлах")
        chunk = "\n" + "\n".join(block)
        if len(text) + len(chunk) > PROMPT_MAX:
            break
        text += chunk
    return text


def parse_rank(text: str, keys: set[str]) -> list[dict]:
    data = loads_answer(text)
    results = data.get("results")
    if not isinstance(results, list):
        raise ValueError("нет списка results")
    out, seen = [], set()
    for item in results:
        if not isinstance(item, dict) or item.get("key") not in keys or item["key"] in seen:
            continue
        seen.add(item["key"])
        why = item.get("why", "")
        out.append({"key": item["key"], "why": (why if isinstance(why, str) else str(why)).strip()[:WHY_MAX]})
        if len(out) >= RESULTS:
            break
    return out


def _entry(session: Session, matched: list[str], names: dict, summaries: dict) -> dict:
    summary = summaries.get(session.key) or {}
    first = ""
    try:
        messages = get(session.tool).excerpt_messages(session)
        first = next((plain_line(m["text"], 300) for m in messages if m["role"] == "user"), "")
    except (OSError, ValueError):
        pass
    return {"key": session.key, "agent": get(session.tool).name, "project": session.project,
            "date": datetime.date.fromtimestamp(session.updated).isoformat(),
            "title": names.get(session.key) or session.title,
            "summary": plain_line(str(summary.get("summary", "")), 400),
            "first": first, "matched": matched, "snippets": snippets(session, matched)}


def run_search(job: Job, sessions: list[Session], query: str, scope: str, hidden, names: dict,
               summaries: dict, agent_id: str | None = None) -> None:
    """Найти сессии по запросу и опубликовать результат событием result."""
    query = " ".join(query.split())[:QUERY_MAX]
    agent, problem = runner.pick("search", agent_id)
    if agent is None:
        job.emit({"type": "error", "message": problem})
        return
    progress = _Progress(job, agent.id)
    # Модели — только проекты из области поиска: без скрытых, временных и автоматических в «Моих».
    everything = {"terms": [], "agents": [], "projects": [], "days": None}
    projects = sorted({s.project for s in select(sessions, everything, scope, hidden)})
    limit = timeout()
    with tempfile.TemporaryDirectory(prefix="ai-threads-search-") as workdir:
        progress.emit(1, STEP_PLAN)
        code, output, answer = runner.run(job, agent, "search-plan", plan_prompt(query, projects), add_dirs=[],
                                          workdir=Path(workdir), timeout=limit, on_event=lambda event: None)
        if job.cancelled:
            return
        try:
            if code != 0:
                raise ValueError(runner.failure(agent, code, limit))
            plan = parse_plan(answer, projects)
        except (ValueError, KeyError, TypeError) as error:
            plan = fallback_plan(query)
            job.emit({"type": "warning", "message": f"{agent.name} не разобрал запрос ({error}). Ищу по словам запроса.",
                      "output": output})
        job.emit({"type": "plan", **plan})
        if not plan["terms"]:
            job.emit({"type": "result", "result": {"query": query, "plan": plan, "results": [], "ranked": False,
                                                   "scanned": 0}})
            return

        pool = select(sessions, plan, scope, hidden)
        ranked = score(job, pool, plan["terms"], names, summaries,
                       lambda done, total: progress.emit(2, f"{STEP_SCAN}: {done} из {total}"))
        if job.cancelled:
            return
        top = ranked[:CANDIDATES]
        result = {"query": query, "plan": plan, "results": [], "ranked": False, "scanned": len(pool)}
        if not top:
            job.emit({"type": "result", "result": result})
            return

        progress.emit(3, STEP_RANK)
        entries = [_entry(session, matched, names, summaries) for _, session, matched in top]
        scores = {session.key: value for value, session, _ in top}
        code, output, answer = runner.run(job, agent, "search-rank", rank_prompt(query, entries), add_dirs=[],
                                          workdir=Path(workdir), timeout=limit, on_event=lambda event: None)
        if job.cancelled:
            return
        try:
            if code != 0:
                raise ValueError(runner.failure(agent, code, limit))
            chosen = parse_rank(answer, set(scores))
            result["ranked"] = True
        except (ValueError, KeyError, TypeError) as error:
            job.emit({"type": "warning", "message": f"{agent.name} не отобрал сессии ({error}). "
                                                    "Показываю совпадения по словам.", "output": output})
            chosen = [{"key": e["key"], "why": (e["snippets"][0] if e["snippets"] else
                                                "совпали слова: " + ", ".join(e["matched"]))[:WHY_MAX]}
                      for e in entries[:RESULTS]]
        result["results"] = [{**item, "score": scores[item["key"]]} for item in chosen]
        job.emit({"type": "result", "result": result})
