import type { CSSProperties } from 'preact';
import type { IconName } from '../icons';

export type IconSize = 14 | 16 | 18;

export interface IconProps {
  name: IconName;
  /** 18 — кнопки и панель, 16 — чипы, отметки, меню, 14 — внутри строк */
  size?: IconSize;
  /** Залитый вариант (ось FILL) */
  filled?: boolean;
  /** Вращается: для progress_activity */
  spin?: boolean;
  class?: string;
  style?: CSSProperties;
  /** Подпись для экранного диктора; без неё иконка скрыта от него */
  label?: string;
}

/** Иконка Material Symbols. Толщина по размеру: 18 → wght 300, 16 → 350, 14 → 400. */
export function Icon({ name, size = 18, filled, spin, class: cls, style, label }: IconProps) {
  const classes = ['icon', `icon--${size}`, filled && 'icon--filled', spin && 'spin', cls].filter(Boolean).join(' ');
  return (
    <span class={classes} style={style} aria-hidden={label ? undefined : true} role={label ? 'img' : undefined} aria-label={label}>
      {name}
    </span>
  );
}
