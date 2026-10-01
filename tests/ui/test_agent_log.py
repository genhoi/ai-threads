"""«Сессия агента» справа: журнал умного поиска и сводки, ответ агенту, выбор агента и модели.

Заглушки CLI печатают вызовы инструментов, их результаты, ответ и ID сессии: у kimi — session_stub.
Ответ заглушки на сообщение человека — JSON сводки, этого достаточно, чтобы проверить переписку.
"""

import json
import re
from urllib.parse import quote

from playwright.sync_api import expect

from ai_threads import runner

KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
QUERY = 'где я проверял каталог'
CODEX_MODELS = {'agent': 'codex', 'default_model': 'gpt-a', 'default_effort': '', 'efforts': [],
                'models': [{'id': 'gpt-a', 'name': 'GPT A', 'efforts': ['low', 'high'], 'default_effort': 'high'},
                           {'id': 'gpt-b', 'name': 'gpt-b', 'efforts': ['medium', 'xhigh'], 'default_effort': 'medium'}]}


def open_main(page, server, key=KEY):
    page.goto(server.url + f'/#section=mine&session={quote(key)}')
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', key)


def posts(page):
    calls = []
    page.on('request', lambda req: calls.append((req.url.split('/api/')[-1], req.post_data_json))
            if req.method == 'POST' else None)
    return calls


def summarize(page):
    """Составить сводку открытой сессии и открыть её журнал."""
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-text')).to_be_visible(timeout=15000)
    page.get_by_test_id('summary-log').click()
    expect(page.get_by_test_id('agent-session')).to_be_visible()


def test_smart_search_runs_on_the_right(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    expect(page.get_by_test_id('search-full')).to_contain_text(f'Искать «{QUERY}» в журналах целиком')
    expect(page.get_by_test_id('choice-search')).to_have_text('Авто')
    page.get_by_test_id('smart-search').click()

    agent = page.get_by_test_id('agent-session')
    expect(agent).to_be_visible()
    expect(page).to_have_url(re.compile(r'view=agent&log=search%3A\d+'))
    expect(page.get_by_test_id('agent-title')).to_have_text(f'Умный поиск: «{QUERY}»')
    expect(page.get_by_test_id('agent-state')).to_have_text('идёт')
    expect(page.get_by_test_id('agent-name')).to_have_text('Kimi')
    expect(page.get_by_test_id('agent-stop')).to_be_visible()
    # Запрос агенту свёрнут и раскрывается; вызовы инструментов — компактными строками.
    prompt = page.get_by_test_id('log-prompt').first
    expect(prompt).to_contain_text('Нить')
    expect(prompt.locator('.log-pre')).to_be_hidden()
    prompt.locator('summary').click()
    expect(prompt.locator('.log-pre')).to_contain_text('Разбери запрос поиска')
    expect(page.get_by_test_id('log-tool').first).to_contain_text('Read')
    expect(page.get_by_test_id('reply-input')).to_have_count(0)

    # Кандидаты по словам — до отбора агентом.
    expect(page.get_by_test_id('smart-preliminary')).to_have_text('предварительно, агент отбирает…', timeout=15000)
    expect(page.get_by_test_id('row').first.get_by_test_id('smart-why')).to_have_text('совпало: каталог')
    expect(page.get_by_test_id('log-nit').first).to_contain_text('Нить:')
    head = page.get_by_test_id('smart-head')
    expect(head).to_be_visible(timeout=20000)
    expect(page.get_by_test_id('smart-preliminary')).to_have_count(0)
    expect(page.get_by_test_id('row')).to_have_count(2)
    expect(page.get_by_test_id('agent-state')).to_have_text('закончено')
    expect(page.get_by_test_id('agent-stop')).to_have_count(0)
    expect(page.get_by_test_id('log-prompt')).to_have_count(2)
    expect(page.get_by_test_id('reply-input')).to_have_attribute('placeholder', 'Написать агенту…')
    expect(page.get_by_test_id('reply-command')).to_contain_text('kimi --session session_stub')

    # Щелчок по результату открывает сессию справа, «Журнал агента» возвращает журнал.
    page.get_by_test_id('row').first.click()
    expect(page.get_by_test_id('session')).to_be_visible()
    expect(agent).to_have_count(0)
    head.get_by_test_id('smart-log').click()
    expect(page.get_by_test_id('log-prompt')).to_have_count(2)
    # После перезагрузки журнал собирается заново из событий задания.
    page.reload()
    expect(page.get_by_test_id('agent-session')).to_be_visible()
    expect(page.get_by_test_id('log-prompt')).to_have_count(2)
    expect(page.get_by_test_id('agent-title')).to_have_text(f'Умный поиск: «{QUERY}»')
    expect(page.get_by_test_id('reply-input')).to_be_visible()


def test_summary_log_reply_and_terminal(page, ui_server, monkeypatch):
    calls = posts(page)
    open_main(page, ui_server)
    summarize(page)
    expect(page.get_by_test_id('agent-title')).to_have_text('Сводка: Проверить каталог')
    expect(page.get_by_test_id('agent-state')).to_have_text('закончено')
    expect(page.get_by_test_id('log-tool').first).to_contain_text('Read')
    result = page.get_by_test_id('log-result').first
    expect(result.locator('.log-pre')).to_be_hidden()
    result.locator('summary').click()
    expect(result.locator('.log-pre')).to_contain_text('строка журнала')
    expect(page.get_by_test_id('log-text')).to_have_count(1)

    command = page.get_by_test_id('reply-command')
    expect(command).to_contain_text('kimi --session session_stub')
    page.get_by_test_id('reply-copy-command').click()
    assert page.evaluate('window.copiedText') == command.text_content().removeprefix('$ ')

    reply = page.get_by_test_id('reply-input')
    reply.fill('Первая строка')
    reply.press('Shift+Enter')
    reply.press_sequentially('вторая')
    expect(reply).to_have_value('Первая строка\nвторая')
    assert not [path for path, _ in calls if path == 'agent/reply']
    reply.fill('Что дальше?')
    reply.press('Enter')
    expect(page.get_by_test_id('log-user')).to_contain_text('Что дальше?')
    expect(page.get_by_test_id('log-text')).to_have_count(2, timeout=15000)
    expect(reply).to_be_enabled()
    assert ('agent/reply', {'agent': 'kimi', 'session': 'session_stub', 'text': 'Что дальше?'}) in calls

    # Пока агент отвечает, поле неактивно, а «Остановить» в шапке отменяет ответ.
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'hang')
    reply.fill('Подожди')
    page.get_by_test_id('reply-send').click()
    expect(reply).to_be_disabled()
    page.get_by_test_id('agent-stop').click()
    expect(page.get_by_test_id('log-stopped')).to_have_text('Остановлено.')
    expect(reply).to_be_enabled()
    assert any(re.fullmatch(r'jobs/reply(:|%3A)\d+/cancel', path) for path, _ in calls), calls

    # Сервер перезапускали — сессию агента он не помнит.
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'ok')
    runner.SESSIONS.clear()
    reply.fill('Ещё вопрос')
    reply.press('Enter')
    expect(page.get_by_test_id('log-error').last).to_contain_text('не помнит')

    # Журнал с ответами переживает перезагрузку, «← К сессии» возвращает сводку.
    page.reload()
    expect(page.get_by_test_id('log-user')).to_have_count(3)
    expect(page.get_by_test_id('log-error').last).to_contain_text('не помнит')
    page.get_by_test_id('agent-back').click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', KEY)
    expect(page.get_by_test_id('summary-text')).to_be_visible()


def test_summary_log_while_running_and_unknown_job(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    monkeypatch.setenv('AI_THREADS_SUMMARY_TIMEOUT', '30')
    ui_server.store.set_summary(CLAUDE, {'title': 'Старая сводка', 'summary': 'Из прошлого запуска сервера.',
                                         'next_step': '', 'closed': False, 'related': [], 'at': 1790600000})
    open_main(page, ui_server)
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-progress')).to_be_visible()
    page.get_by_test_id('summary-log').click()
    expect(page.get_by_test_id('agent-state')).to_have_text('идёт')
    expect(page.get_by_test_id('log-prompt')).to_contain_text('Запрос агенту')
    expect(page.get_by_test_id('agent-state')).to_have_text('закончено', timeout=15000)
    expect(page.get_by_test_id('log-text')).to_have_count(1)
    page.get_by_test_id('agent-back').click()
    expect(page.get_by_test_id('summary-log')).to_be_visible()
    # Сводка из прошлого запуска сервера: задания нет, ссылки на журнал тоже.
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.get_by_test_id('summary-text')).to_have_text('Из прошлого запуска сервера.')
    expect(page.get_by_test_id('summary-log')).to_have_count(0)


def test_choice_menu(page, ui_server):
    calls = posts(page)
    page.route('**/api/models?agent=codex', lambda route: route.fulfill(json=CODEX_MODELS))
    page.route('**/api/smart-search', lambda route: route.fulfill(json={'job': 'search:99'}))
    page.route('**/api/jobs/search*/events?*', lambda route: route.fulfill(
        content_type='application/x-ndjson',
        body=json.dumps({'n': 1, 'type': 'result', 'result': {'query': QUERY, 'results': [], 'ranked': True}}) + '\n'))
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    choice = page.get_by_test_id('choice-search')
    choice.click()
    menu = page.get_by_test_id('choice-menu')
    agents = menu.get_by_test_id('choice-agent')
    expect(agents).to_have_text(['АвтоKimi', 'Kimi', 'Claude Code', 'Codex', 'Grok'])
    expect(menu).to_contain_text('Модель и уровень рассуждений — как настроено в CLI')
    expect(menu.get_by_test_id('choice-model')).to_have_count(0)
    menu.locator('[data-testid="choice-agent"][data-value="grok"]').click()
    expect(menu.get_by_test_id('choice-model')).to_have_text(['как в CLI (grok-test)', 'grok-test', 'grok-test-fast'])
    expect(menu.get_by_test_id('choice-effort')).to_have_text(['как в CLI', 'low', 'medium', 'high', 'xhigh'])
    menu.locator('[data-testid="choice-model"][data-value="grok-test-fast"]').click()
    menu.locator('[data-testid="choice-effort"][data-value="high"]').click()
    expect(menu.locator('[data-testid="choice-effort"][data-value="high"]')).to_have_attribute('aria-checked', 'true')
    expect(choice).to_have_text('Grok · grok-test-fast · high')
    page.keyboard.press('Escape')
    expect(menu).to_have_count(0)
    page.get_by_test_id('search').press('Control+Enter')
    expect(page.get_by_test_id('smart-empty')).to_be_visible()
    assert ('smart-search', {'q': QUERY, 'scope': 'mine', 'agent': 'grok', 'model': 'grok-test-fast', 'effort': 'high'}) in calls

    # Выбор переживает перезагрузку; у сводки выбор свой.
    page.reload()
    page.get_by_test_id('smart-close').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.get_by_test_id('search').fill(QUERY)
    expect(page.get_by_test_id('choice-search')).to_have_text('Grok · grok-test-fast · high')
    page.get_by_test_id('search').fill('')
    summary_choice = page.get_by_test_id('choice-summary')
    expect(summary_choice).to_have_text('Авто')
    summary_choice.click()
    menu.locator('[data-testid="choice-agent"][data-value="codex"]').click()
    # У Codex уровни свои у каждой модели; уровень, которого у новой модели нет, сбрасывается.
    expect(menu.get_by_test_id('choice-model')).to_have_text(['как в CLI (gpt-a)', 'GPT Agpt-a', 'gpt-b'])
    expect(menu.get_by_test_id('choice-effort')).to_have_text(['как в CLI (high)', 'low', 'high'])
    menu.locator('[data-testid="choice-model"][data-value="gpt-b"]').click()
    expect(menu.get_by_test_id('choice-effort')).to_have_text(['как в CLI (medium)', 'medium', 'xhigh'])
    menu.locator('[data-testid="choice-effort"][data-value="xhigh"]').click()
    expect(summary_choice).to_have_text('Codex · gpt-b · xhigh')
    menu.locator('[data-testid="choice-model"][data-value="gpt-a"]').click()
    expect(summary_choice).to_have_text('Codex · gpt-a')
    # У Kimi уровней рассуждений нет.
    menu.locator('[data-testid="choice-agent"][data-value="kimi"]').click()
    expect(menu.get_by_test_id('choice-model')).to_have_count(1)
    expect(menu.get_by_test_id('choice-effort')).to_have_count(0)
    menu.locator('[data-testid="choice-agent"][data-value="codex"]').click()
    menu.locator('[data-testid="choice-model"][data-value="gpt-b"]').click()
    menu.locator('[data-testid="choice-effort"][data-value="xhigh"]').click()
    page.keyboard.press('Escape')
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-text')).to_be_visible(timeout=15000)
    assert ('summary', {'key': KEY, 'agent': 'codex', 'model': 'gpt-b', 'effort': 'xhigh'}) in calls
    expect(page.locator('[data-testid="summary-row"] .ctx-label .tool-icon')).to_have_attribute('title', 'Codex')

    # «Авто» убирает выбор модели, и в запрос ничего лишнего не уходит.
    page.get_by_test_id('summary-toggle').click()
    page.get_by_test_id('choice-summary').click()
    menu.locator('[data-testid="choice-agent"][data-value=""]').click()
    expect(menu.get_by_test_id('choice-model')).to_have_count(0)
    expect(page.get_by_test_id('choice-summary')).to_have_text('Авто')
    page.keyboard.press('Escape')
    page.get_by_test_id('summary-refresh').click()
    expect(page.get_by_test_id('summary-progress')).to_be_visible()
    expect(page.get_by_test_id('summary-text')).to_be_visible(timeout=15000)
    assert calls.count(('summary', {'key': KEY})) == 1


def test_palette_keyboard(page, ui_server):
    calls = posts(page)
    page.route('**/api/full-search', lambda route: route.fulfill(json={'job': 'fullsearch:99'}))
    page.route('**/api/jobs/fullsearch*/events?*', lambda route: route.fulfill(
        content_type='application/x-ndjson',
        body=json.dumps({'n': 1, 'type': 'result', 'result': {'query': 'catalog', 'terms': ['catalog'], 'results': [],
                                                               'found': 0, 'scanned': 5}}) + '\n'))
    open_main(page, ui_server)
    search = page.get_by_test_id('search')
    search.fill('catalog')
    # Обычный поиск нашёл сессии: Enter откроет выбранную, у полного поиска клавиши нет.
    expect(page.get_by_test_id('search-full').locator('.kbd')).to_have_count(0)
    search.press('ArrowUp')
    expect(page.locator('.feed-action-row')).to_have_class(re.compile('is-sel'))
    search.press('ArrowUp')
    expect(page.get_by_test_id('search-full')).to_have_class(re.compile('is-sel'))
    expect(page.get_by_test_id('search-full').locator('.kbd')).to_have_text('↵')
    search.press('ArrowDown')
    search.press('ArrowDown')
    expect(page.locator('.feed-actions .is-sel')).to_have_count(0)
    expect(page.locator('[data-testid="row"].is-sel')).to_have_count(1)
    search.press('ArrowUp')
    search.press('ArrowUp')
    search.press('Enter')
    expect(page.get_by_test_id('full-empty')).to_have_text('В журналах «catalog» нет')
    assert ('full-search', {'q': 'catalog', 'scope': 'mine'}) in calls


def test_narrow_agent_screen(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    page.set_viewport_size({'width': 390, 'height': 900})
    page.goto(ui_server.url)
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.get_by_test_id('search').fill(QUERY)
    page.get_by_test_id('smart-search').click()
    # В узком окне на экране остаётся список с ходом поиска; журнал — отдельный экран.
    progress = page.get_by_test_id('smart-progress')
    expect(progress).to_be_visible()
    progress.get_by_test_id('smart-log').click()
    agent = page.get_by_test_id('agent-session')
    expect(agent).to_be_visible()
    expect(page.locator('#feed')).to_be_hidden()
    expect(page.get_by_test_id('back-to-list')).to_have_text('← Список')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    expect(page.get_by_test_id('agent-state')).to_have_text('закончено', timeout=20000)
    expect(page.get_by_test_id('reply-input')).to_be_visible()
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.get_by_test_id('back-to-list').click()
    expect(page.get_by_test_id('smart-head')).to_be_visible()
    expect(agent).to_be_hidden()


def test_agent_session_and_menu_in_english(page_en, ui_server):
    page = page_en
    page.goto(ui_server.url + f'/#section=mine&session={quote(KEY)}')
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-text')).to_be_visible(timeout=15000)
    expect(page.get_by_test_id('summary-log')).to_have_text('agent log')
    page.get_by_test_id('summary-log').click()
    expect(page.get_by_test_id('agent-back')).to_have_text('← To the session')
    expect(page.get_by_test_id('agent-title')).to_have_text(re.compile(r'^Summary: '))
    expect(page.get_by_test_id('agent-state')).to_have_text('finished')
    expect(page.get_by_test_id('log-prompt').first).to_contain_text('NitPrompt to the agent')
    expect(page.get_by_test_id('log-result').first.locator('summary')).to_have_text('Tool result')
    expect(page.get_by_test_id('reply-input')).to_have_attribute('placeholder', 'Message the agent…')
    expect(page.get_by_test_id('reply-send')).to_have_text('Send')
    expect(page.locator('.reply-terminal > span')).to_have_text('Continue in terminal:')
    expect(page.get_by_test_id('reply-copy-command')).to_have_text('Copy')
    page.get_by_test_id('reply-input').fill('What next?')
    page.get_by_test_id('reply-input').press('Enter')
    expect(page.get_by_test_id('log-user')).to_have_text('MeWhat next?')
    expect(page.get_by_test_id('log-text')).to_have_count(2, timeout=15000)

    page.get_by_test_id('search').fill('catalog')
    page.get_by_test_id('choice-search').click()
    menu = page.get_by_test_id('choice-menu')
    expect(menu.locator('.menu-sec').first).to_have_text('Agent')
    expect(menu).to_contain_text('Model and reasoning effort as configured in the CLI')
    menu.locator('[data-testid="choice-agent"][data-value="grok"]').click()
    expect(menu.locator('.menu-sec')).to_have_text(['Agent', 'Model', 'Reasoning effort'])
    expect(menu.get_by_test_id('choice-model').first).to_have_text('as in CLI (grok-test)')
    expect(menu.get_by_test_id('choice-effort').first).to_have_text('as in CLI')
    expect(menu.get_by_test_id('choice-agent').first).to_contain_text('Auto')
    page.keyboard.press('Escape')
    expect(page.get_by_test_id('choice-search')).to_have_text('Grok')
    # Подписи шапки журнала, поля ответа и строк-действий — без русских букв (название сессии — данные).
    found = page.evaluate("""() => [...document.querySelectorAll('[data-testid="agent-back"], [data-testid="agent-meta"], .agent-reply, .feed-actions')]
        .flatMap(el => [el.innerText, ...[...el.querySelectorAll('[title],[aria-label],[placeholder]')]
          .map(e => e.getAttribute('title') || e.getAttribute('aria-label') || e.getAttribute('placeholder'))])
        .filter(text => /[А-Яа-яЁё]/.test(text || ''))""")
    assert found == [], found
