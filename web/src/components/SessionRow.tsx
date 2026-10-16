import type { AgentId, SessionState } from '../model';
import { AgentBadge } from './AgentBadge';
import { NewBadge } from './Badges';
import { IconButton } from './Button';
import { Icon } from './Icon';
import { StatusMark } from './StatusMark';

export interface SessionRowProps {
  state: SessionState;
  /** Ждёт и закончила ход после последнего просмотра: ореол, жирное название, «новое» */
  isNew?: boolean;
  /** Отмечена человеком как завершённая: галочка, приглушённое название */
  done?: boolean;
  title: string;
  /** Справа: «26 мин» (ждёт), «7:12» (работает), «вчера» (в истории) */
  time?: string;
  /** Вторая строка: последняя фраза агента или команда, которую он выполняет */
  text?: string;
  exitCode?: number;
  /** Агент и проект: «Codex · shop-api» */
  meta?: string;
  /** Агент: иконка в однострочной строке (дерево проекта) */
  agent?: AgentId;
  /** Сессия в копии репозитория: значок в однострочной строке */
  copy?: boolean;
  /** block — несколько строк («Ждут вас», «Работают»), line — одна (проект, история) */
  layout?: 'block' | 'line';
  /** Строка внутри раскрытого проекта */
  nested?: boolean;
  /** sheet — размеры листа компонентов, panel — плотнее, как в панели слева */
  density?: 'sheet' | 'panel';
  /** true — открыта эта сессия; echo — та же сессия выбрана в другом разделе панели */
  selected?: boolean | 'echo';
  /** Открыто меню «⋯» этой строки: кнопка меню остаётся видна */
  menuOpen?: boolean;
  onOpen?: () => void;
  /** Без обработчика кнопки меню нет */
  onMenu?: (anchor: HTMLElement) => void;
  class?: string;
}

function timeClass(state: SessionState) {
  if (state === 'waiting') return 'srow__time srow__time--waiting';
  if (state === 'working') return 'srow__time srow__time--working';
  if (state === 'starting') return 'srow__time srow__time--starting';
  return 'srow__time';
}

/**
 * Строка сессии. В «Ждут вас» три строки: название, последняя фраза, агент и проект; в «Работают» —
 * две: название с таймером и команда; в проекте и истории — одна. При наведении время сменяется меню «⋯».
 */
export function SessionRow(props: SessionRowProps) {
  const { layout = 'block', density = 'sheet', selected, menuOpen, onMenu, class: cls } = props;
  const classes = [
    'srow',
    `srow--${layout}`,
    `srow--${density}`,
    props.isNew && layout === 'block' && 'srow--new',
    props.nested && 'srow--nested',
    layout === 'line' && (props.done || props.state === 'exited') && 'srow--check',
    layout === 'line' && (props.done || props.state === 'exited' || props.state === 'idle') && 'is-muted',
    selected === true && 'is-selected',
    selected === 'echo' && 'is-echo',
    menuOpen && 'has-menu',
    onMenu && layout === 'block' && 'srow--menu',
    cls,
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div class={classes}>
      <button type="button" class="srow__main" aria-current={selected === true ? 'page' : undefined} onClick={props.onOpen}>
        {layout === 'block' ? <BlockBody {...props} /> : <LineBody {...props} />}
      </button>
      {onMenu && layout === 'block' && (
        <IconButton
          icon="more_horiz"
          label="Меню сессии"
          size={28}
          class="srow__more"
          pressed={menuOpen}
          aria-haspopup="menu"
          aria-expanded={!!menuOpen}
          onClick={(e) => onMenu(e.currentTarget)}
        />
      )}
    </div>
  );
}

function BlockBody({ state, isNew, title, time, text, exitCode, meta, density, onMenu }: SessionRowProps) {
  let second = null;
  if (state === 'error') second = <span class="srow__error">{exitCode === undefined ? 'Ошибка' : `Ошибка · код ${exitCode}`}</span>;
  else if (state === 'working') second = text ? <span class="srow__cmd">{text}</span> : null;
  else if (state === 'starting') second = <span class="srow__text">{text ?? 'Запускается…'}</span>;
  else if (text) second = <span class="srow__text">{text}</span>;

  // Мета-строка есть у ждущих и упавших; у работающих её нет
  const showMeta = meta && (state === 'waiting' || state === 'error' || state === 'exited' || state === 'idle');
  return (
    <>
      <StatusMark state={state} isNew={isNew} class="srow__mark" />
      <span class="srow__body">
        <span class="srow__line1">
          <span class="srow__title">{title}</span>
          {time && <span class={timeClass(state)}>{time}</span>}
          {onMenu && <span class="srow__spacer" />}
        </span>
        {second}
        {showMeta && (
          <span class="srow__meta">
            {meta}
            {state === 'waiting' && isNew && <NewBadge size={density === 'panel' ? 16 : 18} />}
          </span>
        )}
      </span>
    </>
  );
}

function LineBody({ state, done, title, time, copy, agent }: SessionRowProps) {
  const check = done || state === 'exited';
  return (
    <>
      {check ? <Icon name="check" size={14} class="srow__check" label="завершена" /> : <StatusMark state={state} class="srow__mark" />}
      {agent && <AgentBadge agent={agent} size={16} class="srow__agent" />}
      <span class="srow__title">{title}</span>
      {copy && <Icon name="fork_right" size={14} class="srow__copy" label="копия" />}
      {time && <span class="srow__date">{time}</span>}
    </>
  );
}
