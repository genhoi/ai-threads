"""Язык сообщений: выбор языка, английские ответы API, события заданий, терминал, словарь."""

from __future__ import annotations

import ast
import contextvars
import http.client
import importlib
import inspect
import json
import os
import pkgutil
import shlex
import string
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

import ai_threads
from ai_threads import i18n, terminal
from ai_threads.agents import AGENTS
from ai_threads.agents.claude import _where
from ai_threads.catalog import Catalog
from ai_threads.digest import run_digest
from ai_threads.jobs import Jobs
from ai_threads.model import Session
from ai_threads.search import run_search
from ai_threads.server import create_server
from ai_threads.store import Store
from ai_threads.summary import run_summary

from .a_support import homes
from .conftest import all_events, wait_finished

ROOT = Path(__file__).resolve().parents[1]
RESUME = ROOT / "bin" / "nit-resume"


def write_settings(values):
    Path(os.environ["AI_THREADS_CONFIG"]).write_text(json.dumps(values), encoding="utf-8")


def in_language(lang, fn, *args, **kwargs):
    """Вызвать fn в отдельном контексте с языком lang: язык не утекает в другие тесты."""
    def call():
        i18n.use(lang)
        return fn(*args, **kwargs)
    return contextvars.copy_context().run(call)


# --- выбор языка ----------------------------------------------------------

@pytest.mark.parametrize("header,expected", [
    ("ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7", "ru"),
    ("en-US,en;q=0.9,ru;q=0.8", "en"),
    ("de-DE,de;q=0.9,en;q=0.5,ru;q=0.7", "ru"),
    ("ru;q=0.3, en;q=0.6", "en"),
    ("RU-ru", "ru"),
    ("ru;q=abc, en;q=0.1", "en"),
    ("de, fr;q=0.8", None),
    ("", None),
    (None, None),
])
def test_from_header(header, expected):
    assert i18n.from_header(header) == expected


@pytest.mark.parametrize("header,expected", [(None, "en"), ("de", "en"), ("ru-RU", "ru"), ("en-GB", "en")])
def test_resolve_request_without_setting(header, expected):
    assert i18n.resolve(header, request=True) == expected


def test_resolve_without_request_uses_environment(monkeypatch):
    assert i18n.resolve() == "ru"  # conftest: LANG=ru_RU.UTF-8
    monkeypatch.setenv("LANG", "en_US.UTF-8")
    assert i18n.resolve() == "en"
    monkeypatch.setenv("LC_MESSAGES", "ru_RU.UTF-8")
    assert i18n.resolve() == "ru"
    monkeypatch.setenv("LC_ALL", "C.UTF-8")
    assert i18n.resolve() == "en"
    for name in ("LC_ALL", "LC_MESSAGES", "LANG"):
        monkeypatch.delenv(name)
    assert i18n.resolve() == "en"


def test_setting_beats_header_and_environment():
    # Файлы разной длины: настройки перечитываются по размеру и времени изменения файла.
    write_settings({"language": "ru"})
    assert i18n.resolve("en-US,en", request=True) == "ru"
    write_settings({"language": "en", "open_with": "auto"})
    assert i18n.resolve("ru-RU", request=True) == "en"
    assert i18n.resolve() == "en"
    write_settings({"language": "auto"})
    assert i18n.resolve("en", request=True) == "en" and i18n.resolve() == "ru"


def test_t_falls_back_and_formats(monkeypatch):
    monkeypatch.setitem(i18n.MESSAGES, "test.only_ru", {"ru": "только {what}"})
    assert in_language("en", i18n.t, "test.only_ru", what="по-русски") == "только по-русски"
    assert i18n.t("test.no_such_key") == "test.no_such_key"
    assert in_language("en", i18n.t, "runner.timeout", agent="Kimi", seconds=5) == "Kimi didn't answer in 5 s"
    assert i18n.t("runner.timeout", agent="Kimi", seconds=5) == "Kimi не ответил за 5 с"


def test_plural_ru():
    forms = ("сессия", "сессии", "сессий")
    assert [i18n.plural_ru(n, *forms) for n in (1, 2, 5, 11, 12, 21, 22, 25, 111)] == [
        "сессия", "сессии", "сессий", "сессий", "сессий", "сессия", "сессии", "сессий", "сессий"]


# --- API ------------------------------------------------------------------

@pytest.fixture
def server(tmp_path):
    catalog = Catalog(tmp_path / "cache.json")
    assert catalog.wait(10), catalog.error
    server = create_server(0, catalog=catalog, store=Store(tmp_path / "state.json"))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)


def call(server, path, body=None, language=None):
    connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=10)
    host = f"127.0.0.1:{server.server_port}"
    headers = {"Host": host}
    if language is not None:
        headers["Accept-Language"] = language
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers.update({"Content-Type": "application/json", "Origin": f"http://{host}"})
    connection.request("POST" if body is not None else "GET", path, body=data, headers=headers)
    response = connection.getresponse()
    result = response.status, json.loads(response.read())
    connection.close()
    return result


BAD_KEY = "/api/resolve?key=../../etc/passwd"


@pytest.mark.parametrize("language,lang,error", [
    ("en-US,en;q=0.9", "en", "Invalid session key"),
    (None, "en", "Invalid session key"),
    ("de-DE,de;q=0.9", "en", "Invalid session key"),
    ("ru-RU,ru;q=0.9,en;q=0.8", "ru", "Неверный ключ сессии"),
])
def test_api_language_follows_accept_language(server, language, lang, error):
    assert call(server, BAD_KEY, language=language) == (400, {"error": error})
    assert call(server, "/api/config", language=language)[1]["language"] == lang


def test_api_setting_beats_accept_language(server):
    write_settings({"language": "ru"})
    assert call(server, BAD_KEY, language="en")[1] == {"error": "Неверный ключ сессии"}
    assert call(server, "/api/config", language="en")[1]["language"] == "ru"


def test_api_errors_in_english(server):
    assert call(server, "/api/nowhere", language="en") == (404, {"error": "Route not found"})
    assert call(server, "/api/pin", {"key": "codex:abc"}, language="en") == (400, {"error": "Field pinned is required"})
    assert call(server, "/api/settings", {"settings": {"language": "de"}}, language="en") == (
        400, {"error": "language: one of auto, ru, en"})
    assert call(server, "/api/settings", {"settings": {"language": "de"}}, language="ru")[1] == {
        "error": "language: одно из auto, ru, en"}


def test_settings_error_is_shown_in_the_reader_language(server):
    # Причина в файле настроек сохраняется при чтении, а текст собирается при показе.
    write_settings({"open_with": "nope"})
    english = call(server, "/api/config", language="en")[1]["settings_error"]
    russian = call(server, "/api/config", language="ru")[1]["settings_error"]
    assert english.endswith("open_with: one of auto, wt, tmux, plain")
    assert russian.endswith("open_with: одно из auto, wt, tmux, plain")


def test_digest_models_hints_in_english(server, stub_path):
    hints = {m["id"]: m["hint"] for m in call(server, "/api/digest/models", language="en")[1]}
    assert hints["kimi"] == "Write a digest with Kimi"
    hints = {m["id"]: m["hint"] for m in call(server, "/api/digest/models", language="ru")[1]}
    assert hints["kimi"] == "Составить сводку через Kimi"


def test_digest_models_missing_in_english(server, no_tools):
    hints = [m["hint"] for m in call(server, "/api/digest/models", language="en")[1]]
    assert hints[0] == "Kimi is not installed"


def test_agent_labels_in_english():
    zcode = AGENTS["zcode"]
    assert in_language("en", zcode.resume_hint) == "Can only be resumed in the ZCode app"
    assert in_language("en", zcode.to_json)["resume_hint"] == "Can only be resumed in the ZCode app"
    session = Session(tool="zcode", id="sess_1", title="t", cwd="/x", updated=0)
    assert in_language("en", lambda: session.to_json()["resume_hint"]) == "Can only be resumed in the ZCode app"
    assert in_language("en", _where, "interactive", "api") == "Claude Code in a terminal · api"
    assert in_language("en", _where, "bg", None) == "Claude Code background session"
    assert _where("bg", None) == "фоновая сессия Claude Code"


# --- задания --------------------------------------------------------------

@pytest.fixture
def summary_session(tmp_path) -> Session:
    journal = tmp_path / "wire.jsonl"
    journal.write_text('{"type":"metadata"}\n', encoding="utf-8")
    return Session(tool="kimi", id="session_test1", title="Исходное название",
                   cwd=str(tmp_path), updated=time.time(), journal=journal)


def run_job(lang, job_id, kind, target):
    """Запустить задание из контекста с языком lang — как сервер из запроса."""
    job = in_language(lang, Jobs().start, job_id, kind, None, target)
    wait_finished(job)
    return all_events(job)


def test_summary_events_in_english(stub_path, summary_session, store):
    events = run_job("en", "summary:1", "summary",
                     lambda job: run_summary(job, summary_session, "Название", [], store))
    progress = [e["text"] for e in events if e["type"] == "progress"]
    assert progress[0] == "starting"
    assert "reading part 1" in progress and "analyzing the log" in progress
    assert progress[-1] == "writing the summary"
    assert store.summaries[summary_session.key]["title"] == "Тест. Сводка по сессии"


def test_summary_errors_in_english(no_tools, summary_session, store):
    events = run_job("en", "summary:1", "summary",
                     lambda job: run_summary(job, summary_session, "Название", [], store))
    errors = [e["message"] for e in events if e["type"] == "error"]
    assert errors and errors[0].startswith("No agent for summaries found. Install one of: Kimi")


def test_summary_timeout_in_english(stub_path, summary_session, store, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "hang")
    monkeypatch.setenv("AI_THREADS_SUMMARY_TIMEOUT", "1")
    events = run_job("en", "summary:1", "summary",
                     lambda job: run_summary(job, summary_session, "Название", [], store))
    assert [e["message"] for e in events if e["type"] == "error"] == ["Kimi didn't answer in 1 s"]


def test_digest_events_in_english(stub_path, tmp_path, store):
    journal = tmp_path / "a" / "wire.jsonl"
    journal.parent.mkdir()
    journal.write_text('{"type":"row"}\n', encoding="utf-8")
    # Папка вне /tmp: сессии во временных папках в сводку не попадают.
    session = Session(tool="kimi", id="aaa", title="Сессия A", cwd="/home/user/projects/sample-a",
                      updated=time.time(), journal=journal)
    events = run_job("en", "digest", "digest",
                     lambda job: run_digest(job, [session], 3, "kimi", {}, {}, (), store))
    stages = [e["label"] for e in events if e["type"] == "stage"]
    assert stages == ["Collecting", "Reading logs", "Analyzing", "Writing"]
    logs = [e["text"] for e in events if e["type"] == "log"]
    assert logs[0] == ("Sent to the model: 1 session (0 with ready summaries, 0 with excerpts, "
                       "1 to read in full), 0 automatic runs")


@pytest.fixture
def catalog_sessions(homes, tmp_path):
    catalog = Catalog(tmp_path / "cache.json")
    assert catalog.wait(10)
    return catalog.sessions()


def search_events(lang, sessions, query="найди сессии про каталог"):
    return run_job(lang, "search:1", "search",
                   lambda job: run_search(job, sessions, query, "mine", [], {}, {}))


def test_search_events_in_english(stub_path, catalog_sessions):
    events = search_events("en", catalog_sessions)
    progress = [e["text"] for e in events if e["type"] == "progress"]
    assert progress[0] == "understanding the query" and progress[-1] == "picking sessions"
    assert any(text.startswith("searching the logs: ") and " of " in text for text in progress)
    assert not [e for e in events if e["type"] in ("warning", "error")]


def test_search_warnings_in_english(stub_path, catalog_sessions, monkeypatch):
    monkeypatch.setenv("AI_THREADS_STUB_MODE", "garbage")
    events = search_events("en", catalog_sessions, "каталог записей")
    warnings = [e["message"] for e in events if e["type"] == "warning"]
    assert len(warnings) == 2
    assert warnings[0].startswith("Kimi couldn't parse the query (")
    assert warnings[0].endswith("). Searching by the query words.")
    assert warnings[1].startswith("Kimi couldn't pick sessions (")


def test_unexpected_job_error_in_english():
    def broken(job):
        raise RuntimeError("boom")
    events = run_job("en", "broken", "summary", broken)
    assert [e["message"] for e in events if e["type"] == "error"] == ["Unexpected job error: boom"]


# --- скрипт восстановления и терминал ------------------------------------

def test_restore_script_notes_in_english(tmp_path):
    gone = Session(tool="grok", id="g1", title="t", cwd=str(tmp_path / "gone"), updated=0)
    tmux = in_language("en", terminal.restore_script, [gone], "tmux", 8793)
    assert shlex.split(tmux.splitlines()[1])[-1] == "echo 'no folder · no cd'; grok --resume g1"
    assert in_language("en", terminal.restore_script, [], "plain", 8793) == (
        "# all selected sessions are already open or can't be resumed from the terminal")
    with pytest.raises(FileNotFoundError, match="The project folder no longer exists"):
        in_language("en", terminal.reveal, gone, "vscode")


def resume_env(home, lang):
    env = os.environ.copy()
    env.update(HOME=str(home), LANG=lang)
    for name in ("LC_ALL", "LC_MESSAGES", "BASH_ENV"):
        env.pop(name, None)
    return env


def run_resume(args, home, lang):
    completed = subprocess.run([str(RESUME), *args], stdin=subprocess.DEVNULL, capture_output=True,
                               text=True, env=resume_env(home, lang), timeout=30, start_new_session=True)
    return completed.stderr


@pytest.mark.parametrize("lang,usage", [
    ("en_US.UTF-8", "Usage: nit-resume --port PORT KEY"),
    ("C.UTF-8", "Usage: nit-resume --port PORT KEY"),
    ("ru_RU.UTF-8", "Использование: nit-resume --port ПОРТ КЛЮЧ"),
])
def test_resume_usage_follows_environment(tmp_path, lang, usage):
    assert usage in run_resume([], tmp_path, lang)


def test_resume_missing_folder_in_english(tmp_path):
    gone = tmp_path / "gone"

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            payload = json.dumps({"cwd": str(gone), "command_no_cd": "true"}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, fmt, *args):
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        err = run_resume(["--port", str(server.server_address[1]), "grok:g1"], tmp_path, "en_US.UTF-8")
    finally:
        server.shutdown()
        server.server_close()
    assert f"The project folder no longer exists: {gone}" in err
    assert in_language("en", i18n.t, "terminal.missing_note") in err.splitlines()


@pytest.mark.parametrize("lang,message", [("en_US.UTF-8", "the port must be from 1 to 65535"),
                                          ("ru_RU.UTF-8", "порт должен быть от 1 до 65535")])
def test_command_line_follows_environment(lang, message):
    env = {**os.environ, "LANG": lang}
    completed = subprocess.run([sys.executable, "-m", "ai_threads", "--port", "0"], cwd=ROOT, env=env,
                               capture_output=True, text=True, timeout=30)
    assert completed.returncode == 2 and message in completed.stderr


# --- словарь --------------------------------------------------------------

def all_modules() -> list[str]:
    return [info.name for info in pkgutil.walk_packages(ai_threads.__path__, "ai_threads.")]


def used_keys(path: Path) -> set[str]:
    """Ключи-литералы в вызовах t(...), i18n.t(...) и Invalid(...) файла."""
    keys = set()
    for node in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
        if not isinstance(node, ast.Call) or not node.args:
            continue
        func = node.func
        name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", "")
        first = node.args[0]
        if name in ("t", "Invalid") and isinstance(first, ast.Constant) and isinstance(first.value, str):
            keys.add(first.value)
    return keys


def placeholders(text: str) -> set[str]:
    return {name for _, name, _, _ in string.Formatter().parse(text) if name}


def test_dictionary_is_complete():
    for name in all_modules():
        importlib.import_module(name)
    assert len(i18n.MESSAGES) > 50
    for key, entry in i18n.MESSAGES.items():
        assert set(entry) == {"ru", "en"}, key
        ru, en = entry["ru"], entry["en"]
        if callable(ru) or callable(en):
            assert callable(ru) and callable(en), key
            params = inspect.signature(ru).parameters
            assert params.keys() == inspect.signature(en).parameters.keys(), key
            for n in (1, 2, 5):
                assert ru(**{p: n for p in params}) and en(**{p: n for p in params}), key
        else:
            assert ru.strip() and en.strip(), key
            assert placeholders(ru) == placeholders(en), key
    for path in (ROOT / "ai_threads").rglob("*.py"):
        missing = used_keys(path) - set(i18n.MESSAGES)
        assert not missing, (path, missing)
    from ai_threads import digest, summary
    indirect = [*digest.STAGES, summary.STEP_LAUNCH, summary.STEP_ANALYZE, summary.STEP_WRITE,
                *(a.resume_hint_key for a in AGENTS.values() if a.resume_hint_key)]
    assert set(indirect) <= set(i18n.MESSAGES)


def test_each_module_registers_the_keys_it_uses():
    """Модуль, импортированный сам по себе, знает все свои ключи: иначе t() вернул бы ключ."""
    files = {name: Path(importlib.import_module(name).__file__) for name in all_modules()}
    checks = {name: sorted(used_keys(path)) for name, path in files.items() if used_keys(path)}
    script = ("import importlib, json, sys\n"
              "from ai_threads import i18n\n"
              "name, keys = sys.argv[1], json.loads(sys.argv[2])\n"
              "importlib.import_module(name)\n"
              "print(json.dumps([k for k in keys if k not in i18n.MESSAGES]))\n")
    running = {name: subprocess.Popen([sys.executable, "-c", script, name, json.dumps(keys)], cwd=ROOT,
                                      stdout=subprocess.PIPE, text=True)
               for name, keys in checks.items()}
    for name, process in running.items():
        out, _ = process.communicate(timeout=60)
        assert process.returncode == 0 and json.loads(out) == [], name
