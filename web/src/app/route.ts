// Адрес экрана в «#»: #/, #/s/<ключ>, #/history, #/summary, #/settings
import { parseRoute, store } from './state';

function setRoute(hash: string) {
  const route = parseRoute(hash);
  const prev = store.get().route;
  const swapped = !!prev.key && store.get().swaps[prev.key] === route.key;
  const sameSession = prev.name === 'session' && route.name === 'session' && (prev.key === route.key || swapped);
  const st = store.get();
  const opened = route.name === 'session' && route.key ? (st.sessions[route.key] ?? st.extra[route.key]) : undefined;
  const keepNew = sameSession ? st.keepNew : opened?.state === 'waiting' && opened.unseen ? opened.key : null;
  store.set({
    route,
    keepNew,
    // Сведения, правка названия и полный экран относятся к открытой сессии
    ...(sameSession ? {} : { detailsOpen: false, renaming: false }),
    ...(route.name === 'session' ? {} : { full: false }),
  });
}

export function startRouter() {
  window.addEventListener('hashchange', () => setRoute(location.hash));
}

export function navigate(hash: string) {
  if (location.hash === hash || (hash === '#/' && !location.hash)) {
    setRoute(hash);
    return;
  }
  location.hash = hash;
}

/** Сменить адрес без записи в историю браузера: временный ключ сессии → настоящий */
export function replaceRoute(hash: string) {
  history.replaceState(history.state, '', `${location.pathname}${location.search}${hash}`);
  setRoute(hash);
}
