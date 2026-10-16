// Разметка текста сводок: только `код`, **жирный** и ссылки [текст](ключ сессии).
// Всё остальное — обычный текст: строки уходят в DOM текстом, HTML из ответа агента не исполняется.
import type { ComponentChildren } from 'preact';

type Mark = string | { kind: 'code'; text: string } | { kind: 'bold'; children: Mark[] } | { kind: 'link'; key: string; children: Mark[] };

// Ссылка: подпись без «]» и перевода строки, ключ без пробелов и скобок
const LINK = /^\[([^\]\n]+)\]\(([^()\s]+)\)/;

/** Разбор строки. Жирный не вкладывается в жирный, ссылка — в ссылку; незакрытый знак — просто текст */
export function parseMarkup(text: string, inBold = false, inLink = false): Mark[] {
  const out: Mark[] = [];
  let plain = '';
  const flush = () => {
    if (plain) out.push(plain);
    plain = '';
  };
  let i = 0;
  while (i < text.length) {
    const ch = text[i]!;
    if (ch === '`') {
      const end = text.indexOf('`', i + 1);
      if (end > i + 1) {
        flush();
        out.push({ kind: 'code', text: text.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    } else if (ch === '*' && text[i + 1] === '*' && !inBold) {
      const end = text.indexOf('**', i + 2);
      if (end > i + 2) {
        flush();
        out.push({ kind: 'bold', children: parseMarkup(text.slice(i + 2, end), true, inLink) });
        i = end + 2;
        continue;
      }
    } else if (ch === '[' && !inLink) {
      const m = LINK.exec(text.slice(i));
      if (m) {
        flush();
        out.push({ kind: 'link', key: m[2]!, children: parseMarkup(m[1]!, inBold, true) });
        i += m[0].length;
        continue;
      }
    }
    plain += ch;
    i++;
  }
  flush();
  return out;
}

/** Ссылка на сессию: адрес её экрана */
export type LinkHref = (key: string) => string;

function render(marks: Mark[], href: LinkHref): ComponentChildren[] {
  return marks.map((m, i) => {
    if (typeof m === 'string') return m;
    if (m.kind === 'code')
      return (
        <code key={i} class="md-code">
          {m.text}
        </code>
      );
    if (m.kind === 'bold')
      return (
        <strong key={i} class="md-bold">
          {render(m.children, href)}
        </strong>
      );
    return (
      <a key={i} class="md-link" href={href(m.key)}>
        {render(m.children, href)}
      </a>
    );
  });
}

/** Текст сводки с разметкой: код, жирный и ссылки на сессии */
export function Markup({ text, href }: { text: string; href: LinkHref }) {
  return <>{render(parseMarkup(text), href)}</>;
}
