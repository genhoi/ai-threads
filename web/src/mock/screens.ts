// Экраны терминала для режима ?mock: текст из макета Nit-Session.dc.html с цветами ANSI.

const rgb = (hex: string) => {
  const n = parseInt(hex.slice(1), 16);
  return `\x1b[38;2;${(n >> 16) & 255};${(n >> 8) & 255};${n & 255}m`;
};

export const DIM = rgb('#978E81');
export const GREEN = rgb('#6CCB8F');
export const RED = rgb('#F07A72');
export const BLUE = rgb('#8DB6FF');
const FRAME = rgb('#463E33');
export const BOLD = '\x1b[1m';
export const RESET = '\x1b[0m';

/** Длина строки на экране: без управляющих последовательностей */
function width(s: string): number {
  return [...s.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '')].length;
}

/** Рамка запроса разрешения, как рисует Claude Code */
function box(lines: string[]): string[] {
  const w = Math.max(...lines.map(width));
  const pad = (s: string) => s + ' '.repeat(w - width(s));
  return [
    `${FRAME}╭${'─'.repeat(w + 4)}╮${RESET}`,
    ...lines.map((l) => `${FRAME}│${RESET}  ${pad(l)}  ${FRAME}│${RESET}`),
    `${FRAME}╰${'─'.repeat(w + 4)}╯${RESET}`,
  ];
}

const tree = (s: string) => `${DIM}  └ ${RESET}${s}`;

export interface Scene {
  lines: string[];
  /** Курсор после последней строки (приглашение ввода) */
  cursor?: boolean;
  /** Последняя строка обновляется: индикатор работы */
  spinner?: (frame: number, elapsed: number) => string;
}

const SPIN = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];

function elapsedText(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return m ? `${m}m ${s}s` : `${s}s`;
}

const permission: Scene = {
  lines: [
    `${DIM}> ${RESET}BACKEND-10300: добавь лимиты запросов для ключей API. Лимит храни в api_keys, по умолчанию 600 в минуту.`,
    '',
    '● Посмотрю, как сейчас устроены ключи.',
    tree(`${BOLD}Read${RESET} internal/auth/apikey.go ${DIM}(148 строк)${RESET}`),
    tree(`${BOLD}Read${RESET} migrations/0057_api_keys.sql ${DIM}(31 строка)${RESET}`),
    '',
    '● Лимит проверяю в middleware, счётчик держу в Redis с окном в минуту.',
    tree(`${BOLD}Write${RESET} migrations/0058_api_keys_rate_limit.sql ${GREEN}+9${RESET}`),
    tree(`${BOLD}Write${RESET} internal/http/ratelimit.go ${GREEN}+84${RESET}`),
    tree(`${BOLD}Write${RESET} internal/http/ratelimit_test.go ${GREEN}+52${RESET}`),
    tree(`${BOLD}Edit${RESET}  internal/http/router.go ${GREEN}+3${RESET} ${RED}−1${RESET}`),
    '',
    '● Прогоню миграцию на локальной базе, потом тесты пакета http.',
    '',
    ...box([
      `${BOLD}Команда Bash${RESET}`,
      '',
      '  make migrate DB=local',
      `${DIM}  Применить новые миграции к локальной Postgres${RESET}`,
      '',
      'Выполнить?',
      `${BLUE}❯ 1. Да${RESET}`,
      '  2. Да, и не спрашивать про make в этом проекте',
      `  3. Нет, сказать, что сделать иначе ${DIM}(esc)${RESET}`,
    ]),
  ],
};

const working: Scene = {
  lines: [
    `${DIM}> ${RESET}Сделай ревью ветки BACKEND-10296 перед мержем: логика, тесты, миграции.`,
    '',
    `• Смотрю изменения ветки: 9 файлов, ${GREEN}+412${RESET} ${RED}−87${RESET}.`,
    tree('git diff main...BACKEND-10296 --stat'),
    '',
    '• internal/billing/invoice.go: округление идёт до суммирования строк.',
    '  На счёте из 300 позиций итог расходится на 1 копейку. Предложу исправление в конце.',
    '',
    '• Миграция 0055 без индекса на invoices.customer_id, выборка по клиенту станет полной.',
    '',
    '• Прогоняю тесты пакета billing.',
    tree('go test ./internal/billing/...'),
    `${DIM}     ok   internal/billing/tax       0.84s${RESET}`,
    `${DIM}     ok   internal/billing/invoice   2.31s${RESET}`,
    `${DIM}     ...  internal/billing/export${RESET}`,
    '',
  ],
  spinner: (f, sec) => `${BLUE}${SPIN[f % SPIN.length]}${RESET} Работаю ${DIM}· ${elapsedText(sec)} · esc — прервать${RESET}`,
};

const astro: Scene = {
  lines: [
    `${DIM}> ${RESET}Перенеси лендинг с Hugo на Astro. Адреса страниц должны остаться прежними.`,
    '',
    '• Перенёс общий шаблон и 12 страниц, адреса совпадают.',
    tree('astro build'),
    `${DIM}     12 из 14 страниц${RESET}`,
    '',
  ],
  spinner: (f, sec) => `${BLUE}${SPIN[f % SPIN.length]}${RESET} astro build ${DIM}· ${elapsedText(sec)}${RESET}`,
};

const promo: Scene = {
  lines: [
    `${DIM}> ${RESET}Сделай ревью ветки с промокодами в корзине.`,
    '',
    `• Смотрю изменения: 6 файлов, ${GREEN}+188${RESET} ${RED}−41${RESET}.`,
    '• cart/promo.go: скидка применяется до проверки минимальной суммы заказа.',
    '• Тест на просроченный промокод проверяет только дату, без часового пояса.',
    '',
    'Нашёл две проблемы. Поправить обе?',
    '',
  ],
  cursor: true,
};

const android: Scene = {
  lines: [
    `${DIM}> ${RESET}Почини падение на Android 14 при открытии камеры.`,
    '',
    '● Смотрю CameraActivity и разрешения в манифесте.',
    tree(`${BOLD}Read${RESET} app/src/main/AndroidManifest.xml ${DIM}(64 строки)${RESET}`),
    tree(`${BOLD}Bash${RESET} ./gradlew connectedAndroidTest`),
    `${DIM}     FAILURE: Build failed with an exception.${RESET}`,
    '',
    `${RED}cursor-agent: процесс завершился с кодом 1${RESET}`,
    '',
  ],
  cursor: true,
};

export function sceneFor(key: string, prompt?: string): Scene {
  if (key.endsWith('-10300')) return permission;
  if (key.endsWith('-10296')) return working;
  if (key.endsWith('-astro')) return astro;
  if (key.endsWith('-promo')) return promo;
  if (key.endsWith('-android')) return android;
  return {
    lines: prompt ? [`${DIM}> ${RESET}${prompt}`, '', '● Начинаю: смотрю устройство проекта.', ''] : [''],
    cursor: true,
  };
}
