import type { ComponentChildren } from 'preact';
import { IconButton } from '../components';

interface PhoneHeaderProps {
  /** Стрелка назад */
  back?: () => void;
  /** Крестик вместо стрелки: закрыть новую сессию */
  close?: () => void;
  title: string;
  /** Строка под названием: состояние сессии */
  sub?: ComponentChildren;
  /** Кнопки справа */
  actions?: ComponentChildren;
  /** size: list — крупный заголовок списка, page — 18 px, session — название в две строки */
  size?: 'list' | 'page' | 'session';
}

/** Шапка экрана телефона, 64 px: назад, название, кнопки 48 px */
export function PhoneHeader({ back, close, title, sub, actions, size = 'page' }: PhoneHeaderProps) {
  return (
    <header class={`ph-head ph-head--${size}${back || close ? ' ph-head--nav' : ''}`}>
      {back && <IconButton icon="arrow_back" label="Назад" size={48} noTooltip class="ph-head__nav" onClick={back} />}
      {close && <IconButton icon="close" label="Закрыть" size={48} noTooltip class="ph-head__nav" onClick={close} />}
      <div class="ph-head__text">
        <h1 class="ph-head__title">{title}</h1>
        {sub && <span class="ph-head__sub">{sub}</span>}
      </div>
      {actions}
    </header>
  );
}
