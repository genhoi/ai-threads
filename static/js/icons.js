// Иконки и названия агентов. Реестр заполняет registerAgents() из ответа /api/config.
// Встроенные иконки четырёх агентов — запас на время, пока конфиг не пришёл.
const GRAY = '#5a5e66';
const esc = (value = '') => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
export const TOOL_NAMES = { claude: "Claude Code", codex: "Codex", grok: "Grok", kimi: "Kimi" };
const COLORS = {};
const SVG = {
  claude: '<svg viewBox="0 0 24 24" role="img" aria-label="Claude Code"><rect x="0" y="0" width="24" height="24" rx="6" fill="#c4613a"></rect><path d="M12 5.2v13.6M6.1 8.6l11.8 6.8M6.1 15.4l11.8-6.8" stroke="#ffffff" stroke-width="2.2" stroke-linecap="round" fill="none"></path></svg>',
  codex: '<svg viewBox="0 0 24 24" role="img" aria-label="Codex"><rect x="0" y="0" width="24" height="24" rx="6" fill="#3b4859"></rect><path d="M12 4.8l6.2 3.6v7.2L12 19.2l-6.2-3.6V8.4z" stroke="#ffffff" stroke-width="2" stroke-linejoin="round" fill="none"></path><circle cx="12" cy="12" r="1.9" fill="#ffffff"></circle></svg>',
  grok: '<svg viewBox="0 0 24 24" role="img" aria-label="Grok"><rect x="0.5" y="0.5" width="23" height="23" rx="5.5" fill="#101114" stroke="#5a5e66"></rect><circle cx="12" cy="12" r="5.8" stroke="#ffffff" stroke-width="2" fill="none"></circle><path d="M6.6 17.4L17.4 6.6" stroke="#ffffff" stroke-width="2" stroke-linecap="round"></path></svg>',
  kimi: '<svg viewBox="0 0 24 24" role="img" aria-label="Kimi"><rect x="0" y="0" width="24" height="24" rx="6" fill="#2a6fd6"></rect><path d="M14.8 5.4a6.9 6.9 0 1 0 4 11.5 5.4 5.4 0 0 1-4-11.5z" fill="#ffffff"></path></svg>',
};

export function registerAgents(agents) {
  for (const agent of agents || []) {
    if (!agent?.id) continue;
    TOOL_NAMES[agent.id] = agent.name || agent.id;
    if (agent.color) COLORS[agent.id] = agent.color;
    if (agent.icon) SVG[agent.id] = agent.icon;
  }
}

export function toolName(tool) { return TOOL_NAMES[tool] || tool || 'Агент'; }

// Запасная иконка: скруглённый квадрат цвета агента (или серый) с первой буквой названия.
function letterIcon(tool) {
  const name = toolName(tool), letter = (name.trim()[0] || '?').toUpperCase();
  return `<svg viewBox="0 0 24 24" role="img" aria-label="${esc(name)}"><rect x="0" y="0" width="24" height="24" rx="6" fill="${esc(COLORS[tool] || GRAY)}"></rect><text x="12" y="16.3" text-anchor="middle" font-family="system-ui, sans-serif" font-size="12.5" font-weight="600" fill="#ffffff">${esc(letter)}</text></svg>`;
}

export function toolIcon(tool, size = 18) {
  const svg = (SVG[tool] || letterIcon(tool)).replace("<svg ", `<svg width="${size}" height="${size}" `);
  return `<span class="tool-icon" title="${esc(toolName(tool))}" style="display:inline-flex;flex-shrink:0;width:${size}px;height:${size}px;line-height:0">${svg}</span>`;
}
