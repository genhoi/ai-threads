import { render } from 'preact';
import './styles/fonts.css';
import './styles/tokens.css';
import './styles/base.css';
import './styles/components.css';
import './styles/screens.css';
import './styles/pages.css';
import './styles/phone.css';
import './gallery/gallery.css';
import { installShortcuts } from './app/keys';
import { startRouter } from './app/route';
import { connectEvents, loadBootstrap } from './app/sync';
import { App } from './screens/Shell';

async function start() {
  // ?mock — интерфейс без сервера, на данных из макета. Код подмены грузится только здесь
  const params = new URLSearchParams(location.search);
  if (params.has('mock')) {
    const { installMock } = await import('./mock');
    installMock(params.get('mock') ?? '');
  }
  startRouter();
  installShortcuts();
  render(<App />, document.getElementById('app')!);
  await loadBootstrap();
  connectEvents();
}

void start();
