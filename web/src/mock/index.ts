// Режим ?mock: API, поток событий и терминал прямо в браузере, на данных из макетов.
// ?mock=still — часы стоят (10:00), таймеры не идут: для снимков экрана.
// Из консоли и скриптов проверки: window.nitMock.wait(key), .work(key), .add(...).
import type { SearchHit, SearchView } from '../api/search';
import { setTransport, type EventStream, type TerminalSocket } from '../api/transport';
import type { Details, Digest, FinishCheck, JobView, LaunchCheck, LaunchRequest, RemoteView, SessionView, Settings } from '../api/types';
import { freezeClock, now } from '../lib/clock';
import { bootstrapOf, createWorld, digestsOf, installAll, MODELS, periodTitle, remoteOf, type MockWorld } from './data';
import { DIM, RESET, sceneFor, type Scene } from './screens';

let world: MockWorld;
const streams = new Set<MockEventStream>();

function emit(type: string, data: unknown) {
  for (const s of streams) s.dispatch(type, data);
}

function changed(...list: SessionView[]) {
  for (const s of list) world.sessions.set(s.key, s);
  emit('sessions', { changed: list, removed: [] });
}

function patch(key: string, p: Partial<SessionView>): SessionView | undefined {
  const s = world.sessions.get(key);
  if (!s) return undefined;
  const next = { ...s, ...p, updated: now() };
  changed(next);
  return next;
}

// ───────── HTTP ─────────

class HttpError extends Error {
  status: number;
  fields?: Record<string, string>;
  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data ?? null), { status, headers: { 'Content-Type': 'application/json' } });
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function handle(method: string, url: URL, body: Record<string, unknown>): Promise<unknown> {
  const path = url.pathname;
  const q = url.searchParams;
  if (method === 'GET' && path === '/api/bootstrap') return bootstrapOf(world);
  if (method === 'GET' && path === '/api/models') return MODELS[q.get('agent') ?? ''] ?? { default_model: '', default_effort: '', models: [], efforts: [] };
  if (method === 'GET' && path === '/api/launch/check') return launchCheck(q.get('path') ?? '');
  if (method === 'POST' && path === '/api/launch') return launch(body as unknown as LaunchRequest);
  if (method === 'GET' && path === '/api/search') return search(q.get('q') ?? '', q.has('conv'), q.has('service'));
  if (method === 'POST' && path === '/api/search/full') return fullSearch(String(body.q ?? ''));
  if (method === 'POST' && path === '/api/reveal') return { ok: true };
  if (method === 'POST' && path === '/api/project') return projectMark(body);
  if (method === 'GET' && path === '/api/sessions') return sessionsList(q);
  if (method === 'GET' && path === '/api/reboot') return { sessions: world.reboot };
  if (method === 'POST' && path === '/api/reboot') return rebootAction(body);
  if (method === 'GET' && path === '/api/env') return { env: world.env, agents: world.agents };
  if (method === 'GET' && path === '/api/settings') return world.settings;
  if (method === 'POST' && path === '/api/settings') return saveSettings(body as unknown as Settings);
  if (method === 'POST' && path === '/api/import') return sleep(300).then(() => ({ ok: true }));
  if (method === 'GET' && path === '/api/health') return { ok: true };
  if (method === 'GET' && path === '/api/digest') return digestView(Number(q.get('days')));
  if (method === 'POST' && path === '/api/digest') return startDigestJob(body);
  if (method === 'GET' && path === '/api/jobs') return [...world.jobs.values()].filter((j) => !j.done);
  const jc = path.match(/^\/api\/jobs\/([^/]+)\/cancel$/);
  if (method === 'POST' && jc) return cancelJob(decodeURIComponent(jc[1]!));
  if (path === '/api/remote' || path.startsWith('/api/remote/')) return remoteApi(method, path, body);

  const t = path.match(/^\/api\/terminal\/([^/]+)\/(screen|keys)$/);
  if (t) {
    const key = decodeURIComponent(t[1]!);
    const s = world.sessions.get(key);
    if (!s?.terminal) throw new HttpError(404, 'Терминал этой сессии закрыт');
    if (method === 'GET' && t[2] === 'screen') return { text: screenText(key) };
    if (method === 'POST' && t[2] === 'keys') return keys(key, String(body.text ?? ''), (body.keys as string[] | undefined) ?? []);
  }

  const m = path.match(/^\/api\/sessions\/([^/]+)(?:\/(\w+))?$/);
  if (m) {
    const key = decodeURIComponent(m[1]!);
    const action = m[2];
    const s = world.sessions.get(key);
    if (!s) throw new HttpError(404, 'Сессия не найдена');
    if (method === 'GET' && !action) return detailsOf(s);
    if (method === 'GET' && action === 'finish') return finishCheck(s);
    if (method === 'POST') return sessionAction(s, action ?? '', body);
  }
  throw new HttpError(404, `Нет такого запроса: ${method} ${path}`);
}

/**
 * История: как GET /api/sessions — фильтры, сначала недавние (при равном времени — по ключу),
 * страница до limit после курсора <updated>:<ключ> или раньше before. X-Total-Count и
 * X-Next-Cursor — как у сервера; с ?mock=nocursor заголовков нет, как у старого сервера
 */
function sessionsList(q: URLSearchParams): Response | SessionView[] {
  const project = q.get('project');
  const agent = q.get('agent');
  const service = q.get('service') === '1';
  const before = Number(q.get('before')) || 0;
  const limit = Number(q.get('limit')) || 0;
  const matching = [...world.sessions.values()]
    .filter((s) => (!project || (s.repo || s.cwd) === project) && (!agent || s.agent === agent))
    .filter((s) => service || !s.service)
    .sort((a, b) => Math.floor(b.updated) - Math.floor(a.updated) || (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
  let rest = matching.filter((s) => !before || s.updated < before);
  const cursor = q.get('cursor');
  if (cursor && !noCursor) {
    const at = cursor.indexOf(':');
    const cu = Number(cursor.slice(0, at));
    const ck = cursor.slice(at + 1);
    if (at < 0 || !Number.isFinite(cu) || !ck) throw new HttpError(400, 'Неверный курсор страницы: нужен <время>:<ключ>');
    rest = rest.filter((s) => Math.floor(s.updated) < cu || (Math.floor(s.updated) === cu && s.key > ck));
  }
  const page = limit ? rest.slice(0, limit) : rest;
  if (noCursor) return page;
  const last = page[page.length - 1];
  const next = limit && rest.length > limit && last ? `${Math.floor(last.updated)}:${last.key}` : '';
  return new Response(JSON.stringify(page), {
    status: 200,
    headers: { 'Content-Type': 'application/json', 'X-Total-Count': String(matching.length), 'X-Next-Cursor': next },
  });
}

// ───────── Сводки ─────────

let jobSeq = 0;
const jobTimers = new Map<string, number[]>();
/** Следующее задание закончится ошибкой: nitMock.failNext() */
let failNext = false;

function jobEmit(j: JobView) {
  world.jobs.set(j.id, j);
  emit('job', j);
}

function digestView(days: number) {
  if (![3, 5, 7].includes(days)) throw new HttpError(400, 'Период — 3, 5 или 7 дней');
  const job = [...world.jobs.values()].find((j) => j.kind === 'digest' && j.days === days && !j.done) ?? null;
  return { digest: world.digests.get(days) ?? null, job };
}

/** Кто пишет: из запроса, иначе из настроек; «Авто» — первый установленный */
function pickRunner(want: unknown, kind: 'summary' | 'digest'): string {
  const ready = (id: string) => world.agents.some((a) => a.id === id && a.enabled && !!a.program);
  const w = typeof want === 'string' && want ? want : world.settings[kind].agent;
  if (w && w !== 'auto') {
    if (!ready(w)) throw new HttpError(409, `Агент ${w} не установлен`);
    return w;
  }
  const auto = ['kimi', 'claude', 'codex', 'grok'].find(ready);
  if (!auto) throw new HttpError(409, 'Некому писать сводку: установите одного из агентов: Kimi, Claude Code, Codex, Grok');
  return auto;
}

const AGENT_NAMES: Record<string, string> = { kimi: 'Kimi', claude: 'Claude Code', codex: 'Codex', grok: 'Grok' };

/** Ошибка задания, как у internal/summary: текст и хвост вывода агента */
function failure(agent: string): Partial<JobView> {
  return {
    stage: 'failed',
    done: true,
    error: `${AGENT_NAMES[agent] ?? agent} завершился с кодом 1`,
    output: [
      'Reading digest-input.txt (412 lines)',
      'Reading journals/codex-6513270e.txt',
      'error: rate limit exceeded for model kimi-k2 (429)',
      '  retry after 38s',
      'Aborting: no answer was written',
    ].join('\n'),
  };
}

/** Ход задания по плану: [через сколько мс, что поменялось]; в конце finish и done */
function simulate(j: JobView, plan: [number, Partial<JobView>][], finish: () => void) {
  jobEmit(j);
  const fail = failNext;
  failNext = false;
  const step = (p: Partial<JobView>) => {
    const cur = world.jobs.get(j.id);
    if (!cur || cur.done) return;
    jobEmit({ ...cur, ...p, elapsed: Math.max(0, Math.floor(now() - j.started)) });
  };
  const timers = plan.map(([ms, p]) => window.setTimeout(() => step(p), ms));
  const end = (plan[plan.length - 1]?.[0] ?? 0) + 1200;
  timers.push(
    window.setTimeout(() => {
      const cur = world.jobs.get(j.id);
      if (!cur || cur.done) return;
      if (fail) return step(failure(j.agent));
      // Сервер сохраняет итог до последнего события задания
      finish();
      step({ stage: 'done', done: true });
    }, end),
  );
  jobTimers.set(j.id, timers);
}

function startDigestJob(body: Record<string, unknown>) {
  const days = Number(body.days);
  if (![3, 5, 7].includes(days)) throw new HttpError(400, 'Период — 3, 5 или 7 дней');
  const running = [...world.jobs.values()].find((j) => j.kind === 'digest' && j.days === days && !j.done);
  if (running) return { job: running };
  const agent = pickRunner(body.agent, 'digest');
  const job: JobView = { id: `j${++jobSeq}`, kind: 'digest', days, agent, stage: 'starting', started: Math.floor(now()), elapsed: 0, done: false };
  const reads: [number, Partial<JobView>][] = [3, 6, 9, 12, 15].map((n, i) => [700 + i * 450, { stage: 'reading', reads: n }]);
  simulate(job, [[400, { stage: 'reading', reads: 1 }], ...reads, [3300, { stage: 'writing' }]], () => {
    const fresh = { ...digestsOf(now()).get(3)!, days, title: periodTitle(days, now()), at: Math.floor(now()), model: agent };
    world.digests.set(days, fresh);
    emit('digest', fresh);
  });
  return { job };
}

function startSummaryJob(s: SessionView, body: Record<string, unknown>) {
  const running = [...world.jobs.values()].find((j) => j.kind === 'summary' && j.key === s.key && !j.done);
  if (running) return { job: running };
  const agent = pickRunner(body.agent, 'summary');
  const job: JobView = { id: `j${++jobSeq}`, kind: 'summary', key: s.key, agent, stage: 'starting', started: Math.floor(now()), elapsed: 0, done: false };
  simulate(job, [[500, { stage: 'reading', reads: 1 }], [1600, { stage: 'writing' }]], () => {
    const d = world.details.get(s.key);
    const summary = {
      title: '',
      summary: d?.summary?.summary ?? `${s.title}: агент прочитал журнал сессии и коротко пересказал, что сделано.`,
      next_step: d?.summary?.next_step ?? 'Проверить изменения и подготовить MR',
      closed: false,
      related: null,
      at: now(),
      model: agent,
    };
    world.details.set(s.key, { ...(d ?? { messages: [], truncated: false }), summary });
    // Страницы узнают о новой сводке событием sessions
    changed(world.sessions.get(s.key) ?? s);
  });
  return { job };
}

function cancelJob(id: string) {
  const j = world.jobs.get(id);
  if (!j || j.done) return { cancelled: false };
  for (const t of jobTimers.get(id) ?? []) window.clearTimeout(t);
  jobEmit({ ...j, stage: 'cancelled', done: true, elapsed: Math.max(0, Math.floor(now() - j.started)) });
  return { cancelled: true };
}

/** Задание, которое шло до открытия страницы: ?mock=digest-running, summary-running */
function seedJob(p: Partial<JobView> & Pick<JobView, 'kind'>): JobView {
  const j: JobView = { id: `j${++jobSeq}`, agent: 'kimi', stage: 'reading', started: Math.floor(now()) - 84, elapsed: 84, done: false, ...p };
  world.jobs.set(j.id, j);
  return j;
}

// ───────── Доступ с телефона ─────────

function remoteEmit() {
  world.env.remote = { state: world.remote.state, host: world.remote.host };
  // Как у сервера: событие remote только страницам на компьютере
  if (world.env.via !== 'remote') emit('remote', world.remote);
}

let remoteTimers: number[] = [];
/** Узлу нужен вход в Tailscale: ?mock=remote-login; nitMock.login() — «вошли» */
let needsLogin = false;

function setRemoteState(next: Partial<RemoteView>) {
  world.remote = { ...world.remote, ...next, since: Math.floor(now()) };
  remoteEmit();
}

/** Узел поднимается: starting → (вход) → certificate → running */
function bringUp() {
  for (const t of remoteTimers) window.clearTimeout(t);
  const base = remoteOf('running', now(), world.remoteKey);
  setRemoteState({ state: 'starting', error: undefined, login_url: undefined, host: undefined, url: undefined, link: undefined });
  if (needsLogin) {
    remoteTimers = [window.setTimeout(() => setRemoteState({ state: 'needs_login', login_url: remoteOf('needs_login', now(), '').login_url }), 900)];
    return;
  }
  remoteTimers = [
    window.setTimeout(() => setRemoteState({ state: 'certificate', host: base.host, url: base.url, link: base.link }), 900),
    window.setTimeout(() => setRemoteState({ state: 'running' }), 2600),
  ];
}

function remoteApi(method: string, path: string, body: Record<string, unknown>): unknown {
  if (world.env.via === 'remote') throw new HttpError(403, 'Это можно сделать только на компьютере, не с телефона');
  if (method === 'GET' && path === '/api/remote') return world.remote;
  if (method === 'POST' && path === '/api/remote') {
    if (typeof body.funnel === 'boolean') world.remote = { ...world.remote, funnel: body.funnel };
    if (typeof body.enabled === 'boolean') {
      const was = world.remote.enabled;
      world.remote = { ...world.remote, enabled: body.enabled };
      if (!body.enabled) {
        for (const t of remoteTimers) window.clearTimeout(t);
        setRemoteState({ state: 'off', host: undefined, url: undefined, link: undefined, login_url: undefined, error: undefined });
      } else if (!was || world.remote.state === 'error' || world.remote.state === 'off') bringUp();
    }
    if (world.settings.remote) world.settings.remote = { ...world.settings.remote, enabled: world.remote.enabled, funnel: world.remote.funnel };
    remoteEmit();
    return world.remote;
  }
  if (method === 'POST' && path === '/api/remote/key') {
    world.remoteKey = Math.random().toString(36).slice(2, 14) + Math.random().toString(36).slice(2, 14);
    const link = world.remote.url ? `${world.remote.url}?key=${world.remoteKey}` : undefined;
    world.remote = { ...world.remote, link, devices: [] };
    remoteEmit();
    return world.remote;
  }
  const rv = path.match(/^\/api\/remote\/devices\/([^/]+)\/revoke$/);
  if (method === 'POST' && rv) {
    const id = decodeURIComponent(rv[1]!);
    if (!(world.remote.devices ?? []).some((d) => d.id === id)) throw new HttpError(404, 'Такого телефона нет');
    world.remote = { ...world.remote, devices: (world.remote.devices ?? []).filter((d) => d.id !== id) };
    remoteEmit();
    return world.remote;
  }
  throw new HttpError(404, `Нет такого запроса: ${method} ${path}`);
}

/** QR-код ссылки: настоящий код здесь не нужен, достаточно похожей картинки, своей для каждой ссылки */
function qrSvg(link: string): string {
  const n = 29;
  let h = 2166136261;
  for (const ch of link) h = Math.imul(h ^ ch.charCodeAt(0), 16777619);
  const rnd = () => {
    h ^= h << 13;
    h ^= h >>> 17;
    h ^= h << 5;
    return (h >>> 0) / 4294967296;
  };
  const finder = (x: number, y: number) => {
    const inBox = (cx: number, cy: number) => cx >= x && cx < x + 7 && cy >= y && cy < y + 7;
    return { inBox, on: (cx: number, cy: number) => {
      const dx = cx - x;
      const dy = cy - y;
      return dx === 0 || dy === 0 || dx === 6 || dy === 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4);
    } };
  };
  const finders = [finder(0, 0), finder(n - 7, 0), finder(0, n - 7)];
  let rects = '';
  for (let y = 0; y < n; y++)
    for (let x = 0; x < n; x++) {
      const f = finders.find((fd) => fd.inBox(x, y));
      const on = f ? f.on(x, y) : rnd() < 0.5;
      if (on) rects += `<rect x="${x + 2}" y="${y + 2}" width="1" height="1"/>`;
    }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n + 4} ${n + 4}" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><g fill="#000">${rects}</g></svg>`;
}

function rebootAction(body: Record<string, unknown>) {
  const keys = (body.keys as string[] | undefined) ?? [];
  const resumed: string[] = [];
  for (const key of keys) {
    const s = world.sessions.get(key);
    if (!s) continue;
    resumed.push(key);
    patch(key, { terminal: true, state: 'starting', since: now() });
    setTimeout(() => patch(key, { state: 'waiting', since: now(), unseen: true, last: 'Продолжаю с того же места. Что дальше?' }), 1500 + resumed.length * 400);
  }
  if (body.dismiss || keys.length) world.reboot = [];
  return { resumed };
}

// Проверки настроек, как у internal/settings: тексты ошибок те же
const PLACEHOLDERS = ['project', 'branch', 'agent', 'id'];

function quotesOk(flags: string): boolean {
  let quote = '';
  let escaped = false;
  for (const ch of flags) {
    if (escaped) escaped = false;
    else if (quote === "'") {
      if (ch === "'") quote = '';
    } else if (ch === '\\') escaped = true;
    else if (quote === '"') {
      if (ch === '"') quote = '';
    } else if (ch === "'" || ch === '"') quote = ch;
  }
  return !quote && !escaped;
}

function patternError(p: string): string {
  if (!p.trim()) return 'нужна непустая строка до 100 символов без переводов строк';
  let rest = p;
  while (rest) {
    const start = rest.indexOf('{');
    const literal = start < 0 ? rest : rest.slice(0, start);
    const bad = [...literal].find((c) => !/[A-Za-z0-9_-]/.test(c));
    if (bad) return `символ «${bad}» нельзя: вне подстановок только латиница, цифры, - и _`;
    if (start < 0) return '';
    const end = rest.indexOf('}', start);
    if (end < 0) return `неизвестная подстановка ${rest.slice(start)}; есть {project}, {branch}, {agent}, {id}`;
    const name = rest.slice(start + 1, end);
    if (!PLACEHOLDERS.includes(name)) return `неизвестная подстановка {${name}}; есть {project}, {branch}, {agent}, {id}`;
    rest = rest.slice(end + 1);
  }
  return '';
}

function saveSettings(next: Settings): Settings {
  const fields: Record<string, string> = {};
  for (const [id, a] of Object.entries(next.agents ?? {})) {
    if (!quotesOk(a.flags ?? '')) fields[`agents.${id}.flags`] = 'незакрытая кавычка или \\ в конце';
  }
  for (const kind of ['summary', 'digest'] as const) {
    const m = next[kind]?.minutes as unknown;
    if (typeof m !== 'number' || !Number.isInteger(m) || m < 1 || m > 120) fields[`${kind}.minutes`] = 'нужно целое число минут от 1 до 120';
  }
  for (const d of next.service?.temp_dirs ?? []) {
    if (d !== '~' && !d.startsWith('/') && !d.startsWith('~/')) fields['service.temp_dirs'] = `путь ${d} должен начинаться с / или ~/`;
  }
  const pe = patternError(next.tmux?.session_name ?? '');
  if (pe) fields['tmux.session_name'] = pe;
  if (Object.keys(fields).length)
    throw new HttpError(
      400,
      Object.entries(fields)
        .map(([k, v]) => `${k}: ${v}`)
        .join('; '),
      fields,
    );
  const clean: Settings = {
    ...next,
    agents: Object.fromEntries(
      Object.entries(next.agents).map(([id, a]) => [id, { ...a, home: a.home.trim(), program: a.program.trim(), flags: a.flags.trim() }]),
    ),
    tmux: { session_name: next.tmux.session_name.trim() },
    // Как у сервера: без поля remote доступ с телефона остаётся как был
    remote: next.remote ?? world.settings.remote,
  };
  world.settings = clean;
  world.agents = world.agents.map((a) => ({ ...a, enabled: clean.agents[a.id]?.enabled ?? a.enabled }));
  return clean;
}

function detailsOf(s: SessionView): Details {
  const d = world.details.get(s.key);
  const resume = `cd ${s.cwd.replace(/^\/home\/[^/]+/, '~')} && ${s.agent} resume ${s.id}`;
  return {
    session: s,
    messages: d?.messages ?? (s.last ? [{ role: 'assistant', text: s.last, at: s.updated }] : []),
    truncated: false,
    summary: d?.summary,
    resume: d?.resume ?? resume,
    attach: s.terminal ? (d?.attach ?? `tmux attach -t nit-${s.project}-${s.branch ?? s.id}`) : undefined,
    launch: d?.launch,
  };
}

function finishCheck(s: SessionView): FinishCheck {
  return (
    world.finish.get(s.key) ?? {
      worktree: s.worktree,
      dirty: [],
      pushed: true,
      merged: true,
      others: [],
      can_remove: !!s.worktree,
      can_delete_branch: !!s.worktree,
    }
  );
}

function sessionAction(s: SessionView, action: string, body: Record<string, unknown>): unknown {
  switch (action) {
    case 'rename': {
      const title = String(body.name ?? '').trim();
      return patch(s.key, title ? { title, original: s.original || s.title } : { title: s.original || s.title, original: undefined });
    }
    case 'pin':
      return patch(s.key, { pinned: !!body.on });
    case 'hide':
      return patch(s.key, { hidden: !!body.on });
    case 'done':
      return patch(s.key, { done: !!body.on });
    case 'seen':
      return s.unseen ? patch(s.key, { unseen: false }) : s;
    case 'resume': {
      const next = patch(s.key, { terminal: true, state: 'starting', since: now() })!;
      setTimeout(() => patch(s.key, { state: 'waiting', since: now(), last: 'Продолжаю. Что дальше?' }), 1500);
      return next;
    }
    case 'summary':
      return startSummaryJob(s, body);
    case 'finish':
      return patch(s.key, { done: true, terminal: false, state: 'idle', since: undefined, unseen: false });
  }
  throw new HttpError(404, 'Нет такого действия');
}

function projectMark(body: Record<string, unknown>) {
  const path = String(body.path ?? '');
  world.projects = world.projects.map((p) => {
    if (p.path !== path) return p;
    const next = { ...p };
    if (typeof body.pinned === 'boolean') next.pinned = body.pinned;
    if (typeof body.hidden === 'boolean') next.hidden = body.hidden;
    if (typeof body.default_agent === 'string') next.default_agent = (body.default_agent || undefined) as typeof p.default_agent;
    return next;
  });
  emit('projects', world.projects);
  return world.projects.find((p) => p.path === path);
}

function launchCheck(raw: string): LaunchCheck {
  const path = raw.replace(/^~(?=\/|$)/, '/home/dev').replace(/\/+$/, '');
  const project = world.projects.find((p) => p.path === path);
  const known = !!project || /^\/home\/dev\/(projects|work)\/[\w.-]+$/.test(path) || path.startsWith('/home/dev/.nit/worktrees/');
  const busy = [...world.sessions.values()]
    .filter((s) => (s.repo ?? s.cwd) === path && s.cwd === path && s.terminal && !s.done)
    .map((s) => ({ key: s.key, agent: s.agent, title: s.title, state: s.state }));
  const worktrees = [...world.sessions.values()]
    .filter((s) => s.worktree && s.worktree.repo === path)
    .map((s) => s.worktree!);
  if (path === '/home/dev/projects/backend') worktrees.push({ path: '~/.nit/worktrees/backend/BACKEND-10312', branch: 'BACKEND-10312', repo: path });
  return {
    path,
    project: project?.name ?? path.split('/').pop() ?? '',
    exists: known,
    git: project ? project.git : known,
    busy,
    mode: busy.length && (project?.git ?? true) ? 'copy' : 'folder',
    branch: '',
    default_agent: project?.default_agent,
    worktrees,
  };
}

let launches = 0;

function launch(req: LaunchRequest) {
  const check = launchCheck(req.path);
  if (!check.exists) throw new HttpError(400, `Папки ${req.path} нет`);
  if (req.mode === 'copy' && !req.branch) throw new HttpError(400, 'Нужна ветка для копии');
  const n = ++launches;
  const temp = `${req.agent}:new-${n}`;
  const cwd = req.mode === 'copy' ? `/home/dev/.nit/worktrees/${check.project}/${req.branch}` : check.path;
  const base: SessionView = {
    key: temp,
    agent: req.agent,
    id: `new-${n}`,
    title: (req.prompt ?? '').split('\n')[0]!.slice(0, 120),
    project: check.project,
    repo: check.path,
    cwd,
    branch: req.mode === 'copy' ? req.branch : 'main',
    worktree: req.mode === 'copy' ? { path: cwd.replace('/home/dev', '~'), branch: req.branch!, repo: check.path } : undefined,
    created: now(),
    updated: now(),
    state: 'starting',
    since: now(),
    terminal: true,
    resumable: true,
  };
  prompts.set(temp, req.prompt ?? '');
  setTimeout(() => changed(base), 50);
  // Агент назвал свой ID: временный ключ уходит, приходит настоящий
  setTimeout(() => {
    const id = `${Math.random().toString(16).slice(2, 10)}-${n}`;
    const real: SessionView = { ...base, key: `${req.agent}:${id}`, id, state: 'working', since: now(), last_line: 'читаю проект' };
    prompts.set(real.key, req.prompt ?? '');
    world.sessions.delete(temp);
    world.sessions.set(real.key, real);
    emit('sessions', { changed: [real], removed: [temp], renamed: { [temp]: real.key } });
    setTimeout(() => patch(real.key, { state: 'waiting', since: now(), unseen: true, last: 'Посмотрел проект. С чего начать?' }), 6000);
  }, 1800);
  return { key: temp };
}

// ───────── Поиск ─────────

function hitsFor(q: string, conv: boolean, service: boolean): SearchHit[] {
  const needle = q.toLowerCase();
  const out: SearchHit[] = [];
  for (const s of world.sessions.values()) {
    if (s.service && !service) continue;
    if (s.title.toLowerCase().includes(needle)) out.push({ session: s, field: 'title' });
    else if (s.branch?.toLowerCase().includes(needle)) out.push({ session: s, field: 'branch' });
    else if (s.cwd.toLowerCase().includes(needle)) out.push({ session: s, field: 'path' });
    else if (conv) {
      const text = [...(world.details.get(s.key)?.messages ?? []).map((m) => m.text), s.last ?? ''].find((t) => t.toLowerCase().includes(needle));
      if (text) {
        const at = text.toLowerCase().indexOf(needle);
        out.push({ session: s, field: 'conversation', snippet: `…${text.slice(Math.max(0, at - 24), at + needle.length + 24)}…` });
      }
    }
  }
  // Как в макете: напоминания для бота находятся в переписке
  if (conv && 'вебхук'.startsWith(needle.slice(0, 6)) && needle.length >= 3) {
    const s = world.sessions.get('codex:d4e5f6a7-remind');
    if (s && !out.some((h) => h.session.key === s.key))
      out.push({ session: s, field: 'conversation', snippet: '…принимать вебхук Telegram вместо опроса…' });
  }
  // Сначала совпадения в названии, ветке и пути, потом в переписке
  const rank = (h: SearchHit) => (h.field === 'conversation' ? 1 : 0);
  return out.sort((a, b) => rank(a) - rank(b) || b.session.updated - a.session.updated);
}

function search(q: string, conv: boolean, service: boolean): SearchView {
  // Сервер отдаёт просто список; макет показывает ещё «68 сессий, около 20 секунд»
  return { hits: hitsFor(q, conv, service), scope: 68, seconds: 20 };
}

let searches = 0;

function fullSearch(q: string) {
  const id = `s${++searches}`;
  const total = 68;
  let done = 0;
  const found: SearchHit[] = [];
  const tick = () => {
    done = Math.min(total, done + 7);
    if (done > 30 && !found.length) {
      const s = world.sessions.get('kimi:3c5d7e9f-terraform');
      if (s) found.push({ session: s, field: 'log', snippet: `…${q} в журнале запуска terraform plan…` });
    }
    // Находки — только в последнем событии, как у сервера
    emit('search', { id, done, total, finished: done >= total, hits: done >= total ? found : null });
    if (done < total) setTimeout(tick, 250);
  };
  setTimeout(tick, 200);
  return { id };
}

// ───────── Поток событий ─────────

class MockEventStream implements EventStream {
  readyState = 0;
  onopen: ((e: Event) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>();

  constructor() {
    streams.add(this);
    stats.streams++;
    setTimeout(() => {
      // Отключённый телефон: сервер отвечает 401, EventSource закрывается сразу
      if (world.revoked) return this.fail();
      this.readyState = 1;
      this.onopen?.(new Event('open'));
      for (const [type, data] of pending.splice(0)) setTimeout(() => this.dispatch(type, data), 120);
    }, 30);
  }

  /** Соединение закрылось с ошибкой, как у EventSource после ответа не 200 */
  fail() {
    this.readyState = 2;
    streams.delete(this);
    this.onerror?.(new Event('error'));
  }

  addEventListener(type: string, l: (e: MessageEvent<string>) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), l]);
  }

  dispatch(type: string, data: unknown) {
    if (this.readyState !== 1) return;
    const e = new MessageEvent<string>(type, { data: JSON.stringify(data) });
    for (const l of this.listeners.get(type) ?? []) l(e);
  }

  close() {
    this.readyState = 2;
    streams.delete(this);
  }
}

/** События, которые придут сразу после подключения: упавшее задание в ?mock=digest-failed */
const pending: [string, unknown][] = [];

/** Сколько раз открывался поток событий и сколько было запросов: проверка, что отключённый телефон не переподключается */
const stats = { streams: 0, requests: 0 };

// ───────── Терминал ─────────

const prompts = new Map<string, string>();
const encoder = new TextEncoder();
// Экран после ответа агенту: общий для терминала и экрана телефона
const scenes = new Map<string, Scene>();
const typedText = new Map<string, string>();

function sceneOf(key: string): Scene {
  return scenes.get(key) ?? sceneFor(key, prompts.get(key));
}

/** Ответ агенту: работает несколько секунд и снова ждёт */
function answer(key: string, text: string) {
  const scene = sceneOf(key);
  scenes.set(key, { lines: [...scene.lines.filter((l) => !l.includes('❯')), `${DIM}> ${RESET}${text || '1'}`, '', '● Делаю.', ''], cursor: true });
  patch(key, { state: 'working', since: now(), unseen: false, last_line: 'выполняю ответ' });
  setTimeout(() => patch(key, { state: 'waiting', since: now(), unseen: true, last: 'Готово. Что дальше?' }), 4000);
}

const ANSI = /\x1b\[[0-9;?]*[A-Za-z]/g;

/** Экран текстом, как tmux capture-pane: без цветов */
function screenText(key: string): string {
  const scene = sceneOf(key);
  const lines = [...scene.lines];
  const s = world.sessions.get(key);
  if (scene.spinner) lines.push(scene.spinner(0, Math.max(0, now() - (s?.since ?? now()))));
  if (scene.cursor) lines.push(`> ${typedText.get(key) ?? ''}`);
  return lines.map((l) => l.replace(ANSI, '')).join('\n') + '\n\n\n';
}

/** Клавиши с телефона: текст копится в строке ввода, Enter или цифра — ответ */
function keys(key: string, text: string, names: string[]) {
  let typed = (typedText.get(key) ?? '') + text;
  for (const k of names) {
    if (k === 'Enter' || /^\d$/.test(k)) {
      answer(key, typed || k);
      typed = '';
    } else if (k === 'C-c' || k === 'Escape') typed = '';
  }
  typedText.set(key, typed);
  return { ok: true };
}

class MockTerminal implements TerminalSocket {
  binaryType: BinaryType = 'blob';
  readyState = 0;
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent<ArrayBuffer | string>) => void) | null = null;
  onclose: ((e: CloseEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  private key: string;
  private rows: number;
  private scene: Scene;
  private frame = 0;
  private timer = 0;
  private typed = '';

  constructor(url: string) {
    const u = new URL(url);
    this.key = decodeURIComponent(u.pathname.split('/').pop() ?? '');
    this.rows = Number(u.searchParams.get('rows')) || 24;
    const s = world.sessions.get(this.key);
    this.scene = sceneOf(this.key);
    setTimeout(() => {
      if (!s?.terminal) {
        this.readyState = 3;
        this.onclose?.(new CloseEvent('close', { code: 1006 }));
        return;
      }
      this.readyState = 1;
      this.onopen?.(new Event('open'));
      this.draw();
      if (this.scene.spinner) this.timer = window.setInterval(() => this.spin(), 100);
    }, 40);
  }

  private out(text: string) {
    if (this.readyState !== 1) return;
    const bytes = encoder.encode(text);
    this.onmessage?.(new MessageEvent('message', { data: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) }));
  }

  /** Весь экран заново, текст прижат к низу, как у агента в tmux */
  private draw() {
    const lines = [...this.scene.lines];
    if (this.scene.spinner) lines.push(this.spinLine());
    if (this.scene.cursor) lines.push(`${DIM}> ${RESET}${this.typed}`);
    const shown = lines.slice(-this.rows);
    const pad = Math.max(0, this.rows - shown.length);
    const cursor = this.scene.cursor ? '\x1b[?25h' : '\x1b[?25l';
    this.out(`\x1b[H\x1b[2J${cursor}${'\r\n'.repeat(pad)}${shown.join('\r\n')}`);
  }

  private spinLine(): string {
    const s = world.sessions.get(this.key);
    return this.scene.spinner!(this.frame, Math.max(0, now() - (s?.since ?? now())));
  }

  private spin() {
    this.frame++;
    this.out(`\r\x1b[2K${this.spinLine()}`);
  }

  send(data: string | ArrayBufferLike | ArrayBufferView) {
    if (typeof data === 'string') {
      try {
        const msg = JSON.parse(data) as { type?: string; rows?: number };
        if (msg.type === 'resize' && msg.rows && msg.rows !== this.rows) {
          this.rows = msg.rows;
          this.draw();
        }
      } catch {
        // не JSON — не размер
      }
      return;
    }
    const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array((data as ArrayBufferView).buffer);
    const text = new TextDecoder().decode(bytes);
    for (const ch of text) {
      if (ch === '\r') this.enter();
      else if (ch === '\x7f') this.typed = this.typed.slice(0, -1);
      else if (ch >= ' ') this.typed += ch;
    }
    if (this.scene.cursor) this.draw();
  }

  /** Ответ агенту: работает несколько секунд и снова ждёт */
  private enter() {
    answer(this.key, this.typed);
    this.typed = '';
    this.scene = sceneOf(this.key);
  }

  close(code = 1000) {
    if (this.readyState >= 2) return;
    window.clearInterval(this.timer);
    this.readyState = 3;
    setTimeout(() => this.onclose?.(new CloseEvent('close', { code })), 0);
  }
}

// ───────── Подключение ─────────

/** Выгрузка: файл с пометками и настройками */
function exportFile(): Response {
  const d = new Date(now() * 1000);
  const name = `ai-threads-${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}.json`;
  const data = { version: 1, sessions: {}, projects: {}, settings: world.settings };
  return new Response(JSON.stringify(data, null, 2), {
    status: 200,
    headers: { 'Content-Type': 'application/json', 'Content-Disposition': `attachment; filename="${name}"` },
  });
}

/** Старый сервер: история без X-Total-Count и X-Next-Cursor (?mock=nocursor) */
let noCursor = false;

/** Телефон отключили: запросы отвечают 401, поток событий закрывается */
function revoke() {
  world.revoked = true;
  for (const s of [...streams]) s.fail();
}

const REMOTE_MODES: Record<string, RemoteView['state']> = {
  'remote-off': 'off',
  'remote-login': 'needs_login',
  'remote-cert': 'certificate',
  'remote-running': 'running',
  'remote-error': 'error',
};

/**
 * mode — через запятую: still — часы стоят на 10:00; reboot — экран после перезагрузки;
 * empty — первый запуск без tmux и агентов; agents-off — агенты стоят, но выключены.
 * Сводки: digest-none — сводок нет; digest-running, digest-failed — задание за 3 дня идёт
 * или упало; summary-running, summary-failed — то же по сессии BACKEND-10300.
 * Телефон: remote-off, remote-login, remote-cert, remote-running, remote-error — состояние
 * доступа; phone-remote — страница открыта с телефона; revoked — телефон отключают через 0,4 с.
 * nocursor — история без заголовков страниц, как у старого сервера
 */
export function installMock(mode: string) {
  const modes = new Set(mode.split(',').map((m) => m.trim()));
  // 10:00 — время, от которого считаны подписи макета («2 мин», «вчера, 18:40», «9:41»)
  const d = new Date();
  d.setHours(10, 0, 0, 0);
  if (modes.has('still')) freezeClock(d.getTime() / 1000);
  const remoteMode = Object.entries(REMOTE_MODES).find(([m]) => modes.has(m))?.[1];
  world = createWorld(modes.has('still') ? d.getTime() / 1000 : now(), {
    reboot: modes.has('reboot'),
    empty: modes.has('empty'),
    agentsOff: modes.has('agents-off'),
    noDigests: modes.has('digest-none'),
    remote: remoteMode,
    phone: modes.has('phone-remote') || modes.has('revoked'),
  });
  if (world.settings.remote) world.settings.remote = { ...world.settings.remote, enabled: world.remote.enabled };
  noCursor = modes.has('nocursor');
  needsLogin = remoteMode === 'needs_login';
  if (modes.has('digest-running')) seedJob({ kind: 'digest', days: 3, reads: 12 });
  if (modes.has('summary-running')) seedJob({ kind: 'summary', key: 'claude:4f1c2a9e-10300', started: Math.floor(now()) - 42, elapsed: 42 });
  if (modes.has('digest-failed')) {
    const j = seedJob({ kind: 'digest', days: 3, stage: 'writing', started: Math.floor(now()) - 312, elapsed: 312 });
    world.jobs.delete(j.id);
    pending.push(['job', { ...j, ...failure('kimi'), elapsed: 312 }]);
  }
  if (modes.has('summary-failed')) {
    const j = seedJob({ kind: 'summary', key: 'claude:4f1c2a9e-10300', stage: 'writing', started: Math.floor(now()) - 95, elapsed: 95 });
    world.jobs.delete(j.id);
    pending.push(['job', { ...j, ...failure('kimi'), elapsed: 95 }]);
  }
  if (modes.has('revoked')) setTimeout(revoke, 400);

  setTransport({
    fetch: async (input, init) => {
      const url = new URL(input, location.href);
      const method = (init?.method ?? 'GET').toUpperCase();
      stats.requests++;
      await sleep(40);
      if (world.revoked && url.pathname.startsWith('/api/'))
        return json({ error: 'Этот телефон не подключён к «Нити». Откройте ссылку с ключом: на компьютере Настройки → Доступ с телефона.' }, 401);
      if (method === 'GET' && url.pathname === '/api/export') return exportFile();
      if (method === 'GET' && url.pathname === '/api/remote/qr.svg') {
        if (world.env.via === 'remote') return json({ error: 'Это можно сделать только на компьютере, не с телефона' }, 403);
        if (!world.remote.link) return json({ error: 'Ссылки ещё нет: доступ с телефона не включён или узел не в сети' }, 409);
        return new Response(qrSvg(world.remote.link), { status: 200, headers: { 'Content-Type': 'image/svg+xml' } });
      }
      let body: Record<string, unknown> = {};
      try {
        body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
      } catch {
        return json({ error: 'Нужен JSON-объект' }, 400);
      }
      try {
        const out = await handle(method, url, body);
        return out instanceof Response ? out : json(out);
      } catch (e) {
        if (e instanceof HttpError) return json({ error: e.message, fields: e.fields }, e.status);
        return json({ error: String(e) }, 500);
      }
    },
    events: () => new MockEventStream(),
    terminal: (url) => new MockTerminal(url),
  });

  // Для проверок из консоли и скриптов
  (window as unknown as { nitMock: unknown }).nitMock = {
    world: () => world,
    patch,
    /** Сессия закончила ход и ждёт: появится в «Ждут вас» */
    wait: (key: string, last = 'Готово. Посмотрите изменения?') => patch(key, { state: 'waiting', since: now(), unseen: true, last }),
    work: (key: string, line = 'go test ./...') => patch(key, { state: 'working', since: now(), unseen: false, last_line: line }),
    fail: (key: string, code = 1) => patch(key, { state: 'error', since: now(), exit_code: code }),
    add: (s: SessionView) => changed(s),
    /** Первый запуск: «поставить» tmux и агентов, потом «Проверить снова» */
    install: () => installAll(world),
    notice: (kind: string, text: string, key?: string) => emit('notice', { kind, text, key }),
    /** Следующее задание сводки закончится ошибкой */
    failNext: () => {
      failNext = true;
    },
    /** Сводка за период целиком: проверить разметку */
    setDigest: (d: Digest) => {
      world.digests.set(d.days, d);
      emit('digest', d);
    },
    /** Доступ с телефона в состоянии state (off, starting, needs_login, certificate, running, error) */
    remote: (state: RemoteView['state']) => {
      for (const t of remoteTimers) window.clearTimeout(t);
      world.remote = { ...remoteOf(state, now(), world.remoteKey), funnel: world.remote.funnel };
      remoteEmit();
    },
    /** Вошли в Tailscale: узел продолжает подниматься */
    login: () => {
      needsLogin = false;
      if (world.remote.enabled) bringUp();
    },
    /** Отключить этот телефон: запросы отвечают 401 */
    revoke,
    stats: () => ({ ...stats }),
  };
}
