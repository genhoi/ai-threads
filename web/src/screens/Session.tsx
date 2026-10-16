import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { SessionView } from '../api/types';
import { AgentBadge, Button, CopyBadge, IconButton, Menu, StatePill, StatusMark, Tooltip } from '../components';
import { AGENTS } from '../model';
import { goNextWaiting, markSeen, openHome, rename, resume, toggleSidebar } from '../app/actions';
import { isNew, titleOf, waitingList } from '../app/derive';
import { sessionMenu } from '../app/menus';
import { sessionOf, store, useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { timer, whenFull } from '../lib/format';
import { CommandBox, LaunchBlock, Messages, NextStep, SummaryBlock, useSummaryVersion } from './SessionInfo';
import { TerminalPane } from './TerminalPane';
import { useDetails } from './useDetails';

/** Экран сессии: шапка, терминал или карточка из истории, сведения поверх, полный экран */
export function SessionScreen({ sessionKey }: { sessionKey: string }) {
  const session = useApp((s) => sessionOf(s, sessionKey));
  const ready = useApp((s) => s.status === 'ready');
  const [missing, setMissing] = useState('');

  // Сессии нет в каталоге (служебная, из поиска) — взять из сведений
  useEffect(() => {
    if (session || !ready) return;
    setMissing('');
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
  }, [sessionKey, !!session, ready]);

  if (!session) {
    return (
      <main class="sess sess--empty">
        {missing && (
          <div class="page-empty">
            <h1 class="page-empty__title">Сессия не найдена</h1>
            <p class="page-empty__text">{missing}</p>
            <Button variant="regular" icon="arrow_back" onClick={() => openHome()}>
              На главную
            </Button>
          </div>
        )}
      </main>
    );
  }
  return <SessionView_ s={session} />;
}

/** Сколько терминалов держать подключёнными: открытый и те, в которые заходили последними */
const TERMINALS_KEPT = 5;

interface KeptTerminal {
  /** Ключ, с которым терминал открыли: временный ключ новой сессии меняется, экран остаётся */
  id: string;
  key: string;
  used: number;
}

let useCounter = 0;

/**
 * Терминалы сессий, в которые недавно заходили. Скрытый терминал остаётся подключён, и переход
 * к нему не создаёт xterm заново и не ждёт сервер. Закрытые на сервере отпускаются сразу,
 * лишние — начиная с самого давнего
 */
function useKeptTerminals(s: SessionView): KeptTerminal[] {
  const kept = useRef<KeptTerminal[]>([]);
  // Перерисоваться, когда терминал одной из скрытых сессий закрылся
  useApp((st) => kept.current.map((t) => (sessionOf(st, st.swaps[t.key] ?? t.key)?.terminal ? '1' : '0')).join(''));
  const st = store.get();
  let list = kept.current.map((t) => {
    const real = st.swaps[t.key];
    return real ? { ...t, key: real } : t;
  });
  list = list.filter((t) => t.key !== s.key && sessionOf(st, t.key)?.terminal);
  if (s.terminal) {
    const prev = kept.current.find((t) => t.key === s.key || st.swaps[t.key] === s.key);
    list.push({ id: prev?.id ?? s.key, key: s.key, used: ++useCounter });
  }
  // У открытого used самый большой: он не уходит никогда
  while (list.length > TERMINALS_KEPT) {
    const oldest = list.reduce((a, b) => (b.used < a.used ? b : a));
    list = list.filter((t) => t !== oldest);
  }
  // Порядок как при добавлении: Preact не переставляет узлы с холстами WebGL
  const order = new Map(kept.current.map((t, i) => [t.id, i]));
  list.sort((a, b) => (order.get(a.id) ?? Infinity) - (order.get(b.id) ?? Infinity));
  kept.current = list;
  return list;
}

function SessionView_({ s }: { s: SessionView }) {
  const full = useApp((st) => st.full);
  const detailsOpen = useApp((st) => st.detailsOpen);
  const sidebarHidden = useApp((st) => st.sidebarHidden);
  const terminals = useKeptTerminals(s);

  useSeen(s);

  return (
    <main class={`sess${full ? ' is-full' : ''}${sidebarHidden ? ' is-wide' : ''}`}>
      {!full && <Header s={s} />}
      <div class="sess__body">
        {terminals.map((t) => (
          <TerminalPane key={t.id} sessionKey={t.key} alive={t.key === s.key ? s.terminal : true} active={t.key === s.key} />
        ))}
        {!s.terminal && <HistoryCard s={s} />}
      </div>
      {full && <FullPill s={s} />}
      {detailsOpen && !full && <DetailsPanel s={s} />}
    </main>
  );
}

/** Сессия на экране и окно в фокусе — «новое» снимается */
export function useSeen(s: SessionView) {
  useEffect(() => {
    const check = () => {
      if (s.unseen && document.visibilityState === 'visible' && document.hasFocus()) markSeen(s.key);
    };
    check();
    window.addEventListener('focus', check);
    document.addEventListener('visibilitychange', check);
    return () => {
      window.removeEventListener('focus', check);
      document.removeEventListener('visibilitychange', check);
    };
  }, [s.key, s.unseen]);
}

/** Сессия идёт вне «Нити»: почему её нельзя продолжить здесь */
export function elsewhere(s: SessionView): string {
  const agent = AGENTS[s.agent]?.short ?? s.agent;
  return s.where === 'background'
    ? `${agent} ещё работает с этой сессией в фоне. Дождитесь конца, чтобы продолжить здесь.`
    : `${agent} ещё работает с этой сессией в другом терминале. Закройте его, чтобы продолжить здесь.`;
}

function statePill(s: SessionView) {
  if (s.state === 'idle') return <StatePill state="idle" label={`в истории · ${whenFull(s.updated)}`} />;
  if (s.state === 'working' || s.state === 'starting')
    return <StatePill state={s.state} elapsed={timer(s.since ?? s.updated)} label={s.state === 'starting' ? 'запускается' : undefined} />;
  return <StatePill state={s.state} isNew={isNew(s)} exitCode={s.exit_code} />;
}

function Header({ s }: { s: SessionView }) {
  const detailsOpen = useApp((st) => st.detailsOpen);
  const renaming = useApp((st) => st.renaming);
  const sidebarHidden = useApp((st) => st.sidebarHidden);
  const vscode = useApp((st) => !!st.env?.vscode);
  const waiting = useApp((st) => st.sessions);
  const hasNext = useMemo(() => waitingList(waiting).some((w) => w.key !== s.key), [waiting, s.key]);
  const [menu, setMenu] = useState<HTMLElement | null>(null);
  const [resuming, setResuming] = useState(false);
  useTick(s.state === 'working' || s.state === 'starting');

  const title = titleOf(s);
  const resumeReason = s.where
    ? elsewhere(s)
    : !s.resumable
      ? `${AGENTS[s.agent]?.short ?? s.agent} не умеет продолжать сессии`
      : s.missing
        ? 'Папки сессии нет'
        : '';

  return (
    <header class={`sess-head${sidebarHidden ? ' sess-head--wide' : ''}`}>
      {sidebarHidden && <IconButton icon="left_panel_open" label="Показать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />}
      <AgentBadge agent={s.agent} size={32} class="sess-head__agent" />
      {renaming ? (
        <RenameField s={s} />
      ) : (
        <Tooltip content={title} block placement="bottom-start">
          <button
            type="button"
            class={`sess-head__title${detailsOpen ? ' is-open' : ''}`}
            aria-expanded={detailsOpen}
            aria-label={`${title} · сведения`}
            onClick={() => store.set({ detailsOpen: !detailsOpen })}
          >
            <span class="sess-head__text">{title}</span>
          </button>
        </Tooltip>
      )}
      <div class="sess-head__where">
        <span>{s.project}</span>
        {s.branch && <span class="sess-head__branch">{s.branch}</span>}
        {s.worktree && <CopyBadge path={s.worktree.path} />}
      </div>
      {statePill(s)}
      <span class="sess-head__fill" />
      {s.terminal ? (
        <IconButton icon="open_in_full" label="Во весь экран" onClick={() => store.set({ full: true, detailsOpen: false })} />
      ) : (
        <Button
          variant="primary"
          icon="terminal"
          loading={resuming}
          disabled={!!resumeReason}
          tooltip={resumeReason || undefined}
          onClick={async () => {
            setResuming(true);
            await resume(s.key);
            setResuming(false);
          }}
        >
          Продолжить в терминале
        </Button>
      )}
      <IconButton
        icon="more_horiz"
        label="Меню сессии"
        pressed={!!menu}
        aria-haspopup="menu"
        aria-expanded={!!menu}
        onClick={(e) => setMenu(menu ? null : e.currentTarget)}
      />
      <Menu
        open={!!menu}
        anchor={menu}
        label="Меню сессии"
        placement="bottom-end"
        items={sessionMenu(s, {
          inHeader: true,
          vscode,
          hasNext,
          onDetails: () => store.set({ detailsOpen: true }),
          onRename: () => store.set({ renaming: true }),
        })}
        onClose={() => setMenu(null)}
      />
    </header>
  );
}

function RenameField({ s }: { s: SessionView }) {
  const ref = useRef<HTMLInputElement>(null);
  const [value, setValue] = useState(titleOf(s));
  const done = useRef(false);
  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);
  const finish = (save: boolean) => {
    if (done.current) return;
    done.current = true;
    store.set({ renaming: false });
    if (save) void rename(s.key, value);
  };
  return (
    <input
      ref={ref}
      class="sess-head__rename"
      aria-label="Название сессии"
      value={value}
      onInput={(e) => setValue(e.currentTarget.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          finish(true);
        } else if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          finish(false);
        }
      }}
      onBlur={() => finish(true)}
    />
  );
}

/** Сессия в истории: сводка, следующий шаг, переписка, команда и запуск */
function HistoryCard({ s }: { s: SessionView }) {
  const version = useSummaryVersion(s.key);
  const { details, error } = useDetails(s.key, true, `${s.updated}|${version}`);
  const command = details?.resume || details?.attach;
  return (
    <div class="hcard">
      {!details ? (
        <p class="hcard__wait">{error || ' '}</p>
      ) : (
        <>
          <div class="hcard__main">
            <SummaryBlock sessionKey={s.key} summary={details.summary} variant="card" />
            {details.summary?.next_step && <NextStep text={details.summary.next_step} variant="card" />}
            <Messages messages={details.messages ?? []} agent={s.agent} variant="card" limit={4} />
          </div>
          <div class="hcard__side">
            {s.where && <p class="hcard__note">{elsewhere(s)}</p>}
            {command && <CommandBox command={command} variant="card" />}
            <LaunchBlock details={details} session={s} />
          </div>
        </>
      )}
    </div>
  );
}

/** Сведения поверх терминала: панель 440 справа, терминал под ней не меняет размер */
function DetailsPanel({ s }: { s: SessionView }) {
  const version = useSummaryVersion(s.key);
  const { details, error } = useDetails(s.key, true, `${s.updated}|${version}`);
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    closeRef.current?.focus({ preventScroll: true });
  }, []);
  // Закрыли — фокус обратно на название, откуда сведения открываются
  const close = () => {
    store.set({ detailsOpen: false });
    document.querySelector<HTMLElement>('.sess-head__title')?.focus({ preventScroll: true });
  };
  const command = (s.terminal && details?.attach) || details?.resume || details?.attach;
  return (
    <aside
      class="details on-surface-2"
      aria-label="Сведения о сессии"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          close();
        }
      }}
    >
      <div class="details__head">
        <h2 class="details__title">Сведения о сессии</h2>
        <IconButton ref={closeRef} icon="close" label="Закрыть" shortcut={['Esc']} onClick={close} />
      </div>
      <div class="details__body">
        {!details ? (
          error && <p class="details__error">{error}</p>
        ) : (
          <>
            <SummaryBlock sessionKey={s.key} summary={details.summary} variant="panel" />
            {details.summary?.next_step && <NextStep text={details.summary.next_step} variant="panel" />}
            <Messages messages={details.messages ?? []} agent={s.agent} variant="panel" limit={6} />
            {command && <CommandBox command={command} variant="panel" />}
            {details.launch && <LaunchBlock details={details} session={s} />}
          </>
        )}
      </div>
    </aside>
  );
}

/** Полный экран: плашка справа вверху — сессия, ждущие, выход */
function FullPill({ s }: { s: SessionView }) {
  const sessions = useApp((st) => st.sessions);
  const others = useMemo(() => waitingList(sessions).filter((w) => w.key !== s.key).length, [sessions, s.key]);
  const title = titleOf(s);
  return (
    <div class="fpill on-surface-2">
      <AgentBadge agent={s.agent} size={28} raised />
      <Tooltip content={title} block>
        <span class="fpill__title" tabIndex={-1}>
          {title}
        </span>
      </Tooltip>
      <StatusMark state={s.state} isNew={isNew(s)} class="fpill__mark" />
      <span class="fpill__sep" />
      {others > 0 && (
        <Button variant="quiet" size={36} icon="keyboard_arrow_down" class="fpill__next" tooltip="Следующая ждущая" shortcut={['Alt', '↓']} onClick={goNextWaiting}>
          Ждут ещё {others}
        </Button>
      )}
      <IconButton icon="close_fullscreen" label="Выйти из полного экрана" size={36} onClick={() => store.set({ full: false })} />
    </div>
  );
}
