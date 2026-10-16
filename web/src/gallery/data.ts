// Данные для галереи: пункты меню и текст терминала из макетов
import type { MenuEntry } from '../components';
import { AGENTS, type AgentId } from '../model';

export const SESSION_MENU: MenuEntry[] = [
  { id: 'info', label: 'Сведения о сессии', icon: 'info' },
  { id: 'rename', label: 'Переименовать', icon: 'edit' },
  { id: 'pin', label: 'Закрепить', icon: 'push_pin' },
  { id: 'hide', label: 'Скрыть', icon: 'visibility_off' },
  { type: 'separator' },
  { id: 'vscode', label: 'Открыть в VS Code', icon: 'open_in_new', disabled: true, reason: 'VS Code не найден' },
  { id: 'folder', label: 'Открыть папку', icon: 'folder_open' },
  { id: 'command', label: 'Команда для терминала', icon: 'content_copy' },
  { type: 'separator' },
  { id: 'next', label: 'Следующая ждущая', icon: 'keyboard_arrow_down', shortcut: ['Alt', '↓'] },
  { id: 'finish', label: 'Завершить…', icon: 'task_alt' },
];

export function agentMenu(current: AgentId | undefined, onPick?: (id: AgentId) => void): MenuEntry[] {
  const order: AgentId[] = ['claude', 'codex', 'grok', 'cursor'];
  return [
    ...order.map((id) => ({
      id,
      label: AGENTS[id].name,
      agent: id,
      checked: current === id,
      onSelect: () => onPick?.(id),
    })),
    { id: 'zcode', label: AGENTS.zcode.name, agent: 'zcode', disabled: true, reason: 'только в своём приложении', reasonInline: true },
  ];
}

export const PROJECT_MENU: MenuEntry[] = [
  { id: 'path', label: '~/projects/backend', mono: true, trailingIcon: 'content_copy' },
  { type: 'separator' },
  { id: 'new', label: 'Новая сессия здесь', icon: 'add' },
  { id: 'vscode', label: 'Открыть в VS Code', icon: 'open_in_new' },
  { id: 'folder', label: 'Открыть папку', icon: 'folder_open' },
  {
    id: 'copies',
    label: 'Копии проекта',
    icon: 'fork_right',
    value: '2',
    submenu: [
      { id: 'c1', label: 'BACKEND-10300', icon: 'fork_right' },
      { id: 'c2', label: 'BACKEND-10312', icon: 'fork_right' },
    ],
  },
  { id: 'agent', label: 'Агент по умолчанию', agent: 'claude', value: 'Claude', submenu: agentMenu('claude') },
  { type: 'separator' },
  { id: 'pin', label: 'Закрепить', icon: 'push_pin' },
  { id: 'hide', label: 'Скрыть', icon: 'visibility_off' },
];

export interface DemoProject {
  name: string;
  path: string;
  agent: string;
}

export const PROJECTS: DemoProject[] = [
  { name: 'backend', path: '~/projects/backend', agent: 'Claude' },
  { name: 'shop-api', path: '~/projects/shop-api', agent: 'Codex' },
  { name: 'landing', path: '~/projects/landing', agent: 'Grok' },
  { name: 'mobile-app', path: '~/projects/mobile-app', agent: 'Cursor' },
  { name: 'infra', path: '~/work/infra', agent: 'Claude' },
];

export function pickerMenu(query: string, current: string, onPick: (name: string) => void): MenuEntry[] {
  const q = query.trim().toLowerCase();
  const found = PROJECTS.filter((p) => !q || p.name.includes(q) || p.path.includes(q));
  return [
    { type: 'label', label: q ? 'Найдено' : 'Недавние' },
    ...found.map((p) => ({
      id: p.name,
      label: p.name,
      icon: 'folder' as const,
      description: p.path,
      checked: p.name === current ? true : undefined,
      hint: p.name === current ? undefined : p.agent,
      onSelect: () => onPick(p.name),
    })),
    { type: 'separator' },
    { id: 'other', label: 'Другая папка…', icon: 'folder_open', height: 40 },
  ];
}

// Вывод агента для терминала: цвета ANSI как в макете (тусклый, зелёный, красный, синий)
const DIM = '\x1b[90m';
const GREEN = '\x1b[32m';
const RED = '\x1b[31m';
const BLUE = '\x1b[34m';
const BOLD = '\x1b[1m';
const RESET = '\x1b[0m';
const SELECT = '\x1b[48;2;74;64;51m';

export const TERMINAL_DEMO = [
  `${DIM}> ${RESET}BACKEND-10300: добавь лимиты запросов для ключей API. Лимит храни в api_keys, по умолчанию 600 в минуту.`,
  '',
  '● Посмотрю, как сейчас устроены ключи.',
  `${DIM}  └ ${RESET}${BOLD}Read${RESET} internal/auth/apikey.go ${DIM}(148 строк)${RESET}`,
  `${DIM}  └ ${RESET}${BOLD}Write${RESET} internal/http/ratelimit.go ${GREEN}+84${RESET}`,
  `${DIM}  └ ${RESET}${BOLD}Edit${RESET}  internal/http/router.go ${GREEN}+3${RESET} ${RED}−1${RESET}`,
  '',
  `${DIM}$ ${RESET}make migrate DB=local`,
  `Применяю ${SELECT}0058_api_keys_rate_limit.sql${RESET} … ${GREEN}ok${RESET}`,
  '',
  `${BLUE}❯ 1. Да${RESET}`,
  '  2. Да, и не спрашивать про make в этом проекте',
  `  3. Нет, сказать, что сделать иначе ${DIM}(esc)${RESET}`,
  '',
  `${DIM}$ ${RESET}`,
].join('\r\n');
