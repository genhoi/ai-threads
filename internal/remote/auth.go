package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// DeviceCookie — ключ устройства. Значение — 256 случайных бит, на диске только sha256.
	DeviceCookie = "nit_device"
	// GateCookie — cookie шлюза gate: в ней лежал сам ключ входа.
	GateCookie = "gate_key"
	cookieAge  = 365 * 24 * 3600
)

type ctxKey int

const (
	viaRemote ctxKey = iota // запрос прошёл проверку узла; значение — ID устройства
	connKey                 // соединение запроса, чтобы отключённое устройство теряло и открытые терминалы
)

// FromRemote — запрос пришёл через узел Tailscale, а не с компьютера.
func FromRemote(r *http.Request) bool {
	_, ok := r.Context().Value(viaRemote).(string)
	return ok
}

// Guard — проверка запросов с телефона: Host узла, вход по ключу, устройство по cookie и Origin
// у запросов, которые что-то меняют. ResponseWriter не оборачивается: SSE нужен Flusher,
// а WebSocket — Hijacker (грабля #6 gate).
type Guard struct {
	keys *Keys
	host func() string // nit.<tailnet>.ts.net; пусто — узел не поднят
	next http.Handler
	logf func(format string, args ...any)

	mu      sync.Mutex
	conns   map[net.Conn]string // соединение → ID устройства; "" — вошёл по cookie gate
	denied  int                 // отказы в текущей минуте, для журнала
	deniedT time.Time
	quiet   int // сколько отказов не попало в журнал
}

// NewGuard — проверка перед next. host отдаёт адрес узла, logf — журнал входов и отказов.
func NewGuard(keys *Keys, host func() string, next http.Handler, logf func(string, ...any)) *Guard {
	if logf == nil {
		logf = log.Printf
	}
	return &Guard{keys: keys, host: host, next: next, logf: logf, conns: map[net.Conn]string{}}
}

func (g *Guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := g.host()
	if host == "" || !sameHost(r.Host, host) {
		g.deny(r, "чужой Host")
		writeError(w, http.StatusForbidden, "host is not allowed")
		return
	}
	if r.URL.Path == "/gate/healthz" {
		// Проверка живости без входа, как у gate: адрес и так виден по сертификату.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
		return
	}
	// Вход только на корне: на других путях ?key= — ключ терминала «Нити» (грабля #5 gate).
	if r.URL.Path == "/" && r.Method == http.MethodGet && r.URL.Query().Has("key") {
		g.login(w, r)
		return
	}
	dev, ok := g.device(r)
	viaGate := false
	if !ok {
		c, err := r.Cookie(GateCookie)
		if err != nil || !g.keys.KeyOK(c.Value) {
			g.deny(r, "телефон не подключён")
			g.unauthorized(w, r)
			return
		}
		viaGate = true
	}
	// Чужая страница не должна ни менять что-то от имени телефона, ни открыть терминал.
	// Браузер присылает Origin у таких запросов всегда, поэтому без Origin — тоже отказ.
	if changes(r) && r.Header.Get("Origin") != "https://"+host {
		g.deny(r, "чужой Origin")
		writeError(w, http.StatusForbidden, "origin is not allowed")
		return
	}
	switch {
	case viaGate && navigation(r):
		// Телефон, вошедший через gate, получает своё устройство при первом открытии страницы.
		if token, d, err := g.keys.AddDevice(DeviceName(r.UserAgent())); err == nil {
			setDeviceCookie(w, token)
			clearGateCookie(w)
			dev, viaGate = d, false
			g.logf("remote: телефон из gate подключён как %s (%s)", d.Name, d.ID)
		}
	case !viaGate && g.keys.Touch(dev.ID) && !upgrade(r):
		// Cookie продлевается, пока телефоном пользуются.
		if c, err := r.Cookie(DeviceCookie); err == nil {
			setDeviceCookie(w, c.Value)
		}
	}
	if c, ok := r.Context().Value(connKey).(net.Conn); ok {
		g.track(c, dev.ID)
		defer g.untrack(c)
	}
	g.next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viaRemote, dev.ID)))
}

// login — ссылка с ключом выдаёт устройству cookie и уводит на чистый адрес, чтобы ключ
// не остался в истории браузера.
func (g *Guard) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, known := g.device(r)
	if !g.keys.KeyOK(r.URL.Query().Get("key")) {
		if known {
			// Старая ссылка на уже подключённом телефоне: ключ не нужен.
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		g.deny(r, "неверный ключ")
		page(w, http.StatusUnauthorized, "Ключ в ссылке не подходит",
			"Возможно, на компьютере уже сделали новый ключ. Откройте свежую ссылку: на компьютере "+
				"Настройки → Доступ с телефона.")
		return
	}
	if !known {
		token, d, err := g.keys.AddDevice(DeviceName(r.UserAgent()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "can't save the device")
			return
		}
		setDeviceCookie(w, token)
		g.logf("remote: подключён телефон %s (%s)", d.Name, d.ID)
	}
	if _, err := r.Cookie(GateCookie); err == nil {
		clearGateCookie(w)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (g *Guard) device(r *http.Request) (Device, bool) {
	c, err := r.Cookie(DeviceCookie)
	if err != nil {
		return Device{}, false
	}
	return g.keys.Device(c.Value)
}

func (g *Guard) unauthorized(w http.ResponseWriter, r *http.Request) {
	const text = "Этот телефон не подключён к «Нити»."
	const hint = "Откройте ссылку с ключом: на компьютере Настройки → Доступ с телефона."
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusUnauthorized, text+" "+hint)
		return
	}
	page(w, http.StatusUnauthorized, text, hint)
}

// deny пишет отказ в журнал: метод и путь без query (грабля #10 gate), не больше 10 строк
// в минуту — с Funnel адрес виден всему интернету.
func (g *Guard) deny(r *http.Request, why string) {
	g.mu.Lock()
	now := time.Now()
	if now.Sub(g.deniedT) > time.Minute {
		g.denied, g.deniedT = 0, now
	}
	g.denied++
	if g.denied > 10 {
		g.quiet++
		g.mu.Unlock()
		return
	}
	quiet := g.quiet
	g.quiet = 0
	g.mu.Unlock()
	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?…"
	}
	if quiet > 0 {
		g.logf("remote: отказ: %s %s: %s (и ещё %d без записи)", r.Method, path, why, quiet)
		return
	}
	g.logf("remote: отказ: %s %s: %s", r.Method, path, why)
}

func (g *Guard) track(c net.Conn, id string) {
	g.mu.Lock()
	g.conns[c] = id
	g.mu.Unlock()
}

func (g *Guard) untrack(c net.Conn) {
	g.mu.Lock()
	delete(g.conns, c)
	g.mu.Unlock()
}

// cut закрывает соединения отключённых устройств: открытый терминал и поток событий
// обрываются сразу. all — все соединения, и вошедшие по cookie gate тоже.
func (g *Guard) cut(ids []string, all bool) {
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	g.mu.Lock()
	var list []net.Conn
	for c, id := range g.conns {
		if all || drop[id] {
			list = append(list, c)
		}
	}
	g.mu.Unlock()
	for _, c := range list {
		// Close у TLS шлёт close_notify и может ждать до 5 с: не задерживать ответ.
		go c.Close()
	}
}

// NewServer — HTTP-сервер для слушателя узла. Он запоминает соединение каждого запроса,
// чтобы Guard мог оборвать соединения отключённого устройства.
func NewServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// Соединения в userspace-стеке tsnet умирают тихо (грабля #3 gate): простаивающие закрываются.
		IdleTimeout: 2 * time.Minute,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey, c)
		},
		ErrorLog: log.New(handshakeFilter{}, "remote: ", 0),
	}
}

// handshakeFilter не пускает в журнал неудачные TLS-рукопожатия: с Funnel их шлют сканеры.
type handshakeFilter struct{}

func (handshakeFilter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, []byte("TLS handshake error")) {
		log.Print("remote: " + strings.TrimSpace(string(p)))
	}
	return len(p), nil
}

func sameHost(got, host string) bool {
	return strings.EqualFold(got, host) || strings.EqualFold(got, host+":443")
}

// changes — запрос что-то меняет или открывает WebSocket.
func changes(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return upgrade(r)
	}
	return true
}

func upgrade(r *http.Request) bool {
	if r.Header.Get("Upgrade") != "" {
		return true
	}
	for _, v := range r.Header.Values("Connection") {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), "upgrade") {
				return true
			}
		}
	}
	return false
}

// navigation — браузер открывает страницу, а не скрипт запрашивает данные.
func navigation(r *http.Request) bool {
	if r.Method != http.MethodGet || upgrade(r) {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return !strings.HasPrefix(r.URL.Path, "/api/") && strings.Contains(r.Header.Get("Accept"), "text/html")
}

func setDeviceCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: DeviceCookie, Value: token, Path: "/", MaxAge: cookieAge,
		HttpOnly: true, Secure: true,
		// Lax, не Strict: переход по QR-коду кросс-сайтовый, и Strict-cookie не пришла бы
		// на редиректе после входа (грабля #9 gate). Чужие POST и WebSocket закрыты проверкой Origin.
		SameSite: http.SameSiteLaxMode,
	})
}

func clearGateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: GateCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"error": message})
}

// page — маленькая страница с объяснением для телефона.
func page(w http.ResponseWriter, status int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="ru"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>Нить</title>` +
		`<style>body{font:16px/1.5 system-ui,sans-serif;margin:0;padding:32px 16px;color:#1d1d1f;background:#fafafa}` +
		`main{max-width:420px;margin:0 auto}h1{font-size:20px;margin:0 0 12px}` +
		`@media (prefers-color-scheme:dark){body{color:#eee;background:#161616}}</style></head>` +
		`<body><main><h1>` + title + `</h1><p>` + text + `</p></main></body></html>`))
}
