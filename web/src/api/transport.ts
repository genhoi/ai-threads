// Через что идут запросы, поток событий и терминал. Обычно — браузер; в режиме ?mock их
// подменяет src/mock, и интерфейс работает без сервера.

/** Поток событий: то, чем пользуется events.ts у EventSource */
export interface EventStream {
  readonly readyState: number;
  onopen: ((e: Event) => void) | null;
  onerror: ((e: Event) => void) | null;
  addEventListener(type: string, listener: (e: MessageEvent<string>) => void): void;
  close(): void;
}

/** Соединение терминала: то, чем пользуется terminal.ts у WebSocket */
export interface TerminalSocket {
  binaryType: BinaryType;
  readonly readyState: number;
  onopen: ((e: Event) => void) | null;
  onmessage: ((e: MessageEvent<ArrayBuffer | string>) => void) | null;
  onclose: ((e: CloseEvent) => void) | null;
  onerror: ((e: Event) => void) | null;
  send(data: string | ArrayBufferLike | ArrayBufferView): void;
  close(code?: number, reason?: string): void;
}

export interface Transport {
  fetch(input: string, init?: RequestInit): Promise<Response>;
  events(url: string): EventStream;
  terminal(url: string): TerminalSocket;
}

export const transport: Transport = {
  fetch: (input, init) => window.fetch(input, init),
  events: (url) => new EventSource(url),
  terminal: (url) => new WebSocket(url) as TerminalSocket,
};

/** Подменить транспорт целиком или частично */
export function setTransport(t: Partial<Transport>) {
  Object.assign(transport, t);
}
