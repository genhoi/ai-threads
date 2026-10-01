// Экран «Настройки»: агенты, сводки, список сессий, о приложении.
// Форма собирается из /api/config; «Сохранить» отправляет весь файл настроек в POST /api/settings.
import {toolIcon, registerAgents} from './icons.js';
import {addStrings, setLanguage, lang, locale, t} from './i18n.js';
import strings from './lang/settings.js';

addStrings(strings);

const $ = (selector) => document.querySelector(selector);
const test = (id) => `[data-testid="${id}"]`;
const esc = (value = '') => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const svg = (path, size = 16, width = 2, color = 'currentColor') =>
  `<svg width="${size}" height="${size}" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="${color}" stroke-width="${width}" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;
const spinner = (size) => `<span class="spin" style="width:${size}px;height:${size}px"></span>`;
// Как shlex.quote на сервере: без кавычек только безопасные символы.
const quote = (value) => !value ? "''" : /^[A-Za-z0-9_@%+=:,./-]+$/.test(value) ? value : `'${value.replaceAll("'", `'"'"'`)}'`;
const ICON = {
  logo: '<path d="M3 16c3-7 6 1 9-5s6 2 9-4" stroke="var(--accent)"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.6M12 18.6v2.6M21.2 12h-2.6M5.4 12H2.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8M18.5 18.5l-1.8-1.8M7.3 7.3L5.5 5.5"/><circle cx="12" cy="12" r="6.2"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
  again: '<path d="M20 12a8 8 0 1 1-2.3-5.6M20 4v5h-5"/>',
  warn: '<path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17.2v.1"/>',
  lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
  close: '<path d="M6 6l12 12M18 6L6 18"/>',
  next: '<path d="M9 6l6 6-6 6"/>',
};

// Умолчания приходят с сервера (`defaults` в /api/config): значения, равные им, в файл не пишутся.
let DEFAULTS = {summary: {}, digest: {}, search: {}, language: 'auto', temp_dirs: []};
const KEEP = ['open_with', 'tmux_session'];  // настроек нет на экране: переносятся из файла как есть
// Разделы: названия и описания — в словаре по ключам section.<key> и section.<key>.desc.
const SECTIONS = ['agents', 'summaries', 'list', 'about'].map(key => ({key, title: () => t(`section.${key}`), desc: () => t(`section.${key}.desc`)}));
// Кто отвечает и сколько ждать: настройки summary, digest и search, агенты — из /api/digest/models.
// Название, описание и подсказка блока — в словаре по ключам block.<kind>, block.<kind>.desc, block.<kind>.hint.
const BLOCKS = [{kind: 'summary', who: 'block.writer'}, {kind: 'digest', who: 'block.writer'}, {kind: 'search', who: 'block.searcher'}];
const autoModel = () => ({id: 'auto', name: t('auto'), available: true, hint: t('auto.hint')});
const LANGUAGES = [['auto', () => t('language.auto')], ['ru', () => 'Русский'], ['en', () => 'English']];
const GITHUB = 'https://github.com/genhoi/ai-threads';
const NARROW = matchMedia('(max-width: 899px)');

const ui = {config: null, saved: null, form: null, sessions: [], ready: false, models: [], hidden: [],
            section: 'agents', view: 'menu', open: {}, newDir: '', raw: {}, checking: false, checkedAt: 0,
            saving: false, saveError: '', menu: ''};
let diff = [], slots = new Map(), toastTimer;

async function api(path, body) {
  const response = await fetch(path, body === undefined ? {} :
    {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
  if (!response.ok) {
    let message = t('error.status', {status: response.status});
    try { message = (await response.json()).error || message; } catch { /* Ответ может быть без JSON. */ }
    throw new Error(message);
  }
  return response.json();
}
function toast(message, ok = true) {
  clearTimeout(toastTimer);
  $('#toast-area').innerHTML = `<div class="toast" data-testid="toast">${ok ? svg(ICON.check, 14, 2.6, 'var(--ok)') : ''}<span>${esc(message)}</span></div>`;
  toastTimer = setTimeout(() => { $('#toast-area').innerHTML = ''; }, ok ? 3000 : 7000);
}

// --- пути -------------------------------------------------------------------

function tilde(path) {
  const home = (ui.config?.env?.home || '').replace(/\/+$/, '');
  if (!path || !home) return path || '';
  return path === home ? '~' : path.startsWith(home + '/') ? '~' + path.slice(home.length) : path;
}
function expand(dir) { return dir === '~' || dir.startsWith('~/') ? ui.config.env.home + dir.slice(1) : dir; }
// Как str(Path(...)) на сервере: без повторных «/», без «.» и без «/» в конце.
function normalize(path) {
  const lead = /^\/\/(?!\/)/.test(path) ? '//' : path.startsWith('/') ? '/' : '';
  return lead + path.split('/').filter(part => part && part !== '.').join('/') || '.';
}
// Как Session.temp на сервере: папка сессии совпадает со скрытой или лежит внутри неё.
function hiddenCount(dirs) {
  const roots = dirs.map(dir => expand(dir).replace(/\/+$/, '') || '/');
  return ui.sessions.filter(s => {
    const cwd = (s.cwd || '').replace(/\/+$/, '');
    return !s.auto && roots.some(root => cwd === root || cwd.startsWith(root.replace(/\/+$/, '') + '/'));
  }).length;
}

// --- форма ------------------------------------------------------------------

function formOf(config) {
  const s = config.settings, stored = config.stored || {}, agents = {};
  for (const a of config.agents) {
    const own = stored.agents?.[a.id] || {};
    agents[a.id] = {enabled: a.enabled, home: own.home || '', program: own.program || '', skip_approvals: a.skip_approvals};
  }
  const blocks = Object.fromEntries(BLOCKS.map(b => [b.kind, {...DEFAULTS[b.kind], ...s[b.kind]}]));
  return {agents, ...blocks, language: s.language, temp_dirs: [...s.temp_dirs]};
}
// Файл настроек целиком: то, что в нём было, с правками формы и без значений по умолчанию.
function settingsOf(form) {
  const stored = ui.config.stored || {}, out = {}, agents = {};
  for (const key of KEEP) if (key in stored) out[key] = stored[key];
  for (const a of ui.config.agents) {
    const own = form.agents[a.id], values = {};
    if (!own.enabled) values.enabled = false;
    for (const field of ['home', 'program']) if (own[field].trim()) values[field] = own[field].trim();
    if (own.skip_approvals) values.skip_approvals = true;
    if (Object.keys(values).length) agents[a.id] = values;
  }
  if (Object.keys(agents).length) out.agents = agents;
  for (const {kind} of BLOCKS) {
    const values = {};
    for (const field of ['agent', 'timeout']) if (form[kind][field] !== DEFAULTS[kind][field]) values[field] = form[kind][field];
    if (Object.keys(values).length) out[kind] = values;
  }
  if (form.language !== DEFAULTS.language) out.language = form.language;
  if (JSON.stringify(form.temp_dirs) !== JSON.stringify(DEFAULTS.temp_dirs)) out.temp_dirs = form.temp_dirs;
  return out;
}
function flat(value, prefix = '', out = {}) {
  for (const [key, item] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (item && typeof item === 'object' && !Array.isArray(item)) flat(item, path, out); else out[path] = JSON.stringify(item);
  }
  return out;
}
function changes() {
  if (!ui.form) return [];
  const now = flat(ui.form), before = flat(ui.saved);
  return Object.keys(now).filter(key => now[key] !== before[key]);
}
// Путь с точкой на конце — все поля агента: «изменено» у карточки.
const dirty = (path) => path.endsWith('.') ? diff.some(key => key.startsWith(path)) : diff.includes(path);
const dot = (path, extra = '') => `<span class="fdot" data-dirty="${path}" title="${t('changed')}"${extra}${dirty(path) ? '' : ' hidden'}></span>`;

function applyConfig(config, reset = false) {
  if (config.defaults) DEFAULTS = config.defaults;
  // Язык страницы: настройка language или язык браузера. После сохранения render() перерисует шапку на нём.
  setLanguage(config.language);
  registerAgents(config.agents);
  const keep = !reset && ui.form && changes().length;
  ui.config = config;
  ui.saved = formOf(config);
  if (!keep) { ui.form = structuredClone(ui.saved); ui.raw = {}; ui.saveError = ''; }
  const addr = $(test('server-addr'));
  if (addr) addr.textContent = serverAddr();
}
const serverAddr = () => ui.config ? `127.0.0.1:${ui.config.port}` : location.host;
const agentOf = (id) => ui.config.agents.find(a => a.id === id);
const isInstalled = (a) => a.home_exists || !!a.program;

function dirCheck() {
  const value = ui.newDir.trim();
  if (!value || value === '~') return {ok: false, error: ''};
  if (!value.startsWith('/') && !value.startsWith('~/')) return {ok: false, error: t('dirs.badPath')};
  if (ui.form.temp_dirs.includes(value)) return {ok: false, error: t('dirs.duplicate')};
  return {ok: true, error: ''};
}
// Проверка поля по имени: t:<summary|digest|search>, dir.
function errorOf(check) {
  const [kind, a] = check.split(':');
  if (kind === 't') return ui.form[a].timeout === null ? t('timeout.error') : '';
  return kind === 'dir' ? dirCheck().error : '';
}
const errId = (check) => `e-${check.replaceAll(':', '-')}`;
const checkAttrs = (check) => {
  const error = errorOf(check);
  return ` data-check="${check}" aria-invalid="${!!error}"${error ? ` aria-describedby="${errId(check)}"` : ''}`;
};
function problemsNote() {
  return BLOCKS.some(b => errorOf(`t:${b.kind}`)) ? t('timeout.fix') : '';
}

// Примеры команды продолжения на настоящих сессиях агента: последняя ручная и, если у агента
// своя команда для автоматических (codex exec), последняя автоматическая.
function samples(a) {
  const pick = (auto) => {
    const own = ui.sessions.filter(s => s.tool === a.id && s.auto === auto);
    return own.find(s => s.cwd) || own[0];
  };
  const manual = pick(false), auto = a.resume_auto ? pick(true) : null;
  const project = (s) => s.project || t('noProject');
  return [manual ? {label: t('agent.exampleOn', {project: project(manual)}), session: manual}
                 : {label: t('agent.example'), session: {id: '<id>', cwd: '~/projects/demo', auto: false, plain: true}},
          ...auto ? [{label: t('agent.exampleAuto', {by: auto.by, project: project(auto)}), session: auto}] : []];
}
// Как Agent.resume_command и Session.command на сервере, но по ещё не сохранённой форме.
function commandOf(a, session) {
  const own = ui.form.agents[a.id], q = session.plain ? (value) => value : quote, program = own.program.trim();
  let command = (session.auto && a.resume_auto ? a.resume_auto : a.resume).replaceAll('{id}', q(session.id));
  const name = a.program_names.find(n => command.startsWith(n + ' '));
  if (program && name) command = quote(normalize(expand(program))) + command.slice(name.length);
  if (own.skip_approvals && a.skip_flag) command += ' ' + a.skip_flag;
  return session.cwd ? `cd -- ${q(session.cwd)} && ${command}` : command;
}
function status(a) {
  const n = a.sessions, home = tilde(a.home), program = tilde(a.program);
  if (ui.checking) return {kind: 'run', text: t('check.running')};
  // Сессии выключенного агента не читаются, поэтому их число неизвестно.
  if (!a.enabled && (a.home_exists || a.program)) {
    const found = a.program ? t('status.found', {program}) : t('status.foundHome', {home});
    return {kind: 'none', text: t('status.hidden', {found})};
  }
  if (a.home_exists && a.program) return {kind: 'ok', text: t('status.ok', {program, n, home})};
  if (a.home_exists && a.needs_program) return {kind: 'warn', text: t('status.noProgram', {n, home})};
  if (a.home_exists) return {kind: 'ok', text: t('status.homeOnly', {n, home})};
  if (a.program) return {kind: 'ok', text: t('status.programOnly', {program})};
  return {kind: 'none', text: t('status.none')};
}
function programWarn(a) {
  const own = ui.form.agents[a.id], name = a.program_names[0] || a.id;
  if (a.program) return '';
  if (own.program.trim() && own.program === ui.saved.agents[a.id].program) return t('program.broken');
  if (!own.program.trim() && a.home_exists) return t('program.missing', {name});
  return '';
}

// --- части, которые обновляются при вводе ----------------------------------

function slotHtml(name) {
  const [kind, ...rest] = name.split(':');
  if (kind === 'err') {
    const check = rest.join(':'), error = errorOf(check);
    return error ? `<span class="err" id="${errId(check)}" role="alert" data-testid="field-error">${svg(ICON.warn, 14)}${esc(error)}</span>` : '';
  }
  if (kind === 'cmd') return commandPreview(agentOf(rest[0]));
  if (kind === 'prog') {
    const text = programWarn(agentOf(rest[0]));
    return text ? `<span class="hint warn-hint" data-testid="program-warn">${esc(text)}</span>` : '';
  }
  if (kind === 'dir') return `<button type="button" class="btn" id="add-dir-btn" data-testid="add-dir"${dirCheck().ok ? '' : ' disabled'}>${t('dirs.addButton')}</button>`;
  if (kind === 'count') {
    return `<span class="hint" data-testid="hidden-count">${esc(t('dirs.count', {n: hiddenCount(ui.form.temp_dirs)}))}</span>`;
  }
  if (kind === 'bar') return saveBar();
  return '';
}
function slot(name) {
  const html = slotHtml(name);
  slots.set(name, html);
  return `<div class="slot" data-slot="${name}">${html}</div>`;
}

function commandPreview(a) {
  return samples(a).map(({label, session}) => `<div class="preview-box"><span class="hint" data-testid="preview-label">${esc(label)}</span>
    <div class="preview" data-testid="preview"><span class="prompt">$ </span>${esc(commandOf(a, session))}</div></div>`).join('');
}

function saveBar() {
  const n = diff.length, note = problemsNote();
  if (!n) return '';
  return `<div class="savebar" role="region" aria-label="${t('save.region')}" data-testid="savebar">
    <span class="fdot"></span>
    <span class="save-text">${t('save.text')}<span class="save-count" data-testid="save-count">${esc(t('save.count', {n}))}</span></span>
    ${note ? `<span class="save-note" data-testid="save-problems">${esc(note)}</span>` : ''}
    ${ui.saveError ? `<span class="save-note" role="alert" data-testid="save-error">${esc(t('save.failed', {error: ui.saveError}))}</span>` : ''}
    <div class="grow"></div>
    <button type="button" class="btn" id="revert" data-testid="revert"${ui.saving ? ' disabled' : ''}>${t('save.revert')}</button>
    <button type="button" class="btn btn-primary" id="save" data-testid="save"${note || ui.saving ? ' disabled' : ''} title="${note ? t('save.fixFirst') : t('save.hint')}">${ui.saving ? t('save.saving') : t('save.save')}</button>
  </div>`;
}

// --- разделы ----------------------------------------------------------------

function checkNote() {
  if (ui.checking) return '';
  if (Date.now() - ui.checkedAt < 60000) return t('check.just');
  const time = new Date(ui.checkedAt).toLocaleTimeString(locale(), {hour: lang() === 'ru' ? '2-digit' : 'numeric', minute: '2-digit'});
  return t('check.at', {time});
}
function sectionHead(section) {
  const check = section.key !== 'agents' ? '' : `<div class="check">
      <span class="check-note" data-testid="check-note" aria-live="polite">${esc(checkNote())}</span>
      <button type="button" class="btn" id="check-again" data-testid="check-again"${ui.checking ? ' disabled' : ''}>${ui.checking ? `${spinner(13)}${t('check.running')}` : `${svg(ICON.again, 14)}${t('check.again')}`}</button>
    </div>`;
  return `<div class="sec-head"><div class="sec-titles"><h2 id="section-title" tabindex="-1" data-testid="section-title">${esc(section.title())}</h2>
    <span class="sec-desc">${esc(section.desc())}</span></div>${check}</div>`;
}

function emptyPanel() {
  const agents = ui.config.agents, example = agents.find(a => a.home_env)?.home_env || 'CODEX_HOME';
  const code = (value) => `<code>${esc(value)}</code>`;
  const rows = agents.map(a => `${toolIcon(a.id, 18)}<span>${esc(a.name)}</span>
    <span class="searched">${a.needs_program ? t('empty.programHtml', {name: code(a.program_names[0] || a.id)}) : ''}</span>
    <span class="searched">${t('empty.homeHtml', {path: code(a.home_source === 'default' ? a.home_default : tilde(a.home))})}</span>`).join('');
  return `<section class="panel" aria-label="${t('empty.title')}" data-testid="agents-empty">
    <div class="panel-head"><h3 class="h3">${t('empty.title')}</h3>
      <p class="panel-lead">${esc(t('empty.lead'))}</p></div>
    <div class="panel-part"><span class="part-title">${t('empty.where')}</span>
      <div class="searched-grid" data-testid="searched">${rows}</div>
      <span class="hint searched-path" data-testid="searched-path">PATH: <code>${esc(ui.config.env.path)}</code></span></div>
    <div class="panel-part"><span class="part-title">${t('empty.how')}</span>
      <ol class="steps">
        <li>${esc(t('empty.step1'))}</li>
        <li>${t('empty.step2Html', {env: code(example)})}</li>
        <li>${esc(t('empty.step3'))}</li>
      </ol></div>
  </section>`;
}

function homeRow(a) {
  const id = `f-${a.id}-home`, own = ui.form.agents[a.id];
  const field = a.home_source === 'env' ? `<div class="fline">
      <input id="${id}" class="input mono" value="${esc(tilde(a.home))}" disabled data-testid="agent-home">
      <span class="pill mute lock" data-testid="home-env">${svg(ICON.lock, 11, 2.2)}${esc(t('agent.homeEnv', {env: a.home_env}))}</span></div>
    <span class="hint">${esc(t('agent.homeEnvHint', {env: a.home_env}))}</span>`
    : `<input id="${id}" class="input mono" value="${esc(own.home)}" placeholder="${esc(a.home_default)}" data-testid="agent-home" data-agent="${a.id}" data-field="home" autocomplete="off" spellcheck="false">`;
  return `<div class="frow"><label class="flabel" for="${id}">${dot(`agents.${a.id}.home`)}${t('agent.home')}</label><div class="fbox">${field}</div></div>`;
}
function programRow(a) {
  const id = `f-${a.id}-program`, own = ui.form.agents[a.id];
  const placeholder = a.program ? t('agent.programFound', {path: tilde(a.program)}) : t('agent.programMissing', {name: a.program_names[0] || a.id});
  return `<div class="frow"><label class="flabel" for="${id}">${dot(`agents.${a.id}.program`)}${t('agent.program')}</label><div class="fbox">
    <input id="${id}" class="input mono" value="${esc(own.program)}" placeholder="${esc(placeholder)}" data-testid="agent-program" data-agent="${a.id}" data-field="program" autocomplete="off" spellcheck="false">
    ${slot(`prog:${a.id}`)}</div></div>`;
}
function commandRow(a) {
  if (!a.resume) return a.resume_hint ? `<div class="frow"><span class="flabel">${t('agent.resume')}</span>
    <div class="fbox"><span class="fnote" data-testid="resume-hint">${esc(a.resume_hint)}</span></div></div>` : '';
  const id = `skip-${a.id}`, skip = !a.skip_flag ? '' : `<label class="skip" for="${id}">
      <input type="checkbox" id="${id}" data-testid="agent-skip" data-agent="${a.id}"${ui.form.agents[a.id].skip_approvals ? ' checked' : ''} aria-describedby="${id}-hint">
      <span class="skip-text"><span>${t('agent.skip')}</span><span class="hint" id="${id}-hint">${t('agent.skipHintHtml', {flag: `<code>${esc(a.skip_flag)}</code>`})}</span></span></label>`;
  return `<div class="frow"><span class="flabel start">${dot(`agents.${a.id}.skip_approvals`)}${t('agent.resume')}</span>
    <div class="fbox">${skip}${slot(`cmd:${a.id}`)}</div></div>`;
}
function manualHint(a) {
  if (a.home_source === 'env' && !a.needs_program) return '';
  const key = a.home_source === 'env' ? 'agent.manualProgram' : a.needs_program ? 'agent.manualBoth' : 'agent.manualHome';
  return `<span class="hint manual-hint">${t(key)}</span>`;
}
function agentCard(a, heading) {
  const own = ui.form.agents[a.id], st = status(a), installed = isInstalled(a), open = installed || !!ui.open[a.id];
  const mark = st.kind === 'run' ? spinner(12) : `<span class="dot ${st.kind}"></span>`;
  const color = st.kind === 'warn' ? 'var(--warn)' : st.kind === 'none' ? 'var(--text-3)' : 'var(--text-2)';
  const control = installed
    ? `<label class="show-toggle"><span>${t('agent.show')}</span><button type="button" class="switch" role="switch" id="sw-${a.id}" data-testid="agent-enabled" data-agent="${a.id}" aria-checked="${own.enabled}" aria-label="${esc(t('agent.showName', {name: a.name}))}"></button></label>`
    : `<button type="button" class="btn sm" id="open-${a.id}" data-testid="agent-open" data-agent="${a.id}" aria-expanded="${open}">${open ? t('agent.collapse') : t('agent.setManually')}</button>`;
  const body = !open ? '' : `<div class="card-body">${homeRow(a)}${a.needs_program ? programRow(a) : ''}${installed ? commandRow(a) : manualHint(a)}</div>`;
  return `${heading ? `<div class="missing-heading" data-testid="missing-heading">${t('agent.missing')}</div>` : ''}
  <article class="card${installed ? '' : ' muted'}" aria-label="${esc(a.name)}" data-testid="agent-card" data-agent="${a.id}">
    <div class="card-head">${toolIcon(a.id, 28)}
      <div class="card-main"><div class="card-title"><span class="card-name">${esc(a.name)}</span><span class="pill mute" data-dirty="agents.${a.id}." data-testid="agent-dirty"${dirty(`agents.${a.id}.`) ? '' : ' hidden'}>${t('agent.changed')}</span></div>
        <div class="card-status">${mark}<span class="status-text" style="color:${color}" title="${esc(st.text)}" data-testid="agent-status">${esc(st.text)}</span></div></div>
      ${control}</div>${body}
  </article>`;
}
function agentsSection() {
  const agents = ui.config.agents, found = agents.filter(isInstalled), missing = agents.filter(a => !isInstalled(a));
  return `${found.length ? '' : emptyPanel()}${found.map(a => agentCard(a, false)).join('')}${missing.map((a, i) => agentCard(a, !i && found.length > 0)).join('')}`;
}

function summariesSection() {
  const none = !ui.models.some(m => m.available);
  const blocks = BLOCKS.map(b => {
    const own = ui.form[b.kind], check = `t:${b.kind}`;
    const title = t(`block.${b.kind}`), who = t(b.who);
    const minutes = own.timeout === null ? ui.raw[b.kind] ?? '' : String(+(own.timeout / 60).toFixed(2));
    const buttons = [autoModel(), ...ui.models].map(m => `<button type="button" id="pick-${b.kind}-${m.id}" data-testid="pick-${m.id}" data-kind="${b.kind}" data-agent="${m.id}" aria-pressed="${own.agent === m.id}"${m.available ? '' : ' disabled'} title="${esc(m.hint)}">${m.id === 'auto' ? '' : toolIcon(m.id, 14)}${esc(m.name)}</button>`).join('');
    return `<section class="panel" aria-label="${esc(title)}" data-testid="sum-${b.kind}">
      <div class="panel-head"><h3 class="h3">${esc(title)}</h3><span class="hint panel-desc">${esc(t(`block.${b.kind}.desc`))}</span></div>
      <div class="frow"><span class="flabel">${dot(`${b.kind}.agent`)}${esc(who)}</span><div class="fbox">
        <div class="seg-ctl" role="group" aria-label="${esc(`${who}: ${title}`)}">${buttons}</div>
        ${none ? `<span class="hint warn-hint" data-testid="no-runner">${t('noRunner')}</span>` : ''}</div></div>
      <div class="frow"><label class="flabel" for="t-${b.kind}">${dot(`${b.kind}.timeout`)}${t('timeout')}</label><div class="fbox">
        <div class="fline wrap"><input id="t-${b.kind}" class="input num${errorOf(check) ? ' invalid' : ''}" type="number" min="1" max="60" step="1" value="${esc(minutes)}" data-testid="timeout" data-kind="${b.kind}"${checkAttrs(check)}>
          <span class="unit">${t('timeout.unit')}</span><span class="hint timeout-hint">${esc(t(`block.${b.kind}.hint`))}</span></div>${slot(`err:${check}`)}</div></div>
    </section>`;
  }).join('');
  // Язык интерфейса и сводок: «Авто» — язык браузера; названия языков — на самих языках.
  const languages = LANGUAGES.map(([code, label]) => `<button type="button" id="lang-${code}" data-testid="lang-${code}" aria-pressed="${ui.form.language === code}"${code === 'auto' ? ` title="${esc(t('language.autoHint'))}"` : ` lang="${code}"`}>${esc(label())}</button>`).join('');
  return `${blocks}<section class="panel" aria-label="${t('common')}" data-testid="sum-common">
    <div class="frow"><span class="flabel">${dot('language')}${t('language')}</span><div class="fbox">
      <div class="seg-ctl lang" role="group" aria-label="${t('language')}">${languages}</div>
      <span class="hint">${t('language.hint')}</span></div></div>
  </section>`;
}

function listSection() {
  const dirs = ui.form.temp_dirs;
  const pills = dirs.map((dir, i) => `<span class="pathpill" data-testid="hidden-dir" data-path="${esc(dir)}">${esc(dir)}<button type="button" id="rm-dir-${i}" data-testid="hidden-dir-remove" data-path="${esc(dir)}" aria-label="${esc(t('dirs.removeDir', {dir}))}" title="${t('dirs.remove')}">${svg(ICON.close, 12, 2.4)}</button></span>`).join('');
  return `<section class="panel" aria-label="${t('dirs.title')}" data-testid="hidden-dirs">
    <div class="panel-head gap4"><h3 class="h3 with-dot">${t('dirs.title')}${dot('temp_dirs')}</h3>
      <span class="panel-text">${esc(t('dirs.text'))}</span></div>
    <div class="pills">${pills}${dirs.length ? '' : `<span class="hint" data-testid="hidden-empty">${esc(t('dirs.empty'))}</span>`}</div>
    <div class="fbox add-box"><div class="fline"><label for="add-dir" class="sr">${t('dirs.add')}</label>
      <input id="add-dir" class="input mono" placeholder="${esc(t('dirs.addPlaceholder'))}" value="${esc(ui.newDir)}" data-testid="add-dir-input"${checkAttrs('dir')} autocomplete="off" spellcheck="false">${slot('dir')}</div>
      ${slot('err:dir')}</div>
    ${slot('count')}
  </section>`;
}

function aboutSection() {
  const c = ui.config;
  return `<section class="panel" aria-label="${t('section.about')}" data-testid="about"><dl class="dl">
    <dt>${t('about.addr')}</dt><dd><code data-testid="about-addr">127.0.0.1:${esc(c.port)}</code><span class="hint">${t('about.addrHint')}</span></dd>
    <dt>${t('about.data')}</dt><dd><code data-testid="about-data">${esc(tilde(c.data_dir))}</code><span class="hint">${t('about.dataHint')}</span></dd>
    <dt>${t('about.settings')}</dt><dd><code data-testid="about-settings">${esc(tilde(c.settings_path))}</code><span class="hint">${t('about.settingsHint')}</span></dd>
    <dt>${t('about.version')}</dt><dd><span data-testid="about-version">${esc(c.version)}</span><a href="${GITHUB}" target="_blank" rel="noopener" class="about-link" data-testid="about-github">${t('about.github')}</a></dd>
  </dl></section>`;
}

function metas() {
  const agents = ui.config.agents, dirs = ui.form.temp_dirs.length, wanted = ui.form.summary.agent;
  const writer = wanted === 'auto' ? ui.models.find(m => m.available) : ui.models.find(m => m.id === wanted);
  return {agents: t('meta.agents', {found: agents.filter(isInstalled).length, total: agents.length}),
          summaries: writer ? writer.name : t('meta.noAgent'), list: t('meta.dirs', {n: dirs}), about: ui.config.version};
}
function sideMenu() {
  const meta = metas();
  return `<h1 class="side-title">${t('settings')}</h1>
    ${SECTIONS.map(s => `<button type="button" class="mitem" id="m-${s.key}" data-testid="section-${s.key}" data-section="${s.key}"${s.key === ui.section ? ' aria-current="page"' : ''}>${esc(s.title())}<span class="meta">${esc(meta[s.key])}</span></button>`).join('')}
    <div class="grow"></div>
    <div class="hint side-path" data-testid="settings-path">${t('settings.storedIn')}<br><code>${esc(tilde(ui.config.settings_path))}</code></div>`;
}
function narrowMenu() {
  const meta = metas();
  return `<h1 class="narrow-title">${t('settings')}</h1><div class="mrows">
    ${SECTIONS.map(s => `<button type="button" class="mrow" id="m-${s.key}" data-testid="section-${s.key}" data-section="${s.key}">
      <span class="mrow-text"><span class="mrow-title">${esc(s.title())}</span><span class="mrow-desc">${esc(s.desc())}</span></span>
      <span class="mrow-meta">${esc(meta[s.key])}</span>${svg(ICON.next, 14, 2, 'var(--text-3)')}</button>`).join('')}</div>
    <div class="hint narrow-path" data-testid="settings-path">${t('settings.storedIn')} <code>${esc(tilde(ui.config.settings_path))}</code></div>`;
}
function content(narrow) {
  const section = SECTIONS.find(s => s.key === ui.section);
  const body = {agents: agentsSection, summaries: summariesSection, list: listSection, about: aboutSection}[section.key]();
  const problem = ui.config.settings_error ? `<div class="settings-error" role="alert" data-testid="settings-error">${svg(ICON.warn, 14)}<span>${esc(t('settingsError', {error: ui.config.settings_error}))}</span></div>` : '';
  return `<div class="content" data-testid="section" data-section="${section.key}">
    ${narrow ? `<button type="button" class="link back-link" id="back-to-menu" data-testid="back-to-menu">${t('settings.back')}</button>` : ''}
    ${sectionHead(section)}${problem}${body}</div>`;
}

// --- отрисовка --------------------------------------------------------------

function rememberFocus() {
  const element = document.activeElement;
  if (!element?.id || element === document.body) return null;
  let start = null, end = null;
  try { start = element.selectionStart; end = element.selectionEnd; } catch { /* у кнопок выделения нет */ }
  return {id: element.id, start, end};
}
function restoreFocus(saved) {
  const element = saved && document.getElementById(saved.id);
  if (!element || element === document.activeElement || element.disabled) return;
  element.focus({preventScroll: true});
  if (saved.start !== null && saved.start !== undefined) try { element.setSelectionRange(saved.start, saved.end); } catch { /* поле без выделения */ }
}
function render() {
  if (!ui.config) return;
  diff = changes(); slots = new Map();
  const focus = rememberFocus(), top = $('#pane').scrollTop, narrow = NARROW.matches;
  // Язык сменился (сохранили другой или поменяли файл настроек): шапку и каркас — заново.
  if (shellLanguage !== lang()) shell();
  const pane = $('#pane');
  $('#side').innerHTML = narrow ? '' : sideMenu();
  pane.innerHTML = narrow && ui.view === 'menu' ? narrowMenu() : content(narrow);
  pane.scrollTop = top;
  $('#savebar').innerHTML = slot('bar');
  restoreFocus(focus);
}
// После ввода: обновить отметки, ошибки, примеры и полосу сохранения, не трогая поля.
function refresh() {
  diff = changes();
  const focus = rememberFocus();
  for (const element of document.querySelectorAll('[data-dirty]')) element.hidden = !dirty(element.dataset.dirty);
  for (const input of document.querySelectorAll('[data-check]')) {
    const error = errorOf(input.dataset.check);
    input.classList.toggle('invalid', !!error && input.id !== 'add-dir');
    input.setAttribute('aria-invalid', String(!!error));
    if (error) input.setAttribute('aria-describedby', errId(input.dataset.check)); else input.removeAttribute('aria-describedby');
  }
  for (const element of document.querySelectorAll('[data-slot]')) {
    const html = slotHtml(element.dataset.slot);
    if (slots.get(element.dataset.slot) !== html) { slots.set(element.dataset.slot, html); element.innerHTML = html; }
  }
  restoreFocus(focus);
}
function renderNav() {
  const projects = new Set(ui.sessions.filter(s => !s.auto && !s.temp && !ui.hidden.includes(s.key)).map(s => s.project)).size;
  $(test('nav-all') + ' .n').textContent = ui.ready ? ui.sessions.length : '';
  $(test('nav-projects') + ' .n').textContent = ui.ready ? projects : '';
}
function renderMenus() {
  $(test('data-menu')).setAttribute('aria-expanded', ui.menu === 'data');
  $('#data-popup').innerHTML = ui.menu === 'data' ?
    `<div class="menu" role="menu"><a role="menuitem" data-testid="data-export" href="/api/export" download>${t('data.export')}</a><button type="button" role="menuitem" data-testid="data-import">${t('data.import')}</button></div>` : '';
}
let shellLanguage = '';
function shell() {
  shellLanguage = lang();
  document.title = t('page.title');
  $('#app').innerHTML = `<header class="topbar">
    <div class="brand">${svg(ICON.logo, 18, 2.2)}<span>${t('brand')}</span></div>
    <nav class="nav" aria-label="${t('nav.label')}">
      <a href="/" data-testid="nav-mine">${t('nav.mine')}</a>
      <a href="/#section=all" data-testid="nav-all">${t('nav.all')}<span class="n"></span></a>
      <a href="/#section=projects" data-testid="nav-projects">${t('nav.projects')}<span class="n"></span></a>
      <a href="/digest.html" data-testid="nav-digest">${t('nav.digest')}</a>
    </nav>
    <div class="grow"></div>
    <span class="addr" data-testid="server-addr">${esc(serverAddr())}</span>
    <div class="menu-anchor"><button type="button" class="btn" data-testid="data-menu" aria-haspopup="menu" aria-expanded="false">${t('data.menu')}</button><div id="data-popup"></div></div>
    <a href="/settings.html" class="icon-btn on" data-testid="settings-link" aria-label="${t('settings')}" title="${t('settings')}" aria-current="page">${svg(ICON.gear, 18, 1.8)}</a>
    <input type="file" accept="application/json,.json" data-testid="data-import-file" hidden>
  </header>
  <div class="settings-body">
    <aside class="side" id="side" aria-label="${t('settings.sections')}"></aside>
    <div class="settings-main"><div class="pane scroll" id="pane"><div class="loading-view"><div class="loading-card">${t('loading')}</div></div></div><div class="slot" id="savebar"></div></div>
  </div>
  <div id="toast-area" role="status" aria-live="polite"></div>`;
  renderNav();
  renderMenus();
}

// --- действия ---------------------------------------------------------------

function readHash() {
  const section = new URLSearchParams(location.hash.slice(1)).get('section');
  const known = SECTIONS.some(s => s.key === section);
  ui.section = known ? section : 'agents';
  ui.view = known ? 'section' : 'menu';
}
function go(section) {
  const previous = ui.section, hash = section ? `#section=${section}` : '';
  if (location.hash !== hash) history.pushState(null, '', hash || location.pathname);
  readHash();
  $('#pane').scrollTop = 0;
  render();
  if (NARROW.matches) $(section ? '#section-title' : `#m-${previous}`)?.focus();
}
function addDir() {
  if (!dirCheck().ok) return;
  ui.form.temp_dirs.push(ui.newDir.trim());
  ui.newDir = '';
  render();
  $('#add-dir')?.focus();
}
async function recheck() {
  if (ui.checking) return;
  const refocus = document.activeElement?.id === 'check-again';
  ui.checking = true; render();
  try {
    // Проверка на сервере мгновенная; короткая пауза, чтобы было видно, что она прошла.
    const [config, feed, models] = await Promise.all([api('/api/config?refresh=1'), api('/api/sessions'), api('/api/digest/models'),
                                                       new Promise(resolve => setTimeout(resolve, 300))]);
    ui.sessions = feed.sessions; ui.ready = true; ui.models = models;
    applyConfig(config);
    ui.checkedAt = Date.now(); ui.checking = false;
    render(); renderNav();
    toast(t('check.done', {found: config.agents.filter(isInstalled).length, total: config.agents.length}));
  } catch (error) {
    ui.checking = false; render();
    toast(t('check.failed', {error: error.message}), false);
  }
  if (refocus) $('#check-again')?.focus();
}
async function save() {
  if (ui.saving || problemsNote()) return;
  ui.saving = true; ui.saveError = ''; refresh();
  let config;
  try {
    config = await api('/api/settings', {settings: settingsOf(ui.form)});
  } catch (error) {
    ui.saving = false; ui.saveError = error.message; refresh();
    $('#save')?.focus();
    return;
  }
  // Сохранение меняет доступность агентов для сводок и скрытые сессии. Их перечитываем до
  // перерисовки: вторая перерисовка следом за первой съела бы клик, пришедшийся между ними.
  try {
    const [models, feed] = await Promise.all([api('/api/digest/models'), api('/api/sessions')]);
    ui.models = models; ui.sessions = feed.sessions;
  } catch { /* останутся прежние */ }
  ui.saving = false;
  applyConfig(config, true);
  render(); renderNav();
  toast(t('save.done'));
}
function onInput(input) {
  const {agent, field, kind} = input.dataset;
  if (input.type === 'checkbox' && agent) ui.form.agents[agent].skip_approvals = input.checked;
  else if (agent && field) ui.form.agents[agent][field] = input.value;
  else if (input.id === 'add-dir') ui.newDir = input.value;
  else if (kind) {
    const minutes = Number(input.value);
    ui.raw[kind] = input.value;
    ui.form[kind].timeout = input.value.trim() && minutes >= 1 && minutes <= 60 ? Math.round(minutes * 60) : null;
  } else return;
  refresh();
}
async function action(id, element) {
  const {agent, kind, section, path} = element.dataset, form = ui.form;
  if (id.startsWith('section-')) return go(section);
  if (id.startsWith('pick-')) { form[kind].agent = agent; return render(); }
  switch (id) {
    case 'back-to-menu': return go('');
    case 'check-again': return recheck();
    case 'agent-enabled': form.agents[agent].enabled = !form.agents[agent].enabled; return render();
    case 'agent-open': ui.open[agent] = !ui.open[agent]; return render();
    case 'lang-auto': case 'lang-ru': case 'lang-en': form.language = id.slice('lang-'.length); return render();
    case 'hidden-dir-remove': {
      const index = form.temp_dirs.indexOf(path);
      if (index >= 0) form.temp_dirs.splice(index, 1);
      render();
      return ($(`#rm-dir-${index}`) || $(`#rm-dir-${index - 1}`) || $('#add-dir'))?.focus();
    }
    case 'add-dir': return addDir();
    case 'revert': ui.form = structuredClone(ui.saved); ui.raw = {}; ui.saveError = ''; return render();
    case 'save': return save();
    case 'data-menu': ui.menu = ui.menu === 'data' ? '' : 'data'; return renderMenus();
    case 'data-import': ui.menu = ''; renderMenus(); return $(test('data-import-file')).click();
  }
}

function bind() {
  document.addEventListener('click', event => {
    if (ui.menu && !event.target.closest('.menu,.menu-anchor')) { ui.menu = ''; renderMenus(); }
    // Ссылки, поля и подписи работают по умолчанию; подпись переключателя сама нажимает его кнопку.
    const element = event.target.closest('button[data-testid]');
    if (!element || element.disabled || !ui.config && !element.closest('.topbar')) return;
    event.preventDefault();
    Promise.resolve(action(element.dataset.testid, element)).catch(error => toast(error.message, false));
  });
  document.addEventListener('input', event => { if (ui.form && event.target.matches('input')) onInput(event.target); });
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && ui.menu) { ui.menu = ''; renderMenus(); $(test('data-menu')).focus(); }
    if (event.key === 'Enter' && event.target.id === 'add-dir') { event.preventDefault(); addDir(); }
  });
  // Шапку перерисовывает смена языка, поэтому выбор файла слушает документ, а не само поле.
  document.addEventListener('change', async event => {
    if (!event.target.matches(test('data-import-file'))) return;
    const file = event.target.files[0];
    if (!file) return;
    try {
      const state = await api('/api/import', {data: JSON.parse(await file.text())});
      ui.hidden = state.hidden || ui.hidden;
      renderNav();
      toast(t('data.imported'));
    } catch (error) { toast(t('data.importFailed', {error: error.message}), false); }
    event.target.value = '';
  });
  window.addEventListener('beforeunload', event => {
    if (changes().length) { event.preventDefault(); event.returnValue = ''; }
  });
  window.addEventListener('popstate', () => { readHash(); render(); });
  window.addEventListener('hashchange', () => { readHash(); render(); });
  NARROW.addEventListener('change', render);
  // «проверено только что» со временем становится «проверено в 10:14».
  setInterval(() => { const note = $(test('check-note')); if (note) note.textContent = checkNote(); }, 15000);
}

async function loadSessions() {
  while (true) {
    const feed = await api('/api/sessions');
    ui.sessions = feed.sessions;
    if (feed.ready) break;
    await new Promise(resolve => setTimeout(resolve, 300));
  }
  ui.ready = true;
  renderNav();
  refresh();
}
async function init() {
  readHash();
  let failure = null;
  try {
    const [config, state, models] = await Promise.all([api('/api/config'), api('/api/state'), api('/api/digest/models')]);
    ui.hidden = state.hidden || [];
    ui.models = models;
    // Здесь же выбирается язык: каркас страницы ниже рисуется уже на нём.
    applyConfig(config, true);
    ui.checkedAt = Date.now();
    // Ни одного агента: первая карточка открыта, чтобы было видно, где указать пути.
    if (!config.agents.some(isInstalled) && config.agents[0]) ui.open[config.agents[0].id] = true;
  } catch (error) { failure = error; }
  shell();
  bind();
  try {
    if (failure) throw failure;
    render();
    await loadSessions();
  } catch (error) {
    $('#pane').innerHTML = `<div class="loading-view" role="alert"><div class="loading-card">${esc(t('error.load', {error: error.message}))}<a class="btn" href="${esc(location.href)}">${t('retry')}</a></div></div>`;
  }
}
init();
