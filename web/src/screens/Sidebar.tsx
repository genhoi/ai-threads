import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api } from '../api/client';
import type { LaunchCheck, ProjectView, SessionView } from '../api/types';
import { Button, IconButton, Menu, ProjectRow, SessionRow, type MenuEntry } from '../components';
import { AGENTS, type AgentId } from '../model';
import { goNextWaiting, openHome, openSession, toggleSidebar } from '../app/actions';
import { isNew, isWorking, needsYou, projectTree, titleOf, waitingList, workingList, type ProjectNode } from '../app/derive';
import { projectMenu, rowMenu } from '../app/menus';
import { navigate } from '../app/route';
import { canLaunch, store, useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { ago, timer, when } from '../lib/format';
import { RollingCount } from './RollingCount';

interface Structure {
  /** Строки «Ждут вас» и «Работают» целиком: ушедшая из раздела строка дорисовывается такой, какой была */
  waiting: SessionView[];
  working: SessionView[];
  projects: { path: string; rows: string[]; more: number }[];
}

function readExpanded(): Set<string> | null {
  try {
    const raw = localStorage.getItem('nit.expanded');
    return raw ? new Set(JSON.parse(raw) as string[]) : null;
  } catch {
    return null;
  }
}

function saveExpanded(set: Set<string>) {
  try {
    localStorage.setItem('nit.expanded', JSON.stringify([...set]));
  } catch {
    // не запомнится — не страшно
  }
}

type OpenMenu = { kind: 'session'; key: string; anchor: HTMLElement } | { kind: 'project'; path: string; anchor: HTMLElement };

/** Сколько строк «Ждут вас» видно сразу; остальные — под «ещё N» */
const WAITING_ROWS = 5;

/**
 * Вид панели: normal — всё; reboot — только проекты, свёрнутые (экран после перезагрузки);
 * empty — ничего, кроме «Новой сессии» и низа (первый запуск без проектов)
 */
export type SidebarVariant = 'normal' | 'reboot' | 'empty';

/**
 * Панель слева: «Новая сессия», поиск, «Ждут вас», «Работают», «Проекты» и внизу история, сводка,
 * настройки. Пока курсор над панелью или открыто её меню, строки не переставляются и не появляются:
 * щелчок попадает туда, куда целились. Новые строки приходят, когда курсор уйдёт.
 */
export function Sidebar({ onSearch, variant = 'normal' }: { onSearch: () => void; variant?: SidebarVariant }) {
  const sessions = useApp((s) => s.sessions);
  const projects = useApp((s) => s.projects);
  const route = useApp((s) => s.route);
  const ready = useApp((s) => s.status === 'ready');
  const launchOk = useApp(canLaunch);
  const vscode = useApp((s) => !!s.env?.vscode);

  const [hover, setHover] = useState(false);
  const [menu, setMenu] = useState<OpenMenu | null>(null);
  const [expanded, setExpanded] = useState<Set<string> | null>(readExpanded);
  // После перезагрузки проекты свёрнуты; раскрытие здесь не запоминается
  const [rebootExpanded, setRebootExpanded] = useState<Set<string>>(() => new Set());
  const [waitingOpen, setWaitingOpen] = useState(false);

  const keepNew = useApp((s) => s.keepNew);
  const waiting = useMemo(() => waitingList(sessions, keepNew), [sessions, keepNew]);
  const working = useMemo(() => workingList(sessions), [sessions]);
  const tree = useMemo(() => projectTree(projects, sessions), [projects, sessions]);

  // Порядок строк: живой или замороженный, пока курсор над панелью
  const live: Structure = useMemo(
    () => ({
      waiting,
      working,
      projects: tree.map((n) => ({ path: n.project.path, rows: n.rows.map((s) => s.key), more: n.more })),
    }),
    [waiting, working, tree],
  );
  const frozen = ready && (hover || !!menu);
  const snap = useRef(live);
  if (!frozen) snap.current = live;
  const shown = snap.current;

  // Первая отрисовка с данными — без появления строк; дальше новые строки «Ждут вас» раскрываются
  const settled = useRef(false);
  useEffect(() => {
    if (ready) settled.current = true;
  }, [ready]);

  // Раскрыт по умолчанию первый проект
  const isExpanded = (path: string) =>
    variant === 'reboot' ? rebootExpanded.has(path) : expanded ? expanded.has(path) : tree[0]?.project.path === path;
  const toggle = (path: string) => {
    if (variant === 'reboot') {
      const next = new Set(rebootExpanded);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      setRebootExpanded(next);
      return;
    }
    const next = new Set(expanded ?? (tree[0] ? [tree[0].project.path] : []));
    if (next.has(path)) next.delete(path);
    else next.add(path);
    setExpanded(next);
    saveExpanded(next);
  };

  const selected = route.name === 'session' ? route.key : undefined;
  const lookup = (key: string): SessionView | undefined => sessions[key];
  // Строка осталась в разделе — живые данные; ушла из него — прежний вид, чтобы не менялась высота.
  // Скрытые и завершённые человеком уходят сразу: это его собственное действие
  const keepRows = (rows: SessionView[], belongs: (s: SessionView) => boolean) =>
    rows
      .map((old) => {
        const now = sessions[old.key];
        if (!now || now.hidden || now.done) return undefined;
        return belongs(now) ? now : old;
      })
      .filter((s): s is SessionView => !!s);
  const allWaitingRows = keepRows(shown.waiting, needsYou);
  // Не больше пяти строк, чтобы «Работают» и «Проекты» не уезжали вниз; открытая сессия видна всегда
  const waitingRows =
    waitingOpen || allWaitingRows.length <= WAITING_ROWS + 1
      ? allWaitingRows
      : allWaitingRows.filter((s, i) => i < WAITING_ROWS || s.key === selected);
  const waitingMore = allWaitingRows.length - waitingRows.length;
  const workingRows = keepRows(shown.working, isWorking);
  const middle = variant === 'normal';
  const showWaiting = middle && (frozen ? allWaitingRows.length > 0 : waiting.length > 0);
  const showWorking = middle && (frozen ? workingRows.length > 0 : working.length > 0);
  const above = new Set([...waitingRows, ...workingRows].map((s) => s.key));
  const nodes = new Map(tree.map((n) => [n.project.path, n]));
  const projectRows = shown.projects
    .map((p) => ({ node: nodes.get(p.path), rows: p.rows, more: p.more }))
    .filter((p): p is { node: ProjectNode; rows: string[]; more: number } => !!p.node);

  const sessionMenuItems = (key: string): MenuEntry[] => {
    const s = sessions[key];
    return s ? rowMenu(s, vscode) : [];
  };

  return (
    <aside class="sb" aria-label="Панель" onPointerEnter={() => setHover(true)} onPointerLeave={() => setHover(false)}>
      <div class="sb__top">
        {launchOk ? (
          <Button variant="primary" icon="add" class="sb__new" tooltip="Новая сессия" shortcut={['N']} onClick={() => openHome()}>
            Новая сессия
          </Button>
        ) : (
          <Button variant="primary" icon="add" class="sb__new" disabled tooltip="Сначала установите tmux и агента">
            Новая сессия
          </Button>
        )}
        <IconButton icon="search" label="Поиск" shortcut={['Ctrl', 'K']} onClick={onSearch} />
        <IconButton icon="left_panel_close" label="Спрятать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />
      </div>

      <div class="sb__scroll">
        {showWaiting && (
          <section class="sb-sec" aria-label="Ждут вас">
            <div class="sb-head sb-head--waiting">
              <h2 class="sb-head__title">Ждут вас</h2>
              <RollingCount value={waiting.length} class="sb-head__count" />
              <span class="sb-head__fill" />
              <IconButton
                icon="keyboard_arrow_down"
                label="Следующая ждущая"
                shortcut={['Alt', '↓']}
                size={28}
                onClick={goNextWaiting}
              />
            </div>
            {waitingRows.map((s) => (
              <EnterRow key={s.key} animate={settled.current}>
                <WaitingRow
                  s={s}
                  selected={s.key === selected}
                  menuOpen={menu?.kind === 'session' && menu.key === s.key}
                  onMenu={(anchor) => setMenu({ kind: 'session', key: s.key, anchor })}
                />
              </EnterRow>
            ))}
            {waitingMore > 0 && (
              <button type="button" class="sb-more" aria-expanded={false} onClick={() => setWaitingOpen(true)}>
                ещё {waitingMore}
              </button>
            )}
            {waitingOpen && allWaitingRows.length > WAITING_ROWS + 1 && (
              <button type="button" class="sb-more" aria-expanded onClick={() => setWaitingOpen(false)}>
                свернуть
              </button>
            )}
          </section>
        )}

        {showWorking && (
          <section class="sb-sec" aria-label="Работают">
            <div class={`sb-head${showWaiting ? '' : ' sb-head--first'}`}>
              <h2 class="sb-head__title">Работают</h2>
              {working.length > 0 && <span class="sb-head__count">{working.length}</span>}
            </div>
            {workingRows.map((s) => (
              <WorkingRow
                key={s.key}
                s={s}
                selected={s.key === selected}
                menuOpen={menu?.kind === 'session' && menu.key === s.key}
                onMenu={(anchor) => setMenu({ kind: 'session', key: s.key, anchor })}
              />
            ))}
          </section>
        )}

        {variant !== 'empty' && (
          <section class="sb-sec sb-sec--projects" aria-label="Проекты">
            <div class={`sb-head${showWaiting || showWorking ? '' : ' sb-head--first'}`}>
              <h2 class="sb-head__title">Проекты</h2>
              <span class="sb-head__count">{tree.length}</span>
            </div>
            {projectRows.map(({ node, rows, more }) => (
              <ProjectBlock
                key={node.project.path}
                actions={variant === 'normal'}
                node={node}
                more={more}
                rows={rows.map(lookup).filter((s): s is SessionView => !!s && !s.hidden)}
                expanded={isExpanded(node.project.path)}
                selected={selected}
                above={above}
                menuOpen={menu?.kind === 'project' && menu.path === node.project.path}
                onToggle={() => toggle(node.project.path)}
                onMenu={(anchor) => setMenu({ kind: 'project', path: node.project.path, anchor })}
              />
            ))}
          </section>
        )}
      </div>

      <nav class="sb__nav" aria-label="Разделы">
        <Button
          variant="quiet"
          size={36}
          icon="history"
          pressed={route.name === 'history'}
          aria-current={route.name === 'history' ? 'page' : undefined}
          tooltip="История"
          shortcut={['H']}
          onClick={() => navigate('#/history')}
        >
          История
        </Button>
        <Button
          variant="quiet"
          size={36}
          icon="summarize"
          pressed={route.name === 'summary'}
          aria-current={route.name === 'summary' ? 'page' : undefined}
          onClick={() => navigate('#/summary')}
        >
          Сводка
        </Button>
        <span class="sb__fill" />
        <IconButton
          icon="settings"
          label="Настройки"
          size={36}
          pressed={route.name === 'settings'}
          aria-current={route.name === 'settings' ? 'page' : undefined}
          onClick={() => navigate('#/settings')}
        />
      </nav>

      {menu?.kind === 'session' && (
        <Menu
          open
          anchor={menu.anchor}
          label="Меню сессии"
          placement="bottom-end"
          items={sessionMenuItems(menu.key)}
          onClose={() => setMenu(null)}
        />
      )}
      {menu?.kind === 'project' && <ProjectMenu path={menu.path} anchor={menu.anchor} vscode={vscode} onClose={() => setMenu(null)} />}
    </aside>
  );
}

/** Новая строка «Ждут вас»: раскрывается по высоте и один раз вспыхивает */
function EnterRow({ animate, children }: { animate: boolean; children: ComponentChildren }) {
  const [enter, setEnter] = useState(animate);
  // После раскрытия и вспышки класс снимается: строке больше не нужно обрезать содержимое
  useEffect(() => {
    if (!enter) return;
    const t = window.setTimeout(() => setEnter(false), 1500);
    return () => window.clearTimeout(t);
  }, []);
  return <div class={enter ? 'sb-row sb-in' : 'sb-row'}>{children}</div>;
}

interface RowProps {
  s: SessionView;
  selected: boolean;
  menuOpen: boolean;
  onMenu: (anchor: HTMLElement) => void;
}

function WaitingRow({ s, selected, menuOpen, onMenu }: RowProps) {
  useTick();
  return (
    <SessionRow
      density="panel"
      state={s.state}
      isNew={isNew(s)}
      title={titleOf(s)}
      time={ago(s.since ?? s.updated)}
      text={s.last}
      exitCode={s.exit_code}
      meta={`${AGENTS[s.agent]?.short ?? s.agent} · ${s.project}`}
      selected={selected}
      menuOpen={menuOpen}
      onOpen={() => openSession(s.key)}
      onMenu={onMenu}
    />
  );
}

function WorkingRow({ s, selected, menuOpen, onMenu }: RowProps) {
  useTick();
  return (
    <SessionRow
      density="panel"
      state={s.state}
      title={titleOf(s)}
      time={timer(s.since ?? s.updated)}
      text={s.state === 'starting' ? undefined : s.last_line}
      selected={selected}
      menuOpen={menuOpen}
      onOpen={() => openSession(s.key)}
      onMenu={onMenu}
    />
  );
}

interface ProjectBlockProps {
  /** «+» и «⋯» у проекта; после перезагрузки их нет */
  actions: boolean;
  node: ProjectNode;
  /** «ещё N»: число из того же снимка, что и строки */
  more: number;
  rows: SessionView[];
  expanded: boolean;
  selected: string | undefined;
  above: Set<string>;
  menuOpen: boolean;
  onToggle: () => void;
  onMenu: (anchor: HTMLElement) => void;
}

function ProjectBlock({ actions, node, more, rows, expanded, selected, above, menuOpen, onToggle, onMenu }: ProjectBlockProps) {
  const p = node.project;
  return (
    <ProjectRow
      density="panel"
      name={p.name}
      pinned={p.pinned}
      expanded={expanded}
      marks={node.marks}
      menuOpen={menuOpen}
      onToggle={onToggle}
      onNew={actions ? () => openHome(p.path) : undefined}
      onMenu={actions ? onMenu : undefined}
      moreCount={more}
      onMore={() => navigate(`#/history?project=${encodeURIComponent(p.path)}`)}
    >
      {rows.map((s) => {
        const active = s.terminal || (s.state !== 'idle' && s.state !== 'exited');
        return (
          <SessionRow
            key={s.key}
            layout="line"
            nested
            density="panel"
            state={s.state}
            done={s.done}
            title={titleOf(s)}
            time={active ? undefined : when(s.updated)}
            agent={s.agent as AgentId}
            copy={!!s.worktree}
            selected={s.key === selected ? (above.has(s.key) ? 'echo' : true) : false}
            onOpen={() => openSession(s.key)}
          />
        );
      })}
    </ProjectRow>
  );
}

function ProjectMenu({ path, anchor, vscode, onClose }: { path: string; anchor: HTMLElement; vscode: boolean; onClose: () => void }) {
  const project = useApp((s) => s.projects.find((p) => p.path === path));
  const [check, setCheck] = useState<LaunchCheck | null>(null);
  useEffect(() => {
    const ctl = new AbortController();
    api
      .launchCheck(path, ctl.signal)
      .then(setCheck)
      .catch(() => undefined);
    return () => ctl.abort();
  }, [path]);
  if (!project) return null;
  return (
    <Menu
      open
      anchor={anchor}
      label={`Меню проекта ${project.name}`}
      placement="bottom-end"
      items={projectMenu(project, { vscode, check, lastAgent: lastAgentOf(project) })}
      onClose={onClose}
    />
  );
}

/** Агент последней сессии проекта */
export function lastAgentOf(p: ProjectView) {
  let best: SessionView | undefined;
  for (const s of Object.values(store.get().sessions)) {
    if (s.service || (s.repo ?? s.cwd) !== p.path) continue;
    if (!best || s.created > best.created) best = s;
  }
  return best?.agent;
}
