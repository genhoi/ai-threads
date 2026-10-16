// Package tmuxtest — свой сервер tmux и заглушки агентов для тестов.
//
// Тесты никогда не трогают сокет ai-threads: там работают настоящие агенты человека.
// Main ставит отдельный сокет на весь прогон пакета, Server — на один тест.
package tmuxtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/tmux"
)

// Main запускает тесты пакета со своим сервером tmux, даже если тест забыл вызвать Server.
// Агент запускается через /bin/sh, а не через оболочку и ~/.bashrc человека.
func Main(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "nit-tmux-")
	if err != nil {
		panic(err)
	}
	socket := filepath.Join(dir, "s")
	os.Setenv(tmux.SocketEnv, socket)
	os.Setenv("SHELL", "/bin/sh")
	code := m.Run()
	kill(socket)
	os.RemoveAll(dir)
	os.Exit(code)
}

// Server — свой сервер tmux на время теста. Без tmux тест пропускается.
func Server(t testing.TB) string {
	t.Helper()
	if !tmux.Available() {
		t.Skip("tmux is not installed")
	}
	// Временная папка теста заводится раньше: её уборка идёт после остановки сервера.
	t.TempDir()
	// Путь к сокету ограничен 108 символами, поэтому папка — прямо в /tmp.
	dir, err := os.MkdirTemp("/tmp", "nit-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s")
	t.Setenv(tmux.SocketEnv, socket)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() {
		kill(socket)
		os.RemoveAll(dir)
	})
	return socket
}

func kill(socket string) {
	if path, err := exec.LookPath("tmux"); err == nil {
		_ = exec.Command(path, "-S", socket, "kill-server").Run()
	}
}

// AgentTTY — заглушка агента в терминале, как tests/stubs/agent-tty первой версии: печатает
// свои аргументы и отвечает на каждую строку. «quit» завершает её с кодом 4, «exit0» — с кодом 0.
func AgentTTY(t testing.TB) string {
	return script(t, "agent-tty", `#!/bin/sh
echo "fake agent: $*"
while read -r line; do
  [ "$line" = "quit" ] && exit 4
  [ "$line" = "exit0" ] && exit 0
  echo "got: $line"
done
`)
}

// Worker — заглушка, которая работает, а потом ждёт ответа: печатает «step N» раз в 0,1 с
// столько раз, сколько задано первым аргументом, затем «Done. What next?» и молчит до ввода.
// «go» — ещё один такой ход, «quit» — выход с кодом 5, «exit0» — с кодом 0.
func Worker(t testing.TB) string {
	return script(t, "agent-worker", `#!/bin/sh
steps=${1:-5}
work() {
  i=1
  while [ $i -le $steps ]; do echo "step $i"; i=$((i+1)); sleep 0.1; done
  echo "Done. What next?"
}
work
while read -r line; do
  [ "$line" = "quit" ] && exit 5
  [ "$line" = "exit0" ] && exit 0
  [ "$line" = "go" ] && work
done
`)
}

func script(t testing.TB, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Wait ждёт, пока cond не вернёт true; иначе тест падает с сообщением what.
func Wait(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", what)
}

// Screen ждёт, пока на экране сессии не появится текст.
func Screen(t testing.TB, name, text string) string {
	t.Helper()
	var screen string
	Wait(t, "screen of "+name+" shows "+text, func() bool {
		screen, _ = tmux.Capture(name, 0)
		return strings.Contains(screen, text)
	})
	return screen
}

// Run выполняет команду tmux на сервере теста и возвращает её вывод.
func Run(t testing.TB, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", append([]string{"-S", os.Getenv(tmux.SocketEnv)}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
