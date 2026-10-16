// Настройки → Доступ с телефона: узел Tailscale в самой «Нити», QR-код со ссылкой входа,
// новый ключ и подключённые телефоны. Управлять можно только с компьютера.
import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api, errorText, remoteQR } from '../api/client';
import type { DeviceView, RemoteView } from '../api/types';
import { Button, Dialog, DialogPoint, Icon, IconButton, Switch, toast } from '../components';
import { copyText } from '../app/actions';
import { store, useApp } from '../app/state';
import { whenFull } from '../lib/format';

/**
 * Доступ с телефона для экрана настроек: читается при открытии и после переподключения,
 * дальше меняется событиями remote. С телефона — null: там /api/remote отвечает 403
 */
export function useRemoteView(): RemoteView | null {
  const local = useApp((s) => !!s.env && s.env.via !== 'remote');
  const synced = useApp((s) => s.synced);
  const remote = useApp((s) => s.remote);
  useEffect(() => {
    if (!local) return;
    let alive = true;
    api
      .remote()
      .then((v) => alive && store.set({ remote: v }))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [local, synced]);
  return local ? remote : null;
}

/** Ссылка с ключом, ключ скрыт: https://nit.…/?key=k7•••••••••••••Qe */
export function maskLink(link: string): string {
  return link.replace(/([?&]key=)([^&#]*)/, (_, pre: string, key: string) =>
    key.length > 6 ? `${pre}${key.slice(0, 2)}${'•'.repeat(13)}${key.slice(-2)}` : `${pre}${'•'.repeat(key.length)}`,
  );
}

function Shell({ children }: { children: ComponentChildren }) {
  return (
    <section id="set-phone" class="set-sec" aria-labelledby="set-phone-title">
      <div class="set-sec__titles">
        <h2 class="set-sec__title" id="set-phone-title" tabIndex={-1}>
          Доступ с телефона
        </h2>
        <p class="set-sec__sub">Наведите камеру телефона на код. Ключ входит в ссылку, без него «Нить» не откроется.</p>
      </div>
      {children}
    </section>
  );
}

export function PhoneSection() {
  const via = useApp((s) => s.env?.via);
  const remote = useApp((s) => s.remote);
  if (via === 'remote')
    return (
      <Shell>
        <div class="set-card">
          <p class="set-phone__note">
            <Icon name="info" size={16} />
            Доступом с телефона управляют на компьютере: там его включают, показывают QR-код и отключают телефоны.
          </p>
        </div>
      </Shell>
    );
  return <Shell>{remote ? <Controls r={remote} /> : <div class="set-card set-phone__wait" aria-busy="true" />}</Shell>;
}

type Busy = 'enabled' | 'funnel' | 'retry' | 'key' | `device:${string}` | null;

function Controls({ r }: { r: RemoteView }) {
  const [busy, setBusy] = useState<Busy>(null);
  const [confirm, setConfirm] = useState(false);
  const devices = r.devices ?? [];

  const act = async (what: Exclude<Busy, null>, run: () => Promise<RemoteView>, fail: string): Promise<boolean> => {
    setBusy(what);
    try {
      store.set({ remote: await run() });
      return true;
    } catch (e) {
      toast.show({ kind: 'error', title: fail, text: errorText(e) });
      return false;
    } finally {
      setBusy(null);
    }
  };

  return (
    <div class="set-card">
      <div class="set-grid set-grid--pad">
        <span class="set-grid__label">Доступ</span>
        <div class="set-grid__value set-phone__row">
          <Switch
            checked={r.enabled}
            label="Доступ с телефона"
            disabled={busy === 'enabled'}
            onChange={(on) => void act('enabled', () => api.setRemote({ enabled: on }), on ? 'Не получилось включить' : 'Не получилось выключить')}
          />
          <RemoteState r={r} onRetry={() => void act('retry', () => api.setRemote({ enabled: true }), 'Не получилось')} retrying={busy === 'retry'} />
        </div>
        <span class="set-grid__label">Из интернета</span>
        <div class="set-grid__value set-phone__row">
          <Switch
            checked={r.funnel}
            label="Открыть адрес из интернета"
            disabled={busy === 'funnel'}
            onChange={(on) => void act('funnel', () => api.setRemote({ funnel: on }), 'Не получилось')}
          />
          <span class="set-note">Адрес откроется из интернета, но без ключа «Нить» не пустит.</span>
        </div>
      </div>

      {r.state === 'running' && r.link && (
        <div class="set-phone__body set-card__part">
          <QR link={r.link} />
          <div class="set-phone__side">
            <span class="set-grid__label">Ссылка</span>
            <div class="set-link">
              <span class="set-link__text">{maskLink(r.link)}</span>
              <IconButton icon="content_copy" label="Скопировать ссылку" size={32} onClick={() => void copyText(r.link!, 'Ссылка скопирована')} />
            </div>
            <div class="set-inline">
              <Button variant="regular" icon="restart_alt" loading={busy === 'key'} onClick={() => setConfirm(true)}>
                Новый ключ
              </Button>
              <span class="set-note">Все телефоны отключатся, подключите их заново</span>
            </div>
          </div>
        </div>
      )}

      {devices.length > 0 && (
        <div class="set-devices set-card__part">
          <span class="set-devices__label">Подключены · {devices.length}</span>
          {devices.map((d) => (
            <Device
              key={d.id}
              d={d}
              busy={busy === `device:${d.id}`}
              onRevoke={() => void act(`device:${d.id}`, () => api.revokeDevice(d.id), 'Не получилось отключить')}
            />
          ))}
        </div>
      )}

      <Dialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Сделать новый ключ?"
        footer={
          <>
            <Button variant="quiet" onClick={() => setConfirm(false)}>
              Отмена
            </Button>
            <Button
              variant="primary"
              icon="restart_alt"
              loading={busy === 'key'}
              data-autofocus
              onClick={async () => {
                if (await act('key', () => api.remoteKey(), 'Ключ не поменялся')) setConfirm(false);
              }}
            >
              Новый ключ
            </Button>
          </>
        }
      >
        <div class="finish__points">
          <DialogPoint kind="warn">Все телефоны отключатся, подключите их заново</DialogPoint>
          <DialogPoint kind="ok">Старая ссылка и QR-код перестанут работать</DialogPoint>
        </div>
      </Dialog>
    </div>
  );
}

/** Строка состояния узла рядом с переключателем */
function RemoteState({ r, onRetry, retrying }: { r: RemoteView; onRetry: () => void; retrying: boolean }) {
  if (!r.enabled) return <span class="set-remote set-remote--off">Выключен</span>;
  switch (r.state) {
    case 'starting':
    case 'off':
      return (
        <span class="set-remote">
          <Icon name="progress_activity" size={16} spin class="set-remote__icon set-remote__icon--spin" />
          <span>Подключаюсь к Tailscale…</span>
        </span>
      );
    case 'needs_login':
      return (
        <>
          <span class="set-remote">
            <Icon name="warning" size={16} class="set-remote__icon set-remote__icon--warn" />
            <span>Нужно войти в Tailscale</span>
          </span>
          {r.login_url && (
            <a class="btn btn--regular btn--40 btn--lead" href={r.login_url} target="_blank" rel="noopener noreferrer">
              <Icon name="open_in_new" size={18} />
              Войти в Tailscale
            </a>
          )}
        </>
      );
    case 'certificate':
      return (
        <span class="set-remote">
          <Icon name="progress_activity" size={16} spin class="set-remote__icon set-remote__icon--spin" />
          <span>
            {r.host && <span class="set-mono">{r.host}</span>}
            {r.host ? ': в' : 'В'}ыпускаю сертификат, это до минуты
          </span>
        </span>
      );
    case 'running':
      return (
        <span class="set-remote">
          <Icon name="check" size={16} class="set-remote__icon" />
          <span>
            Работает
            {r.host && (
              <>
                {' · '}
                <span class="set-mono">{r.host}</span>
              </>
            )}
          </span>
        </span>
      );
    case 'error':
      return (
        <>
          <span class="set-remote">
            <span class="smark smark--error set-remote__mark" aria-hidden="true" />
            <span>{r.error || 'Не получилось подключиться к Tailscale'}</span>
          </span>
          <Button variant="regular" icon="restart_alt" loading={retrying} onClick={onRetry}>
            Повторить
          </Button>
        </>
      );
    default:
      return null;
  }
}

/** QR-код ссылки входа: картинка с сервера, заново — когда ссылка меняется (новый ключ, другой адрес) */
function QR({ link }: { link: string }) {
  const [src, setSrc] = useState('');
  const [failed, setFailed] = useState(false);
  const url = useRef('');
  useEffect(() => {
    const ctl = new AbortController();
    setFailed(false);
    remoteQR(ctl.signal)
      .then((next) => {
        if (url.current) URL.revokeObjectURL(url.current);
        url.current = next;
        setSrc(next);
      })
      .catch((e) => {
        if ((e as Error).name !== 'AbortError') setFailed(true);
      });
    return () => ctl.abort();
  }, [link]);
  useEffect(
    () => () => {
      if (url.current) URL.revokeObjectURL(url.current);
    },
    [],
  );
  return (
    <div class={`set-phone__qr${src ? ' has-code' : ''}`}>
      {src ? (
        <img class="set-phone__img" src={src} alt="QR-код ссылки входа" width={168} height={168} />
      ) : failed ? (
        <span class="set-phone__qrerr">QR-код не загрузился</span>
      ) : (
        <Icon name="progress_activity" size={18} spin />
      )}
    </div>
  );
}

function Device({ d, busy, onRevoke }: { d: DeviceView; busy: boolean; onRevoke: () => void }) {
  return (
    <div class="set-device">
      <Icon name="smartphone" size={18} class="set-device__icon" />
      <div class="set-device__text">
        <span class="set-device__name">{d.name}</span>
        <span class="set-device__when">{whenFull(d.last_seen || d.created)}</span>
      </div>
      <Button variant="quiet" size={32} loading={busy} onClick={onRevoke}>
        Отключить
      </Button>
    </div>
  );
}
