import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { errorText } from '../api/client';
import {
  fullSearch,
  mergeHits,
  onSearchEvent,
  quickSearch,
  searchDone,
  searchFinished,
  type SearchHit,
  type SearchView,
} from '../api/search';
import type { SessionView } from '../api/types';
import { Dialog, FilterChip, Icon, Kbd, KbdCombo, StatusMark } from '../components';
import { AGENTS } from '../model';
import { openSession } from '../app/actions';
import { isNew, titleOf } from '../app/derive';
import { store, useApp } from '../app/state';
import { plural, when } from '../lib/format';

export function SearchPalette() {
  const open = useApp((s) => s.searchOpen);
  const close = () => store.set({ searchOpen: false });
  // Содержимое создаётся заново при каждом открытии: окно снимается с экрана после закрытия
  return (
    <Dialog open={open} onClose={close} bare top={104} width={680} label="Поиск" class="search">
      <SearchBody onClose={close} />
    </Dialog>
  );
}

interface FullState {
  id: string;
  done: number;
  total: number;
  finished: boolean;
  hits: SearchHit[];
  error?: string;
}

const LABELS: Record<string, string> = { conversation: 'в переписке', summary: 'в сводке', log: 'в журнале' };

function SearchBody({ onClose }: { onClose: () => void }) {
  const sessions = useApp((s) => s.sessions);
  const [q, setQ] = useState('');
  const [conv, setConv] = useState(true);
  const [service, setService] = useState(false);
  const [view, setView] = useState<SearchView | null>(null);
  // На какой запрос ответ: пока новый не пришёл, виден прежний список, но без числа
  const [viewQ, setViewQ] = useState('');
  const [error, setError] = useState('');
  const [active, setActive] = useState(0);
  const [full, setFull] = useState<FullState | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const query = q.trim();

  // Быстрый поиск: после паузы в наборе, прошлый запрос отменяется
  useEffect(() => {
    setFull(null);
    if (!query) {
      setView(null);
      setError('');
      return;
    }
    const ctl = new AbortController();
    const t = window.setTimeout(() => {
      quickSearch(query, { conv, service }, ctl.signal)
        .then((v) => {
          setView(v);
          setViewQ(query);
          setError('');
          setActive(0);
        })
        .catch((e) => {
          if ((e as Error).name !== 'AbortError') setError(errorText(e));
        });
    }, 120);
    return () => {
      window.clearTimeout(t);
      ctl.abort();
    };
  }, [query, conv, service]);

  // Ход полного поиска приходит событием search
  useEffect(
    () =>
      onSearchEvent((e) => {
        setFull((f) =>
          f && f.id === e.id
            ? { ...f, done: searchDone(e), total: e.total, finished: searchFinished(e), hits: mergeHits(f.hits, e.hits) }
            : f,
        );
      }),
    [],
  );

  // Пустой запрос — недавние сессии
  const recent = useMemo<SearchHit[]>(
    () =>
      Object.values(sessions)
        .filter((s) => !s.hidden)
        .sort((a, b) => b.updated - a.updated)
        .slice(0, 6)
        .map((session) => ({ session })),
    [sessions],
  );
  const hits = query ? (view?.hits ?? []) : recent;
  const fullHits = (full?.hits ?? []).filter((h) => !hits.some((x) => x.session.key === h.session.key));
  const rows: ({ kind: 'hit'; hit: SearchHit } | { kind: 'full' })[] = [
    ...hits.map((hit) => ({ kind: 'hit' as const, hit })),
    ...(query ? [{ kind: 'full' as const }] : []),
    ...fullHits.map((hit) => ({ kind: 'hit' as const, hit })),
  ];
  const current = Math.min(active, Math.max(0, rows.length - 1));

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-row="${current}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [current]);

  const startFull = async () => {
    if (!query) return;
    try {
      const { id } = await fullSearch(query, { service });
      setFull({ id, done: 0, total: view?.scope ?? 0, finished: false, hits: [] });
    } catch (e) {
      setFull({ id: '', done: 0, total: 0, finished: true, hits: [], error: errorText(e) });
    }
  };

  const openHit = (h: SearchHit) => {
    const st = store.get();
    if (!st.sessions[h.session.key]) store.set({ extra: { ...st.extra, [h.session.key]: h.session } });
    onClose();
    openSession(h.session.key);
  };

  const activate = (i: number) => {
    const row = rows[i];
    if (!row) return;
    if (row.kind === 'full') void startFull();
    else openHit(row.hit);
  };

  const scope = view?.scope ?? Object.keys(sessions).length;
  let index = 0;
  const hitRow = (hit: SearchHit) => {
    const i = index++;
    return <HitRow key={hit.session.key} i={i} hit={hit} q={query} active={i === current} onHover={() => setActive(i)} onOpen={() => activate(i)} />;
  };

  return (
    <div class="search__panel">
      <div class="search__head">
        <Icon name="search" size={18} class="search__icon" />
        <input
          class="search__input"
          data-autofocus
          role="combobox"
          aria-expanded="true"
          aria-controls="search-list"
          aria-activedescendant={rows.length ? `search-row-${current}` : undefined}
          aria-label="Поиск сессий"
          placeholder="Название, ветка, задача, путь"
          value={q}
          spellcheck={false}
          autocomplete="off"
          onInput={(e) => {
            setQ(e.currentTarget.value);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault();
              setActive((a) => Math.min(rows.length - 1, a + 1));
            } else if (e.key === 'ArrowUp') {
              e.preventDefault();
              setActive((a) => Math.max(0, a - 1));
            } else if (e.key === 'Enter') {
              e.preventDefault();
              activate(current);
            }
          }}
        />
        <Kbd>Esc</Kbd>
      </div>
      <div class="search__filters on-surface-1">
        <span class="search__also">Искать также</span>
        <FilterChip label="В переписке" on={conv} onChange={setConv} />
        <FilterChip label="Служебные" on={service} onChange={setService} />
      </div>
      <div class="search__list" id="search-list" ref={listRef} role="listbox" aria-label="Найденные сессии">
        <div class="search__label">{!query ? 'Недавние' : viewQ === query ? `Сессии · ${hits.length}` : 'Сессии'}</div>
        {error && <div class="search__empty">{error}</div>}
        {query && view && viewQ === query && !hits.length && !error && <div class="search__empty">Ничего не нашлось</div>}
        {hits.map(hitRow)}
        {query && (
          <>
            <div class="search__sep" />
            <FullRow
              i={index++}
              active={current === hits.length}
              full={full}
              scope={scope}
              seconds={view?.seconds}
              onHover={() => setActive(hits.length)}
              onStart={() => void startFull()}
            />
          </>
        )}
        {fullHits.length > 0 && (
          <>
            <div class="search__label">В журналах · {fullHits.length}</div>
            {fullHits.map(hitRow)}
          </>
        )}
      </div>
      <div class="search__foot">
        <span class="search__key">
          <KbdCombo keys={['↑', '↓']} />
          выбрать
        </span>
        <span class="search__key">
          <Kbd>Enter</Kbd>
          открыть
        </span>
        <span class="search__key">
          <Kbd>Esc</Kbd>
          закрыть
        </span>
      </div>
    </div>
  );
}

/** Подсветить совпадения: весь запрос, а если его нет — отдельные слова */
function highlight(text: string, q: string, cls: string): ComponentChildren {
  if (!q) return text;
  const words = [q, ...q.split(/\s+/).filter((w) => w.length > 1)];
  const lower = text.toLowerCase();
  const word = words.find((w) => lower.includes(w.toLowerCase()));
  if (!word) return text;
  const out: ComponentChildren[] = [];
  const w = word.toLowerCase();
  let from = 0;
  let at = lower.indexOf(w);
  while (at !== -1) {
    if (at > from) out.push(text.slice(from, at));
    out.push(
      <mark key={at} class={cls}>
        {text.slice(at, at + w.length)}
      </mark>,
    );
    from = at + w.length;
    at = lower.indexOf(w, from);
  }
  out.push(text.slice(from));
  return out;
}

function Mark({ s }: { s: SessionView }) {
  if (s.done || s.state === 'exited') return <Icon name="check" size={14} class="search__check" label="завершена" />;
  if (s.state === 'idle') return <span class="search__nomark" />;
  return <StatusMark state={s.state} isNew={isNew(s)} />;
}

interface HitRowProps {
  i: number;
  hit: SearchHit;
  q: string;
  active: boolean;
  onHover: () => void;
  onOpen: () => void;
}

function HitRow({ i, hit, q, active, onHover, onOpen }: HitRowProps) {
  const s = hit.session;
  const agent = AGENTS[s.agent]?.short ?? s.agent;
  const branch = s.branch && !['main', 'master'].includes(s.branch) ? s.branch : '';
  const where = hit.field ? LABELS[hit.field] : undefined;
  return (
    <div
      id={`search-row-${i}`}
      class={`search__row${active ? ' is-active' : ''}`}
      role="option"
      aria-selected={active}
      data-row={i}
      onPointerMove={onHover}
      onClick={onOpen}
    >
      <span class="search__mark">
        <Mark s={s} />
      </span>
      <span class="search__body">
        <span class="search__title">{highlight(titleOf(s), q, 'search__hl')}</span>
        <span class="search__meta">
          {agent} · {s.project}
          {branch && (
            <>
              {' · '}
              <span class="search__branch">{branch}</span>
            </>
          )}
          {' · '}
          {s.done ? `завершена ${when(s.updated)}` : when(s.updated)}
          {where && hit.snippet && (
            <>
              {` · ${where}: «`}
              {highlight(hit.snippet, q, 'search__hl-text')}
              {'»'}
            </>
          )}
        </span>
      </span>
      {active && (
        <span class="search__open">
          Открыть
          <Kbd>Enter</Kbd>
        </span>
      )}
    </div>
  );
}

interface FullRowProps {
  i: number;
  active: boolean;
  full: FullState | null;
  scope: number;
  seconds?: number;
  onHover: () => void;
  onStart: () => void;
}

function FullRow({ i, active, full, scope, seconds, onHover, onStart }: FullRowProps) {
  let title = 'Искать во всех журналах';
  let sub = `Если нужного нет выше. Точное совпадение в ${scope} ${plural(scope, 'сессии', 'сессиях', 'сессиях')}`;
  if (seconds) sub += `, около ${seconds} ${plural(seconds, 'секунды', 'секунд', 'секунд')}`;
  let icon: 'manage_search' | 'progress_activity' = 'manage_search';
  if (full?.error) {
    sub = full.error;
  } else if (full && !full.finished) {
    title = 'Ищу во всех журналах';
    icon = 'progress_activity';
    sub = `Просмотрено ${full.done}${full.total ? ` из ${full.total}` : ''}`;
  } else if (full) {
    title = 'Поиск по журналам закончен';
    sub = `Просмотрено ${full.done} ${plural(full.done, 'сессия', 'сессии', 'сессий')} · найдено ${full.hits.length}`;
  }
  return (
    <div
      id={`search-row-${i}`}
      class={`search__row search__row--full${active ? ' is-active' : ''}`}
      role="option" aria-selected={active} data-row={i} onPointerMove={onHover} onClick={onStart}>
      <Icon name={icon} size={18} spin={icon === 'progress_activity'} class="search__full-icon" />
      <span class="search__body">
        <span class="search__full-title">{title}</span>
        <span class="search__meta">{sub}</span>
      </span>
    </div>
  );
}
