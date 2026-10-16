package summary

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Виды заданий.
const (
	KindSummary = "summary" // сводка по сессии
	KindDigest  = "digest"  // сводка за несколько дней
)

// Этапы задания. Последние три — итог: задание закончилось.
const (
	StageStarting  = "starting"  // готовит данные и запускает агента
	StageReading   = "reading"   // агент читает журналы
	StageWriting   = "writing"   // агент пишет ответ
	StageDone      = "done"      // результат сохранён
	StageFailed    = "failed"    // ошибка; текст в Error
	StageCancelled = "cancelled" // отменено человеком или остановкой «Нити»
)

// Target — цель задания. По одной цели идёт не больше одного задания.
type Target struct {
	Kind string
	Key  string // ключ сессии для summary
	Days int    // период для digest
}

func (t Target) id() string {
	if t.Kind == KindDigest {
		return t.Kind + ":" + strconv.Itoa(t.Days)
	}
	return t.Kind + ":" + t.Key
}

// Status — снимок задания для интерфейса.
type Status struct {
	ID      string
	Target  Target
	Agent   string // кто пишет: kimi, claude, codex, grok
	Stage   string
	Reads   int // сколько раз агент вызывал инструменты чтения
	Started time.Time
	Elapsed time.Duration
	Done    bool
	Error   string
	Output  string // хвост вывода агента при ошибке
}

// Job — одно задание. Агент сообщает ход через Read и Writing.
type Job struct {
	id      string
	target  Target
	agent   string
	started time.Time
	cancel  context.CancelFunc
	jobs    *Jobs

	mu     sync.Mutex
	stage  string
	reads  int
	done   bool
	ended  time.Time
	errMsg string
	output string
}

// Jobs — идущие задания. Изменения уходят в onChange вне блокировок.
type Jobs struct {
	onChange func(Status)

	mu      sync.Mutex
	n       int
	running map[string]*Job // по Target.id
	closed  bool
	wg      sync.WaitGroup
}

// NewJobs — пустой список заданий; onChange может быть nil.
func NewJobs(onChange func(Status)) *Jobs {
	if onChange == nil {
		onChange = func(Status) {}
	}
	return &Jobs{onChange: onChange, running: map[string]*Job{}}
}

// ErrClosed — «Нить» останавливается, новые задания не запускаются.
var ErrClosed = errors.New("jobs are stopped")

// Start запускает fn для цели t. Если по цели уже идёт задание, возвращает его и false.
// fn должна вернуться, когда отменён ctx.
func (js *Jobs) Start(t Target, agent string, fn func(ctx context.Context, j *Job) error) (*Job, bool, error) {
	js.mu.Lock()
	if js.closed {
		js.mu.Unlock()
		return nil, false, ErrClosed
	}
	if j := js.running[t.id()]; j != nil {
		js.mu.Unlock()
		return j, false, nil
	}
	js.n++
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{id: "job:" + strconv.Itoa(js.n), target: t, agent: agent, started: time.Now(), cancel: cancel,
		jobs: js, stage: StageStarting}
	js.running[t.id()] = j
	js.wg.Add(1)
	js.mu.Unlock()
	js.onChange(j.Status())
	go js.run(ctx, j, fn)
	return j, true, nil
}

func (js *Jobs) run(ctx context.Context, j *Job, fn func(context.Context, *Job) error) {
	defer js.wg.Done()
	defer j.cancel()
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		err = fn(ctx, j)
	}()
	j.mu.Lock()
	j.done, j.ended = true, time.Now()
	var e *Error
	switch {
	case ctx.Err() != nil:
		j.stage = StageCancelled
	case errors.As(err, &e):
		j.stage, j.errMsg, j.output = StageFailed, e.Text, e.Output
	case err != nil:
		j.stage, j.errMsg = StageFailed, err.Error()
	default:
		j.stage = StageDone
	}
	j.mu.Unlock()
	// Задание уходит из идущих раньше последнего события: кто получил событие, уже может
	// запустить новое по той же цели.
	js.mu.Lock()
	if js.running[j.target.id()] == j {
		delete(js.running, j.target.id())
	}
	js.mu.Unlock()
	js.onChange(j.Status())
}

// Find — идущее задание по цели или nil.
func (js *Jobs) Find(t Target) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.running[t.id()]
}

// Running — снимки идущих заданий.
func (js *Jobs) Running() []Status {
	js.mu.Lock()
	list := make([]*Job, 0, len(js.running))
	for _, j := range js.running {
		list = append(list, j)
	}
	js.mu.Unlock()
	out := make([]Status, 0, len(list))
	for _, j := range list {
		out = append(out, j.Status())
	}
	return out
}

// Busy — агент сейчас пишет сводку в каком-то задании.
func (js *Jobs) Busy(agent string) bool {
	js.mu.Lock()
	defer js.mu.Unlock()
	for _, j := range js.running {
		if j.agent == agent {
			return true
		}
	}
	return false
}

// Cancel отменяет идущее задание по id; false — такого нет или оно уже закончилось.
func (js *Jobs) Cancel(id string) bool {
	js.mu.Lock()
	var found *Job
	for _, j := range js.running {
		if j.id == id {
			found = j
		}
	}
	js.mu.Unlock()
	if found == nil {
		return false
	}
	found.cancel()
	return true
}

// Close отменяет все задания и ждёт, пока они закончатся. Новые больше не запускаются.
func (js *Jobs) Close() {
	js.mu.Lock()
	js.closed = true
	for _, j := range js.running {
		j.cancel()
	}
	js.mu.Unlock()
	js.wg.Wait()
}

// Wait ждёт, пока закончатся все задания; для тестов.
func (js *Jobs) Wait() { js.wg.Wait() }

// ID — номер задания: job:1, job:2, …
func (j *Job) ID() string { return j.id }

// Status — снимок задания.
func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	end := time.Now()
	if j.done {
		end = j.ended
	}
	return Status{ID: j.id, Target: j.target, Agent: j.agent, Stage: j.stage, Reads: j.reads, Started: j.started,
		Elapsed: end.Sub(j.started), Done: j.done, Error: j.errMsg, Output: j.output}
}

// Read — агент открыл журнал или вызвал другой инструмент.
func (j *Job) Read() {
	j.set(func() bool {
		j.reads++
		j.stage = StageReading
		return true
	})
}

// Writing — агент начал писать ответ.
func (j *Job) Writing() {
	j.set(func() bool {
		if j.stage == StageWriting {
			return false
		}
		j.stage = StageWriting
		return true
	})
}

// set меняет задание, пока оно идёт, и сообщает об изменении.
func (j *Job) set(change func() bool) {
	if j == nil {
		return
	}
	j.mu.Lock()
	changed := !j.done && change()
	j.mu.Unlock()
	if changed {
		j.jobs.onChange(j.Status())
	}
}
