import { useEffect, useRef, useState } from 'preact/hooks';
import { api, errorText } from '../api/client';
import type { Details } from '../api/types';

/**
 * Сведения о сессии: переписка, сводка, команды. Перечитываются, когда сессия меняется
 * (version — время её последнего изменения и готовая сводка), но не чаще раза в 3 секунды.
 */
export function useDetails(key: string, enabled: boolean, version: number | string) {
  const [details, setDetails] = useState<Details | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const lastAt = useRef(0);
  const timer = useRef(0);
  const seq = useRef(0);

  const load = () => {
    const id = ++seq.current;
    lastAt.current = Date.now();
    setLoading(true);
    api
      .details(key)
      .then((d) => {
        if (id !== seq.current) return;
        setDetails(d);
        setError('');
      })
      .catch((e) => {
        if (id === seq.current) setError(errorText(e));
      })
      .finally(() => {
        if (id === seq.current) setLoading(false);
      });
  };

  // Другая сессия — сведения с нуля
  useEffect(() => {
    setDetails(null);
    setError('');
    lastAt.current = 0;
  }, [key]);

  useEffect(() => {
    if (!enabled) return;
    window.clearTimeout(timer.current);
    const wait = Math.max(0, 3000 - (Date.now() - lastAt.current));
    if (wait === 0) load();
    else timer.current = window.setTimeout(load, wait);
    return () => window.clearTimeout(timer.current);
  }, [key, enabled, version]);

  return { details, error, loading, reload: load, setDetails };
}
