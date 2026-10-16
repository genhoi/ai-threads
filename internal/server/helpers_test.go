package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

// TestMain прячет окружение человека: домашняя папка, данные и настройки — временные,
// папки агентов — копии эталонов, tmux — свой сервер, а в PATH нет настоящих агентов.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "server-home-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME": home, "AI_THREADS_DATA": filepath.Join(home, "data"), "AI_THREADS_CONFIG": filepath.Join(home, "settings.json"),
		"PATH": "/usr/bin:/bin", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		os.Setenv(k, v)
	}
	os.Unsetenv("WSL_DISTRO_NAME")
	// Тесты идут и в WSL: признак WSL берётся только из переменной, которую тест задаёт сам.
	wslMarker = filepath.Join(home, "no-wsl")
	os.Unsetenv("XDG_DATA_HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	for _, name := range agentstest.HomeEnvs {
		os.Unsetenv(name)
	}
	defer os.RemoveAll(home)
	tmuxtest.Main(m)
}

// env — сервер на настоящем порте с эталонными журналами, своим tmux и заглушками агентов.
type env struct {
	t      *testing.T
	s      *Server
	url    string
	port   int
	origin string
	homes  map[string]string
	stub   string // заглушка агента: печатает аргументы и отвечает на строки
	conf   *settings.File
	marks  *store.Store
}

// setup поднимает сервер. Codex, Claude Code и Kimi запускаются заглушкой, ZCode и Cursor
// выключены, чтобы не собирать их базы. opts меняют Options до запуска.
func setup(t *testing.T, opts ...func(*Options)) *env {
	t.Helper()
	tmuxtest.Server(t)
	homes := agentstest.Homes(t)
	stub := tmuxtest.AgentTTY(t)
	dir := t.TempDir()
	// HOME — своя на тест: в ней появятся копии ~/.nit/worktrees.
	t.Setenv("HOME", filepath.Join(dir, "home"))
	os.MkdirAll(filepath.Join(dir, "home"), 0o755)
	conf := settings.New(filepath.Join(dir, "settings.json"))
	s := settings.Defaults()
	for _, id := range []string{"codex", "claude", "grok", "kimi"} {
		a := s.Agents[id]
		a.Home = homes[id]
		if id != "grok" {
			a.Program = stub
		}
		s.Agents[id] = a
	}
	s.Agents["zcode"] = settings.Agent{Enabled: false}
	s.Agents["cursor"] = settings.Agent{Enabled: false}
	// Временные папки теста лежат в /tmp: по умолчанию их сессии были бы служебными.
	s.Service.TempDirs = []string{"/var/tmp"}
	if err := conf.Save(s); err != nil {
		t.Fatal(err)
	}
	marks, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	o := Options{Port: port, Version: "test", Settings: conf, Store: marks,
		CacheFile: filepath.Join(dir, "catalog.json"), StateFile: filepath.Join(dir, "terminals.json"),
		CatalogInterval: 50 * time.Millisecond, LiveInterval: 50 * time.Millisecond, LiveThreshold: 300 * time.Millisecond,
		RemoteKeys: remoteKeys(t, dir), RemoteNode: noNode(t)}
	for _, fn := range opts {
		fn(&o)
	}
	srv := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.Run(ctx)
		close(done)
	}()
	hs := &httptest.Server{Listener: ln, Config: &http.Server{Handler: srv}}
	hs.Start()
	t.Cleanup(func() {
		cancel()
		<-done
		hs.Close()
	})
	select {
	case <-srv.catalog.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("catalog is not ready")
	}
	return &env{t: t, s: srv, url: "http://127.0.0.1:" + strconv.Itoa(port), port: port,
		origin: "http://127.0.0.1:" + strconv.Itoa(port), homes: homes, stub: stub, conf: conf, marks: marks}
}

// do выполняет запрос; у изменяющих запросов — Origin самой «Нити».
func (e *env) do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			reader = strings.NewReader(b)
		case []byte:
			reader = strings.NewReader(string(b))
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				e.t.Fatal(err)
			}
			reader = strings.NewReader(string(raw))
		}
	}
	req, err := http.NewRequest(method, e.url+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", e.origin)
	}
	req.Header.Set("Accept-Language", "en")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// json выполняет запрос, ждёт статус want и разбирает ответ в out.
func (e *env) json(method, path string, body any, want int, out any) {
	e.t.Helper()
	code, raw := e.do(method, path, body)
	if code != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, code, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
}

// stream — поток событий SSE.
type stream struct {
	t      *testing.T
	events chan sseEvent
	cancel func()
}

type sseEvent struct {
	name string
	data string
}

func (e *env) events() *stream {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.url+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		e.t.Fatalf("content type %q", ct)
	}
	st := &stream{t: e.t, events: make(chan sseEvent, 1000), cancel: func() { cancel(); resp.Body.Close() }}
	go func() {
		defer close(st.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = line[7:]
			case strings.HasPrefix(line, "data: "):
				ev.data = line[6:]
			case line == "":
				if ev.name != "" {
					st.events <- ev
				}
				ev = sseEvent{}
			}
		}
	}()
	e.t.Cleanup(st.cancel)
	return st
}

// wait ждёт событие name, для которого ok вернёт true.
func (st *stream) wait(name string, ok func(data string) bool) string {
	st.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, open := <-st.events:
			if !open {
				st.t.Fatalf("stream closed while waiting for %s", name)
			}
			if ev.name == name && ok(ev.data) {
				return ev.data
			}
			if testing.Verbose() {
				st.t.Logf("skip event %s: %.300s", ev.name, ev.data)
			}
		case <-deadline:
			st.t.Fatalf("timed out waiting for event %s", name)
		}
	}
}

// wait ждёт условие до 10 секунд.
func wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", what)
}

// git выполняет команду git в папке dir.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo — репозиторий backend с одним коммитом в main, отправленным в origin.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "backend")
	remote := filepath.Join(root, "origin.git")
	git(t, root, "init", "-q", "--bare", "-b", "main", remote)
	git(t, root, "init", "-q", "-b", "main", dir)
	os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "-u", "origin", "main")
	git(t, dir, "remote", "set-head", "origin", "main")
	return dir
}

func writeRows(t *testing.T, path string, rows ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codexJournal пишет журнал Codex: сессия id в папке cwd, созданная в created. Время — с долями
// секунды, как пишет сам Codex.
func codexJournal(t *testing.T, home, id, cwd string, created time.Time, prompt, answer string) {
	t.Helper()
	at := created.UTC().Format(time.RFC3339Nano)
	path := filepath.Join(home, "sessions", created.Format("2006/01/02"),
		fmt.Sprintf("rollout-%s-%s.jsonl", created.UTC().Format("2006-01-02T15-04-05"), id))
	writeRows(t, path,
		map[string]any{"timestamp": at, "type": "session_meta", "payload": map[string]any{"id": id, "timestamp": at, "cwd": cwd, "source": "cli"}},
		map[string]any{"timestamp": at, "type": "response_item", "payload": map[string]any{"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": prompt}}}},
		map[string]any{"timestamp": at, "type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": answer}}}},
	)
}

// claudeJournal пишет журнал Claude Code: сессия id в папке cwd.
func claudeJournal(t *testing.T, home, id, cwd, prompt string) {
	t.Helper()
	at := time.Now().UTC().Format(time.RFC3339)
	writeRows(t, filepath.Join(home, "projects", "-test", id+".jsonl"),
		map[string]any{"type": "user", "timestamp": at, "entrypoint": "cli", "cwd": cwd, "sessionId": id,
			"message": map[string]any{"role": "user", "content": prompt}},
		map[string]any{"type": "assistant", "timestamp": at, "cwd": cwd,
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Готово."}}}},
	)
}

// page — GET-запрос к истории: сессии и заголовки ответа.
func (e *env) page(path string) ([]SessionView, http.Header) {
	e.t.Helper()
	resp, err := http.Get(e.url + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out []SessionView
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil {
		e.t.Fatalf("GET %s: %d %s", path, resp.StatusCode, raw)
	}
	return out, resp.Header
}

// session — сессия по ключу через API.
func (e *env) session(key string) SessionView {
	e.t.Helper()
	var d Details
	e.json(http.MethodGet, "/api/sessions/"+key, nil, http.StatusOK, &d)
	return d.Session
}

// waitSession ждёт, пока сессия не появится в каталоге.
func (e *env) waitSession(key string) {
	e.t.Helper()
	wait(e.t, "session "+key+" in catalog", func() bool {
		_, ok := e.s.catalog.Get(key)
		return ok
	})
}
