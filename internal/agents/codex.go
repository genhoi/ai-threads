package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/genhoi/ai-threads/internal/model"
)

// Codex CLI: $CODEX_HOME/sessions/**/rollout-*.jsonl, названия в session_index.jsonl.

func newCodex(c Config) Agent {
	b := &base{cfg: c, spec: spec{
		Info: Info{ID: "codex", Name: "Codex", Short: "Cx", HomeEnv: "CODEX_HOME", HomeDefault: ".codex",
			Programs: []string{"codex"}, SkipFlag: "--dangerously-bypass-approvals-and-sandbox", Resumable: true,
			Runner: true, PlainJournal: true},
		resume:     []string{"codex", "resume", "{id}"},
		resumeAuto: []string{"codex", "exec", "resume", "{id}"},
		promptArg:  true,
		choice: func(model, effort string) []string {
			argv := flags("-m", model)
			if effort != "" {
				argv = append(argv, "-c", `model_reasoning_effort="`+effort+`"`)
			}
			return argv
		},
		models:     codexModels,
		headless:   codexHeadless,
		answerFile: true,
	}}
	return &jsonlAgent{
		base: b,
		discover: func(home string) []string {
			return walk(filepath.Join(home, "sessions"), "rollout-*.jsonl")
		},
		message: func(row map[string]any) (string, any, any, bool) {
			if row["type"] != "response_item" {
				return "", nil, nil, false
			}
			payload := obj(row["payload"])
			if payload["type"] != "message" {
				return "", nil, nil, false
			}
			return str(payload["role"]), payload["content"], row["timestamp"], true
		},
		info: func(unit string, head func() []map[string]any) (map[string]any, bool) {
			if !isUUID(last36(stem(unit))) {
				return nil, false
			}
			meta := map[string]any{}
			for _, row := range head() {
				if row["type"] == "session_meta" {
					if payload := obj(row["payload"]); payload != nil {
						meta = payload
					}
					break
				}
			}
			// Подагенты пишут в source объект с родителем; это не отдельные сессии.
			if _, sub := meta["source"].(map[string]any); sub {
				return nil, false
			}
			return meta, true
		},
		describe: func(p *parsed) {
			p.id = last36(stem(p.unit))
			p.cwd = str(p.info["cwd"])
			p.branch = str(obj(p.info["git"])["branch"])
			p.created = timestamp(p.info["timestamp"], p.created)
			p.auto = p.info["source"] == "exec"
			if p.auto {
				p.by = "codex exec"
			}
		},
		validID: isUUID,
		indexes: func(home string, c *Cache) map[string]IndexValue {
			path := filepath.Join(home, "session_index.jsonl")
			index := c.updateIndex(path, func(values map[string]IndexValue, row map[string]any) {
				if id, ok := row["id"].(string); ok {
					if title := cleanTitle(row["thread_name"]); title != "" {
						values[id] = IndexValue{Title: title}
					}
				}
			})
			c.keepIndexes(map[string]bool{path: true})
			return index
		},
		finish: func(s *model.Session, e *Entry, index map[string]IndexValue) {
			s.Title = firstNonEmpty(index[s.ID].Title, e.First)
		},
	}
}

// last36 — последние 36 символов имени: UUID в rollout-<время>-<uuid>.
func last36(name string) string {
	if len(name) < 36 {
		return name
	}
	return name[len(name)-36:]
}

func codexHeadless(program string, o HeadlessOptions) ([]string, string, error) {
	if o.Session != "" {
		return []string{program, "exec", "resume", o.Session, "--json", "-c", `sandbox_mode="read-only"`,
			"--skip-git-repo-check", "-o", o.OutFile, "-"}, o.Prompt, nil
	}
	stdin, err := shellPrompt(o.Instructions, o.Prompt)
	if err != nil {
		return nil, "", err
	}
	return []string{program, "exec", "--json", "-s", "read-only", "--skip-git-repo-check", "-o", o.OutFile, "-"},
		stdin, nil
}

func codexModels(b *base) Models {
	data := readJSON(filepath.Join(b.Home(), "models_cache.json"))
	var models []Model
	list, _ := data["models"].([]any)
	for _, raw := range list {
		item := obj(raw)
		slug, ok := item["slug"].(string)
		if item == nil || item["visibility"] == "hide" || !ok {
			continue
		}
		var efforts []string
		levels, _ := item["supported_reasoning_levels"].([]any)
		for _, level := range levels {
			if effort, ok := obj(level)["effort"].(string); ok {
				efforts = append(efforts, effort)
			}
		}
		models = append(models, Model{ID: slug, Name: firstNonEmpty(str(item["display_name"]), slug),
			Efforts: efforts, DefaultEffort: str(item["default_reasoning_level"])})
	}
	top := tomlTopLevel(filepath.Join(b.Home(), "config.toml"))
	return Models{DefaultModel: top["model"], DefaultEffort: top["model_reasoning_effort"], Models: models}
}

var tomlString = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*=\s*"([^"]*)"$`)

// tomlTopLevel — строковые значения TOML до первой секции.
func tomlTopLevel(path string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break
		}
		if m := tomlString.FindStringSubmatch(line); m != nil {
			values[m[1]] = m[2]
		}
	}
	return values
}
