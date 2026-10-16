package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/model"
)

// TestMain прячет настоящие папки агентов.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "catalog-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	for _, name := range agentstest.HomeEnvs {
		os.Unsetenv(name)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func key(tool string, n int) string {
	if tool == "kimi" {
		return "kimi:session_" + agentstest.SID(n)
	}
	return tool + ":" + agentstest.SID(n)
}

// jsonlAgents — Codex, Claude Code, Grok и Kimi на копиях эталонов: 13 сессий.
func jsonlAgents(t *testing.T) (map[string]string, []agents.Agent) {
	t.Helper()
	homes := agentstest.Homes(t)
	var list []agents.Agent
	for _, id := range []string{"codex", "claude", "grok", "kimi"} {
		list = append(list, agents.New(id, agents.Config{Home: homes[id]}))
	}
	return homes, list
}

// start запускает каталог и ждёт первого обхода.
func start(t *testing.T, o Options) *Catalog {
	t.Helper()
	c := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case <-c.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("первый обход не закончился")
	}
	return c
}

// stop останавливает каталог: Run пишет кэш перед выходом.
func stop(t *testing.T, o Options) *Catalog {
	t.Helper()
	c := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()
	<-c.Ready()
	cancel()
	<-done
	return c
}

func sessionsJSON(t *testing.T, c *Catalog) string {
	t.Helper()
	data, err := json.Marshal(c.Sessions())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPersistentCacheDoesNotOpenUnchangedFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root читает файлы без прав")
	}
	homes, list := jsonlAgents(t)
	cacheFile := filepath.Join(t.TempDir(), "catalog.json")
	first := stop(t, Options{Agents: list, CacheFile: cacheFile})
	if n := len(first.Sessions()); n != 13 {
		t.Fatalf("sessions = %d", n)
	}
	// Права на чтение не входят в отпечаток файла: если каталог откроет журнал, разбор сломается.
	var locked []string
	for _, home := range homes {
		_ = filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.Mode().IsRegular() {
				locked = append(locked, path)
				return os.Chmod(path, 0)
			}
			return nil
		})
	}
	t.Cleanup(func() {
		for _, path := range locked {
			_ = os.Chmod(path, 0o644)
		}
	})
	second := start(t, Options{Agents: list, CacheFile: cacheFile})
	if got, want := sessionsJSON(t, second), sessionsJSON(t, first); got != want {
		t.Fatalf("после кэша:\n%s\nбыло:\n%s", got, want)
	}
}

// events собирает события каталога.
type events struct {
	mu   sync.Mutex
	list []Event
}

func (e *events) add(ev Event) {
	e.mu.Lock()
	e.list = append(e.list, ev)
	e.mu.Unlock()
}

// wait ждёт события, для которого ok вернёт true.
func (e *events) wait(t *testing.T, ok func(Event) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		found := slices.ContainsFunc(e.list, ok)
		e.mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("событие не пришло")
}

func TestChangesAndDeletionsArePushed(t *testing.T) {
	_, list := jsonlAgents(t)
	c := start(t, Options{Agents: list, Interval: 20 * time.Millisecond})
	got := &events{}
	defer c.Subscribe(got.add)()
	modified, _ := c.Get(key("grok", 1))
	deleted, _ := c.Get(key("claude", 1))
	f, err := os.OpenFile(modified.Journal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type": "assistant", "content": "Новый ответ"}` + "\n")
	f.Close()
	if err := os.Remove(deleted.Meta); err != nil {
		t.Fatal(err)
	}
	got.wait(t, func(e Event) bool { return slices.Contains(e.Changed, modified.Key()) })
	got.wait(t, func(e Event) bool { return slices.Contains(e.Removed, deleted.Key()) })
	if s, _ := c.Get(modified.Key()); s.Last != "Новый ответ" {
		t.Fatalf("last = %q", s.Last)
	}
	if _, ok := c.Get(deleted.Key()); ok {
		t.Fatal("удалённая сессия осталась")
	}
	// Без изменений события не приходят.
	got.mu.Lock()
	got.list = nil
	got.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	got.mu.Lock()
	defer got.mu.Unlock()
	if len(got.list) != 0 {
		t.Fatalf("лишние события: %v", got.list)
	}
}

// slowAgent задерживает первое чтение, пока тест не отпустит его.
type slowAgent struct {
	agents.Agent
	entered, release chan struct{}
	calls            atomic.Int32
}

func slow(a agents.Agent) *slowAgent {
	return &slowAgent{Agent: a, entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *slowAgent) Scan(c *agents.Cache, units []string, onUnit func([]model.Session)) []model.Session {
	if s.calls.Add(1) == 1 {
		close(s.entered)
		<-s.release
	}
	return s.Agent.Scan(c, units, onUnit)
}

func TestFirstScanProgress(t *testing.T) {
	_, list := jsonlAgents(t)
	claude := slow(list[1])
	list[1] = claude
	c := New(Options{Agents: list})
	got := &events{}
	defer c.Subscribe(got.add)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	<-claude.entered
	p := c.Progress()
	if p.Ready || p.Done <= 0 || p.Done >= p.Total {
		t.Errorf("progress = %+v", p)
	}
	want := map[string]string{"codex": "done", "claude": "reading", "grok": "waiting", "kimi": "waiting"}
	if !mapsEqual(p.Agents, want) {
		t.Errorf("agents = %v", p.Agents)
	}
	// Прочитанное видно, не дожидаясь конца обхода.
	if n := len(c.Sessions()); n != 3 {
		t.Errorf("sessions = %d", n)
	}
	close(claude.release)
	<-c.Ready()
	p = c.Progress()
	if !p.Ready || p.Done != p.Total || len(c.Sessions()) != 13 {
		t.Fatalf("progress = %+v", p)
	}
	for id, state := range p.Agents {
		if state != "done" {
			t.Errorf("%s: %s", id, state)
		}
	}
	got.wait(t, func(e Event) bool { return e.Progress.Ready && e.Progress.Done == e.Progress.Total })
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestBadCacheIsRebuilt(t *testing.T) {
	for _, content := range []string{"{broken", "[]", `{"not-a-path": 42}`, `{"Version": 999, "Agents": {"codex": {"Files": {}}}}`,
		`{"Version": 1, "Agents": {"codex": {"Files": {"x": 12}}}}`} {
		_, list := jsonlAgents(t)
		cacheFile := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(cacheFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if n := len(stop(t, Options{Agents: list, CacheFile: cacheFile}).Sessions()); n != 13 {
			t.Errorf("%s: sessions = %d", content, n)
		}
	}
}

func TestForcedRefreshDuringScanRunsAgain(t *testing.T) {
	_, list := jsonlAgents(t)
	codex := slow(list[0])
	list[0] = codex
	c := New(Options{Agents: list, Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	<-codex.entered
	c.Refresh()
	close(codex.release)
	<-c.Ready()
	deadline := time.Now().Add(5 * time.Second)
	for codex.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := codex.calls.Load(); n != 2 {
		t.Fatalf("calls = %d", n)
	}
}

func TestSetAgentsRemovesSessionsOfDisabledAgent(t *testing.T) {
	_, list := jsonlAgents(t)
	c := start(t, Options{Agents: list, Interval: time.Hour})
	got := &events{}
	defer c.Subscribe(got.add)()
	c.SetAgents(slices.Delete(slices.Clone(list), 1, 2))
	got.wait(t, func(e Event) bool { return slices.Contains(e.Removed, key("claude", 1)) && e.Progress.Ready })
	for _, s := range c.Sessions() {
		if s.Agent == "claude" {
			t.Fatalf("осталась %s", s.Key())
		}
	}
	if c.Agent("claude") != nil || c.Agent("codex") == nil {
		t.Fatal("набор агентов не сменился")
	}
}

func TestCatalogReadsAndCachesSQLiteAgents(t *testing.T) {
	homes := agentstest.Homes(t)
	agentstest.ZCode(t, homes["zcode"])
	agentstest.CursorChats(t, homes["cursor"])
	list := []agents.Agent{agents.New("zcode", agents.Config{Home: homes["zcode"]}),
		agents.New("cursor", agents.Config{Home: homes["cursor"]})}
	cacheFile := filepath.Join(t.TempDir(), "catalog.json")
	first := stop(t, Options{Agents: list, CacheFile: cacheFile})
	var keys []string
	for _, s := range first.Sessions() {
		keys = append(keys, s.Key())
	}
	slices.Sort(keys)
	want := []string{"cursor:" + agentstest.SID(1), "cursor:" + agentstest.SID(2), "cursor:" + agentstest.SID(3),
		"cursor:" + agentstest.SID(4), "zcode:sess_" + agentstest.SID(1), "zcode:sess_" + agentstest.SID(3),
		"zcode:sess_" + agentstest.SID(4)}
	if !slices.Equal(keys, want) {
		t.Fatalf("keys = %v", keys)
	}
	if p := first.Progress(); p.Done != 5 || p.Total != 5 {
		t.Fatalf("progress = %+v", p)
	}
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), agentstest.Secret) || !strings.Contains(string(data), "db.sqlite") {
		t.Fatal("кэш без базы ZCode или с ключом Cursor Agent")
	}
	again := stop(t, Options{Agents: list, CacheFile: cacheFile})
	if sessionsJSON(t, again) != sessionsJSON(t, first) {
		t.Fatal("после кэша сессии другие")
	}
}

func TestSessionsAreNewestFirst(t *testing.T) {
	homes, list := jsonlAgents(t)
	c := start(t, Options{Agents: list, Interval: 20 * time.Millisecond})
	got := &events{}
	defer c.Subscribe(got.add)()
	later := time.Now()
	unit := filepath.Join(homes["grok"], "sessions", agentstest.SID(3), "summary.json")
	data, _ := os.ReadFile(unit)
	text := strings.Replace(string(data), `"last_active_at": "2026-09-28T10:00:00Z"`, `"last_active_at": "`+later.UTC().Format(time.RFC3339)+`"`, 1)
	if err := os.WriteFile(unit, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	got.wait(t, func(e Event) bool { return slices.Contains(e.Changed, key("grok", 3)) })
	if first := c.Sessions()[0]; first.Key() != key("grok", 3) {
		t.Fatalf("first = %s", first.Key())
	}
}
