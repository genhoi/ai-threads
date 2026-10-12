"""WebSocket (RFC 6455) поверх соединения http.server: рукопожатие и кадры.

Здесь только то, что нужно терминалу: текстовые и двоичные сообщения, сообщения из нескольких
кадров, ping и закрытие. Расширений вроде сжатия нет: браузер их предлагает, сервер не соглашается.
"""

import base64
import binascii
import hashlib
import struct
import threading

GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
CONTINUATION, TEXT, BINARY, CLOSE, PING, PONG = 0x0, 0x1, 0x2, 0x8, 0x9, 0xA
# Ввод с клавиатуры и вставка из буфера: мегабайта хватает с запасом.
MAX_MESSAGE = 1 << 20


class ProtocolError(Exception):
    """Клиент нарушил протокол. code — код закрытия: 1002 — ошибка протокола, 1009 — слишком большое сообщение."""

    def __init__(self, code: int = 1002):
        super().__init__(code)
        self.code = code


def valid_key(key) -> bool:
    """Sec-WebSocket-Key — 16 случайных байт в base64."""
    try:
        return isinstance(key, str) and len(base64.b64decode(key, validate=True)) == 16
    except (binascii.Error, ValueError):
        return False


def accept_key(key: str) -> str:
    """Значение Sec-WebSocket-Accept для ключа клиента."""
    return base64.b64encode(hashlib.sha1((key + GUID).encode("ascii")).digest()).decode("ascii")


def encode_frame(opcode: int, payload: bytes, *, fin: bool = True, mask: bytes | None = None) -> bytes:
    """Кадр целиком. Сервер шлёт кадры без маски; mask нужен клиенту, в том числе в тестах."""
    head = bytes([(0x80 if fin else 0) | opcode])
    size = len(payload)
    flag = 0x80 if mask else 0
    if size < 126:
        head += bytes([flag | size])
    elif size < 1 << 16:
        head += bytes([flag | 126]) + struct.pack("!H", size)
    else:
        head += bytes([flag | 127]) + struct.pack("!Q", size)
    if mask:
        return head + mask + _unmask(payload, mask)
    return head + payload


def _unmask(payload: bytes, mask: bytes) -> bytes:
    if not payload:
        return b""
    # XOR целыми числами быстрее, чем побайтно: вставка из буфера бывает длинной.
    key = int.from_bytes((mask * (len(payload) // 4 + 1))[:len(payload)], "big")
    return (int.from_bytes(payload, "big") ^ key).to_bytes(len(payload), "big")


class Connection:
    """Сообщения одного соединения. receive вызывает один поток, send — любой."""

    def __init__(self, rfile, wfile):
        self._rfile, self._wfile = rfile, wfile
        self._lock = threading.Lock()
        self.closed = False

    def _read(self, size: int) -> bytes:
        data = self._rfile.read(size) if size else b""
        if len(data) < size:
            raise EOFError
        return data

    def _frame(self) -> tuple[bool, int, bytes]:
        first, second = self._read(2)
        fin, opcode, size = bool(first & 0x80), first & 0x0F, second & 0x7F
        # Расширения не согласованы, а кадры клиента обязаны быть с маской.
        if first & 0x70 or not second & 0x80:
            raise ProtocolError()
        if size == 126:
            size = struct.unpack("!H", self._read(2))[0]
        elif size == 127:
            size = struct.unpack("!Q", self._read(8))[0]
        if opcode >= 0x8 and (not fin or size > 125):
            raise ProtocolError()
        if size > MAX_MESSAGE:
            raise ProtocolError(1009)
        mask = self._read(4)
        return fin, opcode, _unmask(self._read(size), mask)

    def receive(self) -> tuple[int, bytes] | None:
        """Следующее сообщение (TEXT или BINARY, данные) или None, когда клиент закрыл соединение.
        На ping отвечает сам."""
        kind, message = None, bytearray()
        while True:
            fin, opcode, payload = self._frame()
            if opcode == PING:
                self.send(PONG, payload)
                continue
            if opcode == PONG:
                continue
            if opcode == CLOSE:
                code = struct.unpack("!H", payload[:2])[0] if len(payload) >= 2 else 1000
                # Коды 1005, 1006 и 1015 только для отчётов, в кадре их слать нельзя.
                self.close(code if 1000 <= code < 5000 and code not in (1004, 1005, 1006, 1015) else 1000)
                return None
            if opcode == CONTINUATION:
                if kind is None:
                    raise ProtocolError()
            elif opcode in (TEXT, BINARY):
                if kind is not None:
                    raise ProtocolError()
                kind = opcode
            else:
                raise ProtocolError()
            message += payload
            if len(message) > MAX_MESSAGE:
                raise ProtocolError(1009)
            if fin:
                return kind, bytes(message)

    def send(self, opcode: int, payload: bytes) -> bool:
        """False, если соединение уже закрыто."""
        frame = encode_frame(opcode, payload)
        with self._lock:
            if self.closed:
                return False
            self._wfile.write(frame)
            self._wfile.flush()
            return True

    def close(self, code: int = 1000) -> None:
        """Отправить кадр закрытия, если он ещё не отправлен. Обрыв связи не ошибка."""
        with self._lock:
            if self.closed:
                return
            self.closed = True
            try:
                self._wfile.write(encode_frame(CLOSE, struct.pack("!H", code)))
                self._wfile.flush()
            except OSError:
                pass
