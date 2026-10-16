// Расчёт места для всплывающих элементов: подсказки, меню, подменю.

export type Placement =
  | 'bottom'
  | 'bottom-start'
  | 'bottom-end'
  | 'top'
  | 'top-start'
  | 'top-end'
  | 'right-start'
  | 'left-start';

export interface Position {
  left: number;
  top: number;
  /** Куда в итоге открылось: при нехватке места сторона меняется */
  placement: Placement;
}

const EDGE = 8; // от края окна

/** Место для элемента размером size рядом с anchor. Координаты для position: fixed. */
export function place(anchor: DOMRect, size: { width: number; height: number }, placement: Placement, gap = 8): Position {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  let p = placement;

  if (p.startsWith('bottom') && anchor.bottom + gap + size.height > vh - EDGE && anchor.top - gap - size.height >= EDGE) {
    p = p.replace('bottom', 'top') as Placement;
  } else if (p.startsWith('top') && anchor.top - gap - size.height < EDGE && anchor.bottom + gap + size.height <= vh - EDGE) {
    p = p.replace('top', 'bottom') as Placement;
  } else if (p === 'right-start' && anchor.right + gap + size.width > vw - EDGE && anchor.left - gap - size.width >= EDGE) {
    p = 'left-start';
  } else if (p === 'left-start' && anchor.left - gap - size.width < EDGE) {
    p = 'right-start';
  }

  let left: number;
  let top: number;
  switch (p) {
    case 'bottom':
    case 'top':
      left = anchor.left + anchor.width / 2 - size.width / 2;
      top = p === 'bottom' ? anchor.bottom + gap : anchor.top - gap - size.height;
      break;
    case 'bottom-start':
    case 'top-start':
      left = anchor.left;
      top = p === 'bottom-start' ? anchor.bottom + gap : anchor.top - gap - size.height;
      break;
    case 'bottom-end':
    case 'top-end':
      left = anchor.right - size.width;
      top = p === 'bottom-end' ? anchor.bottom + gap : anchor.top - gap - size.height;
      break;
    case 'right-start':
      left = anchor.right + gap;
      top = anchor.top;
      break;
    case 'left-start':
      left = anchor.left - gap - size.width;
      top = anchor.top;
      break;
  }

  left = Math.max(EDGE, Math.min(left, vw - EDGE - size.width));
  top = Math.max(EDGE, Math.min(top, vh - EDGE - size.height));
  return { left: Math.round(left), top: Math.round(top), placement: p };
}
