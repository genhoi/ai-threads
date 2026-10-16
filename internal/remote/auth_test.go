package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// inner — обработчик после Guard: страница, эхо API, поток событий и WebSocket-эхо.
func inner(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !FromRemote(r) {
			t.Error("request after Guard is not marked remote")
		}
		w.Write([]byte("<title>Нить</title>"))
	})
	mux.HandleFunc("/api/echo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"method": r.Method, "key": r.URL.Query().Get("key")})
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: hello\ndata: {}\n\n"))
		if err := rc.Flush(); err != nil {
			t.Errorf("flush through Guard: %v", err)
			return
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("GET /api/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if conn.Write(context.Background(), typ, data) != nil {
				return
			}
		}
	})
	return mux
}

func TestLoginSetsDeviceCookie(t *testing.T) {
	r := newRig(t, inner(t))
	key := r.keys.Key()
	if len(key) != 32 {
		t.Fatalf("pairing key %q: want 128 bits in hex", key)
	}
	p := r.phone(iphoneUA)
	resp, _ := p.do(http.MethodGet, "/?key="+key, nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("login: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var c *http.Cookie
	for _, x := range resp.Cookies() {
		if x.Name == DeviceCookie {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" ||
		c.MaxAge != 365*24*3600 || len(c.Value) != 43 {
		t.Fatalf("cookie %+v", c)
	}
	if strings.Contains(c.Value, key) || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cookie must be a fresh token, response must not be cached: %v", resp.Header)
	}
	devs := r.keys.Devices()
	if len(devs) != 1 || devs[0].Name != "iPhone · Safari" || devs[0].Created == 0 || devs[0].LastSeen == 0 {
		t.Fatalf("devices %+v", devs)
	}

	// С cookie страница открывается; повторный вход по ссылке нового устройства не заводит.
	if resp, body := p.do(http.MethodGet, "/", nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, "<title>") {
		t.Fatalf("page: %d %s", resp.StatusCode, body)
	}
	p.login(key)
	if n := len(r.keys.Devices()); n != 1 {
		t.Fatalf("second login made %d devices", n)
	}
	// Второй телефон — второе устройство.
	r.phone(pixelUA).login(key)
	if devs := r.keys.Devices(); len(devs) != 2 || devs[0].Name != "Pixel 8 · Chrome" && devs[1].Name != "Pixel 8 · Chrome" {
		t.Fatalf("devices %+v", devs)
	}

	// Ни ключ входа, ни ключ устройства в журнал не попадают; путь — без query (грабля #10).
	logs := r.log.String()
	if strings.Contains(logs, key) || strings.Contains(logs, c.Value) || !strings.Contains(logs, "iPhone · Safari") {
		t.Fatalf("log: %s", logs)
	}
}

func TestWrongKeyAndNoDevice(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	const wrong = "0123456789abcdef0123456789abcdef"
	resp, body := p.do(http.MethodGet, "/?key="+wrong, nil)
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 || !strings.Contains(body, "Доступ с телефона") {
		t.Fatalf("wrong key: %d %v %s", resp.StatusCode, resp.Cookies(), body)
	}
	if strings.Contains(r.log.String(), wrong) || !strings.Contains(r.log.String(), "GET /?…: неверный ключ") {
		t.Fatalf("log: %s", r.log.String())
	}
	resp, body = p.do(http.MethodGet, "/", nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "Настройки → Доступ с телефона") {
		t.Fatalf("no cookie: %d %s", resp.StatusCode, body)
	}
	resp, body = p.do(http.MethodGet, "/api/echo", nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("api without cookie: %d %s", resp.StatusCode, body)
	}
	p.setCookie(DeviceCookie, "forged-token-forged-token-forged-token-1234")
	if resp, _ := p.do(http.MethodGet, "/api/echo", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged cookie: %d", resp.StatusCode)
	}
	if n := len(r.keys.Devices()); n != 0 {
		t.Fatalf("devices: %d", n)
	}
}

// Вход по ?key= — только на корне: на других путях key — ключ терминала (грабля #5).
func TestKeyOnlyOnRoot(t *testing.T) {
	r := newRig(t, inner(t))
	key := r.keys.Key()
	p := r.phone(iphoneUA)
	for _, path := range []string{"/index.html?key=" + key, "/api/echo?key=" + key} {
		if resp, _ := p.do(http.MethodGet, path, nil); resp.StatusCode != http.StatusUnauthorized || p.cookie(DeviceCookie) != "" {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
	p.login(key)
	resp, body := p.do(http.MethodGet, "/api/echo?key=claude:abc", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"key":"claude:abc"`) {
		t.Fatalf("terminal key must reach the handler: %d %s", resp.StatusCode, body)
	}
	// POST на корень с ?key= — не вход.
	if resp, _ := r.phone(iphoneUA).do(http.MethodPost, "/?key="+key, map[string]string{"Origin": "https://" + testHost}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST login: %d", resp.StatusCode)
	}
}

func TestHostAndOrigin(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	p.login(r.keys.Key())
	origin := "https://" + testHost
	cases := []struct {
		name   string
		method string
		header map[string]string
		want   int
	}{
		{"get without origin", http.MethodGet, nil, http.StatusOK},
		{"host with port", http.MethodGet, map[string]string{"Host": testHost + ":443"}, http.StatusOK},
		{"foreign host", http.MethodGet, map[string]string{"Host": "evil.example"}, http.StatusForbidden},
		{"short host", http.MethodGet, map[string]string{"Host": "nit"}, http.StatusForbidden},
		{"local host", http.MethodGet, map[string]string{"Host": "127.0.0.1:8765"}, http.StatusForbidden},
		{"post without origin", http.MethodPost, nil, http.StatusForbidden},
		{"post from foreign site", http.MethodPost, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"post from http", http.MethodPost, map[string]string{"Origin": "http://" + testHost}, http.StatusForbidden},
		{"post from itself", http.MethodPost, map[string]string{"Origin": origin}, http.StatusOK},
		{"delete from itself", http.MethodDelete, map[string]string{"Origin": origin}, http.StatusOK},
		{"upgrade without origin", http.MethodGet, map[string]string{"Connection": "Upgrade", "Upgrade": "websocket"}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if resp, body := p.do(c.method, "/api/echo", c.header); resp.StatusCode != c.want {
				t.Fatalf("got %d, want %d: %s", resp.StatusCode, c.want, body)
			}
		})
	}
}

// dialWS открывает настоящий WebSocket через Guard; origin пустой — без заголовка.
func (r *rig) dialWS(p *phone, origin string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header := http.Header{"User-Agent": {p.ua}}
	if origin != "" {
		header.Set("Origin", origin)
	}
	return websocket.Dial(ctx, "wss://"+testHost+"/api/ws", &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: r.transport(), Jar: p.jar}, HTTPHeader: header})
}

// WebSocket проходит через Guard по-настоящему: апгрейд и обмен сообщениями (грабля #6),
// а без своего Origin — отказ.
func TestWebSocketThroughGuard(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	p.login(r.keys.Key())
	for _, origin := range []string{"", "https://evil.example"} {
		if _, resp, err := r.dialWS(p, origin); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: %v %v", origin, resp, err)
		}
	}
	conn, _, err := r.dialWS(p, "https://"+testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := conn.Read(ctx); err != nil || string(data) != "ping" {
		t.Fatalf("echo %q %v", data, err)
	}
	// Телефон без cookie апгрейд не получает.
	if _, resp, err := r.dialWS(r.phone(iphoneUA), "https://"+testHost); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without device: %v %v", resp, err)
	}
}

// Поток событий не буферизуется Guard: первое событие приходит, пока обработчик ещё работает.
func TestSSEFlushThroughGuard(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	p.login(r.keys.Key())
	resp, lines := r.stream(p)
	defer resp.Body.Close()
	select {
	case line := <-lines:
		if line != "event: hello" {
			t.Fatalf("first line %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event was not flushed")
	}
}

// stream открывает /api/events и отдаёт строки потока.
func (r *rig) stream(p *phone) (*http.Response, chan string) {
	r.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "https://"+testHost+"/api/events", nil)
	req.Header.Set("User-Agent", p.ua)
	client := &http.Client{Transport: r.transport(), Jar: p.jar}
	resp, err := client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("events: %d", resp.StatusCode)
	}
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return resp, lines
}

// Отключённый телефон теряет и открытый WebSocket, и поток событий; другой остаётся.
func TestRevokeCutsConnections(t *testing.T) {
	r := newRig(t, inner(t))
	key := r.keys.Key()
	a, b := r.phone(iphoneUA), r.phone(pixelUA)
	a.login(key)
	b.login(key)
	connA, _, err := r.dialWS(a, "https://"+testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer connA.CloseNow()
	connB, _, err := r.dialWS(b, "https://"+testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer connB.CloseNow()
	respA, linesA := r.stream(a)
	defer respA.Body.Close()
	<-linesA

	var idA string
	for _, d := range r.keys.Devices() {
		if d.Name == "iPhone · Safari" {
			idA = d.ID
		}
	}
	if ok, err := r.m.Revoke(idA); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := connA.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("revoked websocket still open: %v", err)
	}
	if !closedWithin(linesA, 5*time.Second) {
		t.Fatal("revoked event stream still open")
	}
	if resp, _ := a.do(http.MethodGet, "/api/echo", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked device: %d", resp.StatusCode)
	}
	if err := connB.Write(ctx, websocket.MessageText, []byte("b")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := connB.Read(ctx); err != nil || string(data) != "b" {
		t.Fatalf("other phone: %q %v", data, err)
	}
	if ok, _ := r.m.Revoke(idA); ok {
		t.Fatal("revoked twice")
	}
}

func closedWithin(lines chan string, d time.Duration) bool {
	timeout := time.After(d)
	for {
		select {
		case _, open := <-lines:
			if !open {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// «Новый ключ»: старая ссылка не работает, все телефоны отключены, новая ссылка работает.
func TestNewKeyRevokesAll(t *testing.T) {
	r := newRig(t, inner(t))
	old := r.keys.Key()
	a := r.phone(iphoneUA)
	a.login(old)
	conn, _, err := r.dialWS(a, "https://"+testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := r.m.NewKey(); err != nil {
		t.Fatal(err)
	}
	key := r.keys.Key()
	if key == old || len(key) != 32 || len(r.keys.Devices()) != 0 {
		t.Fatalf("key %q devices %v", key, r.keys.Devices())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("websocket survived the new key: %v", err)
	}
	if resp, _ := a.do(http.MethodGet, "/", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old device: %d", resp.StatusCode)
	}
	if resp, _ := r.phone(iphoneUA).do(http.MethodGet, "/?key="+old, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old link: %d", resp.StatusCode)
	}
	r.phone(iphoneUA).login(key)
}

// Телефон, вошедший через gate (cookie gate_key с ключом), после перехода получает устройство.
func TestGateCookieBecomesDevice(t *testing.T) {
	r := newRig(t, inner(t))
	key := r.keys.Key()
	p := r.phone(pixelUA)
	p.setCookie(GateCookie, key)
	// Запрос скрипта пропускается, но устройства не заводит: их могло бы стать несколько разом.
	if resp, _ := p.do(http.MethodGet, "/api/echo", map[string]string{"Sec-Fetch-Mode": "cors"}); resp.StatusCode != http.StatusOK ||
		len(r.keys.Devices()) != 0 {
		t.Fatalf("api via gate cookie: %d, devices %d", resp.StatusCode, len(r.keys.Devices()))
	}
	resp, body := p.do(http.MethodGet, "/", map[string]string{"Sec-Fetch-Mode": "navigate"})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "<title>") {
		t.Fatalf("page: %d", resp.StatusCode)
	}
	if p.cookie(DeviceCookie) == "" || p.cookie(GateCookie) != "" {
		t.Fatalf("cookies after conversion: device %q gate %q", p.cookie(DeviceCookie), p.cookie(GateCookie))
	}
	if devs := r.keys.Devices(); len(devs) != 1 || devs[0].Name != "Pixel 8 · Chrome" {
		t.Fatalf("devices %+v", devs)
	}
	if resp, _ := p.do(http.MethodGet, "/api/echo", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("device after conversion: %d", resp.StatusCode)
	}
	// Чужое значение gate_key — не вход; после нового ключа старое — тоже.
	q := r.phone(iphoneUA)
	q.setCookie(GateCookie, "ffffffffffffffffffffffffffffffff")
	if resp, _ := q.do(http.MethodGet, "/", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong gate cookie: %d", resp.StatusCode)
	}
	q.setCookie(GateCookie, key)
	r.m.NewKey()
	if resp, _ := q.do(http.MethodGet, "/", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale gate cookie: %d", resp.StatusCode)
	}
	if strings.Contains(r.log.String(), key) {
		t.Fatal("key in log")
	}
}

// Отказы с Funnel шлёт весь интернет: в журнал — не больше десятка в минуту.
func TestDenialLogLimit(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	for range 30 {
		p.do(http.MethodGet, "/wp-login.php", nil)
	}
	if n := strings.Count(r.log.String(), "отказ"); n != 10 {
		t.Fatalf("denials logged: %d\n%s", n, r.log.String())
	}
}

// Пока узел не получил адрес, Guard не пускает никого.
func TestGuardWithoutHost(t *testing.T) {
	keys, _ := Open(t.TempDir() + "/remote.json")
	g := NewGuard(keys, func() string { return "" }, inner(t), func(string, ...any) {})
	req, _ := http.NewRequest(http.MethodGet, "https://"+testHost+"/", nil)
	req.Host = testHost
	w := &recorder{header: http.Header{}}
	g.ServeHTTP(w, req)
	if w.code != http.StatusForbidden {
		t.Fatalf("got %d", w.code)
	}
}

type recorder struct {
	header http.Header
	code   int
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return io.Discard.Write(b) }
func (r *recorder) WriteHeader(code int)        { r.code = code }

// /gate/healthz отвечает без входа, как у gate; чужой Host — всё равно отказ.
func TestHealthz(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	if resp, body := p.do(http.MethodGet, "/gate/healthz", nil); resp.StatusCode != http.StatusOK || body != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, body)
	}
	if resp, _ := p.do(http.MethodGet, "/gate/healthz", map[string]string{"Host": "evil.example"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("healthz with foreign host: %d", resp.StatusCode)
	}
}

// Выключили доступ — открытый терминал телефона обрывается сразу, а не когда умрёт узел.
func TestDisableCutsWebSocket(t *testing.T) {
	r := newRig(t, inner(t))
	p := r.phone(iphoneUA)
	p.login(r.keys.Key())
	conn, _, err := r.dialWS(p, "https://"+testHost)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	r.m.Apply(Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("websocket survived the stop: %v", err)
	}
}
