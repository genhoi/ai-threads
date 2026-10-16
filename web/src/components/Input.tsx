import type { AccessibleInputHTMLAttributes, ComponentChildren, InputHTMLAttributes, Ref, TargetedPointerEvent } from 'preact';
import { useId } from 'preact/hooks';
import type { IconName } from '../icons';
import { Icon } from './Icon';

type NativeInput = Omit<InputHTMLAttributes<HTMLInputElement>, 'size' | 'prefix' | 'ref' | 'label' | 'icon' | 'type'>;

export interface InputProps extends NativeInput {
  type?: 'text' | 'search' | 'url' | 'email' | 'tel' | 'password' | 'number';
  /** Подпись для экранного диктора. Видимую подпись экран ставит сам через <label for={id}> */
  label?: string;
  /** Иконка слева: folder, search */
  icon?: IconName;
  /** Моноширинный текст: пути, ветки, команды */
  mono?: boolean;
  /** Подсказка в поле тоже моноширинная (по умолчанию — шрифтом интерфейса) */
  monoPlaceholder?: boolean;
  /** Текст ошибки под полем; поле краснеет */
  error?: string;
  /** default — на фоне; sunken — тёмное поле на карточке; outline — только граница */
  tone?: 'default' | 'sunken' | 'outline';
  /** 40 — компьютер, 48 — телефон */
  size?: 40 | 48;
  /** Подпись внутри поля перед значением: «ветка» */
  prefix?: string;
  /** Текст справа в поле: «найдена в PATH» */
  suffix?: ComponentChildren;
  /** Кнопка справа в поле: скопировать */
  action?: ComponentChildren;
  disabled?: boolean;
  /** Класс обёртки */
  class?: string;
  ref?: Ref<HTMLInputElement>;
}

/** Поле ввода. Высота 40, капсула, фокус — янтарная граница с ореолом. */
export function Input({
  label,
  icon,
  mono,
  monoPlaceholder,
  error,
  tone = 'default',
  size = 40,
  prefix,
  suffix,
  action,
  disabled,
  class: cls,
  id,
  type = 'text',
  ref,
  ...rest
}: InputProps) {
  const autoId = useId();
  const inputId = (id as string | undefined) ?? `in-${autoId}`;
  const errorId = error ? `${inputId}-error` : undefined;
  const classes = [
    'field',
    `field--${tone}`,
    size === 48 && 'field--48',
    mono && 'field--mono',
    monoPlaceholder && 'field--mono-ph',
    icon && 'field--icon',
    action && 'field--action',
    error && 'is-error',
    disabled && 'is-disabled',
    cls,
  ]
    .filter(Boolean)
    .join(' ');

  // Типы preact для <input> различают роли по type; собранные свойства приводятся к ним
  const inputAttrs = {
    ...rest,
    ref,
    id: inputId,
    type,
    class: 'field__input',
    disabled,
    'aria-invalid': error ? true : undefined,
    'aria-describedby': errorId,
    spellcheck: mono ? false : rest.spellcheck,
    autocomplete: rest.autocomplete ?? 'off',
  } as unknown as AccessibleInputHTMLAttributes<HTMLInputElement>;

  // Щелчок по полю мимо текста ставит курсор в поле
  const focusInput = (e: TargetedPointerEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement;
    if (target.closest('button, a, input')) return;
    e.preventDefault();
    e.currentTarget.querySelector('input')?.focus();
  };

  return (
    <div class={classes}>
      {label && (
        <label class="sr-only" for={inputId}>
          {label}
        </label>
      )}
      <div class="field__box" onPointerDown={focusInput}>
        {icon && <Icon name={icon} size={18} class="field__icon" />}
        {prefix && <span class="field__prefix">{prefix}</span>}
        <input {...inputAttrs} />
        {suffix && <span class="field__suffix">{suffix}</span>}
        {action}
      </div>
      {error && (
        <span class="field__error" id={errorId}>
          {error}
        </span>
      )}
    </div>
  );
}
