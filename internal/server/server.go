// Package server отдаёт интерфейс «Нити» и её HTTP API: сессии всех агентов с пометками
// человека и состоянием терминалов, запуск и продолжение агентов в tmux, поиск и поток событий.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/catalog"
	"github.com/genhoi/ai-threads/internal/live"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/remote"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/summary"
)

type Options struct {
	// Port — порт, на котором слушает сервер. Из него складываются разрешённые Host.
	Port     int
	Version  string
	Settings *settings.File
	Store    *store.Store
	// CacheFile — кэш разбора журналов; пусто — без кэша на диске.
	CacheFile string
	// StateFile — последние состояния терминалов для экрана после перезагрузки; пусто — не хранить.
	StateFile string
	// RemoteKeys — ключ входа с телефона и устройства (remote.json); nil — из папки данных.
	RemoteKeys *remote.Keys
	// RemoteNode — узел Tailscale для доступа с телефона; nil — настоящий tsnet. Тесты
	// подставляют узел со своим TLS-слушателем.
	RemoteNode remote.NodeFactory

	// Для тестов: как часто каталог ищет изменения и как часто смотрятся экраны терминалов.
	CatalogInterval time.Duration
	LiveInterval    time.Duration
	LiveThreshold   time.Duration
}

// Server — «Нить» целиком: каталог, слежение за терминалами, пометки, настройки и API над ними.
type Server struct {
	opts     Options
	settings *settings.File
	store    *store.Store
	catalog  *catalog.Catalog
	tracker  *live.Tracker
	handler  http.Handler    // запросы с компьютера: guard перед mux
	mux      http.Handler    // все маршруты; с телефона к ним ведёт remote.Guard
	remote   *remote.Manager // доступ с телефона
	events   *hub
	static   *staticFiles
	conv     *convCache
	full     *fullSearch
	jobs     *summary.Jobs // сводки, которые пишут агенты без человека
	done     chan struct{} // закрывается, когда Run завершается: потоки событий уходят
	resolve  chan struct{} // будит поиск настоящих ключей запущенных сессий

	mu       sync.Mutex
	agents   []agents.Agent           // все шесть по текущим настройкам
	agentCfg map[string]agents.Config // настройки, по которым они собраны
	registry map[string]agents.LiveStatus
	pending  map[string]*pendingLaunch // запущенные «Нитью» сессии, которых ещё нет в каталоге
	launches map[string]LaunchInfo     // с какой моделью запускала «Нить»; до перезапуска
	dirs     map[string]dirCheck
	lastSeen *lastStates // последние состояния терминалов для экрана после перезагрузки
}

// New собирает сервер. Каталог и слежение за терминалами работают, пока идёт Run.
func New(o Options) *Server {
	if o.Settings == nil {
		o.Settings = settings.New(settings.Path())
	}
	if o.Store == nil {
		st, err := store.Open(store.DefaultPath())
		if err != nil {
			panic(err)
		}
		o.Store = st
	}
	if o.RemoteKeys == nil {
		keys, err := remote.Open(filepath.Join(store.DataDir(), "remote.json"))
		if err != nil {
			panic(err)
		}
		o.RemoteKeys = keys
	}
	s := &Server{
		opts:     o,
		settings: o.Settings,
		store:    o.Store,
		events:   newHub(),
		conv:     newConvCache(),
		full:     &fullSearch{},
		done:     make(chan struct{}),
		resolve:  make(chan struct{}, 1),
		registry: map[string]agents.LiveStatus{},
		pending:  map[string]*pendingLaunch{},
		launches: map[string]LaunchInfo{},
		dirs:     map[string]dirCheck{},
		lastSeen: loadLastStates(o.StateFile),
	}
	cfg := agentConfigs(s.settings.Get())
	s.agents, s.agentCfg = agents.All(cfg), cfg
	s.catalog = catalog.New(catalog.Options{Agents: agents.Active(s.agents), CacheFile: o.CacheFile,
		Interval: o.CatalogInterval})
	s.tracker = live.New(live.Options{Interval: o.LiveInterval, Threshold: o.LiveThreshold, Override: s.override})
	s.jobs = summary.NewJobs(s.jobChanged)
	s.static = loadStatic()
	s.mux = s.routes()
	s.handler = guard(o.Port, s.mux)
	// С телефона — те же маршруты, но за своей проверкой: Host узла, устройство и Origin.
	s.remote = remote.NewManager(remote.Options{Keys: o.RemoteKeys, Handler: noStore(s.mux),
		NewNode: o.RemoteNode, OnChange: s.events.touchRemote})
	s.store.OnChange(s.storeChanged)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// Run читает журналы, следит за терминалами и рассылает изменения, пока не отменён ctx.
// Перед выходом пишет кэш каталога.
func (s *Server) Run(ctx context.Context) {
	var wg sync.WaitGroup
	cancelCatalog := s.catalog.Subscribe(s.catalogChanged)
	defer cancelCatalog()
	sub := s.tracker.Subscribe()
	defer sub.Close()
	s.restorePending()
	s.syncRemote()

	wg.Add(5)
	go func() { defer wg.Done(); s.catalog.Run(ctx) }()
	go func() { defer wg.Done(); s.tracker.Run(ctx) }()
	go func() { defer wg.Done(); s.watchTracker(ctx, sub) }()
	go func() { defer wg.Done(); s.pump(ctx) }()
	go func() { defer wg.Done(); s.background(ctx) }()
	<-ctx.Done()
	close(s.done)
	// Узел Tailscale закрывается при выходе, не раньше: до этого он обслуживает телефон.
	s.remote.Close()
	s.full.cancel()
	s.jobs.Close()
	wg.Wait()
	s.saveLastStates()
}

// background — редкие дела: найти ID запущенных сессий, подхватить правку настроек руками
// и новую папку агента, запомнить состояния терминалов.
func (s *Server) background(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.resolve:
			s.resolvePending()
		case <-tick.C:
			s.syncAgents()
			s.syncRemote()
			s.resolvePending()
			s.saveLastStates()
		}
	}
}

// agentConfigs — настройки агентов для пакета agents.
func agentConfigs(st settings.Settings) map[string]agents.Config {
	cfg := map[string]agents.Config{}
	for _, id := range agents.IDs {
		a := st.Agent(id)
		cfg[id] = agents.Config{Home: a.Home, Program: a.Program, Disabled: !a.Enabled, SkipApprovals: a.SkipApprovals}
	}
	return cfg
}

// syncAgents пересобирает агентов, если настройки поменялись, и даёт каталогу новый набор,
// если поменялись папки, программы или включённые агенты, или у агента появилась папка.
func (s *Server) syncAgents() {
	cfg := agentConfigs(s.settings.Get())
	s.mu.Lock()
	scanChanged := false
	if !maps.Equal(cfg, s.agentCfg) {
		for id, c := range cfg {
			old := s.agentCfg[id]
			old.SkipApprovals = c.SkipApprovals
			scanChanged = scanChanged || old != c
		}
		s.agents, s.agentCfg = agents.All(cfg), cfg
	}
	list := s.agents
	s.mu.Unlock()
	active := agents.Active(list)
	current := s.catalog.Agents()
	if !scanChanged && slices.EqualFunc(active, current, func(a, b agents.Agent) bool {
		return a.ID() == b.ID() && a.Home() == b.Home()
	}) {
		return
	}
	s.catalog.SetAgents(active)
}

// agent — агент по id из текущего набора; nil для неизвестного.
func (s *Server) agent(id string) agents.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.ID() == id {
			return a
		}
	}
	return nil
}

func (s *Server) allAgents() []agents.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.agents)
}

// override — состояния из реестров агентов для слежения за терминалами. Догадки (реестр Grok
// знает только, что процесс жив) не передаются: в терминале «Нити» экран точнее. Заодно
// запоминает весь реестр: по нему видно, что сессия идёт вне «Нити».
func (s *Server) override() map[string]model.State {
	has := func(key string) bool {
		if _, ok := s.catalog.Get(key); ok {
			return true
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.pending[key] != nil
	}
	found := agents.LiveAll(s.catalog.Agents(), has)
	s.mu.Lock()
	var changed []string
	for key, st := range found {
		if old, ok := s.registry[key]; !ok || old != st {
			changed = append(changed, key)
		}
	}
	for key := range s.registry {
		if _, ok := found[key]; !ok {
			changed = append(changed, key)
		}
	}
	s.registry = found
	s.mu.Unlock()
	if len(changed) > 0 {
		s.events.touch(changed...)
	}
	states := make(map[string]model.State, len(found))
	for key, st := range found {
		if !st.Guess {
			states[key] = st.State
		}
	}
	return states
}

// elsewhere — запись реестра о сессии, если агент идёт с ней вне «Нити».
func (s *Server) elsewhere(key string) (agents.LiveStatus, bool) {
	if l, ok := s.tracker.Get(key); ok && l.Terminal != "" {
		return agents.LiveStatus{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.registry[key]
	return st, ok
}

func (s *Server) catalogChanged(e catalog.Event) {
	s.events.touch(e.Changed...)
	s.events.touch(e.Removed...)
	s.events.progress(e.Progress)
	if len(e.Changed) > 0 {
		select {
		case s.resolve <- struct{}{}:
		default:
		}
	}
}

func (s *Server) storeChanged(c store.Change) {
	switch c.Kind {
	case store.KindSession, store.KindTerminal:
		s.events.touch(c.Key)
	case store.KindProject:
		s.events.touchProjects()
	case store.KindAll:
		s.events.touchAll()
	case store.KindDigest:
		if c.Key != "" {
			s.digestChanged(c.Key)
		}
	}
}

// watchTracker передаёт изменения терминалов в поток событий.
func (s *Server) watchTracker(ctx context.Context, sub *live.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.C:
		}
		changes := sub.Take()
		keys := make([]string, 0, len(changes))
		for key, l := range changes {
			keys = append(keys, key)
			if l.State == model.StateIdle {
				s.terminalGone(key)
			}
		}
		s.events.touch(keys...)
	}
}

// dirCheck — есть ли папка; проверка живёт полминуты: папки на /mnt/c в WSL отвечают медленно.
type dirCheck struct {
	exists bool
	at     time.Time
}

func (s *Server) dirExists(dir string) bool {
	if dir == "" {
		return false
	}
	s.mu.Lock()
	c, ok := s.dirs[dir]
	s.mu.Unlock()
	if ok && time.Since(c.at) < 30*time.Second {
		return c.exists
	}
	exists := isDir(dir)
	s.mu.Lock()
	s.dirs[dir] = dirCheck{exists, time.Now()}
	s.mu.Unlock()
	return exists
}

func (s *Server) forgetDir(dir string) {
	s.mu.Lock()
	delete(s.dirs, dir)
	s.mu.Unlock()
}

// routes — все запросы API и раздача интерфейса.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if err := fn(w, r); err != nil {
				s.fail(w, r, err)
			}
		})
	}
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	h("GET /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	h("GET /api/sessions", s.handleSessions)
	h("GET /api/sessions/{key}", s.handleDetails)
	h("GET /api/sessions/{key}/finish", s.handleFinishCheck)
	h("POST /api/sessions/{key}/{action}", s.handleSessionAction)
	h("POST /api/project", s.handleProject)
	h("GET /api/launch/check", s.handleLaunchCheck)
	h("POST /api/launch", s.handleLaunch)
	h("GET /api/models", s.handleModels)
	mux.Handle("GET /api/terminal/{key}", s.terminalHandler())
	h("GET /api/terminal/{key}/screen", s.handleScreen)
	h("POST /api/terminal/{key}/keys", s.handleKeys)
	h("GET /api/search", s.handleSearch)
	h("POST /api/search/full", s.handleFullSearch)
	h("GET /api/digest", s.handleDigest)
	h("POST /api/digest", s.handleStartDigest)
	h("GET /api/jobs", s.handleJobs)
	h("POST /api/jobs/{id}/cancel", s.handleCancelJob)
	h("GET /api/reboot", s.handleReboot)
	h("POST /api/reboot", s.handleRebootAction)
	h("GET /api/settings", s.handleSettings)
	h("POST /api/settings", s.handleSaveSettings)
	h("GET /api/env", s.handleEnv)
	h("POST /api/reveal", s.handleReveal)
	h("GET /api/export", s.handleExport)
	h("POST /api/import", s.handleImport)
	// Управление доступом с телефона — только с компьютера: в ответах ключ входа.
	h("GET /api/remote", s.localOnly(s.handleRemote))
	h("POST /api/remote", s.localOnly(s.handleSetRemote))
	h("POST /api/remote/key", s.localOnly(s.handleRemoteKey))
	h("POST /api/remote/devices/{id}/revoke", s.localOnly(s.handleRevokeDevice))
	h("GET /api/remote/qr.svg", s.localOnly(s.handleRemoteQR))
	h("/api/", func(w http.ResponseWriter, r *http.Request) error { return apiErr(http.StatusNotFound, "no_route") })
	mux.Handle("/", s.static)
	return mux
}

// guard пускает только запросы к самой «Нити».
//
// Host проверяется от подмены DNS: чужой сайт может направить своё имя на 127.0.0.1,
// и браузер пошлёт запрос с этим именем. Origin проверяется у запросов, которые что-то
// меняют, и у WebSocket: иначе чужая страница могла бы печатать в терминал агента.
func guard(port int, next http.Handler) http.Handler {
	next = noStore(next)
	p := strconv.Itoa(port)
	hosts := map[string]bool{"127.0.0.1:" + p: true, "localhost:" + p: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			writeError(w, http.StatusForbidden, "host is not allowed")
			return
		}
		if changes(r) && r.Header.Get("Origin") != "http://"+r.Host {
			writeError(w, http.StatusForbidden, "origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// noStore — ответы API всегда свежие: состояние меняется каждую секунду.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func changes(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorView{Error: message})
}

// Пределы тела запроса: выгрузка с пометками бывает большой, остальное — маленькие объекты.
const (
	bodyLimit   = 1 << 20
	importLimit = 64 << 20
)

// decode читает тело запроса как JSON-объект. Пустое тело — нулевое значение, если allowEmpty.
func decode(r *http.Request, into any, limit int64, allowEmpty bool) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return apiErr(http.StatusBadRequest, "bad_json")
	}
	if int64(len(raw)) > limit {
		return apiErr(http.StatusRequestEntityTooLarge, "too_large")
	}
	if len(strings.TrimSpace(string(raw))) == 0 && allowEmpty {
		return nil
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil || probe == nil {
		return apiErr(http.StatusBadRequest, "bad_json")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return apiErr(http.StatusBadRequest, "bad_json")
	}
	return nil
}

// fail пишет ошибку обработчика: свою, пометок, настроек или любую другую как 500.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	lang := s.lang(r)
	var ae *apiError
	var inv *store.InvalidError
	var fields settings.Errors
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.status, ErrorView{Error: ae.text(lang), Fields: ae.fields})
	case errors.As(err, &inv):
		view := ErrorView{Error: inv.Text(lang)}
		if inv.Field != "" {
			view.Fields = map[string]string{inv.Field: inv.Text(lang)}
		}
		writeJSON(w, http.StatusBadRequest, view)
	case errors.As(err, &fields):
		view := ErrorView{Fields: map[string]string{}}
		var parts []string
		for _, fe := range fields {
			text := fe.Text(lang)
			if fe.Field == "" {
				parts = append(parts, text)
				continue
			}
			view.Fields[fe.Field] = text
			parts = append(parts, fe.Field+": "+text)
		}
		view.Error = strings.Join(parts, "; ")
		writeJSON(w, http.StatusBadRequest, view)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// lang — язык сообщений: из настроек, а при auto — по браузеру.
func (s *Server) lang(r *http.Request) string {
	switch l := s.settings.Get().Language; l {
	case "ru", "en":
		return l
	}
	if r != nil {
		for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
			tag := strings.ToLower(strings.TrimSpace(part))
			if strings.HasPrefix(tag, "ru") {
				return "ru"
			}
			if tag != "" {
				return "en"
			}
		}
	}
	return "ru"
}

// realPath — путь без ссылок; если папки нет — просто очищенный.
func realPath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// inside — папка dir совпадает с root или лежит внутри неё.
func inside(dir, root string) bool {
	if dir == "" || root == "" {
		return false
	}
	dir, root = realPath(dir), realPath(root)
	return dir == root || strings.HasPrefix(dir, strings.TrimRight(root, "/")+"/")
}
