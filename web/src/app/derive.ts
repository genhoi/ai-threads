// Списки панели из сессий: «Ждут вас», «Работают», дерево проектов.
import type { ProjectView, SessionView } from '../api/types';
import type { ProjectMark } from '../components';

/** Сессия видна в списках: не скрыта и не служебная */
function listed(s: SessionView): boolean {
  return !s.hidden && !s.service;
}

/** Ждёт человека: закончила ход или упала. Завершённые не в счёт */
export function needsYou(s: SessionView): boolean {
  return listed(s) && !s.done && (s.state === 'waiting' || s.state === 'error');
}

export function isWorking(s: SessionView): boolean {
  return listed(s) && !s.done && (s.state === 'working' || s.state === 'starting');
}

/** Терминал открыт или агент ещё что-то делает */
export function isActive(s: SessionView): boolean {
  return s.terminal || (s.state !== 'idle' && s.state !== 'exited');
}

export function isNew(s: SessionView): boolean {
  return s.state === 'waiting' && !!s.unseen;
}

/**
 * «Ждут вас»: сначала новые, потом кто дольше ждёт. keepNew — открытая сессия, которая была новой,
 * когда её открыли: она остаётся на своём месте, пока открыта, хотя «новое» с неё уже снято.
 */
export function waitingList(sessions: Record<string, SessionView>, keepNew?: string | null): SessionView[] {
  const fresh = (s: SessionView) => Number(isNew(s) || (s.key === keepNew && s.state === 'waiting'));
  return Object.values(sessions)
    .filter(needsYou)
    .sort((a, b) => fresh(b) - fresh(a) || (a.since ?? a.updated) - (b.since ?? b.updated) || cmpKey(a, b));
}

/** «Работают»: недавно начавшие сверху */
export function workingList(sessions: Record<string, SessionView>): SessionView[] {
  return Object.values(sessions)
    .filter(isWorking)
    .sort((a, b) => (b.since ?? b.updated) - (a.since ?? a.updated) || cmpKey(a, b));
}

function cmpKey(a: SessionView, b: SessionView): number {
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

export interface ProjectNode {
  project: ProjectView;
  /** Сначала активные, потом несколько последних */
  rows: SessionView[];
  activeCount: number;
  /** Сколько сессий не показано */
  more: number;
  marks: ProjectMark[];
}

const RECENT_ROWS = 3;

/** Сессии по проектам: путь основного репозитория, иначе имя проекта */
export function sessionsByProject(projects: ProjectView[], sessions: Record<string, SessionView>): Map<string, SessionView[]> {
  const byPath = new Map<string, SessionView[]>();
  const pathOfName = new Map<string, string>();
  for (const p of projects) {
    byPath.set(p.path, []);
    if (!pathOfName.has(p.name)) pathOfName.set(p.name, p.path);
  }
  for (const s of Object.values(sessions)) {
    if (!listed(s)) continue;
    const path = byPath.has(s.repo ?? '') ? s.repo! : byPath.has(s.cwd) ? s.cwd : pathOfName.get(s.project);
    if (path) byPath.get(path)!.push(s);
  }
  return byPath;
}

/** Порядок проектов в панели и выборе проекта: закреплённые, потом недавние */
export function sortProjects(projects: ProjectView[]): ProjectView[] {
  return [...projects].sort(
    (a, b) => Number(!!b.pinned) - Number(!!a.pinned) || b.updated - a.updated || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0),
  );
}

export function projectTree(projects: ProjectView[], sessions: Record<string, SessionView>): ProjectNode[] {
  const byPath = sessionsByProject(projects, sessions);
  return sortProjects(projects.filter((p) => !p.hidden))
    .map((project) => {
      const all = byPath.get(project.path) ?? [];
      const active = all.filter(isActive).sort((a, b) => b.created - a.created || cmpKey(a, b));
      const idle = all
        .filter((s) => !isActive(s))
        .sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || b.updated - a.updated || cmpKey(a, b));
      const recent = idle.slice(0, Math.max(1, RECENT_ROWS - active.length));
      const rows = [...active, ...recent];
      const total = Math.max(project.sessions, all.length);
      const marks = active
        .filter((s) => !s.done && s.state !== 'idle' && s.state !== 'exited')
        .map((s) => ({ state: s.state, isNew: isNew(s) }));
      return { project, rows, activeCount: active.length, more: Math.max(0, total - rows.length), marks };
    });
}

/** Следующая ждущая после текущей, по кругу */
export function nextWaiting(list: SessionView[], current: string | undefined): SessionView | undefined {
  if (!list.length) return undefined;
  const i = list.findIndex((s) => s.key === current);
  if (i === -1) return list[0];
  if (list.length === 1) return undefined;
  return list[(i + 1) % list.length];
}

/** Название для экрана: своё, из журнала или по первому запросу */
export function titleOf(s: SessionView): string {
  return s.title || s.original || 'Без названия';
}
