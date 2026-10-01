"""Сессия агента в том виде, в каком её видят все модули приложения."""

from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class Session:
    tool: str                 # id агента из ai_threads.agents
    id: str                   # для Kimi — session_<uuid>
    title: str                # исходное название
    cwd: str                  # папка сессии, может быть пустой
    updated: float            # время последней активности, unix-секунды
    created: float = 0.0
    branch: str = ""
    auto: bool = False        # запуск без человека: codex exec, claude -p, grok headless, kimi -p и т. п.
    by: str = "я"             # подпись «запустил»
    last: str = ""            # последний ответ агента, до 300 символов
    empty: bool = False       # в журнале нет текстовых сообщений
    journal: Path = field(default_factory=Path)  # файл с перепиской
    meta_path: Path = field(default_factory=Path)  # файл, по которому найдена сессия

    @property
    def key(self) -> str:
        return f"{self.tool}:{self.id}"

    @property
    def project(self) -> str:
        cwd = self.cwd.rstrip("/")
        return Path(cwd).name if cwd else "Без проекта"

    @property
    def missing(self) -> bool:
        return not (self.cwd and Path(self.cwd).is_dir())

    @property
    def temp(self) -> bool:
        from .settings import temp_dirs
        cwd = self.cwd.rstrip("/")
        return any(cwd == root or cwd.startswith(root.rstrip("/") + "/") for root in temp_dirs())

    @property
    def command_no_cd(self) -> str:
        """Команда продолжения из настроек агента; пустая, если из терминала продолжить нельзя."""
        from .agents import get
        return get(self.tool).resume_command(self)

    @property
    def resume_hint(self) -> str:
        from .agents import get
        return "" if self.command_no_cd else get(self.tool).resume_hint

    @property
    def command(self) -> str:
        import shlex
        command = self.command_no_cd
        if not command or not self.cwd:
            return command
        return f"cd -- {shlex.quote(self.cwd)} && {command}"

    def to_json(self) -> dict:
        command_no_cd = self.command_no_cd
        return {"key": self.key, "tool": self.tool, "id": self.id, "title": self.title, "cwd": self.cwd,
                "project": self.project, "branch": self.branch, "updated": self.updated, "created": self.created,
                "command": self.command, "command_no_cd": command_no_cd, "resume_hint": self.resume_hint,
                "auto": self.auto, "by": self.by, "temp": self.temp, "missing": self.missing, "last": self.last,
                "empty": self.empty}
