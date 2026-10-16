package agents

import (
	"os"
	"testing"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
)

// TestMain прячет настоящие папки агентов: домашняя папка — пустая временная, переменные
// окружения с папками агентов очищены.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "agents-home-")
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
