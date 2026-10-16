package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/summary"
)

// runnerStub — заглушка агента без человека в формате потока Kimi (role и tool_calls).
// mode: ok — два чтения и ответ answer; fail — stderr и код 2; hang — одно чтение и ожидание,
// пока её не убьют. В папку rec она кладёт аргументы и файл со списком сессий сводки.
func runnerStub(t *testing.T, mode, answer string) (program, rec string) {
	t.Helper()
	dir := t.TempDir()
	rec = filepath.Join(dir, "rec")
	os.Mkdir(rec, 0o755)
	read, _ := json.Marshal(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "t1",
		"type": "function", "function": map[string]any{"name": "Read", "arguments": `{"path":"/x"}`}}}})
	body := "#!/bin/sh\nrec='" + rec + "'\nprintf '%s\\n' \"$@\" > \"$rec/argv\"\n" +
		"if [ -f digest-input.txt ]; then cp digest-input.txt \"$rec/\"; fi\n"
	switch mode {
	case "ok":
		said, _ := json.Marshal(map[string]any{"role": "assistant", "content": answer})
		body += "cat <<'NIT_STUB'\n" + string(read) + "\n" + string(read) + "\n" + string(said) + "\nNIT_STUB\n"
	case "fail":
		body += "echo 'boom: тестовая ошибка заглушки' >&2\nexit 2\n"
	case "hang":
		body += "cat <<'NIT_STUB'\n" + string(read) + "\nNIT_STUB\nexec sleep 30\n"
	}
	program = filepath.Join(dir, "kimi")
	if err := os.WriteFile(program, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, rec
}

// useRunners задаёт программы агентов сводок: id → путь; пустой путь — программы нет.
func (e *env) useRunners(programs map[string]string, change func(*settings.Settings)) {
	e.t.Helper()
	set := e.conf.Get()
	for id, program := range programs {
		a := set.Agents[id]
		a.Program = program
		if program == "" {
			a.Program = filepath.Join(e.t.TempDir(), "missing")
		}
		set.Agents[id] = a
	}
	if change != nil {
		change(&set)
	}
	if err := e.s.applySettings(set); err != nil {
		e.t.Fatal(err)
	}
}

// waitJob ждёт событие job с итогом задания id.
func (st *stream) waitJob(id string) JobView {
	st.t.Helper()
	var job JobView
	st.wait("job", func(data string) bool {
		json.Unmarshal([]byte(data), &job)
		return job.ID == id && job.Done
	})
	return job
}

// waitAll читает события, пока каждое из want не встретится хотя бы раз, в любом порядке.
// Возвращает данные первого подходящего события каждого вида.
func (st *stream) waitAll(want map[string]func(data string) bool) map[string]string {
	st.t.Helper()
	got := map[string]string{}
	deadline := time.After(10 * time.Second)
	for len(got) < len(want) {
		select {
		case ev, open := <-st.events:
			if !open {
				st.t.Fatal("stream closed")
			}
			if ok := want[ev.name]; ok != nil && got[ev.name] == "" && ok(ev.data) {
				got[ev.name] = ev.data
			}
		case <-deadline:
			st.t.Fatalf("timed out waiting for events; got %v", got)
		}
	}
	return got
}

func TestSessionSummary(t *testing.T) {
	key := "claude:" + agentstest.SID(1)
	answer := "```json\n" + `{"title": "Каталог: проверка названий", "summary": "Запись найдена, название верное.",` +
		`"next_step": "Проверить остальные записи", "closed": false,` +
		`"related": [{"key": "codex:` + agentstest.SID(1) + `", "why": "та же задача"}, {"key": "ghost:x", "why": "нет"}]}` + "\n```"
	program, rec := runnerStub(t, "ok", answer)
	e := setup(t)
	e.useRunners(map[string]string{"kimi": program}, func(s *settings.Settings) { s.Summary.Agent = "kimi" })
	st := e.events()

	var started JobStarted
	e.json(http.MethodPost, "/api/sessions/"+key+"/summary", nil, http.StatusOK, &started)
	job := started.Job
	if job.ID == "" || job.Kind != "summary" || job.Key != key || job.Agent != "kimi" || job.Days != 0 || job.Done {
		t.Fatalf("job = %+v", job)
	}
	// Сессия приходит событием sessions, итог — событием job; порядок между ними не задан.
	got := st.waitAll(map[string]func(string) bool{
		"job": func(data string) bool {
			return strings.Contains(data, `"id":"`+job.ID+`"`) && strings.Contains(data, `"done":true`)
		},
		"sessions": func(data string) bool { return strings.Contains(data, key) },
	})
	var final JobView
	json.Unmarshal([]byte(got["job"]), &final)
	if final.Stage != "done" || final.Error != "" || final.Reads != 2 {
		t.Fatalf("final = %+v", final)
	}
	// Сводка сохранена до последнего события: сведения уже её отдают.
	var d Details
	e.json(http.MethodGet, "/api/sessions/"+key, nil, http.StatusOK, &d)
	if d.Summary == nil || d.Summary.Title != "Каталог: проверка названий" || d.Summary.Model != "kimi" ||
		d.Summary.NextStep != "Проверить остальные записи" {
		t.Fatalf("summary = %+v", d.Summary)
	}
	for _, r := range d.Summary.Related {
		if r.Key == "ghost:x" {
			t.Fatalf("related = %+v", d.Summary.Related)
		}
	}
	// Запрос — в аргументах Kimi, ответ — на языке интерфейса (Accept-Language: en).
	argv, _ := os.ReadFile(filepath.Join(rec, "argv"))
	if !strings.Contains(string(argv), "Путь к журналу: ") || !strings.Contains(string(argv), "in English") {
		t.Fatalf("argv = %s", argv)
	}
	runs, _ := os.ReadDir(filepath.Join(filepath.Dir(e.marks.Path()), "runs"))
	if len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}

	if code, _ := e.do(http.MethodPost, "/api/sessions/claude:nope/summary", nil); code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", code)
	}
	if code, raw := e.do(http.MethodPost, "/api/sessions/"+key+"/summary", map[string]string{"agent": "zcode"}); code != http.StatusBadRequest ||
		!strings.Contains(string(raw), "Only Kimi") {
		t.Fatalf("not a runner: %d %s", code, raw)
	}
	if code, raw := e.do(http.MethodPost, "/api/sessions/"+key+"/summary", map[string]string{"agent": "grok"}); code != http.StatusConflict ||
		!strings.Contains(string(raw), "not installed") {
		t.Fatalf("grok: %d %s", code, raw)
	}
}

// «Авто» — первый установленный в порядке Kimi, Claude Code, Codex, Grok, не занятый другой сводкой.
func TestRunnerChoice(t *testing.T) {
	hang, _ := runnerStub(t, "hang", "")
	e := setup(t)
	journal := filepath.Join(e.homes["claude"], "projects", "-test", "a1a1a1a1-0000-4000-8000-000000000001.jsonl")
	claudeJournal(t, e.homes["claude"], "a1a1a1a1-0000-4000-8000-000000000001", t.TempDir(), "Свежая задача")
	if _, err := os.Stat(journal); err != nil {
		t.Fatal(err)
	}
	e.waitSession("claude:a1a1a1a1-0000-4000-8000-000000000001")
	// Claude Code и Codex — та же заглушка, но через свои флаги.
	e.useRunners(map[string]string{"kimi": hang, "claude": hang, "codex": "", "grok": ""}, nil)
	st := e.events()

	var digest JobStarted
	e.json(http.MethodPost, "/api/digest", DigestRequest{Days: 3}, http.StatusOK, &digest)
	if digest.Job.Agent != "kimi" || digest.Job.Days != 3 || digest.Job.Kind != "digest" {
		t.Fatalf("digest job = %+v", digest.Job)
	}
	var sum JobStarted
	key := "codex:" + agentstest.SID(1)
	e.json(http.MethodPost, "/api/sessions/"+key+"/summary", nil, http.StatusOK, &sum)
	if sum.Job.Agent != "claude" {
		t.Fatalf("summary job = %+v", sum.Job)
	}
	// Повторный запуск возвращает идущее задание.
	var again JobStarted
	e.json(http.MethodPost, "/api/sessions/"+key+"/summary", SummaryRequest{Agent: "kimi"}, http.StatusOK, &again)
	if again.Job.ID != sum.Job.ID || again.Job.Agent != "claude" {
		t.Fatalf("again = %+v", again.Job)
	}
	var running []JobView
	e.json(http.MethodGet, "/api/jobs", nil, http.StatusOK, &running)
	if len(running) != 2 || running[0].ID != digest.Job.ID || running[1].ID != sum.Job.ID {
		t.Fatalf("running = %+v", running)
	}
	for _, id := range []string{digest.Job.ID, sum.Job.ID} {
		var res CancelResult
		e.json(http.MethodPost, "/api/jobs/"+id+"/cancel", nil, http.StatusOK, &res)
		if !res.Cancelled {
			t.Fatalf("cancel %s", id)
		}
		if final := st.waitJob(id); final.Stage != "cancelled" || final.Error != "" {
			t.Fatalf("final = %+v", final)
		}
	}
	var res CancelResult
	e.json(http.MethodPost, "/api/jobs/"+sum.Job.ID+"/cancel", nil, http.StatusOK, &res)
	if res.Cancelled {
		t.Fatal("a finished job was cancelled")
	}

	// Kimi выключен — пишет Claude Code; нет никого — ошибка с подсказкой.
	e.useRunners(nil, func(s *settings.Settings) {
		a := s.Agents["kimi"]
		a.Enabled = false
		s.Agents["kimi"] = a
	})
	e.json(http.MethodPost, "/api/sessions/"+key+"/summary", nil, http.StatusOK, &sum)
	if sum.Job.Agent != "claude" {
		t.Fatalf("summary job = %+v", sum.Job)
	}
	e.s.jobs.Cancel(sum.Job.ID)
	st.waitJob(sum.Job.ID)
	e.useRunners(map[string]string{"claude": ""}, nil)
	if code, raw := e.do(http.MethodPost, "/api/digest", DigestRequest{Days: 3}); code != http.StatusConflict ||
		!strings.Contains(string(raw), "Kimi, Claude Code, Codex, Grok") {
		t.Fatalf("no runner: %d %s", code, raw)
	}
}

func TestDigest(t *testing.T) {
	ids := []string{"b1b1b1b1-0000-4000-8000-000000000001", "b1b1b1b1-0000-4000-8000-000000000002",
		"b1b1b1b1-0000-4000-8000-000000000003"}
	root := t.TempDir()
	backend, shop := filepath.Join(root, "backend"), filepath.Join(root, "shop")
	keys := []string{"claude:" + ids[0], "claude:" + ids[1], "codex:" + ids[2]}
	answer := `{"lead": "Два проекта, три сессии.", "projects": [` +
		`{"name": "backend", "items": [{"text": "[BACKEND-1](` + keys[0] + `): лимиты в ` + "`main`" + `.", "keys": ["` + keys[1] + `", "ghost:x"]}], "left": "Показать журнал."},` +
		`{"name": "shop", "items": [{"text": "Поиск по каталогу.", "keys": ["` + keys[2] + `"]}], "left": ""}]}`
	program, rec := runnerStub(t, "ok", answer)
	e := setup(t)
	claudeJournal(t, e.homes["claude"], ids[0], backend, "BACKEND-1: лимиты")
	claudeJournal(t, e.homes["claude"], ids[1], backend, "Вебхуки")
	codexJournal(t, e.homes["codex"], ids[2], shop, time.Now(), "Поиск по каталогу", "Готово")
	// Служебная сессия claude -p за тот же период в сводку не попадает.
	auto := "b1b1b1b1-0000-4000-8000-000000000009"
	writeRows(t, filepath.Join(e.homes["claude"], "projects", "-test", auto+".jsonl"),
		map[string]any{"type": "user", "timestamp": time.Now().UTC().Format(time.RFC3339), "entrypoint": "sdk-cli",
			"cwd": backend, "sessionId": auto, "message": map[string]any{"role": "user", "content": "Ревью"}})
	for _, key := range append(keys, "claude:"+auto) {
		e.waitSession(key)
	}
	e.useRunners(map[string]string{"kimi": program}, func(s *settings.Settings) { s.Digest.Agent = "kimi" })
	st := e.events()

	var view DigestView
	e.json(http.MethodGet, "/api/digest?days=7", nil, http.StatusOK, &view)
	if view.Digest != nil || view.Job != nil {
		t.Fatalf("view = %+v", view)
	}
	var started JobStarted
	e.json(http.MethodPost, "/api/digest", DigestRequest{Days: 7}, http.StatusOK, &started)
	// Сводка приходит событием digest раньше итога задания.
	var event summary.Digest
	st.wait("digest", func(data string) bool { return json.Unmarshal([]byte(data), &event) == nil && event.Days == 7 })
	if final := st.waitJob(started.Job.ID); final.Stage != "done" || final.Kind != "digest" || final.Days != 7 {
		t.Fatalf("final = %+v", final)
	}

	e.json(http.MethodGet, "/api/digest?days=7", nil, http.StatusOK, &view)
	d := view.Digest
	if view.Job != nil || d == nil || d.Days != 7 || d.Journals != 3 || d.Model != "kimi" || d.Lang != "en" ||
		!strings.HasPrefix(d.Title, "Summary for ") || d.Lead != "Два проекта, три сессии." || d.Legacy {
		t.Fatalf("digest = %+v", d)
	}
	if len(d.Projects) != 2 || d.Projects[0].Path != backend || d.Projects[1].Path != shop ||
		strings.Join(d.Projects[0].Items[0].Keys, ",") != keys[1]+","+keys[0] || d.Projects[0].Left != "Показать журнал." {
		t.Fatalf("projects = %+v", d.Projects)
	}
	if event.Title != d.Title || event.At != d.At {
		t.Fatalf("event = %+v", event)
	}
	input, _ := os.ReadFile(filepath.Join(rec, "digest-input.txt"))
	if !strings.Contains(string(input), "Сессий: 3, проектов: 2.") || strings.Contains(string(input), auto) ||
		strings.Contains(string(input), agentstest.SID(1)) {
		t.Fatalf("input:\n%s", input)
	}

	for _, path := range []string{"/api/digest?days=4", "/api/digest", "/api/digest?days=x"} {
		if code, _ := e.do(http.MethodGet, path, nil); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", path, code)
		}
	}
	for _, body := range []any{DigestRequest{Days: 6}, nil, "[]", DigestRequest{Days: 3, Agent: "vim"}} {
		if code, _ := e.do(http.MethodPost, "/api/digest", body); code != http.StatusBadRequest {
			t.Fatalf("%v: %d", body, code)
		}
	}
}

// Отмена и ошибка оставляют прежнюю сводку; сводка первой версии читается с legacy.
func TestDigestKeepsPrevious(t *testing.T) {
	hang, _ := runnerStub(t, "hang", "")
	fail, _ := runnerStub(t, "fail", "")
	e := setup(t)
	id := "c1c1c1c1-0000-4000-8000-000000000001"
	claudeJournal(t, e.homes["claude"], id, t.TempDir(), "Свежая задача")
	e.waitSession("claude:" + id)
	old := `{"at": ` + jsonFloat(time.Now()) + `, "model": "grok", "result": {"lead": "Старая сводка", "projects": [` +
		`{"name": "sample app", "bullets": [{"text": "Сделано", "keys": ["claude:` + agentstest.SID(1) + `"]}]}],` +
		`"tails": [{"id": "t1", "text": "Влить", "key": "claude:` + agentstest.SID(1) + `"}], "autos": []}}`
	if err := e.marks.SetDigest(3, json.RawMessage(old)); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		var view DigestView
		e.json(http.MethodGet, "/api/digest?days=3", nil, http.StatusOK, &view)
		d := view.Digest
		if view.Job != nil || d == nil || !d.Legacy || d.Lead != "Старая сводка" || len(d.Projects) != 1 ||
			d.Projects[0].Path != agentstest.Cwd || d.Projects[0].Left != "Влить." || !strings.HasPrefix(d.Title, "Summary for ") {
			t.Fatalf("view = %+v", view)
		}
	}
	check()

	e.useRunners(map[string]string{"kimi": hang}, func(s *settings.Settings) { s.Digest.Agent = "kimi" })
	st := e.events()
	var started JobStarted
	e.json(http.MethodPost, "/api/digest", DigestRequest{Days: 3}, http.StatusOK, &started)
	st.wait("job", func(data string) bool { return strings.Contains(data, `"stage":"reading"`) })
	var view DigestView
	e.json(http.MethodGet, "/api/digest?days=3", nil, http.StatusOK, &view)
	if view.Job == nil || view.Job.ID != started.Job.ID || view.Job.Stage != "reading" || view.Job.Reads != 1 {
		t.Fatalf("running = %+v", view.Job)
	}
	e.json(http.MethodPost, "/api/jobs/"+started.Job.ID+"/cancel", nil, http.StatusOK, nil)
	if final := st.waitJob(started.Job.ID); final.Stage != "cancelled" {
		t.Fatalf("final = %+v", final)
	}
	check()

	e.useRunners(map[string]string{"kimi": fail}, nil)
	e.json(http.MethodPost, "/api/digest", DigestRequest{Days: 3}, http.StatusOK, &started)
	final := st.waitJob(started.Job.ID)
	if final.Stage != "failed" || final.Error != "Kimi exited with code 2" || !strings.Contains(final.Output, "boom") {
		t.Fatalf("final = %+v", final)
	}
	check()

	t.Setenv("AI_THREADS_DIGEST_TIMEOUT", "0.3")
	e.useRunners(map[string]string{"kimi": hang}, nil)
	e.json(http.MethodPost, "/api/digest", DigestRequest{Days: 3}, http.StatusOK, &started)
	if final := st.waitJob(started.Job.ID); final.Stage != "failed" || final.Error != "Kimi didn't answer in 1 s" {
		t.Fatalf("final = %+v", final)
	}
	check()

	// За период без сессий агента не зовут.
	if err := os.Remove(filepath.Join(e.homes["claude"], "projects", "-test", id+".jsonl")); err != nil {
		t.Fatal(err)
	}
	wait(t, "session is gone", func() bool { _, ok := e.s.catalog.Get("claude:" + id); return !ok })
	if code, raw := e.do(http.MethodPost, "/api/digest", DigestRequest{Days: 3}); code != http.StatusConflict ||
		!strings.Contains(string(raw), "no sessions") {
		t.Fatalf("empty period: %d %s", code, raw)
	}
}

func jsonFloat(t time.Time) string {
	raw, _ := json.Marshal(float64(t.UnixMilli()) / 1000)
	return string(raw)
}
