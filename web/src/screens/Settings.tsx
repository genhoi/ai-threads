import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api, downloadExport, errorText } from '../api/client';
import type { RunnerSettings, SessionView, Settings } from '../api/types';
import type { IconName } from '../icons';
import { Button, Chip, Icon, IconButton, Input, KbdCombo, Menu, Segmented, TagChip, toast } from '../components';
import type { AgentId } from '../model';
import { toggleSidebar } from '../app/actions';
import { agentName, autoRunner, runnerMenu } from '../app/runners';
import { loadBootstrap } from '../app/sync';
import { store, useApp } from '../app/state';
import { plural, tildify } from '../lib/format';
import { AgentsSection } from './SettingsAgents';
import { PhoneSection, useRemoteView } from './SettingsPhone';
import { useSettingsDraft, type SaveStatus, type SettingsChange } from './useSettingsDraft';

const SECTIONS = [
  { id: 'agents', label: 'Агенты' },
  { id: 'summary', label: 'Сводка' },
  { id: 'service', label: 'Служебные сессии' },
  { id: 'tmux', label: 'tmux' },
  { id: 'phone', label: 'Доступ с телефона' },
  { id: 'data', label: 'Данные' },
  { id: 'keys', label: 'Сочетания клавиш' },
  { id: 'about', label: 'О приложении' },
] as const;

type SectionId = (typeof SECTIONS)[number]['id'];

const SHORTCUTS: { label: string; keys: string[] }[] = [
  { label: 'Новая сессия', keys: ['N'] },
  { label: 'Поиск', keys: ['Ctrl', 'K'] },
  { label: 'Следующая ждущая', keys: ['Alt', '↓'] },
  { label: 'Спрятать панель', keys: ['Ctrl', 'B'] },
  { label: 'История', keys: ['H'] },
  { label: 'Запустить в окне запуска', keys: ['Ctrl', 'Enter'] },
];

/** Настройки: одна страница, слева оглавление. Правки сохраняются сразу */
export function SettingsPage({ phone }: { phone?: boolean }) {
  const sidebarHidden = useApp((s) => s.sidebarHidden);
  const env = useApp((s) => s.env);
  const agentsCount = useApp((s) => s.agents.length);
  const agents = useApp((s) => s.agents);
  const counted = useApp((s) => s.counts?.service);
  const { draft, errors, status, change } = useSettingsDraft();
  const remote = useRemoteView();
  const scrollRef = useRef<HTMLElement>(null);
  const [active, setActive] = useState<SectionId>('agents');
  const [fetched, setFetched] = useState<number | null>(null);
  const serviceCount = counted ?? fetched;

  // Сколько служебных — из первой загрузки; старый сервер его не знает, тогда один запрос со служебными
  useEffect(() => {
    if (counted !== undefined) return;
    const ctl = new AbortController();
    api
      .sessions({ service: true }, ctl.signal)
      .then((all) => setFetched((all ?? []).filter((s: SessionView) => s.service).length))
      .catch(() => undefined);
    return () => ctl.abort();
  }, [counted === undefined]);

  // Раздел в оглавлении — тот, чей заголовок последним ушёл под верх страницы
  useEffect(() => {
    const root = scrollRef.current;
    if (!root) return;
    const onScroll = () => {
      const top = root.getBoundingClientRect().top + 120;
      let current: SectionId = 'agents';
      for (const sec of SECTIONS) {
        const el = document.getElementById(`set-${sec.id}`);
        if (el && el.getBoundingClientRect().top <= top) current = sec.id;
      }
      if (root.scrollTop + root.clientHeight >= root.scrollHeight - 4) current = 'about';
      setActive(current);
    };
    root.addEventListener('scroll', onScroll, { passive: true });
    return () => root.removeEventListener('scroll', onScroll);
  }, [!!draft]);

  // Прокрутка только страницы настроек: scrollIntoView сдвигал бы и каркас приложения
  const go = (id: SectionId) => {
    const el = document.getElementById(`set-${id}`);
    const root = scrollRef.current;
    if (!el || !root) return;
    const smooth = !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const top = el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop - 16;
    root.scrollTo({ top, behavior: smooth ? 'smooth' : 'auto' });
    el.querySelector<HTMLElement>('.set-sec__title')?.focus({ preventScroll: true });
    setActive(id);
  };

  const runner = draft?.summary.agent === 'auto' ? autoRunner(agents) : (draft?.summary.agent as AgentId | undefined);
  const devices = remote?.devices?.length ?? 0;
  const meta: Partial<Record<SectionId, string>> = {
    agents: agentsCount ? String(agentsCount) : undefined,
    summary: runner ? agentName(runner) : undefined,
    service: serviceCount !== null ? String(serviceCount) : undefined,
    tmux: env?.tmux_version,
    // Сколько телефонов подключено: когда доступ включён или телефоны остались с прошлого раза
    phone: remote && (remote.enabled || devices > 0) ? String(devices) : undefined,
    about: env?.version,
  };

  return (
    <main class={`settings${phone ? ' settings--phone' : ''}`} ref={scrollRef}>
      <div class="settings__grid">
        <nav class="set-toc" aria-label="Разделы настроек">
          <div class="set-toc__top">
            {sidebarHidden && !phone && (
              <IconButton icon="left_panel_open" label="Показать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />
            )}
            <h1 class="set-toc__title">Настройки</h1>
          </div>
          {SECTIONS.map((sec) => (
            <button
              key={sec.id}
              type="button"
              class={`set-toc__item${active === sec.id ? ' is-active' : ''}`}
              aria-current={active === sec.id ? 'location' : undefined}
              onClick={() => go(sec.id)}
            >
              <span class="set-toc__label">{sec.label}</span>
              {meta[sec.id] && <span class="set-toc__meta">{meta[sec.id]}</span>}
            </button>
          ))}
          {env?.settings_path && (
            <p class="set-where">
              Настройки хранятся в<span class="set-where__path">{tildify(env.settings_path)}</span>
            </p>
          )}
          <SaveNote status={status} errors={errors} />
        </nav>

        <div class="set-main">
          {!draft ? (
            <p class="set-loading" role="status">
              <Icon name="progress_activity" size={18} spin />
            </p>
          ) : (
            <>
              {phone && <SaveNote status={status} errors={errors} />}
              <AgentsSection draft={draft} errors={errors} change={change} />
              <SummarySection draft={draft} errors={errors} change={change} />
              <ServiceSection draft={draft} errors={errors} change={change} count={serviceCount} />
              <TmuxSection draft={draft} errors={errors} change={change} />
              <PhoneSection />
              <DataSection />
              <KeysSection />
              <AboutSection />
            </>
          )}
        </div>
      </div>
    </main>
  );
}

/** «сохранено» или «не сохранено», тихо, под оглавлением */
function SaveNote({ status, errors }: { status: SaveStatus; errors: Record<string, string> }) {
  const failed = status === 'error' && Object.keys(errors).length > 0;
  return (
    <p class={`set-save${failed ? ' is-error' : ''}${status === 'saved' || failed ? ' is-shown' : ''}`} role="status" aria-live="polite">
      {failed ? 'не сохранено: исправьте поле с ошибкой' : status === 'saved' ? 'сохранено' : ''}
    </p>
  );
}

interface SectionProps {
  draft: Settings;
  errors: Record<string, string>;
  change: SettingsChange;
}

function Section({ id, title, sub, children }: { id: SectionId; title: string; sub?: ComponentChildren; children: ComponentChildren }) {
  return (
    <section id={`set-${id}`} class="set-sec" aria-labelledby={`set-${id}-title`}>
      <div class="set-sec__titles">
        <h2 class="set-sec__title" id={`set-${id}-title`} tabIndex={-1}>
          {title}
        </h2>
        {sub && <p class="set-sec__sub">{sub}</p>}
      </div>
      {children}
    </section>
  );
}

function FieldLabel({ children, top, for: htmlFor }: { children: ComponentChildren; top?: boolean; for?: string }) {
  const cls = `set-grid__label${top ? ' set-grid__label--top' : ''}`;
  return htmlFor ? (
    <label class={cls} for={htmlFor}>
      {children}
    </label>
  ) : (
    <span class={cls}>{children}</span>
  );
}

function SubHead({ title, text }: { title: string; text: string }) {
  return (
    <span class="set-subhead">
      <span class="set-subhead__title">{title}</span>
      <span class="set-subhead__text">{text}</span>
    </span>
  );
}

// ───────── Сводка ─────────

function SummarySection({ draft, errors, change }: SectionProps) {
  const setRunner = (kind: 'summary' | 'digest', patch: Partial<RunnerSettings>, typing = false) =>
    change((s) => ({ ...s, [kind]: { ...s[kind], ...patch } }), typing);
  return (
    <Section id="summary" title="Сводка" sub="Кто пишет сводки и сколько ждать ответа.">
      <div class="set-card">
        <div class="set-grid set-grid--pad">
          <SubHead title="Сводка по сессии" text="О чём сессия и на чём остановилась. Составляется по кнопке в сведениях о сессии." />
          <RunnerFields
            kind="summary"
            r={draft.summary}
            errors={errors}
            note="обычно ответ приходит за 1–3 минуты"
            onChange={(patch, typing) => setRunner('summary', patch, typing)}
          />
        </div>
        <div class="set-grid set-grid--pad set-card__part">
          <SubHead title="Сводка за несколько дней" text="Страница «Сводка»: итоги по проектам и что осталось." />
          <RunnerFields
            kind="digest"
            r={draft.digest}
            errors={errors}
            note="агент читает много журналов"
            onChange={(patch, typing) => setRunner('digest', patch, typing)}
          />
        </div>
        <div class="set-grid set-grid--pad set-card__part">
          <FieldLabel top>Язык</FieldLabel>
          <div class="set-grid__value">
            <Segmented<string>
              label="Язык"
              tone="sunken"
              value={draft.language}
              hint={errors.language ?? 'Язык интерфейса и сводок'}
              onChange={(language) => change((s) => ({ ...s, language }))}
              options={[
                { value: 'auto', label: 'Авто' },
                { value: 'ru', label: 'Русский' },
                { value: 'en', label: 'English' },
              ]}
            />
          </div>
        </div>
      </div>
    </Section>
  );
}

function RunnerFields({
  kind,
  r,
  errors,
  note,
  onChange,
}: {
  kind: 'summary' | 'digest';
  r: RunnerSettings;
  errors: Record<string, string>;
  note: string;
  onChange: (patch: Partial<RunnerSettings>, typing?: boolean) => void;
}) {
  const agents = useApp((s) => s.agents);
  const chip = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [minutes, setMinutes] = useState(String(r.minutes));
  // Сохранённое значение пришло с сервера — показать его, если человек не правит поле
  const editing = useRef(false);
  useEffect(() => {
    if (!editing.current) setMinutes(String(r.minutes));
  }, [r.minutes]);

  const auto = autoRunner(agents);
  const items = runnerMenu(r.agent, agents, (agent) => onChange({ agent }));
  const minutesId = `set-${kind}-minutes`;
  const minutesError = errors[`${kind}.minutes`];
  return (
    <>
      <FieldLabel>Кто пишет</FieldLabel>
      <div class="set-grid__value">
        <div class="set-inline">
          {r.agent === 'auto' ? (
            <Chip ref={chip} value="Авто" open={open} aria-label="Кто пишет: Авто" onClick={() => setOpen(!open)} />
          ) : (
            <Chip ref={chip} agent={r.agent as AgentId} open={open} aria-label={`Кто пишет: ${agentName(r.agent)}`} onClick={() => setOpen(!open)} />
          )}
          {r.agent === 'auto' && auto && <span class="set-note">сейчас {agentName(auto)}: первый свободный из установленных</span>}
        </div>
        {errors[`${kind}.agent`] && <span class="set-error">{errors[`${kind}.agent`]}</span>}
        <Menu open={open} anchor={chip.current} label="Кто пишет" width={260} gap={10} items={items} activeId={r.agent} onClose={() => setOpen(false)} />
      </div>
      <FieldLabel for={minutesId}>Ждать ответа</FieldLabel>
      <div class="set-grid__value">
        <div class="set-inline set-inline--10">
          <input
            id={minutesId}
            class={`set-num${minutesError ? ' is-error' : ''}`}
            inputMode="numeric"
            autocomplete="off"
            value={minutes}
            aria-invalid={minutesError ? true : undefined}
            aria-describedby={minutesError ? `${minutesId}-error` : undefined}
            onFocus={() => (editing.current = true)}
            onBlur={() => {
              editing.current = false;
              setMinutes(String(r.minutes));
            }}
            onInput={(e) => {
              const v = e.currentTarget.value;
              setMinutes(v);
              // Не число — уйдёт как есть, и сервер скажет, что не так
              onChange({ minutes: /^\d+$/.test(v.trim()) ? Number(v.trim()) : (v as unknown as number) }, true);
            }}
          />
          <span class="set-unit">мин</span>
          <span class="set-note">{note}</span>
        </div>
        {minutesError && (
          <span class="set-error" id={`${minutesId}-error`}>
            {minutesError}
          </span>
        )}
      </div>
    </>
  );
}

// ───────── Служебные сессии ─────────

type ListName = 'auto_commands' | 'temp_dirs';

function ServiceSection({ draft, errors, change, count }: SectionProps & { count: number | null }) {
  const commands = draft.service.auto_commands ?? [];
  const dirs = draft.service.temp_dirs ?? [];

  const setList = (name: ListName, list: string[]) => change((s) => ({ ...s, service: { ...s.service, [name]: list } }));

  const remove = (name: ListName, value: string) => {
    const before = name === 'auto_commands' ? commands : dirs;
    void setList(name, before.filter((x) => x !== value));
    toast.show({
      title: name === 'auto_commands' ? 'Команда убрана' : 'Папка убрана',
      text: value,
      icon: 'close',
      action: { label: 'Отменить', onClick: () => void setList(name, before) },
    });
  };

  return (
    <Section
      id="service"
      title="Служебные сессии"
      sub={`Скрыты из панели и истории, видны фильтром «Служебные».${count !== null ? ` Сейчас таких ${count}.` : ''}`}
    >
      <div class="set-card">
        <div class="set-stack">
          <SubHead
            title="Автоматические запуски"
            text="Сессии, начатые этими командами. Обычно это ревью и сводки, которые запускают другие агенты."
          />
          <div class="set-tags">
            {commands.map((c) => (
              <TagChip key={c} label={c} onRemove={() => remove('auto_commands', c)} />
            ))}
          </div>
          <AddToList
            list={commands}
            error={errors['service.auto_commands']}
            icon="terminal"
            label="Добавить команду"
            placeholder="Добавить команду, например gemini -p"
            onSave={(list) => setList('auto_commands', list)}
          />
        </div>
        <div class="set-stack set-card__part">
          <SubHead title="Временные папки" text="Сессии, запущенные в этих папках и вложенных в них." />
          <div class="set-tags">
            {dirs.map((d) => (
              <TagChip key={d} label={d} onRemove={() => remove('temp_dirs', d)} />
            ))}
          </div>
          <AddToList
            list={dirs}
            error={errors['service.temp_dirs']}
            icon="folder"
            label="Добавить папку"
            placeholder="Добавить папку, например ~/scratch"
            onSave={(list) => setList('temp_dirs', list)}
          />
        </div>
      </div>
    </Section>
  );
}

/** Поле «Добавить» под списком служебных: значение уходит на сервер, ошибка — под полем */
function AddToList({
  list,
  error,
  icon,
  label,
  placeholder,
  onSave,
}: {
  list: string[];
  error: string | undefined;
  icon: IconName;
  label: string;
  placeholder: string;
  onSave: (list: string[]) => Promise<boolean>;
}) {
  const [value, setValue] = useState('');
  const [shown, setShown] = useState('');

  // Ошибка у списка, пока человек правит поле, показывается под полем
  useEffect(() => {
    if (error) setShown(error);
  }, [error]);

  const add = async () => {
    const v = value.trim();
    if (!v) return;
    if (list.includes(v)) {
      setValue('');
      return;
    }
    setShown('');
    const ok = await onSave([...list, v]);
    if (ok) {
      setValue('');
      return;
    }
    // Сервер не принял значение: вернуть список, текст оставить в поле
    void onSave(list);
  };

  return (
    <form
      class="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        void add();
      }}
    >
      <Input
        class="set-add__input"
        tone="sunken"
        icon={icon}
        mono
        monoPlaceholder
        label={label}
        placeholder={placeholder}
        value={value}
        error={shown || undefined}
        onInput={(e) => {
          setValue(e.currentTarget.value);
          setShown('');
        }}
      />
      <Button type="submit" variant="regular" icon="add" disabled={!value.trim()}>
        Добавить
      </Button>
    </form>
  );
}

// ───────── tmux ─────────

/** Имя сессии tmux, как его делает сервер (internal/tmux.Sanitize), без хвоста-хеша */
function sanitizeName(raw: string): string {
  let out = '';
  let dash = false;
  for (const ch of raw) {
    if (ch === '.' || ch === ':') {
      out += '_';
      dash = false;
    } else if (ch === '-' || ch === '_' || /[\p{L}\p{N}]/u.test(ch)) {
      if (ch === '-' && dash) continue;
      out += ch;
      dash = ch === '-';
    } else if (!dash) {
      out += '-';
      dash = true;
    }
  }
  out = out.replace(/^-+|-+$/g, '');
  if ([...out].length > 64) out = [...out].slice(0, 64).join('').replace(/-+$/, '');
  return out || 'nit';
}

function exampleName(pattern: string, s: SessionView | undefined): string {
  if (!s) return '';
  const vars: Record<string, string> = {
    project: s.project,
    branch: s.worktree?.branch || s.branch || '',
    agent: s.agent,
    id: s.id,
  };
  const expand = (p: string) => {
    let full = true;
    for (const [k, v] of Object.entries(vars)) {
      if (p.includes(`{${k}}`)) {
        full = full && v !== '';
        p = p.split(`{${k}}`).join(v);
      }
    }
    return { name: p, full };
  };
  let { name, full } = expand(pattern || 'nit-{project}-{branch}');
  if (!full) name = expand('nit-{agent}-{id}').name;
  return sanitizeName(name);
}

function TmuxSection({ draft, errors, change }: SectionProps) {
  const env = useApp((s) => s.env);
  const open = useApp((s) => Object.values(s.sessions).filter((x) => x.terminal).length);
  const sample = useApp((s) => {
    let best: SessionView | undefined;
    for (const x of Object.values(s.sessions)) if (!x.service && (!best || x.updated > best.updated)) best = x;
    return best;
  });
  const example = useMemo(() => exampleName(draft.tmux.session_name, sample), [draft.tmux.session_name, sample?.key]);
  const nameId = 'set-tmux-name';
  return (
    <Section id="tmux" title="tmux" sub="В нём агенты работают, даже если закрыть страницу.">
      <div class="set-card set-grid set-grid--pad">
        <FieldLabel>Состояние</FieldLabel>
        <span class="set-grid__value set-state">
          {env?.tmux ? (
            <>
              <Icon name="check" size={16} class="set-state__icon" />
              <span>
                tmux {env.tmux_version}
                {env.tmux_path && (
                  <>
                    {' · '}
                    <span class="set-mono">{env.tmux_path}</span>
                  </>
                )}
                {' · '}
                {open} {plural(open, 'сессия', 'сессии', 'сессий')} «Нити» {plural(open, 'открыта', 'открыты', 'открыто')}
              </span>
            </>
          ) : (
            <>
              <span class="smark smark--error" aria-hidden="true" />
              tmux не найден
            </>
          )}
        </span>
        <FieldLabel top for={nameId}>
          Имя сессий
        </FieldLabel>
        <div class="set-grid__value set-stack set-stack--6">
          <Input
            id={nameId}
            tone="sunken"
            mono
            value={draft.tmux.session_name}
            error={errors['tmux.session_name']}
            onInput={(e) => {
              const v = e.currentTarget.value;
              change((s) => ({ ...s, tmux: { ...s.tmux, session_name: v } }), true);
            }}
          />
          <span class="set-caption">
            {example && (
              <>
                Например, <span class="set-mono">{example}</span>.{' '}
              </>
            )}
            Подключиться вручную: <span class="set-mono">tmux attach -t …</span>
          </span>
        </div>
      </div>
    </Section>
  );
}

// ───────── Данные ─────────

function DataSection() {
  const file = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState<'export' | 'import' | null>(null);
  return (
    <Section
      id="data"
      title="Данные"
      sub="Названия, закрепления, скрытые сессии и настройки одним файлом. Журналы агентов остаются в их папках."
    >
      <div class="set-card set-data">
        <Button
          variant="regular"
          icon="download"
          loading={busy === 'export'}
          onClick={async () => {
            setBusy('export');
            try {
              await downloadExport();
            } catch (e) {
              toast.show({ kind: 'error', title: 'Не удалось выгрузить', text: errorText(e) });
            } finally {
              setBusy(null);
            }
          }}
        >
          Выгрузить
        </Button>
        <Button variant="regular" icon="upload" loading={busy === 'import'} onClick={() => file.current?.click()}>
          Загрузить
        </Button>
        <input
          ref={file}
          type="file"
          accept=".json,application/json"
          class="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={async (e) => {
            const f = e.currentTarget.files?.[0];
            e.currentTarget.value = '';
            if (!f) return;
            setBusy('import');
            try {
              await api.importData(await f.text());
              toast.show({ title: 'Данные загружены', icon: 'upload', text: f.name });
              await loadBootstrap();
              store.set({ settings: await api.settings() });
            } catch (err) {
              toast.show({ kind: 'error', title: 'Не удалось загрузить', text: errorText(err) });
            } finally {
              setBusy(null);
            }
          }}
        />
      </div>
    </Section>
  );
}

// ───────── Сочетания клавиш и о приложении ─────────

function KeysSection() {
  return (
    <Section id="keys" title="Сочетания клавиш" sub="Alt+↓ работает и когда фокус в терминале. Остальные клавиши в терминале уходят агенту.">
      <div class="set-card set-keys">
        {SHORTCUTS.map((k) => (
          <div class="set-keys__row" key={k.label}>
            <span class="set-keys__label">{k.label}</span>
            <KbdCombo keys={k.keys} size="md" />
          </div>
        ))}
      </div>
    </Section>
  );
}

function AboutSection() {
  const env = useApp((s) => s.env);
  return (
    <Section id="about" title="О приложении">
      <div class="set-card set-grid set-grid--pad set-grid--about">
        <span class="set-grid__label">Версия</span>
        <span class="set-mono set-mono--13">{env?.version ?? '—'}</span>
        <span class="set-grid__label">Сервер</span>
        <span class="set-mono set-mono--13">{env?.addr ?? '—'}</span>
      </div>
    </Section>
  );
}
