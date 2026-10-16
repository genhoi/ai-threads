package terminal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/genhoi/ai-threads/internal/terminal"
	"github.com/genhoi/ai-threads/internal/tmux"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

func TestMain(m *testing.M) { tmuxtest.Main(m) }

// serve — сервер с одним терминалом: ключ claude:abc ведёт в сессию tmux nit-test.
func serve(t *testing.T) *httptest.Server {
	t.Helper()
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-test", Cwd: t.TempDir(), Argv: []string{agent, "--resume", "abc"}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/terminal/{key}", terminal.Handler(func(key string) (string, bool) {
		return "nit-test", key == "claude:abc"
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// until читает двоичные сообщения, пока в выводе не появится text.
func until(t *testing.T, conn *websocket.Conn, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out strings.Builder
	for !strings.Contains(out.String(), text) {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no %q in output %q: %v", text, out.String(), err)
		}
		if typ != websocket.MessageBinary {
			t.Fatalf("message type %v", typ)
		}
		out.Write(data)
	}
}

func write(t *testing.T, conn *websocket.Conn, typ websocket.MessageType, data string) {
	t.Helper()
	if err := conn.Write(context.Background(), typ, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func clients(t *testing.T) string {
	return tmuxtest.Run(t, "list-clients", "-F", "#{client_width}x#{client_height}")
}

func TestStreamsTerminalUntilItIsClosed(t *testing.T) {
	srv := serve(t)
	conn := dial(t, srv, "/api/terminal/claude:abc?cols=100&rows=30")
	until(t, conn, "fake agent: --resume abc")
	tmuxtest.Wait(t, "client 100x30", func() bool { return clients(t) == "100x30" })
	write(t, conn, websocket.MessageBinary, "hello\r")
	until(t, conn, "got: hello")
	write(t, conn, websocket.MessageText, `{"type":"resize","cols":120,"rows":35}`)
	tmuxtest.Wait(t, "client resized", func() bool { return clients(t) == "120x35" })
	write(t, conn, websocket.MessageText, `not json`) // мусор не рвёт соединение
	write(t, conn, websocket.MessageBinary, "quit\r")
	tmuxtest.Wait(t, "agent exited with 4", func() bool {
		p, _, _ := tmux.Get("nit-test")
		return p.Dead && p.ExitCode == 4
	})
	// Закрытый терминал завершает клиента tmux, и сервер закрывает соединение с кодом 1000.
	if err := tmux.Close("nit-test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
			t.Fatalf("close: %v", err)
		}
		break
	}
}

func TestClosingSocketKeepsAgentRunning(t *testing.T) {
	srv := serve(t)
	conn := dial(t, srv, "/api/terminal/claude:abc")
	until(t, conn, "fake agent")
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	tmuxtest.Wait(t, "tmux client gone", func() bool { return clients(t) == "" })
	if p, ok, _ := tmux.Get("nit-test"); !ok || p.Dead {
		t.Fatalf("agent must keep running: %+v", p)
	}
}

func TestFullViewKeepsWindowSize(t *testing.T) {
	srv := serve(t)
	wide := dial(t, srv, "/api/terminal/claude:abc?cols=150&rows=40")
	until(t, wide, "fake agent")
	full := dial(t, srv, "/api/terminal/claude:abc?cols=60&rows=20&view=full")
	until(t, full, "fake agent")
	tmuxtest.Wait(t, "two clients", func() bool { return len(strings.Fields(clients(t))) == 2 })
	write(t, full, websocket.MessageText, `{"type":"resize","cols":50,"rows":15}`)
	time.Sleep(300 * time.Millisecond)
	if got := tmuxtest.Run(t, "display-message", "-p", "-t", "=nit-test:", "#{window_width}x#{window_height}"); got != "150x40" {
		t.Fatalf("window = %s", got)
	}
}

func TestUnknownTerminal(t *testing.T) {
	srv := serve(t)
	for _, path := range []string{"/api/terminal/claude:nope", "/api/terminal/claude:abc"} {
		if path == "/api/terminal/claude:abc" {
			tmux.Close("nit-test") // ключ известен, а сессии tmux уже нет
		}
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
	}
}
