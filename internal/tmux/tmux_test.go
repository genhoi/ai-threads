package tmux_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/tmux"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

func TestMain(m *testing.M) { tmuxtest.Main(m) }

func TestNames(t *testing.T) {
	cases := []struct {
		pattern string
		vars    tmux.NameVars
		want    string
	}{
		{"", tmux.NameVars{Project: "backend", Branch: "BACKEND-10300"}, "nit-backend-BACKEND-10300"},
		{"", tmux.NameVars{Project: "backend", Branch: "feature/new thing"}, "nit-backend-feature-new-thing"},
		// Ветки нет: запасной шаблон по агенту и ID.
		{"", tmux.NameVars{Project: "notes", Agent: "claude", ID: "abc-1_2"}, "nit-claude-abc-1_2"},
		{"{agent}@{project}", tmux.NameVars{Project: "api", Agent: "codex"}, "codex-api"},
		{"", tmux.NameVars{}, "nit"},
		{"", tmux.NameVars{Project: "проект", Branch: "main"}, "nit-проект-main"},
	}
	for _, c := range cases {
		if got := tmux.Name(c.pattern, c.vars); got != c.want {
			t.Errorf("Name(%q, %+v) = %q, want %q", c.pattern, c.vars, got, c.want)
		}
	}
	// Точку tmux не принимает: она становится «_», а хеш различает «a.b» и «a_b».
	dotted := tmux.Sanitize("nit-kimi-a.b")
	if !strings.HasPrefix(dotted, "nit-kimi-a_b-") || dotted == tmux.Sanitize("nit-kimi-a_b") {
		t.Errorf("dotted name %q", dotted)
	}
	if got := tmux.Sanitize("x;y'z--"); got != "x-y-z" {
		t.Errorf("Sanitize = %q", got)
	}
	if got := len([]rune(tmux.Sanitize(strings.Repeat("a", 100)))); got != 64 {
		t.Errorf("long name has %d runes", got)
	}
}

func TestShellJoin(t *testing.T) {
	got := tmux.ShellJoin([]string{"claude", "--model", "opus", "fix it's bug", "", "a;b"})
	want := `claude --model opus 'fix it'\''s bug' '' 'a;b'`
	if got != want {
		t.Fatalf("ShellJoin = %s, want %s", got, want)
	}
}

func TestOpenAttachExitAndClose(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	dir := t.TempDir()
	pane, err := tmux.Open(tmux.OpenOptions{Name: "nit-test", Cwd: dir, Argv: []string{agent, "--resume", "abc"},
		Cols: 100, Rows: 30, Key: "claude:abc"})
	if err != nil {
		t.Fatal(err)
	}
	if pane.Dead || pane.Key != "claude:abc" || pane.Cwd != dir || pane.Created.IsZero() || pane.PID == 0 {
		t.Fatalf("pane = %+v", pane)
	}
	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-test", Cwd: dir, Argv: []string{agent}}); err == nil {
		t.Fatal("second session with the same name must fail")
	}

	att, err := tmux.Attach("nit-test", 90, 25, tmux.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer att.Close()
	out := reader(att)
	out.until(t, "fake agent: --resume abc")
	if _, err := att.Write([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	out.until(t, "got: hello")
	if err := att.Resize(130, 40); err != nil {
		t.Fatal(err)
	}
	tmuxtest.Wait(t, "client resized", func() bool { return clients(t) == "130x40" })

	if _, err := att.Write([]byte("quit\r")); err != nil {
		t.Fatal(err)
	}
	var dead tmux.Pane
	tmuxtest.Wait(t, "agent exited", func() bool {
		dead, _, _ = tmux.Get("nit-test")
		return dead.Dead
	})
	if dead.ExitCode != 4 || dead.DiedAt.IsZero() {
		t.Fatalf("dead pane = %+v", dead)
	}

	// Закрытый терминал завершает клиента tmux: чтение заканчивается.
	if err := tmux.Close("nit-test"); err != nil {
		t.Fatal(err)
	}
	out.eof(t)
	if panes, _ := tmux.List(); len(panes) != 0 {
		t.Fatalf("panes after close: %+v", panes)
	}
	if err := tmux.Close("nit-test"); err != nil {
		t.Fatalf("closing a closed terminal: %v", err)
	}
}

func TestOddFolderKeyAndCommand(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	// Точка с запятой в конце аргумента разрезала бы команду tmux.
	dir := filepath.Join(t.TempDir(), "odd;")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := tmux.Name("", tmux.NameVars{Agent: "kimi", ID: "id.with.dots"})
	pane, err := tmux.Open(tmux.OpenOptions{Name: name, Cwd: dir, Argv: []string{agent, "it's", "x;"}, Key: "kimi:x;"})
	if err != nil {
		t.Fatal(err)
	}
	if pane.Name != name || pane.Cwd != dir || pane.Key != "kimi:x;" {
		t.Fatalf("pane = %+v", pane)
	}
	tmuxtest.Screen(t, name, "fake agent: it's x;")
	if err := tmux.SetKey(name, "kimi:new"); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := tmux.Get(name); p.Key != "kimi:new" {
		t.Fatalf("key = %q", p.Key)
	}
}

func TestOpenFails(t *testing.T) {
	tmuxtest.Server(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "x", Cwd: filepath.Join(t.TempDir(), "gone"), Argv: []string{"true"}}); err == nil {
		t.Fatal("missing folder must fail")
	}
	if _, err := tmux.Open(tmux.OpenOptions{Name: "x", Cwd: t.TempDir()}); err == nil {
		t.Fatal("empty command must fail")
	}
	// Агента нет в PATH: оболочка выходит с кодом 127, окно остаётся с кодом.
	pane, err := tmux.Open(tmux.OpenOptions{Name: "x", Cwd: t.TempDir(), Argv: []string{"nit-no-such-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	tmuxtest.Wait(t, "shell exited", func() bool {
		pane, _, _ = tmux.Get("x")
		return pane.Dead
	})
	if pane.ExitCode != 127 {
		t.Fatalf("exit code = %d", pane.ExitCode)
	}
	if _, err := tmux.Attach("missing", 80, 24, tmux.AttachOptions{}); err != tmux.ErrNotFound {
		t.Fatalf("attach to missing session: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := tmux.Open(tmux.OpenOptions{Name: "y", Cwd: t.TempDir(), Argv: []string{"true"}}); err != tmux.ErrNoTmux {
		t.Fatalf("without tmux: %v", err)
	}
	if panes, err := tmux.List(); err != tmux.ErrNoTmux || panes != nil {
		t.Fatalf("List without tmux: %v %v", panes, err)
	}
}

func TestListWithoutServer(t *testing.T) {
	tmuxtest.Server(t)
	panes, err := tmux.List()
	if err != nil || len(panes) != 0 {
		t.Fatalf("List = %v, %v", panes, err)
	}
	name, err := tmux.Unique("nit-a")
	if err != nil || name != "nit-a" {
		t.Fatalf("Unique = %q, %v", name, err)
	}
}

func TestUnique(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	for _, name := range []string{"nit-a", "nit-a-2"} {
		if _, err := tmux.Open(tmux.OpenOptions{Name: name, Cwd: t.TempDir(), Argv: []string{agent}}); err != nil {
			t.Fatal(err)
		}
	}
	if name, _ := tmux.Unique("nit-a"); name != "nit-a-3" {
		t.Fatalf("Unique = %q", name)
	}
	if name, _ := tmux.Unique("nit-b"); name != "nit-b" {
		t.Fatalf("Unique = %q", name)
	}
}

func TestCaptureAndKeys(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "k", Cwd: t.TempDir(), Argv: []string{agent}, Cols: 40}); err != nil {
		t.Fatal(err)
	}
	tmuxtest.Screen(t, "k", "fake agent")
	if err := tmux.SendText("k", "one;"); err != nil {
		t.Fatal(err)
	}
	if err := tmux.SendKeys("k", "Enter"); err != nil {
		t.Fatal(err)
	}
	tmuxtest.Screen(t, "k", "got: one;")
	// Длинная строка шире окна: склеенная, а не разрезанная по 40 знаков.
	long := strings.Repeat("x", 60)
	if err := tmux.SendText("k", long); err != nil {
		t.Fatal(err)
	}
	if err := tmux.SendKeys("k", "Enter"); err != nil {
		t.Fatal(err)
	}
	screen := tmuxtest.Screen(t, "k", "got: "+long)
	if strings.HasSuffix(screen, " ") || strings.HasSuffix(screen, "\n") {
		t.Fatalf("screen is not trimmed: %q", screen)
	}
	// Многострочный текст: строки приходят агенту по одной.
	if err := tmux.SendText("k", "two\nthree\n"); err != nil {
		t.Fatal(err)
	}
	tmuxtest.Screen(t, "k", "got: three")
	if err := tmux.SendKeys("k", "C-c", "Up"); err != nil {
		t.Fatal(err)
	}
	if err := tmux.SendKeys("k", "kill-server"); err == nil {
		t.Fatal("unknown key must be refused")
	}
	if err := tmux.SendKeys("k", "Enter;"); err == nil {
		t.Fatal("unknown key must be refused")
	}
	// Ctrl+C дошёл до терминала: tmux показывает его как ^C.
	tmuxtest.Screen(t, "k", "^C")
	if _, err := tmux.Capture("missing", 0); err == nil {
		t.Fatal("capture of missing session must fail")
	}
}

func TestCaptureHistory(t *testing.T) {
	tmuxtest.Server(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "lines")
	os.WriteFile(script, []byte("#!/bin/sh\ni=1\nwhile [ $i -le 30 ]; do echo line$i; i=$((i+1)); done\nsleep 30\n"), 0o755)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "h", Cwd: dir, Argv: []string{script}, Rows: 10}); err != nil {
		t.Fatal(err)
	}
	tmuxtest.Screen(t, "h", "line30")
	screen, _ := tmux.Capture("h", 0)
	if strings.Contains(screen, "line1\n") {
		t.Fatalf("visible screen has history: %q", screen)
	}
	full, _ := tmux.Capture("h", 100)
	if !strings.Contains(full, "line1\n") || !strings.HasSuffix(full, "line30") {
		t.Fatalf("history capture: %q", full)
	}
}

func TestIgnoreSizeKeepsAgentWindow(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "w", Cwd: t.TempDir(), Argv: []string{agent}}); err != nil {
		t.Fatal(err)
	}
	wide, err := tmux.Attach("w", 150, 40, tmux.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer wide.Close()
	go io.Copy(io.Discard, wide)
	tmuxtest.Wait(t, "window follows the wide client", func() bool { return window(t, "w") == "150x40" })
	narrow, err := tmux.Attach("w", 40, 12, tmux.AttachOptions{IgnoreSize: true})
	if err != nil {
		t.Fatal(err)
	}
	defer narrow.Close()
	go io.Copy(io.Discard, narrow)
	tmuxtest.Wait(t, "two clients", func() bool { return len(strings.Fields(clients(t))) == 2 })
	narrow.Write([]byte("x"))
	time.Sleep(300 * time.Millisecond)
	if got := window(t, "w"); got != "150x40" {
		t.Fatalf("narrow ignore-size client changed the window to %s", got)
	}
}

func TestAttachCommandReachesSameServer(t *testing.T) {
	socket := tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "c", Cwd: t.TempDir(), Argv: []string{agent}}); err != nil {
		t.Fatal(err)
	}
	command := tmux.AttachCommand("c")
	if command != "tmux -S "+socket+" attach-session -t =c" {
		t.Fatalf("command = %s", command)
	}
	listed, err := exec.Command("sh", "-c", strings.Replace(command, "attach-session", "list-panes", 1)).CombinedOutput()
	if err != nil || len(strings.Split(strings.TrimSpace(string(listed)), "\n")) != 1 {
		t.Fatalf("list-panes: %v %s", err, listed)
	}
}

func TestDefaultSocket(t *testing.T) {
	// Только строка команды: tmux не вызывается, сокет ai-threads человека не трогается.
	t.Setenv(tmux.SocketEnv, "")
	if got := tmux.AttachCommand("nit-x"); got != "tmux -L ai-threads attach-session -t =nit-x" {
		t.Fatalf("command = %s", got)
	}
}

func clients(t *testing.T) string {
	return tmuxtest.Run(t, "list-clients", "-F", "#{client_width}x#{client_height}")
}

func window(t *testing.T, name string) string {
	return tmuxtest.Run(t, "display-message", "-p", "-t", "="+name+":", "#{window_width}x#{window_height}")
}

type output struct {
	ch  chan []byte
	buf bytes.Buffer
}

func reader(r io.Reader) *output {
	o := &output{ch: make(chan []byte, 64)}
	go func() {
		defer close(o.ch)
		for {
			p := make([]byte, 4096)
			n, err := r.Read(p)
			if n > 0 {
				o.ch <- p[:n]
			}
			if err != nil {
				return
			}
		}
	}()
	return o
}

func (o *output) until(t *testing.T, text string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for !strings.Contains(o.buf.String(), text) {
		select {
		case p, ok := <-o.ch:
			if !ok {
				t.Fatalf("output ended before %q: %q", text, o.buf.String())
			}
			o.buf.Write(p)
		case <-timeout:
			t.Fatalf("no %q in output: %q", text, o.buf.String())
		}
	}
}

func (o *output) eof(t *testing.T) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-o.ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("output did not end")
		}
	}
}
