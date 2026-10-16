// Пункты меню «⋯» сессии и проекта
import type { LaunchCheck, ProjectView, SessionView } from '../api/types';
import type { MenuEntry } from '../components';
import { AGENTS, type AgentId } from '../model';
import {
  copyCommand,
  copyText,
  goNextWaiting,
  openHome,
  openSession,
  reveal,
  setDone,
  setHidden,
  setPinned,
  setProjectAgent,
  setProjectHidden,
  setProjectPinned,
  startFinish,
} from './actions';
import { store } from './state';
import { tildify } from '../lib/format';

interface SessionMenuOptions {
  /** Меню в шапке открытой сессии; в строке панели — без «Следующей ждущей» */
  inHeader: boolean;
  vscode: boolean;
  /** Есть другая ждущая */
  hasNext: boolean;
  /** Открыть сведения или начать правку названия у открытой сессии */
  onDetails: () => void;
  onRename: () => void;
}

export function sessionMenu(s: SessionView, o: SessionMenuOptions): MenuEntry[] {
  const items: MenuEntry[] = [
    { id: 'info', label: 'Сведения о сессии', icon: 'info', onSelect: o.onDetails },
    { id: 'rename', label: 'Переименовать', icon: 'edit', onSelect: o.onRename },
    { id: 'pin', label: s.pinned ? 'Открепить' : 'Закрепить', icon: 'push_pin', onSelect: () => void setPinned(s.key, !s.pinned) },
    { id: 'hide', label: s.hidden ? 'Показать' : 'Скрыть', icon: 'visibility_off', onSelect: () => void setHidden(s.key, !s.hidden) },
    { type: 'separator' },
    {
      id: 'vscode',
      label: 'Открыть в VS Code',
      icon: 'open_in_new',
      disabled: !o.vscode || !!s.missing,
      reason: s.missing ? 'Папки сессии нет' : 'VS Code не найден',
      onSelect: () => void reveal({ key: s.key }, 'vscode'),
    },
    {
      id: 'folder',
      label: 'Открыть папку',
      icon: 'folder_open',
      disabled: !!s.missing,
      reason: 'Папки сессии нет',
      onSelect: () => void reveal({ key: s.key }, 'folder'),
    },
    { id: 'command', label: 'Команда для терминала', icon: 'content_copy', onSelect: () => void copyCommand(s.key) },
    { type: 'separator' },
  ];
  if (o.inHeader)
    items.push({
      id: 'next',
      label: 'Следующая ждущая',
      icon: 'keyboard_arrow_down',
      shortcut: ['Alt', '↓'],
      disabled: !o.hasNext,
      onSelect: goNextWaiting,
    });
  if (s.done) items.push({ id: 'undone', label: 'Вернуть из завершённых', icon: 'task_alt', onSelect: () => void setDone(s.key, false) });
  else items.push({ id: 'finish', label: 'Завершить…', icon: 'task_alt', onSelect: () => startFinish(s.key) });
  return items;
}

/** Меню сессии в строке панели: сведения и правка названия — в открытой сессии */
export function rowMenu(s: SessionView, vscode: boolean): MenuEntry[] {
  return sessionMenu(s, {
    inHeader: false,
    vscode,
    hasNext: false,
    onDetails: () => {
      openSession(s.key);
      store.set({ detailsOpen: true });
    },
    onRename: () => {
      openSession(s.key);
      store.set({ renaming: true });
    },
  });
}

const ORDER: AgentId[] = ['claude', 'codex', 'grok', 'kimi', 'cursor'];

export function projectMenu(p: ProjectView, o: { vscode: boolean; check: LaunchCheck | null; lastAgent?: AgentId }): MenuEntry[] {
  const { agents } = store.get();
  const installed = ORDER.filter((id) => agents.some((a) => a.id === id && a.enabled && a.program));
  const current = p.default_agent;
  const agentItems: MenuEntry[] = installed.map((id) => ({
    id: `agent-${id}`,
    label: AGENTS[id].name,
    agent: id,
    checked: current === id,
    onSelect: () => void setProjectAgent(p.path, id),
  }));
  if (agents.some((a) => a.id === 'zcode' && a.enabled))
    agentItems.push({
      id: 'agent-zcode',
      label: AGENTS.zcode.name,
      agent: 'zcode',
      disabled: true,
      reason: 'только в своём приложении',
      reasonInline: true,
    });
  if (current) agentItems.push({ type: 'separator' }, { id: 'agent-none', label: 'Последний запущенный', icon: 'history', onSelect: () => void setProjectAgent(p.path, '') });

  const copies = o.check?.worktrees ?? [];
  const items: MenuEntry[] = [
    { id: 'path', label: tildify(p.path), mono: true, trailingIcon: 'content_copy', onSelect: () => void copyText(p.path, 'Путь скопирован') },
    { type: 'separator' },
    { id: 'new', label: 'Новая сессия здесь', icon: 'add', onSelect: () => openHome(p.path) },
    {
      id: 'vscode',
      label: 'Открыть в VS Code',
      icon: 'open_in_new',
      disabled: !o.vscode,
      reason: 'VS Code не найден',
      onSelect: () => void reveal({ path: p.path }, 'vscode'),
    },
    { id: 'folder', label: 'Открыть папку', icon: 'folder_open', onSelect: () => void reveal({ path: p.path }, 'folder') },
  ];
  if (copies.length)
    items.push({
      id: 'copies',
      label: 'Копии проекта',
      icon: 'fork_right',
      value: String(copies.length),
      submenuWidth: 260,
      submenu: copies.map((w) => ({
        id: `copy-${w.path}`,
        label: w.branch,
        icon: 'fork_right' as const,
        // Новая сессия в этой копии
        onSelect: () => openHome(w.path),
      })),
    });
  const shownAgent = current ?? o.lastAgent;
  items.push(
    {
      id: 'agent',
      label: 'Агент по умолчанию',
      ...(shownAgent ? { agent: shownAgent } : { icon: 'history' as const }),
      value: current ? AGENTS[current].short : 'последний',
      submenu: agentItems,
    },
    { type: 'separator' },
    { id: 'pin', label: p.pinned ? 'Открепить' : 'Закрепить', icon: 'push_pin', onSelect: () => void setProjectPinned(p.path, !p.pinned) },
    { id: 'hide', label: 'Скрыть', icon: 'visibility_off', onSelect: () => void setProjectHidden(p.path, true) },
  );
  return items;
}
