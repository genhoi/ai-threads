import { useEffect, useRef, useState } from 'preact/hooks';
import { STATE_LABELS, type SessionState } from '../model';
import { Icon } from './Icon';

export interface StatusMarkProps {
  state: SessionState;
  /** Ждёт и закончила ход после последнего просмотра: круг с ореолом */
  isNew?: boolean;
  /** sm — строки и плашки, md — телефон */
  size?: 'sm' | 'md';
  /** Рядом есть подпись состояния: диктору знак не читать */
  decorative?: boolean;
  class?: string;
}

const CROSSFADE_MS = 180; // --base

function Mark({ state, isNew, cls }: { state: SessionState; isNew?: boolean | undefined; cls?: string }) {
  if (state === 'exited') return <Icon name="check" size={14} class={`smark smark--exited ${cls ?? ''}`} />;
  const classes = ['smark', `smark--${state}`, state === 'waiting' && isNew && 'smark--new', (state === 'starting' || state === 'working') && 'spin', cls]
    .filter(Boolean)
    .join(' ');
  return <span class={classes} />;
}

/**
 * Знак состояния: пунктирное кольцо (запускается), кольцо с разрывом (работает), круг (ждёт),
 * круг с ореолом (ждёт, новое), ромб (ошибка), галочка (завершилась), пусто (в истории).
 * Смена состояния — перекрёстное затухание за --base.
 */
export function StatusMark({ state, isNew, size = 'sm', decorative, class: cls }: StatusMarkProps) {
  const [prev, setPrev] = useState<{ state: SessionState; isNew: boolean | undefined } | null>(null);
  const last = useRef({ state, isNew });

  useEffect(() => {
    const old = last.current;
    last.current = { state, isNew };
    if (old.state === state && !!old.isNew === !!isNew) return;
    setPrev(old);
    const t = window.setTimeout(() => setPrev(null), CROSSFADE_MS);
    return () => window.clearTimeout(t);
  }, [state, isNew]);

  const label = state === 'waiting' && isNew ? `${STATE_LABELS.waiting}, новое` : STATE_LABELS[state];
  return (
    <span
      class={['smark-slot', `smark-slot--${size}`, cls].filter(Boolean).join(' ')}
      role={decorative || state === 'idle' ? undefined : 'img'}
      aria-label={decorative || state === 'idle' ? undefined : label}
      aria-hidden={decorative || state === 'idle' || undefined}
    >
      {prev && <Mark state={prev.state} isNew={prev.isNew} cls="smark--leaving" />}
      <Mark key={`${state}-${!!isNew}`} state={state} isNew={isNew} cls={prev ? 'smark--entering' : undefined} />
    </span>
  );
}
