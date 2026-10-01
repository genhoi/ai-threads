# Как устроена «Нить»

Локальное веб-приложение. Сервер на Python без сторонних пакетов читает журналы агентов и отдаёт
JSON. Страница — HTML, CSS и JavaScript без сборки и фреймворков, файлы в `static/`. Интерфейс и
сообщения — на русском, имена в коде — на английском.

## Модули

| Файл | Что делает |
|---|---|
| `ai_threads/__main__.py` | запуск `python3 -m ai_threads --port 8765` |
| `ai_threads/server.py` | HTTP API и статика, проверка `Host` и `Origin` |
| `ai_threads/agents/` | агенты: где лежат сессии, как их читать и продолжать, как запустить агента для сводки |
| `ai_threads/sources/` | общий разбор журналов (`common.py`) и обращение к агентам по id |
| `ai_threads/catalog.py` | фоновое чтение всех агентов, кеш разбора, поиск по переписке |
| `ai_threads/model.py` | `Session` — сессия в том виде, в каком её видят остальные модули |
| `ai_threads/settings.py` | настройки пользователя из `settings.json` |
| `ai_threads/store.py` | пользовательские данные: названия, закрепления, отметки, сводки |
| `ai_threads/live.py` | статусы запущенных сессий по реестрам агентов |
| `ai_threads/terminal.py` | скрипт восстановления сессий, проводник Windows и VS Code |
| `ai_threads/jobs.py` | фоновые задания с событиями, отменой и пределом времени |
| `ai_threads/runner.py` | выбор и запуск агента для сводок, общий поток событий |
| `ai_threads/summary.py`, `digest.py` | сводка по сессии и сводка за 3–5 дней |
| `ai_threads/search.py` | умный поиск: разбор запроса агентом, поиск слов в журналах, отбор агентом |
| `bin/nit-resume` | открывает сессию во вкладке Windows Terminal из скрипта восстановления |
| `summary-agent.md`, `digest-agent.md`, `search-*-agent.md` | инструкции модели для сводок и умного поиска |

## Агенты

Агент — объект класса `Agent` из `ai_threads/agents/base.py`, по модулю на агента. Реестр и порядок
агентов — список `_ORDER` в `ai_threads/agents/__init__.py`. Остальные модули и страница берут
список агентов только из реестра: страница получает названия, цвета и иконки из `GET /api/config`.

Что задаёт агент:

- `id`, `name`, `color`, `icon` (SVG 24×24);
- `home_env` и `home_default` — переменная окружения и папка с сессиями по умолчанию;
- `programs`, `program_fallbacks` — имя программы в `PATH` и запасные пути от папки агента;
- `resume`, `resume_auto`, `skip_flag` — команда продолжения (ID подставляется через `shlex.quote`) и
  флаг, который добавляет галочка «Запускать без подтверждений» (`agents.<id>.skip_approvals`). Команду
  собирает код, своих шаблонов в настройках нет. Пустая `resume` значит, что из терминала сессию не
  продолжить, тогда интерфейс показывает `resume_hint`;
- `discover()` и `scan(cache, units, on_unit)` — единицы чтения (обычно файлы) и сессии из них;
- `transcript(session, limit_bytes)` и `excerpt_messages(session)` — переписка для экрана, поиска и
  выдержек в сводке;
- `live(sessions)` — запущенные сейчас сессии, если агент ведёт их реестр;
- `runner`, `headless(...)`, `stream_events(row)` — запуск без человека для сводок и перевод строк
  его потока в общие события.

Агенты с журналом в построчном JSON наследуют `JsonlAgent`: у них общие обход файлов по шаблону
`pattern`, кеш по размеру и времени изменения файла и разбор начала (1 МБ) и конца (256 КБ) журнала.
Агенту остаётся описать, как достать сообщение из строки (`message_of`), данные сессии
(`read_info`, `describe`) и индексы названий (`indexes`, `finish`).

Агенты с базой SQLite (ZCode, Cursor Agent) наследуют `SqliteAgent` из `agents/sqlite.py`: база
открывается только для чтения (`mode=ro`, а если журнал WAL пуст — ещё и `immutable=1`, чтобы рядом
не появлялись файлы `-wal` и `-shm`), переписка ограничена 200 сообщениями, занятая или битая база
не роняет каталог — остаются сессии из кеша.

Как подключить нового агента:

1. Создать `ai_threads/agents/<id>.py` с подклассом `Agent` или `JsonlAgent` и объектом `AGENT`.
2. Добавить `AGENT` в `_ORDER` в `ai_threads/agents/__init__.py`.
3. Положить синтетические фикстуры в `tests/fixtures/<id>/` и написать тесты разбора. Автофикстура
   `isolated_agents` в `tests/conftest.py` сама направит папку нового агента во временную.

### Источники

| Агент | Сессии | Название | Признак автоматического запуска |
|---|---|---|---|
| Codex | `sessions/**/rollout-*.jsonl`, id — последние 36 символов имени | `session_index.jsonl`, иначе первое сообщение | `session_meta.payload.source == "exec"`; подагенты (`source` — объект) пропускаются |
| Claude Code | `projects/*/<uuid>.jsonl` | строка `ai-title` в журнале, иначе `history.jsonl`, иначе первое сообщение | `entrypoint == "sdk-cli"` |
| Grok | `sessions/**/summary.json`, переписка в `chat_history.jsonl` | `generated_title`, иначе `session_summary` | `session_kind == "headless"`; `subagent` пропускается |
| Kimi | `sessions/*/session_*/state.json`, переписка в `agents/main/wire.jsonl` | `title`, иначе `lastPrompt` | ни один запрос сессии не встречается в `user-history/*.jsonl` |
| ZCode | база `cli/db/db.sqlite`, таблицы `session`, `message`, `part` | `session.title` | нет; подагенты и прогоны workflow (заполнен `parent_id`) пропускаются, ответвления и побочные чаты остаются |
| Cursor Agent | `chats/<md5 папки>/<id>/meta.json` и `store.db` | `meta.json`, иначе название из `store.db` | нет |

Временная сессия — та, что запущена в папке из `temp_dirs` (по умолчанию `/tmp`, `/var/tmp`,
`~/.local`). Раздел «Мои» — без автоматических, временных и скрытых; «Все» — все.

## Данные

- `~/.config/ai-threads/settings.json` (`AI_THREADS_CONFIG`) — настройки, только отличия от
  умолчаний. Поля и умолчания — `DEFAULTS` в `ai_threads/settings.py` и таблица в README.
- `~/.local/share/ai-threads/` (`AI_THREADS_DATA`):
  - `state.json` — названия, закрепления, отметки, сводки. Запись через временный файл, `fsync` и
    `os.replace`. Повреждённый файл не затирается: сервер не запустится и назовёт причину.
  - `cache.json` — кеш разбора журналов; повреждённый молча пересобирается.

## API

Ответы — JSON в UTF-8, потоки заданий — NDJSON. Сервер слушает только `127.0.0.1`; у всех запросов
`Host` — `127.0.0.1:<порт>` или `localhost:<порт>`, у `POST` ещё и `Origin` того же адреса. Тело
`POST` — до 1 МБ у импорта и до 16 КБ у остальных. Браузер передаёт ключ сессии `<агент>:<id>`, пути
сервер находит сам.

| Метод и путь | Что делает |
|---|---|
| `GET /api/sessions` | `{ready, progress, sessions, port}`; повторный вызов перечитывает только изменённые файлы, не чаще раза в 3 секунды |
| `GET /api/config` | агенты (найдена ли папка и программа, число сессий, команды), `runners` — кто может составить сводку, `env` — WSL, tmux, VS Code, настройки |
| `POST /api/settings` `{settings}` | записать настройки целиком; ответ — как у `/api/config` |
| `GET /api/state`, `GET /api/export` | пользовательские данные; выгрузка — файлом |
| `POST /api/pin`, `/api/name`, `/api/done`, `/api/hide` | закрепить, переименовать, отметить завершённой, скрыть |
| `POST /api/import`, `/api/migrate` | загрузить выгрузку; перенести данные старой версии из `localStorage` |
| `GET /api/session?key=` | последние 60 сообщений и признак, что журнал прочитан не целиком |
| `GET /api/search?q=&scope=mine\|all` | поиск по переписке: `[{key, snippet}]` |
| `GET /api/resolve?key=` | `{cwd, command, command_no_cd, tool, project}` для `bin/nit-resume` |
| `GET /api/restore-script?keys=&fmt=wt\|tmux\|plain` | скрипт восстановления; `wt` только в WSL |
| `POST /api/reveal` `{key, app}` | `explorer` — папка в проводнике (только WSL), `vscode` — папка в VS Code |
| `GET /api/live` | статусы запущенных сессий `{key: {state, where, since}}` |
| `POST /api/summary` `{key}` | сводка по сессии; `{job}` |
| `GET /api/digest?days=`, `POST /api/digest` `{days, model}` | сводка за 3–5 дней: сохранённая и новая |
| `GET /api/digest/models` | агенты для сводок и установлены ли они |
| `POST /api/digest/tail` `{id, done}` | отметить пункт «Что осталось сделать» |
| `POST /api/smart-search` `{q, scope}` | умный поиск; новый запрос останавливает идущий; `{job}` |
| `GET /api/jobs`, `GET /api/jobs/<job>/events?since=N`, `POST /api/jobs/<job>/cancel` | идущие задания, их события с номера `N`, отмена |

## Сводки

`runner.py` выбирает агента (из запроса, из настроек или первого установленного из Kimi, Claude
Code, Codex, Grok), запускает его во временной папке и разбирает поток. Инструкция — файл
`summary-agent.md` или `digest-agent.md`: Kimi получает его ключом `--agent-file`, остальным он
идёт в начале запроса.

| Агент | Команда |
|---|---|
| Kimi | `kimi --model <kimi_model> --agent-file <инструкция> --add-dir <папка журнала> --output-format stream-json --prompt <запрос>` |
| Claude Code | `claude -p --output-format stream-json --verbose --tools Read --permission-mode dontAsk --add-dir <папка журнала>`, запрос в stdin |
| Codex | `codex exec --json -s read-only --skip-git-repo-check -o answer.json -`, запрос в stdin |
| Grok | `grok -p <запрос> --sandbox read-only --output-format streaming-messages-json` |

Журналы ZCode и Cursor Agent лежат в SQLite, поэтому для сводки переписка сначала выгружается
текстом во временную папку. Сводка за несколько дней кладёт список сессий с выдержками в файл
`digest-input.txt`: у CLI есть предел длины аргумента.

## Умный поиск

`search.py` в три шага. Агент получает запрос, список агентов и проектов и возвращает слова для
поиска (на русском и английском, имена из кода как есть), агентов, проекты и период. Приложение ищет
слова во всём журнале каждой подходящей сессии, включая вывод команд и прочитанные файлы (у агентов с
SQLite — в переписке), и оценивает совпадения: вес слова тем больше, чем в меньшем числе сессий оно
встречается; совпадение в названии и сводке весит больше. Тридцать лучших кандидатов с фрагментами
переписки уходят агенту, он выбирает подходящие и пишет `why`. Если агент не разобрал запрос, поиск
идёт по словам запроса; если не удался отбор, показываются кандидаты по оценке.

События задания: `progress` (шаг сводки по сессии), `stage`, `session`, `note`, `log`, `tokens`
(сводка за несколько дней), `plan` и `warning` (умный поиск), `result`, `error` (с кодом и хвостом вывода CLI). У каждого события
есть порядковый номер `n`, поэтому страница может переподключиться к потоку.
