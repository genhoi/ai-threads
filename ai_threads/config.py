"""Пути приложения. Переменные окружения читаются при каждом вызове."""

import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
STATIC = ROOT / "static"


def home() -> Path:
    return Path.home()


def data_dir() -> Path:
    return Path(os.environ.get("AI_THREADS_DATA") or home() / ".local" / "share" / "ai-threads")


def wsl_distro() -> str:
    """Имя дистрибутива WSL или пустая строка вне WSL."""
    return os.environ.get("WSL_DISTRO_NAME", "").strip()
