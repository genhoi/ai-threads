package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/tmux"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

// paneKey — метка @nit_key терминала.
func paneKey(t *testing.T, name string) string {
	t.Helper()
	p, ok, err := tmux.Get(name)
	if err != nil || !ok {
		t.Fatalf("pane %s: %v %v", name, ok, err)
	}
	return p.Key
}

// TestLaunchFolderResolvesKey — Codex не принимает ID при запуске: ключ временный, пока
// в каталоге не появится его журнал в той же папке. Потом терминал, записи и состояние
// переходят на настоящий ключ, а страницы получают переименование.
func TestLaunchFolderResolvesKey(t *testing.T) {
	e := setup(t)
	dir := repo(t)
	st := e.events()
	var resp LaunchResponse
	e.json(http.MethodPost, "/api/launch", LaunchRequest{Path: dir, Agent: "codex", Mode: "folder", Prompt: "Почини сборку",
		Model: "gpt-5", Effort: "high"}, http.StatusOK, &resp)
	temp := resp.Key
	if !strings.HasPrefix(temp, "codex:new-") {
		t.Fatalf("key %q", temp)
	}
	m := e.marks.Marks(temp)
	if m.Terminal == nil || m.Terminal.Cwd != dir || m.Worktree != nil {
		t.Fatalf("marks: %+v", m)
	}
	name := m.Terminal.Tmux
	if name != "nit-backend-main" {
		t.Fatalf("tmux name %q", name)
	}
	screen := tmuxtest.Screen(t, name, "fake agent:")
	if !strings.Contains(screen, "-m gpt-5") || !strings.Contains(screen, "Почини сборку") {
		t.Fatalf("argv: %s", screen)
	}
	// Пока журнала нет, сессия видна по временному ключу: название — из запроса.
	var d Details
	e.json(http.MethodGet, "/api/sessions/"+temp, nil, http.StatusOK, &d)
	if d.Session.Title != "Почини сборку" || !d.Session.Terminal || d.Session.Project != "backend" ||
		d.Launch == nil || d.Launch.Model != "gpt-5" || d.Attach == "" || d.Session.Resumable {
		t.Fatalf("pending details: %+v", d)
	}
	st.wait("sessions", func(data string) bool { return strings.Contains(data, temp) })
	e.json(http.MethodPost, "/api/sessions/"+temp+"/pin", map[string]bool{"on": true}, http.StatusOK, nil)

	// Сессия до запуска в той же папке новой не считается.
	codexJournal(t, e.homes["codex"], "44444444-4444-4444-8444-444444444444", dir, time.Now().Add(-time.Hour), "старое", "ok")
	e.waitSession("codex:44444444-4444-4444-8444-444444444444")
	real := "codex:55555555-5555-4555-8555-555555555555"
	codexJournal(t, e.homes["codex"], "55555555-5555-4555-8555-555555555555", dir, time.Now(), "Почини сборку", "Готово")
	data := st.wait("sessions", func(data string) bool { return strings.Contains(data, `"renamed"`) })
	var ev SessionsEvent
	json.Unmarshal([]byte(data), &ev)
	if ev.Renamed[temp] != real || !contains(ev.Removed, temp) {
		t.Fatalf("event: %+v", ev)
	}
	if got := paneKey(t, name); got != real {
		t.Fatalf("tmux key %q", got)
	}
	if e.marks.Marks(temp).Terminal != nil || e.marks.Marks(real).Terminal == nil {
		t.Fatal("terminal record did not move")
	}
	if e.marks.Marks(temp).Pinned || !e.marks.Marks(real).Pinned {
		t.Fatal("pin did not move")
	}
	wait(t, "real key has the terminal", func() bool {
		var d Details
		e.json(http.MethodGet, "/api/sessions/"+real, nil, http.StatusOK, &d)
		return d.Session.Terminal && d.Launch != nil && d.Launch.Effort == "high"
	})
	if code, _ := e.do(http.MethodGet, "/api/sessions/"+temp, nil); code != http.StatusNotFound {
		t.Fatalf("temporary key still answers: %d", code)
	}

	// Папка занята: проверка предлагает копию, ветку из запроса и агента проекта.
	var check LaunchCheck
	e.json(http.MethodGet, "/api/launch/check?path="+dir+"&prompt=BACKEND-10300+limits", nil, http.StatusOK, &check)
	if !check.Exists || !check.Git || check.Mode != "copy" || len(check.Busy) != 1 || check.Busy[0].Key != real ||
		check.Branch != "BACKEND-10300" || check.DefaultAgent != "codex" || check.Project != "backend" || len(check.Recent) == 0 {
		t.Fatalf("check: %+v", check)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestLaunchCopyAndFinish — Claude Code получает ID при запуске; копия с новой веткой;
// завершение не удаляет копию с незакоммиченными файлами и не меняет ничего.
func TestLaunchCopyAndFinish(t *testing.T) {
	e := setup(t)
	dir := repo(t)
	var resp LaunchResponse
	e.json(http.MethodPost, "/api/launch", LaunchRequest{Path: dir, Agent: "claude", Mode: "copy", Branch: "feature-x",
		Prompt: "-n начать"}, http.StatusOK, &resp)
	key := resp.Key
	if !strings.HasPrefix(key, "claude:") || strings.Contains(key, "new-") {
		t.Fatalf("key %q", key)
	}
	_, id, _ := strings.Cut(key, ":")
	wt := e.marks.Marks(key).Worktree
	home, _ := os.UserHomeDir()
	if wt == nil || wt.Path != filepath.Join(home, ".nit", "worktrees", "backend", "feature-x") || wt.Branch != "feature-x" || wt.Repo != dir {
		t.Fatalf("worktree: %+v", wt)
	}
	name := e.marks.Marks(key).Terminal.Tmux
	screen := tmuxtest.Screen(t, name, "fake agent:")
	if !strings.Contains(screen, "--session-id "+id) || !strings.Contains(screen, "-- -n начать") {
		t.Fatalf("argv: %s", screen)
	}
	v := e.session(key)
	if v.Worktree == nil || v.Worktree.Branch != "feature-x" || v.Cwd != wt.Path {
		t.Fatalf("view: %+v", v)
	}

	// Незакоммиченный файл: копию не удалить, и завершение ничего не трогает.
	os.WriteFile(filepath.Join(wt.Path, "new.txt"), []byte("x"), 0o644)
	var check FinishCheck
	e.json(http.MethodGet, "/api/sessions/"+key+"/finish", nil, http.StatusOK, &check)
	if check.CanRemove || len(check.Dirty) != 1 || check.Dirty[0] != "new.txt" || !check.Merged || !check.CanDeleteBranch {
		t.Fatalf("check: %+v", check)
	}
	var ev ErrorView
	e.json(http.MethodPost, "/api/sessions/"+key+"/finish", FinishRequest{RemoveWorktree: true}, http.StatusConflict, &ev)
	if !strings.Contains(ev.Error, "new.txt") || !tmux.Exists(name) || e.marks.Marks(key).Done {
		t.Fatalf("refused finish changed something: %+v", ev)
	}

	// Чистая копия удаляется вместе с веткой; терминал закрывается.
	os.Remove(filepath.Join(wt.Path, "new.txt"))
	// Журнал появился: после закрытия терминала сессия остаётся в истории.
	claudeJournal(t, e.homes["claude"], id, wt.Path, "-n начать")
	e.waitSession(key)
	v = SessionView{}
	e.json(http.MethodPost, "/api/sessions/"+key+"/finish", FinishRequest{RemoveWorktree: true, DeleteBranch: true}, http.StatusOK, &v)
	if !v.Done || v.Terminal || v.State != model.StateIdle || v.Worktree != nil {
		t.Fatalf("finished: %+v", v)
	}
	if tmux.Exists(name) || isDir(wt.Path) || strings.Contains(git(t, dir, "branch"), "feature-x") {
		t.Fatal("terminal, copy or branch survived")
	}
	if m := e.marks.Marks(key); m.Terminal != nil || m.Worktree != nil {
		t.Fatalf("marks: %+v", m)
	}
}

// TestLaunchTypesPrompt — Kimi не принимает запрос аргументом: «Нить» набирает его сама,
// когда агент показал первый экран.
func TestLaunchTypesPrompt(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	var resp LaunchResponse
	e.json(http.MethodPost, "/api/launch", LaunchRequest{Path: dir, Agent: "kimi", Prompt: "ping"}, http.StatusOK, &resp)
	name := e.marks.Marks(resp.Key).Terminal.Tmux
	if name != "nit-kimi-"+strings.TrimPrefix(resp.Key, "kimi:") {
		t.Fatalf("tmux name %q", name)
	}
	screen := tmuxtest.Screen(t, name, "got: ping")
	if strings.Contains(screen, "fake agent: ping") {
		t.Fatalf("prompt must not be in argv: %s", screen)
	}
}

func TestLaunchErrors(t *testing.T) {
	e := setup(t)
	st := e.events()
	dir := t.TempDir()
	cases := []struct {
		req  LaunchRequest
		want int
	}{
		{LaunchRequest{Path: "relative", Agent: "codex"}, http.StatusBadRequest},
		{LaunchRequest{Path: filepath.Join(dir, "gone"), Agent: "codex"}, http.StatusBadRequest},
		{LaunchRequest{Path: dir, Agent: "nope"}, http.StatusBadRequest},
		{LaunchRequest{Path: dir, Agent: "grok"}, http.StatusConflict}, // программы нет
		{LaunchRequest{Path: dir, Agent: "codex", Model: "x; rm -rf"}, http.StatusBadRequest},
		{LaunchRequest{Path: dir, Agent: "codex", Mode: "copy"}, http.StatusBadRequest}, // не git
		{LaunchRequest{Path: dir, Agent: "codex", Mode: "elsewhere"}, http.StatusBadRequest},
	}
	for _, c := range cases {
		var ev ErrorView
		e.json(http.MethodPost, "/api/launch", c.req, c.want, &ev)
		if ev.Error == "" {
			t.Fatalf("%+v: no error text", c.req)
		}
	}
	data := st.wait("notice", func(string) bool { return true })
	var n NoticeEvent
	json.Unmarshal([]byte(data), &n)
	if n.Kind != "error" || n.Text == "" {
		t.Fatalf("notice: %+v", n)
	}
}

// TestResume — сессия открывается в терминале; агент, который идёт с ней вне «Нити»,
// второй раз не запускается.
func TestResume(t *testing.T) {
	e := setup(t)
	cwd := t.TempDir()
	id := "66666666-6666-4666-8666-666666666666"
	key := "codex:" + id
	codexJournal(t, e.homes["codex"], id, cwd, time.Now(), "Задача", "Ответ")
	e.waitSession(key)
	var v SessionView
	e.json(http.MethodPost, "/api/sessions/"+key+"/resume", nil, http.StatusOK, &v)
	if !v.Terminal || (v.State != model.StateStarting && v.State != model.StateWorking) {
		t.Fatalf("resumed: %+v", v)
	}
	name := e.marks.Marks(key).Terminal.Tmux
	tmuxtest.Screen(t, name, "fake agent: resume "+id)
	// Второй раз — тот же терминал.
	e.json(http.MethodPost, "/api/sessions/"+key+"/resume", nil, http.StatusOK, &v)
	panes, _ := tmux.List()
	if len(panes) != 1 {
		t.Fatalf("panes: %+v", panes)
	}

	// Claude Code сообщает в реестре, что сессия идёт в другом терминале.
	cid := "77777777-7777-4777-8777-777777777777"
	ckey := "claude:" + cid
	claudeJournal(t, e.homes["claude"], cid, cwd, "Другая задача")
	e.waitSession(ckey)
	registry := filepath.Join(e.homes["claude"], "sessions", "1.json")
	os.MkdirAll(filepath.Dir(registry), 0o755)
	raw, _ := json.Marshal(map[string]any{"sessionId": cid, "pid": os.Getpid(), "status": "busy", "kind": "interactive",
		"updatedAt": time.Now().UnixMilli()})
	os.WriteFile(registry, raw, 0o644)
	wait(t, "registry is read", func() bool {
		v := e.session(ckey)
		return v.Where == "terminal" && v.State == model.StateWorking && !v.Terminal
	})
	var ev ErrorView
	e.json(http.MethodPost, "/api/sessions/"+ckey+"/resume", nil, http.StatusConflict, &ev)
	if !strings.Contains(ev.Error, "another terminal") {
		t.Fatalf("error: %+v", ev)
	}
}

// TestGrokRegistryIsGuess — реестр Grok знает только, что процесс жив: сессию вне «Нити» он
// показывает, а состояние терминала «Нити» не перебивает, иначе Grok никогда бы не ждал.
func TestGrokRegistryIsGuess(t *testing.T) {
	e := setup(t)
	key := "grok:" + agentstest.SID(1)
	e.waitSession(key)
	raw, _ := json.Marshal([]any{map[string]any{"session_id": agentstest.SID(1), "pid": os.Getpid(), "cwd": "/tmp/g",
		"opened_at": time.Now().Unix()}})
	os.WriteFile(filepath.Join(e.homes["grok"], "active_sessions.json"), raw, 0o644)
	if states := e.s.override(); len(states) != 0 {
		t.Fatalf("override: %v", states)
	}
	if st, ok := e.s.elsewhere(key); !ok || st.Where != "terminal" {
		t.Fatalf("elsewhere: %+v %v", st, ok)
	}
}

// TestTerminal — WebSocket к терминалу, экран текстом и клавиши с телефона.
func TestTerminal(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	var resp LaunchResponse
	e.json(http.MethodPost, "/api/launch", LaunchRequest{Path: dir, Agent: "claude"}, http.StatusOK, &resp)
	key := resp.Key
	name := e.marks.Marks(key).Terminal.Tmux
	tmuxtest.Screen(t, name, "fake agent:")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	header := http.Header{"Origin": {e.origin}}
	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(e.port)+"/api/terminal/"+key+"?cols=100&rows=30",
		&websocket.DialOptions{HTTPHeader: header})
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
	if e.marks.Marks(key).Seen == 0 {
		t.Fatal("watching the terminal must mark the session seen")
	}
	if _, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(e.port)+"/api/terminal/"+key,
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.example"}}}); err == nil {
		t.Fatal("foreign origin must be refused")
	}

	e.json(http.MethodPost, "/api/terminal/"+key+"/keys", KeysRequest{Text: "abc", Keys: []string{"Enter"}}, http.StatusOK, nil)
	var screen ScreenView
	wait(t, "keys reach the agent", func() bool {
		e.json(http.MethodGet, "/api/terminal/"+key+"/screen?history=100", nil, http.StatusOK, &screen)
		return strings.Contains(screen.Text, "got: abc")
	})
	if code, _ := e.do(http.MethodPost, "/api/terminal/"+key+"/keys", KeysRequest{Keys: []string{"rm"}}); code != http.StatusBadRequest {
		t.Fatalf("unknown key: %d", code)
	}
	if code, _ := e.do(http.MethodGet, "/api/terminal/claude:nope/screen", nil); code != http.StatusNotFound {
		t.Fatalf("closed terminal: %d", code)
	}
}

// TestSearch — быстрый поиск по полям и переписке, полный — по журналам с ходом в событиях.
func TestSearch(t *testing.T) {
	e := setup(t)
	var hits []SearchHit
	e.json(http.MethodGet, "/api/search?q=%D0%BF%D1%80%D0%BE%D0%B2%D0%B5%D1%80%D0%B8%D1%82%D1%8C+%D0%BA%D0%B0%D1%82%D0%B0%D0%BB%D0%BE%D0%B3",
		nil, http.StatusOK, &hits) // «проверить каталог»
	if len(hits) == 0 || hits[0].Field != "title" || !strings.Contains(hits[0].Snippet, "Проверить каталог") {
		t.Fatalf("title hits: %+v", hits)
	}
	q := "%D0%BF%D1%80%D0%BE%D0%B2%D0%B5%D1%80%D0%BA%D0%B0+%D0%B7%D0%B0%D0%B2%D0%B5%D1%80%D1%88%D0%B5%D0%BD%D0%B0" // «проверка завершена»
	e.json(http.MethodGet, "/api/search?q="+q, nil, http.StatusOK, &hits)
	if len(hits) != 0 {
		t.Fatalf("conversation is searched only with conv=1: %+v", hits)
	}
	e.json(http.MethodGet, "/api/search?conv=1&q="+q, nil, http.StatusOK, &hits)
	if len(hits) == 0 || hits[0].Field != "conversation" || !strings.Contains(hits[0].Snippet, "Проверка завершена") {
		t.Fatalf("conversation hits: %+v", hits)
	}
	for _, h := range hits {
		if h.Session.Service {
			t.Fatalf("service session without service=1: %+v", h.Session)
		}
	}

	// Путь к файлу есть только в вызове инструмента: его находит полный поиск.
	st := e.events()
	var started FullSearchStarted
	e.json(http.MethodPost, "/api/search/full", FullSearchRequest{Q: "sample/APP.py"}, http.StatusOK, &started)
	data := st.wait("search", func(data string) bool {
		var ev SearchEvent
		json.Unmarshal([]byte(data), &ev)
		return ev.ID == started.ID && ev.Finished
	})
	var ev SearchEvent
	json.Unmarshal([]byte(data), &ev)
	if len(ev.Hits) != 1 || ev.Hits[0].Session.Key != "claude:"+agentstest.SID(1) || ev.Hits[0].Field != "log" ||
		!strings.Contains(ev.Hits[0].Snippet, "app.py") || ev.Done != ev.Total {
		t.Fatalf("full search: %+v", ev)
	}
	if code, _ := e.do(http.MethodPost, "/api/search/full", FullSearchRequest{Q: " "}); code != http.StatusBadRequest {
		t.Fatalf("empty query: %d", code)
	}
}

func TestNeedles(t *testing.T) {
	esc := func(codes ...string) string {
		var b strings.Builder
		for _, c := range codes {
			b.WriteString(`\u` + c)
		}
		return b.String()
	}
	got := needles("База")
	want := []string{"база", esc("0431", "0430", "0437", "0430"), "База", esc("0411", "0430", "0437", "0430"),
		"БАЗА", esc("0411", "0410", "0417", "0410")}
	if len(got) != len(want) {
		t.Fatalf("needles: %q", got)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("needles[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if n := needles("Main.GO"); len(n) != 1 || string(n[0]) != "main.go" {
		t.Fatalf("ascii: %q", n)
	}
	if string(asciiLower([]byte("ABC Дом"))) != "abc Дом" {
		t.Fatal("asciiLower must change only ASCII")
	}
}

// TestReboot — терминалы, которых tmux больше не знает, предлагаются к продолжению.
func TestReboot(t *testing.T) {
	e := setup(t)
	cwd := t.TempDir()
	id := "88888888-8888-4888-8888-888888888888"
	key := "codex:" + id
	codexJournal(t, e.homes["codex"], id, cwd, time.Now(), "Задача", "Ответ")
	e.waitSession(key)
	if err := e.marks.RecordTerminal(key, store.Terminal{Tmux: "nit-gone", Cwd: cwd}); err != nil {
		t.Fatal(err)
	}
	other := "claude:" + agentstest.SID(1)
	if err := e.marks.RecordTerminal(other, store.Terminal{Tmux: "nit-gone-2", Cwd: "/home/user/projects/sample app"}); err != nil {
		t.Fatal(err)
	}
	e.s.mu.Lock()
	e.s.lastSeen.items[key] = lastState{State: model.StateWorking, Since: 1000, At: 1000 + 7*60}
	e.s.mu.Unlock()
	var view RebootView
	e.json(http.MethodGet, "/api/reboot", nil, http.StatusOK, &view)
	if len(view.Sessions) != 2 {
		t.Fatalf("reboot: %+v", view)
	}
	var mine RebootSession
	for _, rs := range view.Sessions {
		if rs.Session.Key == key {
			mine = rs
		}
	}
	if mine.Was != model.StateWorking || mine.Minutes != 7 {
		t.Fatalf("reboot session: %+v", mine)
	}
	var res RebootResult
	e.json(http.MethodPost, "/api/reboot", RebootRequest{Keys: []string{key}, Dismiss: true}, http.StatusOK, &res)
	if len(res.Resumed) != 1 || res.Resumed[0] != key {
		t.Fatalf("result: %+v", res)
	}
	tmuxtest.Screen(t, e.marks.Marks(key).Terminal.Tmux, "fake agent: resume "+id)
	if e.marks.Marks(other).Terminal != nil {
		t.Fatal("dismissed terminal is remembered")
	}
	e.json(http.MethodGet, "/api/reboot", nil, http.StatusOK, &view)
	if len(view.Sessions) != 0 {
		t.Fatalf("after: %+v", view)
	}
}

// TestRestorePending — после перезапуска «Нити» терминал с временным ключом снова ждёт журнал.
func TestRestorePending(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	temp := "codex:new-abc"
	pane, err := tmux.Open(tmux.OpenOptions{Name: "nit-restored", Key: temp, Cwd: dir, Argv: []string{e.stub}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.marks.RecordTerminal(temp, store.Terminal{Tmux: pane.Name, Cwd: dir, Started: time.Now().Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	e.s.restorePending()
	if _, ok := e.s.session(temp); !ok {
		t.Fatal("restored session is not visible")
	}
	codexJournal(t, e.homes["codex"], "99999999-9999-4999-8999-999999999999", dir, time.Now(), "Задача", "Ответ")
	wait(t, "restored key resolves", func() bool { return paneKey(t, "nit-restored") == "codex:99999999-9999-4999-8999-999999999999" })
}
