"""Терминалы: агенты сессий в tmux и подключение к ним из страницы.

Агент живёт в сессии tmux, а не в процессе «Нити»: закрытая вкладка браузера, перезапуск «Нити»
или оборванная связь его не останавливают. Сервер tmux свой (сокет ai-threads, без ~/.tmux.conf),
поэтому настройки ниже не задевают tmux человека: префикса нет и Ctrl+B уходит агенту, строки
состояния нет, колесо мыши листает историю, а после выхода агента окно остаётся, чтобы был виден
код выхода. Каждая сессия «Нити» — своя сессия tmux с меткой @nit_key, ключом сессии.

Страница подключается через Attachment: `tmux attach-session` в псевдотерминале, байты которого
сервер передаёт по WebSocket.
"""

import fcntl
import hashlib
import os
import pty
import shlex
import shutil
import signal
import struct
import subprocess
import termios

from . import i18n
from .model import Session

SOCKET = "ai-threads"
# -q: настройки, которых нет в старом tmux, пропускаются молча, а не ломают запуск.
OPTIONS = (
    ("-sq", "escape-time", "0"),
    ("-sq", "set-clipboard", "on"),
    ("-asq", "terminal-features", ",xterm-256color:RGB:clipboard"),
    ("-gq", "prefix", "None"),
    ("-gq", "prefix2", "None"),
    ("-gq", "status", "off"),
    ("-gq", "mouse", "on"),
    ("-gq", "history-limit", "50000"),
    ("-gq", "remain-on-exit", "on"),
    ("-gq", "remain-on-exit-format", ""),
    ("-gq", "default-terminal", "tmux-256color"),
    ("-gq", "window-size", "latest"),
    ("-gq", "focus-events", "on"),
)
FORMAT = "\t".join(("#{session_name}", "#{@nit_key}", "#{pane_dead}", "#{pane_dead_status}",
                    "#{window_activity}", "#{session_created}"))

i18n.add({
    "tmux.missing": {"ru": "Для терминала нужен tmux. Установите его, например: sudo apt install tmux",
                     "en": "The terminal needs tmux. Install it, for example: sudo apt install tmux"},
    "tmux.no_command": {"ru": "Эту сессию нельзя продолжить из терминала",
                        "en": "This session can't be resumed from the terminal"},
    "tmux.no_folder": {"ru": "Папки проекта больше нет: агенту негде продолжить сессию",
                       "en": "The project folder is gone, so the agent has nowhere to resume the session"},
    "tmux.failed": {"ru": "Не удалось запустить терминал: {error}", "en": "Couldn't start the terminal: {error}"},
})


class TerminalError(RuntimeError):
    """Терминал нельзя открыть: нет tmux, папки или команды продолжения."""


def available() -> bool:
    return bool(shutil.which("tmux"))


def _base() -> list[str]:
    # AI_THREADS_TMUX_SOCKET — путь к сокету: тесты запускают свой сервер tmux и убирают его за собой.
    path = os.environ.get("AI_THREADS_TMUX_SOCKET")
    return ["tmux", "-S", path] if path else ["tmux", "-L", SOCKET]


def _env() -> dict:
    # Внутри tmux человека attach отказывается работать, пока задан TMUX.
    env = {k: v for k, v in os.environ.items() if k not in ("TMUX", "TMUX_PANE")}
    env["TERM"] = "xterm-256color"
    return env


def _run(args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(_base() + ["-f", os.devnull] + args, capture_output=True, text=True, env=_env(), timeout=10)


def _arg(value: str) -> str:
    """tmux режет команды по аргументу, который кончается на «;»: такая точка с запятой экранируется."""
    return value[:-1] + "\\;" if value.endswith(";") else value


def name_of(key: str) -> str:
    """Имя сессии tmux. Точку tmux в имени не принимает: у таких ID к имени добавляется хеш ключа."""
    tool, _, session_id = key.partition(":")
    name = f"nit-{tool}-{session_id}"
    if "." in name:
        name = name.replace(".", "_") + "-" + hashlib.sha1(key.encode()).hexdigest()[:6]
    return name


def attach_command(key: str) -> str:
    """Команда, которая подключит к тому же агенту любой другой терминал."""
    return shlex.join(_base() + ["attach-session", "-t", "=" + name_of(key)])


def terminals() -> dict[str, dict]:
    """Открытые терминалы: ключ сессии → {state: running|exited, code, activity, started, attach}."""
    if not available():
        return {}
    completed = _run(["list-panes", "-a", "-F", FORMAT])
    if completed.returncode != 0:
        return {}  # сервер tmux не запущен: терминалов нет
    found = {}
    for line in completed.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 6 or not parts[1] or parts[1] in found or name_of(parts[1]) != parts[0]:
            continue
        name, key, dead, code, activity, created = parts
        found[key] = {"state": "exited" if dead == "1" else "running",
                      "code": int(code) if dead == "1" and code.lstrip("-").isdigit() else None,
                      "activity": int(activity) if activity.isdigit() else 0,
                      "started": int(created) if created.isdigit() else 0,
                      "attach": attach_command(key)}
    return found


def open_terminal(session: Session, cols: int = 120, rows: int = 32) -> dict:
    """Запустить агента сессии в tmux. Идущий терминал остаётся как есть, завершённый запускается заново."""
    if not available():
        raise TerminalError(i18n.t("tmux.missing"))
    key = session.key
    current = terminals().get(key)
    if current and current["state"] == "running":
        return current
    command = session.command_no_cd
    if not command:
        raise TerminalError(i18n.t("tmux.no_command"))
    if session.missing:
        raise TerminalError(i18n.t("tmux.no_folder"))
    name = name_of(key)
    if current:
        _run(["kill-session", "-t", "=" + name])
    # Агент запускается через интерактивную оболочку человека: так подхватываются PATH и алиасы
    # из ~/.bashrc, как в обычной вкладке терминала. Код выхода оболочки — код выхода агента.
    shell = os.environ.get("SHELL") or "/bin/sh"
    args = ["start-server"]
    for scope, option, value in OPTIONS:
        args += [";", "set-option", scope, option, value]
    args += [";", "new-session", "-d", "-s", name, "-x", str(cols), "-y", str(rows), "-c", _arg(session.cwd),
             "-e", "COLORTERM=truecolor", "--", shell, "-ic", _arg(command),
             # Цель set-option — панель, поэтому точное имя сессии пишется с двоеточием.
             ";", "set-option", "-t", f"={name}:", "@nit_key", key]
    completed = _run(args)
    started = terminals().get(key)
    if started:
        return started
    error = completed.stderr.strip() or f"tmux: {completed.returncode}"
    raise TerminalError(i18n.t("tmux.failed", error=error))


def close_terminal(key: str) -> None:
    """Закрыть терминал: tmux завершает агента вместе с сессией."""
    if available():
        _run(["kill-session", "-t", "=" + name_of(key)])


class Attachment:
    """`tmux attach-session` в псевдотерминале: что пишет tmux — читать, что ввёл человек — писать."""

    def __init__(self, key: str, cols: int, rows: int):
        self.master, slave = pty.openpty()
        self._size(cols, rows)
        try:
            # Управляющий терминал клиенту tmux не нужен: размер он перечитывает по SIGWINCH,
            # который шлёт resize, а Ctrl+C уходит в сессию байтом, а не сигналом.
            self.process = subprocess.Popen(_base() + ["attach-session", "-t", "=" + name_of(key)],
                                            stdin=slave, stdout=slave, stderr=slave, env=_env(),
                                            start_new_session=True, close_fds=True)
        except OSError:
            os.close(self.master)
            raise
        finally:
            os.close(slave)

    def _size(self, cols: int, rows: int) -> None:
        fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    def read(self) -> bytes:
        """Следующая порция вывода; b"", когда клиент tmux завершился."""
        try:
            return os.read(self.master, 65536)
        except OSError:
            return b""

    def write(self, data: bytes) -> None:
        view = memoryview(data)
        while view:
            view = view[os.write(self.master, view):]

    def resize(self, cols: int, rows: int) -> None:
        self._size(cols, rows)
        try:
            self.process.send_signal(signal.SIGWINCH)
        except ProcessLookupError:
            pass

    def close(self) -> None:
        """Отключиться от сессии. Агент продолжает работать в tmux."""
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(2)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        try:
            os.close(self.master)
        except OSError:
            pass


def clamp(value, low: int, high: int, default: int) -> int:
    """Число из запроса в пределах [low, high]; не число — default."""
    try:
        number = int(value)
    except (TypeError, ValueError):
        return default
    return max(low, min(high, number))
