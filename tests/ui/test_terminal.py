"""Терминал справа: сессия открывается в терминале, с агентом можно переписываться, видно его выход.

Вместо claude запускается заглушка tests/stubs/agent-tty, а у тестов свой сервер tmux.
"""

from dataclasses import replace
from pathlib import Path
from urllib.parse import quote

import pytest
from playwright.sync_api import expect

from ai_threads import tmux

ROOT = Path(__file__).resolve().parents[2]
AGENT = ROOT / 'tests/stubs/agent-tty'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
SESSION_ID = CLAUDE.split(':', 1)[1]


@pytest.fixture
def term_server(ui_server, tmux_server, save_settings, tmp_path, monkeypatch):
    """У сессий фикстур нет папок на диске, а агенту нужна папка: сессия Claude «переезжает» во временную."""
    project = tmp_path / 'project'
    project.mkdir()
    catalog = ui_server.catalog
    get, sessions = catalog.get, catalog.sessions
    moved = lambda s: replace(s, cwd=str(project)) if s is not None and s.key == CLAUDE else s
    monkeypatch.setattr(catalog, 'get', lambda key: moved(get(key)))
    monkeypatch.setattr(catalog, 'sessions', lambda: [moved(s) for s in sessions()])
    save_settings({'agents': {'claude': {'program': str(AGENT)}}})
    return ui_server


def open_claude(page, server):
    page.goto(server.url + f'/#section=mine&session={quote(CLAUDE)}')
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', CLAUDE)


def screen(page):
    return page.locator('.term-host .xterm-rows')


def type_line(page, text):
    page.locator('.term-host').click()
    page.keyboard.type(text)
    page.keyboard.press('Enter')


def test_talk_to_agent_exit_resume_and_close(page, term_server):
    open_claude(page, term_server)
    expect(page.get_by_test_id('term-open')).to_have_attribute('title', 'Агент запустится справа, в этом окне')
    page.get_by_test_id('term-open').click()
    expect(page.get_by_test_id('term-title')).to_be_visible()
    expect(screen(page)).to_contain_text(f'fake agent: --resume {SESSION_ID}', timeout=10000)
    expect(page.get_by_test_id('term-starting')).to_have_count(0)
    expect(page.get_by_test_id('term-state')).to_have_text('открыта')
    expect(page.get_by_test_id('term-view-term')).to_have_attribute('aria-pressed', 'true')
    type_line(page, 'привет')
    expect(screen(page)).to_contain_text('got: привет')

    # Карточка сессии — вторая вкладка: терминал при этом не закрывается.
    page.get_by_test_id('term-view-card').click()
    expect(page.get_by_test_id('term-pane')).to_be_hidden()
    expect(page.get_by_test_id('summary-row')).to_be_visible()
    page.get_by_test_id('term-show').click()
    expect(page.get_by_test_id('term-pane')).to_be_visible()
    expect(screen(page)).to_contain_text('got: привет')

    # Агент вышел с кодом 4: полоса поверх терминала, состояние в шапке, кнопка в карточке.
    type_line(page, 'quit')
    expect(page.get_by_test_id('term-exited')).to_contain_text('Агент завершился с кодом 4', timeout=10000)
    expect(page.get_by_test_id('term-state')).to_have_text('завершился · код 4')
    assert tmux.terminals()[CLAUDE]['code'] == 4
    page.get_by_test_id('term-exited').get_by_test_id('term-resume').click()
    expect(page.get_by_test_id('term-exited')).to_have_count(0)
    expect(screen(page)).to_contain_text('fake agent', timeout=10000)
    assert tmux.terminals()[CLAUDE]['state'] == 'running'

    page.get_by_test_id('pin').click()
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-pressed', 'true')
    page.get_by_test_id('term-menu').click()
    page.get_by_test_id('term-copy-attach').click()
    assert page.evaluate('window.copiedText') == tmux.attach_command(CLAUDE)
    expect(page.get_by_test_id('toast')).to_contain_text('подключит другой терминал к этому же агенту')
    page.get_by_test_id('term-menu').click()
    expect(page.get_by_test_id('term-close')).to_contain_text('агент остановится')
    page.get_by_test_id('term-close').click()
    expect(page.get_by_test_id('term-bar')).to_have_count(0)
    expect(page.get_by_test_id('session-title')).to_be_visible()
    expect(page.get_by_test_id('term-open')).to_be_visible()
    assert tmux.terminals() == {}


def test_terminal_survives_reload_and_hotkey(page, term_server):
    open_claude(page, term_server)
    page.keyboard.press('o')
    expect(screen(page)).to_contain_text('fake agent', timeout=10000)
    type_line(page, 'до перезагрузки')
    expect(screen(page)).to_contain_text('got: до перезагрузки')
    # Фокус вне терминала, иначе браузер спросит, уходить ли со страницы.
    page.get_by_test_id('search').focus()
    page.reload()
    expect(page.get_by_test_id('term-bar')).to_be_visible()
    expect(screen(page)).to_contain_text('got: до перезагрузки', timeout=10000)
    # В другой сессии терминала нет, а вернувшись, видим тот же агент.
    page.locator('[data-testid="row"][data-key^="codex:"]').first.click()
    expect(page.get_by_test_id('term-bar')).to_have_count(0)
    page.goto(term_server.url + f'/#section=all&session={quote(CLAUDE)}')
    expect(screen(page)).to_contain_text('got: до перезагрузки', timeout=10000)
    assert list(tmux.terminals()) == [CLAUDE]


def test_terminal_closed_elsewhere_returns_to_card(page, term_server):
    open_claude(page, term_server)
    page.get_by_test_id('term-open').click()
    expect(screen(page)).to_contain_text('fake agent', timeout=10000)
    tmux.close_terminal(CLAUDE)
    expect(page.get_by_test_id('term-bar')).to_have_count(0, timeout=10000)
    expect(page.get_by_test_id('term-open')).to_be_visible()


def test_session_open_elsewhere_or_without_tmux_is_not_started(page, term_server):
    page.route('**/api/live', lambda route: route.fulfill(json={CLAUDE: {'state': 'work', 'where': 'терминал', 'since': 1}}))
    open_claude(page, term_server)
    button = page.get_by_test_id('term-open')
    expect(button).to_have_attribute('aria-disabled', 'true', timeout=7000)
    expect(button).to_have_attribute('title', 'Сессия уже открыта в другом терминале. Закройте её там, иначе на одну сессию запустятся два агента')
    # aria-disabled: Playwright считает кнопку выключенной, а человек нажать может и увидит причину.
    button.click(force=True)
    expect(page.get_by_test_id('toast')).to_contain_text('уже открыта в другом терминале')
    assert tmux.terminals() == {}

    def no_tmux(route):
        data = route.fetch().json()
        data['env']['tmux'] = False
        route.fulfill(json=data)
    page.unroute('**/api/live')
    page.route('**/api/live', lambda route: route.fulfill(json={}))
    page.route('**/api/config', no_tmux)
    page.reload()
    expect(page.get_by_test_id('term-open')).to_have_attribute('title', 'Для терминала нужен tmux. Установите его, например: sudo apt install tmux')
    page.keyboard.press('o')
    expect(page.get_by_test_id('toast')).to_contain_text('нужен tmux')
    assert tmux.terminals() == {}


def test_mouse_selection_is_copied_and_wheel_scrolls_history(page, term_server):
    open_claude(page, term_server)
    page.get_by_test_id('term-open').click()
    expect(screen(page)).to_contain_text('fake agent', timeout=10000)
    for n in range(60):
        type_line(page, f'строка {n}')
    expect(screen(page)).to_contain_text('got: строка 59')
    expect(screen(page)).not_to_contain_text('fake agent')
    # Колесо мыши листает историю tmux назад, к началу вывода.
    page.mouse.move(*center(page.locator('.term-host')))
    for _ in range(20):
        page.mouse.wheel(0, -400)
    expect(screen(page)).to_contain_text('fake agent', timeout=5000)
    page.keyboard.press('q')  # выйти из просмотра истории
    expect(screen(page)).to_contain_text('got: строка 59')
    # Выделение мышью делает tmux и отдаёт его в буфер обмена последовательностью OSC 52.
    row = screen(page).locator('div', has_text='got: строка 59').last
    box = row.bounding_box()
    page.mouse.move(box['x'] + 2, box['y'] + box['height'] / 2)
    page.mouse.down()
    page.mouse.move(box['x'] + 100, box['y'] + box['height'] / 2, steps=5)
    page.mouse.up()
    page.wait_for_function('window.copiedText && window.copiedText.startsWith("got:")', timeout=5000)
    # С Shift выделяет сам xterm, а Ctrl+C копирует выделенное и не уходит агенту.
    page.evaluate('window.copiedText = ""')
    row = screen(page).locator('div', has_text='got: строка 58').last
    box = row.bounding_box()
    page.keyboard.down('Shift')
    page.mouse.move(box['x'] + 2, box['y'] + box['height'] / 2)
    page.mouse.down()
    page.mouse.move(box['x'] + 200, box['y'] + box['height'] / 2, steps=5)
    page.mouse.up()
    page.keyboard.up('Shift')
    page.keyboard.press('Control+c')
    page.wait_for_function('window.copiedText && window.copiedText.startsWith("got: строка 58")', timeout=5000)
    type_line(page, 'после копирования')
    expect(screen(page)).to_contain_text('got: после копирования')


def center(locator):
    box = locator.bounding_box()
    return box['x'] + box['width'] / 2, box['y'] + box['height'] / 2


def test_terminal_reference_screens(page, term_server):
    """Снимки для сверки с досками design/Term*.dc.html: test-results/screens/Term*.png."""
    if page.context.browser.browser_type.name != 'chromium':
        return
    directory = ROOT / 'test-results/screens'
    directory.mkdir(parents=True, exist_ok=True)
    def shot(name):
        page.evaluate('document.fonts.ready')
        page.screenshot(path=str(directory / f'{name}.png'), animations='disabled')
    open_claude(page, term_server)
    shot('Term-Card')
    page.get_by_test_id('term-open').click()
    expect(screen(page)).to_contain_text('fake agent', timeout=10000)
    type_line(page, 'привет')
    expect(screen(page)).to_contain_text('got: привет')
    shot('Term')
    page.get_by_test_id('term-menu').click()
    shot('Term-menu')
    page.keyboard.press('Escape')
    page.get_by_test_id('term-view-card').click()
    shot('Term-session')
    page.get_by_test_id('term-view-term').click()
    type_line(page, 'quit')
    expect(page.get_by_test_id('term-exited')).to_be_visible(timeout=10000)
    shot('Term-Exited')
    page.set_viewport_size({'width': 760, 'height': 900})
    shot('Term-Narrow')
