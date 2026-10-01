"""Экран «Настройки»: агенты и их статусы, команды продолжения, сводки, скрытые папки,
пустое состояние, узкое окно и сохранение в файл настроек."""

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

SKIP = '--dangerously-bypass-approvals-and-sandbox'


def settings_file():
    return Path(os.environ['AI_THREADS_CONFIG'])


def stored():
    return json.loads(settings_file().read_text(encoding='utf-8'))


def open_settings(page, server, section=''):
    page.goto(server.url + '/settings.html' + (f'#section={section}' if section else ''))
    expect(page.get_by_test_id('section-title')).to_be_visible()


def card(page, agent):
    return page.locator(f'[data-testid="agent-card"][data-agent="{agent}"]')


def commands(page, server, tool):
    """Команды продолжения из /api/sessions, как их показывает пример: ручная и автоматическая сессия."""
    sessions = [s for s in page.request.get(server.url + '/api/sessions').json()['sessions'] if s['tool'] == tool]
    return ['$ ' + next(s for s in sessions if s['auto'] == auto and s['cwd'])['command'] for auto in (False, True)]


def save(page):
    with page.expect_response('**/api/settings') as response:
        page.get_by_test_id('save').click()
    assert response.value.ok
    expect(page.get_by_test_id('toast')).to_contain_text('Сохранено')
    expect(page.get_by_test_id('savebar')).to_have_count(0)


@pytest.fixture
def zcode_home(tmp_path, monkeypatch):
    """Пустая папка ZCode: агент найден, сессий у него нет. Задаётся до запуска сервера."""
    home = tmp_path / 'zcode-empty'
    home.mkdir()
    monkeypatch.setenv('ZCODE_HOME', str(home))
    return home


@pytest.fixture
def bare_server(tmp_path, monkeypatch):
    """Сервер без агентов: папок нет (их убирает isolated_agents), в PATH нет программ."""
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
        yield SimpleNamespace(url=f'http://127.0.0.1:{server.server_port}', server=server, catalog=catalog, path=str(programs))
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)


def test_agents_statuses_and_examples(page, ui_server):
    open_settings(page, ui_server)
    sessions = ui_server.catalog.sessions()
    expect(page.get_by_test_id('section-agents')).to_have_attribute('aria-current', 'page')
    expect(page.get_by_test_id('nav-all')).to_have_text(f'Все{len(sessions)}')
    expect(page.get_by_test_id('settings-link')).to_have_attribute('aria-current', 'page')
    expect(page.get_by_test_id('settings-path')).to_contain_text('settings.json')
    expect(page.get_by_test_id('server-addr')).to_have_text(re.compile(r'^127\.0\.0\.1:\d+$'))
    config = page.request.get(ui_server.url + '/api/config').json()
    found = [a for a in config['agents'] if a['home_exists'] or a['program']]
    expect(page.get_by_test_id('section-agents')).to_contain_text(f'{len(found)} из {len(config["agents"])}')
    # найденные агенты сверху, в порядке /api/config; ZCode без папки — под заголовком «НЕ НАЙДЕНЫ»
    order = page.get_by_test_id('agent-card').evaluate_all('cards => cards.map(c => c.dataset.agent)')
    assert order == [a['id'] for a in found] + [a['id'] for a in config['agents'] if a not in found]
    assert order[:4] == ['codex', 'claude', 'grok', 'kimi']
    expect(page.get_by_test_id('missing-heading')).to_have_text('НЕ НАЙДЕНЫ')
    expect(card(page, 'codex').get_by_test_id('agent-status')).to_have_text(
        re.compile(r'^Найден: \S*stubs/codex · 3 сессии в \S*/tools/codex$'))
    expect(card(page, 'kimi').get_by_test_id('agent-status')).to_contain_text('· 4 сессии в')
    expect(card(page, 'codex').locator('.dot.ok')).to_have_count(1)
    expect(card(page, 'zcode').get_by_test_id('agent-status')).to_have_text('Не установлен')
    expect(card(page, 'zcode').locator('.dot.none')).to_have_count(1)
    expect(card(page, 'zcode').get_by_test_id('agent-open')).to_have_text('Указать вручную')
    expect(card(page, 'zcode').locator('.card-body')).to_have_count(0)
    card(page, 'zcode').get_by_test_id('agent-open').click()
    expect(card(page, 'zcode').get_by_test_id('agent-open')).to_have_attribute('aria-expanded', 'true')
    expect(card(page, 'zcode').get_by_test_id('agent-home')).to_be_visible()
    expect(card(page, 'zcode').get_by_test_id('agent-program')).to_have_count(0)
    # папка из переменной окружения: поле заблокировано
    codex = card(page, 'codex')
    expect(codex.get_by_test_id('agent-home')).to_be_disabled()
    expect(codex.get_by_test_id('agent-home')).to_have_value(str(ui_server.homes / 'codex'))
    expect(codex.get_by_test_id('home-env')).to_have_text('из CODEX_HOME')
    expect(codex.get_by_test_id('agent-program')).to_have_attribute('placeholder', re.compile(r'^найдена: \S*stubs/codex$'))
    expect(codex.get_by_role('switch')).to_have_attribute('aria-checked', 'true')
    # пример команды — на последней сессии, с кавычками вокруг папки, как у сервера
    manual = next(s for s in sessions if s.tool == 'codex' and not s.auto and s.cwd)
    auto = next(s for s in sessions if s.tool == 'codex' and s.auto)
    expect(codex.get_by_test_id('preview-label')).to_have_text(
        ['Пример на сессии sample app:', 'Пример на сессии из codex exec в sample app:'])
    expect(codex.get_by_test_id('preview')).to_have_text(['$ ' + manual.command, '$ ' + auto.command])
    expect(codex.get_by_test_id('agent-skip')).not_to_be_checked()
    expect(codex.locator('.skip')).to_contain_text(f'Запускать без подтвержденийДобавит к команде флаг {SKIP}')
    # у остальных агентов одна строка примера и свой флаг; полей для шаблона команды нет
    claude = card(page, 'claude')
    expect(claude.get_by_test_id('preview')).to_have_count(1)
    expect(claude.locator('.skip')).to_contain_text('Добавит к команде флаг --dangerously-skip-permissions')
    expect(card(page, 'kimi').locator('.skip')).to_contain_text('Добавит к команде флаг --auto')
    expect(page.locator('[data-testid="command-input"], [data-testid="insert-id"]')).to_have_count(0)


def test_skip_approvals_and_program_in_command(page, ui_server):
    open_settings(page, ui_server)
    codex = card(page, 'codex')
    before = commands(page, ui_server, 'codex')
    expect(codex.get_by_test_id('preview')).to_have_text(before)
    codex.get_by_test_id('agent-skip').check()
    expect(codex.get_by_test_id('agent-dirty')).to_be_visible()
    expect(codex.locator('[data-dirty="agents.codex.skip_approvals"]')).to_be_visible()
    expect(codex.get_by_test_id('preview')).to_have_text([c + ' ' + SKIP for c in before])
    expect(page.get_by_test_id('save-count')).to_have_text(' · 1 изменение')
    save(page)
    assert stored() == {'agents': {'codex': {'skip_approvals': True}}}
    after = commands(page, ui_server, 'codex')
    assert all(c.endswith(' ' + SKIP) for c in after)
    expect(codex.get_by_test_id('preview')).to_have_text(after)
    expect(codex.get_by_test_id('agent-dirty')).to_be_hidden()
    # путь к программе из поля заменяет имя программы в команде, в кавычках, если нужно
    codex.get_by_test_id('agent-program').fill('~/bin/codex dev')
    expect(codex.get_by_test_id('preview').first).to_contain_text("/bin/codex dev' resume ")
    save(page)
    assert stored() == {'agents': {'codex': {'skip_approvals': True, 'program': '~/bin/codex dev'}}}
    expect(codex.get_by_test_id('preview')).to_have_text(commands(page, ui_server, 'codex'))
    page.reload()
    codex = card(page, 'codex')
    expect(codex.get_by_test_id('agent-skip')).to_be_checked()
    expect(codex.get_by_test_id('program-warn')).to_have_text('По этому пути программы нет или её нельзя запустить.')
    codex.get_by_test_id('agent-skip').uncheck()
    codex.get_by_test_id('agent-program').fill('')
    save(page)
    assert stored() == {}
    expect(codex.get_by_test_id('preview')).to_have_text(before)


def test_revert_returns_saved(page, ui_server):
    calls = []
    page.on('request', lambda request: calls.append(request.url) if request.method == 'POST' else None)
    open_settings(page, ui_server)
    codex = card(page, 'codex')
    before = commands(page, ui_server, 'codex')
    codex.get_by_test_id('agent-skip').check()
    codex.get_by_test_id('agent-program').fill('/opt/codex/bin/codex')
    card(page, 'grok').get_by_role('switch').click()
    expect(page.get_by_test_id('save-count')).to_have_text(' · 3 изменения')
    expect(codex.get_by_test_id('preview').first).to_contain_text(f'&& /opt/codex/bin/codex resume ')
    expect(codex.get_by_test_id('preview').first).to_contain_text(SKIP)
    page.get_by_test_id('revert').click()
    expect(codex.get_by_test_id('agent-skip')).not_to_be_checked()
    expect(codex.get_by_test_id('agent-program')).to_have_value('')
    expect(card(page, 'grok').get_by_role('switch')).to_have_attribute('aria-checked', 'true')
    expect(codex.get_by_test_id('preview')).to_have_text(before)
    expect(codex.get_by_test_id('agent-dirty')).to_be_hidden()
    expect(page.get_by_test_id('savebar')).to_have_count(0)
    assert not calls
    assert not settings_file().exists()


def test_disable_agent_and_unsaved_warning(page, ui_server):
    open_settings(page, ui_server)
    leave = 'new Promise(r => { const e = new Event("beforeunload", {cancelable: true}); dispatchEvent(e); r(e.defaultPrevented); })'
    assert page.evaluate(leave) is False
    switch = card(page, 'grok').get_by_role('switch', name='Показывать Grok в списке')
    # щелчок по подписи тоже переключает
    card(page, 'grok').get_by_text('Показывать в списке').click()
    expect(switch).to_have_attribute('aria-checked', 'false')
    assert page.evaluate(leave) is True
    save(page)
    assert stored() == {'agents': {'grok': {'enabled': False}}}
    assert page.evaluate(leave) is False
    grok = next(a for a in page.request.get(ui_server.url + '/api/config').json()['agents'] if a['id'] == 'grok')
    assert grok['enabled'] is False
    # с клавиатуры: переключатель остаётся в фокусе после перерисовки
    switch.focus()
    page.keyboard.press('Space')
    expect(switch).to_have_attribute('aria-checked', 'true')
    expect(switch).to_be_focused()
    save(page)
    assert stored() == {}


def test_summaries_timeout_agent_and_language(page, ui_server):
    open_settings(page, ui_server)
    page.get_by_test_id('section-summaries').click()
    expect(page).to_have_url(re.compile(r'#section=summaries$'))
    expect(page.get_by_test_id('section-title')).to_have_text('Сводки и поиск')
    expect(page.get_by_test_id('section')).to_contain_text('Кто пишет сводки и ищет сессии, сколько ждать ответа.')
    session, digest, search = (page.get_by_test_id(f'sum-{kind}') for kind in ('summary', 'digest', 'search'))
    expect(session.get_by_test_id('pick-auto')).to_have_attribute('aria-pressed', 'true')
    expect(session.get_by_test_id('pick-auto')).to_have_attribute('title', 'Kimi, иначе первый установленный')
    expect(session.get_by_test_id('timeout')).to_have_value('5')
    expect(digest.get_by_test_id('timeout')).to_have_value('15')
    expect(session).to_contain_text('обычно ответ приходит за 1–3 минуты')
    # умный поиск: те же агенты, свои подписи, по умолчанию 5 минут
    expect(search).to_contain_text('Умный поиск')
    expect(search).to_contain_text('Агент разбирает запрос и отбирает подходящие сессии. Искать слова в журналах «Нить» умеет сама.')
    expect(search).to_contain_text('агент отвечает дважды: обычно 1–3 минуты')
    expect(search.get_by_role('group', name='Кто ищет: Умный поиск')).to_be_visible()
    expect(search.get_by_test_id('pick-auto')).to_have_attribute('aria-pressed', 'true')
    expect(search.get_by_test_id('timeout')).to_have_value('5')
    search.get_by_test_id('timeout').fill('61')
    expect(search.get_by_test_id('field-error')).to_have_text('Укажите от 1 до 60 минут')
    expect(page.get_by_test_id('save')).to_be_disabled()
    expect(page.get_by_test_id('save-problems')).to_have_text('Исправьте время ожидания, чтобы сохранить')
    search.get_by_test_id('timeout').fill('2')
    search.get_by_test_id('pick-codex').click()
    expect(search.get_by_test_id('pick-codex')).to_have_attribute('aria-pressed', 'true')
    expect(session.get_by_test_id('pick-auto')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('no-runner')).to_have_count(0)
    session.get_by_test_id('timeout').fill('0')
    expect(session.get_by_test_id('field-error')).to_have_text('Укажите от 1 до 60 минут')
    expect(page.get_by_test_id('save')).to_be_disabled()
    session.get_by_test_id('timeout').fill('7')
    expect(page.get_by_test_id('save')).to_be_enabled()
    digest.get_by_test_id('pick-claude').click()
    expect(digest.get_by_test_id('pick-claude')).to_have_attribute('aria-pressed', 'true')
    expect(digest.get_by_test_id('pick-auto')).to_have_attribute('aria-pressed', 'false')
    # язык по умолчанию — как в браузере; явный русский пишется в файл, страница остаётся русской
    expect(page.get_by_test_id('lang-auto')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('lang-auto')).to_have_attribute('title', 'как в браузере')
    page.get_by_test_id('lang-ru').click()
    expect(page.get_by_test_id('lang-ru')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('lang-auto')).to_have_attribute('aria-pressed', 'false')
    expect(page.get_by_test_id('save-count')).to_have_text(' · 5 изменений')
    save(page)
    assert stored() == {'summary': {'timeout': 420}, 'digest': {'agent': 'claude'},
                        'search': {'agent': 'codex', 'timeout': 120},
                        'language': 'ru'}
    page.reload()
    expect(page.get_by_test_id('sum-summary').get_by_test_id('timeout')).to_have_value('7')
    expect(page.get_by_test_id('sum-search').get_by_test_id('timeout')).to_have_value('2')
    # возврат к умолчаниям убирает ключ из файла
    page.get_by_test_id('sum-search').get_by_test_id('timeout').fill('5')
    page.get_by_test_id('sum-search').get_by_test_id('pick-auto').click()
    save(page)
    assert 'search' not in stored()
    expect(page.get_by_test_id('section-summaries')).to_contain_text('Kimi')


def test_hidden_dirs_add_remove(page, ui_server):
    open_settings(page, ui_server, 'list')
    pills = page.get_by_test_id('hidden-dir')
    expect(pills).to_have_count(3)
    expect(page.get_by_test_id('hidden-count')).to_have_text('Сейчас из-за этих папок скрыто 0 сессий.')
    add = page.get_by_test_id('add-dir-input')
    add.fill('projects')
    expect(page.get_by_test_id('field-error')).to_have_text('Путь должен начинаться с / или ~/')
    expect(page.get_by_test_id('add-dir')).to_be_disabled()
    add.fill('/tmp')
    expect(page.get_by_test_id('add-dir')).to_be_disabled()
    add.fill('/home/user/projects')
    expect(page.get_by_test_id('add-dir')).to_be_enabled()
    add.press('Enter')
    expect(pills).to_have_count(4)
    expect(add).to_have_value('')
    hidden = sum(1 for s in ui_server.catalog.sessions() if not s.auto and s.cwd.startswith('/home/user/projects/'))
    assert hidden
    expect(page.get_by_test_id('hidden-count')).to_contain_text(f' {hidden} ')
    page.get_by_role('button', name='Убрать /var/tmp').click()
    expect(pills).to_have_count(3)
    expect(page.get_by_test_id('section-list')).to_contain_text('3 папки')
    save(page)
    assert stored() == {'temp_dirs': ['/tmp', '~/.local', '/home/user/projects']}
    feed = page.request.get(ui_server.url + '/api/sessions').json()['sessions']
    assert sum(1 for s in feed if s['temp'] and not s['auto']) == hidden
    for _ in range(3):
        page.get_by_test_id('hidden-dir-remove').first.click()
    expect(page.get_by_test_id('hidden-empty')).to_be_visible()
    expect(page.get_by_test_id('add-dir-input')).to_be_focused()
    save(page)
    assert stored() == {'temp_dirs': []}


def test_zcode_has_no_command(page, zcode_home, ui_server):
    open_settings(page, ui_server)
    zcode = card(page, 'zcode')
    expect(zcode.get_by_test_id('agent-status')).to_have_text(f'Найдено: 0 сессий в {zcode_home}')
    expect(zcode.locator('.dot.ok')).to_have_count(1)
    expect(zcode.get_by_test_id('resume-hint')).to_have_text('Продолжить можно только в приложении ZCode')
    expect(zcode.get_by_test_id('agent-skip')).to_have_count(0)
    expect(zcode.get_by_test_id('preview')).to_have_count(0)
    expect(zcode.get_by_test_id('agent-program')).to_have_count(0)
    expect(zcode.get_by_role('switch')).to_be_visible()


def test_about_section_and_broken_settings(page, ui_server):
    settings_file().write_text('{"language": ', encoding='utf-8')
    open_settings(page, ui_server, 'about')
    expect(page.get_by_test_id('settings-error')).to_contain_text('Файл настроек не прочитан, работают умолчания:')
    port = ui_server.server.server_port
    expect(page.get_by_test_id('about-addr')).to_have_text(f'127.0.0.1:{port}')
    expect(page.get_by_test_id('about-settings')).to_have_text(str(settings_file()))
    expect(page.get_by_test_id('about-data')).to_have_text(str(ui_server.tmp / 'data'))
    expect(page.get_by_test_id('about-version')).to_have_text(re.compile(r'^\d+\.\d+\.\d+$'))
    expect(page.get_by_test_id('about-github')).to_have_attribute('href', 'https://github.com/genhoi/ai-threads')
    expect(page.get_by_test_id('about')).to_contain_text('Правки вручную подхватываются без перезапуска.')


def test_recheck_and_data_menu(page, ui_server):
    open_settings(page, ui_server)
    expect(page.get_by_test_id('check-note')).to_have_text('проверено только что')
    config = page.request.get(ui_server.url + '/api/config').json()
    found = sum(1 for a in config['agents'] if a['home_exists'] or a['program'])
    with page.expect_request('**/api/config?refresh=1'):
        page.get_by_test_id('check-again').click()
    expect(page.get_by_test_id('toast')).to_have_text(f'Проверка закончена: найдено {found} из {len(config["agents"])}')
    expect(page.get_by_test_id('check-again')).to_have_text('Проверить снова')
    expect(page.get_by_test_id('check-again')).to_be_enabled()
    # «Данные»: выгрузка и загрузка JSON; скрытая сессия меняет число проектов в шапке
    expect(page.get_by_test_id('nav-projects')).to_have_text('Проекты2')
    page.get_by_test_id('data-menu').click()
    expect(page.get_by_test_id('data-menu')).to_have_attribute('aria-expanded', 'true')
    expect(page.get_by_test_id('data-export')).to_have_attribute('href', '/api/export')
    page.get_by_test_id('data-import-file').set_input_files({
        'name': 'import.json', 'mimeType': 'application/json',
        'buffer': json.dumps({'version': 2, 'hidden': ['claude:00000000-0000-4000-8000-000000000003']}).encode()})
    expect(page.get_by_test_id('toast')).to_have_text('Данные загружены')
    expect(page.get_by_test_id('nav-projects')).to_have_text('Проекты1')


def test_empty_state(page, bare_server):
    open_settings(page, bare_server)
    panel = page.get_by_test_id('agents-empty')
    expect(panel).to_contain_text('Агенты не найдены')
    expect(panel).to_contain_text('программа codex — нет в PATH')
    expect(panel).to_contain_text('absent-codex — нет папки')
    expect(page.get_by_test_id('searched-path')).to_have_text(f'PATH: {bare_server.path}')
    agents = page.request.get(bare_server.url + '/api/config').json()['agents']
    # у ZCode нет программы — в его строке только папка
    zcode = [a['id'] for a in agents].index('zcode')
    expect(panel.locator('.searched').nth(2 * zcode)).to_have_text('')
    expect(panel.locator('.searched').nth(2 * zcode + 1)).to_contain_text('absent-zcode — нет папки')
    expect(page.get_by_test_id('missing-heading')).to_have_count(0)
    expect(page.get_by_test_id('agent-status')).to_have_text(['Не установлен'] * len(agents))
    expect(page.get_by_test_id('section-agents')).to_contain_text(f'0 из {len(agents)}')
    # первая карточка открыта: видно, где указать пути
    expect(card(page, 'codex').get_by_test_id('agent-open')).to_have_text('Свернуть')
    expect(card(page, 'codex').get_by_test_id('agent-program')).to_have_attribute(
        'placeholder', 'не найдена — укажите путь, например ~/.local/bin/codex')
    expect(card(page, 'claude').get_by_test_id('agent-open')).to_have_text('Указать вручную')
    page.get_by_test_id('section-summaries').click()
    expect(page.get_by_test_id('no-runner').first).to_have_text('Нет ни одного установленного агента — сводки недоступны.')
    expect(page.get_by_test_id('sum-summary').get_by_test_id('pick-kimi')).to_be_disabled()
    expect(page.get_by_test_id('sum-summary').get_by_test_id('pick-auto')).to_be_enabled()
    expect(page.get_by_test_id('sum-search').get_by_test_id('pick-codex')).to_be_disabled()
    expect(page.get_by_test_id('no-runner')).to_have_count(3)
    expect(page.get_by_test_id('section-summaries')).to_contain_text('нет агента')


def test_narrow_window(page, ui_server):
    page.set_viewport_size({'width': 760, 'height': 900})
    page.goto(ui_server.url + '/settings.html')
    rows = page.locator('.mrow')
    expect(rows).to_have_count(4)
    expect(page.locator('.side')).to_be_hidden()
    expect(page.get_by_test_id('server-addr')).to_be_hidden()
    expect(page.get_by_test_id('section-list')).to_contain_text('Какие сессии попадают в «Мои».')
    page.get_by_test_id('section-list').click()
    expect(page).to_have_url(re.compile(r'#section=list$'))
    expect(page.get_by_test_id('section-title')).to_have_text('Список сессий')
    expect(page.get_by_test_id('section-title')).to_be_focused()
    page.get_by_test_id('add-dir-input').fill('~/scratch')
    page.get_by_test_id('add-dir').click()
    bar = page.get_by_test_id('savebar')
    expect(bar).to_be_visible()
    box = bar.bounding_box()
    assert abs(box['y'] + box['height'] - 900) < 1
    assert page.evaluate('document.documentElement.scrollWidth') <= 760
    page.get_by_test_id('back-to-menu').click()
    expect(rows).to_have_count(4)
    expect(page.get_by_test_id('section-list')).to_be_focused()
    expect(bar).to_be_visible()
    page.go_back()
    expect(page.get_by_test_id('section-title')).to_have_text('Список сессий')
    # ширина больше 900 px: меню разделов слева, раздел справа
    page.set_viewport_size({'width': 1200, 'height': 900})
    expect(page.locator('.side')).to_be_visible()
    expect(page.get_by_test_id('back-to-menu')).to_have_count(0)
    expect(page.get_by_test_id('section-list')).to_have_attribute('aria-current', 'page')


def test_server_error_is_shown_in_savebar(page, ui_server):
    open_settings(page, ui_server)
    page.route('**/api/settings', lambda route: route.fulfill(
        status=400, content_type='application/json; charset=utf-8',
        body=json.dumps({'error': 'agents.codex.home: нужна строка до 1000 символов'}, ensure_ascii=False)))
    card(page, 'codex').get_by_test_id('agent-program').fill('~/bin/codex')
    page.get_by_test_id('save').click()
    expect(page.get_by_test_id('save-error')).to_have_text('Не удалось сохранить: agents.codex.home: нужна строка до 1000 символов')
    expect(page.get_by_test_id('savebar')).to_be_visible()
    expect(page.get_by_test_id('save')).to_be_enabled()
    expect(card(page, 'codex').get_by_test_id('agent-program')).to_have_value('~/bin/codex')
    assert not settings_file().exists()
