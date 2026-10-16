package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fieldErrors(t *testing.T, err error) map[string]string {
	t.Helper()
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("ожидались ошибки полей, получено %v", err)
	}
	out := map[string]string{}
	for _, e := range errs {
		out[e.Field] = e.Code
	}
	return out
}

// Файл, записанный первой версией, читается без ошибок: секунды — в минуты, temp_dirs — в service,
// search отбрасывается.
func TestV1File(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1-settings.json")
	must(t, err)
	s, err := Parse(raw)
	must(t, err)
	want := Defaults()
	want.Agents["codex"] = Agent{Enabled: true, Home: "~/.codex", SkipApprovals: true}
	want.Agents["claude"] = Agent{Enabled: false, Program: "/usr/local/bin/claude"}
	want.Summary = Runner{Agent: "kimi", Minutes: 8}
	want.Language = "ru"
	want.Service.TempDirs = []string{"/tmp", "~/scratch"}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got  %+v\nwant %+v", s, want)
	}

	// Сохраняются только отличия от умолчаний, уже в новом виде.
	f := New(filepath.Join(t.TempDir(), "settings.json"))
	must(t, f.Save(s))
	var stored map[string]any
	data, _ := os.ReadFile(f.Path())
	must(t, json.Unmarshal(data, &stored))
	wantStored := map[string]any{
		"agents": map[string]any{
			"codex":  map[string]any{"home": "~/.codex", "skip_approvals": true},
			"claude": map[string]any{"enabled": false, "program": "/usr/local/bin/claude"},
		},
		"summary":  map[string]any{"agent": "kimi", "minutes": 8.0},
		"language": "ru",
		"service":  map[string]any{"temp_dirs": []any{"/tmp", "~/scratch"}},
	}
	if !reflect.DeepEqual(stored, wantStored) {
		t.Fatalf("file = %s", data)
	}
	if got := f.Get(); !reflect.DeepEqual(got, want) || f.Err() != nil {
		t.Fatalf("reloaded = %+v, err = %v", got, f.Err())
	}
}

func TestDefaultsAreNotWritten(t *testing.T) {
	f := New(filepath.Join(t.TempDir(), "sub", "settings.json"))
	must(t, f.Save(Defaults()))
	if data, _ := os.ReadFile(f.Path()); string(data) != "{}\n" {
		t.Fatalf("file = %q", data)
	}
	// Пустой список — тоже выбор человека: он записывается и читается обратно пустым.
	s := Defaults()
	s.Service.AutoCommands = []string{}
	must(t, f.Save(s))
	if got := f.Get().Service.AutoCommands; got == nil || len(got) != 0 {
		t.Fatalf("auto_commands = %#v", got)
	}
	// nil вместо агентов и списков — умолчания; пустые строки и 0 минут — ошибка у поля.
	s = Defaults()
	s.Agents, s.Service.AutoCommands, s.Service.TempDirs = nil, nil, nil
	must(t, f.Save(s))
	if got := f.Get(); !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("got %+v", got)
	}
	// summary и digest — агент и минуты, язык, tmux.session_name, remote.hostname и remote.state_dir.
	if err := f.Save(Settings{}); len(fieldErrors(t, err)) != 8 {
		t.Fatal(err)
	}
}

func TestUIRoundTrip(t *testing.T) {
	s := Defaults()
	s.Agents["kimi"] = Agent{Enabled: true, Flags: `--model "k2 turbo" --yolo`}
	s.Digest = Runner{Agent: "claude", Minutes: 30}
	s.Tmux.SessionName = "nit-{agent}-{id}"
	raw, err := json.Marshal(s)
	must(t, err)
	got, err := Parse(raw)
	must(t, err)
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("got %+v", got)
	}
	if args := got.Agent("kimi").Args(); !slices.Equal(args, []string{"--model", "k2 turbo", "--yolo"}) {
		t.Fatalf("args = %q", args)
	}
}

func TestRetiredFieldsAreDropped(t *testing.T) {
	s, err := Parse([]byte(`{"open_with": "tmux", "tmux_session": "work", "search": {"agent": "kimi", "timeout": 60}, "language": "en"}`))
	must(t, err)
	want := Defaults()
	want.Language = "en"
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %+v", s)
	}
}

func TestFieldErrors(t *testing.T) {
	s, err := Parse([]byte(`{
		"agents": {"codex": {"resume": "codex resume {id}", "skip_approvals": "yes", "home": "/ok"},
		           "vim": {}, "claude": {"flags": "--add-dir 'x", "program": "a\nb"}, "grok": 5},
		"summary": {"agent": "gpt", "minutes": 0},
		"digest": {"minutes": 2.5, "extra": 1},
		"language": "de",
		"service": {"auto_commands": ["codex exec", ""], "temp_dirs": ["tmp", "/ok"], "x": 1},
		"temp_dirs": ["relative"],
		"tmux": {"session_name": "nit.{project}:{host}"},
		"colour": "red"
	}`))
	got := fieldErrors(t, err)
	want := map[string]string{
		"agents.codex.resume": "unknown", "agents.codex.skip_approvals": "bool", "agents.vim": "unknown_agent",
		"agents.claude.flags": "quote", "agents.claude.program": "string", "agents.grok": "object",
		"summary.agent": "runner", "summary.minutes": "minutes", "digest.minutes": "minutes", "digest.extra": "unknown",
		"language": "one_of", "service.auto_commands": "required", "service.temp_dirs": "temp_path", "service.x": "unknown",
		"tmux.session_name": "name_chars", "colour": "unknown",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
	// Верные поля применены, неверные остались по умолчанию.
	def := Defaults()
	if s.Agents["codex"].Home != "/ok" || s.Agents["codex"].SkipApprovals || s.Language != "auto" ||
		!slices.Equal(s.Service.TempDirs, def.Service.TempDirs) || s.Tmux != def.Tmux {
		t.Fatalf("settings = %+v", s)
	}
	var errs Errors
	errors.As(err, &errs)
	for _, e := range errs {
		if e.Text("ru") == e.Code || e.Text("en") == e.Code {
			t.Errorf("нет текста для %s", e.Code)
		}
	}
}

func TestErrorTexts(t *testing.T) {
	e := FieldError{Field: "service.temp_dirs", Code: "temp_path", Value: "tmp"}
	if e.Error() != "service.temp_dirs: путь tmp должен начинаться с / или ~/" || e.Text("en") != "path tmp must start with / or ~/" {
		t.Fatal(e.Error(), e.Text("en"))
	}
	if _, err := Parse([]byte(`{"summary": {"timeout": 5}}`)); fieldErrors(t, err)["summary.timeout"] != "seconds" {
		t.Fatal(err)
	}
	for _, raw := range []string{"{broken", "[]", "null"} {
		if _, err := Parse([]byte(raw)); fieldErrors(t, err)[""] != "json" {
			t.Fatal(raw, err)
		}
	}
}

func TestTmuxPattern(t *testing.T) {
	for pattern, code := range map[string]string{
		"nit-{project}-{branch}": "", "nit_{agent}-{id}": "", "{project}": "",
		"nit-{project": "placeholder", "nit-{cwd}": "placeholder", "nit {id}": "name_chars", "нить-{id}": "name_chars",
	} {
		if got, _ := checkPattern(pattern); got != code {
			t.Errorf("%q: %q, want %q", pattern, got, code)
		}
	}
}

func TestSplitFlags(t *testing.T) {
	for in, want := range map[string][]string{
		"":                             nil,
		"  --model opus  ":             {"--model", "opus"},
		`--add-dir '../my dir' -c a=b`: {"--add-dir", "../my dir", "-c", "a=b"},
		`"a \"b\" \x" c\ d ''`:         {`a "b" \x`, "c d", ""},
		`--prompt="hello world"`:       {"--prompt=hello world"},
		`--config 'model="gpt-5.4"'`:   {"--config", `model="gpt-5.4"`},
		"--model\tk2":                  {"--model", "k2"},
	} {
		got, err := SplitFlags(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{`'open`, `"open`, `trailing\`} {
		if _, err := SplitFlags(bad); err == nil {
			t.Errorf("%q разобран", bad)
		}
	}
}

func TestBrokenFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	must(t, os.WriteFile(path, []byte("{broken"), 0o600))
	f := New(path)
	if !reflect.DeepEqual(f.Get(), Defaults()) || f.Err() == nil {
		t.Fatalf("err = %v", f.Err())
	}
	// Одно неверное поле не отменяет остальные.
	must(t, os.WriteFile(path, []byte(`{"language": "en", "summary": {"minutes": 500}}`), 0o600))
	if s := f.Get(); s.Language != "en" || s.Summary.Minutes != 5 || fieldErrors(t, f.Err())["summary.minutes"] != "minutes" {
		t.Fatalf("settings = %+v, err = %v", s, f.Err())
	}
	// Неверные настройки не записываются.
	bad := Defaults()
	bad.Language = "de"
	if err := f.Save(bad); fieldErrors(t, err)["language"] != "one_of" {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != `{"language": "en", "summary": {"minutes": 500}}` {
		t.Fatalf("file = %s", data)
	}
}

func TestFileReloadsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := New(path)
	if f.Get().Language != "auto" || f.Err() != nil {
		t.Fatal("нет файла — умолчания без ошибки")
	}
	must(t, os.WriteFile(path, []byte(`{"language": "ru"}`), 0o600))
	if f.Get().Language != "ru" {
		t.Fatal("новый файл не подхвачен")
	}
	must(t, os.WriteFile(path, []byte(`{"language": "en", "tmux": {"session_name": "x-{id}"}}`), 0o600))
	if s := f.Get(); s.Language != "en" || s.Tmux.SessionName != "x-{id}" {
		t.Fatalf("правка не подхвачена: %+v", s)
	}
	s := f.Get()
	s.Agents["codex"] = Agent{}
	s.Service.TempDirs[0] = "/changed"
	if again := f.Get(); !again.Agents["codex"].Enabled || again.Service.TempDirs[0] != "/tmp" {
		t.Fatal("Get делит память с кешем")
	}
	must(t, os.Remove(path))
	if f.Get().Language != "auto" {
		t.Fatal("удалённый файл — умолчания")
	}
}

func TestTimeoutAndTempDirs(t *testing.T) {
	s := Defaults()
	t.Setenv("AI_THREADS_SUMMARY_TIMEOUT", "")
	t.Setenv("AI_THREADS_DIGEST_TIMEOUT", "")
	if s.Timeout("summary") != 5*time.Minute || s.Timeout("digest") != 15*time.Minute {
		t.Fatal(s.Timeout("summary"), s.Timeout("digest"))
	}
	t.Setenv("AI_THREADS_DIGEST_TIMEOUT", "1.5")
	t.Setenv("AI_THREADS_SUMMARY_TIMEOUT", "bad")
	if s.Timeout("digest") != 1500*time.Millisecond || s.Timeout("summary") != 5*time.Minute {
		t.Fatal(s.Timeout("digest"), s.Timeout("summary"))
	}
	t.Setenv("HOME", "/home/u")
	s.Service.TempDirs = []string{"/tmp/", "~/.local", "~", "/"}
	if got := s.TempDirPaths(); !slices.Equal(got, []string{"/tmp", "/home/u/.local", "/home/u", "/"}) {
		t.Fatal(got)
	}
}

func TestPath(t *testing.T) {
	t.Setenv("AI_THREADS_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if Path() != "/xdg/ai-threads/settings.json" {
		t.Fatal(Path())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if Path() != "/home/u/.config/ai-threads/settings.json" {
		t.Fatal(Path())
	}
	t.Setenv("AI_THREADS_CONFIG", "/etc/nit.json")
	if Path() != "/etc/nit.json" {
		t.Fatal(Path())
	}
}

// Доступ с телефона: в файл попадают только отличия, неверные значения — ошибки у полей.
func TestRemote(t *testing.T) {
	def := Defaults().Remote
	if def != (Remote{Hostname: "nit", StateDir: "~/.threadgate"}) {
		t.Fatalf("defaults = %+v", def)
	}
	f := New(filepath.Join(t.TempDir(), "settings.json"))
	s := Defaults()
	s.Remote.Enabled = true
	must(t, f.Save(s))
	if data, _ := os.ReadFile(f.Path()); string(data) != "{\n  \"remote\": {\n    \"enabled\": true\n  }\n}\n" {
		t.Fatalf("file = %s", data)
	}
	s.Remote = Remote{Enabled: true, Funnel: true, Hostname: "nit-2", StateDir: "/srv/nit"}
	must(t, f.Save(s))
	if got := f.Get().Remote; got != s.Remote {
		t.Fatalf("reloaded = %+v", got)
	}
	// Выключенный доступ с умолчаниями из файла пропадает.
	s.Remote = def
	must(t, f.Save(s))
	if data, _ := os.ReadFile(f.Path()); string(data) != "{}\n" {
		t.Fatalf("file = %s", data)
	}

	_, err := Parse([]byte(`{"remote": {"enabled": "yes", "funnel": 1, "hostname": "Nit.Home", "state_dir": "state", "port": 443}}`))
	want := map[string]string{"remote.enabled": "bool", "remote.funnel": "bool", "remote.hostname": "hostname",
		"remote.state_dir": "temp_path", "remote.port": "unknown"}
	if got := fieldErrors(t, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, host := range []string{"-nit", "nit-", "nit_2", "нить", strings.Repeat("a", 64), ""} {
		raw, _ := json.Marshal(map[string]any{"remote": map[string]any{"hostname": host}})
		if _, err := Parse(raw); err == nil {
			t.Errorf("hostname %q принят", host)
		}
	}
	for _, dir := range []string{"~", "relative/dir", ""} {
		raw, _ := json.Marshal(map[string]any{"remote": map[string]any{"state_dir": dir}})
		if _, err := Parse(raw); err == nil {
			t.Errorf("state_dir %q принят", dir)
		}
	}
	got, err := Parse([]byte(`{"remote": {"hostname": "a1-b", "state_dir": "~/nit-node", "enabled": null}}`))
	must(t, err)
	if got.Remote != (Remote{Hostname: "a1-b", StateDir: "~/nit-node"}) {
		t.Fatalf("got %+v", got.Remote)
	}
	t.Setenv("HOME", "/home/u")
	if dir := got.Remote.RemoteDir(); dir != "/home/u/nit-node" {
		t.Fatal(dir)
	}
	if dir := (Remote{StateDir: "/srv/x"}).RemoteDir(); dir != "/srv/x" {
		t.Fatal(dir)
	}
}
