package agents

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

// Grok CLI: $GROK_HOME/sessions/**/summary.json, переписка в chat_history.jsonl рядом,
// запущенные сессии в $GROK_HOME/active_sessions.json.

func newGrok(c Config) Agent {
	b := &base{cfg: c, spec: spec{
		Info: Info{ID: "grok", Name: "Grok", Short: "Gr", HomeEnv: "GROK_HOME", HomeDefault: ".grok",
			Programs: []string{"grok"}, SkipFlag: "--always-approve", Resumable: true, Runner: true, PlainJournal: true},
		resume:      []string{"grok", "--resume", "{id}"},
		sessionFlag: "--session-id",
		promptArg:   true,
		choice: func(model, effort string) []string {
			return flags("-m", model, "--reasoning-effort", effort)
		},
		models:   grokModels,
		headless: grokHeadless,
		live:     grokLive,
	}}
	return &jsonlAgent{
		base: b,
		discover: func(home string) []string {
			return walk(filepath.Join(home, "sessions"), "summary.json")
		},
		journal: func(unit string) string {
			return filepath.Join(filepath.Dir(unit), "chat_history.jsonl")
		},
		message: func(row map[string]any) (string, any, any, bool) {
			return str(row["type"]), row["content"], or(row["timestamp"], row["time"]), true
		},
		info: func(unit string, _ func() []map[string]any) (map[string]any, bool) {
			data := readJSON(unit)
			// Подагенты — не отдельные сессии.
			return data, len(data) > 0 && data["session_kind"] != "subagent"
		},
		describe: func(p *parsed) {
			info := obj(p.info["info"])
			p.id = firstNonEmpty(str(info["id"]), filepath.Base(filepath.Dir(p.unit)))
			p.cwd = str(info["cwd"])
			p.title = firstNonEmpty(cleanTitle(or(p.info["generated_title"], p.info["session_summary"])), p.title)
			p.branch = str(p.info["head_branch"])
			p.updated = timestamp(or(p.info["last_active_at"], p.info["updated_at"]), p.updated)
			p.created = timestamp(p.info["created_at"], p.created)
			p.auto = p.info["session_kind"] == "headless"
			if p.auto {
				p.by = "grok headless"
			}
		},
		validID: isUUID,
	}
}

func grokHeadless(program string, o HeadlessOptions) ([]string, string, error) {
	if o.Session != "" {
		return []string{program, "-p", o.Prompt, "--resume", o.Session, "--sandbox", "read-only",
			"--output-format", "streaming-messages-json"}, "", nil
	}
	prompt, err := shellPrompt(o.Instructions, o.Prompt)
	if err != nil {
		return nil, "", err
	}
	return []string{program, "-p", prompt, "--sandbox", "read-only", "--output-format", "streaming-messages-json"}, "", nil
}

// grokModels спрашивает список у самой программы: «grok models».
func grokModels(b *base) Models {
	models := Models{Efforts: []string{"low", "medium", "high", "xhigh"}}
	program := b.Program()
	if program == "" {
		return models
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, _ := exec.CommandContext(ctx, program, "models").Output()
	for line := range strings.Lines(string(output)) {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Default model:"); ok {
			models.DefaultModel = strings.TrimSpace(rest)
		} else if strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- ") {
			if id, _, _ := strings.Cut(line[2:], " "); strings.TrimSpace(id) != "" {
				id = strings.TrimSpace(id)
				models.Models = append(models.Models, Model{ID: id, Name: id})
			}
		}
	}
	return models
}

// grokLive читает реестр active_sessions.json. Формат не описан; в grok 1.0.40 у записи
// четыре поля: session_id, pid, cwd, opened_at. Поля «ждёт / работает» нет, поэтому живая
// запись — это «работает» с пометкой Guess. Короткий grok -p реестр не заполняет.
func grokLive(b *base, has func(string) bool) []LiveStatus {
	records, _ := readSmallJSON(filepath.Join(b.Home(), "active_sessions.json")).([]any)
	var found []LiveStatus
	for _, raw := range records {
		record := obj(raw)
		sid := str(field(record, "session_id", "sessionId"))
		if sid == "" {
			continue
		}
		key := "grok:" + sid
		if !has(key) || !processAlive(field(record, "pid"), nil) {
			continue
		}
		since, _ := unixSeconds(field(record, "opened_at", "openedAt"))
		found = append(found, LiveStatus{Key: key, State: model.StateWorking, Where: "terminal", Since: unixTime(since), Guess: true})
	}
	return found
}

// field — значение первого из полей, которое есть в записи.
func field(record map[string]any, names ...string) any {
	for _, name := range names {
		if v, ok := record[name]; ok {
			return v
		}
	}
	return nil
}
