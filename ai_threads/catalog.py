"""Каталог с фоновым чтением и кешем по размеру и времени изменения файла."""

import copy
import json
import os
import tempfile
import threading
import time
from pathlib import Path

from . import config, sources
from .model import Session
from .sources.common import SEARCH_BYTES, fingerprint


class Catalog:
    def __init__(self, path: Path | None = None):
        self.path = Path(path) if path is not None else config.data_dir() / "cache.json"
        self._lock = threading.RLock()
        self._sessions = {}
        self._cache = {}
        self._search_cache = {}
        self._ready = False
        self._progress = {"done": 0, "total": 0, "tools": {}}
        self._thread = None
        self._again = False
        self._last_scan = float("-inf")
        self.error = None
        self.refresh()

    @property
    def ready(self) -> bool:
        with self._lock:
            return self._ready

    def refresh(self, *, force=False):
        with self._lock:
            if self._thread is not None and self._thread.is_alive():
                # Настройки поменялись посреди чтения: тот же поток прочитает всё ещё раз.
                self._again = self._again or force
                return
            if not force and time.monotonic() - self._last_scan < 3:
                return
            self._last_scan = time.monotonic()
            self._thread = threading.Thread(target=self._scan, name="nit-catalog", daemon=True)
            self._thread.start()

    def wait(self, timeout: float = 30) -> bool:
        with self._lock:
            thread = self._thread
        if thread:
            thread.join(timeout)
        return self.ready and not thread.is_alive()

    def _scan(self):
        while True:
            self._scan_once()
            with self._lock:
                if not self._again:
                    return
                self._again = False
                self._last_scan = time.monotonic()

    def _scan_once(self):
        try:
            if not self._ready:
                try:
                    data = json.loads(self.path.read_text(encoding="utf-8"))
                    if isinstance(data, dict):
                        self._cache = data
                except (OSError, ValueError):
                    pass
            from .agents import active
            tools = [agent.id for agent in active()]
            paths = {tool: sources.discover(tool) for tool in tools}
            with self._lock:
                self._progress = {"done": 0, "total": sum(map(len, paths.values())),
                                  "tools": dict.fromkeys(tools, "waiting")}
            sessions = {}

            def on_read(found):
                with self._lock:
                    self._progress["done"] += 1
                    if not self._ready:
                        for session in found:
                            self._sessions[session.key] = session

            for tool in tools:
                with self._lock:
                    self._progress["tools"][tool] = "reading"
                for session in sources.scan(tool, self._cache, paths=paths[tool], on_read=on_read):
                    sessions[session.key] = session
                with self._lock:
                    self._progress["tools"][tool] = "done"
            existing = {str(p) for values in paths.values() for p in values}
            self._cache = {k: v for k, v in self._cache.items()
                           if isinstance(v, dict) and (k in existing or v.get("kind"))}
            with self._lock:
                self._sessions = sessions
                self._search_cache = {k: v for k, v in self._search_cache.items() if k in sessions}
                self._ready = True
                self.error = None
            self._save_cache()
        except Exception as exc:
            # Ошибка фонового потока доступна вызывающему коду и не скрывает уже прочитанные сессии.
            self.error = str(exc)

    def _save_cache(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        fd, name = tempfile.mkstemp(prefix=".cache-", suffix=".tmp", dir=self.path.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as stream:
                json.dump(self._cache, stream, ensure_ascii=False, allow_nan=False)
            os.replace(name, self.path)
        finally:
            if os.path.exists(name):
                os.unlink(name)

    def sessions(self) -> list[Session]:
        self.refresh()
        with self._lock:
            return sorted(self._sessions.values(), key=lambda s: s.updated, reverse=True)

    def get(self, key: str) -> Session | None:
        with self._lock:
            return self._sessions.get(key)

    def progress(self) -> dict:
        with self._lock:
            return copy.deepcopy(self._progress)

    def search(self, q: str, keys) -> list[dict]:
        needle = q.strip().lower()
        if not needle:
            return []
        result = []
        for key in keys:
            session = self.get(key)
            if session is None:
                continue
            # У баз SQLite в режиме WAL основной файл меняется не сразу: время из каталога
            # отмечает новые сообщения раньше, чем размер и время изменения файла.
            stamp = (fingerprint(session.journal), session.updated)
            with self._lock:
                cached = self._search_cache.get(key)
            if cached is None or cached[0] != stamp:
                from .agents import get
                rows, _ = get(session.tool).transcript(session, SEARCH_BYTES, text_limit=None)
                text = "\n".join(r["text"] for r in rows)
                cached = (stamp, text)
                with self._lock:
                    self._search_cache[key] = cached
            text = cached[1]
            index = text.lower().find(needle)
            if index >= 0:
                start = max(0, index - 50)
                result.append({"key": key, "snippet": text[start:start + 160]})
        return result
