package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/genhoi/ai-threads/internal/model"
)

// Kimi Code CLI: $KIMI_CODE_HOME/sessions/*/session_*/state.json, переписка в
// agents/main/wire.jsonl, история ручного ввода в user-history/*.jsonl.

func newKimi(c Config) Agent {
	b := &base{cfg: c, spec: spec{
		Info: Info{ID: "kimi", Name: "Kimi", Short: "Ki", HomeEnv: "KIMI_CODE_HOME", HomeDefault: ".kimi-code",
			Programs: []string{"kimi"}, SkipFlag: "--auto", Resumable: true, Runner: true, PlainJournal: true},
		fallbacks: []string{"bin/kimi"},
		resume:    []string{"kimi", "--session", "{id}"},
		// Запрос аргументом kimi принимает только с -p, без интерфейса.
		promptArg: false,
		choice: func(model, _ string) []string {
			return flags("--model", model)
		},
		models:   kimiModels,
		headless: kimiHeadless,
	}}
	return &jsonlAgent{
		base: b,
		discover: func(home string) []string {
			return glob(home, "sessions/*/session_*/state.json")
		},
		journal: func(unit string) string {
			return filepath.Join(filepath.Dir(unit), "agents", "main", "wire.jsonl")
		},
		message: kimiMessage,
		info: func(unit string, _ func() []map[string]any) (map[string]any, bool) {
			data := readJSON(unit)
			return data, len(data) > 0
		},
		describe: func(p *parsed) {
			sid := firstNonEmpty(str(p.info["id"]), filepath.Base(filepath.Dir(p.unit)))
			if !strings.HasPrefix(sid, "session_") {
				sid = "session_" + sid
			}
			p.id = sid
			p.cwd = firstNonEmpty(str(p.info["cwd"]), str(p.info["workDir"]))
			p.title = firstNonEmpty(cleanTitle(or(p.info["title"], p.info["lastPrompt"])), p.title)
			p.created = timestamp(p.info["createdAt"], p.created)
			p.updated = timestamp(p.info["updatedAt"], p.updated)
			p.entry.Prompts = []string{}
			for _, row := range p.rows {
				if row["type"] == "turn.prompt" {
					if value := prefix(row["input"]); !slices.Contains(p.entry.Prompts, value) {
						p.entry.Prompts = append(p.entry.Prompts, value)
					}
				}
			}
		},
		validID: func(id string) bool {
			return isUUID(strings.TrimPrefix(id, "session_"))
		},
		// Запросы, набранные человеком. Сессия, чьих запросов тут нет, запущена другим агентом.
		indexes: func(home string, c *Cache) map[string]IndexValue {
			history := map[string]IndexValue{}
			read := map[string]bool{}
			for _, path := range glob(home, "user-history/*.jsonl") {
				read[path] = true
				values := c.updateIndex(path, func(values map[string]IndexValue, row map[string]any) {
					if value := prefix(row["content"]); value != "" {
						values[value] = IndexValue{}
					}
				})
				for k, v := range values {
					history[k] = v
				}
			}
			c.keepIndexes(read)
			return history
		},
		finish: func(s *model.Session, e *Entry, history map[string]IndexValue) {
			typed := false
			for _, p := range e.Prompts {
				if _, ok := history[p]; ok && p != "" {
					typed = true
				}
			}
			s.Auto = len(e.Prompts) > 0 && !typed
			s.By = ""
			if s.Auto {
				s.By = "kimi -p"
			}
		},
	}
}

func kimiMessage(row map[string]any) (string, any, any, bool) {
	at := or(row["timestamp"], row["time"])
	switch row["type"] {
	case "agent.message.appended":
		envelope := obj(row["message"])
		message := obj(envelope["message"])
		return str(message["role"]), message["content"], or(obj(envelope["meta"])["createdAt"], at), true
	case "context.append_message":
		// Формат с контекстом: вставки окружения и завершения задач — не от человека.
		message := obj(row["message"])
		if kind := obj(message["origin"])["kind"]; kind != nil && kind != "user" {
			return "", nil, nil, false
		}
		return str(message["role"]), message["content"], at, true
	case "context.append_loop_event":
		event := obj(row["event"])
		if event["type"] == "content.part" && obj(event["part"])["type"] == "text" {
			return "assistant", event["part"], at, true
		}
	}
	return "", nil, nil, false
}

func kimiHeadless(program string, o HeadlessOptions) ([]string, string, error) {
	argv := []string{program}
	// С --session файл агента kimi не принимает: инструкция уже в сессии.
	if o.Instructions != "" && o.Session == "" {
		argv = append(argv, "--agent-file", o.Instructions)
	}
	argv = append(argv, addDirArgs(o.AddDirs)...)
	if o.Session != "" {
		argv = append(argv, "--session", o.Session)
	}
	return append(argv, "--output-format", "stream-json", "--prompt", o.Prompt), "", nil
}

var kimiSection = regexp.MustCompile(`^\[models\."([^"]+)"\]$`)

// kimiModels — модели из разделов [models."…"] файла config.toml.
func kimiModels(b *base) Models {
	path := filepath.Join(b.Home(), "config.toml")
	var ids []string
	names := map[string]string{}
	section := ""
	data, _ := os.ReadFile(path)
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = ""
			if m := kimiSection.FindStringSubmatch(line); m != nil {
				section = m[1]
				ids = append(ids, section)
			}
		} else if section != "" && strings.HasPrefix(line, "display_name") {
			if _, value, ok := strings.Cut(line, "="); ok {
				names[section] = strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
	}
	models := Models{DefaultModel: tomlTopLevel(path)["default_model"]}
	for _, id := range ids {
		models.Models = append(models.Models, Model{ID: id, Name: firstNonEmpty(names[id], id)})
	}
	return models
}
