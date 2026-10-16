// Запросы к серверу «Нити». Ошибка ответа — ApiError с текстом и ошибками по полям из ErrorView.
import type {
  Bootstrap,
  CancelResult,
  Details,
  DigestDays,
  DigestRequest,
  DigestView,
  EnvCheck,
  ErrorView,
  FinishCheck,
  FinishRequest,
  JobStarted,
  JobView,
  LaunchCheck,
  LaunchRequest,
  LaunchResponse,
  ModelsView,
  FlagRequest,
  KeysRequest,
  ProjectRequest,
  ProjectView,
  RebootRequest,
  RebootResult,
  RebootView,
  RemoteRequest,
  RemoteView,
  RenameRequest,
  RevealRequest,
  ScreenView,
  SessionView,
  Settings,
  SummaryRequest,
} from './types';
import type { AgentId } from '../model';
import { transport } from './transport';

export class ApiError extends Error {
  status: number;
  fields: Record<string, string>;

  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.fields = fields ?? {};
  }
}

/** Язык интерфейса: на нём сервер пишет ошибки, если в настройках язык «Авто» */
export const UI_LANG = 'ru';

/** Тело запроса как есть, без JSON.stringify: файл выгрузки */
export class RawBody {
  constructor(readonly text: string) {}
}

// 401 отвечает только проверка телефона: устройство отключили или сделали новый ключ.
// Что делать, решает состояние приложения: оно подписывается здесь, чтобы client не зависел от него
let unauthorized: ((status: number) => void) | null = null;

/** Подписка на ответ 401 любого запроса */
export function onUnauthorized(fn: ((status: number) => void) | null) {
  unauthorized = fn;
}

export async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  return (await requestFull<T>(method, path, body, signal)).data;
}

/** Запрос с заголовками ответа: история читает из них число сессий и следующую страницу */
export async function requestFull<T>(
  method: 'GET' | 'POST',
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<{ data: T; headers: Headers }> {
  const headers: Record<string, string> = { Accept: 'application/json', 'Accept-Language': UI_LANG };
  const init: RequestInit = { method, signal: signal ?? null, headers };
  if (method === 'POST') {
    headers['Content-Type'] = 'application/json';
    init.body = body instanceof RawBody ? body.text : JSON.stringify(body ?? {});
  }
  let res: Response;
  try {
    res = await transport.fetch(path, init);
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'Нет связи с «Нитью»');
  }
  const text = await res.text();
  let data: unknown = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = undefined;
    }
  }
  if (!res.ok) {
    if (res.status === 401) unauthorized?.(res.status);
    const err = data as Partial<ErrorView> | undefined;
    throw new ApiError(res.status, err?.error || text.trim() || `HTTP ${res.status}`, err?.fields);
  }
  return { data: data as T, headers: res.headers };
}

const enc = encodeURIComponent;

/** Фильтры истории: GET /api/sessions */
export interface SessionsQuery {
  /** Папка проекта */
  project?: string;
  agent?: string;
  /** Вместе со служебными */
  service?: boolean;
  /** Время Unix: только сессии, изменённые раньше, — следующая страница у старого сервера */
  before?: number;
  /** Следующая страница: значение заголовка X-Next-Cursor (<updated>:<ключ>) */
  cursor?: string;
  limit?: number;
}

/** Страница истории. total и next — из заголовков; у старого сервера их нет */
export interface SessionsPage {
  items: SessionView[];
  /** X-Total-Count: сколько сессий подходит под фильтры всего */
  total?: number;
  /** X-Next-Cursor: курсор следующей страницы; пусто — страниц больше нет */
  next?: string;
}

function sessionsUrl(q: SessionsQuery): string {
  const p = new URLSearchParams();
  if (q.project) p.set('project', q.project);
  if (q.agent) p.set('agent', q.agent);
  if (q.service) p.set('service', '1');
  if (q.before) p.set('before', String(q.before));
  if (q.cursor) p.set('cursor', q.cursor);
  if (q.limit) p.set('limit', String(q.limit));
  const qs = p.toString();
  return `/api/sessions${qs ? '?' + qs : ''}`;
}

const terminalPath = (key: string, action: string) => `/api/terminal/${enc(key)}/${action}`;
const sessionPath = (key: string, action?: string) => `/api/sessions/${enc(key)}${action ? '/' + action : ''}`;

export const api = {
  bootstrap: () => request<Bootstrap>('GET', '/api/bootstrap'),

  details: (key: string, signal?: AbortSignal) => request<Details>('GET', sessionPath(key), undefined, signal),
  /** Пустое название возвращает название из журнала */
  // Пометки, продолжение и завершение отвечают сессией после изменения
  rename: (key: string, name: string) => request<SessionView>('POST', sessionPath(key, 'rename'), { name } satisfies RenameRequest),
  pin: (key: string, on: boolean) => request<SessionView>('POST', sessionPath(key, 'pin'), { on } satisfies FlagRequest),
  hide: (key: string, on: boolean) => request<SessionView>('POST', sessionPath(key, 'hide'), { on } satisfies FlagRequest),
  done: (key: string, on: boolean) => request<SessionView>('POST', sessionPath(key, 'done'), { on } satisfies FlagRequest),
  seen: (key: string) => request<SessionView>('POST', sessionPath(key, 'seen'), {}),
  resume: (key: string) => request<SessionView>('POST', sessionPath(key, 'resume'), {}),
  /** Составить сводку по сессии; ответ — задание, ход придёт событиями job */
  summarize: (key: string, agent?: string) =>
    request<JobStarted>('POST', sessionPath(key, 'summary'), (agent ? { agent } : {}) satisfies SummaryRequest),
  finishCheck: (key: string) => request<FinishCheck>('GET', sessionPath(key, 'finish')),
  finish: (key: string, req: FinishRequest) => request<SessionView>('POST', sessionPath(key, 'finish'), req),

  /** prompt — чтобы сервер предложил имя ветки по запросу */
  launchCheck: (path: string, signal?: AbortSignal, prompt?: string) =>
    request<LaunchCheck>('GET', `/api/launch/check?path=${enc(path)}${prompt ? `&prompt=${enc(prompt)}` : ''}`, undefined, signal),
  launch: (req: LaunchRequest) => request<LaunchResponse>('POST', '/api/launch', req),
  models: (agent: AgentId) => request<ModelsView>('GET', `/api/models?agent=${enc(agent)}`),

  reveal: (req: RevealRequest) => request<unknown>('POST', '/api/reveal', req),

  project: (req: ProjectRequest) => request<ProjectView>('POST', '/api/project', req),

  /** История: сначала недавние */
  sessions: (q: SessionsQuery, signal?: AbortSignal) => request<SessionView[] | null>('GET', sessionsUrl(q), undefined, signal),
  /** История страницей: вместе с числом сессий и курсором следующей страницы, если сервер их знает */
  sessionsPage: async (q: SessionsQuery, signal?: AbortSignal): Promise<SessionsPage> => {
    const { data, headers } = await requestFull<SessionView[] | null>('GET', sessionsUrl(q), undefined, signal);
    const total = headers.get('X-Total-Count');
    const n = total === null || total.trim() === '' ? NaN : Number(total);
    return {
      items: data ?? [],
      total: Number.isFinite(n) ? n : undefined,
      next: headers.has('X-Next-Cursor') ? (headers.get('X-Next-Cursor') ?? '') : undefined,
    };
  },

  /** Сводка за период и идущее задание */
  digest: (days: DigestDays, signal?: AbortSignal) => request<DigestView>('GET', `/api/digest?days=${days}`, undefined, signal),
  /** Составить сводку за период; если задание уже идёт — ответ о нём */
  startDigest: (req: DigestRequest) => request<JobStarted>('POST', '/api/digest', req),
  /** Идущие задания: для страницы, открытой посреди работы */
  jobs: () => request<JobView[] | null>('GET', '/api/jobs'),
  /** Остановить агента; прежняя сводка остаётся */
  cancelJob: (id: string) => request<CancelResult>('POST', `/api/jobs/${enc(id)}/cancel`, {}),

  /** Доступ с телефона: только с компьютера, с телефона — 403 */
  remote: () => request<RemoteView>('GET', '/api/remote'),
  setRemote: (req: RemoteRequest) => request<RemoteView>('POST', '/api/remote', req),
  /** Новый ключ: старые ссылки перестают работать, все телефоны отключаются */
  remoteKey: () => request<RemoteView>('POST', '/api/remote/key', {}),
  revokeDevice: (id: string) => request<RemoteView>('POST', `/api/remote/devices/${enc(id)}/revoke`, {}),

  /** Сервер на связи и пускает: телефону — проверка, не отключили ли его */
  health: () => request<{ ok: boolean }>('GET', '/api/health'),

  /** Терминалы, открытые до перезагрузки */
  reboot: () => request<RebootView>('GET', '/api/reboot'),
  /** Продолжить выбранные; dismiss — забыть остальные */
  rebootAction: (req: RebootRequest) => request<RebootResult>('POST', '/api/reboot', req),

  /** tmux и агенты заново: «Проверить снова» */
  env: () => request<EnvCheck>('GET', '/api/env'),

  settings: () => request<Settings>('GET', '/api/settings'),
  /** Настройки целиком; ответ — настройки после проверки */
  saveSettings: (s: Settings) => request<Settings>('POST', '/api/settings', s),

  /** Экран терминала текстом: history строк истории и сам экран */
  screen: (key: string, history: number, signal?: AbortSignal) =>
    request<ScreenView>('GET', `${terminalPath(key, 'screen')}?history=${history}`, undefined, signal),
  /** Текст как есть, потом клавиши по именам tmux */
  keys: (key: string, req: KeysRequest) => request<unknown>('POST', terminalPath(key, 'keys'), req),

  /** Загрузить файл выгрузки как есть */
  importData: (text: string) => request<unknown>('POST', '/api/import', new RawBody(text)),
};

/** Выгрузка одним файлом: запрос через transport (работает и с ?mock), потом сохранение */
export async function downloadExport(): Promise<void> {
  let res: Response;
  try {
    res = await transport.fetch('/api/export', { headers: { 'Accept-Language': UI_LANG } });
  } catch {
    throw new ApiError(0, 'Нет связи с «Нитью»');
  }
  if (!res.ok) {
    const text = await res.text();
    let message = text.trim() || `HTTP ${res.status}`;
    try {
      message = (JSON.parse(text) as ErrorView).error || message;
    } catch {
      // не JSON — текст как есть
    }
    throw new ApiError(res.status, message);
  }
  const blob = await res.blob();
  const name = /filename="?([^";]+)"?/.exec(res.headers.get('Content-Disposition') ?? '')?.[1] ?? 'ai-threads.json';
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** QR-код ссылки входа с телефона: через transport, чтобы работал и с ?mock. Ответ — адрес картинки blob: */
export async function remoteQR(signal?: AbortSignal): Promise<string> {
  let res: Response;
  try {
    res = await transport.fetch('/api/remote/qr.svg', { headers: { 'Accept-Language': UI_LANG }, signal: signal ?? null });
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'Нет связи с «Нитью»');
  }
  if (!res.ok) {
    const text = await res.text();
    let message = text.trim() || `HTTP ${res.status}`;
    try {
      message = (JSON.parse(text) as ErrorView).error || message;
    } catch {
      // не JSON — текст как есть
    }
    throw new ApiError(res.status, message);
  }
  const svg = await res.text();
  return URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' }));
}

/** Текст ошибки для сообщения */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}
