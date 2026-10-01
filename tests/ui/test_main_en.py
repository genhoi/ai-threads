"""Главный экран на английском: язык браузера en-US или настройка language.

Тексты, которые присылает сервер (ход умного поиска, подсказки, скрипт восстановления), здесь
не проверяются. Кириллица в данных фикстур заменена латиницей: тогда кириллица на странице может
быть только в текстах самой страницы.
"""

import json
import re
import time
from datetime import datetime
from urllib.parse import quote
from zoneinfo import ZoneInfo

from playwright.sync_api import expect

KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
GROK = 'grok:00000000-0000-4000-8000-000000000001'
AUTO = 'codex:00000000-0000-4000-8000-000000000002'
# Сессия без названия и без папки: в журнале нет ни заголовка, ни cwd.
BARE = 'claude:00000000-0000-4000-8000-000000000003'
QUERY = 'where did I check the catalog'
CYRILLIC = re.compile('[А-Яа-яЁё]')
MESSAGES = [{'role': 'user', 'text': 'Check the catalog entry', 'at': None},
            {'role': 'assistant', 'text': 'The entry is there.', 'at': None}]
# Тексты сервера и модели: их язык задаёт сервер, а не страница.
SERVER_TEXT = ['[data-testid="script"]', '[data-testid="resume-hint"]',
               '[data-testid="smart-progress"] .smart-text', '[data-testid="smart-warning"]',
               '[data-testid="smart-why"]', '[data-testid="full-snippet"]',
               # Журнал агента: запрос модели, её ответы, вывод инструментов, строки «Нити» и команда.
               '[data-testid="agent-log"] .msg-text', '.log-pre', '.log-args', '[data-testid="log-nit"]',
               '[data-testid="log-error"]', '[data-testid="log-warning"]', '[data-testid="log-usage"]',
               '[data-testid="log-model"]', '[data-testid="agent-model"]', '[data-testid="reply-command"]']
FIND_CYRILLIC = """skip => {
  const cyrillic = /[А-Яа-яЁё]/, skipped = el => skip.some(selector => el.closest(selector));
  const found = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) {
    const node = walker.currentNode, el = node.parentElement;
    if (el && el.getClientRects().length && !skipped(el) && cyrillic.test(node.textContent)) found.push(node.textContent.trim());
  }
  for (const el of document.querySelectorAll('[title], [aria-label], [placeholder]')) {
    if (skipped(el)) continue;
    for (const name of ['title', 'aria-label', 'placeholder']) {
      const value = el.getAttribute(name);
      if (value && cyrillic.test(value)) found.push(`${name}: ${value}`);
    }
  }
  if (cyrillic.test(document.title)) found.push(`<title>: ${document.title}`);
  return found;
}"""


def english_data(page, updated=None):
    """Подменить данные фикстур латиницей; updated — новое время изменения отдельных сессий."""
    def sessions(route):
        data = route.fetch().json()
        for index, session in enumerate(data['sessions']):
            for field, value in session.items():
                if isinstance(value, str) and CYRILLIC.search(value):
                    session[field] = f'Session {index}' if field == 'title' else 'Plain text'
            if session['key'] in (KEY, CLAUDE, GROK):
                session['title'] = 'Check the catalog'
            session['updated'] = (updated or {}).get(session['key'], session['updated'])
        route.fulfill(json=data)
    page.route('**/api/sessions', sessions)
    page.route('**/api/session?*', lambda route: route.fulfill(json={'messages': MESSAGES, 'truncated': False}))


def open_main(page, server, key=KEY, section='mine'):
    page.goto(server.url + f'/#section={section}&session={quote(key)}')
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', key)


def assert_no_cyrillic(page, skip=()):
    assert page.evaluate(FIND_CYRILLIC, SERVER_TEXT + list(skip)) == []


def test_header_feed_and_card(page_en, ui_server):
    page = page_en
    english_data(page)
    open_main(page, ui_server, BARE)
    expect(page.locator('html')).to_have_attribute('lang', 'en')
    expect(page).to_have_title('Nit')
    expect(page.locator('.brand')).to_have_text('nit')
    expect(page.get_by_test_id('nav-mine')).to_have_text('Mine')
    expect(page.get_by_test_id('nav-all')).to_have_text(re.compile(r'^All \d+$'))
    expect(page.get_by_test_id('nav-projects')).to_have_text(re.compile(r'^Projects \d+$'))
    expect(page.get_by_test_id('nav-digest')).to_have_text('Digest')
    expect(page.get_by_test_id('data-menu')).to_have_text('Data')
    expect(page.get_by_test_id('settings-link')).to_have_attribute('aria-label', 'Settings')
    expect(page.get_by_test_id('search')).to_have_attribute('placeholder', 'Title, branch, path, ID, summary')
    expect(page.get_by_test_id('search-conv')).to_have_text('+ conversation')
    expect(page.get_by_test_id('feed-actions')).to_have_count(0)
    expect(page.get_by_test_id('pick-yesterday')).to_have_text(re.compile(r'^Yesterday · \d+$'))
    expect(page.get_by_test_id('pick-enter')).to_have_text('Select…')
    expect(page.get_by_test_id('toggle-hide-done')).to_have_attribute('title', 'Hide done sessions')
    expect(page.get_by_test_id('pinned-section').locator('.sec')).to_have_text('Pinned')
    expect(page.get_by_test_id('feed-end')).to_have_text('No earlier sessions')

    row = page.locator(f'[data-testid="row"][data-key="{BARE}"]')
    expect(row.locator('.row-title')).to_have_text('Untitled')
    expect(row.locator('.row-project')).to_have_text('No project')
    expect(page.get_by_test_id('session-title')).to_have_text('Untitled')
    meta = page.get_by_test_id('session-meta')
    expect(meta.locator('.meta-project')).to_have_text('No project')
    expect(meta).to_contain_text('started by me')
    expect(page.get_by_test_id('rename')).to_have_attribute('aria-label', 'Rename')
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-label', 'Pin to top')
    expect(page.get_by_test_id('copy-command')).to_contain_text('Copy')
    expect(page.get_by_test_id('missing-banner')).to_contain_text('The project folder is gone')
    expect(page.get_by_test_id('copy-no-cd')).to_have_text('Copy without cd')
    expect(page.get_by_test_id('summary-row')).to_contain_text('About')
    expect(page.get_by_test_id('summary-row')).to_contain_text('The log has no messages')
    expect(page.locator('.conversation-heading strong')).to_have_text('Conversation')
    assert_no_cyrillic(page)

    page.get_by_test_id('copy-command').click()
    expect(page.get_by_test_id('copy-command')).to_contain_text('Copied')
    page.get_by_test_id('rename').click()
    expect(page.get_by_test_id('rename-input')).to_have_value('')
    expect(page.get_by_test_id('rename-input')).to_have_attribute('placeholder', 'Untitled')
    expect(page.get_by_test_id('rename-save')).to_contain_text('Save')
    expect(page.get_by_test_id('rename-cancel')).to_have_text('Cancel')
    page.get_by_test_id('rename-cancel').click()

    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('message')).to_have_count(2)
    expect(page.locator('[data-testid="message"] .me')).to_have_text('Me')
    expect(page.get_by_test_id('summary-run')).to_have_text('Summarize')
    expect(page.locator('.summary-idle')).to_have_text('No summary yet.')
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-label', 'Unpin')
    assert_no_cyrillic(page)


def test_automated_session_shows_who_started_it(page_en, ui_server):
    english_data(page_en)
    open_main(page_en, ui_server, AUTO, section='all')
    expect(page_en.get_by_test_id('nav-all')).to_have_class('on')
    expect(page_en.get_by_test_id('session-meta')).to_contain_text('started by codex exec')


def test_menus_toasts_and_live_banner(page_en, ui_server):
    page = page_en
    english_data(page)
    status = {KEY: {'state': 'wait', 'where': 'tmux nit:1', 'since': 1}}
    page.route('**/api/live', lambda route: route.fulfill(json=status))
    open_main(page, ui_server)
    expect(page.get_by_test_id('wait-next')).to_have_text('1 waiting for you')
    expect(page.get_by_test_id('live-banner')).to_have_text('Agent is waiting for your reply · tmux nit:1. No need to open it again.')
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"] .dot.wait')).to_have_attribute('title', 'Agent is waiting for your reply')

    page.get_by_test_id('data-menu').click()
    expect(page.get_by_test_id('data-export')).to_have_text('Export JSON')
    expect(page.get_by_test_id('data-import')).to_have_text('Import JSON…')
    page.keyboard.press('Escape')

    page.get_by_test_id('session-menu').click()
    expect(page.locator('#session-popup .menu')).to_have_attribute('aria-label', 'Session actions')
    expect(page.get_by_test_id('menu-copy-id')).to_contain_text('Copy ID')
    expect(page.get_by_test_id('menu-copy-path')).to_have_text('Copy path')
    expect(page.get_by_test_id('menu-explorer')).to_have_text('Open folder in Explorer')
    expect(page.get_by_test_id('menu-vscode')).to_have_text('Open in VS Code')
    expect(page.get_by_test_id('menu-done')).to_have_text('Mark as done')
    expect(page.get_by_test_id('menu-hide')).to_have_text('Hide from list')
    assert_no_cyrillic(page)
    page.get_by_test_id('menu-copy-id').click()
    expect(page.get_by_test_id('toast')).to_have_text('ID copied: ' + KEY.split(':', 1)[1])

    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-done').click()
    expect(page.get_by_test_id('toast')).to_contain_text('Session marked as done')
    expect(page.get_by_test_id('toast-undo')).to_have_text('Undo')
    expect(page.locator('.title-line .pill')).to_have_text('done')
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-done')).to_have_text('Mark as not done')
    page.keyboard.press('Escape')
    page.get_by_test_id('toast-undo').click()
    expect(page.get_by_test_id('toast')).to_have_text('Undone')

    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-hide').click()
    expect(page.get_by_test_id('toast')).to_contain_text('Session hidden from the list. You can still find it under All')
    expect(page.get_by_test_id('feed-end')).to_have_text('No earlier sessions · 1 hidden')
    assert_no_cyrillic(page)


def test_search_without_results_and_smart_button(page_en, ui_server):
    page = page_en
    english_data(page)
    open_main(page, ui_server)
    smart = page.get_by_test_id('smart-search')
    expect(smart).to_have_count(0)
    page.get_by_test_id('search').fill('no-such-session-anywhere')
    # Строки-действия над лентой: полный поиск (Enter, раз обычный ничего не нашёл) и вопрос агенту.
    expect(page.get_by_test_id('search-full')).to_have_text('Search full logs for “no-such-session-anywhere”↵')
    expect(smart).to_have_text('Ask the agent: “no-such-session-anywhere”Ctrl+↵')
    expect(page.get_by_test_id('choice-search')).to_have_text('Auto')
    expect(page.get_by_test_id('choice-search')).to_have_attribute('title', 'Agent, model and reasoning effort')
    empty = page.get_by_test_id('no-results')
    expect(empty).to_contain_text('Nothing found for “no-such-session-anywhere”')
    expect(empty.locator('.empty-note')).to_have_text(
        'Searched titles, branches, paths, IDs and summaries. You can also search conversation text.')
    expect(page.get_by_test_id('search-more')).to_have_text('Search conversations too')
    expect(page.get_by_test_id('search-all')).to_have_text('Search all')
    expect(page.get_by_test_id('search-clear')).to_contain_text('Clear')
    expect(smart).to_be_enabled()
    expect(smart).to_have_attribute('title', 'The agent reads your query and finds matching sessions · Ctrl+Enter')
    assert_no_cyrillic(page)
    page.get_by_test_id('search-conv').click()
    expect(page.get_by_test_id('search-conv')).to_have_attribute('aria-pressed', 'true')
    expect(empty.locator('.empty-note')).to_contain_text('Searched conversations too')
    page.get_by_test_id('search').fill('x' * 501)
    expect(smart).to_have_attribute('title', 'Query is longer than 500 characters')

    page.get_by_test_id('search').fill('catalog')
    expect(page.locator('[data-testid="row"] .row-sub b').first).to_have_text(
        re.compile(r'^(original|branch|path|ID|summary|conversation):$'))

    page.get_by_test_id('search').fill('')
    page.get_by_test_id('nav-projects').click()
    page.get_by_test_id('search').fill('no-such-project')
    expect(page.get_by_test_id('no-results')).to_have_text('Nothing found for “no-such-project”')


def test_smart_button_without_agents(page_en, ui_server):
    def config(route):
        data = route.fetch().json()
        data['runners'] = []
        route.fulfill(json=data)
    page_en.route('**/api/config', config)
    english_data(page_en)
    open_main(page_en, ui_server, CLAUDE)
    expect(page_en.get_by_test_id('summary-no-runner')).to_have_text(re.compile(r'^No agent for summaries\. Install .*, or .+$'))
    page_en.get_by_test_id('search').fill(QUERY)
    expect(page_en.get_by_test_id('smart-search')).to_be_disabled()
    expect(page_en.get_by_test_id('smart-search')).to_have_attribute('title', re.compile(r'^No agent for summaries\. Install '))


def test_smart_search_panels(page_en, ui_server, monkeypatch):
    page = page_en
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    monkeypatch.setenv('AI_THREADS_STUB_TERMS', 'catalog')
    monkeypatch.setenv('AI_THREADS_STUB_AGENTS', 'codex')
    english_data(page)
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    page.get_by_test_id('smart-search').click()
    progress = page.get_by_test_id('smart-progress')
    expect(progress).to_contain_text(f'Smart search: “{QUERY}”')
    expect(progress.get_by_test_id('smart-cancel')).to_have_text('Stop')
    expect(page.get_by_test_id('smart-plan')).to_have_text('Searching for: catalog. Agents: Codex.', timeout=10000)
    assert_no_cyrillic(page)
    head = page.get_by_test_id('smart-head')
    expect(head).to_contain_text(f'Smart search: “{QUERY}”', timeout=20000)
    expect(head.locator('small')).to_have_text(re.compile(r'^(1 session|[2-9] sessions) · picked by Kimi$'))
    expect(head.get_by_test_id('smart-close')).to_have_attribute('aria-label', 'Back to the feed')
    assert_no_cyrillic(page)
    head.get_by_test_id('smart-close').click()
    expect(head).to_have_count(0)

    # Ошибку присылает сервер; страница добавляет только своё начало и кнопки.
    failed = []

    def events(route):
        if failed:
            route.continue_()
            return
        failed.append(route.request.url)
        route.fulfill(content_type='application/x-ndjson',
                      body=json.dumps({'n': 1, 'type': 'error', 'message': 'agent exited with code 2'}) + '\n')
    page.route(re.compile(r'/api/jobs/search[^/]*/events'), events)
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'ok')
    monkeypatch.setenv('AI_THREADS_STUB_TERMS', 'message_profile')
    page.get_by_test_id('smart-search').click()
    error = page.get_by_test_id('smart-error')
    expect(error).to_contain_text('Search failed: agent exited with code 2')
    expect(error.get_by_test_id('smart-retry')).to_have_text('Retry')
    expect(error.get_by_test_id('smart-close')).to_have_attribute('aria-label', 'Close')
    assert_no_cyrillic(page)
    page.get_by_test_id('smart-retry').click()
    expect(page.get_by_test_id('smart-empty')).to_have_text('No matching sessions. Searched for: message_profile', timeout=20000)
    assert_no_cyrillic(page)


def test_summary_block(page_en, ui_server):
    page = page_en
    summary = {'title': 'Catalog checked', 'summary': 'Found the entry in the catalog.',
               'next_step': 'Check search in conversations.', 'closed': True,
               'related': [{'key': CLAUDE, 'why': 'same topic'}], 'at': 1790600000, 'model': 'kimi'}
    ui_server.store.set_summary(KEY, summary)
    english_data(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('summary-text')).to_have_text(summary['summary'])
    expect(page.get_by_test_id('summary-toggle')).to_have_text('more')
    expect(page.get_by_test_id('next-step').locator('.ctx-label')).to_have_text('Next step')
    expect(page.get_by_test_id('copy-next')).to_have_text('Copy for the agent')
    expect(page.get_by_test_id('copy-next')).to_have_attribute('title', 'Copies a message to paste to the agent right after you resume')
    expect(page.get_by_test_id('closed-hint')).to_contain_text('Status')
    expect(page.get_by_test_id('closed-hint')).to_contain_text('Kimi: the task looks done.')
    expect(page.get_by_test_id('summary-done')).to_have_text('Mark as done')
    expect(page.locator('.ctx-row', has=page.get_by_test_id('related-chip')).locator('.ctx-label')).to_have_text('Related')
    page.get_by_test_id('summary-toggle').click()
    expect(page.locator('.summary-when')).to_have_text(re.compile(r'^summary by Kimi · \w+, \w{3} \d+, \d+:\d\d [AP]M$'))
    expect(page.get_by_test_id('summary-toggle')).to_have_text('less')
    expect(page.get_by_test_id('summary-title')).to_have_text('Title: “Catalog checked”')
    expect(page.get_by_test_id('summary-apply')).to_have_text('Apply')
    expect(page.get_by_test_id('summary-refresh')).to_have_text('Refresh summary')
    assert_no_cyrillic(page)
    page.get_by_test_id('summary-apply').click()
    expect(page.get_by_test_id('summary-applied')).to_have_text('applied')
    page.get_by_test_id('copy-next').click()
    assert page.evaluate('window.copiedText') == "Let's pick up where we left off. Next step: Check search in conversations."

    error = {'n': 1, 'type': 'error', 'message': 'Kimi exited with code 7', 'code': 7, 'output': 'context limit'}
    page.route('**/api/summary', lambda route: route.fulfill(json={'job': 'summary:' + CLAUDE}))
    page.route('**/api/jobs/*/events?*', lambda route: route.fulfill(
        content_type='application/x-ndjson', body=json.dumps(error) + '\n'))
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-error')).to_contain_text('Summary failed: Kimi exited with code 7 · code 7')
    expect(page.get_by_test_id('summary-retry')).to_have_text('Retry')
    assert_no_cyrillic(page)


def test_restore_mode(page_en, ui_server):
    page = page_en
    english_data(page)
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    restore = page.get_by_test_id('restore')
    expect(restore.locator('h1')).to_have_text('Restore sessions')
    expect(restore.locator('.restore-heading .restore-note')).to_have_text('0 selected')
    expect(restore).to_contain_text("Check sessions in the list, or pick all of yesterday's at once.")
    expect(restore.get_by_test_id('pick-yesterday')).to_have_text("Select yesterday's")
    expect(restore.get_by_test_id('pick-done')).to_contain_text('Close')
    expect(page.get_by_test_id('pick-count')).to_have_text('0 selected')
    tools = page.locator('.pick-tools')
    expect(tools.get_by_test_id('pick-yesterday')).to_have_text('yesterday')
    expect(tools.get_by_test_id('pick-today')).to_have_text('today')
    expect(tools.get_by_test_id('pick-none')).to_have_text('none')
    expect(tools.get_by_test_id('pick-next')).to_have_text('Next')
    expect(tools.get_by_test_id('pick-done')).to_have_text('Done')
    assert_no_cyrillic(page)

    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.get_by_test_id('pick-count')).to_have_text('2 selected')
    expect(restore.locator('.restore-heading .restore-note')).to_have_text('2 selected')
    expect(page.locator(f'[data-testid="picked-row"][data-key="{KEY}"]')).to_contain_text('no folder · no cd')
    expect(page.locator(f'[data-testid="picked-row"][data-key="{KEY}"] [data-testid="pick-remove"]')).to_have_attribute('aria-label', 'Remove from list')
    expect(page.get_by_test_id('fmt-wt')).to_have_text('Windows Terminal tabs')
    expect(page.get_by_test_id('fmt-tmux')).to_have_text('tmux windows')
    expect(page.get_by_test_id('fmt-plain')).to_have_text('command list')
    expect(page.get_by_test_id('copy-script')).to_contain_text('Copy script')
    expect(page.get_by_test_id('copy-script')).to_be_enabled()
    expect(page.locator('.script-note')).to_contain_text('nit-resume is a Nit helper')
    assert_no_cyrillic(page)
    page.get_by_test_id('fmt-tmux').click()
    expect(page.locator('.script-note')).to_have_text('Runs inside WSL. Sessions open as windows in the tmux session nit.')
    page.get_by_test_id('fmt-plain').click()
    expect(page.locator('.script-note')).to_have_text('Paste each command into its own WSL tab.')
    page.get_by_test_id('copy-script').click()
    expect(page.get_by_test_id('copy-script')).to_contain_text('Copied')
    assert_no_cyrillic(page)


def test_dates_and_projects(page_en, ui_server):
    page = page_en
    zone = ZoneInfo('Europe/Samara')
    page.clock.set_fixed_time(datetime(2026, 10, 5, 15, 0, tzinfo=zone))
    english_data(page, updated={
        KEY: datetime(2026, 10, 2, 14, 0, tzinfo=zone).timestamp(),
        CLAUDE: datetime(2026, 10, 5, 9, 30, tzinfo=zone).timestamp(),
        GROK: datetime(2026, 10, 4, 20, 15, tzinfo=zone).timestamp(),
    })
    open_main(page, ui_server)
    days = page.locator('.sec.day')
    expect(days.nth(0)).to_have_text('Today')
    expect(days.nth(1)).to_have_text('Yesterday')
    expect(page.locator('.sec.day', has_text='Monday, Sep 28')).to_have_count(1)
    expect(page.locator(f'[data-testid="row"][data-key="{CLAUDE}"] .row-tail')).to_have_text('9:30 AM')
    expect(page.locator(f'[data-testid="pinned-section"] [data-key="{KEY}"] .row-tail')).to_have_text('Fri')
    expect(page.get_by_test_id('session-meta')).to_contain_text('Friday, Oct 2, 2:00 PM')

    page.get_by_test_id('nav-projects').click()
    bare = page.locator('[data-testid="project-row"][data-project=""]')
    expect(bare).to_contain_text('No project')
    bare.click()
    expect(page.get_by_test_id('project-filter')).to_have_text('Project: No project')
    expect(page.get_by_test_id('project-filter-clear')).to_have_attribute('aria-label', 'Clear project filter')
    expect(page.locator(f'[data-testid="row"][data-key="{BARE}"]')).to_be_visible()
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"]')).to_have_count(0)
    page.reload()
    expect(page.get_by_test_id('project-filter')).to_have_text('Project: No project')
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"]')).to_have_count(0)
    page.get_by_test_id('project-filter-clear').click()
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"]')).to_have_count(1)
    assert_no_cyrillic(page)


def test_loading_screen(page_en, ui_server):
    polls = []

    def sessions(route):
        polls.append(1)
        if len(polls) < 3:
            route.fulfill(json={'ready': False, 'sessions': [], 'port': ui_server.server.server_port,
                                'progress': {'done': 412, 'total': 659, 'tools': {'codex': 'done', 'claude': 'done', 'grok': 'reading', 'kimi': 'waiting'}}})
        else:
            route.continue_()
    page_en.route('**/api/sessions', sessions)
    page_en.goto(ui_server.url)
    expect(page_en.get_by_test_id('loading')).to_contain_text('Reading session logs')
    expect(page_en.get_by_test_id('loading-progress')).to_have_text('412 of 659 · Codex, Claude Code done · reading Grok')
    expect(page_en.get_by_test_id('row').first).to_be_visible()


def test_language_setting_overrides_browser(page, ui_server, save_settings):
    """Русский браузер, в настройках английский: до ответа сервера — русский, потом английский."""
    save_settings({'language': 'en'})
    held = []
    page.route('**/api/config', lambda route: held.append(route))
    page.goto(ui_server.url)
    expect(page.get_by_test_id('loading')).to_contain_text('Читаю журналы сессий')
    expect(page.get_by_test_id('nav-mine')).to_have_text('Мои')
    page.get_by_test_id('search').fill('catalog')
    deadline = time.monotonic() + 5
    while not held and time.monotonic() < deadline:
        page.wait_for_timeout(20)
    held[0].continue_()
    expect(page.get_by_test_id('nav-mine')).to_have_text('Mine')
    expect(page.locator('html')).to_have_attribute('lang', 'en')
    expect(page.get_by_test_id('search')).to_have_value('catalog')
    expect(page.get_by_test_id('search')).to_have_attribute('placeholder', 'Title, branch, path, ID, summary')
    expect(page.locator('[data-testid="row"] mark').first).to_have_text('catalog')
    # Каркас перерисован: поле поиска и ленту по-прежнему слушают.
    page.get_by_test_id('search').fill('no-such-session-anywhere')
    expect(page.get_by_test_id('no-results')).to_contain_text('Nothing found for “no-such-session-anywhere”')


def test_russian_setting_overrides_english_browser(page_en, ui_server):
    response = page_en.request.post(ui_server.url + '/api/settings', data={'settings': {'language': 'ru'}},
                                    headers={'Origin': ui_server.url})
    assert response.ok, response.text()
    page_en.goto(ui_server.url)
    expect(page_en.get_by_test_id('row').first).to_be_visible()
    expect(page_en.get_by_test_id('nav-mine')).to_have_text('Мои')
    expect(page_en.locator('html')).to_have_attribute('lang', 'ru')
    expect(page_en).to_have_title('Нить')
    expect(page_en.locator('.brand')).to_have_text('нить')
