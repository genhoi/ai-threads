import type { JSX } from 'preact';
import { AGENTS, type AgentId } from '../model';

export type AgentBadgeSize = 16 | 18 | 24 | 28 | 32;

export interface AgentBadgeProps {
  agent: AgentId;
  /** 16 — строки дерева и истории, 18 — меню, 24 — чипы, 28 — списки и полный экран, 32 — шапка сессии и настройки */
  size?: AgentBadgeSize;
  /** Оставлено для совместимости: у иконок своя подложка, светлее она не бывает */
  raised?: boolean;
  disabled?: boolean;
  /** Рядом уже написано имя агента: диктору иконку не читать */
  decorative?: boolean;
  class?: string;
}

// Иконки агентов из первой версии «Нити»: скруглённый квадрат цвета агента и знак на нём.
const ICONS: Record<AgentId, JSX.Element> = {
  claude: (
    <>
      <rect width="24" height="24" rx="6" fill="#c4613a" />
      <path d="M12 5.2v13.6M6.1 8.6l11.8 6.8M6.1 15.4l11.8-6.8" stroke="#fff" stroke-width="2.2" stroke-linecap="round" fill="none" />
    </>
  ),
  codex: (
    <>
      <rect width="24" height="24" rx="6" fill="#3b4859" />
      <path d="M12 4.8l6.2 3.6v7.2L12 19.2l-6.2-3.6V8.4z" stroke="#fff" stroke-width="2" stroke-linejoin="round" fill="none" />
      <circle cx="12" cy="12" r="1.9" fill="#fff" />
    </>
  ),
  grok: (
    <>
      <rect x="0.5" y="0.5" width="23" height="23" rx="5.5" fill="#101114" stroke="#5a5e66" />
      <circle cx="12" cy="12" r="5.8" stroke="#fff" stroke-width="2" fill="none" />
      <path d="M6.6 17.4L17.4 6.6" stroke="#fff" stroke-width="2" stroke-linecap="round" />
    </>
  ),
  kimi: (
    <>
      <rect width="24" height="24" rx="6" fill="#2a6fd6" />
      <path d="M14.8 5.4a6.9 6.9 0 1 0 4 11.5 5.4 5.4 0 0 1-4-11.5z" fill="#fff" />
    </>
  ),
  cursor: (
    <>
      <rect width="24" height="24" rx="6" fill="#e8e6e1" />
      <path d="M8 5.5l9.2 7-4.3.8 2.4 4.4-2 1.1-2.4-4.4-2.9 3z" fill="#131417" />
    </>
  ),
  zcode: (
    <>
      <rect width="24" height="24" rx="6" fill="#86d8a8" />
      <path d="M7 7h10L7 17h10" stroke="#0d1f14" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" fill="none" />
    </>
  ),
};

/** Иконка агента: 16, 18, 24, 28 или 32 px */
export function AgentBadge({ agent, size = 24, disabled, decorative, class: cls }: AgentBadgeProps) {
  const info = AGENTS[agent];
  const classes = ['agent-badge', `agent-badge--${size}`, disabled && 'is-disabled', cls].filter(Boolean).join(' ');
  return (
    <span class={classes} role={decorative ? undefined : 'img'} aria-label={decorative ? undefined : info?.name ?? agent} aria-hidden={decorative || undefined} title={decorative ? undefined : info?.name}>
      {ICONS[agent] ? (
        <svg viewBox="0 0 24 24" width={size} height={size} aria-hidden="true">
          {ICONS[agent]}
        </svg>
      ) : (
        info?.code ?? agent.slice(0, 2)
      )}
    </span>
  );
}
