import { navigate } from '../app/route';

// Переходы внутри приложения: «Назад» идёт по истории браузера, только если пришли изнутри
let inside = 0;
let lastHash = location.hash;
window.addEventListener('hashchange', () => {
  if (location.hash !== lastHash) inside++;
  lastHash = location.hash;
});

/** Назад: по истории браузера, а если экран открыли сразу по ссылке — на parent */
export function goBack(parent = '#/') {
  if (inside > 0) {
    inside -= 2;
    history.back();
  } else navigate(parent);
}
