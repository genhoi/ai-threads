"""Полный поиск без агента: строка над лентой, ход поиска, результат с фрагментами, пусто, остановка.

В журналах фикстур слово «каталоге» есть, «message_profile» — нет. Чтобы увидеть ход поиска,
тест замедляет подсчёт совпадений на сервере: он работает в том же процессе.
"""

import re
import time

import pytest
from playwright.sync_api import expect

from ai_threads import search

KEY = 'codex:00000000-0000-4000-8000-000000000001'


@pytest.fixture
def slow_scan(monkeypatch):
    """Подсчёт совпадений сообщает «1 из N» и ждёт pause секунд (или отмены задания)."""
    original, state = search.score, {'pause': 0.0}

    def score(job, pool, terms, names, summaries, on_progress):
        on_progress(1, len(pool))
        deadline = time.monotonic() + state['pause']
        while time.monotonic() < deadline and not job.cancelled:
            time.sleep(0.05)
        return original(job, pool, terms, names, summaries, on_progress)
    monkeypatch.setattr(search, 'score', score)
    return state


def posts(page):
    calls = []
    page.on('request', lambda req: calls.append((req.url.split('/api/')[-1], req.post_data_json))
            if req.method == 'POST' else None)
    return calls


def open_main(page, server):
    page.goto(server.url + '/')
    expect(page.get_by_test_id('row').first).to_be_visible()


def test_enter_progress_result_reload_and_close(page, ui_server, slow_scan):
    slow_scan['pause'] = 1.5
    calls = posts(page)
    open_main(page, ui_server)
    search_box = page.get_by_test_id('search')
    search_box.fill('каталоге')
    expect(page.get_by_test_id('no-results')).to_be_visible()
    full = page.get_by_test_id('search-full')
    expect(full).to_have_text('Искать «каталоге» в журналах целиком↵')
    search_box.press('Enter')
    progress = page.get_by_test_id('full-progress')
    expect(progress.locator('.smart-text')).to_have_text(re.compile(r'^Ищу «каталоге» в журналах: 1 из \d+$'))
    expect(progress.get_by_test_id('full-bar')).to_be_visible()
    expect(progress.get_by_test_id('full-cancel')).to_have_text('Остановить')
    head = page.get_by_test_id('full-head')
    expect(head.locator('strong')).to_have_text('Полный поиск: «каталоге»', timeout=10000)
    expect(head.locator('small')).to_have_text(re.compile(r'^\d+ сесси(я|и|й)$'))
    expect(progress).to_have_count(0)
    rows = page.get_by_test_id('row')
    expect(rows.first).to_be_visible()
    snippet = rows.first.get_by_test_id('full-snippet')
    expect(snippet.locator('mark').first).to_have_text(re.compile('^каталоге$', re.I))
    assert ('full-search', {'q': 'каталоге', 'scope': 'mine'}) in calls
    count = rows.count()

    # Новый ввод в поле результат не сбрасывает; переживает перезагрузку; закрывает крестик.
    search_box.fill('другое')
    expect(head).to_be_visible()
    expect(rows).to_have_count(count)
    page.reload()
    expect(page.get_by_test_id('full-head').locator('strong')).to_have_text('Полный поиск: «каталоге»')
    expect(page.get_by_test_id('row')).to_have_count(count)
    page.get_by_test_id('full-close').click()
    expect(page.get_by_test_id('full-head')).to_have_count(0)
    expect(page.get_by_test_id('pinned-section')).to_be_visible()


def test_stop_empty_and_scope(page, ui_server, slow_scan):
    calls = posts(page)
    open_main(page, ui_server)
    slow_scan['pause'] = 30
    page.get_by_test_id('search').fill('каталоге')
    page.get_by_test_id('search-full').click()
    progress = page.get_by_test_id('full-progress')
    expect(progress).to_contain_text('Ищу «каталоге» в журналах')
    progress.get_by_test_id('full-cancel').click()
    expect(progress).to_have_count(0)
    expect(page.get_by_test_id('full-head')).to_have_count(0)
    expect(page.get_by_test_id('full-error')).to_have_count(0)
    assert any(re.fullmatch(r'jobs/fullsearch(:|%3A)\d+/cancel', path) for path, _ in calls), calls

    slow_scan['pause'] = 0
    page.get_by_test_id('nav-all').click()
    page.get_by_test_id('search').fill('message_profile')
    page.get_by_test_id('search').press('Enter')
    expect(page.get_by_test_id('full-empty')).to_have_text('В журналах «message_profile» нет')
    expect(page.get_by_test_id('full-head').locator('small')).to_have_count(0)
    assert ('full-search', {'q': 'message_profile', 'scope': 'all'}) in calls


def test_full_search_in_english(page_en, ui_server, slow_scan):
    slow_scan['pause'] = 1.5
    page = page_en
    open_main(page, ui_server)
    page.get_by_test_id('search').fill('каталоге')
    expect(page.get_by_test_id('search-full')).to_have_text('Search full logs for “каталоге”↵')
    page.get_by_test_id('search').press('Enter')
    progress = page.get_by_test_id('full-progress')
    expect(progress.locator('.smart-text')).to_have_text(re.compile(r'^Searching full logs for “каталоге”: 1 of \d+$'))
    expect(progress.get_by_test_id('full-cancel')).to_have_text('Stop')
    head = page.get_by_test_id('full-head')
    expect(head.locator('strong')).to_have_text('Full search: “каталоге”', timeout=10000)
    expect(head.locator('small')).to_have_text(re.compile(r'^\d+ sessions?$'))
    expect(head.get_by_test_id('full-close')).to_have_attribute('aria-label', 'Back to the feed')
    page.get_by_test_id('search').fill('message_profile')
    page.get_by_test_id('full-close').click()
    page.get_by_test_id('search').press('Enter')
    expect(page.get_by_test_id('full-empty')).to_have_text('No “message_profile” in the logs')
