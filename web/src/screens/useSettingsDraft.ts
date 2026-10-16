import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError, api, errorText } from '../api/client';
import type { Settings } from '../api/types';
import { toast } from '../components';
import { store, useApp } from '../app/state';

export type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

/** Пауза после набора в текстовом поле перед сохранением */
const TYPING_PAUSE = 600;

/** Агенты и tmux заново: после сохранения настроек и по «Проверить снова». Ответ — текст ошибки или пусто */
export async function refreshEnv(): Promise<string> {
  try {
    const check = await api.env();
    store.set({ env: check.env, agents: check.agents ?? [] });
    return '';
  } catch (e) {
    return errorText(e);
  }
}

/**
 * Настройки правятся на месте и сохраняются сразу, текст — после паузы в наборе. На сервер уходят
 * настройки целиком, как он их отдал, с правками поверх: незнакомые экрану поля не теряются.
 * Ошибки — по полям, как их назвал сервер: agents.claude.flags, tmux.session_name.
 */
export function useSettingsDraft() {
  const initial = useApp((s) => s.settings);
  const [draft, setDraftState] = useState<Settings | null>(initial);
  const draftRef = useRef(draft);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [status, setStatus] = useState<SaveStatus>('idle');
  const edits = useRef(0);
  const timer = useRef(0);
  const running = useRef(false);
  const again = useRef(false);
  const waiters = useRef<((ok: boolean) => void)[]>([]);

  const put = (s: Settings) => {
    draftRef.current = s;
    setDraftState(s);
  };

  // Свежие настройки с сервера: файл могли поправить руками
  useEffect(() => {
    api
      .settings()
      .then((s) => {
        if (edits.current === 0) put(s);
        store.set({ settings: s });
      })
      .catch(() => undefined);
  }, []);

  const run = async () => {
    window.clearTimeout(timer.current);
    timer.current = 0;
    if (running.current) {
      again.current = true;
      return;
    }
    const body = draftRef.current;
    if (!body) return;
    running.current = true;
    const mine = waiters.current;
    waiters.current = [];
    setStatus('saving');
    let ok = false;
    try {
      // Доступ с телефона меняется своими запросами (/api/remote), и в черновике он может быть старым.
      // Без поля remote сервер оставляет его как есть
      const { remote: _remote, ...rest } = body;
      // Сервер убирает пробелы по краям; поле остаётся как набрано, иначе пробел посреди набора пропал бы
      const saved = await api.saveSettings(rest as Settings);
      ok = true;
      store.set({ settings: saved });
      setErrors({});
      setStatus('saved');
      void refreshEnv();
    } catch (e) {
      if (e instanceof ApiError && Object.keys(e.fields).length) setErrors(e.fields);
      else toast.show({ kind: 'error', title: 'Не получилось', text: errorText(e) });
      setStatus('error');
    }
    running.current = false;
    for (const w of mine) w(ok);
    if (again.current) {
      again.current = false;
      void run();
    }
  };

  /** Правка: mutate получает текущие настройки и возвращает новые. typing — сохранить после паузы */
  const change = (mutate: (s: Settings) => Settings, typing = false): Promise<boolean> => {
    const cur = draftRef.current;
    if (!cur) return Promise.resolve(false);
    put(mutate(cur));
    edits.current++;
    window.clearTimeout(timer.current);
    return new Promise<boolean>((resolve) => {
      waiters.current.push(resolve);
      if (typing) timer.current = window.setTimeout(() => void run(), TYPING_PAUSE);
      else void run();
    });
  };

  // Ушли со страницы посреди паузы — сохранить сразу
  useEffect(
    () => () => {
      if (timer.current) void run();
    },
    [],
  );

  // «сохранено» гаснет через пару секунд
  useEffect(() => {
    if (status !== 'saved') return;
    const t = window.setTimeout(() => setStatus('idle'), 2500);
    return () => window.clearTimeout(t);
  }, [status]);

  return { draft, errors, status, change };
}

export type SettingsChange = ReturnType<typeof useSettingsDraft>['change'];
