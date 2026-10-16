// Скачивает шрифты из Google Fonts в public/fonts: Manrope и JetBrains Mono (латиница и кириллица)
// и подмножество Material Symbols Outlined с иконками из src/icons.ts. Запускается вручную
// (npm run fonts), когда меняется список иконок; результат лежит в git.
import { mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { ICON_NAMES } from '../src/icons.ts';

const OUT = fileURLToPath(new URL('../public/fonts/', import.meta.url));
// Google отдаёт woff2 только современному браузеру.
const UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36';

const TEXT_CSS =
  'https://fonts.googleapis.com/css2?family=Manrope:wght@400;500;600;700;800' +
  '&family=JetBrains+Mono:wght@400;500;700&display=swap';
const ICONS_CSS =
  'https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20,300..400,0..1,0' +
  `&icon_names=${[...ICON_NAMES].sort().join(',')}&display=block`;

const LICENSES = {
  'OFL-Manrope.txt': 'https://raw.githubusercontent.com/google/fonts/main/ofl/manrope/OFL.txt',
  'OFL-JetBrainsMono.txt': 'https://raw.githubusercontent.com/google/fonts/main/ofl/jetbrainsmono/OFL.txt',
  'LICENSE-MaterialSymbols.txt': 'https://raw.githubusercontent.com/google/material-design-icons/master/LICENSE',
};

async function get(url, as = 'text') {
  const res = await fetch(url, { headers: { 'User-Agent': UA } });
  if (!res.ok) throw new Error(`${res.status} ${url}`);
  return as === 'bytes' ? new Uint8Array(await res.arrayBuffer()) : res.text();
}

// Разбирает ответ css2: подмножество из комментария, семейство, адрес файла и unicode-range.
function faces(css) {
  const out = [];
  const re = /(?:\/\* ([\w-]+) \*\/\s*)?@font-face\s*{([^}]*)}/g;
  for (const m of css.matchAll(re)) {
    const body = m[2];
    out.push({
      subset: m[1] ?? '',
      family: /font-family:\s*'([^']+)'/.exec(body)?.[1],
      url: /url\(([^)]+)\)/.exec(body)?.[1],
      range: /unicode-range:\s*([^;]+);/.exec(body)?.[1] ?? '',
    });
  }
  return out;
}

async function save(name, bytes) {
  await writeFile(OUT + name, bytes);
  console.log(`${name.padEnd(36)} ${(bytes.length / 1024).toFixed(1)} KB`);
}

await mkdir(OUT, { recursive: true });

const files = new Map();
for (const f of faces(await get(TEXT_CSS))) {
  if (f.subset !== 'latin' && f.subset !== 'cyrillic') continue;
  const slug = f.family.toLowerCase().replace(/\s+/g, '-');
  const name = `${slug}-${f.subset}.woff2`;
  if (files.has(name)) {
    // Шрифты переменные: все начертания одного подмножества — один файл.
    if (files.get(name).url !== f.url) throw new Error(`разные файлы для ${name}`);
    continue;
  }
  files.set(name, f);
}
for (const [name, f] of files) {
  await save(name, await get(f.url, 'bytes'));
  console.log(`  unicode-range: ${f.range}`);
}

const [icons] = faces(await get(ICONS_CSS));
await save('material-symbols-outlined.woff2', await get(icons.url, 'bytes'));

for (const [name, url] of Object.entries(LICENSES)) {
  await save(name, new TextEncoder().encode(await get(url)));
}
