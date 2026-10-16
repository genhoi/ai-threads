package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

// repoTTL — столько верен найденный корень репозитория: папка может стать репозиторием,
// а копия — исчезнуть.
const repoTTL = 5 * time.Minute

// RepoRoot — корень основного репозитория для папки dir. Для копии (git worktree) — папка
// исходного репозитория, для обычного репозитория — его верхняя папка. Пусто вне git и если
// папки нет. git не запускается: читаются только файлы .git.
func RepoRoot(dir string) string {
	root, _ := repoRoot(dir)
	return root
}

// repoRoot — корень и признак, что папка есть.
func repoRoot(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false
	}
	for d := dir; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			if info.IsDir() {
				return d, true
			}
			if info.Mode().IsRegular() {
				return worktreeRepo(d), true
			}
		}
		if filepath.Dir(d) == d {
			return "", true
		}
	}
}

// worktreeRepo — исходный репозиторий копии top, у которой .git — файл
// «gitdir: <репозиторий>/.git/worktrees/<имя>». У подмодуля gitdir указывает на
// .git/modules/…: это свой репозиторий, его корень — top.
func worktreeRepo(top string) string {
	data, err := os.ReadFile(filepath.Join(top, ".git"))
	if err != nil {
		return top
	}
	line, _, _ := strings.Cut(string(data), "\n")
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return top
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(top, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	if filepath.Base(filepath.Dir(gitdir)) != "worktrees" {
		return top
	}
	common := filepath.Dir(filepath.Dir(gitdir))
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	// Голый репозиторий: другой папки, кроме него самого, у проекта нет.
	return common
}

// repos — корни репозиториев по папкам сессий.
type repos struct {
	known   map[string]string // папка → корень; переживает перезапуск через кэш на диске
	checked map[string]time.Time
}

func newRepos() *repos {
	return &repos{known: map[string]string{}, checked: map[string]time.Time{}}
}

// root — корень репозитория папки dir. Если папки больше нет (копию удалили после
// завершения), остаётся найденный раньше корень: так сессия не теряет проект.
func (r *repos) root(dir string) string {
	if dir == "" {
		return ""
	}
	if at, ok := r.checked[dir]; ok && time.Since(at) < repoTTL {
		return r.known[dir]
	}
	root, exists := repoRoot(dir)
	r.checked[dir] = time.Now()
	if exists {
		if root == "" {
			delete(r.known, dir)
		} else {
			r.known[dir] = root
		}
	}
	return r.known[dir]
}

// Temp — сессия во временной папке: её папка — одна из dirs или внутри неё. «~/» в dirs
// раскрывается.
func Temp(s model.Session, dirs []string) bool {
	cwd := strings.TrimRight(s.Cwd, "/")
	if cwd == "" {
		return false
	}
	home, _ := os.UserHomeDir()
	for _, root := range dirs {
		if root == "~" || strings.HasPrefix(root, "~/") {
			root = home + root[1:]
		}
		root = strings.TrimRight(root, "/")
		if root == "" {
			root = "/"
		}
		if cwd == root || strings.HasPrefix(cwd, strings.TrimRight(root, "/")+"/") {
			return true
		}
	}
	return false
}

// Service — служебная сессия: запуск без человека или сессия во временной папке. Такие
// скрыты из списков и показываются фильтром.
func Service(s model.Session, dirs []string) bool {
	return s.Auto || Temp(s, dirs)
}
