import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import type { IconName } from '../icons';
import { Button, IconButton } from './Button';
import { Icon } from './Icon';
import { StatusMark } from './StatusMark';

export type ToastKind = 'info' | 'waiting' | 'error';

export interface ToastOptions {
  /** info — иконка и текст; waiting — новая ждущая сессия; error — ромб и подробность моноширинным */
  kind?: ToastKind;
  /** Иконка у info: task_alt по умолчанию */
  icon?: IconName;
  title: string;
  /** Вторая строка: последняя фраза агента или текст ошибки */
  text?: string;
  /** Действие справа: «Отменить», «Открыть» */
  action?: { label: string; onClick: () => void };
  /** Кнопка «Закрыть»; по умолчанию есть у info и error */
  closable?: boolean;
  /** Сколько показывать, мс; 6 секунд по умолчанию */
  duration?: number;
}

export interface ToastBoxProps extends ToastOptions {
  onClose?: () => void;
  class?: string;
}

/** Само сообщение, без таймера. Им же рисуются состояния в галерее. */
export function ToastBox({ kind = 'info', icon = 'task_alt', title, text, action, closable, onClose, class: cls }: ToastBoxProps) {
  const showClose = closable ?? kind !== 'waiting';
  let lead: ComponentChildren;
  if (kind === 'waiting') lead = <StatusMark state="waiting" isNew decorative class="toast__mark" />;
  else if (kind === 'error') lead = <StatusMark state="error" decorative class="toast__mark" />;
  else lead = <Icon name={icon} size={18} class="toast__icon" />;

  return (
    <div class={`toast toast--${kind} on-surface-2${cls ? ' ' + cls : ''}`} role={kind === 'error' ? 'alert' : 'status'}>
      {lead}
      {text ? (
        <div class="toast__body">
          <span class="toast__title">{title}</span>
          <span class="toast__text">{text}</span>
        </div>
      ) : (
        <span class="toast__title toast__title--single">{title}</span>
      )}
      {action && (
        <Button
          variant="quiet"
          size={32}
          accent
          onClick={() => {
            action.onClick();
            onClose?.();
          }}
        >
          {action.label}
        </Button>
      )}
      {showClose && <IconButton icon="close" label="Закрыть" size={32} noTooltip onClick={onClose} />}
    </div>
  );
}

// Очередь сообщений: показать можно из любого места, рисует их ToastHost
interface ToastItem extends ToastOptions {
  id: number;
}

let items: ToastItem[] = [];
let nextId = 1;
const listeners = new Set<(list: ToastItem[]) => void>();

function emit() {
  for (const l of listeners) l(items);
}

export const toast = {
  /** Показать сообщение; вернёт id для dismiss */
  show(options: ToastOptions): number {
    const id = nextId++;
    items = [...items, { ...options, id }];
    emit();
    return id;
  },
  dismiss(id: number) {
    items = items.filter((t) => t.id !== id);
    emit();
  },
};

const EXIT_MS = 160;

/** Сообщения внизу справа. Через 6 с уходят; пока курсор над сообщением, таймер стоит. */
export function ToastHost() {
  const [list, setList] = useState(items);
  useEffect(() => {
    listeners.add(setList);
    return () => {
      listeners.delete(setList);
    };
  }, []);
  return (
    <div class="toast-host" aria-live="polite">
      {list.map((t) => (
        <TimedToast key={t.id} item={t} />
      ))}
    </div>
  );
}

function TimedToast({ item }: { item: ToastItem }) {
  const [leaving, setLeaving] = useState(false);
  const left = useRef(item.duration ?? 6000);
  const started = useRef(0);
  const timer = useRef(0);

  const leave = () => {
    window.clearTimeout(timer.current);
    setLeaving(true);
    window.setTimeout(() => toast.dismiss(item.id), EXIT_MS);
  };
  const run = () => {
    started.current = performance.now();
    timer.current = window.setTimeout(leave, left.current);
  };
  const pause = () => {
    window.clearTimeout(timer.current);
    left.current = Math.max(1000, left.current - (performance.now() - started.current));
  };

  useEffect(() => {
    run();
    return () => window.clearTimeout(timer.current);
  }, []);

  return (
    <div
      class={`toast-slot${leaving ? ' is-leaving' : ''}`}
      onPointerEnter={pause}
      onPointerLeave={() => !leaving && run()}
      onFocusIn={pause}
      onFocusOut={(e) => {
        if (!leaving && !e.currentTarget.contains(e.relatedTarget as Node | null)) run();
      }}
    >
      <ToastBox {...item} onClose={leave} />
    </div>
  );
}
