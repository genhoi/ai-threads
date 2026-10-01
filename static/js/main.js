import {toolIcon, toolName, registerAgents} from './icons.js';
import {addStrings, setLanguage, lang, locale, t} from './i18n.js';
import strings from './lang/main.js';

addStrings(strings);

const $ = (selector) => document.querySelector(selector);
const test = (id) => `[data-testid="${id}"]`;
const esc = (value = '') => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const svg = (path, size = 16, strokeWidth = 1.8) => `<svg width="${size}" height="${size}" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="${strokeWidth}" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;
const icons = {
  terminal: svg('<path d="M4 17l4-4-4-4M11 19h9"/>'),
  search: svg('<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4 4"/>'),
  rename: svg('<path d="M4 20h4L19 9l-4-4L4 16v4zM13.5 6.5l4 4"/>'),
  star: svg('<path d="M12 3.5l2.6 5.5 5.9.6-4.5 4 1.3 5.9-5.3-3-5.3 3 1.3-5.9-4.5-4 5.9-.6z"/>',18),
  menu: `<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="5.5" cy="12" r="1.7" fill="currentColor"/><circle cx="12" cy="12" r="1.7" fill="currentColor"/><circle cx="18.5" cy="12" r="1.7" fill="currentColor"/></svg>`,
  done: svg('<circle cx="12" cy="12" r="8.5"/><path d="M8 12.3l2.8 2.8L16.2 9.6"/>'),
  last: svg('<path d="M4 6h16M4 11h16M4 16h9"/>'),
  close: svg('<path d="M6 6l12 12M18 6L6 18"/>',14),
  info: svg('<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.2M12 7.8v.1"/>'),
  spark: svg('<path d="M11 4l1.8 5.2L18 11l-5.2 1.8L11 18l-1.8-5.2L4 11l5.2-1.8z"/><path d="M18.5 3v4M16.5 5h4"/>',13,2),
  branch: svg('<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="7" r="2"/><path d="M6 7v10M18 9c0 5-6 4-11 8"/>',14),
};
const starFilled = `<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3.5l2.6 5.5 5.9.6-4.5 4 1.3 5.9-5.3-3-5.3 3 1.3-5.9-4.5-4 5.9-.6z" fill="currentColor"/></svg>`;
const warnIcon = `<svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4l9 16H3z" stroke="currentColor" stroke-width="2" fill="none" stroke-linejoin="round"/><path d="M12 10v4M12 17.2v.1" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`;
const checkBox = `<svg width="12" height="12" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7" stroke="currentColor" stroke-width="3" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
const flashes = new WeakMap();
const button = (id, text, cls = 'btn', extra = '') => `<button type="button" data-testid="${id}" class="${cls}" ${extra}>${text}</button>`;
const kbd = (key) => `<span class="kbd" aria-hidden="true">${key}</span>`;
// project: null — лента без фильтра по проекту; '' — фильтр по сессиям без проекта.
const ui = {section:'mine', key:'', view:'list', project:null, q:'', inConv:false, hideDone:false, showLast:true,
  ready:false, sessions:[], state:{pins:[], names:{}, summaries:{}, done:[], hidden:[]}, live:{},
  limit:50, picking:false, picked:new Set(), fmt:'plain', editing:false, summaryOpen:false, jobs:new Map(),
  messages:new Map(), expanded:new Set(), snippets:new Map(), menu:'', script:null,
  config:{agents:[], runners:[], env:{}, settings:{}}, smart:null, full:null, log:'', cursor:''};
let sessionMap = new Map(), visible = [], searchController, searchTimer, toastTimer, undo, renderingFeed = false;

async function api(path, body, options = {}) {
  const response = await fetch(path, {...options, ...(body === undefined ? {} : {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)})});
  if (!response.ok) {
    let message = t('error.status', {status:response.status});
    try { message = (await response.json()).error || message; } catch { /* Ответ сервера может быть без JSON. */ }
    throw new Error(message);
  }
  return options.text ? response.text() : response.json();
}
function toast(message, action) {
  clearTimeout(toastTimer); undo = action;
  $('#toast-area').innerHTML = `<div class="toast" data-testid="toast" role="status"><span>${esc(message)}</span>${action ? button('toast-undo',esc(t('toast.undo')),'link') : ''}</div>`;
  toastTimer = setTimeout(() => { $('#toast-area').innerHTML = ''; undo = null; }, 7000);
}
function eventElement(event) {
  const node = event.target;
  return node instanceof Element ? node : node?.parentElement || null;
}
function flash(element, html) {
  if (!element?.isConnected) return;
  const previous = flashes.get(element);
  const original = previous ? previous.original : element.innerHTML;
  clearTimeout(previous?.timer);
  element.innerHTML = html;
  element.classList.add('done');
  const timer = setTimeout(() => {
    flashes.delete(element);
    if (!element.isConnected) return;
    element.innerHTML = original;
    element.classList.remove('done');
  }, 2400);
  flashes.set(element, {original, timer});
}
async function copy(text, element, notice) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const area = document.createElement('textarea');
    area.value = text; area.style.cssText = 'position:fixed;left:-9999px';
    document.body.append(area); area.select();
    const copied = document.execCommand('copy'); area.remove();
    if (!copied) throw new Error(t('copy.failed'));
  }
  if (element?.isConnected) {
    const badge = element.querySelector('.kbd');
    flash(element, `${esc(t('copied'))}${badge ? badge.outerHTML : ''}`);
  } else toast(notice || t('copied'));
}
// Название и проект могут прийти пустыми: подпись для них ставит страница на своём языке.
function name(s) { return ui.state.names[s.key] || s.title || t('untitled'); }
function projectLabel(project) { return project || t('no-project'); }
function env() { return ui.config.env || {}; }
function applyConfig(config) {
  ui.config = config; registerAgents(config.agents); renderChoices();
  const chosen = config.settings?.open_with;
  ui.fmt = formats().includes(chosen) ? chosen : env().wsl ? 'wt' : env().tmux ? 'tmux' : 'plain';
}
function formats() { return ['wt','tmux','plain'].filter(fmt => fmt !== 'wt' || env().wsl); }
// Кто составит сводку или проведёт умный поиск: агент, выбранный в меню рядом с кнопкой,
// иначе агент из настроек, иначе первый установленный. auto — без учёта меню.
function plannedRunner(kind = 'summary', auto = false) {
  const chosen = auto ? '' : choiceOf(kind).agent;
  if (chosen) return chosen;
  const wanted = ui.config.settings?.[kind]?.agent;
  return wanted && wanted !== 'auto' ? wanted : ui.config.runners[0] || '';
}
function noRunnerHint() {
  const order = ui.config.runner_ids || ui.config.agents.filter(a => a.runner).map(a => a.id);
  const names = order.map(id => toolName(id));
  return t('no-runner', {names:names.length ? new Intl.ListFormat(locale(), {type:'disjunction'}).format(names) : t('no-runner.any')});
}
const narrow = () => matchMedia('(max-width: 899px)').matches;
const minutes = (seconds) => `${Math.floor(Math.max(0,seconds)/60)}:${String(Math.floor(Math.max(0,seconds)%60)).padStart(2,'0')}`;
function mine(s) { return !s.auto && !s.temp && !ui.state.hidden.includes(s.key); }
function day(timestamp) {
  const date = new Date(timestamp * 1000);
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}
function calendarDay(date) { return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`; }
function dayDistance(timestamp) {
  const date = new Date(timestamp * 1000), today = new Date();
  return Math.round((Date.UTC(today.getFullYear(), today.getMonth(), today.getDate()) - Date.UTC(date.getFullYear(), date.getMonth(), date.getDate())) / 86400000);
}
// «28 сен» / «Sep 28», год — только у прошлых лет.
function dayMonth(date) {
  const year = date.getFullYear() !== new Date().getFullYear() ? date.getFullYear() : '';
  return t('date.day-month', {day:date.getDate(), month:t('date.months').split(' ')[date.getMonth()], year});
}
function weekday(date, width) { return date.toLocaleDateString(locale(), {weekday:width}); }
// Заголовок дня: «Сегодня», «Понедельник, 28 сен». inline — для середины строки: «сегодня», «понедельник, 28 сен».
function dayLabel(timestamp, inline = false) {
  const today = new Date(), yesterday = new Date(); yesterday.setDate(today.getDate() - 1);
  const key = day(timestamp);
  if (key === calendarDay(today)) return t(inline ? 'date.today-inline' : 'date.today');
  if (key === calendarDay(yesterday)) return t(inline ? 'date.yesterday-inline' : 'date.yesterday');
  const date = new Date(timestamp * 1000), long = weekday(date, 'long');
  return `${inline ? long : long.charAt(0).toUpperCase() + long.slice(1)}, ${dayMonth(date)}`;
}
function pinnedMark(timestamp) {
  const today = new Date(), yesterday = new Date(); yesterday.setDate(today.getDate() - 1);
  const key = day(timestamp);
  if (key === calendarDay(today)) return time(timestamp);
  if (key === calendarDay(yesterday)) return t('date.yesterday-inline');
  const date = new Date(timestamp * 1000), diff = dayDistance(timestamp);
  if (diff > 1 && diff < 7) return weekday(date, 'short');
  return dayMonth(date);
}
// По-русски 09:05, по-английски 9:05 AM.
function time(timestamp) { return new Date(timestamp * 1000).toLocaleTimeString(locale(), {hour:lang() === 'ru' ? '2-digit' : 'numeric', minute:'2-digit'}); }
function highlighted(text, query = ui.q) {
  const value = String(text || ''), q = query.trim().toLocaleLowerCase();
  if (!q) return esc(value);
  let start = 0, result = '', index;
  while ((index = value.toLocaleLowerCase().indexOf(q, start)) >= 0) {
    result += esc(value.slice(start, index)) + '<mark>' + esc(value.slice(index, index + q.length)) + '</mark>';
    start = index + q.length;
  }
  return result + esc(value.slice(start));
}
// Подсветить в тексте любое из слов без учёта регистра: фрагменты полного поиска.
function marked(text, terms) {
  const value = String(text || ''), lower = value.toLocaleLowerCase();
  const words = terms.map(w => String(w).toLocaleLowerCase()).filter(Boolean);
  let result = '', at = 0;
  while (words.length) {
    let start = -1, size = 0;
    for (const word of words) {
      const index = lower.indexOf(word, at);
      if (index >= 0 && (start < 0 || index < start || (index === start && word.length > size))) { start = index; size = word.length; }
    }
    if (start < 0) break;
    result += esc(value.slice(at, start)) + '<mark>' + esc(value.slice(start, start + size)) + '</mark>';
    at = start + size;
  }
  return result + esc(value.slice(at));
}
// Где искать запрос; второе значение — ключ подписи «field.*» под строкой сессии.
function fields(s) {
  const summary = ui.state.summaries[s.key];
  return [[name(s),'title'],[s.title,'original'],[projectLabel(s.project),'project'],[s.branch,'branch'],[s.cwd,'path'],[s.id,'id'],
    [summary ? `${summary.title} ${summary.summary} ${summary.next_step || ''}` : '', 'summary'],[ui.snippets.get(s.key),'conversation']];
}
function matches(s) { const q = ui.q.trim().toLocaleLowerCase(); return !q || fields(s).some(([value]) => String(value || '').toLocaleLowerCase().includes(q)); }
function inProject(s) { return ui.project === null || s.project === ui.project; }
function filtered() {
  return ui.sessions.filter(s => (ui.section === 'all' || mine(s)) && inProject(s)
    && (!ui.hideDone || !ui.state.done.includes(s.key) || s.key === ui.key) && matches(s));
}
function waiting() { return ui.sessions.filter(s => !s.auto && !s.temp && ui.live[s.key]?.state === 'wait'); }
function readHash() {
  const hash = new URLSearchParams(location.hash.slice(1));
  ui.section = ['mine','all','projects'].includes(hash.get('section')) ? hash.get('section') : 'mine';
  ui.key = hash.get('session') || hash.get('key') || '';
  ui.view = hash.get('view') || (ui.key ? 'session' : 'list');
  ui.project = hash.get('project');
  // log — задание, чья «сессия агента» открыта справа (view=agent): search:<n> или summary:<ключ>.
  ui.log = hash.get('log') || '';
  if (ui.view === 'agent' && !ui.log) ui.view = ui.key ? 'session' : 'list';
  if (ui.view === 'session') ui.log = '';
}
function writeHash(replace = false) {
  const hash = new URLSearchParams({section:ui.section});
  if (ui.key) hash.set('session',ui.key);
  if (ui.project !== null) hash.set('project',ui.project);
  hash.set('view',ui.view);
  if (ui.log) hash.set('log',ui.log);
  history[replace ? 'replaceState' : 'pushState'](null,'',`#${hash}`);
}
// Каркас страницы. Рисуется до ответа сервера на языке браузера и ещё раз, если /api/config
// назначил другой язык, поэтому обработчики событий висят на document, а не на элементах каркаса.
function shell() {
  document.title = t('app.title');
  $('#app').innerHTML = `<header class="topbar"><div class="brand">${svg('<path d="M3 16c3-7 6 1 9-5s6 2 9-4" stroke="var(--accent)"/>',18,2.2)}<span>${esc(t('brand'))}</span></div>
    <nav class="nav" aria-label="${esc(t('nav.label'))}"><a href="#section=mine" data-testid="nav-mine">${esc(t('nav.mine'))}</a><a href="#section=all" data-testid="nav-all">${esc(t('nav.all'))} <span class="n"></span></a><a href="#section=projects" data-testid="nav-projects">${esc(t('nav.projects'))} <span class="n"></span></a><a href="/digest.html" data-testid="nav-digest">${esc(t('nav.digest'))}</a></nav>
    <div class="grow"></div>${button('wait-next','','wait-btn','hidden')}<span class="addr" data-testid="server-addr">${esc(location.host)}</span>
    <div class="menu-anchor">${button('data-menu',esc(t('data')),'btn','aria-haspopup="menu" aria-expanded="false"')}<div id="data-popup"></div></div><a href="/settings.html" class="icon-btn" aria-label="${esc(t('settings'))}" title="${esc(t('settings'))}" data-testid="settings-link"><svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8" fill="none"></circle><path d="M12 2.8v2.6M12 18.6v2.6M21.2 12h-2.6M5.4 12H2.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8M18.5 18.5l-1.8-1.8M7.3 7.3L5.5 5.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path><circle cx="12" cy="12" r="6.2" stroke="currentColor" stroke-width="1.8" fill="none"></circle></svg></a>
    <input type="file" accept="application/json,.json" data-testid="data-import-file" hidden></header>
    <div class="workspace"><aside class="sidebar"><div class="search-wrap"><label for="q" class="sr">${esc(t('search.label'))}</label><div class="search"><span class="search-icon">${icons.search}</span><input id="q" data-testid="search" type="search" placeholder="${esc(t('search.placeholder'))}" autocomplete="off" value="${esc(ui.q)}">${kbd('/')}<div class="search-actions">${button('search-conv',esc(t('search.conv')),'toggle',`aria-pressed="${ui.inConv}" title="${esc(t('search.conv-title'))}"`)}</div></div></div><div id="feed-tools"></div><div id="feed-banners"></div><div id="smart-area"></div><div id="full-area"></div><div class="feed scroll" id="feed"></div></aside><main class="main" id="main"></main></div><div id="toast-area"></div><div id="choice-popup"></div>`;
}
function layout() { $('.workspace').classList.toggle('detail', ui.view === 'session' || ui.view === 'agent' || !ui.ready); }
function renderNav() {
  for (const section of ['mine','all','projects']) $(test(`nav-${section}`)).classList.toggle('on',ui.section === section);
  $(test('nav-all') + ' .n').textContent = ui.sessions.length;
  $(test('nav-projects') + ' .n').textContent = new Set(ui.sessions.filter(mine).map(s => s.project)).size;
  const waits = waiting();
  const btn = $(test('wait-next')); btn.hidden = !waits.length;
  btn.innerHTML = `<span class="dot wait"></span>${esc(t('wait-count', {n:waits.length}))}`;
}
function dayPick(offset) {
  const date = new Date(); date.setDate(date.getDate() + offset);
  return ui.sessions.filter(s => mine(s) && inProject(s) && day(s.updated) === calendarDay(date) && !ui.state.done.includes(s.key) && !ui.live[s.key]);
}
function legacyData() {
  try {
    const pins = JSON.parse(localStorage.getItem('nit.pins') || '[]');
    const names = JSON.parse(localStorage.getItem('nit.names') || '{}');
    return {pins:Array.isArray(pins) ? pins : Object.keys(pins).filter(k => pins[k]), names:names && typeof names === 'object' && !Array.isArray(names) ? names : {}};
  } catch { return {pins:[],names:{}}; }
}
function renderTools() {
  const hideTitle = esc(t(ui.hideDone ? 'feed.show-done' : 'feed.hide-done')), lastTitle = esc(t('feed.show-last'));
  $('#feed-tools').innerHTML = !ui.ready ? '' : ui.picking ? `<div class="feed-tools pick-tools"><strong data-testid="pick-count">${esc(t('pick.count', {n:ui.picked.size}))}</strong>${button('pick-yesterday',esc(t('pick.yesterday')),'link')}${button('pick-today',esc(t('pick.today')),'link')}${button('pick-none',esc(t('pick.none')),'link')}<span class="grow"></span>${button('pick-next',esc(t('pick.next')),'btn btn-primary pick-next')}${button('pick-done',esc(t('pick.done')))}</div>` : `<div class="feed-tools">${button('pick-yesterday',`${icons.terminal}${esc(t('feed.yesterday', {n:dayPick(-1).length}))}`)}${button('pick-enter',esc(t('feed.pick')),'link')}<span class="grow"></span>${button('toggle-hide-done',icons.done,'icon-btn',`aria-pressed="${ui.hideDone}" aria-label="${hideTitle}" title="${hideTitle}"`)}${button('toggle-last',icons.last,'icon-btn',`aria-pressed="${ui.showLast}" aria-label="${lastTitle}" title="${lastTitle}"`)}</div>`;
  const legacy = legacyData();
  $('#feed-banners').innerHTML = (ui.project !== null ? `<div class="feed-banner" data-testid="project-filter">${esc(t('project.filter', {project:projectLabel(ui.project)}))}${button('project-filter-clear',icons.close,'icon-btn',`aria-label="${esc(t('project.filter-clear'))}"`)}</div>` : '') +
    (!ui.state.migrated_local_storage && (legacy.pins.length || Object.keys(legacy.names).length) ? `<div class="feed-banner" data-testid="migrate-banner">${esc(t('migrate.banner', {pins:legacy.pins.length, names:Object.keys(legacy.names).length}))}${button('migrate-button',esc(t('migrate.button')),'btn sm')}</div>` : '');
}
function rowHit(s) {
  const q = ui.q.trim().toLocaleLowerCase();
  if (!q || name(s).toLocaleLowerCase().includes(q)) return null;
  for (const [value, field] of fields(s)) {
    if (field === 'title') continue;
    const text = String(value || '');
    if (!text.toLocaleLowerCase().includes(q)) continue;
    if (field === 'project') return null;
    const index = text.toLocaleLowerCase().indexOf(q);
    const start = Math.max(0, index - 26);
    return [(start > 0 ? '…' : '') + text.slice(start), t(`field.${field}`)];
  }
  return null;
}
// why — почему сессия попала в результат умного поиска; found — фрагмент журнала из полного поиска
// и найденные слова: {snippet, terms}.
function row(s, pinned = false, why = null, found = null) {
  const hit = why === null && !found && rowHit(s);
  const status = ui.live[s.key]?.state, done = ui.state.done.includes(s.key);
  const cls = `row${ui.picking ? ' picking' : ''}${!ui.picking && !agentShown() && s.key === ui.key ? ' is-sel' : ''}${ui.picking && ui.picked.has(s.key) ? ' is-picked' : ''}${done ? ' is-done' : ''}`;
  return `<button type="button" class="${cls}" data-testid="row" data-key="${esc(s.key)}" ${ui.picking ? `aria-pressed="${ui.picked.has(s.key)}"` : ''}>
    ${ui.picking ? `<span class="chk ${ui.picked.has(s.key) ? 'on' : ''}" aria-hidden="true">${checkBox}</span>` : ''}${toolIcon(s.tool,18)}<span class="row-content"><span class="row-top"><span class="row-project">${highlighted(projectLabel(s.project))}</span>${s.branch ? `<span class="row-branch">${highlighted(s.branch)}</span>` : ''}${s.missing ? `<span class="warn-text">${esc(t('row.no-folder'))}</span>` : ''}<span class="row-tail">${done ? `<span class="ok-text">${esc(t('done'))}</span>` : ''}${status ? `<span class="dot ${status}" title="${esc(t(status === 'wait' ? 'live.wait' : 'live.work'))}"></span>` : ''}${pinned ? pinnedMark(s.updated) : time(s.updated)}</span></span><span class="row-title">${highlighted(name(s))}</span>${found ? `<span class="row-sub full-snippet" data-testid="full-snippet">${marked(found.snippet, found.terms)}</span>` : why !== null ? `<span class="row-sub smart-why" data-testid="smart-why" title="${esc(why)}">${esc(why)}</span>` : hit ? `<span class="row-sub"><b>${esc(hit[1])}:</b> ${highlighted(hit[0])}</span>` : ui.showLast && s.last ? `<span class="row-sub">${esc(s.last)}</span>` : ''}</span></button>`;
}
function renderFeed(reset = false) {
  if (!ui.ready) return;
  const feed = $('#feed');
  const scroll = reset ? 0 : feed.scrollTop;
  if (reset) ui.limit = 50;
  renderNav(); renderTools(); renderSmart(); renderFull();
  renderingFeed = true;
  try {
    if (ui.section === 'projects') {
      visible = [];
      const projects = new Map(), q = ui.q.trim().toLocaleLowerCase();
      for (const s of ui.sessions.filter(mine)) {
        const entry = projects.get(s.project) || {count:0,updated:s.updated}; entry.count++; projects.set(s.project,entry);
      }
      const items = [...projects].filter(([p]) => projectLabel(p).toLocaleLowerCase().includes(q));
      feed.innerHTML = items.map(([p,v]) => `<button class="row project-entry" data-testid="project-row" data-project="${esc(p)}"><span><strong>${highlighted(projectLabel(p))}</strong><small>${esc(dayLabel(v.updated))}, ${time(v.updated)}</small></span><span>${v.count}</span></button>`).join('') || (q ? `<div class="empty-feed" data-testid="no-results"><div>${esc(t('feed.not-found', {q:ui.q}))}</div></div>` : `<div class="empty-feed">${esc(t('feed.no-projects'))}</div>`);
      feed.scrollTop = scroll;
      return;
    }
    feed.innerHTML = feedBody();
    feed.scrollTop = scroll;
  } finally { renderingFeed = false; }
  renderChoices();
}
// Лента под строками-действиями: результат умного или полного поиска, кандидаты или обычный список.
function feedBody() {
  if (ui.smart?.result) {
    // Результат умного поиска заменяет ленту, пока его не закрыли.
    const why = new Map(ui.smart.result.results.map(r => [r.key, r.why || '']));
    visible = smartSessions();
    const terms = ui.smart.result.plan?.terms || [];
    return feedActions(visible.length) + (visible.map(s => row(s, true, why.get(s.key))).join('') || `<div class="empty-feed" data-testid="smart-empty"><div>${esc(t('smart.empty'))}${terms.length ? esc(t('smart.searched', {terms:terms.join(', ')})) : ''}</div></div>`);
  }
  if (ui.smart?.running && ui.smart.candidates?.length) {
    // Кандидаты по совпадениям слов, пока агент отбирает: предварительный результат.
    const matched = new Map(ui.smart.candidates.map(c => [c.key, c.matched || []]));
    visible = ui.smart.candidates.filter(c => sessionMap.has(c.key)).map(c => sessionMap.get(c.key));
    return feedActions(visible.length) + `<div class="smart-prelim" data-testid="smart-preliminary">${esc(t('smart.preliminary'))}</div>`
      + visible.map(s => row(s, true, t('smart.matched', {words:matched.get(s.key).join(', ')}))).join('');
  }
  if (ui.full?.result) {
    // Результат полного поиска тоже заменяет ленту, пока его не закрыли.
    const result = ui.full.result, snippets = new Map(result.results.map(r => [r.key, r.snippet || '']));
    visible = fullSessions();
    return feedActions(visible.length) + (visible.map(s => row(s, true, null, {snippet:snippets.get(s.key), terms:result.terms || []})).join('')
      || `<div class="empty-feed" data-testid="full-empty"><div>${esc(t('full.empty', {q:ui.full.q || result.query}))}</div></div>`);
  }
  const all = filtered(), pinned = all.filter(s => ui.state.pins.includes(s.key)), recent = all.filter(s => !ui.state.pins.includes(s.key));
  visible = pinned.concat(recent);
  const actions = feedActions(visible.length);
  // Пока идёт умный или полный поиск, пустая лента по тем же словам только отвлекает от панели поиска.
  if ((ui.smart?.running || ui.full?.running) && !all.length) return actions;
  const displayed = visible.slice(0,ui.limit);
  const pins = displayed.filter(s => ui.state.pins.includes(s.key));
  let html = pins.length ? `<section data-testid="pinned-section"><div class="sec">${esc(t('feed.pinned'))}</div>${pins.map(s => row(s, true)).join('')}</section>` : '';
  let currentDay;
  for (const s of displayed.filter(s => !ui.state.pins.includes(s.key))) {
    if (currentDay !== day(s.updated)) { currentDay = day(s.updated); html += `<div class="sec day">${esc(dayLabel(s.updated))}</div>`; }
    html += row(s);
  }
  const hint = t(ui.inConv ? 'feed.hint-conv' : 'feed.hint');
  const hiddenCount = ui.section === 'mine' ? ui.state.hidden.length : 0;
  const empty = ui.q.trim()
    ? `<div class="empty-feed" data-testid="no-results"><div>${esc(t('feed.not-found', {q:ui.q}))}</div><div class="empty-note">${esc(hint)}</div><div class="actions">${!ui.inConv ? button('search-more',esc(t('feed.search-conv'))) : ''}${button('search-all',esc(t('feed.search-all')))}${button('search-clear',`${esc(t('feed.search-clear'))} ${kbd('Esc')}`)}</div></div>`
    : `<div class="empty-feed">${esc(t('feed.empty'))}</div>`;
  const end = `<div class="feed-end" data-testid="feed-end">${esc(t('feed.end'))}${hiddenCount ? esc(t('feed.hidden', {n:hiddenCount})) : ''}</div>`;
  html += !all.length ? empty : displayed.length < all.length ? `<div class="feed-end">${button('feed-more',esc(t('feed.more')),'link')}</div>` : (ui.q.trim() ? '' : end);
  return actions + html;
}
function renderLoading(progress = {}) {
  const tools = Object.entries(progress.tools || {});
  const finished = tools.filter(([,s]) => s === 'done').map(([t]) => toolName(t));
  const reading = tools.filter(([,s]) => s === 'reading').map(([t]) => toolName(t));
  const widths = ['82%','64%','90%','71%','58%','86%','69%','77%','60%'];
  $('#feed').innerHTML = `<div class="skeletons">${widths.map(w => `<div><div class="skel" style="width:38%"></div><div class="skel" style="width:${w}"></div></div>`).join('')}</div>`;
  $('#main').innerHTML = `<div class="loading-view" data-testid="loading"><div class="loading-card"><div class="with-icon"><span class="spin"></span>${esc(t('loading.title'))}</div><div class="loading-bar"><div style="width:${progress.total ? Math.round(progress.done/progress.total*100) : 0}%"></div></div><div class="loading-progress" data-testid="loading-progress">${esc(t('loading.progress', {done:progress.done || 0, total:progress.total || 0}))}${finished.length ? esc(t('loading.ready', {names:finished.join(', ')})) : ''}${reading.length ? esc(t('loading.reading', {names:reading.join(', ')})) : ''}</div></div></div>`;
  layout();
}
function renderTitle() {
  const s = sessionMap.get(ui.key); if (!s || !$('#session-top')) return;
  const custom = ui.state.names[s.key], pinned = ui.state.pins.includes(s.key);
  const starLabel = esc(t(pinned ? 'unpin' : 'pin')), renameLabel = esc(t('rename')), original = s.title || t('untitled');
  // В поле — само название: у сессии без названия поле пустое, подпись «Без названия» только в подсказке.
  $('#session-top').innerHTML = `${button('back-to-list',esc(t('back')),'link back')}${ui.editing ? `<form class="rename-form"><label for="rn" class="sr">${esc(t('rename.label'))}</label><input id="rn" class="rename" data-testid="rename-input" maxlength="200" value="${esc(custom || s.title)}" placeholder="${esc(t('untitled'))}"><button type="submit" class="btn" data-testid="rename-save">${esc(t('rename.save'))} ${kbd('Enter')}</button>${button('rename-cancel',esc(t('rename.cancel')))}${custom ? button('rename-reset',esc(t('rename.reset')),'link') : ''}</form>` : `<div class="title-line"><h1 data-testid="session-title">${esc(name(s))}</h1>${button('rename',icons.rename,'icon-btn',`aria-label="${renameLabel}" title="${renameLabel}"`)}${ui.state.done.includes(s.key) ? `<span class="pill ok">${esc(t('done'))}</span>` : ''}<div class="grow"></div>${button('pin',pinned ? starFilled : icons.star,'icon-btn',`aria-pressed="${pinned}" aria-label="${starLabel}" title="${starLabel}"`)}${button('session-menu',icons.menu,'icon-btn',`aria-label="${esc(t('session.actions'))}" aria-haspopup="menu" aria-expanded="${ui.menu === 'session'}"`)}<div id="session-popup"></div></div>`}
    ${custom ? `<div class="orig" data-testid="session-orig" title="${esc(original)}">${esc(t('session.original', {title:original}))}</div>` : ''}<div class="meta" data-testid="session-meta"><span class="with-icon">${toolIcon(s.tool,16)}${esc(toolName(s.tool))}</span><span class="meta-project">${esc(projectLabel(s.project))}</span>${s.branch ? `<span class="branch with-icon">${icons.branch}${esc(s.branch)}</span>` : ''}<span>${esc(dayLabel(s.updated, true))}, ${time(s.updated)}</span><span>${esc(s.by ? t('session.by', {by:s.by}) : t('session.by-me'))}</span></div>`;
  if (ui.editing) { $(test('rename-input')).focus(); $(test('rename-input')).select(); }
  renderMenus();
}
function renderLive() {
  const s = sessionMap.get(ui.key); if (!s || !$('#live-area')) return;
  const live = ui.live[s.key];
  $('#live-area').hidden = !live;
  $('#live-area').innerHTML = live ? `<div class="notice ${live.state}" data-testid="live-banner"><span class="dot ${live.state}"></span><span><b>${esc(t(live.state === 'wait' ? 'live.wait' : 'live.work'))}</b> · ${esc(live.where)}.${live.state === 'wait' ? esc(t('live.no-reopen')) : ''}</span></div>` : '';
}
function renderSummary() {
  const s = sessionMap.get(ui.key); if (!s || !$('#summary')) return;
  const summary = ui.state.summaries[s.key], job = ui.jobs.get(s.key);
  // Иконка и подпись — агента, который составил сводку; во время работы — агента из событий прогресса.
  const agent = job?.running || job?.error ? job.agent || plannedRunner() : summary ? summary.model || '' : plannedRunner();
  const noRunner = !ui.config.runners.length ? `disabled title="${esc(noRunnerHint())}"` : '';
  // Ссылка на журнал агента: пока сводка составляется и после, пока сервер помнит задание.
  const logId = `summary:${s.key}`, log = logs.get(logId);
  const logLink = log && !log.gone && (log.live.size || (log.loaded && log.entries.length)) ? button('summary-log',esc(t('log.open-summary')),'link') : '';
  let content;
  if (job?.running) content = `<div class="summary-progress" data-testid="summary-progress"><span class="spin"></span><span class="progress-copy" aria-live="polite">${agent ? `${esc(toolName(agent))} ` : ''}${esc(job.text || t('starting'))}<span class="elapsed">${Math.floor((job.elapsed || 0)/60)}:${String(Math.floor((job.elapsed || 0)%60)).padStart(2,'0')}</span></span><div class="segments">${Array.from({length:Math.min(job.steps || 7,30)},(_,i) => `<span class="seg ${i+1 < job.step ? 'done' : i+1 === job.step ? 'now' : ''}"></span>`).join('')}</div></div>${logLink}`;
  else if (job?.error) content = `<div class="summary-error" data-testid="summary-error" role="alert">${esc(t('summary.failed', {error:job.error}))}${job.code !== undefined && job.code !== null ? esc(t('summary.code', {code:job.code})) : ''}${job.output ? `<pre class="error-output">${esc(job.output)}</pre>` : ''}</div>${logLink}${button('summary-retry',esc(t('retry')),'btn sm',noRunner)}${choiceButton('summary')}`;
  else if (summary) content = `<span class="ellip ${ui.summaryOpen ? 'summary-when' : ''}" ${!ui.summaryOpen ? 'data-testid="summary-text"' : ''}>${ui.summaryOpen ? `${esc(summary.model ? t('summary.by', {agent:toolName(summary.model)}) : t('summary.label'))}${summary.at ? ` · ${esc(dayLabel(summary.at, true))}, ${time(summary.at)}` : ''}` : esc(summary.summary)}</span>${logLink}${button('summary-toggle',esc(t(ui.summaryOpen ? 'summary.collapse' : 'summary.expand')),'link')}`;
  else if (s.empty) content = `<span class="ellip summary-idle">${esc(t('summary.empty-log'))}</span>`;
  else content = `<span class="ellip summary-idle" ${noRunner ? 'data-testid="summary-no-runner"' : ''}>${esc(noRunner ? noRunnerHint() : t('summary.none'))}</span>${button('summary-run',esc(t('summary.run')),'btn sm',noRunner)}${choiceButton('summary')}`;
  let html = `<div class="ctx-row" data-testid="summary-row"><span class="ctx-label with-icon">${agent ? toolIcon(agent,14) : ''}${esc(t('summary.about'))}</span>${content}</div>`;
  if (summary && !job?.running && !job?.error) {
    if (ui.summaryOpen) html += `<div class="ctx-row summary-body"><p data-testid="summary-text">${esc(summary.summary)}</p><div class="actions"><span data-testid="summary-title">${esc(t('summary.title', {title:summary.title}))}</span>${ui.state.names[s.key] === summary.title ? `<span class="ok-text" data-testid="summary-applied">${esc(t('summary.applied'))}</span>` : button('summary-apply',esc(t('summary.apply')),'btn sm')}<span class="grow"></span>${button('summary-refresh',esc(t('summary.refresh')),'link',noRunner)}${choiceButton('summary')}</div></div>`;
    if (summary.next_step && !ui.state.done.includes(s.key)) html += `<div class="ctx-row" data-testid="next-step"><span class="ctx-label">${esc(t('summary.next'))}</span><span class="ellip" title="${esc(summary.next_step)}">${esc(summary.next_step)}</span>${button('copy-next',esc(t('summary.copy-next')),'btn sm',`title="${esc(t('summary.copy-next-title'))}"`)}</div>`;
    if (summary.closed && !ui.state.done.includes(s.key)) html += `<div class="ctx-row" data-testid="closed-hint"><span class="ctx-label">${esc(t('summary.status'))}</span><span class="ellip">${esc(summary.model ? t('summary.closed-by', {agent:toolName(summary.model)}) : t('summary.closed'))}</span>${button('summary-done',esc(t('menu.done')),'btn sm')}</div>`;
  }
  const related = summary ? (summary.related || []).filter(r => sessionMap.has(r.key)) : [];
  if (related.length) html += `<div class="ctx-row"><span class="ctx-label">${esc(t('summary.related'))}</span><div class="related">${related.map(r => `<button class="chip-s" data-testid="related-chip" data-key="${esc(r.key)}" title="${esc(r.why)}">${toolIcon(sessionMap.get(r.key).tool,14)}<span class="t">${esc(name(sessionMap.get(r.key)))}</span><span class="why">· ${esc(r.why)}</span></button>`).join('')}</div></div>`;
  $('#summary').innerHTML = html;
  renderChoices();
  // Сводка составлена раньше: если сервер ещё помнит задание, журнал можно собрать из его событий.
  if (summary && !job && !log && jobsChecked) loadLog(logId, {key:s.key});
}
function renderConversation() {
  const s = sessionMap.get(ui.key), container = $(test('conversation')); if (!s || !container) return;
  const data = ui.messages.get(s.key);
  if (!data) { container.innerHTML = `<div class="conversation-empty">${esc(t('conv.loading'))}</div>`; return; }
  if (data.error) { container.innerHTML = `<div class="conversation-empty" role="alert">${esc(data.error)} ${button('conversation-retry',esc(t('retry')),'link')}</div>`; return; }
  $('#conversation-note').textContent = data.messages.length ? t('conv.note', {n:data.messages.length}) + (data.truncated ? t('conv.truncated') : '') : '';
  const query = ui.inConv ? ui.q.trim() : '';
  container.innerHTML = `<div>${data.messages.length ? data.messages.map((m,i) => {
    const hit = query && m.text.toLocaleLowerCase().includes(query.toLocaleLowerCase());
    const clamped = m.text.length > 360 && !ui.expanded.has(`${s.key}:${i}`) && !hit;
    return `<div class="msg" data-testid="message" data-role="${esc(m.role)}"><div class="speaker">${m.role === 'user' ? `<span class="me">${esc(t('conv.me'))}</span>` : `<span class="with-icon">${toolIcon(s.tool,14)}${esc(toolName(s.tool))}</span>`}<time>${m.at ? time(new Date(m.at)/1000) : ''}</time></div><div class="row-content"><div class="msg-text ${clamped ? 'is-clamped' : ''}">${highlighted(m.text, query)}</div>${m.text.length > 360 ? button('message-toggle',esc(t(ui.expanded.has(`${s.key}:${i}`) ? 'conv.collapse' : 'conv.expand')),'link',`data-index="${i}"`) : ''}</div></div>`;
  }).join('') : `<div class="conversation-empty" data-testid="conversation-empty">${esc(t('conv.empty'))}<br>${esc(t('conv.empty-note'))}</div>`}</div>`;
}
async function loadConversation(key) {
  try { const data = await api(`/api/session?${new URLSearchParams({key})}`); ui.messages.set(key,data); }
  catch (error) { ui.messages.set(key,{error:error.message}); }
  if (ui.key === key) renderConversation();
}
function commandMarkup(s) {
  const prefix = s.command.slice(0, s.command.length - s.command_no_cd.length);
  const cd = prefix ? `<span class="muted">cd -- </span>${esc(prefix.slice(6,-4))}<span class="muted"> &amp;&amp; </span>` : '';
  // ID выделяется там, где он стоит в команде: после него могут идти флаги из настроек агента.
  const command = s.command_no_cd, at = command.indexOf(s.id);
  if (at < 0) return cd + esc(command);
  return cd + esc(command.slice(0,at)) + `<span class="prompt">${esc(s.id)}</span>` + esc(command.slice(at + s.id.length));
}
function commandBlock(s) {
  if (!s.command_no_cd) return `<div class="command-box no-command" data-testid="resume-hint">${icons.info}<span>${esc(s.resume_hint || t('command.none'))}</span></div>`;
  return `<div class="command-box" data-testid="command" data-command="${esc(s.command)}"><code><span class="prompt">$ </span>${commandMarkup(s)}</code><div class="command-actions">${button('copy-command',`${esc(t('copy'))} ${kbd('C')}`,'btn btn-primary','aria-live="polite"')}</div></div>`;
}
function renderSession() {
  const s = sessionMap.get(ui.key);
  if (!s) { $('#main').innerHTML = `<div class="loading-view muted">${esc(t('session.select'))}</div>`; return; }
  $('#main').innerHTML = `<section class="session" data-testid="session" data-key="${esc(s.key)}"><div class="session-top" id="session-top"></div><div class="command-area">${commandBlock(s)}<div id="live-area"></div>${s.missing && s.command_no_cd ? `<div class="notice missing" data-testid="missing-banner" role="alert">${warnIcon}<span>${esc(t('command.missing'))}</span>${button('copy-no-cd',esc(t('command.copy-no-cd')),'btn sm')}</div>` : ''}</div><section class="ctx" id="summary" aria-label="${esc(t('summary.aria'))}"></section><div class="conversation-heading"><strong>${esc(t('conv.heading'))}</strong><small id="conversation-note"></small></div><div class="conversation scroll" data-testid="conversation"></div></section>`;
  renderTitle(); renderLive(); renderSummary(); renderConversation();
  if (!ui.messages.has(s.key)) loadConversation(s.key);
}
function renderMenus() {
  renderChoices();
  $(test('data-menu')).setAttribute('aria-expanded',ui.menu === 'data');
  $('#data-popup').innerHTML = ui.menu === 'data' ? `<div class="menu" role="menu"><a role="menuitem" data-testid="data-export" href="/api/export" download>${esc(t('data.export'))}</a>${button('data-import',esc(t('data.import')),'','role="menuitem"')}</div>` : '';
  const popup = $('#session-popup'), s = sessionMap.get(ui.key);
  if (!popup || !s) return;
  $(test('session-menu')).setAttribute('aria-expanded',ui.menu === 'session');
  const item = (id, key, extra = '') => button(id, esc(t(key)), '', `role="menuitem" ${extra}`);
  popup.innerHTML = ui.menu === 'session' ? `<div class="menu" role="menu" aria-label="${esc(t('session.actions'))}">${button('menu-copy-id',`${esc(t('menu.copy-id'))}<span class="hint">${esc(s.id.slice(0,8))}…</span>`,'','role="menuitem"')}${item('menu-copy-path','menu.copy-path')}${env().wsl ? item('menu-explorer','menu.explorer',s.missing ? 'disabled' : '') : ''}${env().code ? item('menu-vscode','menu.vscode',s.missing ? 'disabled' : '') : ''}<div class="sep"></div>${item('menu-done',ui.state.done.includes(s.key) ? 'menu.undone' : 'menu.done')}${item('menu-hide',ui.state.hidden.includes(s.key) ? 'menu.unhide' : 'menu.hide')}</div>` : '';
}
function closeMenu() { ui.menu = ''; renderMenus(); }
function pickedSessions() { return ui.sessions.filter(s => ui.picked.has(s.key)); }
// В скрипт идут сессии, которые ещё не открыты и которые можно продолжить из терминала.
function restoreKeys() { return pickedSessions().filter(s => !ui.live[s.key] && s.command_no_cd).map(s => s.key); }
let scriptRequest = 0;
async function loadScript() {
  const revision = ++scriptRequest, keys = restoreKeys(); ui.script = null;
  if (!keys.length) {
    const live = pickedSessions().filter(s => ui.live[s.key]).length;
    ui.script = t(live === pickedSessions().length ? 'restore.all-open' : live ? 'restore.open-or-blocked' : 'restore.all-blocked');
    if ($(test('script'))) $(test('script')).textContent = ui.script;
    return;
  }
  try {
    const script = await api(`/api/restore-script?${new URLSearchParams({keys:keys.join(','),fmt:ui.fmt})}`,undefined,{text:true});
    if (revision !== scriptRequest) return;
    ui.script = script;
    if ($(test('script'))) $(test('script')).textContent = script;
    if ($(test('copy-script'))) $(test('copy-script')).disabled = false;
  } catch (error) {
    if (revision === scriptRequest && $(test('script'))) $(test('script')).textContent = error.message;
  }
}
function renderRestore() {
  ui.script = null; scriptRequest++;
  const picked = pickedSessions(), open = picked.filter(s => ui.live[s.key]).length;
  const blocked = picked.filter(s => !ui.live[s.key] && !s.command_no_cd).length, wsl = env().wsl;
  const notes = {
    wt: t('restore.note-wt'),
    tmux: (wsl ? t('restore.note-wsl') : '') + t('restore.note-tmux', {name:ui.config.settings?.tmux_session || 'nit'}),
    plain: t(wsl ? 'restore.note-plain-wsl' : 'restore.note-plain'),
  };
  const pill = (cls, key) => `<span class="pill ${cls}">${esc(t(key))}</span>`;
  const badges = s => (ui.live[s.key] ? pill('ok','restore.badge-open') : '') + (!s.command_no_cd ? pill('warn','restore.badge-blocked') : s.missing ? pill('warn','restore.badge-missing') : '');
  const note = t('restore.picked', {n:picked.length}) + (open ? ` · ${t('restore.open', {n:open})}` : '') + (blocked ? ` · ${t('restore.blocked', {n:blocked})}` : '');
  $('#main').innerHTML = `<section class="restore" data-testid="restore"><div class="restore-heading">${button('back-to-list',esc(t('back')),'link back')}<h1>${esc(t('restore.title'))}</h1><span class="restore-note">${esc(note)}</span><div class="grow"></div>${button('pick-done',`${esc(t('close'))} ${kbd('Esc')}`)}</div>${!picked.length ? `<div class="conversation-empty">${esc(t('restore.empty'))}<br>${button('pick-yesterday',esc(t('restore.pick-yesterday')),'link')}</div>` : `<div class="picked-list scroll">${picked.map(s => `<div class="pick-row" data-testid="picked-row" data-key="${esc(s.key)}">${toolIcon(s.tool,18)}<span class="pick-project">${esc(projectLabel(s.project))}</span><span class="pick-title">${esc(name(s))}</span><span class="pick-badges">${badges(s)}</span>${button('pick-remove',icons.close,'icon-btn',`aria-label="${esc(t('restore.remove'))}" data-key="${esc(s.key)}"`)}</div>`).join('')}</div><div class="actions"><span class="restore-note">${esc(t('restore.how'))}</span><div class="seg-ctl" role="group" aria-label="${esc(t('restore.format'))}">${formats().map(fmt => button(`fmt-${fmt}`,esc(t(`restore.fmt-${fmt}`)),'',`aria-pressed="${ui.fmt === fmt}"`)).join('')}</div><span class="grow"></span>${button('copy-script',`${esc(t('restore.copy'))} ${kbd('C')}`,'btn btn-primary','disabled')}</div><pre class="script scroll" data-testid="script">${esc(t('restore.preparing'))}</pre><div class="script-note">${esc(notes[ui.fmt])}</div>`}</section>`;
  if (picked.length) loadScript();
}
function renderMain() { if (ui.ready) ui.picking ? renderRestore() : agentShown() ? renderAgent() : renderSession(); layout(); }
function select(key, show = true, replace = false) {
  if (!sessionMap.has(key)) return;
  const changed = ui.key !== key;
  const fromAgent = agentShown();
  ui.key = key; ui.editing = false; ui.summaryOpen = false; ui.menu = ''; ui.picking = false; ui.log = ''; ui.cursor = '';
  if (show || ui.view === 'agent') ui.view = 'session';
  writeHash(replace); renderFeed();
  if (changed || fromAgent || !$(test('session'))) renderMain(); else { renderTitle(); renderSummary(); layout(); }
}
let stateWrite = Promise.resolve();
function change(path, body) {
  // Снимки состояния применяются последовательно, включая чтение после завершения сводки.
  const pending = stateWrite.then(async () => {
    ui.state = await api(path,body);
    renderFeed(); if (!ui.editing) renderTitle(); renderSummary();
  });
  stateWrite = pending.catch(() => {});
  return pending;
}
async function saveName(reset = false) {
  const key = ui.key, input = $(test('rename-input')), value = reset ? null : input.value.trim();
  await change('/api/name',{key,name:value});
  if (input.isConnected) { ui.editing = false; renderTitle(); }
}
async function mark(field) {
  const key = ui.key, collection = field === 'done' ? 'done' : 'hidden', value = !ui.state[collection].includes(key);
  const path = field === 'done' ? '/api/done' : '/api/hide';
  await change(path,{key,[collection]:value}); closeMenu();
  const message = field === 'done' ? value ? 'toast.done' : 'toast.reopened' : value ? 'toast.hidden' : 'toast.unhidden';
  toast(t(message), async () => { await change(path,{key,[collection]:!value}); toast(t('toast.undone')); });
}
async function startSummary() {
  const key = ui.key, id = `summary:${key}`;
  if (ui.jobs.get(key)?.running) return;
  // Новый запуск — то же задание summary:<ключ>: прежний журнал и ответы в нём забываются.
  resetLog(id, 'summary', {key});
  ui.jobs.set(key,{running:true}); renderSummary();
  try { const {job} = await api('/api/summary',{key, ...choiceBody('summary')}); watchJob(job,key); }
  catch (error) { ui.jobs.set(key,{error:error.message}); if (ui.key === key) renderSummary(); }
}
// Поток событий задания (NDJSON): handle — на каждое событие, chunk — после каждой порции.
async function readEvents(response, handle, chunk = () => {}) {
  const reader = response.body.getReader(), decoder = new TextDecoder(); let buffer = '';
  while (true) {
    const {value,done} = await reader.read();
    buffer += decoder.decode(value,{stream:!done});
    const lines = buffer.split('\n'); buffer = lines.pop();
    if (done && buffer.trim()) lines.push(buffer);
    for (const line of lines) if (line.trim()) await handle(JSON.parse(line));
    chunk();
    if (done) break;
  }
}
async function watchJob(id,key) {
  if (ui.jobs.get(key)?.connected) return;
  const state = {...ui.jobs.get(key),running:true,connected:true,since:0}; ui.jobs.set(key,state);
  const log = ensureLog(id, 'summary', {key}); log.live.add(id);
  if (ui.key === key) renderSummary();
  logChanged(log);
  const handle = async (event) => {
    logEvent(log, id, event);
    state.since = event.n;
    if (event.type === 'progress') Object.assign(state,event);
    if (event.type === 'result') { await change('/api/state'); state.running = false; }
    if (event.type === 'error') Object.assign(state,{running:false,error:event.message,code:event.code,output:event.output});
    if (ui.key === key) renderSummary();
  };
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(id)}/events?since=${state.since}`);
    if (!response.ok) throw new Error(t('summary.connect-failed', {status:response.status}));
    await readEvents(response, handle);
    // Остановленная из журнала сводка просто заканчивается: блок «О чём» возвращается к прежнему виду.
    if (state.running && state.cancelRequested) state.running = false;
    if (state.running) throw new Error(t('summary.lost'));
  } catch (error) { Object.assign(state,{running:false,error:error.message}); }
  finally {
    state.connected = false; logFinished(log, id);
    if (ui.key === key) renderSummary();
  }
}
async function resumeJobs() {
  let jobs = [];
  try { jobs = await api('/api/jobs'); }
  catch { /* Недоступность заданий не мешает читать журнал. Ошибка запуска видна в блоке сводки. */ }
  jobsChecked = true;
  for (const job of jobs) if (job.kind === 'summary') watchJob(job.job,job.key);
  resumeSmart(jobs); resumeFull(jobs);
  // Законченные задания из прошлой загрузки страницы: журнал собирается из событий, пока сервер помнит задание.
  if (ui.smart?.job && !ui.smart.running && !logs.has(ui.smart.job)) loadLog(ui.smart.job, {q:ui.smart.q});
  if (agentShown()) renderMain(); else renderSummary();
}
function resumeSmart(jobs) {
  // Умный поиск после перезагрузки: идущий на сервере или запущенный с этой вкладки и, возможно, уже готовый.
  const running = jobs.find(job => job.kind === 'search'), pending = stored(SMART_JOB), id = running?.job || pending?.job;
  if (!id) { stored(SMART_JOB, null); return; }
  // Запрос записан до ответа сервера: после быстрой перезагрузки номера задания в записи ещё нет.
  const known = pending && (!pending.job || pending.job === id) ? pending : {};
  const state = {job:id, q:known.q || running?.q || '', scope:known.scope || 'mine', started:running?.started || known.started || Date.now()/1000,
    running:true, warnings:[], since:0, resumed:true};
  ui.smart = state; renderSmart(); renderFeed(); watchSmart(state);
}

// Умный поиск: агент разбирает запрос, сервер ищет слова в журналах, агент отбирает сессии.
const SMART_JOB = 'nit.smart.job', SMART_RESULT = 'nit.smart.result';
function stored(name, value) {
  try {
    if (value === undefined) return JSON.parse(sessionStorage.getItem(name) || 'null');
    if (value === null) sessionStorage.removeItem(name); else sessionStorage.setItem(name, JSON.stringify(value));
  } catch { /* Без sessionStorage поиск работает, но не переживает перезагрузку. */ }
  return null;
}
function smartSessions() { return ui.smart.result.results.filter(r => sessionMap.has(r.key)).map(r => sessionMap.get(r.key)); }
function planText(plan) {
  const parts = [t('smart.plan-terms', {terms:plan.terms.join(' · ')})];
  if (plan.agents?.length) parts.push(t('smart.plan-agents', {agents:plan.agents.map(toolName).join(', ')}));
  if (plan.projects?.length) parts.push(t('smart.plan-projects', {projects:plan.projects.join(', ')}));
  if (plan.days) parts.push(t('smart.plan-days', {n:plan.days}));
  return parts.join(' ');
}
// Строки-действия над лентой, как в палитре команд: полный поиск без агента и вопрос агенту с выбором агента
// и модели. «↵» — у полного поиска, когда обычный ничего не нашёл; стрелки выделяют строки вместе с лентой.
function feedActions(found) {
  const q = ui.q.trim();
  if (!q || ui.section === 'projects') return '';
  const long = q.length > 500, off = !ui.config.runners.length || long;
  const hint = !ui.config.runners.length ? noRunnerHint() : long ? t('smart.too-long') : t('smart.hint');
  return `<div class="feed-actions" data-testid="feed-actions"><button type="button" class="feed-action${ui.cursor === 'full' ? ' is-sel' : ''}" data-testid="search-full" ${long ? 'disabled' : ''}>${icons.search}<span class="feed-action-text">${esc(t('full.action', {q}))}</span>${ui.cursor === 'full' || (!ui.cursor && !found) ? kbd('↵') : ''}</button>`
    + `<div class="feed-action-row${ui.cursor === 'smart' ? ' is-sel' : ''}"><button type="button" class="feed-action" data-testid="smart-search" title="${esc(hint)}" ${off ? 'disabled' : ''}>${icons.spark}<span class="feed-action-text">${esc(t('smart.action', {q}))}</span>${kbd('Ctrl+↵')}</button>${choiceButton('search')}</div></div>`;
}
function feedActionIds() {
  if (!ui.q.trim() || ui.section === 'projects') return [];
  return ['full','smart'].filter(id => !$(test(id === 'full' ? 'search-full' : 'smart-search'))?.disabled);
}
function renderSmart() {
  const area = $('#smart-area'), st = ui.smart; if (!area) return;
  if (!st || (st.result && ui.section === 'projects')) { area.innerHTML = ''; return; }
  const agent = st.agent || plannedRunner('search');
  const title = st.q ? `<div class="smart-title">${esc(t('smart.heading', {q:st.q}))}</div>` : '';
  const plan = st.plan && st.running ? `<div class="smart-line" data-testid="smart-plan">${esc(planText(st.plan))}</div>` : '';
  const warnings = st.warnings.map(w => `<div class="smart-line quiet" data-testid="smart-warning">${esc(w)}</div>`).join('');
  // «Журнал агента» возвращает справа «сессию агента» этого поиска; пока она на экране, кнопка не нужна.
  const log = st.job && logs.get(st.job), onScreen = agentShown() && ui.log === st.job && (ui.view === 'agent' || !narrow());
  const logButton = log && !log.gone && !onScreen ? button('smart-log',esc(t('log.open')),'btn sm') : '';
  if (st.running) area.innerHTML = `<div class="smart-panel" data-testid="smart-progress">${title}<div class="smart-top">${agent ? toolIcon(agent,16) : ''}<span class="smart-text" aria-live="polite">${agent ? `${esc(toolName(agent))} ` : ''}${esc(st.text || t('starting'))}</span><span class="smart-elapsed">${minutes(Date.now()/1000 - st.started)}</span>${logButton}${button('smart-cancel',esc(t('smart.stop')),'btn sm')}</div>${plan}${warnings}</div>`;
  else if (st.error) {
    const close = esc(t('close'));
    area.innerHTML = `<div class="smart-panel is-error" data-testid="smart-error" role="alert">${title}<div class="smart-top"><span class="smart-text">${esc(t('smart.failed', {error:st.error}))}</span>${logButton}${button('smart-retry',esc(t('retry')),'btn sm',ui.config.runners.length ? '' : `disabled title="${esc(noRunnerHint())}"`)}${button('smart-close',icons.close,'icon-btn',`aria-label="${close}" title="${close}"`)}</div>${warnings}</div>`;
  } else {
    const count = smartSessions().length, back = esc(t('smart.back'));
    const by = !st.result.ranked ? t('smart.words') : st.agent ? t('smart.ranked-by', {agent:toolName(st.agent)}) : t('smart.ranked');
    area.innerHTML = `<div class="smart-panel smart-head" data-testid="smart-head"><div class="smart-top"><span class="smart-text"><strong>${esc(t('smart.heading', {q:st.q || st.result.query}))}</strong>${count ? `<small>${esc(t('smart.count', {n:count}))} · ${esc(by)}</small>` : ''}</span>${logButton}${button('smart-close',icons.close,'icon-btn',`aria-label="${back}" title="${back}"`)}</div>${warnings}</div>`;
  }
}
async function startSmart(q = ui.q.trim(), scope = ui.section === 'all' ? 'all' : 'mine') {
  if (!q || q.length > 500 || !ui.config.runners.length) return;
  // Умный и полный поиск показывают результат в одной ленте: новый заменяет прежний.
  if (ui.full) { if (ui.full.running) await cancelFull().catch(() => {}); closeFull(); }
  const state = {q, scope, started:Date.now()/1000, running:true, warnings:[], since:0};
  ui.smart = state; stored(SMART_RESULT, null); stored(SMART_JOB, {q, scope, started:state.started});
  renderSmart(); renderFeed(true);
  try {
    state.job = (await api('/api/smart-search',{q, scope, ...choiceBody('search')})).job;
    if (ui.smart !== state) return;
    stored(SMART_JOB, {job:state.job, q, scope, started:state.started});
    if (state.cancelRequested) await cancelSmart();
    watchSmart(state);
    // Справа — «сессия агента» этого поиска. В узком окне на экране остаётся список с ходом поиска.
    if (narrow()) { ui.log = state.job; writeHash(true); renderMain(); } else openLog(state.job);
  } catch (error) {
    Object.assign(state, {running:false, error:error.message});
    if (ui.smart === state) renderSmart();
  }
}
async function watchSmart(state) {
  const current = () => ui.smart === state;
  const log = ensureLog(state.job, 'search', {q:state.q}); log.live.add(state.job); logChanged(log);
  let fresh = false;
  const handle = (event) => {
    logEvent(log, state.job, event);
    state.since = event.n;
    if (event.type === 'progress') Object.assign(state, {text:event.text, agent:event.agent});
    if (event.type === 'plan') state.plan = event;
    if (event.type === 'candidates') { state.candidates = event.results || []; fresh = true; }
    if (event.type === 'warning') state.warnings.push(event.message);
    if (event.type === 'result') Object.assign(state, {running:false, result:event.result});
    if (event.type === 'error') Object.assign(state, {running:false, error:event.message});
  };
  let ended = false;
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(state.job)}/events?since=${state.since}`);
    if (response.status === 404 && state.resumed) { ended = true; log.gone = true; }
    else {
      if (!response.ok) throw new Error(t('smart.connect-failed', {status:response.status}));
      await readEvents(response, handle, () => {
        if (!current()) return;
        // Кандидаты и результат меняют ленту, остальное — только панель над ней.
        if (state.result || fresh) { fresh = false; renderFeed(true); } else renderSmart();
      });
      ended = state.running && state.cancelRequested;
      if (state.running && !ended) throw new Error(t('smart.stopped'));
    }
  } catch (error) {
    // Обрыв связи (в том числе при перезагрузке страницы) не снимает задание: запись нужна, чтобы подключиться снова.
    state.lost = error instanceof TypeError || error.name === 'AbortError';
    Object.assign(state, {running:false, error:state.lost ? t('smart.lost') : error.message});
  } finally { logFinished(log, state.job); }
  if (!current()) return;
  if (!state.lost) stored(SMART_JOB, null);
  if (ended) ui.smart = null;
  else if (state.result) stored(SMART_RESULT, {q:state.q, scope:state.scope, agent:state.agent, warnings:state.warnings, result:state.result, job:state.job});
  renderSmart(); renderFeed(!!state.result);
}
async function cancelSmart() {
  const state = ui.smart; if (!state?.running) return;
  state.cancelRequested = true;
  const log = logs.get(state.job); if (log) log.stopping = true;
  if (state.job) await api(`/api/jobs/${encodeURIComponent(state.job)}/cancel`,{});
}
function closeSmart() { ui.smart = null; stored(SMART_RESULT, null); stored(SMART_JOB, null); renderSmart(); renderFeed(true); }

// Полный поиск без агента: все слова запроса по журналам целиком, вместе с выводом команд.
// Как и умный поиск, переживает перезагрузку: задание и результат лежат в sessionStorage.
const FULL_JOB = 'nit.full.job', FULL_RESULT = 'nit.full.result';
function fullSessions() { return ui.full.result.results.filter(r => sessionMap.has(r.key)).map(r => sessionMap.get(r.key)); }
function renderFull() {
  const area = $('#full-area'), st = ui.full; if (!area) return;
  if (!st || ui.section === 'projects') { area.innerHTML = ''; return; }
  if (st.running) {
    const percent = st.total ? Math.min(100, Math.round(st.done / st.total * 100)) : 0;
    const text = st.total ? t('full.progress', {q:st.q, done:st.done, total:st.total}) : t('full.starting', {q:st.q});
    area.innerHTML = `<div class="smart-panel" data-testid="full-progress"><div class="smart-top"><span class="smart-text" aria-live="polite">${esc(text)}</span><span class="smart-elapsed">${minutes(Date.now()/1000 - st.started)}</span>${button('full-cancel',esc(t('smart.stop')),'btn sm')}</div><div class="full-bar" data-testid="full-bar"><div style="width:${percent}%"></div></div></div>`;
  } else if (st.error) {
    const close = esc(t('close'));
    area.innerHTML = `<div class="smart-panel is-error" data-testid="full-error" role="alert"><div class="smart-top"><span class="smart-text">${esc(t('full.failed', {error:st.error}))}</span>${button('full-retry',esc(t('retry')),'btn sm')}${button('full-close',icons.close,'icon-btn',`aria-label="${close}" title="${close}"`)}</div></div>`;
  } else {
    const count = fullSessions().length, back = esc(t('smart.back'));
    area.innerHTML = `<div class="smart-panel smart-head" data-testid="full-head"><div class="smart-top"><span class="smart-text"><strong>${esc(t('full.heading', {q:st.q || st.result.query}))}</strong>${count ? `<small>${esc(t('smart.count', {n:count}))}</small>` : ''}</span>${button('full-close',icons.close,'icon-btn',`aria-label="${back}" title="${back}"`)}</div></div>`;
  }
}
async function startFull(q = ui.q.trim(), scope = ui.section === 'all' ? 'all' : 'mine') {
  if (!q || q.length > 500) return;
  if (ui.smart) { if (ui.smart.running) await cancelSmart().catch(() => {}); closeSmart(); }
  const state = {q, scope, started:Date.now()/1000, running:true, done:0, total:0, since:0};
  ui.full = state; stored(FULL_RESULT, null); stored(FULL_JOB, {q, scope, started:state.started});
  renderFeed(true);
  try {
    state.job = (await api('/api/full-search', {q, scope})).job;
    if (ui.full !== state) return;
    stored(FULL_JOB, {job:state.job, q, scope, started:state.started});
    if (state.cancelRequested) await cancelFull();
    watchFull(state);
  } catch (error) {
    Object.assign(state, {running:false, error:error.message});
    if (ui.full === state) renderFeed();
  }
}
async function watchFull(state) {
  const current = () => ui.full === state;
  const handle = (event) => {
    state.since = event.n;
    if (event.type === 'progress') Object.assign(state, {done:event.done || 0, total:event.total || 0});
    if (event.type === 'result') Object.assign(state, {running:false, result:event.result});
    if (event.type === 'error') Object.assign(state, {running:false, error:event.message});
  };
  let ended = false;
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(state.job)}/events?since=${state.since}`);
    if (response.status === 404 && state.resumed) ended = true;
    else {
      if (!response.ok) throw new Error(t('smart.connect-failed', {status:response.status}));
      await readEvents(response, handle, () => { if (current()) state.result ? renderFeed(true) : renderFull(); });
      ended = state.running && state.cancelRequested;
      if (state.running && !ended) throw new Error(t('smart.stopped'));
    }
  } catch (error) {
    state.lost = error instanceof TypeError || error.name === 'AbortError';
    Object.assign(state, {running:false, error:state.lost ? t('smart.lost') : error.message});
  }
  if (!current()) return;
  if (!state.lost) stored(FULL_JOB, null);
  if (ended) ui.full = null;
  else if (state.result) stored(FULL_RESULT, {q:state.q, scope:state.scope, result:state.result});
  renderFeed(!!state.result);
}
async function cancelFull() {
  const state = ui.full; if (!state?.running) return;
  state.cancelRequested = true;
  if (state.job) await api(`/api/jobs/${encodeURIComponent(state.job)}/cancel`,{});
}
function closeFull() { ui.full = null; stored(FULL_RESULT, null); stored(FULL_JOB, null); renderFeed(true); }
function resumeFull(jobs) {
  const running = jobs.find(job => job.kind === 'fullsearch'), pending = stored(FULL_JOB), id = running?.job || pending?.job;
  if (!id) { stored(FULL_JOB, null); return; }
  const known = pending && (!pending.job || pending.job === id) ? pending : {};
  const state = {job:id, q:known.q || running?.q || '', scope:known.scope || 'mine', started:running?.started || known.started || Date.now()/1000,
    running:true, done:0, total:0, since:0, resumed:true};
  ui.full = state; renderFeed(); watchFull(state);
}

// Журнал агента — «сессия агента» справа, на месте переписки: что агент получил, думал, писал, какие
// инструменты вызывал. События копят уже существующие подписки на задания (watchJob, watchSmart),
// ответы агенту — followJob; ответ дописывается в тот же журнал. Ключ журнала — id задания поиска или сводки.
const REPLIES = 'nit.log.replies', LONG_ENTRY = 360;
const logs = new Map();
// /api/jobs прочитан: про законченные задания уже можно спрашивать сервер, не открывая второй поток к идущим.
let jobsChecked = false;
function logKind(id) { return id.startsWith('summary:') ? 'summary' : 'search'; }
function ensureLog(id, kind = logKind(id), extra = {}) {
  let log = logs.get(id);
  if (!log) {
    log = {id, kind, key:kind === 'summary' ? id.slice(8) : '', q:'', entries:[], seen:{}, live:new Set(), session:null, replying:'',
      model:'', agent:'', t0:Date.now()/1000, last:0, loaded:false, loading:false, gone:false, stopping:false, draft:''};
    logs.set(id, log);
  }
  return Object.assign(log, extra);
}
function resetLog(id, kind, extra) {
  logs.delete(id); replyJobs(id, null);
  const log = ensureLog(id, kind, extra);
  if (shown(log)) renderAgent();
  return log;
}
// Ответы агенту по журналам: id задания поиска или сводки → [{job, text}], чтобы собрать журнал после перезагрузки.
// Своё сообщение страница показывает сама: если сервер не помнит сессию, задание ответа его не повторит.
function replyJobs(id, list) {
  const all = stored(REPLIES) || {};
  if (list === undefined) return all[id] || [];
  if (list === null) delete all[id]; else all[id] = list;
  stored(REPLIES, all);
  return list || [];
}
function shown(log) { return !!log && agentShown() && ui.log === log.id && !!$('#agent-entries'); }
function agentShown() { return !!ui.log && ui.view !== 'session' && !ui.picking; }
function logEvent(log, job, event, reply = false) {
  if (!event.n || event.n <= (log.seen[job] || 0)) return;
  log.seen[job] = event.n;
  // Сообщение человека в задании ответа уже в журнале — его добавила страница при отправке.
  if (reply && event.type === 'trace' && event.kind === 'prompt') return;
  if (typeof event.elapsed === 'number') { log.last = event.elapsed; log.t0 = Date.now()/1000 - event.elapsed; }
  let entry = null;
  if (event.type === 'trace') {
    if (event.kind === 'model' && event.text) log.model = event.text;
    if (event.agent && event.agent !== 'nit') log.agent = event.agent;
    entry = {kind:event.kind, text:String(event.text || ''), tool:event.tool || '', agent:event.agent || '', reply};
  } else if (event.type === 'agent_session') log.session = {agent:event.agent, session:event.session, command:event.command || ''};
  else if (event.type === 'progress' && event.agent && !log.agent) log.agent = event.agent;
  else if (event.type === 'warning' || event.type === 'error') entry = {kind:event.type, text:String(event.message || ''), code:event.code, output:event.output || ''};
  if (entry) pushLog(log, entry);
  logChanged(log);
}
function pushLog(log, entry) {
  log.entries.push(entry);
  if (!shown(log)) return;
  const list = $('#agent-entries');
  list.querySelector('.log-empty')?.remove();
  list.insertAdjacentHTML('beforeend', logEntry(entry));
}
function logChanged(log) {
  if (!shown(log)) return;
  renderAgentHead(); renderAgentFoot();
  const empty = $('#agent-entries .log-empty');
  if (empty) empty.textContent = t(log.gone ? 'log.gone' : log.live.size || log.loading ? 'log.waiting' : 'log.empty');
}
// Задание журнала закончилось: отметить, дописать «Остановлено», если его остановили отсюда.
function logFinished(log, job) {
  log.live.delete(job);
  if (log.replying === job) log.replying = '';
  if (log.stopping && !log.live.size) { log.stopping = false; pushLog(log, {kind:'stopped', text:''}); }
  if (job === log.id) log.loaded = true;
  logChanged(log);
}
// Прочитать события задания в журнал: идущего — до конца работы, законченного — сразу все.
async function followJob(log, job, reply = false) {
  log.live.add(job); logChanged(log);
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(job)}/events?since=${log.seen[job] || 0}`);
    if (response.status === 404) { if (job === log.id) log.gone = true; return false; }
    if (!response.ok) throw new Error(t('log.connect-failed', {status:response.status}));
    await readEvents(response, event => logEvent(log, job, event, reply));
    return true;
  } catch (error) {
    pushLog(log, {kind:'error', text:error.message});
    return false;
  } finally { logFinished(log, job); }
}
// Журнал законченного задания, например после перезагрузки страницы: события с начала и ответы агенту.
async function loadLog(id, extra) {
  const log = ensureLog(id, logKind(id), extra);
  if (log.loading || log.loaded || log.live.size) return log;
  log.loading = true;
  const found = await followJob(log, id);
  log.loading = false; log.loaded = true;
  if (log.kind === 'summary' && ui.key === log.key) renderSummary();
  if (log.kind === 'search' && ui.smart?.job === log.id) renderSmart();
  if (shown(log)) renderAgent();
  // Ответы агенту — по порядку; идущий ответ держит поток до конца, как при отправке.
  if (found) for (const {job, text} of replyJobs(id)) {
    pushLog(log, {kind:'prompt', text, reply:true}); log.replying = job;
    await followJob(log, job, true);
  }
  return log;
}
// Показать справа «сессию агента». Сводка возвращается к своей сессии кнопкой «← К сессии».
function openLog(id) {
  ui.log = id; ui.view = 'agent'; ui.picking = false; ui.editing = false; ui.menu = '';
  writeHash(); renderFeed(); renderMain();
}
function closeLog() {
  const log = logs.get(ui.log);
  if (log?.kind === 'summary' && sessionMap.has(log.key)) ui.key = log.key;
  ui.log = ''; ui.view = 'session'; writeHash(); renderFeed(); renderMain();
}
function agentTitle(log) {
  if (log?.kind === 'summary') { const s = sessionMap.get(log.key); return s ? t('log.summary', {title:name(s)}) : t('log.title'); }
  return log?.q ? t('smart.heading', {q:log.q}) : t('log.search');
}
function renderAgent() {
  let log = logs.get(ui.log);
  // Журнала ещё нет (перезагрузка, ссылка): собрать из событий задания, пока сервер его помнит.
  if (jobsChecked && (!log || (!log.loaded && !log.live.size && !log.loading))) {
    loadLog(ui.log, logKind(ui.log) === 'search' && ui.smart?.job === ui.log ? {q:ui.smart.q} : {});
    log = logs.get(ui.log);
  }
  const entries = log?.entries.length ? log.entries.map(logEntry).join('')
    : `<div class="log-empty">${esc(t(!log || log.gone ? (log?.loading || !jobsChecked ? 'log.waiting' : 'log.gone') : log.live.size || log.loading ? 'log.waiting' : 'log.empty'))}</div>`;
  $('#main').innerHTML = `<section class="session agent-session" data-testid="agent-session" data-log="${esc(ui.log)}"><div class="session-top" id="agent-top"></div><div class="conversation scroll" data-testid="agent-log"><div id="agent-entries">${entries}</div></div><div class="agent-reply" id="agent-reply" hidden></div></section>`;
  renderAgentHead(true); renderAgentFoot(true);
}
// Шапка: заголовок, агент, модель из trace model, идёт/закончено, время и «Остановить». Перерисовывается при смене состояния.
function renderAgentHead(force = false) {
  const top = $('#agent-top'); if (!top) return;
  const log = logs.get(ui.log), running = !!log?.live.size && !log.loading;
  const agent = log?.agent || (log ? plannedRunner(log.kind) : '');
  const state = [agentTitle(log), agent, log?.model, running, lang()].join('|');
  if (!force && top.dataset.state === state) { renderAgentTime(); return; }
  top.dataset.state = state;
  const back = log?.kind === 'summary' ? button('agent-back',esc(t('log.back-session')),'link agent-back') : button('back-to-list',esc(t('back')),'link back');
  top.innerHTML = `${back}<div class="title-line"><h1 data-testid="agent-title">${esc(agentTitle(log))}</h1><div class="grow"></div>${running ? button('agent-stop',esc(t('smart.stop')),'btn sm') : ''}</div>
    <div class="meta" data-testid="agent-meta">${agent ? `<span class="with-icon" data-testid="agent-name">${toolIcon(agent,16)}${esc(toolName(agent))}</span>` : ''}${log?.model ? `<span class="branch" data-testid="agent-model">${esc(log.model)}</span>` : ''}<span class="with-icon agent-state${running ? ' is-running' : ''}" data-testid="agent-state">${running ? '<span class="spin"></span>' : ''}${esc(t(running ? 'log.running' : 'log.finished'))}</span><span class="agent-time" data-testid="agent-time"></span></div>`;
  renderAgentTime();
}
function renderAgentTime() {
  const log = logs.get(ui.log), place = $(test('agent-time')); if (!log || !place) return;
  place.textContent = minutes(log.live.size && !log.loading ? Date.now()/1000 - log.t0 : log.last);
}
// Поле ответа: после agent_session, когда запуск закончился. Перерисовывается только при смене состояния,
// чтобы не терять набранный текст и фокус.
function renderAgentFoot(force = false) {
  const foot = $('#agent-reply'); if (!foot) return;
  const log = logs.get(ui.log), show = !!log?.session && !log.live.has(log.id) && !log.loading;
  const busy = show && !!log.replying;
  const state = show ? [busy, log.session.agent, log.session.session, log.session.command, lang()].join('|') : '';
  if (!force && foot.dataset.state === state) return;
  foot.dataset.state = state; foot.hidden = !show;
  if (!show) { foot.innerHTML = ''; return; }
  foot.innerHTML = `<div class="reply-form"><span class="prompt" aria-hidden="true">›</span><label for="log-reply" class="sr">${esc(t('reply.label'))}</label><textarea id="log-reply" data-testid="reply-input" rows="1" placeholder="${esc(t('reply.placeholder'))}" title="${esc(t('reply.hint'))}" ${busy ? 'disabled' : ''}>${esc(log.draft)}</textarea>${button('reply-send',esc(t('reply.send')),'btn sm',busy ? 'disabled' : '')}</div>${log.session.command ? `<div class="reply-terminal"><span>${esc(t('reply.terminal'))}</span><code data-testid="reply-command"><span class="prompt">$ </span>${esc(log.session.command)}</code>${button('reply-copy-command',esc(t('copy')),'btn sm')}</div>` : ''}`;
}
// Сообщение журнала — в оформлении переписки сессии (.msg); вызовы и результаты инструментов, строки «Нити»
// и расход токенов — компактными строками.
function logEntry(entry) {
  const text = esc(entry.text), long = entry.text.length > LONG_ENTRY;
  const agent = entry.agent && entry.agent !== 'nit' ? entry.agent : '';
  const message = (testid, speaker, cls = '') => `<div class="msg log-msg ${cls}" data-testid="${testid}"><div class="speaker">${speaker}</div><div class="row-content"><div class="msg-text${long ? ' is-clamped' : ''}">${text}</div>${long ? button('log-more',esc(t('conv.expand')),'link') : ''}</div></div>`;
  const line = (testid, body, cls = '') => `<div class="log-row ${cls}" data-testid="${testid}"><span></span><div class="log-line">${body}</div></div>`;
  const output = entry.output ? `<details class="log-details"><summary>${esc(t('log.output'))}</summary><pre class="log-pre">${esc(entry.output)}</pre></details>` : '';
  switch (entry.kind) {
    case 'prompt':
      if (entry.reply) return message('log-user', `<span class="me">${esc(t('conv.me'))}</span>`);
      return `<div class="msg log-msg" data-testid="log-prompt"><div class="speaker"><span class="nit-name">${esc(t('app.title'))}</span></div><div class="row-content"><details class="log-details"><summary>${esc(t('log.prompt'))}</summary><pre class="log-pre">${text}</pre></details></div></div>`;
    case 'thinking': return message('log-thinking', `<span class="muted">${esc(t('log.thinking'))}</span>`, 'is-thinking');
    case 'text': return message('log-text', agent ? `<span class="with-icon">${toolIcon(agent,14)}${esc(toolName(agent))}</span>` : '');
    case 'tool': return line('log-tool', `<span class="log-tool-name">${esc(entry.tool || '?')}</span> <code class="log-args">${text}</code>`, 'is-tool');
    case 'result': return line('log-result', `<details class="log-details"><summary>${esc(t('log.result'))}</summary><pre class="log-pre">${text}</pre></details>`);
    case 'model': return line('log-model', text, 'is-quiet');
    case 'usage': return line('log-usage', text, 'is-quiet');
    case 'nit': return line('log-nit', `<b>${esc(t('app.title'))}:</b> ${text}`, 'is-quiet');
    case 'warning': return line('log-warning', `${text}${output}`, 'is-warning');
    case 'error': return line('log-error', `${text}${entry.code !== undefined && entry.code !== null ? esc(t('summary.code', {code:entry.code})) : ''}${output}`, 'is-error');
    case 'stopped': return line('log-stopped', esc(t('log.stopped')), 'is-quiet');
    default: return entry.text ? line('log-other', text, 'is-quiet') : '';
  }
}
async function sendReply() {
  const log = logs.get(ui.log), input = $('#log-reply');
  if (!log?.session || log.replying || log.live.size || !input) return;
  const text = input.value.trim(); if (!text) return;
  const {job} = await api('/api/agent/reply', {agent:log.session.agent, session:log.session.session, text});
  log.draft = ''; log.replying = job;
  replyJobs(log.id, [...replyJobs(log.id), {job, text}]);
  pushLog(log, {kind:'prompt', text, reply:true});
  await followJob(log, job, true);
  if (shown(log)) $('#log-reply')?.focus();
}
// «Остановить» в шапке: ответ агенту, умный поиск или сводку — что сейчас идёт.
async function stopAgent() {
  const log = logs.get(ui.log); if (!log?.live.size) return;
  log.stopping = true;
  if (log.replying) return api(`/api/jobs/${encodeURIComponent(log.replying)}/cancel`,{});
  if (log.kind === 'search' && ui.smart?.job === log.id) return cancelSmart();
  const state = ui.jobs.get(log.key); if (state) state.cancelRequested = true;
  return api(`/api/jobs/${encodeURIComponent(log.id)}/cancel`,{});
}

// Выбор агента, модели и уровня рассуждений для умного поиска и для сводки. Хранится в localStorage
// отдельно для каждого; пустые значения — «Авто» и «как настроено в CLI», в запрос они не попадают.
const CHOICE = {search:'nit.choice.search', summary:'nit.choice.summary'}, choiceMemory = {};
const models = new Map();
function choiceOf(scope) {
  let value = choiceMemory[scope];
  if (!value) {
    try { value = JSON.parse(localStorage.getItem(CHOICE[scope]) || 'null'); } catch { /* Без localStorage выбор живёт до перезагрузки. */ }
  }
  value = value && typeof value === 'object' ? value : {};
  // Агента нет среди установленных — выбор не действует, пока агент не появится снова.
  if (!ui.config.runners.includes(value.agent)) return {agent:'', model:'', effort:''};
  return {agent:value.agent, model:String(value.model || ''), effort:String(value.effort || '')};
}
function saveChoice(scope, value) {
  choiceMemory[scope] = value;
  try { localStorage.setItem(CHOICE[scope], JSON.stringify(value)); } catch { /* См. choiceOf. */ }
}
function choiceBody(scope) { return Object.fromEntries(Object.entries(choiceOf(scope)).filter(([, value]) => value)); }
function choiceLabel(scope) {
  const choice = choiceOf(scope);
  return choice.agent ? [toolName(choice.agent), choice.model, choice.effort].filter(Boolean).join(' · ') : t('choice.auto');
}
const chevron = svg('<path d="M7 10l5 5 5-5"/>', 12, 2);
function choiceButton(scope) {
  return `<span class="menu-anchor choice-anchor">${button(`choice-${scope}`,`<span class="choice-label">${esc(choiceLabel(scope))}</span>${chevron}`,'toggle choice-btn',`aria-haspopup="menu" aria-expanded="${ui.menu === `choice-${scope}`}" title="${esc(t('choice.title'))}" ${ui.config.runners.length ? '' : 'disabled'}`)}</span>`;
}
// Модели и уровни рассуждений выбранного агента: уровни своей модели (Codex) или общие (Claude, Grok).
function effortsOf(data, model) {
  if (!data) return [];
  const found = (data.models || []).find(m => m.id === (model || data.default_model));
  return found?.efforts?.length ? found.efforts : data.efforts || [];
}
function defaultEffort(data, model) {
  const found = (data?.models || []).find(m => m.id === (model || data?.default_model));
  return data?.default_effort || found?.default_effort || '';
}
function choiceMenu(scope) {
  const choice = choiceOf(scope);
  const item = (kind, value, label, checked, hint = '') => `<button type="button" role="menuitemradio" aria-checked="${checked}" data-testid="choice-${kind}" data-value="${esc(value)}"><span class="choice-mark">${checked ? checkBox : ''}</span><span class="choice-text">${esc(label)}</span>${hint ? `<span class="hint">${esc(hint)}</span>` : ''}</button>`;
  const auto = plannedRunner(scope, true);
  let html = `<div class="menu-sec">${esc(t('choice.agent'))}</div>${item('agent', '', t('choice.auto'), !choice.agent, auto ? toolName(auto) : '')}`
    + ui.config.runners.map(id => item('agent', id, toolName(id), choice.agent === id)).join('');
  if (!choice.agent) html += `<div class="menu-note">${esc(t('choice.auto-note'))}</div>`;
  else {
    const entry = models.get(choice.agent) || {}, data = entry.data, list = data?.models || [];
    html += `<div class="sep"></div><div class="menu-sec">${esc(t('choice.model'))}</div>`
      + item('model', '', data?.default_model ? t('choice.cli-value', {value:data.default_model}) : t('choice.cli'), !choice.model);
    if (entry.loading && !data) html += `<div class="menu-note">${esc(t('choice.loading'))}</div>`;
    if (entry.error) html += `<div class="menu-note">${esc(t('choice.failed', {error:entry.error}))}</div>`;
    html += list.map(m => item('model', m.id, m.name || m.id, choice.model === m.id, m.name && m.name !== m.id ? m.id : '')).join('');
    if (choice.model && !list.some(m => m.id === choice.model)) html += item('model', choice.model, choice.model, true);
    const efforts = effortsOf(data, choice.model);
    if (efforts.length || choice.effort) {
      const fallback = defaultEffort(data, choice.model);
      html += `<div class="sep"></div><div class="menu-sec">${esc(t('choice.effort'))}</div>`
        + item('effort', '', fallback ? t('choice.cli-value', {value:fallback}) : t('choice.cli'), !choice.effort)
        + efforts.map(effort => item('effort', effort, effort, choice.effort === effort)).join('');
      if (choice.effort && !efforts.includes(choice.effort)) html += item('effort', choice.effort, choice.effort, true);
    }
  }
  return `<div class="menu choice-menu" role="menu" aria-label="${esc(t('choice.title'))}" data-testid="choice-menu" data-scope="${scope}">${html}</div>`;
}
// Подписи кнопок и меню. Меню всплывает над страницей (position: fixed): блок «О чём» прокручивается и обрезал бы его.
function renderChoices() {
  for (const scope of ['search','summary']) {
    const btn = $(test(`choice-${scope}`)); if (!btn) continue;
    btn.querySelector('.choice-label').textContent = choiceLabel(scope);
    btn.setAttribute('aria-expanded', ui.menu === `choice-${scope}`);
    btn.disabled = !ui.config.runners.length;
  }
  const popup = $('#choice-popup'); if (!popup) return;
  const scope = ui.menu.startsWith('choice-') ? ui.menu.slice(7) : '', anchor = scope && $(test(`choice-${scope}`));
  if (!anchor) { popup.innerHTML = ''; return; }
  popup.innerHTML = choiceMenu(scope);
  const rect = anchor.getBoundingClientRect(), menu = popup.firstElementChild;
  menu.style.top = `${Math.round(rect.bottom + 4)}px`;
  menu.style.left = `${Math.round(Math.max(8, Math.min(rect.left, innerWidth - menu.offsetWidth - 8)))}px`;
}
async function loadModels(agent) {
  const entry = models.get(agent) || {};
  Object.assign(entry, {loading:true, error:''}); models.set(agent, entry); renderChoices();
  try { entry.data = await api(`/api/models?${new URLSearchParams({agent})}`); }
  catch (error) { entry.error = error.message; }
  entry.loading = false; renderChoices();
}
function toggleChoice(scope) {
  ui.menu = ui.menu === `choice-${scope}` ? '' : `choice-${scope}`;
  renderMenus();
  const agent = choiceOf(scope).agent;
  if (ui.menu && agent) loadModels(agent);
}
function choose(kind, value) {
  const scope = ui.menu.slice(7), choice = choiceOf(scope);
  if (kind === 'agent') {
    saveChoice(scope, {agent:value, model:'', effort:''});
    if (value) loadModels(value);
  } else if (kind === 'model') {
    const efforts = effortsOf(models.get(choice.agent)?.data, value);
    saveChoice(scope, {...choice, model:value, effort:efforts.includes(choice.effort) ? choice.effort : ''});
  } else saveChoice(scope, {...choice, effort:value});
  renderChoices();
  if (scope === 'summary') renderSummary();
  if (scope === 'search') renderSmart();
}
async function pollLive() {
  try {
    const next = await api('/api/live'), changed = JSON.stringify(next) !== JSON.stringify(ui.live);
    ui.live = next;
    if (changed) { renderNav(); renderFeed(); renderLive(); if (ui.picking) renderRestore(); }
  } catch { /* Следующий опрос обновит статусы; команды обрабатывают собственные ошибки. */ }
}
function search() {
  ui.cursor = ''; clearTimeout(searchTimer); searchController?.abort(); ui.snippets.clear(); renderFeed(true);
  if ($(test('conversation'))) renderConversation();
  if (ui.inConv && ui.q.trim()) searchTimer = setTimeout(async () => {
    searchController = new AbortController();
    try {
      const hits = await api(`/api/search?${new URLSearchParams({q:ui.q.trim(),scope:ui.section === 'all' ? 'all' : 'mine'})}`,undefined,{signal:searchController.signal});
      ui.snippets = new Map(hits.map(h => [h.key,h.snippet])); renderFeed(true);
    } catch (error) { if (error.name !== 'AbortError') toast(error.message); }
  },200);
}
function togglePick(key) {
  ui.picked.has(key) ? ui.picked.delete(key) : ui.picked.add(key);
  renderFeed(); renderRestore();
}
function pickDay(offset) {
  ui.picking = true; ui.picked = new Set(dayPick(offset).map(s => s.key)); ui.view = 'session';
  renderFeed(); renderMain();
}
function exitPick() { ui.picking = false; ui.picked.clear(); renderFeed(); renderMain(); }
async function action(id, element) {
  const s = sessionMap.get(ui.key);
  switch (id) {
    case 'nav-mine': case 'nav-all': case 'nav-projects':
      ui.section = id.slice(4); ui.project = null; ui.view = 'list'; ui.picking = false; ui.editing = false;
      writeHash(); search(); renderMain(); break;
    case 'wait-next': {
      const waits = waiting();
      const next = waits[(waits.findIndex(s => s.key === ui.key) + 1) % waits.length];
      if (next) select(next.key); break;
    }
    case 'project-row': ui.project = element.dataset.project; ui.section = 'mine'; ui.view = 'list'; writeHash(); search(); layout(); break;
    case 'project-filter-clear': ui.project = null; writeHash(); search(); break;
    case 'row': ui.picking ? togglePick(element.dataset.key) : select(element.dataset.key); break;
    case 'related-chip': select(element.dataset.key); break;
    case 'back-to-list': ui.view = 'list'; writeHash(); layout(); renderSmart(); break;
    case 'search-conv': case 'search-more': ui.inConv = !ui.inConv; $(test('search-conv')).setAttribute('aria-pressed',ui.inConv); search(); renderConversation(); break;
    case 'search-all': ui.section = 'all'; writeHash(); search(); break;
    case 'search-clear': ui.q = ''; $('#q').value = ''; search(); break;
    case 'smart-search': await startSmart(); break;
    case 'smart-retry': await startSmart(ui.smart.q, ui.smart.scope); break;
    case 'smart-cancel': await cancelSmart(); break;
    case 'smart-close': closeSmart(); break;
    case 'smart-log': if (ui.smart?.job) openLog(ui.smart.job); break;
    case 'search-full': await startFull(); break;
    case 'full-cancel': await cancelFull(); break;
    case 'full-retry': await startFull(ui.full.q, ui.full.scope); break;
    case 'full-close': closeFull(); break;
    case 'summary-log': openLog(`summary:${s.key}`); break;
    case 'agent-back': closeLog(); break;
    case 'agent-stop': await stopAgent(); break;
    case 'log-more': {
      const clamped = element.previousElementSibling.classList.toggle('is-clamped');
      element.textContent = t(clamped ? 'conv.expand' : 'conv.collapse'); break;
    }
    case 'reply-send': await sendReply(); break;
    case 'reply-copy-command': { const log = logs.get(ui.log); if (log?.session?.command) await copy(log.session.command, element); break; }
    case 'choice-search': case 'choice-summary': toggleChoice(id.slice(7)); break;
    case 'choice-agent': case 'choice-model': case 'choice-effort': choose(id.slice(7), element.dataset.value); break;
    case 'toggle-hide-done': ui.hideDone = !ui.hideDone; renderFeed(true); break;
    case 'toggle-last': ui.showLast = !ui.showLast; renderFeed(); break;
    case 'feed-more': ui.limit += 50; renderFeed(); break;
    case 'data-menu': ui.menu = ui.menu === 'data' ? '' : 'data'; renderMenus(); break;
    case 'session-menu': ui.menu = ui.menu === 'session' ? '' : 'session'; renderMenus(); break;
    case 'data-import': closeMenu(); $(test('data-import-file')).click(); break;
    case 'migrate-button': await change('/api/migrate',legacyData()); toast(t('toast.migrated')); break;
    case 'rename': ui.editing = true; closeMenu(); renderTitle(); break;
    case 'rename-save': await saveName(); break;
    case 'rename-cancel': ui.editing = false; renderTitle(); break;
    case 'rename-reset': await saveName(true); break;
    case 'pin': await change('/api/pin',{key:s.key,pinned:!ui.state.pins.includes(s.key)}); break;
    case 'menu-done': case 'summary-done': await mark('done'); break;
    case 'menu-hide': await mark('hidden'); break;
    case 'copy-command': if (s.command) await copy(s.command,element); break;
    case 'copy-no-cd': await copy(s.command_no_cd,element); break;
    case 'menu-copy-id': await copy(s.id, null, t('toast.id-copied', {id:s.id})); closeMenu(); break;
    case 'menu-copy-path': await copy(s.cwd, null, t('toast.path-copied', {path:s.cwd})); closeMenu(); break;
    case 'menu-explorer': case 'menu-vscode': await api('/api/reveal',{key:s.key,app:id === 'menu-explorer' ? 'explorer' : 'vscode'}); closeMenu(); break;
    case 'summary-run': case 'summary-retry': case 'summary-refresh': await startSummary(); break;
    case 'summary-toggle': ui.summaryOpen = !ui.summaryOpen; renderSummary(); break;
    case 'summary-apply': await change('/api/name',{key:s.key,name:ui.state.summaries[s.key].title}); break;
    case 'copy-next': await copy(t('summary.next-message', {step:ui.state.summaries[s.key].next_step.replace(/[.\s]+$/,'')}),element); break;
    case 'message-toggle': {
      const key = `${s.key}:${element.dataset.index}`;
      ui.expanded.has(key) ? ui.expanded.delete(key) : ui.expanded.add(key);
      const scroll = $(test('conversation')).scrollTop; renderConversation(); $(test('conversation')).scrollTop = scroll; break;
    }
    case 'conversation-retry': ui.messages.delete(s.key); renderConversation(); await loadConversation(s.key); break;
    case 'pick-enter': ui.picking = true; ui.view = 'list'; renderFeed(); renderMain(); break;
    case 'pick-yesterday': pickDay(-1); break;
    case 'pick-today': pickDay(0); break;
    case 'pick-none': ui.picked.clear(); renderFeed(); renderRestore(); break;
    case 'pick-remove': togglePick(element.dataset.key); break;
    case 'pick-next': ui.view = 'session'; layout(); break;
    case 'pick-done': exitPick(); break;
    case 'fmt-wt': case 'fmt-tmux': case 'fmt-plain': ui.fmt = id.slice(4); renderRestore(); break;
    case 'copy-script': if (ui.script !== null) await copy(ui.script,element); break;
    case 'toast-undo': if (undo) await undo(); break;
  }
}
async function safely(id, element) {
  if (element?.disabled) return;
  if (element?.tagName === 'BUTTON') element.disabled = true;
  try { await action(id,element); }
  catch (error) { toast(error.message); }
  finally { if (element?.isConnected) element.disabled = false; }
}
// Стрелки из поля поиска: строки-действия над лентой, затем сессии.
function moveSelection(offset) {
  const actions = feedActionIds(), total = actions.length + visible.length;
  if (!total) return;
  const selected = agentShown() ? -1 : visible.findIndex(s => s.key === ui.key);
  const current = ui.cursor ? actions.indexOf(ui.cursor) : selected < 0 ? -1 : actions.length + selected;
  const next = Math.max(0, Math.min(total - 1, current + offset));
  if (next < actions.length) {
    ui.cursor = actions[next]; renderFeed();
    $('.feed-actions .is-sel')?.scrollIntoView({block:'nearest'});
    return;
  }
  const index = next - actions.length;
  ui.limit = Math.max(ui.limit,index+1);
  select(visible[index].key, false, true);
  $(test('row') + '.is-sel')?.scrollIntoView({block:'nearest'});
}
function bind() {
  document.addEventListener('click',event => {
    const target = eventElement(event);
    if (!target) return;
    if (ui.menu && !target.closest('.menu,.menu-anchor,[data-testid="session-menu"]')) { closeMenu(); return; }
    const element = target.closest('[data-testid]'), id = element?.dataset.testid;
    if (!element || ['nav-digest','settings-link','data-export','search','rename-input','data-import-file','rename-save'].includes(id) || !element.matches('button,a')) return;
    event.preventDefault(); safely(id,element);
  });
  document.addEventListener('submit',event => {
    if (event.target.matches('.rename-form')) { event.preventDefault(); safely('rename-save',$(test('rename-save'))); }
  });
  document.addEventListener('input',event => {
    if (event.target.id === 'q') { ui.q = event.target.value; search(); }
    if (event.target.id === 'log-reply' && logs.has(ui.log)) logs.get(ui.log).draft = event.target.value;
  });
  // Прокрутка не всплывает: ленту слушаем на фазе перехвата.
  document.addEventListener('scroll',event => {
    if (event.target.id !== 'feed' || renderingFeed) return;
    const feed = event.target;
    if (ui.section !== 'projects' && visible.length > ui.limit && feed.scrollTop + feed.clientHeight >= feed.scrollHeight - 160) { ui.limit += 50; renderFeed(); }
  }, true);
  document.addEventListener('change',async event => {
    if (event.target.dataset?.testid !== 'data-import-file') return;
    const file = event.target.files[0]; if (!file) return;
    try { await change('/api/import',{data:JSON.parse(await file.text())}); toast(t('toast.imported')); }
    catch (error) { toast(t('toast.import-failed', {error:error.message})); }
    event.target.value = '';
  });
  document.addEventListener('keydown',event => {
    const target = eventElement(event);
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && target?.id === 'q' && !event.isComposing) { event.preventDefault(); safely('smart-search',$(test('smart-search'))); return; }
    if (event.ctrlKey || event.altKey || event.metaKey || event.isComposing) return;
    const input = target?.closest('input,textarea,[contenteditable="true"]');
    if (input) {
      if (input.id === 'rn' && event.key === 'Escape') { event.preventDefault(); ui.editing = false; renderTitle(); }
      // Ответ агенту: Enter — отправить, Shift+Enter — новая строка.
      if (input.id === 'log-reply' && event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); safely('reply-send',$(test('reply-send'))); return; }
      if (input.id !== 'q') return;
      if (event.key === 'Escape') { event.preventDefault(); ui.q = ''; input.value = ''; search(); input.blur(); }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); moveSelection(event.key === 'ArrowDown' ? 1 : -1); }
      if (event.key === 'Enter') {
        event.preventDefault();
        // Выделенная строка-действие; иначе выбранная сессия; если обычный поиск ничего не нашёл — полный поиск.
        if (ui.cursor) { const id = ui.cursor === 'full' ? 'search-full' : 'smart-search'; safely(id,$(test(id))); return; }
        const s = visible.find(s => s.key === ui.key) || visible[0];
        if (s) select(s.key);
        else if (ui.q.trim() && ui.section !== 'projects') { safely('search-full',$(test('search-full'))); return; }
        input.blur();
      }
      return;
    }
    if (event.key === '/' && !event.repeat) {
      event.preventDefault();
      if (narrow() && ui.view !== 'list') { ui.view = 'list'; writeHash(); layout(); }
      $('#q').focus();
    }
    if (event.key === 'Escape') { if (ui.menu) { closeMenu(); return; } if (ui.picking) exitPick(); }
    if (!event.repeat && event.key.toLowerCase() === 'c' && ((ui.key && !agentShown()) || ui.picking)) safely(ui.picking ? 'copy-script' : 'copy-command',$(test(ui.picking ? 'copy-script' : 'copy-command')));
  });
  window.addEventListener('hashchange',() => { readHash(); ui.editing = false; ui.picking = false; search(); renderMain(); });
  window.addEventListener('popstate',() => { readHash(); ui.editing = false; ui.picking = false; search(); renderMain(); });
}
async function init() {
  readHash(); shell(); renderNav(); bind(); renderLoading();
  try {
    // Агенты, окружение и настройки нужны до первой отрисовки списка: иконки, меню, формат восстановления.
    const [state, config] = await Promise.all([api('/api/state'), api('/api/config')]);
    // Язык страницы задаёт сервер (настройка language или язык браузера). Если он другой, каркас перерисовывается.
    const before = lang();
    setLanguage(config.language);
    if (lang() !== before) {
      const focused = document.activeElement?.id === 'q';
      shell(); renderNav(); renderLoading();
      if (focused) $('#q').focus();
    }
    ui.state = state; applyConfig(config);
    while (!ui.ready) {
      const result = await api('/api/sessions');
      ui.ready = result.ready; ui.sessions = result.sessions;
      $(test('server-addr')).textContent = `127.0.0.1:${result.port}`;
      if (!ui.ready) { renderNav(); renderLoading(result.progress); await new Promise(resolve => setTimeout(resolve,300)); }
    }
    sessionMap = new Map(ui.sessions.map(s => [s.key,s]));
    const saved = stored(SMART_RESULT);
    if (saved?.result && !stored(SMART_JOB)) ui.smart = {...saved, warnings:saved.warnings || [], running:false};
    const savedFull = stored(FULL_RESULT);
    if (savedFull?.result && !stored(FULL_JOB) && !ui.smart) ui.full = {...savedFull, running:false};
    if (!sessionMap.has(ui.key)) ui.key = (ui.sessions.find(s => mine(s) && ui.state.pins.includes(s.key)) || ui.sessions.find(mine) || ui.sessions[0])?.key || '';
    writeHash(true); renderFeed(); renderMain(); resumeJobs(); pollLive();
    setInterval(pollLive,5000);
    setInterval(() => {
      for (const [panel, state] of [['smart-progress', ui.smart], ['full-progress', ui.full]]) {
        const elapsed = $(`${test(panel)} .smart-elapsed`);
        if (elapsed && state?.running) elapsed.textContent = minutes(Date.now()/1000 - state.started);
      }
      renderAgentTime();
    },1000);
  } catch (error) {
    $('#main').innerHTML = `<div class="loading-view" role="alert"><div class="loading-card">${esc(t('loading.failed', {error:error.message}))}<a class="btn" href="${esc(location.href)}">${esc(t('retry'))}</a></div></div>`;
    ui.view = 'session'; layout();
  }
}
init();
