import { Icon } from './Icon';
import { Tooltip } from './Tooltip';

export interface NewBadgeProps {
  /** 18 — лист компонентов и телефон, 16 — плотная строка панели */
  size?: 16 | 18;
  class?: string;
}

/** Плашка «новое»: агент закончил ход после последнего просмотра */
export function NewBadge({ size = 18, class: cls }: NewBadgeProps) {
  return <span class={`new-badge new-badge--${size}${cls ? ' ' + cls : ''}`}>новое</span>;
}

export const COPY_TITLE = 'Копия репозитория, своя ветка';

export interface CopyBadgeProps {
  /** Папка копии: ~/.nit/worktrees/backend/BACKEND-10300 */
  path: string;
  class?: string;
}

/** Отметка копии (git worktree) в шапке: контурная капсула, путь — в подсказке */
export function CopyBadge({ path, class: cls }: CopyBadgeProps) {
  return (
    <Tooltip
      block
      placement="bottom-start"
      content={
        <>
          <span class="tooltip__title">{COPY_TITLE}</span>
          <span class="tooltip__path">{path}</span>
        </>
      }
    >
      <span class={`copy-badge${cls ? ' ' + cls : ''}`} tabIndex={0} role="img" aria-label={`${COPY_TITLE}: ${path}`}>
        <Icon name="fork_right" size={16} />
        копия
      </span>
    </Tooltip>
  );
}
