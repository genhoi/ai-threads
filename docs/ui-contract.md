# Контракт интерфейса

Общий для тех, кто пишет страницы, и тех, кто пишет UI-тесты. Тесты ищут элементы по
`data-testid` и видимому тексту. Страница обязана выставлять эти атрибуты; менять
имена можно только вместе с этим файлом.

## Файлы

- `static/index.html` — главный экран (разделы «Мои», «Все», «Проекты»).
- `static/digest.html` — экран «Сводка» за 3–5 дней.
- `static/settings.html` — экран «Настройки»; свои стили в `static/css/settings.css`.
- `static/css/base.css` — шрифты, переменные и общие классы обеих страниц: `.row`, `.sec.day`,
  `.btn`, `.btn-primary`, `.icon-btn`, `.menu`, `.toast`, `.msg`, `.ctx`, `.ctx-row`, `.pill`,
  `.chip-s`, `.seg-ctl`, `.kbd`…
- `static/css/digest.css` — отличия экрана «Сводка».
- `static/js/icons.js` — иконки и названия агентов: `registerAgents(list)`, `toolIcon(tool, size)`,
  `toolName(tool)`, `TOOL_NAMES`.
- Остальной JavaScript — ES-модули в `static/js/`, без сборки.

## Агенты и окружение

Обе страницы при загрузке запрашивают `GET /api/config` и до первой отрисовки списка передают
`agents` в `registerAgents`. Название, цвет и SVG-иконка агента берутся оттуда. У агента без иконки —
квадрат его цвета с первой буквой названия; у агента, которого нет в списке, такой же квадрат серого
цвета. Пока ответа нет, у Codex, Claude Code, Grok и Kimi встроенные иконки.

Из того же ответа главный экран берёт:
- `env.wsl` — есть ли формат «вкладки Windows Terminal» и пункт меню «Открыть папку в проводнике»;
- `env.code` — есть ли пункт «Открыть в VS Code»;
- `env.tmux` и `settings.open_with` — какой формат восстановления выбран сначала;
- `settings.tmux_session` — имя tmux-сессии в пояснении под скриптом;
- `runners` и `settings.summary.agent` — кто составит сводку по сессии и можно ли её составить;
- `runners` и `settings.search.agent` — кто проведёт умный поиск и доступен ли он.

## Язык

Язык страницы (`ru` или `en`) приходит в `GET /api/config` полем `language`: это настройка
`language`, а при `auto` — язык браузера из Accept-Language. Страница вызывает
`setLanguage(config.language)` до первой отрисовки списка; экран загрузки до ответа сервера — на языке
браузера. Тексты страниц лежат в словарях `static/js/lang/<страница>.js` (`{ru: {...}, en: {...}}`) и
берутся через `t()` из `static/js/i18n.js`, даты и время форматируются через `locale()`. Сообщения
сервера выводятся как есть. В UI-тестах фикстура `page` открывает браузер с `locale='ru-RU'`,
`page_en` — с `locale='en-US'`; обе задают Accept-Language явно, потому что WebKit не передаёт его в
запросы, отправленные тестом через `route.fetch()`.

Копирование в буфер: `navigator.clipboard.writeText`, при ошибке — `document.execCommand("copy")`
через временное `textarea`. Тесты подменяют `navigator.clipboard`.

## Главный экран (`index.html`)

Шапка:
| testid | элемент |
|---|---|
| `nav-mine`, `nav-all`, `nav-projects`, `nav-digest` | ссылки «Мои», «Все N», «Проекты N», «Сводка»; активная — класс `on` |
| `wait-next` | «N ждёт вас» / «N ждут вас»; щелчок открывает следующую ждущую сессию |
| `server-addr` | `127.0.0.1:<порт>` |
| `data-menu`, `data-export`, `data-import`, `data-import-file` | «Данные» и пункты меню; `data-import-file` — скрытый `input[type=file]` |
| `settings-link` | иконка-шестерёнка справа от «Данные», ссылка на `/settings.html`; такая же на экране «Сводка» |

Левая колонка:
| testid | элемент |
|---|---|
| `search` | поле поиска `#q` |
| `search-conv` | переключатель «+ переписка», `aria-pressed` |
| `feed-actions` | строки-действия вверху ленты, пока в поле есть запрос (кроме раздела «Проекты»); стрелки ↑/↓ из поля выделяют их вместе с лентой (класс `is-sel`), Enter запускает выделенную |
| `search-full` | «Искать «<запрос>» в журналах целиком» — полный поиск без агента; подпись «↵», если обычный поиск ничего не нашёл: тогда Enter в поле запускает его, иначе Enter открывает выбранную сессию |
| `smart-search` | «Спросить агента: «<запрос>»», подпись «Ctrl+↵»; неактивна при запросе длиннее 500 символов или пустом `runners`. То же — Ctrl+Enter в поле. Область поиска — `all` в разделе «Все», иначе `mine` |
| `choice-search`, `choice-summary` | кнопка-меню справа от «Спросить агента» и рядом с «Составить», «Повторить», «Обновить сводку»: «Авто» или «Codex · gpt-6.1-sol · xhigh». Выбор хранится в `localStorage` (`nit.choice.search`, `nit.choice.summary`), в запрос уходят непустые `agent`, `model`, `effort` |
| `choice-menu`, `choice-agent`, `choice-model`, `choice-effort` | меню выбора; у пунктов `data-value` (пустое — «Авто» или «как в CLI (…)») и `aria-checked`. Агенты — «Авто» и `runners`; модели и уровни — из `GET /api/models?agent=` при открытии меню; у Codex уровни своей модели, у Kimi пункта уровня нет |
| `smart-progress`, `smart-cancel` | панель над лентой, пока идёт умный поиск: запрос, иконка агента, «<агент> <шаг>», прошедшее время, «Остановить» |
| `smart-log` | «Журнал агента» на панелях умного поиска: возвращает справа «сессию агента» этого поиска; не показывается, пока она на экране |
| `smart-preliminary` | «предварительно, агент отбирает…» над кандидатами из события `candidates`; в их строках `smart-why` — «совпало: <слова>»; событие `result` заменяет их отобранными |
| `smart-plan` | строка плана после события `plan`: «Ищу: слово · слово. Агенты: … Проекты: …» |
| `smart-warning` | тихая строка предупреждения: агент не разобрал запрос или не отобрал сессии |
| `smart-error`, `smart-retry` | ошибка поиска в панели, «Повторить» тот же запрос; крестик `smart-close` убирает панель |
| `smart-head`, `smart-close` | результат: «Умный поиск: «<запрос>»», «N сессий · отобрал <агент>» или «· совпадения по словам»; крестик возвращает обычную ленту |
| `smart-why` | в строке результата вместо последнего ответа — почему сессия подходит |
| `smart-empty` | «Подходящих сессий нет. Искал: <слова>» |
| `full-progress`, `full-bar`, `full-cancel` | идёт полный поиск: «Ищу «<запрос>» в журналах: N из M» по событиям `progress`, полоса по `done/total`, время, «Остановить» |
| `full-error`, `full-retry` | ошибка полного поиска, «Повторить»; крестик `full-close` убирает панель |
| `full-head`, `full-close` | результат: «Полный поиск: «<запрос>»», «N сессий»; крестик возвращает обычную ленту |
| `full-snippet` | в строке результата вместо последнего ответа — фрагмент журнала, найденные слова в `<mark>` |
| `full-empty` | «В журналах «<запрос>» нет» |
| `pick-yesterday`, `pick-enter` | «Вчерашние · N», «Выбрать…» |
| `toggle-hide-done`, `toggle-last` | скрыть завершённые; последний ответ агента в строке; `aria-pressed` |
| `pick-count`, `pick-today`, `pick-none`, `pick-done`, `pick-next` | панель режима выбора |
| `migrate-banner`, `migrate-button` | полоса переноса из старой версии |
| `project-filter`, `project-filter-clear` | фильтр ленты по проекту |
| `loading`, `loading-progress` | первое чтение журналов: «Читаю журналы сессий», «412 из 659 · …» |
| `row` | строка сессии, атрибут `data-key`; выбранная — класс `is-sel`; в режиме выбора `aria-pressed` |
| `pinned-section` | блок «Закреплённые» |
| `feed-end`, `no-results` | «Раньше сессий нет…», «Ничего не найдено по «…»» |
| `project-row` | строка списка проектов, атрибут `data-project`; у сессий без проекта он пустой, подпись «Без проекта» |

Правая часть, сессия (`session`, атрибут `data-key`):
| testid | элемент |
|---|---|
| `back-to-list` | «← Список» в узком окне |
| `session-title`, `session-orig`, `session-meta` | название, «Исходное: …», строка инструмента, проекта, ветки, времени, «запустил …» |
| `rename`, `rename-input`, `rename-save`, `rename-cancel`, `rename-reset` | переименование |
| `pin` | звезда, `aria-pressed` |
| `session-menu`, `menu-copy-id`, `menu-copy-path`, `menu-explorer`, `menu-vscode`, `menu-done`, `menu-hide` | меню действий; `menu-explorer` только в WSL (`env.wsl`), `menu-vscode` только если есть программа `code` (`env.code`) |
| `command` | блок команды продолжения, `data-command` — полный текст команды; ID сессии выделен там, где стоит в команде, флаги после него выводятся как есть |
| `copy-command` | «Скопировать» / «Скопировано», клавиша C |
| `resume-hint` | вместо блока команды, если сессию нельзя продолжить из терминала (`command_no_cd` пустая): подпись из `resume_hint`, например «Продолжить можно только в приложении ZCode» |
| `live-banner` | «Агент ждёт вашего ответа» / «Агент работает» |
| `missing-banner`, `copy-no-cd` | папки нет; «Скопировать без cd»; только у сессии с командой |
| `summary-row` | строка «О чём»; иконка — агента, который составил сводку (`model` сводки), во время работы — агента из события прогресса (`agent`), до первого события — того, кто составит сводку |
| `summary-run` | «Составить» (сводки нет); неактивна, если `runners` пуст |
| `summary-no-runner` | вместо «Сводки пока нет», если `runners` пуст: «Нет агента для сводок: установите …» |
| `summary-progress` | идёт сводка: «<агент> <шаг>», время, полоска шагов |
| `summary-error`, `summary-retry` | «Сводка не получилась: …», «Повторить» |
| `summary-log` | ссылка «журнал агента» в строке «О чём»: пока сводка составляется и после, пока сервер помнит задание `summary:<ключ>` |
| `summary-text`, `summary-toggle`, `summary-title`, `summary-apply`, `summary-applied`, `summary-refresh` | готовая сводка |
| `next-step`, `copy-next` | «Следующий шаг», «Скопировать для агента» |
| `closed-hint` | «<агент>: похоже, задача закрыта.» |
| `related-chip` | связанная сессия, атрибут `data-key` |
| `conversation` | прокручиваемый контейнер переписки |
| `message` | сообщение, атрибут `data-role` = `user` или `assistant` |
| `message-toggle` | «Показать полностью» / «Свернуть» |
| `conversation-empty` | «У этой сессии нет сообщений…» |

Правая часть, сессия агента (`agent-session`, атрибут `data-log` — id задания): журнал умного поиска
или сводки на месте переписки. Открывается при запуске умного поиска (в узком окне — кнопкой `smart-log`),
кнопками `smart-log` и `summary-log`; щелчок по сессии в ленте возвращает сессию.
| testid | элемент |
|---|---|
| `agent-back` | «← К сессии» у журнала сводки |
| `back-to-list` | «← Список» у журнала умного поиска в узком окне |
| `agent-title` | «Умный поиск: «<запрос>»» или «Сводка: <название сессии>» |
| `agent-meta`, `agent-name`, `agent-model`, `agent-state`, `agent-time` | иконка и имя агента, модель из `trace` `model`, «идёт» / «закончено», время |
| `agent-stop` | «Остановить», пока идёт поиск, сводка или ответ агенту |
| `agent-log` | прокручиваемая переписка журнала, новое дописывается снизу |
| `log-prompt` | запрос агенту от «Нити», свёрнут (`details`); каждый запрос начинает новый запуск |
| `log-user` | сообщение человека агенту |
| `log-text`, `log-thinking` | ответ агента; рассуждение приглушённо. Длинные свёрнуты, `log-more` — «Показать полностью» / «Свернуть» |
| `log-tool`, `log-result` | вызов инструмента (имя и аргументы моноширинным); его результат, свёрнут |
| `log-model`, `log-usage`, `log-nit` | модель, расход токенов, строки «Нити» — мелким серым |
| `log-warning`, `log-error`, `log-stopped` | предупреждение и ошибка задания, «Остановлено.» |
| `reply-input`, `reply-send` | «Написать агенту…»: после `agent_session`, когда запуск закончился; Enter — отправить, Shift+Enter — новая строка; неактивно, пока агент отвечает |
| `reply-command`, `reply-copy-command` | «Продолжить в терминале: $ <command из agent_session>», «Скопировать» |

Журнал копит события из подписок на задания, которые страница уже держит; второй поток к идущему
заданию она не открывает. Ответ агенту — `POST /api/agent/reply`; его события дописываются в тот же
журнал, а id заданий ответа и тексты лежат в `sessionStorage` (`nit.log.replies`). После перезагрузки
журнал собирается из событий заданий с `since=0`, пока сервер их помнит.

Правая часть, восстановление (`restore`):
| testid | элемент |
|---|---|
| `picked-row` | выбранная сессия, атрибут `data-key`, кнопка удаления внутри; у сессии без команды пометка «нельзя продолжить из терминала», в скрипт она не входит |
| `fmt-wt`, `fmt-tmux`, `fmt-plain` | формат, `aria-pressed`; `fmt-wt` только в WSL. Сначала выбран `settings.open_with`; при `auto` — Windows Terminal в WSL, иначе tmux, если он установлен, иначе список команд |
| `copy-script`, `script` | «Скопировать скрипт» (клавиша C), текст скрипта |

Приложение сессии не открывает: скрипт копируется и запускается вручную.

Умный поиск занимает 20–90 секунд. Пока он идёт, запрос и номер задания лежат в `sessionStorage`
(`nit.smart.job`): после перезагрузки страница подключается к событиям задания из `GET /api/jobs`
(`kind: "search"`) или к заданию с этой вкладки. Результат лежит там же (`nit.smart.result`), пока
его не закрыли крестиком. Новый поиск заменяет прошлый результат, а на сервере останавливает идущий.
Полный поиск (`POST /api/full-search`) хранится так же: `nit.full.job` и `nit.full.result`. Умный и
полный поиск показывают результат в одной ленте, поэтому новый закрывает прежний. Новый ввод в поле
результат не сбрасывает.

Прочее: `toast`, `toast-undo` («Вернуть»).

Узкое окно: ширина меньше 900 px — одна колонка; список и сессия переключаются, горизонтальной
прокрутки страницы нет.

## Экран «Сводка» (`digest.html`)

| testid | элемент |
|---|---|
| `days-3`, `days-4`, `days-5` | период, `aria-pressed` |
| `model-<id>` | модель: по кнопке на каждый элемент `GET /api/digest/models` в том же порядке; `aria-pressed`; `disabled` и подсказка `hint`, если агент не установлен. Сначала выбран `settings.digest.agent`, если он установлен, иначе модель, выбранная здесь раньше, иначе первая доступная |
| `digest-meta`, `digest-copy`, `digest-regen`, `digest-stop` | строка «когда и кем составлена», «Скопировать текстом», «Составить заново», «Остановить» |
| `digest-running`, `digest-stages`, `digest-log`, `digest-notes`, `digest-tokens` | идёт составление |
| `digest-error`, `digest-retry`, `digest-fallback` | ошибка, «Повторить», «Составить через …» |
| `digest-lead`, `digest-project`, `digest-tail`, `digest-auto` | готовая сводка; `digest-tail` — строка с чекбоксом, атрибут `data-tail` |
| `digest-sessions`, `digest-session` | правая колонка «По сессиям»; у строки `data-key` и `data-status` |
| `digest-empty` | сводки за период ещё нет |

Журнал агента: кнопка `digest-agent-log` (на экране хода, у ошибки и в заголовке готовой сводки, пока
сервер помнит запуск) показывает на месте сводки «сессию агента» с теми же testid, что на главном
экране (`agent-session`, `agent-back`, `agent-log`, `log-*`, `reply-input`, `reply-send`,
`reply-command`, `reply-copy-command`). Модель и уровень рассуждений — `digest-agent-model` и
`digest-effort`, выбор хранится в `localStorage` (`nit.digest.choice`).

## Экран «Настройки» (`settings.html`)

Форма собирается из `GET /api/config`, `GET /api/sessions` и `GET /api/digest/models`. «Сохранить»
отправляет `POST /api/settings {settings}` — весь объект настроек без значений по умолчанию; ответ —
новый `/api/config`. «Проверить снова» запрашивает `GET /api/config?refresh=1`: сервер перечитывает
журналы и отвечает со свежим числом сессий. Раздел — в адресе `#section=agents|summaries|list|about`;
уже 900 px сначала показан список разделов.

| testid | элемент |
|---|---|
| `settings-link` | шестерёнка в шапке, `aria-current="page"` |
| `section-agents`, `section-summaries`, `section-list`, `section-about` | пункты меню разделов |
| `section-title`, `back-to-menu`, `settings-path`, `settings-error` | заголовок раздела, «← Настройки» в узком окне, путь к файлу настроек, предупреждение о нечитаемом файле |
| `check-note`, `check-again`, `agents-empty`, `searched-path`, `missing-heading` | «проверено …», «Проверить снова», пустое состояние и строка PATH в нём, заголовок «НЕ НАЙДЕНЫ» |
| `agent-card` (атрибут `data-agent`) | карточка агента; внутри `agent-status`, `agent-dirty`, `agent-enabled` (`role="switch"`), `agent-open`, `agent-home`, `home-env`, `agent-program`, `program-warn`, `resume-hint` |
| `agent-skip` (атрибут `data-agent`), `preview-label`, `preview` | флажок «Запускать без подтверждений» (`agents.<id>.skip_approvals`) и пример команды; у Codex второй пример — на сессии из `codex exec` |
| `sum-summary`, `sum-digest`, `sum-search`, `sum-common` | блоки раздела «Сводки и поиск»; внутри `pick-auto`, `pick-<агент>`, `timeout`, `no-runner`, а также язык `lang-auto`, `lang-ru`, `lang-en` |
| `hidden-dirs`, `hidden-dir`, `hidden-dir-remove`, `hidden-empty`, `add-dir-input`, `add-dir`, `hidden-count` | раздел «Список сессий» |
| `about`, `about-addr`, `about-data`, `about-settings`, `about-version`, `about-github` | раздел «О приложении» |
| `savebar`, `save-count`, `save-problems`, `save-error`, `revert`, `save` | полоса несохранённых изменений |
| `toast` | уведомление «Сохранено», «Проверка закончена: …» |

## Адрес страницы

Главный экран держит состояние в `location.hash` как параметры: `#section=mine|all|projects`,
`&session=<ключ>`, `&project=<имя>` (пустое имя — сессии без проекта), `&view=list|session|agent`,
`&log=<id задания>` — чья «сессия агента» открыта справа (`search:<n>` или `summary:<ключ>`).
Ссылка с другой страницы на сессию: `/#section=all&session=<ключ в encodeURIComponent>&view=session`.
