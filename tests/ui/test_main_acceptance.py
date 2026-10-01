"""Приёмка главного экрана: расхождения с макетом, спецификацией и контрактом."""

import re
from datetime import datetime
from urllib.parse import quote
from zoneinfo import ZoneInfo

from playwright.sync_api import expect

KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
AUTO = 'codex:00000000-0000-4000-8000-000000000002'
SUMMARY = {
    'title': 'Каталог проверен',
    'summary': 'Найдена запись в каталоге. Проверено название.',
    'next_step': 'Проверить поиск по переписке',
    'closed': True,
    'related': [{'key': CLAUDE, 'why': 'та же тема'}],
    'at': 1790600000,
    'model': 'kimi',
}


def open_main(page, server, key=None):
    page.goto(server.url + (f'/#section=mine&session={quote(key)}' if key else '/'))
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', key or KEY)


def test_escape_closes_menu_before_leaving_pick_mode(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    expect(page.get_by_test_id('restore')).to_be_visible()
    page.get_by_test_id('data-menu').click()
    expect(page.get_by_test_id('data-export')).to_be_visible()
    page.keyboard.press('Escape')
    expect(page.get_by_test_id('data-export')).to_have_count(0)
    expect(page.get_by_test_id('restore')).to_be_visible()
    page.keyboard.press('Escape')
    expect(page.get_by_test_id('restore')).to_have_count(0)


def test_click_outside_menu_does_not_activate_the_row(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-copy-id')).to_be_visible()
    page.locator('[data-testid="row"]').nth(3).click()
    expect(page.get_by_test_id('menu-copy-id')).to_have_count(0)
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', KEY)


def test_card_has_no_open_button_and_copy_keeps_shortcut(page, ui_server):
    calls = []
    page.on('request', lambda req: calls.append(req.url) if req.method == 'POST' else None)
    open_main(page, ui_server)
    expect(page.get_by_test_id('open-terminal')).to_have_count(0)
    page.keyboard.press('o')
    page.get_by_test_id('copy-command').click()
    expect(page.get_by_test_id('copy-command')).to_contain_text('Скопировано')
    assert page.get_by_test_id('copy-command').locator('.kbd').count() == 1
    expect(page.get_by_test_id('copy-command')).not_to_contain_text('Скопировано', timeout=4000)
    expect(page.get_by_test_id('copy-command').locator('.kbd')).to_have_text('C')
    assert not calls


def test_slash_keeps_wide_session_and_narrow_list_survives_reload(page, ui_server):
    open_main(page, ui_server)
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page).to_have_url(re.compile(r'view=session'))
    page.keyboard.press('/')
    expect(page.get_by_test_id('search')).to_be_focused()
    expect(page).to_have_url(re.compile(r'view=session'))
    page.set_viewport_size({'width': 760, 'height': 900})
    expect(page.get_by_test_id('session')).to_be_visible()
    page.evaluate('document.activeElement.blur()')
    page.keyboard.press('/')
    expect(page.get_by_test_id('search')).to_be_focused()
    expect(page.get_by_test_id('session')).to_be_hidden()
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_be_hidden()


def test_arrow_keys_move_selection_without_history_entries(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('search').focus()
    before = page.evaluate('history.length')
    page.keyboard.press('ArrowDown')
    expect(page.get_by_test_id('session')).not_to_have_attribute('data-key', KEY)
    page.keyboard.press('ArrowDown')
    page.keyboard.press('ArrowUp')
    assert page.evaluate('history.length') == before


def test_wait_button_counts_only_manual_sessions(page, ui_server):
    status = {
        AUTO: {'state': 'wait', 'where': 'фон', 'since': 1},
        KEY: {'state': 'wait', 'where': 'tmux nit:1', 'since': 1},
    }
    page.route('**/api/live', lambda route: route.fulfill(json=status))
    open_main(page, ui_server)
    expect(page.get_by_test_id('wait-next')).to_have_text('1 ждёт вас')
    page.get_by_test_id('wait-next').click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', KEY)
    expect(page.get_by_test_id('live-banner')).to_contain_text('Открывать заново не нужно')


def test_hide_done_keeps_the_open_session(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-done').click()
    expect(page.get_by_test_id('menu-done')).to_have_count(0)
    expect(page.locator('.title-line .pill')).to_have_text('завершена')
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-done').click()
    expect(page.get_by_test_id('menu-done')).to_have_count(0)
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.get_by_test_id('toggle-hide-done').click()
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"]')).to_be_visible()
    expect(page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]')).to_have_count(0)
    expect(page.get_by_test_id('toggle-hide-done')).to_have_attribute('title', 'Показать завершённые')


def test_feed_end_search_hidden_count_and_project_empty_text(page, ui_server):
    open_main(page, ui_server)
    expect(page.get_by_test_id('feed-end')).to_have_text('Раньше сессий нет')
    page.get_by_test_id('search').fill('catalog')
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('feed-end')).to_have_count(0)
    page.get_by_test_id('search').fill('')
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-hide').click()
    expect(page.get_by_test_id('toast')).to_contain_text('Всех сессиях')
    expect(page.get_by_test_id('feed-end')).to_contain_text('скрыто 1')
    page.get_by_test_id('nav-projects').click()
    page.get_by_test_id('search').fill('нет-такого-проекта')
    expect(page.get_by_test_id('no-results')).to_contain_text('Ничего не найдено по «нет-такого-проекта»')


def test_conversation_search_hint_highlight_and_unclamp(page, ui_server):
    long = 'Начало переписки. ' + ('абзац ' * 80) + 'УНИКАЛЬНОЕ_СЛОВО ' + ('хвост ' * 20)
    page.route('**/api/session?*', lambda route: route.fulfill(json={'messages': [
        {'role': 'assistant', 'text': long, 'at': None},
        {'role': 'user', 'text': 'короткое', 'at': None},
    ], 'truncated': False}))
    open_main(page, ui_server)
    page.get_by_test_id('search').fill('нет-такого-текста')
    page.get_by_test_id('search-conv').click()
    expect(page.get_by_test_id('no-results')).to_contain_text('Автоматические запуски')
    note = page.locator('[data-testid="no-results"] .empty-note')
    expect(note).to_have_css('color', 'rgb(176, 173, 165)')
    page.get_by_test_id('search').fill('УНИКАЛЬНОЕ_СЛОВО')
    mark = page.locator('[data-testid="message"] mark')
    expect(mark).to_have_text('УНИКАЛЬНОЕ_СЛОВО')
    expect(page.locator('[data-testid="message"] .msg-text').first).not_to_have_class(re.compile(r'is-clamped'))


def test_search_skips_project_subtitle_and_marks_long_hit(page, ui_server):
    text = ('начало ' * 12) + 'уникальныйфрагмент' + (' конец' * 4)
    ui_server.store.set_summary(KEY, {**SUMMARY, 'summary': text, 'title': 'Заголовок сводки', 'next_step': ''})
    open_main(page, ui_server)
    page.get_by_test_id('search').fill('sample app')
    expect(page.locator('[data-testid="row"]').first.locator('.row-sub')).not_to_contain_text('проект:')
    page.get_by_test_id('search').fill('уникальныйфрагмент')
    row = page.locator(f'[data-testid="row"][data-key="{KEY}"]')
    expect(row.locator('.row-sub')).to_contain_text('…')
    expect(row.locator('mark')).to_have_text('уникальныйфрагмент')


def test_restore_heading_notes_and_skipped_singular(page, ui_server):
    status = {KEY: {'state': 'work', 'where': 'tmux nit:1', 'since': 1}}
    page.route('**/api/live', lambda route: route.fulfill(json=status))
    open_main(page, ui_server)
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"] .dot.work')).to_be_visible()
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    note = page.locator('[data-testid="restore"] .restore-heading .restore-note')
    expect(note).to_have_text('1 выбрано · 1 уже открыта — пропущу')
    expect(page.locator('.script-note')).to_contain_text('nit-resume')
    page.get_by_test_id('fmt-tmux').click()
    expect(page.locator('.script-note')).to_have_text('Выполняется внутри WSL. Сессии откроются окнами в tmux-сессии nit.')
    page.get_by_test_id('fmt-plain').click()
    expect(page.locator('.script-note')).to_have_text('Вставляйте по одной в отдельные вкладки WSL.')


def test_related_stays_while_summary_runs(page, ui_server):
    ui_server.store.set_summary(KEY, SUMMARY)
    page.route('**/api/jobs', lambda route: route.fulfill(json=[{
        'job': 'summary:' + KEY, 'kind': 'summary', 'key': KEY, 'started': 1}]))
    page.route('**/api/jobs/*/events?*', lambda route: None)
    open_main(page, ui_server)
    expect(page.get_by_test_id('summary-progress')).to_contain_text('Kimi запускается')
    expect(page.get_by_test_id('related-chip')).to_be_visible()


def test_copy_id_toast_and_menu_hint(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-copy-id').locator('.hint')).to_have_text('00000000…')
    fill = page.get_by_test_id('session-menu').locator('circle').first.get_attribute('fill')
    assert fill == 'currentColor'
    page.get_by_test_id('menu-copy-id').click()
    expect(page.get_by_test_id('toast')).to_contain_text('ID скопирован: ' + KEY.split(':', 1)[1])


def test_dates_pinned_mark_and_empty_branch(page, ui_server):
    kimi = next(session for session in ui_server.catalog.sessions() if session.tool == 'kimi' and not session.branch and not session.auto and not session.temp)
    page.clock.set_fixed_time(datetime(2026, 10, 5, 15, 0, tzinfo=ZoneInfo('Europe/Samara')))
    open_main(page, ui_server)
    expect(page.locator('.sec.day').first).to_have_text('Понедельник, 28 сен')
    expect(page.get_by_test_id('session-meta')).to_contain_text('понедельник, 28 сен, 14:00')
    expect(page.locator(f'[data-testid="pinned-section"] [data-key="{KEY}"] .row-tail')).to_have_text('28 сен')
    expect(page.locator('[data-testid="session-meta"] .meta-project')).to_have_css('color', 'rgb(232, 230, 225)')
    kimi_row = page.locator(f'[data-testid="row"][data-key="{kimi.key}"]')
    expect(kimi_row).to_be_visible()
    expect(kimi_row.locator('.row-branch')).to_have_count(0)


def test_summary_idle_color_open_meta_and_missing_icon(page, ui_server):
    open_main(page, ui_server, CLAUDE)
    expect(page.locator('[data-testid="summary-row"] .summary-idle')).to_have_css('color', 'rgb(176, 173, 165)')
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('missing-banner').locator('svg')).to_be_visible()
    ui_server.store.set_summary(KEY, SUMMARY)
    page.reload()
    page.get_by_test_id('summary-toggle').click()
    expect(page.locator('[data-testid="summary-row"] .summary-when')).to_have_css('font-size', '11.5px')


def test_pick_next_matches_toolbar_and_checkbox_is_icon(page, ui_server):
    page.set_viewport_size({'width': 760, 'height': 900})
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    expect(page.get_by_test_id('pick-next')).to_have_css('height', '30px')
    expect(page.locator('.chk svg').first).to_be_attached()
    expect(page.locator('.chk').first).not_to_contain_text('✓')


def test_loading_header_counts_partial_sessions(page, ui_server):
    source = ui_server.catalog.get(KEY).to_json()
    polls = []

    def sessions(route):
        polls.append(1)
        if len(polls) < 3:
            route.fulfill(json={'ready': False, 'sessions': [source], 'port': ui_server.server.server_port,
                                'progress': {'done': 1, 'total': 10, 'tools': {'codex': 'reading'}}})
        else:
            route.continue_()

    page.route('**/api/sessions', sessions)
    page.goto(ui_server.url)
    page.wait_for_function("""() => {
      const progress = document.querySelector('[data-testid="loading-progress"]');
      const count = document.querySelector('[data-testid="nav-all"] .n');
      return progress && progress.textContent.includes('1 из 10') && count && count.textContent.trim() === '1';
    }""")


def test_feed_keeps_scroll_when_row_is_chosen(page, ui_server):
    source = ui_server.catalog.get(KEY).to_json()
    sessions = [{**source, 'key': f'codex:scroll-{i}', 'id': f'scroll-{i}', 'title': f'Сессия {i}',
                 'updated': source['updated'] - i} for i in range(80)]
    page.route('**/api/sessions', lambda route: route.fulfill(json={
        'ready': True, 'sessions': sessions, 'port': ui_server.server.server_port, 'progress': {}}))
    page.route('**/api/session?*', lambda route: route.fulfill(json={'messages': [], 'truncated': False}))
    page.goto(ui_server.url)
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.locator('#feed').evaluate('el => el.scrollTop = 450')
    page.locator('[data-testid="row"]').nth(8).click()
    assert page.locator('#feed').evaluate('el => el.scrollTop') > 200
