// Типы API, те же, что в internal/server/api.go. Имена полей — как в JSON, через подчёркивание.
// Время — секунды Unix. Меняя api.go, меняйте и здесь.
import type { AgentId, SessionState } from '../model';

export interface SessionView {
  key: string;
  agent: AgentId;
  id: string;
  /** Своё название, иначе из журнала, иначе пусто */
  title: string;
  /** Название из журнала, если человек дал своё */
  original?: string;
  project: string;
  /** Корень основного репозитория */
  repo?: string;
  cwd: string;
  branch?: string;
  /** Сессия работает в копии */
  worktree?: WorktreeView;
  created: number;
  updated: number;

  state: SessionState;
  /** С какого момента в этом состоянии */
  since?: number;
  /** Для error */
  exit_code?: number;
  /** «новое» */
  unseen?: boolean;
  /** Последний ответ агента */
  last?: string;
  /** Последняя строка терминала, для «Работают» */
  last_line?: string;
  /** Терминал открыт в «Нити» */
  terminal: boolean;
  /** Сессия идёт вне «Нити»: terminal или background */
  where?: string;

  pinned?: boolean;
  done?: boolean;
  hidden?: boolean;
  /** Автоматический запуск или временная папка */
  service?: boolean;
  /** Кто запустил без человека */
  by?: string;
  /** Папки сессии нет */
  missing?: boolean;
  /** Можно продолжить в терминале */
  resumable: boolean;
}

export interface WorktreeView {
  path: string;
  branch: string;
  repo: string;
}

export interface ProjectView {
  name: string;
  path: string;
  git: boolean;
  pinned?: boolean;
  hidden?: boolean;
  default_agent?: AgentId;
  /** Без служебных */
  sessions: number;
  /** Последняя активность в проекте */
  updated: number;
}

export interface AgentView {
  id: AgentId;
  name: string;
  short: string;
  enabled: boolean;
  /** Пусто — программа не найдена */
  program?: string;
  home: string;
  home_source: string;
  home_found: boolean;
  sessions: number;
  /** Сессию можно продолжить в терминале (у ZCode нельзя) */
  resumable: boolean;
  /** Умеет писать сводки */
  runner: boolean;
  skip_flag?: string;
}

export interface EnvView {
  version: string;
  addr: string;
  tmux: boolean;
  tmux_version?: string;
  tmux_path?: string;
  wsl: boolean;
  vscode: boolean;
  platform: string;
  /** Откуда пришёл запрос: с компьютера или с телефона через узел Tailscale */
  via: 'local' | 'remote';
  /** Доступ с телефона коротко, без ключа */
  remote: RemoteBrief;
  /** Файл настроек, который читает «Нить», путь целиком */
  settings_path: string;
  /** Папка данных: пометки, кэш каталога, ключи телефона */
  data_dir: string;
}

export interface ProgressView {
  done: number;
  total: number;
  ready: boolean;
  /** id → waiting, reading или done */
  agents: Record<string, string>;
}

// Настройки: internal/settings.Settings
export interface AgentSettings {
  enabled: boolean;
  home: string;
  program: string;
  flags: string;
  skip_approvals: boolean;
}

export interface RunnerSettings {
  agent: string;
  minutes: number;
}

/** Доступ с телефона через узел Tailscale: internal/settings.Remote */
export interface RemoteSettings {
  enabled: boolean;
  /** Адрес открыт и из интернета */
  funnel: boolean;
  /** Имя узла в tailnet */
  hostname: string;
  /** Каталог состояния узла; начинается с / или ~/ */
  state_dir: string;
}

export interface Settings {
  agents: Record<string, AgentSettings>;
  summary: RunnerSettings;
  digest: RunnerSettings;
  language: string;
  service: { auto_commands: string[]; temp_dirs: string[] };
  tmux: { session_name: string };
  /** Есть у сервера с доступом с телефона; экран настроек пересылает его как есть */
  remote?: RemoteSettings;
}

/** Сколько сессий всего и сколько из них служебных; all − service — длина Bootstrap.sessions */
export interface SessionCounts {
  service: number;
  all: number;
}

export interface Bootstrap {
  env: EnvView;
  agents: AgentView[];
  projects: ProjectView[];
  sessions: SessionView[];
  progress: ProgressView;
  settings: Settings;
  counts: SessionCounts;
}

// События потока /api/events
export interface SessionsEvent {
  changed: SessionView[] | null;
  removed: string[] | null;
  /** Временный ключ запущенной сессии → настоящий; старый тогда в removed, новый в changed */
  renamed?: Record<string, string>;
}

export interface NoticeEvent {
  kind: 'error' | 'waiting' | 'info';
  key?: string;
  text: string;
}

// Сводка по сессии: internal/store.Summary
export interface Summary {
  title: string;
  summary: string;
  next_step: string;
  closed: boolean;
  related: { key: string; why: string }[] | null;
  /** Секунды с долями */
  at: number;
  /** Агент, который составил сводку */
  model: string;
}

export interface Details {
  session: SessionView;
  /** Последние сообщения, не больше 60 */
  messages: MessageView[] | null;
  truncated: boolean;
  summary?: Summary;
  /** Команда продолжения в другом терминале, с cd */
  resume?: string;
  /** Подключиться к терминалу «Нити» */
  attach?: string;
  launch?: LaunchInfo;
}

export interface MessageView {
  role: 'user' | 'assistant';
  text: string;
  at?: number;
}

export interface LaunchInfo {
  at: number;
  model?: string;
  effort?: string;
}

export interface BusyView {
  key: string;
  agent: AgentId;
  title: string;
  state: SessionState;
}

export interface LaunchCheck {
  path: string;
  project: string;
  exists: boolean;
  git: boolean;
  busy: BusyView[] | null;
  /** Предложение: folder или copy */
  mode: LaunchMode;
  branch?: string;
  default_agent?: AgentId;
  recent?: BusyView[];
  worktrees?: WorktreeView[];
}

export type LaunchMode = 'folder' | 'copy';

export interface LaunchRequest {
  path: string;
  agent: AgentId;
  model?: string;
  effort?: string;
  mode: LaunchMode;
  branch?: string;
  prompt?: string;
}

export interface LaunchResponse {
  /** Пока агент не назвал свой ID, ключ временный: <агент>:new-… */
  key: string;
}

export interface FinishCheck {
  worktree?: WorktreeView;
  /** Незакоммиченные файлы копии */
  dirty: string[] | null;
  pushed: boolean;
  merged: boolean;
  /** Ключи других сессий с терминалом в этой копии */
  others: string[] | null;
  can_remove: boolean;
  can_delete_branch: boolean;
}

export interface FinishRequest {
  remove_worktree: boolean;
  delete_branch: boolean;
}

export interface ErrorView {
  error: string;
  fields?: Record<string, string>;
}

// GET /api/models?agent= — модели и уровни рассуждений агента
export interface ModelView {
  id: string;
  name: string;
  efforts?: string[];
  default_effort?: string;
}

export interface ModelsView {
  /** Что настроено в CLI агента; пусто — не знаем */
  default_model: string;
  default_effort: string;
  models: ModelView[] | null;
  /** Уровни агента, если у моделей нет своих */
  efforts: string[] | null;
}

/** GET /api/env: проверка tmux и агентов для первого запуска */
export interface EnvCheck {
  env: EnvView;
  agents: AgentView[];
}

/** Тело POST /api/sessions/{key}/rename; пустое название возвращает название из журнала */
export interface RenameRequest {
  name: string;
}

/** Тело POST /api/sessions/{key}/{pin,hide,done} */
export interface FlagRequest {
  on: boolean;
}

/** GET /api/terminal/{key}/screen: экран текстом для телефона */
export interface ScreenView {
  text: string;
}

/** POST /api/terminal/{key}/keys: сначала текст, потом клавиши по именам tmux */
export interface KeysRequest {
  text?: string;
  keys?: string[];
}

/** POST /api/reboot: продолжить сессии keys; dismiss — забыть остальные */
export interface RebootRequest {
  keys?: string[];
  dismiss?: boolean;
}

export interface RebootResult {
  resumed: string[];
  /** Ключ → почему не продолжилась */
  failed?: Record<string, string>;
}

export interface RebootSession {
  session: SessionView;
  was: SessionState;
  code?: number;
  minutes?: number;
}

export interface RebootView {
  sessions: RebootSession[];
}

/** POST /api/reveal: открыть папку сессии (key) или любую папку (path) */
export interface RevealRequest {
  key?: string;
  path?: string;
  app: 'folder' | 'vscode';
}

/** POST /api/project: пометки проекта; не заданные поля не меняются */
export interface ProjectRequest {
  path: string;
  pinned?: boolean;
  hidden?: boolean;
  /** Пустая строка снимает */
  default_agent?: string;
}

// ───────── Сводки ─────────
// Пишет агент без человека в задании. Ход приходит событиями job; готовая сводка по сессии
// сохраняется до последнего события job, сводка за несколько дней приходит событием digest.

/** Кто пишет сводку: auto или агент; пусто — из настроек */
export type RunnerChoice = 'auto' | 'kimi' | 'claude' | 'codex' | 'grok';

/** Тело POST /api/sessions/{key}/summary, необязательное */
export interface SummaryRequest {
  agent?: string;
}

/** Периоды страницы «Сводка»: 3, 5 дней и неделя */
export type DigestDays = 3 | 5 | 7;

/** Тело POST /api/digest */
export interface DigestRequest {
  days: DigestDays;
  agent?: string;
}

/** starting — готовит данные и запускает агента, reading — агент читает журналы, writing — пишет ответ; дальше итог */
export type JobStage = 'starting' | 'reading' | 'writing' | 'done' | 'failed' | 'cancelled';

/** Задание сводки: событие job и ответы на запуск */
export interface JobView {
  id: string;
  kind: 'summary' | 'digest';
  /** Сессия, для summary */
  key?: string;
  /** Период, для digest */
  days?: number;
  /** Кто пишет: kimi, claude, codex или grok */
  agent: string;
  stage: JobStage;
  /** Сколько раз агент открывал журналы */
  reads?: number;
  /** Начало, секунды Unix */
  started: number;
  /** Секунды от начала до события */
  elapsed: number;
  /** Задание закончилось: done, failed или cancelled */
  done: boolean;
  /** Почему failed */
  error?: string;
  /** Хвост вывода агента при ошибке */
  output?: string;
}

/** Ответ на запуск сводки. Если по той же сессии или периоду задание уже шло — это оно */
export interface JobStarted {
  job: JobView;
}

/** Пункт раздела сводки: что сделано и о каких сессиях */
export interface DigestItem {
  /** Одна строка с разметкой `код`, **жирный** и [текст](ключ сессии) */
  text: string;
  keys: string[];
}

/** Раздел сводки — проект */
export interface DigestProject {
  name: string;
  /** Папка проекта; пусто, если агент назвал проект не из списка */
  path: string;
  items: DigestItem[];
  /** «Осталось.»; пусто — ничего не осталось */
  left: string;
}

/** Сводка за несколько дней: internal/summary.Digest */
export interface Digest {
  days: number;
  /** Первый день периода, ГГГГ-ММ-ДД */
  from: string;
  /** Последний день — день составления */
  to: string;
  /** «Итоги за 13–15 октября» */
  title: string;
  /** Вводный абзац, с той же разметкой, что пункты */
  lead: string;
  projects: DigestProject[];
  /** По скольким сессиям составлена; у сводки первой версии 0 */
  journals: number;
  /** Когда составлена, секунды Unix */
  at: number;
  /** Кто составил: kimi, claude, codex, grok */
  model: string;
  lang: string;
  /** Сводка первой версии «Нити» */
  legacy?: boolean;
}

/** GET /api/digest?days=: сохранённая сводка за период и идущее задание */
export interface DigestView {
  /** null — сводку за этот период ещё не составляли */
  digest: Digest | null;
  /** null — задание не идёт */
  job: JobView | null;
}

/** POST /api/jobs/{id}/cancel; false — задания нет или оно уже закончилось */
export interface CancelResult {
  cancelled: boolean;
}

// ───────── Доступ с телефона ─────────
// Узел Tailscale живёт в самой «Нити». Телефон входит по ссылке с ключом и получает cookie
// устройства. Все запросы /api/remote — только с компьютера: с телефона они отвечают 403.

/**
 * off — выключен, starting — узел поднимается, needs_login — ждёт входа в Tailscale,
 * certificate — адрес есть, выпускается первый сертификат, running — телефон может войти,
 * error — не вышло (новая попытка через время, сразу — POST /api/remote {"enabled": true})
 */
export type RemoteState = 'off' | 'starting' | 'needs_login' | 'certificate' | 'running' | 'error';

/** Доступ с телефона для первой отрисовки */
export interface RemoteBrief {
  state: RemoteState;
  /** nit.<сеть>.ts.net, когда узел в сети */
  host?: string;
}

/** GET и POST /api/remote, POST /api/remote/key, POST /api/remote/devices/{id}/revoke и событие remote */
export interface RemoteView {
  /** Включён в настройках */
  enabled: boolean;
  /** Адрес открыт и из интернета */
  funnel: boolean;
  state: RemoteState;
  /** С какого момента в этом состоянии */
  since?: number;
  /** Для needs_login: ссылка входа в Tailscale */
  login_url?: string;
  /** nit.<сеть>.ts.net; в certificate и running */
  host?: string;
  /** https://<host>/ */
  url?: string;
  /** https://<host>/?key=<ключ>: ссылка входа для QR-кода */
  link?: string;
  /** Для error: причина на языке интерфейса */
  error?: string;
  /** Подключённые телефоны, последние заходившие первыми */
  devices: DeviceView[] | null;
}

/** Подключённый телефон. Имя — из User-Agent: «iPhone · Safari» */
export interface DeviceView {
  id: string;
  name: string;
  created: number;
  /** Обновляется не чаще раза в минуту */
  last_seen: number;
}

/** Тело POST /api/remote; не заданные поля не меняются */
export interface RemoteRequest {
  enabled?: boolean;
  funnel?: boolean;
}
