// Сборка интерфейса. `node build.mjs` — в dist (лежит в git и вшивается в бинарник),
// `node build.mjs --dev` — в .dev с пересборкой и сервером на 127.0.0.1:5180.
import * as esbuild from 'esbuild';
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const dev = process.argv.includes('--dev');
const root = fileURLToPath(new URL('.', import.meta.url));
const outdir = root + (dev ? '.dev' : 'dist');
const port = 5180;

// Лицензии библиотек, которые попадают в app.js.
const bundled = ['preact', '@xterm/xterm', '@xterm/addon-fit', '@xterm/addon-webgl'];

async function prepare() {
  await rm(outdir, { recursive: true, force: true });
  await mkdir(outdir, { recursive: true });
  // index.html и шрифты с лицензиями копируются как есть.
  await cp(root + 'public', outdir, { recursive: true });
  const parts = [];
  for (const name of bundled) {
    const pkg = JSON.parse(await readFile(`${root}node_modules/${name}/package.json`, 'utf8'));
    const text = await readFile(`${root}node_modules/${name}/LICENSE`, 'utf8');
    parts.push(`${name} ${pkg.version}\n\n${text.trim()}\n`);
  }
  await writeFile(`${outdir}/licenses.txt`, parts.join('\n' + '-'.repeat(72) + '\n\n'));
}

/** @type {import('esbuild').BuildOptions} */
const options = {
  absWorkingDir: root,
  entryPoints: { app: 'src/main.tsx' },
  outdir,
  bundle: true,
  format: 'esm',
  // xterm — отдельным файлом, грузится, когда открывается терминал
  splitting: true,
  chunkNames: 'chunks/[name]-[hash]',
  target: ['chrome120', 'firefox121', 'safari17'],
  jsx: 'automatic',
  jsxImportSource: 'preact',
  // Шрифты лежат рядом в fonts/ и не проходят через сборку.
  external: ['*.woff2'],
  minify: !dev,
  sourcemap: dev ? 'linked' : false,
  legalComments: 'eof',
  logLevel: 'info',
  define: { 'process.env.NODE_ENV': JSON.stringify(dev ? 'development' : 'production') },
  // В режиме разработки страница перезагружается после пересборки. Баннер попадает в каждый
  // кусок сборки, а соединение нужно одно: иначе кончаются соединения браузера к серверу.
  banner: dev
    ? {
        js: "if (!globalThis.__nitReload) { globalThis.__nitReload = new EventSource('/esbuild'); globalThis.__nitReload.addEventListener('change', () => location.reload()); }",
      }
    : {},
};

await prepare();
if (dev) {
  const ctx = await esbuild.context(options);
  await ctx.watch();
  await ctx.serve({ servedir: outdir, host: '127.0.0.1', port });
  console.log(`http://127.0.0.1:${port}/#gallery`);
} else {
  await esbuild.build(options);
}
