package agents

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/model"
)

// stub — исполняемый файл, который печатает output.
func stub(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub")
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOrderAndInfo(t *testing.T) {
	list := All(nil)
	var ids, shorts []string
	for _, a := range list {
		ids = append(ids, a.ID())
		shorts = append(shorts, a.Info().Short)
	}
	if !slices.Equal(ids, []string{"codex", "claude", "grok", "kimi", "zcode", "cursor"}) {
		t.Fatalf("ids = %v", ids)
	}
	if !slices.Equal(shorts, []string{"Cx", "Cl", "Gr", "Ki", "ZC", "Cu"}) {
		t.Fatalf("shorts = %v", shorts)
	}
	for _, a := range list {
		info := a.Info()
		if info.HomeEnv == "" || info.Name == "" {
			t.Errorf("%s: %+v", a.ID(), info)
		}
		if runner := slices.Contains([]string{"codex", "claude", "grok", "kimi"}, a.ID()); info.Runner != runner {
			t.Errorf("%s: runner = %v", a.ID(), info.Runner)
		}
	}
	if New("nope", Config{}) != nil {
		t.Error("неизвестный агент")
	}
}

func TestHomeSources(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	t.Setenv("CODEX_HOME", "")
	codex := New("codex", Config{})
	if codex.Home() != "/home/someone/.codex" || codex.HomeSource() != "default" {
		t.Fatalf("home = %q (%s)", codex.Home(), codex.HomeSource())
	}
	codex = New("codex", Config{Home: "~/codex"})
	if codex.Home() != "/home/someone/codex" || codex.HomeSource() != "settings" {
		t.Fatalf("home = %q (%s)", codex.Home(), codex.HomeSource())
	}
	// Переменная окружения важнее настроек, как в первой версии.
	t.Setenv("CODEX_HOME", "/env/codex")
	if codex.Home() != "/env/codex" || codex.HomeSource() != "env" {
		t.Fatalf("home = %q (%s)", codex.Home(), codex.HomeSource())
	}
}

func TestActiveNeedsHomeAndEnabled(t *testing.T) {
	homes := agentstest.Homes(t)
	configs := map[string]Config{}
	for id, home := range homes {
		configs[id] = Config{Home: home}
	}
	configs["grok"] = Config{Home: homes["grok"], Disabled: true}
	var ids []string
	for _, a := range Active(All(configs)) {
		ids = append(ids, a.ID())
	}
	// Папки cursor в эталонах нет, grok выключен.
	if !slices.Equal(ids, []string{"codex", "claude", "kimi", "zcode"}) {
		t.Fatalf("active = %v", ids)
	}
}

func TestProgramLookup(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	home := t.TempDir()
	kimi := New("kimi", Config{Home: home})
	if kimi.Program() != "" {
		t.Fatal("программы нет")
	}
	// Kimi ставится в свою папку, а не в PATH.
	write(t, filepath.Join(home, "bin", "kimi"), "")
	if kimi.Program() != filepath.Join(home, "bin", "kimi") {
		t.Fatalf("program = %q", kimi.Program())
	}
	if argv := kimi.Resume(model.Session{ID: "session_x"}); argv[0] != filepath.Join(home, "bin", "kimi") {
		t.Fatalf("resume = %q", argv)
	}
	write(t, filepath.Join(bin, "kimi"), "")
	_ = os.Chmod(filepath.Join(bin, "kimi"), 0o755)
	if kimi.Program() != filepath.Join(bin, "kimi") || kimi.Resume(model.Session{ID: "x"})[0] != "kimi" {
		t.Fatalf("program = %q", kimi.Program())
	}
	// Программа из настроек должна существовать и запускаться.
	if New("codex", Config{Program: filepath.Join(bin, "none")}).Program() != "" {
		t.Fatal("несуществующая программа из настроек")
	}
}

func TestResumeWithProgramAndSkipApprovals(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	claude := New("claude", Config{Program: "~/bin/claude", SkipApprovals: true})
	got := claude.Resume(model.Session{ID: "abc"})
	if want := []string{"/home/someone/bin/claude", "--resume", "abc", "--dangerously-skip-permissions"}; !slices.Equal(got, want) {
		t.Fatalf("resume = %q", got)
	}
}

func TestLaunch(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	id := agentstest.SID(5)
	cases := []struct {
		agent  string
		config Config
		opts   LaunchOptions
		argv   []string
		sid    string
		prompt bool
	}{
		{"claude", Config{SkipApprovals: true}, LaunchOptions{Model: "opus", Effort: "high", Prompt: "Почини тест", SessionID: id},
			[]string{"claude", "--model", "opus", "--effort", "high", "--session-id", id, "--dangerously-skip-permissions", "Почини тест"}, id, true},
		{"codex", Config{SkipApprovals: true}, LaunchOptions{Model: "gpt", Effort: "high", Prompt: "-v флаг"},
			[]string{"codex", "-m", "gpt", "-c", `model_reasoning_effort="high"`, "--dangerously-bypass-approvals-and-sandbox", "--", "-v флаг"}, "", true},
		{"grok", Config{}, LaunchOptions{Model: "g", Effort: "low", Prompt: "Привет", SessionID: id},
			[]string{"grok", "-m", "g", "--reasoning-effort", "low", "--session-id", id, "Привет"}, id, true},
		{"kimi", Config{}, LaunchOptions{Model: "k", Effort: "high", Prompt: "Привет"}, []string{"kimi", "--model", "k"}, "", false},
		{"cursor", Config{SkipApprovals: true}, LaunchOptions{Model: "m", Prompt: "Привет"},
			[]string{"cursor-agent", "--model", "m", "--force", "Привет"}, "", true},
		{"codex", Config{}, LaunchOptions{}, []string{"codex"}, "", false},
		{"zcode", Config{}, LaunchOptions{Prompt: "Привет"}, nil, "", false},
	}
	for _, tc := range cases {
		cmd := New(tc.agent, tc.config).Launch(tc.opts)
		if !slices.Equal(cmd.Argv, tc.argv) || cmd.SessionID != tc.sid || cmd.PromptInArgs != tc.prompt {
			t.Errorf("%s: %+v", tc.agent, cmd)
		}
	}
	// Без заданного ID Claude получает новый UUID.
	cmd := New("claude", Config{}).Launch(LaunchOptions{})
	if !isUUID(cmd.SessionID) || !slices.Equal(cmd.Argv, []string{"claude", "--session-id", cmd.SessionID}) {
		t.Fatalf("cmd = %+v", cmd)
	}
	if other := New("claude", Config{}).Launch(LaunchOptions{}); other.SessionID == cmd.SessionID {
		t.Fatal("одинаковые ID")
	}
}

func TestHeadlessChoiceGoesRightAfterProgram(t *testing.T) {
	program := stub(t, "")
	instructions := filepath.Join(t.TempDir(), "agent.md")
	write(t, instructions, "---\nname: summary\n---\n\nСоставь сводку.\n")
	cases := map[string][]string{
		"codex":  {"-m", "m1", "-c", `model_reasoning_effort="high"`},
		"claude": {"--model", "m1", "--effort", "high"},
		"grok":   {"-m", "m1", "--reasoning-effort", "high"},
		"kimi":   {"--model", "m1"},
	}
	for id, flags := range cases {
		agent := New(id, Config{Program: program})
		opts := HeadlessOptions{Prompt: "запрос", Instructions: instructions, OutFile: "/tmp/answer.json", Model: "m1", Effort: "high"}
		cmd, err := agent.Headless(opts)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !slices.Equal(cmd.Argv[1:1+len(flags)], flags) || cmd.Argv[0] != program {
			t.Errorf("%s: argv = %q", id, cmd.Argv)
		}
		opts.Model, opts.Effort = "", ""
		cmd, _ = agent.Headless(opts)
		for _, flag := range []string{"-m", "--model", "--effort", "--reasoning-effort"} {
			if slices.Contains(cmd.Argv, flag) {
				t.Errorf("%s: %q без выбора модели", id, cmd.Argv)
			}
		}
		if (cmd.OutFile != "") != (id == "codex") {
			t.Errorf("%s: out file = %q", id, cmd.OutFile)
		}
	}
	claude, _ := New("claude", Config{Program: program}).Headless(HeadlessOptions{Prompt: "запрос", Instructions: instructions,
		AddDirs: []string{"/a"}})
	if claude.Stdin != "Составь сводку.\n\n---\n\nзапрос" || !slices.Contains(claude.Argv, "--add-dir") {
		t.Errorf("claude = %+v", claude)
	}
	grok, _ := New("grok", Config{Program: program}).Headless(HeadlessOptions{Prompt: "ответ", Session: "s1"})
	if want := []string{program, "-p", "ответ", "--resume", "s1", "--sandbox", "read-only", "--output-format", "streaming-messages-json"}; !slices.Equal(grok.Argv, want) || grok.Stdin != "" {
		t.Errorf("grok reply = %+v", grok)
	}
	if _, err := New("cursor", Config{Program: program}).Headless(HeadlessOptions{}); err == nil {
		t.Error("Cursor Agent сводки не составляет")
	}
	if _, err := New("claude", Config{Program: "/nonexistent"}).Headless(HeadlessOptions{}); err == nil {
		t.Error("без программы запустить нельзя")
	}
}

func TestCodexModelsFromItsCache(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "models_cache.json"), `{"models": [
		{"slug": "gpt-new", "display_name": "GPT-New", "default_reasoning_level": "low",
		 "supported_reasoning_levels": [{"effort": "low"}, {"effort": "max"}], "visibility": "list"},
		{"slug": "hidden", "display_name": "Hidden", "visibility": "hide"}]}`)
	write(t, filepath.Join(home, "config.toml"), "model = \"gpt-new\"\nmodel_reasoning_effort = \"max\"\n\n[x]\nmodel = \"no\"\n")
	models := New("codex", Config{Home: home}).Models()
	if models.DefaultModel != "gpt-new" || models.DefaultEffort != "max" || len(models.Models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	m := models.Models[0]
	if m.ID != "gpt-new" || m.Name != "GPT-New" || !slices.Equal(m.Efforts, []string{"low", "max"}) || m.DefaultEffort != "low" {
		t.Fatalf("model = %+v", m)
	}
}

func TestKimiModelsFromConfig(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "config.toml"), "default_model = \"kimi-code/a\"\n\n[models.\"kimi-code/a\"]\nmodel = \"a\"\n"+
		"display_name = \"Model A\"\n\n[models.\"kimi-code/b\"]\nmodel = \"b\"\n")
	models := New("kimi", Config{Home: home}).Models()
	if models.DefaultModel != "kimi-code/a" || len(models.Models) != 2 ||
		models.Models[0].ID != "kimi-code/a" || models.Models[0].Name != "Model A" || models.Models[1].Name != "kimi-code/b" {
		t.Fatalf("models = %+v", models)
	}
}

func TestClaudeAliasesAndGrokCommand(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "settings.json"), `{"model": "sonnet"}`)
	claude := New("claude", Config{Home: home}).Models()
	if claude.DefaultModel != "sonnet" || claude.Models[0].ID != "opus" ||
		!slices.Equal(claude.Efforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("claude = %+v", claude)
	}
	program := stub(t, "You are logged in.\n\nDefault model: grok-test\n\nAvailable models:\n  * grok-test (default)\n  - grok-test-fast")
	grok := New("grok", Config{Program: program})
	models := grok.Models()
	if models.DefaultModel != "grok-test" || len(models.Models) != 2 || models.Models[1].ID != "grok-test-fast" {
		t.Fatalf("grok = %+v", models)
	}
	// Список кэшируется: программа больше не спрашивается.
	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}
	if again := grok.Models(); len(again.Models) != 2 {
		t.Fatalf("grok = %+v", again)
	}
}

func TestValidChoice(t *testing.T) {
	for _, ok := range []string{"", "gpt-6.1-sol", "xhigh", "kimi-code/a", "claude-opus@1:x+y"} {
		if !ValidChoice(ok) {
			t.Errorf("%q", ok)
		}
	}
	for _, bad := range []string{"a b", "x;y", `x"y`, string(make([]byte, 101))} {
		if ValidChoice(bad) {
			t.Errorf("%q", bad)
		}
	}
}
