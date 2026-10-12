"""Кадры WebSocket: рукопожатие, маска, сообщения из нескольких кадров, ping и закрытие."""

import io
import struct

import pytest

from ai_threads import websocket as ws

MASK = b"\x01\x02\x03\x04"


def client(*frames: bytes) -> tuple[ws.Connection, io.BytesIO]:
    out = io.BytesIO()
    return ws.Connection(io.BytesIO(b"".join(frames)), out), out


def test_accept_key_from_rfc():
    # Пример из RFC 6455, раздел 1.3.
    assert ws.accept_key("dGhlIHNhbXBsZSBub25jZQ==") == "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
    assert ws.valid_key("dGhlIHNhbXBsZSBub25jZQ==")
    assert not ws.valid_key("c2hvcnQ=") and not ws.valid_key("не base64") and not ws.valid_key(None)


@pytest.mark.parametrize("size", [0, 5, 125, 126, 65535, 65536])
def test_message_sizes_roundtrip(size):
    payload = bytes(range(256)) * (size // 256) + bytes(range(size % 256))
    conn, _ = client(ws.encode_frame(ws.BINARY, payload, mask=MASK))
    assert conn.receive() == (ws.BINARY, payload)


def test_fragments_ping_and_close():
    conn, out = client(
        ws.encode_frame(ws.TEXT, "при".encode(), fin=False, mask=MASK),
        ws.encode_frame(ws.PING, b"p", mask=MASK),
        ws.encode_frame(ws.CONTINUATION, "вет".encode(), mask=MASK),
        ws.encode_frame(ws.CLOSE, struct.pack("!H", 1001), mask=MASK),
    )
    assert conn.receive() == (ws.TEXT, "привет".encode())
    assert conn.receive() is None and conn.closed
    # Ответы сервера без маски: pong с теми же данными, затем закрытие с кодом клиента.
    assert out.getvalue() == ws.encode_frame(ws.PONG, b"p") + ws.encode_frame(ws.CLOSE, struct.pack("!H", 1001))
    assert not conn.send(ws.BINARY, b"after close")


def test_reserved_close_code_is_not_echoed():
    conn, out = client(ws.encode_frame(ws.CLOSE, struct.pack("!H", 1005), mask=MASK))
    assert conn.receive() is None
    assert out.getvalue() == ws.encode_frame(ws.CLOSE, struct.pack("!H", 1000))


@pytest.mark.parametrize("frame,code", [
    (ws.encode_frame(ws.BINARY, b"x"), 1002),                                  # без маски
    (ws.encode_frame(ws.CONTINUATION, b"x", mask=MASK), 1002),                 # продолжение без начала
    (ws.encode_frame(ws.PING, b"x" * 126, mask=MASK), 1002),                   # длинный управляющий кадр
    (bytes([0x82, 0xFF]) + struct.pack("!Q", ws.MAX_MESSAGE + 1) + MASK, 1009),  # слишком большое сообщение
    (bytes([0xC2, 0x81]) + MASK + b"x", 1002),                                 # сжатие не согласовано
])
def test_protocol_errors(frame, code):
    conn, _ = client(frame)
    with pytest.raises(ws.ProtocolError) as error:
        conn.receive()
    assert error.value.code == code


def test_cut_connection_is_eof():
    conn, _ = client(ws.encode_frame(ws.BINARY, b"hello", mask=MASK)[:-2])
    with pytest.raises(EOFError):
        conn.receive()
