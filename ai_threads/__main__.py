"""Запуск: python3 -m ai_threads --port 8765."""

import argparse
import fcntl
import sys

from . import settings
from .config import data_dir
from .server import create_server


def lock_data_dir():
    """Файл блокировки в папке данных. Два сервера с одной папкой затирали бы изменения
    друг друга: у каждого своя копия state.json в памяти. Возвращает (открытый файл, порт
    другого сервера); блокировка держится, пока процесс жив."""
    path = data_dir() / "server.lock"
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = open(path, "a+", encoding="utf-8")
    try:
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        handle.seek(0)
        other = handle.read().strip()
        handle.close()
        return None, other
    return handle, ""


def main():
    parser = argparse.ArgumentParser(description="Нить — локальный каталог сессий")
    parser.add_argument("--port", type=int, default=8765, help="Порт сервера, по умолчанию 8765")
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error("порт должен быть от 1 до 65535")
    lock, other = lock_data_dir()
    if lock is None:
        where = f" на порту {other}" if other else ""
        parser.exit(1, f"«Нить» с данными в {data_dir()} уже запущена{where}. Откройте её или остановите "
                       "прежде, чем запускать вторую.\n")
    try:
        server = create_server(args.port)
    except ValueError as error:
        # Store не затирает повреждённый state.json; человеку нужна причина, а не трассировка.
        parser.exit(1, f"Не удалось прочитать данные «Нити» в {data_dir()}: {error}. Файлы не изменены.\n")
    except OSError as error:
        parser.exit(1, f"Не удалось занять порт {args.port}: {error.strerror}\n")
    problem = settings.error()
    if problem:
        # Повреждённые настройки не мешают запуску: работают умолчания.
        print(f"Настройки не прочитаны, взяты умолчания. {problem}", file=sys.stderr, flush=True)
    lock.seek(0)
    lock.truncate()
    lock.write(str(server.server_port))
    lock.flush()
    with server:
        print(f"Нить: http://127.0.0.1:{server.server_port}", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
