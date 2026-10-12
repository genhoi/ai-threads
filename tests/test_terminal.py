"""Проводник Windows и VS Code через подставные программы."""

import json
import os

import pytest

from ai_threads.model import Session
from ai_threads.terminal import reveal

# Сообщения по-русски: в тестах язык сообщений русский (см. conftest).
NO_FOLDER = "Папки проекта больше нет"


@pytest.fixture(autouse=True)
def isolated(tmp_path, monkeypatch):
    monkeypatch.setenv("WSL_DISTRO_NAME", "Ubuntu")
    return tmp_path


def make_session(tool, session_id, cwd):
    return Session(tool=tool, id=session_id, title="t", cwd=str(cwd), updated=0)


def install_fakes(tmp_path, monkeypatch):
    bindir = tmp_path / "fake-bin"
    bindir.mkdir()
    log = tmp_path / "argv.jsonl"
    script = bindir / "record.py"
    script.write_text(
        "#!/usr/bin/env python3\n"
        "import json, os, sys\n"
        "path = os.environ.get('NIT_ARGV_LOG')\n"
        "if path:\n"
        "    name = os.path.basename(sys.argv[0])\n"
        "    with open(path, 'a', encoding='utf-8') as fh:\n"
        "        fh.write(json.dumps([name, *sys.argv[1:]], ensure_ascii=False) + '\\n')\n",
        encoding="utf-8",
    )
    script.chmod(0o755)
    for name in ("explorer.exe", "code"):
        (bindir / name).symlink_to(script)
    monkeypatch.setenv("NIT_ARGV_LOG", str(log))
    monkeypatch.setenv("PATH", str(bindir) + os.pathsep + os.environ["PATH"])
    return log, bindir


def test_reveal_explorer_and_code(isolated, monkeypatch):
    project = isolated / "api"
    project.mkdir()
    session = make_session("claude", "id", project)
    log, fake = install_fakes(isolated, monkeypatch)
    # wslpath есть только в WSL: заглушка печатает путь Windows.
    windows = "\\\\wsl.localhost\\Ubuntu\\api"
    (fake / "wslpath").write_text(f"#!/bin/sh\nprintf '%s\\n' '{windows}'\n", encoding="utf-8")
    (fake / "wslpath").chmod(0o755)
    reveal(session, "explorer")
    reveal(session, "vscode")
    calls = [json.loads(line) for line in log.read_text(encoding="utf-8").splitlines()]
    assert calls[0] == ["explorer.exe", windows]
    assert calls[1] == ["code", "--remote", "wsl+Ubuntu", str(project)]

    missing = make_session("claude", "id", isolated / "gone")
    with pytest.raises(FileNotFoundError, match=NO_FOLDER):
        reveal(missing, "explorer")
    assert len(log.read_text(encoding="utf-8").splitlines()) == 2


def test_reveal_explorer_needs_wsl(isolated, monkeypatch):
    project = isolated / "api"
    project.mkdir()
    monkeypatch.delenv("WSL_DISTRO_NAME")
    with pytest.raises(RuntimeError, match="только в WSL"):
        reveal(make_session("claude", "id", project), "explorer")
