// Части сведений о сессии: сводка, следующий шаг, переписка, команда, запуск.
// Одни и те же в карточке сессии в истории (card) и в панели сведений (panel).
import { useState } from 'preact/hooks';
import type { Details, MessageView, SessionView, Summary } from '../api/types';
import { Button, IconButton } from '../components';
import { AGENTS } from '../model';
import { copyText } from '../app/actions';
import { dismissJob, shownJob, startSummary, summaryJob } from '../app/jobs';
import { agentShort } from '../app/runners';
import { useApp } from '../app/state';
import { clock, whenFull } from '../lib/format';
import { effortLabel } from './Home';
import { AgoLong, JobFailure, JobProgress } from './JobStatus';

type Variant = 'card' | 'panel';

/**
 * Версия сведений для useDetails: сводка по сессии сохраняется до последнего события job,
 * поэтому после готового задания сведения перечитываются. synced — после переподключения
 */
export function useSummaryVersion(key: string): string {
  const doneId = useApp((s) => {
    const j = summaryJob(s, key);
    return j?.stage === 'done' ? j.id : '';
  });
  const synced = useApp((s) => s.synced);
  return `${doneId}|${synced}`;
}

/** Сводка по сессии: текст, кто и когда составил, «Обновить», ход задания и ошибка */
export function SummaryBlock({ sessionKey, summary, variant }: { sessionKey: string; summary: Summary | undefined; variant: Variant }) {
  const job = useApp((s) => shownJob(summaryJob(s, sessionKey)));
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const running = !!job && !job.done;
  const refresh = async () => {
    // Новая попытка: прошлая ошибка больше не нужна
    if (job?.stage === 'failed') dismissJob(job.id);
    setStarting(true);
    setError('');
    const e = await startSummary(sessionKey);
    setStarting(false);
    setError(e);
  };
  return (
    <section class={`si-summary si-summary--${variant}`}>
      <div class="si-head">
        <h3 class="si-label si-head__label">Сводка</h3>
        {summary && (
          <span class="si-head__meta">
            {agentShort(summary.model)} · <AgoLong at={summary.at} />
          </span>
        )}
        {!running && (
          <IconButton icon="restart_alt" label={summary ? 'Обновить сводку' : 'Составить сводку'} size={28} loading={starting} onClick={refresh} />
        )}
      </div>
      {running && <JobProgress job={job} variant={variant} />}
      {job?.stage === 'failed' && (
        <JobFailure error={job.error ?? ''} output={job.output} variant={variant} onClose={() => dismissJob(job.id)} />
      )}
      {error && <JobFailure error={error} variant={variant} onClose={() => setError('')} />}
      {summary?.summary ? (
        <p class="si-summary__text">{summary.summary}</p>
      ) : (
        !running && <p class="si-summary__text si-summary__text--empty">Сводки пока нет.</p>
      )}
    </section>
  );
}

export function NextStep({ text, variant }: { text: string; variant: Variant }) {
  const copy = () => void copyText(`Продолжи: ${text}`, 'Скопировано для агента');
  return (
    <div class={`si-next si-next--${variant}`}>
      <div class="si-next__body">
        <span class="si-next__label">Следующий шаг</span>
        <span class="si-next__text">{text}</span>
      </div>
      {variant === 'card' ? (
        <Button variant="quiet" size={32} icon="content_copy" class="si-next__copy" onClick={copy}>
          Для агента
        </Button>
      ) : (
        <IconButton icon="content_copy" label="Скопировать для агента" size={32} class="si-next__copy" onClick={copy} />
      )}
    </div>
  );
}

export function Messages({ messages, agent, variant, limit }: { messages: MessageView[]; agent: string; variant: Variant; limit: number }) {
  const shown = messages.slice(-limit);
  if (!shown.length) return null;
  const label = variant === 'card' ? `Переписка · последние ${shown.length}` : 'Переписка';
  return (
    <section class={`si-msgs si-msgs--${variant}`}>
      <h3 class="si-label si-msgs__label">{label}</h3>
      {shown.map((m, i) => {
        const who = m.role === 'user' ? 'Вы' : agentShort(agent);
        return variant === 'card' ? (
          <div key={i} class={`si-msg si-msg--card si-msg--${m.role}`}>
            <div class="si-msg__who">
              <span class="si-msg__name">{who}</span>
              {m.at ? <span class="si-msg__time">{clock(m.at)}</span> : null}
            </div>
            <span class="si-msg__text">{m.text}</span>
          </div>
        ) : (
          <div key={i} class={`si-msg si-msg--panel si-msg--${m.role}`}>
            <span class="si-msg__name">
              {who}
              {m.at ? ` · ${clock(m.at)}` : ''}
            </span>
            <span class="si-msg__text">{m.text}</span>
          </div>
        );
      })}
    </section>
  );
}

export function CommandBox({ command, variant }: { command: string; variant: Variant }) {
  return (
    <section class={`si-cmd si-cmd--${variant}`}>
      <h3 class="si-label">Подключиться из другого терминала</h3>
      <div class="si-cmd__box">
        <code class="si-cmd__text">{command}</code>
        <IconButton icon="content_copy" label="Скопировать" size={32} onClick={() => void copyText(command, 'Команда скопирована')} />
      </div>
    </section>
  );
}

export function LaunchBlock({ details, session }: { details: Details; session: SessionView }) {
  const l = details.launch;
  const agent = AGENTS[session.agent]?.short ?? session.agent;
  const model = l?.model ? `модель ${l.model}` : 'модель как в CLI';
  const effort = l?.effort ? ` · рассуждения ${effortLabel(l.effort)}` : '';
  return (
    <section class="si-launch">
      <h3 class="si-label si-launch__label">Запуск</h3>
      <span>
        {whenFull(l?.at ?? session.created)} · {l ? 'из «Нити»' : 'вне «Нити»'}
      </span>
      <span>
        {agent} · {model}
        {effort}
      </span>
    </section>
  );
}
