package summary

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
)

func TestPeriod(t *testing.T) {
	now := time.Date(2026, 10, 15, 13, 30, 0, 0, time.Local)
	from, to := Period(3, now)
	if !from.Equal(time.Date(2026, 10, 13, 0, 0, 0, 0, time.Local)) || !to.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("period = %v – %v", from, to)
	}
	edge := model.Session{Updated: from}
	before := model.Session{Updated: from.Add(-time.Second)}
	if !InPeriod(edge, 3, now) || InPeriod(before, 3, now) || !InPeriod(before, 7, now) {
		t.Fatal("in period")
	}
	for days, ok := range map[int]bool{3: true, 4: false, 5: true, 6: false, 7: true, 0: false} {
		if ValidDays(days) != ok {
			t.Errorf("valid days %d", days)
		}
	}
	if daysRu(3) != "3 дня" || daysRu(5) != "5 дней" || daysRu(7) != "7 дней" || daysRu(21) != "21 день" || daysRu(12) != "12 дней" {
		t.Fatal("days in Russian")
	}
}

func TestPeriodTitle(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }
	for _, c := range []struct {
		from, to time.Time
		lang     string
		want     string
	}{
		{day(2026, 10, 13), day(2026, 10, 15), "ru", "Итоги за 13–15 октября"},
		{day(2026, 9, 29), day(2026, 10, 5), "ru", "Итоги за 29 сентября – 5 октября"},
		{day(2025, 12, 29), day(2026, 1, 4), "ru", "Итоги за 29 декабря 2025 – 4 января 2026"},
		{day(2026, 10, 13), day(2026, 10, 15), "en", "Summary for October 13–15"},
		{day(2026, 9, 29), day(2026, 10, 5), "en", "Summary for September 29 – October 5"},
		{day(2025, 12, 29), day(2026, 1, 4), "en", "Summary for December 29, 2025 – January 4, 2026"},
	} {
		if got := periodTitle(c.from, c.to, c.lang); got != c.want {
			t.Errorf("%s: %q, want %q", c.want, got, c.want)
		}
	}
}

// Выдержка — первый запрос человека и три последних сообщения, не длиннее бюджета.
func TestExcerpt(t *testing.T) {
	claude := agents.New("claude", agents.Config{})
	sess := claudeSession(t, "44444444-4444-4444-8444-444444444444", "/home/u/p", time.Now(),
		[2]string{"user", "Первое сообщение"}, [2]string{"assistant", "Первый ответ"},
		[2]string{"user", "Второй вопрос"}, [2]string{"assistant", "Итоговый ответ"})
	got := excerpt(claude, sess)
	if !strings.HasPrefix(got, "начало: Первое сообщение … конец: assistant: Первый ответ | user: Второй вопрос | assistant: Итоговый ответ") {
		t.Fatalf("excerpt = %q", got)
	}
	long := strings.Repeat("слово ", 2000)
	sess = claudeSession(t, "55555555-5555-4555-8555-555555555555", "/home/u/p", time.Now(),
		[2]string{"user", long}, [2]string{"assistant", long})
	if got := excerpt(claude, sess); len([]rune(got)) > excerptBudget+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long excerpt: %d runes", len([]rune(got)))
	}
	if excerpt(nil, sess) != "" {
		t.Fatal("excerpt without an agent")
	}
}

func TestParseDigest(t *testing.T) {
	names := map[string]string{"backend": "/home/u/backend", "shop-api": "/home/u/shop-api"}
	paths := map[string]string{"claude:a": "/home/u/backend", "codex:b": "/home/u/backend", "kimi:c": "/home/u/shop-api"}
	answer := "ЗАМЕТКА backend: прочитал\n```json\n" + mustJSON(map[string]any{
		"lead": "Два проекта,\n три сессии. [Ждёт](kimi:c) ответа.",
		"projects": []any{
			map[string]any{"name": "backend", "items": []any{
				map[string]any{"text": "[BACKEND-1](claude:a): лимиты, `main`, **влит**.", "keys": []any{"codex:b", "ghost:x", 5, "codex:b"}},
				map[string]any{"text": "[BACKEND-2](ghost:y): чужая ссылка.", "keys": nil},
				map[string]any{"text": "  ", "keys": []any{"claude:a"}},
			}, "left": "Ревью [BACKEND-3](codex:b) нашло ошибку в `invoice.go`."},
			map[string]any{"name": "Shop-API", "items": []any{map[string]any{"text": "Поиск по каталогу.", "keys": []any{"kimi:c"}}}},
			map[string]any{"name": "мобильное", "items": []any{map[string]any{"text": "Падение на Android.", "keys": []any{"kimi:c"}}}},
			map[string]any{"name": "пустой", "items": []any{}, "left": ""},
			map[string]any{"items": []any{map[string]any{"text": "без имени"}}},
		},
	}) + "\n```"
	d, err := parseDigest(answer, names, paths)
	if err != nil {
		t.Fatal(err)
	}
	if d.Lead != "Два проекта, три сессии. [Ждёт](kimi:c) ответа." {
		t.Fatalf("lead = %q", d.Lead)
	}
	want := []Project{
		{Name: "backend", Path: "/home/u/backend", Items: []Item{
			{Text: "[BACKEND-1](claude:a): лимиты, `main`, **влит**.", Keys: []string{"codex:b", "claude:a"}},
			{Text: "BACKEND-2: чужая ссылка.", Keys: []string{}},
		}, Left: "Ревью [BACKEND-3](codex:b) нашло ошибку в `invoice.go`."},
		{Name: "Shop-API", Path: "/home/u/shop-api", Items: []Item{{Text: "Поиск по каталогу.", Keys: []string{"kimi:c"}}}},
		{Name: "мобильное", Path: "/home/u/shop-api", Items: []Item{{Text: "Падение на Android.", Keys: []string{"kimi:c"}}}},
	}
	if !reflect.DeepEqual(d.Projects, want) {
		t.Fatalf("projects = %+v", d.Projects)
	}
	for name, answer := range map[string]string{
		"not json":     "не json",
		"no lead":      `{"projects": [{"name": "p", "items": [{"text": "t"}]}]}`,
		"not list":     `{"lead": "x", "projects": "nope"}`,
		"no projects":  `{"lead": "x", "projects": []}`,
		"empty itself": `{"lead": "x", "projects": [{"name": "p", "items": [], "left": " "}]}`,
	} {
		if _, err := parseDigest(answer, names, paths); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Сводка за период через каждого агента: файл со списком сессий, запрос и проверенный ответ.
func TestMakeDigestAllRunners(t *testing.T) {
	now := time.Now()
	claude := agents.New("claude", agents.Config{})
	a := claudeSession(t, "aaaaaaaa-0000-4000-8000-000000000001", "/home/u/backend", now.Add(-time.Hour),
		[2]string{"user", "BACKEND-10300: лимиты запросов"}, [2]string{"assistant", "MR !482 влит"})
	a.Branch = "BACKEND-10300"
	b := claudeSession(t, "bbbbbbbb-0000-4000-8000-000000000002", "/home/u/backend", now.Add(-2*time.Hour),
		[2]string{"user", "Вебхуки"}, [2]string{"assistant", "MR !479 открыт"})
	c := claudeSession(t, "cccccccc-0000-4000-8000-000000000003", "/home/u/shop", now,
		[2]string{"user", "Ревью промокодов"}, [2]string{"assistant", "Скидка применяется до налога. Что делаем?"})
	in := DigestInput{Days: 3, Now: now, Sessions: []DigestSession{
		{Session: b, Source: claude, Path: "/home/u/backend", Title: "Вебхуки для партнёров",
			Summary: &store.Summary{Title: "Вебхуки", Summary: "Подпись HMAC.", NextStep: "Показать журнал доставок"}},
		{Session: a, Source: claude, Path: "/home/u/backend", Done: true},
		{Session: c, Source: claude, Path: "/home/u/shop", State: model.StateWaiting},
	}}
	answer := mustJSON(map[string]any{
		"lead": "Два проекта, три сессии.",
		"projects": []any{
			map[string]any{"name": "backend", "items": []any{
				map[string]any{"text": "[BACKEND-10300](" + a.Key() + "): лимиты, MR !482 влит в `main`.", "keys": []any{a.Key()}},
				map[string]any{"text": "Вебхуки: подпись HMAC.", "keys": []any{b.Key(), "ghost:x"}},
			}, "left": "Показать журнал доставок в админке."},
			map[string]any{"name": "shop", "items": []any{map[string]any{"text": "Ревью промокодов. **Codex ждёт ответа.**", "keys": []any{c.Key()}}}, "left": ""},
		},
	})
	for _, id := range []string{"kimi", "claude", "codex", "grok"} {
		t.Run(id, func(t *testing.T) {
			st := newStub(t, id, modeOK, answer)
			run := newRun(t, id, st.program)
			d, err := MakeDigest(context.Background(), nil, run, in)
			if err != nil {
				t.Fatal(err)
			}
			from, to := Period(3, now)
			if d.Days != 3 || d.From != from.Format(time.DateOnly) || d.To != to.Format(time.DateOnly) ||
				d.Title != periodTitle(from, to, "ru") || d.Journals != 3 || d.Model != id || d.Lang != "ru" ||
				d.At < now.Unix() || d.Legacy {
				t.Fatalf("digest = %+v", d)
			}
			if len(d.Projects) != 2 || d.Projects[0].Path != "/home/u/backend" || d.Projects[1].Path != "/home/u/shop" ||
				d.Projects[0].Left != "Показать журнал доставок в админке." ||
				!reflect.DeepEqual(d.Projects[0].Items[1].Keys, []string{b.Key()}) {
				t.Fatalf("projects = %+v", d.Projects)
			}

			input := st.read(t, inputFile)
			// Проекты по последней активности: shop свежее backend.
			if strings.Index(input, "## Проект shop") > strings.Index(input, "## Проект backend") {
				t.Fatalf("project order:\n%s", input)
			}
			for _, want := range []string{
				"Сессий: 3, проектов: 2.", "## Проект backend\nПапка: /home/u/backend",
				"- ключ: " + a.Key() + "\n  агент: Claude Code\n  название: Сессия " + a.ID + "\n  ветка: BACKEND-10300",
				"человек отметил сессию завершённой",
				"название: Вебхуки для партнёров",
				"готовая сводка: Вебхуки. Подпись HMAC. Следующий шаг: Показать журнал доставок",
				"выдержка: начало: Ревью промокодов … конец: user: Ревью промокодов | assistant: Скидка применяется до налога. Что делаем?",
				"сейчас: ждёт ответа человека",
				"журнал: " + c.Journal + " (",
			} {
				if !strings.Contains(input, want) {
					t.Fatalf("input has no %q:\n%s", want, input)
				}
			}
			prompt := st.read(t, "argv") + st.read(t, "stdin")
			if !strings.Contains(prompt, "Составь итоги за 3 дня") || !strings.Contains(prompt, inputFile) ||
				!strings.Contains(prompt, "на русском языке") {
				t.Fatalf("prompt = %s", prompt)
			}
			if id == "kimi" || id == "claude" {
				argv := st.read(t, "argv")
				for _, s := range in.Sessions {
					if !strings.Contains(argv, "--add-dir\n"+dirOf(s.Session.Journal)) {
						t.Fatalf("argv has no folder of %s:\n%s", s.Session.Key(), argv)
					}
				}
			}
		})
	}
}

func dirOf(path string) string { return path[:strings.LastIndex(path, "/")] }

func TestMakeDigestErrors(t *testing.T) {
	now := time.Now()
	var e *Error
	_, err := MakeDigest(context.Background(), nil, Run{Lang: "en", Dir: t.TempDir()}, DigestInput{Days: 3, Now: now})
	if !errors.As(err, &e) || e.Text != "There are no sessions with messages in this period" {
		t.Fatalf("no sessions: %v", err)
	}
	sess := claudeSession(t, "dddddddd-0000-4000-8000-000000000004", "/home/u/p", now, [2]string{"user", "Привет"})
	in := DigestInput{Days: 5, Now: now, Sessions: []DigestSession{{Session: sess, Path: "/home/u/p"}}}
	_, err = MakeDigest(context.Background(), nil, newRun(t, "codex", newStub(t, "codex", modeGarbage, "").program), in)
	if !errors.As(err, &e) || e.Text != "Codex вернул неверный ответ. Попробуйте ещё раз." || e.Output == "" {
		t.Fatalf("garbage: %#v", err)
	}
	_, err = MakeDigest(context.Background(), nil, newRun(t, "grok", newStub(t, "grok", modeFail, "").program), in)
	if !errors.As(err, &e) || e.Text != "Grok завершился с кодом 2" || !strings.Contains(e.Output, "boom") {
		t.Fatalf("fail: %#v", err)
	}
}

func TestDecode(t *testing.T) {
	if d, err := Decode(nil, 3, "ru", nil); d != nil || err != nil {
		t.Fatal(d, err)
	}
	if _, err := Decode(json.RawMessage(`{"days": "три"}`), 3, "ru", nil); err == nil {
		t.Fatal("bad digest decoded")
	}
	// Новая сводка читается как записана.
	d := Digest{Days: 7, From: "2026-10-09", To: "2026-10-15", Title: "Итоги за 9–15 октября", Lead: "Лид",
		Projects: []Project{{Name: "backend", Path: "/b", Items: []Item{{Text: "Пункт", Keys: []string{"claude:a"}}}}},
		Journals: 4, At: 1792000000, Model: "kimi", Lang: "ru"}
	raw, _ := json.Marshal(d)
	got, err := Decode(raw, 7, "en", nil)
	if err != nil || !reflect.DeepEqual(*got, d) {
		t.Fatalf("decoded = %+v, %v", got, err)
	}

	// Сводка первой версии: пункты из bullets, хвосты — к проекту той же сессии или папки.
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.Local)
	v1 := `{"at": ` + jsonNumber(at) + `, "model": "grok", "result": {"lead": "Итоги <b>", "projects": [
		{"name": "backend", "bullets": [{"text": "Сделано", "keys": ["claude:a"]}, {"text": " ", "keys": []}]},
		{"name": "shop", "bullets": [{"text": "Поиск", "keys": ["kimi:c"]}]},
		{"name": "пусто", "bullets": []}],
		"tails": [{"id": "t1", "text": "Влить", "key": "claude:a"}, {"id": "t2", "text": "Проверить индекс!", "key": "kimi:z"},
			{"id": "t3", "text": "Ничьё", "key": "codex:q"}],
		"autos": [{"key": "codex:r", "project": "backend", "text": "Ревью"}]}}`
	paths := map[string]string{"claude:a": "/home/u/backend", "kimi:c": "/home/u/shop", "kimi:z": "/home/u/shop"}
	old, err := Decode(json.RawMessage(v1), 3, "ru", func(key string) string { return paths[key] })
	if err != nil {
		t.Fatal(err)
	}
	want := &Digest{Days: 3, From: "2026-09-26", To: "2026-09-28", Title: "Итоги за 26–28 сентября", Lead: "Итоги <b>",
		Projects: []Project{
			{Name: "backend", Path: "/home/u/backend", Items: []Item{{Text: "Сделано", Keys: []string{"claude:a"}}}, Left: "Влить."},
			{Name: "shop", Path: "/home/u/shop", Items: []Item{{Text: "Поиск", Keys: []string{"kimi:c"}}}, Left: "Проверить индекс!"},
		},
		At: at.Unix(), Model: "grok", Lang: "ru", Legacy: true}
	if !reflect.DeepEqual(old, want) {
		t.Fatalf("legacy = %+v\nwant     %+v", old, want)
	}
}

func jsonNumber(t time.Time) string {
	raw, _ := json.Marshal(float64(t.UnixMilli()) / 1000)
	return string(raw)
}
