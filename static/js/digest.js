// Экран «Сводка» за 3–5 дней: готовая сводка, составление, ошибка, пустое состояние,
// журнал агента с ответами ему и выбор модели и уровня рассуждений.
import {toolIcon, toolName, registerAgents} from './icons.js';
import {addStrings, setLanguage, lang, locale, t} from './i18n.js';
import strings from './lang/digest.js';

addStrings(strings);

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

const STAGES = ['collect', 'read', 'parse', 'write'];
// Подсказка у строки сессии: что с ней сделала модель; без известного статуса — «Открыть сессию».
const STATUSES = ['ok', 'read', 'now', 'queue', 'skip'];
const hint = (status) => STATUSES.includes(status) ? t(`status.${status}`) : t('openSession');

const ui = {days: 5, model: '', ready: false, sessions: [], names: {}, hidden: [], tailsDone: {},
            models: [], digests: new Map(), phase: 'boot', error: null, run: null, copied: false, menu: '',
            choice: {}, agentModels: {}, log: null, agentView: false};
let toastTimer, tickTimer;

async function api(path, body, options = {}) {
  const response = await fetch(path, {...options, ...(body === undefined ? {} :
    {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)})});
  if (!response.ok) {
    let message = t('error.status', {status: response.status});
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
async function writeClipboard(text) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const area = document.createElement('textarea');
    area.value = text; area.style.cssText = 'position:fixed;left:-9999px';
    document.body.append(area); area.select();
    const copied = document.execCommand('copy'); area.remove();
    if (!copied) throw new Error(t('copyFailed'));
  }
}
// localStorage бывает недоступен (приватное окно, запрет сайта): тогда выбор просто не запоминается.
function readStored(key, fallback) {
  try { return JSON.parse(localStorage.getItem(key)) ?? fallback; } catch { return fallback; }
}
function writeStored(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* не запомнится */ }
}
async function copyText(text, element) {
  await writeClipboard(text);
  if (element?.isConnected) {
    ui.copied = true; renderToolbar();
    setTimeout(() => { ui.copied = false; if ($(test('digest-copy'))) renderToolbar(); }, 2400);
  } else toast(t('copied'));
}

function dayKey(d) { return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`; }
function dayLabel(ts) {
  const d = new Date(ts * 1000), today = new Date(), yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return t('today');
  if (dayKey(d) === dayKey(yesterday)) return t('yesterday');
  return d.toLocaleDateString(locale(), {weekday: 'long', day: 'numeric', month: 'short',
    ...(d.getFullYear() !== today.getFullYear() ? {year: 'numeric'} : {})});
}
// Время: по-русски «07:30», по-английски «7:30 AM».
function clock(d) { return d.toLocaleTimeString(locale(), {hour: lang() === 'ru' ? '2-digit' : 'numeric', minute: '2-digit'}); }
function timeLabel(ts) { return clock(new Date(ts * 1000)); }
// Месяц в той форме, в какой он стоит рядом с числом: «сентября», «September».
function monthName(d) {
  return new Intl.DateTimeFormat(locale(), {day: 'numeric', month: 'long'}).formatToParts(d).find(p => p.type === 'month').value;
}
function dayMonth(d) { return t('month.day', {day: d.getDate(), month: monthName(d)}); }
function fmtElapsed(seconds) {
  seconds = Math.max(0, Math.floor(seconds));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
function rangeLabel(days = ui.days) {
  const end = new Date(); end.setHours(0, 0, 0, 0);
  const start = new Date(end); start.setDate(start.getDate() - (days - 1));
  if (start.getTime() === end.getTime()) return dayMonth(end);
  if (start.getMonth() === end.getMonth() && start.getFullYear() === end.getFullYear())
    return t('month.range', {start: start.getDate(), end: end.getDate(), month: monthName(end)});
  return t('month.span', {start: dayMonth(start), end: dayMonth(end)});
}
function fmtDigestAt(at) {
  const d = new Date(at * 1000), now = new Date(), time = clock(d);
  const yesterday = new Date(now); yesterday.setDate(now.getDate() - 1);
  if (dayKey(d) === dayKey(now)) return t('at.today', {time});
  if (dayKey(d) === dayKey(yesterday)) return t('at.yesterday', {time});
  return t('at.date', {date: dayMonth(d), time});
}
const daysLabel = (n = ui.days) => t('days', {n});
function startOfPeriod() {
  const d = new Date(); d.setHours(0, 0, 0, 0); d.setDate(d.getDate() - (ui.days - 1));
  return d.getTime() / 1000;
}
function sessionName(s) { return ui.names[s.key] || s.title || t('untitled'); }
function projectName(s) { return s.project || t('noProject'); }
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
  return `<a href="/#section=all&session=${encodeURIComponent(key)}&view=session" class="chip-s" title="${esc(t('openSession'))}">${toolIcon(tool, 14)}<span>${esc(text)}</span></a>`;
}

// --- модель и уровень рассуждений -------------------------------------------
// Списки отдаёт CLI агента через /api/models; пустое значение — «как в CLI», его не передаём.
// Выбор запоминается для каждого агента: {kimi: {model, effort}, ...}.
const CHOICE_KEY = 'nit.digest.choice';
const NO_MODELS = {default_model: '', default_effort: '', models: [], efforts: []};
const modelRequests = new Map();
function loadAgentModels(agent) {
  if (!modelRequests.has(agent)) {
    modelRequests.set(agent, api(`/api/models?agent=${encodeURIComponent(agent)}`).catch(() => NO_MODELS).then(info => {
      ui.agentModels[agent] = info;
      if (agent === ui.model) renderToolbar();
      return info;
    }));
  }
  return modelRequests.get(agent);
}
// Уровни рассуждений: свои у модели (Codex) или общие у агента (Claude Code, Grok); у Kimi их нет.
function effortLevels(info, model) {
  const entry = info.models.find(m => m.id === (model || info.default_model));
  return entry?.efforts?.length ? entry.efforts : info.efforts || [];
}
// Запомненный выбор, проверенный по списку CLI: пропавшие модель или уровень — снова «как в CLI».
function choiceFor(agent) {
  const saved = ui.choice[agent] || {}, info = ui.agentModels[agent];
  if (!info) return {model: '', effort: ''};
  const model = info.models.some(m => m.id === saved.model) ? saved.model : '';
  return {model, effort: effortLevels(info, model).includes(saved.effort) ? saved.effort : ''};
}
function setChoice(field, value) {
  // У новой модели прежнего уровня может не быть: повторная проверка сбросит его на «как в CLI».
  ui.choice[ui.model] = {...choiceFor(ui.model), [field]: value};
  ui.choice[ui.model] = choiceFor(ui.model);
  writeStored(CHOICE_KEY, ui.choice);
  renderToolbar();
}
function choiceControls(running) {
  const info = ui.agentModels[ui.model], {model, effort} = choiceFor(ui.model);
  const cli = (value) => value ? t('choice.cliValue', {value}) : t('choice.cli');
  const option = (value, label, selected) => `<option value="${esc(value)}"${selected ? ' selected' : ''}>${esc(label)}</option>`;
  const off = running || !info ? ' disabled' : '';
  const models = [option('', cli(info?.default_model), !model), ...(info?.models || []).map(m => option(m.id, m.name, m.id === model))];
  const select = (id, label, options) =>
    `<select class="pick" data-testid="${id}" aria-label="${t(label)}" title="${t(label)}"${off}>${options.join('')}</select>`;
  const levels = info ? effortLevels(info, model) : [];
  const effortDefault = info && (info.default_effort || info.models.find(m => m.id === (model || info.default_model))?.default_effort);
  return select('digest-agent-model', 'choice.model', models) +
    (levels.length ? select('digest-effort', 'choice.effort', [option('', cli(effortDefault), !effort), ...levels.map(l => option(l, l, l === effort))]) : '');
}

function shell() {
  $('#app').innerHTML = `<header class="topbar">
    <div class="brand">${svg('<path d="M3 16c3-7 6 1 9-5s6 2 9-4" stroke="var(--accent)"/>', 18)}<span>${t('brand')}</span></div>
    <nav class="nav" aria-label="${t('nav.label')}">
      <a href="/#section=mine" data-testid="nav-mine">${t('nav.mine')}</a>
      <a href="/#section=all" data-testid="nav-all">${t('nav.all')}<span class="n"></span></a>
      <a href="/#section=projects" data-testid="nav-projects">${t('nav.projects')}<span class="n"></span></a>
      <a href="/digest.html" data-testid="nav-digest" class="on" aria-current="page">${t('nav.digest')}</a>
    </nav>
    <div class="grow"></div>
    <span class="addr" data-testid="server-addr">${esc(location.host)}</span>
    <div class="menu-anchor">${button('data-menu', t('data.menu'), 'btn', 'aria-haspopup="menu" aria-expanded="false"')}<div id="data-popup"></div></div>
    <a href="/settings.html" class="icon-btn" aria-label="${t('settings')}" title="${t('settings')}" data-testid="settings-link"><svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8" fill="none"></circle><path d="M12 2.8v2.6M12 18.6v2.6M21.2 12h-2.6M5.4 12H2.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8M18.5 18.5l-1.8-1.8M7.3 7.3L5.5 5.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path><circle cx="12" cy="12" r="6.2" stroke="currentColor" stroke-width="1.8" fill="none"></circle></svg></a>
    <input type="file" accept="application/json,.json" data-testid="data-import-file" hidden>
  </header>
  <div class="digest-toolbar" id="toolbar"></div>
  <div class="digest-body">
    <main class="digest-main scroll" id="main"></main>
    <aside class="digest-side" id="side" aria-label="${t('side.title')}"></aside>
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
    `<div class="menu" role="menu"><a role="menuitem" data-testid="data-export" href="/api/export" download>${t('data.export')}</a>${button('data-import', t('data.import'), '', 'role="menuitem"')}</div>` : '';
}

function metaLine() {
  if (ui.phase === 'running') return t('meta.running');
  if (ui.phase === 'error') return t('meta.error');
  const digest = digestForDays();
  if (!digest) return t('meta.none', {days: daysLabel()});
  const list = periodSessions();
  const projects = new Set(list.map(s => s.project)).size;
  return t('meta.ready', {when: fmtDigestAt(digest.at), model: toolName(digest.model), sessions: list.length, projects});
}
function renderToolbar() {
  const running = ui.phase === 'running';
  // Перерисовка не должна уводить фокус с кнопки или списка, которым только что пользовались.
  const focused = $('#toolbar').contains(document.activeElement) ? document.activeElement.dataset.testid : '';
  const daysButtons = [3, 4, 5].map(d =>
    `<button type="button" data-testid="days-${d}" aria-pressed="${ui.days === d}" ${running ? 'disabled' : ''}>${esc(daysLabel(d))}</button>`).join('');
  const modelButtons = ui.models.map(m =>
    `<button type="button" data-testid="model-${m.id}" aria-pressed="${ui.model === m.id}" ${!m.available || running ? 'disabled' : ''} title="${esc(m.hint)}">${toolIcon(m.id, 14)}${esc(m.name)}</button>`).join('');
  $('#toolbar').innerHTML = `<h1>${t('toolbar.title')}</h1>
    <div class="seg-ctl" role="group" aria-label="${t('toolbar.period')}">${daysButtons}</div>
    <div class="seg-ctl" role="group" aria-label="${t('toolbar.model')}">${modelButtons}</div>
    <div class="digest-choice">${choiceControls(running)}</div>
    <div class="grow"></div>
    <span class="digest-meta" data-testid="digest-meta">${esc(metaLine())}</span>
    ${button('digest-copy', ui.copied ? t('copied') : t('copy'), `btn${ui.copied ? ' done' : ''}`, running ? 'disabled' : '')}
    ${button('digest-regen', digestForDays() ? t('regenerate') : t('generate'), 'btn btn-primary', running ? 'disabled' : '')}`;
  if (focused) $('#toolbar ' + test(focused))?.focus();
}

function stageCaption(index, run) {
  if (index === 0) {
    const manual = periodSessions().filter(s => !s.empty).length;
    return t('stage.collectNote', {sessions: manual, autos: periodAutos().length});
  }
  if (index === 1) {
    const active = run.readDone + run.nowCount;
    if (!active) return run.neededTotal ? t('stage.toRead', {n: run.neededTotal}) : t('stage.nothingToRead');
    return t('stage.readDone', {n: run.readDone}) + (run.nowCount ? t('stage.reading', {n: run.nowCount}) : '');
  }
  if (index === 2) return t('stage.parseNote');
  return t('stage.writeNote');
}
function renderRunning() {
  const run = ui.run, name = toolName(run.model);
  const elapsed = fmtElapsed(Date.now() / 1000 - run.started);
  const stages = STAGES.map((stage, i) => {
    const label = t(`stage.${stage}`);
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
  }).join('') : `<li class="next log-empty"><span class="log-t"></span><span class="log-i">${iconCircle(10)}</span><span>${t('run.logEmpty')}</span></li>`;
  const tokens = run.tokens ? `<div class="tokens" data-testid="digest-tokens" title="${esc(t('run.tokensHint'))}">
      <span class="tokens-label">${esc(t('run.tokens', {used: Math.round(run.tokens.used / 1000), limit: Math.round(run.tokens.limit / 1000)}))}</span>
      <div class="tokens-bar"><div style="width:${Math.min(100, run.tokens.used / run.tokens.limit * 100)}%"></div></div></div>` : '';
  $('#main').innerHTML = `<div class="digest-inner" data-testid="digest-running">
    <div class="run-head">
      ${toolIcon(run.model, 22)}
      <div class="run-head-text">
        <span class="run-title">${esc(t('run.title', {model: name, range: rangeLabel()}))}</span>
        <span class="run-sub">${t('run.elapsed')} <span class="run-elapsed">${elapsed}</span> · ${esc(t('run.note'))}</span>
      </div>
      <div class="grow"></div>
      ${tokens}
      ${logButton()}
      ${button('digest-stop', t('run.stop'))}
    </div>
    <div class="stages" aria-label="${t('run.stages')}" data-testid="digest-stages">${stages}</div>
    <section aria-label="${t('run.log')}" aria-live="polite">
      <h2 class="h2" style="margin-bottom:6px">${t('run.log')}</h2>
      <ol class="log" data-testid="digest-log">${log}</ol>
    </section>
    <section aria-label="${t('run.notes')}" data-testid="digest-notes">
      <h2 class="h2" style="margin-bottom:6px">${t('run.notes')} · ${run.notes.length}</h2>
      ${run.notes.length ? run.notes.map(n => `<div class="note"><span style="font-family:var(--font-mono);font-size:var(--fs-xs);color:var(--text-2);padding-top:2px">${esc(n.project)}</span><span>${esc(n.text)}</span></div>`).join('')
        : `<p class="notes-empty">${t('run.notesEmpty')}</p>`}
    </section>
  </div>`;
}

function digestText(digest) {
  const out = [t('text.title', {range: rangeLabel()}), '', digest.result.lead, ''];
  for (const project of digest.result.projects) {
    out.push(project.name);
    for (const bullet of project.bullets) out.push(`— ${bullet.text}`);
    out.push('');
  }
  out.push(t('ready.tails'));
  for (const tail of digest.result.tails) out.push(`${ui.tailsDone[tail.id] ? '[x]' : '[ ]'} ${tail.text}`);
  return out.join('\n');
}
function renderReady() {
  const digest = digestForDays(), result = digest.result;
  const list = periodSessions();
  const projects = result.projects.map(p => {
    const count = list.filter(s => s.project === p.name).length;
    return `<div class="proj" data-testid="digest-project">
      <div class="proj-side"><span class="proj-name">${esc(p.name)}</span><span class="proj-count">${esc(t('ready.sessions', {n: count}))}</span></div>
      <div>${p.bullets.map(b => `<div class="bul"><div class="bul-text">${esc(b.text)}</div>
        ${b.keys.length ? `<div class="chips">${b.keys.map(k => chip(k)).join('')}</div>` : ''}</div>`).join('')}</div>
    </div>`;
  }).join('');
  const tailsLeft = result.tails.filter(tail => !ui.tailsDone[tail.id]).length;
  const tails = result.tails.map(tail => {
    const done = !!ui.tailsDone[tail.id];
    const session = sessionByKey(tail.key);
    return `<div class="tail ${done ? 'is-done' : ''}" data-testid="digest-tail" data-tail="${esc(tail.id)}">
      <input type="checkbox" id="tail-${esc(tail.id)}" ${done ? 'checked' : ''}>
      <label for="tail-${esc(tail.id)}">${esc(tail.text)}</label>
      ${chip(tail.key, session ? projectName(session) : '')}
    </div>`;
  }).join('');
  const autos = result.autos.map(a => `<div class="autorow" data-testid="digest-auto">
      ${toolIcon(String(a.key).split(':')[0], 16)}<span class="auto-proj">${esc(a.project)}</span><span>${esc(a.text)}</span>
    </div>`).join('');
  $('#main').innerHTML = `<div class="digest-inner">
    <section aria-label="${t('ready.lead')}" data-testid="digest-lead" style="display:flex;flex-direction:column;gap:8px">
      <div class="lead-head"><h2 class="h2">${t('ready.lead')} · ${esc(rangeLabel())}</h2>${logButton('btn sm')}</div>
      <p class="lead">${esc(result.lead)}</p>
    </section>
    ${result.projects.length ? `<section aria-label="${t('ready.projects')}"><h2 class="h2" style="margin-bottom:6px">${t('ready.projects')}</h2>${projects}</section>` : ''}
    ${result.tails.length ? `<section aria-label="${t('ready.tails')}"><h2 class="h2" style="margin-bottom:6px">${t('ready.tails')} · ${t('ready.tailsLeft', {left: tailsLeft, total: result.tails.length})}</h2>${tails}</section>` : ''}
    <section aria-label="${t('ready.autos')}" style="display:flex;flex-direction:column;gap:8px">
      <h2 class="h2">${t('ready.autos')} · ${result.autos.length}</h2>
      <p class="autos-intro">${t('ready.autosIntro')}</p>
      ${autos || `<p class="autos-empty">${t('ready.autosEmpty')}</p>`}
    </section>
  </div>`;
}

function renderError() {
  const error = ui.error, fallback = fallbackModel();
  $('#main').innerHTML = `<div class="digest-inner">
    <div class="digest-error" data-testid="digest-error" role="alert">
      <span class="error-title">${t('error.title')}</span>
      <span class="error-text">${esc(error.message)}${error.code !== undefined && error.code !== null ? ` · ${esc(t('error.code', {code: error.code}))}` : ''}</span>
      ${error.output ? `<pre class="error-output">${esc(error.output)}</pre>` : ''}
      <div class="error-actions">${button('digest-retry', t('retry'))}${logButton()}
        ${fallback ? button('digest-fallback', esc(t('error.fallback', {model: toolName(fallback)}))) : ''}</div>
    </div>
  </div>`;
}

function renderEmpty() {
  const manual = periodSessions().length, autos = periodAutos().length;
  $('#main').innerHTML = `<div class="digest-inner">
    <div class="empty-card" data-testid="digest-empty">
      <span class="empty-title">${esc(t('empty.title', {range: rangeLabel()}))}</span>
      <p class="empty-text">${esc(t('empty.text', {sessions: manual, autos}))}</p>
      <p class="empty-note">${t('empty.note')}</p>
      ${button('digest-run', t('generate'), 'btn btn-primary')}
    </div>
  </div>`;
}

function renderMain() {
  // Журнал агента на месте сводки. Другой период или новый запуск без журнала — снова сводка.
  if (ui.agentView && !logAvailable()) ui.agentView = false;
  if (ui.agentView) {
    if (shown.log === ui.log && $('#agent-entries')) updateAgentView(); else renderAgentView();
    return;
  }
  $('#main').classList.remove('is-agent');
  // Ход составления перерисовывается на каждое событие: кнопка в фокусе должна в нём остаться.
  const focused = $('#main').contains(document.activeElement) ? document.activeElement.dataset.testid : '';
  if (ui.phase === 'running') renderRunning();
  else if (ui.phase === 'error') renderError();
  else if (ui.phase === 'ready') renderReady();
  else if (ui.phase === 'empty') renderEmpty();
  else $('#main').innerHTML = `<div class="digest-inner"><div class="side-loading">${t('loading')}</div></div>`;
  if (focused) $('#main ' + test(focused))?.focus();
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
      data-key="${esc(s.key)}" data-status="${status}" title="${esc(hint(status))}">
      <span class="srow-time">${timeLabel(s.updated)}</span>${toolIcon(s.tool, 18)}
      <span class="srow-main"><span class="srow-title"><span class="srow-proj">${esc(projectName(s))}</span> · ${esc(sessionName(s))}</span>
      ${s.last ? `<span class="srow-res">${esc(s.last)}</span>` : ''}</span>
      <span class="srow-st">${mark}</span></a>`;
  }
  if (!ui.ready) html = `<div class="side-loading">${t('side.loading')}</div>`;
  else if (!list.length) html = `<div class="side-loading">${t('side.none')}</div>`;
  const legend = running ? `<div class="side-legend">
      <span>${iconCheck(11)}${t('legend.ok')}</span><span>${iconCheck(11, 'var(--accent)')}${t('legend.read')}</span>
      <span>${spinner(11)}${t('legend.now')}</span><span>${iconCircle(9)}${t('legend.queue')}</span><span>${iconDash(11)}${t('legend.skip')}</span>
    </div>` : '';
  $('#side').innerHTML = `<div class="side-head"><span class="side-title">${t('side.title')}</span>
      <span class="side-note">${esc(t('side.count', {n: list.length, days: daysLabel()}) + t(running ? 'side.running' : 'side.idle'))}</span></div>
    <div class="side-list scroll" data-testid="digest-sessions">${html}</div>${legend}`;
}

function render() { renderToolbar(); renderMain(); renderSide(); }

// --- журнал агента ---------------------------------------------------------
// Журнал последнего запуска сводки: события trace из потока задания «digest» (что отправлено
// агенту, что модель думает, пишет, какие инструменты вызывает), ошибки, сессия агента для
// ответа и ответы человека. «Журнал агента» показывает его на месте сводки, как переписку сессии.
// Сервер помнит задания до перезапуска; номера заданий с ответами запоминаются, чтобы после
// перезагрузки страницы переписка была целиком.
const REPLIES_KEY = 'nit.digest.replies';
const LONG_ENTRY = 400;

function newLog(agent) {
  return {agent, model: '', entries: [], session: null, result: null, running: true, reply: null, draft: '', t0: 0, last: 0};
}
function logEvent(log, event, reply = false) {
  if (!log) return;
  // Время в шапке — от начала запуска сводки; у ответов агенту свой отсчёт, он не нужен.
  if (!reply && typeof event.elapsed === 'number') { log.last = event.elapsed; log.t0 = Date.now() / 1000 - event.elapsed; }
  if (event.type === 'trace') {
    if (event.agent && event.agent !== 'nit') log.agent = event.agent;
    if (event.kind === 'model' && event.text) log.model = event.text;
    // В ответе первым приходит текст человека.
    log.entries.push({kind: reply && event.kind === 'prompt' ? 'you' : event.kind, text: String(event.text ?? ''),
                      tool: event.tool || '', agent: event.agent || ''});
  } else if (event.type === 'agent_session') {
    log.session = {agent: event.agent, session: event.session, command: event.command || ''};
  } else if (event.type === 'error' || event.type === 'warning') {
    log.entries.push({kind: event.type, text: event.message || '', code: event.code, output: event.output || ''});
  } else if (event.type === 'result' && !reply) {
    log.result = event.result;
  } else return;
  if (log === ui.log) updateAgentView();
}
// Журнал есть, пока идёт составление, после ошибки и у готовой сводки, которую составил этот запуск.
function logAvailable() {
  const log = ui.log, digest = digestForDays();
  if (!log) return false;
  if (ui.phase === 'running' || ui.phase === 'error') return true;
  return ui.phase === 'ready' && !!log.result && JSON.stringify(log.result) === JSON.stringify(digest?.result);
}
const logButton = (cls = 'btn') => logAvailable() ? button('digest-agent-log', t('log.open'), cls) : '';

// Журнал — в оформлении переписки сессии, как «сессия агента» на главном экране: сообщения (.msg)
// для запроса, ответов человека, рассуждений и текста агента; вызовы и результаты инструментов,
// строки «Нити», модель и расход токенов — компактными строками.
function entryHtml(entry, log) {
  const text = esc(entry.text), long = entry.text.length > LONG_ENTRY || entry.text.split('\n').length > 6;
  const agent = entry.agent && entry.agent !== 'nit' ? entry.agent : log.agent;
  const message = (testid, speaker, cls = '') => `<div class="msg log-msg ${cls}" data-testid="${testid}"><div class="speaker">${speaker}</div>
    <div class="row-content"><div class="msg-text${long ? ' is-clamped' : ''}">${text}</div>${long ? button('log-more', t('log.expand'), 'link') : ''}</div></div>`;
  const line = (testid, body, cls = '') => `<div class="log-row ${cls}" data-testid="${testid}"><span></span><div class="log-line">${body}</div></div>`;
  const fold = (summary, body) => `<details class="log-details"><summary>${summary}</summary><pre class="log-pre">${body}</pre></details>`;
  const output = entry.output ? fold(t('log.output'), esc(entry.output)) : '';
  switch (entry.kind) {
    case 'prompt': return `<div class="msg log-msg" data-testid="log-prompt"><div class="speaker"><span class="nit-name">${t('log.nit')}</span></div>
      <div class="row-content">${fold(t('log.prompt'), text)}</div></div>`;
    case 'you': return message('log-user', `<span class="me">${t('log.you')}</span>`);
    case 'thinking': return message('log-thinking', `<span class="muted">${t('log.thinking')}</span>`, 'is-thinking');
    case 'text': return message('log-text', agent ? `<span class="with-icon">${toolIcon(agent, 14)}${esc(toolName(agent))}</span>` : '');
    case 'tool': return line('log-tool', `<span class="log-tool-name">${esc(entry.tool || '?')}</span> <code class="log-args">${text}</code>`, 'is-tool');
    case 'result': return line('log-result', fold(t('log.result'), text));
    case 'model': return line('log-model', esc(t('log.model', {model: entry.text})), 'is-quiet');
    case 'usage': return line('log-usage', text, 'is-quiet');
    case 'nit': return line('log-nit', `<b>${t('log.nit')}:</b> ${text}`, 'is-quiet');
    case 'stopped': return line('log-stopped', t('log.stopped'), 'is-quiet');
    case 'warning': return line('log-warning', `${text}${output}`, 'is-warning');
    case 'error': return line('log-error', `${text}${entry.code !== undefined && entry.code !== null
      ? ` · ${esc(t('error.code', {code: entry.code}))}` : ''}${output}`, 'is-error');
    default: return entry.text ? line('log-other', text, 'is-quiet') : '';
  }
}
// Шапка: к сводке, заголовок, «Остановить», агент, модель (trace model), идёт/закончено и время.
function headKey(log) { return JSON.stringify([log.agent, log.model, log.running, !!log.reply, ui.days]); }
function headHtml(log) {
  const working = log.running || !!log.reply, agent = log.agent;
  return `${button('agent-back', t('log.back'), 'link agent-back')}
    <div class="title-line"><h1 data-testid="agent-title">${esc(t('text.title', {range: rangeLabel()}))}</h1><div class="grow"></div>
      ${working ? button('agent-stop', t('run.stop'), 'btn sm') : ''}</div>
    <div class="meta" data-testid="agent-meta">${agent ? `<span class="with-icon" data-testid="agent-name">${toolIcon(agent, 16)}${esc(toolName(agent))}</span>` : ''}
      ${log.model ? `<span class="branch" data-testid="agent-model">${esc(log.model)}</span>` : ''}
      <span class="with-icon agent-state${working ? ' is-running' : ''}" data-testid="agent-state">${working ? spinner(12) : ''}${t(working ? 'log.running' : 'log.finished')}</span>
      <span class="agent-time" data-testid="agent-time">${logTime(log)}</span></div>`;
}
const logTime = (log) => fmtElapsed(Math.max(0, log.running ? Date.now() / 1000 - log.t0 : log.last));
// Ответ агенту и команда для терминала: после того как агент назвал сессию и запуск закончился.
function footKey(log) { return JSON.stringify([log.running, log.session, !!log.reply]); }
function footHtml(log) {
  if (log.running) return '';
  if (!log.session) return `<p class="log-note">${t('log.noSession')}</p>`;
  const busy = !!log.reply;
  return `<div class="reply-form"><span class="prompt" aria-hidden="true">›</span><label for="log-reply" class="sr">${t('reply.label')}</label>
      <textarea id="log-reply" data-testid="reply-input" rows="1" placeholder="${t('reply.placeholder')}" title="${t('reply.hint')}"${busy ? ' disabled' : ''}>${esc(log.draft)}</textarea>
      ${button('reply-send', t('reply.send'), 'btn sm', busy || !log.draft.trim() ? 'disabled' : '')}</div>
    ${log.session.command ? `<div class="reply-terminal"><span>${t('terminal.title')}</span>
      <code data-testid="reply-command"><span class="prompt">$ </span>${esc(log.session.command)}</code>${button('reply-copy-command', t('terminal.copy'), 'btn sm')}</div>` : ''}`;
}
// Новые строки дописываются в конец, а не перерисовывают переписку: раскрытые блоки не сворачиваются.
// Переписка прижата к низу (column-reverse у .conversation), поэтому последнее видно без прокрутки.
let shown = {log: null, count: 0, head: null, foot: null};
function renderAgentView() {
  $('#main').classList.add('is-agent');
  $('#main').innerHTML = `<section class="session agent-session" data-testid="agent-session">
    <div class="session-top" id="agent-top"></div>
    <div class="conversation scroll" data-testid="agent-log"><div id="agent-entries"></div></div>
    <div class="agent-reply" id="agent-reply"></div></section>`;
  shown = {log: ui.log, count: 0, head: null, foot: null};
  updateAgentView();
}
function updateAgentView() {
  const log = ui.log, list = $('#agent-entries');
  if (!ui.agentView || !log || !list || shown.log !== log) return;
  const head = headKey(log);
  if (head !== shown.head) {
    const hadFocus = $('#agent-top').contains(document.activeElement);
    $('#agent-top').innerHTML = headHtml(log);
    shown.head = head;
    if (hadFocus) $(test('agent-back'))?.focus();
  } else $(test('agent-time')).textContent = logTime(log);
  if (log.entries.length > shown.count) {
    list.querySelector('.log-empty')?.remove();
    list.insertAdjacentHTML('beforeend', log.entries.slice(shown.count).map(entry => entryHtml(entry, log)).join(''));
    shown.count = log.entries.length;
  } else if (!log.entries.length && !list.firstChild) {
    list.innerHTML = `<div class="log-empty">${t(log.running ? 'log.waiting' : 'log.empty')}</div>`;
  }
  const foot = $('#agent-reply'), key = footKey(log);
  if (key !== shown.foot) {
    // Поле блокируется на время ответа: фокус возвращается в него, когда ответ пришёл.
    const hadFocus = foot.contains(document.activeElement) || document.activeElement?.dataset?.testid === 'agent-stop';
    foot.innerHTML = footHtml(log);
    shown.foot = key;
    if (hadFocus || !log.reply && shown.refocus) { $('#log-reply:not(:disabled)')?.focus(); shown.refocus = false; }
  }
}
function showAgentView(show) {
  ui.agentView = show;
  renderMain();
  (show ? $(test('agent-back')) : $(test('digest-agent-log')))?.focus();
}

// Ответ агенту продолжает ту же сессию CLI отдельным заданием; его события дописываются в журнал.
async function followReply(log, job) {
  // «Остановить» могли нажать, пока сервер ещё не назвал задание: тогда остановить сразу.
  log.reply = {job, stopping: !!log.reply?.stopping};
  if (log.reply.stopping) cancelReply(job);
  updateAgentView();
  let answered = false;
  try {
    await readEvents(job, 0, event => {
      if (event.type === 'trace' && event.kind === 'text') answered = true;
      // Ответ CLI обычно уже пришёл строкой text; если нет — показать его из результата.
      if (event.type === 'result' && !answered && event.result?.answer) {
        logEvent(log, {type: 'trace', kind: 'text', text: event.result.answer}, true);
      } else logEvent(log, event, true);
    });
  } catch (error) {
    if (error.status !== 404) log.entries.push({kind: 'error', text: error.message});
  }
  if (log.reply.stopping) log.entries.push({kind: 'stopped', text: ''});
  log.reply = null;
  if (log === ui.log) updateAgentView();
}
async function sendReply() {
  const log = ui.log, text = $('#log-reply')?.value.trim();
  if (!log?.session || log.running || log.reply || !text) return;
  log.reply = {job: null, stopping: false};
  updateAgentView();
  let job;
  try {
    ({job} = await api('/api/agent/reply', {agent: log.session.agent, session: log.session.session, text}));
  } catch (error) {
    log.reply = null;
    updateAgentView();
    toast(error.message);
    return;
  }
  log.draft = '';
  shown.refocus = true;  // когда агент ответит, можно сразу писать дальше
  const saved = readStored(REPLIES_KEY, null);
  writeStored(REPLIES_KEY, {session: log.session.session,
                            jobs: [...(saved?.session === log.session.session ? saved.jobs : []), job]});
  followReply(log, job);
}
async function cancelReply(job) {
  try { await api(`/api/jobs/${encodeURIComponent(job)}/cancel`, {}); } catch { /* задание уже закончилось */ }
}
async function stopReply() {
  const reply = ui.log?.reply;
  if (!reply || reply.stopping) return;
  reply.stopping = true;
  if (reply.job) await cancelReply(reply.job);
}
// После перезагрузки: журнал законченного запуска и ответы в его сессию, если сервер их помнит.
async function loadLog() {
  const log = newLog('');
  try { await readEvents('digest', 0, event => logEvent(log, event)); }
  catch { return; }
  log.running = false;
  if (ui.log) return;  // пока читали, начался новый запуск
  ui.log = log;
  render();
  const saved = readStored(REPLIES_KEY, null);
  if (log.session && saved?.session === log.session.session) {
    for (const job of saved.jobs) await followReply(log, job);
  }
}

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

// Поток событий задания: строки JSON, пока задание не закончится.
async function readEvents(jobId, since, onEvent) {
  const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/events?since=${since}`);
  if (!response.ok) throw Object.assign(new Error(t('error.connect', {status: response.status})), {status: response.status});
  const reader = response.body.getReader(), decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const {value, done} = await reader.read();
    buffer += decoder.decode(value, {stream: !done});
    const lines = buffer.split('\n'); buffer = lines.pop();
    for (const line of lines) if (line.trim()) onEvent(JSON.parse(line));
    if (done) {
      if (buffer.trim()) onEvent(JSON.parse(buffer));
      return;
    }
  }
}

function handleEvent(run, event) {
  run.since = event.n;
  logEvent(run.log, event);
  // Журнал агента обновляется сам; остальной экран от этих событий не меняется.
  if (event.type === 'trace' || event.type === 'agent_session') return;
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
      // Новая сводка уже сохранена: перечитать её до того, как экран хода сменится сводкой.
      run.refresh = refreshDigest().catch(() => {});
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
    await readEvents(jobId, since, event => handleEvent(run, event));
  } catch (error) {
    if (ui.run === run && !run.finished && !run.cancelRequested) {
      ui.error = {message: error.message};
      ui.phase = 'error';
    }
  } finally {
    await run.refresh;
    run.log.running = false;
    updateAgentView();
    if (ui.run === run) ui.run = null;
    if (ui.phase !== 'error') finishRun();
    else render();
  }
}

function newRun(model, started) {
  ui.log = newLog(model);
  return {model, started, stage: 0, logs: [], notes: [], tokens: null, log: ui.log, statuses: new Map(),
          neededTotal: 0, readDone: 0, nowCount: 0, since: 0, cancelRequested: false, finished: false};
}
async function startDigest(model = ui.model) {
  if (ui.phase === 'running') return;
  writePrefs();
  ui.run = newRun(model, Date.now() / 1000);
  ui.phase = 'running';
  ui.error = null;
  writeStored(REPLIES_KEY, null);
  render();
  try {
    await loadAgentModels(model);
    const {model: agentModel, effort} = choiceFor(model);
    const {job} = await api('/api/digest', {days: ui.days, model, ...(agentModel ? {agent_model: agentModel} : {}),
                                            ...(effort ? {effort} : {})});
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
  ui.run = newRun(ui.model, job.started);
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
  loadAgentModels(model);
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
    case 'digest-agent-log': showAgentView(true); break;
    case 'agent-back': showAgentView(false); break;
    case 'agent-stop': if (ui.log?.reply) await stopReply(); else await stopDigest(); break;
    case 'log-more': {
      const text = element.previousElementSibling, open = text.classList.toggle('is-clamped');
      element.textContent = t(open ? 'log.expand' : 'log.collapse');
      break;
    }
    case 'reply-send': await sendReply(); break;
    case 'reply-copy-command': {
      await writeClipboard(ui.log.session.command);
      element.textContent = t('terminal.copied');
      setTimeout(() => { if (element.isConnected) element.textContent = t('terminal.copy'); }, 2400);
      break;
    }
    case 'data-menu': ui.menu = ui.menu === 'data' ? '' : 'data'; renderMenus(); break;
    case 'data-import': ui.menu = ''; renderMenus(); $(test('data-import-file')).click(); break;
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
    // Действия — только у кнопок: ссылки, поля, списки и раскрывающиеся блоки журнала работают сами.
    const element = event.target.closest('button[data-testid]');
    if (!element) return;
    event.preventDefault();
    safely(element.dataset.testid, element);
  });
  document.addEventListener('change', event => {
    const tail = event.target.closest('[data-testid="digest-tail"] input');
    if (tail) toggleTail(event.target.closest('[data-tail]').dataset.tail, event.target);
    if (event.target.matches(test('digest-agent-model'))) setChoice('model', event.target.value);
    if (event.target.matches(test('digest-effort'))) setChoice('effort', event.target.value);
  });
  document.addEventListener('input', event => {
    if (event.target.id !== 'log-reply' || !ui.log) return;
    ui.log.draft = event.target.value;
    const send = $(test('reply-send'));
    if (send) send.disabled = !event.target.value.trim();
  });
  document.addEventListener('keydown', event => {
    // Enter отправляет, Shift+Enter переносит строку; во время набора через IME Enter не трогаем.
    if (event.target.id === 'log-reply' && event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      sendReply();
    } else if (event.key === 'Escape' && ui.agentView && event.target.id !== 'log-reply') showAgentView(false);
  });
  $(test('data-import-file')).addEventListener('change', async event => {
    const file = event.target.files[0];
    if (!file) return;
    try {
      const state = await api('/api/import', {data: JSON.parse(await file.text())});
      ui.names = state.names || ui.names;
      ui.hidden = state.hidden || ui.hidden;
      ui.tailsDone = state.digest_tails || ui.tailsDone;
      toast(t('data.imported'));
      renderNavCounts();
      render();
    } catch (error) { toast(t('data.importFailed', {error: error.message})); }
    event.target.value = '';
  });
}

async function init() {
  readPrefs();
  // Язык приходит в /api/config: страница рисуется уже на нём.
  let config = null, failure = null;
  try {
    config = await api('/api/config');
    setLanguage(config.language);
    registerAgents(config.agents);
  } catch (error) { failure = error; }
  document.title = t('page.title');
  shell();
  bind();
  render();
  tickTimer = setInterval(() => {
    if (ui.phase === 'running' && ui.run) {
      const elapsed = $(test('digest-running'))?.querySelector('.run-elapsed');
      if (elapsed) elapsed.textContent = fmtElapsed(Date.now() / 1000 - ui.run.started);
    }
    const place = ui.agentView && ui.log?.running && $(test('agent-time'));
    if (place) place.textContent = logTime(ui.log);
  }, 1000);
  setInterval(async () => {
    if (ui.phase === 'running') return;
    try { if ((await api('/api/jobs')).some(j => j.kind === 'digest')) await resumeDigest(); } catch { /* следующий опрос */ }
  }, 5000);
  try {
    if (failure) throw failure;
    const [state, models, jobs] = await Promise.all([api('/api/state'), api('/api/digest/models'), api('/api/jobs')]);
    ui.names = state.names || {};
    ui.hidden = state.hidden || [];
    ui.tailsDone = state.digest_tails || {};
    ui.models = models;
    ui.model = defaultModel(config.settings);
    const choice = readStored(CHOICE_KEY, {});
    ui.choice = choice && typeof choice === 'object' ? choice : {};
    loadAgentModels(ui.model);
    if (jobs.some(j => j.kind === 'digest')) await resumeDigest();
    else loadLog();
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
    $('#main').innerHTML = `<div class="digest-inner"><div class="loading-view" role="alert"><div class="loading-card">${esc(t('error.load', {error: error.message}))}<a class="btn" href="${esc(location.href)}">${t('retry')}</a></div></div></div>`;
  }
}
init();
