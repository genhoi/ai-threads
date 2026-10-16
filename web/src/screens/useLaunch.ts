// Окно запуска без вида: проект, агент, модель, рассуждения, папка или копия, ветка и запуск.
// Общее у главной на компьютере и экрана «Новая сессия» на телефоне.
import { useEffect, useMemo, useState } from 'preact/hooks';
import { ApiError, api, errorText } from '../api/client';
import type { LaunchCheck, LaunchMode, ModelsView, ProjectView, SessionView } from '../api/types';
import { AGENTS, type AgentId } from '../model';
import { openSession } from '../app/actions';
import { projectTree } from '../app/derive';
import { addProvisional } from '../app/sync';
import { replaceRoute } from '../app/route';
import { canLaunch, launchableAgents, sessionHash, store, useApp } from '../app/state';
import { now } from '../lib/clock';
import { joinAnd, tildify } from '../lib/format';
import { lastAgentOf } from './Sidebar';

const EFFORTS: Record<string, string> = {
  minimal: 'минимальные',
  low: 'низкие',
  medium: 'средние',
  high: 'высокие',
  xhigh: 'очень высокие',
  max: 'максимальные',
};

export const effortLabel = (e: string) => EFFORTS[e] ?? e;

/** Агенты в выборе: порядок окна запуска */
export const AGENT_ORDER: AgentId[] = ['claude', 'codex', 'grok', 'kimi', 'cursor'];

export function remember(name: string, value: string) {
  try {
    localStorage.setItem(name, value);
  } catch {
    // не запомнится
  }
}

export function recall(name: string): string {
  try {
    return localStorage.getItem(name) ?? '';
  } catch {
    return '';
  }
}

/** Ветка из запроса: номер задачи вроде BACKEND-10312 */
function branchFromPrompt(prompt: string): string {
  return prompt.match(/\b[A-Z][A-Z0-9]+-\d+\b/)?.[0] ?? '';
}

// Модели агентов не меняются за время работы: один запрос на агента
const modelsCache = new Map<AgentId, Promise<ModelsView>>();
function loadModels(agent: AgentId): Promise<ModelsView> {
  let p = modelsCache.get(agent);
  if (!p) {
    p = api.models(agent);
    p.catch(() => modelsCache.delete(agent));
    modelsCache.set(agent, p);
  }
  return p;
}

export interface LaunchHint {
  text: string;
  error?: boolean;
}

/**
 * Состояние окна запуска. initialProject — проект, с которым открыли окно. phone — подсказки
 * в словах экрана телефона.
 */
export function useLaunch(initialProject?: string, phone = false) {
  const projects = useApp((s) => s.projects);
  const sessions = useApp((s) => s.sessions);
  const agents = useApp((s) => s.agents);
  const launchOk = useApp(canLaunch);
  const installed = useMemo(() => launchableAgents(store.get()), [agents]);

  const tree = useMemo(() => projectTree(projects, sessions), [projects, sessions]);
  const recentProjects = tree.map((n) => n.project);

  const [prompt, setPrompt] = useState(() => recall('nit.draft'));
  const [path, setPath] = useState<string>(() => initialProject ?? '');
  const [agent, setAgent] = useState<AgentId | ''>('');
  const [model, setModel] = useState('');
  const [effort, setEffort] = useState('');
  const [mode, setMode] = useState<LaunchMode>('folder');
  const [branch, setBranchState] = useState('');
  const [branchTouched, setBranchTouched] = useState(false);
  const [check, setCheck] = useState<LaunchCheck | null>(null);
  const [checkError, setCheckError] = useState('');
  const [models, setModels] = useState<ModelsView | null>(null);
  const [launching, setLaunching] = useState(false);
  const [error, setError] = useState('');

  // Проект по умолчанию: последний, где запускали, иначе самый свежий (не закреплённый первым)
  useEffect(() => {
    if (path || !projects.length) return;
    const last = recall('nit.lastProject');
    const freshest = [...recentProjects].sort((a, b) => b.updated - a.updated)[0];
    setPath(projects.some((p) => p.path === last) ? last : (freshest?.path ?? ''));
  }, [projects.length]);

  // Что известно о папке: git, кто в ней работает, предложенная ветка
  useEffect(() => {
    if (!path) return;
    setCheck(null);
    setCheckError('');
    const ctl = new AbortController();
    api
      .launchCheck(path, ctl.signal)
      .then((c) => {
        setCheck(c);
        setMode(c.git ? c.mode : 'folder');
        setBranchTouched(false);
      })
      .catch((e) => {
        if ((e as Error).name !== 'AbortError') setCheckError(errorText(e));
      });
    return () => ctl.abort();
  }, [path]);

  // Имя ветки по запросу предлагает сервер; пока он отвечает — номер задачи из запроса
  const [promptBranch, setPromptBranch] = useState('');
  useEffect(() => {
    const text = prompt.trim();
    if (!path || !text) {
      setPromptBranch('');
      return;
    }
    const ctl = new AbortController();
    const t = window.setTimeout(() => {
      api
        .launchCheck(path, ctl.signal, text.slice(0, 2000))
        .then((c) => setPromptBranch(c.branch ?? ''))
        .catch(() => undefined);
    }, 400);
    return () => {
      window.clearTimeout(t);
      ctl.abort();
    };
  }, [path, prompt]);
  useEffect(() => setPromptBranch(''), [path]);

  const project: ProjectView | undefined = projects.find((p) => p.path === path);
  const projectName = check?.project || project?.name || path.split('/').filter(Boolean).pop() || '';

  // Агент по умолчанию: настроенный у проекта, иначе последний запущенный здесь
  useEffect(() => {
    if (!installed.length) return;
    const ids = installed.map((a) => a.id);
    const candidates = [
      check?.default_agent,
      project?.default_agent,
      project ? lastAgentOf(project) : undefined,
      recall('nit.lastAgent') as AgentId,
      ids[0],
    ];
    const pick = candidates.find((id): id is AgentId => !!id && ids.includes(id));
    if (pick) setAgent(pick);
  }, [check, path, installed]);

  useEffect(() => {
    setModel('');
    setEffort('');
    setModels(null);
    if (!agent) return;
    let live = true;
    loadModels(agent)
      .then((m) => live && setModels(m))
      .catch(() => live && setModels({ default_model: '', default_effort: '', models: [], efforts: [] }));
    return () => {
      live = false;
    };
  }, [agent]);

  useEffect(() => remember('nit.draft', prompt), [prompt]);

  const modelList = models?.models ?? [];
  const chosenModel = modelList.find((m) => m.id === (model || models?.default_model));
  const efforts = (chosenModel?.efforts?.length ? chosenModel.efforts : models?.efforts) ?? [];
  const suggested = (prompt.trim() && promptBranch) || branchFromPrompt(prompt) || check?.branch || '';
  const shownBranch = branchTouched ? branch : suggested;
  const busy = (check?.busy ?? []).filter((b) => b.state !== 'idle');
  const gitOk = check ? check.git : (project?.git ?? true);

  // Выбранный уровень есть не у всех моделей: при смене модели сбрасывается
  useEffect(() => {
    if (effort && !efforts.includes(effort)) setEffort('');
  }, [model, models]);

  const hint = ((): LaunchHint | null => {
    if (error) return { text: error, error: true };
    if (!launchOk) return { text: 'Сначала установите tmux и агента', error: true };
    if (checkError) return { text: checkError, error: true };
    if (!check) return null;
    if (!check.exists) return { text: `Папки ${tildify(check.path)} нет`, error: true };
    const names = joinAnd([...new Set(busy.map((b) => AGENTS[b.agent]?.short ?? b.agent))]);
    const verb = busy.length > 1 ? 'работают' : 'работает';
    if (busy.length && mode === 'copy')
      return phone
        ? { text: `В папке ${projectName} ${verb} ${names}, поэтому выбрана копия.` }
        : { text: `В папке ${projectName} уже ${verb} ${names}, поэтому сессия запустится в копии с новой веткой.` };
    if (busy.length) return { text: `В папке ${projectName} уже ${verb} ${names}: агенты будут править одни и те же файлы.` };
    if (!check.git) return { text: `Папка ${projectName} не в git, поэтому копию сделать нельзя.` };
    return null;
  })();

  const ready = launchOk && !!agent && !!check?.exists && !launching && (mode === 'folder' || !!shownBranch.trim());
  /** Запуск в копии без ветки: подсказка на кнопке */
  const needsBranch = !launching && launchOk && !!check?.exists && mode === 'copy' && !shownBranch.trim();

  const launch = async () => {
    if (!ready || !agent) return;
    setLaunching(true);
    setError('');
    const req = {
      path: check?.path || path,
      agent,
      model: model || undefined,
      effort: effort || undefined,
      mode,
      branch: mode === 'copy' ? shownBranch.trim() : undefined,
      prompt: prompt.trim() || undefined,
    };
    try {
      const res = await api.launch(req);
      const t = now();
      const first = prompt.trim().split('\n')[0] ?? '';
      const provisional: SessionView = {
        key: res.key,
        agent,
        id: res.key.slice(res.key.indexOf(':') + 1),
        title: first.slice(0, 120) || 'Новая сессия',
        project: projectName,
        repo: check?.path || path,
        cwd: check?.path || path,
        branch: req.branch,
        created: t,
        updated: t,
        state: 'starting',
        since: t,
        terminal: true,
        resumable: false,
      };
      addProvisional(provisional);
      remember('nit.lastProject', path);
      remember('nit.lastAgent', agent);
      setPrompt('');
      // На телефоне экран новой сессии заменяется сессией: «Назад» ведёт к списку
      if (phone) replaceRoute(sessionHash(res.key));
      else openSession(res.key);
    } catch (e) {
      const fields = e instanceof ApiError ? Object.values(e.fields) : [];
      setError(fields[0] || errorText(e));
    } finally {
      setLaunching(false);
    }
  };

  return {
    // данные
    projects,
    recentProjects,
    installed,
    agents,
    launchOk,
    project,
    projectName,
    check,
    models,
    modelList,
    efforts,
    gitOk,
    shownBranch,
    hint,
    ready,
    needsBranch,
    launching,
    // выбранное
    prompt,
    path,
    agent,
    model,
    effort,
    mode,
    // изменения
    setPrompt,
    setPath: (p: string) => {
      setPath(p);
      setError('');
    },
    setAgent,
    setModel,
    setEffort,
    setMode: (m: LaunchMode) => {
      setMode(m);
      setError('');
    },
    setBranch: (b: string) => {
      setBranchState(b);
      setBranchTouched(true);
    },
    launch,
  };
}

export type LaunchForm = ReturnType<typeof useLaunch>;
