// Состояние приложения: данные сервера, адрес экрана и флаги интерфейса.
import type {
  AgentView,
  Digest,
  EnvView,
  JobView,
  ProgressView,
  ProjectView,
  RebootSession,
  RemoteView,
  SessionCounts,
  SessionView,
  Settings,
} from '../api/types';
import { createStore, useSelect, type Store } from '../lib/store';

export type RouteName = 'home' | 'new' | 'session' | 'history' | 'summary' | 'settings' | 'gallery';

export interface Route {
  name: RouteName;
  /** Ключ сессии для session */
  key?: string;
  /** Параметры после «?»: history?project= */
  query: Record<string, string>;
}

export interface AppState {
  status: 'loading' | 'ready' | 'error';
  error: string;
  env: EnvView | null;
  agents: AgentView[];
  settings: Settings | null;
  projects: ProjectView[];
  /** Сессии каталога по ключу (без служебных) */
  sessions: Record<string, SessionView>;
  /** Сессии не из каталога: служебные, открытые из поиска */
  extra: Record<string, SessionView>;
  progress: ProgressView | null;
  /** Поток событий на связи */
  online: boolean;
  /** Терминалы, открытые до перезагрузки: пока список не пуст, главная предлагает их продолжить */
  reboot: RebootSession[];
  /** Человек ушёл с экрана после перезагрузки к новой сессии: до перезагрузки страницы он не мешает */
  rebootLater: boolean;
  /** Сколько сессий всего и служебных, из первой загрузки */
  counts: SessionCounts | null;
  /** Задания сводок по id: идущие и последние закончившиеся, пока их не убрали */
  jobs: Record<string, JobView>;
  /** Сводки за период по числу дней: null — ещё не составляли, нет ключа — не загружена */
  digests: Partial<Record<number, Digest | null>>;
  /** Доступ с телефона: только на компьютере, после первого запроса */
  remote: RemoteView | null;
  /** Телефон отключили от «Нити»: вместо приложения — страница об этом */
  revoked: boolean;
  /** Сколько раз состояние перечитано целиком: после переподключения страницы перечитывают своё */
  synced: number;

  route: Route;
  sidebarHidden: boolean;
  /** Терминал во весь экран */
  full: boolean;
  searchOpen: boolean;
  /** Сведения о сессии поверх терминала */
  detailsOpen: boolean;
  /** Название сессии правится в шапке */
  renaming: boolean;
  /** Диалог «Завершить задачу?» для этой сессии */
  finishKey: string | null;
  /** Временный ключ новой сессии → настоящий */
  swaps: Record<string, string>;
  /** Открытая сессия была новой, когда её открыли: в «Ждут вас» она не съезжает вниз */
  keepNew: string | null;
  /** Запрос открыть окно запуска: проект и номер, по которому поле получает фокус */
  launch: { project?: string; seq: number };
}

export function parseRoute(hash: string): Route {
  const raw = hash.replace(/^#/, '');
  if (raw === 'gallery') return { name: 'gallery', query: {} };
  const [pathPart = '', queryPart = ''] = raw.split('?');
  const query: Record<string, string> = {};
  for (const [k, v] of new URLSearchParams(queryPart)) query[k] = v;
  const path = pathPart.replace(/^\/+/, '');
  if (path.startsWith('s/')) {
    const key = decodeURIComponent(path.slice(2));
    if (key) return { name: 'session', key, query };
  }
  if (path === 'history') return { name: 'history', query };
  if (path === 'new') return { name: 'new', query };
  if (path === 'summary') return { name: 'summary', query };
  if (path === 'settings') return { name: 'settings', query };
  return { name: 'home', query };
}

export function sessionHash(key: string): string {
  return `#/s/${encodeURIComponent(key)}`;
}

function readFlag(name: string): boolean {
  try {
    return localStorage.getItem(name) === '1';
  } catch {
    return false;
  }
}

export const store: Store<AppState> = createStore<AppState>({
  status: 'loading',
  error: '',
  env: null,
  agents: [],
  settings: null,
  projects: [],
  sessions: {},
  extra: {},
  progress: null,
  online: true,
  reboot: [],
  rebootLater: false,
  counts: null,
  jobs: {},
  digests: {},
  remote: null,
  revoked: false,
  synced: 0,
  route: parseRoute(location.hash),
  sidebarHidden: readFlag('nit.sidebarHidden'),
  full: false,
  searchOpen: false,
  detailsOpen: false,
  renaming: false,
  finishKey: null,
  swaps: {},
  keepNew: null,
  launch: { seq: 0 },
});

export function useApp<R>(select: (s: AppState) => R, equal?: (a: R, b: R) => boolean): R {
  return useSelect(store, select, equal);
}

/** Сессия по ключу: из каталога или из открытых отдельно */
export function sessionOf(s: AppState, key: string | undefined): SessionView | undefined {
  if (!key) return undefined;
  return s.sessions[key] ?? s.extra[key];
}

/** Установленные агенты, которых можно запустить */
export function launchableAgents(s: AppState): AgentView[] {
  return s.agents.filter((a) => a.enabled && !!a.program && a.id !== 'zcode');
}

/** Можно ли запускать: есть tmux и хотя бы один агент */
export function canLaunch(s: AppState): boolean {
  return !!s.env?.tmux && launchableAgents(s).length > 0;
}

/** Первый запуск: данные пришли, а запускать нечем — главная объясняет, что поставить */
export function needsSetup(s: AppState): boolean {
  return s.status === 'ready' && !!s.env && !canLaunch(s);
}

/** Главная предлагает продолжить сессии после перезагрузки */
export function showsReboot(s: AppState): boolean {
  return s.status === 'ready' && s.reboot.length > 0 && !s.rebootLater && canLaunch(s);
}
