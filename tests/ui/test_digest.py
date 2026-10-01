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
    # журнал агента после ошибки: запрос и ошибка; номера сессии нет — ответить нельзя
    page.get_by_test_id('digest-agent-log').click()
    view = page.get_by_test_id('agent-session')
    expect(view.get_by_test_id('log-error')).to_contain_text('Kimi завершился с кодом 2')
    expect(view.get_by_test_id('log-prompt')).to_have_count(1)
    expect(view.locator('.log-note')).to_have_text('Агент не сообщил номер сессии, поэтому ответить ему отсюда нельзя.')
    expect(view.get_by_test_id('reply-input')).to_have_count(0)
    view.get_by_test_id('agent-back').click()
    expect(page.get_by_test_id('digest-error')).to_be_visible()
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'ok')
    calls = posts(page)
    page.get_by_test_id('digest-fallback').click()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=20000)
    assert calls[-1] == ('digest', {'days': 5, 'model': 'claude'})


def log_kinds(view):
    return view.locator('#agent-entries > *').evaluate_all('els => els.map(e => e.dataset.testid)')


def test_agent_log_reply_and_terminal(page, ui_server):
    calls = posts(page)
    open_digest(page, ui_server)
    page.get_by_test_id('model-claude').click()
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=20000)
    page.get_by_test_id('digest-agent-log').click()
    # журнал на месте сводки, как сессия агента: шапка с агентом, моделью, состоянием и временем
    view = page.get_by_test_id('agent-session')
    expect(page.get_by_test_id('digest-lead')).to_have_count(0)
    expect(view.get_by_test_id('agent-back')).to_have_text('← К сводке')
    expect(view.get_by_test_id('agent-title')).to_have_text('Сводка за 24–28 сентября')
    expect(view.get_by_test_id('agent-name')).to_have_text('Claude Code')
    expect(view.get_by_test_id('agent-model')).to_have_text('stub-model')
    expect(view.get_by_test_id('agent-state')).to_have_text('закончено')
    expect(view.get_by_test_id('agent-time')).to_have_text(re.compile(r'^\d+:\d\d$'))
    expect(view.get_by_test_id('agent-stop')).to_have_count(0)
    kinds = log_kinds(view)
    assert kinds[0] == 'log-prompt' and {'log-model', 'log-tool', 'log-result', 'log-text', 'log-usage'} <= set(kinds), kinds
    # запрос агенту — свёрнутое сообщение «Нити»; текст агента — сообщение с его подписью
    prompt = view.get_by_test_id('log-prompt')
    expect(prompt.locator('.speaker')).to_have_text('Нить')
    expect(prompt.locator('summary')).to_have_text('Запрос агенту')
    expect(prompt.locator('pre')).to_be_hidden()
    expect(view.get_by_test_id('log-text').first.locator('.speaker')).to_have_text('Claude Code')
    expect(view.get_by_test_id('log-model')).to_have_text('Модель: stub-model')
    expect(view.get_by_test_id('log-tool').first).to_contain_text('Read')
    result = view.get_by_test_id('log-result').first
    expect(result.locator('summary')).to_have_text('Результат инструмента')
    expect(result.locator('pre')).to_be_hidden()
    result.locator('summary').click()
    expect(result.locator('pre')).to_contain_text('строка журнала')
    expect(view.get_by_test_id('log-usage')).to_contain_text('900')
    # продолжить в терминале той же сессией
    expect(view.locator('.reply-terminal')).to_contain_text('Продолжить в терминале:')
    command = view.get_by_test_id('reply-command')
    expect(command).to_contain_text('claude --resume stub-session')
    view.get_by_test_id('reply-copy-command').click()
    expect(view.get_by_test_id('reply-copy-command')).to_have_text('Скопировано')
    assert command.inner_text() == '$ ' + page.evaluate('window.copiedText')
    # ответ агенту: Shift+Enter переносит строку, Enter отправляет
    reply = view.get_by_test_id('reply-input')
    expect(reply).to_have_attribute('placeholder', 'Написать агенту…')
    expect(view.get_by_test_id('reply-send')).to_be_disabled()
    expect(reply).to_have_attribute('title', 'Enter — отправить, Shift+Enter — новая строка')
    reply.fill('Почему так?')
    reply.press('Shift+Enter')
    reply.press_sequentially('Объясни')
    expect(reply).to_have_value('Почему так?\nОбъясни')
    reply.press('Enter')
    you = view.get_by_test_id('log-user')
    expect(you.locator('.speaker')).to_have_text('Я')
    expect(you).to_contain_text('Объясни')
    expect(reply).to_be_enabled(timeout=15000)
    expect(reply).to_have_value('')
    expect(reply).to_be_focused()
    assert calls[-1] == ('agent/reply', {'agent': 'claude', 'session': 'stub-session', 'text': 'Почему так?\nОбъясни'})
    kinds = log_kinds(view)
    assert kinds.count('log-prompt') == 1 and 'log-text' in kinds[kinds.index('log-user'):], kinds
    # после перезагрузки журнал с ответом на месте, пока сервер помнит задание
    page.reload()
    page.get_by_test_id('digest-agent-log').click()
    expect(view.get_by_test_id('log-user')).to_contain_text('Объясни')
    expect(view.get_by_test_id('reply-input')).to_be_enabled()
    page.keyboard.press('Escape')
    expect(page.get_by_test_id('digest-lead')).to_be_visible()
    expect(page.get_by_test_id('digest-agent-log')).to_be_focused()
    # у сводки за другой период журнала этого запуска нет
    page.get_by_test_id('days-3').click()
    expect(page.get_by_test_id('digest-agent-log')).to_have_count(0)


def test_agent_log_while_running_and_stop_reply(page, ui_server, monkeypatch):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    open_digest(page, ui_server)
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-running')).to_be_visible()
    page.get_by_test_id('digest-agent-log').click()
    view = page.get_by_test_id('agent-session')
    # строки приходят по ходу запуска; отвечать можно, только когда запуск закончился
    expect(view.get_by_test_id('log-tool').first).to_contain_text('Read')
    expect(view.get_by_test_id('agent-state')).to_have_text('идёт')
    expect(view.get_by_test_id('agent-stop')).to_be_visible()
    expect(view.get_by_test_id('reply-input')).to_have_count(0)
    expect(view.get_by_test_id('reply-command')).to_contain_text('session_stub', timeout=20000)
    expect(view.get_by_test_id('agent-state')).to_have_text('закончено')
    # долгий ответ останавливается
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'hang')
    view.get_by_test_id('reply-input').fill('Ещё раз')
    view.get_by_test_id('reply-send').click()
    stop = view.get_by_test_id('agent-stop')
    expect(stop).to_have_text('Остановить')
    expect(view.get_by_test_id('reply-input')).to_be_disabled()
    expect(view.get_by_test_id('agent-state')).to_have_text('идёт')
    with page.expect_request(re.compile(r'/api/jobs/reply(:|%3A)\d+/cancel$')):
        stop.click()
    expect(view.get_by_test_id('log-stopped')).to_have_text('Остановлено.')
    expect(view.get_by_test_id('reply-input')).to_be_enabled()
    view.get_by_test_id('agent-back').click()
    expect(page.get_by_test_id('digest-lead')).to_be_visible()


def test_model_and_effort_choice(page, ui_server):
    calls = posts(page)
    open_digest(page, ui_server)
    page.get_by_test_id('model-grok').click()
    model, effort = page.get_by_test_id('digest-agent-model'), page.get_by_test_id('digest-effort')
    expect(model.locator('option')).to_have_text(['как в CLI (grok-test)', 'grok-test', 'grok-test-fast'])
    expect(model).to_have_attribute('aria-label', 'Модель агента')
    expect(effort.locator('option')).to_have_text(['как в CLI', 'low', 'medium', 'high', 'xhigh'])
    expect(effort).to_have_attribute('aria-label', 'Уровень рассуждений')
    model.select_option('grok-test-fast')
    effort.select_option('high')
    page.get_by_test_id('digest-run').click()
    expect(model).to_be_disabled()
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Главное за период', timeout=20000)
    assert calls[-1] == ('digest', {'days': 5, 'model': 'grok', 'agent_model': 'grok-test-fast', 'effort': 'high'})
    # у Claude Code уровни общие для всех моделей, у Kimi в этом окружении ни моделей, ни уровней
    page.get_by_test_id('model-claude').click()
    expect(page.get_by_test_id('digest-agent-model').locator('option')).to_have_text(
        ['как в CLI', 'opus', 'sonnet', 'fable', 'haiku'])
    expect(page.get_by_test_id('digest-effort').locator('option')).to_have_text(
        ['как в CLI', 'low', 'medium', 'high', 'xhigh', 'max'])
    page.get_by_test_id('model-kimi').click()
    expect(page.get_by_test_id('digest-agent-model').locator('option')).to_have_text(['как в CLI'])
    expect(page.get_by_test_id('digest-effort')).to_have_count(0)
    # выбор запоминается для каждого агента; «как в CLI» не передаётся
    page.reload()
    page.get_by_test_id('model-grok').click()
    expect(page.get_by_test_id('digest-agent-model')).to_have_value('grok-test-fast')
    expect(page.get_by_test_id('digest-effort')).to_have_value('high')
    page.get_by_test_id('digest-agent-model').select_option('')
    page.get_by_test_id('digest-effort').select_option('')
    sent = len(calls)
    page.get_by_test_id('digest-regen').click()
    # Кнопка может стать активной раньше, чем уйдёт запрос: ждём сам запрос.
    for _ in range(200):
        if len(calls) > sent:
            break
        page.wait_for_timeout(50)
    assert calls[-1] == ('digest', {'days': 5, 'model': 'grok'})


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
