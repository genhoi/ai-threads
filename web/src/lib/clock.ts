import { useEffect, useState } from 'preact/hooks';

// Часы интерфейса. В режиме ?mock их можно остановить, чтобы снимки экранов совпадали с макетом.
let frozenAt: number | null = null;
let offset = 0;

/** Сейчас, в секундах Unix */
export function now(): number {
  return frozenAt ?? Date.now() / 1000 + offset;
}

export function freezeClock(at: number | null) {
  frozenAt = at;
  emit();
}

// Один общий таймер на всё: все счётчики меняются в одну и ту же секунду
const listeners = new Set<() => void>();
let timer = 0;

function emit() {
  for (const l of listeners) l();
}

function schedule() {
  // До начала следующей секунды
  const ms = 1000 - (Date.now() % 1000) + 5;
  timer = window.setTimeout(() => {
    emit();
    schedule();
  }, ms);
}

/** Перерисовывает компонент каждую секунду; выключено, если active=false */
export function useTick(active = true): number {
  const [, set] = useState(0);
  useEffect(() => {
    if (!active) return;
    const l = () => set((x) => x + 1);
    listeners.add(l);
    if (listeners.size === 1) schedule();
    return () => {
      listeners.delete(l);
      if (listeners.size === 0) window.clearTimeout(timer);
    };
  }, [active]);
  return now();
}
