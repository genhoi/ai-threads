// Связь с сервером: первая загрузка, поток событий и разбор событий в состояние.
import { api, errorText, onUnauthorized } from '../api/client';
import { emitSearchEvent, type SearchEvent } from '../api/search';
import { transport, type EventStream } from '../api/transport';
import type { Digest, JobView, NoticeEvent, ProgressView, ProjectView, RemoteView, SessionView, SessionsEvent } from '../api/types';
import { toast } from '../components';
import { AGENTS } from '../model';
import { needsYou, titleOf } from './derive';
import { applyDigest, applyJob, loadJobs } from './jobs';
import { navigate, replaceRoute } from './route';
import { sessionHash, store } from './state';

// 401 отвечает только проверка телефона: его отключили на компьютере или сделали новый ключ.
// Пока сервер не ответил, откуда пришли (env нет), 401 значит то же самое
onUnauthorized(() => {
  const { env } = store.get();
  if (!env || env.via === 'remote') markRevoked();
});

/** Телефон отключён: приложение сменяется страницей об этом, поток событий больше не переподключается */
function markRevoked() {
  if (store.get().revoked) return;
  window.clearTimeout(retryTimer);
  const es = stream;
  stream = null;
  es?.close();
  store.set({ revoked: true, online: false });
}

export async function loadBootstrap(): Promise<void> {
  try {
    const b = await api.bootstrap();
    const sessions: Record<string, SessionView> = {};
    for (const s of b.sessions ?? []) sessions[s.key] = s;
    // Временные сессии, которые сервер ещё не знает под этим ключом, остаются до события
    const keep = Object.fromEntries(Object.entries(store.get().sessions).filter(([k]) => provisional.has(k) && !sessions[k]));
    // Страницу открыли сразу на новой сессии: она остаётся на своём месте в «Ждут вас»
    const { route, status } = store.get();
    const opened = route.name === 'session' && route.key ? sessions[route.key] : undefined;
    const keepNew = status === 'loading' && opened?.state === 'waiting' && opened.unseen ? opened.key : store.get().keepNew;
    store.set({
      keepNew,
      status: 'ready',
      error: '',
      env: b.env,
      agents: b.agents ?? [],
      settings: b.settings,
      projects: b.projects ?? [],
      sessions: { ...keep, ...sessions },
      progress: b.progress,
      counts: b.counts ?? null,
      synced: store.get().synced + 1,
    });
    void loadReboot();
    void loadJobs();
  } catch (e) {
    if (store.get().status !== 'ready') store.set({ status: 'error', error: errorText(e) });
  }
}

/** Терминалы, которых не стало после перезагрузки. Ошибка не мешает работе: экран просто не покажется */
async function loadReboot() {
  try {
    const r = await api.reboot();
    const list = r.sessions ?? [];
    // Новая перезагрузка, пока страница была открыта: экран снова показывается
    const fresh = list.length > 0 && store.get().reboot.length === 0;
    store.set({ reboot: list, ...(fresh ? { rebootLater: false } : {}) });
  } catch {
    store.set({ reboot: [] });
  }
}

// Временные ключи новых сессий: <агент>:new-…, пока агент не назвал свой ID
const provisional = new Map<string, { agent: string; cwd: string; at: number }>();

/** Сессия только что запущена: показать её сразу, не дожидаясь события */
export function addProvisional(s: SessionView) {
  provisional.set(s.key, { agent: s.agent, cwd: s.cwd, at: Date.now() });
  const { sessions } = store.get();
  if (!sessions[s.key]) store.set({ sessions: { ...sessions, [s.key]: s } });
}

function findSwap(temp: string, changed: SessionView[], known: Record<string, SessionView>): SessionView | undefined {
  const info = provisional.get(temp) ?? (known[temp] ? { agent: known[temp]!.agent, cwd: known[temp]!.cwd, at: 0 } : undefined);
  if (!info) return undefined;
  const fresh = changed.filter((s) => s.key !== temp && !known[s.key] && s.agent === info.agent);
  return fresh.find((s) => s.cwd === info.cwd) ?? (fresh.length === 1 ? fresh[0] : undefined);
}

const lastToast = new Map<string, number>();

function canToast(key: string): boolean {
  const at = lastToast.get(key) ?? 0;
  if (Date.now() - at < 10_000) return false;
  lastToast.set(key, Date.now());
  return true;
}

function onScreen(key: string): boolean {
  const { route } = store.get();
  return route.name === 'session' && route.key === key && document.visibilityState === 'visible' && document.hasFocus();
}

function notifyChange(prev: SessionView | undefined, next: SessionView) {
  if (!needsYou(next) || onScreen(next.key)) return;
  const agent = AGENTS[next.agent]?.short ?? next.agent;
  const open = { label: 'Открыть', onClick: () => navigate(sessionHash(next.key)) };
  if (next.state === 'waiting' && next.unseen && !(prev?.state === 'waiting' && prev.unseen)) {
    if (!canToast(next.key)) return;
    toast.show({ kind: 'waiting', title: `${agent} ждёт · ${next.project}`, text: next.last || titleOf(next), action: open });
  } else if (next.state === 'error' && prev && prev.state !== 'error') {
    if (!canToast(next.key)) return;
    const code = next.exit_code === undefined ? 'ошибка' : `ошибка · код ${next.exit_code}`;
    toast.show({ kind: 'error', title: `${agent} · ${next.project}`, text: `${titleOf(next)}: ${code}`, action: open });
  }
}

export function applySessions(e: SessionsEvent) {
  const state = store.get();
  const sessions = { ...state.sessions };
  const extra = { ...state.extra };
  const swaps = { ...state.swaps };
  const changed = e.changed ?? [];
  let swapTo: string | undefined;

  // Сервер сам говорит, какой временный ключ стал каким
  for (const [temp, real] of Object.entries(e.renamed ?? {})) {
    swaps[temp] = real;
    provisional.delete(temp);
    if (state.route.name === 'session' && state.route.key === temp) swapTo = real;
  }

  for (const key of e.removed ?? []) {
    if (!swaps[key] && (key.includes(':new-') || provisional.has(key))) {
      const real = findSwap(key, changed, state.sessions);
      if (real) {
        swaps[key] = real.key;
        provisional.delete(key);
        if (state.route.name === 'session' && state.route.key === key) swapTo = real.key;
      }
    }
    delete sessions[key];
    delete extra[key];
  }

  for (const s of changed) {
    const prev = state.sessions[s.key] ?? state.extra[s.key];
    if (s.service && !state.sessions[s.key]) extra[s.key] = s;
    else sessions[s.key] = s;
    if (prev) delete extra[s.key];
    notifyChange(prev, s);
  }

  // Временный ключ мог уйти раньше настоящего: ловим настоящий в следующих событиях
  for (const [temp, info] of provisional) {
    if (sessions[temp]) continue;
    if (Date.now() - info.at > 60_000) {
      provisional.delete(temp);
      continue;
    }
    const real = changed.find((s) => s.agent === info.agent && s.cwd === info.cwd && !state.sessions[s.key]);
    if (real) {
      swaps[temp] = real.key;
      provisional.delete(temp);
      if (state.route.name === 'session' && state.route.key === temp) swapTo = real.key;
    }
  }

  store.set({ sessions, extra, swaps });
  if (swapTo) replaceRoute(sessionHash(swapTo));
  // Открытая сессия исчезла совсем (например, новая так и не получила свой ID и её закрыли) — на главную
  else if (state.route.name === 'session' && state.route.key && (e.removed ?? []).includes(state.route.key) && !sessions[state.route.key])
    replaceRoute('#/');
}

function parse<T>(e: MessageEvent<string>): T | undefined {
  try {
    return JSON.parse(e.data) as T;
  } catch {
    return undefined;
  }
}

let stream: EventStream | null = null;
let retry = 0;
let retryTimer = 0;

/** Поток событий. Обрыв — переподключение с паузой, после него состояние перечитывается целиком */
export function connectEvents() {
  window.clearTimeout(retryTimer);
  if (store.get().revoked) return;
  stream?.close();
  const es = transport.events('/api/events');
  stream = es;
  let opened = false;

  es.onopen = () => {
    if (retry > 0 || opened) void loadBootstrap();
    opened = true;
    retry = 0;
    store.set({ online: true });
  };
  es.onerror = () => {
    if (stream !== es) return;
    store.set({ online: false });
    // EventSource переподключается сам, пока не закрылся окончательно
    if (es.readyState !== 2) return;
    es.close();
    retry++;
    const delay = Math.min(10_000, 500 * 2 ** Math.min(retry, 5));
    const again = () => {
      if (stream === es && !store.get().revoked) retryTimer = window.setTimeout(connectEvents, delay);
    };
    // С телефона поток закрывается и тогда, когда телефон отключили: сервер отвечает 401, и это
    // видно только по обычному запросу. 401 покажет страницу «Этот телефон отключён», иначе — снова
    if (store.get().env?.via === 'remote') void api.health().then(again, again);
    else again();
  };

  es.addEventListener('sessions', (e) => {
    const data = parse<SessionsEvent>(e);
    if (data) applySessions(data);
  });
  es.addEventListener('projects', (e) => {
    const data = parse<ProjectView[]>(e);
    if (data) store.set({ projects: data });
  });
  es.addEventListener('progress', (e) => {
    const data = parse<ProgressView>(e);
    if (data) store.set({ progress: data });
  });
  es.addEventListener('notice', (e) => {
    const n = parse<NoticeEvent>(e);
    if (n) showNotice(n);
  });
  es.addEventListener('search', (e) => {
    const data = parse<SearchEvent>(e);
    if (data) emitSearchEvent(data);
  });
  es.addEventListener('job', (e) => {
    const data = parse<JobView>(e);
    if (data) applyJob(data);
  });
  es.addEventListener('digest', (e) => {
    const data = parse<Digest>(e);
    if (data) applyDigest(data);
  });
  // Только на компьютер: в событии ссылка входа с ключом
  es.addEventListener('remote', (e) => {
    const data = parse<RemoteView>(e);
    if (!data) return;
    const { env } = store.get();
    store.set({ remote: data, ...(env ? { env: { ...env, remote: { state: data.state, host: data.host } } } : {}) });
  });
}

function showNotice(n: NoticeEvent) {
  const s = n.key ? (store.get().sessions[n.key] ?? store.get().extra[n.key]) : undefined;
  const open = n.key ? { label: 'Открыть', onClick: () => navigate(sessionHash(n.key!)) } : undefined;
  if (n.kind === 'waiting') {
    // То же, что уже показано по смене состояния, второй раз не показывать
    if (!n.key || onScreen(n.key) || !canToast(n.key)) return;
    const title = s ? `${AGENTS[s.agent]?.short ?? s.agent} ждёт · ${s.project}` : n.text;
    toast.show({ kind: 'waiting', title, text: s ? n.text : undefined, action: open });
    return;
  }
  const [first = '', ...rest] = n.text.split('\n');
  if (n.kind === 'error') toast.show({ kind: 'error', title: first, text: rest.join('\n') || undefined, action: open });
  else toast.show({ kind: 'info', icon: 'info', title: first, text: rest.join('\n') || undefined, action: open });
}
