// Package catalog собирает сессии всех включённых агентов, хранит разбор журналов в кэше на
// диске и следит за изменениями: раз в несколько секунд сверяет размер и время изменения
// файлов и перечитывает только изменившиеся. Об изменениях сообщает подписчикам — сервер
// передаёт их в интерфейс.
package catalog

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
)

const (
	defaultInterval = 3 * time.Second
	// emitEvery — не чаще этого при полном обходе подписчики узнают о ходе чтения.
	emitEvery = 100 * time.Millisecond
	// saveEvery — не чаще этого кэш пишется на диск, пока каталог работает.
	saveEvery = 30 * time.Second
)

// Options — что читать и где хранить кэш.
type Options struct {
	Agents    []agents.Agent // включённые агенты
	CacheFile string         // кэш разбора журналов; пусто — без кэша на диске
	Interval  time.Duration  // как часто искать изменения; 0 — раз в 3 секунды
}

// Progress — ход полного обхода: при запуске и после смены агентов.
type Progress struct {
	Done   int               // прочитано единиц (файлов с сессиями)
	Total  int               // всего единиц
	Ready  bool              // первый обход закончен
	Agents map[string]string // id агента → waiting, reading или done
}

// Event — что изменилось в каталоге.
type Event struct {
	Changed  []string // ключи новых и изменившихся сессий
	Removed  []string // ключи исчезнувших сессий
	Progress Progress
}

// Catalog — сессии всех агентов. Читают его из любых горутин; обходом занимается Run.
type Catalog struct {
	cacheFile string
	interval  time.Duration

	mu          sync.Mutex
	agents      []agents.Agent
	sessions    map[string]model.Session
	progress    Progress
	subscribers map[int]func(Event)
	nextID      int
	fullPending bool
	wake        chan struct{}
	ready       chan struct{}

	// Дальше — только горутина Run.
	caches  map[string]*agents.Cache
	repos   *repos
	saved   []byte
	savedAt time.Time
}

func New(o Options) *Catalog {
	interval := o.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Catalog{
		cacheFile:   o.CacheFile,
		interval:    interval,
		agents:      o.Agents,
		sessions:    map[string]model.Session{},
		progress:    Progress{Agents: map[string]string{}},
		subscribers: map[int]func(Event){},
		wake:        make(chan struct{}, 1),
		ready:       make(chan struct{}),
		caches:      map[string]*agents.Cache{},
		repos:       newRepos(),
	}
}

// Run читает кэш, обходит журналы всех агентов и затем ищет изменения, пока не отменён ctx.
// Перед выходом пишет кэш. Вызывается один раз.
func (c *Catalog) Run(ctx context.Context) {
	c.load()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	full := true
	for {
		c.pass(full)
		select {
		case <-c.ready:
			if time.Since(c.savedAt) >= saveEvery {
				c.save()
			}
		default:
			close(c.ready)
			c.save()
		}
		select {
		case <-ctx.Done():
			c.save()
			return
		case <-ticker.C:
			full = false
		case <-c.wake:
			c.mu.Lock()
			full, c.fullPending = c.fullPending, false
			c.mu.Unlock()
		}
	}
}

// Ready закрывается, когда первый обход закончен.
func (c *Catalog) Ready() <-chan struct{} { return c.ready }

// Refresh просит обойти журналы сейчас, не дожидаясь срока. Если обход уже идёт, следующий
// начнётся сразу после него.
func (c *Catalog) Refresh() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// SetAgents меняет набор агентов после правки настроек: каталог обходит всё заново с ходом
// чтения, сессии выключенных агентов исчезают.
func (c *Catalog) SetAgents(list []agents.Agent) {
	c.mu.Lock()
	c.agents = list
	c.fullPending = true
	c.mu.Unlock()
	c.Refresh()
}

// Agents — агенты, которые каталог читает сейчас.
func (c *Catalog) Agents() []agents.Agent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.agents)
}

// Agent — агент по id из текущего набора или nil.
func (c *Catalog) Agent(id string) agents.Agent {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.agents {
		if a.ID() == id {
			return a
		}
	}
	return nil
}

// Get — сессия по ключу «агент:id».
func (c *Catalog) Get(key string) (model.Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[key]
	return s, ok
}

// Sessions — все сессии, сначала недавние.
func (c *Catalog) Sessions() []model.Session {
	c.mu.Lock()
	list := slices.Collect(maps.Values(c.sessions))
	c.mu.Unlock()
	slices.SortFunc(list, func(a, b model.Session) int {
		if n := b.Updated.Compare(a.Updated); n != 0 {
			return n
		}
		return cmp.Compare(a.Key(), b.Key())
	})
	return list
}

// Progress — ход полного обхода.
func (c *Catalog) Progress() Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progressLocked()
}

func (c *Catalog) progressLocked() Progress {
	p := c.progress
	p.Agents = maps.Clone(c.progress.Agents)
	return p
}

// Subscribe подписывает fn на изменения. fn вызывается из горутины обхода и не должна
// надолго её задерживать. cancel отменяет подписку.
func (c *Catalog) Subscribe(fn func(Event)) (cancel func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	c.subscribers[id] = fn
	return func() {
		c.mu.Lock()
		delete(c.subscribers, id)
		c.mu.Unlock()
	}
}

func (c *Catalog) emit(changed, removed []string) {
	c.mu.Lock()
	event := Event{Changed: changed, Removed: removed, Progress: c.progressLocked()}
	subscribers := slices.Collect(maps.Values(c.subscribers))
	c.mu.Unlock()
	for _, fn := range subscribers {
		fn(event)
	}
}

// pass — один обход всех агентов. Полный обход сообщает ход чтения.
func (c *Catalog) pass(full bool) {
	list := c.Agents()
	units := make([][]string, len(list))
	total := 0
	for i, a := range list {
		units[i] = a.Discover()
		total += len(units[i])
	}
	if full {
		c.mu.Lock()
		c.progress = Progress{Total: total, Ready: c.progress.Ready, Agents: map[string]string{}}
		for _, a := range list {
			c.progress.Agents[a.ID()] = "waiting"
		}
		c.mu.Unlock()
		c.emit(nil, nil)
	}
	seen := map[string]bool{}
	var changed []string
	emitted := time.Now()
	for i, a := range list {
		cache := c.caches[a.ID()]
		if cache == nil {
			cache = &agents.Cache{}
			c.caches[a.ID()] = cache
		}
		c.setAgentState(full, a.ID(), "reading")
		a.Scan(cache, units[i], func(found []model.Session) {
			// Корень репозитория ищется до блокировки: это чтение файлов.
			for j := range found {
				found[j].Repo = c.repos.root(found[j].Cwd)
			}
			c.mu.Lock()
			for _, s := range found {
				key := s.Key()
				seen[key] = true
				if old, ok := c.sessions[key]; !ok || !same(old, s) {
					c.sessions[key] = s
					changed = append(changed, key)
				}
			}
			if full {
				c.progress.Done++
			}
			c.mu.Unlock()
			if full && time.Since(emitted) >= emitEvery {
				c.emit(changed, nil)
				changed, emitted = nil, time.Now()
			}
		})
		c.setAgentState(full, a.ID(), "done")
	}
	for id := range c.caches {
		if !slices.ContainsFunc(list, func(a agents.Agent) bool { return a.ID() == id }) {
			delete(c.caches, id)
		}
	}
	var removed []string
	c.mu.Lock()
	for key := range c.sessions {
		if !seen[key] {
			delete(c.sessions, key)
			removed = append(removed, key)
		}
	}
	if full {
		c.progress.Ready = true
	}
	c.mu.Unlock()
	if full || len(changed) > 0 || len(removed) > 0 {
		c.emit(changed, removed)
	}
}

func (c *Catalog) setAgentState(full bool, id, state string) {
	if !full {
		return
	}
	c.mu.Lock()
	c.progress.Agents[id] = state
	c.mu.Unlock()
}

// same — сессии одинаковы; время сравнивается как момент, а не как значение.
func same(a, b model.Session) bool {
	if !a.Created.Equal(b.Created) || !a.Updated.Equal(b.Updated) {
		return false
	}
	a.Created, a.Updated, b.Created, b.Updated = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return a == b
}

// cacheFile — кэш на диске. Он одноразовый: при любой ошибке читается всё заново.
type cacheFile struct {
	Version int
	Agents  map[string]*agents.Cache
	Repos   map[string]string
}

func (c *Catalog) load() {
	if c.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(c.cacheFile)
	if err != nil {
		return
	}
	var file cacheFile
	if json.Unmarshal(data, &file) != nil || file.Version != agents.CacheVersion {
		return
	}
	for id, cache := range file.Agents {
		if cache != nil {
			c.caches[id] = cache
		}
	}
	for dir, root := range file.Repos {
		c.repos.known[dir] = root
	}
	c.saved = data
}

// save пишет кэш, если он изменился. Файл заменяется целиком, чтобы читатель не увидел
// половину записи.
func (c *Catalog) save() {
	if c.cacheFile == "" {
		return
	}
	c.savedAt = time.Now()
	c.mu.Lock()
	dirs := map[string]bool{}
	for _, s := range c.sessions {
		dirs[s.Cwd] = true
	}
	c.mu.Unlock()
	maps.DeleteFunc(c.repos.known, func(dir, _ string) bool { return !dirs[dir] })
	data, err := json.Marshal(cacheFile{Version: agents.CacheVersion, Agents: c.caches, Repos: c.repos.known})
	if err != nil || bytes.Equal(data, c.saved) {
		return
	}
	dir := filepath.Dir(c.cacheFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), c.cacheFile)
	}
	if err != nil {
		os.Remove(f.Name())
		return
	}
	c.saved = data
}
