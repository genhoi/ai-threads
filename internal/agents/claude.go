package agents

import (
	"path/filepath"
	"strings"

	"github.com/genhoi/ai-threads/internal/model"
)

// Claude Code: $CLAUDE_HOME/projects/*/<uuid>.jsonl, история ввода в history.jsonl,
// запущенные сессии в $CLAUDE_HOME/sessions/*.json.

func newClaude(c Config) Agent {
	b := &base{cfg: c, spec: spec{
		Info: Info{ID: "claude", Name: "Claude Code", Short: "Cl", HomeEnv: "CLAUDE_HOME", HomeDefault: ".claude",
			Programs: []string{"claude"}, SkipFlag: "--dangerously-skip-permissions", Resumable: true, Runner: true,
			PlainJournal: true},
		resume:      []string{"claude", "--resume", "{id}"},
		sessionFlag: "--session-id",
		promptArg:   true,
		choice: func(model, effort string) []string {
			return flags("--model", model, "--effort", effort)
		},
		models:   claudeModels,
		headless: claudeHeadless,
		live:     claudeLive,
	}}
	return &jsonlAgent{
		base: b,
		discover: func(home string) []string {
			// Подагенты лежат глубже, в <uuid>/subagents/: этот шаблон их не видит.
			return glob(home, "projects/*/*.jsonl")
		},
		message: func(row map[string]any) (string, any, any, bool) {
			kind := str(row["type"])
			if kind != "user" && kind != "assistant" || truthy(row["isSidechain"]) {
				return "", nil, nil, false
			}
			value := obj(row["message"])["content"]
			if kind == "user" {
				text, ok := claudeUserText(row, value)
				if !ok {
					return "", nil, nil, false
				}
				value = text
			}
			return kind, value, row["timestamp"], true
		},
		info: func(unit string, _ func() []map[string]any) (map[string]any, bool) {
			return map[string]any{}, isUUID(stem(unit))
		},
		describe: func(p *parsed) {
			for _, row := range p.head {
				if row["type"] == "user" {
					p.auto = row["entrypoint"] == "sdk-cli"
					break
				}
			}
			if p.auto {
				p.by = "claude -p"
			}
			for _, row := range p.rows {
				kind := str(row["type"])
				if (kind == "user" || kind == "assistant") && !truthy(row["isSidechain"]) {
					if cwd := str(row["cwd"]); cwd != "" {
						p.cwd = cwd
					}
					if branch := str(row["gitBranch"]); branch != "" {
						p.branch = branch
					}
				}
				if kind == "ai-title" {
					if title := cleanTitle(row["aiTitle"]); title != "" {
						p.entry.AITitle = title
					}
				}
			}
			p.entry.JournalCwd = p.cwd
		},
		validID: isUUID,
		indexes: func(home string, c *Cache) map[string]IndexValue {
			index := c.updateIndex(filepath.Join(home, "history.jsonl"), func(values map[string]IndexValue, row map[string]any) {
				sid, ok := row["sessionId"].(string)
				if !ok {
					return
				}
				// Название — из первой записи сессии, папка — из последней.
				previous, seen := values[sid]
				if !seen {
					previous.Title = cleanTitle(row["display"])
				}
				if project := str(row["project"]); project != "" {
					previous.Cwd = project
				}
				values[sid] = previous
			})
			c.keepIndexes(map[string]bool{filepath.Join(home, "history.jsonl"): true})
			return index
		},
		finish: func(s *model.Session, e *Entry, index map[string]IndexValue) {
			indexed := index[s.ID]
			s.Title = firstNonEmpty(e.AITitle, indexed.Title, e.First)
			s.Cwd = firstNonEmpty(e.JournalCwd, indexed.Cwd)
		},
	}
}

func claudeModels(b *base) Models {
	configured := readJSON(filepath.Join(b.Home(), "settings.json"))
	var models []Model
	for _, alias := range []string{"opus", "sonnet", "fable", "haiku"} {
		models = append(models, Model{ID: alias, Name: alias})
	}
	return Models{DefaultModel: str(configured["model"]), Models: models,
		Efforts: []string{"low", "medium", "high", "xhigh", "max"}}
}

func claudeHeadless(program string, o HeadlessOptions) ([]string, string, error) {
	argv := []string{program, "-p"}
	stdin := o.Prompt
	if o.Session != "" {
		argv = append(argv, "--resume", o.Session)
	} else {
		var err error
		if stdin, err = shellPrompt(o.Instructions, o.Prompt); err != nil {
			return nil, "", err
		}
	}
	argv = append(argv, "--output-format", "stream-json", "--verbose", "--tools", "Read", "--permission-mode", "dontAsk")
	return append(argv, addDirArgs(o.AddDirs)...), stdin, nil
}

// claudeStates — состояние сессии по полю status реестра Claude Code.
var claudeStates = map[string]model.State{
	"idle": model.StateWaiting, "waiting": model.StateWaiting,
	"busy": model.StateWorking, "shell": model.StateWorking,
}

// claudeLive читает реестр $CLAUDE_HOME/sessions/*.json, который Claude Code ведёт сам.
func claudeLive(b *base, has func(string) bool) []LiveStatus {
	var found []LiveStatus
	for _, path := range glob(b.Home(), "sessions/*.json") {
		record, _ := readSmallJSON(path).(map[string]any)
		sid := str(record["sessionId"])
		if sid == "" {
			continue
		}
		key := "claude:" + sid
		state, known := claudeStates[str(record["status"])]
		if !known || !has(key) || !processAlive(record["pid"], record["procStart"]) {
			continue
		}
		since, ok := unixSeconds(record["statusUpdatedAt"])
		if !ok {
			since, ok = unixSeconds(record["updatedAt"])
		}
		var where string
		switch record["kind"] {
		case "interactive":
			where = "terminal"
		case "bg":
			where = "background"
		}
		if !ok || where == "" {
			continue
		}
		found = append(found, LiveStatus{Key: key, State: state, Where: where,
			Name: strings.TrimSpace(str(record["name"])), Since: unixTime(since)})
	}
	return found
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// flags — пары «флаг значение» с непустыми значениями.
func flags(pairs ...string) []string {
	var argv []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			argv = append(argv, pairs[i], pairs[i+1])
		}
	}
	return argv
}
