import {toolIcon, toolName, registerAgents} from './icons.js';

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
const MONTHS = ['янв','фев','мар','апр','май','июн','июл','авг','сен','окт','ноя','дек'];
const WEEKDAYS = ['воскресенье','понедельник','вторник','среда','четверг','пятница','суббота'];
const WEEKDAYS_SHORT = ['вс','пн','вт','ср','чт','пт','сб'];
const flashes = new WeakMap();
const button = (id, text, cls = 'btn', extra = '') => `<button type="button" data-testid="${id}" class="${cls}" ${extra}>${text}</button>`;
const kbd = (key) => `<span class="kbd" aria-hidden="true">${key}</span>`;
const ui = {section:'mine', key:'', view:'list', project:'', q:'', inConv:false, hideDone:false, showLast:true,
  ready:false, sessions:[], state:{pins:[], names:{}, summaries:{}, done:[], hidden:[]}, live:{},
  limit:50, picking:false, picked:new Set(), fmt:'plain', editing:false, summaryOpen:false, jobs:new Map(),
  messages:new Map(), expanded:new Set(), snippets:new Map(), menu:'', script:null,
  config:{agents:[], runners:[], env:{}, settings:{}}, smart:null};
let sessionMap = new Map(), visible = [], searchController, searchTimer, toastTimer, undo, renderingFeed = false;

async function api(path, body, options = {}) {
  const response = await fetch(path, {...options, ...(body === undefined ? {} : {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)})});
  if (!response.ok) {
    let message = `Ошибка ${response.status}`;
    try { message = (await response.json()).error || message; } catch { /* Ответ сервера может быть без JSON. */ }
    throw new Error(message);
  }
  return options.text ? response.text() : response.json();
}
function toast(message, action) {
  clearTimeout(toastTimer); undo = action;
  $('#toast-area').innerHTML = `<div class="toast" data-testid="toast" role="status"><span>${esc(message)}</span>${action ? button('toast-undo','Вернуть','link') : ''}</div>`;
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
    if (!copied) throw new Error('Не удалось скопировать. Выделите текст команды вручную.');
  }
  if (element?.isConnected) {
    const badge = element.querySelector('.kbd');
    flash(element, `Скопировано${badge ? badge.outerHTML : ''}`);
  } else toast(notice || 'Скопировано');
}
function name(s) { return ui.state.names[s.key] || s.title; }
function env() { return ui.config.env || {}; }
function applyConfig(config) {
  ui.config = config; registerAgents(config.agents); renderSmartButton();
  const chosen = config.settings?.open_with;
  ui.fmt = formats().includes(chosen) ? chosen : env().wsl ? 'wt' : env().tmux ? 'tmux' : 'plain';
}
function formats() { return ['wt','tmux','plain'].filter(fmt => fmt !== 'wt' || env().wsl); }
// Кто составит сводку или проведёт умный поиск: агент из настроек, иначе первый установленный.
function plannedRunner(kind = 'summary') {
  const wanted = ui.config.settings?.[kind]?.agent;
  return wanted && wanted !== 'auto' ? wanted : ui.config.runners[0] || '';
}
function noRunnerHint() {
  const order = ui.config.runner_ids || ui.config.agents.filter(a => a.runner).map(a => a.id);
  const names = order.map(id => toolName(id));
  return `Нет агента для сводок: установите ${names.length > 1 ? `${names.slice(0,-1).join(', ')} или ${names.at(-1)}` : names[0] || 'агента, который составляет сводки'}`;
}
const plural = (n, one, few, many) => n % 10 === 1 && n % 100 !== 11 ? one : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? few : many;
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
function dayLabel(timestamp) {
  const today = new Date(), yesterday = new Date(); yesterday.setDate(today.getDate() - 1);
  const key = day(timestamp);
  if (key === calendarDay(today)) return 'Сегодня';
  if (key === calendarDay(yesterday)) return 'Вчера';
  const date = new Date(timestamp * 1000);
  const weekday = WEEKDAYS[date.getDay()];
  const year = date.getFullYear() !== today.getFullYear() ? ` ${date.getFullYear()}` : '';
  return `${weekday.charAt(0).toUpperCase()}${weekday.slice(1)}, ${date.getDate()} ${MONTHS[date.getMonth()]}${year}`;
}
function pinnedMark(timestamp) {
  const today = new Date(), yesterday = new Date(); yesterday.setDate(today.getDate() - 1);
  const key = day(timestamp);
  if (key === calendarDay(today)) return time(timestamp);
  if (key === calendarDay(yesterday)) return 'вчера';
  const date = new Date(timestamp * 1000), diff = dayDistance(timestamp);
  if (diff > 1 && diff < 7) return WEEKDAYS_SHORT[date.getDay()];
  const year = date.getFullYear() !== today.getFullYear() ? ` ${date.getFullYear()}` : '';
  return `${date.getDate()} ${MONTHS[date.getMonth()]}${year}`;
}
function time(timestamp) { return new Date(timestamp * 1000).toLocaleTimeString('ru-RU', {hour:'2-digit', minute:'2-digit'}); }
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
function fields(s) {
  const summary = ui.state.summaries[s.key];
  return [[name(s),'название'],[s.title,'исходное'],[s.project,'проект'],[s.branch,'ветка'],[s.cwd,'путь'],[s.id,'ID'],
    [summary ? `${summary.title} ${summary.summary} ${summary.next_step || ''}` : '', 'сводка'],[ui.snippets.get(s.key),'переписка']];
}
function matches(s) { const q = ui.q.trim().toLocaleLowerCase(); return !q || fields(s).some(([value]) => String(value || '').toLocaleLowerCase().includes(q)); }
function filtered() {
  return ui.sessions.filter(s => (ui.section === 'all' || mine(s)) && (!ui.project || s.project === ui.project)
    && (!ui.hideDone || !ui.state.done.includes(s.key) || s.key === ui.key) && matches(s));
}
function waiting() { return ui.sessions.filter(s => !s.auto && !s.temp && ui.live[s.key]?.state === 'wait'); }
function readHash() {
  const hash = new URLSearchParams(location.hash.slice(1));
  ui.section = ['mine','all','projects'].includes(hash.get('section')) ? hash.get('section') : 'mine';
  ui.key = hash.get('session') || hash.get('key') || '';
  ui.view = hash.get('view') || (ui.key ? 'session' : 'list');
  ui.project = hash.get('project') || '';
}
function writeHash(replace = false) {
  const hash = new URLSearchParams({section:ui.section});
  if (ui.key) hash.set('session',ui.key);
  if (ui.project) hash.set('project',ui.project);
  hash.set('view',ui.view);
  history[replace ? 'replaceState' : 'pushState'](null,'',`#${hash}`);
}
function shell() {
  $('#app').innerHTML = `<header class="topbar"><div class="brand">${svg('<path d="M3 16c3-7 6 1 9-5s6 2 9-4" stroke="var(--accent)"/>',18,2.2)}<span>нить</span></div>
    <nav class="nav" aria-label="Разделы"><a href="#section=mine" data-testid="nav-mine">Мои</a><a href="#section=all" data-testid="nav-all">Все <span class="n"></span></a><a href="#section=projects" data-testid="nav-projects">Проекты <span class="n"></span></a><a href="/digest.html" data-testid="nav-digest">Сводка</a></nav>
    <div class="grow"></div>${button('wait-next','','wait-btn','hidden')}<span class="addr" data-testid="server-addr">${esc(location.host)}</span>
    <div class="menu-anchor">${button('data-menu','Данные','btn','aria-haspopup="menu" aria-expanded="false"')}<div id="data-popup"></div></div><a href="/settings.html" class="icon-btn" aria-label="Настройки" title="Настройки" data-testid="settings-link"><svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8" fill="none"></circle><path d="M12 2.8v2.6M12 18.6v2.6M21.2 12h-2.6M5.4 12H2.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8M18.5 18.5l-1.8-1.8M7.3 7.3L5.5 5.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"></path><circle cx="12" cy="12" r="6.2" stroke="currentColor" stroke-width="1.8" fill="none"></circle></svg></a>
    <input type="file" accept="application/json,.json" data-testid="data-import-file" hidden></header>
    <div class="workspace"><aside class="sidebar"><div class="search-wrap"><label for="q" class="sr">Поиск сессий</label><div class="search"><span class="search-icon">${icons.search}</span><input id="q" data-testid="search" type="search" placeholder="Название, ветка, путь, ID, сводка" autocomplete="off">${kbd('/')}<div class="search-actions">${button('search-conv','+ переписка','toggle','aria-pressed="false" title="Искать и в тексте переписки"')}${button('smart-search',`${icons.spark}<span>Спросить агента</span>`,'toggle smart-btn','disabled')}</div></div></div><div id="feed-tools"></div><div id="feed-banners"></div><div id="smart-area"></div><div class="feed scroll" id="feed"></div></aside><main class="main" id="main"></main></div><div id="toast-area"></div>`;
}
function layout() { $('.workspace').classList.toggle('detail', ui.view === 'session' || !ui.ready); }
function renderNav() {
  for (const section of ['mine','all','projects']) $(test(`nav-${section}`)).classList.toggle('on',ui.section === section);
  $(test('nav-all') + ' .n').textContent = ui.sessions.length;
  $(test('nav-projects') + ' .n').textContent = new Set(ui.sessions.filter(mine).map(s => s.project)).size;
  const waits = waiting();
  const btn = $(test('wait-next')); btn.hidden = !waits.length;
  btn.innerHTML = `<span class="dot wait"></span>${waits.length} ${waits.length % 10 === 1 && waits.length % 100 !== 11 ? 'ждёт' : 'ждут'} вас`;
}
function dayPick(offset) {
  const date = new Date(); date.setDate(date.getDate() + offset);
  return ui.sessions.filter(s => mine(s) && (!ui.project || s.project === ui.project) && day(s.updated) === calendarDay(date) && !ui.state.done.includes(s.key) && !ui.live[s.key]);
}
function legacyData() {
  try {
    const pins = JSON.parse(localStorage.getItem('nit.pins') || '[]');
    const names = JSON.parse(localStorage.getItem('nit.names') || '{}');
    return {pins:Array.isArray(pins) ? pins : Object.keys(pins).filter(k => pins[k]), names:names && typeof names === 'object' && !Array.isArray(names) ? names : {}};
  } catch { return {pins:[],names:{}}; }
}
function renderTools() {
  const hideTitle = ui.hideDone ? 'Показать завершённые' : 'Скрыть завершённые';
  $('#feed-tools').innerHTML = !ui.ready ? '' : ui.picking ? `<div class="feed-tools pick-tools"><strong data-testid="pick-count">Выбрано ${ui.picked.size}</strong>${button('pick-yesterday','вчерашние','link')}${button('pick-today','сегодняшние','link')}${button('pick-none','снять','link')}<span class="grow"></span>${button('pick-next','Дальше','btn btn-primary pick-next')}${button('pick-done','Готово')}</div>` : `<div class="feed-tools">${button('pick-yesterday',`${icons.terminal}Вчерашние · ${dayPick(-1).length}`)}${button('pick-enter','Выбрать…','link')}<span class="grow"></span>${button('toggle-hide-done',icons.done,'icon-btn',`aria-pressed="${ui.hideDone}" aria-label="${hideTitle}" title="${hideTitle}"`)}${button('toggle-last',icons.last,'icon-btn',`aria-pressed="${ui.showLast}" aria-label="Последний ответ агента в строке" title="Последний ответ агента в строке"`)}</div>`;
  const legacy = legacyData();
  $('#feed-banners').innerHTML = (ui.project ? `<div class="feed-banner" data-testid="project-filter">Проект: ${esc(ui.project)}${button('project-filter-clear',icons.close,'icon-btn','aria-label="Снять фильтр проекта"')}</div>` : '') +
    (!ui.state.migrated_local_storage && (legacy.pins.length || Object.keys(legacy.names).length) ? `<div class="feed-banner" data-testid="migrate-banner">Нашлись закрепления и названия прошлой версии: ${legacy.pins.length} и ${Object.keys(legacy.names).length}${button('migrate-button','Перенести','btn sm')}</div>` : '');
}
function rowHit(s) {
  const q = ui.q.trim().toLocaleLowerCase();
  if (!q || name(s).toLocaleLowerCase().includes(q)) return null;
  for (const [value, label] of fields(s)) {
    if (label === 'название') continue;
    const text = String(value || '');
    if (!text.toLocaleLowerCase().includes(q)) continue;
    if (label === 'проект') return null;
    const index = text.toLocaleLowerCase().indexOf(q);
    const start = Math.max(0, index - 26);
    return [(start > 0 ? '…' : '') + text.slice(start), label];
  }
  return null;
}
function row(s, pinned = false, why = null) {
  const hit = why === null && rowHit(s);
  const status = ui.live[s.key]?.state, done = ui.state.done.includes(s.key);
  const cls = `row${ui.picking ? ' picking' : ''}${!ui.picking && s.key === ui.key ? ' is-sel' : ''}${ui.picking && ui.picked.has(s.key) ? ' is-picked' : ''}${done ? ' is-done' : ''}`;
  return `<button type="button" class="${cls}" data-testid="row" data-key="${esc(s.key)}" ${ui.picking ? `aria-pressed="${ui.picked.has(s.key)}"` : ''}>
    ${ui.picking ? `<span class="chk ${ui.picked.has(s.key) ? 'on' : ''}" aria-hidden="true">${checkBox}</span>` : ''}${toolIcon(s.tool,18)}<span class="row-content"><span class="row-top"><span class="row-project">${highlighted(s.project)}</span>${s.branch ? `<span class="row-branch">${highlighted(s.branch)}</span>` : ''}${s.missing ? '<span class="warn-text">нет папки</span>' : ''}<span class="row-tail">${done ? '<span class="ok-text">завершена</span>' : ''}${status ? `<span class="dot ${status}" title="${status === 'wait' ? 'Агент ждёт вашего ответа' : 'Агент работает'}"></span>` : ''}${pinned ? pinnedMark(s.updated) : time(s.updated)}</span></span><span class="row-title">${highlighted(name(s))}</span>${why !== null ? `<span class="row-sub smart-why" data-testid="smart-why" title="${esc(why)}">${esc(why)}</span>` : hit ? `<span class="row-sub"><b>${hit[1]}:</b> ${highlighted(hit[0])}</span>` : ui.showLast && s.last ? `<span class="row-sub">${esc(s.last)}</span>` : ''}</span></button>`;
}
function renderFeed(reset = false) {
  if (!ui.ready) return;
  const feed = $('#feed');
  const scroll = reset ? 0 : feed.scrollTop;
  if (reset) ui.limit = 50;
  renderNav(); renderTools(); renderSmart();
  renderingFeed = true;
  try {
    if (ui.section === 'projects') {
      visible = [];
      const projects = new Map(), q = ui.q.trim().toLocaleLowerCase();
      for (const s of ui.sessions.filter(mine)) {
        const entry = projects.get(s.project) || {count:0,updated:s.updated}; entry.count++; projects.set(s.project,entry);
      }
      const items = [...projects].filter(([p]) => p.toLocaleLowerCase().includes(q));
      feed.innerHTML = items.map(([p,v]) => `<button class="row project-entry" data-testid="project-row" data-project="${esc(p)}"><span><strong>${highlighted(p)}</strong><small>${dayLabel(v.updated)}, ${time(v.updated)}</small></span><span>${v.count}</span></button>`).join('') || (q ? `<div class="empty-feed" data-testid="no-results"><div>Ничего не найдено по «${esc(ui.q)}»</div></div>` : '<div class="empty-feed">Проектов пока нет</div>');
      feed.scrollTop = scroll;
      return;
    }
    if (ui.smart?.result) {
      // Результат умного поиска заменяет ленту, пока его не закрыли.
      const why = new Map(ui.smart.result.results.map(r => [r.key, r.why || '']));
      visible = smartSessions();
      const terms = ui.smart.result.plan?.terms || [];
      feed.innerHTML = visible.map(s => row(s, true, why.get(s.key))).join('') || `<div class="empty-feed" data-testid="smart-empty"><div>Подходящих сессий нет.${terms.length ? ` Искал: ${esc(terms.join(', '))}` : ''}</div></div>`;
      feed.scrollTop = scroll;
      return;
    }
    const all = filtered(), pinned = all.filter(s => ui.state.pins.includes(s.key)), recent = all.filter(s => !ui.state.pins.includes(s.key));
    visible = pinned.concat(recent);
    const displayed = visible.slice(0,ui.limit);
    const pins = displayed.filter(s => ui.state.pins.includes(s.key));
    let html = pins.length ? `<section data-testid="pinned-section"><div class="sec">Закреплённые</div>${pins.map(s => row(s, true)).join('')}</section>` : '';
    let currentDay;
    for (const s of displayed.filter(s => !ui.state.pins.includes(s.key))) {
      if (currentDay !== day(s.updated)) { currentDay = day(s.updated); html += `<div class="sec day">${esc(dayLabel(s.updated))}</div>`; }
      html += row(s);
    }
    const hint = ui.inConv
      ? 'Искали и в переписке. Автоматические запуски и временные папки ищутся во «Всех сессиях».'
      : 'Искали в названиях, ветках, путях, ID и сводках. Можно поискать и в тексте переписки.';
    const hiddenCount = ui.section === 'mine' ? ui.state.hidden.length : 0;
    const empty = ui.q.trim()
      ? `<div class="empty-feed" data-testid="no-results"><div>Ничего не найдено по «${esc(ui.q)}»</div><div class="empty-note">${hint}</div><div class="actions">${!ui.inConv ? button('search-more','Искать и в переписке') : ''}${button('search-all','Искать во всех')}${button('search-clear',`Сбросить ${kbd('Esc')}`)}</div></div>`
      : '<div class="empty-feed">Здесь пока нет сессий</div>';
    // Пока идёт умный поиск, пустая лента по тем же словам только отвлекает от панели поиска.
    if (ui.smart?.running && !all.length) { feed.innerHTML = ''; return; }
    const end = `<div class="feed-end" data-testid="feed-end">Раньше сессий нет${hiddenCount ? ` · скрыто ${hiddenCount}` : ''}</div>`;
    html += !all.length ? empty : displayed.length < all.length ? `<div class="feed-end">${button('feed-more','Показать более ранние','link')}</div>` : (ui.q.trim() ? '' : end);
    feed.innerHTML = html;
    feed.scrollTop = scroll;
  } finally { renderingFeed = false; }
}
function renderLoading(progress = {}) {
  const tools = Object.entries(progress.tools || {});
  const finished = tools.filter(([,s]) => s === 'done').map(([t]) => toolName(t));
  const reading = tools.filter(([,s]) => s === 'reading').map(([t]) => toolName(t));
  const widths = ['82%','64%','90%','71%','58%','86%','69%','77%','60%'];
  $('#feed').innerHTML = `<div class="skeletons">${widths.map(w => `<div><div class="skel" style="width:38%"></div><div class="skel" style="width:${w}"></div></div>`).join('')}</div>`;
  $('#main').innerHTML = `<div class="loading-view" data-testid="loading"><div class="loading-card"><div class="with-icon"><span class="spin"></span>Читаю журналы сессий</div><div class="loading-bar"><div style="width:${progress.total ? Math.round(progress.done/progress.total*100) : 0}%"></div></div><div class="loading-progress" data-testid="loading-progress">${progress.done || 0} из ${progress.total || 0}${finished.length ? ` · ${finished.join(', ')} готовы` : ''}${reading.length ? ` · читаю ${reading.join(', ')}` : ''}</div></div></div>`;
  layout();
}
function renderTitle() {
  const s = sessionMap.get(ui.key); if (!s || !$('#session-top')) return;
  const custom = ui.state.names[s.key], pinned = ui.state.pins.includes(s.key);
  const starLabel = pinned ? 'Открепить' : 'Закрепить наверху';
  $('#session-top').innerHTML = `${button('back-to-list','← Список','link back')}${ui.editing ? `<form class="rename-form"><label for="rn" class="sr">Своё название сессии</label><input id="rn" class="rename" data-testid="rename-input" maxlength="200" value="${esc(name(s))}"><button type="submit" class="btn" data-testid="rename-save">Сохранить ${kbd('Enter')}</button>${button('rename-cancel','Отмена')}${custom ? button('rename-reset','Вернуть исходное','link') : ''}</form>` : `<div class="title-line"><h1 data-testid="session-title">${esc(name(s))}</h1>${button('rename',icons.rename,'icon-btn','aria-label="Переименовать" title="Переименовать"')}${ui.state.done.includes(s.key) ? '<span class="pill ok">завершена</span>' : ''}<div class="grow"></div>${button('pin',pinned ? starFilled : icons.star,'icon-btn',`aria-pressed="${pinned}" aria-label="${starLabel}" title="${starLabel}"`)}${button('session-menu',icons.menu,'icon-btn',`aria-label="Действия с сессией" aria-haspopup="menu" aria-expanded="${ui.menu === 'session'}"`)}<div id="session-popup"></div></div>`}
    ${custom ? `<div class="orig" data-testid="session-orig" title="${esc(s.title)}">Исходное: ${esc(s.title)}</div>` : ''}<div class="meta" data-testid="session-meta"><span class="with-icon">${toolIcon(s.tool,16)}${esc(toolName(s.tool))}</span><span class="meta-project">${esc(s.project)}</span>${s.branch ? `<span class="branch with-icon">${icons.branch}${esc(s.branch)}</span>` : ''}<span>${dayLabel(s.updated).toLocaleLowerCase()}, ${time(s.updated)}</span><span>запустил ${esc(s.by)}</span></div>`;
  if (ui.editing) { $(test('rename-input')).focus(); $(test('rename-input')).select(); }
  renderMenus();
}
function renderLive() {
  const s = sessionMap.get(ui.key); if (!s || !$('#live-area')) return;
  const live = ui.live[s.key];
  $('#live-area').hidden = !live;
  $('#live-area').innerHTML = live ? `<div class="notice ${live.state}" data-testid="live-banner"><span class="dot ${live.state}"></span><span><b>${live.state === 'wait' ? 'Агент ждёт вашего ответа' : 'Агент работает'}</b> · ${esc(live.where)}.${live.state === 'wait' ? ' Открывать заново не нужно.' : ''}</span></div>` : '';
}
function renderSummary() {
  const s = sessionMap.get(ui.key); if (!s || !$('#summary')) return;
  const summary = ui.state.summaries[s.key], job = ui.jobs.get(s.key);
  // Иконка и подпись — агента, который составил сводку; во время работы — агента из событий прогресса.
  const agent = job?.running || job?.error ? job.agent || plannedRunner() : summary ? summary.model || '' : plannedRunner();
  const noRunner = !ui.config.runners.length ? `disabled title="${esc(noRunnerHint())}"` : '';
  let content;
  if (job?.running) content = `<div class="summary-progress" data-testid="summary-progress"><span class="spin"></span><span class="progress-copy" aria-live="polite">${agent ? `${esc(toolName(agent))} ` : ''}${esc(job.text || 'запускается')}<span class="elapsed">${Math.floor((job.elapsed || 0)/60)}:${String(Math.floor((job.elapsed || 0)%60)).padStart(2,'0')}</span></span><div class="segments">${Array.from({length:Math.min(job.steps || 7,30)},(_,i) => `<span class="seg ${i+1 < job.step ? 'done' : i+1 === job.step ? 'now' : ''}"></span>`).join('')}</div></div>`;
  else if (job?.error) content = `<div class="summary-error" data-testid="summary-error" role="alert">Сводка не получилась: ${esc(job.error)}${job.code !== undefined && job.code !== null ? ` · код ${esc(job.code)}` : ''}${job.output ? `<pre class="error-output">${esc(job.output)}</pre>` : ''}</div>${button('summary-retry','Повторить','btn sm',noRunner)}`;
  else if (summary) content = `<span class="ellip ${ui.summaryOpen ? 'summary-when' : ''}" ${!ui.summaryOpen ? 'data-testid="summary-text"' : ''}>${ui.summaryOpen ? `сводка${summary.model ? ` ${esc(toolName(summary.model))}` : ''}${summary.at ? ` · ${dayLabel(summary.at).toLocaleLowerCase()}, ${time(summary.at)}` : ''}` : esc(summary.summary)}</span>${button('summary-toggle',ui.summaryOpen ? 'свернуть' : 'развернуть','link')}`;
  else if (s.empty) content = '<span class="ellip summary-idle">В журнале нет сообщений — сводку составить не из чего.</span>';
  else content = `<span class="ellip summary-idle" ${noRunner ? 'data-testid="summary-no-runner"' : ''}>${noRunner ? esc(noRunnerHint()) : 'Сводки пока нет.'}</span>${button('summary-run','Составить','btn sm',noRunner)}`;
  let html = `<div class="ctx-row" data-testid="summary-row"><span class="ctx-label with-icon">${agent ? toolIcon(agent,14) : ''}О чём</span>${content}</div>`;
  if (summary && !job?.running && !job?.error) {
    if (ui.summaryOpen) html += `<div class="ctx-row summary-body"><p data-testid="summary-text">${esc(summary.summary)}</p><div class="actions"><span data-testid="summary-title">Название: «${esc(summary.title)}»</span>${ui.state.names[s.key] === summary.title ? '<span class="ok-text" data-testid="summary-applied">применено</span>' : button('summary-apply','Применить','btn sm')}<span class="grow"></span>${button('summary-refresh','Обновить сводку','link',noRunner)}</div></div>`;
    if (summary.next_step && !ui.state.done.includes(s.key)) html += `<div class="ctx-row" data-testid="next-step"><span class="ctx-label">Следующий шаг</span><span class="ellip" title="${esc(summary.next_step)}">${esc(summary.next_step)}</span>${button('copy-next','Скопировать для агента','btn sm','title="Скопирует сообщение, которое можно вставить агенту сразу после продолжения"')}</div>`;
    if (summary.closed && !ui.state.done.includes(s.key)) html += `<div class="ctx-row" data-testid="closed-hint"><span class="ctx-label">Статус</span><span class="ellip">${summary.model ? `${esc(toolName(summary.model))}: похоже` : 'Похоже'}, задача закрыта.</span>${button('summary-done','Отметить завершённой','btn sm')}</div>`;
  }
  const related = summary ? (summary.related || []).filter(r => sessionMap.has(r.key)) : [];
  if (related.length) html += `<div class="ctx-row"><span class="ctx-label">Связанные</span><div class="related">${related.map(r => `<button class="chip-s" data-testid="related-chip" data-key="${esc(r.key)}" title="${esc(r.why)}">${toolIcon(sessionMap.get(r.key).tool,14)}<span class="t">${esc(name(sessionMap.get(r.key)))}</span><span class="why">· ${esc(r.why)}</span></button>`).join('')}</div></div>`;
  $('#summary').innerHTML = html;
}
function renderConversation() {
  const s = sessionMap.get(ui.key), container = $(test('conversation')); if (!s || !container) return;
  const data = ui.messages.get(s.key);
  if (!data) { container.innerHTML = '<div class="conversation-empty">Читаю переписку…</div>'; return; }
  if (data.error) { container.innerHTML = `<div class="conversation-empty" role="alert">${esc(data.error)} ${button('conversation-retry','Повторить','link')}</div>`; return; }
  $('#conversation-note').textContent = data.messages.length ? `последние ${data.messages.length} · длинные свёрнуты${data.truncated ? ' · показан конец журнала' : ''}` : '';
  const query = ui.inConv ? ui.q.trim() : '';
  container.innerHTML = `<div>${data.messages.length ? data.messages.map((m,i) => {
    const hit = query && m.text.toLocaleLowerCase().includes(query.toLocaleLowerCase());
    const clamped = m.text.length > 360 && !ui.expanded.has(`${s.key}:${i}`) && !hit;
    return `<div class="msg" data-testid="message" data-role="${esc(m.role)}"><div class="speaker">${m.role === 'user' ? '<span class="me">Я</span>' : `<span class="with-icon">${toolIcon(s.tool,14)}${esc(toolName(s.tool))}</span>`}<time>${m.at ? time(new Date(m.at)/1000) : ''}</time></div><div class="row-content"><div class="msg-text ${clamped ? 'is-clamped' : ''}">${highlighted(m.text, query)}</div>${m.text.length > 360 ? button('message-toggle',ui.expanded.has(`${s.key}:${i}`) ? 'Свернуть' : 'Показать полностью','link',`data-index="${i}"`) : ''}</div></div>`;
  }).join('') : '<div class="conversation-empty" data-testid="conversation-empty">У этой сессии нет сообщений.<br>Агент был закрыт до первого ответа — команда продолжения всё равно работает.</div>'}</div>`;
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
  if (!s.command_no_cd) return `<div class="command-box no-command" data-testid="resume-hint">${icons.info}<span>${esc(s.resume_hint || 'Эту сессию нельзя продолжить из терминала.')}</span></div>`;
  return `<div class="command-box" data-testid="command" data-command="${esc(s.command)}"><code><span class="prompt">$ </span>${commandMarkup(s)}</code><div class="command-actions">${button('copy-command',`Скопировать ${kbd('C')}`,'btn btn-primary','aria-live="polite"')}</div></div>`;
}
function renderSession() {
  const s = sessionMap.get(ui.key);
  if (!s) { $('#main').innerHTML = '<div class="loading-view muted">Выберите сессию в списке</div>'; return; }
  $('#main').innerHTML = `<section class="session" data-testid="session" data-key="${esc(s.key)}"><div class="session-top" id="session-top"></div><div class="command-area">${commandBlock(s)}<div id="live-area"></div>${s.missing && s.command_no_cd ? `<div class="notice missing" data-testid="missing-banner" role="alert">${warnIcon}<span>Папки проекта больше нет — команда остановится на cd.</span>${button('copy-no-cd','Скопировать без cd','btn sm')}</div>` : ''}</div><section class="ctx" id="summary" aria-label="О чём сессия"></section><div class="conversation-heading"><strong>Переписка</strong><small id="conversation-note"></small></div><div class="conversation scroll" data-testid="conversation"></div></section>`;
  renderTitle(); renderLive(); renderSummary(); renderConversation();
  if (!ui.messages.has(s.key)) loadConversation(s.key);
}
function renderMenus() {
  $(test('data-menu')).setAttribute('aria-expanded',ui.menu === 'data');
  $('#data-popup').innerHTML = ui.menu === 'data' ? `<div class="menu" role="menu"><a role="menuitem" data-testid="data-export" href="/api/export" download>Выгрузить JSON</a>${button('data-import','Загрузить JSON…','','role="menuitem"')}</div>` : '';
  const popup = $('#session-popup'), s = sessionMap.get(ui.key);
  if (!popup || !s) return;
  $(test('session-menu')).setAttribute('aria-expanded',ui.menu === 'session');
  popup.innerHTML = ui.menu === 'session' ? `<div class="menu" role="menu" aria-label="Действия с сессией">${button('menu-copy-id',`Скопировать ID<span class="hint">${esc(s.id.slice(0,8))}…</span>`,'','role="menuitem"')}${button('menu-copy-path','Скопировать путь','','role="menuitem"')}${env().wsl ? button('menu-explorer','Открыть папку в проводнике','',`role="menuitem" ${s.missing ? 'disabled' : ''}`) : ''}${env().code ? button('menu-vscode','Открыть в VS Code','',`role="menuitem" ${s.missing ? 'disabled' : ''}`) : ''}<div class="sep"></div>${button('menu-done',ui.state.done.includes(s.key) ? 'Вернуть в работу' : 'Отметить завершённой','','role="menuitem"')}${button('menu-hide',ui.state.hidden.includes(s.key) ? 'Вернуть в список' : 'Скрыть из списка','','role="menuitem"')}</div>` : '';
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
    ui.script = live === pickedSessions().length ? '# Все выбранные сессии уже открыты.'
      : `# Выбранные сессии ${live ? 'уже открыты или их ' : ''}нельзя продолжить из терминала.`;
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
    wt: 'Одна команда из WSL откроет вкладки в текущем окне Windows Terminal, цвет вкладки — по агенту. nit-resume — помощник «Нити»: находит сессию, переходит в папку проекта и запускает агента в вашей обычной оболочке, поэтому PATH и алиасы из ~/.bashrc работают. Если папки нет, запускает из домашней.',
    tmux: `${wsl ? 'Выполняется внутри WSL. ' : ''}Сессии откроются окнами в tmux-сессии ${esc(ui.config.settings?.tmux_session || 'nit')}.`,
    plain: `Вставляйте по одной в отдельные вкладки ${wsl ? 'WSL' : 'терминала'}.`,
  };
  const labels = {wt:'вкладки Windows Terminal', tmux:'окна tmux', plain:'список команд'};
  const badges = s => (ui.live[s.key] ? '<span class="pill ok">уже открыта · пропущу</span>' : '') + (!s.command_no_cd ? '<span class="pill warn">нельзя продолжить из терминала</span>' : s.missing ? '<span class="pill warn">нет папки · без cd</span>' : '');
  $('#main').innerHTML = `<section class="restore" data-testid="restore"><div class="restore-heading">${button('back-to-list','← Список','link back')}<h1>Восстановить сессии</h1><span class="restore-note">${picked.length} выбрано${open ? ` · ${open} ${open === 1 ? 'уже открыта — пропущу' : 'уже открыты — пропущу'}` : ''}${blocked ? ` · ${blocked} нельзя продолжить из терминала` : ''}</span><div class="grow"></div>${button('pick-done',`Закрыть ${kbd('Esc')}`)}</div>${!picked.length ? `<div class="conversation-empty">Отметьте сессии в списке или выберите сразу все вчерашние.<br>${button('pick-yesterday','Выбрать вчерашние','link')}</div>` : `<div class="picked-list scroll">${picked.map(s => `<div class="pick-row" data-testid="picked-row" data-key="${esc(s.key)}">${toolIcon(s.tool,18)}<span class="pick-project">${esc(s.project)}</span><span class="pick-title">${esc(name(s))}</span><span class="pick-badges">${badges(s)}</span>${button('pick-remove',icons.close,'icon-btn',`aria-label="Убрать из списка" data-key="${esc(s.key)}"`)}</div>`).join('')}</div><div class="actions"><span class="restore-note">Как открыть</span><div class="seg-ctl" role="group" aria-label="Формат">${formats().map(fmt => button(`fmt-${fmt}`,labels[fmt],'',`aria-pressed="${ui.fmt === fmt}"`)).join('')}</div><span class="grow"></span>${button('copy-script',`Скопировать скрипт ${kbd('C')}`,'btn btn-primary','disabled')}</div><pre class="script scroll" data-testid="script">Готовлю скрипт…</pre><div class="script-note">${notes[ui.fmt]}</div>`}</section>`;
  if (picked.length) loadScript();
}
function renderMain() { if (ui.ready) ui.picking ? renderRestore() : renderSession(); layout(); }
function select(key, show = true, replace = false) {
  if (!sessionMap.has(key)) return;
  const changed = ui.key !== key;
  ui.key = key; ui.editing = false; ui.summaryOpen = false; ui.menu = ''; ui.picking = false;
  if (show) ui.view = 'session';
  writeHash(replace); renderFeed();
  if (changed || !$(test('session'))) renderMain(); else { renderTitle(); renderSummary(); layout(); }
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
  toast(field === 'done' ? value ? 'Сессия отмечена завершённой' : 'Сессия снова в работе' : value ? 'Сессия скрыта из списка. Найти её можно во «Всех сессиях»' : 'Сессия возвращена в список', async () => { await change(path,{key,[collection]:!value}); toast('Возвращено'); });
}
async function startSummary() {
  const key = ui.key;
  if (ui.jobs.get(key)?.running) return;
  ui.jobs.set(key,{running:true,text:'запускается'}); renderSummary();
  try { const {job} = await api('/api/summary',{key}); watchJob(job,key); }
  catch (error) { ui.jobs.set(key,{error:error.message}); if (ui.key === key) renderSummary(); }
}
async function watchJob(id,key) {
  if (ui.jobs.get(key)?.connected) return;
  const state = {...ui.jobs.get(key),running:true,connected:true,since:0}; ui.jobs.set(key,state);
  if (ui.key === key) renderSummary();
  const handle = async (event) => {
    state.since = event.n;
    if (event.type === 'progress') Object.assign(state,event);
    if (event.type === 'result') { await change('/api/state'); state.running = false; }
    if (event.type === 'error') Object.assign(state,{running:false,error:event.message,code:event.code,output:event.output});
    if (ui.key === key) renderSummary();
  };
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(id)}/events?since=${state.since}`);
    if (!response.ok) throw new Error(`Не удалось подключиться к сводке (${response.status})`);
    const reader = response.body.getReader(), decoder = new TextDecoder(); let buffer = '';
    while (true) {
      const {value,done} = await reader.read();
      buffer += decoder.decode(value,{stream:!done});
      const lines = buffer.split('\n'); buffer = lines.pop();
      for (const line of lines) if (line.trim()) await handle(JSON.parse(line));
      if (done) { if (buffer.trim()) await handle(JSON.parse(buffer)); break; }
    }
    if (state.running) throw new Error('Связь со сводкой прервалась. Подключитесь снова кнопкой «Повторить».');
  } catch (error) { Object.assign(state,{running:false,error:error.message}); }
  finally { state.connected = false; if (ui.key === key) renderSummary(); }
}
async function resumeJobs() {
  let jobs = [];
  try { jobs = await api('/api/jobs'); }
  catch { /* Недоступность заданий не мешает читать журнал. Ошибка запуска видна в блоке сводки. */ }
  for (const job of jobs) if (job.kind === 'summary') watchJob(job.job,job.key);
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
  const parts = [`Ищу: ${plan.terms.join(' · ')}.`];
  if (plan.agents?.length) parts.push(`Агенты: ${plan.agents.map(toolName).join(', ')}.`);
  if (plan.projects?.length) parts.push(`Проекты: ${plan.projects.join(', ')}.`);
  if (plan.days) parts.push(`За ${plan.days} ${plural(plan.days,'день','дня','дней')}.`);
  return parts.join(' ');
}
function renderSmartButton() {
  const smart = $(test('smart-search')); if (!smart) return;
  const q = ui.q.trim();
  smart.disabled = !q || q.length > 500 || !ui.config.runners.length;
  smart.title = !ui.config.runners.length ? noRunnerHint() : q.length > 500 ? 'Запрос длиннее 500 символов' : 'Агент разберёт запрос и найдёт сессии · Ctrl+Enter';
}
function renderSmart() {
  const area = $('#smart-area'), st = ui.smart; if (!area) return;
  if (!st || (st.result && ui.section === 'projects')) { area.innerHTML = ''; return; }
  const agent = st.agent || plannedRunner('search');
  const title = st.q ? `<div class="smart-title">Умный поиск: «${esc(st.q)}»</div>` : '';
  const plan = st.plan && st.running ? `<div class="smart-line" data-testid="smart-plan">${esc(planText(st.plan))}</div>` : '';
  const warnings = st.warnings.map(w => `<div class="smart-line quiet" data-testid="smart-warning">${esc(w)}</div>`).join('');
  if (st.running) area.innerHTML = `<div class="smart-panel" data-testid="smart-progress">${title}<div class="smart-top">${agent ? toolIcon(agent,16) : ''}<span class="smart-text" aria-live="polite">${agent ? `${esc(toolName(agent))} ` : ''}${esc(st.text || 'запускается')}</span><span class="smart-elapsed">${minutes(Date.now()/1000 - st.started)}</span>${button('smart-cancel','Остановить','btn sm')}</div>${plan}${warnings}</div>`;
  else if (st.error) area.innerHTML = `<div class="smart-panel is-error" data-testid="smart-error" role="alert">${title}<div class="smart-top"><span class="smart-text">Поиск не получился: ${esc(st.error)}</span>${button('smart-retry','Повторить','btn sm',ui.config.runners.length ? '' : `disabled title="${esc(noRunnerHint())}"`)}${button('smart-close',icons.close,'icon-btn','aria-label="Закрыть" title="Закрыть"')}</div>${warnings}</div>`;
  else {
    const count = smartSessions().length, by = st.result.ranked ? `отобрал ${st.agent ? esc(toolName(st.agent)) : 'агент'}` : 'совпадения по словам';
    area.innerHTML = `<div class="smart-panel smart-head" data-testid="smart-head"><div class="smart-top"><span class="smart-text"><strong>Умный поиск: «${esc(st.q || st.result.query)}»</strong>${count ? `<small>${count} ${plural(count,'сессия','сессии','сессий')} · ${by}</small>` : ''}</span>${button('smart-close',icons.close,'icon-btn','aria-label="Вернуться к ленте" title="Вернуться к ленте"')}</div>${warnings}</div>`;
  }
}
async function startSmart(q = ui.q.trim(), scope = ui.section === 'all' ? 'all' : 'mine') {
  if (!q || q.length > 500 || !ui.config.runners.length) return;
  const state = {q, scope, started:Date.now()/1000, running:true, warnings:[], since:0};
  ui.smart = state; stored(SMART_RESULT, null); stored(SMART_JOB, {q, scope, started:state.started});
  renderSmart(); renderFeed(true);
  try {
    state.job = (await api('/api/smart-search',{q,scope})).job;
    if (ui.smart !== state) return;
    stored(SMART_JOB, {job:state.job, q, scope, started:state.started});
    if (state.cancelRequested) await cancelSmart();
    watchSmart(state);
  } catch (error) {
    Object.assign(state, {running:false, error:error.message});
    if (ui.smart === state) renderSmart();
  }
}
async function watchSmart(state) {
  const current = () => ui.smart === state;
  const handle = (event) => {
    state.since = event.n;
    if (event.type === 'progress') Object.assign(state, {text:event.text, agent:event.agent});
    if (event.type === 'plan') state.plan = event;
    if (event.type === 'warning') state.warnings.push(event.message);
    if (event.type === 'result') Object.assign(state, {running:false, result:event.result});
    if (event.type === 'error') Object.assign(state, {running:false, error:event.message});
  };
  let ended = false;
  try {
    const response = await fetch(`/api/jobs/${encodeURIComponent(state.job)}/events?since=${state.since}`);
    if (response.status === 404 && state.resumed) ended = true;
    else {
      if (!response.ok) throw new Error(`не удалось подключиться (${response.status})`);
      const reader = response.body.getReader(), decoder = new TextDecoder(); let buffer = '';
      while (true) {
        const {value,done} = await reader.read();
        buffer += decoder.decode(value,{stream:!done});
        const lines = buffer.split('\n'); buffer = lines.pop();
        if (done && buffer.trim()) lines.push(buffer);
        for (const line of lines) if (line.trim()) handle(JSON.parse(line));
        if (current()) state.result ? renderFeed(true) : renderSmart();
        if (done) break;
      }
      ended = state.running && state.cancelRequested;
      if (state.running && !ended) throw new Error('задание остановлено до результата');
    }
  } catch (error) {
    // Обрыв связи (в том числе при перезагрузке страницы) не снимает задание: запись нужна, чтобы подключиться снова.
    state.lost = error instanceof TypeError || error.name === 'AbortError';
    Object.assign(state, {running:false, error:state.lost ? 'связь с сервером прервалась' : error.message});
  }
  if (!current()) return;
  if (!state.lost) stored(SMART_JOB, null);
  if (ended) ui.smart = null;
  else if (state.result) stored(SMART_RESULT, {q:state.q, scope:state.scope, agent:state.agent, warnings:state.warnings, result:state.result});
  renderSmart(); renderFeed(!!state.result);
}
async function cancelSmart() {
  const state = ui.smart; if (!state?.running) return;
  state.cancelRequested = true;
  if (state.job) await api(`/api/jobs/${encodeURIComponent(state.job)}/cancel`,{});
}
function closeSmart() { ui.smart = null; stored(SMART_RESULT, null); stored(SMART_JOB, null); renderSmart(); renderFeed(true); }
async function pollLive() {
  try {
    const next = await api('/api/live'), changed = JSON.stringify(next) !== JSON.stringify(ui.live);
    ui.live = next;
    if (changed) { renderNav(); renderFeed(); renderLive(); if (ui.picking) renderRestore(); }
  } catch { /* Следующий опрос обновит статусы; команды обрабатывают собственные ошибки. */ }
}
function search() {
  renderSmartButton(); clearTimeout(searchTimer); searchController?.abort(); ui.snippets.clear(); renderFeed(true);
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
      ui.section = id.slice(4); ui.project = ''; ui.view = 'list'; ui.picking = false; ui.editing = false;
      writeHash(); search(); renderMain(); break;
    case 'wait-next': {
      const waits = waiting();
      const next = waits[(waits.findIndex(s => s.key === ui.key) + 1) % waits.length];
      if (next) select(next.key); break;
    }
    case 'project-row': ui.project = element.dataset.project; ui.section = 'mine'; ui.view = 'list'; writeHash(); search(); layout(); break;
    case 'project-filter-clear': ui.project = ''; writeHash(); search(); break;
    case 'row': ui.picking ? togglePick(element.dataset.key) : select(element.dataset.key); break;
    case 'related-chip': select(element.dataset.key); break;
    case 'back-to-list': ui.view = 'list'; writeHash(); layout(); break;
    case 'search-conv': case 'search-more': ui.inConv = !ui.inConv; $(test('search-conv')).setAttribute('aria-pressed',ui.inConv); search(); renderConversation(); break;
    case 'search-all': ui.section = 'all'; writeHash(); search(); break;
    case 'search-clear': ui.q = ''; $('#q').value = ''; search(); break;
    case 'smart-search': await startSmart(); break;
    case 'smart-retry': await startSmart(ui.smart.q, ui.smart.scope); break;
    case 'smart-cancel': await cancelSmart(); break;
    case 'smart-close': closeSmart(); break;
    case 'toggle-hide-done': ui.hideDone = !ui.hideDone; renderFeed(true); break;
    case 'toggle-last': ui.showLast = !ui.showLast; renderFeed(); break;
    case 'feed-more': ui.limit += 50; renderFeed(); break;
    case 'data-menu': ui.menu = ui.menu === 'data' ? '' : 'data'; renderMenus(); break;
    case 'session-menu': ui.menu = ui.menu === 'session' ? '' : 'session'; renderMenus(); break;
    case 'data-import': closeMenu(); $(test('data-import-file')).click(); break;
    case 'migrate-button': await change('/api/migrate',legacyData()); toast('Перенесено'); break;
    case 'rename': ui.editing = true; closeMenu(); renderTitle(); break;
    case 'rename-save': await saveName(); break;
    case 'rename-cancel': ui.editing = false; renderTitle(); break;
    case 'rename-reset': await saveName(true); break;
    case 'pin': await change('/api/pin',{key:s.key,pinned:!ui.state.pins.includes(s.key)}); break;
    case 'menu-done': case 'summary-done': await mark('done'); break;
    case 'menu-hide': await mark('hidden'); break;
    case 'copy-command': if (s.command) await copy(s.command,element); break;
    case 'copy-no-cd': await copy(s.command_no_cd,element); break;
    case 'menu-copy-id': await copy(s.id, null, `ID скопирован: ${s.id}`); closeMenu(); break;
    case 'menu-copy-path': await copy(s.cwd, null, `Путь скопирован: ${s.cwd}`); closeMenu(); break;
    case 'menu-explorer': case 'menu-vscode': await api('/api/reveal',{key:s.key,app:id === 'menu-explorer' ? 'explorer' : 'vscode'}); closeMenu(); break;
    case 'summary-run': case 'summary-retry': case 'summary-refresh': await startSummary(); break;
    case 'summary-toggle': ui.summaryOpen = !ui.summaryOpen; renderSummary(); break;
    case 'summary-apply': await change('/api/name',{key:s.key,name:ui.state.summaries[s.key].title}); break;
    case 'copy-next': await copy(`Продолжаем с того места, где остановились. Следующий шаг: ${ui.state.summaries[s.key].next_step.replace(/[.\s]+$/,'')}.`,element); break;
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
function moveSelection(offset) {
  if (!visible.length) return;
  const index = visible.findIndex(s => s.key === ui.key), nextIndex = Math.max(0,Math.min(visible.length-1,index + offset));
  ui.limit = Math.max(ui.limit,nextIndex+1);
  select(visible[nextIndex].key, false, true);
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
  $('#q').addEventListener('input',event => { ui.q = event.target.value; search(); });
  $('#feed').addEventListener('scroll',() => {
    if (renderingFeed) return;
    const feed = $('#feed');
    if (ui.section !== 'projects' && visible.length > ui.limit && feed.scrollTop + feed.clientHeight >= feed.scrollHeight - 160) { ui.limit += 50; renderFeed(); }
  });
  $(test('data-import-file')).addEventListener('change',async event => {
    const file = event.target.files[0]; if (!file) return;
    try { await change('/api/import',{data:JSON.parse(await file.text())}); toast('Данные загружены'); }
    catch (error) { toast(`Не удалось загрузить JSON: ${error.message}`); }
    event.target.value = '';
  });
  document.addEventListener('keydown',event => {
    const target = eventElement(event);
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && target?.id === 'q' && !event.isComposing) { event.preventDefault(); safely('smart-search',$(test('smart-search'))); return; }
    if (event.ctrlKey || event.altKey || event.metaKey || event.isComposing) return;
    const input = target?.closest('input,textarea,[contenteditable="true"]');
    if (input) {
      if (input.id === 'rn' && event.key === 'Escape') { event.preventDefault(); ui.editing = false; renderTitle(); }
      if (input.id !== 'q') return;
      if (event.key === 'Escape') { event.preventDefault(); ui.q = ''; input.value = ''; search(); input.blur(); }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); moveSelection(event.key === 'ArrowDown' ? 1 : -1); }
      if (event.key === 'Enter') { event.preventDefault(); const s = visible.find(s => s.key === ui.key) || visible[0]; if (s) select(s.key); input.blur(); }
      return;
    }
    if (event.key === '/' && !event.repeat) {
      event.preventDefault();
      if (matchMedia('(max-width: 899px)').matches && ui.view !== 'list') { ui.view = 'list'; writeHash(); layout(); }
      $('#q').focus();
    }
    if (event.key === 'Escape') { if (ui.menu) { closeMenu(); return; } if (ui.picking) exitPick(); }
    if (!event.repeat && event.key.toLowerCase() === 'c' && (ui.key || ui.picking)) safely(ui.picking ? 'copy-script' : 'copy-command',$(test(ui.picking ? 'copy-script' : 'copy-command')));
  });
  window.addEventListener('hashchange',() => { readHash(); ui.editing = false; ui.picking = false; search(); renderMain(); });
  window.addEventListener('popstate',() => { readHash(); ui.editing = false; ui.picking = false; search(); renderMain(); });
}
async function init() {
  readHash(); shell(); renderNav(); bind(); renderLoading();
  try {
    // Агенты, окружение и настройки нужны до первой отрисовки списка: иконки, меню, формат восстановления.
    const [state, config] = await Promise.all([api('/api/state'), api('/api/config')]);
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
    if (!sessionMap.has(ui.key)) ui.key = (ui.sessions.find(s => mine(s) && ui.state.pins.includes(s.key)) || ui.sessions.find(mine) || ui.sessions[0])?.key || '';
    writeHash(true); renderFeed(); renderMain(); resumeJobs(); pollLive();
    setInterval(pollLive,5000);
    setInterval(() => { const elapsed = $('.smart-elapsed'); if (elapsed && ui.smart?.running) elapsed.textContent = minutes(Date.now()/1000 - ui.smart.started); },1000);
  } catch (error) {
    $('#main').innerHTML = `<div class="loading-view" role="alert"><div class="loading-card">Не удалось прочитать сессии: ${esc(error.message)}<a class="btn" href="${esc(location.href)}">Повторить</a></div></div>`;
    ui.view = 'session'; layout();
  }
}
init();
