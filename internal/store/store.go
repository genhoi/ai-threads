// Package store хранит пометки человека о сессиях и проектах в state.json: названия, закрепления,
// завершённые и скрытые сессии, просмотры, сводки, копии репозитория и открытые терминалы.
//
// Файл совместим с первой версией: она пишет version 2 с теми же полями. Новые поля (seen,
// projects, worktrees, terminals) первая версия пропускает при чтении, но теряет, когда сама
// перезаписывает файл. Поля, которых этот пакет не знает, сохраняются при перезаписи.
package store

import (
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

	"github.com/genhoi/ai-threads/internal/model"
)

// DataDir — папка данных: $AI_THREADS_DATA, иначе $XDG_DATA_HOME/ai-threads,
// иначе ~/.local/share/ai-threads.
func DataDir() string {
	if dir := os.Getenv("AI_THREADS_DATA"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "ai-threads")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ai-threads")
}

// DefaultPath — state.json в папке данных.
func DefaultPath() string { return filepath.Join(DataDir(), "state.json") }

// Change — что поменялось. Приходит подписчикам OnChange после записи на диск.
type Change struct {
	Kind string // KindSession, KindProject, KindTerminal, KindDigest или KindAll
	Key  string // ключ сессии, путь проекта или период сводки в днях; у KindAll и отметок сводки пусто
}

const (
	KindSession  = "session"  // название, закрепление, завершение, скрытие, просмотр, сводка, копия
	KindProject  = "project"  // пометки проекта
	KindTerminal = "terminal" // открытые терминалы
	KindDigest   = "digest"   // сводка за несколько дней и её отметки
	KindAll      = "all"      // загрузка выгрузки: поменяться могло что угодно
)

// Store — пометки человека. Методы можно звать из нескольких горутин.
type Store struct {
	path  string
	extra map[string]json.RawMessage // чужие поля state.json, только переписываются обратно

	mu      sync.Mutex
	st      State
	digests map[string]json.RawMessage
	hooks   []func(Change)
}

// Open читает state.json. Файла нет — пустые пометки, файл появится при первом изменении.
// Повреждённый файл не затирается: Open возвращает ошибку.
func Open(path string) (*Store, error) {
	s := &Store{path: path, st: emptyState(), digests: map[string]json.RawMessage{},
		extra: map[string]json.RawMessage{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.st, s.digests, s.extra = f.State, f.digests, f.extra
	return s, nil
}

// Path — файл, в котором лежат пометки.
func (s *Store) Path() string { return s.path }

// OnChange подписывает fn на изменения. fn зовётся после записи на диск, вне блокировки.
func (s *Store) OnChange(fn func(Change)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = append(s.hooks, fn)
}

// Snapshot — копия всех пометок, кроме сводок за несколько дней.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.clone()
}

// Marks — пометки одной сессии.
func (s *Store) Marks(key string) Marks {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.st.Marks(key)
	if m.Summary != nil {
		m.Summary.Related = slices.Clone(m.Summary.Related)
	}
	return m
}

// update меняет копию состояния, пишет её на диск и только потом подменяет состояние в памяти:
// при сбое записи не меняются ни файл, ни память. change возвращает false, если менять нечего.
func (s *Store) update(c Change, change func(st *State, digests map[string]json.RawMessage) bool) error {
	s.mu.Lock()
	st, digests := s.st.clone(), maps.Clone(s.digests)
	if !change(&st, digests) {
		s.mu.Unlock()
		return nil
	}
	raw, err := encode(st, digests, s.extra)
	if err == nil {
		err = writeFile(s.path, raw)
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.st, s.digests = st, digests
	hooks := slices.Clone(s.hooks)
	s.mu.Unlock()
	for _, fn := range hooks {
		fn(c)
	}
	return nil
}

// writeFile пишет атомарно: временный файл рядом, fsync, rename. Файл доступен только владельцу:
// в нём названия рабочих задач.
func writeFile(path string, raw []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
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

func (s *Store) setFlag(list func(*State) *[]string, key string, on bool) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		l := list(st)
		i := slices.Index(*l, key)
		switch {
		case on && i < 0:
			*l = append(*l, key)
		case !on && i >= 0:
			*l = slices.Delete(*l, i, i+1)
		default:
			return false
		}
		return true
	})
}

// SetPinned закрепляет сессию или снимает закрепление.
func (s *Store) SetPinned(key string, on bool) error {
	return s.setFlag(func(st *State) *[]string { return &st.Pins }, key, on)
}

// SetDone отмечает сессию завершённой или снимает отметку.
func (s *Store) SetDone(key string, on bool) error {
	return s.setFlag(func(st *State) *[]string { return &st.Done }, key, on)
}

// SetHidden скрывает сессию или возвращает её в списки.
func (s *Store) SetHidden(key string, on bool) error {
	return s.setFlag(func(st *State) *[]string { return &st.Hidden }, key, on)
}

// SetName задаёт название сессии; пустое название возвращает название из журнала.
func (s *Store) SetName(key, name string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if name != "" {
		var err error
		if name, err = cleanName("name", name); err != nil {
			return err
		}
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		old, ok := st.Names[key]
		if name == "" {
			delete(st.Names, key)
			return ok
		}
		st.Names[key] = name
		return old != name
	})
}

// MarkSeen запоминает, когда человек смотрел сессию: по этому времени снимается отметка «новое».
func (s *Store) MarkSeen(key string, at time.Time) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	sec := at.Unix()
	if sec < 0 {
		return &InvalidError{Field: "seen", Code: "seen"}
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		if st.Seen[key] == sec {
			return false
		}
		st.Seen[key] = sec
		return true
	})
}

// SetSummary сохраняет сводку по сессии.
func (s *Store) SetSummary(key string, sum Summary) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	sum.Related = slices.Clone(sum.Related)
	if err := sum.clean("summary"); err != nil {
		return err
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		st.Summaries[key] = sum
		return true
	})
}

// checkDays — периоды сводки: 3, 5 и 7 дней, а 4 — от первой версии.
func checkDays(days int) error {
	if days < 3 || days > 5 && days != 7 {
		return &InvalidError{Field: "days", Code: "days"}
	}
	return nil
}

// SetDigest сохраняет сводку за 3, 4, 5 или 7 дней. Её устройство — дело пакета сводок, здесь
// она хранится как есть.
func (s *Store) SetDigest(days int, digest json.RawMessage) error {
	if err := checkDays(days); err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(digest, &obj) != nil || obj == nil {
		return &InvalidError{Field: "digest", Code: "digest"}
	}
	value := slices.Clone(json.RawMessage(digest))
	return s.update(Change{KindDigest, strconv.Itoa(days)}, func(_ *State, digests map[string]json.RawMessage) bool {
		digests[strconv.Itoa(days)] = value
		return true
	})
}

// Digest — сводка за days дней или nil, если её ещё не составляли.
func (s *Store) Digest(days int) (json.RawMessage, error) {
	if err := checkDays(days); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.digests[strconv.Itoa(days)]), nil
}

// SetTail отмечает пункт «что осталось» в сводке за несколько дней.
func (s *Store) SetTail(id string, done bool) error {
	if strings.TrimSpace(id) == "" {
		return &InvalidError{Field: "id", Code: "tail"}
	}
	return s.update(Change{KindDigest, ""}, func(st *State, _ map[string]json.RawMessage) bool {
		old, ok := st.DigestTails[id]
		st.DigestTails[id] = done
		return !ok || old != done
	})
}

// updateProject меняет пометки проекта. Проект без пометок удаляется из файла.
func (s *Store) updateProject(path string, change func(p *Project)) error {
	path, err := cleanPath("path", path)
	if err != nil {
		return err
	}
	return s.update(Change{KindProject, path}, func(st *State, _ map[string]json.RawMessage) bool {
		old := st.Projects[path]
		p := old
		change(&p)
		if p == old {
			return false
		}
		if p == (Project{}) {
			delete(st.Projects, path)
		} else {
			st.Projects[path] = p
		}
		return true
	})
}

// SetProjectPinned закрепляет проект в панели.
func (s *Store) SetProjectPinned(path string, on bool) error {
	return s.updateProject(path, func(p *Project) { p.Pinned = on })
}

// SetProjectHidden скрывает проект из панели.
func (s *Store) SetProjectHidden(path string, on bool) error {
	return s.updateProject(path, func(p *Project) { p.Hidden = on })
}

// SetProjectAgent задаёт агента по умолчанию для новых сессий в проекте; пустой — снять.
func (s *Store) SetProjectAgent(path, agent string) error {
	if err := validAgent("agent", agent, true); err != nil {
		return err
	}
	return s.updateProject(path, func(p *Project) { p.DefaultAgent = agent })
}

// SetWorktree запоминает, что сессия работает в копии репозитория.
func (s *Store) SetWorktree(key string, w model.Worktree) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := checkWorktree("worktree", w); err != nil {
		return err
	}
	w.Path = filepath.Clean(w.Path)
	if w.Repo != "" {
		w.Repo = filepath.Clean(w.Repo)
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		old, ok := st.Worktrees[key]
		st.Worktrees[key] = w
		return !ok || old != w
	})
}

// ForgetWorktree забывает копию сессии, например после её удаления.
func (s *Store) ForgetWorktree(key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	return s.update(Change{KindSession, key}, func(st *State, _ map[string]json.RawMessage) bool {
		_, ok := st.Worktrees[key]
		delete(st.Worktrees, key)
		return ok
	})
}

// RecordTerminal запоминает терминал, который открыла «Нить». Агент по умолчанию — из ключа,
// время открытия — сейчас.
func (s *Store) RecordTerminal(key string, t Terminal) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := t.clean("terminal", key); err != nil {
		return err
	}
	if t.Cwd != "" {
		t.Cwd = filepath.Clean(t.Cwd)
	}
	if t.Started == 0 {
		t.Started = time.Now().Unix()
	}
	return s.update(Change{KindTerminal, key}, func(st *State, _ map[string]json.RawMessage) bool {
		old, ok := st.Terminals[key]
		st.Terminals[key] = t
		return !ok || old != t
	})
}

// ForgetTerminal забывает терминал: его закрыли в «Нити».
func (s *Store) ForgetTerminal(key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	return s.update(Change{KindTerminal, key}, func(st *State, _ map[string]json.RawMessage) bool {
		_, ok := st.Terminals[key]
		delete(st.Terminals, key)
		return ok
	})
}

// Export — пометки одним файлом, как /api/export первой версии, плюс seen, projects и worktrees.
// Сводки за несколько дней и открытые терминалы в выгрузку не входят: первые составляются
// заново, вторые имеют смысл только на этом компьютере.
func (s *Store) Export() ([]byte, error) {
	st := s.Snapshot()
	st.Terminals = nil
	return encode(st, nil, nil)
}

// Import добавляет пометки из выгрузки, как первая версия: списки объединяются, названия
// и сводки из файла заменяют свои, просмотр берётся поздний. Неверный файл не меняет ничего.
// Поля, которых пакет не знает (например, настройки), пропускаются.
func (s *Store) Import(raw []byte) error {
	in, err := parse(raw)
	if err != nil {
		return err
	}
	return s.update(Change{KindAll, ""}, func(st *State, _ map[string]json.RawMessage) bool {
		st.Pins = union(st.Pins, in.Pins)
		st.Done = union(st.Done, in.Done)
		st.Hidden = union(st.Hidden, in.Hidden)
		maps.Copy(st.Names, in.Names)
		maps.Copy(st.Summaries, in.Summaries)
		maps.Copy(st.DigestTails, in.DigestTails)
		maps.Copy(st.Projects, in.Projects)
		maps.Copy(st.Worktrees, in.Worktrees)
		for key, at := range in.Seen {
			st.Seen[key] = max(st.Seen[key], at)
		}
		st.MigratedLocalStorage = st.MigratedLocalStorage || in.MigratedLocalStorage
		return true
	})
}

// Migrate переносит закрепления и названия из localStorage страницы первой версии.
func (s *Store) Migrate(pins []string, names map[string]string) error {
	data := map[string]any{"version": 2, "pins": orEmpty(pins), "migrated_local_storage": true}
	if names != nil {
		data["names"] = names
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.Import(raw)
}

func union(a, b []string) []string {
	for _, v := range b {
		if !slices.Contains(a, v) {
			a = append(a, v)
		}
	}
	return a
}
