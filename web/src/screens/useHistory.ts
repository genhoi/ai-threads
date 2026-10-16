import { useEffect, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { SessionView } from '../api/types';
import { store, useApp } from '../app/state';
import { dayKey, dayTitle } from '../lib/format';

/** Фильтры истории, как в адресе #/history?project=&agent=&service=1 */
export interface HistoryFilters {
  project?: string;
  agent?: string;
  service: boolean;
}

export function filtersOf(query: Record<string, string>): HistoryFilters {
  return { project: query.project || undefined, agent: query.agent || undefined, service: query.service === '1' };
}

export function historyHash(f: HistoryFilters): string {
  const p = new URLSearchParams();
  if (f.project) p.set('project', f.project);
  if (f.agent) p.set('agent', f.agent);
  if (f.service) p.set('service', '1');
  const qs = p.toString();
  return `#/history${qs ? '?' + qs : ''}`;
}

const PAGE = 60;

/** Папка проекта сессии, как её считает сервер: основной репозиторий, иначе папка сессии */
export function projectPathOf(s: SessionView): string {
  return s.repo || s.cwd;
}

function matches(s: SessionView, f: HistoryFilters): boolean {
  return (!f.project || projectPathOf(s) === f.project) && (!f.agent || s.agent === f.agent);
}

interface ListState {
  items: SessionView[];
  loading: boolean;
  done: boolean;
  error: string;
  /** X-Total-Count первой страницы: сколько сессий под фильтрами всего */
  total?: number;
  /** cursor — сервер листает курсором из X-Next-Cursor; before — старый сервер, по времени */
  mode: 'cursor' | 'before' | null;
}

interface PageResult {
  added: SessionView[];
  done: boolean;
  total?: number;
}

/**
 * История страницами по 60. Сервер отдаёт курсор следующей страницы (X-Next-Cursor) и сколько
 * сессий всего (X-Total-Count); старый сервер — нет, тогда следующая страница — сессии,
 * изменённые раньше последней показанной. Порядок строк держится, пока экран открыт;
 * содержимое строк — живое, из событий.
 */
export function useHistory(f: HistoryFilters) {
  const [state, setState] = useState<ListState>({ items: [], loading: true, done: false, error: '', mode: null });
  const stateRef = useRef(state);
  stateRef.current = state;
  const seq = useRef(0);
  const next = useRef('');
  const filterKey = `${f.project ?? ''}\u0000${f.agent ?? ''}\u0000${f.service ? 1 : 0}`;

  const fetchPage = async (id: number, items: SessionView[], mode: ListState['mode']): Promise<PageResult | null> => {
    const seen = new Set(items.map((s) => s.key));
    if (!items.length || mode === 'cursor') {
      const res = await api.sessionsPage({ ...f, cursor: items.length ? next.current : undefined, limit: PAGE });
      if (id !== seq.current) return null;
      if (res.total !== undefined) {
        next.current = res.next ?? '';
        return { added: res.items.filter((s) => !seen.has(s.key)), done: !res.next, total: res.total };
      }
      return { added: res.items, done: res.items.length < PAGE };
    }
    // Старый сервер: before+1 и отсев показанных, чтобы сессии с той же секундой на стыке страниц не терялись
    const before = items[items.length - 1]!.updated;
    const page = (await api.sessions({ ...f, before: before + 1, limit: PAGE })) ?? [];
    if (id !== seq.current) return null;
    const fresh = page.filter((s) => !seen.has(s.key));
    if (!fresh.length && page.length >= PAGE) {
      // Больше страницы сессий с одной и той же секундой: пропустить эту секунду
      const rest = (await api.sessions({ ...f, before, limit: PAGE })) ?? [];
      if (id !== seq.current) return null;
      const added = rest.filter((s) => !seen.has(s.key));
      return { added, done: !added.length };
    }
    return { added: fresh, done: page.length < PAGE || !fresh.length };
  };

  const load = (reset: boolean) => {
    const cur = stateRef.current;
    if (!reset && (cur.loading || cur.done)) return;
    const id = reset ? ++seq.current : seq.current;
    const items = reset ? [] : cur.items;
    const mode = reset ? null : cur.mode;
    const total = reset ? undefined : cur.total;
    if (reset) next.current = '';
    setState({ items, loading: true, done: false, error: '', total, mode });
    fetchPage(id, items, mode)
      .then((page) => {
        if (!page || id !== seq.current) return;
        setState({
          items: [...items, ...page.added],
          loading: false,
          done: page.done,
          error: '',
          total: page.total ?? total,
          mode: mode ?? (page.total !== undefined ? 'cursor' : 'before'),
        });
      })
      .catch((e) => {
        if (id === seq.current) setState({ items, loading: false, done: false, error: errorText(e), total, mode });
      });
  };

  useEffect(() => {
    load(true);
    return () => {
      seq.current++;
    };
  }, [filterKey]);

  // Сколько всего: из X-Total-Count. Старый сервер его не знает: без служебных — по сессиям из
  // первой загрузки, со служебными — один полный запрос
  const listed = useApp((s) => {
    let n = 0;
    for (const x of Object.values(s.sessions)) if (!x.service && matches(x, f)) n++;
    return n;
  });
  const [withService, setWithService] = useState<number | null>(null);
  const legacy = state.mode === 'before';
  useEffect(() => {
    setWithService(null);
    if (!f.service || !legacy) return;
    const ctl = new AbortController();
    api
      .sessions({ project: f.project, agent: f.agent, service: true }, ctl.signal)
      .then((all) => setWithService((all ?? []).length))
      .catch(() => undefined);
    return () => ctl.abort();
  }, [filterKey, legacy]);
  const total = state.total ?? (state.mode === null ? null : f.service ? withService : listed);

  return { ...state, total, more: () => load(false), retry: () => load(stateRef.current.items.length === 0) };
}

/**
 * Живая сессия для строки: состояние, название и последняя фраза из событий. Время изменения —
 * из загрузки: по нему строка стоит в своём дне, и список не перестраивается под курсором
 */
export function useLive(items: SessionView[]): SessionView[] {
  const sessions = useApp((s) => s.sessions);
  const extra = useApp((s) => s.extra);
  return items.map((it) => {
    const live = sessions[it.key] ?? extra[it.key];
    return live ? { ...live, updated: it.updated } : it;
  });
}

export interface DayGroup {
  key: string;
  title: string;
  items: SessionView[];
}

/** Строки по дням последнего изменения */
export function byDay(items: SessionView[]): DayGroup[] {
  const groups: DayGroup[] = [];
  for (const s of items) {
    const key = dayKey(s.updated);
    let g = groups[groups.length - 1];
    if (!g || g.key !== key) {
      g = { key, title: dayTitle(s.updated), items: [] };
      groups.push(g);
    }
    g.items.push(s);
  }
  return groups;
}

/**
 * Подгрузка следующей страницы, когда низ списка близко. count — сколько строк уже есть:
 * после каждой страницы проверка заново, иначе короткий список так и остался бы недогруженным
 */
export function useInfinite(more: () => void, active: boolean, count: number) {
  const ref = useRef<HTMLDivElement>(null);
  const moreRef = useRef(more);
  moreRef.current = more;
  useEffect(() => {
    const el = ref.current;
    if (!el || !active) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) moreRef.current();
      },
      { rootMargin: '600px 0px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [active, count]);
  return ref;
}

/** Имя проекта по папке: из списка проектов, иначе последняя часть пути */
export function projectName(path: string | undefined): string {
  if (!path) return '';
  return store.get().projects.find((p) => p.path === path)?.name ?? path.split('/').filter(Boolean).pop() ?? path;
}
