package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

func liveHomes(t *testing.T) (claude, grok string, list []Agent) {
	t.Helper()
	root := t.TempDir()
	claude, grok = filepath.Join(root, "claude"), filepath.Join(root, "grok")
	if err := os.MkdirAll(filepath.Join(claude, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(grok, 0o755); err != nil {
		t.Fatal(err)
	}
	list = All(map[string]Config{"claude": {Home: claude}, "grok": {Home: grok},
		"codex": {Home: filepath.Join(root, "codex")}, "kimi": {Home: filepath.Join(root, "kimi")}})
	return claude, grok, list
}

func selfStart(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", os.Getpid()))
	if err != nil {
		t.Skip("нет /proc")
	}
	start, ok := startTime(string(data))
	if !ok {
		t.Fatal("нет поля starttime")
	}
	return start
}

func deadPID(t *testing.T) int {
	t.Helper()
	for pid := 2_100_000_000; pid > 1; pid-- {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return pid
		}
	}
	t.Fatal("не нашёлся свободный pid")
	return 0
}

type claudeRecord struct {
	pid               int
	sid, status, kind string
	name              string
	proc              any
	since             float64
	file              string
}

func writeClaude(t *testing.T, home string, r claudeRecord) {
	t.Helper()
	if r.kind == "" {
		r.kind = "interactive"
	}
	record := map[string]any{"pid": r.pid, "sessionId": r.sid, "cwd": "/tmp/proj", "kind": r.kind, "entrypoint": "cli",
		"status": r.status, "updatedAt": r.since, "statusUpdatedAt": r.since, "name": r.name}
	if r.proc != nil {
		record["procStart"] = r.proc
	}
	if r.file == "" {
		r.file = fmt.Sprintf("%d-%s.json", r.pid, r.sid)
	}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(home, "sessions", r.file), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func known(keys ...string) func(string) bool {
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return func(k string) bool { return set[k] }
}

func TestStartTimeIgnoresSpacesAndParenthesesInComm(t *testing.T) {
	text := "12 (we)ird proc) S"
	for n := 1; n <= 20; n++ {
		text += fmt.Sprintf(" %d", n)
	}
	// После имени идут поля 3 и дальше; поле 22 — двадцатое из них, здесь «19».
	if got, ok := startTime(text); !ok || got != "19" {
		t.Fatalf("starttime = %q", got)
	}
}

func TestClaudeStatusesMapAndWhere(t *testing.T) {
	claude, _, list := liveHomes(t)
	pid, start := os.Getpid(), selfStart(t)
	since := 1_790_609_915_299.0
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "idle-1", status: "idle", name: "имя", proc: start, since: since})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "wait-1", status: "waiting", kind: "bg", proc: start, since: since})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "busy-1", status: "busy", name: "  ", proc: start, since: since})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "shell-1", status: "shell", kind: "bg", name: "фон", proc: start, since: since})
	found := LiveAll(list, known("claude:idle-1", "claude:wait-1", "claude:busy-1", "claude:shell-1"))
	want := map[string]LiveStatus{
		"claude:idle-1":  {Key: "claude:idle-1", State: model.StateWaiting, Where: "terminal", Name: "имя"},
		"claude:wait-1":  {Key: "claude:wait-1", State: model.StateWaiting, Where: "background"},
		"claude:busy-1":  {Key: "claude:busy-1", State: model.StateWorking, Where: "terminal"},
		"claude:shell-1": {Key: "claude:shell-1", State: model.StateWorking, Where: "background", Name: "фон"},
	}
	for k, w := range want {
		got := found[k]
		if !got.Since.Equal(time.UnixMilli(int64(since))) {
			t.Errorf("%s: since = %v", k, got.Since)
		}
		got.Since = time.Time{}
		if got != w {
			t.Errorf("%s: %+v, want %+v", k, got, w)
		}
	}
}

func TestLiveSecondsAreNotDivided(t *testing.T) {
	claude, _, list := liveHomes(t)
	writeClaude(t, claude, claudeRecord{pid: os.Getpid(), sid: "s", status: "idle", proc: selfStart(t), since: 1_700_000_000})
	if got := LiveAll(list, known("claude:s"))["claude:s"].Since.Unix(); got != 1_700_000_000 {
		t.Fatalf("since = %d", got)
	}
}

func TestDeadPIDProcStartMismatchAndUnknownAreAbsent(t *testing.T) {
	claude, _, list := liveHomes(t)
	pid, start := os.Getpid(), selfStart(t)
	writeClaude(t, claude, claudeRecord{pid: deadPID(t), sid: "dead", status: "busy", proc: "1", since: 1_700_000_000_000})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "reused", status: "busy", proc: "not-this-process", since: 1_700_000_000_000})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "unknown", status: "thinking", proc: start, since: 1_700_000_000_000})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "nokind", status: "idle", kind: "other", proc: start, since: 1_700_000_000_000})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "alive", status: "shell", proc: start, since: 1_700_000_000_000})
	// procStart можно не передавать: тогда достаточно живого pid.
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "nostart", status: "idle", since: 1_700_000_001_000})
	writeClaude(t, claude, claudeRecord{pid: 0, sid: "zero", status: "idle", since: 1_700_000_001_000})
	found := LiveAll(list, known("claude:dead", "claude:reused", "claude:unknown", "claude:nokind", "claude:alive",
		"claude:nostart", "claude:zero"))
	if len(found) != 2 || found["claude:alive"].State != model.StateWorking || found["claude:nostart"].Key == "" {
		t.Fatalf("found = %v", found)
	}
}

func TestBrokenJSONAndKeyFilesDoNotHideTheRest(t *testing.T) {
	claude, _, list := liveHomes(t)
	pid, start := os.Getpid(), selfStart(t)
	write(t, filepath.Join(claude, "sessions", "broken.json"), "{")
	write(t, filepath.Join(claude, "sessions", "not-object.json"), "[]")
	secret, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": "from-key", "kind": "interactive", "status": "busy",
		"statusUpdatedAt": 1_700_000_000_000, "procStart": start})
	write(t, filepath.Join(claude, "sessions", fmt.Sprintf("%d.key", pid)), string(secret))
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "ok", status: "idle", proc: start, since: 1_700_000_000_000})
	found := LiveAll(list, known("claude:ok", "claude:from-key"))
	if len(found) != 1 || found["claude:ok"].Key == "" {
		t.Fatalf("found = %v", found)
	}
}

func TestSameSessionKeepsTheNewerRecord(t *testing.T) {
	claude, _, list := liveHomes(t)
	pid, start := os.Getpid(), selfStart(t)
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "same", status: "busy", name: "новее", proc: start, since: 1_800_000_000_000, file: "a.json"})
	writeClaude(t, claude, claudeRecord{pid: pid, sid: "same", status: "idle", proc: start, since: 1_700_000_000_000, file: "b.json"})
	got := LiveAll(list, known("claude:same"))["claude:same"]
	if got.State != model.StateWorking || got.Since.Unix() != 1_800_000_000 || got.Name != "новее" {
		t.Fatalf("got = %+v", got)
	}
}

func TestSessionMissingFromCatalogIsNotReported(t *testing.T) {
	claude, _, list := liveHomes(t)
	writeClaude(t, claude, claudeRecord{pid: os.Getpid(), sid: "orphan", status: "waiting", proc: selfStart(t), since: 1_700_000_000_000})
	if found := LiveAll(list, known()); len(found) != 0 {
		t.Fatalf("found = %v", found)
	}
	if found := LiveAll(list, known("claude:other")); len(found) != 0 {
		t.Fatalf("found = %v", found)
	}
}

func TestGrokRegistryAliveDeadAndBroken(t *testing.T) {
	_, grok, list := liveHomes(t)
	pid := os.Getpid()
	opened := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	records, _ := json.Marshal([]any{
		map[string]any{"session_id": "alive", "pid": pid, "cwd": "/tmp/g", "opened_at": 1_700_000_000},
		map[string]any{"sessionId": "camel", "pid": pid, "cwd": "/tmp/g", "openedAt": opened.Format(time.RFC3339)},
		map[string]any{"session_id": "dead", "pid": deadPID(t), "cwd": "/tmp/g", "opened_at": 1_700_000_000},
		map[string]any{"session_id": "orphan", "pid": pid, "cwd": "/tmp/g", "opened_at": 1_700_000_000_000},
		"мусор",
	})
	write(t, filepath.Join(grok, "active_sessions.json"), string(records))
	found := LiveAll(list, known("grok:alive", "grok:camel", "grok:dead", "claude:alive"))
	if got := found["grok:alive"]; got.State != model.StateWorking || !got.Guess || got.Where != "terminal" || got.Since.Unix() != 1_700_000_000 {
		t.Errorf("alive = %+v", got)
	}
	if got := found["grok:camel"]; got.State != model.StateWorking || !got.Since.Equal(opened) {
		t.Errorf("camel = %+v", got)
	}
	if len(found) != 2 {
		t.Errorf("found = %v", found)
	}
	write(t, filepath.Join(grok, "active_sessions.json"), "{")
	if found := LiveAll(list, known("grok:alive")); len(found) != 0 {
		t.Errorf("broken: %v", found)
	}
	write(t, filepath.Join(grok, "active_sessions.json"), "[]")
	if found := LiveAll(list, known("grok:alive")); len(found) != 0 {
		t.Errorf("empty: %v", found)
	}
}

func TestCodexAndKimiHaveNoStatus(t *testing.T) {
	_, _, list := liveHomes(t)
	if found := LiveAll(list, known("codex:1", "kimi:session_1")); len(found) != 0 {
		t.Fatalf("found = %v", found)
	}
}

func TestMissingRegistriesReturnEmpty(t *testing.T) {
	root := t.TempDir()
	list := All(map[string]Config{"claude": {Home: filepath.Join(root, "no-claude")}, "grok": {Home: filepath.Join(root, "no-grok")}})
	if found := LiveAll(list, known("claude:x")); len(found) != 0 {
		t.Fatalf("found = %v", found)
	}
}
