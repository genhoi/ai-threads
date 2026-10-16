import { useEffect, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { FinishCheck, SessionView } from '../api/types';
import { Button, Dialog, DialogPoint, toast } from '../components';
import { AGENTS } from '../model';
import { applyView, setDone } from '../app/actions';
import { titleOf } from '../app/derive';
import { sessionOf, store, useApp } from '../app/state';
import { joinAnd, plural } from '../lib/format';

/** «Завершить задачу?»: что произойдёт, и почему копия может остаться */
export function FinishDialog() {
  const key = useApp((s) => s.finishKey);
  const session = useApp((s) => (key ? sessionOf(s, key) : undefined));
  const [check, setCheck] = useState<FinishCheck | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const last = useRef<{ s: SessionView; check: FinishCheck | null } | null>(null);
  const confirm = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    setCheck(null);
    setError('');
    if (!key) return;
    const ctl = { live: true };
    api
      .finishCheck(key)
      .then((c) => ctl.live && setCheck(c))
      .catch((e) => ctl.live && setError(errorText(e)));
    return () => {
      ctl.live = false;
    };
  }, [key]);

  // Пока окно гаснет, показывать то же содержимое
  if (key && session) last.current = { s: session, check };
  const view = last.current;
  const close = () => store.set({ finishKey: null });

  const finish = async () => {
    if (!key || !view) return;
    const c = view.check;
    const removes = !!c?.worktree && !!c.can_remove;
    setBusy(true);
    try {
      applyView(await api.finish(key, { remove_worktree: removes, delete_branch: removes && !!c?.can_delete_branch }));
      close();
      toast.show({
        title: 'Сессия завершена',
        action: removes ? undefined : { label: 'Отменить', onClick: () => void setDone(key, false) },
      });
    } catch (e) {
      toast.show({ kind: 'error', title: 'Не удалось завершить', text: errorText(e) });
    } finally {
      setBusy(false);
    }
  };

  const s = view?.s;
  return (
    <Dialog
      open={!!key && !!session}
      onClose={close}
      title="Завершить задачу?"
      subtitle={s ? `${titleOf(s)} · ${s.project}` : undefined}
      initialFocus={confirm}
      footer={
        <>
          <Button variant="quiet" onClick={close}>
            Отмена
          </Button>
          <Button ref={confirm} variant="primary" icon="task_alt" loading={busy} disabled={!view?.check && !error} onClick={finish}>
            Завершить
          </Button>
        </>
      }
    >
      {s && <Points s={s} check={view?.check ?? null} error={error} />}
    </Dialog>
  );
}

function Points({ s, check, error }: { s: SessionView; check: FinishCheck | null; error: string }) {
  const sessions = useApp((st) => st.sessions);
  if (!check && !error) return <div class="finish__points finish__points--wait" aria-busy="true" />;

  const wt = check?.worktree ?? s.worktree;
  const branch = wt?.branch ?? s.branch ?? '';
  const dirty = check?.dirty ?? [];
  const others = (check?.others ?? []).filter((k) => k !== s.key);
  const removes = !!wt && !!check?.can_remove;

  const reasons: string[] = [];
  if (dirty.length) {
    const files = dirty.length <= 3 ? `: ${joinAnd(dirty)}` : '';
    reasons.push(`${dirty.length} ${plural(dirty.length, 'файл', 'файла', 'файлов')} не ${dirty.length === 1 ? 'закоммичен' : 'закоммичены'}${files}. Закоммитьте их или удалите копию вручную.`);
  }
  if (check && !check.pushed && !check.merged) reasons.push('Ветка никуда не отправлена. Отправьте её или удалите копию вручную.');
  if (others.length) {
    const names = others.map((k) => {
      const o = sessions[k];
      return o ? `${AGENTS[o.agent]?.short ?? o.agent} (${titleOf(o)})` : k;
    });
    reasons.push(`В копии ещё ${others.length > 1 ? 'работают' : 'работает'} ${joinAnd(names)}.`);
  }

  return (
    <>
      <div class="finish__points">
        {s.terminal && <DialogPoint kind="ok">Агент остановится, терминал закроется</DialogPoint>}
        <DialogPoint kind="ok">Сессия отметится завершённой и останется в проекте и поиске</DialogPoint>
        {wt && removes && (
          <DialogPoint kind="ok">
            {check?.can_delete_branch ? 'Копия и ветка ' : 'Копия удалится, ветка '}
            <span class="finish__branch">{branch}</span>
            {check?.can_delete_branch ? ' удалятся' : ' останется'}
          </DialogPoint>
        )}
        {wt && !removes && (
          <DialogPoint kind="warn" detail={reasons.join(' ') || undefined}>
            Копия и ветка <span class="finish__branch">{branch}</span> не удалятся
          </DialogPoint>
        )}
        {error && <DialogPoint kind="warn">{error}</DialogPoint>}
      </div>
      {wt && !removes && <p class="finish__note">Пока копия не удалена, завершение можно отменить.</p>}
    </>
  );
}
