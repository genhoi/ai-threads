// Поиск: быстрый (GET /api/search) и полный по журналам (POST /api/search/full, ход — событием search).
// Формы — как в internal/server/api.go (SearchHit, SearchEvent); всё про поиск — в этом файле.
import { request } from './client';
import type { SessionView } from './types';

/** Где нашлось совпадение: title, original, project, branch, path, id, summary, conversation, log */
export type SearchField = string;

export interface SearchHit {
  session: SessionView;
  field?: SearchField;
  /** Отрывок вокруг совпадения: для переписки, сводки и журнала */
  snippet?: string;
}

/** Ответ быстрого поиска. Сервер отдаёт список находок; поля ниже — если он узнает больше */
export interface SearchView {
  hits: SearchHit[];
  /** Сколько сессий просмотрит полный поиск и сколько секунд это займёт */
  scope?: number;
  seconds?: number;
}

/** Событие search: ход полного поиска. Находки — в последнем событии, с finished */
export interface SearchEvent {
  id: string;
  done: number;
  total: number;
  finished?: boolean;
  hits: SearchHit[] | null;
}

export interface FullSearchRequest {
  q: string;
  service?: boolean;
}

export interface FullSearchStarted {
  id: string;
}

export async function quickSearch(q: string, opts: { conv: boolean; service: boolean }, signal?: AbortSignal): Promise<SearchView> {
  const params = new URLSearchParams({ q });
  if (opts.conv) params.set('conv', '1');
  if (opts.service) params.set('service', '1');
  const res = await request<SearchHit[] | SearchView | null>('GET', `/api/search?${params}`, undefined, signal);
  if (Array.isArray(res)) return { hits: res };
  return { ...res, hits: res?.hits ?? [] };
}

export function fullSearch(q: string, opts: { service: boolean }): Promise<FullSearchStarted> {
  return request<FullSearchStarted>('POST', '/api/search/full', { q, service: opts.service } satisfies FullSearchRequest);
}

/** Закончен ли полный поиск */
export function searchFinished(e: SearchEvent): boolean {
  return e.finished ?? (e.total > 0 && e.done >= e.total);
}

/** Сколько просмотрено */
export function searchDone(e: SearchEvent): number {
  return e.done;
}

/** Слить найденное: по ключу сессии, новое заменяет старое, порядок появления сохраняется */
export function mergeHits(old: SearchHit[], add: SearchHit[] | null): SearchHit[] {
  if (!add?.length) return old;
  const byKey = new Map(old.map((h) => [h.session.key, h]));
  for (const h of add) byKey.set(h.session.key, h);
  return [...byKey.values()];
}

// Подписка на событие search: её зовёт поток событий
type SearchListener = (e: SearchEvent) => void;
const listeners = new Set<SearchListener>();

export function onSearchEvent(l: SearchListener): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export function emitSearchEvent(e: SearchEvent) {
  for (const l of listeners) l(e);
}
