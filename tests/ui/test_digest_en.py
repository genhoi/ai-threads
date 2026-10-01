"""Экран «Сводка» в английском браузере: тексты страницы, даты и время по-английски.
Тексты, которые присылает сервер (ход сводки, ошибки, подсказки моделей), здесь не проверяются."""

import re
from datetime import datetime
from zoneinfo import ZoneInfo

from playwright.sync_api import expect

FROZEN = datetime(2026, 9, 28, 12, 0, tzinfo=ZoneInfo('Europe/Samara'))
KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
AUTO = 'codex:00000000-0000-4000-8000-000000000002'


def open_digest(page, server):
    page.clock.set_fixed_time(FROZEN)
    page.goto(server.url + '/digest.html')


def ready_digest():
    return {'at': datetime(2026, 9, 28, 4, 15, tzinfo=ZoneInfo('Europe/Samara')).timestamp(), 'model': 'kimi',
            'result': {'lead': 'Main work went into sample app.',
                       'projects': [{'name': 'sample app', 'bullets': [{'text': 'Fixed the cart.', 'keys': [KEY]}]}],
                       'tails': [{'id': 't1', 'text': 'Check the morning peak', 'key': KEY},
                                 {'id': 't2', 'text': 'Agree on the rollout date', 'key': CLAUDE}],
                       'autos': [{'key': AUTO, 'project': 'sample app', 'text': 'Review found a blocker.'}]}}


def test_empty_state_in_english(page_en, ui_server):
    page = page_en
    open_digest(page, ui_server)
    empty = page.get_by_test_id('digest-empty')
    expect(empty).to_contain_text('No digest for September 24–28 yet')
    expect(empty).to_contain_text('Covers 9 of your sessions and 4 automated runs from this period. Usually takes 2–5 minutes.')
    expect(page.get_by_test_id('digest-run')).to_have_text('Generate')
    assert page.evaluate('document.documentElement.lang') == 'en'
    assert page.title() == 'Nit — digest'
    # шапка, период и строка о сводке
    expect(page.get_by_test_id('nav-mine')).to_have_text('Mine')
    expect(page.get_by_test_id('nav-all')).to_contain_text('All sessions')
    expect(page.get_by_test_id('nav-projects')).to_contain_text('Projects')
    expect(page.get_by_test_id('nav-digest')).to_have_text('Digest')
    expect(page.get_by_test_id('settings-link')).to_have_attribute('aria-label', 'Settings')
    expect(page.locator('.digest-toolbar h1')).to_have_text('Digest')
    expect(page.locator('[data-testid^="days-"]')).to_have_text(['3 days', '4 days', '5 days'])
    expect(page.get_by_role('group', name='Period')).to_be_visible()
    expect(page.get_by_test_id('digest-meta')).to_have_text('no digest for 5 days')
    expect(page.get_by_test_id('digest-regen')).to_have_text('Generate')
    expect(page.get_by_test_id('digest-copy')).to_have_text('Copy as text')
    page.get_by_test_id('days-3').click()
    expect(empty).to_contain_text('No digest for September 26–28 yet')
    expect(page.get_by_test_id('digest-meta')).to_have_text('no digest for 3 days')
    # правая колонка: время по-английски, пустые название и проект подписаны
    side = page.get_by_role('complementary', name='By session')
    expect(side).to_contain_text('By session')
    expect(side.locator('.side-note')).to_have_text(re.compile(r'^\d+ in 3 days · one line per session$'))
    expect(side.locator('.day').first).to_have_text('Today')
    expect(page.get_by_test_id('digest-session').first).to_have_attribute('title', 'Open session')
    times = page.locator('.srow-time').all_inner_texts()
    assert times and all(re.fullmatch(r'\d{1,2}:\d{2}\s[AP]M', t) for t in times), times
    page.get_by_test_id('days-5').click()
    expect(side).to_contain_text('sample app · Untitled')
    expect(side).to_contain_text('No project · Untitled')
    # меню «Данные»
    page.get_by_test_id('data-menu').click()
    expect(page.get_by_test_id('data-menu')).to_have_text('Data')
    expect(page.get_by_test_id('data-export')).to_have_text('Export JSON')
    expect(page.get_by_test_id('data-import')).to_have_text('Import JSON…')


def test_ready_digest_in_english(page_en, ui_server):
    page = page_en
    ui_server.store.set_digest(5, ready_digest())
    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-lead')).to_contain_text('Highlights · September 24–28')
    expect(page.get_by_test_id('digest-meta')).to_have_text(
        re.compile(r'^generated today at 4:15\sAM · Kimi · \d+ sessions in \d+ projects$'))
    expect(page.get_by_test_id('digest-regen')).to_have_text('Regenerate')
    main = page.locator('#main')
    expect(main.get_by_role('heading', name='By project')).to_be_visible()
    expect(page.get_by_test_id('digest-project').locator('.proj-count')).to_have_text(re.compile(r'^\d+ sessions?$'))
    expect(main.get_by_role('heading', name='Still to do · 2 of 2')).to_be_visible()
    expect(main.get_by_role('heading', name='Automated runs · 1')).to_be_visible()
    expect(main).to_contain_text('Other agents ran these CLIs for reviews and independent checks.')
    expect(page.locator('.chip-s').first).to_have_attribute('title', 'Open session')
    page.get_by_test_id('digest-tail').first.locator('input').check()
    expect(main.get_by_role('heading', name='Still to do · 1 of 2')).to_be_visible()
    page.get_by_test_id('digest-copy').click()
    expect(page.get_by_test_id('digest-copy')).to_have_text('Copied')
    text = page.evaluate('window.copiedText')
    assert text.startswith('Digest for September 24–28\n')
    assert '\nStill to do\n[x] Check the morning peak\n[ ] Agree on the rollout date' in text


def test_running_and_error_in_english(page_en, ui_server, monkeypatch):
    page = page_en
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    open_digest(page, ui_server)
    page.get_by_test_id('digest-run').click()
    running = page.get_by_test_id('digest-running')
    expect(running).to_contain_text('Kimi is generating the digest for September 24–28')
    expect(running).to_contain_text('Elapsed')
    expect(running).to_contain_text('usually 2–5 minutes · stopping brings back the previous digest')
    expect(page.get_by_test_id('digest-stages')).to_have_text(
        re.compile(r'Collect.*Read logs.*Analyze.*projects and automated runs.*Write.*highlights and to-dos', re.S))
    expect(page.get_by_role('heading', name='What the model is doing')).to_be_visible()
    expect(page.get_by_test_id('digest-notes')).to_contain_text('Notes so far')
    expect(page.get_by_test_id('digest-meta')).to_have_text('generating…')
    expect(page.locator('.side-legend')).to_have_text(re.compile(
        r'existing summary\s*log read\s*reading\s*queued\s*skipped'))
    expect(page.get_by_test_id('digest-stop')).to_have_text('Stop')
    page.get_by_test_id('digest-stop').click()
    expect(page.get_by_test_id('digest-empty')).to_be_visible(timeout=15000)
    # ошибка модели: заголовок и кнопки страницы по-английски, текст ошибки — от сервера
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'error')
    page.get_by_test_id('digest-run').click()
    error = page.get_by_test_id('digest-error')
    expect(error).to_contain_text('Digest failed')
    expect(error).to_contain_text('code 2')
    expect(page.get_by_test_id('digest-retry')).to_have_text('Retry')
    expect(page.get_by_test_id('digest-fallback')).to_have_text('Generate with Claude Code')
    expect(page.get_by_test_id('digest-meta')).to_have_text('not generated')


def test_language_setting_beats_browser(page_en, ui_server):
    page = page_en
    response = page.request.post(ui_server.url + '/api/settings', data={'settings': {'language': 'ru'}},
                                 headers={'Origin': ui_server.url})
    assert response.ok, response.text()
    open_digest(page, ui_server)
    expect(page.get_by_test_id('digest-empty')).to_contain_text('Сводки за 24–28 сентября ещё нет')
    expect(page.locator('[data-testid^="days-"]')).to_have_text(['3 дня', '4 дня', '5 дней'])
    expect(page.get_by_test_id('nav-mine')).to_have_text('Мои')
    assert page.evaluate('document.documentElement.lang') == 'ru'


def test_agent_log_and_choice_in_english(page_en, ui_server):
    page = page_en
    open_digest(page, ui_server)
    page.get_by_test_id('model-grok').click()
    model, effort = page.get_by_test_id('digest-agent-model'), page.get_by_test_id('digest-effort')
    expect(model.locator('option')).to_have_text(['CLI default (grok-test)', 'grok-test', 'grok-test-fast'])
    expect(model).to_have_attribute('aria-label', 'Agent model')
    expect(effort.locator('option').first).to_have_text('CLI default')
    expect(effort).to_have_attribute('aria-label', 'Reasoning effort')
    page.get_by_test_id('model-claude').click()
    page.get_by_test_id('digest-run').click()
    expect(page.get_by_test_id('digest-agent-log')).to_have_text('Agent log', timeout=20000)
    expect(page.get_by_test_id('digest-lead')).to_be_visible()
    page.get_by_test_id('digest-agent-log').click()
    view = page.get_by_test_id('agent-session')
    expect(view.get_by_test_id('agent-back')).to_have_text('← To the digest')
    expect(view.get_by_test_id('agent-title')).to_have_text('Digest for September 24–28')
    expect(view.get_by_test_id('agent-state')).to_have_text('finished')
    expect(view.get_by_test_id('log-prompt').locator('.speaker')).to_have_text('Nit')
    expect(view.get_by_test_id('log-prompt').locator('summary')).to_have_text('Prompt to the agent')
    expect(view.get_by_test_id('log-model')).to_contain_text('Model: stub-model')
    expect(view.get_by_test_id('log-result').first.locator('summary')).to_have_text('Tool result')
    expect(view.locator('.reply-terminal')).to_contain_text('Continue in terminal:')
    expect(view.get_by_test_id('reply-copy-command')).to_have_text('Copy')
    view.get_by_test_id('reply-copy-command').click()
    expect(view.get_by_test_id('reply-copy-command')).to_have_text('Copied')
    reply = view.get_by_test_id('reply-input')
    expect(reply).to_have_attribute('placeholder', 'Message the agent…')
    expect(reply).to_have_attribute('title', 'Enter to send, Shift+Enter for a new line')
    expect(view.get_by_test_id('reply-send')).to_have_text('Send')
    reply.fill('Why?')
    reply.press('Enter')
    expect(view.get_by_test_id('log-user').locator('.speaker')).to_have_text('Me')
    expect(reply).to_be_enabled(timeout=15000)
