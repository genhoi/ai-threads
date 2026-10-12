"""Терминалы в tmux: запуск агента, подключение, код выхода, закрытие и WebSocket сервера.

Вместо агента — заглушка tests/stubs/agent-tty: печатает аргументы и отвечает на каждую строку.
Каждый тест работает со своим сервером tmux (фикстура tmux_server).
"""

import base64
import json
import os
import select
import socket
import struct
import subprocess
import threading
import time
from pathlib import Path

import pytest

from ai_threads import tmux
from ai_threads import websocket as ws
from ai_threads.model import Session
from ai_threads.server import create_server
from ai_threads.store import Store

AGENT = Path(__file__).resolve().parent / "stubs" / "agent-tty"
MASK = b"\x0a\x0b\x0c\x0d"


@pytest.fixture(autouse=True)
def fake_agent(tmux_server):
    """claude и codex в терминале — заглушка agent-tty."""
    Path(os.environ["AI_THREADS_CONFIG"]).write_text(json.dumps(
        {"agents": {"claude": {"program": str(AGENT)}, "codex": {"program": str(AGENT)}}}), encoding="utf-8")


def session(cwd, session_id="abc", tool="claude"):
    return Session(tool=tool, id=session_id, title="t", cwd=str(cwd), updated=0)


def wait(predicate, timeout=5.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(0.05)
    raise AssertionError("условие не выполнилось")


def read_until(attachment, needle, timeout=5.0):
    data, deadline = b"", time.monotonic() + timeout
    while needle not in data and time.monotonic() < deadline:
        if select.select([attachment.master], [], [], 0.1)[0]:
            chunk = attachment.read()
            if not chunk:
                break
            data += chunk
    assert needle in data, data[-500:]
    return data


def clients():
    return tmux._run(["list-clients", "-F", "#{client_width}x#{client_height}"]).stdout.split()


def test_names_are_exact_targets():
    assert tmux.name_of("claude:abc-1_2") == "nit-claude-abc-1_2"
    dotted = tmux.name_of("kimi:a.b")
    assert dotted.startswith("nit-kimi-a_b-") and dotted != tmux.name_of("kimi:a_b")
    assert tmux._arg("x;") == "x\\;" and tmux._arg("x") == "x"


def test_open_attach_exit_and_close(tmp_path):
    work = session(tmp_path)
    opened = tmux.open_terminal(work, 100, 30)
    assert opened["state"] == "running" and opened["attach"].endswith("attach-session -t =nit-claude-abc")
    assert tmux.open_terminal(work)["started"] == opened["started"]  # второй запуск не нужен
    attachment = tmux.Attachment(work.key, 90, 25)
    try:
        read_until(attachment, b"fake agent: --resume abc")
        attachment.write(b"hello\r")
        read_until(attachment, b"got: hello")
        attachment.resize(130, 40)
        wait(lambda: clients() == ["130x40"])
        attachment.write(b"quit\r")
        exited = wait(lambda: tmux.terminals().get(work.key, {}).get("state") == "exited" and tmux.terminals()[work.key])
        assert exited["code"] == 4
    finally:
        attachment.close()
    assert tmux.open_terminal(work)["state"] == "running"  # завершённый запускается заново
    tmux.close_terminal(work.key)
    assert tmux.terminals() == {}


def test_folder_with_semicolon_and_dot_in_id(tmp_path):
    folder = tmp_path / "odd;"
    folder.mkdir()
    work = session(folder, "id.with.dots")
    tmux.open_terminal(work)
    name = tmux.name_of(work.key)
    path = tmux._run(["display-message", "-p", "-t", f"={name}:", "#{pane_current_path}"]).stdout.strip()
    assert path == str(folder)
    assert list(tmux.terminals()) == [work.key]


def test_cannot_open_without_folder_command_or_tmux(tmp_path, monkeypatch):
    with pytest.raises(tmux.TerminalError, match="Папки проекта больше нет"):
        tmux.open_terminal(session(tmp_path / "gone"))
    with pytest.raises(tmux.TerminalError, match="нельзя продолжить из терминала"):
        tmux.open_terminal(session(tmp_path, tool="zcode"))
    monkeypatch.setenv("PATH", str(tmp_path))
    with pytest.raises(tmux.TerminalError, match="нужен tmux"):
        tmux.open_terminal(session(tmp_path))
    assert tmux.terminals() == {}


# --- сервер ------------------------------------------------------------------

class Catalog:
    """Каталог из одной сессии в существующей папке: агенту есть где запуститься."""

    def __init__(self, item):
        self.item, self.ready = item, True

    def get(self, key):
        return self.item if key == self.item.key else None

    def sessions(self):
        return [self.item]


@pytest.fixture
def server(tmp_path):
    work = session(tmp_path / "project")
    (tmp_path / "project").mkdir()
    instance = create_server(0, catalog=Catalog(work), store=Store(tmp_path / "state.json"))
    thread = threading.Thread(target=instance.serve_forever, daemon=True)
    thread.start()
    try:
        yield instance, work
    finally:
        instance.shutdown()
        instance.server_close()


def post(server, path, body, origin=True):
    host = f"127.0.0.1:{server.server_port}"
    headers = {"Content-Type": "application/json", "Host": host}
    if origin:
        headers["Origin"] = f"http://{host}"
    import http.client
    connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=10)
    connection.request("POST", path, json.dumps(body), headers)
    response = connection.getresponse()
    result = response.status, json.loads(response.read())
    connection.close()
    return result


def handshake(server, key, origin="self", headers=None):
    """Запрос WebSocket сырым сокетом: (статус, сокет с непрочитанным остатком)."""
    host = f"127.0.0.1:{server.server_port}"
    lines = [f"GET /api/terminal/socket?key={key}&cols=100&rows=30 HTTP/1.1", f"Host: {host}"]
    if origin:
        lines.append(f"Origin: {'http://' + host if origin == 'self' else origin}")
    token = base64.b64encode(os.urandom(16)).decode()
    defaults = {"Upgrade": "websocket", "Connection": "Upgrade", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": token}
    lines += [f"{name}: {value}" for name, value in {**defaults, **(headers or {})}.items()]
    sock = socket.create_connection(("127.0.0.1", server.server_port), timeout=10)
    sock.sendall(("\r\n".join(lines) + "\r\n\r\n").encode())
    reader = sock.makefile("rb")
    status = int(reader.readline().split()[1])
    head = {}
    while (line := reader.readline().strip()):
        name, _, value = line.decode().partition(":")
        head[name.lower()] = value.strip()
    if status == 101:
        assert head["sec-websocket-accept"] == ws.accept_key(token)
    return status, sock, reader


def frame(reader):
    first, second = reader.read(2)
    size = second & 0x7F
    if size == 126:
        size = struct.unpack("!H", reader.read(2))[0]
    elif size == 127:
        size = struct.unpack("!Q", reader.read(8))[0]
    return first & 0x0F, reader.read(size)


def receive_until(reader, needle):
    data = b""
    while needle not in data:
        opcode, payload = frame(reader)
        assert opcode == ws.BINARY, (opcode, payload)
        data += payload
    return data


def test_socket_requires_own_origin_and_open_terminal(server):
    instance, work = server
    assert post(instance, "/api/terminal/open", {"key": work.key}, origin=False)[0] == 403
    assert handshake(instance, work.key)[0] == 404  # терминал ещё не открыт
    status, terminal = post(instance, "/api/terminal/open", {"key": work.key, "cols": 100, "rows": 30})
    assert status == 200 and terminal["state"] == "running"
    assert handshake(instance, work.key, origin=None)[0] == 403
    assert handshake(instance, work.key, origin="http://evil.example")[0] == 403
    assert handshake(instance, work.key, headers={"Upgrade": "h2c"})[0] == 400
    assert handshake(instance, work.key, headers={"Sec-WebSocket-Key": "short"})[0] == 400
    assert handshake(instance, "claude:../../etc")[0] == 400


def test_socket_streams_terminal_until_it_is_closed(server):
    instance, work = server
    post(instance, "/api/terminal/open", {"key": work.key})
    status, sock, reader = handshake(instance, work.key)
    assert status == 101
    receive_until(reader, b"fake agent: --resume abc")
    sock.sendall(ws.encode_frame(ws.BINARY, b"hello\r", mask=MASK))
    receive_until(reader, b"got: hello")
    sock.sendall(ws.encode_frame(ws.TEXT, json.dumps({"type": "resize", "cols": 120, "rows": 35}).encode(), mask=MASK))
    wait(lambda: clients() == ["120x35"])
    sock.sendall(ws.encode_frame(ws.BINARY, b"quit\r", mask=MASK))
    terminals = lambda: json.loads(get(instance, "/api/terminals"))
    assert wait(lambda: terminals().get(work.key, {}).get("state") == "exited" and terminals())[work.key]["code"] == 4
    # Закрытый терминал завершает клиента tmux, и сервер закрывает соединение.
    assert post(instance, "/api/terminal/close", {"key": work.key}) == (200, {"ok": True})
    opcode = ws.BINARY
    while opcode == ws.BINARY:
        opcode, payload = frame(reader)
    assert opcode == ws.CLOSE and payload == struct.pack("!H", 1000)
    sock.close()
    assert terminals() == {}


def test_closing_socket_keeps_agent_running(server):
    instance, work = server
    post(instance, "/api/terminal/open", {"key": work.key})
    status, sock, reader = handshake(instance, work.key)
    receive_until(reader, b"fake agent")
    sock.sendall(ws.encode_frame(ws.CLOSE, struct.pack("!H", 1000), mask=MASK))
    opcode = ws.BINARY
    while opcode == ws.BINARY:
        opcode, _ = frame(reader)
    assert opcode == ws.CLOSE
    sock.close()
    wait(lambda: clients() == [])
    assert tmux.terminals()[work.key]["state"] == "running"


def get(server, path):
    import http.client
    connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=10)
    connection.request("GET", path, headers={"Host": f"127.0.0.1:{server.server_port}"})
    body = connection.getresponse().read()
    connection.close()
    return body


def test_attach_command_reaches_the_same_agent(tmp_path):
    work = session(tmp_path)
    tmux.open_terminal(work)
    command = tmux.terminals()[work.key]["attach"]
    # Тот же сервер tmux и та же сессия: команда для другого терминала подключается к агенту, а не запускает второго.
    listed = subprocess.run(command.replace("attach-session", "list-panes"), shell=True, capture_output=True, text=True)
    assert listed.returncode == 0 and len(listed.stdout.splitlines()) == 1
