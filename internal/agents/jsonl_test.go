package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/model"
)

var jsonlTools = []string{"codex", "claude", "grok", "kimi"}

// setup — агенты, которые читают копии эталонов во временной папке.
func setup(t *testing.T) (map[string]string, map[string]Agent) {
	t.Helper()
	homes := agentstest.Homes(t)
	list := map[string]Agent{}
	for id, home := range homes {
		list[id] = New(id, Config{Home: home})
	}
	return homes, list
}

func key(tool string, n int) string {
	if tool == "kimi" {
		return "kimi:session_" + agentstest.SID(n)
	}
	return tool + ":" + agentstest.SID(n)
}

func scan(a Agent, c *Cache) map[string]model.Session {
	sessions := map[string]model.Session{}
	for _, s := range a.Scan(c, a.Discover(), nil) {
		sessions[s.Key()] = s
	}
	return sessions
}

func pairs(messages []model.Message) [][2]string {
	var result [][2]string
	for _, m := range messages {
		result = append(result, [2]string{m.Role, m.Text})
	}
	return result
}

func appendLine(t *testing.T, path string, row any) {
	t.Helper()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

var conversation = [][2]string{
	{"user", "Найди запись в каталоге и проверь её название"},
	{"assistant", "Запись найдена. Проверка завершена."},
}

func TestManualAndAutomatic(t *testing.T) {
	_, agents := setup(t)
	by := map[string]string{"codex": "codex exec", "claude": "claude -p", "grok": "grok headless", "kimi": "kimi -p"}
	resume := map[string][]string{"codex": {"codex", "resume"}, "claude": {"claude", "--resume"},
		"grok": {"grok", "--resume"}, "kimi": {"kimi", "--session"}}
	t.Setenv("PATH", t.TempDir())
	for _, tool := range jsonlTools {
		t.Run(tool, func(t *testing.T) {
			agent := agents[tool]
			sessions := scan(agent, &Cache{})
			manual, auto, empty := sessions[key(tool, 1)], sessions[key(tool, 2)], sessions[key(tool, 3)]
			wantID := agentstest.SID(1)
			if tool == "kimi" {
				wantID = "session_" + wantID
			}
			if manual.ID != wantID || manual.Title != "Проверить каталог" {
				t.Fatalf("manual = %+v", manual)
			}
			if manual.Cwd != agentstest.Cwd || manual.Project() != "sample app" {
				t.Errorf("cwd = %q, project = %q", manual.Cwd, manual.Project())
			}
			wantBranch := "feature/catalog"
			if tool == "kimi" {
				wantBranch = ""
			}
			if manual.Branch != wantBranch {
				t.Errorf("branch = %q", manual.Branch)
			}
			if manual.Created.Unix() != agentstest.Created || manual.Updated.Unix() != agentstest.Updated {
				t.Errorf("created = %v, updated = %v", manual.Created, manual.Updated)
			}
			if manual.Auto || manual.By != "" || manual.Empty || manual.Last != "Запись найдена. Проверка завершена." {
				t.Errorf("manual = %+v", manual)
			}
			if !isFile(manual.Journal) || !isFile(manual.Meta) {
				t.Errorf("journal = %q, meta = %q", manual.Journal, manual.Meta)
			}
			if !auto.Auto || auto.By != by[tool] {
				t.Errorf("auto = %+v", auto)
			}
			if got, want := agent.Resume(manual), append(slices.Clone(resume[tool]), manual.ID); !slices.Equal(got, want) {
				t.Errorf("resume = %q, want %q", got, want)
			}
			if tool == "codex" {
				if got := agent.Resume(auto); !slices.Equal(got, []string{"codex", "exec", "resume", auto.ID}) {
					t.Errorf("resume auto = %q", got)
				}
			}
			if messages, _ := agent.Transcript(empty, MessageBytes); !empty.Empty || empty.Last != "" || len(messages) != 0 {
				t.Errorf("empty = %+v, messages = %v", empty, messages)
			}
			if _, ok := sessions[key(tool, 9)]; ok {
				t.Error("подагент попал в каталог")
			}
		})
	}
}

func TestMessagesContainOnlyConversation(t *testing.T) {
	_, agents := setup(t)
	for _, tool := range jsonlTools {
		t.Run(tool, func(t *testing.T) {
			s := scan(agents[tool], &Cache{})[key(tool, 1)]
			messages, truncated := agents[tool].Transcript(s, MessageBytes)
			if !slices.Equal(pairs(messages), conversation) || truncated {
				t.Fatalf("messages = %v, truncated = %v", pairs(messages), truncated)
			}
			for _, m := range messages {
				if !m.At.IsZero() && m.At.UTC().Format("2006-01-02") != "2026-09-28" {
					t.Errorf("at = %v", m.At)
				}
			}
			if excerpt := agents[tool].Excerpt(s); !slices.Equal(pairs(excerpt), conversation) {
				t.Errorf("excerpt = %v", pairs(excerpt))
			}
			if raw := string(agents[tool].RawText(s)); !strings.Contains(raw, "Запись найдена") {
				t.Errorf("raw text = %q", raw)
			}
		})
	}
}

func TestKimiOldFormat(t *testing.T) {
	_, agents := setup(t)
	s := scan(agents["kimi"], &Cache{})[key("kimi", 4)]
	if s.ID != "session_"+agentstest.SID(4) || s.Cwd != agentstest.Cwd || s.Created.Unix() != agentstest.Created || s.Auto {
		t.Fatalf("session = %+v", s)
	}
}

func TestKimiContextFormatExcludesInjectionsAndTools(t *testing.T) {
	homes, agents := setup(t)
	kimi := agents["kimi"]
	s := scan(kimi, &Cache{})[key("kimi", 2)]
	data, err := os.ReadFile(filepath.Join(homes["kimi"], "formats", "context-wire.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Journal, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s = scan(kimi, &Cache{})[key("kimi", 2)]
	if !s.Auto || s.Empty || s.Last != "Запись найдена. Проверка завершена." {
		t.Fatalf("session = %+v", s)
	}
	messages, _ := kimi.Transcript(s, MessageBytes)
	want := [][2]string{{"user", "Проверь изменения без ручного запуска"}, {"assistant", "Запись найдена. Проверка завершена."}}
	if !slices.Equal(pairs(messages), want) {
		t.Fatalf("messages = %v", pairs(messages))
	}
}

func TestCodexUsesFirstSource(t *testing.T) {
	_, agents := setup(t)
	codex := agents["codex"]
	appendLine(t, codex.Discover()[0], map[string]any{"type": "session_meta", "payload": map[string]any{"source": "exec"}})
	if s := scan(codex, &Cache{})[key("codex", 1)]; s.Auto || s.By != "" {
		t.Fatalf("session = %+v", s)
	}
}

func TestTitlePrecedenceAndKimiPromptMatching(t *testing.T) {
	homes, agents := setup(t)
	cache := &Cache{}
	claude := scan(agents["claude"], cache)
	// Заголовок из журнала важнее истории ввода; из истории берётся первая запись сессии.
	if claude[key("claude", 1)].Title != "Проверить каталог" || claude[key("claude", 2)].Title != "Проверить изменения" {
		t.Fatalf("titles = %q, %q", claude[key("claude", 1)].Title, claude[key("claude", 2)].Title)
	}
	kimiCache := &Cache{}
	if !scan(agents["kimi"], kimiCache)[key("kimi", 2)].Auto {
		t.Fatal("сессия 2 должна быть автоматической")
	}
	appendLine(t, filepath.Join(homes["kimi"], "user-history", "history.jsonl"),
		map[string]any{"content": "Проверь  изменения\nбез ручного запуска"})
	if scan(agents["kimi"], kimiCache)[key("kimi", 2)].Auto {
		t.Fatal("запрос из истории ввода набран человеком")
	}
}

func TestLargeJournalReadsOnlyHeadAndTail(t *testing.T) {
	_, agents := setup(t)
	for _, tool := range jsonlTools {
		t.Run(tool, func(t *testing.T) {
			agent := agents[tool].(*jsonlAgent)
			var unit string
			for _, path := range agent.Discover() {
				if strings.Contains(path, agentstest.SID(1)) {
					unit = path
				}
			}
			journal := agent.journalOf(unit)
			original, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			padding := []byte(`{"type":"ignored","text":"` + strings.Repeat("x", 1000) + "\"}\n")
			data := slices.Clone(original)
			for len(data) < 12<<20 {
				data = append(data, padding...)
			}
			data = append(data, original...)
			if err := os.WriteFile(journal, data, 0o644); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			s := scan(agent, &Cache{})[key(tool, 1)]
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("scan took %v", elapsed)
			}
			if s.Title != "Проверить каталог" || s.Last != "Запись найдена. Проверка завершена." {
				t.Fatalf("session = %+v", s)
			}
		})
	}
}

func TestIncompleteBoundariesAndServiceText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	data := `{"type":"ignored"}` + "\n" + strings.Repeat("x", 5000) + "\n" + `{"type":"user","content":"hello"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows := jsonRows(path, 100, true); len(rows) != 1 || rows[0]["content"] != "hello" {
		t.Errorf("tail rows = %v", rows)
	}
	if rows := jsonRows(path, 25, false); len(rows) != 1 || rows[0]["type"] != "ignored" {
		t.Errorf("head rows = %v", rows)
	}
	_, agents := setup(t)
	rows := []map[string]any{
		{"type": "user", "content": "<environment_context>Служебный текст</environment_context>\nВопрос"},
		{"type": "user", "content": "<INSTRUCTIONS>Служебный текст"},
		{"type": "assistant", "content": []any{map[string]any{"type": "thinking", "text": "Скрыто"},
			map[string]any{"type": "text", "text": "Ответ"}}},
	}
	got := pairs(agents["grok"].(*jsonlAgent).messages(rows, 0))
	if want := [][2]string{{"user", "Вопрос"}, {"assistant", "Ответ"}}; !slices.Equal(got, want) {
		t.Errorf("messages = %v", got)
	}
}

func TestTranscriptLimitAndTruncation(t *testing.T) {
	_, agents := setup(t)
	grok := agents["grok"]
	s := scan(grok, &Cache{})[key("grok", 1)]
	var lines []string
	for n := range 80 {
		lines = append(lines, fmt.Sprintf(`{"type":"user","content":"%03d"}`, n))
	}
	if err := os.WriteFile(s.Journal, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	messages, truncated := grok.Transcript(s, MessageBytes)
	if truncated || len(messages) != 80 {
		t.Fatalf("len = %d, truncated = %v", len(messages), truncated)
	}
	messages, truncated = grok.Transcript(s, 30*int64(len(lines[0])+1))
	if !truncated || len(messages) != 30 || messages[0].Text != "050" || messages[29].Text != "079" {
		t.Fatalf("len = %d, truncated = %v, first = %q", len(messages), truncated, messages[0].Text)
	}
}

func TestClaudeServiceRecordsAreNotUserMessages(t *testing.T) {
	user := func(content string, extra map[string]any) map[string]any {
		row := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
		for k, v := range extra {
			row[k] = v
		}
		return row
	}
	human := map[string]any{"origin": map[string]any{"kind": "human"}}
	rows := []map[string]any{
		user("Первый вопрос", map[string]any{"origin": map[string]any{"kind": "human"}, "promptSource": "typed"}),
		user("<task-notification><task-id>1</task-id></task-notification>", map[string]any{"origin": map[string]any{"kind": "task-notification"}}),
		user("Another Claude session sent a message", map[string]any{"origin": map[string]any{"kind": "peer"}, "isMeta": true}),
		user("Base directory for this skill: /x", map[string]any{"isMeta": true}),
		user("This session is being continued from a previous conversation", map[string]any{"isCompactSummary": true,
			"isVisibleInTranscriptOnly": true}),
		user("<local-command-stdout>Set model</local-command-stdout>", nil),
		user("<command-name>/model</command-name>", nil),
		user("<bash-stdout>ok</bash-stdout>", nil),
		user("<bash-input>docker ps</bash-input>", nil),
		user(`<pasted_content id="1a2b">Вставленный текст</pasted_content> и вопрос`, human),
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Ответ"}}}},
	}
	_, agents := setup(t)
	got := pairs(agents["claude"].(*jsonlAgent).messages(rows, 0))
	want := [][2]string{{"user", "Первый вопрос"}, {"user", "! docker ps"}, {"user", "Вставленный текст и вопрос"}, {"assistant", "Ответ"}}
	if !slices.Equal(got, want) {
		t.Fatalf("messages = %v", got)
	}
}

func TestCodexServiceBlocksAreRemoved(t *testing.T) {
	part := map[string]any{"type": "input_text", "text": "<recommended_plugins>список</recommended_plugins>\n" +
		`<image name=[Image #1] path="/tmp/a.png">` + "\n</image>\nПочини тест"}
	rows := []map[string]any{{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{part}}}}
	_, agents := setup(t)
	if got := pairs(agents["codex"].(*jsonlAgent).messages(rows, 0)); !slices.Equal(got, [][2]string{{"user", "Почини тест"}}) {
		t.Fatalf("messages = %v", got)
	}
}

func TestUnchangedFilesAreNotReread(t *testing.T) {
	_, agents := setup(t)
	claude := agents["claude"]
	cache := &Cache{}
	first := scan(claude, cache)
	// Кэш переживает запись на диск.
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Cache{}
	if err := json.Unmarshal(data, restored); err != nil {
		t.Fatal(err)
	}
	if again := scan(claude, restored); !sameSessions(again, first) {
		t.Fatalf("после кэша: %v\nбыло: %v", again, first)
	}
	// Запись кэша верна, пока файл не менялся: подмена видна, значит, файл не читался.
	s := first[key("claude", 1)]
	restored.Files[s.Meta].Session.Last = "из кэша"
	if got := scan(claude, restored)[s.Key()].Last; got != "из кэша" {
		t.Fatalf("last = %q", got)
	}
	appendLine(t, s.Journal, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": "Новый ответ"}})
	if got := scan(claude, restored)[s.Key()].Last; got != "Новый ответ" {
		t.Fatalf("last = %q", got)
	}
	// Удалённый файл забывается.
	if err := os.Remove(s.Meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := scan(claude, restored)[s.Key()]; ok || restored.Files[s.Meta] != nil {
		t.Fatal("удалённая сессия осталась")
	}
}

func sameSessions(a, b map[string]model.Session) bool {
	if len(a) != len(b) {
		return false
	}
	for k, s := range a {
		o := b[k]
		if !s.Created.Equal(o.Created) || !s.Updated.Equal(o.Updated) {
			return false
		}
		s.Created, s.Updated, o.Created, o.Updated = time.Time{}, time.Time{}, time.Time{}, time.Time{}
		if s != o {
			return false
		}
	}
	return true
}

func TestAppendIndexOffsetAndTruncation(t *testing.T) {
	cases := []struct {
		tool, index string
		n           int
		record      map[string]any
		title       string
	}{
		{"codex", "session_index.jsonl", 1, map[string]any{"id": agentstest.SID(1), "thread_name": "Новое название"}, "Новое название"},
		{"claude", "history.jsonl", 3, map[string]any{"sessionId": agentstest.SID(3), "display": "Новая история",
			"project": "/home/user/new"}, "Новая история"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			homes, agents := setup(t)
			agent := agents[tc.tool]
			cache := &Cache{}
			scan(agent, cache)
			path := filepath.Join(homes[tc.tool], tc.index)
			offset := stamp(path)[0]
			data, _ := json.Marshal(tc.record)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write(data)
			// Строка без перевода ещё дописывается: смещение на её начале.
			scan(agent, cache)
			if got := cache.Indexes[path].Offset; got != offset {
				t.Fatalf("offset = %d, want %d", got, offset)
			}
			_, _ = f.Write([]byte("\n"))
			f.Close()
			if got := scan(agent, cache)[key(tc.tool, tc.n)].Title; got != tc.title {
				t.Fatalf("title = %q", got)
			}
			if got := cache.Indexes[path].Offset; got != stamp(path)[0] {
				t.Fatalf("offset = %d", got)
			}
			// Файл стал короче — индекс читается заново.
			if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := scan(agent, cache)[key(tc.tool, tc.n)].Title; got == tc.title {
				t.Fatalf("title = %q остался после усечения индекса", got)
			}
		})
	}
}

func TestPlainLine(t *testing.T) {
	text := "## Итог\n**Готово:** `make test` прошёл.\n- пункт session_id"
	if got := plainLine(text); got != "Итог Готово: make test прошёл. пункт session_id" {
		t.Fatalf("plain line = %q", got)
	}
	if got := plainLine(strings.Repeat("я", 400)); len([]rune(got)) != 300 {
		t.Fatalf("len = %d", len([]rune(got)))
	}
}

func TestCleanTitleAndVisibleText(t *testing.T) {
	cases := map[string]string{
		"## Заголовок с\n\nпереносом":                                   "Заголовок с переносом",
		"<system-reminder>скрыто</system-reminder>  Видно":              "Видно",
		"# AGENTS.md instructions for /x\nТекст":                        "Текст",
		"До <user_instructions>незакрытая вставка":                      "До",
		"<INSTRUCTIONS>а</INSTRUCTIONS><INSTRUCTIONS>б</INSTRUCTIONS>в": "в",
	}
	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTimestamp(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{
		{"2026-09-28T08:00:00Z", agentstest.Created},
		{"2026-09-28T08:00:00", agentstest.Created},
		{"2026-09-28T11:00:00+03:00", agentstest.Created},
		{"2026-09-28T08:00:00.500Z", agentstest.Created + 0.5},
		{float64(agentstest.Created) * 1000, agentstest.Created},
		{float64(agentstest.Created), agentstest.Created},
		{"не время", 7},
		{true, 7},
		{nil, 7},
	}
	for _, tc := range cases {
		if got := timestamp(tc.in, 7); got != tc.want {
			t.Errorf("timestamp(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
