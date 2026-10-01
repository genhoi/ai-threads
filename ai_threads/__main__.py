"""Запуск: python3 -m ai_threads --port 8765."""

import argparse
import fcntl
import sys

from . import i18n, settings
from .config import data_dir
from .server import create_server

# Язык сообщений в терминале — настройка language или переменные окружения (i18n.current без запроса).
i18n.add({
    "main.description": {"ru": "Нить — локальный каталог сессий", "en": "Nit — a local catalog of agent sessions"},
    "main.port_help": {"ru": "Порт сервера, по умолчанию 8765", "en": "Server port, 8765 by default"},
    "main.bad_port": {"ru": "порт должен быть от 1 до 65535", "en": "the port must be from 1 to 65535"},
    "main.on_port": {"ru": " на порту {port}", "en": " on port {port}"},
    "main.running": {"ru": "«Нить» с данными в {data} уже запущена{where}. Откройте её или остановите прежде, "
                           "чем запускать вторую.",
                     "en": "Nit with data in {data} is already running{where}. Open it, or stop it before "
                           "starting another one."},
    "main.bad_data": {"ru": "Не удалось прочитать данные «Нити» в {data}: {error}. Файлы не изменены.",
                      "en": "Couldn't read Nit data in {data}: {error}. The files were not changed."},
    "main.port_busy": {"ru": "Не удалось занять порт {port}: {error}", "en": "Couldn't use port {port}: {error}"},
    "main.bad_settings": {"ru": "Настройки не прочитаны, взяты умолчания. {problem}",
                          "en": "Couldn't read the settings, using the defaults. {problem}"},
    "main.listening": {"ru": "Нить: {url}", "en": "Nit: {url}"},
})


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
    parser = argparse.ArgumentParser(description=i18n.t("main.description"))
    parser.add_argument("--port", type=int, default=8765, help=i18n.t("main.port_help"))
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error(i18n.t("main.bad_port"))
    lock, other = lock_data_dir()
    if lock is None:
        where = i18n.t("main.on_port", port=other) if other else ""
        parser.exit(1, i18n.t("main.running", data=data_dir(), where=where) + "\n")
    try:
        server = create_server(args.port)
    except ValueError as error:
        # Store не затирает повреждённый state.json; человеку нужна причина, а не трассировка.
        parser.exit(1, i18n.t("main.bad_data", data=data_dir(), error=error) + "\n")
    except OSError as error:
        parser.exit(1, i18n.t("main.port_busy", port=args.port, error=error.strerror) + "\n")
    problem = settings.error()
    if problem:
        # Повреждённые настройки не мешают запуску: работают умолчания.
        print(i18n.t("main.bad_settings", problem=problem), file=sys.stderr, flush=True)
    lock.seek(0)
    lock.truncate()
    lock.write(str(server.server_port))
    lock.flush()
    with server:
        print(i18n.t("main.listening", url=f"http://127.0.0.1:{server.server_port}"), flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
