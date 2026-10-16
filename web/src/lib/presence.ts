import { useEffect, useState } from 'preact/hooks';

/**
 * Держит элемент в DOM, пока он исчезает: после open=false ещё exitMs он «closing», потом снимается.
 * Исчезновение — только прозрачность, поэтому таймер, а не animationend.
 */
export function usePresence(open: boolean, exitMs: number): { mounted: boolean; closing: boolean } {
  const [mounted, setMounted] = useState(open);

  useEffect(() => {
    if (open) {
      setMounted(true);
      return;
    }
    const t = window.setTimeout(() => setMounted(false), exitMs);
    return () => window.clearTimeout(t);
  }, [open, exitMs]);

  return { mounted: open || mounted, closing: !open && mounted };
}
