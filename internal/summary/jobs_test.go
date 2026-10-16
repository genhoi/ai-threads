package summary

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// По одной цели идёт одно задание; повторный запуск возвращает идущее.
func TestJobsOnePerTarget(t *testing.T) {
	rec := &recorder{}
	jobs := NewJobs(rec.add)
	release := make(chan struct{})
	block := func(ctx context.Context, j *Job) error {
		<-release
		return nil
	}
	first, started, err := jobs.Start(Target{Kind: KindSummary, Key: "kimi:a"}, "kimi", block)
	if err != nil || !started {
		t.Fatal(started, err)
	}
	same, started, _ := jobs.Start(Target{Kind: KindSummary, Key: "kimi:a"}, "claude", block)
	if same != first || started {
		t.Fatal("a second job started for the same session")
	}
	digest, started, _ := jobs.Start(Target{Kind: KindDigest, Days: 3}, "claude", block)
	if !started || digest == first || digest.ID() == first.ID() {
		t.Fatal("a digest didn't start next to a summary")
	}
	week, _, _ := jobs.Start(Target{Kind: KindDigest, Days: 7}, "claude", block)
	if week == digest {
		t.Fatal("periods share a job")
	}
	if !jobs.Busy("kimi") || !jobs.Busy("claude") || jobs.Busy("codex") || len(jobs.Running()) != 3 {
		t.Fatalf("running = %+v", jobs.Running())
	}
	if jobs.Find(Target{Kind: KindDigest, Days: 3}) != digest || jobs.Find(Target{Kind: KindDigest, Days: 5}) != nil {
		t.Fatal("find")
	}
	close(release)
	jobs.Wait()
	if len(jobs.Running()) != 0 || jobs.Busy("kimi") {
		t.Fatalf("running = %+v", jobs.Running())
	}
	st := first.Status()
	if st.Stage != StageDone || !st.Done || st.Target.Key != "kimi:a" || st.Agent != "kimi" {
		t.Fatalf("status = %+v", st)
	}
	var stages []string
	for _, s := range rec.all() {
		if s.ID == first.ID() {
			stages = append(stages, s.Stage)
		}
	}
	if strings.Join(stages, ",") != "starting,done" {
		t.Fatalf("stages = %v", stages)
	}
	// Закончилось — по той же цели запускается новое.
	again, started, _ := jobs.Start(Target{Kind: KindSummary, Key: "kimi:a"}, "kimi", func(context.Context, *Job) error { return nil })
	if !started || again == first {
		t.Fatal("no new job after the first finished")
	}
	jobs.Wait()
}

func TestJobsErrors(t *testing.T) {
	rec := &recorder{}
	jobs := NewJobs(rec.add)
	cases := map[string]func(context.Context, *Job) error{
		"own": func(context.Context, *Job) error {
			return &Error{Text: "Kimi завершился с кодом 2", Output: "boom"}
		},
		"plain": func(context.Context, *Job) error { return errors.New("disk full") },
		"panic": func(context.Context, *Job) error { panic("oops") },
	}
	ids := map[string]string{}
	for name, fn := range cases {
		j, _, _ := jobs.Start(Target{Kind: KindSummary, Key: "kimi:" + name}, "kimi", fn)
		ids[name] = j.ID()
	}
	jobs.Wait()
	if st := rec.last(ids["own"]); st.Stage != StageFailed || st.Error != "Kimi завершился с кодом 2" || st.Output != "boom" {
		t.Fatalf("own = %+v", st)
	}
	if st := rec.last(ids["plain"]); st.Stage != StageFailed || st.Error != "disk full" {
		t.Fatalf("plain = %+v", st)
	}
	if st := rec.last(ids["panic"]); st.Stage != StageFailed || st.Error != "panic: oops" {
		t.Fatalf("panic = %+v", st)
	}
}

// Close останавливает идущие задания и больше не пускает новые.
func TestJobsClose(t *testing.T) {
	rec := &recorder{}
	jobs := NewJobs(rec.add)
	if jobs.Cancel("job:404") {
		t.Fatal("cancelled an unknown job")
	}
	j, _, _ := jobs.Start(Target{Kind: KindDigest, Days: 5}, "grok", func(ctx context.Context, j *Job) error {
		j.Read()
		j.Read()
		j.Writing()
		j.Writing()
		<-ctx.Done()
		return ctx.Err()
	})
	waitFor(t, "writing", func() bool { return rec.last(j.ID()).Stage == StageWriting })
	jobs.Close()
	st := rec.last(j.ID())
	if st.Stage != StageCancelled || st.Reads != 2 || !st.Done {
		t.Fatalf("status = %+v", st)
	}
	// starting, reading, reading, writing, cancelled: повторный Writing не шлёт события.
	if n := len(rec.all()); n != 5 {
		t.Fatalf("events = %+v", rec.all())
	}
	if _, _, err := jobs.Start(Target{Kind: KindDigest, Days: 3}, "grok", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close: %v", err)
	}
}
