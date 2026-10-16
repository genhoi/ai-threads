import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { SessionView } from '../api/types';
import { Button, Icon, IconButton, Input, Menu, StatusMark, toast, type MenuEntry } from '../components';
import type { IconName } from '../icons';
import { AGENTS, STATE_LABELS } from '../model';
import { goNextWaiting, resume, setDone, setHidden, setPinned, startFinish } from '../app/actions';
import { isNew, titleOf, waitingList } from '../app/derive';
import { sessionOf, store, useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { timer } from '../lib/format';
import { Messages } from '../screens/SessionInfo';
import { elsewhere, useSeen } from '../screens/Session';
import { useDetails } from '../screens/useDetails';
import { goBack } from './nav';
import { PhoneHeader } from './PhoneHeader';

/** Сколько строк истории терминала брать вместе с экраном */
const HISTORY_LINES = 200;
const POLL_MS = 1000;

/** Быстрые клавиши: имена клавиш tmux */
const KEYS: { key: string; label?: string; icon?: IconName; aria: string }[] = [
  { key: '1', label: '1', aria: '1' },
  { key: '2', label: '2', aria: '2' },
  { key: '3', label: '3', aria: '3' },
  { key: 'Enter', label: 'Enter', aria: 'Enter' },
  { key: 'Escape', label: 'Esc', aria: 'Esc' },
  { key: 'C-c', label: 'Ctrl+C', aria: 'Ctrl+C' },
  { key: 'Up', icon: 'keyboard_arrow_up', aria: 'Стрелка вверх' },
  { key: 'Down', icon: 'keyboard_arrow_down', aria: 'Стрелка вниз' },
];

/** Телефон, сессия: вывод терминала текстом, быстрые клавиши и ответ */
export function PhoneSession({ sessionKey }: { sessionKey: string }) {
  const s = useApp((st) => sessionOf(st, sessionKey));
  const ready = useApp((st) => st.status === 'ready');
  const [missing, setMissing] = useState('');

  // Сессии нет в списках (служебная, из поиска) — взять из сведений
  useEffect(() => {
    if (s || !ready) return;
    const ctl = new AbortController();
    api
      .details(sessionKey, ctl.signal)
      .then((d) => {
        const st = store.get();
        if (!sessionOf(st, d.session.key)) store.set({ extra: { ...st.extra, [d.session.key]: d.session } });
      })
      .catch((e) => {
        if ((e as Error).name !== 'AbortError') setMissing(errorText(e));
      });
    return () => ctl.abort();
  }, [sessionKey, !!s, ready]);

  if (!s)
    return (
      <div class="ph-screen">
        <PhoneHeader back={() => goBack('#/')} title={missing ? 'Сессия не найдена' : ''} />
        {missing && <p class="ph-empty">{missing}</p>}
      </div>
    );
  return <SessionBody s={s} />;
}

function stateText(s: SessionView): string {
  if (s.state === 'error') return s.exit_code === undefined ? 'ошибка' : `ошибка · код ${s.exit_code}`;
  if (s.state === 'working') return `${STATE_LABELS.working} ${timer(s.since ?? s.updated)}`;
  return STATE_LABELS[s.state];
}

function SessionBody({ s }: { s: SessionView }) {
  useSeen(s);
  useTick(s.state === 'working');
  const sessions = useApp((st) => st.sessions);
  const hasNext = useMemo(() => waitingList(sessions).some((w) => w.key !== s.key), [sessions, s.key]);
  const [menu, setMenu] = useState<HTMLElement | null>(null);

  const items: MenuEntry[] = [
    { id: 'next', label: 'Следующая ждущая', icon: 'keyboard_arrow_down', disabled: !hasNext, onSelect: goNextWaiting },
    { type: 'separator' },
    { id: 'pin', label: s.pinned ? 'Открепить' : 'Закрепить', icon: 'push_pin', onSelect: () => void setPinned(s.key, !s.pinned) },
    { id: 'hide', label: s.hidden ? 'Показать' : 'Скрыть', icon: 'visibility_off', onSelect: () => void setHidden(s.key, !s.hidden) },
    { type: 'separator' },
    s.done
      ? { id: 'undone', label: 'Вернуть из завершённых', icon: 'task_alt', onSelect: () => void setDone(s.key, false) }
      : { id: 'finish', label: 'Завершить…', icon: 'task_alt', onSelect: () => startFinish(s.key) },
  ];

  const sub = (
    <>
      {s.state !== 'idle' && <StatusMark state={s.state} isNew={isNew(s)} decorative />}
      <span class={`ph-st ph-st--${s.state}`}>{stateText(s)}</span>
      <span class="ph-head__where">
        · {AGENTS[s.agent]?.short ?? s.agent} · {s.project}
      </span>
    </>
  );

  return (
    <div class="ph-screen">
      <PhoneHeader
        size="session"
        back={() => goBack('#/')}
        title={titleOf(s)}
        sub={sub}
        actions={
          <IconButton
            icon="more_horiz"
            label="Меню сессии"
            size={48}
            noTooltip
            pressed={!!menu}
            aria-haspopup="menu"
            aria-expanded={!!menu}
            onClick={(e) => setMenu(menu ? null : e.currentTarget)}
          />
        }
      />
      {s.terminal ? <Live s={s} /> : <Closed s={s} />}
      <Menu open={!!menu} anchor={menu} label="Меню сессии" placement="bottom-end" width={260} items={items} onClose={() => setMenu(null)} />
    </div>
  );
}

/** Экран терминала текстом: раз в секунду, пока страница видна */
function useScreen(key: string) {
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState('');
  const kick = useRef<() => void>(() => undefined);

  useEffect(() => {
    let alive = true;
    let timerId = 0;
    let ctl: AbortController | null = null;
    const tick = async () => {
      window.clearTimeout(timerId);
      if (!alive) return;
      if (document.visibilityState !== 'visible') return;
      ctl?.abort();
      const mine = new AbortController();
      ctl = mine;
      try {
        const r = await api.screen(key, HISTORY_LINES, mine.signal);
        if (!alive || ctl !== mine) return;
        setText(clean(r.text));
        setError('');
      } catch (e) {
        if (!alive || (e as Error).name === 'AbortError') return;
        setError(errorText(e));
      }
      if (alive && ctl === mine) timerId = window.setTimeout(() => void tick(), POLL_MS);
    };
    kick.current = () => void tick();
    void tick();
    const onVisible = () => {
      if (document.visibilityState === 'visible') void tick();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      alive = false;
      window.clearTimeout(timerId);
      ctl?.abort();
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [key]);

  return { text, error, refresh: () => kick.current() };
}

/** Без пробелов в концах строк и пустых строк внизу экрана */
function clean(text: string): string {
  return text
    .split('\n')
    .map((l) => l.replace(/\s+$/, ''))
    .join('\n')
    .replace(/^\n+/, '')
    .replace(/\n+$/, '');
}

function Live({ s }: { s: SessionView }) {
  const { text, error, refresh } = useScreen(s.key);
  const outRef = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const [reply, setReply] = useState('');
  const [sending, setSending] = useState(false);

  // Новые строки — сразу внизу, если человек не листает выше
  useLayoutEffect(() => {
    const el = outRef.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [text]);

  const send = async (keys: string[], textToSend?: string) => {
    try {
      await api.keys(s.key, { text: textToSend, keys });
      atBottom.current = true;
      refresh();
      return true;
    } catch (e) {
      toast.show({ kind: 'error', title: 'Не получилось', text: errorText(e) });
      return false;
    }
  };

  return (
    <>
      <div
        ref={outRef}
        class="ph-out"
        role="log"
        aria-label="Терминал"
        aria-live="off"
        tabIndex={0}
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        <pre class="ph-out__text">{text ?? ''}</pre>
        {error && text === null && <p class="ph-out__error">{error}</p>}
      </div>
      <div class="ph-keys" role="toolbar" aria-label="Быстрые клавиши">
        {KEYS.map((k) => (
          <button
            key={k.key}
            type="button"
            class={`ph-key${k.icon ? ' ph-key--icon' : ''}${k.label && k.label.length > 1 ? ' ph-key--word' : ''}`}
            aria-label={k.aria}
            // Нажатие не забирает фокус у поля ответа: клавиатура телефона не прячется
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => void send([k.key])}
          >
            {k.icon ? <Icon name={k.icon} size={18} /> : k.label}
          </button>
        ))}
      </div>
      <form
        class="ph-reply"
        onSubmit={async (e) => {
          e.preventDefault();
          const value = reply;
          if (!value.trim() || sending) return;
          setSending(true);
          if (await send(['Enter'], value)) setReply('');
          setSending(false);
        }}
      >
        <Input
          class="ph-reply__field"
          size={48}
          label="Ответ агенту"
          placeholder="Ответ агенту"
          value={reply}
          enterKeyHint="send"
          onInput={(e) => setReply(e.currentTarget.value)}
        />
        <IconButton type="submit" icon="arrow_upward" label="Отправить" size={48} variant="primary" noTooltip loading={sending} disabled={!reply.trim()} />
      </form>
    </>
  );
}

/** Терминала нет: последние сообщения и «Продолжить в терминале» */
function Closed({ s }: { s: SessionView }) {
  const { details, error } = useDetails(s.key, true, s.updated);
  const [resuming, setResuming] = useState(false);
  const reason = s.where
    ? elsewhere(s)
    : !s.resumable
      ? `${AGENTS[s.agent]?.short ?? s.agent} не умеет продолжать сессии`
      : s.missing
        ? 'Папки сессии нет'
        : '';
  return (
    <>
      <div class="ph-closed">
        {details ? (
          <Messages messages={details.messages ?? []} agent={s.agent} variant="panel" limit={8} />
        ) : (
          error && <p class="ph-out__error">{error}</p>
        )}
      </div>
      <div class="ph-foot">
        {reason && <p class="ph-hint">{reason}</p>}
        <Button
          variant="primary"
          size={48}
          icon="terminal"
          block
          class="ph-big"
          loading={resuming}
          disabled={!!reason}
          onClick={async () => {
            setResuming(true);
            await resume(s.key);
            setResuming(false);
          }}
        >
          Продолжить в терминале
        </Button>
      </div>
    </>
  );
}
