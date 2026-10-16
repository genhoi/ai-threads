// Кто пишет сводки: агенты, которые это умеют, и выбор «Авто».
import type { AgentView } from '../api/types';
import type { MenuEntry } from '../components';
import { AGENTS, type AgentId } from '../model';

/** Агенты, которые пишут сводки, в порядке выбора при «Авто» */
export const RUNNERS: AgentId[] = ['kimi', 'claude', 'codex', 'grok'];

/** Агент установлен и включён: его можно позвать писать сводку */
export function runnerReady(agents: AgentView[], id: string): boolean {
  return agents.some((a) => a.id === id && a.enabled && !!a.program);
}

/** Кто пишет сводку при «Авто»: первый установленный в порядке Kimi, Claude Code, Codex, Grok */
export function autoRunner(agents: AgentView[]): AgentId | undefined {
  return RUNNERS.find((id) => runnerReady(agents, id));
}

export function agentName(id: string): string {
  return AGENTS[id as AgentId]?.name ?? id;
}

/** Короткое имя агента: «Kimi», «Claude»; незнакомый — с большой буквы */
export function agentShort(id: string): string {
  return AGENTS[id as AgentId]?.short ?? (id ? id[0]!.toUpperCase() + id.slice(1) : '');
}

/** Меню «Кто пишет»: «Авто» и агенты; не установленные недоступны, кроме уже выбранного */
export function runnerMenu(current: string, agents: AgentView[], onSelect: (id: string) => void): MenuEntry[] {
  return [
    { id: 'auto', label: 'Авто', icon: 'history', checked: current === 'auto', onSelect: () => onSelect('auto') },
    { type: 'separator' },
    ...RUNNERS.map((id) => ({
      id,
      label: agentName(id),
      agent: id,
      checked: current === id,
      disabled: !runnerReady(agents, id) && current !== id,
      reason: 'не установлен',
      reasonInline: true,
      onSelect: () => onSelect(id),
    })),
  ];
}
