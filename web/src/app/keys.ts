// Сочетания клавиш: N, Ctrl+K, Alt+↓, Ctrl+B, H. Пока человек печатает в поле, они молчат;
// в терминале клавиши уходят агенту, кроме Alt+↓ (его ловит сам терминал).
import { goNextWaiting, openHome, toggleSidebar } from './actions';
import { navigate } from './route';
import { store } from './state';

function isTyping(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el?.matches) return false;
  return el.matches('input, textarea, select, [contenteditable=""], [contenteditable="true"]');
}

export function installShortcuts() {
  document.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.isComposing) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest?.('.xterm')) return;

    const st = store.get();
    if (e.key === 'Escape') {
      // Поиск закрывается и тогда, когда фокус ушёл из его окна
      if (st.searchOpen) {
        store.set({ searchOpen: false });
        e.preventDefault();
        return;
      }
      if (st.detailsOpen && !target?.closest?.('.menu, .dialog')) {
        store.set({ detailsOpen: false });
        e.preventDefault();
      }
      return;
    }

    // Под диалогом и поиском сочетания не работают
    if (st.searchOpen || st.finishKey || isTyping(target) || target?.closest?.('.menu')) return;

    const mod = e.ctrlKey || e.metaKey;
    if (mod && !e.altKey && !e.shiftKey && e.code === 'KeyK') {
      e.preventDefault();
      if (st.status === 'ready') store.set({ searchOpen: true });
      return;
    }
    if (mod && !e.altKey && !e.shiftKey && e.code === 'KeyB') {
      e.preventDefault();
      if (!st.full) toggleSidebar();
      return;
    }
    if (e.altKey && !mod && !e.shiftKey && e.key === 'ArrowDown') {
      e.preventDefault();
      goNextWaiting();
      return;
    }
    if (mod || e.altKey || e.shiftKey) return;
    if (e.code === 'KeyN') {
      e.preventDefault();
      openHome();
    } else if (e.code === 'KeyH') {
      e.preventDefault();
      navigate('#/history');
    }
  });
}
