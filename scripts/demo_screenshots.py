"""Выдуманные сессии агентов на английском и снимки работающей «Нити» на них — для README.

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
    ("A", "codex", "shop-api", "feature/search-index", "Speed up product catalog search", [
        ("user", (0, 7, 55), "Which database queries in shop-api were the slowest over the last day?"),
        ("assistant", (0, 8, 10), "pg_stat_statements puts catalog search on top: 2.1 s on average, 38k calls a day. The stock report comes next at 900 ms, but it runs rarely."),
        ("user", (0, 8, 20), "Catalog search takes 2–3 seconds for queries like \"white sneakers\". Find what is slow and suggest a fix."),
        ("assistant", (0, 8, 31), "I checked EXPLAIN ANALYZE for the query in internal/search/repository.go. Postgres scans the whole products table, 1.2M rows: ILIKE '%…%' can't use a regular index.\n\nI suggest a pg_trgm trigram index on name and description and ordering by similarity(). The query gets a bit more complex, but the API response stays the same."),
        ("user", (0, 8, 40), "Go ahead. Put the migration in its own file and build the index without locking the table."),
        ("assistant", (0, 9, 12), "Done. Added migrations/0042_products_trgm.sql with CREATE INDEX CONCURRENTLY and updated the query in repository.go. On a copy of the database p95 for search dropped from 2.4 s to 85 ms, and the search package tests pass."),
        ("user", (0, 9, 35), "Great. Open a ticket to run the migration on staging."),
        ("assistant", (0, 9, 41), "Opened SHOP-128 \"Run migration 0042 on staging\" and attached the before and after numbers. Anything else for now?"),
    ], False),
    ("B", "claude", "shop-api", "feature/promo-codes", "Promo codes in the cart: MR review", [
        ("user", (0, 9, 5), "Review the MR with promo codes in the cart. Pay attention to the discount math and code expiry."),
        ("assistant", (0, 9, 50), "Found two problems.\n\n1. The discount is applied before the item price is rounded, so for orders with three or more items the total is one cent off the receipt.\n2. Code expiry is not checked when the cart is opened again: an expired code keeps working.\n\nThe rest looks fine. Should I leave comments on the MR?"),
    ], False),
    ("X1", "codex", "shop-api", "feature/search-index", "Review migration 0042", [
        ("user", (0, 9, 20), "Check migrations/0042_products_trgm.sql: locks, rollback, time on a large table."),
        ("assistant", (0, 9, 26), "No issues: the index is built without locks, the rollback drops it, and on 1.2M rows it took 40 s."),
    ], True),
    ("C", "kimi", "landing", "main", "Move the landing page to Astro", [
        ("user", (0, 8, 5), "Move the landing page from Gatsby to Astro and keep the current page URLs."),
        ("assistant", (0, 8, 50), "Moved 12 pages and the shared layout. URLs match, no redirects needed. The build now takes 14 s instead of 48. The signup form is left: it depends on a Gatsby plugin."),
    ], False),
    ("D", "grok", "infra", "main", "Disk usage alerts", [
        ("user", (1, 22, 50), "Add a Prometheus alert for nodes whose disk is more than 85% full."),
        ("assistant", (1, 23, 12), "Added the NodeDiskAlmostFull rule to alerts/node.yml: it fires when less than 15% is free for more than 10 minutes. Checked it with promtool."),
    ], False),
    ("E", "claude", "mobile-app", "release/2.3", "Crash on Android 14 when opening the camera", [
        ("user", (1, 21, 30), "The app crashes on Android 14 when the camera opens from the profile. Stack trace in crash.txt."),
        ("assistant", (1, 22, 14), "The cause is partial photo access on Android 14: the user picks a few photos, but the code expects access to the whole gallery and crashes on an empty list. Added a check and a request for partial access. It still needs a test on a device."),
    ], False),
    ("X2", "claude", "mobile-app", "release/2.3", "Advice: Android 14 permissions", [
        ("user", (1, 22, 30), "How do I request partial photo access on Android 14? Keep it short."),
        ("assistant", (1, 22, 40), "Request READ_MEDIA_VISUAL_USER_SELECTED together with READ_MEDIA_IMAGES and handle the case when only some photos are selected."),
    ], True),
    ("F", "codex", "data-pipeline", "feat/dedup", "Deduplicate events in ClickHouse", [
        ("user", (1, 18, 30), "The events table has duplicates after the mobile client resends events. How do we get rid of them?"),
        ("assistant", (1, 19, 2), "Switched the table to ReplacingMergeTree keyed by (user_id, event_id) and added FINAL to the reports where accuracy matters. A one-off script, scripts/dedup_events.sql, moves the old data."),
    ], False),
    ("G", "kimi", "infra", "terraform-modules", "Split terraform into modules", [
        ("user", (1, 15, 50), "Split the root terraform into modules: network, database, cluster."),
        ("assistant", (1, 16, 40), "Moved the network, database and cluster into modules/network, modules/postgres and modules/k8s. terraform plan shows 0 changes: the state moved with moved blocks."),
    ], False),
    ("H", "grok", "notes-bot", "main", "/remind command for the notes bot", [
        ("user", (1, 14, 30), "Add a /remind command to the bot: \"/remind tomorrow at 9 buy cat food\"."),
        ("assistant", (1, 15, 10), "Added /remind with parsing for times like \"tomorrow at 9\" and a reminder queue in SQLite. The date parsing tests pass."),
    ], False),
    ("I", "claude", "landing", "feature/dark-theme", "Dark theme for the landing page", [
        ("user", (2, 17, 40), "Add a dark theme that follows the system setting, without a toggle."),
        ("assistant", (2, 18, 20), "Moved colors to CSS variables and added prefers-color-scheme. Checked text contrast: at least 4.5:1 everywhere."),
    ], False),
    ("J", "codex", "mobile-app", "feature/offline", "Offline mode: request queue", [
        ("user", (2, 11, 20), "Orders get lost without a network. We need a request queue that sends when the network is back."),
        ("assistant", (2, 12, 0), "Added a queue in the local database: requests are stored and sent in order once the network is back. The server rejects a repeated order by its request key."),
    ], False),
    ("K", "kimi", "data-pipeline", "main", "Kafka or Redpanda for the event stream", [
        ("user", (3, 16, 50), "Compare Kafka and Redpanda for an event stream of 5k messages per second."),
        ("assistant", (3, 17, 30), "Both handle this load. Redpanda is easier to run: a single binary without ZooKeeper, and Kafka clients work unchanged. I suggest Redpanda."),
    ], False),
    ("L", "claude", "infra", "main", "Check that database backups restore", [
        ("user", (3, 10, 20), "Check that the nightly database backup actually restores."),
        ("assistant", (3, 11, 0), "Restored yesterday's backup on a separate machine: 18 minutes, and the data matches production at backup time."),
    ], False),
    ("M", "grok", "shop-api", "master", "Upgrade Go to 1.25", [
        ("user", (4, 15, 30), "Upgrade the project to Go 1.25."),
        ("assistant", (4, 16, 0), "Updated go.mod and the build image. Build and tests pass, no linter warnings."),
    ], False),
    ("N", "codex", "notes-bot", "main", "Store notes in SQLite", [
        ("user", (4, 13, 20), "Move the notes from a JSON file to SQLite."),
        ("assistant", (4, 14, 0), "Done: notes and tags tables, and a one-off import of the old file on startup. Full-text search uses FTS5."),
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
            keys["A"]: {"title": "Catalog search: trigram index instead of a full scan",
                        "summary": "Catalog search took 2–3 s: ILIKE scanned the whole products table. Added a pg_trgm index on name and description (migration 0042, no table lock) and ordering by similarity. On a copy of the database p95 dropped from 2.4 s to 85 ms, tests pass. Ticket SHOP-128 tracks running the migration on staging.",
                        "next_step": "Run migration 0042 on staging and compare search p95 with the database copy",
                        "closed": False, "related": [{"key": keys["M"], "why": "same service"}],
                        "at": summary_at, "model": "kimi"},
            keys["D"]: {"title": "NodeDiskAlmostFull alert for nodes",
                        "summary": "Added a Prometheus rule: less than 15% free for more than 10 minutes. Checked with promtool.",
                        "next_step": "", "closed": True, "related": [], "at": summary_at, "model": "kimi"},
            keys["E"]: {"title": "Android 14: camera crash with partial photo access",
                        "summary": "The profile camera crashed when the user granted access to only some photos. Added an empty list check and a partial access request. Not tested on a device yet.",
                        "next_step": "Test the fix on an Android 14 device",
                        "closed": False, "related": [], "at": summary_at, "model": "kimi"},
        },
        "digests": {"5": {"at": (TODAY + timedelta(hours=7, minutes=30)).timestamp(), "model": "claude", "result": {
            "lead": "Most of the work went into shop-api and mobile-app. In shop-api, catalog search got 28 times faster, and the promo code MR turned up two bugs. In mobile-app, the Android 14 camera crash is fixed; infra got a disk alert and terraform modules.",
            "projects": [
                {"name": "shop-api", "bullets": [
                    {"text": "Catalog search went from 2.4 s to 85 ms: pg_trgm index, migration 0042.", "keys": [keys["A"]]},
                    {"text": "Two bugs in the promo code MR: a one-cent rounding error and expired codes.", "keys": [keys["B"]]},
                    {"text": "Go upgraded to 1.25, build and tests pass.", "keys": [keys["M"]]}]},
                {"name": "mobile-app", "bullets": [
                    {"text": "The Android 14 camera crash is fixed and waits for a device test.", "keys": [keys["E"]]},
                    {"text": "Offline mode: orders wait in a queue and go out when the network is back.", "keys": [keys["J"]]}]},
                {"name": "infra", "bullets": [
                    {"text": "NodeDiskAlmostFull alert when less than 15% of the disk is free.", "keys": [keys["D"]]},
                    {"text": "Terraform split into network, postgres and k8s modules with no infrastructure changes.", "keys": [keys["G"]]},
                    {"text": "Database restore from backup checked: 18 minutes.", "keys": [keys["L"]]}]},
                {"name": "landing", "bullets": [
                    {"text": "The landing page moved to Astro, builds take 14 s instead of 48.", "keys": [keys["C"]]},
                    {"text": "Dark theme that follows the system setting.", "keys": [keys["I"]]}]},
                {"name": "data-pipeline", "bullets": [
                    {"text": "Duplicate events removed with ReplacingMergeTree.", "keys": [keys["F"]]},
                    {"text": "Redpanda chosen for the event stream: Kafka clients work unchanged.", "keys": [keys["K"]]}]},
                {"name": "notes-bot", "bullets": [
                    {"text": "The /remind command and notes stored in SQLite with full-text search.", "keys": [keys["H"], keys["N"]]}]},
            ],
            "tails": [
                {"id": "t1", "text": "Run migration 0042 on staging", "key": keys["A"]},
                {"id": "t2", "text": "Leave comments on the promo code MR", "key": keys["B"]},
                {"id": "t3", "text": "Test the camera fix on an Android 14 device", "key": keys["E"]},
                {"id": "t4", "text": "Move the landing page signup form", "key": keys["C"]}],
            "autos": [{"key": keys["X1"], "project": "shop-api",
                       "text": "Migration 0042 review: no issues, the index is built without locks."}],
        }}},
    }
    DATA.mkdir(parents=True, exist_ok=True)
    (DATA / "state.json").write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")
    return keys


def demo_agent_job(server, keys):
    """Законченное задание умного поиска с журналом агента — для снимка «сессии агента».
    Настоящий CLI не запускается: события те же, что пишет runner.run."""
    workdir = "~/.local/share/ai-threads/runs/20261001-0912-search"
    events = [
        {"type": "progress", "step": 1, "steps": 3, "text": "understanding the query", "agent": "kimi"},
        {"type": "trace", "agent": "kimi", "kind": "prompt", "text": "Parse the search request following the agent instructions.\nRequest: where did I work on databases and migrations\nAgents (id — name): codex — Codex, claude — Claude Code, grok — Grok, kimi — Kimi"},
        {"type": "trace", "agent": "kimi", "kind": "model", "text": "kimi-code/kimi-for-coding"},
        {"type": "trace", "agent": "kimi", "kind": "thinking", "text": "The person wants sessions about databases and schema changes. Code names will be things like migration files, index types and engines. I should give both English terms and common names: migration, index, postgres, clickhouse, sqlite."},
        {"type": "trace", "agent": "kimi", "kind": "text", "text": '{"terms": ["migration", "index", "postgres", "clickhouse", "sqlite"], "agents": [], "projects": [], "days": null}'},
        {"type": "agent_session", "agent": "kimi", "session": "session_5f0c2a9e-1b7d-4c1e-9a3f-2d8e6b4c7a10",
         "command": f"cd -- {workdir} && kimi --session session_5f0c2a9e-1b7d-4c1e-9a3f-2d8e6b4c7a10"},
        {"type": "plan", "terms": ["migration", "index", "postgres", "clickhouse", "sqlite"], "agents": [], "projects": [], "days": None},
        {"type": "trace", "agent": "nit", "kind": "nit", "text": "Searching the logs of 14 sessions for: migration, index, postgres, clickhouse, sqlite"},
        {"type": "trace", "agent": "nit", "kind": "nit", "text": "Matches in 7 sessions, the top 7 went to the agent"},
        {"type": "progress", "step": 3, "steps": 3, "text": "picking sessions", "agent": "kimi"},
        {"type": "trace", "agent": "kimi", "kind": "prompt", "text": "Pick the sessions that answer the request following the agent instructions.\nRequest: where did I work on databases and migrations\n\nCandidates:\n- key: codex:…\n  project: shop-api\n  title: Speed up product catalog search"},
        {"type": "trace", "agent": "kimi", "kind": "tool", "tool": "Read", "text": '{"path": "/home/dev/.codex/sessions/2026/10/01/rollout-2026-10-01T07-55-00.jsonl", "offset": 0}'},
        {"type": "trace", "agent": "kimi", "kind": "result", "text": "{\"type\": \"response_item\", \"payload\": {\"role\": \"assistant\", \"content\": \"Done. Added migrations/0042_products_trgm.sql with CREATE INDEX CONCURRENTLY…\"}}"},
        {"type": "trace", "agent": "kimi", "kind": "thinking", "text": "Migration 0042 and the ClickHouse dedup are real schema work. The SQLite notes move and the backup restore check are database work too. The Go upgrade only mentions the database in passing, so it stays out."},
        {"type": "trace", "agent": "kimi", "kind": "text", "text": "Five sessions fit: migration 0042 in shop-api, the ClickHouse dedup, notes moved to SQLite, the backup restore check and the postgres module in terraform."},
        {"type": "trace", "agent": "kimi", "kind": "usage", "text": "in 18421 · out 912"},
        {"type": "result", "result": {"query": "where did I work on databases and migrations", "ranked": True, "scanned": 14,
                                      "plan": {"terms": ["migration", "index"], "agents": [], "projects": [], "days": None},
                                      "results": []}},
    ]

    def emit_all(job):
        for event in events:
            job.emit(event)

    job = server.jobs.start("search:demo", "search", None, emit_all)
    job.query = "where did I work on databases and migrations"
    while not job.finished:
        time.sleep(0.05)


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
    demo_agent_job(server, keys)
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
        smart = {"q": "where did I work on databases and migrations", "scope": "mine", "agent": "kimi", "warnings": [], "job": "search:demo",
                 "result": {"query": "where did I work on databases and migrations", "ranked": True, "scanned": 14,
                            "plan": {"terms": ["migration", "index", "postgres", "clickhouse", "sqlite"],
                                     "agents": [], "projects": [], "days": None},
                            "results": [
                                {"key": keys["A"], "score": 9.4, "why": "Migration 0042: a pg_trgm index instead of a full scan of products, search p95 from 2.4 s to 85 ms."},
                                {"key": keys["F"], "score": 7.1, "why": "Duplicate events: the table moved to ReplacingMergeTree, with a one-off move of old data in ClickHouse."},
                                {"key": keys["N"], "score": 6.2, "why": "Notes moved from JSON to SQLite: notes and tags tables, full-text search with FTS5."},
                                {"key": keys["L"], "score": 4.8, "why": "Restoring the database from the nightly backup: 18 minutes, the data matches."},
                                {"key": keys["G"], "score": 3.5, "why": "A postgres module in terraform: the database moved to modules/postgres with no infrastructure changes."}]}}
        smart_script = f"sessionStorage.setItem('nit.smart.result', {json.dumps(json.dumps(smart, ensure_ascii=False))});"
        # Снимок, адрес и элемент, появление которого значит «страница готова»: сетевой тишины
        # не бывает — страница опрашивает статусы и держит поток событий заданий.
        for name, width, path, init, ready in (
                ("main", 1440, f"/#section=mine&session={keys['A']}&view=session", None, '[data-testid="message"]'),
                ("digest", 1440, "/digest.html", None, '[data-testid="digest-lead"]'),
                ("narrow", 760, f"/#section=mine&session={keys['A']}&view=list", None, '[data-testid="row"]'),
                ("settings", 1440, "/settings.html#section=agents", None, '[data-testid="agent-card"]'),
                ("search", 1440, f"/#section=mine&session={keys['F']}&view=session", smart_script,
                 '[data-testid="smart-why"]'),
                ("agent", 1440, f"/#section=mine&session={keys['F']}&view=agent&log=search%3Ademo", smart_script,
                 '[data-testid="agent-log"]')):
            ctx = browser.new_context(viewport={"width": width, "height": 900}, device_scale_factor=2,
                                      locale="en-US")
            page = ctx.new_page()
            if init:
                page.add_init_script(init)
            page.route("http://127.0.0.1:8765/**", proxy)
            page.goto("http://127.0.0.1:8765" + path)
            page.wait_for_selector(ready, timeout=30000)
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
