"""Фоновые задания: события с номерами, повторное подключение, отмена, предел времени.

События задания нумеруются с 1 и хранятся в памяти, поэтому клиент может
отключиться и подключиться снова, передав номер последнего виденного события.
"""

from __future__ import annotations

import contextvars
import os
import re
import signal
import subprocess
import threading
import time
from collections import deque
from typing import Callable

from . import i18n

#: Длина хвоста вывода процесса в событиях об ошибках.
TAIL_CHARS = 1200

_ANSI_RE = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")

i18n.add({
    "jobs.unexpected": {"ru": "Неожиданная ошибка задания: {error}", "en": "Unexpected job error: {error}"},
})


class _Tail:
    """Потокобезопасный хвост последних строк вывода без ANSI-кодов."""

    def __init__(self, limit: int):
        self._limit = limit
        self._lines: deque[str] = deque()
        self._size = 0
        self._lock = threading.Lock()

    def add(self, line: str) -> None:
        line = _ANSI_RE.sub("", line).rstrip("\r")
        with self._lock:
            self._lines.append(line)
            self._size += len(line) + 1
            while self._size > self._limit and len(self._lines) > 1:
                self._size -= len(self._lines.popleft()) + 1

    def text(self) -> str:
        with self._lock:
            return "\n".join(self._lines)[-self._limit:]


def _kill_tree(process: subprocess.Popen) -> None:
    """Убить процесс и его потомков: всё дерево через /proc и всю группу."""
    pid = process.pid
    for child in _descendants(pid):
        _signal_kill(child)
    _signal_kill(pid)
    try:
        os.killpg(pid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError, OSError):
        pass


def _signal_kill(pid: int) -> None:
    try:
        os.kill(pid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError, OSError):
        pass


def _descendants(pid: int) -> list[int]:
    """PID всех потомков процесса по /proc (только Linux)."""
    table: dict[int, int] = {}
    try:
        entries = os.listdir("/proc")
    except OSError:
        return []
    for entry in entries:
        if not entry.isdigit():
            continue
        try:
            with open(f"/proc/{entry}/stat", encoding="ascii", errors="replace") as fh:
                stat = fh.read()
            # формат: "pid (comm) state ppid ..."; comm может содержать скобки
            rest = stat[stat.rfind(")") + 2:].split()
            table[int(entry)] = int(rest[1])
        except (OSError, ValueError, IndexError):
            continue
    out: list[int] = []
    stack = [pid]
    while stack:
        parent = stack.pop()
        for candidate, ppid in table.items():
            if ppid == parent:
                out.append(candidate)
                stack.append(candidate)
    return out


class Job:
    """Одно задание: поток событий, отметка об отмене, запуск процесса."""

    def __init__(self, job_id: str, kind: str, key: str | None = None):
        self.id = job_id
        self.kind = kind
        self.key = key
        self.started = time.time()
        self._t0 = time.monotonic()
        self._cond = threading.Condition()
        self._events: list[dict] = []
        self._next_n = 1
        self._done = False
        self._cancelled = False
        self._proc_lock = threading.Lock()
        self._process: subprocess.Popen | None = None

    @property
    def cancelled(self) -> bool:
        return self._cancelled

    @property
    def finished(self) -> bool:
        with self._cond:
            return self._done

    def emit(self, event: dict) -> dict:
        """Добавить событие, проставив порядковый номер и время с запуска."""
        with self._cond:
            out = dict(event)
            out["n"] = self._next_n
            out["elapsed"] = int(time.monotonic() - self._t0)
            self._next_n += 1
            self._events.append(out)
            self._cond.notify_all()
            return out

    def events(self, since: int, timeout: float) -> tuple[list[dict], bool]:
        """События с номером больше `since`.

        Ждёт новые события не дольше `timeout` секунд. Возвращает пару
        (события, задание закончено); после завершения задания с непустым
        хвостом событий следующий вызов вернёт пустой список и done=True.
        """
        deadline = time.monotonic() + max(0.0, timeout)
        with self._cond:
            while True:
                fresh = [e for e in self._events if e["n"] > since]
                if fresh:
                    return fresh, self._done
                if self._done:
                    return [], True
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return [], False
                self._cond.wait(remaining)

    def _mark_done(self) -> None:
        with self._cond:
            self._done = True
            self._cond.notify_all()

    def cancel(self) -> None:
        """Отменить задание и убить запущенный процесс с потомками."""
        self._cancelled = True
        with self._proc_lock:
            process = self._process
        if process is not None and process.poll() is None:
            _kill_tree(process)

    def run_process(
        self,
        argv,
        cwd=None,
        timeout: float | None = None,
        on_line: Callable[[str], None] | None = None,
        input_text: str | None = None,
    ) -> tuple[int, str]:
        """Запустить процесс без оболочки и дождаться завершения.

        Каждая строка stdout передаётся в `on_line`. `input_text`, если дан,
        пишется в stdin процесса (промпт для CLI, которые читают его там).
        Возвращает пару (код завершения, хвост вывода stderr и stdout до
        TAIL_CHARS символов). По таймауту и по cancel() процесс и его
        потомки убиваются; в этом случае код завершения — -9 (SIGKILL).
        """
        argv = [str(a) for a in argv]
        tail = _Tail(TAIL_CHARS)
        if self._cancelled:
            return -9, tail.text()
        process = subprocess.Popen(
            argv,
            cwd=str(cwd) if cwd else None,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            stdin=subprocess.PIPE if input_text is not None else None,
            text=True,
            encoding="utf-8",
            errors="replace",
            start_new_session=True,
        )
        with self._proc_lock:
            self._process = process
        if self._cancelled:
            _kill_tree(process)
        if input_text is not None and process.stdin is not None:
            def write_stdin() -> None:
                try:
                    process.stdin.write(input_text)
                    process.stdin.close()
                except (BrokenPipeError, OSError, ValueError):
                    pass
            writer = threading.Thread(target=write_stdin, daemon=True)
            writer.start()

        def read(stream, is_stdout: bool) -> None:
            try:
                for line in iter(stream.readline, ""):
                    line = line.rstrip("\n")
                    tail.add(line)
                    if is_stdout and on_line is not None:
                        try:
                            on_line(line)
                        except Exception:
                            pass
            finally:
                stream.close()

        readers = []
        for stream, is_stdout in ((process.stdout, True), (process.stderr, False)):
            # on_line публикует события задания: читатель работает в контексте задания, с его языком.
            context = contextvars.copy_context()
            thread = threading.Thread(target=context.run, args=(read, stream, is_stdout), daemon=True)
            thread.start()
            readers.append(thread)

        deadline = time.monotonic() + timeout if timeout else None
        try:
            while process.poll() is None:
                if self._cancelled or (deadline is not None and time.monotonic() >= deadline):
                    _kill_tree(process)
                    process.wait()
                    break
                time.sleep(0.05)
        finally:
            with self._proc_lock:
                if self._process is process:
                    self._process = None
        for thread in readers:
            thread.join(timeout=5)
        code = process.returncode
        return (code if code is not None else -9), tail.text()


class Jobs:
    """Реестр заданий: запуск с защитой от повторов, поиск, отмена."""

    def __init__(self):
        self._lock = threading.Lock()
        self._jobs: dict[str, Job] = {}

    def start(self, job_id: str, kind: str, key: str | None,
              target: Callable[[Job], None]) -> Job:
        """Запустить задание. Идущее задание с тем же id не запускается
        второй раз — возвращается существующее."""
        with self._lock:
            existing = self._jobs.get(job_id)
            if existing is not None and not existing.finished:
                return existing
            job = Job(job_id, kind, key)
            self._jobs[job_id] = job
        # Задание наследует контекст запроса, который его запустил: прежде всего язык сообщений.
        context = contextvars.copy_context()
        thread = threading.Thread(target=context.run, args=(self._run, job, target),
                                  name=f"ai-threads-job-{job_id}", daemon=True)
        thread.start()
        return job

    def _run(self, job: Job, target: Callable[[Job], None]) -> None:
        try:
            target(job)
        except Exception as exc:  # задание не должно ронять поток молча
            if not job.cancelled:
                job.emit({"type": "error", "message": i18n.t("jobs.unexpected", error=exc)})
        finally:
            job._mark_done()

    def get(self, job_id: str) -> Job | None:
        with self._lock:
            return self._jobs.get(job_id)

    def running(self) -> list[Job]:
        with self._lock:
            jobs = list(self._jobs.values())
        return [job for job in jobs if not job.finished]

    def cancel(self, job_id: str) -> bool:
        job = self.get(job_id)
        if job is None or job.finished:
            return False
        job.cancel()
        return True
