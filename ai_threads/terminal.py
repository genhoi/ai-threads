"""Открыть папку сессии в проводнике Windows (только WSL) или в VS Code и что для этого есть в системе."""

import os
import shutil
import subprocess

from . import config, i18n
from .model import Session

i18n.add({
    "terminal.no_folder": {"ru": "Папки проекта больше нет", "en": "The project folder no longer exists"},
    "terminal.explorer_wsl": {"ru": "Проводник Windows доступен только в WSL",
                              "en": "Windows Explorer is only available in WSL"},
    "terminal.no_windows_path": {"ru": "Не удалось преобразовать путь в формат Windows",
                                 "en": "Couldn't convert the path to a Windows path"},
    "terminal.no_vscode": {"ru": "Не нашёл VS Code: программы code нет в PATH",
                           "en": "VS Code not found: there is no code program in PATH"},
    "terminal.unknown_app": {"ru": "Неизвестное приложение: {app}", "en": "Unknown app: {app}"},
    "terminal.no_distro": {"ru": "Не задана переменная WSL_DISTRO_NAME", "en": "WSL_DISTRO_NAME is not set"},
})


def environment() -> dict:
    """Что есть в системе для открытия сессий и папок."""
    distro = config.wsl_distro()
    return {"wsl": bool(distro), "distro": distro, "tmux": bool(shutil.which("tmux")),
            "code": bool(shutil.which("code")), "home": str(config.home()), "path": os.environ.get("PATH", "")}


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


def _distro() -> str:
    name = config.wsl_distro()
    if not name:
        raise RuntimeError(i18n.t("terminal.no_distro"))
    return name


def _run(argv: list[str]) -> int:
    completed = subprocess.run(argv, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return completed.returncode
