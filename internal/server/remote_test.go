package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/genhoi/ai-threads/internal/remote"
	"github.com/genhoi/ai-threads/internal/remote/remotetest"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

func remoteKeys(t *testing.T, dir string) *remote.Keys {
	t.Helper()
	keys, err := remote.Open(filepath.Join(dir, "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// noNode — настоящего tsnet в тестах нет: узел сразу отвечает ошибкой.
func noNode(t *testing.T) remote.NodeFactory {
	return func(remote.Config) remote.Node { return failNode{} }
}

type failNode struct{}

var errNoTailscale = errors.New("tsnet is not available in tests")

func (failNode) Up(context.Context) (string, error)            { return "", errNoTailscale }
func (failNode) Login(context.Context) (string, string, error) { return "", "", errNoTailscale }
func (failNode) Listen(bool) (net.Listener, error)             { return nil, errNoTailscale }
func (failNode) Cert(context.Context, string) error            { return errNoTailscale }
func (failNode) Close() error                                  { return nil }

func ptr[T any](v T) *T { return &v }

// phoneClient — браузер телефона: ходит на https://<узел> через слушатель подделки.
type phoneClient struct {
	t      *testing.T
	addr   string
	jar    http.CookieJar
	client *http.Client
}

func newPhone(t *testing.T, node *remotetest.Node) *phoneClient {
	jar, _ := cookiejar.New(nil)
	return &phoneClient{t: t, addr: node.Addr(), jar: jar, client: &http.Client{
		Transport: remotetest.Transport(node.Addr()), Jar: jar, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

const phoneOrigin = "https://" + remotetest.Host

// do — запрос с телефона; у изменяющих запросов Origin узла.
func (p *phoneClient) do(method, path string, body any) (*http.Response, []byte) {
	p.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, phoneOrigin+path, reader)
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1")
	if method != http.MethodGet {
		req.Header.Set("Origin", phoneOrigin)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func (p *phoneClient) status(method, path string, body any) int {
	p.t.Helper()
	resp, _ := p.do(method, path, body)
	return resp.StatusCode
}

// enableRemote включает доступ с телефона и ждёт running.
func (e *env) enableRemote() RemoteView {
	e.t.Helper()
	var v RemoteView
	e.json(http.MethodPost, "/api/remote", RemoteRequest{Enabled: ptr(true)}, http.StatusOK, &v)
	if !v.Enabled {
		e.t.Fatalf("enable: %+v", v)
	}
	wait(e.t, "remote is running", func() bool {
		e.json(http.MethodGet, "/api/remote", nil, http.StatusOK, &v)
		return v.State == "running"
	})
	return v
}

func TestRemoteAPI(t *testing.T) {
	nodes := &remotetest.Nodes{}
	e := setup(t, func(o *Options) { o.RemoteNode = nodes.Factory })
	ev := e.events()

	var v RemoteView
	e.json(http.MethodGet, "/api/remote", nil, http.StatusOK, &v)
	if v.State != "off" || v.Enabled || v.Link != "" || v.Devices == nil {
		t.Fatalf("initial: %+v", v)
	}
	if code, _ := e.do(http.MethodGet, "/api/remote/qr.svg", nil); code != http.StatusConflict {
		t.Fatalf("qr without link: %d", code)
	}

	v = e.enableRemote()
	key := e.s.remote.Keys().Key()
	if v.Host != remotetest.Host || v.URL != phoneOrigin+"/" || v.Link != phoneOrigin+"/?key="+key || len(key) != 32 {
		t.Fatalf("running: %+v", v)
	}
	if r := e.conf.Get().Remote; !r.Enabled || r.Funnel || nodes.Last().Cfg.Dir != r.RemoteDir() || nodes.Last().Cfg.Hostname != "nit" {
		t.Fatalf("settings %+v, node %+v", r, nodes.Last().Cfg)
	}
	ev.wait("remote", func(data string) bool { return strings.Contains(data, `"state":"running"`) })

	req, _ := http.NewRequest(http.MethodGet, e.url+"/api/remote/qr.svg", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	svg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(string(svg), "<svg") {
		t.Fatalf("qr: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// Телефон входит по ссылке и видит ту же «Нить», но не управление доступом.
	p := newPhone(t, nodes.Last())
	if code := p.status(http.MethodGet, "/api/bootstrap", nil); code != http.StatusUnauthorized {
		t.Fatalf("before login: %d", code)
	}
	resp, _ = p.do(http.MethodGet, "/?key="+key, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	ev.wait("remote", func(data string) bool { return strings.Contains(data, `"name":"iPhone · Safari"`) })
	resp, raw := p.do(http.MethodGet, "/api/bootstrap", nil)
	var b Bootstrap
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &b) != nil || b.Env.Via != "remote" ||
		b.Env.Remote.State != "running" || b.Env.Remote.Host != remotetest.Host || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("phone bootstrap: %d %+v %v", resp.StatusCode, b.Env, resp.Header)
	}
	var local Bootstrap
	e.json(http.MethodGet, "/api/bootstrap", nil, http.StatusOK, &local)
	if local.Env.Via != "local" || local.Env.Remote.State != "running" {
		t.Fatalf("local env: %+v", local.Env)
	}
	if resp, body := p.do(http.MethodGet, "/", nil); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>") {
		t.Fatalf("phone page: %d", resp.StatusCode)
	}

	// Управление доступом — только с компьютера.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/remote"}, {http.MethodPost, "/api/remote"}, {http.MethodPost, "/api/remote/key"},
		{http.MethodPost, "/api/remote/devices/x/revoke"}, {http.MethodGet, "/api/remote/qr.svg"},
	} {
		var body any
		if c.method == http.MethodPost {
			body = map[string]any{"enabled": false}
		}
		if code := p.status(c.method, c.path, body); code != http.StatusForbidden {
			t.Fatalf("phone %s %s: %d", c.method, c.path, code)
		}
	}
	// Изменяющий запрос с телефона без своего Origin — отказ, со своим — доходит до обработчика.
	req, _ = http.NewRequest(http.MethodPost, phoneOrigin+"/api/project", strings.NewReader(`{}`))
	if resp, err := p.client.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("post without origin: %v %v", resp, err)
	}
	if code := p.status(http.MethodPost, "/api/project", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("post from phone: %d", code)
	}

	// Настройки с телефона и без поля remote, как и выгрузка, доступ не выключают.
	set := e.conf.Get()
	set.Remote.Enabled = false
	set.Language = "en"
	if code := p.status(http.MethodPost, "/api/settings", set); code != http.StatusOK {
		t.Fatalf("phone settings: %d", code)
	}
	if got := e.conf.Get(); !got.Remote.Enabled || got.Language != "en" {
		t.Fatalf("phone settings: %+v", got.Remote)
	}
	var top map[string]any
	raw, _ = json.Marshal(e.conf.Get())
	json.Unmarshal(raw, &top)
	delete(top, "remote")
	e.json(http.MethodPost, "/api/settings", top, http.StatusOK, nil)
	top["remote"] = map[string]any{"enabled": false}
	e.json(http.MethodPost, "/api/import", map[string]any{"version": 2, "settings": top}, http.StatusOK, nil)
	if !e.conf.Get().Remote.Enabled || e.s.remote.Status().State != "running" {
		t.Fatal("settings without remote and import must keep phone access")
	}
	var fe ErrorView
	e.json(http.MethodPost, "/api/settings", map[string]any{"remote": map[string]any{"hostname": "Bad_Name"}}, http.StatusBadRequest, &fe)
	if fe.Fields["remote.hostname"] == "" {
		t.Fatalf("validation: %+v", fe)
	}

	// В выгрузке нет ни ключа входа, ни хешей устройств.
	_, export := e.do(http.MethodGet, "/api/export", nil)
	hash := ""
	if raw, err := os.ReadFile(e.s.remote.Keys().Path()); err == nil {
		var f struct {
			Devices []struct {
				Hash string `json:"token_sha256"`
			} `json:"devices"`
		}
		json.Unmarshal(raw, &f)
		if len(f.Devices) == 1 {
			hash = f.Devices[0].Hash
		}
	}
	if hash == "" || strings.Contains(string(export), key) || strings.Contains(string(export), hash) {
		t.Fatalf("export leaks secrets or no device hash: %q", hash)
	}
	if info, err := os.Stat(e.s.remote.Keys().Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("remote.json mode: %v %v", info, err)
	}

	// Funnel перезапускает узел с другим слушателем.
	e.json(http.MethodPost, "/api/remote", RemoteRequest{Funnel: ptr(true)}, http.StatusOK, &v)
	wait(t, "funnel listener", func() bool {
		e.json(http.MethodGet, "/api/remote", nil, http.StatusOK, &v)
		return v.State == "running" && v.Funnel && nodes.Last().Funnel()
	})

	// Новый ключ: прежняя ссылка и прежний телефон больше не входят.
	var fresh RemoteView
	e.json(http.MethodPost, "/api/remote/key", nil, http.StatusOK, &fresh)
	if fresh.Link == phoneOrigin+"/?key="+key || fresh.Link != phoneOrigin+"/?key="+e.s.remote.Keys().Key() || len(fresh.Devices) != 0 {
		t.Fatalf("new key: %+v", fresh)
	}
	p = newPhone(t, nodes.Last())
	if resp, _ := p.do(http.MethodGet, "/?key="+key, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old link: %d", resp.StatusCode)
	}

	// Выключили — узел закрыт, состояние off.
	var off RemoteView
	e.json(http.MethodPost, "/api/remote", RemoteRequest{Enabled: ptr(false)}, http.StatusOK, &off)
	if off.State != "off" || off.Enabled || !nodes.Last().Closed() || off.Link != "" || off.Host != "" {
		t.Fatalf("off: %+v", off)
	}
	if code, _ := e.do(http.MethodPost, "/api/remote/devices/nope/revoke", nil); code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", code)
	}
}

// Терминал агента с телефона: настоящий WebSocket через проверку узла; отключили телефон —
// терминал и поток событий оборвались.
func TestRemoteTerminal(t *testing.T) {
	nodes := &remotetest.Nodes{}
	e := setup(t, func(o *Options) { o.RemoteNode = nodes.Factory })
	v := e.enableRemote()
	p := newPhone(t, nodes.Last())
	if resp, _ := p.do(http.MethodGet, strings.TrimPrefix(v.Link, phoneOrigin), nil); resp.StatusCode != http.StatusFound {
		t.Fatalf("login: %d", resp.StatusCode)
	}

	var launched LaunchResponse
	e.json(http.MethodPost, "/api/launch", LaunchRequest{Path: t.TempDir(), Agent: "claude"}, http.StatusOK, &launched)
	key := launched.Key
	tmuxtest.Screen(t, e.marks.Marks(key).Terminal.Tmux, "fake agent:")

	// Поток событий с телефона: первое событие приходит сразу, без буферизации.
	req, _ := http.NewRequest(http.MethodGet, phoneOrigin+"/api/events", nil)
	stream, err := (&http.Client{Transport: remotetest.Transport(p.addr), Jar: p.jar}).Do(req)
	if err != nil || stream.StatusCode != http.StatusOK {
		t.Fatalf("phone events: %v %v", stream, err)
	}
	defer stream.Body.Close()
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stream.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	select {
	case line := <-lines:
		if line != "retry: 3000" {
			t.Fatalf("first line %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phone event stream is buffered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(origin string) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, "wss://"+remotetest.Host+"/api/terminal/"+key+"?cols=100&rows=30",
			&websocket.DialOptions{HTTPClient: &http.Client{Transport: remotetest.Transport(p.addr), Jar: p.jar},
				HTTPHeader: http.Header{"Origin": {origin}}})
	}
	if _, resp, err := dial("http://127.0.0.1:8765"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("local origin through the node: %v %v", resp, err)
	}
	conn, _, err := dial(phoneOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	for !strings.Contains(seen.String(), "got: hello") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v; seen %q", err, seen.String())
		}
		seen.Write(data)
	}

	var view RemoteView
	e.json(http.MethodGet, "/api/remote", nil, http.StatusOK, &view)
	if len(view.Devices) != 1 {
		t.Fatalf("devices: %+v", view.Devices)
	}
	e.json(http.MethodPost, "/api/remote/devices/"+view.Devices[0].ID+"/revoke", nil, http.StatusOK, &view)
	if len(view.Devices) != 0 {
		t.Fatalf("after revoke: %+v", view.Devices)
	}
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("revoked phone keeps the terminal")
			}
			break
		}
	}
	deadline := time.After(5 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-lines:
		case <-deadline:
			t.Fatal("revoked phone keeps the event stream")
		}
	}
	if code := p.status(http.MethodGet, "/api/health", nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked phone: %d", code)
	}
	// Агент жив: отключился только телефон.
	tmuxtest.Screen(t, e.marks.Marks(key).Terminal.Tmux, "got: hello")
}

// Без узла (настоящего tsnet в тестах нет) включение заканчивается ошибкой с причиной.
func TestRemoteErrorState(t *testing.T) {
	e := setup(t)
	var v RemoteView
	e.json(http.MethodPost, "/api/remote", RemoteRequest{Enabled: ptr(true)}, http.StatusOK, &v)
	wait(t, "error state", func() bool {
		e.json(http.MethodGet, "/api/remote", nil, http.StatusOK, &v)
		return v.State == "error"
	})
	if v.Error != errNoTailscale.Error() || v.Link != "" || v.Host != "" {
		t.Fatalf("error view: %+v", v)
	}
}
