import { useMemo } from 'preact/hooks';
import type { SessionView } from '../api/types';
import { Icon, IconButton, NewBadge, StatusMark } from '../components';
import { AGENTS } from '../model';
import { openNew, openSession } from '../app/actions';
import { isNew, projectTree, titleOf, waitingList, workingList, type ProjectNode } from '../app/derive';
import { navigate } from '../app/route';
import { canLaunch, store, useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { ago, timer } from '../lib/format';
import { PhoneHeader } from './PhoneHeader';

/** Телефон, список: «Ждут вас» карточками, «Работают», «Проекты» */
export function PhoneList() {
  const sessions = useApp((s) => s.sessions);
  const projects = useApp((s) => s.projects);
  const keepNew = useApp((s) => s.keepNew);
  const launchOk = useApp(canLaunch);
  const waiting = useMemo(() => waitingList(sessions, keepNew), [sessions, keepNew]);
  const working = useMemo(() => workingList(sessions), [sessions]);
  const tree = useMemo(() => projectTree(projects, sessions), [projects, sessions]);

  return (
    <div class="ph-screen">
      <PhoneHeader
        size="list"
        title="Нить"
        actions={
          <>
            <IconButton icon="search" label="Поиск" size={48} noTooltip onClick={() => store.set({ searchOpen: true })} />
            <IconButton
              icon="add"
              label="Новая сессия"
              size={48}
              variant="primary"
              noTooltip
              disabled={!launchOk}
              onClick={() => openNew()}
            />
          </>
        }
      />
      <div class="ph-scroll ph-list">
        {waiting.length > 0 && (
          <section aria-label="Ждут вас">
            <h2 class="ph-sechead ph-sechead--waiting">
              Ждут вас<span class="ph-sechead__count">{waiting.length}</span>
            </h2>
            <div class="ph-cards">
              {waiting.map((s) => (
                <WaitingCard key={s.key} s={s} />
              ))}
            </div>
          </section>
        )}
        {working.length > 0 && (
          <section aria-label="Работают">
            <h2 class="ph-sechead">
              Работают<span class="ph-sechead__count">{working.length}</span>
            </h2>
            {working.map((s) => (
              <WorkingRow key={s.key} s={s} />
            ))}
          </section>
        )}
        {tree.length > 0 && (
          <section aria-label="Проекты">
            <h2 class="ph-sechead">
              Проекты<span class="ph-sechead__count">{tree.length}</span>
            </h2>
            <div class="ph-projects">
              {tree.map((n) => (
                <ProjectRow key={n.project.path} n={n} />
              ))}
            </div>
          </section>
        )}
        {!waiting.length && !working.length && !tree.length && (
          <p class="ph-empty">{launchOk ? 'Сессий пока нет' : 'Сначала установите tmux и агента'}</p>
        )}
      </div>
    </div>
  );
}

function WaitingCard({ s }: { s: SessionView }) {
  useTick();
  const error = s.state === 'error';
  return (
    <button type="button" class="ph-card" onClick={() => openSession(s.key)}>
      <span class="ph-mark">
        <StatusMark state={s.state} isNew={isNew(s)} size="md" />
      </span>
      <span class="ph-card__body">
        <span class="ph-card__line1">
          <span class="ph-card__title">{titleOf(s)}</span>
          <span class={`ph-card__time${error ? '' : ' is-waiting'}`}>{ago(s.since ?? s.updated)}</span>
        </span>
        {error ? (
          <span class="ph-card__error">{s.exit_code === undefined ? 'Ошибка' : `Ошибка · код ${s.exit_code}`}</span>
        ) : (
          s.last && <span class="ph-card__text">{s.last}</span>
        )}
        <span class="ph-card__meta">
          {AGENTS[s.agent]?.short ?? s.agent} · {s.project}
          {isNew(s) && <NewBadge size={18} />}
        </span>
      </span>
    </button>
  );
}

function WorkingRow({ s }: { s: SessionView }) {
  useTick();
  return (
    <button type="button" class="ph-work" onClick={() => openSession(s.key)}>
      <span class="ph-mark">
        <StatusMark state={s.state} size="md" />
      </span>
      <span class="ph-work__body">
        <span class="ph-work__line1">
          <span class="ph-work__title">{titleOf(s)}</span>
          <span class="ph-work__time">{timer(s.since ?? s.updated)}</span>
        </span>
        {s.state === 'starting' ? (
          <span class="ph-work__cmd">Запускается…</span>
        ) : (
          s.last_line && <span class="ph-work__cmd">{s.last_line}</span>
        )}
      </span>
    </button>
  );
}

function ProjectRow({ n }: { n: ProjectNode }) {
  const p = n.project;
  return (
    <button type="button" class="ph-proj" onClick={() => navigate(`#/history?project=${encodeURIComponent(p.path)}`)}>
      <span class="ph-proj__name">{p.name}</span>
      {n.marks.map((m, i) => (
        <StatusMark key={i} state={m.state} size="md" />
      ))}
      <Icon name="chevron_right" size={18} class="ph-chev" />
    </button>
  );
}
