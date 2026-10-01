"""Сценарии главного экрана: видимый результат, запросы и сохранённые данные."""

import json
import re
import shutil
import time
from pathlib import Path
from urllib.parse import quote

import pytest
from playwright.sync_api import expect

ROOT = Path(__file__).resolve().parents[2]
KEY = 'codex:00000000-0000-4000-8000-000000000001'
CLAUDE = 'claude:00000000-0000-4000-8000-000000000001'
SUMMARY = {'title': 'Каталог проверен', 'summary': 'Найдена запись в каталоге. Проверено название.',
           'next_step': 'Проверить поиск по переписке', 'closed': True,
           'related': [{'key': CLAUDE, 'why': 'та же тема'}], 'at': 1790600000, 'model': 'kimi'}


def open_main(page, server, key=None):
    page.goto(server.url + (f'/#section=mine&session={quote(key)}' if key else '/'))
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', key or KEY)


def wait_pending(page, pending):
    deadline = time.monotonic() + 5
    while not pending and time.monotonic() < deadline:
        page.wait_for_timeout(20)
    assert len(pending) == 1


def posts(page):
    calls = []
    page.on('request', lambda req: calls.append((req.url.split('/api/')[-1], req.post_data_json))
            if req.method == 'POST' else None)
    return calls


def test_feed_search_and_open(page, ui_server):
    calls = posts(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('pinned-section')).to_contain_text('Проверить каталог')
    assert page.get_by_test_id('row').count() > 2
    page.get_by_test_id('search').fill('feature/catalog')
    expect(page.locator('[data-testid="row"] mark').first).to_have_text('feature/catalog')
    page.get_by_test_id('search').fill('абсолютно-несуществующая-сессия')
    expect(page.get_by_test_id('no-results')).to_contain_text('Ничего не найдено')
    page.get_by_test_id('search').fill('Запись найдена')
    expect(page.get_by_test_id('no-results')).to_be_visible()
    with page.expect_request('**/api/search?*') as request:
        page.get_by_test_id('search-conv').click()
    assert 'scope=mine' in request.value.url
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.get_by_test_id('row').filter(has_text='Проверить каталог').last.click()
    expect(page.get_by_test_id('message').last).to_contain_text('Запись найдена')
    page.get_by_test_id('nav-all').click()
    expect(page.get_by_test_id('nav-all')).to_have_class('on')
    page.reload()
    expect(page.get_by_test_id('nav-all')).to_have_class('on')
    assert not calls


def test_settings_link_in_header(page, ui_server):
    page.route('**/settings.html', lambda route: route.fulfill(content_type='text/html; charset=utf-8',
                                                              body='<title>Настройки</title>'))
    open_main(page, ui_server)
    link = page.get_by_test_id('settings-link')
    expect(link).to_have_attribute('href', '/settings.html')
    expect(link).to_have_attribute('aria-label', 'Настройки')
    link.click()
    expect(page).to_have_url(ui_server.url + '/settings.html')


def test_copy_rename_pin_and_reload(page, ui_server):
    calls = posts(page)
    open_main(page, ui_server)
    command = page.get_by_test_id('command').get_attribute('data-command')
    page.get_by_test_id('copy-command').click()
    expect(page.get_by_test_id('copy-command')).to_contain_text('Скопировано')
    assert page.evaluate('window.copiedText') == command
    assert not calls
    page.get_by_test_id('rename').click()
    page.get_by_test_id('rename-input').fill('Моё название <каталога>')
    page.get_by_test_id('rename-input').press('Enter')
    expect(page.get_by_test_id('session-title')).to_have_text('Моё название <каталога>')
    expect(page.get_by_test_id('session-orig')).to_contain_text('Проверить каталог')
    assert calls[-1] == ('name', {'key': KEY, 'name': 'Моё название <каталога>'})
    page.reload()
    expect(page.get_by_test_id('session-title')).to_have_text('Моё название <каталога>')
    page.get_by_test_id('rename').click()
    page.get_by_test_id('rename-input').fill('Отмена')
    page.get_by_test_id('rename-input').press('Escape')
    expect(page.get_by_test_id('session-title')).to_have_text('Моё название <каталога>')
    page.get_by_test_id('rename').click()
    page.get_by_test_id('rename-reset').click()
    expect(page.get_by_test_id('session-title')).to_have_text('Проверить каталог')
    page.get_by_test_id('pin').click()
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-pressed', 'false')
    assert KEY not in ui_server.store.state()['pins']
    page.get_by_test_id('pin').click()
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-pressed', 'true')
    assert KEY in ui_server.store.state()['pins']
    assert [path for path, _ in calls] == ['name', 'name', 'pin', 'pin']


def test_keyboard_projects_done_hide_and_undo(page, ui_server):
    calls = posts(page)
    open_main(page, ui_server)
    page.keyboard.press('/')
    expect(page.get_by_test_id('search')).to_be_focused()
    page.get_by_test_id('search').fill('catalog')
    page.get_by_test_id('search').press('ArrowDown')
    page.get_by_test_id('search').press('Enter')
    selected = page.get_by_test_id('session').get_attribute('data-key')
    page.keyboard.press('c')
    assert page.evaluate('window.copiedText') == page.get_by_test_id('command').get_attribute('data-command')
    assert not calls
    page.get_by_test_id('nav-projects').click()
    page.get_by_test_id('search').fill('')
    page.get_by_test_id('project-row').first.click()
    expect(page.get_by_test_id('project-filter')).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('project-filter')).to_be_visible()
    page.get_by_test_id('project-filter-clear').click()
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-done').click()
    expect(page.get_by_test_id('toast')).to_contain_text('завершённой')
    assert selected in ui_server.store.state()['done']
    page.get_by_test_id('toast-undo').click()
    expect(page.get_by_test_id('toast')).to_contain_text('Возвращено')
    assert selected not in ui_server.store.state()['done']
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-hide').click()
    expect(page.locator(f'[data-testid="row"][data-key="{selected}"]')).to_have_count(0)
    page.get_by_test_id('toast-undo').click()
    expect(page.locator(f'[data-testid="row"][data-key="{selected}"]')).to_have_count(1)


def test_migration_and_import_export(page, ui_server, tmp_path):
    calls = posts(page)
    page.add_init_script(f"localStorage.setItem('nit.pins', JSON.stringify([{json.dumps(CLAUDE)}]));"
                         f"localStorage.setItem('nit.names', JSON.stringify({{{json.dumps(KEY)}:'Старое имя'}}));")
    open_main(page, ui_server)
    expect(page.get_by_test_id('migrate-banner')).to_contain_text('1 и 1')
    page.get_by_test_id('migrate-button').click()
    expect(page.get_by_test_id('migrate-banner')).to_have_count(0)
    expect(page.get_by_test_id('session-title')).to_have_text('Старое имя')
    assert calls[-1] == ('migrate', {'pins': [CLAUDE], 'names': {KEY: 'Старое имя'}})
    page.reload()
    expect(page.get_by_test_id('migrate-banner')).to_have_count(0)
    page.get_by_test_id('data-menu').click()
    with page.expect_download() as download:
        page.get_by_test_id('data-export').click()
    exported = tmp_path / 'export.json'
    download.value.save_as(exported)
    data = json.loads(exported.read_text())
    assert data['names'][KEY] == 'Старое имя' and 'digests' not in data
    page.get_by_test_id('data-import-file').set_input_files({
        'name': 'import.json', 'mimeType': 'application/json',
        'buffer': json.dumps({'version': 1, 'pins': [], 'names': {KEY: 'Загруженное имя'}}).encode()})
    expect(page.get_by_test_id('session-title')).to_have_text('Загруженное имя')


def test_narrow_navigation_and_long_conversation(page, ui_server):
    messages = [{'role': 'user' if i % 2 == 0 else 'assistant', 'text': f'Сообщение {i}: ' + 'Текст переписки. ' * 70, 'at': None} for i in range(60)]
    page.route('**/api/session?*', lambda route: route.fulfill(json={'messages': messages, 'truncated': True}))
    page.set_viewport_size({'width': 760, 'height': 900})
    page.goto(ui_server.url)
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_be_hidden()
    page.get_by_test_id('row').first.click()
    expect(page.get_by_test_id('session')).to_be_visible()
    expect(page.get_by_test_id('row').first).to_be_hidden()
    last = page.get_by_test_id('message').last
    expect(last).to_be_in_viewport()
    expect(last).to_contain_text('Сообщение 59')
    last.get_by_test_id('message-toggle').click()
    expect(last.get_by_test_id('message-toggle')).to_have_text('Свернуть')
    page.reload()
    expect(page.get_by_test_id('session')).to_be_visible()
    expect(page.get_by_test_id('message').last).to_be_in_viewport()
    for width in [760, 390]:
        page.set_viewport_size({'width': width, 'height': 900})
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.get_by_test_id('back-to-list').click()
    expect(page.get_by_test_id('row').first).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('session')).to_be_hidden()


def test_restore_script_formats_without_open(page, ui_server):
    calls = posts(page)
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    expect(page.get_by_test_id('restore')).to_be_visible()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('pick-count')).to_have_text('Выбрано 1')
    expect(page.get_by_test_id('fmt-wt')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('script')).to_contain_text('wt.exe -w 0')
    expect(page.get_by_test_id('open-all')).to_have_count(0)
    page.keyboard.press('o')
    page.get_by_test_id('copy-script').click()
    assert KEY in page.evaluate('window.copiedText')
    page.get_by_test_id('fmt-tmux').click()
    expect(page.get_by_test_id('script')).to_contain_text('tmux attach -t nit')
    page.get_by_test_id('fmt-plain').click()
    expect(page.get_by_test_id('script')).to_contain_text('codex resume')
    page.get_by_test_id('copy-script').click()
    assert page.evaluate('window.copiedText').startswith('codex resume')
    assert not calls
    page.keyboard.press('Escape')
    expect(page.get_by_test_id('restore')).to_have_count(0)


def test_restore_format_from_settings(page, ui_server, save_settings):
    save_settings({'open_with': 'tmux', 'tmux_session': 'work'})
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('fmt-tmux')).to_have_attribute('aria-pressed', 'true')
    expect(page.get_by_test_id('script')).to_contain_text('tmux attach -t work')
    expect(page.locator('.script-note')).to_contain_text('tmux-сессии work')


def test_outside_wsl_hides_windows_terminal_and_explorer(page, no_wsl):
    open_main(page, no_wsl)
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-copy-id')).to_be_visible()
    expect(page.get_by_test_id('menu-explorer')).to_have_count(0)
    expect(page.get_by_test_id('menu-vscode')).to_be_visible()
    page.keyboard.press('Escape')
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('fmt-tmux')).to_be_visible()
    expect(page.get_by_test_id('fmt-wt')).to_have_count(0)
    expected = 'tmux' if shutil.which('tmux') else 'plain'
    expect(page.get_by_test_id(f'fmt-{expected}')).to_have_attribute('aria-pressed', 'true')
    page.get_by_test_id('fmt-tmux').click()
    expect(page.locator('.script-note')).to_have_text('Сессии откроются окнами в tmux-сессии nit.')
    page.get_by_test_id('fmt-plain').click()
    expect(page.locator('.script-note')).to_have_text('Вставляйте по одной в отдельные вкладки терминала.')
    expect(page.get_by_test_id('script')).to_contain_text('codex resume')


def test_command_highlights_id_before_flags(page, ui_server, save_settings):
    flag = '--dangerously-bypass-approvals-and-sandbox'
    save_settings({'agents': {'codex': {'skip_approvals': True}}})
    open_main(page, ui_server)
    session_id = KEY.split(':', 1)[1]
    command = page.get_by_test_id('command')
    expect(command).to_have_attribute('data-command', re.compile(re.escape(f'codex resume {session_id} {flag}') + '$'))
    expect(command.locator('code > .prompt')).to_have_text(['$ ', session_id])
    assert command.locator('code').text_content() == '$ ' + command.get_attribute('data-command')
    page.get_by_test_id('copy-command').click()
    assert page.evaluate('window.copiedText').endswith(f'{session_id} {flag}')


def test_session_without_command_shows_hint(page, ui_server):
    hint = 'Продолжить можно только в приложении ZCode'
    def sessions(route):
        data = route.fetch().json()
        for item in data['sessions']:
            if item['key'] == KEY:
                item.update(command='', command_no_cd='', resume_hint=hint)
        route.fulfill(json=data)
    page.route('**/api/sessions', sessions)
    calls = posts(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('resume-hint')).to_have_text(hint)
    expect(page.get_by_test_id('command')).to_have_count(0)
    expect(page.get_by_test_id('copy-command')).to_have_count(0)
    expect(page.get_by_test_id('missing-banner')).to_have_count(0)
    page.evaluate('window.copiedText = "Буфер не менялся"')
    page.keyboard.press('c')
    assert page.evaluate('window.copiedText') == 'Буфер не менялся'
    page.get_by_test_id('session-menu').click()
    page.get_by_test_id('menu-copy-id').click()
    assert page.evaluate('window.copiedText') == KEY.split(':', 1)[1]
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.locator(f'[data-testid="picked-row"][data-key="{KEY}"]')).to_contain_text('нельзя продолжить из терминала')
    expect(page.get_by_test_id('script')).to_have_text('# Выбранные сессии нельзя продолжить из терминала.')
    expect(page.get_by_test_id('copy-script')).to_be_disabled()
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.locator('[data-testid="restore"] .restore-heading .restore-note')).to_have_text('2 выбрано · 1 нельзя продолжить из терминала')
    expect(page.get_by_test_id('script')).to_contain_text(CLAUDE)
    expect(page.get_by_test_id('script')).not_to_contain_text(KEY)
    assert not calls


def test_summary_event_contract_and_apply(page, ui_server):
    calls = posts(page)
    page.route('**/api/summary', lambda route: route.fulfill(json={'job': 'summary:' + KEY}))
    def result(route):
        ui_server.store.set_summary(KEY, SUMMARY)
        route.fulfill(content_type='application/x-ndjson', body=json.dumps({'n': 1, 'type': 'result', 'result': SUMMARY}) + '\n')
    page.route('**/api/jobs/*/events?*', result)
    open_main(page, ui_server)
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-text')).to_have_text(SUMMARY['summary'])
    assert KEY not in ui_server.store.state()['names']
    page.get_by_test_id('summary-toggle').click()
    page.get_by_test_id('summary-apply').click()
    expect(page.get_by_test_id('session-title')).to_have_text(SUMMARY['title'])
    expect(page.get_by_test_id('summary-applied')).to_be_visible()
    page.get_by_test_id('copy-next').click()
    assert page.evaluate('window.copiedText') == f"Продолжаем с того места, где остановились. Следующий шаг: {SUMMARY['next_step'].rstrip('.')}."
    expect(page.get_by_test_id('closed-hint')).to_be_visible()
    page.get_by_test_id('related-chip').click()
    expect(page.get_by_test_id('session')).to_have_attribute('data-key', CLAUDE)
    assert calls == [('summary', {'key': KEY}), ('name', {'key': KEY, 'name': SUMMARY['title']})]


def test_summary_error_and_resume_after_reload(page, ui_server):
    error = {'n': 1, 'type': 'error', 'message': 'Kimi завершился с кодом 7', 'code': 7, 'output': 'Ошибка контекста'}
    page.route('**/api/jobs', lambda route: route.fulfill(json=[{'job': 'summary:' + KEY, 'kind': 'summary', 'key': KEY, 'started': 1}]))
    page.route('**/api/jobs/*/events?*', lambda route: route.fulfill(content_type='application/x-ndjson', body=json.dumps(error) + '\n'))
    calls = posts(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('summary-error')).to_contain_text('код 7')
    expect(page.get_by_test_id('summary-retry')).to_be_visible()
    page.reload()
    expect(page.get_by_test_id('summary-error')).to_contain_text('Ошибка контекста')
    assert not calls


def test_delayed_writes_preserve_other_session_and_draft(page, ui_server):
    delayed = []
    def hold_name(route):
        response = route.fetch()
        delayed.append((route, response))
    page.route('**/api/name', hold_name)
    open_main(page, ui_server)
    page.get_by_test_id('rename').click()
    page.get_by_test_id('rename-input').fill('Название A')
    page.get_by_test_id('rename-input').press('Enter')
    page.wait_for_function('document.querySelector("[data-testid=rename-save]").disabled')
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    page.get_by_test_id('rename').click()
    page.get_by_test_id('rename-input').fill('Черновик B')
    wait_pending(page, delayed)
    route, response = delayed.pop()
    route.fulfill(response=response)
    expect(page.locator(f'[data-testid="row"][data-key="{KEY}"]')).to_contain_text('Название A')
    expect(page.get_by_test_id('rename-input')).to_have_value('Черновик B')
    assert CLAUDE not in ui_server.store.state()['names']
    page.get_by_test_id('rename-cancel').click()
    delayed_pins = []
    def hold_pin(route):
        response = route.fetch()
        delayed_pins.append((route, response))
    page.route('**/api/pin', hold_pin)
    page.get_by_test_id('pin').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.get_by_test_id('pin').click()
    wait_pending(page, delayed_pins)
    route, response = delayed_pins.pop()
    route.fulfill(response=response)
    expect(page.get_by_test_id('pinned-section')).to_contain_text('Проверить каталог')
    wait_pending(page, delayed_pins)
    route, response = delayed_pins.pop()
    route.fulfill(response=response)
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-pressed', 'false')
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.get_by_test_id('pin')).to_have_attribute('aria-pressed', 'true')
    assert ui_server.store.state()['pins'] == [CLAUDE]


def test_same_row_summary_and_projects_keyboard(page, ui_server):
    ui_server.store.set_summary(KEY, SUMMARY)
    open_main(page, ui_server)
    page.get_by_test_id('summary-toggle').click()
    expect(page.get_by_test_id('summary-title')).to_be_visible()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('summary-title')).to_have_count(0)
    page.get_by_test_id('summary-toggle').click()
    expect(page.get_by_test_id('summary-title')).to_be_visible()
    page.set_viewport_size({'width': 760, 'height': 900})
    page.get_by_test_id('nav-projects').click()
    page.get_by_test_id('search').fill('нет-такого-проекта')
    page.get_by_test_id('search').press('ArrowDown')
    page.get_by_test_id('search').press('Enter')
    expect(page.get_by_test_id('no-results')).to_be_visible()
    expect(page.get_by_test_id('session')).to_be_hidden()


def test_running_job_is_visible_before_first_event(page, ui_server):
    pending = []
    page.route('**/api/jobs', lambda route: route.fulfill(json=[{'job': 'summary:' + KEY, 'kind': 'summary', 'key': KEY, 'started': 1}]))
    page.route('**/api/jobs/*/events?*', lambda route: pending.append(route))
    calls = posts(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('summary-progress')).to_contain_text('Kimi запускается')
    expect(page.get_by_test_id('summary-run')).to_have_count(0)
    assert not calls
    ui_server.store.set_summary(KEY, SUMMARY)
    # Панель хода видна до запроса событий: дождаться его, а заодно убедиться, что поток один.
    wait_pending(page, pending)
    pending[0].fulfill(content_type='application/x-ndjson', body=json.dumps({'n': 1, 'type': 'result', 'result': SUMMARY}) + '\n')
    expect(page.get_by_test_id('summary-text')).to_have_text(SUMMARY['summary'])


def test_summary_label_follows_model_and_progress_agent(page, ui_server, monkeypatch, save_settings):
    monkeypatch.setenv('AI_THREADS_STUB_MODE', 'slow')
    monkeypatch.setenv('AI_THREADS_SUMMARY_TIMEOUT', '30')
    ui_server.store.set_summary(CLAUDE, {**SUMMARY, 'related': [], 'model': 'claude'})
    open_main(page, ui_server)
    label = page.locator('[data-testid="summary-row"] .ctx-label .tool-icon')
    expect(label).to_have_attribute('title', 'Kimi')
    # Страница ещё ждёт Kimi; кто на самом деле составляет сводку, видно из событий прогресса.
    save_settings({'summary': {'agent': 'codex'}})
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-progress')).to_contain_text('Codex')
    expect(label).to_have_attribute('title', 'Codex')
    expect(page.get_by_test_id('summary-text')).to_be_visible(timeout=20000)
    assert ui_server.store.state()['summaries'][KEY]['model'] == 'codex'
    expect(label).to_have_attribute('title', 'Codex')
    page.get_by_test_id('summary-toggle').click()
    expect(page.locator('[data-testid="summary-row"] .summary-when')).to_contain_text('сводка Codex')
    expect(page.get_by_test_id('closed-hint')).to_contain_text('Codex: похоже, задача закрыта.')
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(label).to_have_attribute('title', 'Claude Code')
    expect(page.get_by_test_id('closed-hint')).to_contain_text('Claude Code: похоже, задача закрыта.')


def test_config_without_runners_and_vscode(page, ui_server):
    def config(route):
        data = route.fetch().json()
        data['runners'] = []
        data['env']['code'] = False
        route.fulfill(json=data)
    page.route('**/api/config', config)
    open_main(page, ui_server, CLAUDE)
    expect(page.get_by_test_id('summary-run')).to_be_disabled()
    expect(page.get_by_test_id('summary-run')).to_have_attribute('title', re.compile('^Нет агента для сводок: установите .*Kimi'))
    expect(page.get_by_test_id('summary-no-runner')).to_contain_text('Нет агента для сводок')
    expect(page.locator('[data-testid="summary-row"] .tool-icon')).to_have_count(0)
    page.get_by_test_id('search').fill('где я проверял каталог')
    expect(page.get_by_test_id('smart-search')).to_be_disabled()
    expect(page.get_by_test_id('smart-search')).to_have_attribute('title', re.compile('^Нет агента для сводок'))
    page.get_by_test_id('search').fill('')
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-explorer')).to_be_visible()
    expect(page.get_by_test_id('menu-vscode')).to_have_count(0)


def test_loading_poll_live_and_no_implicit_open(page, ui_server):
    polls = []
    def sessions(route):
        polls.append(1)
        if len(polls) < 3:
            route.fulfill(json={'ready': False, 'sessions': [], 'port': ui_server.server.server_port,
                                'progress': {'done': 412, 'total': 659, 'tools': {'codex': 'done', 'claude': 'done', 'grok': 'reading', 'kimi': 'waiting'}}})
        else:
            route.continue_()
    page.route('**/api/sessions', sessions)
    status = {KEY: {'state': 'wait', 'where': 'tmux nit:1', 'since': 1}}
    page.route('**/api/live', lambda route: route.fulfill(json=status))
    calls = posts(page)
    page.goto(ui_server.url)
    expect(page.get_by_test_id('loading')).to_contain_text('Читаю журналы сессий')
    expect(page.get_by_test_id('loading-progress')).to_contain_text('412 из 659')
    expect(page.get_by_test_id('row').first).to_be_visible()
    expect(page.get_by_test_id('wait-next')).to_have_text('1 ждёт вас')
    page.get_by_test_id('wait-next').click()
    expect(page.get_by_test_id('live-banner')).to_contain_text('Открывать заново не нужно')
    status[KEY]['state'] = 'work'
    expect(page.get_by_test_id('live-banner')).to_contain_text('Агент работает', timeout=7000)
    assert len(polls) == 3 and not calls


def test_large_feed_chunking_and_calendar_boundaries(page, ui_server):
    from datetime import datetime, timedelta
    from zoneinfo import ZoneInfo
    current = datetime(2026, 9, 28, 0, 5, tzinfo=ZoneInfo('Europe/Samara'))
    page.clock.set_fixed_time(current)
    source = ui_server.catalog.get(KEY).to_json()
    sessions = [{**source, 'key': f'codex:test-{i}', 'id': f'test-{i}', 'title': f'Сессия {i}',
                 'updated': (current - timedelta(minutes=i)).timestamp()} for i in range(1000)]
    page.route('**/api/sessions', lambda route: route.fulfill(json={'ready': True, 'sessions': sessions, 'port': 8794, 'progress': {}}))
    page.route('**/api/session?*', lambda route: route.fulfill(json={'messages': [], 'truncated': False}))
    page.goto(ui_server.url)
    expect(page.get_by_test_id('row')).to_have_count(50)
    expect(page.get_by_test_id('pick-yesterday')).to_contain_text('994')
    page.locator('#feed').evaluate('el => el.scrollTop = el.scrollHeight')
    expect(page.get_by_test_id('row')).to_have_count(100)
    page.get_by_test_id('search').fill('Сессия 999')
    expect(page.get_by_test_id('row')).to_have_count(1)
    page.get_by_test_id('search').fill('')
    page.get_by_test_id('pick-yesterday').click()
    expect(page.get_by_test_id('pick-count')).to_have_text('Выбрано 994')


def test_clipboard_fallback_and_missing_folder(page, ui_server):
    page.add_init_script("navigator.clipboard.writeText = async () => { throw new Error('denied'); }; document.execCommand = command => { window.fallbackCopy = {command, text: document.activeElement.value}; return true; };")
    calls = posts(page)
    open_main(page, ui_server)
    expect(page.get_by_test_id('missing-banner')).to_be_visible()
    page.get_by_test_id('copy-no-cd').click()
    copied = page.evaluate('window.fallbackCopy')
    assert copied == {'command': 'copy', 'text': ui_server.catalog.get(KEY).command_no_cd}
    assert page.locator('textarea').count() == 0
    page.get_by_test_id('session-menu').click()
    expect(page.get_by_test_id('menu-explorer')).to_be_disabled()
    expect(page.get_by_test_id('menu-vscode')).to_be_disabled()
    assert not calls


@pytest.mark.parametrize('operation', ['migrate', 'import'])
def test_summary_result_survives_delayed_state_response(page, ui_server, operation):
    pending = []
    events = []
    page.route('**/api/summary', lambda route: route.fulfill(json={'job': 'summary:' + KEY}))
    page.route('**/api/jobs/*/events?*', lambda route: events.append(route))
    def hold(route):
        pending.append((route, route.fetch()))
    page.route('**/api/' + operation, hold)
    page.add_init_script(f"localStorage.setItem('nit.names', JSON.stringify({{{json.dumps(KEY)}:'Из старой версии'}}));")
    open_main(page, ui_server)
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-progress')).to_be_visible()
    if operation == 'migrate':
        page.get_by_test_id('migrate-button').click()
    else:
        page.get_by_test_id('data-import-file').set_input_files({'name': 'import.json', 'mimeType': 'application/json',
            'buffer': json.dumps({'version': 1, 'pins': [], 'names': {KEY: 'Из импорта'}}).encode()})
    wait_pending(page, pending)
    ui_server.store.set_summary(KEY, SUMMARY)
    events[0].fulfill(content_type='application/x-ndjson', body=json.dumps({'n': 1, 'type': 'result', 'result': SUMMARY}) + '\n')
    route, response = pending[0]
    route.fulfill(response=response)
    expect(page.get_by_test_id('toast')).to_be_visible()
    expect(page.get_by_test_id('summary-text')).to_have_text(SUMMARY['summary'])
    page.reload()
    expect(page.get_by_test_id('summary-text')).to_have_text(SUMMARY['summary'])


def test_reference_screens(page, ui_server):
    if page.context.browser.browser_type.name != 'chromium':
        return
    directory = ROOT / 'test-results/screens'
    directory.mkdir(parents=True, exist_ok=True)
    def shot(name):
        page.evaluate('document.fonts.ready')
        page.screenshot(path=str(directory / f'{name}.png'), animations='disabled')
    ui_server.store.set_name(KEY, 'Каталог сессий')
    ui_server.store.set_summary(KEY, SUMMARY)
    open_main(page, ui_server)
    expect(page.get_by_test_id('message').last).to_be_visible()
    shot('Main')
    page.get_by_test_id('session-menu').click()
    shot('Main-menu')
    page.keyboard.press('Escape')
    page.get_by_test_id('rename').click()
    shot('Main-rename')
    page.get_by_test_id('rename-cancel').click()
    page.get_by_test_id('summary-toggle').click()
    expect(page.get_by_test_id('summary-refresh')).to_be_in_viewport(ratio=1)
    shot('Main-summary-open')
    page.get_by_test_id('summary-toggle').click()
    page.get_by_test_id('search').fill('catalog')
    shot('Main-search')
    page.get_by_test_id('search').fill('абракадабра')
    shot('Main-noresults')
    page.get_by_test_id('search').press('Escape')
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.get_by_test_id('script')).to_contain_text('wt.exe')
    assert page.locator('#feed').evaluate('el => el.scrollWidth <= el.clientWidth')
    shot('Main-restore')
    page.get_by_test_id('fmt-tmux').click()
    expect(page.get_by_test_id('script')).to_contain_text('tmux attach')
    shot('Main-restore-tmux')
    page.keyboard.press('Escape')
    page.locator(f'[data-testid="row"][data-key="{CLAUDE}"]').click()
    expect(page.get_by_test_id('message').last).to_be_visible()
    shot('Main-nosummary')
    page.route('**/api/summary', lambda route: route.fulfill(json={'job': 'summary:' + CLAUDE}))
    events = []
    page.route('**/api/jobs/*/events?*', lambda route: events.append(route))
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-progress')).to_be_visible()
    shot('Main-summary-running')
    events[0].fulfill(content_type='application/x-ndjson', body=json.dumps({'n': 1, 'type': 'error', 'message': 'Kimi завершился с кодом 1', 'code': 1, 'output': 'Превышен лимит контекста'}) + '\n')
    expect(page.get_by_test_id('summary-error')).to_be_visible()
    shot('Main-summary-error')
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    page.set_viewport_size({'width': 760, 'height': 900})
    shot('Narrow-session')
    page.get_by_test_id('back-to-list').click()
    shot('Narrow-list')
    shot('Main-Narrow')
    page.set_viewport_size({'width': 1440, 'height': 900})
    page.route('**/api/sessions', lambda route: route.fulfill(json={'ready': False, 'sessions': [], 'port': ui_server.server.server_port,
        'progress': {'done': 412, 'total': 659, 'tools': {'codex': 'done', 'claude': 'done', 'grok': 'reading', 'kimi': 'waiting'}}}))
    page.reload()
    expect(page.get_by_test_id('loading-progress')).to_contain_text('412 из 659')
    shot('Main-loading')


def test_import_summary_after_model_result_uses_saved_state(page, ui_server):
    pending = []
    events = []
    imported = {**SUMMARY, 'title': 'Из резервной копии', 'summary': 'Импортированная сводка'}
    page.route('**/api/summary', lambda route: route.fulfill(json={'job': 'summary:' + KEY}))
    page.route('**/api/jobs/*/events?*', lambda route: events.append(route))
    page.route('**/api/import', lambda route: pending.append(route))
    open_main(page, ui_server)
    page.get_by_test_id('summary-run').click()
    expect(page.get_by_test_id('summary-progress')).to_be_visible()
    page.get_by_test_id('data-import-file').set_input_files({'name': 'import.json', 'mimeType': 'application/json',
        'buffer': json.dumps({'version': 2, 'summaries': {KEY: imported}}).encode()})
    wait_pending(page, pending)
    ui_server.store.set_summary(KEY, SUMMARY)
    events[0].fulfill(content_type='application/x-ndjson', body=json.dumps({'n': 1, 'type': 'result', 'result': SUMMARY}) + '\n')
    pending[0].fulfill(response=pending[0].fetch())
    expect(page.get_by_test_id('summary-text')).to_have_text(imported['summary'])
    assert ui_server.store.state()['summaries'][KEY]['summary'] == imported['summary']
    page.reload()
    expect(page.get_by_test_id('summary-text')).to_have_text(imported['summary'])


def test_clearing_restore_selection_does_not_copy_old_script(page, ui_server):
    open_main(page, ui_server)
    page.get_by_test_id('pick-enter').click()
    page.locator(f'[data-testid="row"][data-key="{KEY}"]').click()
    expect(page.get_by_test_id('copy-script')).to_be_enabled()
    page.get_by_test_id('copy-script').click()
    assert KEY in page.evaluate('window.copiedText')
    page.get_by_test_id('pick-none').click()
    page.evaluate('window.copiedText = "Буфер не менялся"')
    page.keyboard.press('c')
    assert page.evaluate('window.copiedText') == 'Буфер не менялся'
