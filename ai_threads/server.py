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

from . import __version__, config, i18n, runner, settings, sources
from .agents import AGENTS, RUNNERS
from .catalog import Catalog
from .store import SESSION_ID, Store, validate_days, validate_key


i18n.add({
    "server.bad_host": {"ru": "Недопустимый Host", "en": "Host is not allowed"},
    "server.bad_agent_session": {"ru": "Неверный агент или ID сессии", "en": "Invalid agent or session ID"},
    "server.bad_reply": {"ru": "Нужен текст сообщения до 20000 символов", "en": "Message text up to 20000 characters is required"},
    "server.bad_origin": {"ru": "Недопустимый Origin", "en": "Origin is not allowed"},
    "server.need_length": {"ru": "Нужен Content-Length", "en": "Content-Length is required"},
    "server.bad_length": {"ru": "Неверный размер тела", "en": "Invalid body size"},
    "server.too_large": {"ru": "Тело запроса слишком большое", "en": "Request body is too large"},
    "server.need_object": {"ru": "Нужен JSON-объект", "en": "A JSON object is required"},
    "server.not_connected": {"ru": "Модуль ещё не подключён", "en": "This module is not connected yet"},
    "server.failed": {"ru": "Не удалось выполнить запрос", "en": "The request failed"},
    "server.no_session": {"ru": "Сессия не найдена", "en": "Session not found"},
    "server.need_keys": {"ru": "Нужен список ключей сессий", "en": "A list of session keys is required"},
    "server.no_job": {"ru": "Задание не найдено", "en": "Job not found"},
    "server.bad_scope": {"ru": "Неверная область поиска", "en": "Invalid search scope"},
    "server.bad_since": {"ru": "Неверный номер события", "en": "Invalid event number"},
    "server.bad_format": {"ru": "Неверный формат скрипта", "en": "Invalid script format"},
    "server.no_route": {"ru": "Маршрут не найден", "en": "Route not found"},
    "server.need_field": {"ru": "Нужно поле {field}", "en": "Field {field} is required"},
    "server.bad_model": {"ru": "Неверная модель", "en": "Invalid model"},
    "server.bad_query": {"ru": "Нужен запрос до 500 символов", "en": "The query must be 1 to 500 characters"},
    "server.bad_app": {"ru": "Неверное приложение", "en": "Invalid app"},
    "server.no_file": {"ru": "Файл не найден", "en": "File not found"},
})


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
        # Язык сообщений этого запроса: настройка language или язык браузера.
        i18n.use(i18n.resolve(self.headers.get("Accept-Language"), request=True))
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
            raise HTTPError(403, i18n.t("server.bad_host"))
        if self.command == "POST" and self.headers.get_all("Origin", []) != [f"http://{hosts[0]}"]:
            raise HTTPError(403, i18n.t("server.bad_origin"))

    def _body(self, path):
        limit = 1024 * 1024 if path == "/api/import" else 16 * 1024
        lengths = self.headers.get_all("Content-Length", [])
        if self.headers.get("Transfer-Encoding") or len(lengths) != 1:
            raise HTTPError(400, i18n.t("server.need_length"))
        try:
            length = int(lengths[0])
        except ValueError:
            raise HTTPError(400, i18n.t("server.bad_length")) from None
        if length < 0:
            raise HTTPError(400, i18n.t("server.bad_length"))
        if length > limit:
            raise HTTPError(413, i18n.t("server.too_large"))
        try:
            body = json.loads(self.rfile.read(length), parse_constant=lambda value: (_ for _ in ()).throw(ValueError()))
        except (ValueError, UnicodeDecodeError):
            raise HTTPError(400, i18n.t("server.need_object")) from None
        if not isinstance(body, dict):
            raise HTTPError(400, i18n.t("server.need_object"))
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
                status, message = 503, i18n.t("server.not_connected")
            elif isinstance(exc, ValueError):
                status, message = 400, str(exc)
            else:
                status, message = 500, i18n.t("server.failed")
            self._send({"error": message}, status)

    def _session(self, key):
        validate_key(key)
        session = self.server.catalog.get(key)
        if session is None:
            raise HTTPError(404, i18n.t("server.no_session"))
        return session

    def _selected(self, keys):
        if not isinstance(keys, list) or not keys:
            raise ValueError(i18n.t("server.need_keys"))
        return [self._session(key) for key in dict.fromkeys(validate_key(k) for k in keys)]

    def _job(self, job_id):
        job = self.server.jobs.get(job_id)
        if job is None:
            raise HTTPError(404, i18n.t("server.no_job"))
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
                raise ValueError(i18n.t("server.bad_scope"))
            hidden = set(store.state()["hidden"])
            keys = [s.key for s in catalog.sessions() if scope == "all" or (not s.auto and not s.temp and s.key not in hidden)]
            self._send(catalog.search(arg("q"), keys))
        elif path == "/api/jobs":
            self._send([{"job": j.id, "kind": j.kind, "key": j.key, "started": j.started,
                         **({"q": j.query} if getattr(j, "query", None) else {})} for j in self.server.jobs.running()])
        elif path.startswith("/api/jobs/") and path.endswith("/events"):
            since = int(arg("since", "0"))
            if since < 0:
                raise ValueError(i18n.t("server.bad_since"))
            self._events(self._job(path[len("/api/jobs/"):-len("/events")]), since)
        elif path == "/api/live":
            self._send(_module("live").live_status({s.key: s for s in catalog.sessions()}))
        elif path == "/api/restore-script":
            fmt = arg("fmt", "plain")
            if fmt not in ("wt", "tmux", "plain"):
                raise ValueError(i18n.t("server.bad_format"))
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
        elif path == "/api/models":
            agent_id = arg("agent")
            if agent_id not in RUNNERS:
                raise ValueError(i18n.t("server.bad_model"))
            self._send({"agent": agent_id, **runner.models(AGENTS[agent_id])})
        elif path == "/api/digest":
            self._send(store.digest(validate_days(int(arg("days", "3")))))
        elif path.startswith("/api/"):
            raise HTTPError(404, i18n.t("server.no_route"))
        else:
            self._static(path)

    def _post(self, path, body):
        catalog, store = self.server.catalog, self.server.store
        flags = {"/api/pin": (store.set_pin, "pinned"), "/api/done": (store.set_done, "done"),
                 "/api/hide": (store.set_hidden, "hidden"), "/api/name": (store.set_name, "name")}
        if path in flags:
            setter, field = flags[path]
            if field not in body:
                raise ValueError(i18n.t("server.need_field", field=field))
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
            agent_id, model, effort = self._choice(body)
            job = self.server.jobs.start(f"summary:{session.key}", "summary", session.key,
                                         lambda job: run(job, session, title, candidates, store,
                                                         agent_id=agent_id, model=model, effort=effort))
            self._send({"job": job.id})
        elif path.startswith("/api/jobs/") and path.endswith("/cancel"):
            job = self._job(path[len("/api/jobs/"):-len("/cancel")])
            job.cancel()
            self._send({"cancelled": True})
        elif path == "/api/digest":
            days = validate_days(body.get("days"))
            model = body.get("model")
            if model not in RUNNERS:
                raise ValueError(i18n.t("server.bad_model"))
            run = _module("digest").run_digest
            state = store.state()
            sessions = catalog.sessions()
            agent_model, effort = runner.choice(body.get("agent_model"), body.get("effort"))
            job = self.server.jobs.start("digest", "digest", None,
                                         lambda job: run(job, sessions, days, model, state["names"], state["summaries"],
                                                         state["hidden"], store, agent_model=agent_model, effort=effort))
            self._send({"job": job.id})
        elif path == "/api/full-search":
            # Полный поиск без агента: журналы целиком. Новый запрос останавливает идущий.
            query = body.get("q")
            scope = body.get("scope", "mine")
            if not isinstance(query, str) or not query.strip() or len(query) > 500:
                raise ValueError(i18n.t("server.bad_query"))
            if scope not in ("mine", "all"):
                raise ValueError(i18n.t("server.bad_scope"))
            jobs = self.server.jobs
            for job in jobs.running():
                if job.kind == "fullsearch":
                    job.cancel()
            with self.server._jobs_lock:
                self.server.search_count += 1
                job_id = f"fullsearch:{self.server.search_count}"
            run = _module("search").run_full_search
            state = store.state()
            sessions = catalog.sessions()
            job = jobs.start(job_id, "fullsearch", None,
                             lambda job: run(job, sessions, query, scope, state["hidden"], state["names"],
                                             state["summaries"]))
            job.query = query
            self._send({"job": job.id})
        elif path == "/api/smart-search":
            query = body.get("q")
            scope = body.get("scope", "mine")
            if not isinstance(query, str) or not query.strip() or len(query) > 500:
                raise ValueError(i18n.t("server.bad_query"))
            if scope not in ("mine", "all"):
                raise ValueError(i18n.t("server.bad_scope"))
            jobs = self.server.jobs
            # Новый запрос заменяет идущий: старый поиск останавливается.
            for job in jobs.running():
                if job.kind == "search":
                    job.cancel()
            with self.server._jobs_lock:
                self.server.search_count += 1
                job_id = f"search:{self.server.search_count}"
            run = _module("search").run_search
            agent_id, model, effort = self._choice(body)
            state = store.state()
            sessions = catalog.sessions()
            job = jobs.start(job_id, "search", None,
                             lambda job: run(job, sessions, query, scope, state["hidden"], state["names"],
                                             state["summaries"], agent_id=agent_id, model=model, effort=effort))
            # Страница, открытая в другой вкладке, показывает запрос идущего поиска.
            job.query = query
            self._send({"job": job.id})
        elif path == "/api/agent/reply":
            # Ответ человека в сессию агента, начатую сводкой или умным поиском.
            agent_id, session_id, text = body.get("agent"), body.get("session"), body.get("text")
            if agent_id not in RUNNERS or not isinstance(session_id, str) or not SESSION_ID.fullmatch(session_id):
                raise ValueError(i18n.t("server.bad_agent_session"))
            if not isinstance(text, str) or not text.strip() or len(text) > 20000:
                raise ValueError(i18n.t("server.bad_reply"))
            jobs = self.server.jobs
            with self.server._jobs_lock:
                self.server.search_count += 1
                job_id = f"reply:{self.server.search_count}"
            reply = _module("runner").reply
            job = jobs.start(job_id, "reply", session_id, lambda job: reply(job, agent_id, session_id, text.strip()))
            self._send({"job": job.id})
        elif path == "/api/digest/tail":
            self._send(store.set_tail(body.get("id"), body.get("done")))
        elif path == "/api/settings":
            settings.save(body.get("settings"))
            # Настройка language могла поменяться: ответ уже на новом языке.
            i18n.use(i18n.resolve(self.headers.get("Accept-Language"), request=True))
            catalog.refresh(force=True)
            catalog.wait(10)
            self._send(self._config())
        elif path == "/api/reveal":
            session = self._session(body.get("key"))
            app = body.get("app")
            if app not in ("explorer", "vscode"):
                raise ValueError(i18n.t("server.bad_app"))
            try:
                _module("terminal").reveal(session, app)
            except (FileNotFoundError, RuntimeError) as error:
                raise HTTPError(409, str(error)) from None
            self._send({"ok": True})
        else:
            raise HTTPError(404, i18n.t("server.no_route"))

    def _choice(self, body):
        """Агент, модель и уровень рассуждений из запроса: пустые — по настройкам и CLI."""
        agent_id = body.get("agent") or None
        if agent_id is not None and agent_id not in RUNNERS:
            raise ValueError(i18n.t("server.bad_model"))
        return (agent_id, *runner.choice(body.get("model"), body.get("effort")))

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
                "data_dir": str(config.data_dir()), "port": self.server.server_port, "version": __version__,
                "language": i18n.current()}

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
                raise HTTPError(404, i18n.t("server.no_file"))
            content = file.read_bytes()
        except (OSError, ValueError):
            raise HTTPError(404, i18n.t("server.no_file")) from None
        mime = mimetypes.guess_type(file.name)[0] or "application/octet-stream"
        if mime.startswith("text/") or mime in ("application/javascript", "application/json"):
            mime += "; charset=utf-8"
        self._send(content, content_type=mime, headers={"Cache-Control": "no-cache"})


def create_server(port=8765, **kwargs) -> Server:
    return Server(port, **kwargs)
