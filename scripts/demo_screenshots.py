"""Выдуманные сессии агентов и снимки работающей «Нити» на них — для README.

Запуск из корня репозитория после ./scripts/setup.sh:

    .venv/bin/python scripts/demo_screenshots.py [папка]

По умолчанию снимки пишутся в docs/images. Журналы, настройки и данные приложения
создаются во временной папке; настоящие журналы агентов не читаются.
"""

import json
import os
import random
import shutil
import sys
import tempfile
import threading
import time
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
OUT = Path(sys.argv[1] if len(sys.argv) > 1 else REPO / "docs" / "images").resolve()
DEMO = Path(tempfile.mkdtemp(prefix="ai-threads-demo-"))
# Своя домашняя папка: на снимках обычные пути вроде ~/.codex, а программы агентов —
# заглушки в ~/.local/bin, чтобы на экране настроек агенты были «найдены».
HOME = DEMO / "home"
HOMES = {"codex": HOME / ".codex", "claude": HOME / ".claude", "grok": HOME / ".grok", "kimi": HOME / ".kimi-code"}
DATA = HOME / ".local" / "share" / "ai-threads"
NOW = datetime.now().astimezone()
TZ = NOW.tzinfo
TODAY = NOW.replace(hour=0, minute=0, second=0, microsecond=0)
BASE = "/home/dev/projects"
rng = random.Random(7)


def at(days_ago, hh, mm):
    return TODAY - timedelta(days=days_ago) + timedelta(hours=hh, minutes=mm)


def iso(dt):
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")


def ms(dt):
    return int(dt.timestamp() * 1000)


def new_id():
    return str(uuid.UUID(int=rng.getrandbits(128), version=4))


# (ключ для ссылок, инструмент, проект, ветка, название, [(роль, (дни назад, ч, м), текст)], автозапуск)
S = [
    ("A", "codex", "shop-api", "feature/search-index", "Ускорить поиск по каталогу товаров", [
        ("user", (0, 7, 55), "Посмотри, какие запросы к базе в shop-api самые медленные за последние сутки."),
        ("assistant", (0, 8, 10), "По pg_stat_statements лидирует поиск по каталогу: в среднем 2,1 с, 38 тыс. вызовов за сутки. Следом отчёт по остаткам — 900 мс, но он вызывается редко."),
        ("user", (0, 8, 20), "Поиск по каталогу отвечает 2–3 секунды на запросах вроде «кроссовки белые». Найди, что тормозит, и предложи исправление."),
        ("assistant", (0, 8, 31), "Посмотрел EXPLAIN ANALYZE запроса из internal/search/repository.go. Postgres читает таблицу products целиком — 1,2 млн строк: условие ILIKE '%…%' обычным индексом не ускоряется.\n\nПредлагаю триграммный индекс pg_trgm по name и description и сортировку по similarity(). Запрос станет чуть сложнее, но ответ API не меняется."),
        ("user", (0, 8, 40), "Делай. Миграцию отдельным файлом, индекс создавай без блокировки таблицы."),
        ("assistant", (0, 9, 12), "Готово. Добавил migrations/0042_products_trgm.sql с CREATE INDEX CONCURRENTLY и поправил запрос в repository.go. На копии базы p95 поиска упал с 2,4 с до 85 мс, тесты пакета search проходят."),
        ("user", (0, 9, 35), "Отлично. Заведи задачу прогнать миграцию на стейдже."),
        ("assistant", (0, 9, 41), "Завёл SHOP-128 «Прогнать миграцию 0042 на стейдже» и приложил замеры до и после. Что-то ещё сейчас?"),
    ], False),
    ("B", "claude", "shop-api", "feature/promo-codes", "Промокоды в корзине: ревью MR", [
        ("user", (0, 9, 5), "Посмотри MR с промокодами в корзине. Особенно расчёт скидки и сроки действия кодов."),
        ("assistant", (0, 9, 50), "Нашёл две проблемы.\n\n1. Скидка считается до округления цены позиции, поэтому на заказах из трёх и более товаров итог расходится с чеком на копейку.\n2. При повторном открытии корзины срок действия промокода не проверяется: истёкший код продолжает работать.\n\nОстальное в порядке. Оставить комментарии в MR?"),
    ], False),
    ("X1", "codex", "shop-api", "feature/search-index", "Ревью миграции 0042", [
        ("user", (0, 9, 20), "Проверь миграцию migrations/0042_products_trgm.sql: блокировки, откат, время на большой таблице."),
        ("assistant", (0, 9, 26), "Замечаний нет: индекс создаётся без блокировки, откат удаляет его, на 1,2 млн строк создание заняло 40 с."),
    ], True),
    ("C", "kimi", "landing", "main", "Перевести лендинг на Astro", [
        ("user", (0, 8, 5), "Перенеси лендинг с Gatsby на Astro, сохрани текущие адреса страниц."),
        ("assistant", (0, 8, 50), "Перенёс 12 страниц и общий макет. Адреса совпадают, редиректы не нужны. Сборка теперь 14 с вместо 48. Осталась форма подписки — она зависит от плагина Gatsby."),
    ], False),
    ("D", "grok", "infra", "main", "Алерты на заполнение диска", [
        ("user", (1, 22, 50), "Добавь алерт в Prometheus, когда диск на нодах заполнен больше чем на 85%."),
        ("assistant", (1, 23, 12), "Добавил правило NodeDiskAlmostFull в alerts/node.yml: срабатывает, если свободно меньше 15% дольше 10 минут. Проверил через promtool — правило проходит."),
    ], False),
    ("E", "claude", "mobile-app", "release/2.3", "Падение на Android 14 при открытии камеры", [
        ("user", (1, 21, 30), "Крэш на Android 14 при открытии камеры в профиле. Стек в crash.txt."),
        ("assistant", (1, 22, 14), "Причина — частичный доступ к фото на Android 14: пользователь выбирает несколько снимков, а код ждёт доступ ко всей галерее и падает на пустом списке. Добавил проверку и запрос частичного доступа. Нужно проверить на устройстве."),
    ], False),
    ("X2", "claude", "mobile-app", "release/2.3", "Совет: разрешения Android 14", [
        ("user", (1, 22, 30), "Как правильно запрашивать частичный доступ к фото на Android 14? Коротко."),
        ("assistant", (1, 22, 40), "Запрашивать READ_MEDIA_VISUAL_USER_SELECTED вместе с READ_MEDIA_IMAGES и обрабатывать случай, когда выбрана часть фото."),
    ], True),
    ("F", "codex", "data-pipeline", "feat/dedup", "Дедупликация событий в ClickHouse", [
        ("user", (1, 18, 30), "В таблице events дубли после повторной отправки из мобильного клиента. Как убрать?"),
        ("assistant", (1, 19, 2), "Перевёл таблицу на ReplacingMergeTree с ключом (user_id, event_id) и добавил FINAL в отчёты, где важна точность. Старые данные переносит разовый скрипт scripts/dedup_events.sql."),
    ], False),
    ("G", "kimi", "infra", "terraform-modules", "Разбить terraform на модули", [
        ("user", (1, 15, 50), "Разбей корневой terraform на модули: сеть, база, кластер."),
        ("assistant", (1, 16, 40), "Вынес сеть, базу и кластер в modules/network, modules/postgres и modules/k8s. terraform plan показывает 0 изменений: состояние перенесено блоками moved."),
    ], False),
    ("H", "grok", "notes-bot", "main", "Команда /remind для бота заметок", [
        ("user", (1, 14, 30), "Добавь в бота команду /remind: «/remind завтра в 9 купить корм»."),
        ("assistant", (1, 15, 10), "Добавил /remind с разбором времени вроде «завтра в 9» и очередь напоминаний в SQLite. Тесты на разбор дат проходят."),
    ], False),
    ("I", "claude", "landing", "feature/dark-theme", "Тёмная тема для лендинга", [
        ("user", (2, 17, 40), "Сделай тёмную тему по системной настройке, без переключателя."),
        ("assistant", (2, 18, 20), "Перевёл цвета на CSS-переменные и добавил prefers-color-scheme. Контраст текста проверил: везде не ниже 4.5:1."),
    ], False),
    ("J", "codex", "mobile-app", "feature/offline", "Офлайн-режим: очередь запросов", [
        ("user", (2, 11, 20), "Без сети заказы теряются. Нужна очередь запросов, которая отправится при появлении сети."),
        ("assistant", (2, 12, 0), "Добавил очередь в локальной базе: запросы сохраняются и уходят по порядку, когда сеть вернулась. Повтор одного и того же заказа сервер отклоняет по ключу запроса."),
    ], False),
    ("K", "kimi", "data-pipeline", "main", "Kafka или Redpanda для потока событий", [
        ("user", (3, 16, 50), "Сравни Kafka и Redpanda для потока событий на 5 тыс. сообщений в секунду."),
        ("assistant", (3, 17, 30), "Для такого потока подходят обе. Redpanda проще в эксплуатации: один бинарник без ZooKeeper, клиенты Kafka работают без изменений. Предлагаю Redpanda."),
    ], False),
    ("L", "claude", "infra", "main", "Проверить восстановление базы из бэкапа", [
        ("user", (3, 10, 20), "Проверь, что ночной бэкап базы реально восстанавливается."),
        ("assistant", (3, 11, 0), "Восстановил вчерашний бэкап на отдельной машине: 18 минут, данные сходятся с продом на момент снятия."),
    ], False),
    ("M", "grok", "shop-api", "master", "Обновить Go до 1.25", [
        ("user", (4, 15, 30), "Обнови проект до Go 1.25."),
        ("assistant", (4, 16, 0), "Обновил go.mod и образ сборки. Сборка и тесты проходят, предупреждений линтера нет."),
    ], False),
    ("N", "codex", "notes-bot", "main", "Хранение заметок в SQLite", [
        ("user", (4, 13, 20), "Перенеси заметки из JSON-файла в SQLite."),
        ("assistant", (4, 14, 0), "Перенёс: таблицы notes и tags, разовый перенос старого файла при запуске. Поиск по тексту — через FTS5."),
    ], False),
]


def write_jsonl(path, rows):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows), encoding="utf-8")


def build():
    keys, codex_index, claude_history, kimi_history = {}, [], [], []
    for ref, tool, project, branch, title, msgs, auto in S:
        sid = new_id()
        cwd = f"{BASE}/{project}"
        times = [at(*m[1]) for m in msgs]
        start, end = times[0], times[-1]
        if tool == "codex":
            home = HOMES["codex"]
            name = f"rollout-{start.strftime('%Y-%m-%dT%H-%M-%S')}-{sid}.jsonl"
            rows = [{"timestamp": iso(start), "type": "session_meta",
                     "payload": {"id": sid, "timestamp": iso(start), "cwd": cwd,
                                 "source": "exec" if auto else "cli", "git": {"branch": branch}}}]
            for (role, _, text), dt in zip(msgs, times):
                kind = "input_text" if role == "user" else "output_text"
                rows.append({"timestamp": iso(dt), "type": "response_item",
                             "payload": {"type": "message", "role": role, "content": [{"type": kind, "text": text}]}})
            write_jsonl(home / "sessions" / start.strftime("%Y/%m/%d") / name, rows)
            codex_index.append({"id": sid, "thread_name": title})
            keys[ref] = f"codex:{sid}"
        elif tool == "claude":
            home = HOMES["claude"]
            rows = []
            for (role, _, text), dt in zip(msgs, times):
                content = text if role == "user" else [{"type": "text", "text": text}]
                rows.append({"type": role, "isSidechain": False, "timestamp": iso(dt),
                             "entrypoint": "sdk-cli" if auto else "cli", "cwd": cwd, "sessionId": sid,
                             "gitBranch": branch, "message": {"role": role, "content": content}})
            rows.append({"type": "ai-title", "aiTitle": title, "sessionId": sid})
            write_jsonl(home / "projects" / cwd.replace("/", "-") / f"{sid}.jsonl", rows)
            claude_history.append({"sessionId": sid, "display": msgs[0][2], "project": cwd})
            keys[ref] = f"claude:{sid}"
        elif tool == "grok":
            folder = HOMES["grok"] / "sessions" / sid
            folder.mkdir(parents=True, exist_ok=True)
            summary = {"info": {"id": sid, "cwd": cwd}, "generated_title": title, "session_summary": title,
                       "created_at": iso(start), "updated_at": iso(end), "last_active_at": iso(end),
                       "head_branch": branch, "chat_format_version": 1}
            if auto:
                summary["session_kind"] = "headless"
            (folder / "summary.json").write_text(json.dumps(summary, ensure_ascii=False), encoding="utf-8")
            write_jsonl(folder / "chat_history.jsonl",
                        [{"type": role, "content": [{"type": "text", "text": text}] if role == "user" else text}
                         for role, _, text in msgs])
            keys[ref] = f"grok:{sid}"
        else:
            folder = HOMES["kimi"] / "sessions" / "demo" / f"session_{sid}"
            folder.mkdir(parents=True, exist_ok=True)
            state = {"id": f"session_{sid}", "version": 2, "cwd": cwd, "title": title,
                     "createdAt": ms(start), "updatedAt": ms(end)}
            (folder / "state.json").write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")
            rows = []
            for (role, _, text), dt in zip(msgs, times):
                if role == "user":
                    rows.append({"type": "turn.prompt", "agentId": "main",
                                 "input": [{"type": "text", "text": text}], "time": ms(dt)})
                    if not auto:
                        kimi_history.append({"content": text})
                rows.append({"type": "agent.message.appended", "time": ms(dt), "message": {
                    "message": {"role": role, "content": [{"type": "text", "text": text}]},
                    "meta": {"createdAt": iso(dt), "source": role}}})
            write_jsonl(folder / "agents" / "main" / "wire.jsonl", rows)
            keys[ref] = f"kimi:session_{sid}"
        # Время изменения файлов — как у настоящих журналов.
        for path in HOMES[tool].rglob("*"):
            if path.is_file() and sid in str(path):
                os.utime(path, (end.timestamp(), end.timestamp()))
    write_jsonl(HOMES["codex"] / "session_index.jsonl", codex_index)
    write_jsonl(HOMES["claude"] / "history.jsonl", claude_history)
    write_jsonl(HOMES["kimi"] / "user-history" / "history.jsonl", kimi_history)
    (HOMES["grok"] / "active_sessions.json").write_text("[]", encoding="utf-8")

    # Две сессии Claude Code открыты и ждут ответа: реестр указывает на живой процесс этого скрипта.
    live = HOMES["claude"] / "sessions"
    live.mkdir(parents=True, exist_ok=True)
    for i, (ref, minutes) in enumerate((("B", 6), ("E", 11 * 60))):
        record = {"sessionId": keys[ref].split(":", 1)[1], "pid": os.getpid(), "status": "waiting",
                  "kind": "interactive", "statusUpdatedAt": ms(NOW - timedelta(minutes=minutes))}
        (live / f"{90000 + i}.json").write_text(json.dumps(record), encoding="utf-8")

    summary_at = (NOW - timedelta(minutes=12)).timestamp()
    state = {
        "version": 2, "pins": [keys["A"], keys["C"]], "names": {}, "done": [keys["D"]], "hidden": [],
        "migrated_local_storage": True, "digest_tails": {"t4": True},
        "summaries": {
            keys["A"]: {"title": "Поиск по каталогу: триграммный индекс вместо полного прохода",
                        "summary": "Поиск по каталогу отвечал 2–3 с: условие ILIKE читало всю таблицу products. Добавлен индекс pg_trgm по name и description (миграция 0042, без блокировки таблицы) и сортировка по похожести. На копии базы p95 упал с 2,4 с до 85 мс, тесты проходят. Заведена задача SHOP-128 на прогон миграции на стейдже.",
                        "next_step": "Прогнать миграцию 0042 на стейдже и сравнить p95 поиска с копией базы",
                        "closed": False, "related": [{"key": keys["M"], "why": "тот же сервис"}],
                        "at": summary_at, "model": "kimi"},
            keys["D"]: {"title": "Алерт NodeDiskAlmostFull для нод",
                        "summary": "Добавлено правило Prometheus: свободно меньше 15% дольше 10 минут. Проверено через promtool.",
                        "next_step": "", "closed": True, "related": [], "at": summary_at, "model": "kimi"},
            keys["E"]: {"title": "Android 14: падение камеры при частичном доступе к фото",
                        "summary": "Камера в профиле падала, когда пользователь давал доступ только к части фото. Добавлены проверка пустого списка и запрос частичного доступа. На устройстве ещё не проверено.",
                        "next_step": "Проверить исправление на устройстве с Android 14",
                        "closed": False, "related": [], "at": summary_at, "model": "kimi"},
        },
        "digests": {"5": {"at": (TODAY + timedelta(hours=7, minutes=30)).timestamp(), "model": "claude", "result": {
            "lead": "Основная работа шла в shop-api и mobile-app. В shop-api поиск по каталогу стал быстрее в 28 раз, а в MR с промокодами нашлись две ошибки. В mobile-app исправлено падение камеры на Android 14, в infra появились алерт на диск и модули terraform.",
            "projects": [
                {"name": "shop-api", "bullets": [
                    {"text": "Поиск по каталогу ускорен с 2,4 с до 85 мс: индекс pg_trgm, миграция 0042.", "keys": [keys["A"]]},
                    {"text": "В MR с промокодами две ошибки: копейка при округлении и истёкшие коды.", "keys": [keys["B"]]},
                    {"text": "Go обновлён до 1.25, сборка и тесты проходят.", "keys": [keys["M"]]}]},
                {"name": "mobile-app", "bullets": [
                    {"text": "Падение камеры на Android 14 исправлено, ждёт проверки на устройстве.", "keys": [keys["E"]]},
                    {"text": "Офлайн-режим: заказы копятся в очереди и уходят, когда вернулась сеть.", "keys": [keys["J"]]}]},
                {"name": "infra", "bullets": [
                    {"text": "Алерт NodeDiskAlmostFull при свободном месте меньше 15%.", "keys": [keys["D"]]},
                    {"text": "Terraform разбит на модули network, postgres и k8s без изменений в инфраструктуре.", "keys": [keys["G"]]},
                    {"text": "Восстановление базы из бэкапа проверено: 18 минут.", "keys": [keys["L"]]}]},
                {"name": "landing", "bullets": [
                    {"text": "Лендинг переведён на Astro, сборка 14 с вместо 48.", "keys": [keys["C"]]},
                    {"text": "Тёмная тема по системной настройке.", "keys": [keys["I"]]}]},
                {"name": "data-pipeline", "bullets": [
                    {"text": "Дубли событий убраны через ReplacingMergeTree.", "keys": [keys["F"]]},
                    {"text": "Для потока событий выбрана Redpanda: клиенты Kafka работают без изменений.", "keys": [keys["K"]]}]},
                {"name": "notes-bot", "bullets": [
                    {"text": "Команда /remind и хранение заметок в SQLite с поиском по тексту.", "keys": [keys["H"], keys["N"]]}]},
            ],
            "tails": [
                {"id": "t1", "text": "Прогнать миграцию 0042 на стейдже", "key": keys["A"]},
                {"id": "t2", "text": "Оставить комментарии в MR с промокодами", "key": keys["B"]},
                {"id": "t3", "text": "Проверить исправление камеры на устройстве с Android 14", "key": keys["E"]},
                {"id": "t4", "text": "Перенести форму подписки лендинга", "key": keys["C"]}],
            "autos": [{"key": keys["X1"], "project": "shop-api",
                       "text": "Ревью миграции 0042: замечаний нет, индекс создаётся без блокировки."}],
        }}},
    }
    DATA.mkdir(parents=True, exist_ok=True)
    (DATA / "state.json").write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")
    return keys


def shoot(keys):
    sys.path.insert(0, str(REPO))
    from ai_threads.agents import AGENTS
    bindir = HOME / ".local" / "bin"
    bindir.mkdir(parents=True, exist_ok=True)
    for name in ("codex", "claude", "grok", "kimi"):
        (bindir / name).write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        (bindir / name).chmod(0o755)
    # Браузеры Playwright лежат в настоящей домашней папке.
    os.environ.setdefault("PLAYWRIGHT_BROWSERS_PATH", str(Path.home() / ".cache" / "ms-playwright"))
    for agent in AGENTS.values():
        os.environ.pop(agent.home_env, None)
    os.environ.update(HOME=str(HOME), PATH=f"{bindir}:/usr/bin:/bin", WSL_DISTRO_NAME="Ubuntu")
    for name in ("AI_THREADS_DATA", "AI_THREADS_CONFIG", "XDG_CONFIG_HOME"):
        os.environ.pop(name, None)
    from ai_threads import model
    from ai_threads.catalog import Catalog
    from ai_threads.server import create_server
    from playwright.sync_api import sync_playwright

    # Папок /home/dev/projects/* здесь нет; на снимке они должны выглядеть существующими.
    model.Session.missing = property(lambda self: False)
    catalog = Catalog()
    assert catalog.wait(30)
    server = create_server(0, catalog=catalog)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_port}"

    # Страница открыта на привычном 127.0.0.1:8765, запросы уходят на демо-сервер.
    def proxy(route):
        real = route.request.url.replace("127.0.0.1:8765", f"127.0.0.1:{server.server_port}")
        response = route.fetch(url=real)
        if "/api/sessions" in real or "/api/config" in real:
            body = response.json()
            body["port"] = 8765
            route.fulfill(response=response, json=body)
        else:
            route.fulfill(response=response)

    with sync_playwright() as p:
        browser = p.chromium.launch()
        # Готовый результат умного поиска: страница хранит его в sessionStorage до закрытия.
        smart = {"q": "где я работал с базами данных и миграциями", "scope": "mine", "agent": "kimi", "warnings": [],
                 "result": {"query": "где я работал с базами данных и миграциями", "ranked": True, "scanned": 14,
                            "plan": {"terms": ["миграц", "migration", "индекс", "postgres", "clickhouse", "sqlite"],
                                     "agents": [], "projects": [], "days": None},
                            "results": [
                                {"key": keys["A"], "score": 9.4, "why": "Миграция 0042: индекс pg_trgm вместо полного прохода по products, p95 поиска с 2,4 с до 85 мс."},
                                {"key": keys["F"], "score": 7.1, "why": "Дубли в events: таблица на ReplacingMergeTree и разовый перенос старых данных в ClickHouse."},
                                {"key": keys["N"], "score": 6.2, "why": "Перенос заметок из JSON в SQLite: таблицы notes и tags, поиск через FTS5."},
                                {"key": keys["L"], "score": 4.8, "why": "Проверка восстановления базы из ночного бэкапа: 18 минут, данные сходятся."},
                                {"key": keys["G"], "score": 3.5, "why": "Модуль postgres в terraform: база вынесена в modules/postgres без изменений в инфраструктуре."}]}}
        smart_script = f"sessionStorage.setItem('nit.smart.result', {json.dumps(json.dumps(smart, ensure_ascii=False))});"
        for name, width, path, init in (("main", 1440, f"/#section=mine&session={keys['A']}&view=session", None),
                                        ("digest", 1440, "/digest.html", None),
                                        ("narrow", 760, f"/#section=mine&session={keys['A']}&view=list", None),
                                        ("settings", 1440, "/settings.html#section=agents", None),
                                        ("search", 1440, f"/#section=mine&session={keys['F']}&view=session", smart_script)):
            ctx = browser.new_context(viewport={"width": width, "height": 900}, device_scale_factor=2,
                                      locale="ru-RU")
            page = ctx.new_page()
            if init:
                page.add_init_script(init)
            page.route("http://127.0.0.1:8765/**", proxy)
            page.goto("http://127.0.0.1:8765" + path)
            page.wait_for_load_state("networkidle")
            time.sleep(1.5)
            page.screenshot(path=str(OUT / f"screen-{name}.png"))
            ctx.close()
        browser.close()
    server.shutdown()


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    try:
        shoot(build())
    finally:
        shutil.rmtree(DEMO, ignore_errors=True)
    print("Снимки:", OUT)
