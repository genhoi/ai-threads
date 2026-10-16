import type { TargetedKeyboardEvent } from 'preact';
import { useId, useRef } from 'preact/hooks';
import type { IconName } from '../icons';
import { Icon } from './Icon';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  icon?: IconName;
  disabled?: boolean;
  /** Почему пункт недоступен: показывается под переключателем */
  reason?: string;
}

export interface SegmentedProps<T extends string> {
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange?: (value: T) => void;
  /** Подпись группы для экранного диктора */
  label: string;
  /** Строка под переключателем; без неё там причина недоступного пункта */
  hint?: string;
  /** default — на фоне; sunken — тёмный, на карточке */
  tone?: 'default' | 'sunken';
  /** lg — телефон: пункты 44 px на всю ширину */
  size?: 'md' | 'lg';
  class?: string;
  /** Состояния для галереи: пункт под курсором и пункт в фокусе */
  demoHover?: T;
  demoFocus?: T;
}

/** Переключатель вкладок: один из двух-трёх вариантов. Стрелки переключают. */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  label,
  hint,
  tone = 'default',
  size = 'md',
  class: cls,
  demoHover,
  demoFocus,
}: SegmentedProps<T>) {
  const id = useId();
  const trackRef = useRef<HTMLDivElement>(null);
  const reason = hint ?? options.find((o) => o.disabled && o.reason)?.reason;
  const hintId = reason ? `seg-${id}-hint` : undefined;

  const move = (from: number, step: number) => {
    for (let i = 1; i <= options.length; i++) {
      const next = (from + step * i + options.length) % options.length;
      const opt = options[next];
      if (opt && !opt.disabled) {
        onChange?.(opt.value);
        trackRef.current?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus();
        return;
      }
    }
  };

  const onKeyDown = (e: TargetedKeyboardEvent<HTMLDivElement>) => {
    const current = options.findIndex((o) => o.value === value);
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') move(current, 1);
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') move(current, -1);
    else return;
    e.preventDefault();
  };

  return (
    <div class={['seg', `seg--${tone}`, `seg--${size}`, cls].filter(Boolean).join(' ')}>
      <div class="seg__track" role="radiogroup" aria-label={label} aria-describedby={hintId} ref={trackRef} onKeyDown={onKeyDown}>
        {options.map((o) => {
          const selected = o.value === value;
          const classes = [
            'seg__opt',
            o.icon && 'seg__opt--icon',
            demoHover === o.value && 'is-hover',
            demoFocus === o.value && 'is-focus',
          ]
            .filter(Boolean)
            .join(' ');
          return (
            <button
              type="button"
              key={o.value}
              role="radio"
              class={classes}
              aria-checked={selected}
              aria-disabled={o.disabled || undefined}
              tabIndex={selected ? 0 : -1}
              onClick={() => !o.disabled && !selected && onChange?.(o.value)}
            >
              {o.icon && <Icon name={o.icon} size={16} />}
              {o.label}
            </button>
          );
        })}
      </div>
      {reason && (
        <span class="seg__hint" id={hintId}>
          {reason}
        </span>
      )}
    </div>
  );
}
