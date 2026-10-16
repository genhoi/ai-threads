import { useEffect, useReducer, useRef } from 'preact/hooks';

// Маленькое хранилище: состояние целиком, подписка и выбор части в компоненте.

export interface Store<T> {
  get(): T;
  set(patch: Partial<T> | ((s: T) => Partial<T>)): void;
  subscribe(l: () => void): () => void;
}

export function createStore<T extends object>(initial: T): Store<T> {
  let state = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    set(patch) {
      const p = typeof patch === 'function' ? patch(state) : patch;
      state = { ...state, ...p };
      for (const l of listeners) l();
    },
    subscribe(l) {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
  };
}

/** Часть состояния; компонент перерисовывается, только когда она изменилась */
export function useSelect<T, R>(store: Store<T>, select: (s: T) => R, equal: (a: R, b: R) => boolean = Object.is): R {
  const [, force] = useReducer((x: number) => x + 1, 0);
  const selectRef = useRef(select);
  selectRef.current = select;
  const value = select(store.get());
  const last = useRef(value);
  last.current = value;
  useEffect(
    () =>
      store.subscribe(() => {
        const next = selectRef.current(store.get());
        if (!equal(next, last.current)) force(0);
      }),
    [store],
  );
  return value;
}

/** Поверхностное сравнение объектов и массивов */
export function shallow<R>(a: R, b: R): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== 'object' || typeof b !== 'object' || !a || !b) return false;
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) if (!Object.is((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k])) return false;
  return true;
}

/**
 * Порядок списка, замороженный на время frozen (курсор над списком): строки не переставляются
 * и не появляются под курсором, исчезнувшие уходят. Когда заморозка снята — живой порядок.
 */
export function useStableOrder<T>(items: T[], keyOf: (item: T) => string, frozen: boolean): T[] {
  const snap = useRef<string[]>([]);
  if (!frozen) {
    snap.current = items.map(keyOf);
    return items;
  }
  const byKey = new Map(items.map((it) => [keyOf(it), it]));
  return snap.current.map((k) => byKey.get(k)).filter((it): it is T => it !== undefined);
}
