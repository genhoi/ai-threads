package summary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
)

// TestMain прячет окружение человека: настоящие агенты не должны найтись ни в PATH,
// ни в домашней папке.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "summary-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("PATH", "/usr/bin:/bin")
	for _, name := range []string{"CODEX_HOME", "CLAUDE_HOME", "GROK_HOME", "KIMI_CODE_HOME", "ZCODE_HOME", "CURSOR_HOME",
		"AI_THREADS_SUMMARY_TIMEOUT", "AI_THREADS_DIGEST_TIMEOUT"} {
		os.Unsetenv(name)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// Поведение заглушки.
const (
	modeOK      = "ok"      // читает два раза и отвечает
	modeGarbage = "garbage" // отвечает не JSON
	modeFail    = "fail"    // пишет в stderr и выходит с кодом 2
	modeHang    = "hang"    // читает один раз и висит, пока её не убьют
)

// stub — заглушка CLI агента id. Она записывает в rec свои аргументы, stdin, папку запуска
// и файл со списком сессий, затем печатает поток в формате агента с ответом answer.
// Codex, как настоящий, кладёт ответ в файл -o, а в поток пишет другой текст.
type stub struct {
	program string
	rec     string
}

func newStub(t *testing.T, id, mode, answer string) stub {
	t.Helper()
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	if err := os.Mkdir(rec, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\nrec='" + rec + "'\n")
	b.WriteString(`printf '%s\n' "$@" > "$rec/argv"
cat > "$rec/stdin"
pwd > "$rec/cwd"
if [ -f digest-input.txt ]; then cp digest-input.txt "$rec/"; fi
out=
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out=$2; fi
  shift
done
`)
	rows := streamRows(id, answer, 2)
	switch mode {
	case modeOK, modeGarbage:
		if mode == modeGarbage {
			answer = "не удалось разобрать журнал, попробуйте позже"
			rows = streamRows(id, answer, 2)
		}
		b.WriteString("cat <<'NIT_STUB'\n" + strings.Join(rows, "\n") + "\nNIT_STUB\n")
		if id == "codex" {
			b.WriteString("[ -n \"$out\" ] && cat > \"$out\" <<'NIT_STUB'\n" + answer + "\nNIT_STUB\n")
		}
	case modeFail:
		b.WriteString("echo 'boom: тестовая ошибка заглушки' >&2\nexit 2\n")
	case modeHang:
		b.WriteString("cat <<'NIT_STUB'\n" + strings.Join(streamRows(id, "", 1), "\n") + "\nNIT_STUB\nexec sleep 30\n")
	}
	program := filepath.Join(dir, id)
	if err := os.WriteFile(program, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub{program: program, rec: rec}
}

// read — что заглушка записала о запуске: argv, stdin, cwd или digest-input.txt.
func (s stub) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.rec, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// streamRows — строки потока агента: reads вызовов чтения и ответ answer (пустой — без ответа).
func streamRows(id, answer string, reads int) []string {
	var rows []any
	add := func(v any) { rows = append(rows, v) }
	type m = map[string]any
	switch id {
	case "kimi":
		for i := range reads {
			add(m{"role": "assistant", "tool_calls": []any{m{"id": "tool", "type": "function",
				"function": m{"name": "Read", "arguments": `{"path":"/x","offset":` + string(rune('0'+i)) + `}`}}}})
			add(m{"role": "tool", "tool_call_id": "tool", "content": "строка журнала"})
		}
		if answer != "" {
			add(m{"role": "assistant", "content": answer})
			add(m{"role": "meta", "type": "session.resume_hint", "session_id": "session_stub"})
		}
	case "claude", "grok":
		add(m{"type": "system", "subtype": "init", "session_id": "stub", "model": "stub-model"})
		for range reads {
			add(m{"type": "assistant", "message": m{"role": "assistant", "content": []any{
				m{"type": "tool_use", "id": "t", "name": "Read", "input": m{"file_path": "/x"}}}}})
			add(m{"type": "user", "message": m{"role": "user", "content": []any{
				m{"type": "tool_result", "tool_use_id": "t", "content": "строка журнала"}}}})
		}
		if answer != "" {
			add(m{"type": "assistant", "message": m{"role": "assistant", "content": []any{m{"type": "text", "text": answer}}}})
			add(m{"type": "result", "subtype": "success", "result": answer, "usage": m{"input_tokens": 900, "output_tokens": 120}})
		}
	case "codex":
		add(m{"type": "thread.started", "thread_id": "stub-thread"})
		for i := range reads {
			id := "item_" + string(rune('0'+i))
			add(m{"type": "item.started", "item": m{"id": id, "type": "command_execution", "command": "cat /x"}})
			add(m{"type": "item.completed", "item": m{"id": id, "type": "command_execution", "command": "cat /x",
				"aggregated_output": "строка журнала", "exit_code": 0}})
		}
		if answer != "" {
			add(m{"type": "item.completed", "item": m{"id": "final", "type": "agent_message", "text": "Ответ в файле."}})
			add(m{"type": "turn.completed", "usage": m{"input_tokens": 800, "output_tokens": 90}})
		}
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		out = append(out, string(raw))
	}
	return out
}

// runner — агент id, у которого программа — заглушка.
func runner(t *testing.T, id, program string) agents.Agent {
	t.Helper()
	return agents.New(id, agents.Config{Program: program, Home: t.TempDir()})
}

// newRun — запуск заглушки: рабочие папки во временной папке теста.
func newRun(t *testing.T, id, program string) Run {
	return Run{Runner: runner(t, id, program), Timeout: 20 * time.Second, Lang: "ru", Dir: filepath.Join(t.TempDir(), "runs")}
}

// claudeSession пишет журнал Claude Code с перепиской и возвращает сессию.
func claudeSession(t *testing.T, id, cwd string, updated time.Time, messages ...[2]string) model.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".jsonl")
	var b strings.Builder
	at := updated.UTC().Format(time.RFC3339)
	for _, msg := range messages {
		var row map[string]any
		if msg[0] == "user" {
			row = map[string]any{"type": "user", "timestamp": at, "cwd": cwd, "message": map[string]any{"role": "user", "content": msg[1]}}
		} else {
			row = map[string]any{"type": "assistant", "timestamp": at, "cwd": cwd,
				"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": msg[1]}}}}
		}
		raw, _ := json.Marshal(row)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return model.Session{Agent: "claude", ID: id, Title: "Сессия " + id, Cwd: cwd, Updated: updated, Journal: path}
}

// recorder — события заданий по порядку.
type recorder struct {
	mu   sync.Mutex
	list []Status
}

func (r *recorder) add(st Status) {
	r.mu.Lock()
	r.list = append(r.list, st)
	r.mu.Unlock()
}

func (r *recorder) all() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Status(nil), r.list...)
}

// last — последнее событие задания id.
func (r *recorder) last(id string) Status {
	var st Status
	for _, s := range r.all() {
		if s.ID == id {
			st = s
		}
	}
	return st
}

// waitFor ждёт условие до 10 секунд.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", what)
}
