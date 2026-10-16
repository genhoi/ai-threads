import type { ComponentType } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Button, IconButton, preloadTerminal, ToastHost } from '../components';
import { toggleSidebar } from '../app/actions';
import { needsYou } from '../app/derive';
import { loadBootstrap } from '../app/sync';
import { needsSetup, showsReboot, store, useApp } from '../app/state';
import { usePhoneLayout } from '../lib/media';
import { PhoneApp } from '../phone/PhoneApp';
import { FinishDialog } from './FinishDialog';
import { FirstRun } from './FirstRun';
import { History } from './History';
import { Home } from './Home';
import { Reboot } from './Reboot';
import { Revoked } from './Revoked';
import { SearchPalette } from './SearchPalette';
import { SessionScreen } from './Session';
import { SettingsPage } from './Settings';
import { SummaryPage } from './Summary';
import { Sidebar, type SidebarVariant } from './Sidebar';

// Галерея — отдельным файлом: в обычной работе она не нужна
function GalleryRoute() {
  const [Gallery, setGallery] = useState<ComponentType | null>(null);
  useEffect(() => {
    import('../gallery/Gallery').then((m) => setGallery(() => m.Gallery));
  }, []);
  return Gallery ? <Gallery /> : null;
}

/** Заголовок вкладки: «(3) Нить», где 3 — сколько ждут */
function useDocumentTitle() {
  const count = useApp((s) => Object.values(s.sessions).filter(needsYou).length);
  useEffect(() => {
    document.title = count ? `(${count}) Нить` : 'Нить';
  }, [count]);
}

export function App() {
  const route = useApp((s) => s.route);
  const status = useApp((s) => s.status);
  const error = useApp((s) => s.error);
  const sidebarHidden = useApp((s) => s.sidebarHidden);
  const full = useApp((s) => s.full);
  const setup = useApp(needsSetup);
  const reboot = useApp(showsReboot);
  const noProjects = useApp((s) => s.projects.length === 0);
  const revoked = useApp((s) => s.revoked);
  const phone = usePhoneLayout();
  useDocumentTitle();

  // xterm и шрифт терминала — заранее, пока браузер свободен: первая открытая сессия их не ждёт.
  // На телефоне терминал текстом, xterm там не нужен
  useEffect(() => {
    if (phone) return;
    if (typeof requestIdleCallback === 'function') requestIdleCallback(preloadTerminal, { timeout: 3000 });
    else setTimeout(preloadTerminal, 1500);
  }, [phone]);

  // Телефон отключили на компьютере: вместо приложения — что случилось и как подключиться снова
  if (revoked) return <Revoked />;

  if (route.name === 'gallery')
    return (
      <>
        <GalleryRoute />
        <ToastHost />
      </>
    );

  if (status === 'error')
    return (
      <div class="shell shell--down">
        <div class="page-empty">
          <h1 class="page-empty__title">Нет связи с «Нитью»</h1>
          <p class="page-empty__text">{error}</p>
          <Button variant="regular" icon="restart_alt" onClick={() => void loadBootstrap()}>
            Повторить
          </Button>
        </div>
      </div>
    );

  // Телефон и узкое окно: свои экраны вместо панели и терминала
  if (phone) return <PhoneApp />;

  const showSidebar = !sidebarHidden && !full;
  const home = route.name === 'home' || route.name === 'new';
  let main;
  let variant: SidebarVariant = 'normal';
  if (status === 'loading') main = <main class="page" aria-busy="true" />;
  else if (route.name === 'session' && route.key) main = <SessionScreen sessionKey={route.key} />;
  else if (route.name === 'history') main = <History />;
  else if (route.name === 'settings') main = <SettingsPage />;
  else if (route.name === 'summary') main = <SummaryPage />;
  else if (home && setup) {
    main = <FirstRun />;
    if (noProjects) variant = 'empty';
  } else if (home && reboot) {
    main = <Reboot />;
    variant = 'reboot';
  } else main = <Home />;
  // У главной, перезагрузки и первого запуска нет своей шапки: кнопка «Показать панель» — там же, где была «Спрятать»
  const reveal = !showSidebar && !full && (home || status === 'loading');

  return (
    <div class="shell">
      {showSidebar && <Sidebar variant={variant} onSearch={() => store.set({ searchOpen: true })} />}
      {main}
      {reveal && (
        <div class="shell__reveal">
          <IconButton icon="left_panel_open" label="Показать панель" shortcut={['Ctrl', 'B']} onClick={toggleSidebar} />
        </div>
      )}
      <SearchPalette />
      <FinishDialog />
      <ToastHost />
    </div>
  );
}
