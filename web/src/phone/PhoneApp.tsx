import type { ComponentChildren } from 'preact';
import { useEffect, useReducer, useRef } from 'preact/hooks';
import { ToastHost } from '../components';
import { useApp, type Route } from '../app/state';
import { FinishDialog } from '../screens/FinishDialog';
import { SearchPalette } from '../screens/SearchPalette';
import { SettingsPage } from '../screens/Settings';
import { SummaryPage } from '../screens/Summary';
import { goBack } from './nav';
import { PhoneHeader } from './PhoneHeader';
import { PhoneHistory } from './PhoneHistory';
import { PhoneList } from './PhoneList';
import { PhoneNew } from './PhoneNew';
import { PhoneSession } from './PhoneSession';

/** Глубина экрана: дальше от списка — въезжает справа, ближе — уезжает вправо */
function depth(r: Route): number {
  if (r.name === 'home') return 0;
  if (r.name === 'session') return 2;
  return 1;
}

function routeId(r: Route): string {
  return `${r.name}|${r.key ?? ''}|${new URLSearchParams(r.query).toString()}`;
}

const SLIDE_MS = 240; // --slow

interface Transition {
  from: Route;
  dir: 'forward' | 'back';
  id: number;
}

/** Телефон: список, сессия, новая сессия, история проекта. Переход — сдвиг справа за --slow */
export function PhoneApp() {
  const route = useApp((s) => s.route);
  const last = useRef(route);
  const trans = useRef<Transition | null>(null);
  const seq = useRef(0);
  const [, force] = useReducer((x: number) => x + 1, 0);

  if (route !== last.current) {
    const prev = last.current;
    last.current = route;
    const d = depth(route) - depth(prev);
    trans.current = d && routeId(prev) !== routeId(route) ? { from: prev, dir: d > 0 ? 'forward' : 'back', id: ++seq.current } : null;
  }

  const id = trans.current?.id;
  useEffect(() => {
    if (!id) return;
    const t = window.setTimeout(() => {
      trans.current = null;
      force(0);
    }, SLIDE_MS + 20);
    return () => window.clearTimeout(t);
  }, [id]);

  const t = trans.current;
  let layers: { route: Route; cls: string }[];
  if (t?.dir === 'forward') layers = [{ route: t.from, cls: 'ph-layer is-under' }, { route, cls: 'ph-layer ph-enter' }];
  else if (t?.dir === 'back') layers = [{ route, cls: 'ph-layer' }, { route: t.from, cls: 'ph-layer ph-leave' }];
  else layers = [{ route, cls: 'ph-layer' }];

  return (
    <div class="ph">
      {layers.map((l) => (
        <div key={routeId(l.route)} class={l.cls} aria-hidden={l.route !== route || undefined} inert={l.route !== route || undefined}>
          <Screen route={l.route} />
        </div>
      ))}
      <SearchPalette />
      <FinishDialog />
      <ToastHost />
    </div>
  );
}

function Screen({ route }: { route: Route }) {
  const status = useApp((s) => s.status);
  if (status === 'loading') return <div class="ph-screen" aria-busy="true" />;
  switch (route.name) {
    case 'session':
      return route.key ? <PhoneSession sessionKey={route.key} /> : <PhoneList />;
    case 'new':
      return <PhoneNew />;
    case 'history':
      return <PhoneHistory />;
    case 'settings':
      return (
        <PhonePage title="Настройки">
          <SettingsPage phone />
        </PhonePage>
      );
    case 'summary':
      return (
        <PhonePage title="Сводка">
          <SummaryPage phone />
        </PhonePage>
      );
    default:
      return <PhoneList />;
  }
}

function PhonePage({ title, children }: { title: string; children: ComponentChildren }) {
  return (
    <div class="ph-screen">
      <PhoneHeader back={() => goBack('#/')} title={title} />
      <div class="ph-page">{children}</div>
    </div>
  );
}
