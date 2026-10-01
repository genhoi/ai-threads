"""Сценарии экрана «Сводка»: пустое состояние, составление, ошибка,
остановка, чекбоксы хвостов, переходы по чипам, снимки для сравнения с макетом."""

import time
import re
from datetime import datetime
from pathlib import Path
from urllib.parse import quote
from zoneinfo import ZoneInfo

import pytest
from playwright.sync_api import expect

ROOT = Path(__file__).resolve().parents[2]
KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
KIMI = 'kimi:session_00000000-0000-4000-8000-000000000001'
AUTO = 'codex:00000000-0000-4000-8000-000000000002'
FROZEN = datetime(2026, 9, 28, 12, 0, tzinfo=ZoneInfo('Europe/Samara'))


def posts(page):
    calls = []
    page.on('request', lambda req: calls.append((req.url.split('/api/')[-1], req.post_data_json))
            if req.method == 'POST' else None)
    return calls


def open_digest(page, server, path='/digest.html'):
    page.clock.set_fixed_time(FROZEN)
    page.goto(server.url + path)


def rich_digest():
    return {'at': datetime(2026, 9, 28, 4, 15, tzinfo=ZoneInfo('Europe/Samara')).timestamp(),
            'model': 'kimi',
            'result': {
                'lead': 'Основная работа шла в sample app: закрыт рост 502 на /api/cart '
                        'после релиза 2.41, готов план платной доставки и отревьюен MR 3571. '
                        'Параллельно начата переделка интерфейса «Нити».',
                'projects': [{'name': 'sample app', 'bullets': [
                    {'text': 'Рост 502 на /api/cart после релиза 2.41 разобран и закрыт: '
                             'cart-api падал по памяти, лимит поднят до 1 GB, добавлен '
                             'алерт CartApiRestarts.',
                     'keys': [KEY, CLAUDE]},
                    {'text': 'Готов план релиза платной доставки: флаг paid_delivery, '
                             'выкат 5% → 50% → 100%.',
                     'keys': [KIMI]}]}],
                'tails': [{'id': 't1', 'text': 'Проверить утренний пик на /api/cart '
                                                'после поднятия лимита', 'key': KEY},
                          {'id': 't2', 'text': 'Согласовать дату выката платной доставки на 5%',
                           'key': CLAUDE}],
                'autos': [{'key': AUTO, 'project': 'mnp-go-first',
                           'text': 'Ревью sms-gateway !714 «сервис MNP на Go»: '
                                   'есть блокирующее замечание.'}]}}


def test_empty_state_period_model_and_compose(page, ui_server):
    calls = posts(page)
    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-empty')).to_be_visible()
    expect(page.get_by_test_id('digest-empty')).to_contain_text('ещё нет')
    expect(page.get_by_test_id('digest-empty')).to_contain_text('9 ваших сессий и 4 автоматических')
    expect(page.get_by_test_id('days-5')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('model-kimi')).to_have_attribute('aria-pressed', 'true')
    # период переключает вид, сам сводку не запускает
    page.get_by_test_id('days-3').click()
    expect(page.get_by_test_id('days-3')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('digest-empty')).to_be_visible()
    assert not calls
    # выбор модели: следующий запуск пойдёт через неё
    page.get_by_test_id('model-claude').click()
    expect(page.get_by_test_id('model-claude')).to_have_attribute('aria-pressed', 'true')
    assert not calls
    # правая колонка: 9 ручных сессий периода, без статусов в покое
    rows = page.get_by_test_id('digest-session')
    expect(rows.first).to_be_visible()
    assert rows.count() == 9
    assert rows.first.get_attribute('data-status') == ''
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=20000)
    assert calls[-1] == ('digest', {'days': 3, 'model': 'claude'})
    expect(page.get_by_test_id('digest-meta')).to_contain_text('Claude Code')
    expect(page.get_by_test_id('digest-regen')).to_have_text('Составить заново')
    expect(page.get_by_test_id('digest-empty')).to_have_count(0)
    # скопировать текстом отдаёт текст сводки
    page.get_by_test_id('digest-copy').click()
    assert 'Главное за период' in page.evaluate('window.copiedText')
    assert 'Что осталось сделать' in page.evaluate('window.copiedText')


def test_settings_link_in_header(page, ui_server):
    page.route('**/settings.html', lambda route: route.fulfill(content_type='text/html; charset=utf-8',
                                                              body='<title>Настройки</title>'))
    open_digest(page, ui_server)
    link = page.get_by_test_id('settings-link')
    expect(link).to_have_attribute('href', '/settings.html')
    link.click()
    expect(page).to_have_url(ui_server.url + '/settings.html')


def test_running_progress_and_stop_returns_previous(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    ui_server.store.set_digest(5, {'at': FROZEN.timestamp() - 8 * 3600, 'model': 'kimi',
                                   'result': {'lead': 'Прошлая сводка', 'projects': [],
                                              'tails': [], 'autos': []}})
    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Прошлая сводка')
    page.get_by_test_id('digest-regen').click()
    expect(page.get_by_test_id('digest-running')).to_contain_text('Kimi составляет сводку за')
    expect(page.get_by_test_id('digest-stages')).to_contain_text('Сбор')
    expect(page.get_by_test_id('digest-stages')).to_contain_text('Чтение журналов')
    expect(page.get_by_test_id('digest-log')).to_contain_text('Передано модели')
    expect(page.get_by_test_id('digest-notes')).to_contain_text('Заметки по ходу')
    expect(page.get_by_test_id('digest-notes')).to_contain_text('Появятся, когда модель дочитает')
    # контекст расхода Kimi не сообщает — полоски контекста нет
    expect(page.get_by_test_id('digest-tokens')).to_have_count(0)
    # статусы сессий честно отражают чтение журналов заглушкой
    statuses = page.locator('[data-testid="digest-session"]').evaluate_all(
        'els => els.map(e => e.dataset.status)')
    assert 'now' in statuses and 'ok' in statuses and 'skip' in statuses
    # период и модель заблокированы на время прогона
    expect(page.get_by_test_id('days-3')).to_be_disabled()
    expect(page.get_by_test_id('model-claude')).to_be_disabled()
    page.get_by_test_id('digest-stop').click()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Прошлая сводка', timeout=15000)
    assert ui_server.store.digest(5)['result']['lead'] == 'Прошлая сводка'
    expect(page.get_by_test_id('days-3')).to_be_enabled()


def test_error_retry_and_fallback(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'error')
    open_digest(page, ui_server)
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-error')).to_contain_text('Сводка не получилась')
    expect(page.get_by_test_id('digest-error')).to_contain_text('Kimi завершился с кодом 2')
    expect(page.get_by_test_id('digest-error')).to_contain_text('boom')
    expect(page.get_by_test_id('digest-meta')).to_have_text('не составлена')
    expect(page.get_by_test_id('digest-retry')).to_be_visible()
    expect(page.get_by_test_id('digest-fallback')).to_have_text('Составить через Claude Code')
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'ok')
    calls = posts(page)
    page.get_by_test_id('digest-fallback').click()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=20000)
    assert calls[-1] == ('digest', {'days': 5, 'model': 'claude'})


def test_running_job_survives_reload(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    open_digest(page, ui_server)
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-running')).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('digest-running')).to_be_visible()
    expect(page.get_by_test_id('digest-running')).to_contain_text('Kimi составляет сводку за')
    expect(page.get_by_test_id('digest-log')).to_contain_text('Передано модели', timeout=10000)
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=25000)


def test_tail_checkbox_survives_reload(page, ui_server):
    ui_server.store.set_digest(3, {'at': FROZEN.timestamp() - 8 * 3600, 'model': 'kimi',
                                   'result': {'lead': 'Главное', 'projects': [],
                                              'tails': [{'id': 't1', 'text': 'Доделать следом',
                                                         'key': CLAUDE}],
                                              'autos': []}})
    open_digest(page, ui_server, '/digest.html#days=3')
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное')
    row = page.get_by_test_id('digest-tail')
    expect(row).to_contain_text('Доделать следом')
    expect(row.locator('input')).not_to_be_checked()
    calls = posts(page)
    row.locator('input').check()
    expect(row).to_have_class(re.compile("is-done"))
    assert ('digest/tail', {'id': 't1', 'done': True}) in calls
    page.reload()
    expect(page.get_by_test_id('digest-tail').locator('input')).to_be_checked()
    expect(page.get_by_test_id("digest-tail")).to_have_class(re.compile("is-done"))
    expect(page.get_by_test_id('digest-tail')).to_contain_text('Доделать следом')


def test_chip_opens_session_on_main_screen(page, ui_server):
    ui_server.store.set_digest(5, {'at': time.time(), 'model': 'kimi',
                                   'result': {'lead': 'Главное',
                                              'projects': [{'name': 'sample app', 'bullets': [
                                                  {'text': 'Сделано важное', 'keys': [KEY]}]}],
                                              'tails': [{'id': 't1', 'text': 'Доделать',
                                                         'key': KEY}],
                                              'autos': []}})
    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное')
    page.locator('[data-testid="digest-project"] .chip-s').first.click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', KEY, timeout=15000)
    assert 'section=all' in page.url and 'view=session' in page.url
    assert f'session={quote(KEY)}' in page.url


def test_tail_chip_opens_session(page, ui_server):
    ui_server.store.set_digest(5, {'at': time.time(), 'model': 'kimi',
                                   'result': {'lead': 'Главное', 'projects': [],
                                              'tails': [{'id': 't1', 'text': 'Доделать',
                                                         'key': CLAUDE}],
                                              'autos': []}})
    open_digest(page, ui_server)
    page.locator('[data-testid="digest-tail"] .chip-s').click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', CLAUDE, timeout=15000)


def test_model_unavailable_disabled_with_hint(page, ui_server):
    page.route('**/api/digest/models', lambda route: route.fulfill(json=[
        {'id': 'kimi', 'name': 'Kimi', 'available': True, 'hint': 'Составить сводку через Kimi'},
        {'id': 'claude', 'name': 'Claude Code', 'available': True,
         'hint': 'Составить сводку через Claude Code'},
        {'id': 'codex', 'name': 'Codex', 'available': True, 'hint': 'Составить сводку через Codex'},
        {'id': 'grok', 'name': 'Grok', 'available': False, 'hint': 'Grok не установлен'}]))
    open_digest(page, ui_server)
    expect(page.get_by_test_id('model-grok')).to_be_disabled()
    expect(page.get_by_test_id('model-grok')).to_have_attribute('title', 'Grok не установлен')
    expect(page.get_by_test_id('model-kimi')).to_be_enabled()


def test_models_follow_server_list(page, ui_server):
    page.route('**/api/digest/models', lambda route: route.fulfill(json=[
        {'id': 'kimi', 'name': 'Kimi', 'available': False, 'hint': 'Kimi не установлен'},
        {'id': 'newbie', 'name': 'Новичок', 'available': True, 'hint': 'Составить сводку через Новичок'},
        {'id': 'claude', 'name': 'Claude Code', 'available': True,
         'hint': 'Составить сводку через Claude Code'}]))
    open_digest(page, ui_server)
    buttons = page.locator('[data-testid^="model-"]')
    expect(buttons).to_have_count(3)
    assert buttons.evaluate_all('els => els.map(e => e.dataset.testid)') == ['model-kimi', 'model-newbie', 'model-claude']
    # Kimi не установлен: по умолчанию первая доступная модель из списка сервера.
    expect(page.get_by_test_id('model-newbie')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('model-newbie').locator('.tool-icon text')).to_have_text('N')
    page.get_by_test_id('model-claude').click()
    expect(page.get_by_test_id('model-claude')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('model-newbie')).to_have_attribute('aria-pressed', 'false')


def test_default_model_from_settings(page, ui_server, save_settings):
    save_settings({'digest': {'agent': 'codex'}})
    page.add_init_script("localStorage.setItem('nit.digest.model', 'claude')")
    open_digest(page, ui_server)
    expect(page.get_by_test_id('model-codex')).to_have_attribute('aria-pressed', 'true')
    save_settings({})
    page.reload()
    expect(page.get_by_test_id('model-claude')).to_have_attribute('aria-pressed', 'true')


def test_reference_screens(page, ui_server, monkeypatch):
    if page.context.browser.browser_type.name != 'chromium':
        return
    directory = ROOT / 'test-results/screens'
    directory.mkdir(parents=True, exist_ok=True)

    def shot(name):
        page.evaluate('document.fonts.ready')
        page.screenshot(path=str(directory / f'{name}.png'), animations='disabled')

    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-empty')).to_be_visible()
    shot('Digest-Empty')
    ui_server.store.set_digest(5, rich_digest())
    page.reload()
    expect(page.get_by_test_id('digest-lead')).to_be_visible()
    expect(page.get_by_test_id('digest-tail').first).to_be_visible()
    shot('Digest')
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    page.get_by_test_id('digest-regen').click()
    expect(page.get_by_test_id('digest-log')).to_contain_text('Передано модели')
    expect(page.get_by_test_id('digest-stages')).to_contain_text('Чтение журналов')
    shot('Digest-Run')
    expect(page.get_by_test_id('digest-lead')).to_be_visible(timeout=25000)
