import { AgentBadge, Dialog, Icon } from '../components';
import type { AgentId } from '../model';

export interface SheetOption {
  id: string;
  label: string;
  /** Вторая строка моноширинным: путь проекта */
  description?: string;
  agent?: AgentId;
  /** Тихая подпись справа: «по умолчанию» */
  hint?: string;
  disabled?: boolean;
}

interface SheetProps {
  open: boolean;
  title: string;
  options: SheetOption[];
  value: string;
  onPick: (id: string) => void;
  onClose: () => void;
}

/** Выбор одного значения на телефоне: лист снизу, строки 52 px, галочка у выбранного */
export function Sheet({ open, title, options, value, onPick, onClose }: SheetProps) {
  return (
    <Dialog open={open} onClose={onClose} title={title} width={600} class="ph-sheet">
      <div class="ph-sheet__list" role="listbox" aria-label={title}>
        {options.map((o) => {
          const selected = o.id === value;
          return (
            <button
              key={o.id}
              type="button"
              role="option"
              class="ph-opt"
              aria-selected={selected}
              aria-disabled={o.disabled || undefined}
              data-autofocus={selected || undefined}
              onClick={() => {
                if (o.disabled) return;
                onPick(o.id);
                onClose();
              }}
            >
              {o.agent && <AgentBadge agent={o.agent} size={24} decorative disabled={o.disabled} />}
              <span class="ph-opt__text">
                <span class="ph-opt__label">{o.label}</span>
                {o.description && <span class="ph-opt__desc">{o.description}</span>}
              </span>
              {o.hint && <span class="ph-opt__hint">{o.hint}</span>}
              {selected && <Icon name="check" size={18} class="ph-opt__check" />}
            </button>
          );
        })}
      </div>
    </Dialog>
  );
}
