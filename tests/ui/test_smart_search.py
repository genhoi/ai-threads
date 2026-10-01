"""Умный поиск на главном экране: запуск, ход поиска, результат, пусто, отмена, перезагрузка, ошибка.

Заглушки CLI отвечают на оба шага поиска: слова задаёт AI_THREADS_STUB_TERMS (по умолчанию
«каталог»), отбор возвращает первые два ключа кандидатов в обратном порядке с why «нашлось: <ключ>».
"""

import json
import re
import time

from playwright.sync_api import expect

KEY = 'codex:00000000-0000-4000-8000-000000000001'
QUERY = 'где я проверял каталог'


def open_main(page, server):
    page.goto(server.url + '/')
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', KEY)


def posts(page):
    calls = []
    page.on('request', lambda req: calls.append((req.url.split('/api/')[-1], req.post_data_json))
            if req.method == 'POST' else None)
    return calls


def wait_jobs_finished(server, timeout=10):
    deadline = time.monotonic() + timeout
    while server.server.jobs.running() and time.monotonic() < deadline:
        time.sleep(0.05)
    assert not server.server.jobs.running()


def test_button_result_why_reload_and_close(page, ui_server):
    calls = posts(page)
    open_main(page, ui_server)
    smart = page.get_by_test_id('smart-search')
    # Без запроса строк-действий над лентой нет; с запросом — «Спросить агента: «…»» и Ctrl+↵.
    expect(smart).to_have_count(0)
    page.get_by_test_id('search').fill(QUERY)
    expect(smart).to_be_enabled()
    expect(smart).to_contain_text(f'Спросить агента: «{QUERY}»')
    expect(smart.locator('.kbd')).to_have_text('Ctrl+↵')
    expect(smart).to_have_attribute('title', 'Агент разберёт запрос и найдёт сессии · Ctrl+Enter')
    smart.click()
    head = page.get_by_test_id('smart-head')
    expect(head).to_contain_text(f'Умный поиск: «{QUERY}»', timeout=20000)
    expect(head).to_contain_text('2 сессии · отобрал Kimi')
    assert ('smart-search', {'q': QUERY, 'scope': 'mine'}) in calls
    rows = page.get_by_test_id('row')
    expect(rows).to_have_count(2)
    for index in range(2):
        key = rows.nth(index).get_attribute('data-key')
        expect(rows.nth(index).get_by_test_id('smart-why')).to_have_text(f'нашлось: {key}')
    first = rows.first.get_attribute('data-key')
    rows.first.click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', first)
    expect(rows).to_have_count(2)
    # Результат живёт в sessionStorage, пока его не закрыли.
    page.reload()
    expect(page.get_by_test_id('smart-head')).to_contain_text(QUERY)
    expect(page.get_by_test_id('row')).to_have_count(2)
    page.get_by_test_id('smart-close').click()
    expect(page.get_by_test_id('smart-head')).to_have_count(0)
    expect(page.get_by_test_id('smart-why')).to_have_count(0)
    expect(page.get_by_test_id('pinned-section')).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('pinned-section')).to_be_visible()
    expect(page.get_by_test_id('smart-head')).to_have_count(0)


def test_ctrl_enter_all_scope_progress_and_plan(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    monkeypatch.setenv('AI_THREADS_STUB_TERMS', 'каталог,catalog')
    monkeypatch.setenv('AI_THREADS_STUB_AGENTS', 'codex')
    calls = posts(page)
    open_main(page, ui_server)
    page.get_by_test_id('nav-all').click()
    page.get_by_test_id('search').fill('каталог в codex')
    page.get_by_test_id('search').press('Control+Enter')
    progress = page.get_by_test_id('smart-progress')
    expect(progress).to_contain_text('Умный поиск: «каталог в codex»')
    expect(progress).to_contain_text('Kimi понимает запрос')
    expect(progress.get_by_test_id('smart-cancel')).to_have_text('Остановить')
    expect(page.get_by_test_id('smart-plan')).to_have_text('Ищу: каталог · catalog. Агенты: Codex.', timeout=10000)
    expect(progress).to_contain_text('Kimi отбирает сессии', timeout=10000)
    expect(page.get_by_test_id('smart-head')).to_be_visible(timeout=20000)
    expect(page.get_by_test_id('smart-progress')).to_have_count(0)
    keys = page.get_by_test_id('row').evaluate_all('els => els.map(e => e.dataset.key)')
    assert keys and all(key.startswith('codex:') for key in keys)
    assert ('smart-search', {'q': 'каталог в codex', 'scope': 'all'}) in calls


def test_nothing_found_shows_terms(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_TERMS', 'message_profile')
    open_main(page, ui_server)
    page.get_by_test_id('search').fill('где партиционировал message_profile')
    page.get_by_test_id('smart-search').click()
    expect(page.get_by_test_id('smart-empty')).to_have_text('Подходящих сессий нет. Искал: message_profile', timeout=20000)
    expect(page.get_by_test_id('smart-head')).to_contain_text('Умный поиск: «где партиционировал message_profile»')
    expect(page.get_by_test_id('row')).to_have_count(0)


def test_cancel_returns_to_feed(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'hang')
    calls = posts(page)
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    page.get_by_test_id('smart-search').click()
    expect(page.get_by_test_id('smart-progress')).to_contain_text('Kimi понимает запрос')
    page.get_by_test_id('smart-cancel').click()
    expect(page.get_by_test_id('smart-progress')).to_have_count(0)
    expect(page.get_by_test_id('smart-error')).to_have_count(0)
    expect(page.get_by_test_id('smart-head')).to_have_count(0)
    assert any(re.fullmatch(r'jobs/search(:|%3A)\d+/cancel', path) for path, _ in calls), calls
    wait_jobs_finished(ui_server)
    page.reload()
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('smart-progress')).to_have_count(0)


def test_reload_during_search_reconnects_and_fits_narrow(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    page.set_viewport_size({'width': 390, 'height': 900})
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    page.get_by_test_id('smart-search').click()
    expect(page.get_by_test_id('smart-progress')).to_contain_text('Kimi понимает запрос')
    page.reload()
    progress = page.get_by_test_id('smart-progress')
    expect(progress).to_contain_text(f'Умный поиск: «{QUERY}»')
    expect(progress).to_contain_text('Kimi')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    assert progress.evaluate('el => el.scrollWidth <= el.clientWidth')
    head = page.get_by_test_id('smart-head')
    expect(head).to_contain_text('2 сессии · отобрал Kimi', timeout=20000)
    assert head.evaluate('el => el.scrollWidth <= el.clientWidth')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')


def test_error_retry_and_word_matches(page, ui_server, monkeypatch):
    failed = []

    def events(route):
        if failed:
            route.continue_()
            return
        failed.append(route.request.url)
        route.fulfill(content_type='application/x-ndjson',
                      body=json.dumps({'n': 1, 'type': 'error', 'message': 'Kimi завершился с кодом 2'}) + '\n')
    page.route(re.compile(r'/api/jobs/search[^/]*/events'), events)
    open_main(page, ui_server)
    page.get_by_test_id('search').fill(QUERY)
    page.get_by_test_id('smart-search').click()
    expect(page.get_by_test_id('smart-error')).to_contain_text('Поиск не получился: Kimi завершился с кодом 2')
    # Агент отвечает не JSON: поиск идёт по словам запроса и показывает совпадения без отбора.
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'garbage')
    page.get_by_test_id('smart-retry').click()
    head = page.get_by_test_id('smart-head')
    expect(head).to_contain_text('совпадения по словам', timeout=20000)
    expect(page.get_by_test_id('smart-error')).to_have_count(0)
    warnings = page.get_by_test_id('smart-warning')
    expect(warnings).to_have_count(2)
    expect(warnings.first).to_contain_text('Kimi не разобрал запрос')
    expect(page.get_by_test_id('row').first.get_by_test_id('smart-why')).not_to_be_empty()
