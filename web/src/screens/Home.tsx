import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { AgentView, LaunchMode, ProjectView, SessionView } from '../api/types';
import { Button, Chip, Input, Menu, ProjectChip, Segmented, StatusMark, type MenuEntry } from '../components';
import { AGENTS } from '../model';
import { openSession } from '../app/actions';
import { isNew, titleOf, waitingList } from '../app/derive';
import { useApp } from '../app/state';
import { useTick } from '../lib/clock';
import { useStableOrder } from '../lib/store';
import { ago, tildify } from '../lib/format';
import { lastAgentOf } from './Sidebar';
import { AGENT_ORDER, effortLabel, useLaunch } from './useLaunch';

export { effortLabel } from './useLaunch';

const isPath = (q: string) => /^(~|\/|\.\.?\/)/.test(q.trim());

type OpenMenu = 'project' | 'agent' | 'model' | 'effort' | null;

/** Главная: «Что сделать?», окно запуска, ждущие и недавние проекты */
export function Home() {
  const sessions = useApp((s) => s.sessions);
  const launchReq = useApp((s) => s.launch);
  const form = useLaunch(launchReq.project);
  const {
    projects,
    recentProjects,
    installed,
    agents,
    projectName,
    models,
    modelList,
    efforts,
    gitOk,
    shownBranch,
    hint,
    ready,
    needsBranch,
    launching,
    prompt,
    path,
    agent,
    model,
    effort,
    mode,
    setPrompt,
    setAgent,
    setModel,
    setEffort,
    setMode,
    setBranch,
    launch,
  } = form;

  const promptRef = useRef<HTMLTextAreaElement>(null);
  const projectChip = useRef<HTMLButtonElement>(null);
  const agentChip = useRef<HTMLButtonElement>(null);
  const modelChip = useRef<HTMLButtonElement>(null);
  const effortChip = useRef<HTMLButtonElement>(null);

  const [open, setOpen] = useState<OpenMenu>(null);
  const [focused, setFocused] = useState(false);

  // «+» у проекта, N, «Новая сессия»: выбрать проект и поставить курсор в поле
  useEffect(() => {
    if (launchReq.project) form.setPath(launchReq.project);
    const t = promptRef.current;
    if (t) {
      t.focus({ preventScroll: true });
      t.setSelectionRange(t.value.length, t.value.length);
    }
  }, [launchReq.seq]);

  // Поле запроса растёт с текстом
  useEffect(() => {
    const t = promptRef.current;
    if (!t) return;
    t.style.height = 'auto';
    t.style.height = `${t.scrollHeight}px`;
  }, [prompt]);

  const projectItems = (query: string): MenuEntry[] => {
    const q = query.trim().toLowerCase();
    const found = recentProjects.filter((p) => !q || p.name.toLowerCase().includes(q) || p.path.toLowerCase().includes(q));
    const items: MenuEntry[] = [];
    if (isPath(query)) {
      const typed = query.trim();
      items.push({
        id: 'typed',
        label: typed.replace(/\/+$/, '').split('/').pop() || typed,
        icon: 'folder_open',
        description: typed,
        keepOpen: true,
        onSelect: () => void pickTyped(typed),
      });
    }
    items.push(
      { type: 'label', label: q ? 'Найдено' : 'Недавние' },
      ...found.slice(0, q ? 8 : 5).map((p) => {
        const last = p.default_agent ?? lastAgentOf(p);
        return {
          id: `p:${p.path}`,
          label: p.name,
          icon: 'folder' as const,
          description: tildify(p.path),
          checked: p.path === path ? true : undefined,
          hint: p.path === path || !last ? undefined : AGENTS[last].short,
          onSelect: () => choose(p.path),
        };
      }),
      { type: 'separator' },
      { id: 'other', label: 'Другая папка…', icon: 'folder_open', height: 40, keepOpen: true, onSelect: () => setQuery('~/') },
    );
    return items;
  };

  const choose = (p: string) => {
    form.setPath(p);
    promptRef.current?.focus({ preventScroll: true });
  };

  // Путь, вставленный в поиск проекта: проверить, что папка есть
  const [query, setQueryState] = useState('');
  const [pathError, setPathError] = useState('');
  const pickerInput = useRef<HTMLInputElement>(null);
  const setQuery = (q: string) => {
    setQueryState(q);
    setPathError('');
    pickerInput.current?.focus();
  };
  const pickTyped = async (typed: string) => {
    try {
      const c = await api.launchCheck(typed);
      if (!c.exists) {
        setPathError(`Папки ${typed} нет`);
        return;
      }
      setOpen(null);
      choose(c.path);
    } catch (e) {
      setPathError(errorText(e));
    }
  };

  useEffect(() => {
    if (open !== 'project') {
      setQueryState('');
      setPathError('');
    }
  }, [open]);

  const pickerItems = projectItems(query);
  const firstPick = pickerItems.find((e): e is Extract<MenuEntry, { id: string }> => 'id' in e && !!e.id);

  const agentItems: MenuEntry[] = [
    ...AGENT_ORDER.filter((id) => installed.some((a) => a.id === id)).map((id) => ({
      id,
      label: AGENTS[id].name,
      agent: id,
      checked: id === agent,
      onSelect: () => setAgent(id),
    })),
  ];
  if (agents.some((a: AgentView) => a.id === 'zcode' && a.enabled))
    agentItems.push({ id: 'zcode', label: AGENTS.zcode.name, agent: 'zcode', disabled: true, reason: 'только в своём приложении', reasonInline: true });

  const defaultModelName = modelList.find((m) => m.id === models?.default_model)?.name || models?.default_model;
  const modelItems: MenuEntry[] = [
    { id: '', label: 'как в CLI', checked: model === '', hint: model === '' ? undefined : defaultModelName || undefined, onSelect: () => setModel('') },
    ...modelList.map((m) => ({ id: m.id, label: m.name || m.id, checked: m.id === model, onSelect: () => setModel(m.id) })),
  ];
  const effortItems: MenuEntry[] = [
    {
      id: '',
      label: 'как в CLI',
      checked: effort === '',
      hint: effort === '' || !models?.default_effort ? undefined : effortLabel(models.default_effort),
      onSelect: () => setEffort(''),
    },
    ...efforts.map((e) => ({ id: e, label: effortLabel(e), checked: e === effort, onSelect: () => setEffort(e) })),
  ];
  // Карточки и проекты не переставляются, пока над ними курсор
  const [hoverCards, setHoverCards] = useState(false);
  const [hoverChips, setHoverChips] = useState(false);
  const keepNew = useApp((s) => s.keepNew);
  const liveWaiting = useMemo(() => waitingList(sessions, keepNew).slice(0, 6), [sessions, keepNew]);
  const waiting = useStableOrder(liveWaiting, (s) => s.key, hoverCards);
  const waitingCount = useMemo(() => waitingList(sessions).length, [sessions]);
  const liveRecent = useMemo(() => [...recentProjects].sort((a, b) => b.updated - a.updated).slice(0, 4), [projects]);
  const recentChips = useStableOrder(liveRecent, (p) => p.path, hoverChips);

  return (
    <main class="home">
      <div class="home__inner">
        <h1 class="home__title">Что сделать?</h1>
        <div
          class={`launch${focused || open ? ' is-active' : ''}`}
          onFocusIn={() => setFocused(true)}
          onFocusOut={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocused(false);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              void launch();
            }
          }}
        >
          <label class="sr-only" for="launch-prompt">
            Запрос агенту
          </label>
          <textarea
            id="launch-prompt"
            ref={promptRef}
            class="launch__prompt"
            rows={2}
            value={prompt}
            placeholder="Задача для агента, можно оставить пустым"
            onInput={(e) => setPrompt(e.currentTarget.value)}
          />
          <div class="launch__chips">
            <Chip
              ref={projectChip}
              icon="folder"
              value={projectName || 'Проект'}
              open={open === 'project'}
              aria-label={`Проект: ${projectName || 'не выбран'}`}
              onClick={() => setOpen(open === 'project' ? null : 'project')}
            />
            {agent ? (
              <Chip
                ref={agentChip}
                agent={agent}
                open={open === 'agent'}
                aria-label={`Агент: ${AGENTS[agent].name}`}
                onClick={() => setOpen(open === 'agent' ? null : 'agent')}
              />
            ) : (
              <Chip ref={agentChip} label="Агент" value="нет" disabled />
            )}
            <Chip
              ref={modelChip}
              label="Модель"
              value={model ? (modelList.find((m) => m.id === model)?.name ?? model) : 'как в CLI'}
              open={open === 'model'}
              disabled={!agent || (models !== null && !modelList.length)}
              onClick={() => setOpen(open === 'model' ? null : 'model')}
            />
            <Chip
              ref={effortChip}
              label="Рассуждения"
              value={models && !efforts.length ? 'нет у агента' : effort ? effortLabel(effort) : 'как в CLI'}
              open={open === 'effort'}
              disabled={!agent || (models !== null && !efforts.length)}
              onClick={() => setOpen(open === 'effort' ? null : 'effort')}
            />
          </div>
          <div class="launch__foot">
            <Segmented<LaunchMode>
              label="Где работать"
              tone="sunken"
              hint=""
              value={mode}
              onChange={setMode}
              options={[
                { value: 'folder', label: 'Папка проекта' },
                { value: 'copy', label: 'Копия', icon: 'fork_right', disabled: !gitOk, reason: 'Папка не в git' },
              ]}
            />
            {mode === 'copy' && (
              <Input
                class="launch__branch"
                tone="outline"
                mono
                prefix="ветка"
                label="Ветка копии"
                value={shownBranch}
                style={{ width: `${Math.max(10, shownBranch.length + 1)}ch` }}
                onInput={(e) => setBranch(e.currentTarget.value)}
              />
            )}
            <span class="launch__fill" />
            <Button
              variant="primary"
              size={48}
              hint="Ctrl+Enter"
              loading={launching}
              disabled={!ready}
              tooltip={needsBranch ? 'Нужна ветка для копии' : undefined}
              onClick={() => void launch()}
            >
              Запустить
            </Button>
          </div>
        </div>
        <p class={`home__hint${hint?.error ? ' is-error' : ''}`} role={hint?.error ? 'alert' : undefined}>
          {hint?.text}
        </p>

        {waitingCount > 0 && (
          <>
            <h2 class="home__sechead">
              Ждут вас<span class="home__count">{waitingCount}</span>
            </h2>
            <div class="home__cards" onPointerEnter={() => setHoverCards(true)} onPointerLeave={() => setHoverCards(false)}>
              {waiting.map((s) => (
                <WaitingCard key={s.key} s={s} />
              ))}
            </div>
          </>
        )}

        {recentChips.length > 0 && (
          <>
            <h2 class="home__label">Недавние проекты</h2>
            <div class="home__chips" onPointerEnter={() => setHoverChips(true)} onPointerLeave={() => setHoverChips(false)}>
              {recentChips.map((p) => (
                <RecentChip key={p.path} p={p} current={p.path === path} onPick={() => choose(p.path)} />
              ))}
            </div>
          </>
        )}
      </div>

      <Menu
        open={open === 'project'}
        anchor={projectChip.current}
        label="Проект"
        width={380}
        items={pickerItems}
        activeId={query ? firstPick?.id : path ? `p:${path}` : undefined}
        header={
          <Input
            ref={pickerInput}
            icon="search"
            label="Найти проект или вставить путь"
            placeholder="Найти проект или вставить путь"
            value={query}
            error={pathError || undefined}
            onInput={(e) => {
              setQueryState(e.currentTarget.value);
              setPathError('');
            }}
          />
        }
        onClose={() => setOpen(null)}
      />
      <Menu open={open === 'agent'} anchor={agentChip.current} label="Агент" width={260} gap={10} items={agentItems} activeId={agent || undefined} onClose={() => setOpen(null)} />
      <Menu open={open === 'model'} anchor={modelChip.current} label="Модель" width={260} items={modelItems} activeId={model} onClose={() => setOpen(null)} />
      <Menu open={open === 'effort'} anchor={effortChip.current} label="Рассуждения" width={260} items={effortItems} activeId={effort} onClose={() => setOpen(null)} />
    </main>
  );
}

function RecentChip({ p, current, onPick }: { p: ProjectView; current: boolean; onPick: () => void }) {
  useTick();
  return <ProjectChip name={p.name} when={ago(p.updated)} aria-pressed={current} onClick={onPick} />;
}

function WaitingCard({ s }: { s: SessionView }) {
  useTick();
  return (
    <button type="button" class="wcard" onClick={() => openSession(s.key)}>
      <span class="wcard__meta">
        <StatusMark state={s.state} isNew={isNew(s)} />
        <span class="wcard__who">
          {AGENTS[s.agent]?.short ?? s.agent} · {s.project}
        </span>
        <span class={s.state === 'waiting' ? 'wcard__time is-waiting' : 'wcard__time'}>{ago(s.since ?? s.updated)}</span>
      </span>
      <span class="wcard__title">{titleOf(s)}</span>
      {s.state === 'error' ? (
        <span class="wcard__error">{s.exit_code === undefined ? 'Ошибка' : `Ошибка · код ${s.exit_code}`}</span>
      ) : (
        <span class="wcard__text">{s.last || ' '}</span>
      )}
    </button>
  );
}
