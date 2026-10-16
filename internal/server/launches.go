package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/catalog"
	"github.com/genhoi/ai-threads/internal/launch"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/tmux"
)

const (
	// launchSlack — запас на округление времени в журналах: сессия, созданная чуть раньше
	// запуска по часам журнала, всё равно считается новой.
	launchSlack = 5 * time.Second
	maxPrompt   = 100_000
	maxRecent   = 5
)

// pendingLaunch — сессия, которую запустила «Нить», а в каталоге её ещё нет: агент не записал
// журнал. У агентов, которые не принимают ID при запуске, ключ временный (<агент>:new-…),
// и настоящий находится по журналу: тот же агент, та же папка, создан после запуска.
type pendingLaunch struct {
	key    string
	agent  string
	temp   bool
	cwd    string
	repo   string
	branch string
	title  string
	at     time.Time
	tmux   string
	known  map[string]bool // сессии агента в этой папке до запуска: их новой не считать
}

func (p *pendingLaunch) session() model.Session {
	_, id, _ := strings.Cut(p.key, ":")
	return model.Session{Agent: p.agent, ID: id, Title: p.title, Cwd: p.cwd, Repo: p.repo, Branch: p.branch,
		Created: p.at, Updated: p.at}
}

// restorePending — после перезапуска «Нити» терминалы, чьих сессий ещё нет в каталоге,
// снова ждут свой журнал. Сессии из каталога отсеет первый же resolvePending.
func (s *Server) restorePending() {
	panes, err := tmux.List()
	if err != nil {
		return
	}
	alive := map[string]string{}
	for _, p := range panes {
		alive[p.Name] = p.Key
	}
	st := s.store.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, t := range st.Terminals {
		if alive[t.Tmux] != key {
			continue
		}
		agent, id, _ := strings.Cut(key, ":")
		s.pending[key] = &pendingLaunch{key: key, agent: agent, temp: strings.HasPrefix(id, "new-"), cwd: t.Cwd,
			repo: catalog.RepoRoot(t.Cwd), at: time.Unix(t.Started, 0), tmux: t.Tmux}
	}
}

// resolvePending находит запущенные сессии в каталоге: у настоящих ключей просто перестаёт
// их подменять, временным ищет настоящий ключ.
func (s *Server) resolvePending() {
	s.mu.Lock()
	var temps []*pendingLaunch
	var reals []string
	for key, p := range s.pending {
		if p.temp {
			temps = append(temps, p)
		} else {
			reals = append(reals, key)
		}
	}
	s.mu.Unlock()
	for _, key := range reals {
		if _, ok := s.catalog.Get(key); ok {
			s.mu.Lock()
			delete(s.pending, key)
			s.mu.Unlock()
			s.events.touch(key)
			s.events.touchProjects()
		}
	}
	if len(temps) == 0 {
		return
	}
	// Раньше запущенный забирает раньше созданную сессию.
	slices.SortFunc(temps, func(a, b *pendingLaunch) int { return a.at.Compare(b.at) })
	claimed := map[string]bool{}
	for key := range s.store.Snapshot().Terminals {
		claimed[key] = true
	}
	list := s.catalog.Sessions()
	for _, p := range temps {
		var best *model.Session
		for i := range list {
			sess := &list[i]
			key := sess.Key()
			if sess.Agent != p.agent || sess.Auto || p.known[key] || claimed[key] {
				continue
			}
			if sess.Created.Before(p.at.Add(-launchSlack)) || !sameDir(sess.Cwd, p.cwd) {
				continue
			}
			if best == nil || sess.Created.Before(best.Created) {
				best = sess
			}
		}
		if best != nil && s.adopt(p, best.Key()) {
			claimed[best.Key()] = true
		}
	}
}

// adopt переводит запущенную сессию с временного ключа на настоящий: метка терминала,
// записи о терминале и копии, сведения о запуске. Слежение за терминалом переносит
// состояние и «новое» само, по метке.
func (s *Server) adopt(p *pendingLaunch, real string) bool {
	if err := tmux.SetKey(p.tmux, real); err != nil {
		return false
	}
	m := s.store.Marks(p.key)
	if m.Terminal != nil {
		_ = s.store.RecordTerminal(real, *m.Terminal)
		_ = s.store.ForgetTerminal(p.key)
	}
	if m.Worktree != nil {
		_ = s.store.SetWorktree(real, *m.Worktree)
		_ = s.store.ForgetWorktree(p.key)
	}
	// Пометки, которые человек успел поставить, пока ID не был известен.
	if m.Name != "" {
		_ = s.store.SetName(real, m.Name)
		_ = s.store.SetName(p.key, "")
	}
	flags := []struct {
		on  bool
		set func(string, bool) error
	}{{m.Pinned, s.store.SetPinned}, {m.Hidden, s.store.SetHidden}, {m.Done, s.store.SetDone}}
	for _, f := range flags {
		if f.on {
			_ = f.set(real, true)
			_ = f.set(p.key, false)
		}
	}
	if m.Seen > 0 {
		_ = s.store.MarkSeen(real, time.Unix(m.Seen, 0))
	}
	s.mu.Lock()
	delete(s.pending, p.key)
	if info, ok := s.launches[p.key]; ok {
		s.launches[real] = info
		delete(s.launches, p.key)
	}
	s.mu.Unlock()
	s.tracker.Poll()
	s.events.rename(p.key, real)
	return true
}

// terminalGone — терминал сессии закрыли. Сессия, которой нет в каталоге, исчезает: агент
// не успел записать журнал, продолжать нечего.
func (s *Server) terminalGone(key string) {
	if _, ok := s.catalog.Get(key); ok {
		return
	}
	s.mu.Lock()
	p := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if p == nil {
		return
	}
	_ = s.store.ForgetTerminal(key)
	_ = s.store.ForgetWorktree(key)
	s.events.touchProjects()
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b) || realPath(a) == realPath(b)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

// oneLine — название из запроса: одна строка до 150 символов.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > limit {
		text = string([]rune(text)[:limit])
	}
	return text
}

func randomID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// fallbackBranch — имя ветки, когда из запроса его не сложить.
func fallbackBranch() string { return "nit-" + time.Now().Format("0102-1504") }

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) error {
	var req LaunchRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	key, err := s.launch(req)
	if err != nil {
		s.noticeErr(r, "", err)
		return err
	}
	writeJSON(w, http.StatusOK, LaunchResponse{Key: key})
	return nil
}

// launch запускает новую сессию в папке проекта или в новой копии и возвращает её ключ.
func (s *Server) launch(req LaunchRequest) (string, error) {
	path := expandHome(strings.TrimSpace(req.Path))
	if !filepath.IsAbs(path) {
		return "", apiErr(http.StatusBadRequest, "bad_path")
	}
	path = filepath.Clean(path)
	if !isDir(path) {
		return "", apiErr(http.StatusBadRequest, "no_dir", "value", path)
	}
	a := s.agent(req.Agent)
	if err := s.checkAgent(a, req.Agent); err != nil {
		return "", err
	}
	name := a.Info().Name
	if !agents.ValidChoice(req.Model) || !agents.ValidChoice(req.Effort) {
		return "", apiErr(http.StatusBadRequest, "bad_choice")
	}
	if utf8.RuneCountInString(req.Prompt) > maxPrompt {
		return "", apiErr(http.StatusBadRequest, "bad_prompt")
	}
	mode := req.Mode
	if mode == "" {
		mode = "folder"
	}
	if mode != "folder" && mode != "copy" {
		return "", apiErr(http.StatusBadRequest, "bad_mode")
	}
	if !tmux.Available() {
		return "", apiErr(http.StatusConflict, "no_tmux")
	}
	cmd := a.Launch(agents.LaunchOptions{Model: req.Model, Effort: req.Effort, Prompt: req.Prompt})
	if len(cmd.Argv) == 0 {
		return "", apiErr(http.StatusBadRequest, "no_launch", "agent", name)
	}
	set := s.settings.Get()
	argv := slices.Concat(cmd.Argv[:1], set.Agent(a.ID()).Args(), cmd.Argv[1:])

	cwd, project, branch, repoMain := path, filepath.Base(path), "", ""
	var wt *model.Worktree
	repo, repoErr := launch.RepoOf(path)
	if repoErr == nil {
		branch, repoMain, project = repo.Branch, repo.Main, filepath.Base(repo.Main)
		if w, ok := repo.Worktree(); ok {
			// «Ещё агент здесь» в копии: новая сессия работает в той же копии.
			wt = &w
		}
	}
	created := false
	if mode == "copy" {
		if repoErr != nil {
			return "", apiErr(http.StatusBadRequest, "not_git")
		}
		b := strings.TrimSpace(req.Branch)
		if b == "" {
			b = launch.SuggestBranch(req.Prompt)
			if b == "" {
				b = fallbackBranch()
			}
			b = launch.FreeBranch(repo.Main, b)
		}
		dir, err := launch.WorktreeDir(project, b)
		if err != nil {
			return "", apiErr(http.StatusConflict, "copy_failed", "value", err.Error())
		}
		w, err := launch.CreateWorktree(repo.Root, dir, b, "")
		if err != nil {
			return "", apiErr(http.StatusConflict, "copy_failed", "value", err.Error())
		}
		wt, cwd, branch, created = &w, w.Path, b, true
	}

	id, temp := cmd.SessionID, cmd.SessionID == ""
	if temp {
		id = "new-" + randomID()
	}
	key := a.ID() + ":" + id
	p := &pendingLaunch{key: key, agent: a.ID(), temp: temp, cwd: cwd, repo: repoMain, branch: branch,
		title: oneLine(req.Prompt, 150), at: time.Now()}
	if temp {
		p.known = map[string]bool{}
		for _, sess := range s.catalog.Sessions() {
			if sess.Agent == p.agent && sameDir(sess.Cwd, cwd) {
				p.known[sess.Key()] = true
			}
		}
	}
	tmuxName := tmux.Name(set.Tmux.SessionName, tmux.NameVars{Project: project, Branch: branch, Agent: a.ID(), ID: id})
	pane, err := launch.Start(launch.Options{Name: tmuxName, Key: key, Cwd: cwd, Argv: argv})
	if err != nil {
		if created {
			dropWorktree(*wt)
		}
		return "", apiErr(http.StatusInternalServerError, "launch_failed", "agent", name, "value", err.Error())
	}
	p.tmux = pane.Name
	s.mu.Lock()
	s.pending[key] = p
	s.launches[key] = LaunchInfo{At: p.at.Unix(), Model: req.Model, Effort: req.Effort}
	s.mu.Unlock()
	if err := s.store.RecordTerminal(key, store.Terminal{Tmux: pane.Name, Cwd: cwd, Agent: a.ID()}); err != nil {
		return "", err
	}
	if wt != nil {
		if err := s.store.SetWorktree(key, *wt); err != nil {
			return "", err
		}
	}
	if req.Prompt != "" && !cmd.PromptInArgs {
		go s.typePrompt(pane.Name, req.Prompt)
	}
	s.tracker.Poll()
	s.events.touch(key)
	s.events.touchProjects()
	return key, nil
}

// dropWorktree убирает копию, в которой агент так и не запустился. Ветку — если git
// разрешит: свежая ветка обычно уже есть в основной.
func dropWorktree(wt model.Worktree) {
	if launch.Remove(wt, true, "") != nil {
		_ = launch.Remove(wt, false, "")
	}
}

// typePrompt набирает первый запрос в терминале агента, который не принимает его аргументом
// (Kimi): ждёт первый экран агента, потом — пока экран не замрёт, и вставляет текст с Enter.
func (s *Server) typePrompt(name, prompt string) {
	deadline := time.Now().Add(time.Minute)
	sleep := func(d time.Duration) bool {
		select {
		case <-s.done:
			return false
		case <-time.After(d):
			return time.Now().Before(deadline)
		}
	}
	for {
		if !sleep(200 * time.Millisecond) {
			return
		}
		l, ok := s.liveByTerminal(name)
		if !ok {
			if !tmux.Exists(name) {
				return
			}
			continue
		}
		if l.State == model.StateExited || l.State == model.StateError {
			return
		}
		if l.State != "" && l.State != model.StateStarting {
			break
		}
	}
	// Агент дорисовывает поле ввода: текст, вставленный раньше, пропал бы.
	last, stable := "", time.Now()
	for {
		screen, err := tmux.Capture(name, 0)
		if err != nil {
			return
		}
		if screen != last {
			last, stable = screen, time.Now()
		} else if time.Since(stable) >= 700*time.Millisecond {
			break
		}
		if !sleep(100 * time.Millisecond) {
			return
		}
	}
	if err := tmux.SendText(name, prompt); err != nil {
		return
	}
	// Пауза, чтобы агент принял вставку целиком, а не только первый кусок.
	time.Sleep(150 * time.Millisecond)
	_ = tmux.SendKeys(name, "Enter")
}

// liveByTerminal — состояние терминала по имени сессии tmux: ключ мог смениться.
func (s *Server) liveByTerminal(name string) (model.Live, bool) {
	for _, l := range s.tracker.All() {
		if l.Terminal == name {
			return l, true
		}
	}
	return model.Live{}, false
}

// handleLaunchCheck — что известно о папке до запуска: git ли это, кто в ней уже работает,
// где работать, какую ветку и какого агента предложить.
func (s *Server) handleLaunchCheck(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	path := expandHome(strings.TrimSpace(q.Get("path")))
	if !filepath.IsAbs(path) {
		return apiErr(http.StatusBadRequest, "bad_path")
	}
	path = filepath.Clean(path)
	out := LaunchCheck{Path: path, Project: filepath.Base(path), Busy: []BusyView{}, Mode: "folder", Exists: isDir(path)}
	c := s.viewCtx()
	list := s.sessions()
	projectDir, root := path, path
	var repo launch.Repo
	if out.Exists {
		var err error
		if repo, err = launch.RepoOf(path); err == nil {
			out.Git, projectDir, root, out.Project = true, repo.Main, repo.Root, filepath.Base(repo.Main)
		}
	}
	if out.Exists {
		// Кто работает в этой рабочей папке: терминалы «Нити» и сессии вне неё по реестрам.
		keys := map[string]bool{}
		for key, l := range c.live {
			if l.State != model.StateExited && l.State != model.StateError {
				keys[key] = true
			}
		}
		for key := range c.registry {
			keys[key] = true
		}
		for key := range keys {
			sess, ok := s.session(key)
			if !ok || !inside(sess.Cwd, root) {
				continue
			}
			v := c.view(sess)
			out.Busy = append(out.Busy, BusyView{Key: key, Agent: v.Agent, Title: v.Title, State: v.State})
		}
		slices.SortFunc(out.Busy, func(a, b BusyView) int { return strings.Compare(a.Key, b.Key) })
	}
	if out.Git {
		if len(out.Busy) > 0 {
			out.Mode = "copy"
		}
		b := launch.SuggestBranch(q.Get("prompt"))
		if b == "" {
			b = fallbackBranch()
		}
		out.Branch = launch.FreeBranch(repo.Main, b)
		if list, err := launch.Worktrees(repo.Main); err == nil {
			for _, wt := range list {
				if filepath.Clean(wt.Path) != filepath.Clean(repo.Main) {
					out.Worktrees = append(out.Worktrees, WorktreeView{Path: wt.Path, Branch: wt.Branch, Repo: wt.Repo})
				}
			}
		}
	}
	out.DefaultAgent = s.defaultAgent(c, projectDir, list)
	for _, sess := range list {
		if len(out.Recent) >= maxRecent {
			break
		}
		if projectPath(sess) != projectDir || c.service(sess) || c.marks.Marks(sess.Key()).Hidden {
			continue
		}
		v := c.view(sess)
		out.Recent = append(out.Recent, BusyView{Key: v.Key, Agent: v.Agent, Title: v.Title, State: v.State})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// defaultAgent — агент по умолчанию для проекта: из пометок проекта, иначе тот, что запускали
// в проекте последним, иначе последний вообще. Только установленные и запускаемые.
func (s *Server) defaultAgent(c *viewCtx, projectDir string, list []model.Session) string {
	usable := map[string]bool{}
	for id, a := range c.agents {
		usable[id] = c.installed[id] && len(a.Launch(agents.LaunchOptions{}).Argv) > 0
	}
	if pref := c.marks.Projects[projectDir].DefaultAgent; usable[pref] {
		return pref
	}
	for _, sess := range list {
		if projectPath(sess) == projectDir && usable[sess.Agent] && !c.service(sess) {
			return sess.Agent
		}
	}
	for _, sess := range list {
		if usable[sess.Agent] && !c.service(sess) {
			return sess.Agent
		}
	}
	for _, id := range agents.IDs {
		if usable[id] {
			return id
		}
	}
	return ""
}
