// Ход и ошибка задания сводки: одни и те же на странице «Сводка», в сведениях о сессии и в карточке истории.
import { useState } from 'preact/hooks';
import type { JobView } from '../api/types';
import { Button, Icon, IconButton, toast } from '../components';
import { cancelJob } from '../app/jobs';
import { agentShort } from '../app/runners';
import { useTick } from '../lib/clock';
import { agoLong, timer } from '../lib/format';

/** Что сейчас делает агент: «Kimi читает журналы · прочитано 12» */
export function stageText(j: JobView): string {
  const who = agentShort(j.agent);
  const digest = j.kind === 'digest';
  switch (j.stage) {
    case 'starting':
      return digest ? `Готовлю журналы и запускаю ${who}` : `Запускаю ${who}`;
    case 'reading':
      return digest ? `${who} читает журналы${j.reads ? ` · прочитано ${j.reads}` : ''}` : `${who} читает журнал сессии`;
    case 'writing':
      return `${who} пишет сводку`;
    case 'failed':
      return 'Сводка не составилась';
    case 'cancelled':
      return 'Остановлено';
    default:
      return 'Готово';
  }
}

type Variant = 'page' | 'card' | 'panel';

/** Задание идёт: что делает агент, сколько прошло и «Остановить» */
export function JobProgress({ job, variant }: { job: JobView; variant: Variant }) {
  useTick(!job.done);
  const [stopping, setStopping] = useState(false);
  const stop = async () => {
    setStopping(true);
    const error = await cancelJob(job.id);
    setStopping(false);
    if (error) toast.show({ kind: 'error', title: 'Не получилось остановить', text: error });
  };
  return (
    <div class={`job job--${variant}`}>
      <Icon name="progress_activity" size={16} spin class="job__spin" />
      <span class="job__text" role="status">
        {stageText(job)}
      </span>
      <span class="job__time">{timer(job.started)}</span>
      <Button variant="quiet" size={32} icon="close" class="job__stop" loading={stopping} onClick={stop}>
        Остановить
      </Button>
    </div>
  );
}

/** Сводка не составилась: причина и, если есть, хвост вывода агента под «Вывод агента» */
export function JobFailure({
  error,
  output,
  variant,
  onClose,
}: {
  error: string;
  output?: string;
  variant: Variant;
  onClose?: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div class={`jobfail jobfail--${variant}`} role="alert">
      <div class="jobfail__head">
        <Icon name="error" size={16} class="jobfail__icon" />
        <span class="jobfail__title">Сводка не составилась</span>
        {onClose && <IconButton icon="close" label="Убрать" size={28} class="jobfail__close" onClick={onClose} />}
      </div>
      {error && <p class="jobfail__text">{error}</p>}
      {output && (
        <>
          <button type="button" class={`jobfail__toggle${open ? ' is-open' : ''}`} aria-expanded={open} onClick={() => setOpen(!open)}>
            Вывод агента
            <Icon name="expand_more" size={16} class="jobfail__chev" />
          </button>
          {open && <pre class="jobfail__out">{output}</pre>}
        </>
      )}
    </div>
  );
}

/** «10 мин назад», обновляется само */
export function AgoLong({ at }: { at: number }) {
  useTick();
  return <>{agoLong(at)}</>;
}
