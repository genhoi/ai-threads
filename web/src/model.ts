// Общие типы интерфейса. Совпадают с internal/model на Go.

export type AgentId = 'claude' | 'codex' | 'grok' | 'kimi' | 'cursor' | 'zcode';

export interface AgentInfo {
  /** Две буквы в кружке вместо иконки агента */
  code: string;
  /** Полное название: в чипе, меню, подсказке */
  name: string;
  /** Короткое: в строках «Codex · shop-api» */
  short: string;
}

export const AGENTS: Record<AgentId, AgentInfo> = {
  claude: { code: 'Cl', name: 'Claude Code', short: 'Claude' },
  codex: { code: 'Cx', name: 'Codex', short: 'Codex' },
  grok: { code: 'Gr', name: 'Grok', short: 'Grok' },
  kimi: { code: 'Ki', name: 'Kimi', short: 'Kimi' },
  cursor: { code: 'Cu', name: 'Cursor Agent', short: 'Cursor' },
  zcode: { code: 'ZC', name: 'ZCode', short: 'ZCode' },
};

/** Состояние сессии, как в internal/model.State */
export type SessionState = 'starting' | 'working' | 'waiting' | 'error' | 'exited' | 'idle';

/** Подписи состояний для экранного диктора и плашек */
export const STATE_LABELS: Record<SessionState, string> = {
  starting: 'запускается',
  working: 'работает',
  waiting: 'ждёт вас',
  error: 'ошибка',
  exited: 'завершилась',
  idle: 'в истории',
};
