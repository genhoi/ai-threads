"""Общая логика заглушек CLI (kimi, claude, codex, grok).

Поведение задаётся переменной окружения AI_THREADS_STUB_MODE:
ok — успешный прогон с чтением журналов и ответом;
error — код 2 и текст в stderr;
hang — висит до убийства;
garbage — неверный ответ (не JSON);
slow — успех через 3 секунды, события печатаются по ходу.
"""

from __future__ import annotations

import json
import os
import re
import sys
import time

JOURNAL_RE = re.compile(r"(?:Путь к журналу|журнал):\s*(/\S+)")

SUMMARY_ANSWER = {
    "title": "Тест. Сводка по сессии",
    "summary": "Суть задачи и подтверждённый результат из журнала.",
    "next_step": "Проверить результат вручную",
    "closed": True,
    "related": [
        {"key": "kimi:aaa", "why": "та же тема"},
        {"key": "kimi:bbb", "why": "похожий запрос, очень длинное пояснение, "
                                  "которое точно длиннее сорока символов"},
        {"key": "kimi:ccc", "why": "третья связь"},
        {"key": "kimi:ddd", "why": "четвёртая связь"},
        {"key": "ghost:x", "why": "ключа нет в списке"},
    ],
}

DIGEST_NOTES = ["ЗАМЕТКА proj-a: первая находка по проекту A",
                "ЗАМЕТКА proj-b: вторая находка по проекту B"]

DIGEST_ANSWER = {
    "lead": "Главное за период. Второе предложение.",
    "projects": [
        {"name": "proj-a",
         "bullets": [{"text": "Сделано важное в проекте A",
                      "keys": ["kimi:aaa", "ghost:x"]}]},
        {"name": "proj-b",
         "bullets": [{"text": "Ещё работа в проекте B", "keys": ["kimi:bbb"]}]},
    ],
    "tails": [
        {"id": "t1", "text": "Доделать следом", "key": "claude:bbb"},
        {"id": "t1", "text": "Ещё один хвост", "key": "ghost:x"},
    ],
    "autos": [
        {"key": "codex:auto1", "project": "proj-a",
         "text": "Важное замечание из автоматического ревью"},
        {"key": "grok:nope", "project": "x", "text": "чужой ключ"},
    ],
}


def mode() -> str:
    return os.environ.get("AI_THREADS_STUB_MODE", "ok")


def out(line: dict) -> None:
    print(json.dumps(line, ensure_ascii=False), flush=True)


def journals_from(prompt: str) -> list[str]:
    """Пути журналов из текста запроса (для событий чтения)."""
    seen, paths = set(), []
    for match in JOURNAL_RE.finditer(prompt):
        path = match.group(1)
        if path not in seen:
            seen.add(path)
            paths.append(path)
    return paths


def digest_sources(prompt: str) -> str:
    """Текст запроса плюс файл списка сессий: сводка передаёт его модели
    через digest-input.txt в рабочей папке, а не через argv."""
    path = os.path.join(os.getcwd(), "digest-input.txt")
    if os.path.isfile(path):
        with open(path, encoding="utf-8") as fh:
            return prompt + "\n" + fh.read()
    return prompt


def is_digest(prompt: str) -> bool:
    """Сводка за период начинается с «Составь сводку за»."""
    return "Составь сводку за" in prompt


def answer_json(digest: bool) -> str:
    data = DIGEST_ANSWER if digest else SUMMARY_ANSWER
    return json.dumps(data, ensure_ascii=False)


def search_answer(prompt: str) -> str | None:
    """Ответ на шаги умного поиска. Слова для поиска задаёт AI_THREADS_STUB_TERMS
    (через запятую), отбор возвращает первые два ключа кандидатов в обратном порядке."""
    if "Разбери запрос поиска" in prompt:
        terms = [t for t in os.environ.get("AI_THREADS_STUB_TERMS", "каталог").split(",") if t]
        agents = [a for a in os.environ.get("AI_THREADS_STUB_AGENTS", "").split(",") if a]
        return "```json\n" + json.dumps({"terms": terms, "agents": agents, "projects": [], "days": None},
                                           ensure_ascii=False) + "\n```"
    if "Отбери сессии по запросу" in prompt:
        keys = re.findall(r"^- ключ: (\S+)$", prompt, flags=re.M)[:2][::-1]
        return json.dumps({"results": [{"key": k, "why": f"нашлось: {k}"} for k in keys]}, ensure_ascii=False)
    return None


def answer_text(digest: bool, garbage: bool, prompt: str = "") -> str:
    if garbage:
        return "не удалось разобрать журнал, попробуйте позже"
    search = search_answer(prompt)
    if search is not None:
        return search
    lines = []
    if digest:
        lines.extend(DIGEST_NOTES)
        lines.append("```json")
        lines.append(answer_json(True))
        lines.append("```")
    else:
        lines.append("```json")
        lines.append(answer_json(False))
        lines.append("```")
    return "\n".join(lines)


def fail_or_hang() -> bool:
    """Обработать режимы error и hang. True — дальше не идём."""
    current = mode()
    if current == "error":
        sys.stderr.write("boom: тестовая ошибка заглушки\n")
        sys.stderr.flush()
        sys.exit(2)
    if current == "hang":
        while True:
            time.sleep(3600)
    return current == "slow"


def pause_if_slow() -> None:
    if mode() == "slow":
        time.sleep(3)
