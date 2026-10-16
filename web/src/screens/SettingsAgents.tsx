import type { ComponentChildren } from 'preact';
import { useEffect, useId, useState } from 'preact/hooks';
import { api } from '../api/client';
import type { AgentSettings, AgentView, Settings } from '../api/types';
import { AgentBadge, Button, Checkbox, Icon, IconButton, Input, Switch, toast } from '../components';
import type { AgentId } from '../model';
import { titleOf } from '../app/derive';
import { useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { agoLong, sessionsCount, tildify } from '../lib/format';
import { refreshEnv, type SettingsChange } from './useSettingsDraft';

// Порядок карточек как в макете Nit-Settings
const ORDER: AgentId[] = ['claude', 'codex', 'kimi', 'grok', 'cursor', 'zcode'];

const FLAGS_PLACEHOLDER = 'например --model opus --add-dir ../shared';

interface Props {
  draft: Settings;
  errors: Record<string, string>;
  change: SettingsChange;
}

function agentSettings(s: Settings, id: string): AgentSettings {
  return s.agents[id] ?? { enabled: true, home: '', program: '', flags: '', skip_approvals: false };
}

/** Раздел «Агенты»: раскрытая карточка — отдельно, свёрнутые подряд — одной карточкой */
export function AgentsSection({ draft, errors, change }: Props) {
  const agents = useApp((s) => s.agents);
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const [checkedAt, setCheckedAt] = useState(() => Date.now() / 1000);
  const [checking, setChecking] = useState(false);

  const list = ORDER.map((id) => agents.find((a) => a.id === id)).filter((a): a is AgentView => !!a);
  const toggle = (id: string) => {
    const next = new Set(open);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setOpen(next);
  };
  const set = (id: string, patch: Partial<AgentSettings>, typing = false) =>
    change((s) => ({ ...s, agents: { ...s.agents, [id]: { ...agentSettings(s, id), ...patch } } }), typing);

  // Свёрнутые агенты подряд — в одну карточку
  const groups: { open: boolean; items: AgentView[] }[] = [];
  for (const a of list) {
    const isOpen = open.has(a.id);
    const last = groups[groups.length - 1];
    if (!isOpen && last && !last.open) last.items.push(a);
    else groups.push({ open: isOpen, items: [a] });
  }

  return (
    <section id="set-agents" class="set-sec" aria-labelledby="set-agents-title">
      <div class="set-sec__head">
        <div class="set-sec__titles">
          <h2 class="set-sec__title" id="set-agents-title" tabIndex={-1}>
            Агенты
          </h2>
          <p class="set-sec__sub">Где «Нить» берёт сессии и как запускает агентов.</p>
        </div>
        <CheckedAgo at={checkedAt} />
        <Button
          variant="regular"
          icon="restart_alt"
          loading={checking}
          onClick={async () => {
            setChecking(true);
            const error = await refreshEnv();
            if (error) toast.show({ kind: 'error', title: 'Не получилось', text: error });
            else setCheckedAt(Date.now() / 1000);
            setChecking(false);
          }}
        >
          Проверить снова
        </Button>
      </div>
      {groups.map((g) =>
        g.open ? (
          <AgentCard
            key={g.items[0]!.id}
            a={g.items[0]!}
            s={agentSettings(draft, g.items[0]!.id)}
            errors={errors}
            onCollapse={() => toggle(g.items[0]!.id)}
            set={(patch, typing) => set(g.items[0]!.id, patch, typing)}
          />
        ) : (
          <div class="set-card set-card--list" key={g.items.map((a) => a.id).join(',')}>
            {g.items.map((a) => (
              <AgentRow
                key={a.id}
                a={a}
                s={agentSettings(draft, a.id)}
                onExpand={() => toggle(a.id)}
                onEnabled={(on) => set(a.id, { enabled: on })}
              />
            ))}
          </div>
        ),
      )}
    </section>
  );
}

function CheckedAgo({ at }: { at: number }) {
  useTick();
  return <span class="set-sec__meta">проверено {agoLong(at)}</span>;
}

/** Коротко о состоянии агента: «найден · 449 сессий · без подтверждений» */
function shortStatus(a: AgentView, s: AgentSettings): string {
  if (!a.resumable) return [a.sessions ? sessionsCount(a.sessions) : '', `продолжить можно только в приложении ${a.name}`].filter(Boolean).join(' · ');
  const parts = [a.program ? 'найден' : 'не найден'];
  if (a.sessions) parts.push(sessionsCount(a.sessions));
  if (s.skip_approvals && a.skip_flag) parts.push('без подтверждений');
  return parts.join(' · ');
}

function AgentRow({ a, s, onExpand, onEnabled }: { a: AgentView; s: AgentSettings; onExpand: () => void; onEnabled: (on: boolean) => void }) {
  return (
    <div class="ag-row">
      <button type="button" class="ag-row__toggle" aria-expanded={false} onClick={onExpand}>
        <AgentBadge agent={a.id} size={32} decorative />
        <span class="ag-row__text">
          <span class="ag-row__name">{a.name}</span>
          <span class="ag-row__status">{shortStatus(a, s)}</span>
        </span>
      </button>
      <Switch label={`Показывать в списке: ${a.name}`} checked={s.enabled} onChange={onEnabled} class="ag-row__switch" />
      <span class="ag-row__chev" aria-hidden="true">
        <Icon name="expand_more" size={18} />
      </span>
    </div>
  );
}

interface CardProps {
  a: AgentView;
  s: AgentSettings;
  errors: Record<string, string>;
  onCollapse: () => void;
  set: (patch: Partial<AgentSettings>, typing?: boolean) => void;
}

function AgentCard({ a, s, errors, onCollapse, set }: CardProps) {
  const id = useId();
  const field = (name: string) => errors[`agents.${a.id}.${name}`];
  const home = tildify(a.home);
  const status = !a.resumable ? (
    shortStatus(a, s)
  ) : (
    <>
      {a.program ? (
        <>
          <Icon name="check" size={14} class="ag-head__ok" />
          найден: <span class="ag-mono">{tildify(a.program)}</span>
        </>
      ) : (
        'не найден'
      )}
      {' · '}
      {a.home_found ? (
        <>
          {sessionsCount(a.sessions)} в <span class="ag-mono">{home}</span>
        </>
      ) : (
        <>
          папки <span class="ag-mono">{home}</span> нет
        </>
      )}
    </>
  );
  return (
    <div class="set-card ag-card">
      <div class="ag-head">
        <AgentBadge agent={a.id} size={32} decorative />
        <div class="ag-head__text">
          <span class="ag-head__name">{a.name}</span>
          <span class="ag-head__status">{status}</span>
        </div>
        <Switch label="Показывать в списке" showLabel checked={s.enabled} onChange={(on) => set({ enabled: on })} />
        <IconButton icon="expand_more" label="Свернуть" size={36} class="ag-head__collapse" aria-expanded onClick={onCollapse} />
      </div>
      <div class="set-grid ag-fields">
        <Row label="Папка с сессиями" for={`${id}-home`}>
          <Input
            id={`${id}-home`}
            tone="sunken"
            mono
            monoPlaceholder
            value={s.home}
            placeholder={home}
            error={field('home')}
            onInput={(e) => set({ home: e.currentTarget.value }, true)}
          />
        </Row>
        {a.resumable && (
          <>
            <Row label="Программа" for={`${id}-program`}>
              <Input
                id={`${id}-program`}
                tone="sunken"
                mono
                monoPlaceholder
                value={s.program}
                placeholder={a.program ? tildify(a.program) : 'не найдена'}
                suffix={!s.program && a.program ? 'найдена в PATH' : undefined}
                error={field('program')}
                onInput={(e) => set({ program: e.currentTarget.value }, true)}
              />
            </Row>
            <Row label="Флаги запуска" for={`${id}-flags`}>
              <Input
                id={`${id}-flags`}
                tone="sunken"
                mono
                monoPlaceholder
                value={s.flags}
                placeholder={FLAGS_PLACEHOLDER}
                error={field('flags')}
                onInput={(e) => set({ flags: e.currentTarget.value }, true)}
              />
            </Row>
            {a.skip_flag && (
              <Row label="Команда продолжения" top={2}>
                <Checkbox
                  checked={s.skip_approvals}
                  onChange={(on) => set({ skip_approvals: on })}
                  label="Запускать без подтверждений"
                  description={
                    <>
                      Добавит флаг <span class="ag-mono ag-mono--strong">{a.skip_flag}</span>
                    </>
                  }
                />
              </Row>
            )}
            <ResumeExample agent={a.id} />
          </>
        )}
      </div>
    </div>
  );
}

/** Строка сетки: подпись и значение. top — подпись у верха значения, с этим отступом сверху */
function Row({ label, for: htmlFor, top, children }: { label: string; for?: string; top?: number; children: ComponentChildren }) {
  const cls = `set-grid__label${top !== undefined ? ' set-grid__label--top' : ''}`;
  const style = top !== undefined ? { paddingTop: `${top}px` } : undefined;
  return (
    <>
      {htmlFor ? (
        <label class={cls} style={style} for={htmlFor}>
          {label}
        </label>
      ) : (
        <span class={cls} style={style}>
          {label}
        </span>
      )}
      <div class="set-grid__value">{children}</div>
    </>
  );
}

/** «Пример»: команда продолжения последней сессии агента, с флагами из настроек */
function ResumeExample({ agent }: { agent: AgentId }) {
  const latest = useApp((st) => {
    let best: { key: string; updated: number } | undefined;
    for (const s of Object.values(st.sessions)) {
      if (s.agent !== agent || s.service || !s.resumable) continue;
      if (!best || s.updated > best.updated) best = s;
    }
    return best?.key;
  });
  const session = useApp((st) => (latest ? st.sessions[latest] : undefined));
  // Настройки сохранились — команда могла поменяться
  const version = useApp((st) => st.settings);
  const [command, setCommand] = useState('');
  useEffect(() => {
    if (!latest) return;
    const ctl = new AbortController();
    api
      .details(latest, ctl.signal)
      .then((d) => setCommand(d.resume ?? ''))
      .catch(() => undefined);
    return () => ctl.abort();
  }, [latest, version]);
  if (!session || !command) return null;
  return (
    <Row label="Пример" top={12}>
      <div class="ag-example">
        <code class="ag-example__cmd">
          <span class="ag-example__prompt">$</span> {command}
        </code>
        <span class="ag-example__caption">
          На сессии {titleOf(session)} в {session.project}
        </span>
      </div>
    </Row>
  );
}
