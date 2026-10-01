"""HTTP API приложения; сервер доступен только через loopback."""

import importlib
import json
import mimetypes
import threading
import time
from datetime import date
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit

from . import __version__, config, runner, settings, sources
from .agents import AGENTS, RUNNERS
from .catalog import Catalog
from .store import Store, validate_days, validate_key


class HTTPError(Exception):
    def __init__(self, status, message):
        self.status, self.message = status, message


def _module(name):
    return importlib.import_module(f"ai_threads.{name}")


class Server(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, port=8765, *, catalog=None, store=None, static=None):
        store = store if store is not None else Store(config.data_dir() / "state.json")
        # Порт занимается до чтения журналов: при занятом порте фоновое чтение не нужно.
        super().__init__(("127.0.0.1", port), Handler)
        self.store = store
        self.catalog = catalog if catalog is not None else Catalog()
        self.static = Path(static) if static is not None else config.STATIC
        self._jobs = None
        self._jobs_lock = threading.Lock()
        self.search_count = 0

    @property
    def jobs(self):
        with self._jobs_lock:
            if self._jobs is None:
                self._jobs = _module("jobs").Jobs()
            return self._jobs


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format, *args):
        pass

    def _send(self, value, status=200, *, content_type="application/json; charset=utf-8", headers=None):
        if isinstance(value, bytes):
            body = value
        elif content_type.startswith("application/json"):
            body = json.dumps(value, ensure_ascii=False, allow_nan=False).encode("utf-8")
        else:
            body = value.encode("utf-8") if isinstance(value, str) else value
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("X-Content-Type-Options", "nosniff")
        if unquote(urlsplit(self.path).path).startswith("/api/"):
            self.send_header("Cache-Control", "no-store")
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def parse_request(self):
        if not super().parse_request():
            return False
        try:
            self._security()
        except HTTPError as exc:
            self.close_connection = True
            self._send({"error": exc.message}, exc.status)
            return False
        return True

    def _security(self):
        hosts = self.headers.get_all("Host", [])
        port = self.server.server_port
        if len(hosts) != 1 or hosts[0] not in (f"127.0.0.1:{port}", f"localhost:{port}"):
            raise HTTPError(403, "Недопустимый Host")
        if self.command == "POST" and self.headers.get_all("Origin", []) != [f"http://{hosts[0]}"]:
            raise HTTPError(403, "Недопустимый Origin")

    def _body(self, path):
        limit = 1024 * 1024 if path == "/api/import" else 16 * 1024
        lengths = self.headers.get_all("Content-Length", [])
        if self.headers.get("Transfer-Encoding") or len(lengths) != 1:
            raise HTTPError(400, "Нужен Content-Length")
        try:
            length = int(lengths[0])
        except ValueError:
            raise HTTPError(400, "Неверный размер тела") from None
        if length < 0:
            raise HTTPError(400, "Неверный размер тела")
        if length > limit:
            raise HTTPError(413, "Тело запроса слишком большое")
        try:
            body = json.loads(self.rfile.read(length), parse_constant=lambda value: (_ for _ in ()).throw(ValueError()))
        except (ValueError, UnicodeDecodeError):
            raise HTTPError(400, "Нужен JSON-объект") from None
        if not isinstance(body, dict):
            raise HTTPError(400, "Нужен JSON-объект")
        return body

    def do_GET(self):
        self._handle()

    def do_POST(self):
        self._handle()

    def _handle(self):
        try:
            parsed = urlsplit(self.path)
            path = unquote(parsed.path)
            query = parse_qs(parsed.query, keep_blank_values=True)
            if self.command == "POST":
                self._post(path, self._body(path))
            else:
                self._get(path, query)
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True
        except Exception as exc:
            self.close_connection = True
            if isinstance(exc, HTTPError):
                status, message = exc.status, exc.message
            elif isinstance(exc, ImportError):
                status, message = 503, "Модуль ещё не подключён"
            elif isinstance(exc, ValueError):
                status, message = 400, str(exc)
            else:
                status, message = 500, "Не удалось выполнить запрос"
            self._send({"error": message}, status)

    def _session(self, key):
        validate_key(key)
        session = self.server.catalog.get(key)
        if session is None:
            raise HTTPError(404, "Сессия не найдена")
        return session

    def _selected(self, keys):
        if not isinstance(keys, list) or not keys:
            raise ValueError("Нужен список ключей сессий")
        return [self._session(key) for key in dict.fromkeys(validate_key(k) for k in keys)]

    def _job(self, job_id):
        job = self.server.jobs.get(job_id)
        if job is None:
            raise HTTPError(404, "Задание не найдено")
        return job

    def _get(self, path, query):
        catalog, store = self.server.catalog, self.server.store
        arg = lambda name, default="": query.get(name, [default])[0]
        if path == "/api/sessions":
            ready = catalog.ready
            sessions = catalog.sessions()
            self._send({"ready": ready, "progress": catalog.progress(),
                        "sessions": [s.to_json() for s in sessions], "port": self.server.server_port})
        elif path in ("/api/state", "/api/export"):
            headers = {"Content-Disposition": f'attachment; filename="ai-threads-{date.today().isoformat()}.json"'} if path.endswith("export") else None
            self._send(store.export(), headers=headers)
        elif path == "/api/session":
            self._send(sources.transcript(self._session(arg("key"))))
        elif path == "/api/resolve":
            session = self._session(arg("key"))
            self._send({name: getattr(session, name) for name in ("cwd", "command", "command_no_cd", "tool", "project")})
        elif path == "/api/search":
            scope = arg("scope", "mine")
            if scope not in ("mine", "all"):
                raise ValueError("Неверная область поиска")
            hidden = set(store.state()["hidden"])
            keys = [s.key for s in catalog.sessions() if scope == "all" or (not s.auto and not s.temp and s.key not in hidden)]
            self._send(catalog.search(arg("q"), keys))
        elif path == "/api/jobs":
            self._send([{"job": j.id, "kind": j.kind, "key": j.key, "started": j.started,
                         **({"q": j.query} if getattr(j, "query", None) else {})} for j in self.server.jobs.running()])
        elif path.startswith("/api/jobs/") and path.endswith("/events"):
            since = int(arg("since", "0"))
            if since < 0:
                raise ValueError("Неверный номер события")
            self._events(self._job(path[len("/api/jobs/"):-len("/events")]), since)
        elif path == "/api/live":
            self._send(_module("live").live_status({s.key: s for s in catalog.sessions()}))
        elif path == "/api/restore-script":
            fmt = arg("fmt", "plain")
            if fmt not in ("wt", "tmux", "plain"):
                raise ValueError("Неверный формат скрипта")
            sessions = self._selected(arg("keys").split(","))
            self._send(_module("terminal").restore_script(sessions, fmt, self.server.server_port), content_type="text/plain; charset=utf-8")
        elif path == "/api/config":
            if arg("refresh") == "1":
                # «Проверить снова»: перечитать журналы, чтобы число сессий было свежим.
                catalog.refresh(force=True)
                catalog.wait(10)
            self._send(self._config())
        elif path == "/api/digest/models":
            self._send(_module("digest").models())
        elif path == "/api/digest":
            self._send(store.digest(validate_days(int(arg("days", "3")))))
        elif path.startswith("/api/"):
            raise HTTPError(404, "Маршрут не найден")
        else:
            self._static(path)

    def _post(self, path, body):
        catalog, store = self.server.catalog, self.server.store
        flags = {"/api/pin": (store.set_pin, "pinned"), "/api/done": (store.set_done, "done"),
                 "/api/hide": (store.set_hidden, "hidden"), "/api/name": (store.set_name, "name")}
        if path in flags:
            setter, field = flags[path]
            if field not in body:
                raise ValueError(f"Нужно поле {field}")
            self._send(setter(body.get("key"), body[field]))
        elif path == "/api/import":
            self._send(store.import_(body.get("data")))
        elif path == "/api/migrate":
            self._send(store.migrate(body.get("pins"), body.get("names")))
        elif path == "/api/summary":
            session = self._session(body.get("key"))
            run = _module("summary").run_summary
            state = store.state()
            title = state["names"].get(session.key, session.title)
            # Модели уходят только названия ручных сессий из той же папки за 30 дней.
            hidden = set(state["hidden"])
            candidates = [(s.key, state["names"].get(s.key, s.title)) for s in catalog.sessions()
                          if s.key != session.key and session.cwd and s.cwd == session.cwd
                          and not (s.auto or s.temp or s.key in hidden)
                          and s.updated >= time.time() - 30 * 86400][:30]
            job = self.server.jobs.start(f"summary:{session.key}", "summary", session.key,
                                         lambda job: run(job, session, title, candidates, store))
            self._send({"job": job.id})
        elif path.startswith("/api/jobs/") and path.endswith("/cancel"):
            job = self._job(path[len("/api/jobs/"):-len("/cancel")])
            job.cancel()
            self._send({"cancelled": True})
        elif path == "/api/digest":
            days = validate_days(body.get("days"))
            model = body.get("model")
            if model not in RUNNERS:
                raise ValueError("Неверная модель")
            run = _module("digest").run_digest
            state = store.state()
            sessions = catalog.sessions()
            job = self.server.jobs.start("digest", "digest", None,
                                         lambda job: run(job, sessions, days, model, state["names"], state["summaries"], state["hidden"], store))
            self._send({"job": job.id})
        elif path == "/api/smart-search":
            query = body.get("q")
            scope = body.get("scope", "mine")
            if not isinstance(query, str) or not query.strip() or len(query) > 500:
                raise ValueError("Нужен запрос до 500 символов")
            if scope not in ("mine", "all"):
                raise ValueError("Неверная область поиска")
            jobs = self.server.jobs
            # Новый запрос заменяет идущий: старый поиск останавливается.
            for job in jobs.running():
                if job.kind == "search":
                    job.cancel()
            with self.server._jobs_lock:
                self.server.search_count += 1
                job_id = f"search:{self.server.search_count}"
            run = _module("search").run_search
            state = store.state()
            sessions = catalog.sessions()
            job = jobs.start(job_id, "search", None,
                             lambda job: run(job, sessions, query, scope, state["hidden"], state["names"],
                                             state["summaries"]))
            # Страница, открытая в другой вкладке, показывает запрос идущего поиска.
            job.query = query
            self._send({"job": job.id})
        elif path == "/api/digest/tail":
            self._send(store.set_tail(body.get("id"), body.get("done")))
        elif path == "/api/settings":
            settings.save(body.get("settings"))
            catalog.refresh(force=True)
            catalog.wait(10)
            self._send(self._config())
        elif path == "/api/reveal":
            session = self._session(body.get("key"))
            app = body.get("app")
            if app not in ("explorer", "vscode"):
                raise ValueError("Неверное приложение")
            try:
                _module("terminal").reveal(session, app)
            except (FileNotFoundError, RuntimeError) as error:
                raise HTTPError(409, str(error)) from None
            self._send({"ok": True})
        else:
            raise HTTPError(404, "Маршрут не найден")

    def _config(self):
        """Агенты с найденными папками и программами, окружение и настройки."""
        from .terminal import environment
        counts = {}
        for session in self.server.catalog.sessions():
            counts[session.tool] = counts.get(session.tool, 0) + 1
        return {"agents": [agent.to_json(counts.get(agent.id, 0)) for agent in AGENTS.values()],
                "runners": runner.available(), "runner_ids": list(RUNNERS), "env": environment(),
                "settings": settings.effective(), "stored": settings.stored(), "defaults": settings.DEFAULTS,
                "settings_path": str(settings.path()), "settings_error": settings.error(),
                "data_dir": str(config.data_dir()), "port": self.server.server_port, "version": __version__}

    def _events(self, job, since):
        self.send_response(200)
        self.send_header("Content-Type", "application/x-ndjson; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        while True:
            events, done = job.events(since, timeout=1.0)
            for event in events:
                self.wfile.write((json.dumps(event, ensure_ascii=False) + "\n").encode("utf-8"))
                since = event["n"]
            self.wfile.flush()
            if done:
                return

    def _static(self, path):
        root = self.server.static.resolve()
        try:
            file = (root / (path.lstrip("/") or "index.html")).resolve()
            if not file.is_relative_to(root) or not file.is_file():
                raise HTTPError(404, "Файл не найден")
            content = file.read_bytes()
        except (OSError, ValueError):
            raise HTTPError(404, "Файл не найден") from None
        mime = mimetypes.guess_type(file.name)[0] or "application/octet-stream"
        if mime.startswith("text/") or mime in ("application/javascript", "application/json"):
            mime += "; charset=utf-8"
        self._send(content, content_type=mime, headers={"Cache-Control": "no-cache"})


def create_server(port=8765, **kwargs) -> Server:
    return Server(port, **kwargs)
