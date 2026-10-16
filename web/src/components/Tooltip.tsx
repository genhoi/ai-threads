import { createPortal } from 'preact';
import type { ComponentChildren } from 'preact';
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { place, type Placement } from '../lib/position';
import { KbdCombo } from './Kbd';

const DELAY = 500; // подсказка появляется через 500 мс после наведения
const WARM = 400; // если подсказка только что была открыта, следующая — сразу

let openCount = 0;
let lastClosedAt = 0;

function isWarm() {
  return openCount > 0 || performance.now() - lastClosedAt < WARM;
}

export interface TooltipBoxProps {
  children: ComponentChildren;
  /** Клавиши справа: ['Ctrl', 'K'] */
  shortcut?: readonly string[] | undefined;
  /** Многострочная: полное название, пояснение с путём */
  block?: boolean | undefined;
  class?: string | undefined;
  id?: string | undefined;
  style?: Record<string, string | number> | undefined;
}

/** Сам пузырь подсказки, без поведения. Им же рисуются состояния в галерее. */
export function TooltipBox({ children, shortcut, block, class: cls, id, style }: TooltipBoxProps) {
  const classes = ['tooltip', block ? 'tooltip--block' : shortcut?.length ? 'tooltip--kbd' : '', cls]
    .filter(Boolean)
    .join(' ');
  return (
    <div class={classes} role="tooltip" id={id} style={style}>
      {children}
      {shortcut?.length ? <KbdCombo keys={shortcut} /> : null}
    </div>
  );
}

export interface TooltipProps {
  /** Текст подсказки; пусто — подсказки нет */
  content?: ComponentChildren;
  shortcut?: readonly string[] | undefined;
  block?: boolean;
  placement?: Placement;
  disabled?: boolean;
  /** Один элемент, к которому относится подсказка */
  children: ComponentChildren;
}

/**
 * Подсказка к элементу: через 500 мс после наведения или фокуса с клавиатуры, без задержки,
 * если только что была открыта другая. Прячется при уходе курсора, нажатии и Esc.
 */
export function Tooltip({ content, shortcut, block, placement = 'bottom', disabled, children }: TooltipProps) {
  const hostRef = useRef<HTMLSpanElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const timer = useRef(0);
  const active = !disabled && content != null && content !== '';

  useEffect(() => {
    const host = hostRef.current;
    if (!host || !active) return;

    const show = () => {
      window.clearTimeout(timer.current);
      if (isWarm()) setVisible(true);
      else timer.current = window.setTimeout(() => setVisible(true), DELAY);
    };
    const hide = () => {
      window.clearTimeout(timer.current);
      setVisible(false);
    };
    const onOver = (e: PointerEvent) => {
      if (e.pointerType === 'touch') return;
      if (!host.contains(e.relatedTarget as Node | null)) show();
    };
    const onOut = (e: PointerEvent) => {
      if (!host.contains(e.relatedTarget as Node | null)) hide();
    };
    const onFocusIn = (e: FocusEvent) => {
      if ((e.target as Element).matches(':focus-visible')) show();
    };
    const onFocusOut = (e: FocusEvent) => {
      if (!host.contains(e.relatedTarget as Node | null)) hide();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') hide();
    };

    host.addEventListener('pointerover', onOver);
    host.addEventListener('pointerout', onOut);
    host.addEventListener('pointerdown', hide);
    host.addEventListener('focusin', onFocusIn);
    host.addEventListener('focusout', onFocusOut);
    document.addEventListener('keydown', onKey);
    window.addEventListener('scroll', hide, true);
    window.addEventListener('blur', hide);
    return () => {
      window.clearTimeout(timer.current);
      host.removeEventListener('pointerover', onOver);
      host.removeEventListener('pointerout', onOut);
      host.removeEventListener('pointerdown', hide);
      host.removeEventListener('focusin', onFocusIn);
      host.removeEventListener('focusout', onFocusOut);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', hide, true);
      window.removeEventListener('blur', hide);
    };
  }, [active]);

  // Учёт открытых подсказок для «без задержки у соседней кнопки»
  useEffect(() => {
    if (!visible) return;
    openCount++;
    return () => {
      openCount--;
      lastClosedAt = performance.now();
      setPos(null);
    };
  }, [visible]);

  useLayoutEffect(() => {
    if (!visible || pos) return;
    const anchor = hostRef.current?.firstElementChild;
    const box = boxRef.current;
    if (!anchor || !box) return;
    const p = place(anchor.getBoundingClientRect(), { width: box.offsetWidth, height: box.offsetHeight }, placement);
    setPos({ left: p.left, top: p.top });
  });

  const show = visible && active;
  return (
    <span class="tooltip-host" ref={hostRef}>
      {children}
      {show &&
        createPortal(
          <div
            ref={boxRef}
            class="tooltip-layer"
            style={pos ? { left: `${pos.left}px`, top: `${pos.top}px` } : { left: '0px', top: '0px', visibility: 'hidden' }}
          >
            <TooltipBox shortcut={shortcut} block={block}>
              {content}
            </TooltipBox>
          </div>,
          document.body,
        )}
    </span>
  );
}
