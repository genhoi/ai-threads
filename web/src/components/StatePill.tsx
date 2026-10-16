import type { SessionState } from '../model';
import { STATE_LABELS } from '../model';
import { StatusMark } from './StatusMark';

export interface StatePillProps {
  state: SessionState;
  isNew?: boolean;
  /** Сколько работает или ждёт: «7:12», «2 мин»; у «в истории» — когда: «вчера, 18:40» */
  elapsed?: string;
  /** Код выхода для ошибки */
  exitCode?: number;
  /** Своя подпись вместо собранной из состояния */
  label?: string;
  class?: string;
}

function pillLabel({ state, elapsed, exitCode, label }: StatePillProps): string {
  if (label) return label;
  if (state === 'error') return exitCode === undefined ? STATE_LABELS.error : `ошибка · код ${exitCode}`;
  if (elapsed && (state === 'working' || state === 'waiting' || state === 'idle')) return `${STATE_LABELS[state]} · ${elapsed}`;
  return STATE_LABELS[state];
}

/**
 * Плашка состояния в шапке сессии: «работает · 7:12», «ждёт вас», «ошибка · код 1».
 * «В истории» — просто подпись без плашки. Смена цвета — за --base.
 */
export function StatePill(props: StatePillProps) {
  const { state, isNew, class: cls } = props;
  const text = pillLabel(props);
  if (state === 'idle') return <span class={`state-text${cls ? ' ' + cls : ''}`}>{text}</span>;
  return (
    <span class={`state-pill state-pill--${state}${cls ? ' ' + cls : ''}`}>
      <StatusMark state={state} isNew={isNew} decorative />
      {text}
    </span>
  );
}
