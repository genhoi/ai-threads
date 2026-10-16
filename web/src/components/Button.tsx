import type { ButtonHTMLAttributes, ComponentChildren, MouseEventHandler, Ref, TargetedMouseEvent } from 'preact';
import type { IconName } from '../icons';
import { Icon } from './Icon';
import { Tooltip } from './Tooltip';

export type ButtonVariant = 'primary' | 'regular' | 'quiet';
export type ButtonSize = 32 | 36 | 40 | 48;

type NativeButton = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'size' | 'icon' | 'disabled' | 'ref'>;

export interface ButtonProps extends NativeButton {
  /** primary — одна на экран, regular — с границей, quiet — без фона */
  variant?: ButtonVariant;
  /** 32 — в сообщениях и строках, 36 — внизу панели, 40 — по умолчанию, 48 — окно запуска и телефон */
  size?: ButtonSize;
  /** Иконка слева */
  icon?: IconName;
  /** Подсказка клавиш справа на кнопке: «Ctrl+Enter» */
  hint?: string;
  /** Идёт действие: иконка меняется на вращающийся progress_activity, нажатия не проходят */
  loading?: boolean;
  /** Недоступна. Кнопка остаётся в фокусе, чтобы была видна причина в подсказке */
  disabled?: boolean;
  /** Выбрана: активный пункт навигации, открытое меню. Только вид: aria-current или aria-expanded ставит экран */
  pressed?: boolean;
  /** Янтарный текст: действие в сообщении («Отменить», «Открыть») */
  accent?: boolean;
  /** На всю ширину */
  block?: boolean;
  /** Подсказка при наведении */
  tooltip?: ComponentChildren;
  /** Сочетание клавиш в подсказке */
  shortcut?: readonly string[];
  ref?: Ref<HTMLButtonElement>;
  children?: ComponentChildren;
}

function blockWhenInactive(inactive: boolean | undefined, handler: MouseEventHandler<HTMLButtonElement> | undefined) {
  return (e: TargetedMouseEvent<HTMLButtonElement>) => {
    if (inactive) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    handler?.(e);
  };
}

/** Кнопка с текстом: основная, обычная или тихая */
export function Button({
  variant = 'regular',
  size = 40,
  icon,
  hint,
  loading,
  disabled,
  pressed,
  accent,
  block,
  tooltip,
  shortcut,
  class: cls,
  type = 'button',
  onClick,
  children,
  ref,
  ...rest
}: ButtonProps) {
  const lead = loading ? 'progress_activity' : icon;
  const classes = [
    'btn',
    `btn--${variant}`,
    `btn--${size}`,
    lead && 'btn--lead',
    hint && 'btn--hint',
    loading && 'is-loading',
    pressed && 'is-pressed',
    accent && 'btn--accent',
    block && 'btn--block',
    cls,
  ]
    .filter(Boolean)
    .join(' ');

  const button = (
    <button
      {...rest}
      ref={ref}
      type={type}
      class={classes}
      aria-disabled={disabled || undefined}
      aria-busy={loading || undefined}
      aria-keyshortcuts={shortcut?.join('+')}
      onClick={blockWhenInactive(disabled || loading, onClick)}
    >
      {lead && <Icon name={lead} size={size === 32 ? 16 : 18} spin={loading} />}
      {children}
      {hint && <span class="btn__hint">{hint}</span>}
    </button>
  );

  return tooltip || shortcut ? (
    <Tooltip content={tooltip} shortcut={shortcut}>
      {button}
    </Tooltip>
  ) : (
    button
  );
}

export type IconButtonSize = 24 | 28 | 32 | 36 | 40 | 48;

export interface IconButtonProps extends Omit<NativeButton, 'label'> {
  icon: IconName;
  /** Подпись: для экранного диктора и в подсказке */
  label: string;
  /** Сочетание клавиш в подсказке: ['Ctrl', 'K'] */
  shortcut?: readonly string[];
  /** 40 по умолчанию; 28 — в строках, 32 — копирование, 48 — телефон */
  size?: IconButtonSize;
  /** primary — янтарный круг (телефон: новая сессия, отправить) */
  variant?: 'quiet' | 'primary';
  loading?: boolean;
  disabled?: boolean;
  /** Открыто меню этой кнопки или выбран пункт. Только вид: aria-expanded ставит экран */
  pressed?: boolean;
  /** Без подсказки при наведении (подпись остаётся для диктора) */
  noTooltip?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

const ICON_SIZE: Record<IconButtonSize, 14 | 16 | 18> = { 24: 14, 28: 16, 32: 16, 36: 18, 40: 18, 48: 18 };

/** Кнопка-иконка: круглая, подпись в подсказке */
export function IconButton({
  icon,
  label,
  shortcut,
  size = 40,
  variant = 'quiet',
  loading,
  disabled,
  pressed,
  noTooltip,
  class: cls,
  type = 'button',
  onClick,
  ref,
  ...rest
}: IconButtonProps) {
  const classes = [
    'btn',
    'btn--icon',
    `btn--icon-${size}`,
    variant === 'primary' && 'btn--primary',
    loading && 'is-loading',
    pressed && 'is-pressed',
    cls,
  ]
    .filter(Boolean)
    .join(' ');

  const button = (
    <button
      {...rest}
      ref={ref}
      type={type}
      class={classes}
      aria-label={label}
      aria-disabled={disabled || undefined}
      aria-busy={loading || undefined}
      aria-keyshortcuts={shortcut?.join('+')}
      onClick={blockWhenInactive(disabled || loading, onClick)}
    >
      <Icon name={loading ? 'progress_activity' : icon} size={ICON_SIZE[size]} spin={loading} />
    </button>
  );

  return noTooltip ? (
    button
  ) : (
    <Tooltip content={label} shortcut={shortcut}>
      {button}
    </Tooltip>
  );
}
