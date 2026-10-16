import { useEffect, useRef, useState } from 'preact/hooks';
import { transport, type TerminalSocket } from '../api/transport';
import { Icon, Terminal, type TerminalHandle } from '../components';
import { goNextWaiting } from '../app/actions';

interface TerminalPaneProps {
  /** Ключ сессии; смена временного ключа на настоящий переподключает тот же экран */
  sessionKey: string;
  /** Терминал открыт на сервере: без него не переподключаться */
  alive: boolean;
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
export function TerminalPane({ sessionKey, alive }: TerminalPaneProps) {
  const term = useRef<TerminalHandle | null>(null);
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

  const connect = () => {
    const t = term.current;
    if (!t || !aliveRef.current) return;
    disconnect();
    setLink('connecting');
    const s = transport.terminal(socketUrl(keyRef.current, t.cols || 80, t.rows || 24));
    s.binaryType = 'arraybuffer';
    sock.current = s;
    let opened = false;
    s.onopen = () => {
      if (sock.current !== s) return;
      opened = true;
      retry.current.count = 0;
      setLink('open');
      // Размер мог смениться, пока шло подключение
      if (term.current) s.send(JSON.stringify({ type: 'resize', cols: term.current.cols, rows: term.current.rows }));
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
    <div class="sess-term">
      <Terminal
        autoFocus
        class="sess-term__sheet"
        onReady={(h) => {
          term.current = h;
          connect();
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
      {slow && alive && (
        <div class="sess-term__link" role="status">
          <Icon name="progress_activity" size={16} spin />
          {link === 'lost' ? 'Нет связи с терминалом, подключаюсь' : 'Подключаюсь к терминалу'}
        </div>
      )}
    </div>
  );
}
