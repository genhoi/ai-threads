import { Button } from '../components';

/**
 * Телефон отключили от «Нити»: на компьютере нажали «Отключить» или сделали новый ключ.
 * Страница на весь экран, и на телефоне, и в узком окне; приложение за ней больше не работает
 */
export function Revoked() {
  return (
    <div class="revoked">
      <div class="revoked__inner" role="alert">
        <h1 class="revoked__title">Этот телефон отключён от «Нити»</h1>
        <p class="revoked__text">
          Чтобы подключить его снова, откройте на телефоне ссылку из настроек на компьютере: Настройки → Доступ с телефона. Можно навести камеру на QR-код.
        </p>
        <Button variant="regular" size={48} icon="restart_alt" onClick={() => location.replace('/')}>
          Открыть заново
        </Button>
      </div>
    </div>
  );
}
