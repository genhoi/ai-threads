// Экран «Сводка» за 3–5 дней: готовая сводка, составление, ошибка, пустое состояние.
import {toolIcon, toolName, registerAgents} from './icons.js';

const $ = (selector) => document.querySelector(selector);
const test = (id) => `[data-testid="${id}"]`;
const esc = (value = '') => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const svg = (path, size = 16, color = 'currentColor') =>
  `<svg width="${size}" height="${size}" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="${color}" stroke-width="${size <= 11 ? 3 : size <= 13 ? 2.8 : 1.8}" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;
const iconCheck = (size, color = 'var(--ok)') => svg('<path d="M5 12.5l4.5 4.5L19 7"/>', size, color);
const iconCircle = (size) => svg('<circle cx="12" cy="12" r="8"/>', size);
const iconDash = (size) => svg('<path d="M6 12h12"/>', size);
const spinner = (size = 14) => `<span class="spin" style="width:${size}px;height:${size}px"></span>`;
const button = (id, text, cls = 'btn', extra = '') => `<button type="button" data-testid="${id}" class="${cls}" ${extra}>${text}</button>`;

const STAGES = ['Сбор', 'Чтение журналов', 'Разбор', 'Текст'];
const MONTHS_GEN = ['января','февраля','марта','апреля','мая','июня','июля','августа','сентября','октября','ноября','декабря'];
const HINTS = {ok: 'Взяты готовая сводка или выдержка', read: 'Журнал прочитан', now: 'Модель читает сейчас',
               queue: 'В очереди', skip: 'Пропущена: в журнале нет сообщений', '': 'Открыть сессию'};

const ui = {days: 5, model: '', ready: false, sessions: [], names: {}, hidden: [], tailsDone: {},
            models: [], digests: new Map(), phase: 'boot', error: null, run: null, copied: false, menu: ''};
let toastTimer, tickTimer;

async function api(path, body, options = {}) {
  const response = await fetch(path, {...options, ...(body === undefined ? {} :
    {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)})});
  if (!response.ok) {
    let message = `Ошибка ${response.status}`;
    try { message = (await response.json()).error || message; } catch { /* Ответ может быть без JSON. */ }
    throw new Error(message);
  }
  return options.text ? response.text() : response.json();
}
function toast(message) {
  clearTimeout(toastTimer);
  $('#toast-area').innerHTML = `<div class="toast" data-testid="toast" role="status"><span>${esc(message)}</span></div>`;
  toastTimer = setTimeout(() => { $('#toast-area').innerHTML = ''; }, 7000);
}
async function copyText(text, element) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const area = document.createElement('textarea');
    area.value = text; area.style.cssText = 'position:fixed;left:-9999px';
    document.body.append(area); area.select();
    const copied = document.execCommand('copy'); area.remove();
    if (!copied) throw new Error('Не удалось скопировать. Выделите текст вручную.');
  }
  if (element?.isConnected) {
    ui.copied = true; renderToolbar();
    setTimeout(() => { ui.copied = false; if ($(test('digest-copy'))) renderToolbar(); }, 2400);
  } else toast('Скопировано');
}

function dayKey(d) { return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`; }
function dayLabel(ts) {
  const d = new Date(ts * 1000), today = new Date(), yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return 'Сегодня';
  if (dayKey(d) === dayKey(yesterday)) return 'Вчера';
  return d.toLocaleDateString('ru-RU', {weekday: 'long', day: 'numeric', month: 'short',
    ...(d.getFullYear() !== today.getFullYear() ? {year: 'numeric'} : {})});
}
function timeLabel(ts) { return new Date(ts * 1000).toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'}); }
function fmtElapsed(seconds) {
  seconds = Math.max(0, Math.floor(seconds));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
function rangeLabel(days = ui.days) {
  const end = new Date(); end.setHours(0, 0, 0, 0);
  const start = new Date(end); start.setDate(start.getDate() - (days - 1));
  if (start.getTime() === end.getTime()) return `${end.getDate()} ${MONTHS_GEN[end.getMonth()]}`;
  if (start.getMonth() === end.getMonth() && start.getFullYear() === end.getFullYear())
    return `${start.getDate()}–${end.getDate()} ${MONTHS_GEN[end.getMonth()]}`;
  return `${start.getDate()} ${MONTHS_GEN[start.getMonth()]} — ${end.getDate()} ${MONTHS_GEN[end.getMonth()]}`;
}
function fmtDigestAt(at) {
  const d = new Date(at * 1000), now = new Date();
  const hm = d.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
  const yesterday = new Date(now); yesterday.setDate(now.getDate() - 1);
  if (dayKey(d) === dayKey(now)) return `сегодня, в ${hm}`;
  if (dayKey(d) === dayKey(yesterday)) return `вчера, в ${hm}`;
  return `${d.getDate()} ${MONTHS_GEN[d.getMonth()]}, в ${hm}`;
}
function startOfPeriod() {
  const d = new Date(); d.setHours(0, 0, 0, 0); d.setDate(d.getDate() - (ui.days - 1));
  return d.getTime() / 1000;
}
function sessionName(s) { return ui.names[s.key] || s.title; }
function sessionByKey(key) { return ui.sessions.find(s => s.key === key); }
function periodSessions() {
  const start = startOfPeriod();
  return ui.sessions.filter(s => !s.auto && !s.temp && !ui.hidden.includes(s.key) && s.updated >= start)
    .sort((a, b) => b.updated - a.updated);
}
function periodAutos() {
  const start = startOfPeriod();
  return ui.sessions.filter(s => s.auto && s.updated >= start);
}
function digestForDays() { return ui.digests.get(ui.days) || null; }
function modelAvailable(id) { return ui.models.find(m => m.id === id)?.available; }
function fallbackModel() { return ui.models.find(m => m.id !== ui.model && m.available)?.id || null; }
// Модель по умолчанию: из настроек, если она установлена, иначе выбранная здесь раньше, иначе первая доступная.
function defaultModel(settings) {
  const wanted = settings?.digest?.agent;
  if (wanted && wanted !== 'auto' && modelAvailable(wanted)) return wanted;
  return modelAvailable(ui.model) ? ui.model : ui.models.find(m => m.available)?.id || ui.model;
}
function chip(key, label) {
  const session = sessionByKey(key);
  const tool = session ? session.tool : String(key).split(':')[0];
  const text = label || (session ? sessionName(session) : key);
  return `<a href="/#section=all&session=${encodeURIComponent(key)}&view=session" class="chip-s" title="Открыть сессию">${toolIcon(tool, 14)}<span>${esc(text)}</span></a>`;
}

function shell() {
  $('#app').innerHTML = `<header class="topbar">
    <div class="brand">${svg('<path d="M3 16c3-7 6 1 9-5s6 2 9-4" stroke="var(--accent)"/>', 18)}<span>нить</span></div>
    <nav class="nav" aria-label="Разделы">
      <a href="/#section=mine" data-testid="nav-mine">Мои</a>
      <a href="/#section=all" data-testid="nav-all">Все сессии<span class="n"></span></a>
      <a href="/#section=projects" data-testid="nav-projects">Проекты<span class="n"></span></a>
      <a href="/digest.html" data-testid="nav-digest" class="on" aria-current="page">Сводка</a>
    </nav>
    <div class="grow"></div>
    <span class="addr" data-testid="server-addr">${esc(location.host)}</span>
    <div class="menu-anchor">${button('data-menu', 'Данные', 'btn', 'aria-haspopup="menu" aria-expanded="false"')}<div id="data-popup"></div></div>
    <a href="/settings.html" class="icon-btn" aria-label="Настройки" title="Настройки" data-testid="settings-link"><svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8" fill="none"></circle><path d="M12 2.8v2.6M12 18.6v2.6M21.2 12h-2.6M5.4 12H2.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8M18.5 18.5l-1.8-1.8M7.3 7.3L5.5 5.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path><circle cx="12" cy="12" r="6.2" stroke="currentColor" stroke-width="1.8" fill="none"></circle></svg></a>
    <input type="file" accept="application/json,.json" data-testid="data-import-file" hidden>
  </header>
  <div class="digest-toolbar" id="toolbar"></div>
  <div class="digest-body">
    <main class="digest-main scroll" id="main"></main>
    <aside class="digest-side" id="side" aria-label="По сессиям"></aside>
  </div>
  <div id="toast-area"></div>`;
  renderNavCounts();
  renderMenus();
}
function renderNavCounts() {
  const all = $(test('nav-all') + ' .n'), projects = $(test('nav-projects') + ' .n');
  if (!all) return;
  all.textContent = ui.sessions.length ? ` ${ui.sessions.length}` : '';
  projects.textContent = ui.sessions.length ?
    ` ${new Set(ui.sessions.filter(s => !s.auto && !s.temp && !ui.hidden.includes(s.key)).map(s => s.project)).size}` : '';
}
function renderMenus() {
  $(test('data-menu'))?.setAttribute('aria-expanded', ui.menu === 'data');
  const popup = $('#data-popup');
  if (popup) popup.innerHTML = ui.menu === 'data' ?
    `<div class="menu" role="menu"><a role="menuitem" data-testid="data-export" href="/api/export" download>Выгрузить JSON</a>${button('data-import', 'Загрузить JSON…', '', 'role="menuitem"')}</div>` : '';
}

function metaLine() {
  if (ui.phase === 'running') return 'составляется…';
  if (ui.phase === 'error') return 'не составлена';
  const digest = digestForDays();
  if (!digest) return `сводки за ${ui.days === 5 ? '5 дней' : ui.days + ' дня'} нет`;
  const list = periodSessions();
  const projects = new Set(list.map(s => s.project)).size;
  return `составлена ${fmtDigestAt(digest.at)} · ${toolName(digest.model)} · ${list.length} сессий в ${projects} проектах`;
}
function renderToolbar() {
  const running = ui.phase === 'running';
  const daysButtons = [3, 4, 5].map(d =>
    `<button type="button" data-testid="days-${d}" aria-pressed="${ui.days === d}" ${running ? 'disabled' : ''}>${d === 5 ? '5 дней' : d + ' дня'}</button>`).join('');
  const modelButtons = ui.models.map(m =>
    `<button type="button" data-testid="model-${m.id}" aria-pressed="${ui.model === m.id}" ${!m.available || running ? 'disabled' : ''} title="${esc(m.hint)}">${toolIcon(m.id, 14)}${esc(m.name)}</button>`).join('');
  $('#toolbar').innerHTML = `<h1>Сводка</h1>
    <div class="seg-ctl" role="group" aria-label="Период">${daysButtons}</div>
    <div class="seg-ctl" role="group" aria-label="Кто составляет">${modelButtons}</div>
    <div class="grow"></div>
    <span class="digest-meta" data-testid="digest-meta">${esc(metaLine())}</span>
    ${button('digest-copy', ui.copied ? 'Скопировано' : 'Скопировать текстом', `btn${ui.copied ? ' done' : ''}`, running ? 'disabled' : '')}
    ${button('digest-regen', digestForDays() ? 'Составить заново' : 'Составить', 'btn btn-primary', running ? 'disabled' : '')}`;
}

function stageCaption(index, run) {
  if (index === 0) {
    const manual = periodSessions().filter(s => !s.empty).length;
    return `${manual} сессий · ${periodAutos().length} автозапусков`;
  }
  if (index === 1) {
    const active = run.readDone + run.nowCount;
    if (!active) return run.neededTotal ? `к прочтению: ${run.neededTotal}` : 'журналы читать не пришлось';
    return `прочитано ${run.readDone}${run.nowCount ? ` · читает ${run.nowCount}` : ''}`;
  }
  if (index === 2) return 'проекты и автозапуски';
  return 'главное и задачи';
}
function renderRunning() {
  const run = ui.run, name = toolName(run.model);
  const elapsed = fmtElapsed(Date.now() / 1000 - run.started);
  const stages = STAGES.map((label, i) => {
    const state = i < run.stage ? 'done' : i === run.stage ? 'now' : 'next';
    const mark = state === 'done' ? iconCheck(14) : state === 'now' ? spinner(14) :
      `<span style="font-family:var(--font-mono);font-size:var(--fs-xs);color:var(--text-3)">${i + 1}</span>`;
    return `<div class="stage ${state}"><div class="stage-bar"></div>
      <div style="display:flex;align-items:center;gap:8px;font-size:var(--fs-m);font-weight:500">${mark}${esc(label)}</div>
      <span style="font-size:var(--fs-xs);color:var(--text-3)">${esc(stageCaption(i, run))}</span></div>`;
  }).join('');
  const log = run.logs.length ? run.logs.map((entry, i) => {
    const last = i === run.logs.length - 1;
    const mark = last ? spinner(13) : iconCheck(13);
    return `<li class="${last ? 'now' : 'done'}"><span class="log-t">${entry.t}</span><span class="log-i">${mark}</span><span>${esc(entry.text)}</span></li>`;
  }).join('') : `<li class="next log-empty"><span class="log-t"></span><span class="log-i">${iconCircle(10)}</span><span>Жду первые события модели…</span></li>`;
  const tokens = run.tokens ? `<div class="tokens" data-testid="digest-tokens" title="Сколько контекста модели уже занято материалом">
      <span class="tokens-label">контекст ${Math.round(run.tokens.used / 1000)} тыс. из ${Math.round(run.tokens.limit / 1000)} тыс.</span>
      <div class="tokens-bar"><div style="width:${Math.min(100, run.tokens.used / run.tokens.limit * 100)}%"></div></div></div>` : '';
  $('#main').innerHTML = `<div class="digest-inner" data-testid="digest-running">
    <div class="run-head">
      ${toolIcon(run.model, 22)}
      <div class="run-head-text">
        <span class="run-title">${esc(name)} составляет сводку за ${esc(rangeLabel())}</span>
        <span class="run-sub">Прошло <span class="run-elapsed">${elapsed}</span> · обычно 2–5 минут · прошлая сводка вернётся, если остановить</span>
      </div>
      <div class="grow"></div>
      ${tokens}
      ${button('digest-stop', 'Остановить')}
    </div>
    <div class="stages" aria-label="Этапы" data-testid="digest-stages">${stages}</div>
    <section aria-label="Что делает модель" aria-live="polite">
      <h2 class="h2" style="margin-bottom:6px">Что делает модель</h2>
      <ol class="log" data-testid="digest-log">${log}</ol>
    </section>
    <section aria-label="Заметки по ходу" data-testid="digest-notes">
      <h2 class="h2" style="margin-bottom:6px">Заметки по ходу · ${run.notes.length}</h2>
      ${run.notes.length ? run.notes.map(n => `<div class="note"><span style="font-family:var(--font-mono);font-size:var(--fs-xs);color:var(--text-2);padding-top:2px">${esc(n.project)}</span><span>${esc(n.text)}</span></div>`).join('')
        : '<p class="notes-empty">Появятся, когда модель дочитает первый журнал.</p>'}
    </section>
  </div>`;
}

function digestText(digest) {
  const out = [`Сводка за ${rangeLabel()}`, '', digest.result.lead, ''];
  for (const project of digest.result.projects) {
    out.push(project.name);
    for (const bullet of project.bullets) out.push(`— ${bullet.text}`);
    out.push('');
  }
  out.push('Что осталось сделать');
  for (const tail of digest.result.tails) out.push(`${ui.tailsDone[tail.id] ? '[x]' : '[ ]'} ${tail.text}`);
  return out.join('\n');
}
function renderReady() {
  const digest = digestForDays(), result = digest.result;
  const list = periodSessions();
  const projects = result.projects.map(p => {
    const count = list.filter(s => s.project === p.name).length;
    return `<div class="proj" data-testid="digest-project">
      <div class="proj-side"><span class="proj-name">${esc(p.name)}</span><span class="proj-count">${count} ${count === 1 ? 'сессия' : count < 5 ? 'сессии' : 'сессий'}</span></div>
      <div>${p.bullets.map(b => `<div class="bul"><div class="bul-text">${esc(b.text)}</div>
        ${b.keys.length ? `<div class="chips">${b.keys.map(k => chip(k)).join('')}</div>` : ''}</div>`).join('')}</div>
    </div>`;
  }).join('');
  const tailsLeft = result.tails.filter(t => !ui.tailsDone[t.id]).length;
  const tails = result.tails.map(t => {
    const done = !!ui.tailsDone[t.id];
    const session = sessionByKey(t.key);
    return `<div class="tail ${done ? 'is-done' : ''}" data-testid="digest-tail" data-tail="${esc(t.id)}">
      <input type="checkbox" id="tail-${esc(t.id)}" ${done ? 'checked' : ''}>
      <label for="tail-${esc(t.id)}">${esc(t.text)}</label>
      ${chip(t.key, session ? session.project : '')}
    </div>`;
  }).join('');
  const autos = result.autos.map(a => `<div class="autorow" data-testid="digest-auto">
      ${toolIcon(String(a.key).split(':')[0], 16)}<span class="auto-proj">${esc(a.project)}</span><span>${esc(a.text)}</span>
    </div>`).join('');
  $('#main').innerHTML = `<div class="digest-inner">
    <section aria-label="Главное" data-testid="digest-lead" style="display:flex;flex-direction:column;gap:8px">
      <h2 class="h2">Главное · ${esc(rangeLabel())}</h2>
      <p class="lead">${esc(result.lead)}</p>
    </section>
    ${result.projects.length ? `<section aria-label="По проектам"><h2 class="h2" style="margin-bottom:6px">По проектам</h2>${projects}</section>` : ''}
    ${result.tails.length ? `<section aria-label="Что осталось сделать"><h2 class="h2" style="margin-bottom:6px">Что осталось сделать · ${tailsLeft} из ${result.tails.length}</h2>${tails}</section>` : ''}
    <section aria-label="Автоматические запуски" style="display:flex;flex-direction:column;gap:8px">
      <h2 class="h2">Автоматические запуски · ${result.autos.length}</h2>
      <p class="autos-intro">Другие агенты запускали CLI для ревью и независимых проверок. В списке сессий они скрыты; здесь только то, где нашлось что-то важное.</p>
      ${autos || '<p class="autos-empty">Важного не нашлось.</p>'}
    </section>
  </div>`;
}

function renderError() {
  const error = ui.error, fallback = fallbackModel();
  $('#main').innerHTML = `<div class="digest-inner">
    <div class="digest-error" data-testid="digest-error" role="alert">
      <span class="error-title">Сводка не получилась</span>
      <span class="error-text">${esc(error.message)}${error.code !== undefined && error.code !== null ? ` · код ${esc(error.code)}` : ''}</span>
      ${error.output ? `<pre class="error-output">${esc(error.output)}</pre>` : ''}
      <div class="error-actions">${button('digest-retry', 'Повторить')}
        ${fallback ? button('digest-fallback', `Составить через ${esc(toolName(fallback))}`) : ''}</div>
    </div>
  </div>`;
}

function renderEmpty() {
  const manual = periodSessions().length, autos = periodAutos().length;
  $('#main').innerHTML = `<div class="digest-inner">
    <div class="empty-card" data-testid="digest-empty">
      <span class="empty-title">Сводки за ${esc(rangeLabel())} ещё нет</span>
      <p class="empty-text">Составлю из ${manual} ваших сессий и ${autos} автоматических запусков за период. Обычно это 2–5 минут.</p>
      <p class="empty-note">Модель разберёт журналы и выделит главное, итоги по проектам и то, что осталось сделать.</p>
      ${button('digest-run', 'Составить', 'btn btn-primary')}
    </div>
  </div>`;
}

function renderMain() {
  if (ui.phase === 'running') renderRunning();
  else if (ui.phase === 'error') renderError();
  else if (ui.phase === 'ready') renderReady();
  else if (ui.phase === 'empty') renderEmpty();
  else $('#main').innerHTML = '<div class="digest-inner"><div class="side-loading">Читаю данные…</div></div>';
}

function renderSide() {
  const running = ui.phase === 'running';
  const list = periodSessions();
  let html = '', lastDay = null;
  for (const s of list) {
    const label = dayLabel(s.updated);
    if (label !== lastDay) { html += `<div class="day">${esc(label)}</div>`; lastDay = label; }
    const status = running && ui.run ? ui.run.statuses.get(s.key) || 'queue' : '';
    const cls = `srow${status === 'queue' ? ' queue' : ''}${status === 'now' ? ' now' : ''}`;
    const mark = status === 'ok' ? iconCheck(13) : status === 'read' ? iconCheck(13, 'var(--accent)') :
      status === 'now' ? spinner(13) : status === 'queue' ? iconCircle(10) : status === 'skip' ? iconDash(12) : '';
    html += `<a href="/#section=all&session=${encodeURIComponent(s.key)}&view=session" class="${cls}" data-testid="digest-session"
      data-key="${esc(s.key)}" data-status="${status}" title="${esc(HINTS[status] || HINTS[''])}">
      <span class="srow-time">${timeLabel(s.updated)}</span>${toolIcon(s.tool, 18)}
      <span class="srow-main"><span class="srow-title"><span class="srow-proj">${esc(s.project)}</span> · ${esc(sessionName(s))}</span>
      ${s.last ? `<span class="srow-res">${esc(s.last)}</span>` : ''}</span>
      <span class="srow-st">${mark}</span></a>`;
  }
  if (!ui.ready) html = '<div class="side-loading">Читаю журналы сессий…</div>';
  else if (!list.length) html = '<div class="side-loading">В этом периоде ваших сессий нет.</div>';
  const legend = running ? `<div class="side-legend">
      <span>${iconCheck(11)}готовая сводка</span><span>${iconCheck(11, 'var(--accent)')}прочитан журнал</span>
      <span>${spinner(11)}читает</span><span>${iconCircle(9)}в очереди</span><span>${iconDash(11)}пропущена</span>
    </div>` : '';
  $('#side').innerHTML = `<div class="side-head"><span class="side-title">По сессиям</span>
      <span class="side-note">${list.length} за ${ui.days === 5 ? '5 дней' : ui.days + ' дня'}${running ? ' · что уже учтено' : ' · одна строка на сессию'}</span></div>
    <div class="side-list scroll" data-testid="digest-sessions">${html}</div>${legend}`;
}

function render() { renderToolbar(); renderMain(); renderSide(); }

function readPrefs() {
  const days = parseInt(new URLSearchParams(location.hash.slice(1)).get('days') || '', 10);
  if ([3, 4, 5].includes(days)) ui.days = days;
  const model = localStorage.getItem('nit.digest.model');
  if (model) ui.model = model;
}
function writePrefs() {
  const params = new URLSearchParams(location.hash.slice(1));
  params.set('days', String(ui.days));
  const hash = `#${params}`;
  if (location.hash !== hash) history.replaceState(null, '', hash || location.pathname);
  localStorage.setItem('nit.digest.model', ui.model);
}

async function refreshDigest() {
  const digest = await api(`/api/digest?days=${ui.days}`);
  ui.digests.set(ui.days, digest);
  return digest;
}
function finishRun() {
  ui.run = null;
  ui.phase = digestForDays() ? 'ready' : 'empty';
  render();
}

function handleEvent(run, event) {
  run.since = event.n;
  switch (event.type) {
    case 'stage':
      run.stage = Math.max(run.stage, event.index);
      if (event.agent && event.agent !== ui.model) ui.model = event.agent;
      break;
    case 'log':
      run.logs.push({t: fmtElapsed(event.elapsed || 0), text: event.text});
      if (!run.initLog) run.initLog = event.text;
      break;
    case 'note':
      run.notes.push({project: event.project, text: event.text});
      break;
    case 'tokens':
      run.tokens = {used: event.used, limit: event.limit};
      break;
    case 'session': {
      const prev = run.statuses.get(event.key);
      if (prev === undefined && event.status === 'queued') run.neededTotal++;
      if (event.status === 'now' && prev !== 'now') run.nowCount++;
      if (event.status === 'read') { if (prev === 'now') run.nowCount--; run.readDone++; }
      run.statuses.set(event.key, event.status);
      break;
    }
    case 'result':
      run.finished = true;
      refreshDigest()
        .then(() => { ui.run = null; ui.phase = digestForDays() ? 'ready' : 'empty'; render(); })
        .catch(() => finishRun());
      break;
    case 'error':
      run.finished = true;
      ui.error = {message: event.message, code: event.code, output: event.output};
      ui.phase = 'error';
      break;
  }
  if (!run.finished) render();
}

async function watchDigest(jobId, since) {
  const run = ui.run;
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/events?since=${since}`);
    if (!response.ok) throw new Error(`Не удалось подключиться к сводке (${response.status})`);
    const reader = response.body.getReader(), decoder = new TextDecoder();
    let buffer = '';
    while (true) {
      const {value, done} = await reader.read();
      buffer += decoder.decode(value, {stream: !done});
      const lines = buffer.split('\n'); buffer = lines.pop();
      for (const line of lines) if (line.trim()) handleEvent(run, JSON.parse(line));
      if (done) {
        if (buffer.trim()) handleEvent(run, JSON.parse(buffer));
        break;
      }
    }
  } catch (error) {
    if (ui.run === run && !run.finished && !run.cancelRequested) {
      ui.error = {message: error.message};
      ui.phase = 'error';
    }
  } finally {
    if (ui.run === run) ui.run = null;
    if (ui.phase !== 'error') finishRun();
    else render();
  }
}

async function startDigest(model = ui.model) {
  if (ui.phase === 'running') return;
  writePrefs();
  ui.run = {model, started: Date.now() / 1000, stage: 0, logs: [], notes: [], tokens: null,
            statuses: new Map(), neededTotal: 0, readDone: 0, nowCount: 0, since: 0, cancelRequested: false, finished: false};
  ui.phase = 'running';
  ui.error = null;
  render();
  try {
    const {job} = await api('/api/digest', {days: ui.days, model});
    watchDigest(job, 0);
  } catch (error) {
    ui.run = null;
    ui.phase = digestForDays() ? 'ready' : 'empty';
    render();
    toast(error.message);
  }
}

async function stopDigest() {
  const run = ui.run;
  if (!run) return;
  run.cancelRequested = true;
  try { await api('/api/jobs/digest/cancel', {}); } catch { /* Конец потока всё равно вернёт прошлую сводку. */ }
}

async function resumeDigest() {
  let job;
  try { job = (await api('/api/jobs')).find(j => j.kind === 'digest'); }
  catch { return false; }
  if (!job) return false;
  ui.run = {model: ui.model, started: job.started, stage: 0, logs: [], notes: [], tokens: null,
            statuses: new Map(), neededTotal: 0, readDone: 0, nowCount: 0, since: 0, cancelRequested: false, finished: false};
  ui.phase = 'running';
  render();
  watchDigest(job.job, 0);
  return true;
}

async function setDays(days) {
  if (ui.phase === 'running' || ui.days === days) return;
  ui.days = days;
  writePrefs();
  ui.error = null;
  if (!ui.digests.has(ui.days)) {
    try { await refreshDigest(); } catch { /* Повторный запрос при отрисовке. */ }
  }
  ui.phase = digestForDays() ? 'ready' : 'empty';
  render();
}
function setModel(model) {
  if (ui.phase === 'running' || ui.model === model || !modelAvailable(model)) return;
  ui.model = model;
  writePrefs();
  render();
}

async function toggleTail(id, checkbox) {
  const done = checkbox.checked;
  checkbox.disabled = true;
  try {
    const state = await api('/api/digest/tail', {id, done});
    ui.tailsDone = state.digest_tails || ui.tailsDone;
  } catch (error) {
    checkbox.checked = !done;
    toast(error.message);
  }
  checkbox.disabled = false;
  renderMain();
}

async function action(id, element) {
  if (id.startsWith('model-')) { setModel(id.slice('model-'.length)); return; }
  switch (id) {
    case 'days-3': await setDays(3); break;
    case 'days-4': await setDays(4); break;
    case 'days-5': await setDays(5); break;
    case 'digest-run': case 'digest-regen': await startDigest(); break;
    case 'digest-retry': await startDigest(ui.model); break;
    case 'digest-fallback': {
      const fallback = fallbackModel();
      if (fallback) await startDigest(fallback);
      break;
    }
    case 'digest-stop': await stopDigest(); break;
    case 'digest-copy': {
      const digest = digestForDays();
      if (digest) await copyText(digestText(digest), element);
      break;
    }
    case 'data-menu': ui.menu = ui.menu === 'data' ? '' : 'data'; renderMenus(); break;
    case 'data-import': ui.menu = ''; renderMenus(); $(test('data-import-file')).click(); break;
    case 'data-import-file': break;
  }
}
async function safely(id, element) {
  if (element?.disabled) return;
  if (element?.tagName === 'INPUT') return;
  if (element?.tagName === 'BUTTON') element.disabled = true;
  try { await action(id, element); }
  catch (error) { toast(error.message); }
  finally { if (element?.isConnected && element.tagName === 'BUTTON') element.disabled = false; }
}

function bind() {
  document.addEventListener('click', event => {
    if (ui.menu && !event.target.closest('.menu,.menu-anchor')) { ui.menu = ''; renderMenus(); }
    // ссылки (навигация, чипы, выгрузка) и поля ввода работают по умолчанию
    if (event.target.closest('a') || event.target.matches('input,textarea')) return;
    const element = event.target.closest('[data-testid]'), id = element?.dataset.testid;
    if (!element) return;
    event.preventDefault();
    safely(id, element);
  });
  document.addEventListener('change', event => {
    const tail = event.target.closest('[data-testid="digest-tail"] input');
    if (tail) toggleTail(event.target.closest('[data-tail]').dataset.tail, event.target);
  });
  $(test('data-import-file')).addEventListener('change', async event => {
    const file = event.target.files[0];
    if (!file) return;
    try {
      const state = await api('/api/import', {data: JSON.parse(await file.text())});
      ui.names = state.names || ui.names;
      ui.hidden = state.hidden || ui.hidden;
      ui.tailsDone = state.digest_tails || ui.tailsDone;
      toast('Данные загружены');
      renderNavCounts();
      render();
    } catch (error) { toast(`Не удалось загрузить JSON: ${error.message}`); }
    event.target.value = '';
  });
}

async function init() {
  readPrefs();
  shell();
  bind();
  render();
  tickTimer = setInterval(() => {
    if (ui.phase === 'running' && ui.run) {
      const elapsed = $(test('digest-running'))?.querySelector('.run-elapsed');
      if (elapsed) elapsed.textContent = fmtElapsed(Date.now() / 1000 - ui.run.started);
    }
  }, 1000);
  setInterval(async () => {
    if (ui.phase === 'running') return;
    try { if ((await api('/api/jobs')).some(j => j.kind === 'digest')) await resumeDigest(); } catch { /* следующий опрос */ }
  }, 5000);
  try {
    const [state, models, jobs, config] = await Promise.all([api('/api/state'), api('/api/digest/models'), api('/api/jobs'), api('/api/config')]);
    registerAgents(config.agents);
    ui.names = state.names || {};
    ui.hidden = state.hidden || [];
    ui.tailsDone = state.digest_tails || {};
    ui.models = models;
    ui.model = defaultModel(config.settings);
    if (jobs.some(j => j.kind === 'digest')) await resumeDigest();
    await refreshDigest();
    if (ui.phase !== 'running') ui.phase = digestForDays() ? 'ready' : 'empty';
    render();
    while (!ui.ready) {
      const feed = await api('/api/sessions');
      ui.ready = feed.ready;
      ui.sessions = feed.sessions;
      if (!ui.ready) { await new Promise(resolve => setTimeout(resolve, 300)); }
    }
    renderNavCounts();
    render();
  } catch (error) {
    $('#main').innerHTML = `<div class="digest-inner"><div class="loading-view" role="alert"><div class="loading-card">Не удалось прочитать данные: ${esc(error.message)}<a class="btn" href="${esc(location.href)}">Повторить</a></div></div></div>`;
  }
}
init();
