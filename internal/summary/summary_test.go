package summary

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
)

// summaryAnswer — ответ модели, как у заглушек первой версии: related с лишними и чужими ключами.
var summaryAnswer = "Вот сводка:\n```json\n" + mustJSON(map[string]any{
	"title":     "Тест. Сводка по сессии",
	"summary":   "Суть задачи и подтверждённый результат из журнала.",
	"next_step": "Проверить результат вручную",
	"closed":    true,
	"related": []any{
		map[string]any{"key": "kimi:aaa", "why": "та же тема"},
		map[string]any{"key": "kimi:bbb", "why": "похожий запрос, очень длинное пояснение, которое точно длиннее сорока символов"},
		map[string]any{"key": "kimi:aaa", "why": "повтор"},
		map[string]any{"key": "ghost:x", "why": "ключа нет в списке"},
		map[string]any{"key": "kimi:ccc", "why": "третья связь"},
		map[string]any{"key": "kimi:ddd", "why": "четвёртая связь"},
	},
}) + "\n```"

var candidates = []Candidate{{"kimi:aaa", "Сессия A"}, {"kimi:bbb", "Сессия B"}, {"kimi:ccc", "Сессия C"},
	{"kimi:ddd", "Сессия D"}}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestParseSummary(t *testing.T) {
	valid := map[string]bool{"kimi:aaa": true, "kimi:bbb": true, "kimi:ccc": true, "kimi:ddd": true}
	sum, err := parseSummary(summaryAnswer, valid)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Title != "Тест. Сводка по сессии" || !sum.Closed || sum.NextStep != "Проверить результат вручную" {
		t.Fatalf("summary = %+v", sum)
	}
	var keys []string
	for _, r := range sum.Related {
		keys = append(keys, r.Key)
		if len([]rune(r.Why)) > whyMax {
			t.Fatalf("why is too long: %q", r.Why)
		}
	}
	if !slices.Equal(keys, []string{"kimi:aaa", "kimi:bbb", "kimi:ccc"}) {
		t.Fatalf("related = %v", keys)
	}

	// Слова вокруг объекта, длинное название, шаг не строкой.
	long := strings.Repeat("я", 120)
	sum, err = parseSummary(`Готово. {"title": "`+long+`", "summary": " Текст ", "next_step": 5, "related": "нет"} Конец.`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(sum.Title)) != titleMax || sum.Summary != "Текст" || sum.NextStep != "5" || sum.Closed ||
		sum.Related == nil || len(sum.Related) != 0 {
		t.Fatalf("summary = %+v", sum)
	}

	for name, answer := range map[string]string{
		"not json":      "не удалось разобрать журнал",
		"array":         `[{"title": "a", "summary": "b"}]`,
		"no title":      `{"summary": "b"}`,
		"empty summary": `{"title": "a", "summary": "  "}`,
		"not strings":   `{"title": 1, "summary": "b"}`,
	} {
		if _, err := parseSummary(answer, nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Сводка через каждого из четырёх агентов: запрос, ход работы и ответ, а у Codex — из файла.
func TestSummarizeAllRunners(t *testing.T) {
	sess := claudeSession(t, "11111111-1111-4111-8111-111111111111", "/home/u/projects/shop", time.Now(),
		[2]string{"user", "Добавь поиск по каталогу"}, [2]string{"assistant", "Готово, индекс pg_trgm."})
	sess.Branch = "SHOP-1"
	for _, id := range []string{"kimi", "claude", "codex", "grok"} {
		t.Run(id, func(t *testing.T) {
			st := newStub(t, id, modeOK, summaryAnswer)
			run := newRun(t, id, st.program)
			rec := &recorder{}
			jobs := NewJobs(rec.add)
			var got store.Summary
			job, started, err := jobs.Start(Target{Kind: KindSummary, Key: sess.Key()}, id, func(ctx context.Context, j *Job) error {
				var err error
				got, err = Summarize(ctx, j, run, SessionInput{Session: sess, Source: agents.New("claude", agents.Config{}),
					Title: "Своё название", Candidates: candidates})
				return err
			})
			if err != nil || !started {
				t.Fatal(started, err)
			}
			jobs.Wait()
			if final := job.Status(); final.Stage != StageDone || final.Error != "" {
				t.Fatalf("final = %+v", final)
			}
			if got.Title != "Тест. Сводка по сессии" || got.Model != id || got.At < float64(time.Now().Add(-time.Minute).Unix()) ||
				len(got.Related) != 3 {
				t.Fatalf("summary = %+v", got)
			}
			var stages []string
			for _, s := range rec.all() {
				if len(stages) == 0 || stages[len(stages)-1] != s.Stage {
					stages = append(stages, s.Stage)
				}
			}
			if stages[0] != StageStarting || !slices.Contains(stages, StageReading) || stages[len(stages)-1] != StageDone ||
				stages[len(stages)-2] != StageWriting {
				t.Fatalf("stages = %v", stages)
			}
			if reads := rec.last(job.ID()).Reads; reads != 2 {
				t.Fatalf("reads = %d", reads)
			}

			argv := st.read(t, "argv")
			prompt := argv + st.read(t, "stdin")
			for _, want := range []string{"Инструмент: Claude Code", "Проект: shop", "Текущее название: Своё название",
				"Ветка: SHOP-1", "Путь к журналу: " + sess.Journal, "- kimi:aaa — Сессия A", "на русском языке"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt has no %q:\n%s", want, prompt)
				}
			}
			cwd := strings.TrimSpace(st.read(t, "cwd"))
			if filepath.Dir(cwd) != run.Dir || !strings.Contains(filepath.Base(cwd), "-summary-") {
				t.Fatalf("cwd = %s", cwd)
			}
			switch id {
			case "kimi":
				// Kimi получает файл инструкции и папку журнала.
				if !strings.Contains(argv, "--agent-file\n"+filepath.Join(cwd, "summary-agent.md")) ||
					!strings.Contains(argv, "--add-dir\n"+filepath.Dir(sess.Journal)) {
					t.Fatalf("argv = %s", argv)
				}
			case "claude", "codex":
				// Инструкция без шапки YAML — в начале stdin.
				stdin := st.read(t, "stdin")
				if !strings.HasPrefix(stdin, "Ты составляешь краткую сводку") {
					t.Fatalf("stdin = %.200s", stdin)
				}
			}
		})
	}
}

// Ошибки агента: код выхода с хвостом stderr, неверный ответ, предел времени.
func TestSummarizeErrors(t *testing.T) {
	sess := claudeSession(t, "22222222-2222-4222-8222-222222222222", "/home/u/p", time.Now(), [2]string{"user", "Привет"})
	summarize := func(run Run) error {
		_, err := Summarize(context.Background(), nil, run, SessionInput{Session: sess})
		return err
	}
	var e *Error
	err := summarize(newRun(t, "claude", newStub(t, "claude", modeFail, "").program))
	if !errors.As(err, &e) || e.Text != "Claude Code завершился с кодом 2" || !strings.Contains(e.Output, "boom") {
		t.Fatalf("fail: %#v", err)
	}
	err = summarize(newRun(t, "grok", newStub(t, "grok", modeGarbage, "").program))
	if !errors.As(err, &e) || !strings.Contains(e.Text, "без названия или сводки") || !strings.Contains(e.Output, "не удалось разобрать") {
		t.Fatalf("garbage: %#v", err)
	}
	run := newRun(t, "kimi", newStub(t, "kimi", modeHang, "").program)
	run.Timeout, run.Lang = 300*time.Millisecond, "en"
	start := time.Now()
	err = summarize(run)
	if !errors.As(err, &e) || e.Text != "Kimi didn't answer in 1 s" {
		t.Fatalf("timeout: %#v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the agent was not killed: %v", time.Since(start))
	}
	// Ответ без единого текста.
	err = summarize(newRun(t, "kimi", newStub(t, "kimi", modeOK, "").program))
	if !errors.As(err, &e) || e.Text != "Kimi вернул пустой ответ" {
		t.Fatalf("empty: %#v", err)
	}
	// Программы нет.
	missing := Run{Runner: agents.New("kimi", agents.Config{Program: "/nonexistent/kimi"}), Lang: "ru", Dir: t.TempDir()}
	if err := summarize(missing); !errors.As(err, &e) || !strings.HasPrefix(e.Text, "Не удалось запустить Kimi") {
		t.Fatalf("missing: %#v", err)
	}
}

// Отмена убивает агента сразу и не даёт результата.
func TestSummarizeCancel(t *testing.T) {
	sess := claudeSession(t, "33333333-3333-4333-8333-333333333333", "/home/u/p", time.Now(), [2]string{"user", "Привет"})
	run := newRun(t, "claude", newStub(t, "claude", modeHang, "").program)
	rec := &recorder{}
	jobs := NewJobs(rec.add)
	saved := false
	job, _, _ := jobs.Start(Target{Kind: KindSummary, Key: sess.Key()}, "claude", func(ctx context.Context, j *Job) error {
		if _, err := Summarize(ctx, j, run, SessionInput{Session: sess}); err != nil {
			return err
		}
		saved = true
		return nil
	})
	waitFor(t, "agent reads", func() bool { return rec.last(job.ID()).Stage == StageReading })
	start := time.Now()
	if !jobs.Cancel(job.ID()) {
		t.Fatal("not cancelled")
	}
	jobs.Wait()
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("the agent was not killed: %v", time.Since(start))
	}
	if final := rec.last(job.ID()); final.Stage != StageCancelled || !final.Done || final.Error != "" || saved {
		t.Fatalf("final = %+v, saved %v", final, saved)
	}
	if jobs.Cancel(job.ID()) {
		t.Fatal("a finished job was cancelled")
	}
}

// fakeSQLite — агент, чей журнал нельзя дать модели как есть (как ZCode): переписка уходит
// в файл рабочей папки.
type fakeSQLite struct {
	agents.Agent
}

func (fakeSQLite) Info() agents.Info { return agents.Info{ID: "zcode", Name: "ZCode"} }

func (fakeSQLite) Transcript(model.Session, int64) ([]model.Message, bool) {
	return []model.Message{{Role: "user", Text: "Почини сборку"}, {Role: "assistant", Text: "Починил."}}, false
}

func TestSummarizeExportsTranscript(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db.sqlite")
	os.WriteFile(db, []byte("sqlite"), 0o644)
	sess := model.Session{Agent: "zcode", ID: "z1", Title: "Сборка", Cwd: "/home/u/p", Journal: db}
	st := newStub(t, "kimi", modeOK, summaryAnswer)
	run := newRun(t, "kimi", st.program)
	if _, err := Summarize(context.Background(), nil, run, SessionInput{Session: sess, Source: fakeSQLite{}}); err != nil {
		t.Fatal(err)
	}
	argv := st.read(t, "argv")
	cwd := strings.TrimSpace(st.read(t, "cwd"))
	exported := filepath.Join(cwd, transcriptFile)
	if !strings.Contains(argv, "Путь к журналу: "+exported) || strings.Contains(argv, "--add-dir") {
		t.Fatalf("argv = %s", argv)
	}
	data, err := os.ReadFile(exported)
	if err != nil || !strings.Contains(string(data), "## user\nПочини сборку") || !strings.Contains(string(data), "## assistant\nПочинил.") {
		t.Fatalf("transcript = %q, %v", data, err)
	}
}

func TestOldRunDirsAreRemoved(t *testing.T) {
	run := Run{Lang: "ru", Dir: t.TempDir()}
	old := filepath.Join(run.Dir, "20200101-000000-summary-old")
	os.Mkdir(old, 0o755)
	stamp := time.Now().Add(-runsKeep - time.Hour)
	os.Chtimes(old, stamp, stamp)
	dir, err := run.workdir(KindSummary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("an old run folder is kept")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal(err)
	}
}
