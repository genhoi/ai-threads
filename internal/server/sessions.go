package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/launch"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/tmux"
)

const (
	// detailsBytes — столько байт с конца журнала читают сведения о сессии сначала.
	detailsBytes = 2 << 20
	maxMessages  = 60
)

func (s *Server) handleDetails(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	sess, ok := s.session(key)
	if !ok {
		return notFound()
	}
	c := s.viewCtx()
	d := Details{Session: c.view(sess), Messages: []MessageView{}}
	a := s.agent(sess.Agent)
	if a != nil && sess.Journal != "" {
		messages, truncated := a.Transcript(sess, detailsBytes)
		if truncated && len(messages) < maxMessages {
			// В журнале Claude Code мегабайты вывода команд: двух мегабайт хвоста бывает мало
			// даже на несколько сообщений.
			messages, truncated = a.Transcript(sess, agents.MessageBytes)
		}
		if len(messages) > maxMessages {
			messages, truncated = messages[len(messages)-maxMessages:], true
		}
		for _, m := range messages {
			d.Messages = append(d.Messages, MessageView{Role: m.Role, Text: m.Text, At: unix(m.At)})
		}
		d.Truncated = truncated
	}
	d.Summary = c.marks.Marks(key).Summary
	if a != nil && !d.Session.Missing && hasJournal(sess) {
		if argv := s.resumeArgv(a, sess); argv != nil {
			d.Resume = tmux.ShellJoin(argv)
			if sess.Cwd != "" {
				d.Resume = "cd -- " + tmux.ShellJoin([]string{sess.Cwd}) + " && " + d.Resume
			}
		}
	}
	if d.Session.Terminal {
		if name, ok := s.terminalName(key); ok {
			d.Attach = tmux.AttachCommand(name)
		}
	}
	s.mu.Lock()
	if info, ok := s.launches[key]; ok {
		d.Launch = &info
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, d)
	return nil
}

// resumeArgv — команда продолжения с флагами запуска из настроек. Автоматическую сессию
// человек продолжает разговором, поэтому берётся обычная команда, а не codex exec resume.
func (s *Server) resumeArgv(a agents.Agent, sess model.Session) []string {
	sess.Auto = false
	argv := a.Resume(sess)
	if len(argv) == 0 {
		return nil
	}
	flags := s.settings.Get().Agent(a.ID()).Args()
	return slices.Concat(argv[:1], flags, argv[1:])
}

func (s *Server) handleSessionAction(w http.ResponseWriter, r *http.Request) error {
	key, action := r.PathValue("key"), r.PathValue("action")
	switch action {
	case "rename", "pin", "hide", "done", "seen":
	case "resume":
		view, err := s.resume(key)
		if err != nil {
			s.noticeErr(r, key, err)
			return err
		}
		writeJSON(w, http.StatusOK, view)
		return nil
	case "finish":
		return s.handleFinish(w, r)
	case "summary":
		return s.handleSummary(w, r)
	default:
		return apiErr(http.StatusNotFound, "no_route")
	}
	if _, ok := s.session(key); !ok {
		return notFound()
	}
	var err error
	switch action {
	case "rename":
		var req RenameRequest
		if err := decode(r, &req, bodyLimit, false); err != nil {
			return err
		}
		err = s.store.SetName(key, req.Name)
	case "pin", "hide", "done":
		var req struct {
			On *bool `json:"on"`
		}
		if err := decode(r, &req, bodyLimit, false); err != nil {
			return err
		}
		if req.On == nil {
			return apiErr(http.StatusBadRequest, "need_field", "field", "on")
		}
		set := map[string]func(string, bool) error{"pin": s.store.SetPinned, "hide": s.store.SetHidden, "done": s.store.SetDone}
		err = set[action](key, *req.On)
	case "seen":
		err = s.markSeen(key)
	}
	if err != nil {
		return err
	}
	return s.writeView(w, key)
}

func (s *Server) writeView(w http.ResponseWriter, key string) error {
	sess, ok := s.session(key)
	if !ok {
		return notFound()
	}
	writeJSON(w, http.StatusOK, s.viewCtx().view(sess))
	return nil
}

// markSeen — человек посмотрел сессию: «новое» снимается сейчас и не вернётся после перезапуска.
func (s *Server) markSeen(key string) error {
	s.tracker.MarkSeen(key)
	return s.store.MarkSeen(key, time.Now())
}

// noticeErr показывает ошибку запуска всплывающим сообщением на всех открытых страницах.
func (s *Server) noticeErr(r *http.Request, key string, err error) {
	var ae *apiError
	text := err.Error()
	if errors.As(err, &ae) {
		text = ae.text(s.lang(r))
	}
	s.notice("error", key, text)
}

// resume открывает сессию в терминале. Если агент ещё идёт с ней вне «Нити», второй не
// запускается: два агента в одной сессии испортили бы журнал.
func (s *Server) resume(key string) (SessionView, error) {
	sess, ok := s.catalog.Get(key)
	if !ok {
		return SessionView{}, notFound()
	}
	a := s.agent(sess.Agent)
	if err := s.checkAgent(a, sess.Agent); err != nil {
		return SessionView{}, err
	}
	argv := s.resumeArgv(a, sess)
	if argv == nil {
		return SessionView{}, apiErr(http.StatusBadRequest, "no_resume")
	}
	s.forgetDir(sess.Cwd)
	if sess.Cwd == "" || !s.dirExists(sess.Cwd) {
		return SessionView{}, apiErr(http.StatusConflict, "no_folder")
	}
	if st, ok := s.elsewhere(key); ok {
		code := "elsewhere_terminal"
		if st.Where == "background" {
			code = "elsewhere_background"
		}
		return SessionView{}, apiErr(http.StatusConflict, code, "agent", a.Info().Name)
	}
	if !tmux.Available() {
		return SessionView{}, apiErr(http.StatusConflict, "no_tmux")
	}
	name := tmux.Name(s.settings.Get().Tmux.SessionName,
		tmux.NameVars{Project: sess.Project(), Branch: sess.Branch, Agent: sess.Agent, ID: sess.ID})
	pane, err := launch.Start(launch.Options{Name: name, Key: key, Cwd: sess.Cwd, Argv: argv})
	if err != nil {
		return SessionView{}, apiErr(http.StatusInternalServerError, "launch_failed", "agent", a.Info().Name, "value", err.Error())
	}
	if err := s.store.RecordTerminal(key, store.Terminal{Tmux: pane.Name, Cwd: sess.Cwd, Agent: sess.Agent}); err != nil {
		return SessionView{}, err
	}
	s.tracker.Poll()
	s.events.touch(key)
	return s.viewCtx().view(sess), nil
}

// checkAgent — агент включён и установлен.
func (s *Server) checkAgent(a agents.Agent, id string) error {
	switch {
	case a == nil:
		return apiErr(http.StatusBadRequest, "unknown_agent", "value", id)
	case !a.Enabled():
		return apiErr(http.StatusConflict, "agent_off", "agent", a.Info().Name)
	case a.Program() == "":
		return apiErr(http.StatusConflict, "not_installed", "agent", a.Info().Name)
	}
	return nil
}

// finishCheck — что помешает удалить копию сессии.
func (s *Server) finishCheck(key string) (FinishCheck, *launch.Finish, error) {
	out := FinishCheck{Dirty: []string{}, Others: []string{}}
	wt := s.store.Marks(key).Worktree
	if wt == nil {
		return out, nil, nil
	}
	out.Worktree = &WorktreeView{Path: wt.Path, Branch: wt.Branch, Repo: wt.Repo}
	if !isDir(wt.Path) {
		// Копию уже удалили: удалять нечего.
		return out, nil, nil
	}
	self, _ := s.terminalName(key)
	f, err := launch.Check(*wt, self)
	if err != nil {
		return out, nil, err
	}
	keys := map[string]string{}
	if panes, err := tmux.List(); err == nil {
		for _, p := range panes {
			keys[p.Name] = p.Key
		}
	}
	for _, name := range f.Others {
		if k := keys[name]; k != "" {
			name = k
		}
		out.Others = append(out.Others, name)
	}
	if f.Dirty != nil {
		out.Dirty = f.Dirty
	}
	out.Pushed, out.Merged = f.Pushed, f.Merged
	out.CanRemove, out.CanDeleteBranch = f.CanRemove(), f.CanDeleteBranch()
	return out, &f, nil
}

func (s *Server) handleFinishCheck(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	if _, ok := s.session(key); !ok {
		return notFound()
	}
	out, _, err := s.finishCheck(key)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// handleFinish завершает сессию: закрывает терминал (агент останавливается вместе с ним),
// отмечает сессию и, если просили и можно, удаляет копию. Проверки копии — до всего
// остального: если удалить её нельзя, не меняется ничего.
func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	sess, ok := s.session(key)
	if !ok {
		return notFound()
	}
	var req FinishRequest
	if err := decode(r, &req, bodyLimit, true); err != nil {
		return err
	}
	wt := s.store.Marks(key).Worktree
	if req.RemoveWorktree {
		check, f, err := s.finishCheck(key)
		switch {
		case err != nil:
			return err
		case wt == nil || f == nil:
			return apiErr(http.StatusConflict, "no_copy")
		case len(check.Dirty) > 0:
			return apiErr(http.StatusConflict, "dirty", "value", strings.Join(check.Dirty, ", "))
		case len(check.Others) > 0:
			return apiErr(http.StatusConflict, "busy", "value", strings.Join(check.Others, ", "))
		case req.DeleteBranch && !check.CanDeleteBranch:
			return apiErr(http.StatusConflict, "not_pushed", "value", wt.Branch)
		}
	}
	name, hasTerminal := s.terminalName(key)
	if hasTerminal {
		if err := tmux.Close(name); err != nil {
			return err
		}
	}
	if err := s.store.SetDone(key, true); err != nil {
		return err
	}
	if err := s.store.ForgetTerminal(key); err != nil {
		return err
	}
	view := s.viewCtx().view(sess)
	if req.RemoveWorktree && wt != nil {
		if err := launch.Remove(*wt, req.DeleteBranch, name); err != nil {
			return apiErr(http.StatusConflict, "remove_failed", "value", err.Error())
		}
		// Копии больше нет: её забывают все сессии, которые в ней работали.
		for k, other := range s.store.Snapshot().Worktrees {
			if filepath.Clean(other.Path) == filepath.Clean(wt.Path) {
				_ = s.store.ForgetWorktree(k)
			}
		}
		s.forgetDir(wt.Path)
	}
	s.tracker.Poll()
	if sess, ok := s.session(key); ok {
		view = s.viewCtx().view(sess)
	} else {
		// Сессия без журнала (агент ничего не успел) исчезает вместе с терминалом.
		view.State, view.Terminal, view.Done, view.Worktree = model.StateIdle, false, true, nil
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

// handleProject — пометки проекта: закрепить, скрыть, агент по умолчанию.
func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) error {
	var req ProjectRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	path := expandHome(strings.TrimSpace(req.Path))
	if !filepath.IsAbs(path) {
		return apiErr(http.StatusBadRequest, "bad_path")
	}
	path = filepath.Clean(path)
	if req.Pinned != nil {
		if err := s.store.SetProjectPinned(path, *req.Pinned); err != nil {
			return err
		}
	}
	if req.Hidden != nil {
		if err := s.store.SetProjectHidden(path, *req.Hidden); err != nil {
			return err
		}
	}
	if req.DefaultAgent != nil {
		if err := s.store.SetProjectAgent(path, *req.DefaultAgent); err != nil {
			return err
		}
	}
	c := s.viewCtx()
	for _, p := range c.projects(s.sessions()) {
		if p.Path == path {
			writeJSON(w, http.StatusOK, p)
			return nil
		}
	}
	pref := c.marks.Projects[path]
	writeJSON(w, http.StatusOK, ProjectView{Name: filepath.Base(path), Path: path, Git: launchGit(path),
		Pinned: pref.Pinned, Hidden: pref.Hidden, DefaultAgent: pref.DefaultAgent})
	return nil
}

func launchGit(path string) bool {
	_, err := launch.RepoOf(path)
	return err == nil
}
