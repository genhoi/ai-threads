"""Скрипт восстановления сессий для копирования (вкладки Windows Terminal в WSL, окна tmux
или список команд), проводник Windows и VS Code.

Сами сессии приложение не открывает: человек копирует скрипт и запускает его в терминале.
Порт сервера передаёт вызывающий: он знает, на каком порту слушает. Вкладка Windows Terminal
запускает `bin/nit-resume`, который по этому порту запрашивает `GET /api/resolve?key=` и
получает `{"cwd", "command_no_cd"}`.
"""

import os
import shlex
import shutil
import subprocess
from pathlib import Path

from . import config, i18n, settings
from .live import live_status
from .model import Session

# Пометка missing_note совпадает с той, что печатает bin/nit-resume.
i18n.add({
    "terminal.already_open": {"ru": "уже открыта", "en": "already open"},
    "terminal.no_command": {"ru": "нельзя продолжить из терминала", "en": "can't be resumed from the terminal"},
    "terminal.missing_note": {"ru": "нет папки · без cd", "en": "no folder · no cd"},
    "terminal.all_open": {"ru": "# все выбранные сессии уже открыты или их нельзя продолжить из терминала",
                          "en": "# all selected sessions are already open or can't be resumed from the terminal"},
    "terminal.no_folder": {"ru": "Папки проекта больше нет", "en": "The project folder no longer exists"},
    "terminal.need_wsl": {"ru": "Windows Terminal доступен только в WSL",
                          "en": "Windows Terminal is only available in WSL"},
    "terminal.unknown_format": {"ru": "Неизвестный формат: {fmt}", "en": "Unknown format: {fmt}"},
    "terminal.explorer_wsl": {"ru": "Проводник Windows доступен только в WSL",
                              "en": "Windows Explorer is only available in WSL"},
    "terminal.no_windows_path": {"ru": "Не удалось преобразовать путь в формат Windows",
                                 "en": "Couldn't convert the path to a Windows path"},
    "terminal.no_vscode": {"ru": "Не нашёл VS Code: программы code нет в PATH",
                           "en": "VS Code not found: there is no code program in PATH"},
    "terminal.unknown_app": {"ru": "Неизвестное приложение: {app}", "en": "Unknown app: {app}"},
    "terminal.no_distro": {"ru": "Не задана переменная WSL_DISTRO_NAME", "en": "WSL_DISTRO_NAME is not set"},
    "terminal.bad_port": {"ru": "Некорректный порт", "en": "Invalid port"},
})


def environment() -> dict:
    """Что есть в системе для открытия сессий и папок."""
    distro = config.wsl_distro()
    return {"wsl": bool(distro), "distro": distro, "tmux": bool(shutil.which("tmux")),
            "code": bool(shutil.which("code")), "home": str(config.home()), "path": os.environ.get("PATH", "")}


def tab_color(tool: str) -> str:
    from .agents import AGENTS
    agent = AGENTS.get(tool)
    return agent.color if agent else "#5a5e66"


def resume_executable() -> Path:
    return config.ROOT / "bin" / "nit-resume"


def restore_script(sessions: list[Session], fmt: str, port: int) -> str:
    """Текст скрипта wt, tmux или plain. Уже запущенные сессии в текст не входят."""
    if fmt not in ("wt", "tmux", "plain"):
        raise ValueError(i18n.t("terminal.unknown_format", fmt=fmt))
    if fmt == "wt" and not config.wsl_distro():
        raise ValueError(i18n.t("terminal.need_wsl"))
    runnable, _skipped, _notes = _partition(sessions)
    if not runnable:
        return i18n.t("terminal.all_open")
    if fmt == "wt":
        return _wt_script(_wt_argv(runnable, port))
    if fmt == "tmux":
        return _tmux_script(runnable, settings.effective()["tmux_session"])
    return _plain_script(runnable)


def reveal(session: Session, app: str) -> None:
    """Показать папку в проводнике Windows (только WSL) или открыть её в VS Code."""
    if session.missing:
        raise FileNotFoundError(i18n.t("terminal.no_folder"))
    if app == "explorer":
        if not config.wsl_distro():
            raise RuntimeError(i18n.t("terminal.explorer_wsl"))
        completed = subprocess.run(
            ["wslpath", "-w", session.cwd],
            capture_output=True,
            text=True,
        )
        windows = completed.stdout.strip()
        if completed.returncode != 0 or not windows:
            detail = completed.stderr.strip() or i18n.t("terminal.no_windows_path")
            raise RuntimeError(detail)
        _run(["explorer.exe", windows])
    elif app == "vscode":
        if not shutil.which("code"):
            raise RuntimeError(i18n.t("terminal.no_vscode"))
        if config.wsl_distro():
            _run(["code", "--remote", f"wsl+{_distro()}", session.cwd])
        else:
            _run(["code", session.cwd])
    else:
        raise ValueError(i18n.t("terminal.unknown_app", app=app))


def _partition(sessions: list[Session]) -> tuple[list[Session], list[dict], list[dict]]:
    live = live_status({session.key: session for session in sessions})
    runnable: list[Session] = []
    skipped: list[dict] = []
    notes: list[dict] = []
    for session in sessions:
        if session.key in live:
            skipped.append({"key": session.key, "reason": i18n.t("terminal.already_open")})
            continue
        if not session.command_no_cd:
            skipped.append({"key": session.key, "reason": i18n.t("terminal.no_command")})
            continue
        runnable.append(session)
        if session.missing:
            notes.append({"key": session.key, "note": i18n.t("terminal.missing_note")})
    return runnable, skipped, notes


def _wt_argv(sessions: list[Session], port: int) -> list[str]:
    """Аргументы wt.exe. `;` внутри аргумента экранируется как `\\;`.

    Windows Terminal режет команды по `;` уже после разбора argv. Отдельный
    аргумент `;` начинает следующую вкладку, `\\;` внутри аргумента остаётся
    символом «;» в заголовке или пути.
    """
    distro = _distro()
    resume = str(resume_executable())
    port_text = _port_text(port)
    argv = ["wt.exe", "-w", "0"]
    for index, session in enumerate(sessions):
        if index:
            argv.append(";")
        argv.extend([
            "new-tab",
            "--title", _wt_escape(_label(session)),
            "--tabColor", tab_color(session.tool),
            "wsl.exe",
            "-d", _wt_escape(distro),
            "-e", _wt_escape(resume),
            "--port", port_text,
            _wt_escape(session.key),
        ])
    return argv


def _label(session: Session) -> str:
    """Заголовок вкладки или окна: проект, а у сессии без папки — начало ID."""
    return session.project or session.id[:8]


def _wt_escape(value: str) -> str:
    return value.replace(";", "\\;")


def _wt_script(argv: list[str]) -> str:
    groups: list[list[str]] = []
    current: list[str] = []
    for arg in argv[3:]:
        if arg == ";":
            groups.append(current)
            current = []
        else:
            current.append(arg)
    if current:
        groups.append(current)
    lines = ["wt.exe -w 0 \\"]
    last = len(groups) - 1
    for index, group in enumerate(groups):
        rendered = " ".join(shlex.quote(part) for part in group)
        if index < last:
            lines.append(f"  {rendered} \\; \\")
        else:
            lines.append(f"  {rendered}")
    return "\n".join(lines)


def _tmux_script(sessions: list[Session], name: str) -> str:
    quoted = shlex.quote(name)
    lines = [f"tmux has-session -t {quoted} 2>/dev/null || tmux new-session -d -s {quoted}"]
    lines.extend(_shell_join(_tmux_window_argv(session, name)) for session in sessions)
    lines.append(f"tmux attach -t {quoted}")
    return "\n".join(lines)


def _tmux_window_argv(session: Session, name: str) -> list[str]:
    argv = ["tmux", "new-window", "-t", name, "-n", _label(session)]
    if session.missing:
        argv.extend(["-c", str(Path.home())])
    else:
        argv.extend(["-c", session.cwd])
    command = session.command_no_cd
    if session.missing:
        note = shlex.quote(i18n.t("terminal.missing_note"))
        command = f"echo {note}; {command}"
    argv.append(command)
    return argv


def _plain_script(sessions: list[Session]) -> str:
    lines = []
    for session in sessions:
        lines.append(session.command_no_cd if session.missing else session.command)
    return "\n".join(lines)


def _shell_join(argv: list[str]) -> str:
    return " ".join(shlex.quote(part) for part in argv)


def _distro() -> str:
    name = config.wsl_distro()
    if not name:
        raise RuntimeError(i18n.t("terminal.no_distro"))
    return name


def _port_text(port: int) -> str:
    if isinstance(port, bool) or not isinstance(port, int) or not 0 < port < 65536:
        raise ValueError(i18n.t("terminal.bad_port"))
    return str(port)


def _run(argv: list[str]) -> int:
    completed = subprocess.run(argv, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return completed.returncode
