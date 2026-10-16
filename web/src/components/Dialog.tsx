import { createPortal } from 'preact';
import type { ComponentChildren, RefObject, TargetedKeyboardEvent } from 'preact';
import { useEffect, useId, useRef } from 'preact/hooks';
import type { IconName } from '../icons';
import { usePresence } from '../lib/presence';
import { Icon } from './Icon';

export interface DialogProps {
  open: boolean;
  /** Esc, щелчок по затемнению */
  onClose: () => void;
  title?: string;
  /** Строка под заголовком: «BACKEND-10300: лимиты запросов · backend» */
  subtitle?: string;
  children?: ComponentChildren;
  /** Кнопки справа внизу; основное действие последним */
  footer?: ComponentChildren;
  /** 480 по умолчанию */
  width?: number;
  /** Что получит фокус при открытии; иначе элемент с data-autofocus или первый доступный */
  initialFocus?: RefObject<HTMLElement | null>;
  /** Без полей, заголовка и подвала: окно поиска */
  bare?: boolean;
  /** Отступ сверху вместо центровки: окно поиска (104) */
  top?: number;
  /** Подпись для диктора, если нет заголовка */
  label?: string;
  closeOnOverlay?: boolean;
  /** В потоке страницы, без затемнения всего окна и ловушки фокуса: для галереи */
  inline?: boolean;
  class?: string;
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.offsetParent !== null || el === document.activeElement);
}

/**
 * Диалог: ширина 480, поля 32, затемнение под ним. Появление — прозрачность и scale(.98)→1 за --base,
 * затемнение — за 160 мс, закрытие — прозрачностью за --fast. Фокус заперт внутри, Esc закрывает.
 */
export function Dialog(props: DialogProps) {
  const { mounted, closing } = usePresence(props.open, props.inline ? 0 : 120);
  if (props.inline) return <DialogLayer {...props} closing={false} />;
  if (!mounted) return null;
  return createPortal(<DialogLayer {...props} closing={closing} />, document.body);
}

function DialogLayer({
  onClose,
  title,
  subtitle,
  children,
  footer,
  width = 480,
  initialFocus,
  bare,
  top,
  label,
  closeOnOverlay = true,
  inline,
  class: cls,
  closing,
}: DialogProps & { closing: boolean }) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const downOnOverlay = useRef(false);

  const before = useRef<HTMLElement | null>(null);
  const restoreFocus = () => {
    const el = before.current;
    before.current = null;
    if (el && document.contains(el)) el.focus({ preventScroll: true });
  };

  // Фокус внутрь сразу, не дожидаясь конца анимации
  useEffect(() => {
    if (inline) return;
    before.current = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel) {
      const target =
        initialFocus?.current ?? panel.querySelector<HTMLElement>('[data-autofocus]') ?? focusables(panel)[0] ?? panel;
      target.focus({ preventScroll: true });
    }
    return restoreFocus;
  }, [inline]);

  // Закрылся — фокус обратно сразу, пока окно ещё гаснет
  useEffect(() => {
    if (closing) restoreFocus();
  }, [closing]);

  const onKeyDown = (e: TargetedKeyboardEvent<HTMLDivElement>) => {
    if (inline) return;
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== 'Tab') return;
    const list = focusables(e.currentTarget);
    if (!list.length) {
      e.preventDefault();
      return;
    }
    const first = list[0]!;
    const last = list[list.length - 1]!;
    if (e.shiftKey && (document.activeElement === first || document.activeElement === e.currentTarget)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  const overlayClasses = ['dialog-overlay', inline && 'is-inline', closing && 'is-closing', top !== undefined && 'dialog-overlay--top']
    .filter(Boolean)
    .join(' ');
  const panelClasses = ['dialog', 'on-surface-1', bare && 'dialog--bare', cls].filter(Boolean).join(' ');

  return (
    <div
      class={overlayClasses}
      style={top !== undefined ? { paddingTop: `${top}px` } : undefined}
      onPointerDown={(e) => {
        downOnOverlay.current = e.target === e.currentTarget;
      }}
      onClick={(e) => {
        if (!inline && closeOnOverlay && downOnOverlay.current && e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={panelRef}
        class={panelClasses}
        style={{ width: `${width}px` }}
        role="dialog"
        aria-modal={inline ? undefined : true}
        aria-labelledby={title ? titleId : undefined}
        aria-label={title ? undefined : label}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        {!bare && (title || subtitle) && (
          <div class="dialog__head">
            {title && (
              <h2 class="dialog__title" id={titleId}>
                {title}
              </h2>
            )}
            {subtitle && <p class="dialog__subtitle">{subtitle}</p>}
          </div>
        )}
        {children}
        {!bare && footer && <div class="dialog__foot">{footer}</div>}
      </div>
    </div>
  );
}

export interface DialogPointProps {
  /** check — произойдёт, warning — не произойдёт или требует внимания */
  kind: 'ok' | 'warn';
  children: ComponentChildren;
  /** Пояснение строкой ниже, вторичным цветом */
  detail?: ComponentChildren;
}

/** Пункт списка «что произойдёт» в диалоге: галочка или предупреждение и текст */
export function DialogPoint({ kind, children, detail }: DialogPointProps) {
  const icon: IconName = kind === 'ok' ? 'check' : 'warning';
  return (
    <div class={`dialog-point dialog-point--${kind}`}>
      <Icon name={icon} size={16} class="dialog-point__icon" />
      {detail ? (
        <span class="dialog-point__text">
          <span>{children}</span>
          <span class="dialog-point__detail">{detail}</span>
        </span>
      ) : (
        <span>{children}</span>
      )}
    </div>
  );
}
