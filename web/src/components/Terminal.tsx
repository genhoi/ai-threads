import type { Ref } from 'preact';
import { useEffect, useImperativeHandle, useRef } from 'preact/hooks';
import type { ITheme, Terminal as XTerm } from '@xterm/xterm';

export interface TerminalHandle {
  /** Вывод агента; до готовности терминала копится и выводится потом */
  write(data: string | Uint8Array): void;
  focus(): void;
  /** Подогнать размер под контейнер сейчас */
  fit(): void;
  clear(): void;
  /** Сбросить экран целиком: перед повторным подключением, tmux нарисует его заново */
  reset(): void;
  readonly cols: number;
  readonly rows: number;
}

export interface TerminalProps {
  /** Ввод с клавиатуры для агента */
  onData?: (data: string) => void;
  /** Новый размер в символах после подгонки */
  onResize?: (size: { cols: number; rows: number }) => void;
  /** Терминал открыт, шрифт загружен, размер подогнан */
  onReady?: (handle: TerminalHandle) => void;
  /** Клавиша до терминала: false — терминал её не получит (сочетания «Нити», например Alt+↓) */
  onKey?: (e: KeyboardEvent) => boolean;
  /** Лист: фон, граница, скругление 18, тень и поля 20/24/22. Без него — только экран */
  sheet?: boolean;
  autoFocus?: boolean;
  /** Сколько строк хранить для прокрутки */
  scrollback?: number;
  class?: string;
  ref?: Ref<TerminalHandle>;
}

const FONT_SIZE = 13;
const ROW_HEIGHT = 17; // JetBrains Mono 13/17

function token(styles: CSSStyleDeclaration, name: string): string {
  return styles.getPropertyValue(name).trim();
}

/** Цвета терминала из токенов (--term-*); остальные цвета выводит агент */
function themeFromTokens(): ITheme {
  const s = getComputedStyle(document.documentElement);
  const bg = token(s, '--term-bg');
  const selection = token(s, '--term-selection');
  return {
    background: bg,
    foreground: token(s, '--term-fg'),
    cursor: token(s, '--term-cursor'),
    cursorAccent: bg,
    selectionBackground: selection,
    selectionInactiveBackground: selection,
    brightBlack: token(s, '--term-dim'),
    red: token(s, '--term-red'),
    green: token(s, '--term-green'),
    blue: token(s, '--term-blue'),
    scrollbarSliderBackground: token(s, '--border'),
    scrollbarSliderHoverBackground: token(s, '--border-hover'),
    scrollbarSliderActiveBackground: token(s, '--border-hover'),
  };
}

/**
 * Терминал xterm.js без сети: только экран, ввод и размер. Размер подгоняется под контейнер
 * сразу, без анимации: каждое изменение перерисовывает экран агента.
 */
export function Terminal({ onData, onResize, onReady, onKey, sheet = true, autoFocus, scrollback = 5000, class: cls, ref }: TerminalProps) {
  const screenRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<(() => void) | null>(null);
  const pending = useRef<(string | Uint8Array)[]>([]);
  const callbacks = useRef({ onData, onResize, onReady, onKey });
  callbacks.current = { onData, onResize, onReady, onKey };

  const handle: TerminalHandle = {
    write(data) {
      if (termRef.current) termRef.current.write(data);
      else pending.current.push(data);
    },
    focus() {
      termRef.current?.focus();
    },
    fit() {
      fitRef.current?.();
    },
    clear() {
      termRef.current?.clear();
    },
    reset() {
      termRef.current?.reset();
    },
    get cols() {
      return termRef.current?.cols ?? 0;
    },
    get rows() {
      return termRef.current?.rows ?? 0;
    },
  };
  useImperativeHandle(ref ?? null, () => handle, []);

  useEffect(() => {
    const screen = screenRef.current;
    if (!screen) return;
    let disposed = false;
    let observer: ResizeObserver | undefined;
    let frame = 0;
    let term: XTerm | undefined;

    (async () => {
      // xterm грузится отдельным файлом: экранам без терминала он не нужен
      const [{ Terminal: XTermCtor }, { FitAddon }, webglModule] = await Promise.all([
        import('@xterm/xterm'),
        import('@xterm/addon-fit'),
        import('@xterm/addon-webgl').catch(() => null),
      ]);
      // Размер ячейки меряется по шрифту: сначала дождаться JetBrains Mono
      await Promise.all([
        document.fonts.load(`400 ${FONT_SIZE}px "JetBrains Mono"`),
        document.fonts.load(`700 ${FONT_SIZE}px "JetBrains Mono"`),
      ]).catch(() => undefined);
      if (disposed) return;

      term = new XTermCtor({
        fontFamily: "'JetBrains Mono', ui-monospace, monospace",
        fontSize: FONT_SIZE,
        fontWeight: 400,
        fontWeightBold: 700,
        lineHeight: 1,
        letterSpacing: 0,
        cursorStyle: 'block',
        cursorBlink: false,
        scrollback,
        theme: themeFromTokens(),
      });
      const fitAddon = new FitAddon();
      term.loadAddon(fitAddon);
      term.open(screen);

      // Строка 17 px. xterm берёт высоту строки из шрифта и умеет только увеличивать её (lineHeight ≥ 1)
      const rowEl = screen.querySelector<HTMLElement>('.xterm-rows > div');
      const natural = rowEl?.getBoundingClientRect().height ?? 0;
      if (natural > 0 && natural < ROW_HEIGHT - 0.01) term.options.lineHeight = ROW_HEIGHT / natural;

      // WebGL рисует рамки и линии псевдографики сам, на всю высоту строки: без него рамки
      // Claude Code выходят пунктиром. Нет WebGL или контекст потерян — остаётся обычная отрисовка
      if (webglModule) {
        try {
          const webgl = new webglModule.WebglAddon();
          webgl.onContextLoss(() => webgl.dispose());
          term.loadAddon(webgl);
        } catch {
          // WebGL недоступен: xterm рисует через DOM
        }
      }

      const fit = () => {
        if (screen.clientWidth > 0 && screen.clientHeight > 0) fitAddon.fit();
      };
      fitRef.current = fit;
      fit();

      term.attachCustomKeyEventHandler((e) => callbacks.current.onKey?.(e) ?? true);
      term.onData((d) => callbacks.current.onData?.(d));
      term.onResize(({ cols, rows }) => callbacks.current.onResize?.({ cols, rows }));
      termRef.current = term;
      for (const chunk of pending.current) term.write(chunk);
      pending.current = [];

      observer = new ResizeObserver(() => {
        cancelAnimationFrame(frame);
        frame = requestAnimationFrame(fit);
      });
      observer.observe(screen);

      if (autoFocus) term.focus();
      callbacks.current.onResize?.({ cols: term.cols, rows: term.rows });
      callbacks.current.onReady?.(handle);
    })();

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer?.disconnect();
      fitRef.current = null;
      termRef.current = null;
      term?.dispose();
    };
  }, []);

  return (
    <div class={['term', sheet && 'term--sheet', cls].filter(Boolean).join(' ')}>
      <div class="term__screen" ref={screenRef} />
    </div>
  );
}
