"""Скрипты восстановления, экранирование wt и запуск через подставные программы."""

import json
import os
import re
import shlex
import signal
import subprocess
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import pytest

from ai_threads.live import _starttime
from ai_threads.model import Session
from ai_threads import terminal
from ai_threads.terminal import (
    tab_color,
    restore_script,
    reveal,
)

# Пометки по-русски: в тестах язык сообщений русский (см. conftest).
ALL_OPEN_COMMENT = "# все выбранные сессии уже открыты или их нельзя продолжить из терминала"
MISSING_NOTE = "нет папки · без cd"
NO_FOLDER = "Папки проекта больше нет"

ROOT = Path(__file__).resolve().parents[1]
RESUME = ROOT / "bin" / "nit-resume"
_WT_DELIM = re.compile(r"^;|[^\\];")


@pytest.fixture(autouse=True)
def isolated(tmp_path, monkeypatch):
    claude = tmp_path / "claude"
    (claude / "sessions").mkdir(parents=True)
    monkeypatch.setenv("CLAUDE_HOME", str(claude))
    monkeypatch.setenv("GROK_HOME", str(tmp_path / "grok"))
    monkeypatch.setenv("CODEX_HOME", str(tmp_path / "codex"))
    monkeypatch.setenv("KIMI_CODE_HOME", str(tmp_path / "kimi"))
    monkeypatch.setenv("WSL_DISTRO_NAME", "Ubuntu")
    (tmp_path / "grok").mkdir()
    return tmp_path


def make_session(tool, session_id, cwd, by=""):
    return Session(tool=tool, id=session_id, title="t", cwd=str(cwd), updated=0, by=by)


def bash_argv(script):
    """Разобрать команду так же, как bash разбирает переносы с \\ и кавычки."""
    normalized = script.replace("\\\n", "")
    parsed = shlex.split(normalized)
    completed = subprocess.run(
        ["bash", "--noprofile", "--norc", "-c", 'set -f; eval "set -- $1"; printf "%s\\0" "$@"', "bash", script],
        check=True,
        capture_output=True,
    )
    from_bash = [part.decode() for part in completed.stdout.split(b"\0")[:-1]]
    assert from_bash == parsed
    return parsed


def _add_arg(commands, arg):
    commands[-1].append(arg.replace("\\;", ";"))


def _add_commands_for_arg(commands, arg):
    """Повтор разбора Windows Terminal: `;` разделяет команды, `\\;` — литерал."""
    remaining = arg
    match = _WT_DELIM.search(remaining)
    while True:
        if match is None:
            _add_arg(commands, remaining)
            return
        matched_first = len(match.group(0)) == 1
        position = match.start() if matched_first else match.start() + 1
        head = remaining[:position]
        if head:
            _add_arg(commands, head)
        commands.append(["wt.exe"])
        remaining = remaining[match.end():]
        if remaining == "":
            return
        match = _WT_DELIM.search(remaining)


def wt_commands(argv):
    commands = [[]]
    for arg in argv:
        _add_commands_for_arg(commands, arg)
    return commands


def test_plain_tmux_and_wt_scripts(tmp_path):
    project = tmp_path / "api"
    project.mkdir()
    gone = tmp_path / "missing-dir"
    claude = make_session("claude", "abc-def", project)
    codex = make_session("codex", "exec-1", project, by="codex exec")
    grok = make_session("grok", "g1", gone)
    kimi = make_session("kimi", "session_aaa", project)
    plain = restore_script([claude, grok], "plain", 8793)
    assert plain.splitlines() == [
        claude.command,
        grok.command_no_cd,
    ]
    assert plain.startswith("cd -- ")
    assert "missing-dir" not in plain.splitlines()[1]

    tmux = restore_script([claude, grok], "tmux", 8793)
    assert tmux.splitlines()[0] == "tmux has-session -t nit 2>/dev/null || tmux new-session -d -s nit"
    assert tmux.splitlines()[-1] == "tmux attach -t nit"
    assert shlex.split(tmux.splitlines()[1]) == [
        "tmux", "new-window", "-t", "nit", "-n", "api", "-c", str(project), claude.command_no_cd,
    ]
    home = str(Path.home())
    assert shlex.split(tmux.splitlines()[2]) == [
        "tmux", "new-window", "-t", "nit", "-n", "missing-dir", "-c", home,
        f"echo {shlex.quote(MISSING_NOTE)}; {grok.command_no_cd}",
    ]

    wt = restore_script([claude, codex, grok, kimi], "wt", 8793)
    argv = bash_argv(wt)
    commands = wt_commands(argv)
    assert len(commands) == 4
    colors = {session.tool: tab_color(session.tool) for session in (claude, codex, grok, kimi)}
    keys = [session.key for session in (claude, codex, grok, kimi)]
    for index, command in enumerate(commands):
        assert command[command.index("--tabColor") + 1] == list(colors.values())[index]
        assert command[-3:] == ["--port", "8793", keys[index]]
        assert "wsl.exe" in command and "Ubuntu" in command
        assert command[command.index("-e") + 1] == str(RESUME)
    assert commands[0][:3] == ["wt.exe", "-w", "0"]
    assert commands[1][0] == "wt.exe"
    assert "codex exec resume" not in wt  # во вкладке wt команда не пишется, только ключ


def test_dangerous_path_roundtrip(tmp_path, monkeypatch):
    name = "проект тест;\"'файл"
    project = tmp_path / name
    project.mkdir()
    resume = Path("/opt/нит resume;\"'bin/nit-resume")
    monkeypatch.setattr(terminal, "resume_executable", lambda: resume)
    first = make_session("claude", "id-1", project)
    second = make_session("kimi", "session_2", tmp_path)
    script = restore_script([first, second], "wt", 8793)
    argv = bash_argv(script)
    assert argv == terminal._wt_argv([first, second], 8793)
    commands = wt_commands(argv)
    assert len(commands) == 2
    assert commands[0][commands[0].index("--title") + 1] == name
    assert commands[0][commands[0].index("-e") + 1] == str(resume)
    assert commands[1][commands[1].index("--title") + 1] == tmp_path.name
    plain = restore_script([first], "plain", 8793)
    assert shlex.split(plain)[:3] == ["cd", "--", str(project)]
    tmux_line = restore_script([first], "tmux", 8793).splitlines()[1]
    assert shlex.split(tmux_line)[shlex.split(tmux_line).index("-c") + 1] == str(project)


def test_running_session_is_left_out_of_script(isolated, monkeypatch):
    claude_home = Path(os.environ["CLAUDE_HOME"])
    project = isolated / "api"
    project.mkdir()
    pid = os.getpid()
    start = _starttime(Path(f"/proc/{pid}/stat").read_text(encoding="utf-8"))
    (claude_home / "sessions" / "live.json").write_text(json.dumps({
        "pid": pid,
        "sessionId": "live-id",
        "kind": "interactive",
        "status": "busy",
        "statusUpdatedAt": 1_700_000_000_000,
        "procStart": start,
    }), encoding="utf-8")
    running = make_session("claude", "live-id", project)
    other = make_session("codex", "other", project)
    script = restore_script([running, other], "wt", 8793)
    assert "live-id" not in script
    assert "codex:other" in script
    assert restore_script([running], "plain", 8793) == ALL_OPEN_COMMENT


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


def test_nit_resume_against_local_server(tmp_path):
    seen = {}
    project = tmp_path / "проект"
    project.mkdir()
    home = tmp_path / "home"
    home.mkdir()
    (home / ".bashrc").write_text(
        'alias mark_ran=\'pwd > "$HOME/where.txt"; echo ran > "$HOME/ran.txt"\'\n',
        encoding="utf-8",
    )

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            seen["host"] = self.headers.get("Host")
            parsed = urlparse(self.path)
            seen["key"] = parse_qs(parsed.query).get("key", [""])[0]
            if parsed.path != "/api/resolve":
                self.send_error(404)
                return
            if seen["key"] == "claude:missing":
                body = {"cwd": str(tmp_path / "нет-папки"), "command_no_cd": "mark_ran"}
            else:
                body = {"cwd": str(project), "command_no_cd": "mark_ran"}
            payload = json.dumps(body).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, fmt, *args):
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    port = server.server_address[1]
    import threading
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        _run_resume(port, "claude:abc def", home, project)
        assert seen["host"] == f"127.0.0.1:{port}"
        assert seen["key"] == "claude:abc def"
        assert (home / "where.txt").read_text(encoding="utf-8").strip() == str(project)
        (home / "where.txt").unlink()
        (home / "ran.txt").unlink()

        err = _run_resume(port, "claude:missing", home, home)
        assert MISSING_NOTE in err
        assert "Папки проекта больше нет" in err
        assert (home / "where.txt").read_text(encoding="utf-8").strip() == str(home)
    finally:
        server.shutdown()


def test_resume_script_is_executable_bash():
    assert os.access(RESUME, os.X_OK)
    subprocess.run(["bash", "-n", str(RESUME)], check=True)


def test_real_tmux_script_with_echo(tmp_path, monkeypatch):
    """Скрипт tmux, который человек копирует, открывает окно с командой сессии."""
    if subprocess.run(["tmux", "-V"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0:
        pytest.skip("tmux недоступен")
    project = tmp_path / "nit-echo-proj"
    project.mkdir()
    Path(os.environ["AI_THREADS_CONFIG"]).write_text(json.dumps({"tmux_session": "nit-test"}), encoding="utf-8")

    class Echo(Session):
        @property
        def command_no_cd(self):
            return "echo ok; sleep 2"

    script = restore_script([Echo(tool="claude", id="echo", title="t", cwd=str(project), updated=0)], "tmux", 8793)
    lines = script.splitlines()
    assert lines[-1] == "tmux attach -t nit-test"
    subprocess.run(["tmux", "kill-session", "-t", "nit-test"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        subprocess.run(["bash", "-c", "\n".join(lines[:-1])], check=True)
        windows = subprocess.check_output(
            ["tmux", "list-windows", "-t", "nit-test", "-F", "#{window_name}"],
            text=True,
        )
        assert "nit-echo-proj" in windows.split()
        time.sleep(0.5)
        pane = subprocess.check_output(["tmux", "capture-pane", "-p", "-t", "nit-test:nit-echo-proj"], text=True)
        assert "ok" in pane
    finally:
        subprocess.run(["tmux", "kill-session", "-t", "nit-test"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


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
        "        fh.write(json.dumps([name, *sys.argv[1:]], ensure_ascii=False) + '\\n')\n"
        "if len(sys.argv) >= 2 and sys.argv[1] == 'has-session':\n"
        "    raise SystemExit(0 if os.environ.get('NIT_TMUX_EXISTS') == '1' else 1)\n",
        encoding="utf-8",
    )
    script.chmod(0o755)
    for name in ("wt.exe", "tmux", "explorer.exe", "code"):
        (bindir / name).symlink_to(script)
    monkeypatch.setenv("NIT_ARGV_LOG", str(log))
    monkeypatch.setenv("PATH", str(bindir) + os.pathsep + os.environ["PATH"])
    return log, bindir


def _run_resume(port, key, home, expected_pwd):
    env = os.environ.copy()
    env["HOME"] = str(home)
    env.pop("BASH_ENV", None)
    err_path = home / "stderr.txt"
    err_file = err_path.open("w", encoding="utf-8")
    proc = subprocess.Popen(
        [str(RESUME), "--port", str(port), key],
        stdin=subprocess.PIPE,
        stdout=subprocess.DEVNULL,
        stderr=err_file,
        env=env,
        start_new_session=True,
    )
    try:
        deadline = time.time() + 8
        while time.time() < deadline and not (home / "ran.txt").exists():
            if proc.poll() is not None:
                break
            time.sleep(0.05)
        err_file.flush()
        err = err_path.read_text(encoding="utf-8")
        assert (home / "ran.txt").exists(), err
        assert proc.poll() is None, err
        assert (home / "where.txt").read_text(encoding="utf-8").strip() == str(expected_pwd)
        return err
    finally:
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
        proc.wait(timeout=5)
        err_file.close()
