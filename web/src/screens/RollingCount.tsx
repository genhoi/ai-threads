import { useEffect, useRef, useState } from 'preact/hooks';

/** Счётчик: старое число уходит вверх, новое приходит снизу за 160 мс */
export function RollingCount({ value, class: cls }: { value: number; class?: string }) {
  const [old, setOld] = useState<{ v: number; id: number } | null>(null);
  const last = useRef(value);
  const seq = useRef(0);

  useEffect(() => {
    if (last.current === value) return;
    const prev = last.current;
    last.current = value;
    const id = ++seq.current;
    setOld({ v: prev, id });
    const t = window.setTimeout(() => setOld((o) => (o?.id === id ? null : o)), 170);
    return () => window.clearTimeout(t);
  }, [value]);

  return (
    <span class={`roll${cls ? ' ' + cls : ''}`}>
      {old && (
        <span key={`old-${old.id}`} class="roll__old" aria-hidden="true">
          {old.v}
        </span>
      )}
      <span key={`new-${value}`} class={old ? 'roll__new' : undefined}>
        {value}
      </span>
    </span>
  );
}
