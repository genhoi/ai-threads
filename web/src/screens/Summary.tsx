import { useEffect, useRef, useState } from 'preact/hooks';
import { errorText } from '../api/client';
import type { Digest, DigestDays, JobView } from '../api/types';
import { Button, Chip, Icon, IconButton, Menu, Segmented, toast } from '../components';
import type { AgentId } from '../model';
import { toggleSidebar } from '../app/actions';
import { digestJob, dismissJob, loadDigest, shownJob, startDigest } from '../app/jobs';
import { replaceRoute } from '../app/route';
import { agentName, runnerMenu } from '../app/runners';
import { sessionHash, useApp } from '../app/state';
import { Markup } from '../lib/markup';
import { plural } from '../lib/format';
import { AgoLong, JobFailure, JobProgress } from './JobStatus';

const PERIODS = [
  { value: '3', label: '3 дня' },
  { value: '5', label: '5 дней' },
  { value: '7', label: 'Неделя' },
] as const;

/** Период из адреса #/summary?days=5; по умолчанию 3 дня */
function daysOf(raw: string | undefined): DigestDays {
  return raw === '5' ? 5 : raw === '7' ? 7 : 3;
}

const PERIOD_WORDS: Record<DigestDays, string> = { 3: 'за 3 дня', 5: 'за 5 дней', 7: 'за неделю' };

// Кого выбрали в чипе «Кто пишет»: до перезагрузки страницы, на всех периодах
let chosenRunner: string | null = null;

/** Сводка за несколько дней: период, кто пишет, «Обновить» и сама сводка статьёй */
export function SummaryPage({ phone }: { phone?: boolean }) {
  const query = useApp((s) => s.route.query);
  const days = daysOf(query.days);
  const sidebarHidden = useApp((s) => s.sidebarHidden);
  const digest = useApp((s) => s.digests[days]);
  const job = useApp((s) => shownJob(digestJob(s, days)));
  const synced = useApp((s) => s.synced);
  const agents = useApp((s) => s.agents);
  const fromSettings = useApp((s) => s.settings?.digest.agent ?? 'auto');
  const [runner, setRunner] = useState(chosenRunner ?? fromSettings);
  const [menuOpen, setMenuOpen] = useState(false);
  const chip = useRef<HTMLButtonElement>(null);
  const [loadError, setLoadError] = useState('');
  const [starting, setStarting] = useState(false);
  const running = !!job && !job.done;

  // Настройки пришли позже страницы: выбор из них, пока человек сам не выбрал
  useEffect(() => {
    if (chosenRunner === null) setRunner(fromSettings);
  }, [fromSettings]);

  // Сводка за период и задание, которое шло до открытия страницы; после переподключения — заново
  useEffect(() => {
    const ctl = new AbortController();
    setLoadError('');
    loadDigest(days, ctl.signal).catch((e) => {
      if ((e as Error).name !== 'AbortError') setLoadError(errorText(e));
    });
    return () => ctl.abort();
  }, [days, synced]);

  const refresh = async () => {
    if (job?.stage === 'failed') dismissJob(job.id);
    setStarting(true);
    const error = await startDigest(days, runner);
    setStarting(false);
    if (error) toast.show({ kind: 'error', title: 'Сводка не запустилась', text: error });
  };

  const choose = (id: string) => {
    chosenRunner = id;
    setRunner(id);
  };

  const meta = digest ? (
    <span class="sum__meta">
      {digest.journals > 0 && `По ${digest.journals} ${plural(digest.journals, 'журналу', 'журналам', 'журналам')} · `}
      <AgoLong at={digest.at} />
    </span>
  ) : null;

  const controls = (
    <>
      <Segmented<string>
        label="Период"
        value={String(days)}
        options={PERIODS}
        onChange={(v) => replaceRoute(v === '3' ? '#/summary' : `#/summary?days=${v}`)}
      />
      <span class="sum__fill" />
      {!phone && meta}
      {runner === 'auto' ? (
        <Chip ref={chip} value="Авто" open={menuOpen} title="Какой агент пишет сводку" aria-label="Кто пишет: Авто" onClick={() => setMenuOpen(!menuOpen)} />
      ) : (
        <Chip
          ref={chip}
          agent={runner as AgentId}
          open={menuOpen}
          title="Какой агент пишет сводку"
          aria-label={`Кто пишет: ${agentName(runner)}`}
          onClick={() => setMenuOpen(!menuOpen)}
        />
      )}
      <Menu
        open={menuOpen}
        anchor={chip.current}
        label="Кто пишет"
        width={260}
        gap={10}
        placement="bottom-end"
        items={runnerMenu(runner, agents, choose)}
        activeId={runner}
        onClose={() => setMenuOpen(false)}
      />
      <Button
        variant="regular"
        icon="restart_alt"
        loading={starting}
        disabled={running}
        tooltip={running ? 'Сводка уже составляется' : undefined}
        onClick={refresh}
      >
        Обновить
      </Button>
    </>
  );

  return (
    <main class={`page sum${phone ? ' sum--phone' : ''}${sidebarHidden ? ' is-wide' : ''}`}>
      {phone ? (
        <div class="sum__bar">
          {controls}
          {meta}
        </div>
      ) : (
        <header class={`page__head sum__head${sidebarHidden ? ' page__head--wide' : ''}`}>
          {sidebarHidden && <IconButton icon="left_panel_open" label="Показать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />}
          <h1 class="page__title sum__title">Сводка</h1>
          {controls}
        </header>
      )}
      <div class="sum__sheet on-surface-1">
        <div class="sum__inner">
          <Body days={days} digest={digest} job={job} loadError={loadError} starting={starting} onStart={refresh} />
        </div>
      </div>
    </main>
  );
}

function Body({
  days,
  digest,
  job,
  loadError,
  starting,
  onStart,
}: {
  days: DigestDays;
  digest: Digest | null | undefined;
  job: JobView | undefined;
  loadError: string;
  starting: boolean;
  onStart: () => void;
}) {
  const status = job && (job.done ? <JobFailure error={job.error ?? ''} output={job.output} variant="page" onClose={() => dismissJob(job.id)} /> : <JobProgress job={job} variant="page" />);

  if (digest === undefined) {
    if (!loadError) return <Icon name="progress_activity" size={18} spin class="sum__loading" />;
    return (
      <div class="sum-empty">
        <h2 class="sum-empty__title">Сводка не загрузилась</h2>
        <p class="sum-empty__text">{loadError}</p>
      </div>
    );
  }

  if (!digest) {
    return (
      <div class="sum-empty">
        {status}
        {job && !job.done ? (
          <p class="sum-empty__text">Обычно это несколько минут. Страницу можно закрыть: агент продолжит работу.</p>
        ) : (
          <>
            <h2 class="sum-empty__title">Сводки {PERIOD_WORDS[days]} ещё нет</h2>
            <p class="sum-empty__text">Агент прочитает журналы сессий за эти дни и напишет, что сделано в каждом проекте и что осталось.</p>
            <Button variant="primary" icon="summarize" loading={starting} onClick={onStart}>
              Составить сводку
            </Button>
          </>
        )}
      </div>
    );
  }

  return (
    <>
      {status && <div class="sum__status">{status}</div>}
      <Article d={digest} />
    </>
  );
}

/** Сводка статьёй: заголовок, вводный абзац, по разделу на проект и «Осталось.» */
function Article({ d }: { d: Digest }) {
  return (
    <article class="dg">
      <h1 class="dg__title">{d.title}</h1>
      {d.legacy && (
        <p class="dg__note">
          <Icon name="info" size={16} />
          Сводка из первой версии «Нити». «Обновить» составит новую.
        </p>
      )}
      {d.lead && (
        <p class="dg__lead">
          <Markup text={d.lead} href={sessionHash} />
        </p>
      )}
      {d.projects.map((p, i) => (
        <section key={`${i}-${p.name}`} class="dg__project" aria-label={p.name}>
          <h2 class="dg__name">{p.name}</h2>
          {p.items.length > 0 && (
            <ul class="dg__items">
              {p.items.map((it, j) => (
                <li key={j}>
                  <Markup text={it.text} href={sessionHash} />
                </li>
              ))}
            </ul>
          )}
          {p.left && (
            <p class="dg__left">
              <strong class="md-bold">Осталось.</strong> <Markup text={p.left} href={sessionHash} />
            </p>
          )}
        </section>
      ))}
    </article>
  );
}
