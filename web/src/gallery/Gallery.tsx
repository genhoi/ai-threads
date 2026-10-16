import type { ComponentChildren } from 'preact';
import { useRef, useState } from 'preact/hooks';
import {
  AgentBadge,
  Button,
  Checkbox,
  Chip,
  CopyBadge,
  COPY_TITLE,
  Dialog,
  DialogPoint,
  FilterChip,
  Icon,
  IconButton,
  Input,
  Kbd,
  KbdCombo,
  Menu,
  NewBadge,
  ProjectChip,
  ProjectRow,
  Segmented,
  SessionRow,
  StatePill,
  StatusMark,
  Switch,
  TagChip,
  Terminal,
  ToastBox,
  TooltipBox,
  toast,
  useMenu,
  type TerminalHandle,
} from '../components';
import { ICON_NAMES } from '../icons';
import type { AgentId, SessionState } from '../model';
import { agentMenu, PROJECT_MENU, pickerMenu, SESSION_MENU, TERMINAL_DEMO } from './data';

const noop = () => undefined;

function Section({ title, text, children }: { title: string; text: string; children: ComponentChildren }) {
  return (
    <section class="gal-section">
      <div class="gal-section__aside">
        <h2 class="gal-section__title">{title}</h2>
        <p class="gal-section__text">{text}</p>
      </div>
      <div class="gal-section__body">{children}</div>
    </section>
  );
}

function Cap({ children }: { children: ComponentChildren }) {
  return <span class="gal-cap">{children}</span>;
}

function Cell({ cap, children }: { cap: string; children: ComponentChildren }) {
  return (
    <div class="gal-cell">
      <Cap>{cap}</Cap>
      {children}
    </div>
  );
}

const STATES_6 = ['Обычное', 'Наведение', 'Фокус', 'Нажатие', 'Недоступно', 'Загрузка'];

/** Все компоненты во всех состояниях, как на листе «Компоненты». Открывается по #gallery. */
export function Gallery() {
  return (
    <div class="gal">
      <header class="gal__head">
        <span class="gal__kicker">Нить · направление «Простор»</span>
        <h1 class="gal__title">Компоненты</h1>
      </header>
      <Buttons />
      <Inputs />
      <Segments />
      <Chips />
      <SessionRows />
      <ProjectRows />
      <Marks />
      <Menus />
      <Dialogs />
      <Toasts />
      <Toggles />
      <TerminalSection />
      <Icons />
    </div>
  );
}

function Buttons() {
  const [busy, setBusy] = useState(false);
  return (
    <Section title="Кнопки" text="Высота 32, 40 или 48. Основная одна на экран.">
      <div class="gal-grid gal-grid--states">
        <span />
        {STATES_6.map((s) => (
          <Cap key={s}>{s}</Cap>
        ))}

        <span class="gal-rowlabel">Основная</span>
        <div><Button variant="primary" icon="add" shortcut={['N']} tooltip="Новая сессия">Новая сессия</Button></div>
        <div><Button variant="primary" icon="add" class="is-hover">Новая сессия</Button></div>
        <div><Button variant="primary" icon="add" class="is-focus">Новая сессия</Button></div>
        <div><Button variant="primary" icon="add" class="is-active">Новая сессия</Button></div>
        <div><Button variant="primary" icon="add" disabled tooltip="Сначала установите tmux и агента">Новая сессия</Button></div>
        <div><Button variant="primary" icon="add" loading>Запускаю…</Button></div>

        <span class="gal-rowlabel">Обычная</span>
        <div><Button icon="folder_open">Открыть папку</Button></div>
        <div><Button icon="folder_open" class="is-hover">Открыть папку</Button></div>
        <div><Button icon="folder_open" class="is-focus">Открыть папку</Button></div>
        <div><Button icon="folder_open" class="is-active">Открыть папку</Button></div>
        <div><Button icon="folder_open" disabled>Открыть папку</Button></div>
        <div><Button icon="folder_open" loading>Открываю…</Button></div>

        <span class="gal-rowlabel">Тихая</span>
        <div><Button variant="quiet">Отмена</Button></div>
        <div><Button variant="quiet" class="is-hover">Отмена</Button></div>
        <div><Button variant="quiet" class="is-focus">Отмена</Button></div>
        <div><Button variant="quiet" class="is-active">Отмена</Button></div>
        <div><Button variant="quiet" disabled>Отмена</Button></div>
        <div><Button variant="quiet" loading>Загружаю…</Button></div>

        <span class="gal-rowlabel">Иконка</span>
        <div><IconButton icon="search" label="Поиск" shortcut={['Ctrl', 'K']} /></div>
        <div><IconButton icon="search" label="Поиск" class="is-hover" /></div>
        <div><IconButton icon="search" label="Поиск" class="is-focus" /></div>
        <div><IconButton icon="search" label="Поиск" class="is-active" /></div>
        <div><IconButton icon="search" label="Поиск" disabled /></div>
        <div><IconButton icon="search" label="Поиск" loading /></div>
      </div>

      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Размеры</span>
        <div class="gal-pair">
          <Button variant="primary" size={32} icon="add">Отменить</Button>
          <Cap>32 · в сообщениях и строках</Cap>
        </div>
        <div class="gal-pair">
          <Button variant="primary" icon="add">Новая сессия</Button>
          <Cap>40 · по умолчанию</Cap>
        </div>
        <div class="gal-pair">
          <Button variant="primary" size={48} hint="Ctrl+Enter">Запустить</Button>
          <Cap>48 · окно запуска и телефон</Cap>
        </div>
      </div>

      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">В экранах</span>
        <Button variant="primary" size={48} icon="terminal">Продолжить 4 сессии</Button>
        <Button variant="quiet" size={48}>Не сейчас</Button>
        <Button variant="quiet" size={36} icon="history" pressed>История</Button>
        <Button variant="quiet" size={36} icon="summarize">Сводка</Button>
        <Button variant="quiet" size={32} icon="content_copy">Для агента</Button>
        <Button variant="quiet" size={32}>Выбрать все</Button>
      </div>

      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Кнопки-иконки</span>
        <IconButton icon="close" label="Убрать" size={24} />
        <IconButton icon="keyboard_arrow_down" label="Следующая ждущая" shortcut={['Alt', '↓']} size={28} />
        <IconButton icon="content_copy" label="Скопировать" size={32} />
        <IconButton icon="settings" label="Настройки" size={36} />
        <IconButton icon="more_horiz" label="Меню сессии" pressed />
        <IconButton icon="arrow_back" label="Назад" size={48} />
        <IconButton icon="add" label="Новая сессия" size={48} variant="primary" />
        <Button variant="regular" loading={busy} icon="restart_alt" onClick={() => { setBusy(true); window.setTimeout(() => setBusy(false), 1500); }}>
          {busy ? 'Проверяю…' : 'Проверить снова'}
        </Button>
      </div>
    </Section>
  );
}

function DemoInput(props: Parameters<typeof Input>[0]) {
  const [value, setValue] = useState(String(props.value ?? ''));
  return <Input {...props} value={value} onInput={(e) => setValue(e.currentTarget.value)} />;
}

function Inputs() {
  return (
    <Section title="Поле ввода" text="Высота 40. Пути и ветки моноширинным.">
      <div class="gal-grid gal-grid--6">
        {['Пустое', 'Наведение', 'Фокус', 'Заполнено', 'Ошибка', 'Недоступно'].map((s) => (
          <Cap key={s}>{s}</Cap>
        ))}
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono />
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono class="is-hover" />
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono class="is-focus" />
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono value="~/projects/backend" />
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono value="~/projects/backnd" error="Такой папки нет" />
        <DemoInput icon="folder" label="Путь к папке" placeholder="Путь к папке" mono disabled />
      </div>
      <div class="gal-grid gal-grid--3">
        <Cell cap="На карточке · моноширинная подсказка">
          <DemoInput tone="sunken" label="Флаги запуска" placeholder="например --model opus --add-dir ../shared" mono monoPlaceholder />
        </Cell>
        <Cell cap="Текст справа">
          <DemoInput tone="sunken" label="Программа" value="~/.local/bin/claude" mono suffix="найдена в PATH" />
        </Cell>
        <Cell cap="Кнопка справа">
          <DemoInput
            tone="sunken"
            label="Ссылка"
            value="https://192.168.1.24:8765/?key=k7•••••••••••••Qe"
            mono
            readOnly
            action={<IconButton icon="content_copy" label="Скопировать" size={32} />}
          />
        </Cell>
        <Cell cap="Подпись внутри · ветка">
          <div>
            <DemoInput tone="outline" label="Ветка" prefix="ветка" value="BACKEND-10312" mono class="gal-inline" />
          </div>
        </Cell>
        <Cell cap="Поиск">
          <DemoInput icon="search" label="Найти проект" placeholder="Найти проект или вставить путь" type="search" />
        </Cell>
        <Cell cap="Телефон · 48">
          <DemoInput size={48} label="Ответ агенту" placeholder="Ответ агенту" />
        </Cell>
      </div>
    </Section>
  );
}

function Segments() {
  const [where, setWhere] = useState<'folder' | 'copy'>('copy');
  const [days, setDays] = useState<'3' | '5' | '7'>('3');
  const whereOpts = [
    { value: 'folder' as const, label: 'Папка проекта' },
    { value: 'copy' as const, label: 'Копия', icon: 'fork_right' as const },
  ];
  const daysOpts = [
    { value: '3' as const, label: '3 дня' },
    { value: '5' as const, label: '5 дней' },
    { value: '7' as const, label: 'Неделя' },
  ];
  return (
    <Section title="Переключатель вкладок" text="Выбор одного из двух-трёх вариантов.">
      <div class="gal-flow">
        <Cell cap="Обычное">
          <Segmented label="Где работать" options={whereOpts} value={where} onChange={setWhere} />
        </Cell>
        <Cell cap="Наведение на пункт">
          <Segmented label="Где работать" options={whereOpts} value="copy" demoHover="folder" />
        </Cell>
        <Cell cap="Фокус">
          <Segmented label="Где работать" options={whereOpts} value="copy" demoFocus="copy" />
        </Cell>
        <Cell cap="Пункт недоступен">
          <Segmented
            label="Где работать"
            options={[
              { value: 'folder', label: 'Папка проекта' },
              { value: 'copy', label: 'Копия', icon: 'fork_right', disabled: true, reason: 'Папка не в git, копию сделать нельзя' },
            ]}
            value="folder"
          />
        </Cell>
        <Cell cap="Три пункта">
          <Segmented label="За сколько дней" options={daysOpts} value={days} onChange={setDays} />
        </Cell>
        <Cell cap="На карточке · с пояснением">
          <Segmented
            label="Язык"
            tone="sunken"
            hint="Язык интерфейса и сводок"
            options={[
              { value: 'auto', label: 'Авто' },
              { value: 'ru', label: 'Русский' },
              { value: 'en', label: 'English' },
            ]}
            value="auto"
          />
        </Cell>
      </div>
      <div class="gal-phone">
        <Cap>Телефон · на всю ширину</Cap>
        <Segmented label="Где работать" size="lg" options={whereOpts} value={where} onChange={setWhere} />
      </div>
    </Section>
  );
}

function Chips() {
  const [service, setService] = useState(true);
  const [agent, setAgent] = useState<AgentId>('claude');
  const [project, setProject] = useState('backend');
  const [query, setQuery] = useState('');
  const agentM = useMenu();
  const projectM = useMenu();
  return (
    <Section title="Чип выбора" text="Выбор значения открывает список. Фильтр включается и выключается.">
      <div class="gal-grid gal-grid--chips">
        <span />
        {['Обычное', 'Наведение', 'Открыт · выбран', 'Фокус', 'Недоступно'].map((s) => (
          <Cap key={s}>{s}</Cap>
        ))}
        <span class="gal-rowlabel">Значение</span>
        <div><Chip label="Модель" value="как в CLI" /></div>
        <div><Chip label="Модель" value="как в CLI" class="is-hover" /></div>
        <div><Chip label="Модель" value="как в CLI" open /></div>
        <div><Chip label="Модель" value="как в CLI" class="is-focus" /></div>
        <div><Chip label="Рассуждения" value="нет у агента" disabled /></div>
        <span class="gal-rowlabel">Фильтр</span>
        <div><FilterChip label="Служебные" on={false} /></div>
        <div><FilterChip label="Служебные" on={false} class="is-hover" /></div>
        <div><FilterChip label="Служебные" on /></div>
        <div><FilterChip label="Служебные" on={false} class="is-focus" /></div>
        <div><FilterChip label="Служебные" on={false} disabled /></div>
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">С иконкой</span>
        <Chip icon="folder" value="backend" />
        <Chip agent="claude" />
        <Cap>Метка «Cl» — место для иконки агента 24 px</Cap>
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Работают</span>
        <Chip
          icon="folder"
          value={project}
          open={projectM.open}
          onClick={(e) => {
            setQuery('');
            projectM.toggle(e);
          }}
        />
        <Chip agent={agent} open={agentM.open} onClick={agentM.toggle} />
        <FilterChip label="Служебные" on={service} onChange={setService} />
        <Menu
          open={agentM.open}
          anchor={agentM.anchor}
          onClose={agentM.close}
          label="Агент"
          width={220}
          gap={10}
          items={agentMenu(agent, setAgent)}
        />
        <Menu
          open={projectM.open}
          anchor={projectM.anchor}
          onClose={projectM.close}
          label="Проект"
          width={380}
          header={
            <Input
              icon="search"
              label="Найти проект"
              placeholder="Найти проект или вставить путь"
              value={query}
              onInput={(e) => setQuery(e.currentTarget.value)}
            />
          }
          items={pickerMenu(query, project, setProject)}
        />
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Списки</span>
        <TagChip label="codex exec" />
        <TagChip label="claude -p" />
        <TagChip label="/tmp" />
        <ProjectChip name="backend" when="сейчас" />
        <ProjectChip name="shop-api" when="26 мин" class="is-hover" />
      </div>
    </Section>
  );
}

function SessionRows() {
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const openMenu = (id: string) => (el: HTMLElement) => {
    setAnchor(el);
    setMenuFor((cur) => (cur === id ? null : id));
  };
  return (
    <Section
      title="Строка сессии"
      text="В «Ждут вас» три строки: название, последняя фраза, агент и проект. В «Работают» — две, в проекте — одна."
    >
      <div class="gal-grid gal-grid--rows">
        <Cell cap="Ждёт">
          <SessionRow state="waiting" title="Промокоды в корзине: ревью" time="26 мин" text="Нашёл две проблемы. Поправить обе?" meta="Codex · shop-api" onMenu={openMenu('a')} menuOpen={menuFor === 'a'} />
        </Cell>
        <Cell cap="Новое">
          <SessionRow state="waiting" isNew title="BACKEND-10300: лимиты запросов" time="2 мин" text="Выполнить make migrate DB=local?" meta="Claude · backend" onMenu={openMenu('b')} menuOpen={menuFor === 'b'} />
        </Cell>
        <Cell cap="Ошибка">
          <SessionRow state="error" exitCode={1} title="Падение на Android 14 с камерой" time="34 мин" meta="Cursor · mobile-app" onMenu={openMenu('c')} menuOpen={menuFor === 'c'} />
        </Cell>
        <Cell cap="Работает">
          <SessionRow state="working" title="Ревью ветки BACKEND-10296" time="7:12" text="go test ./internal/billing/..." />
        </Cell>
        <Cell cap="Запускается">
          <SessionRow state="starting" title="BACKEND-10312: выгрузка отчёта" time="0:03" />
        </Cell>
        <Cell cap="Наведение · время сменяется меню">
          <SessionRow state="waiting" title="Промокоды в корзине: ревью" time="26 мин" text="Нашёл две проблемы. Поправить обе?" meta="Codex · shop-api" class="is-hover" onMenu={noop} />
        </Cell>
        <Cell cap="Выбрана">
          <SessionRow state="waiting" isNew selected title="BACKEND-10300: лимиты запросов" time="2 мин" text="Выполнить make migrate DB=local?" meta="Claude · backend" />
        </Cell>
        <Cell cap="В истории · без отметки">
          <SessionRow layout="line" state="idle" title="BACKEND-10281: вебхуки для партнёров" time="вчера" />
        </Cell>
        <Cell cap="Завершена">
          <SessionRow layout="line" state="idle" done title="BACKEND-10274: пагинация в API" time="12 окт" />
        </Cell>
      </div>
      <Menu open={menuFor !== null} anchor={anchor} onClose={() => setMenuFor(null)} label="Меню сессии" placement="bottom-end" items={SESSION_MENU} />
      <div class="gal-panel">
        <Cap>Панель слева · плотнее: поля 8, строки 18 и 16</Cap>
        <div class="gal-panel__list">
          <SessionRow density="panel" state="waiting" isNew selected title="BACKEND-10300: лимиты запросов" time="2 мин" text="Выполнить make migrate DB=local?" meta="Claude · backend" onMenu={noop} />
          <SessionRow density="panel" state="error" exitCode={1} title="Падение на Android 14 с камерой" time="34 мин" meta="Cursor · mobile-app" onMenu={noop} />
          <SessionRow density="panel" state="working" title="Перенос лендинга на Astro" time="18:40" text="astro build · 12 из 14 страниц" onMenu={noop} />
        </div>
      </div>
    </Section>
  );
}

function ProjectRows() {
  const [open, setOpen] = useState(true);
  return (
    <Section title="Строка проекта" text="Справа знаки активных сессий. При наведении — «+» и меню проекта.">
      <div class="gal-grid gal-grid--projects">
        <Cell cap="Свёрнута">
          <ProjectRow name="shop-api" marks={[{ state: 'waiting' }]} onNew={noop} onMenu={noop} />
        </Cell>
        <Cell cap="Раскрыта">
          <ProjectRow
            name="backend"
            expanded={open}
            onToggle={() => setOpen(!open)}
            marks={[{ state: 'waiting' }, { state: 'working' }]}
            moreCount={12}
            onNew={noop}
            onMenu={noop}
          >
            <SessionRow layout="line" nested state="waiting" title="BACKEND-10300: лимиты" copy />
            <SessionRow layout="line" nested state="working" title="Ревью ветки BACKEND-10296" />
            <SessionRow layout="line" nested state="idle" title="BACKEND-10281: вебхуки" time="вчера" />
          </ProjectRow>
        </Cell>
        <Cell cap="Наведение">
          <ProjectRow name="shop-api" marks={[{ state: 'waiting' }]} class="is-hover" menuOpen onNew={noop} onMenu={noop} />
        </Cell>
        <Cell cap="Закреплена">
          <ProjectRow name="landing" pinned marks={[{ state: 'working' }]} onNew={noop} onMenu={noop} />
        </Cell>
      </div>
      <div class="gal-panel">
        <Cap>Панель слева · 36, 34 и 28</Cap>
        <div class="gal-panel__list">
          <ProjectRow density="panel" name="backend" expanded marks={[{ state: 'waiting' }, { state: 'working' }]} moreCount={12} onNew={noop} onMenu={noop}>
            <SessionRow layout="line" nested density="panel" selected="echo" state="waiting" title="BACKEND-10300: лимиты" copy />
            <SessionRow layout="line" nested density="panel" state="working" title="Ревью ветки BACKEND-10296" />
            <SessionRow layout="line" nested density="panel" state="idle" title="BACKEND-10281: вебхуки" time="вчера" />
          </ProjectRow>
          <ProjectRow density="panel" name="mobile-app" marks={[{ state: 'error' }]} onNew={noop} onMenu={noop} />
        </div>
      </div>
    </Section>
  );
}

const CYCLE: { state: SessionState; isNew?: boolean }[] = [
  { state: 'starting' },
  { state: 'working' },
  { state: 'waiting', isNew: true },
  { state: 'waiting' },
  { state: 'error' },
  { state: 'exited' },
];

function Marks() {
  const [i, setI] = useState(0);
  const cur = CYCLE[i % CYCLE.length]!;
  return (
    <Section title="Отметки" text="Знак — в строках, плашка — в шапке сессии. Копия — контурная капсула.">
      <div class="gal-stack">
        <Cap>Знак состояния</Cap>
        <div class="gal-marks">
          <span class="gal-mark"><StatusMark state="starting" />запускается</span>
          <span class="gal-mark"><StatusMark state="working" />работает</span>
          <span class="gal-mark"><StatusMark state="waiting" />ждёт</span>
          <span class="gal-mark"><StatusMark state="waiting" isNew />ждёт · новое</span>
          <span class="gal-mark"><StatusMark state="error" />ошибка</span>
          <span class="gal-mark gal-mark--check"><StatusMark state="exited" />завершилась</span>
          <span class="gal-mark"><StatusMark state="idle" />в истории</span>
        </div>
      </div>
      <div class="gal-stack">
        <Cap>Плашка состояния</Cap>
        <div class="gal-wrap">
          <StatePill state="starting" />
          <StatePill state="working" elapsed="7:12" />
          <StatePill state="waiting" />
          <StatePill state="error" exitCode={1} />
          <StatePill state="exited" />
          <NewBadge />
          <NewBadge size={16} />
          <StatePill state="idle" elapsed="вчера, 18:40" />
        </div>
      </div>
      <div class="gal-stack">
        <Cap>Смена состояния · перекрёстное затухание за 180 мс</Cap>
        <div class="gal-wrap">
          <StatePill state={cur.state} isNew={cur.isNew} elapsed={cur.state === 'working' ? '7:12' : undefined} exitCode={1} />
          <Button variant="regular" size={32} onClick={() => setI(i + 1)}>
            Следующее состояние
          </Button>
        </div>
      </div>
      <div class="gal-stack">
        <Cap>Отметка копии · в шапке, при наведении, в строке</Cap>
        <div class="gal-copy">
          <CopyBadge path="~/.nit/worktrees/backend/BACKEND-10300" />
          <div class="gal-copy__hover">
            <span class="copy-badge is-hover">
              <Icon name="fork_right" size={16} />
              копия
            </span>
            <TooltipBox block>
              <span class="tooltip__title">{COPY_TITLE}</span>
              <span class="tooltip__path">~/.nit/worktrees/backend/BACKEND-10300</span>
            </TooltipBox>
          </div>
          <div class="gal-w272">
            <SessionRow layout="line" state="waiting" title="BACKEND-10300: лимиты" copy class="is-hover" />
          </div>
        </div>
      </div>
      <div class="gal-stack">
        <Cap>Иконка агента · 18 в меню, 24 в чипах, 28 в списках, 32 в шапке</Cap>
        <div class="gal-wrap">
          {(['claude', 'codex', 'grok', 'kimi', 'cursor', 'zcode'] as AgentId[]).map((a) => (
            <AgentBadge key={a} agent={a} size={18} />
          ))}
          <span class="gal-gap" />
          {(['claude', 'codex', 'grok', 'kimi', 'cursor', 'zcode'] as AgentId[]).map((a) => (
            <AgentBadge key={a} agent={a} size={24} />
          ))}
          <span class="gal-gap" />
          <AgentBadge agent="claude" size={28} />
          <AgentBadge agent="claude" size={28} raised />
          <AgentBadge agent="codex" size={32} />
          <AgentBadge agent="zcode" size={18} disabled />
        </div>
      </div>
      <div class="gal-stack">
        <Cap>Клавиши · в подсказках и меню 20, в настройках 24</Cap>
        <div class="gal-wrap">
          <Kbd>N</Kbd>
          <KbdCombo keys={['Ctrl', 'K']} />
          <KbdCombo keys={['Alt', '↓']} />
          <Kbd>Esc</Kbd>
          <span class="gal-gap" />
          <Kbd size="md">N</Kbd>
          <KbdCombo size="md" keys={['Ctrl', 'Enter']} />
        </div>
      </div>
    </Section>
  );
}

function Menus() {
  const session = useMenu();
  const project = useMenu();
  return (
    <Section title="Меню" text="Пункт 36 px: иконка, подпись, справа сочетание, значение или стрелка подменю.">
      <div class="gal-menus">
        <Cell cap="Меню сессии «⋯» · пункт под курсором, недоступный пункт">
          <Menu inline open onClose={noop} label="Меню сессии" items={SESSION_MENU} activeId="pin" />
        </Cell>
        <Cell cap="Меню проекта · путь, значение, подменю">
          <Menu inline open onClose={noop} label="Меню проекта" items={PROJECT_MENU} activeId="agent" />
        </Cell>
        <div class="gal-menus__sub">
          <Cell cap="Подменю">
            <Menu inline open onClose={noop} label="Агент по умолчанию" width={220} gap={10} items={agentMenu('claude')} activeId="claude" />
          </Cell>
        </div>
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Работают</span>
        <IconButton icon="more_horiz" label="Меню сессии" pressed={session.open} aria-haspopup="menu" aria-expanded={session.open} onClick={session.toggle} />
        <Button variant="regular" icon="folder" onClick={project.toggle} aria-haspopup="menu" aria-expanded={project.open}>
          Меню проекта
        </Button>
        <Cap>Стрелки, Enter, → и ← для подменю, Esc, первая буква</Cap>
        <Menu open={session.open} anchor={session.anchor} onClose={session.close} label="Меню сессии" placement="bottom-end" items={SESSION_MENU} />
        <Menu open={project.open} anchor={project.anchor} onClose={project.close} label="Меню проекта" items={PROJECT_MENU} />
      </div>
    </Section>
  );
}

function FinishBody() {
  return (
    <div class="gal-points">
      <DialogPoint kind="ok">Агент остановится, терминал закроется</DialogPoint>
      <DialogPoint kind="ok">Сессия отметится завершённой и останется в проекте и поиске</DialogPoint>
      <DialogPoint kind="warn" detail="2 файла не закоммичены. Закоммитьте их или удалите копию вручную.">
        Копия и ветка <span class="gal-code">BACKEND-10300</span> не удалятся
      </DialogPoint>
    </div>
  );
}

function Dialogs() {
  const [open, setOpen] = useState(false);
  const footer = (close: () => void) => (
    <>
      <Button variant="quiet" onClick={close}>
        Отмена
      </Button>
      <Button
        variant="primary"
        icon="task_alt"
        data-autofocus
        onClick={() => {
          close();
          toast.show({ title: 'Сессия завершена', action: { label: 'Отменить', onClick: noop } });
        }}
      >
        Завершить
      </Button>
    </>
  );
  return (
    <Section title="Диалог" text="Ширина 480, поля 32. Справа внизу — основное действие.">
      <div class="gal-dialog">
        <Dialog inline open onClose={noop} title="Завершить задачу?" subtitle="BACKEND-10300: лимиты запросов · backend" footer={footer(noop)}>
          <FinishBody />
        </Dialog>
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Работает</span>
        <Button variant="regular" icon="task_alt" onClick={() => setOpen(true)}>
          Завершить…
        </Button>
        <Cap>Фокус заперт внутри, Esc и щелчок по затемнению закрывают</Cap>
      </div>
      <Dialog open={open} onClose={() => setOpen(false)} title="Завершить задачу?" subtitle="BACKEND-10300: лимиты запросов · backend" footer={footer(() => setOpen(false))}>
        <FinishBody />
      </Dialog>
    </Section>
  );
}

function Toasts() {
  return (
    <Section
      title="Сообщение и подсказка"
      text="Сообщение — внизу справа, 6 секунд, под курсором не исчезает. Подсказка — через 500 мс, с клавишей, если она есть."
    >
      <div class="gal-wrap gal-wrap--top">
        <Cell cap="С действием">
          <ToastBox title="Сессия завершена" action={{ label: 'Отменить', onClick: noop }} />
        </Cell>
        <Cell cap="Новая ждущая">
          <ToastBox kind="waiting" title="Codex ждёт · shop-api" text="Нашёл две проблемы. Поправить обе?" action={{ label: 'Открыть', onClick: noop }} />
        </Cell>
        <Cell cap="Ошибка">
          <ToastBox kind="error" title="Codex не запустился" text="codex: command not found" />
        </Cell>
      </div>
      <div class="gal-line">
        <span class="gal-rowlabel gal-w110">Работают</span>
        <Button variant="regular" size={32} onClick={() => toast.show({ title: 'Сессия завершена', action: { label: 'Отменить', onClick: noop } })}>
          С действием
        </Button>
        <Button
          variant="regular"
          size={32}
          onClick={() =>
            toast.show({ kind: 'waiting', title: 'Codex ждёт · shop-api', text: 'Нашёл две проблемы. Поправить обе?', action: { label: 'Открыть', onClick: noop } })
          }
        >
          Новая ждущая
        </Button>
        <Button variant="regular" size={32} onClick={() => toast.show({ kind: 'error', title: 'Codex не запустился', text: 'codex: command not found' })}>
          Ошибка
        </Button>
      </div>
      <div class="gal-tips">
        <div class="gal-stack gal-stack--start">
          <Cap>Подсказка с клавишей</Cap>
          <Button variant="primary" icon="add" class="is-hover">
            Новая сессия
          </Button>
          <TooltipBox shortcut={['N']}>Новая сессия</TooltipBox>
        </div>
        <div class="gal-stack">
          <Cap>Другие сочетания</Cap>
          <div class="gal-wrap">
            <TooltipBox shortcut={['Ctrl', 'K']} class="gal-flat">Поиск</TooltipBox>
            <TooltipBox shortcut={['Alt', '↓']} class="gal-flat">Следующая ждущая</TooltipBox>
            <TooltipBox shortcut={['Ctrl', 'B']} class="gal-flat">Спрятать панель</TooltipBox>
            <TooltipBox shortcut={['H']} class="gal-flat">История</TooltipBox>
          </div>
        </div>
        <div class="gal-stack">
          <Cap>Полное название</Cap>
          <TooltipBox block class="gal-flat">BACKEND-10300: лимиты запросов для ключей API</TooltipBox>
        </div>
        <div class="gal-stack">
          <Cap>Наведите: соседняя подсказка — сразу</Cap>
          <div class="gal-wrap">
            <IconButton icon="search" label="Поиск" shortcut={['Ctrl', 'K']} />
            <IconButton icon="left_panel_close" label="Спрятать панель" shortcut={['Ctrl', 'B']} />
            <IconButton icon="history" label="История" shortcut={['H']} />
            <IconButton icon="settings" label="Настройки" />
          </div>
        </div>
      </div>
    </Section>
  );
}

function Toggles() {
  const [on, setOn] = useState(true);
  const [skip, setSkip] = useState(false);
  const [picked, setPicked] = useState(true);
  return (
    <Section title="Переключатель и флажок" text="Из настроек и экрана перезагрузки.">
      <div class="gal-flow">
        <Cell cap="Включён">
          <Switch label="Показывать в списке" checked={on} onChange={setOn} />
        </Cell>
        <Cell cap="Выключен">
          <Switch label="Показывать в списке" checked={false} />
        </Cell>
        <Cell cap="Фокус">
          <Switch label="Показывать в списке" checked class="is-focus" />
        </Cell>
        <Cell cap="Недоступен">
          <Switch label="Показывать в списке" checked={false} disabled />
        </Cell>
        <Cell cap="С подписью">
          <Switch label="Показывать в списке" showLabel checked={on} onChange={setOn} />
        </Cell>
      </div>
      <div class="gal-flow">
        <Cell cap="Отмечен">
          <Checkbox ariaLabel="Продолжить сессию" checked={picked} onChange={setPicked} />
        </Cell>
        <Cell cap="Не отмечен">
          <Checkbox ariaLabel="Продолжить сессию" checked={false} />
        </Cell>
        <Cell cap="Фокус">
          <Checkbox ariaLabel="Продолжить сессию" checked class="is-focus" />
        </Cell>
        <Cell cap="Недоступен">
          <Checkbox ariaLabel="Продолжить сессию" checked disabled />
        </Cell>
        <Cell cap="С подписью и пояснением">
          <Checkbox
            checked={skip}
            onChange={setSkip}
            label="Запускать без подтверждений"
            description={
              <>
                Добавит флаг <span class="gal-code gal-code--sm">--dangerously-skip-permissions</span>
              </>
            }
          />
        </Cell>
      </div>
    </Section>
  );
}

function TerminalSection() {
  const term = useRef<TerminalHandle>(null);
  const [size, setSize] = useState('');
  const line = useRef('');
  return (
    <Section title="Терминал" text="xterm.js: JetBrains Mono 13/17, цвета из токенов. Размер подгоняется сразу, без анимации.">
      <div class="gal-term">
        <Terminal
          ref={term}
          onReady={(t) => t.write(TERMINAL_DEMO)}
          onResize={({ cols, rows }) => setSize(`${cols} × ${rows}`)}
          onData={(d) => {
            // Эхо для проверки ввода: Enter — новая строка с приглашением
            const t = term.current;
            if (!t) return;
            if (d === '\r') {
              t.write(`\r\n\x1b[90m$ \x1b[0m`);
              line.current = '';
            } else if (d === '\x7f') {
              if (line.current) {
                line.current = line.current.slice(0, -1);
                t.write('\b \b');
              }
            } else if (d >= ' ') {
              line.current += d;
              t.write(d);
            }
          }}
        />
      </div>
      <div class="gal-line">
        <Cap>Размер {size || '…'} · потяните угол рамки, чтобы проверить подгонку</Cap>
      </div>
    </Section>
  );
}

function Icons() {
  return (
    <Section title="Иконки" text="Material Symbols Outlined, только эти 45. Толщина: 300 на 18 px, 350 на 16 px, 400 на 14 px.">
      <div class="gal-wrap">
        {([18, 16, 14] as const).map((s) => (
          <span key={s} class="gal-icons-size">
            <Icon name="search" size={s} />
            <Icon name="fork_right" size={s} />
            <Icon name="more_horiz" size={s} />
            <Cap>{s}</Cap>
          </span>
        ))}
      </div>
      <div class="gal-icons">
        {ICON_NAMES.map((n) => (
          <div key={n} class="gal-icon">
            <Icon name={n} size={18} />
            <span class="gal-icon__name">{n}</span>
          </div>
        ))}
      </div>
    </Section>
  );
}
