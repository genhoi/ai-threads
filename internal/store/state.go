package store

import (
	"bytes"
	"encoding/json"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/genhoi/ai-threads/internal/model"
)

// Агенты, чьи сессии можно пометить. Ключ сессии — <агент>:<id>, как в первой версии.
var (
	agentIDs  = []string{"codex", "claude", "grok", "kimi", "zcode", "cursor"}
	sessionID = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// Summary — сводка по сессии, которую составил агент. Поля как у первой версии.
type Summary struct {
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	NextStep string    `json:"next_step"`
	Closed   bool      `json:"closed"`
	Related  []Related `json:"related"`
	At       float64   `json:"at"`    // секунды Unix с долями: так пишет первая версия
	Model    string    `json:"model"` // агент, который составил сводку
}

// Related — связанная сессия в сводке.
type Related struct {
	Key string `json:"key"`
	Why string `json:"why"`
}

// Project — пометки проекта. Ключ — путь к папке основного репозитория.
type Project struct {
	Pinned       bool   `json:"pinned,omitempty"`
	Hidden       bool   `json:"hidden,omitempty"`
	DefaultAgent string `json:"default_agent,omitempty"`
}

// Terminal — терминал tmux, который открыла «Нить». Запись нужна, чтобы после перезагрузки
// показать, какие сессии были открыты.
type Terminal struct {
	Tmux    string `json:"tmux"` // имя сессии tmux
	Cwd     string `json:"cwd"`
	Started int64  `json:"started"` // секунды Unix
	Agent   string `json:"agent"`
}

// worktree — копия репозитория в файле; в коде это model.Worktree.
type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Repo   string `json:"repo"`
}

// State — снимок пометок. Это копия: менять её можно, на хранилище это не влияет.
type State struct {
	Pins        []string // в порядке закрепления
	Names       map[string]string
	Done        []string
	Hidden      []string
	Summaries   map[string]Summary
	DigestTails map[string]bool  // отмеченные пункты «что осталось» в сводке за несколько дней
	Seen        map[string]int64 // когда человек последний раз смотрел сессию, секунды Unix
	Projects    map[string]Project
	Worktrees   map[string]model.Worktree
	Terminals   map[string]Terminal
	// Пометки из localStorage первой версии уже перенесены.
	MigratedLocalStorage bool
}

// Marks — пометки одной сессии.
type Marks struct {
	Name     string
	Pinned   bool
	Done     bool
	Hidden   bool
	Seen     int64 // секунды Unix; 0 — не смотрел
	Summary  *Summary
	Worktree *model.Worktree
	Terminal *Terminal
}

// Marks — пометки сессии по ключу.
func (st State) Marks(key string) Marks {
	m := Marks{
		Name:   st.Names[key],
		Pinned: slices.Contains(st.Pins, key),
		Done:   slices.Contains(st.Done, key),
		Hidden: slices.Contains(st.Hidden, key),
		Seen:   st.Seen[key],
	}
	if v, ok := st.Summaries[key]; ok {
		m.Summary = &v
	}
	if v, ok := st.Worktrees[key]; ok {
		m.Worktree = &v
	}
	if v, ok := st.Terminals[key]; ok {
		m.Terminal = &v
	}
	return m
}

func emptyState() State {
	return State{
		Pins: []string{}, Done: []string{}, Hidden: []string{},
		Names: map[string]string{}, Summaries: map[string]Summary{}, DigestTails: map[string]bool{},
		Seen: map[string]int64{}, Projects: map[string]Project{},
		Worktrees: map[string]model.Worktree{}, Terminals: map[string]Terminal{},
	}
}

// clone — глубокая копия: снимок и запись на диск не делят память с хранилищем.
func (st State) clone() State {
	c := st
	c.Pins = slices.Clone(st.Pins)
	c.Done = slices.Clone(st.Done)
	c.Hidden = slices.Clone(st.Hidden)
	c.Names = maps.Clone(st.Names)
	c.Summaries = make(map[string]Summary, len(st.Summaries))
	for k, v := range st.Summaries {
		v.Related = slices.Clone(v.Related)
		c.Summaries[k] = v
	}
	c.DigestTails = maps.Clone(st.DigestTails)
	c.Seen = maps.Clone(st.Seen)
	c.Projects = maps.Clone(st.Projects)
	c.Worktrees = maps.Clone(st.Worktrees)
	c.Terminals = maps.Clone(st.Terminals)
	return c
}

// file — содержимое state.json, разобранное и проверенное.
type file struct {
	State
	digests map[string]json.RawMessage // сводки за 3, 4, 5 и 7 дней как есть
	extra   map[string]json.RawMessage // поля, которых пакет не знает
}

// Поля state.json, которые пакет понимает. Первая версия знает все, кроме последних четырёх.
var known = []string{"version", "pins", "names", "summaries", "done", "hidden", "digests",
	"digest_tails", "migrated_local_storage", "seen", "projects", "worktrees", "terminals"}

// parse разбирает state.json или файл выгрузки. Как в первой версии, у данных версии 1 берутся
// только закрепления и названия.
func parse(raw []byte) (file, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return file{}, &InvalidError{Code: "json", Value: err.Error()}
	}
	version := string(bytes.TrimSpace(top["version"]))
	if version != "1" && version != "2" {
		return file{}, &InvalidError{Field: "version", Code: "version"}
	}
	f := file{State: emptyState(), digests: map[string]json.RawMessage{}, extra: map[string]json.RawMessage{}}
	for name, value := range top {
		if !slices.Contains(known, name) {
			f.extra[name] = value
		}
	}
	var err error
	if f.Pins, err = keyList("pins", top["pins"]); err != nil {
		return file{}, err
	}
	if err := decodeMap("names", top["names"], &f.Names, func(key string, name *string) (err error) {
		*name, err = cleanName("names", *name)
		return err
	}); err != nil {
		return file{}, err
	}
	if version == "1" {
		return f, nil
	}
	if f.Done, err = keyList("done", top["done"]); err != nil {
		return file{}, err
	}
	if f.Hidden, err = keyList("hidden", top["hidden"]); err != nil {
		return file{}, err
	}
	if err := decodeMap("summaries", top["summaries"], &f.Summaries, func(_ string, s *Summary) error {
		return s.clean("summaries")
	}); err != nil {
		return file{}, err
	}
	if err := decode("digest_tails", top["digest_tails"], &f.DigestTails, "object"); err != nil {
		return file{}, err
	}
	for id := range f.DigestTails {
		if strings.TrimSpace(id) == "" {
			return file{}, &InvalidError{Field: "digest_tails", Code: "tail"}
		}
	}
	if err := decode("migrated_local_storage", top["migrated_local_storage"], &f.MigratedLocalStorage, "bool"); err != nil {
		return file{}, err
	}
	if err := decode("digests", top["digests"], &f.digests, "object"); err != nil {
		return file{}, err
	}
	if err := decodeMap("seen", top["seen"], &f.Seen, func(_ string, at *int64) error {
		if *at < 0 {
			return &InvalidError{Field: "seen", Code: "seen"}
		}
		return nil
	}); err != nil {
		return file{}, err
	}
	var projects map[string]Project
	if err := decode("projects", top["projects"], &projects, "object"); err != nil {
		return file{}, err
	}
	for path, p := range projects {
		clean, err := cleanPath("projects", path)
		if err != nil {
			return file{}, err
		}
		if err := validAgent("projects", p.DefaultAgent, true); err != nil {
			return file{}, err
		}
		f.Projects[clean] = p
	}
	var worktrees map[string]worktree
	if err := decodeMap("worktrees", top["worktrees"], &worktrees, func(_ string, w *worktree) error {
		return checkWorktree("worktrees", model.Worktree(*w))
	}); err != nil {
		return file{}, err
	}
	for key, w := range worktrees {
		f.Worktrees[key] = model.Worktree(w)
	}
	if err := decodeMap("terminals", top["terminals"], &f.Terminals, func(key string, t *Terminal) error {
		return t.clean("terminals", key)
	}); err != nil {
		return file{}, err
	}
	return f, nil
}

// decode разбирает одно поле. Отсутствующее поле оставляет значение как есть, null — ошибка,
// как в первой версии.
func decode(field string, raw json.RawMessage, into any, code string) error {
	if raw == nil {
		return nil
	}
	if string(bytes.TrimSpace(raw)) == "null" || json.Unmarshal(raw, into) != nil {
		return &InvalidError{Field: field, Code: code}
	}
	return nil
}

// decodeMap разбирает объект «ключ сессии → значение» и проверяет каждую пару.
func decodeMap[V any](field string, raw json.RawMessage, into *map[string]V, check func(string, *V) error) error {
	var m map[string]V
	if err := decode(field, raw, &m, "object"); err != nil || m == nil {
		return err
	}
	for key, v := range m {
		if err := checkKey(field, key); err != nil {
			return err
		}
		if err := check(key, &v); err != nil {
			return err
		}
		m[key] = v
	}
	if *into == nil {
		*into = m
		return nil
	}
	maps.Copy(*into, m)
	return nil
}

func keyList(field string, raw json.RawMessage) ([]string, error) {
	var keys []string
	if err := decode(field, raw, &keys, "list"); err != nil {
		return nil, err
	}
	out := []string{}
	for _, key := range keys {
		if err := checkKey(field, key); err != nil {
			return nil, err
		}
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	return out, nil
}

// encode — state.json или выгрузка. Пустые digests и terminals (nil) не пишутся: так выгрузка
// обходится без них, а state.json всегда их содержит.
func encode(st State, digests, extra map[string]json.RawMessage) ([]byte, error) {
	out := map[string]any{}
	for k, v := range extra {
		out[k] = v
	}
	worktrees := make(map[string]worktree, len(st.Worktrees))
	for k, w := range st.Worktrees {
		worktrees[k] = worktree(w)
	}
	summaries := make(map[string]Summary, len(st.Summaries))
	for k, s := range st.Summaries {
		// Первая версия не примет related: null.
		if s.Related == nil {
			s.Related = []Related{}
		}
		summaries[k] = s
	}
	out["version"] = 2
	out["pins"] = orEmpty(st.Pins)
	out["names"] = st.Names
	out["summaries"] = summaries
	out["done"] = orEmpty(st.Done)
	out["hidden"] = orEmpty(st.Hidden)
	out["digest_tails"] = st.DigestTails
	out["migrated_local_storage"] = st.MigratedLocalStorage
	out["seen"] = st.Seen
	out["projects"] = st.Projects
	out["worktrees"] = worktrees
	if digests != nil {
		out["digests"] = digests
	}
	if st.Terminals != nil {
		out["terminals"] = st.Terminals
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// ValidKey проверяет ключ сессии: агент из списка и id из латиницы, цифр, «_», «.», «-».
func ValidKey(key string) error { return checkKey("key", key) }

func checkKey(field, key string) error {
	agent, id, ok := strings.Cut(key, ":")
	if !ok || !slices.Contains(agentIDs, agent) || !sessionID.MatchString(id) {
		return &InvalidError{Field: field, Code: "key"}
	}
	return nil
}

func validAgent(field, agent string, empty bool) error {
	if (empty && agent == "") || slices.Contains(agentIDs, agent) {
		return nil
	}
	return &InvalidError{Field: field, Code: "agent", Value: agent}
}

// cleanName — название: от 1 до 200 символов, без пробелов по краям.
func cleanName(field, name string) (string, error) {
	if utf8.RuneCountInString(name) > 200 || strings.TrimSpace(name) == "" {
		return "", &InvalidError{Field: field, Code: "name"}
	}
	return strings.TrimSpace(name), nil
}

// cleanPath — путь к папке: абсолютный, без «..» и лишних «/».
func cleanPath(field, path string) (string, error) {
	if !filepath.IsAbs(path) || len(path) > 4096 || hasControl(path) {
		return "", &InvalidError{Field: field, Code: "path", Value: path}
	}
	return filepath.Clean(path), nil
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

func (s *Summary) clean(field string) error {
	title, err := cleanName(field, s.Title)
	if err != nil {
		return err
	}
	s.Title = title
	if strings.TrimSpace(s.Summary) == "" {
		return &InvalidError{Field: field, Code: "summary"}
	}
	if s.Related == nil {
		s.Related = []Related{}
	}
	for _, r := range s.Related {
		if err := checkKey(field, r.Key); err != nil {
			return &InvalidError{Field: field, Code: "related"}
		}
	}
	return nil
}

func checkWorktree(field string, w model.Worktree) error {
	if _, err := cleanPath(field, w.Path); err != nil {
		return err
	}
	if w.Repo != "" {
		if _, err := cleanPath(field, w.Repo); err != nil {
			return err
		}
	}
	if len(w.Branch) > 255 || hasControl(w.Branch) {
		return &InvalidError{Field: field, Code: "branch", Value: w.Branch}
	}
	return nil
}

// clean проверяет запись о терминале; агент по умолчанию берётся из ключа.
func (t *Terminal) clean(field, key string) error {
	if strings.TrimSpace(t.Tmux) == "" || len(t.Tmux) > 200 || hasControl(t.Tmux) {
		return &InvalidError{Field: field, Code: "tmux"}
	}
	if t.Cwd != "" {
		if _, err := cleanPath(field, t.Cwd); err != nil {
			return err
		}
	}
	if t.Agent == "" {
		t.Agent, _, _ = strings.Cut(key, ":")
	}
	return validAgent(field, t.Agent, false)
}

// InvalidError — неверные данные: от интерфейса, из выгрузки или из state.json. Field — поле,
// к которому относится ошибка; интерфейс показывает Text рядом с ним.
type InvalidError struct {
	Field string
	Code  string
	Value string // подставляется в текст вместо {value}
}

var messages = map[string][2]string{
	"json":    {"Данные не читаются как JSON-объект: {value}", "The data is not a JSON object: {value}"},
	"version": {"Поддерживаются данные версий 1 и 2", "Only data versions 1 and 2 are supported"},
	"key":     {"Неверный ключ сессии", "Invalid session key"},
	"name":    {"Название должно содержать от 1 до 200 символов", "The name must be 1 to 200 characters"},
	"list":    {"Нужен список", "Must be a list"},
	"object":  {"Нужен объект", "Must be an object"},
	"bool":    {"Ожидается логическое значение", "Expected true or false"},
	"summary": {"Текст сводки не должен быть пустым", "The summary text must not be empty"},
	"related": {"Неверная связанная сессия", "Invalid related session"},
	"days":    {"Период должен быть 3, 4, 5 или 7 дней", "The period must be 3, 4, 5 or 7 days"},
	"digest":  {"Сводка должна быть JSON-объектом", "The digest must be a JSON object"},
	"tail":    {"Неверный пункт сводки", "Invalid digest item"},
	"seen":    {"Нужно время в секундах Unix", "Must be a Unix time in seconds"},
	"path":    {"Нужен абсолютный путь: {value}", "Must be an absolute path: {value}"},
	"agent":   {"Неизвестный агент: {value}", "Unknown agent: {value}"},
	"branch":  {"Неверное имя ветки: {value}", "Invalid branch name: {value}"},
	"tmux":    {"Нужно имя сессии tmux", "A tmux session name is required"},
}

// Text — сообщение на языке интерфейса: en — английский, иначе русский.
func (e *InvalidError) Text(lang string) string {
	pair, ok := messages[e.Code]
	if !ok {
		return e.Code
	}
	text := pair[0]
	if lang == "en" {
		text = pair[1]
	}
	return strings.ReplaceAll(text, "{value}", e.Value)
}

func (e *InvalidError) Error() string {
	if e.Field == "" {
		return e.Text("ru")
	}
	return e.Field + ": " + e.Text("ru")
}
