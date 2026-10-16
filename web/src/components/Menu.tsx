import { createPortal } from 'preact';
import type { ComponentChildren, CSSProperties, TargetedKeyboardEvent } from 'preact';
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { IconName } from '../icons';
import { place, type Placement } from '../lib/position';
import { usePresence } from '../lib/presence';
import type { AgentId } from '../model';
import { AgentBadge } from './AgentBadge';
import { Icon } from './Icon';
import { KbdCombo } from './Kbd';

export interface MenuItemEntry {
  type?: 'item';
  id: string;
  label: string;
  /** Иконка 18 слева */
  icon?: IconName;
  /** Иконка агента 18 слева вместо иконки */
  agent?: AgentId;
  /** Вторая строка моноширинным: путь проекта. Пункт становится 48 px */
  description?: string;
  /** Подпись моноширинным: путь в меню проекта */
  mono?: boolean;
  /** Сочетание клавиш справа */
  shortcut?: readonly string[];
  /** Значение справа: «Claude», «2» */
  value?: string;
  /** Тихая подпись справа: агент проекта в выборе проекта */
  hint?: string;
  /** Иконка справа: content_copy у пути */
  trailingIcon?: IconName;
  /** Отмечен галочкой справа */
  checked?: boolean;
  disabled?: boolean;
  /** Почему недоступен: строкой под подписью или справа (reasonInline) */
  reason?: string;
  reasonInline?: boolean;
  submenu?: MenuEntry[];
  submenuWidth?: number;
  /** Своя высота пункта, например 40 у «Другая папка…» */
  height?: 36 | 40 | 44 | 48;
  onSelect?: () => void;
  /** Не закрывать меню после выбора */
  keepOpen?: boolean;
}

export type MenuEntry = MenuItemEntry | { type: 'separator' } | { type: 'label'; label: string };

export type MenuCloseReason = 'escape' | 'select' | 'outside' | 'tab' | 'left';

export interface MenuProps {
  open: boolean;
  onClose: (reason: MenuCloseReason) => void;
  /** Элемент, от которого открывается меню; в него же вернётся фокус */
  anchor?: HTMLElement | null;
  items: MenuEntry[];
  /** Подпись меню для экранного диктора */
  label: string;
  /** Наименьшая ширина: 300 по умолчанию, 220 у подменю, 380 у выбора проекта */
  width?: number;
  placement?: Placement;
  /** Над пунктами: поле поиска */
  header?: ComponentChildren;
  /** Расстояние между иконкой и подписью: 12, в списке агентов 10 */
  gap?: 10 | 12;
  /** Вызывается при выборе любого пункта */
  onSelect?: (id: string) => void;
  /** В потоке страницы, без всплытия: для галереи */
  inline?: boolean;
  /** Пункт под курсором с самого начала; смена значения переводит курсор на новый пункт */
  activeId?: string;
  class?: string;
  /** Для подменю: общий корень, сдвиг по вертикали */
  tree?: string;
  isSub?: boolean;
  /** Перевести фокус в меню при открытии (по умолчанию да; подменю — только с клавиатуры) */
  autoFocus?: boolean;
}

const SUBMENU_DELAY = 150;
const EXIT_MS = 100; // закрытие — прозрачностью за 100 мс

function isItem(e: MenuEntry): e is MenuItemEntry {
  return e.type === undefined || e.type === 'item';
}

function enabledItems(items: MenuEntry[]): MenuItemEntry[] {
  return items.filter((e): e is MenuItemEntry => isItem(e) && !e.disabled);
}

/**
 * Меню: пункты 36 px с иконкой, подписью и справа сочетанием, значением или стрелкой подменю.
 * Стрелки — по пунктам, Enter — выбрать, → и ← — подменю, Esc — закрыть, буква — к пункту на неё.
 */
export function Menu(props: MenuProps) {
  const { open, inline } = props;
  const { mounted, closing } = usePresence(open, inline ? 0 : EXIT_MS);
  if (!mounted && !inline) return null;
  if (inline) return <MenuPanel {...props} closing={false} />;
  return createPortal(<MenuPanel {...props} closing={closing} />, document.body);
}

function MenuPanel({
  onClose,
  anchor,
  items,
  label,
  width = 300,
  placement = 'bottom-start',
  header,
  gap = 12,
  onSelect,
  inline,
  activeId: initialActive,
  class: cls,
  tree,
  isSub,
  autoFocus = true,
  closing,
}: MenuProps & { closing: boolean }) {
  const ownId = useId();
  const treeId = tree ?? `menu-${ownId}`;
  const panelRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<string | undefined>(initialActive);
  const [sub, setSub] = useState<{ id: string; el: HTMLElement; focus: boolean } | null>(null);
  const subTimer = useRef(0);
  const typed = useRef({ text: '', at: 0 });

  const close = useCallback(
    (reason: MenuCloseReason) => {
      if (reason !== 'outside' && anchor && !inline) anchor.focus({ preventScroll: true });
      onClose(reason);
    },
    [anchor, inline, onClose],
  );

  // Место рядом с кнопкой; подменю — вровень с пунктом
  const anchorAt = useRef<{ x: number; y: number } | null>(null);
  useLayoutEffect(() => {
    if (inline || !anchor || !panelRef.current) return;
    const el = panelRef.current;
    const rect = anchor.getBoundingClientRect();
    anchorAt.current = { x: rect.left, y: rect.top };
    const p = place(rect, { width: el.offsetWidth, height: el.offsetHeight }, placement, isSub ? 4 : 6);
    // Прямо в стиль, до отрисовки: меню сразу на месте и может получить фокус
    el.style.left = `${p.left}px`;
    el.style.top = `${isSub ? Math.max(8, p.top - 6) : p.top}px`;
  }, [anchor, inline, placement, isSub, items.length]);

  // Фокус в меню сразу при открытии: в поле поиска, если оно есть
  useEffect(() => {
    if (inline || closing || !autoFocus) return;
    const el = panelRef.current;
    if (!el) return;
    const input = el.querySelector<HTMLInputElement>('input');
    (input ?? el).focus({ preventScroll: true });
  }, [inline]);

  // Щелчок мимо меню и его подменю закрывает; прокрутка страницы и смена размера окна — тоже
  useEffect(() => {
    if (inline || isSub || closing) return;
    const onDown = (e: PointerEvent) => {
      const t = e.target as Element | null;
      if (t?.closest(`[data-menu-tree="${treeId}"]`)) return;
      if (anchor && anchor.contains(t)) return;
      close('outside');
    };
    // Закрывает только прокрутка, которая сдвинула кнопку: запоздалое событие от фокуса не в счёт
    const onScroll = (e: Event) => {
      if ((e.target as Element | null)?.closest?.(`[data-menu-tree="${treeId}"]`)) return;
      const at = anchorAt.current;
      const rect = anchor?.getBoundingClientRect();
      if (at && rect && Math.abs(rect.left - at.x) < 1 && Math.abs(rect.top - at.y) < 1) return;
      close('outside');
    };
    const onResize = () => close('outside');
    document.addEventListener('pointerdown', onDown, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
  }, [inline, isSub, closing, anchor, close, treeId]);

  useEffect(() => () => window.clearTimeout(subTimer.current), []);

  // Экран сменил пункт под курсором: например, поиск в выборе проекта отфильтровал список
  const firstActive = useRef(true);
  useEffect(() => {
    if (firstActive.current) {
      firstActive.current = false;
      return;
    }
    if (initialActive !== undefined) setActive(initialActive);
  }, [initialActive]);

  const itemEl = (id: string) => panelRef.current?.querySelector<HTMLElement>(`[data-item="${CSS.escape(id)}"]`) ?? null;

  const activate = (item: MenuItemEntry, viaKeyboard: boolean) => {
    if (item.disabled) return;
    if (item.submenu) {
      const el = itemEl(item.id);
      if (el) setSub({ id: item.id, el, focus: viaKeyboard });
      return;
    }
    item.onSelect?.();
    onSelect?.(item.id);
    if (!item.keepOpen) close('select');
  };

  const step = (dir: 1 | -1) => {
    const list = enabledItems(items);
    if (!list.length) return;
    const i = list.findIndex((it) => it.id === active);
    const next = i === -1 ? (dir === 1 ? 0 : list.length - 1) : (i + dir + list.length) % list.length;
    setActive(list[next]!.id);
  };

  const onKeyDown = (e: TargetedKeyboardEvent<HTMLDivElement>) => {
    const inInput = (e.target as HTMLElement).tagName === 'INPUT';
    const current = enabledItems(items).find((it) => it.id === active);
    switch (e.key) {
      case 'ArrowDown':
        step(1);
        break;
      case 'ArrowUp':
        step(-1);
        break;
      case 'Home':
        if (inInput) return;
        setActive(enabledItems(items)[0]?.id);
        break;
      case 'End':
        if (inInput) return;
        setActive(enabledItems(items).at(-1)?.id);
        break;
      case 'Enter':
      case ' ':
        if (e.key === ' ' && inInput) return;
        if (current) activate(current, true);
        break;
      case 'ArrowRight':
        if (inInput || !current?.submenu) return;
        activate(current, true);
        break;
      case 'ArrowLeft':
        if (inInput || !isSub) return;
        close('left');
        break;
      case 'Escape':
        close('escape');
        break;
      case 'Tab':
        close('tab');
        return;
      default: {
        // Буква — к следующему пункту на неё
        if (inInput || e.key.length !== 1 || e.ctrlKey || e.metaKey || e.altKey) return;
        const now = performance.now();
        typed.current.text = now - typed.current.at > 600 ? e.key.toLowerCase() : typed.current.text + e.key.toLowerCase();
        typed.current.at = now;
        const list = enabledItems(items);
        const start = Math.max(0, list.findIndex((it) => it.id === active));
        const order = [...list.slice(start + (typed.current.text.length === 1 ? 1 : 0)), ...list.slice(0, start + 1)];
        const hit = order.find((it) => it.label.toLowerCase().startsWith(typed.current.text));
        if (hit) setActive(hit.id);
        break;
      }
    }
    e.preventDefault();
    e.stopPropagation();
  };

  // Пункт под курсором всегда виден, если список прокручивается
  useEffect(() => {
    const panel = panelRef.current;
    const el = active ? itemEl(active) : null;
    if (!panel || !el || panel.scrollHeight <= panel.clientHeight) return;
    if (el.offsetTop < panel.scrollTop) panel.scrollTop = el.offsetTop;
    else if (el.offsetTop + el.offsetHeight > panel.scrollTop + panel.clientHeight)
      panel.scrollTop = el.offsetTop + el.offsetHeight - panel.clientHeight;
  }, [active]);

  const hoverItem = (item: MenuItemEntry, el: HTMLElement) => {
    if (item.disabled) return;
    setActive(item.id);
    window.clearTimeout(subTimer.current);
    if (item.submenu) {
      if (sub?.id !== item.id) subTimer.current = window.setTimeout(() => setSub({ id: item.id, el, focus: false }), SUBMENU_DELAY);
    } else if (sub) {
      setSub(null);
    }
  };

  // Ширина из макета — наименьшая: длинная подпись расширяет меню, а не обрезается
  const style: CSSProperties = { minWidth: `${width}px`, '--menu-gap': `${gap}px` } as CSSProperties;

  const activeDomId = active ? `${treeId}-${ownId}-${active}` : undefined;
  const subItem = sub ? items.find((e): e is MenuItemEntry => isItem(e) && e.id === sub.id) : undefined;

  return (
    <>
      <div
        ref={panelRef}
        class={['menu', inline && 'is-inline', closing && 'is-closing', cls].filter(Boolean).join(' ')}
        style={style}
        role="menu"
        aria-label={label}
        aria-activedescendant={activeDomId}
        tabIndex={-1}
        data-menu-tree={treeId}
        onKeyDown={onKeyDown}
      >
        {header && <div class="menu__header">{header}</div>}
        {items.map((entry, i) => {
          if (entry.type === 'separator') return <div key={`sep-${i}`} class="menu-sep" role="separator" />;
          if (entry.type === 'label')
            return (
              <div key={`label-${i}`} class="menu-label" role="presentation">
                {entry.label}
              </div>
            );
          return (
            <MenuRow
              key={entry.id}
              item={entry}
              domId={`${treeId}-${ownId}-${entry.id}`}
              active={entry.id === active}
              expanded={sub?.id === entry.id}
              onHover={hoverItem}
              onActivate={(it) => activate(it, false)}
            />
          );
        })}
      </div>
      {sub && subItem?.submenu && (
        <Menu
          key={`${sub.id}-${sub.focus}`}
          open
          inline={false}
          autoFocus={sub.focus}
          anchor={sub.el}
          items={subItem.submenu}
          label={subItem.label}
          width={subItem.submenuWidth ?? 220}
          placement="right-start"
          gap={10}
          tree={treeId}
          isSub
          activeId={sub.focus ? enabledItems(subItem.submenu)[0]?.id : undefined}
          onSelect={onSelect}
          onClose={(reason) => {
            setSub(null);
            if (reason === 'select' || reason === 'tab' || reason === 'outside') close(reason);
            else panelRef.current?.focus({ preventScroll: true });
          }}
        />
      )}
    </>
  );
}

interface MenuRowProps {
  item: MenuItemEntry;
  domId: string;
  active: boolean;
  expanded: boolean;
  onHover: (item: MenuItemEntry, el: HTMLElement) => void;
  onActivate: (item: MenuItemEntry) => void;
}

function MenuRow({ item, domId, active, expanded, onHover, onActivate }: MenuRowProps) {
  const trailing = item.value !== undefined || item.submenu || item.trailingIcon || item.mono;
  const grow = trailing || item.shortcut || item.checked || item.hint || (item.disabled && item.reasonInline);
  const reasonBelow = item.disabled && item.reason && !item.reasonInline;
  const classes = [
    'menu-item',
    trailing && 'menu-item--trail',
    grow && 'menu-item--grow',
    item.mono && 'menu-item--mono',
    item.description && 'menu-item--desc',
    reasonBelow && 'menu-item--reason',
    active && 'is-active',
  ]
    .filter(Boolean)
    .join(' ');
  const role = item.checked !== undefined ? 'menuitemradio' : 'menuitem';

  return (
    <div
      id={domId}
      class={classes}
      style={item.height ? { height: `${item.height}px` } : undefined}
      role={role}
      aria-checked={item.checked !== undefined ? item.checked : undefined}
      aria-disabled={item.disabled || undefined}
      aria-haspopup={item.submenu ? 'menu' : undefined}
      aria-expanded={item.submenu ? expanded : undefined}
      data-item={item.id}
      onPointerMove={(e) => onHover(item, e.currentTarget)}
      onClick={() => onActivate(item)}
    >
      {item.agent ? (
        <AgentBadge agent={item.agent} size={18} disabled={item.disabled} decorative />
      ) : (
        item.icon && <Icon name={item.icon} size={18} class="menu-item__icon" />
      )}
      {item.description || reasonBelow ? (
        <span class="menu-item__text">
          <span class="menu-item__label">{item.label}</span>
          <span class={item.description ? 'menu-item__desc' : 'menu-item__reason'}>{item.description ?? item.reason}</span>
        </span>
      ) : (
        <span class="menu-item__label">{item.label}</span>
      )}
      {item.disabled && item.reasonInline && item.reason && <span class="menu-item__inline-reason">{item.reason}</span>}
      {item.hint && <span class="menu-item__hint">{item.hint}</span>}
      {item.value !== undefined && <span class="menu-item__value">{item.value}</span>}
      {item.shortcut && <KbdCombo keys={item.shortcut} />}
      {item.trailingIcon && <Icon name={item.trailingIcon} size={16} class="menu-item__trail-icon" />}
      {item.checked && <Icon name="check" size={16} class="menu-item__check" />}
      {item.submenu && <Icon name="chevron_right" size={16} class="menu-item__chevron" />}
    </div>
  );
}

/** Состояние меню у кнопки: открыть от нажатой кнопки, закрыть, переключить */
export function useMenu() {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const toggle = useCallback((e: { currentTarget: EventTarget | null }) => {
    setAnchor(e.currentTarget as HTMLElement);
    setOpen((o) => !o);
  }, []);
  const close = useCallback(() => setOpen(false), []);
  return { open, anchor, toggle, close };
}
