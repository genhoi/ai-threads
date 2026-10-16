import type { ButtonHTMLAttributes, Ref } from 'preact';
import type { IconName } from '../icons';
import type { AgentId } from '../model';
import { AGENTS } from '../model';
import { AgentBadge } from './AgentBadge';
import { IconButton } from './Button';
import { Icon } from './Icon';

type NativeButton = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'label' | 'value' | 'icon' | 'disabled' | 'ref'>;

export interface ChipProps extends NativeButton {
  /** Что выбирают: «Модель», «Проект». Без подписи значение светлее */
  label?: string;
  /** Выбранное значение: «как в CLI», «backend» */
  value?: string;
  /** Иконка слева: folder */
  icon?: IconName;
  /** Иконка агента слева, значение по умолчанию — имя агента */
  agent?: AgentId;
  /** Список открыт: стрелка повёрнута и янтарная */
  open?: boolean;
  disabled?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

/** Чип выбора значения. Открывает список (Menu); стрелка поворачивается за --fast. */
export function Chip({ label, value, icon, agent, open, disabled, class: cls, onClick, ref, ...rest }: ChipProps) {
  const shown = value ?? (agent ? AGENTS[agent].name : '');
  const classes = [
    'chip',
    icon && 'chip--icon',
    agent && 'chip--agent',
    !label && 'chip--plain',
    open && 'is-open',
    cls,
  ]
    .filter(Boolean)
    .join(' ');
  return (
    <button
      {...rest}
      ref={ref}
      type="button"
      class={classes}
      aria-haspopup="menu"
      aria-expanded={disabled ? undefined : !!open}
      aria-disabled={disabled || undefined}
      onClick={(e) => (disabled ? e.preventDefault() : onClick?.(e))}
    >
      {icon && <Icon name={icon} size={18} class="chip__icon" />}
      {agent && <AgentBadge agent={agent} size={24} decorative disabled={disabled} />}
      {label}
      {shown && <span class="chip__value">{shown}</span>}
      {!disabled && <Icon name="expand_more" size={16} class="chip__chevron" />}
    </button>
  );
}

export interface FilterChipProps extends Omit<NativeButton, 'onChange'> {
  label: string;
  on: boolean;
  onChange?: (on: boolean) => void;
  disabled?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

/** Фильтр: включается и выключается («Служебные», «В переписке») */
export function FilterChip({ label, on, onChange, disabled, class: cls, ref, ...rest }: FilterChipProps) {
  return (
    <button
      {...rest}
      ref={ref}
      type="button"
      class={['fchip', on && 'is-on', cls].filter(Boolean).join(' ')}
      aria-pressed={on}
      aria-disabled={disabled || undefined}
      onClick={() => !disabled && onChange?.(!on)}
    >
      {on && <Icon name="check" size={16} />}
      {label}
    </button>
  );
}

export interface TagChipProps {
  /** Команда или папка: «codex exec», «/tmp» */
  label: string;
  onRemove?: () => void;
  /** Подпись кнопки удаления */
  removeLabel?: string;
  class?: string;
}

/** Значение списка с кнопкой «Убрать» (настройки служебных сессий) */
export function TagChip({ label, onRemove, removeLabel = 'Убрать', class: cls }: TagChipProps) {
  return (
    <span class={`tag${cls ? ' ' + cls : ''}`}>
      {label}
      <IconButton icon="close" label={`${removeLabel} ${label}`} size={24} class="tag__remove" onClick={onRemove} />
    </span>
  );
}

export interface ProjectChipProps extends NativeButton {
  name: string;
  /** Когда работали: «сейчас», «26 мин», «вчера» */
  when?: string;
  ref?: Ref<HTMLButtonElement>;
}

/** Недавний проект на главной: контурная капсула с папкой */
export function ProjectChip({ name, when, class: cls, ref, ...rest }: ProjectChipProps) {
  return (
    <button {...rest} ref={ref} type="button" class={`pchip${cls ? ' ' + cls : ''}`}>
      <Icon name="folder" size={18} class="pchip__icon" />
      {name}
      {when && <span class="pchip__when">{when}</span>}
    </button>
  );
}
