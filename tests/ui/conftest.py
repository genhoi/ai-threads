"""Браузерные проверки с отдельным сервером и копиями журналов для каждого теста."""

import hashlib
import os
import shutil
import threading
from pathlib import Path
from types import SimpleNamespace

import pytest
from playwright.sync_api import Error, sync_playwright

from ai_threads.catalog import Catalog
from ai_threads.server import create_server
from ai_threads.store import Store

ROOT = Path(__file__).resolve().parents[2]


def log_hashes(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob('*') if p.is_file()}


@pytest.fixture(scope='session', params=['chromium', 'webkit'])
def browser(request):
    with sync_playwright() as playwright:
        try:
            instance = getattr(playwright, request.param).launch()
        except Error as error:
            if request.param == 'webkit' and ('Host system is missing dependencies' in str(error)
                                              or 'error while loading shared libraries' in str(error)):
                pytest.skip('WebKit: отсутствуют системные библиотеки: ' + str(error).splitlines()[0])
            raise
        yield instance
        instance.close()


@pytest.fixture
def page(browser):
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, timezone_id='Europe/Samara')
    context.add_init_script("""Object.defineProperty(navigator, 'clipboard', {
        value: {writeText: async text => {window.copiedText = text;}}, configurable: true});""")
    page = context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    yield page
    page.unroute_all(behavior='ignoreErrors')
    context.close()
    assert not errors, errors


@pytest.fixture
def ui_server(tmp_path, monkeypatch):
    homes = tmp_path / 'tools'
    shutil.copytree(ROOT / 'tests/fixtures', homes)
    for tool, variable in [('codex', 'CODEX_HOME'), ('claude', 'CLAUDE_HOME'),
                           ('grok', 'GROK_HOME'), ('kimi', 'KIMI_CODE_HOME')]:
        monkeypatch.setenv(variable, str(homes / tool))
    monkeypatch.setenv('AI_THREADS_DATA', str(tmp_path / 'data'))
    # Заглушка VS Code: пункт меню виден на любой машине, а щелчок не запускает настоящий редактор.
    programs = tmp_path / 'bin'
    programs.mkdir()
    (programs / 'code').write_text('#!/bin/sh\nexit 0\n')
    (programs / 'code').chmod(0o755)
    monkeypatch.setenv('PATH', os.pathsep.join([str(ROOT / 'tests/stubs'), str(programs), os.environ['PATH']]))
    monkeypatch.setenv('WSL_DISTRO_NAME', 'Ubuntu')
    monkeypatch.setenv('AI_THREADS_SUMMARY_TIMEOUT', '3')
    before = log_hashes(homes)
    catalog = Catalog(tmp_path / 'data/cache.json')
    assert catalog.wait(10), catalog.error
    store = Store(tmp_path / 'data/state.json')
    store.set_pin('codex:00000000-0000-4000-8000-000000000001', True)
    server = create_server(0, catalog=catalog, store=store)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield SimpleNamespace(url=f'http://127.0.0.1:{server.server_port}', server=server,
                              catalog=catalog, store=store, homes=homes, tmp=tmp_path)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)
        catalog.wait(10)
        assert log_hashes(homes) == before, 'Интерфейс изменил журналы фикстур'


@pytest.fixture
def no_wsl(ui_server, monkeypatch):
    """Тот же сервер вне WSL: Windows Terminal и проводник Windows недоступны."""
    monkeypatch.delenv('WSL_DISTRO_NAME')
    return ui_server


@pytest.fixture
def save_settings(page, ui_server):
    """Записать настройки через POST /api/settings, как это сделает экран настроек."""
    def save(values):
        response = page.request.post(ui_server.url + '/api/settings', data={'settings': values},
                                     headers={'Origin': ui_server.url})
        assert response.ok, response.text()
        ui_server.catalog.wait(10)
        return response.json()
    return save
