// Сводки: задания (ход из событий job), запуск и остановка, сводки за период из событий digest.
import { api, errorText } from '../api/client';
import type { Digest, DigestDays, JobView } from '../api/types';
import { store, type AppState } from './state';

/** Новое о задании: из события или ответа на запуск */
export function applyJob(j: JobView) {
  const { jobs } = store.get();
  const prev = jobs[j.id];
  // Ответ на запуск может прийти позже событий: закончившееся задание не возвращается к ходу
  if (prev?.done && !j.done) return;
  store.set({ jobs: prune({ ...jobs, [j.id]: j }) });
}

/** Закончившиеся задания, кроме последнего по каждой сессии и периоду, больше не нужны */
function prune(jobs: Record<string, JobView>): Record<string, JobView> {
  const latest = new Map<string, JobView>();
  for (const j of Object.values(jobs)) {
    const t = target(j);
    const cur = latest.get(t);
    if (!cur || j.started >= cur.started) latest.set(t, j);
  }
  const out: Record<string, JobView> = {};
  for (const j of Object.values(jobs)) if (!j.done || latest.get(target(j)) === j) out[j.id] = j;
  return out;
}

function target(j: JobView): string {
  return j.kind === 'summary' ? `s:${j.key ?? ''}` : `d:${j.days ?? 0}`;
}

/** Последнее задание по сессии или периоду: идущее или закончившееся */
function latest(jobs: Record<string, JobView>, match: (j: JobView) => boolean): JobView | undefined {
  let best: JobView | undefined;
  for (const j of Object.values(jobs)) {
    if (!match(j)) continue;
    if (!best || j.started > best.started || (j.started === best.started && !j.done)) best = j;
  }
  return best;
}

/** Задание, которое показать: идущее или упавшее. Готовое и остановленное не показываются */
export function shownJob(j: JobView | undefined): JobView | undefined {
  return j && (!j.done || j.stage === 'failed') ? j : undefined;
}

export function summaryJob(s: AppState, key: string): JobView | undefined {
  return latest(s.jobs, (j) => j.kind === 'summary' && j.key === key);
}

export function digestJob(s: AppState, days: number): JobView | undefined {
  return latest(s.jobs, (j) => j.kind === 'digest' && j.days === days);
}

/** Задание убрали с экрана: «Закрыть» у ошибки */
export function dismissJob(id: string) {
  const jobs = { ...store.get().jobs };
  delete jobs[id];
  store.set({ jobs });
}

/**
 * Идущие задания с сервера: при загрузке страницы и после переподключения. Задания, которые шли,
 * а в списке их нет, закончились, пока не было связи: итог уже сохранён, показывать нечего.
 */
export async function loadJobs() {
  try {
    const list = (await api.jobs()) ?? [];
    const jobs = { ...store.get().jobs };
    const running = new Set(list.map((j) => j.id));
    for (const [id, j] of Object.entries(jobs)) if (!j.done && !running.has(id)) delete jobs[id];
    for (const j of list) jobs[j.id] = j;
    store.set({ jobs: prune(jobs) });
  } catch {
    // без списка заданий страница просто не покажет ход того, что шло до неё
  }
}

/** Составить сводку по сессии. Ответ — текст ошибки или пусто */
export async function startSummary(key: string, agent?: string): Promise<string> {
  try {
    const r = await api.summarize(key, agent);
    if (r?.job) applyJob(r.job);
    return '';
  } catch (e) {
    return errorText(e);
  }
}

/** Составить сводку за период. Ответ — текст ошибки или пусто */
export async function startDigest(days: DigestDays, agent?: string): Promise<string> {
  try {
    const r = await api.startDigest({ days, agent });
    if (r?.job) applyJob(r.job);
    return '';
  } catch (e) {
    return errorText(e);
  }
}

/** Остановить агента. Итог придёт событием job; если задание уже кончилось — ничего не будет */
export async function cancelJob(id: string): Promise<string> {
  try {
    await api.cancelJob(id);
    return '';
  } catch (e) {
    return errorText(e);
  }
}

/** Сохранённая сводка за период: из события digest или ответа GET /api/digest */
export function applyDigest(d: Digest) {
  store.set((s) => ({ digests: { ...s.digests, [d.days]: d } }));
}

/** Сводка за период и идущее задание с сервера */
export async function loadDigest(days: DigestDays, signal?: AbortSignal) {
  const v = await api.digest(days, signal);
  store.set((s) => ({ digests: { ...s.digests, [days]: v.digest ?? null } }));
  if (v.job) applyJob(v.job);
}
