package live_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/live"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/tmux"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

func TestMain(m *testing.M) { tmuxtest.Main(m) }

var t0 = time.Unix(1_800_000_000, 0)

// world — подставные терминалы, экраны и часы.
type world struct {
	panes    []tmux.Pane
	screens  map[string]string
	now      time.Time
	override map[string]model.State
}

func newWorld() *world { return &world{screens: map[string]string{}, now: t0} }

func (w *world) tracker(o live.Options) *live.Tracker {
	o.List = func() ([]tmux.Pane, error) { return append([]tmux.Pane(nil), w.panes...), nil }
	o.Capture = func(name string) (string, error) { return w.screens[name], nil }
	o.Now = func() time.Time { return w.now }
	o.Override = func() map[string]model.State { return w.override }
	return live.New(o)
}

// step — секунда проходит, экран меняется на screen (пусто — не меняется), опрос.
func (w *world) step(tr *live.Tracker, name, screen string) {
	w.now = w.now.Add(time.Second)
	if screen != "" {
		w.screens[name] = screen
	}
	tr.Poll()
}

func (w *world) pane(name, key string) *tmux.Pane {
	w.panes = append(w.panes, tmux.Pane{Name: name, Key: key, PID: 100 + len(w.panes), Created: w.now})
	return &w.panes[len(w.panes)-1]
}

func get(t *testing.T, tr *live.Tracker, key string) model.Live {
	t.Helper()
	l, ok := tr.Get(key)
	if !ok {
		t.Fatalf("no live state for %s", key)
	}
	return l
}

func expect(t *testing.T, tr *live.Tracker, key string, state model.State, unseen bool) model.Live {
	t.Helper()
	l := get(t, tr, key)
	if l.State != state || l.Unseen != unseen {
		t.Fatalf("%s: state %s unseen %v, want %s %v (%+v)", key, l.State, l.Unseen, state, unseen, l)
	}
	return l
}

func TestNewSessionLifecycle(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: 2500 * time.Millisecond})
	tr.Poll() // первый опрос: терминалов ещё нет

	w.pane("nit-a", "codex:a")
	w.step(tr, "nit-a", "")
	l := expect(t, tr, "codex:a", model.StateStarting, false)
	if l.Terminal != "nit-a" || !l.Since.Equal(t0) {
		t.Fatalf("starting: %+v", l)
	}

	w.step(tr, "nit-a", "• Working (1s)")
	expect(t, tr, "codex:a", model.StateWorking, false)
	w.step(tr, "nit-a", "• Working (2s)")
	w.step(tr, "nit-a", "• Ran tests\n\n› ")
	changedAt := w.now
	w.step(tr, "nit-a", "")
	w.step(tr, "nit-a", "")
	expect(t, tr, "codex:a", model.StateWorking, false) // 2 с без изменений — ещё не порог
	w.step(tr, "nit-a", "")
	l = expect(t, tr, "codex:a", model.StateWaiting, true)
	if !l.Since.Equal(changedAt) || l.LastLine != "Ran tests" {
		t.Fatalf("waiting: %+v", l)
	}

	// Одна перерисовка (сменили размер окна) — агент всё ещё ждёт, время ожидания идёт.
	w.step(tr, "nit-a", "• Ran tests\n\n›  ")
	w.step(tr, "nit-a", "")
	l = expect(t, tr, "codex:a", model.StateWaiting, true)
	if !l.Since.Equal(changedAt) {
		t.Fatalf("since moved: %+v", l)
	}

	tr.MarkSeen("codex:a")
	expect(t, tr, "codex:a", model.StateWaiting, false)

	// Экран меняется два опроса подряд — снова работает.
	w.step(tr, "nit-a", "• Working (1s)")
	expect(t, tr, "codex:a", model.StateWaiting, false)
	w.step(tr, "nit-a", "• Working (2s)")
	expect(t, tr, "codex:a", model.StateWorking, false)

	// Агент вышел с ошибкой: «новое», код выхода.
	died := w.now.Add(500 * time.Millisecond)
	w.panes[0].Dead, w.panes[0].ExitCode, w.panes[0].DiedAt = true, 3, died
	w.step(tr, "nit-a", "")
	l = expect(t, tr, "codex:a", model.StateError, true)
	if l.ExitCode != 3 || !l.Since.Equal(died) {
		t.Fatalf("error: %+v", l)
	}

	// Терминал закрыли — состояния больше нет.
	w.panes = nil
	w.step(tr, "", "")
	if _, ok := tr.Get("codex:a"); ok {
		t.Fatal("closed terminal is still tracked")
	}
}

func TestStartupKeepsUnseenOffAndUsesActivity(t *testing.T) {
	w := newWorld()
	p := w.pane("nit-old", "kimi:old")
	p.Activity = t0.Add(-time.Minute)
	w.screens["nit-old"] = "Done. What next?"
	q := w.pane("nit-busy", "kimi:busy")
	q.Activity = t0
	w.screens["nit-busy"] = "⠋ thinking"
	r := w.pane("nit-dead", "kimi:dead")
	r.Dead, r.ExitCode, r.DiedAt = true, 0, t0.Add(-time.Hour)
	tr := w.tracker(live.Options{})
	tr.Poll()
	l := expect(t, tr, "kimi:old", model.StateWaiting, false)
	if !l.Since.Equal(t0.Add(-time.Minute)) || l.LastLine != "Done. What next?" {
		t.Fatalf("old: %+v", l)
	}
	if l := expect(t, tr, "kimi:busy", model.StateWorking, false); l.LastLine != "thinking" {
		t.Fatalf("busy: %+v", l)
	}
	if l := expect(t, tr, "kimi:dead", model.StateExited, false); !l.Since.Equal(t0.Add(-time.Hour)) {
		t.Fatalf("dead: %+v", l)
	}
	// Терминал без ключа открыт не «Нитью»: за ним не следим.
	w.panes = append(w.panes, tmux.Pane{Name: "foreign"})
	w.step(tr, "", "")
	if len(tr.All()) != 3 {
		t.Fatalf("all = %+v", tr.All())
	}
}

func TestViewedSessionDoesNotGetUnseen(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: 2 * time.Second})
	tr.Poll()
	w.pane("nit-a", "claude:a")
	w.step(tr, "nit-a", "working 1")
	release := tr.View("claude:a")
	w.step(tr, "nit-a", "")
	w.step(tr, "nit-a", "")
	expect(t, tr, "claude:a", model.StateWaiting, false)
	release()
	release() // повторный вызов ничего не ломает
	w.step(tr, "nit-a", "working 2")
	w.step(tr, "nit-a", "working 3")
	w.step(tr, "nit-a", "")
	w.step(tr, "nit-a", "")
	expect(t, tr, "claude:a", model.StateWaiting, true)
	// Открыли терминал — «новое» снимается сразу.
	defer tr.View("claude:a")()
	expect(t, tr, "claude:a", model.StateWaiting, false)
}

func TestOverrideWinsOverScreen(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: time.Second})
	tr.Poll()
	w.pane("nit-c", "claude:c")
	w.step(tr, "nit-c", "spinner 1")
	w.override = map[string]model.State{"claude:c": model.StateWorking}
	w.step(tr, "nit-c", "")
	w.step(tr, "nit-c", "")
	expect(t, tr, "claude:c", model.StateWorking, false) // экран замер, но агент сказал, что работает
	w.override["claude:c"] = model.StateWaiting
	w.step(tr, "nit-c", "spinner 2")
	expect(t, tr, "claude:c", model.StateWaiting, true)
	// Выход процесса сильнее слова агента.
	w.panes[0].Dead, w.panes[0].ExitCode = true, 0
	w.step(tr, "nit-c", "")
	expect(t, tr, "claude:c", model.StateExited, false)
}

func TestThresholdPerAgent(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: 2 * time.Second, Thresholds: map[string]time.Duration{"grok": 5 * time.Second}})
	tr.Poll()
	w.pane("nit-g", "grok:g")
	w.pane("nit-k", "kimi:k")
	w.screens["nit-k"] = "k"
	w.step(tr, "nit-g", "g")
	for i := 0; i < 3; i++ {
		w.step(tr, "nit-g", "")
	}
	expect(t, tr, "kimi:k", model.StateWaiting, true)
	expect(t, tr, "grok:g", model.StateWorking, false)
	w.step(tr, "nit-g", "")
	w.step(tr, "nit-g", "")
	expect(t, tr, "grok:g", model.StateWaiting, true)
}

func TestKeyChangeAndRestart(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: time.Second})
	tr.Poll()
	w.pane("nit-n", "codex:pending-1")
	w.step(tr, "nit-n", "hello")
	w.step(tr, "nit-n", "")
	w.step(tr, "nit-n", "")
	before := expect(t, tr, "codex:pending-1", model.StateWaiting, true)
	sub := tr.Subscribe()
	defer sub.Close()

	// Стал известен ID сессии: состояние и «новое» переходят к новому ключу.
	w.panes[0].Key = "codex:019a"
	w.step(tr, "nit-n", "")
	if l := expect(t, tr, "codex:019a", model.StateWaiting, true); l != before {
		t.Fatalf("after key change: %+v, before %+v", l, before)
	}
	if _, ok := tr.Get("codex:pending-1"); ok {
		t.Fatal("old key is still tracked")
	}
	got := sub.Take()
	want := map[string]model.Live{"codex:pending-1": {State: model.StateIdle}, "codex:019a": before}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %+v", got)
	}

	// Терминал открыли заново под тем же именем: это новый процесс, он снова запускается.
	w.panes[0].PID = 999
	w.panes[0].Created = w.now
	w.screens["nit-n"] = ""
	w.step(tr, "nit-n", "")
	expect(t, tr, "codex:019a", model.StateStarting, false)
}

func TestSubscriptionCoalesces(t *testing.T) {
	w := newWorld()
	tr := w.tracker(live.Options{Threshold: time.Minute})
	tr.Poll()
	sub := tr.Subscribe()
	w.pane("nit-a", "claude:a")
	w.step(tr, "nit-a", "")
	w.step(tr, "nit-a", "one")
	w.step(tr, "nit-a", "two")
	select {
	case <-sub.C:
	default:
		t.Fatal("no signal")
	}
	got := sub.Take()
	if len(got) != 1 || got["claude:a"].State != model.StateWorking || got["claude:a"].LastLine != "two" {
		t.Fatalf("changes = %+v", got)
	}
	if len(sub.Take()) != 0 {
		t.Fatal("changes must be taken once")
	}
	w.step(tr, "nit-a", "")
	if len(sub.Take()) != 0 {
		t.Fatal("nothing changed")
	}
	sub.Close()
	w.step(tr, "nit-a", "three")
	if len(sub.Take()) != 0 {
		t.Fatal("closed subscription got changes")
	}
}

func TestLastLine(t *testing.T) {
	claude := strings.Join([]string{
		"> fix the tests",
		"",
		"⏺ Fixed 3 failing tests in catalog.",
		"",
		"╭──────────────────────────────────────╮",
		"│ >                                    │",
		"╰──────────────────────────────────────╯",
		"  ? for shortcuts",
	}, "\n")
	cases := map[string]string{
		claude: "Fixed 3 failing tests in catalog.",
		"line one\n\x1b[32m✻ Compiling…\x1b[0m": "Compiling…",
		"⠙ Thinking (3s)\n   \n":                "Thinking (3s)",
		"output\n──────────────────────\n":      "output",
		"──────────────────────\n> │":           "",
		"":          "",
		"just text": "just text",
	}
	for screen, want := range cases {
		if got := live.LastLine(screen); got != want {
			t.Errorf("LastLine(%q) = %q, want %q", screen, got, want)
		}
	}
	if got := live.LastLine(strings.Repeat("я", 300)); len([]rune(got)) != 201 {
		t.Errorf("long line has %d runes", len([]rune(got)))
	}
}

// С настоящим tmux: заглушка работает, ждёт, снова работает и выходит с ошибкой.
func TestWithTmux(t *testing.T) {
	tmuxtest.Server(t)
	worker := tmuxtest.Worker(t)
	tr := live.New(live.Options{Interval: 100 * time.Millisecond, Threshold: 600 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tr.Run(ctx)
	sub := tr.Subscribe()
	defer sub.Close()

	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-w", Key: "kimi:w", Cwd: t.TempDir(), Argv: []string{worker, "8"}}); err != nil {
		t.Fatal(err)
	}
	var seen []model.State
	waitFor := func(state model.State) model.Live {
		t.Helper()
		var l model.Live
		tmuxtest.Wait(t, "state "+string(state), func() bool {
			for k, v := range sub.Take() {
				if k == "kimi:w" && (len(seen) == 0 || seen[len(seen)-1] != v.State) {
					seen = append(seen, v.State)
				}
			}
			var ok bool
			l, ok = tr.Get("kimi:w")
			if state == model.StateIdle {
				return !ok
			}
			return l.State == state
		})
		return l
	}
	waitFor(model.StateWorking)
	if l := waitFor(model.StateWaiting); !l.Unseen || l.LastLine != "Done. What next?" {
		t.Fatalf("waiting: %+v", l)
	}
	tr.MarkSeen("kimi:w")
	tmux.SendText("nit-w", "go")
	tmux.SendKeys("nit-w", "Enter")
	waitFor(model.StateWorking)
	waitFor(model.StateWaiting)
	tmux.SendText("nit-w", "quit")
	tmux.SendKeys("nit-w", "Enter")
	if l := waitFor(model.StateError); l.ExitCode != 5 || !l.Unseen {
		t.Fatalf("error: %+v", l)
	}
	tmux.Close("nit-w")
	waitFor(model.StateIdle)
	t.Logf("states: %v", seen)
}
