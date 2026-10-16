import type { ComponentChildren } from 'preact';
import { useId } from 'preact/hooks';
import type { SessionState } from '../model';
import { IconButton } from './Button';
import { Icon } from './Icon';
import { StatusMark } from './StatusMark';

export interface ProjectMark {
  state: SessionState;
  isNew?: boolean;
}

export interface ProjectRowProps {
  name: string;
  expanded?: boolean;
  pinned?: boolean;
  /** Знаки активных сессий справа */
  marks?: ProjectMark[];
  /** sheet — размеры листа компонентов, panel — панели слева */
  density?: 'sheet' | 'panel';
  /** Открыто меню проекта: кнопки остаются видны */
  menuOpen?: boolean;
  onToggle?: () => void;
  /** Новая сессия в проекте; без обработчика кнопки «+» нет */
  onNew?: () => void;
  /** Меню проекта; без обработчика кнопки «⋯» нет */
  onMenu?: (anchor: HTMLElement) => void;
  /** Строки сессий проекта (SessionRow layout="line" nested) */
  children?: ComponentChildren;
  /** Сколько сессий не показано: строка «ещё N» */
  moreCount?: number;
  onMore?: () => void;
  class?: string;
}

/** Строка проекта: стрелка, имя, знаки активных сессий. При наведении — «+» и меню проекта. */
export function ProjectRow({
  name,
  expanded,
  pinned,
  marks = [],
  density = 'sheet',
  menuOpen,
  onToggle,
  onNew,
  onMenu,
  children,
  moreCount,
  onMore,
  class: cls,
}: ProjectRowProps) {
  const groupId = useId();
  const hasActions = !!(onNew || onMenu);
  const classes = [
    'prow',
    `prow--${density}`,
    pinned && 'prow--pinned',
    hasActions && 'prow--actions',
    menuOpen && 'has-menu',
    cls,
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div class="prow-group">
      <div class={classes}>
        <button
          type="button"
          class="prow__main"
          aria-expanded={!!expanded}
          aria-controls={expanded ? `pg-${groupId}` : undefined}
          onClick={onToggle}
        >
          <Icon name={expanded ? 'expand_more' : 'chevron_right'} size={18} class="prow__chevron" />
          <span class="prow__name">{name}</span>
          {pinned && <Icon name="push_pin" size={14} class="prow__pin" label="закреплён" />}
          {marks.length > 0 && (
            <span class="prow__marks">
              {marks.map((m, i) => (
                <StatusMark key={i} state={m.state} />
              ))}
            </span>
          )}
        </button>
        {hasActions && (
          <span class="prow__actions">
            {onNew && <IconButton icon="add" label="Новая сессия в проекте" size={28} onClick={onNew} />}
            {onMenu && (
              <IconButton
                icon="more_horiz"
                label="Меню проекта"
                size={28}
                pressed={menuOpen}
                aria-haspopup="menu"
                aria-expanded={!!menuOpen}
                onClick={(e) => onMenu(e.currentTarget)}
              />
            )}
          </span>
        )}
      </div>
      {expanded && (
        <div class="prow__children" id={`pg-${groupId}`} role="group" aria-label={name}>
          {children}
          {moreCount !== undefined && moreCount > 0 && (
            <button type="button" class="prow__more" onClick={onMore}>
              ещё {moreCount}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
