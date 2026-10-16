// Время и числа по-русски для строк, плашек и подсказок
import { now } from './clock';

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];

/** 1 файл, 2 файла, 5 файлов */
export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

function startOfDay(t: number): number {
  const d = new Date(t * 1000);
  d.setHours(0, 0, 0, 0);
  return d.getTime() / 1000;
}

function hm(t: number): string {
  const d = new Date(t * 1000);
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function dayMonth(t: number): string {
  const d = new Date(t * 1000);
  const year = new Date(now() * 1000).getFullYear();
  const s = `${d.getDate()} ${MONTHS[d.getMonth()]}`;
  return d.getFullYear() === year ? s : `${s} ${d.getFullYear()}`;
}

/** Сколько прошло: «сейчас», «26 мин», «1 ч», «вчера», «12 окт» */
export function ago(t: number | undefined): string {
  if (!t) return '';
  const sec = Math.max(0, now() - t);
  if (sec < 60) return 'сейчас';
  if (sec < 3600) return `${Math.floor(sec / 60)} мин`;
  const today = startOfDay(now());
  if (sec < 6 * 3600 || t >= today) return `${Math.floor(sec / 3600)} ч`;
  if (t >= today - 86400) return 'вчера';
  return dayMonth(t);
}

/** Таймер работы: «7:12», «1:02:03» */
export function timer(since: number | undefined): string {
  if (!since) return '0:00';
  const sec = Math.max(0, Math.floor(now() - since));
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = String(sec % 60).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

/** Когда, коротко: сегодня — «9:41», вчера — «вчера», раньше — «12 окт» */
export function when(t: number | undefined): string {
  if (!t) return '';
  const today = startOfDay(now());
  if (t >= today) return hm(t);
  if (t >= today - 86400) return 'вчера';
  return dayMonth(t);
}

/** Когда, с часами: «сегодня, 9:41», «вчера, 18:40», «12 окт, 18:40» */
export function whenFull(t: number | undefined): string {
  if (!t) return '';
  const today = startOfDay(now());
  if (t >= today) return `сегодня, ${hm(t)}`;
  if (t >= today - 86400) return `вчера, ${hm(t)}`;
  return `${dayMonth(t)}, ${hm(t)}`;
}

/** Время сообщения: «9:41» */
export function clock(t: number | undefined): string {
  return t ? hm(t) : '';
}

/** Сколько назад для сводки: «10 мин назад», «вчера, 18:45» */
export function agoLong(t: number | undefined): string {
  if (!t) return '';
  const sec = Math.max(0, now() - t);
  if (sec < 60) return 'только что';
  if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`;
  if (t >= startOfDay(now())) return `${Math.floor(sec / 3600)} ч назад`;
  return whenFull(t);
}

/** Путь с ~ вместо домашней папки */
export function tildify(path: string, home?: string): string {
  if (home && (path === home || path.startsWith(home + '/'))) return '~' + path.slice(home.length);
  return path.replace(/^\/home\/[^/]+(?=\/|$)/, '~').replace(/^\/Users\/[^/]+(?=\/|$)/, '~');
}

/** «A», «A и B», «A, B и C» */
export function joinAnd(items: string[]): string {
  if (items.length <= 1) return items[0] ?? '';
  return `${items.slice(0, -1).join(', ')} и ${items[items.length - 1]}`;
}

const MONTHS_GEN = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];
const WEEKDAYS = ['Воскресенье', 'Понедельник', 'Вторник', 'Среда', 'Четверг', 'Пятница', 'Суббота'];

/** Начало дня, со сдвигом на days дней: через Date, чтобы переход на летнее время не сбивал */
function dayStart(t: number, days = 0): number {
  const d = new Date(t * 1000);
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() + days);
  return d.getTime() / 1000;
}

/** Ключ дня для группировки: «2026-10-12» */
export function dayKey(t: number): string {
  const d = new Date(t * 1000);
  return `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
}

/** Заголовок дня в истории: «Сегодня», «Вчера», «Понедельник, 12 октября» */
export function dayTitle(t: number): string {
  const today = dayStart(now());
  if (t >= today) return 'Сегодня';
  if (t >= dayStart(now(), -1)) return 'Вчера';
  const d = new Date(t * 1000);
  const year = new Date(now() * 1000).getFullYear();
  const date = `${d.getDate()} ${MONTHS_GEN[d.getMonth()]}${d.getFullYear() === year ? '' : ' ' + d.getFullYear()}`;
  return `${WEEKDAYS[d.getDay()]}, ${date}`;
}

/** 1 сессия, 2 сессии, 5 сессий */
export function sessionsCount(n: number): string {
  return `${n} ${plural(n, 'сессия', 'сессии', 'сессий')}`;
}

const NUMBER_WORDS_F = ['ноль', 'одна', 'две', 'три', 'четыре', 'пять', 'шесть', 'семь', 'восемь', 'девять', 'десять'];

/** Число словом в женском роде до десяти, дальше цифрами: «пять сессий», «12 сессий» */
export function numberWordF(n: number): string {
  return NUMBER_WORDS_F[n] ?? String(n);
}

/** Длительность в минутах: «7 мин», «1 ч 20 мин» */
export function minutesText(min: number): string {
  if (min < 60) return `${min} мин`;
  const h = Math.floor(min / 60);
  const m = min % 60;
  return m ? `${h} ч ${m} мин` : `${h} ч`;
}
