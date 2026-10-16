// Действия интерфейса: открыть, переключить, пометить. Пометки меняются сразу, сервер подтверждает
// событием; при ошибке пометка возвращается и показывается сообщение.
import { api, errorText } from '../api/client';
import type { ProjectView, SessionView } from '../api/types';
import { toast } from '../components';
import { nextWaiting, waitingList } from './derive';
import { navigate } from './route';
import { sessionHash, sessionOf, store } from './state';

export function openSession(key: string) {
  navigate(sessionHash(key));
}

/** Главная с окном запуска; project — сразу выбрать проект */
export function openHome(project?: string) {
  const { launch } = store.get();
  // «Новая сессия» с экрана после перезагрузки — окно запуска, экран вернётся после перезагрузки страницы
  store.set({ launch: { project, seq: launch.seq + 1 }, rebootLater: true });
  navigate('#/');
}

/** Новая сессия на телефоне: свой экран; project — сразу выбрать проект */
export function openNew(project?: string) {
  const { launch } = store.get();
  store.set({ launch: { project, seq: launch.seq + 1 } });
  navigate('#/new');
}

export function toggleSidebar() {
  const hidden = !store.get().sidebarHidden;
  store.set({ sidebarHidden: hidden });
  try {
    localStorage.setItem('nit.sidebarHidden', hidden ? '1' : '0');
  } catch {
    // без localStorage панель просто не запомнится
  }
}

export function currentKey(): string | undefined {
  const { route } = store.get();
  return route.name === 'session' ? route.key : undefined;
}

/** Следующая ждущая после открытой, по кругу */
export function goNextWaiting() {
  const list = waitingList(store.get().sessions, store.get().keepNew);
  const next = nextWaiting(list, currentKey());
  if (next) openSession(next.key);
}

function patchSession(key: string, patch: Partial<SessionView>) {
  const s = store.get();
  if (s.sessions[key]) store.set({ sessions: { ...s.sessions, [key]: { ...s.sessions[key]!, ...patch } } });
  else if (s.extra[key]) store.set({ extra: { ...s.extra, [key]: { ...s.extra[key]!, ...patch } } });
}

/** Сессия, которую вернул сервер после изменения: она точнее, чем догадка экрана */
export function applyView(view: unknown) {
  const v = view as SessionView | null;
  if (v && typeof v === 'object' && typeof v.key === 'string' && typeof v.state === 'string') {
    const s = store.get();
    if (s.sessions[v.key] || !v.service) store.set({ sessions: { ...s.sessions, [v.key]: v } });
    else store.set({ extra: { ...s.extra, [v.key]: v } });
  }
}

async function mark(key: string, patch: Partial<SessionView>, call: () => Promise<unknown>) {
  const before = sessionOf(store.get(), key);
  patchSession(key, patch);
  try {
    applyView(await call());
  } catch (e) {
    if (before) {
      const undo: Partial<SessionView> = {};
      for (const k of Object.keys(patch) as (keyof SessionView)[]) (undo as Record<string, unknown>)[k] = before[k];
      patchSession(key, undo);
    }
    toast.show({ kind: 'error', title: 'Не получилось', text: errorText(e) });
    throw e;
  }
}

export function setPinned(key: string, on: boolean) {
  return mark(key, { pinned: on }, () => api.pin(key, on)).catch(() => undefined);
}

export function setHidden(key: string, on: boolean) {
  return mark(key, { hidden: on }, () => api.hide(key, on))
    .then(() => {
      if (on) toast.show({ title: 'Сессия скрыта', icon: 'visibility_off', action: { label: 'Отменить', onClick: () => void setHidden(key, false) } });
    })
    .catch(() => undefined);
}

export function setDone(key: string, on: boolean) {
  return mark(key, { done: on }, () => api.done(key, on)).catch(() => undefined);
}

export function rename(key: string, title: string) {
  const s = sessionOf(store.get(), key);
  const next = title.trim();
  if (!s || next === s.title) return Promise.resolve();
  // Пустое название — вернуть название из журнала
  const shown = next || s.original || s.title;
  return mark(key, { title: shown, original: next ? s.original || s.title : undefined }, () => api.rename(key, next)).catch(
    () => undefined,
  );
}

export function markSeen(key: string) {
  const s = sessionOf(store.get(), key);
  if (!s?.unseen) return;
  patchSession(key, { unseen: false });
  api.seen(key).catch(() => undefined);
}

export async function resume(key: string): Promise<boolean> {
  try {
    const res = await api.resume(key);
    if (res && typeof res === 'object' && 'key' in res) applyView(res);
    else patchSession(key, { terminal: true, state: 'starting', since: Date.now() / 1000 });
    return true;
  } catch (e) {
    toast.show({ kind: 'error', title: 'Не удалось продолжить', text: errorText(e) });
    return false;
  }
}

export async function reveal(target: { key?: string; path?: string }, app: 'folder' | 'vscode') {
  try {
    await api.reveal({ ...target, app });
  } catch (e) {
    toast.show({ kind: 'error', title: app === 'vscode' ? 'VS Code не открылся' : 'Папка не открылась', text: errorText(e) });
  }
}

/** Скопировать в буфер обмена; без доступа к буферу — через выделение */
export async function copyText(text: string, title = 'Скопировано') {
  let ok = false;
  try {
    await navigator.clipboard.writeText(text);
    ok = true;
  } catch {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    area.remove();
  }
  if (ok) toast.show({ title, icon: 'content_copy', duration: 3000 });
  else toast.show({ kind: 'error', title: 'Не удалось скопировать', text });
}

/** Команда для другого терминала: подключиться к терминалу «Нити» или продолжить сессию */
export async function copyCommand(key: string) {
  try {
    const d = await api.details(key);
    const cmd = (d.session.terminal && d.attach) || d.resume || d.attach;
    if (cmd) await copyText(cmd, 'Команда скопирована');
    else toast.show({ kind: 'error', title: 'Команды нет', text: 'Эту сессию нельзя продолжить в другом терминале' });
  } catch (e) {
    toast.show({ kind: 'error', title: 'Не удалось получить команду', text: errorText(e) });
  }
}

export function startFinish(key: string) {
  store.set({ finishKey: key });
}

function patchProject(path: string, patch: Partial<ProjectView>) {
  store.set((s) => ({ projects: s.projects.map((p) => (p.path === path ? { ...p, ...patch } : p)) }));
}

async function markProject(path: string, patch: Partial<ProjectView>, call: () => Promise<ProjectView>) {
  const before = store.get().projects.find((p) => p.path === path);
  patchProject(path, patch);
  try {
    const view = await call();
    if (view && typeof view === 'object' && view.path === path) patchProject(path, view);
  } catch (e) {
    if (before) patchProject(path, before);
    toast.show({ kind: 'error', title: 'Не получилось', text: errorText(e) });
  }
}

export function setProjectPinned(path: string, on: boolean) {
  return markProject(path, { pinned: on }, () => api.project({ path, pinned: on }));
}

export function setProjectHidden(path: string, on: boolean) {
  return markProject(path, { hidden: on }, () => api.project({ path, hidden: on })).then(() => {
    if (on) toast.show({ title: 'Проект скрыт', icon: 'visibility_off', action: { label: 'Отменить', onClick: () => void setProjectHidden(path, false) } });
  });
}

export function setProjectAgent(path: string, agent: string) {
  return markProject(path, { default_agent: (agent || undefined) as ProjectView['default_agent'] }, () =>
    api.project({ path, default_agent: agent }),
  );
}
