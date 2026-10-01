import hashlib
import http.client
import json
import threading
import types
from pathlib import Path
from urllib.parse import quote

import pytest
from playwright.sync_api import Error, sync_playwright

from ai_threads.catalog import Catalog
from ai_threads.server import create_server
from ai_threads.store import Store
from .a_support import homes, key


@pytest.fixture
def http_server(homes, tmp_path):
    def hashes():
        return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in homes.rglob("*") if p.is_file()}

    before = hashes()
    catalog = Catalog(tmp_path / "data/cache.json")
    assert catalog.wait(10), catalog.error
    store = Store(tmp_path / "data/state.json")
    static = tmp_path / "static"
    static.mkdir()
    (static / "index.html").write_bytes((Path(__file__).parents[1] / "static/index.html").read_bytes())
    (static / "app.js").write_text("const value = 1;\n")
    (static / "data.json").write_text('{"value":1}')
    (static / "outside").symlink_to(homes)
    server = create_server(0, catalog=catalog, store=store, static=static)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)
        catalog.wait(10)
        assert hashes() == before


def request(server, path, body=None, *, method=None, host=None, origin=True, raw=None, headers=None):
    connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=5)
    method = method or ("POST" if body is not None or raw is not None else "GET")
    host = host or f"127.0.0.1:{server.server_port}"
    data = json.dumps(body, ensure_ascii=False).encode() if raw is None and body is not None else raw
    # Без настройки language язык ответа берётся из Accept-Language; здесь ожидания русские.
    request_headers = {"Host": host, "Accept-Language": "ru-RU,ru;q=0.9"}
    if method == "POST":
        request_headers["Content-Type"] = "application/json"
        if origin:
            request_headers["Origin"] = f"http://{host}" if origin is True else origin
    request_headers.update(headers or {})
    connection.request(method, path, body=data, headers=request_headers)
    response = connection.getresponse()
    payload = response.read()
    info = dict(response.getheaders())
    result = json.loads(payload) if info.get("Content-Type", "").startswith("application/json") else payload.decode()
    connection.close()
    assert info["X-Content-Type-Options"] == "nosniff"
    if path.startswith("/api/"):
        assert info["Cache-Control"] == "no-store"
    return response.status, result, info


@pytest.mark.parametrize("host", ["foreign.invalid", "127.0.0.1:8765", "localhost", "127.0.0.1:1@evil.invalid"])
def test_foreign_host_is_forbidden(http_server, host):
    assert request(http_server, "/api/sessions", host=host)[0] == 403


@pytest.mark.parametrize("origin", [False, "http://foreign.invalid", "null", "http://localhost:1"])
def test_post_requires_matching_origin(http_server, origin):
    assert request(http_server, "/api/pin", {"key": key(), "pinned": True}, origin=origin)[0] == 403
    assert http_server.store.state()["pins"] == []


@pytest.mark.parametrize("path,limit", [("/api/name", 16 * 1024), ("/api/import", 1024 * 1024)])
def test_body_size_limit(http_server, path, limit):
    # Сервер обязан отказать по длине до загрузки большого тела.
    assert request(http_server, path, raw=b"", headers={"Content-Length": str(limit + 1)})[0] == 413


@pytest.mark.parametrize("method", ["HEAD", "PATCH", "OPTIONS"])
def test_host_is_checked_for_every_method(http_server, method):
    connection = http.client.HTTPConnection("127.0.0.1", http_server.server_port, timeout=5)
    connection.request(method, "/api/state", headers={"Host": "foreign.invalid"})
    response = connection.getresponse()
    assert response.status == 403
    response.read()
    connection.close()


def test_ready_does_not_label_an_incomplete_snapshot(http_server, monkeypatch):
    catalog = http_server.catalog
    complete = catalog.sessions()
    catalog._ready = False

    def finish_during_snapshot():
        catalog._ready = True
        return complete[:3]

    monkeypatch.setattr(catalog, "sessions", finish_during_snapshot)
    result = request(http_server, "/api/sessions")[1]
    assert len(result["sessions"]) == 3
    assert result["ready"] is False


def test_session_state_and_resolve_routes(http_server):
    status, catalog, _ = request(http_server, "/api/sessions")
    assert status == 200 and catalog["ready"] and len(catalog["sessions"]) == 13
    assert catalog["port"] == http_server.server_port
    assert catalog["sessions"] == sorted(catalog["sessions"], key=lambda s: s["updated"], reverse=True)
    assert "journal" not in catalog["sessions"][0]
    assert request(http_server, "/api/state")[1]["version"] == 2
    for route, field in (("pin", "pinned"), ("done", "done"), ("hide", "hidden")):
        assert request(http_server, "/api/" + route, {"key": key(), field: True})[0] == 200
    assert request(http_server, "/api/name", {"key": key(), "name": "Моё название"})[1]["names"][key()] == "Моё название"
    status, state, _ = request(http_server, "/api/state")
    assert state["pins"] == [key()] and state["done"] == [key()] and state["hidden"] == [key()]
    status, resolved, _ = request(http_server, "/api/resolve?key=" + key())
    assert status == 200 and set(resolved) == {"cwd", "command", "command_no_cd", "tool", "project"}
    assert resolved["command"] == "cd -- '/home/user/projects/sample app' && codex resume " + key().split(":")[1]
    status, transcript, _ = request(http_server, "/api/session?key=" + key())
    assert status == 200 and not transcript["truncated"]
    assert [m["role"] for m in transcript["messages"]] == ["user", "assistant"]
    assert request(http_server, "/api/name", {"key": key(), "name": ""})[1]["names"] == {}
    assert request(http_server, "/api/pin", {"key": key(), "pinned": False})[1]["pins"] == []


def test_export_import_and_migration(http_server):
    assert request(http_server, "/api/import", {"data": {"version": 1, "pins": [key()], "names": {key(): "Перенос"}}})[0] == 200
    status, exported, headers = request(http_server, "/api/export")
    assert status == 200 and "digests" not in exported
    assert headers["Content-Disposition"].startswith('attachment; filename="ai-threads-')
    exported["done"] = [key("grok")]
    assert request(http_server, "/api/import", {"data": exported})[1]["done"] == [key("grok")]
    migrated = request(http_server, "/api/migrate", {"pins": [key("claude")], "names": {key(): "Новое"}})[1]
    assert migrated["migrated_local_storage"] and migrated["pins"] == [key(), key("claude")]
    assert migrated["names"][key()] == "Новое"
    assert request(http_server, "/api/migrate", {"pins": [key("claude")], "names": {key(): "Новое"}})[1] == migrated


def test_search_scope(http_server):
    query = "/api/search?q=" + quote("Запись найдена")
    all_results = request(http_server, query + "&scope=all")[1]
    mine = request(http_server, query + "&scope=mine")[1]
    assert len(all_results) == 9 and len(mine) == 5
    request(http_server, "/api/hide", {"key": key(), "hidden": True})
    assert len(request(http_server, query)[1]) == 4
    assert len(request(http_server, query + "&scope=all")[1]) == 9
    assert all(len(r["snippet"]) <= 160 for r in all_results)


@pytest.mark.parametrize("path,body", [
    ("/api/pin", {"key": "codex:../../etc/passwd", "pinned": True}),
    ("/api/pin", {"key": key(), "pinned": "true"}),
    ("/api/name", {"key": key(), "name": "x" * 201}),
    ("/api/import", {"data": {"version": 2, "names": {"../bad": "Название"}}}),
    ("/api/migrate", {"pins": [], "names": []}),
    ("/api/digest", {"days": True, "model": "kimi"}),
])
def test_invalid_post_does_not_mutate(http_server, path, body):
    before = http_server.store.state()
    assert request(http_server, path, body)[0] == 400
    assert http_server.store.state() == before


def test_invalid_json_and_missing_session(http_server):
    for raw in (b"null", b"[]", b"{broken", b'{"key":NaN}'):
        assert request(http_server, "/api/pin", raw=raw)[0] == 400
    assert request(http_server, "/api/session?key=codex:unknown")[0] == 404
    assert request(http_server, "/api/resolve?key=../../etc/passwd")[0] == 400
    assert request(http_server, "/api/unknown")[0] == 404


def test_static_files_cannot_escape_root(http_server):
    assert "Нить" in request(http_server, "/")[1]
    assert "javascript" in request(http_server, "/app.js")[2]["Content-Type"]
    assert request(http_server, "/data.json")[1] == {"value": 1}
    for path in ("/../state.json", "/%2e%2e/state.json", "/outside/codex/session_index.jsonl", "/%00"):
        assert request(http_server, path)[0] == 404
    assert request(http_server, "/api%2fstate")[2]["Cache-Control"] == "no-store"


def test_unconnected_modules_return_503(http_server, monkeypatch):
    def missing(name):
        raise ImportError(name)

    monkeypatch.setattr("ai_threads.server._module", missing)
    for path, body in [
        ("/api/jobs", None), ("/api/jobs/test/events", None), ("/api/jobs/test/cancel", {}),
        ("/api/live", None), ("/api/digest/models", None),
        ("/api/summary", {"key": key()}), ("/api/digest", {"days": 3, "model": "kimi"}),
        ("/api/reveal", {"key": key(), "app": "explorer"}),
        ("/api/restore-script?keys=" + key(), None),
    ]:
        assert request(http_server, path, body)[:2] == (503, {"error": "Модуль ещё не подключён"})


def test_module_signatures_and_ndjson_reconnect(http_server, monkeypatch):
    calls = []

    class Job:
        def __init__(self, job_id, kind, key):
            self.id, self.kind, self.key, self.started = job_id, kind, key, 1

        def events(self, since, timeout):
            events = [{"n": 1, "type": "progress"}, {"n": 2, "type": "result", "result": {}}]
            return [e for e in events if e["n"] > since], True

        def cancel(self):
            calls.append(("cancel", self.id))

    class Jobs:
        def __init__(self):
            self.items = {}

        def start(self, job_id, kind, key, target):
            if job_id not in self.items:
                self.items[job_id] = Job(job_id, kind, key)
                target(self.items[job_id])
            return self.items[job_id]

        def get(self, job_id):
            return self.items.get(job_id)

        def running(self):
            return list(self.items.values())

    def summary(job, session, current_title, candidates, store, **choice):
        calls.append(("summary", session.key, current_title, candidates))
        store.set_summary(session.key, {"title": "Сводка", "summary": "Результат"})

    def digest(job, sessions, days, model, titles, summaries, hidden, store, **choice):
        assert sessions and isinstance(titles, dict) and isinstance(summaries, dict)
        assert isinstance(hidden, list)
        store.set_digest(days, {"model": model, "result": {"text": "Готово"}})

    def reveal(session, app):
        calls.append(("reveal", session.key, app))

    modules = {"jobs": types.SimpleNamespace(Jobs=Jobs),
               "summary": types.SimpleNamespace(run_summary=summary),
               "digest": types.SimpleNamespace(run_digest=digest, models=lambda: [{"id": "kimi", "available": True}]),
               "live": types.SimpleNamespace(live_status=lambda sessions: {key(): {"state": "work", "where": sessions[key()].cwd, "since": 1}}),
               "terminal": types.SimpleNamespace(restore_script=lambda sessions, fmt, port: f"{fmt}:{port}: " + sessions[0].command,
                                                  reveal=reveal)}
    monkeypatch.setattr("ai_threads.server._module", modules.__getitem__)
    request(http_server, "/api/name", {"key": key(), "name": "Текущее"})
    assert request(http_server, "/api/summary", {"key": key()})[1] == {"job": "summary:" + key()}
    request(http_server, "/api/summary", {"key": key()})
    assert len([c for c in calls if c[0] == "summary"]) == 1
    assert calls[0][2] == "Текущее"
    assert request(http_server, "/api/jobs")[1][0]["job"] == "summary:" + key()
    events = request(http_server, "/api/jobs/summary:" + key() + "/events?since=1")[1]
    assert [json.loads(line)["n"] for line in events.splitlines()] == [2]
    assert request(http_server, "/api/jobs/summary:" + key() + "/cancel", {})[1] == {"cancelled": True}
    assert request(http_server, "/api/live")[1][key()]["state"] == "work"
    assert request(http_server, "/api/digest?days=3")[1] is None
    assert request(http_server, "/api/digest", {"days": 3, "model": "kimi"})[1] == {"job": "digest"}
    assert request(http_server, "/api/digest?days=3")[1]["model"] == "kimi"
    assert request(http_server, "/api/digest/models")[1][0]["available"]
    assert request(http_server, "/api/digest/tail", {"id": "tail-1", "done": True})[1]["digest_tails"] == {"tail-1": True}
    assert request(http_server, "/api/open", {"keys": [key()], "mode": "tmux"})[0] == 404
    assert request(http_server, "/api/restore-script?keys=" + key() + "&fmt=wt")[1].startswith(f"wt:{http_server.server_port}: cd --")
    assert request(http_server, "/api/reveal", {"key": key(), "app": "vscode"})[0] == 200
    assert ("reveal", key(), "vscode") in calls


@pytest.mark.parametrize("engine", ["chromium", "webkit"])
def test_placeholder_link_in_browser(http_server, engine):
    with sync_playwright() as playwright:
        try:
            browser = getattr(playwright, engine).launch()
        except Error as exc:
            if engine == "webkit" and ("Host system is missing dependencies" in str(exc) or "error while loading shared libraries" in str(exc)):
                pytest.skip("WebKit: отсутствуют системные библиотеки: " + str(exc).splitlines()[0])
            raise
        try:
            page = browser.new_page()
            requests = []
            page.on("request", lambda req: requests.append((req.method, req.url)))
            url = f"http://127.0.0.1:{http_server.server_port}"
            page.goto(url)
            assert page.get_by_role("heading", name="Нить").is_visible()
            page.get_by_role("link", name="Открыть каталог сессий").click()
            page.wait_for_url(url + "/api/sessions")
            assert '"ready": true' in page.locator("body").inner_text()
            page.reload()
            page.go_back()
            assert page.get_by_role("heading", name="Нить").is_visible()
            assert ("GET", url + "/api/sessions") in requests
            assert all(method == "GET" for method, _ in requests)
        finally:
            browser.close()


def test_smart_search_route(http_server, monkeypatch):
    import time
    from ai_threads import server as server_module
    calls = []

    def run_search(job, sessions, query, scope, hidden, names, summaries, **choice):
        calls.append((query, scope, len(sessions)))
        job.emit({"type": "result", "result": {"results": []}})

    real = server_module._module
    monkeypatch.setattr("ai_threads.server._module",
                        lambda name: types.SimpleNamespace(run_search=run_search) if name == "search" else real(name))
    assert request(http_server, "/api/smart-search", {"q": "  "})[0] == 400
    assert request(http_server, "/api/smart-search", {"q": "x" * 501})[0] == 400
    assert request(http_server, "/api/smart-search", {"q": "каталог", "scope": "nope"})[0] == 400
    first = request(http_server, "/api/smart-search", {"q": "каталог"})[1]["job"]
    second = request(http_server, "/api/smart-search", {"q": "каталог", "scope": "all"})[1]["job"]
    assert first != second and first.startswith("search:")
    deadline = time.monotonic() + 5
    while len(calls) < 2 and time.monotonic() < deadline:
        time.sleep(0.05)
    assert [c[:2] for c in calls] == [("каталог", "mine"), ("каталог", "all")] and calls[0][2] > 0


def test_summary_candidates_are_manual_sessions_from_the_same_folder(http_server, monkeypatch):
    from ai_threads import server as server_module
    seen = []

    def run_summary(job, session, title, candidates, store, **choice):
        seen.append([key for key, _ in candidates])

    real = server_module._module
    monkeypatch.setattr("ai_threads.server._module",
                        lambda name: types.SimpleNamespace(run_summary=run_summary) if name == "summary" else real(name))
    request(http_server, "/api/hide", {"key": key("grok"), "hidden": True})
    request(http_server, "/api/summary", {"key": key()})
    import time
    deadline = time.monotonic() + 5
    while not seen and time.monotonic() < deadline:
        time.sleep(0.05)
    candidates = set(seen[0])
    assert key("claude") in candidates
    assert key() not in candidates and key("grok") not in candidates
    assert not any(k.endswith("000000000002") for k in candidates)


def test_agent_reply_route_validates_input(http_server):
    assert request(http_server, "/api/agent/reply", {"agent": "zcode", "session": "s1", "text": "x"})[0] == 400
    assert request(http_server, "/api/agent/reply", {"agent": "kimi", "session": "../x", "text": "x"})[0] == 400
    assert request(http_server, "/api/agent/reply", {"agent": "kimi", "session": "s1", "text": " "})[0] == 400
    status, body, _ = request(http_server, "/api/agent/reply", {"agent": "kimi", "session": "s1", "text": "вопрос"})
    assert status == 200 and body["job"].startswith("reply:")


def test_models_route_and_choice_validation(http_server, monkeypatch):
    from ai_threads.agents import AGENTS
    monkeypatch.setattr(AGENTS["claude"], "models", lambda: {"default_model": "opus", "default_effort": "",
                                                            "models": [], "efforts": ["low"]})
    status, body, _ = request(http_server, "/api/models?agent=claude")
    assert status == 200 and body["agent"] == "claude" and body["default_model"] == "opus"
    assert request(http_server, "/api/models?agent=zcode")[0] == 400
    assert request(http_server, "/api/summary", {"key": key(), "model": "bad model"})[0] == 400
    assert request(http_server, "/api/smart-search", {"q": "x", "agent": "zcode"})[0] == 400
    assert request(http_server, "/api/digest", {"days": 3, "model": "kimi", "effort": "x;y"})[0] == 400


def test_full_search_route(http_server):
    import time
    assert request(http_server, "/api/full-search", {"q": " "})[0] == 400
    status, body, _ = request(http_server, "/api/full-search", {"q": "каталоге", "scope": "all"})
    assert status == 200 and body["job"].startswith("fullsearch:")
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        events = request(http_server, f"/api/jobs/{body['job']}/events?since=0")[1]
        if '"result"' in events:
            break
        time.sleep(0.1)
    result = [json.loads(line) for line in events.splitlines() if '"result"' in line][0]["result"]
    assert result["results"] and result["terms"] == ["каталоге"]
