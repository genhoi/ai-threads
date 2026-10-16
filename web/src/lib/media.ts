import { useEffect, useState } from 'preact/hooks';

/** Телефон и узкое окно: до 600 px в ширину */
export const PHONE_QUERY = '(max-width: 600px)';

function matches(query: string): boolean {
  return typeof window !== 'undefined' && !!window.matchMedia && window.matchMedia(query).matches;
}

/** Совпадает ли окно с медиазапросом; меняется вместе с размером окна */
export function useMedia(query: string): boolean {
  const [on, setOn] = useState(() => matches(query));
  useEffect(() => {
    const mq = window.matchMedia(query);
    const update = () => setOn(mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  }, [query]);
  return on;
}

export function usePhoneLayout(): boolean {
  return useMedia(PHONE_QUERY);
}
