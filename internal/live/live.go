// Package live — состояние сессий, открытых в терминалах «Нити»: запускается, работает, ждёт,
// вышла, и отметка «новое».
//
// Состояние берётся по экрану терминала: раз в секунду снимается экран каждого открытого
// терминала, и сравниваются хеши. Экран меняется — агент работает; не меняется дольше порога —
// ждёт. Агенты, которые сами сообщают, работают они или ждут (реестр Claude Code), передаются
// через Options.Override, и их слово сильнее догадки по экрану. Код выхода берётся у tmux.
package live

import (
	"context"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/tmux"
)

const (
	DefaultInterval  = time.Second
	DefaultThreshold = 2500 * time.Millisecond
	// Столько опросов подряд экран должен меняться, чтобы ждущий агент считался снова работающим:
	// одна перерисовка (сменили размер окна, нажали клавишу) — ещё не работа.
	confirmChanges = 2
	maxLastLine    = 200
)

// Options — настройки слежения. Нулевые значения — значения по умолчанию.
type Options struct {
	Interval   time.Duration            // как часто смотреть экраны; 0 — раз в секунду
	Threshold  time.Duration            // экран не меняется дольше — агент ждёт; 0 — 2,5 с
	Thresholds map[string]time.Duration // порог для отдельных агентов: claude, codex…
	// Override — состояния, которые агенты сообщают сами: ключ сессии → состояние.
	// Сильнее догадки по экрану, но не сильнее выхода процесса.
	Override func() map[string]model.State

	// Для тестов: откуда брать терминалы, экраны и время.
	List    func() ([]tmux.Pane, error)
	Capture func(name string) (string, error)
	Now     func() time.Time
}

// Tracker следит за открытыми терминалами «Нити». Терминал узнаётся по метке @nit_key:
// после перезапуска «Нити» состояние восстанавливается само.
type Tracker struct {
	opts   Options
	pollMu sync.Mutex // опросы не идут одновременно

	mu      sync.Mutex
	terms   map[string]*term // по имени сессии tmux
	viewing map[string]int   // ключ → сколько страниц сейчас показывают терминал
	subs    map[*Subscription]struct{}
	polled  bool // первый опрос прошёл
}

type term struct {
	key       string
	pid       int // процесс панели: другой — значит, терминал открыли заново
	live      model.Live
	hash      uint64
	output    bool      // на экране уже что-то было
	changedAt time.Time // когда экран последний раз изменился
	changes   int       // сколько опросов подряд экран менялся
}

// New — слежение с настройками o. Опрос запускает Run.
func New(o Options) *Tracker {
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	if o.Threshold <= 0 {
		o.Threshold = DefaultThreshold
	}
	if o.List == nil {
		o.List = tmux.List
	}
	if o.Capture == nil {
		o.Capture = func(name string) (string, error) { return tmux.Capture(name, 0) }
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Tracker{opts: o, terms: map[string]*term{}, viewing: map[string]int{}, subs: map[*Subscription]struct{}{}}
}

// Run опрашивает терминалы раз в Interval, пока не отменён ctx.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(t.opts.Interval)
	defer tick.Stop()
	for {
		t.Poll()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type snapshot struct {
	pane   tmux.Pane
	screen string
	ok     bool
}

// Poll — один опрос: список терминалов, их экраны, новые состояния, рассылка изменений.
func (t *Tracker) Poll() {
	t.pollMu.Lock()
	defer t.pollMu.Unlock()
	panes, err := t.opts.List()
	if err != nil {
		panes = nil // tmux нет — терминалов нет
	}
	var snaps []snapshot
	for _, p := range panes {
		if p.Key == "" {
			continue // сессия открыта не «Нитью»
		}
		// Экран вышедшего агента тоже снимается: по нему видна последняя строка.
		s := snapshot{pane: p}
		s.screen, err = t.opts.Capture(p.Name)
		s.ok = err == nil
		snaps = append(snaps, s)
	}
	var override map[string]model.State
	if t.opts.Override != nil {
		override = t.opts.Override()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.opts.Now()
	changes := map[string]model.Live{}
	alive := map[string]bool{}
	for _, s := range snaps {
		alive[s.pane.Name] = true
		tm := t.terms[s.pane.Name]
		rekeyed := tm != nil && tm.key != s.pane.Key
		if rekeyed {
			// Ключ сменился: стал известен ID новой сессии. Старый ключ уходит, состояние
			// и отметка «новое» переходят к новому.
			changes[tm.key] = model.Live{State: model.StateIdle}
			tm.key = s.pane.Key
		}
		isNew := tm == nil || tm.pid != s.pane.PID
		if isNew {
			// Новый терминал или тот же, открытый заново.
			tm = &term{key: s.pane.Key, pid: s.pane.PID}
			t.terms[s.pane.Name] = tm
		}
		was := tm.live
		t.update(tm, s, override[tm.key], isNew, now)
		if isNew || rekeyed || was != tm.live {
			changes[tm.key] = tm.live
		}
	}
	for name, tm := range t.terms {
		if !alive[name] {
			delete(t.terms, name)
			changes[tm.key] = model.Live{State: model.StateIdle}
		}
	}
	t.polled = true
	t.notify(changes)
}

// update пересчитывает состояние терминала по снимку. isNew — терминал виден впервые.
func (t *Tracker) update(tm *term, s snapshot, override model.State, isNew bool, now time.Time) {
	p := s.pane
	// Терминалы, открытые до запуска слежения: смотрел ли их человек, неизвестно.
	startup := isNew && !t.polled
	tm.live.Terminal = p.Name
	if s.ok {
		h := fnv.New64a()
		h.Write([]byte(s.screen))
		sum := h.Sum64()
		switch {
		case startup:
			// Когда экран менялся, знает tmux, с точностью до секунды.
			tm.changedAt = p.Activity
		case isNew:
			tm.changedAt = now
		case sum != tm.hash:
			tm.changedAt = now
			tm.changes++
		default:
			tm.changes = 0
		}
		tm.hash = sum
		tm.output = tm.output || strings.TrimSpace(s.screen) != ""
		tm.live.LastLine = LastLine(s.screen)
	}
	if tm.changedAt.IsZero() || tm.changedAt.After(now) {
		tm.changedAt = now
	}

	state, since := tm.live.State, now
	switch {
	case p.Dead:
		state, since = model.StateExited, p.DiedAt
		if p.ExitCode != 0 {
			state = model.StateError
		}
		tm.live.ExitCode = p.ExitCode
	case override != "" && override != model.StateIdle:
		state = override
	case !s.ok && state != "":
		// Экран не сняли: состояние прежнее.
	case !tm.output:
		state, since = model.StateStarting, p.Created
	case now.Sub(tm.changedAt) >= t.threshold(tm.key):
		// Агент ждёт с тех пор, как экран замер, а не с тех пор, как это заметили.
		state, since = model.StateWaiting, tm.changedAt
	case state == model.StateWaiting && tm.changes < confirmChanges:
		// Одна перерисовка: агент всё ещё ждёт.
	default:
		state = model.StateWorking
	}
	if state == tm.live.State {
		return
	}
	if since.IsZero() || since.After(now) {
		since = now
	}
	tm.live.State, tm.live.Since = state, since
	switch {
	case startup:
		tm.live.Unseen = false
	case state == model.StateWaiting || state == model.StateError:
		// Закончил ход, пока человек не смотрел: «новое».
		tm.live.Unseen = t.viewing[tm.key] == 0
	default:
		tm.live.Unseen = false
	}
}

func (t *Tracker) threshold(key string) time.Duration {
	agent, _, _ := strings.Cut(key, ":")
	if d, ok := t.opts.Thresholds[agent]; ok && d > 0 {
		return d
	}
	return t.opts.Threshold
}

// Get — состояние сессии по ключу; false — терминала у сессии нет.
func (t *Tracker) Get(key string) (model.Live, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tm := t.byKey(key); tm != nil {
		return tm.live, true
	}
	return model.Live{}, false
}

// All — состояния всех открытых терминалов по ключам сессий.
func (t *Tracker) All() map[string]model.Live {
	t.mu.Lock()
	defer t.mu.Unlock()
	all := make(map[string]model.Live, len(t.terms))
	for _, tm := range t.terms {
		all[tm.key] = tm.live
	}
	return all
}

func (t *Tracker) byKey(key string) *term {
	for _, tm := range t.terms {
		if tm.key == key {
			return tm
		}
	}
	return nil
}

// MarkSeen снимает отметку «новое»: человек посмотрел сессию.
func (t *Tracker) MarkSeen(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen(key)
}

func (t *Tracker) seen(key string) {
	if tm := t.byKey(key); tm != nil && tm.live.Unseen {
		tm.live.Unseen = false
		t.notify(map[string]model.Live{key: tm.live})
	}
}

// View отмечает, что страница показывает терминал сессии: пока он на экране, закончивший ход
// агент не получает отметку «новое». Возвращает функцию, которую нужно вызвать, когда страница
// терминал больше не показывает.
func (t *Tracker) View(key string) (release func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.viewing[key]++
	t.seen(key)
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.viewing[key]--; t.viewing[key] <= 0 {
				delete(t.viewing, key)
			}
		})
	}
}

// Subscription — изменения состояний для потока событий. Сигнал в C значит, что есть изменения;
// забрать их — Take. Изменения копятся, пока их не забрали: подписчик их не теряет,
// а по одному ключу получает только последнее.
type Subscription struct {
	C       <-chan struct{}
	c       chan struct{}
	t       *Tracker
	pending map[string]model.Live
}

// Subscribe — подписка на изменения. Закрытый терминал приходит как состояние idle.
func (t *Tracker) Subscribe() *Subscription {
	c := make(chan struct{}, 1)
	s := &Subscription{C: c, c: c, t: t, pending: map[string]model.Live{}}
	t.mu.Lock()
	t.subs[s] = struct{}{}
	t.mu.Unlock()
	return s
}

// Take забирает накопленные изменения: ключ сессии → новое состояние.
func (s *Subscription) Take() map[string]model.Live {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	p := s.pending
	s.pending = map[string]model.Live{}
	return p
}

// Close отменяет подписку.
func (s *Subscription) Close() {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	delete(s.t.subs, s)
}

func (t *Tracker) notify(changes map[string]model.Live) {
	if len(changes) == 0 {
		return
	}
	for s := range t.subs {
		for k, v := range changes {
			s.pending[k] = v
		}
		select {
		case s.c <- struct{}{}:
		default:
		}
	}
}
