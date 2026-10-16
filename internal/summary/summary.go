// Package summary пишет сводки агентом без человека: по одной сессии (о чём она и на чём
// остановилась) и за несколько дней (итоги по проектам и что осталось).
//
// Агенту дают инструкцию (summary-agent.md или digest-agent.md), короткий запрос и папки
// журналов только для чтения. Он читает журналы сам и возвращает JSON; пакет разбирает его
// поток ради хода работы, проверяет ответ и отдаёт результат. Сохраняет результат вызывающий.
package summary

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
)

var (
	//go:embed summary-agent.md
	summaryAgent string
	//go:embed digest-agent.md
	digestAgent string
)

const (
	titleMax      = 90
	summaryMax    = 12000
	whyMax        = 40
	relatedMax    = 3
	candidatesMax = 30
	// transcriptFile — переписка текстом в рабочей папке, если журнал агента нельзя дать
	// модели как есть (ZCode и Cursor Agent хранят его в SQLite).
	transcriptFile = "session.txt"
)

// Candidate — сессия того же проекта, которую модель может назвать связанной.
type Candidate struct {
	Key   string
	Title string
}

// SessionInput — что нужно для сводки по сессии.
type SessionInput struct {
	Session    model.Session
	Source     agents.Agent // агент самой сессии: по нему выгружается переписка
	Title      string       // своё название человека; пусто — из журнала
	Candidates []Candidate  // до 30 недавних сессий того же проекта
}

// Summarize пишет сводку по сессии. Ход работы уходит в job; при отмене ctx возвращает ошибку
// контекста.
func Summarize(ctx context.Context, job *Job, run Run, in SessionInput) (store.Summary, error) {
	dir, err := run.workdir(KindSummary)
	if err != nil {
		return store.Summary{}, err
	}
	journal := in.Session.Journal
	var addDirs []string
	if in.Source != nil && !in.Source.Info().PlainJournal {
		journal = filepath.Join(dir, transcriptFile)
		if err := exportTranscript(in.Source, in.Session, journal); err != nil {
			return store.Summary{}, &Error{Text: text(run.Lang, "no_transcript", "", err.Error())}
		}
	} else {
		addDirs = []string{filepath.Dir(journal)}
	}
	answer, err := run.exec(ctx, job, dir, "summary-agent.md", summaryAgent, summaryPrompt(in, journal, run.Lang), addDirs)
	if err != nil {
		return store.Summary{}, err
	}
	valid := map[string]bool{}
	for _, c := range in.Candidates {
		valid[c.Key] = true
	}
	sum, err := parseSummary(answer, valid)
	if err != nil {
		return store.Summary{}, &Error{Text: text(run.Lang, "bad_summary", run.name(), ""), Output: clip(answer, tailChars)}
	}
	sum.At = float64(time.Now().UnixMilli()) / 1000
	sum.Model = run.Runner.ID()
	return sum, nil
}

// summaryPrompt — запрос к агенту, как в первой версии: что за сессия, где журнал и с какими
// сессиями её можно связать.
func summaryPrompt(in SessionInput, journal, lang string) string {
	s := in.Session
	name := s.Agent
	if in.Source != nil {
		name = in.Source.Info().Name
	}
	title := in.Title
	if title == "" {
		title = s.Title
	}
	lines := []string{
		"Инструмент: " + name,
		"Проект: " + s.Project(),
		"Текущее название: " + title,
		"Исходное название: " + s.Title,
	}
	if s.Branch != "" {
		lines = append(lines, "Ветка: "+s.Branch)
	}
	lines = append(lines, "Путь к журналу: "+journal)
	if len(in.Candidates) > 0 {
		lines = append(lines, "Сессии того же проекта за последние 30 дней (для поля related — только из этого списка):")
		for i, c := range in.Candidates {
			if i == candidatesMax {
				break
			}
			lines = append(lines, "- "+c.Key+" — "+oneLine(c.Title, 200))
		}
	}
	lines = append(lines, "Прочитай этот журнал и верни сводку по инструкции агента.", languageLine(lang))
	return strings.Join(lines, "\n")
}

// parseSummary проверяет ответ: нужны непустые строки title и summary; related — только ключи
// из valid, не больше трёх, причина до 40 символов.
func parseSummary(answer string, valid map[string]bool) (store.Summary, error) {
	data, err := loadsAnswer(answer)
	if err != nil {
		return store.Summary{}, err
	}
	title, ok1 := data["title"].(string)
	body, ok2 := data["summary"].(string)
	if !ok1 || !ok2 {
		return store.Summary{}, fmt.Errorf("no string fields title and summary")
	}
	title, body = oneLine(title, 1000), strings.TrimSpace(body)
	if title == "" || body == "" {
		return store.Summary{}, fmt.Errorf("title or summary is empty")
	}
	sum := store.Summary{Title: clip(title, titleMax), Summary: clip(body, summaryMax), Related: []store.Related{}}
	switch v := data["next_step"].(type) {
	case string:
		sum.NextStep = strings.TrimSpace(v)
	case nil:
	default:
		sum.NextStep = strings.TrimSpace(fmt.Sprint(v))
	}
	switch v := data["closed"].(type) {
	case bool:
		sum.Closed = v
	case string:
		sum.Closed = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	related, _ := data["related"].([]any)
	seen := map[string]bool{}
	for _, raw := range related {
		item, _ := raw.(map[string]any)
		key, _ := item["key"].(string)
		if !valid[key] || seen[key] {
			continue
		}
		seen[key] = true
		why := str(item["why"])
		if why == "" && item["why"] != nil {
			why = fmt.Sprint(item["why"])
		}
		sum.Related = append(sum.Related, store.Related{Key: key, Why: clip(strings.TrimSpace(why), whyMax)})
		if len(sum.Related) == relatedMax {
			break
		}
	}
	return sum, nil
}

// exportTranscript пишет переписку текстом: для агентов, чей журнал лежит в базе SQLite.
func exportTranscript(a agents.Agent, s model.Session, path string) error {
	messages, _ := a.Transcript(s, agents.MessageBytes)
	var b strings.Builder
	for _, m := range messages {
		b.WriteString("## " + m.Role)
		if !m.At.IsZero() {
			b.WriteString(" · " + m.At.Format(time.RFC3339))
		}
		b.WriteString("\n" + m.Text + "\n\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
