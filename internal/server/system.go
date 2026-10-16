package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/remote"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/tmux"
)

// --- Настройки ---

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, s.settings.Get())
	return nil
}

// handleSaveSettings — тело — настройки целиком. Неверные поля — 400 с ошибкой у каждого поля;
// файл тогда не меняется. Без поля remote, как и с телефона, доступ с телефона не меняется:
// его включают на компьютере.
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) error {
	raw, err := readBody(r, bodyLimit)
	if err != nil {
		return err
	}
	parsed, err := settings.Parse(raw)
	if err != nil {
		return err
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if rem, ok := top["remote"]; !ok || strings.TrimSpace(string(rem)) == "null" || remote.FromRemote(r) {
		parsed.Remote = s.settings.Get().Remote
	}
	if err := s.applySettings(parsed); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.settings.Get())
	return nil
}

// applySettings сохраняет настройки и применяет их: агенты пересобираются, узел для телефона
// поднимается или останавливается, а если поменялись признаки служебных сессий — страницы
// получают все сессии заново.
func (s *Server) applySettings(next settings.Settings) error {
	old := s.settings.Get()
	if err := s.settings.Save(next); err != nil {
		return err
	}
	s.syncAgents()
	if old.Remote != next.Remote {
		s.syncRemote()
		s.events.touchRemote()
	}
	cur := s.settings.Get()
	if !slices.Equal(old.Service.AutoCommands, cur.Service.AutoCommands) ||
		!slices.Equal(old.Service.TempDirs, cur.Service.TempDirs) {
		s.events.touchAll()
	}
	return nil
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, apiErr(http.StatusBadRequest, "bad_json")
	}
	if int64(len(raw)) > limit {
		return nil, apiErr(http.StatusRequestEntityTooLarge, "too_large")
	}
	return raw, nil
}

// --- Выгрузка и загрузка ---

// handleExport — пометки одним файлом, как у первой версии, и настройки в поле settings.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) error {
	raw, err := s.store.Export()
	if err != nil {
		return err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	set, err := json.Marshal(s.settings.Get())
	if err != nil {
		return err
	}
	data["settings"] = set
	w.Header().Set("Content-Disposition", `attachment; filename="ai-threads-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSON(w, http.StatusOK, data)
	return nil
}

// handleImport добавляет пометки из выгрузки и, если в ней есть settings, заменяет настройки.
// Неверные настройки отклоняют весь файл до того, как что-то поменялось. Доступ с телефона
// выгрузка не включает и не выключает: файл мог прийти с другого компьютера.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) error {
	raw, err := readBody(r, importLimit)
	if err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || top == nil {
		return apiErr(http.StatusBadRequest, "bad_json")
	}
	var next *settings.Settings
	if rawSet, ok := top["settings"]; ok && strings.TrimSpace(string(rawSet)) != "null" {
		parsed, err := settings.Parse(rawSet)
		if err != nil {
			return err
		}
		parsed.Remote = s.settings.Get().Remote
		next = &parsed
	}
	if err := s.store.Import(raw); err != nil {
		return err
	}
	if next != nil {
		if err := s.applySettings(*next); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// --- Открыть папку ---

// startDetached запускает программу и не ждёт её: проводник и VS Code живут сами по себе.
// Тесты подменяют её, чтобы ничего не открывалось.
var startDetached = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// windowsPath — путь в WSL в виде, понятном Windows.
var windowsPath = func(path string) (string, error) {
	out, err := exec.Command("wslpath", "-w", path).Output()
	return strings.TrimSpace(string(out)), err
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) error {
	var req RevealRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	dir := ""
	if req.Key != "" {
		sess, ok := s.session(req.Key)
		if !ok {
			return notFound()
		}
		dir = sess.Cwd
	} else {
		dir = expandHome(strings.TrimSpace(req.Path))
		if !filepath.IsAbs(dir) {
			return apiErr(http.StatusBadRequest, "bad_path")
		}
	}
	if !isDir(dir) {
		return apiErr(http.StatusConflict, "no_dir", "value", dir)
	}
	switch req.App {
	case "folder":
		if inWSL() {
			win, err := windowsPath(dir)
			if err != nil || win == "" {
				return apiErr(http.StatusConflict, "explorer_wsl", "value", "wslpath")
			}
			if err := startDetached("explorer.exe", win); err != nil {
				return apiErr(http.StatusConflict, "explorer_wsl", "value", err.Error())
			}
			break
		}
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		if err := startDetached(opener, dir); err != nil {
			return apiErr(http.StatusConflict, "explorer_wsl", "value", err.Error())
		}
	case "vscode":
		if _, err := exec.LookPath("code"); err != nil {
			return apiErr(http.StatusConflict, "no_vscode")
		}
		args := []string{dir}
		if distro := wslDistro(); distro != "" {
			// Из WSL VS Code открывает папку в удалённом окне этого дистрибутива.
			args = []string{"--remote", "wsl+" + distro, dir}
		}
		if err := startDetached("code", args...); err != nil {
			return apiErr(http.StatusConflict, "no_vscode")
		}
	default:
		return apiErr(http.StatusBadRequest, "bad_app")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// --- После перезагрузки ---

// lastState — последнее известное состояние терминала. Хранится в своём файле: после
// перезагрузки tmux его уже не помнит, а экран продолжения показывает «ждал вас»,
// «работал 7 мин», «вышел с кодом 1».
type lastState struct {
	State model.State `json:"state"`
	Code  int         `json:"code,omitempty"`
	Since int64       `json:"since,omitempty"`
	At    int64       `json:"at"` // когда терминал последний раз видели открытым
}

type lastStates struct {
	items   map[string]lastState
	savedAt time.Time
	changed bool
}

func loadLastStates(path string) *lastStates {
	ls := &lastStates{items: map[string]lastState{}}
	if path == "" {
		return ls
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &ls.items)
		if ls.items == nil {
			ls.items = map[string]lastState{}
		}
	}
	return ls
}

// saveLastStates обновляет состояния открытых терминалов и пишет файл: сразу, если состояние
// сменилось, иначе не чаще раза в полминуты.
func (s *Server) saveLastStates() {
	if s.opts.StateFile == "" {
		return
	}
	now := time.Now()
	terminals := s.store.Snapshot().Terminals
	s.mu.Lock()
	ls := s.lastSeen
	for key, l := range s.tracker.All() {
		if l.State == "" || l.State == model.StateIdle {
			continue
		}
		old := ls.items[key]
		next := lastState{State: l.State, Code: l.ExitCode, Since: unix(l.Since), At: now.Unix()}
		if old.State != next.State || old.Code != next.Code || old.Since != next.Since {
			ls.changed = true
		}
		ls.items[key] = next
	}
	for key := range ls.items {
		if _, ok := terminals[key]; !ok {
			delete(ls.items, key)
			ls.changed = true
		}
	}
	if !ls.changed && now.Sub(ls.savedAt) < 30*time.Second {
		s.mu.Unlock()
		return
	}
	raw, err := json.Marshal(ls.items)
	ls.changed, ls.savedAt = false, now
	s.mu.Unlock()
	if err != nil {
		return
	}
	dir := filepath.Dir(s.opts.StateFile)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".terminals-*.tmp")
	if err != nil {
		return
	}
	_, err = f.Write(raw)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.opts.StateFile)
	}
	if err != nil {
		os.Remove(f.Name())
	}
}

// rebootList — терминалы, которые «Нить» открывала, а tmux их больше не знает: компьютер
// перезагрузился или сервер tmux остановили. Сессии без журнала продолжить нельзя, их нет.
func (s *Server) rebootList() []RebootSession {
	panes, err := tmux.List()
	if err != nil {
		return []RebootSession{}
	}
	alive := map[string]bool{}
	for _, p := range panes {
		alive[p.Name] = true
	}
	c := s.viewCtx()
	type item struct {
		rs      RebootSession
		started int64
	}
	var items []item
	s.mu.Lock()
	states := s.lastSeen.items
	s.mu.Unlock()
	for key, t := range c.marks.Terminals {
		if alive[t.Tmux] {
			continue
		}
		sess, ok := s.catalog.Get(key)
		if !ok {
			continue
		}
		s.mu.Lock()
		was := states[key]
		s.mu.Unlock()
		rs := RebootSession{Session: c.view(sess), Was: was.State}
		switch was.State {
		case model.StateError:
			rs.Code = was.Code
		case model.StateWorking:
			if was.Since > 0 && was.At > was.Since {
				rs.Minutes = int((was.At - was.Since) / 60)
			}
		}
		items = append(items, item{rs, t.Started})
	}
	slices.SortFunc(items, func(a, b item) int { return int(sign(b.started - a.started)) })
	out := make([]RebootSession, 0, len(items))
	for _, it := range items {
		out = append(out, it.rs)
	}
	return out
}

func (s *Server) handleReboot(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, RebootView{Sessions: s.rebootList()})
	return nil
}

// handleRebootAction продолжает выбранные сессии, а с dismiss забывает остальные терминалы,
// которых больше нет.
func (s *Server) handleRebootAction(w http.ResponseWriter, r *http.Request) error {
	var req RebootRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	out := RebootResult{Resumed: []string{}}
	for _, key := range req.Keys {
		if _, err := s.resume(key); err != nil {
			if out.Failed == nil {
				out.Failed = map[string]string{}
			}
			text := err.Error()
			if ae, ok := err.(*apiError); ok {
				text = ae.text(s.lang(r))
			}
			out.Failed[key] = text
			continue
		}
		out.Resumed = append(out.Resumed, key)
	}
	if req.Dismiss {
		panes, err := tmux.List()
		if err == nil {
			alive := map[string]bool{}
			for _, p := range panes {
				alive[p.Name] = true
			}
			for key, t := range s.store.Snapshot().Terminals {
				if !alive[t.Tmux] && !slices.Contains(out.Resumed, key) {
					_ = s.store.ForgetTerminal(key)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// wslMarker — признак WSL на случай, когда переменной WSL_DISTRO_NAME нет: её не получают службы
// systemd. Файл регистрирует сам WSL, чтобы запускать программы Windows. Тесты подменяют путь.
var wslMarker = "/proc/sys/fs/binfmt_misc/WSLInterop"

func inWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	_, err := os.Stat(wslMarker)
	return err == nil
}

// wslDistro — имя дистрибутива WSL. Без переменной окружения берётся из пути корня для Windows:
// wslpath -w / отвечает \\wsl.localhost\Ubuntu\ или \\wsl$\Ubuntu\.
func wslDistro() string {
	if name := os.Getenv("WSL_DISTRO_NAME"); name != "" {
		return name
	}
	if !inWSL() {
		return ""
	}
	out, err := exec.Command("wslpath", "-w", "/").Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(string(out)), `\`), `\`)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
