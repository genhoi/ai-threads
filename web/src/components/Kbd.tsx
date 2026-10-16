export type KbdSize = 'sm' | 'md';

export interface KbdProps {
  children: string;
  /** sm — 20 px: подсказки, меню, поиск; md — 24 px: список сочетаний в настройках */
  size?: KbdSize;
  class?: string;
}

/** Одна клавиша */
export function Kbd({ children, size = 'sm', class: cls }: KbdProps) {
  return <kbd class={`kbd kbd--${size}${cls ? ' ' + cls : ''}`}>{children}</kbd>;
}

export interface KbdComboProps {
  /** Клавиши по порядку: ['Ctrl', 'K'] */
  keys: readonly string[];
  size?: KbdSize;
  class?: string;
}

/** Сочетание клавиш: клавиши рядом, без плюса между ними */
export function KbdCombo({ keys, size = 'sm', class: cls }: KbdComboProps) {
  if (keys.length === 1) return <Kbd size={size} class={cls}>{keys[0]!}</Kbd>;
  return (
    <kbd class={`kbd-combo kbd-combo--${size}${cls ? ' ' + cls : ''}`}>
      {keys.map((k) => (
        <Kbd key={k} size={size}>
          {k}
        </Kbd>
      ))}
    </kbd>
  );
}
