// Данные режима ?mock: те же сессии и проекты, что в макетах (Screens.dc.html и соседние).
import type {
  AgentView,
  Bootstrap,
  Details,
  Digest,
  EnvView,
  FinishCheck,
  JobView,
  MessageView,
  ModelsView,
  ProjectView,
  RebootSession,
  RemoteView,
  SessionView,
  Settings,
  Summary,
} from '../api/types';
import type { AgentId } from '../model';

const HOME = '/home/dev';
const P = (name: string, base = 'projects') => `${HOME}/${base}/${name}`;
const WT = (project: string, branch: string) => `${HOME}/.nit/worktrees/${project}/${branch}`;

export interface MockWorld {
  base: number;
  env: EnvView;
  /** Терминалы, которых не стало после перезагрузки (?mock=reboot) */
  reboot: RebootSession[];
  agents: AgentView[];
  projects: ProjectView[];
  sessions: Map<string, SessionView>;
  details: Map<string, Omit<Details, 'session'>>;
  finish: Map<string, FinishCheck>;
  settings: Settings;
  /** Сводки за период по числу дней */
  digests: Map<number, Digest>;
  /** Задания сводок по id */
  jobs: Map<string, JobView>;
  /** Доступ с телефона */
  remote: RemoteView;
  /** Ключ входа с телефона */
  remoteKey: string;
  /** Телефон отключён: все запросы отвечают 401 */
  revoked: boolean;
}

function agent(id: AgentId, name: string, short: string, sessions: number, extra: Partial<AgentView> = {}): AgentView {
  return {
    id,
    name,
    short,
    enabled: true,
    program: `/usr/local/bin/${id === 'cursor' ? 'cursor-agent' : id}`,
    home: `${HOME}/.${id}`,
    home_source: 'default',
    home_found: true,
    sessions,
    resumable: true,
    runner: ['claude', 'codex', 'grok', 'kimi'].includes(id),
    ...extra,
  };
}

/** Сценарии ?mock=…: reboot — после перезагрузки, empty — первый запуск без tmux и агентов */
export interface MockModes {
  reboot?: boolean;
  empty?: boolean;
  /** Агенты стоят, но все выключены в настройках */
  agentsOff?: boolean;
  /** Сводок за период ещё нет */
  noDigests?: boolean;
  /** Состояние доступа с телефона: off, needs_login, certificate, running, error */
  remote?: RemoteView['state'];
  /** Страница открыта с телефона через узел Tailscale */
  phone?: boolean;
}

/** Мир макета относительно момента base (секунды Unix) */
export function createWorld(base: number, modes: MockModes = {}): MockWorld {
  // «сегодня, 9:41» в макете при base = 10:00: сдвиги считаются от base
  const min = 60;
  const hour = 3600;
  const day = 86400;
  const today = (h: number, m: number) => {
    const d = new Date(base * 1000);
    d.setHours(h, m, 0, 0);
    return Math.min(base - 30, d.getTime() / 1000);
  };
  const daysAgo = (n: number, h: number, m: number) => {
    const d = new Date(base * 1000);
    d.setDate(d.getDate() - n);
    d.setHours(h, m, 0, 0);
    return d.getTime() / 1000;
  };

  const s = (v: Partial<SessionView> & Pick<SessionView, 'key' | 'agent' | 'title' | 'project' | 'cwd'>): SessionView => ({
    id: v.key.split(':')[1] ?? v.key,
    created: base - day,
    updated: base - hour,
    state: 'idle',
    terminal: false,
    resumable: true,
    repo: v.repo ?? v.cwd,
    ...v,
  });

  const sessions: SessionView[] = [
    s({
      key: 'claude:4f1c2a9e-10300',
      agent: 'claude',
      title: 'BACKEND-10300: лимиты запросов для ключей API',
      project: 'backend',
      repo: P('backend'),
      cwd: WT('backend', 'BACKEND-10300'),
      branch: 'BACKEND-10300',
      worktree: { path: '~/.nit/worktrees/backend/BACKEND-10300', branch: 'BACKEND-10300', repo: P('backend') },
      created: today(9, 41),
      updated: base - 2 * min,
      state: 'waiting',
      since: base - 2 * min,
      unseen: true,
      last: 'Выполнить make migrate DB=local?',
      terminal: true,
    }),
    s({
      key: 'cursor:a2d4e6f8-android',
      agent: 'cursor',
      title: 'Падение на Android 14 с камерой',
      project: 'mobile-app',
      cwd: P('mobile-app'),
      branch: 'fix/camera-android14',
      created: today(8, 31),
      updated: base - 20 * min,
      state: 'error',
      since: base - 34 * min,
      exit_code: 1,
      last: 'FAILURE: Build failed with an exception.',
      terminal: true,
    }),
    s({
      key: 'codex:7b3e91c0-promo',
      agent: 'codex',
      title: 'Промокоды в корзине: ревью',
      project: 'shop-api',
      cwd: P('shop-api'),
      branch: 'promo-cart',
      created: today(8, 50),
      updated: base - 26 * min,
      state: 'waiting',
      since: base - 26 * min,
      last: 'Нашёл две проблемы. Поправить обе?',
      terminal: true,
    }),
    s({
      key: 'codex:c19d0e7a-10296',
      agent: 'codex',
      title: 'Ревью ветки BACKEND-10296',
      project: 'backend',
      cwd: P('backend'),
      branch: 'main',
      created: today(9, 12),
      updated: base - 5,
      state: 'working',
      since: base - (7 * min + 12),
      last_line: 'go test ./internal/billing/...',
      terminal: true,
    }),
    s({
      key: 'grok:5e8a2b14-astro',
      agent: 'grok',
      title: 'Перенос лендинга на Astro',
      project: 'landing',
      cwd: P('landing'),
      branch: 'astro',
      created: today(8, 10),
      updated: base - hour,
      state: 'working',
      since: base - (18 * min + 40),
      last_line: 'astro build · 12 из 14 страниц',
      terminal: true,
    }),
    s({
      key: 'codex:6513270e-10281',
      agent: 'codex',
      title: 'BACKEND-10281: вебхуки для партнёров',
      project: 'backend',
      cwd: P('backend'),
      branch: 'BACKEND-10281',
      created: daysAgo(1, 17, 40),
      updated: daysAgo(1, 18, 40),
    }),
    s({
      key: 'claude:0b7f3c21-10274',
      agent: 'claude',
      title: 'BACKEND-10274: пагинация в API',
      project: 'backend',
      cwd: P('backend'),
      branch: 'BACKEND-10274',
      created: daysAgo(3, 15, 2),
      updated: daysAgo(3, 18, 5),
      done: true,
    }),
    s({
      key: 'codex:91aa04d2-dedup',
      agent: 'codex',
      title: 'Дедупликация событий в ClickHouse',
      project: 'data-pipeline',
      cwd: P('data-pipeline'),
      created: daysAgo(1, 13, 0),
      updated: daysAgo(1, 15, 2),
      done: true,
    }),
    s({
      key: 'kimi:3c5d7e9f-terraform',
      agent: 'kimi',
      title: 'Разделить terraform на модули',
      project: 'infra',
      cwd: P('infra', 'work'),
      created: daysAgo(1, 10, 0),
      updated: daysAgo(1, 11, 20),
    }),
    s({
      key: 'codex:d4e5f6a7-remind',
      agent: 'codex',
      title: 'Напоминания /remind для бота',
      project: 'notes-bot',
      cwd: P('notes-bot'),
      created: daysAgo(3, 12, 0),
      updated: daysAgo(3, 14, 30),
    }),
    s({
      key: 'claude:8e9f0a1b-resend',
      agent: 'claude',
      title: 'Повторная отправка вебхуков из админки',
      project: 'shop-api',
      cwd: P('shop-api'),
      created: daysAgo(12, 11, 0),
      updated: daysAgo(12, 16, 0),
      done: true,
    }),
  ];

  sessions.push(...olderSessions(base, s, daysAgo));

  const projects: ProjectView[] = [
    { name: 'backend', path: P('backend'), git: true, sessions: 15, updated: base - 30, default_agent: 'claude' },
    { name: 'mobile-app', path: P('mobile-app'), git: true, sessions: 4, updated: base - 20 * min },
    { name: 'shop-api', path: P('shop-api'), git: true, sessions: 6, updated: base - 26 * min },
    { name: 'landing', path: P('landing'), git: true, pinned: true, sessions: 3, updated: base - hour },
    { name: 'infra', path: P('infra', 'work'), git: true, sessions: 5, updated: daysAgo(1, 16, 0) },
    { name: 'data-pipeline', path: P('data-pipeline'), git: true, sessions: 2, updated: daysAgo(1, 15, 2) },
    { name: 'notes-bot', path: P('notes-bot'), git: false, sessions: 3, updated: daysAgo(3, 14, 30) },
  ];

  const details = new Map<string, Omit<Details, 'session'>>();
  const msg = (role: MessageView['role'], text: string, at: number): MessageView => ({ role, text, at });
  const summary = (text: string, next: string, at: number, model = 'kimi'): Summary => ({
    title: '',
    summary: text,
    next_step: next,
    closed: false,
    related: null,
    at,
    model,
  });

  details.set('claude:4f1c2a9e-10300', {
    messages: [
      msg('user', 'BACKEND-10300: добавь лимиты запросов для ключей API. Лимит храни в api_keys, по умолчанию 600 в минуту.', today(9, 41)),
      msg('assistant', 'Прогоню миграцию на локальной базе, потом тесты пакета http.', today(9, 58)),
    ],
    truncated: false,
    summary: summary(
      'Лимиты запросов для ключей API: колонка rate_limit в api_keys, проверка в middleware, счётчик в Redis с окном в минуту. Миграция и тесты написаны.',
      'Прогнать миграцию 0058 и тесты пакета http',
      base - 10 * min,
    ),
    resume: 'cd ~/.nit/worktrees/backend/BACKEND-10300 && claude --resume 4f1c2a9e-10300',
    attach: 'tmux attach -t nit-backend-BACKEND-10300',
    launch: { at: today(9, 41) },
  });
  details.set('codex:6513270e-10281', {
    messages: [
      msg('user', 'Сделай вебхуки для партнёров: подпись HMAC и журнал доставок.', daysAgo(1, 17, 40)),
      msg('user', 'Добавь повтор, если партнёр ответил 5xx. Не больше шести попыток.', daysAgo(1, 17, 52)),
      msg('assistant', 'Сделал: retry.go, пауза 1, 2, 4, 8, 16 и 32 секунды. Каждая попытка пишется в webhook_deliveries.', daysAgo(1, 18, 20)),
      msg('user', 'Ок. Прогони тесты и подготовь MR.', daysAgo(1, 18, 26)),
      msg('assistant', 'Тесты зелёные, MR !479 открыт. Журнал доставок пока виден только в базе.', daysAgo(1, 18, 40)),
    ],
    truncated: false,
    summary: summary(
      'Партнёрские вебхуки: подпись HMAC-SHA256, повтор с растущей паузой до 6 попыток, журнал доставок в таблице webhook_deliveries. Тесты пакета webhooks проходят.',
      'Показать журнал доставок в админке',
      daysAgo(1, 18, 45),
    ),
    resume: 'cd ~/projects/backend && codex resume 6513270e',
    launch: { at: daysAgo(1, 17, 40) },
  });

  const finish = new Map<string, FinishCheck>();
  finish.set('claude:4f1c2a9e-10300', {
    worktree: { path: '~/.nit/worktrees/backend/BACKEND-10300', branch: 'BACKEND-10300', repo: P('backend') },
    dirty: ['.env.local', 'tmp/bench.txt'],
    pushed: true,
    merged: true,
    others: [],
    can_remove: false,
    can_delete_branch: false,
  });

  const agents: AgentView[] = [
    agent('codex', 'Codex', 'Cx', 412, { skip_flag: '--dangerously-bypass-approvals-and-sandbox' }),
    agent('claude', 'Claude Code', 'Cl', 268, { skip_flag: '--dangerously-skip-permissions' }),
    agent('grok', 'Grok', 'Gr', 37, { skip_flag: '--always-approve' }),
    agent('kimi', 'Kimi', 'Ki', 54, { skip_flag: '--auto' }),
    agent('zcode', 'ZCode', 'ZC', 9, { resumable: false, runner: false, program: undefined }),
    agent('cursor', 'Cursor Agent', 'Cu', 21, { runner: false, skip_flag: '--force' }),
  ];

  const settings: Settings = {
    agents: Object.fromEntries(agents.map((a) => [a.id, { enabled: true, home: '', program: '', flags: '', skip_approvals: false }])),
    summary: { agent: 'auto', minutes: 5 },
    digest: { agent: 'auto', minutes: 15 },
    language: 'auto',
    service: { auto_commands: ['codex exec', 'claude -p', 'grok -p', 'kimi -p'], temp_dirs: ['/tmp', '/var/tmp', '~/.local'] },
    tmux: { session_name: 'nit-{project}-{branch}' },
    remote: { enabled: false, funnel: false, hostname: 'nit', state_dir: '~/.local/share/ai-threads/tsnet' },
  };

  const remoteKey = 'k7Hq2vXbN9sLwT4mZcR8yPfQe';
  const world: MockWorld = {
    base,
    env: {
      version: '0.1.0',
      addr: '127.0.0.1:8765',
      tmux: true,
      tmux_version: '3.4',
      tmux_path: '/usr/bin/tmux',
      wsl: false,
      vscode: true,
      platform: 'linux',
      via: modes.phone ? 'remote' : 'local',
      remote: { state: 'off' },
      settings_path: `${HOME}/.config/ai-threads/settings.json`,
      data_dir: `${HOME}/.local/share/ai-threads`,
    },
    reboot: [],
    agents,
    projects,
    sessions: new Map(sessions.map((x) => [x.key, x])),
    details,
    finish,
    settings,
    digests: modes.noDigests ? new Map() : digestsOf(base),
    jobs: new Map(),
    remote: remoteOf(modes.remote ?? 'off', base, remoteKey),
    remoteKey,
    revoked: false,
  };
  world.env.remote = { state: world.remote.state, host: world.remote.host };
  if (modes.reboot) rebootWorld(world, min);
  if (modes.empty) emptyWorld(world);
  if (modes.agentsOff) {
    for (const a of Object.values(world.settings.agents)) a.enabled = false;
    world.agents = world.agents.map((a) => ({ ...a, enabled: false }));
  }
  return world;
}

const MONTHS_GEN = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];

/** «Итоги за 13–15 октября», как у internal/summary */
export function periodTitle(days: number, at: number): string {
  const to = new Date(at * 1000);
  const from = new Date(at * 1000);
  from.setDate(from.getDate() - (days - 1));
  if (from.getMonth() === to.getMonth()) return `Итоги за ${from.getDate()}–${to.getDate()} ${MONTHS_GEN[to.getMonth()]}`;
  return `Итоги за ${from.getDate()} ${MONTHS_GEN[from.getMonth()]} – ${to.getDate()} ${MONTHS_GEN[to.getMonth()]}`;
}

function isoDay(t: number, minus = 0): string {
  const d = new Date(t * 1000);
  d.setDate(d.getDate() - minus);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

/** Сводка за 3 дня — статья из макета Nit-Summary; за неделю — сводка первой версии; за 5 дней — нет */
export function digestsOf(base: number): Map<number, Digest> {
  const three: Digest = {
    days: 3,
    from: isoDay(base, 2),
    to: isoDay(base),
    title: periodTitle(3, base),
    lead: 'Четыре проекта, 23 сессии. Две задачи влиты, одна на ревью, одна сессия ждёт ответа.',
    projects: [
      {
        name: 'backend',
        path: P('backend'),
        items: [
          { text: '[BACKEND-10300](claude:4f1c2a9e-10300): лимиты запросов для ключей API. MR !482 влит в `main`.', keys: ['claude:4f1c2a9e-10300'] },
          {
            text: '[BACKEND-10281](codex:6513270e-10281): вебхуки для партнёров. Подпись HMAC, повтор до 6 попыток, журнал в `webhook_deliveries`. MR !479 открыт.',
            keys: ['codex:6513270e-10281'],
          },
          { text: '[BACKEND-10274](claude:0b7f3c21-10274): курсорная пагинация для `/keys` и `/invoices`.', keys: ['claude:0b7f3c21-10274'] },
        ],
        left: 'Показать журнал доставок в админке. Ревью [BACKEND-10296](codex:c19d0e7a-10296) нашло ошибку округления в `invoice.go` и миграцию 0055 без индекса.',
      },
      {
        name: 'shop-api',
        path: P('shop-api'),
        items: [
          { text: 'Поиск по каталогу: индекс `pg_trgm`, p95 с 2,4 с до 85 мс.', keys: [] },
          { text: 'Ревью промокодов: скидка применяется до налога. **Codex ждёт ответа.**', keys: ['codex:7b3e91c0-promo'] },
        ],
        left: '',
      },
      {
        name: 'mobile-app',
        path: P('mobile-app'),
        items: [
          {
            text: 'Падение на Android 14: причина в частичном доступе к фото. Исправление в `release/2.3`, но сборка завершилась с кодом 1.',
            keys: ['cursor:a2d4e6f8-android'],
          },
        ],
        left: '',
      },
      {
        name: 'landing',
        path: P('landing'),
        items: [{ text: '12 из 14 страниц перенесены на Astro, адреса совпадают. Остались две страницы с формами заявок.', keys: ['grok:5e8a2b14-astro'] }],
        left: '',
      },
    ],
    journals: 23,
    at: base - 10 * 60,
    model: 'kimi',
    lang: 'ru',
  };
  const week: Digest = {
    days: 7,
    from: isoDay(base - 2 * 86400, 6),
    to: isoDay(base - 2 * 86400),
    title: periodTitle(7, base - 2 * 86400),
    lead: 'Неделя ушла на платежи и инфраструктуру: схема платежей переехала, terraform разделён на модули.',
    projects: [
      {
        name: 'backend',
        path: P('backend'),
        items: [
          { text: 'Миграция платежей на новую схему, старые таблицы оставлены до конца месяца.', keys: [] },
          { text: 'Экспорт счетов в PDF: шаблон на `wkhtmltopdf`, шрифты вшиты.', keys: [] },
        ],
        left: 'Убрать старые таблицы платежей.',
      },
      {
        name: 'infra',
        path: P('infra', 'work'),
        items: [{ text: '[Разделить terraform на модули](kimi:3c5d7e9f-terraform): сеть, база и очереди отдельно.', keys: ['kimi:3c5d7e9f-terraform'] }],
        left: '',
      },
    ],
    journals: 0,
    at: base - 2 * 86400,
    model: 'claude',
    lang: 'ru',
    legacy: true,
  };
  return new Map([
    [3, three],
    [7, week],
  ]);
}

/** Доступ с телефона в нужном состоянии; в running — два телефона, как в макете */
export function remoteOf(state: RemoteView['state'], base: number, key: string): RemoteView {
  const host = 'nit.tail4f2c.ts.net';
  const v: RemoteView = { enabled: state !== 'off', funnel: false, state, since: base - 60, devices: [] };
  if (state === 'needs_login') v.login_url = 'https://login.tailscale.com/a/1f2e3d4c5b6a';
  if (state === 'certificate' || state === 'running') {
    v.host = host;
    v.url = `https://${host}/`;
    v.link = `${v.url}?key=${key}`;
  }
  if (state === 'error') v.error = 'Tailscale не ответил: нет связи с координационным сервером. Повторю через минуту.';
  if (state === 'running') {
    const d = new Date(base * 1000);
    d.setHours(9, 12, 0, 0);
    const today = d.getTime() / 1000;
    d.setDate(d.getDate() - 1);
    d.setHours(22, 40, 0, 0);
    v.devices = [
      { id: 'dev-iphone', name: 'iPhone · Safari', created: base - 5 * 86400, last_seen: today },
      { id: 'dev-pixel', name: 'Pixel 8 · Chrome', created: base - 9 * 86400, last_seen: d.getTime() / 1000 },
    ];
  }
  return v;
}

/** После перезагрузки: терминалов нет, пять сессий ждут решения, как в макете Nit-Reboot */
function rebootWorld(w: MockWorld, min: number) {
  const was: [string, Partial<RebootSession>, string][] = [
    ['claude:4f1c2a9e-10300', { was: 'waiting' }, 'BACKEND-10300'],
    ['codex:c19d0e7a-10296', { was: 'working', minutes: 7 }, 'main'],
    ['codex:7b3e91c0-promo', { was: 'waiting' }, 'feature/promo-codes'],
    ['cursor:a2d4e6f8-android', { was: 'error', code: 1 }, 'release/2.3'],
    ['grok:5e8a2b14-astro', { was: 'working', minutes: 18 }, 'feature/astro'],
  ];
  for (const s of w.sessions.values()) {
    if (!s.terminal) continue;
    w.sessions.set(s.key, { ...s, terminal: false, state: 'idle', since: undefined, unseen: false, exit_code: undefined, updated: w.base - 40 * min });
  }
  w.reboot = was.map(([key, r, branch]) => {
    const s = { ...w.sessions.get(key)!, branch };
    w.sessions.set(key, s);
    return { session: s, was: r.was!, code: r.code, minutes: r.minutes };
  });
}

/** Первый запуск: нет tmux, агенты не найдены, сессий нет */
function emptyWorld(w: MockWorld) {
  w.env = { ...w.env, tmux: false, tmux_version: undefined, tmux_path: undefined };
  w.agents = w.agents.map((a) => ({ ...a, program: undefined, home_found: false, sessions: 0 }));
  w.sessions.clear();
  w.projects = [];
}

/** «Установить» tmux и агентов в первом запуске: для «Проверить снова» */
export function installAll(w: MockWorld) {
  w.env = { ...w.env, tmux: true, tmux_version: '3.4', tmux_path: '/usr/bin/tmux' };
  w.agents = w.agents.map((a) => ({ ...a, program: `/usr/local/bin/${a.id === 'cursor' ? 'cursor-agent' : a.id}` }));
}

// Старые сессии для истории: по нескольку в день за три недели, и служебные запуски
const OLDER: [AgentId, string, string, string?][] = [
  ['codex', 'backend', 'Миграция платежей на новую схему'],
  ['claude', 'shop-api', 'Кэш каталога в Redis', 'Кэш на 5 минут, сброс по событию изменения товара'],
  ['kimi', 'infra', 'Алерты на рост ошибок 5xx', 'Правило в Prometheus и канал в Slack'],
  ['grok', 'landing', 'Форма подписки без перезагрузки страницы'],
  ['claude', 'backend', 'BACKEND-10255: экспорт счетов в PDF', 'Шаблон на wkhtmltopdf, шрифты вшиты'],
  ['cursor', 'mobile-app', 'Тёмная тема в настройках'],
  ['codex', 'data-pipeline', 'Партиционирование событий по дням', 'Партиции по дате события, старые перенесены'],
  ['codex', 'notes-bot', 'Команда /export для заметок'],
  ['claude', 'backend', 'BACKEND-10240: ретраи в клиенте банка', 'Экспоненциальная пауза, не больше 5 попыток'],
  ['kimi', 'infra', 'Обновить Postgres до 17'],
  ['grok', 'landing', 'Сжатие картинок при сборке', 'Сборка стала на 40% меньше'],
  ['codex', 'shop-api', 'Ревью: корзина для гостей'],
  ['claude', 'mobile-app', 'Push-уведомления о статусе заказа'],
  ['codex', 'backend', 'BACKEND-10221: аудит изменений тарифов', 'Журнал изменений в отдельной таблице'],
  ['cursor', 'mobile-app', 'Падение при повороте экрана на планшете'],
  ['kimi', 'data-pipeline', 'Дашборд задержек загрузки'],
  ['claude', 'notes-bot', 'Поиск по заметкам', 'Полнотекстовый поиск через FTS5'],
  ['codex', 'backend', 'Ревью ветки BACKEND-10218'],
];

function olderSessions(
  base: number,
  make: (v: Partial<SessionView> & Pick<SessionView, 'key' | 'agent' | 'title' | 'project' | 'cwd'>) => SessionView,
  daysAgo: (n: number, h: number, m: number) => number,
): SessionView[] {
  void base;
  const out: SessionView[] = [];
  const paths: Record<string, string> = {
    backend: P('backend'),
    'shop-api': P('shop-api'),
    infra: P('infra', 'work'),
    landing: P('landing'),
    'mobile-app': P('mobile-app'),
    'data-pipeline': P('data-pipeline'),
    'notes-bot': P('notes-bot'),
  };
  for (let i = 0; i < 54; i++) {
    const [agent, project, title, last] = OLDER[i % OLDER.length]!;
    const day = 2 + Math.floor(i / 3);
    const h = 18 - (i % 3) * 3;
    const m = (i * 17) % 60;
    const n = String(i).padStart(2, '0');
    out.push(
      make({
        key: `${agent}:${n}a0b1c2-old`,
        agent,
        title: i >= OLDER.length ? `${title} (${Math.floor(i / OLDER.length) + 1})` : title,
        project,
        cwd: paths[project]!,
        branch: i % 4 === 0 ? 'main' : undefined,
        created: daysAgo(day, h - 1, m),
        updated: daysAgo(day, h, m),
        last,
        done: i % 5 === 1,
      }),
    );
  }
  // Служебные: ревью и сводки, которые запускали другие агенты
  for (let i = 0; i < 6; i++) {
    out.push(
      make({
        key: `codex:${i}f0e1d2-svc`,
        agent: i % 2 ? 'claude' : 'codex',
        title: i % 2 ? 'Сводка по сессии BACKEND-10300' : 'Ревью изменений перед мержем',
        project: 'backend',
        cwd: P('backend'),
        created: daysAgo(i, 7, 10 + i),
        updated: daysAgo(i, 7, 20 + i),
        service: true,
        by: i % 2 ? 'claude -p' : 'codex exec',
        last: i % 2 ? 'Сводка готова' : 'Замечаний нет',
      }),
    );
  }
  return out;
}

export function bootstrapOf(w: MockWorld): Bootstrap {
  const all = [...w.sessions.values()];
  const service = all.filter((s) => s.service).length;
  return {
    env: w.env,
    agents: w.agents,
    projects: w.projects,
    sessions: all.filter((s) => !s.service),
    counts: { service, all: all.length },
    progress: { done: 7, total: 7, ready: true, agents: {} },
    settings: w.settings,
  };
}

export const MODELS: Record<string, ModelsView> = {
  claude: {
    default_model: '',
    default_effort: '',
    models: ['opus', 'sonnet', 'fable', 'haiku'].map((id) => ({ id, name: id })),
    efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
  },
  codex: {
    default_model: 'gpt-5-codex',
    default_effort: 'medium',
    models: [
      { id: 'gpt-5-codex', name: 'gpt-5-codex', efforts: ['low', 'medium', 'high'], default_effort: 'medium' },
      { id: 'gpt-5', name: 'gpt-5', efforts: ['minimal', 'low', 'medium', 'high'], default_effort: 'medium' },
    ],
    efforts: [],
  },
  grok: { default_model: 'grok-code-fast', default_effort: '', models: [{ id: 'grok-code-fast', name: 'grok-code-fast' }, { id: 'grok-4', name: 'grok-4' }], efforts: [] },
  kimi: { default_model: 'kimi-k2', default_effort: '', models: [{ id: 'kimi-k2', name: 'kimi-k2' }], efforts: [] },
  cursor: { default_model: '', default_effort: '', models: [], efforts: [] },
};
