import { useEffect, useRef, useState } from 'preact/hooks';
import type { LaunchMode } from '../api/types';
import { AgentBadge, Button, Icon, Segmented } from '../components';
import { AGENTS, type AgentId } from '../model';
import { useApp } from '../app/state';
import { tildify } from '../lib/format';
import { lastAgentOf } from '../screens/Sidebar';
import { AGENT_ORDER, effortLabel, useLaunch } from '../screens/useLaunch';
import { goBack } from './nav';
import { PhoneHeader } from './PhoneHeader';
import { Sheet, type SheetOption } from './Sheet';

type Picker = 'project' | 'agent' | 'model' | 'effort' | null;

/** Телефон, новая сессия: запрос, проект, агент, модель, рассуждения, папка или копия, ветка */
export function PhoneNew() {
  const launchReq = useApp((s) => s.launch);
  const f = useLaunch(launchReq.project, true);
  const [picker, setPicker] = useState<Picker>(null);
  const promptRef = useRef<HTMLTextAreaElement>(null);

  // Поле растёт с текстом
  useEffect(() => {
    const t = promptRef.current;
    if (!t) return;
    t.style.height = 'auto';
    t.style.height = `${t.scrollHeight}px`;
  }, [f.prompt]);

  const projectOptions: SheetOption[] = f.recentProjects.map((p) => {
    const last = p.default_agent ?? lastAgentOf(p);
    return { id: p.path, label: p.name, description: tildify(p.path), hint: last ? AGENTS[last].short : undefined };
  });
  if (f.path && !projectOptions.some((o) => o.id === f.path))
    projectOptions.unshift({ id: f.path, label: f.projectName, description: tildify(f.path) });

  const agentOptions: SheetOption[] = AGENT_ORDER.filter((id) => f.installed.some((a) => a.id === id)).map((id) => ({
    id,
    label: AGENTS[id].name,
    agent: id,
  }));

  const defaultModel = f.modelList.find((m) => m.id === f.models?.default_model)?.name || f.models?.default_model;
  const modelOptions: SheetOption[] = [
    { id: '', label: 'как в CLI', hint: defaultModel || undefined },
    ...f.modelList.map((m) => ({ id: m.id, label: m.name || m.id })),
  ];
  const effortOptions: SheetOption[] = [
    { id: '', label: 'как в CLI', hint: f.models?.default_effort ? effortLabel(f.models.default_effort) : undefined },
    ...f.efforts.map((e) => ({ id: e, label: effortLabel(e) })),
  ];

  const modelValue = f.model ? (f.modelList.find((m) => m.id === f.model)?.name ?? f.model) : 'как в CLI';
  const effortValue = f.models && !f.efforts.length ? 'нет у агента' : f.effort ? effortLabel(f.effort) : 'как в CLI';
  const noModels = !f.agent || (f.models !== null && !f.modelList.length);
  const noEfforts = !f.agent || (f.models !== null && !f.efforts.length);

  return (
    <div class="ph-screen">
      <PhoneHeader close={() => goBack('#/')} title="Новая сессия" />
      <form
        class="ph-new"
        onSubmit={(e) => {
          e.preventDefault();
          void f.launch();
        }}
      >
        <div class="ph-new__body">
          <label class="sr-only" for="ph-prompt">
            Запрос агенту
          </label>
          <textarea
            id="ph-prompt"
            ref={promptRef}
            class="ph-prompt"
            rows={4}
            value={f.prompt}
            placeholder="Задача для агента, можно оставить пустым"
            onInput={(e) => f.setPrompt(e.currentTarget.value)}
          />
          <div class="ph-rows">
            <PickRow label="Проект" value={f.projectName || 'не выбран'} onClick={() => setPicker('project')} />
            <PickRow
              label="Агент"
              value={f.agent ? AGENTS[f.agent].name : 'нет'}
              agent={f.agent || undefined}
              disabled={!agentOptions.length}
              onClick={() => setPicker('agent')}
            />
            <PickRow label="Модель" value={modelValue} disabled={noModels} onClick={() => setPicker('model')} />
            <PickRow label="Рассуждения" value={effortValue} disabled={noEfforts} onClick={() => setPicker('effort')} />
          </div>
          <Segmented<LaunchMode>
            label="Где работать"
            size="lg"
            hint=""
            value={f.mode}
            onChange={f.setMode}
            options={[
              { value: 'folder', label: 'Папка проекта' },
              { value: 'copy', label: 'Копия', icon: 'fork_right', disabled: !f.gitOk, reason: 'Папка не в git' },
            ]}
          />
          {f.mode === 'copy' && (
            <label class="ph-branch">
              <span class="ph-branch__label">Ветка</span>
              <input
                class="ph-branch__input"
                value={f.shownBranch}
                autocomplete="off"
                autocapitalize="off"
                spellcheck={false}
                onInput={(e) => f.setBranch(e.currentTarget.value)}
              />
            </label>
          )}
          {f.hint && (
            <p class={`ph-hint${f.hint.error ? ' is-error' : ''}`} role={f.hint.error ? 'alert' : undefined}>
              {f.hint.text}
            </p>
          )}
          {!f.hint && f.needsBranch && <p class="ph-hint">Нужна ветка для копии</p>}
        </div>
        <div class="ph-foot">
          <Button type="submit" variant="primary" size={48} block class="ph-big" loading={f.launching} disabled={!f.ready}>
            Запустить
          </Button>
        </div>
      </form>

      <Sheet open={picker === 'project'} title="Проект" options={projectOptions} value={f.path} onPick={f.setPath} onClose={() => setPicker(null)} />
      <Sheet
        open={picker === 'agent'}
        title="Агент"
        options={agentOptions}
        value={f.agent}
        onPick={(id) => f.setAgent(id as AgentId)}
        onClose={() => setPicker(null)}
      />
      <Sheet open={picker === 'model'} title="Модель" options={modelOptions} value={f.model} onPick={f.setModel} onClose={() => setPicker(null)} />
      <Sheet open={picker === 'effort'} title="Рассуждения" options={effortOptions} value={f.effort} onPick={f.setEffort} onClose={() => setPicker(null)} />
    </div>
  );
}

function PickRow({
  label,
  value,
  agent,
  disabled,
  onClick,
}: {
  label: string;
  value: string;
  agent?: AgentId;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      class="ph-row"
      aria-haspopup="dialog"
      aria-disabled={disabled || undefined}
      aria-label={`${label}: ${value}`}
      onClick={() => !disabled && onClick()}
    >
      <span class="ph-row__label">{label}</span>
      {agent && <AgentBadge agent={agent} size={24} decorative />}
      <span class="ph-row__value">{value}</span>
      {!disabled && <Icon name="chevron_right" size={18} class="ph-chev" />}
    </button>
  );
}
