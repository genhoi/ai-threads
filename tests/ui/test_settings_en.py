"""Экран «Настройки» в английском браузере: тексты разделов, полоса сохранения и смена языка.
Тексты, которые присылает сервер (подсказки моделей, ошибки), здесь не проверяются."""

import json
import os
import re
import threading
from pathlib import Path
from types import SimpleNamespace

import pytest
from playwright.sync_api import expect

from ai_threads.catalog import Catalog
from ai_threads.server import create_server
from ai_threads.store import Store


def stored():
    path = Path(os.environ['AI_THREADS_CONFIG'])
    return json.loads(path.read_text(encoding='utf-8')) if path.exists() else None


def open_settings(page, server, section=''):
    page.goto(server.url + '/settings.html' + (f'#section={section}' if section else ''))
    expect(page.get_by_test_id('section-title')).to_be_visible()


def card(page, agent):
    return page.locator(f'[data-testid="agent-card"][data-agent="{agent}"]')


def save(page, toast):
    with page.expect_response('**/api/settings') as response:
        page.get_by_test_id('save').click()
    assert response.value.ok
    expect(page.get_by_test_id('toast')).to_have_text(toast)
    expect(page.get_by_test_id('savebar')).to_have_count(0)


@pytest.fixture
def bare_server(tmp_path, monkeypatch):
    """Сервер без агентов: их папок нет, в PATH нет программ."""
    programs = tmp_path / 'bin'
    programs.mkdir()
    monkeypatch.setenv('PATH', str(programs))
    monkeypatch.setenv('AI_THREADS_DATA', str(tmp_path / 'data'))
    catalog = Catalog(tmp_path / 'data/cache.json')
    assert catalog.wait(10), catalog.error
    server = create_server(0, catalog=catalog, store=Store(tmp_path / 'data/state.json'))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield SimpleNamespace(url=f'http://127.0.0.1:{server.server_port}')
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)


def test_agents_in_english(page_en, ui_server):
    page = page_en
    open_settings(page, ui_server)
    assert page.evaluate('document.documentElement.lang') == 'en'
    assert page.title() == 'Nit — settings'
    expect(page.get_by_test_id('section-title')).to_have_text('Agents')
    expect(page.get_by_test_id('section')).to_contain_text('Where Nit finds sessions and how it resumes them.')
    expect(page.locator('.side-title')).to_have_text('Settings')
    expect(page.get_by_test_id('section-agents')).to_have_text(re.compile(r'^Agents\d+ of \d+$'))
    expect(page.get_by_test_id('section-list')).to_have_text('Session list3 folders')
    expect(page.get_by_test_id('settings-path')).to_contain_text('Settings are stored in')
    expect(page.get_by_test_id('nav-mine')).to_have_text('Mine')
    expect(page.get_by_test_id('data-menu')).to_have_text('Data')
    expect(page.get_by_test_id('check-note')).to_have_text('checked just now')
    expect(page.get_by_test_id('missing-heading')).to_have_text('NOT FOUND')
    codex = card(page, 'codex')
    expect(codex.get_by_test_id('agent-status')).to_have_text(
        re.compile(r'^Found: \S*stubs/codex · 3 sessions in \S*/tools/codex$'))
    expect(codex.get_by_test_id('home-env')).to_have_text('from CODEX_HOME')
    expect(codex.get_by_test_id('agent-program')).to_have_attribute('placeholder', re.compile(r'^found: \S*stubs/codex$'))
    expect(codex).to_contain_text('Sessions folder')
    expect(codex).to_contain_text('Resume command')
    expect(codex.locator('.skip')).to_contain_text(
        'Run without approval promptsAdds the --dangerously-bypass-approvals-and-sandbox flag to the command')
    expect(codex.get_by_test_id('preview-label')).to_have_text(
        ['Example for a session in sample app:', 'Example for a codex exec session in sample app:'])
    expect(card(page, 'grok').get_by_role('switch', name='Show Grok in list')).to_have_attribute('aria-checked', 'true')
    expect(card(page, 'grok').locator('.show-toggle')).to_have_text('Show in list')
    zcode = card(page, 'zcode')
    expect(zcode.get_by_test_id('agent-status')).to_have_text('Not installed')
    expect(zcode.get_by_test_id('agent-open')).to_have_text('Set manually')
    zcode.get_by_test_id('agent-open').click()
    expect(zcode.get_by_test_id('agent-open')).to_have_text('Collapse')
    # полоса сохранения
    codex.get_by_test_id('agent-skip').check()
    expect(codex.get_by_test_id('agent-dirty')).to_have_text('changed')
    bar = page.get_by_test_id('savebar')
    expect(bar).to_contain_text('Unsaved changes')
    expect(page.get_by_test_id('save-count')).to_have_text(' · 1 change')
    card(page, 'grok').get_by_role('switch').click()
    expect(page.get_by_test_id('save-count')).to_have_text(' · 2 changes')
    expect(page.get_by_test_id('revert')).to_have_text('Discard')
    expect(page.get_by_test_id('save')).to_have_text('Save')
    expect(page.get_by_test_id('save')).to_have_attribute('title', 'Save settings')
    save(page, 'Saved')
    assert stored() == {'agents': {'codex': {'skip_approvals': True}, 'grok': {'enabled': False}}}
    expect(card(page, 'grok').get_by_test_id('agent-status')).to_have_text(re.compile(r'^Found: \S*stubs/grok · hidden from the list$'))
    with page.expect_request('**/api/config?refresh=1'):
        page.get_by_test_id('check-again').click()
    expect(page.get_by_test_id('toast')).to_have_text(re.compile(r'^Check done: found \d+ of \d+$'))
    expect(page.get_by_test_id('check-again')).to_have_text('Check again')


def test_switch_language_to_russian_and_back(page_en, ui_server):
    page = page_en
    open_settings(page, ui_server, 'summaries')
    expect(page.get_by_test_id('section-title')).to_have_text('Summaries and search')
    expect(page.get_by_test_id('sum-summary')).to_contain_text('Session summary')
    expect(page.get_by_test_id('sum-digest')).to_contain_text('Multi-day digest')
    search = page.get_by_test_id('sum-search')
    expect(search.get_by_role('group', name='Searched by: Smart search')).to_be_visible()
    expect(search.get_by_test_id('pick-auto')).to_have_text('Auto')
    expect(search.get_by_test_id('pick-auto')).to_have_attribute('title', 'Kimi, or the first one installed')
    expect(search).to_contain_text('the agent answers twice: usually 1–3 minutes')
    search.get_by_test_id('timeout').fill('0')
    expect(search.get_by_test_id('field-error')).to_have_text('Enter 1 to 60 minutes')
    expect(page.get_by_test_id('save-problems')).to_have_text('Fix the timeout to save')
    expect(page.get_by_test_id('save')).to_have_attribute('title', 'Fix the errors first')
    page.get_by_test_id('revert').click()
    # язык: «Авто» — как в браузере, названия языков на самих языках
    common = page.get_by_test_id('sum-common')
    expect(common).to_contain_text('For the interface and summaries.')
    expect(page.get_by_role('group', name='Language').get_by_role('button')).to_have_text(['Auto', 'Русский', 'English'])
    expect(page.get_by_test_id('lang-auto')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('lang-auto')).to_have_attribute('title', 'same as the browser')
    # сохранили русский — страница перерисовалась по-русски, раздел и адрес те же
    page.get_by_test_id('lang-ru').click()
    expect(page.get_by_test_id('save-count')).to_have_text(' · 1 change')
    save(page, 'Сохранено')
    assert stored() == {'language': 'ru'}
    expect(page.get_by_test_id('section-title')).to_have_text('Сводки и поиск')
    expect(page.get_by_test_id('lang-ru')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('lang-auto')).to_have_text('Авто')
    expect(page.get_by_test_id('nav-mine')).to_have_text('Мои')
    expect(page.get_by_test_id('data-menu')).to_have_text('Данные')
    expect(page.get_by_test_id('section-agents')).to_contain_text('Агенты')
    expect(page).to_have_url(re.compile(r'#section=summaries$'))
    assert page.evaluate('document.documentElement.lang') == 'ru'
    assert page.title() == 'Нить — настройки'
    page.reload()
    expect(page.get_by_test_id('section-title')).to_have_text('Сводки и поиск')
    # «Авто» возвращает язык браузера
    page.get_by_test_id('lang-auto').click()
    save(page, 'Saved')
    assert stored() == {}
    expect(page.get_by_test_id('section-title')).to_have_text('Summaries and search')
    expect(page.get_by_test_id('nav-mine')).to_have_text('Mine')
    # «Данные» после перерисовки шапки работают
    page.get_by_test_id('data-menu').click()
    expect(page.get_by_test_id('data-export')).to_have_text('Export JSON')
    page.get_by_test_id('data-import-file').set_input_files({
        'name': 'import.json', 'mimeType': 'application/json', 'buffer': json.dumps({'version': 2, 'hidden': []}).encode()})
    expect(page.get_by_test_id('toast')).to_have_text('Data imported')


def test_hidden_dirs_and_about_in_english(page_en, ui_server):
    page = page_en
    open_settings(page, ui_server, 'list')
    expect(page.get_by_test_id('section-title')).to_have_text('Session list')
    expect(page.get_by_test_id('hidden-dirs')).to_contain_text('Hidden folders')
    expect(page.get_by_test_id('hidden-count')).to_have_text('These folders hide 0 sessions right now.')
    add = page.get_by_test_id('add-dir-input')
    expect(add).to_have_attribute('placeholder', 'Add a folder, e.g. ~/scratch')
    add.fill('projects')
    expect(page.get_by_test_id('field-error')).to_have_text('Path must start with / or ~/')
    add.fill('/tmp')
    expect(page.get_by_test_id('field-error')).to_have_text('This folder is already in the list')
    expect(page.get_by_test_id('add-dir')).to_have_text('Add')
    page.get_by_role('button', name='Remove /var/tmp').click()
    expect(page.get_by_test_id('section-list')).to_contain_text('2 folders')
    page.get_by_test_id('section-about').click()
    about = page.get_by_test_id('about')
    expect(about.locator('dt')).to_have_text(['Address', 'Data', 'Settings file', 'Version'])
    expect(about).to_contain_text('Manual edits are picked up without a restart.')
    expect(page.get_by_test_id('about-github')).to_have_text('Source code on GitHub')


def test_empty_state_in_english(page_en, bare_server):
    page = page_en
    open_settings(page, bare_server)
    panel = page.get_by_test_id('agents-empty')
    expect(panel).to_contain_text('No agents found')
    expect(panel).to_contain_text("Nit didn't find any agent programs or session folders.")
    expect(panel).to_contain_text('program codex — not in PATH')
    expect(panel).to_contain_text('absent-codex — no such folder')
    expect(panel).to_contain_text('Where we looked')
    expect(panel.locator('.steps li')).to_have_count(3)
    expect(panel.locator('.steps code')).to_have_count(1)
    expect(card(page, 'codex').get_by_test_id('agent-open')).to_have_text('Collapse')
    expect(card(page, 'codex')).to_contain_text('Set the program path and save. If it is found, the agent shows up in the list.')
    expect(card(page, 'codex').get_by_test_id('agent-program')).to_have_attribute(
        'placeholder', 'not found — set the path, e.g. ~/.local/bin/codex')
    expect(page.get_by_test_id('agent-status').first).to_have_text('Not installed')
    page.get_by_test_id('section-summaries').click()
    expect(page.get_by_test_id('no-runner').first).to_have_text('No agents installed — summaries are unavailable.')
    expect(page.get_by_test_id('section-summaries')).to_contain_text('no agent')


def test_narrow_window_in_english(page_en, ui_server):
    page = page_en
    page.set_viewport_size({'width': 760, 'height': 900})
    page.goto(ui_server.url + '/settings.html')
    expect(page.locator('.narrow-title')).to_have_text('Settings')
    expect(page.get_by_test_id('section-list')).to_contain_text('Which sessions show up in Mine.')
    page.get_by_test_id('section-list').click()
    expect(page.get_by_test_id('back-to-menu')).to_have_text('← Settings')
    assert page.evaluate('document.documentElement.scrollWidth') <= 760
