package server

import (
	"cmp"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/catalog"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/remote"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/tmux"
)

// viewCtx — всё, из чего собираются SessionView, снятое один раз на ответ или пачку событий.
type viewCtx struct {
	s         *Server
	marks     store.State
	set       settings.Settings
	tempDirs  []string
	live      map[string]model.Live
	registry  map[string]agents.LiveStatus
	agents    map[string]agents.Agent
	installed map[string]bool
}

func (s *Server) viewCtx() *viewCtx {
	c := &viewCtx{s: s, marks: s.store.Snapshot(), set: s.settings.Get(), live: s.tracker.All(),
		agents: map[string]agents.Agent{}, installed: map[string]bool{}}
	c.tempDirs = c.set.TempDirPaths()
	for _, a := range s.allAgents() {
		c.agents[a.ID()] = a
		c.installed[a.ID()] = a.Enabled() && a.Program() != ""
	}
	s.mu.Lock()
	c.registry = s.registry
	s.mu.Unlock()
	return c
}

// byAliases — подписи запуска без человека, которые в настройках называются иначе:
// grok -p пишет в журнал session_kind headless.
var byAliases = map[string]string{"grok headless": "grok -p"}

// service — служебная сессия: во временной папке или запущена без человека командой из
// настроек. Убрав команду из настроек, человек возвращает такие сессии в списки.
func (c *viewCtx) service(sess model.Session) bool {
	if catalog.Temp(sess, c.tempDirs) {
		return true
	}
	if !sess.Auto {
		return false
	}
	by := sess.By
	if alias, ok := byAliases[by]; ok {
		by = alias
	}
	return by == "" || slices.Contains(c.set.Service.AutoCommands, by)
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func (c *viewCtx) view(sess model.Session) SessionView {
	key := sess.Key()
	m := c.marks.Marks(key)
	v := SessionView{
		Key: key, Agent: sess.Agent, ID: sess.ID, Title: sess.Title, Project: sess.Project(), Repo: sess.Repo,
		Cwd: sess.Cwd, Branch: sess.Branch, Created: unix(sess.Created), Updated: unix(sess.Updated),
		Last: sess.Last, Pinned: m.Pinned, Done: m.Done, Hidden: m.Hidden, By: sess.By,
	}
	if m.Name != "" {
		v.Title, v.Original = m.Name, sess.Title
	}
	if m.Worktree != nil {
		v.Worktree = &WorktreeView{Path: m.Worktree.Path, Branch: m.Worktree.Branch, Repo: m.Worktree.Repo}
	}
	v.Service = c.service(sess)
	v.Missing = sess.Cwd != "" && !c.s.dirExists(sess.Cwd)
	if a := c.agents[sess.Agent]; a != nil && c.installed[sess.Agent] && sess.Cwd != "" && !v.Missing && hasJournal(sess) {
		v.Resumable = a.Resume(sess) != nil
	}
	if l, ok := c.live[key]; ok {
		v.State, v.Since, v.Unseen, v.LastLine, v.Terminal = l.State, unix(l.Since), l.Unseen, l.LastLine, true
		if l.State == model.StateError {
			v.ExitCode = l.ExitCode
		}
	} else if st, ok := c.registry[key]; ok {
		// Агент идёт с сессией вне «Нити»: его слово о состоянии — единственное.
		v.State, v.Since, v.Where = st.State, unix(st.Since), st.Where
	} else {
		v.State = model.StateIdle
	}
	return v
}

// hasJournal — сессия найдена по журналу. У запущенной «Нитью» сессии, которой ещё нет
// в каталоге, журнала нет: продолжать её нечего.
func hasJournal(sess model.Session) bool { return sess.Journal != "" || sess.Meta != "" }

// projectPath — папка проекта сессии: основной репозиторий, а вне git — папка сессии.
func projectPath(sess model.Session) string {
	if sess.Repo != "" {
		return filepath.Clean(sess.Repo)
	}
	if sess.Cwd == "" {
		return ""
	}
	return filepath.Clean(sess.Cwd)
}

// projects — проекты из папок сессий и закреплённые человеком. Проект только из служебных
// сессий в панель не попадает.
func (c *viewCtx) projects(list []model.Session) []ProjectView {
	byPath := map[string]*ProjectView{}
	for _, sess := range list {
		path := projectPath(sess)
		if path == "" || c.service(sess) {
			continue
		}
		p := byPath[path]
		if p == nil {
			p = &ProjectView{Name: filepath.Base(path), Path: path, Git: sess.Repo != ""}
			byPath[path] = p
		}
		p.Sessions++
		p.Updated = max(p.Updated, unix(sess.Updated))
	}
	for path, pref := range c.marks.Projects {
		if byPath[path] == nil && pref.Pinned {
			byPath[path] = &ProjectView{Name: filepath.Base(path), Path: path, Git: catalog.RepoRoot(path) != ""}
		}
	}
	out := make([]ProjectView, 0, len(byPath))
	for path, p := range byPath {
		pref := c.marks.Projects[path]
		p.Pinned, p.Hidden, p.DefaultAgent = pref.Pinned, pref.Hidden, pref.DefaultAgent
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b ProjectView) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		if n := cmp.Compare(b.Updated, a.Updated); n != 0 {
			return n
		}
		return cmp.Compare(a.Path, b.Path)
	})
	return out
}

// sessions — все сессии: из каталога и запущенные «Нитью», которых в каталоге ещё нет.
// Сначала недавние.
func (s *Server) sessions() []model.Session {
	list := s.catalog.Sessions()
	s.mu.Lock()
	var extra []model.Session
	for key, p := range s.pending {
		if _, ok := s.catalog.Get(key); !ok {
			extra = append(extra, p.session())
		}
	}
	s.mu.Unlock()
	if len(extra) == 0 {
		return list
	}
	list = append(list, extra...)
	slices.SortStableFunc(list, func(a, b model.Session) int { return b.Updated.Compare(a.Updated) })
	return list
}

// session — сессия по ключу из каталога или среди запущенных, которых в каталоге ещё нет.
func (s *Server) session(key string) (model.Session, bool) {
	if sess, ok := s.catalog.Get(key); ok {
		return sess, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending[key]; p != nil {
		return p.session(), true
	}
	return model.Session{}, false
}

func (s *Server) agentViews(counts map[string]int) []AgentView {
	var out []AgentView
	for _, a := range s.allAgents() {
		info := a.Info()
		out = append(out, AgentView{
			ID: info.ID, Name: info.Name, Short: info.Short, Enabled: a.Enabled(), Program: a.Program(),
			Home: a.Home(), HomeSource: a.HomeSource(), HomeFound: isDir(a.Home()), Sessions: counts[info.ID],
			Resumable: info.Resumable, Runner: info.Runner, SkipFlag: info.SkipFlag,
		})
	}
	return out
}

func progressView(p catalog.Progress) ProgressView {
	agents := p.Agents
	if agents == nil {
		agents = map[string]string{}
	}
	return ProgressView{Done: p.Done, Total: p.Total, Ready: p.Ready, Agents: agents}
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) error {
	c := s.viewCtx()
	list := s.sessions()
	counts := map[string]int{}
	views := []SessionView{}
	total := SessionCounts{All: len(list)}
	for _, sess := range list {
		counts[sess.Agent]++
		if c.service(sess) {
			total.Service++
			continue
		}
		views = append(views, c.view(sess))
	}
	writeJSON(w, http.StatusOK, Bootstrap{Env: s.env(r, false), Agents: s.agentViews(counts), Projects: c.projects(list),
		Sessions: views, Counts: total, Progress: progressView(s.catalog.Progress()), Settings: c.set})
	return nil
}

// handleSessions — история с фильтрами: project (папка проекта), agent, service=1 (вместе со
// служебными) — и страницами: limit, cursor из X-Next-Cursor прошлой страницы или before
// (время Unix). Порядок — updated по убыванию, при равном — key по возрастанию.
// X-Total-Count — сколько сессий подходит под фильтры без учёта страниц.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	project := q.Get("project")
	if project != "" {
		project = filepath.Clean(project)
	}
	agent := q.Get("agent")
	service := q.Get("service") == "1"
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	var after *pageCursor
	if raw := q.Get("cursor"); raw != "" {
		cur, ok := parseCursor(raw)
		if !ok {
			return apiErr(http.StatusBadRequest, "bad_cursor")
		}
		after = &cur
	}
	c := s.viewCtx()
	list := s.sessions()
	// Порядок по секундам, а не по времени каталога: курсор знает только секунды, и сессии
	// одной секунды иначе терялись бы между страницами.
	slices.SortFunc(list, func(a, b model.Session) int {
		return pageCursor{unix(a.Updated), a.Key()}.compare(pageCursor{unix(b.Updated), b.Key()})
	})
	out := []SessionView{}
	total, next := 0, ""
	for _, sess := range list {
		if project != "" && projectPath(sess) != project || agent != "" && sess.Agent != agent {
			continue
		}
		if !service && c.service(sess) {
			continue
		}
		total++
		at := pageCursor{unix(sess.Updated), sess.Key()}
		if before > 0 && at.updated >= before || after != nil && at.compare(*after) <= 0 {
			continue
		}
		if limit > 0 && len(out) >= limit {
			// За страницей есть ещё: следующая начнётся после её последней сессии.
			if next == "" {
				last := out[len(out)-1]
				next = pageCursor{last.Updated, last.Key}.String()
			}
			continue
		}
		out = append(out, c.view(sess))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	w.Header().Set("X-Next-Cursor", next)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// pageCursor — место в истории: <updated>:<key>. Ключ сам содержит двоеточие, поэтому
// время — до первого.
type pageCursor struct {
	updated int64
	key     string
}

func parseCursor(raw string) (pageCursor, bool) {
	at, key, ok := strings.Cut(raw, ":")
	updated, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil {
		return pageCursor{}, false
	}
	return pageCursor{updated, key}, true
}

func (p pageCursor) String() string { return strconv.FormatInt(p.updated, 10) + ":" + p.key }

// compare — меньше нуля, если p в списке выше q: новее или в ту же секунду с меньшим ключом.
func (p pageCursor) compare(q pageCursor) int {
	if n := cmp.Compare(q.updated, p.updated); n != 0 {
		return n
	}
	return cmp.Compare(p.key, q.key)
}

// tmuxInfo — версия и путь tmux; tmux -V запускается не чаще раза в минуту.
var tmuxInfo struct {
	sync.Mutex
	at      time.Time
	path    string
	version string
}

func (s *Server) env(r *http.Request, fresh bool) EnvView {
	tmuxInfo.Lock()
	if fresh || time.Since(tmuxInfo.at) > time.Minute {
		tmuxInfo.path, tmuxInfo.version = "", ""
		if path, err := exec.LookPath("tmux"); err == nil {
			tmuxInfo.path = path
			if out, err := exec.Command(path, "-V").Output(); err == nil {
				tmuxInfo.version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "tmux"))
			}
		}
		tmuxInfo.at = time.Now()
	}
	path, version := tmuxInfo.path, tmuxInfo.version
	tmuxInfo.Unlock()
	_, codeErr := exec.LookPath("code")
	return EnvView{
		Version: s.opts.Version, Addr: "127.0.0.1:" + strconv.Itoa(s.opts.Port),
		Tmux: path != "", TmuxVersion: version, TmuxPath: path,
		WSL: inWSL(), VSCode: codeErr == nil, Platform: runtime.GOOS,
		Via: via(r), Remote: s.remoteBrief(),
		// Папка данных — та, где лежит state.json: рядом с ним «Нить» кладёт кэш каталога и ключи телефона.
		SettingsPath: absPath(s.settings.Path()), DataDir: absPath(filepath.Dir(s.store.Path())),
	}
}

// absPath — путь от корня: относительный путь из переменной окружения человеку ничего не скажет.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// via — откуда пришёл запрос: local или remote.
func via(r *http.Request) string {
	if remote.FromRemote(r) {
		return "remote"
	}
	return "local"
}

// handleEnv — проверка для первого запуска. «Проверить снова» после установки агента:
// агенты пересобираются, а каталог подхватывает появившиеся папки.
func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) error {
	s.syncAgents()
	counts := map[string]int{}
	for _, sess := range s.catalog.Sessions() {
		counts[sess.Agent]++
	}
	writeJSON(w, http.StatusOK, EnvCheck{Env: s.env(r, true), Agents: s.agentViews(counts)})
	return nil
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("agent")
	a := s.agent(id)
	if a == nil {
		return apiErr(http.StatusNotFound, "unknown_agent", "value", id)
	}
	m := a.Models()
	view := ModelsView{DefaultModel: m.DefaultModel, DefaultEffort: m.DefaultEffort, Models: []ModelView{},
		Efforts: m.Efforts}
	if view.Efforts == nil {
		view.Efforts = []string{}
	}
	for _, x := range m.Models {
		view.Models = append(view.Models, ModelView{ID: x.ID, Name: x.Name, Efforts: x.Efforts, DefaultEffort: x.DefaultEffort})
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// terminalName — имя сессии tmux с терминалом сессии key: по метке @nit_key, по записи
// «Нити» или по имени, которое давала первая версия.
func (s *Server) terminalName(key string) (string, bool) {
	if l, ok := s.tracker.Get(key); ok && l.Terminal != "" && tmux.Exists(l.Terminal) {
		return l.Terminal, true
	}
	panes, _ := tmux.List()
	for _, p := range panes {
		if p.Key == key {
			return p.Name, true
		}
	}
	if t := s.store.Marks(key).Terminal; t != nil && tmux.Exists(t.Tmux) {
		return t.Tmux, true
	}
	if name := v1Name(key); tmux.Exists(name) {
		return name, true
	}
	return "", false
}
