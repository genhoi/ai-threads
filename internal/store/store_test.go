package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

func key(agent string, n int) string {
	prefix := ""
	if agent == "kimi" {
		prefix = "session_"
	}
	return fmt.Sprintf("%s:%s00000000-0000-4000-8000-%012d", agent, prefix, n)
}

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) *Store {
	return open(t, filepath.Join(t.TempDir(), "state.json"))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(raw, &m))
	return m
}

// Файл, записанный первой версией, читается целиком и после перезаписи сохраняет всё, что в нём было.
func TestV1FileRoundTrip(t *testing.T) {
	src, err := os.ReadFile("testdata/v1-state.json")
	must(t, err)
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, src, 0o600))
	s := open(t, path)

	claude, codex := "claude:77be55f8-a3d7-41bd-b056-f1ee1b56e18c", "codex:01a0a3a7-267b-7570-9088-3fee4642ecb2"
	kimi, grok := "kimi:session_00000000-0000-4000-8000-000000000001", "grok:00000000-0000-4000-8000-000000000002"
	st := s.Snapshot()
	if !slices.Equal(st.Pins, []string{claude, codex, kimi}) || !slices.Equal(st.Done, []string{codex}) ||
		!slices.Equal(st.Hidden, []string{grok}) || !st.MigratedLocalStorage || !st.DigestTails["t1"] {
		t.Fatalf("state = %+v", st)
	}
	m := st.Marks(claude)
	if m.Name != "BACKEND-10300. Партиции message_profile" || !m.Pinned || m.Summary == nil ||
		m.Summary.At != 1791358090.6282146 || m.Summary.Related[0].Key != codex || m.Summary.NextStep != "Влить MR" {
		t.Fatalf("marks = %+v %+v", m, m.Summary)
	}
	digest, err := s.Digest(3)
	must(t, err)
	if !bytes.Contains(digest, []byte(`"lead": "Итоги <b> & «кавычки»"`)) {
		t.Fatalf("digest = %s", digest)
	}

	// Новая пометка не трогает старые поля: файл, кроме неё, совпадает с исходным.
	must(t, s.MarkSeen(claude, time.Unix(1791400000, 0)))
	var before, after map[string]any
	must(t, json.Unmarshal(src, &before))
	after = readJSON(t, path)
	for name, value := range before {
		if !reflect.DeepEqual(after[name], value) {
			t.Errorf("%s: %v != %v", name, after[name], value)
		}
	}
	if seen := after["seen"].(map[string]any); seen[claude] != float64(1791400000) {
		t.Fatalf("seen = %v", seen)
	}
	if !reflect.DeepEqual(open(t, path).Snapshot(), s.Snapshot()) {
		t.Fatal("перечитанный файл отличается от памяти")
	}
}

func TestUnknownFieldsSurviveRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, []byte(`{"version": 2, "pins": [], "future": {"a": [1, 2]}, "flag": true}`), 0o600))
	s := open(t, path)
	must(t, s.SetPinned(key("codex", 1), true))
	m := readJSON(t, path)
	if !reflect.DeepEqual(m["future"], map[string]any{"a": []any{1.0, 2.0}}) || m["flag"] != true {
		t.Fatalf("file = %v", m)
	}
	if export, _ := s.Export(); bytes.Contains(export, []byte("future")) {
		t.Fatal("чужие поля попали в выгрузку")
	}
}

// Как в первой версии: у данных версии 1 берутся только закрепления и названия.
func TestVersion1File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := fmt.Sprintf(`{"version": 1, "pins": [%q], "names": {%q: " Имя "}, "done": [%q]}`,
		key("codex", 1), key("claude", 2), key("codex", 1))
	must(t, os.WriteFile(path, []byte(data), 0o600))
	st := open(t, path).Snapshot()
	if !slices.Equal(st.Pins, []string{key("codex", 1)}) || st.Names[key("claude", 2)] != "Имя" || len(st.Done) != 0 {
		t.Fatalf("state = %+v", st)
	}
}

func TestFailedWriteKeepsDiskAndMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := open(t, path)
	must(t, s.SetPinned(key("codex", 1), true))
	original, err := os.ReadFile(path)
	must(t, err)
	changes := 0
	s.OnChange(func(Change) { changes++ })

	// Переименование не удаётся: на месте файла папка с файлом внутри.
	must(t, os.Rename(path, path+".bak"))
	must(t, os.MkdirAll(filepath.Join(path, "x"), 0o755))
	if err := s.SetName(key("codex", 1), "Название"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if s.Snapshot().Names[key("codex", 1)] != "" || changes != 0 {
		t.Fatal("память поменялась при сбое записи")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".state-*")); len(left) != 0 {
		t.Fatalf("остались временные файлы: %v", left)
	}
	must(t, os.RemoveAll(path))
	must(t, os.Rename(path+".bak", path))
	if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
		t.Fatal("файл поменялся")
	}

	// Повтор после сбоя записывает всё.
	must(t, s.SetName(key("codex", 1), "Название"))
	st := open(t, path).Snapshot()
	if !slices.Equal(st.Pins, []string{key("codex", 1)}) || st.Names[key("codex", 1)] != "Название" || changes != 1 {
		t.Fatalf("state = %+v, changes = %d", st, changes)
	}
}

func TestSetters(t *testing.T) {
	s := newStore(t)
	var got []Change
	s.OnChange(func(c Change) { got = append(got, c) })
	k := key("grok", 1)
	must(t, s.SetPinned(k, true))
	must(t, s.SetPinned(k, true)) // без изменений — без записи и без события
	must(t, s.SetDone(k, true))
	must(t, s.SetHidden(k, true))
	must(t, s.SetName(k, "  Каталог  "))
	must(t, s.MarkSeen(k, time.Unix(100, 0)))
	must(t, s.SetSummary(k, Summary{Title: " Сводка ", Summary: "Текст", At: 1.5, Model: "kimi"}))
	must(t, s.SetWorktree(k, model.Worktree{Path: "/home/u/.nit/worktrees/backend/B-1/", Branch: "B-1", Repo: "/home/u/backend"}))
	must(t, s.RecordTerminal(k, Terminal{Tmux: "nit-backend-B-1", Cwd: "/home/u/backend", Started: 50}))
	must(t, s.SetProjectPinned("/home/u/backend/", true))
	must(t, s.SetProjectAgent("/home/u/backend", "claude"))
	must(t, s.SetDigest(4, json.RawMessage(`{"at": 1, "result": {}}`)))
	must(t, s.SetTail("t1", true))

	m := open(t, s.Path()).Marks(k)
	want := Marks{Name: "Каталог", Pinned: true, Done: true, Hidden: true, Seen: 100,
		Summary:  &Summary{Title: "Сводка", Summary: "Текст", Related: []Related{}, At: 1.5, Model: "kimi"},
		Worktree: &model.Worktree{Path: "/home/u/.nit/worktrees/backend/B-1", Branch: "B-1", Repo: "/home/u/backend"},
		Terminal: &Terminal{Tmux: "nit-backend-B-1", Cwd: "/home/u/backend", Started: 50, Agent: "grok"}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("marks = %+v\nwant  %+v", m, want)
	}
	if p := s.Snapshot().Projects["/home/u/backend"]; p != (Project{Pinned: true, DefaultAgent: "claude"}) {
		t.Fatalf("project = %+v", p)
	}
	if len(got) != 12 || got[0] != (Change{KindSession, k}) || got[7] != (Change{KindTerminal, k}) ||
		got[8] != (Change{KindProject, "/home/u/backend"}) || got[10] != (Change{KindDigest, "4"}) {
		t.Fatalf("changes = %v", got)
	}

	// Снятие пометок убирает записи из файла.
	must(t, s.SetName(k, ""))
	must(t, s.SetPinned(k, false))
	must(t, s.ForgetWorktree(k))
	must(t, s.ForgetTerminal(k))
	must(t, s.SetProjectPinned("/home/u/backend", false))
	must(t, s.SetProjectAgent("/home/u/backend", ""))
	st := open(t, s.Path()).Snapshot()
	if len(st.Names)+len(st.Pins)+len(st.Worktrees)+len(st.Terminals)+len(st.Projects) != 0 {
		t.Fatalf("state = %+v", st)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	s := newStore(t)
	k := key("codex", 1)
	must(t, s.SetPinned(k, true))
	must(t, s.SetSummary(k, Summary{Title: "T", Summary: "S", Related: []Related{{Key: k, Why: "тот же"}}}))
	st := s.Snapshot()
	st.Pins[0] = "x"
	st.Summaries[k].Related[0].Why = "изменено"
	st.Names["y"] = "z"
	m := s.Marks(k)
	m.Summary.Related[0].Why = "изменено"
	if again := s.Snapshot(); again.Pins[0] != k || again.Summaries[k].Related[0].Why != "тот же" || len(again.Names) != 0 {
		t.Fatalf("снимок делит память с хранилищем: %+v", again)
	}
}

// Как /api/import первой версии: версии 1 и 2, списки объединяются, названия из файла важнее.
func TestImportMergesVersions(t *testing.T) {
	s := newStore(t)
	must(t, s.SetPinned(key("codex", 1), true))
	must(t, s.SetName(key("codex", 1), "Раньше"))
	must(t, s.MarkSeen(key("codex", 1), time.Unix(500, 0)))
	must(t, s.Import([]byte(fmt.Sprintf(`{"version": 1, "pins": [%q], "names": {%q: "Теперь"}, "done": [%q]}`,
		key("claude", 1), key("codex", 1), key("claude", 1)))))
	sum := fmt.Sprintf(`{"title": "Сводка", "summary": "Проверка завершена", "related": [{"key": %q, "why": "Одна тема"}]}`, key("claude", 1))
	must(t, s.Import([]byte(fmt.Sprintf(`{"version": 2, "pins": [%q], "names": {%q: "Каталог"}, "done": [%q],
		"hidden": [%q], "summaries": {%q: %s}, "digest_tails": {"tail-1": true}, "settings": {"language": "en"},
		"seen": {%q: 300, %q: 700}, "projects": {"/home/u/p": {"pinned": true}},
		"worktrees": {%q: {"path": "/w", "branch": "b", "repo": "/r"}}, "terminals": {%q: {"tmux": "x"}}}`,
		key("codex", 1), key("grok", 1), key("codex", 1), key("grok", 1), key("codex", 1), sum,
		key("codex", 1), key("kimi", 1), key("codex", 1), key("codex", 1)))))

	st := s.Snapshot()
	if !slices.Equal(st.Pins, []string{key("codex", 1), key("claude", 1)}) ||
		st.Names[key("codex", 1)] != "Теперь" || st.Names[key("grok", 1)] != "Каталог" ||
		!slices.Equal(st.Done, []string{key("codex", 1)}) || !slices.Equal(st.Hidden, []string{key("grok", 1)}) ||
		st.Summaries[key("codex", 1)].Related[0].Why != "Одна тема" || !st.DigestTails["tail-1"] ||
		st.Seen[key("codex", 1)] != 500 || st.Seen[key("kimi", 1)] != 700 || !st.Projects["/home/u/p"].Pinned ||
		st.Worktrees[key("codex", 1)].Branch != "b" || len(st.Terminals) != 0 {
		t.Fatalf("state = %+v", st)
	}

	must(t, s.Migrate([]string{key("kimi", 1)}, map[string]string{key("codex", 1): "Перенос"}))
	st = s.Snapshot()
	if !st.MigratedLocalStorage || st.Names[key("codex", 1)] != "Перенос" || !slices.Contains(st.Pins, key("kimi", 1)) {
		t.Fatalf("state = %+v", st)
	}
	must(t, s.Migrate(nil, nil))
}

func TestExportImportRoundTrip(t *testing.T) {
	s := newStore(t)
	k := key("cursor", 1)
	must(t, s.SetPinned(k, true))
	must(t, s.SetSummary(k, Summary{Title: "T", Summary: "S"}))
	must(t, s.MarkSeen(k, time.Unix(10, 0)))
	must(t, s.SetProjectHidden("/p", true))
	must(t, s.SetWorktree(k, model.Worktree{Path: "/w", Branch: "b"}))
	must(t, s.RecordTerminal(k, Terminal{Tmux: "t"}))
	must(t, s.SetDigest(5, json.RawMessage(`{"x": 1}`)))
	raw, err := s.Export()
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(raw, &m))
	if m["version"] != 2.0 || m["digests"] != nil || m["terminals"] != nil {
		t.Fatalf("export = %s", raw)
	}
	other := newStore(t)
	must(t, other.Import(raw))
	want := s.Snapshot()
	want.Terminals = map[string]Terminal{}
	if got := other.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestInvalidKeysAreRejected(t *testing.T) {
	for _, bad := range []string{"../file", "codex:../../file", "unknown:id", "codex:", "codex:a\n", "codex:другой", "", "codex"} {
		s := newStore(t)
		for _, set := range []func(string, bool) error{s.SetPinned, s.SetDone, s.SetHidden} {
			if err := set(bad, true); err == nil {
				t.Errorf("%q принят", bad)
			}
		}
		for _, err := range []error{s.SetName(bad, "x"), s.MarkSeen(bad, time.Now()), s.ForgetTerminal(bad),
			s.RecordTerminal(bad, Terminal{Tmux: "t"}), s.SetWorktree(bad, model.Worktree{Path: "/w"})} {
			var invalid *InvalidError
			if !errors.As(err, &invalid) || invalid.Code != "key" {
				t.Errorf("%q: %v", bad, err)
			}
		}
		raw, _ := json.Marshal(map[string]any{"version": 2, "pins": []string{key("codex", 1), bad}})
		if err := s.Import(raw); err == nil {
			t.Errorf("%q принят при загрузке", bad)
		}
		if _, err := os.Stat(s.Path()); !os.IsNotExist(err) || len(s.Snapshot().Pins) != 0 {
			t.Errorf("%q: файл создан или пометка сохранена", bad)
		}
	}
}

func TestInvalidImportChangesNothing(t *testing.T) {
	k := key("codex", 1)
	for _, data := range []string{
		`[]`, `{broken`, `{"version": 3}`, `{"version": true}`, `{"version": 2.0}`, `{}`,
		`{"version": 2, "names": {"` + k + `": ""}}`,
		`{"version": 2, "names": {"` + k + `": " "}}`,
		`{"version": 2, "names": {"` + k + `": "` + string(bytes.Repeat([]byte("x"), 201)) + `"}}`,
		`{"version": 2, "names": {"grok:../bad": "Название"}}`,
		`{"version": 2, "done": "codex:id"}`,
		`{"version": 2, "hidden": ["kimi:bad/id"]}`,
		`{"version": 2, "pins": null}`,
		`{"version": 2, "summaries": {"` + k + `": {"title": "Сводка", "summary": "Текст", "related": [{"key": "grok:../bad", "why": "Тема"}]}}}`,
		`{"version": 2, "summaries": {"` + k + `": {"title": "Сводка", "summary": " "}}}`,
		`{"version": 2, "summaries": {"` + k + `": {"title": "Сводка", "summary": "Текст", "closed": "да"}}}`,
		`{"version": 2, "digest_tails": {" ": true}}`,
		`{"version": 2, "seen": {"` + k + `": -1}}`,
		`{"version": 2, "projects": {"relative/path": {}}}`,
		`{"version": 2, "projects": {"/p": {"default_agent": "vim"}}}`,
		`{"version": 2, "worktrees": {"` + k + `": {"path": ""}}}`,
	} {
		s := newStore(t)
		must(t, s.SetPinned(k, true))
		original, _ := os.ReadFile(s.Path())
		var invalid *InvalidError
		if err := s.Import([]byte(data)); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", data, err)
		}
		if got, _ := os.ReadFile(s.Path()); !bytes.Equal(got, original) {
			t.Errorf("%s: файл поменялся", data)
		}
	}
}

func TestInvalidValues(t *testing.T) {
	s := newStore(t)
	k := key("claude", 1)
	for name, err := range map[string]error{
		"name":       s.SetName(k, " "),
		"long name":  s.SetName(k, string(bytes.Repeat([]byte("я"), 201))),
		"summary":    s.SetSummary(k, Summary{Title: "T"}),
		"days":       s.SetDigest(6, json.RawMessage(`{}`)),
		"digest":     s.SetDigest(3, json.RawMessage(`[]`)),
		"tail":       s.SetTail(" ", true),
		"project":    s.SetProjectPinned("rel", true),
		"agent":      s.SetProjectAgent("/p", "vim"),
		"worktree":   s.SetWorktree(k, model.Worktree{Path: "w"}),
		"terminal":   s.RecordTerminal(k, Terminal{}),
		"term agent": s.RecordTerminal(k, Terminal{Tmux: "t", Agent: "vim"}),
	} {
		var invalid *InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("неверные значения записаны")
	}
	if d, err := s.Digest(3); d != nil || err != nil {
		t.Fatalf("digest = %s, %v", d, err)
	}
}

func TestConcurrentWritesAreNotLost(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	for n := range 30 {
		wg.Go(func() {
			if err := s.SetPinned(key("codex", n), true); err != nil {
				t.Error(err)
			}
			if err := s.MarkSeen(key("codex", n), time.Unix(int64(n+1), 0)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	st := open(t, s.Path()).Snapshot()
	if len(st.Pins) != 30 || len(st.Seen) != 30 {
		t.Fatalf("pins = %d, seen = %d", len(st.Pins), len(st.Seen))
	}
}

func TestCorruptFileIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for _, data := range []string{"{broken", `{"version": 2, "pins": ["bad"]}`} {
		must(t, os.WriteFile(path, []byte(data), 0o600))
		if _, err := Open(path); err == nil {
			t.Fatalf("%s: открылся", data)
		}
		if got, _ := os.ReadFile(path); string(got) != data {
			t.Fatal("файл поменялся")
		}
	}
}

func TestErrorText(t *testing.T) {
	err := &InvalidError{Field: "projects", Code: "path", Value: "rel"}
	if err.Text("en") != "Must be an absolute path: rel" || err.Error() != "projects: Нужен абсолютный путь: rel" {
		t.Fatal(err.Text("en"), err.Error())
	}
}

func TestDataDir(t *testing.T) {
	t.Setenv("AI_THREADS_DATA", "")
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if DataDir() != "/xdg/ai-threads" {
		t.Fatal(DataDir())
	}
	t.Setenv("AI_THREADS_DATA", "/data")
	if DefaultPath() != "/data/state.json" {
		t.Fatal(DefaultPath())
	}
	t.Setenv("AI_THREADS_DATA", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/u")
	if DataDir() != "/home/u/.local/share/ai-threads" {
		t.Fatal(DataDir())
	}
}

// Сводка за неделю хранится наравне со сводками за 3–5 дней и переживает перечитывание файла
// как есть; сводки первой версии рядом остаются читаемыми.
func TestDigestWeek(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw, err := os.ReadFile(filepath.Join("testdata", "v1-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, path)
	week := `{"days":7,"from":"2026-10-09","to":"2026-10-15","title":"Итоги за 9–15 октября","lead":"Два проекта. <b>",` +
		`"projects":[{"name":"backend","path":"/home/u/backend","items":[{"text":"[B-1](claude:x): ` + "`main`" + `","keys":["claude:x"]}],"left":""}],` +
		`"journals":4,"at":1792000000,"model":"kimi","lang":"ru"}`
	must(t, s.SetDigest(7, json.RawMessage(week)))
	for _, days := range []int{6, 8, 2} {
		var invalid *InvalidError
		if err := s.SetDigest(days, json.RawMessage(`{}`)); !errors.As(err, &invalid) || invalid.Field != "days" {
			t.Fatalf("days %d: %v", days, err)
		}
	}
	again := open(t, path)
	got, err := again.Digest(7)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(week), &b) != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("digest(7) = %s", got)
	}
	if old, err := again.Digest(3); err != nil || !bytes.Contains(old, []byte(`"bullets"`)) {
		t.Fatalf("digest(3) = %s, %v", old, err)
	}
	if d, err := again.Digest(5); d != nil || err != nil {
		t.Fatalf("digest(5) = %s, %v", d, err)
	}
}
