import type { ComponentChildren, Ref } from 'preact';
import { Icon } from './Icon';

export interface SwitchProps {
  checked: boolean;
  onChange?: (checked: boolean) => void;
  /** Подпись: видимая слева от переключателя или только для диктора */
  label: string;
  showLabel?: boolean;
  disabled?: boolean;
  class?: string;
  ref?: Ref<HTMLButtonElement>;
}

/** Переключатель 40×24 из настроек: «Показывать в списке» */
export function Switch({ checked, onChange, label, showLabel, disabled, class: cls, ref }: SwitchProps) {
  const button = (
    <button
      ref={ref}
      type="button"
      role="switch"
      class={['switch', checked && 'is-on', !showLabel && cls].filter(Boolean).join(' ')}
      aria-checked={checked}
      aria-label={showLabel ? undefined : label}
      aria-disabled={disabled || undefined}
      onClick={() => !disabled && onChange?.(!checked)}
    >
      <span class="switch__knob" />
    </button>
  );
  if (!showLabel) return button;
  return (
    <label class={['switch-field', disabled && 'is-disabled', cls].filter(Boolean).join(' ')}>
      <span class="switch-field__label">{label}</span>
      {button}
    </label>
  );
}

export interface CheckboxProps {
  checked: boolean;
  onChange?: (checked: boolean) => void;
  /** Подпись справа; без неё нужен ariaLabel */
  label?: ComponentChildren;
  /** Пояснение под подписью */
  description?: ComponentChildren;
  ariaLabel?: string;
  disabled?: boolean;
  class?: string;
  ref?: Ref<HTMLInputElement>;
}

/** Флажок 20×20 из настроек и экрана перезагрузки */
export function Checkbox({ checked, onChange, label, description, ariaLabel, disabled, class: cls, ref }: CheckboxProps) {
  return (
    <label class={['check', label != null && 'check--text', disabled && 'is-disabled', cls].filter(Boolean).join(' ')}>
      <input
        ref={ref}
        type="checkbox"
        class="check__input"
        checked={checked}
        disabled={disabled}
        aria-label={ariaLabel}
        onChange={(e) => onChange?.(e.currentTarget.checked)}
      />
      <span class="check__box" aria-hidden="true">
        {checked && <Icon name="check" size={14} />}
      </span>
      {label != null && (
        <span class="check__text">
          <span class="check__label">{label}</span>
          {description && <span class="check__desc">{description}</span>}
        </span>
      )}
    </label>
  );
}
