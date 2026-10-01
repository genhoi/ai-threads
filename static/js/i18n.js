// Переводы интерфейса: русский и английский. Язык приходит с сервера в /api/config → language
// (настройка language или язык браузера); до ответа сервера — язык браузера.
// Каждая страница добавляет свои строки через addStrings({ru: {...}, en: {...}}).
// Значение — строка с {параметрами} или функция от параметров, если нужны формы числа.

const strings = {ru: {}, en: {}};
let current = (navigator.language || '').toLowerCase().startsWith('ru') ? 'ru' : 'en';
document.documentElement.lang = current;

export function addStrings(dict) {
  for (const lang of ['ru', 'en']) Object.assign(strings[lang], dict[lang] || {});
}
export function setLanguage(lang) {
  if (lang !== 'ru' && lang !== 'en') return;
  current = lang;
  document.documentElement.lang = lang;
}
export function lang() { return current; }
// Для Intl и toLocale*: даты, время, дни недели.
export function locale() { return current === 'ru' ? 'ru-RU' : 'en-US'; }
export function t(key, params = {}) {
  const text = strings[current][key] ?? strings.ru[key] ?? key;
  if (typeof text === 'function') return text(params);
  return text.replace(/\{(\w+)\}/g, (match, name) => (name in params ? String(params[name]) : match));
}
// Формы числа: русский — одна, две, пять (1 сессия, 2 сессии, 5 сессий); английский — одна и много.
export function pluralRu(n, one, few, many) {
  const n10 = n % 10, n100 = n % 100;
  if (n10 === 1 && n100 !== 11) return one;
  if (n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14)) return few;
  return many;
}
export function pluralEn(n, one, other) { return n === 1 ? one : other; }
