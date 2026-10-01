"""Язык сообщений, которые видит человек: русский или английский.

Язык задаёт настройка `language` (`ru`, `en`); при `auto` — язык браузера из заголовка
Accept-Language, а если там нет ни русского, ни английского — английский. Без запроса (сообщения
в терминале) — переменные окружения LC_ALL, LC_MESSAGES и LANG. Сервер выбирает язык на каждый запрос; фоновое задание наследует язык запроса, который
его запустил (см. jobs.Jobs.start). Запросы к моделям остаются на русском: язык ответа модели
задаёт отдельная строка (runner.language_line).
"""

import contextvars
import os

LANGS = ("ru", "en")
_current: contextvars.ContextVar = contextvars.ContextVar("language", default=None)


def from_header(header) -> str | None:
    """Язык из Accept-Language: первый по весу из поддерживаемых."""
    if not isinstance(header, str):
        return None
    best, weight = None, -1.0
    for index, part in enumerate(header.split(",")):
        tag, _, params = part.strip().partition(";")
        lang = tag.strip().lower().split("-")[0]
        if lang not in LANGS:
            continue
        q = 1.0
        for param in params.split(";"):
            name, _, value = param.strip().partition("=")
            if name == "q":
                try:
                    q = float(value)
                except ValueError:
                    q = 0.0
        if q > weight:
            best, weight = lang, q
    return best


def from_env() -> str:
    for name in ("LC_ALL", "LC_MESSAGES", "LANG"):
        value = os.environ.get(name, "")
        if value:
            return "ru" if value.lower().startswith("ru") else "en"
    return "en"


def resolve(accept_language=None, *, request: bool = False) -> str:
    """Язык по настройке; при `auto` — у запроса браузера по Accept-Language, а если там нет
    ни русского, ни английского — английский. Без запроса (сообщения в терминале) — по окружению."""
    from . import settings
    chosen = settings.effective()["language"]
    if chosen in LANGS:
        return chosen
    if request:
        return from_header(accept_language) or "en"
    return from_env()


def use(lang: str) -> None:
    _current.set(lang if lang in LANGS else None)


def current() -> str:
    return _current.get() or resolve()


def plural_ru(n: int, one: str, few: str, many: str) -> str:
    n10, n100 = n % 10, n % 100
    if n10 == 1 and n100 != 11:
        return one
    if 2 <= n10 <= 4 and not 12 <= n100 <= 14:
        return few
    return many


def t(key: str, **params) -> str:
    """Сообщение по ключу на текущем языке. Нет перевода — русский текст, нет ключа — сам ключ."""
    entry = MESSAGES.get(key)
    if entry is None:
        return key
    text = entry.get(current()) or entry["ru"]
    return text(**params) if callable(text) else text.format(**params)


# Сообщения сервера. Ключ — «модуль.смысл». Значение — строка с {параметрами} или функция,
# если нужны формы множественного числа.
MESSAGES: dict[str, dict] = {}


def add(messages: dict) -> None:
    MESSAGES.update(messages)
