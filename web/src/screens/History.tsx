import { useRef, useState } from 'preact/hooks';
import type { SessionView } from '../api/types';
import { AgentBadge, Button, Chip, FilterChip, Icon, IconButton, Menu, StatusMark, type MenuEntry } from '../components';
import { AGENTS, type AgentId } from '../model';
import { openSession, toggleSidebar } from '../app/actions';
import { isNew, sortProjects, titleOf } from '../app/derive';
import { replaceRoute } from '../app/route';
import { useApp } from '../app/state';
import { clock, sessionsCount } from '../lib/format';
import { byDay, filtersOf, historyHash, projectName, useHistory, useInfinite, useLive, type HistoryFilters } from './useHistory';

const AGENT_ORDER: AgentId[] = ['claude', 'codex', 'grok', 'kimi', 'cursor', 'zcode'];

/** История: все сессии по дням, фильтры по проекту, агенту и служебным, подгрузка при прокрутке */
export function History() {
  const query = useApp((s) => s.route.query);
  const sidebarHidden = useApp((s) => s.sidebarHidden);
  const projects = useApp((s) => s.projects);
  const f = filtersOf(query);
  const list = useHistory(f);
  const items = useLive(list.items);
  const groups = byDay(items);
  const sentinel = useInfinite(list.more, !list.done && !list.error, list.items.length);
  const projectChip = useRef<HTMLButtonElement>(null);
  const agentChip = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState<'project' | 'agent' | null>(null);

  const set = (patch: Partial<HistoryFilters>) => replaceRoute(historyHash({ ...f, ...patch }));

  const shownProjects = sortProjects(projects.filter((p) => !p.hidden || p.path === f.project));
  const projectItems: MenuEntry[] = [
    { id: '', label: 'Все проекты', icon: 'folder', checked: !f.project, onSelect: () => set({ project: undefined }) },
    { type: 'separator' },
    ...shownProjects.map((p) => ({
      id: p.path,
      label: p.name,
      icon: 'folder' as const,
      checked: p.path === f.project,
      onSelect: () => set({ project: p.path }),
    })),
  ];
  if (f.project && !shownProjects.some((p) => p.path === f.project))
    projectItems.push({ id: f.project, label: projectName(f.project), icon: 'folder', checked: true });

  const agentItems: MenuEntry[] = [
    { id: '', label: 'Все агенты', icon: 'history', checked: !f.agent, onSelect: () => set({ agent: undefined }) },
    { type: 'separator' },
    ...AGENT_ORDER.map((id) => ({ id, label: AGENTS[id].name, agent: id, checked: id === f.agent, onSelect: () => set({ agent: id }) })),
  ];

  const filtered = !!(f.project || f.agent || f.service);
  return (
    <main class="page hist">
      <header class={`page__head${sidebarHidden ? ' page__head--wide' : ''}`}>
        {sidebarHidden && <IconButton icon="left_panel_open" label="Показать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />}
        <h1 class="page__title">История</h1>
        <Chip
          ref={projectChip}
          label="Проект"
          value={f.project ? projectName(f.project) : 'все'}
          open={open === 'project'}
          onClick={() => setOpen(open === 'project' ? null : 'project')}
        />
        <Chip
          ref={agentChip}
          label="Агент"
          value={f.agent ? (AGENTS[f.agent as AgentId]?.name ?? f.agent) : 'все'}
          open={open === 'agent'}
          onClick={() => setOpen(open === 'agent' ? null : 'agent')}
        />
        <FilterChip label="Служебные" on={f.service} onChange={(on) => set({ service: on })} />
        <span class="hist__fill" />
        {list.total !== null && <span class="hist__count">{sessionsCount(list.total)}</span>}
      </header>

      <div class="hist__scroll">
        <div class="hist__list">
          {groups.map((g, i) => (
            <section key={g.key} class="hist__day" aria-label={g.title}>
              <h2 class={`hist__dayname${i === 0 ? ' hist__dayname--first' : ''}`}>{g.title}</h2>
              {g.items.map((s) => (
                <HistoryRow key={s.key} s={s} />
              ))}
            </section>
          ))}
          {list.error ? (
            <div class="hist__state" role="alert">
              <span class="hist__error">{list.error}</span>
              <Button variant="regular" size={32} icon="restart_alt" onClick={list.retry}>
                Повторить
              </Button>
            </div>
          ) : list.loading ? (
            <div class="hist__state" role="status" aria-label="Загрузка">
              <Icon name="progress_activity" size={18} spin />
            </div>
          ) : (
            list.done &&
            !items.length && <p class="hist__empty">{filtered ? 'Ничего не найдено' : 'Сессий пока нет'}</p>
          )}
          <div ref={sentinel} class="hist__sentinel" />
        </div>
      </div>

      <Menu
        open={open === 'project'}
        anchor={projectChip.current}
        label="Проект"
        class="hist-menu"
        items={projectItems}
        activeId={f.project ?? ''}
        onClose={() => setOpen(null)}
      />
      <Menu
        open={open === 'agent'}
        anchor={agentChip.current}
        label="Агент"
        width={260}
        gap={10}
        items={agentItems}
        activeId={f.agent ?? ''}
        onClose={() => setOpen(null)}
      />
    </main>
  );
}

/** Подпись строки: «backend», у завершённой ещё « · завершена». Агента показывает иконка */
export function historyWho(s: SessionView): string {
  return `${s.project}${s.done ? ' · завершена' : ''}`;
}

/** Последняя фраза или «Ошибка · код N» */
export function historyTail(s: SessionView): { text: string; error: boolean } | null {
  if (s.state === 'error') return { text: s.exit_code === undefined ? 'Ошибка' : `Ошибка · код ${s.exit_code}`, error: true };
  const text = s.last || s.last_line;
  return text ? { text, error: false } : null;
}

/** Знак слева: состояние, у завершённой — галочка */
export function HistoryMark({ s, size }: { s: SessionView; size?: 'sm' | 'md' }) {
  if (s.done) return <Icon name="check" size={14} class="hrow__check" label="завершена" />;
  return <StatusMark state={s.state} isNew={isNew(s)} size={size} />;
}

function HistoryRow({ s }: { s: SessionView }) {
  const tail = historyTail(s);
  const classes = ['hrow', isNew(s) && 'hrow--new', s.done && 'hrow--done'].filter(Boolean).join(' ');
  return (
    <button type="button" class={classes} onClick={() => openSession(s.key)}>
      <span class="hrow__mark">
        <HistoryMark s={s} />
      </span>
      <span class="hrow__body">
        <span class="hrow__line1">
          <AgentBadge agent={s.agent as AgentId} size={16} class="hrow__agent" />
          <span class="hrow__title">{titleOf(s)}</span>
          <span class="hrow__time">{clock(s.updated)}</span>
        </span>
        <span class="hrow__meta">
          <span class="hrow__who">
            {historyWho(s)}
            {tail ? ' · ' : ''}
          </span>
          {tail && <span class={tail.error ? 'hrow__error' : undefined}>{tail.text}</span>}
        </span>
      </span>
    </button>
  );
}
