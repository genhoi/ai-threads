import type { SessionView } from '../api/types';
import { AgentBadge, Button, Icon, IconButton } from '../components';
import type { AgentId } from '../model';
import { openNew, openSession } from '../app/actions';
import { titleOf } from '../app/derive';
import { canLaunch, useApp } from '../app/state';
import { clock, sessionsCount } from '../lib/format';
import { HistoryMark, historyTail, historyWho } from '../screens/History';
import { byDay, filtersOf, projectName, useHistory, useInfinite, useLive } from '../screens/useHistory';
import { goBack } from './nav';
import { PhoneHeader } from './PhoneHeader';

/** Телефон: сессии проекта или вся история, по дням */
export function PhoneHistory() {
  const query = useApp((s) => s.route.query);
  const launchOk = useApp(canLaunch);
  const f = filtersOf(query);
  const list = useHistory(f);
  const items = useLive(list.items);
  const groups = byDay(items);
  const sentinel = useInfinite(list.more, !list.done && !list.error, list.items.length);
  const title = f.project ? projectName(f.project) : 'История';

  return (
    <div class="ph-screen">
      <PhoneHeader
        back={() => goBack('#/')}
        title={title}
        sub={list.total !== null ? sessionsCount(list.total) : undefined}
        actions={
          f.project && (
            <IconButton
              icon="add"
              label="Новая сессия в проекте"
              size={48}
              noTooltip
              disabled={!launchOk}
              onClick={() => openNew(f.project)}
            />
          )
        }
      />
      <div class="ph-scroll">
        {groups.map((g) => (
          <section key={g.key} aria-label={g.title}>
            <h2 class="ph-sechead ph-sechead--day">{g.title}</h2>
            {g.items.map((s) => (
              <Row key={s.key} s={s} />
            ))}
          </section>
        ))}
        {list.error ? (
          <div class="ph-state" role="alert">
            <span class="hist__error">{list.error}</span>
            <Button variant="regular" size={40} icon="restart_alt" onClick={list.retry}>
              Повторить
            </Button>
          </div>
        ) : list.loading ? (
          <div class="ph-state" role="status" aria-label="Загрузка">
            <Icon name="progress_activity" size={18} spin />
          </div>
        ) : (
          list.done && !items.length && <p class="ph-empty">Сессий пока нет</p>
        )}
        <div ref={sentinel} class="hist__sentinel" />
      </div>
    </div>
  );
}

function Row({ s }: { s: SessionView }) {
  const tail = historyTail(s);
  return (
    <button type="button" class={`ph-hrow${s.done ? ' is-done' : ''}`} onClick={() => openSession(s.key)}>
      <span class="ph-mark">
        <HistoryMark s={s} size="md" />
      </span>
      <span class="ph-hrow__body">
        <span class="ph-hrow__line1">
          <AgentBadge agent={s.agent as AgentId} size={16} class="hrow__agent" />
          <span class="ph-hrow__title">{titleOf(s)}</span>
          <span class="ph-hrow__time">{clock(s.updated)}</span>
        </span>
        <span class="ph-hrow__meta">
          <span class="hrow__who">
            {historyWho(s)}
            {tail ? ' · ' : ''}
          </span>
          {tail && <span class={tail.error ? 'hrow__error' : undefined}>{tail.text}</span>}
        </span>
      </span>
    </button>
  );
}
