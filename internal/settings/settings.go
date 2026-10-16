// Package settings — настройки «Нити» в settings.json.
//
// В файле лежат только значения, которые отличаются от умолчаний. Файл первой версии читается
// как есть: секунды ожидания сводки переводятся в минуты, temp_dirs переезжает в service,
// а поля, которых больше нет (open_with, tmux_session, search), молча отбрасываются.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// Agents — агенты, у которых есть настройки, в порядке экрана настроек.
	Agents = []string{"codex", "claude", "grok", "kimi", "zcode", "cursor"}
	// Runners — агенты, которые умеют составлять сводки без интерфейса.
	Runners = []string{"kimi", "claude", "codex", "grok"}
	// Languages — язык интерфейса и сводок; auto — по браузеру.
	Languages = []string{"auto", "ru", "en"}
	// Placeholders — подстановки в имени сессии tmux.
	Placeholders = []string{"project", "branch", "agent", "id"}
)

// Settings — настройки целиком: умолчания, поверх которых наложен файл.
type Settings struct {
	Agents   map[string]Agent `json:"agents"`
	Summary  Runner           `json:"summary"` // сводка по сессии
	Digest   Runner           `json:"digest"`  // сводка за несколько дней
	Language string           `json:"language"`
	Service  Service          `json:"service"`
	Tmux     Tmux             `json:"tmux"`
	Remote   Remote           `json:"remote"`
}

// Agent — настройки агента. Пустые Home и Program — найти самим.
type Agent struct {
	Enabled       bool   `json:"enabled"`        // «Показывать в списке»
	Home          string `json:"home"`           // папка с сессиями
	Program       string `json:"program"`        // путь к программе
	Flags         string `json:"flags"`          // «Флаги запуска», как в командной строке
	SkipApprovals bool   `json:"skip_approvals"` // «Запускать без подтверждений»
}

// Runner — кто составляет сводку и сколько ждать ответа.
type Runner struct {
	Agent   string `json:"agent"`   // auto или агент из Runners
	Minutes int    `json:"minutes"` // от 1 до 120
}

// Service — по каким признакам сессия считается служебной и прячется из списков.
type Service struct {
	AutoCommands []string `json:"auto_commands"` // запуски без человека: codex exec, claude -p
	TempDirs     []string `json:"temp_dirs"`     // временные папки; путь начинается с / или ~/
}

// Tmux — настройки сессий tmux.
type Tmux struct {
	SessionName string `json:"session_name"` // шаблон имени, например nit-{project}-{branch}
}

// Remote — доступ с телефона через узел Tailscale в самой «Нити». Ключ входа и устройства
// здесь не лежат: они в remote.json в папке данных.
type Remote struct {
	Enabled  bool   `json:"enabled"`
	Funnel   bool   `json:"funnel"`    // адрес открыт и из интернета, без приложения Tailscale
	Hostname string `json:"hostname"`  // имя узла в tailnet: <hostname>.<сеть>.ts.net
	StateDir string `json:"state_dir"` // каталог состояния узла; начинается с / или ~/
}

// RemoteDir — каталог состояния узла с раскрытым ~.
func (r Remote) RemoteDir() string {
	dir := r.StateDir
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = home + dir[1:]
	}
	return dir
}

// Defaults — умолчания. Каждый вызов возвращает новую копию.
func Defaults() Settings {
	agents := make(map[string]Agent, len(Agents))
	for _, id := range Agents {
		agents[id] = Agent{Enabled: true}
	}
	return Settings{
		Agents:   agents,
		Summary:  Runner{Agent: "auto", Minutes: 5},
		Digest:   Runner{Agent: "auto", Minutes: 15},
		Language: "auto",
		Service: Service{
			AutoCommands: []string{"codex exec", "claude -p", "grok -p", "kimi -p"},
			TempDirs:     []string{"/tmp", "/var/tmp", "~/.local"},
		},
		Tmux: Tmux{SessionName: "nit-{project}-{branch}"},
		// Каталог узла gate: после перехода с gate телефон открывает тот же адрес.
		Remote: Remote{Hostname: "nit", StateDir: "~/.threadgate"},
	}
}

func (s Settings) clone() Settings {
	c := s
	c.Agents = maps.Clone(s.Agents)
	c.Service.AutoCommands = slices.Clone(s.Service.AutoCommands)
	c.Service.TempDirs = slices.Clone(s.Service.TempDirs)
	return c
}

// Agent — настройки агента; у неизвестного — умолчания.
func (s Settings) Agent(id string) Agent {
	if a, ok := s.Agents[id]; ok {
		return a
	}
	return Agent{Enabled: true}
}

// Args — флаги запуска, разобранные как в командной строке. Неверные флаги до сюда не доходят:
// их отсекает проверка.
func (a Agent) Args() []string {
	args, _ := SplitFlags(a.Flags)
	return args
}

// Timeout — сколько ждать сводку: kind — summary или digest. Переменная окружения
// AI_THREADS_SUMMARY_TIMEOUT или AI_THREADS_DIGEST_TIMEOUT (секунды) важнее файла, как в первой версии.
func (s Settings) Timeout(kind string) time.Duration {
	if raw := os.Getenv("AI_THREADS_" + strings.ToUpper(kind) + "_TIMEOUT"); raw != "" {
		if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
			return time.Duration(sec * float64(time.Second))
		}
	}
	r := s.Summary
	if kind == "digest" {
		r = s.Digest
	}
	return time.Duration(r.Minutes) * time.Minute
}

// TempDirPaths — временные папки с раскрытым ~ и без «/» в конце.
func (s Settings) TempDirPaths() []string {
	home, _ := os.UserHomeDir()
	dirs := make([]string, 0, len(s.Service.TempDirs))
	for _, dir := range s.Service.TempDirs {
		if dir == "~" || strings.HasPrefix(dir, "~/") {
			dir = home + dir[1:]
		}
		if dir = strings.TrimRight(dir, "/"); dir == "" {
			dir = "/"
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// Path — файл настроек: $AI_THREADS_CONFIG, иначе $XDG_CONFIG_HOME/ai-threads/settings.json,
// иначе ~/.config/ai-threads/settings.json.
func Path() string {
	if path := os.Getenv("AI_THREADS_CONFIG"); path != "" {
		return path
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ai-threads", "settings.json")
}

// File — файл настроек. Файл перечитывается, когда меняются его размер или время изменения,
// так что правка руками подхватывается без перезапуска. Методы можно звать из нескольких горутин.
type File struct {
	path string

	mu     sync.Mutex
	loaded bool
	stamp  stamp
	cur    Settings
	err    error
}

type stamp struct {
	size  int64
	mtime int64
}

// New — файл настроек по пути; читается при первом обращении.
func New(path string) *File { return &File{path: path} }

// Path — путь к файлу.
func (f *File) Path() string { return f.path }

// load перечитывает файл, если он поменялся. Повреждённый файл не роняет «Нить»: берутся
// умолчания и верные поля, а причина видна в Err.
func (f *File) load() {
	st := stamp{-1, -1}
	if info, err := os.Stat(f.path); err == nil {
		st = stamp{info.Size(), info.ModTime().UnixNano()}
	}
	if f.loaded && st == f.stamp {
		return
	}
	f.loaded, f.stamp, f.cur, f.err = true, st, Defaults(), nil
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		f.err = err
		return
	}
	f.cur, err = Parse(raw)
	if err != nil {
		f.err = fmt.Errorf("%s: %w", f.path, err)
	}
}

// Get — действующие настройки. Это копия, её можно менять.
func (f *File) Get() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	return f.cur.clone()
}

// Err — почему файл прочитан не целиком; nil, если всё в порядке или файла нет. Ошибки полей
// достаются через errors.As в переменную типа Errors.
func (f *File) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	return f.err
}

// Save проверяет настройки и записывает в файл только отличия от умолчаний. При ошибке
// проверки (Errors) файл не меняется.
func (f *File) Save(s Settings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	clean, err := Parse(raw)
	if err != nil {
		return err
	}
	out, err := encodeFile(clean)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := writeFile(f.path, out); err != nil {
		return err
	}
	f.loaded = false
	return nil
}

// Вид файла: только отличия от умолчаний.
type fileAgent struct {
	Enabled       *bool  `json:"enabled,omitempty"`
	Home          string `json:"home,omitempty"`
	Program       string `json:"program,omitempty"`
	Flags         string `json:"flags,omitempty"`
	SkipApprovals bool   `json:"skip_approvals,omitempty"`
}

type fileRunner struct {
	Agent   string `json:"agent,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
}

type fileService struct {
	// Указатели: пустой список, который человек оставил сам, тоже записывается.
	AutoCommands *[]string `json:"auto_commands,omitempty"`
	TempDirs     *[]string `json:"temp_dirs,omitempty"`
}

type fileTmux struct {
	SessionName string `json:"session_name,omitempty"`
}

type fileRemote struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Funnel   bool   `json:"funnel,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	StateDir string `json:"state_dir,omitempty"`
}

type fileSettings struct {
	Agents   map[string]fileAgent `json:"agents,omitempty"`
	Summary  *fileRunner          `json:"summary,omitempty"`
	Digest   *fileRunner          `json:"digest,omitempty"`
	Language string               `json:"language,omitempty"`
	Service  *fileService         `json:"service,omitempty"`
	Tmux     *fileTmux            `json:"tmux,omitempty"`
	Remote   *fileRemote          `json:"remote,omitempty"`
}

func encodeFile(s Settings) ([]byte, error) {
	def := Defaults()
	var out fileSettings
	for id, a := range s.Agents {
		d := def.Agent(id)
		fa := fileAgent{SkipApprovals: a.SkipApprovals && !d.SkipApprovals}
		if a.Enabled != d.Enabled {
			fa.Enabled = &a.Enabled
		}
		if a.Home != d.Home {
			fa.Home = a.Home
		}
		if a.Program != d.Program {
			fa.Program = a.Program
		}
		if a.Flags != d.Flags {
			fa.Flags = a.Flags
		}
		if fa != (fileAgent{}) {
			if out.Agents == nil {
				out.Agents = map[string]fileAgent{}
			}
			out.Agents[id] = fa
		}
	}
	out.Summary = runnerDiff(s.Summary, def.Summary)
	out.Digest = runnerDiff(s.Digest, def.Digest)
	if s.Language != def.Language {
		out.Language = s.Language
	}
	var svc fileService
	if !slices.Equal(s.Service.AutoCommands, def.Service.AutoCommands) {
		svc.AutoCommands = &s.Service.AutoCommands
	}
	if !slices.Equal(s.Service.TempDirs, def.Service.TempDirs) {
		svc.TempDirs = &s.Service.TempDirs
	}
	if svc != (fileService{}) {
		out.Service = &svc
	}
	if s.Tmux != def.Tmux {
		out.Tmux = &fileTmux{SessionName: s.Tmux.SessionName}
	}
	if s.Remote != def.Remote {
		r := fileRemote{Enabled: s.Remote.Enabled, Funnel: s.Remote.Funnel}
		if s.Remote.Hostname != def.Remote.Hostname {
			r.Hostname = s.Remote.Hostname
		}
		if s.Remote.StateDir != def.Remote.StateDir {
			r.StateDir = s.Remote.StateDir
		}
		out.Remote = &r
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func runnerDiff(r, def Runner) *fileRunner {
	var out fileRunner
	if r.Agent != def.Agent {
		out.Agent = r.Agent
	}
	if r.Minutes != def.Minutes {
		out.Minutes = r.Minutes
	}
	if out == (fileRunner{}) {
		return nil
	}
	return &out
}

// writeFile пишет атомарно: временный файл рядом и rename. Права 0600, как у первой версии.
func writeFile(path string, raw []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(raw); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
