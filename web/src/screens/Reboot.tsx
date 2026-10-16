import { useMemo, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { RebootSession } from '../api/types';
import { AgentBadge, Button, Icon, Tooltip, toast } from '../components';
import { AGENTS } from '../model';
import { titleOf } from '../app/derive';
import { store, useApp } from '../app/state';
import { minutesText, numberWordF, plural } from '../lib/format';

/** Почему сессию нельзя продолжить; пусто — можно */
function blocked(r: RebootSession): string {
  const s = r.session;
  if (s.resumable) return '';
  if (s.missing) return 'Папки сессии нет';
  return `${AGENTS[s.agent]?.short ?? s.agent} не умеет продолжать сессии`;
}

/** Что делала сессия до перезагрузки: «ждал вас», «работал 7 мин», «вышел с кодом 1» */
function wasLabel(r: RebootSession): { text: string; error?: boolean } | null {
  switch (r.was) {
    case 'waiting':
      return { text: 'ждал вас' };
    case 'working':
      return { text: r.minutes ? `работал ${minutesText(r.minutes)}` : 'работал' };
    case 'starting':
      return { text: 'запускался' };
    case 'error':
      return { text: r.code === undefined ? 'вышел с ошибкой' : `вышел с кодом ${r.code}`, error: true };
    case 'exited':
      return { text: 'завершился' };
    default:
      return null;
  }
}

/** После перезагрузки: какие сессии были открыты и какие из них продолжить */
export function Reboot() {
  const list = useApp((s) => s.reboot);
  // По умолчанию отмечены все, кроме упавших и тех, что продолжить нельзя
  const [picked, setPicked] = useState<Set<string>>(
    () => new Set(list.filter((r) => r.was !== 'error' && !blocked(r)).map((r) => r.session.key)),
  );
  const [busy, setBusy] = useState<'resume' | 'dismiss' | null>(null);
  const available = useMemo(() => list.filter((r) => !blocked(r)), [list]);
  const chosen = available.filter((r) => picked.has(r.session.key));
  const n = chosen.length;

  const toggle = (key: string, on: boolean) => {
    const next = new Set(picked);
    if (on) next.add(key);
    else next.delete(key);
    setPicked(next);
  };

  // Невыбранные остаются в проектах и истории: их терминалы сервер забывает, чтобы экран не вернулся
  const resume = async () => {
    if (!n) return;
    setBusy('resume');
    try {
      const res = await api.rebootAction({ keys: chosen.map((r) => r.session.key), dismiss: true });
      const failed = Object.entries(res.failed ?? {});
      if (failed.length) {
        const titles = new Map(list.map((r) => [r.session.key, titleOf(r.session)]));
        toast.show({
          kind: 'error',
          title: 'Не удалось продолжить',
          text: failed.map(([key, why]) => `${titles.get(key) ?? key}: ${why}`).join('\n'),
        });
      }
      store.set({ reboot: [] });
    } catch (e) {
      toast.show({ kind: 'error', title: 'Не удалось продолжить', text: errorText(e) });
    } finally {
      setBusy(null);
    }
  };

  const later = async () => {
    setBusy('dismiss');
    try {
      await api.rebootAction({ dismiss: true });
      store.set({ reboot: [] });
    } catch (e) {
      toast.show({ kind: 'error', title: 'Не получилось', text: errorText(e) });
    } finally {
      setBusy(null);
    }
  };

  const total = list.length;
  const opened = total === 1 ? 'была открыта' : 'были открыты';
  return (
    <main class="reboot">
      <div class="reboot__inner">
        <span class="reboot__icon">
          <Icon name="restart_alt" size={18} />
        </span>
        <h1 class="reboot__title">Компьютер перезагрузился</h1>
        <p class="reboot__text">
          Агенты в tmux остановились. Перед этим {opened} {numberWordF(total)} {plural(total, 'сессия', 'сессии', 'сессий')}. Отметьте,
          какие продолжить: каждая откроется в терминале с того места, где остановилась.
        </p>
        <div class="reboot__card on-surface-1">
          <div class="reboot__head">
            <span class="reboot__picked" aria-live="polite">
              Выбрано {n} из {total}
            </span>
            <Button
              variant="quiet"
              size={32}
              disabled={n === available.length}
              onClick={() => setPicked(new Set(available.map((r) => r.session.key)))}
            >
              Выбрать все
            </Button>
          </div>
          {list.map((r) => (
            <RebootRow key={r.session.key} r={r} on={picked.has(r.session.key)} onChange={(on) => toggle(r.session.key, on)} />
          ))}
        </div>
        <div class="reboot__actions">
          <Button variant="primary" size={48} icon="terminal" loading={busy === 'resume'} disabled={!n || !!busy} onClick={() => void resume()}>
            {n ? `Продолжить ${n} ${plural(n, 'сессию', 'сессии', 'сессий')}` : 'Продолжить'}
          </Button>
          <Button variant="quiet" size={48} loading={busy === 'dismiss'} disabled={!!busy} onClick={() => void later()}>
            Не сейчас
          </Button>
        </div>
        <p class="reboot__note">Невыбранные останутся в проектах и в истории, их можно продолжить позже.</p>
      </div>
    </main>
  );
}

function RebootRow({ r, on, onChange }: { r: RebootSession; on: boolean; onChange: (on: boolean) => void }) {
  const s = r.session;
  const reason = blocked(r);
  const was = wasLabel(r);
  const branch = s.worktree?.branch || s.branch;
  const row = (
    <label class={['reboot__row', !on && 'is-off', reason && 'is-disabled'].filter(Boolean).join(' ')}>
      <span class="check">
        <input
          type="checkbox"
          class="check__input"
          checked={on && !reason}
          disabled={!!reason}
          onChange={(e) => onChange(e.currentTarget.checked)}
        />
        <span class="check__box" aria-hidden="true">
          {on && !reason && <Icon name="check" size={14} />}
        </span>
      </span>
      <AgentBadge agent={s.agent} size={28} />
      <span class="reboot__body">
        <span class="reboot__name">{titleOf(s)}</span>
        <span class="reboot__where">
          {s.project}
          {branch && (
            <>
              {' · '}
              <span class="reboot__branch">{branch}</span>
            </>
          )}
          {s.worktree && <Icon name="fork_right" size={14} label="копия" />}
        </span>
      </span>
      {was && <span class={was.error ? 'reboot__was is-error' : 'reboot__was'}>{was.text}</span>}
    </label>
  );
  return reason ? (
    <Tooltip content={reason} block placement="bottom-start">
      {row}
    </Tooltip>
  ) : (
    row
  );
}
