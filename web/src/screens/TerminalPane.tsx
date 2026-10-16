import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { transport, type TerminalSocket } from '../api/transport';
import { Icon, Terminal, type TerminalHandle } from '../components';
import { goNextWaiting } from '../app/actions';

interface TerminalPaneProps {
  /** Ключ сессии; смена временного ключа на настоящий переподключает тот же экран */
  sessionKey: string;
  /** Терминал открыт на сервере: без него не переподключаться */
  alive: boolean;
  /** Терминал на экране. Скрытый остаётся подключён: к нему возвращаются без переподключения */
  active?: boolean;
}

type LinkState = 'connecting' | 'open' | 'lost' | 'closed';

const encoder = new TextEncoder();

function socketUrl(key: string, cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const q = new URLSearchParams({ cols: String(cols), rows: String(rows) });
  return `${proto}://${location.host}/api/terminal/${encodeURIComponent(key)}?${q}`;
}

/**
 * Терминал агента: xterm.js и WebSocket к tmux. Вывод и ввод — двоичными сообщениями, размер —
 * {"type":"resize"}. Обрыв — переподключение с паузой; код 1000 — терминал закрыли, ждать события.
 */
export function TerminalPane({ sessionKey, alive, active = true }: TerminalPaneProps) {
  const term = useRef<TerminalHandle | null>(null);
  // xterm создан и подогнан: до этого его размер неизвестен
  const ready = useRef(false);
  const sock = useRef<TerminalSocket | null>(null);
  const keyRef = useRef(sessionKey);
  const aliveRef = useRef(alive);
  aliveRef.current = alive;
  const retry = useRef({ count: 0, timer: 0 });
  const [link, setLink] = useState<LinkState>('connecting');
  const [slow, setSlow] = useState(false);

  const disconnect = () => {
    window.clearTimeout(retry.current.timer);
    const s = sock.current;
    sock.current = null;
    if (s && s.readyState <= 1) s.close(1000);
  };

  /** cols и rows — размер, пока xterm ещё создаётся; вывод до его готовности копит Terminal */
  const connect = (cols?: number, rows?: number) => {
    const t = term.current;
    if (!t || !aliveRef.current || (!ready.current && !cols)) return;
    disconnect();
    setLink('connecting');
    const s = transport.terminal(socketUrl(keyRef.current, cols ?? (t.cols || 80), rows ?? (t.rows || 24)));
    s.binaryType = 'arraybuffer';
    sock.current = s;
    let opened = false;
    s.onopen = () => {
      if (sock.current !== s) return;
      opened = true;
      retry.current.count = 0;
      setLink('open');
      // Размер мог смениться, пока шло подключение. У xterm, который ещё создаётся, размера нет:
      // его пришлёт onResize
      const t = term.current;
      if (t && t.cols > 0 && t.rows > 0) s.send(JSON.stringify({ type: 'resize', cols: t.cols, rows: t.rows }));
    };
    s.onmessage = (e) => {
      if (sock.current !== s || typeof e.data === 'string') return;
      term.current?.write(new Uint8Array(e.data));
    };
    s.onclose = (e) => {
      if (sock.current !== s) return;
      sock.current = null;
      if (e.code === 1000 && opened) {
        setLink('closed');
        return;
      }
      setLink('lost');
      if (!aliveRef.current) return;
      const n = ++retry.current.count;
      const delay = Math.min(5000, 300 * 2 ** Math.min(n, 4));
      retry.current.timer = window.setTimeout(() => {
        term.current?.reset();
        connect();
      }, delay);
    };
  };

  // Другая сессия под тем же экраном (временный ключ сменился) — переподключиться
  useEffect(() => {
    if (keyRef.current === sessionKey) return;
    keyRef.current = sessionKey;
    term.current?.reset();
    connect();
  }, [sessionKey]);

  // Терминал появился снова (продолжили сессию) или пропал
  useEffect(() => {
    if (alive && !sock.current && term.current && link !== 'open') {
      term.current.reset();
      connect();
    }
  }, [alive]);

  useEffect(() => () => disconnect(), []);

  // Снова на экране — клавиатура ему
  useLayoutEffect(() => {
    if (active) term.current?.focus();
  }, [active]);

  // «Нет связи» — только если подключение затянулось
  useEffect(() => {
    if (link === 'open' || link === 'closed') {
      setSlow(false);
      return;
    }
    const t = window.setTimeout(() => setSlow(true), 1200);
    return () => window.clearTimeout(t);
  }, [link]);

  return (
    <div class="sess-term" hidden={!active}>
      <Terminal
        ref={term}
        autoFocus={active}
        class="sess-term__sheet"
        onSizeKnown={({ cols, rows }) => connect(cols, rows)}
        onReady={() => {
          ready.current = true;
          // Уже подключаемся по известному размеру — настоящий уйдёт в onResize
          if (!sock.current) connect();
        }}
        onData={(d) => {
          const s = sock.current;
          if (s && s.readyState === 1) s.send(encoder.encode(d));
        }}
        onResize={({ cols, rows }) => {
          const s = sock.current;
          if (s && s.readyState === 1) s.send(JSON.stringify({ type: 'resize', cols, rows }));
        }}
        onKey={(e) => {
          // Alt+↓ — следующая ждущая, даже из терминала; остальное уходит агенту
          if (e.altKey && !e.ctrlKey && !e.metaKey && e.key === 'ArrowDown') {
            if (e.type === 'keydown') {
              e.preventDefault();
              goNextWaiting();
            }
            return false;
          }
          return true;
        }}
      />
      {slow && alive && active && (
        <div class="sess-term__link" role="status">
          <Icon name="progress_activity" size={16} spin />
          {link === 'lost' ? 'Нет связи с терминалом, подключаюсь' : 'Подключаюсь к терминалу'}
        </div>
      )}
    </div>
  );
}
