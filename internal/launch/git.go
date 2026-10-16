package launch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/tmux"
)

var (
	ErrNotRepo      = errors.New("not a git repository")
	ErrBranchExists = errors.New("branch already exists")
	ErrDirty        = errors.New("worktree has uncommitted changes")
	ErrBusy         = errors.New("other agents are running in the worktree")
	ErrNotPushed    = errors.New("branch is neither pushed nor merged")
	ErrMainWorktree = errors.New("the main worktree of a repository cannot be removed")
)

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Проверки не ходят в сеть, но и случайный запрос пароля не должен повесить сервер.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Repo — где папка в git.
type Repo struct {
	Root   string // корень рабочей папки: основной репозиторий или копия
	Main   string // корень основного репозитория
	Branch string // текущая ветка; пусто, если HEAD не на ветке
}

// Worktree — копия, если папка в копии, а не в основном репозитории.
func (r Repo) Worktree() (model.Worktree, bool) {
	if r.Root == r.Main {
		return model.Worktree{}, false
	}
	return model.Worktree{Path: r.Root, Branch: r.Branch, Repo: r.Main}, true
}

// RepoOf — репозиторий папки dir. Не git — ErrNotRepo.
func RepoOf(dir string) (Repo, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Repo{}, fmt.Errorf("folder %s does not exist", dir)
	}
	out, err := git(dir, "rev-parse", "--show-toplevel", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Repo{}, ErrNotRepo
	}
	root, common, _ := strings.Cut(out, "\n")
	// Общая папка .git лежит в основном репозитории; у копии своя .git — файл со ссылкой на неё.
	main := root
	if filepath.Base(common) == ".git" {
		main = filepath.Dir(common)
	}
	branch, _ := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	return Repo{Root: root, Main: main, Branch: branch}, nil
}

// WorktreeDir — свободная папка для копии: ~/.nit/worktrees/<проект>/<ветка>. Косая черта
// в ветке становится «-», чтобы копия была одной папкой; занятая папка получает -2, -3…
func WorktreeDir(project, branch string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return freeDir(filepath.Join(home, ".nit", "worktrees", project, strings.ReplaceAll(branch, "/", "-"))), nil
}

func freeDir(dir string) string {
	candidate := dir
	for i := 2; exists(candidate); i++ {
		candidate = dir + "-" + strconv.Itoa(i)
	}
	return candidate
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// BranchExists — есть ли в репозитории локальная ветка.
func BranchExists(repo, branch string) bool {
	_, err := git(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// FreeBranch — имя ветки, которой ещё нет: name, затем name-2, name-3…
func FreeBranch(repo, name string) string {
	candidate := name
	for i := 2; BranchExists(repo, candidate); i++ {
		candidate = name + "-" + strconv.Itoa(i)
	}
	return candidate
}

// CreateWorktree делает копию репозитория repo в папке dir с новой веткой branch
// от base (пусто — от HEAD репозитория).
func CreateWorktree(repo, dir, branch, base string) (model.Worktree, error) {
	r, err := RepoOf(repo)
	if err != nil {
		return model.Worktree{}, err
	}
	if _, err := git(repo, "check-ref-format", "--branch", branch); err != nil || branch == "" {
		return model.Worktree{}, fmt.Errorf("bad branch name %q", branch)
	}
	if BranchExists(repo, branch) {
		return model.Worktree{}, fmt.Errorf("%w: %s", ErrBranchExists, branch)
	}
	if exists(dir) {
		return model.Worktree{}, fmt.Errorf("folder %s already exists", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return model.Worktree{}, err
	}
	args := []string{"worktree", "add", "-b", branch, dir}
	if base != "" {
		args = append(args, base)
	}
	if _, err := git(repo, args...); err != nil {
		return model.Worktree{}, err
	}
	return model.Worktree{Path: dir, Branch: branch, Repo: r.Main}, nil
}

// Worktrees — все рабочие папки репозитория, первой — основная.
func Worktrees(repo string) ([]model.Worktree, error) {
	r, err := RepoOf(repo)
	if err != nil {
		return nil, err
	}
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []model.Worktree
	for _, block := range strings.Split(out, "\n\n") {
		var wt model.Worktree
		for _, line := range strings.Split(block, "\n") {
			field, value, _ := strings.Cut(line, " ")
			switch field {
			case "worktree":
				wt.Path = value
			case "branch":
				wt.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		}
		if wt.Path != "" {
			wt.Repo = r.Main
			list = append(list, wt)
		}
	}
	return list, nil
}

// Finish — что известно о копии перед тем, как её удалить.
type Finish struct {
	Worktree model.Worktree
	Dirty    []string // незакоммиченные файлы, вместе с новыми
	Pushed   bool     // последний коммит ветки есть на удалённом сервере
	Merged   bool     // ветка влита в основную ветку, локальную или на сервере
	Default  string   // основная ветка, с которой сравнивали: origin/main, main…
	Others   []string // имена других терминалов «Нити», где агент работает в этой копии
}

// CanRemove — копию можно удалить, ничего не потеряв: всё закоммичено и других агентов в ней нет.
func (f Finish) CanRemove() bool { return len(f.Dirty) == 0 && len(f.Others) == 0 }

// CanDeleteBranch — ветку можно удалить: её коммиты есть на сервере или в основной ветке.
func (f Finish) CanDeleteBranch() bool { return f.Pushed || f.Merged }

// Check собирает, что помешает удалить копию. self — имя терминала завершаемой сессии:
// его агент другим не считается.
func Check(wt model.Worktree, self string) (Finish, error) {
	f := Finish{Worktree: wt}
	out, err := git(wt.Path, "status", "--porcelain=v1", "-z")
	if err != nil {
		return f, err
	}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		f.Dirty = append(f.Dirty, e[3:])
		// У переименования следом идёт старое имя файла.
		if e[0] == 'R' || e[0] == 'C' {
			i++
		}
	}
	if wt.Branch != "" {
		refs, _ := git(wt.Path, "for-each-ref", "--contains", "refs/heads/"+wt.Branch, "--format=%(refname)", "refs/remotes")
		f.Pushed = refs != ""
		for _, d := range defaultBranches(wt.Path) {
			if f.Default == "" {
				f.Default = d
			}
			if _, err := git(wt.Path, "merge-base", "--is-ancestor", "refs/heads/"+wt.Branch, d); err == nil {
				f.Merged = true
				break
			}
		}
	}
	panes, err := tmux.List()
	if err != nil && !errors.Is(err, tmux.ErrNoTmux) {
		return f, err
	}
	for _, p := range panes {
		if p.Name != self && !p.Dead && inside(p.Cwd, wt.Path) {
			f.Others = append(f.Others, p.Name)
		}
	}
	return f, nil
}

// defaultBranches — основная ветка на сервере (origin/HEAD) и локальные main и master, какие есть.
func defaultBranches(dir string) []string {
	var list []string
	if head, err := git(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		list = append(list, head)
	}
	for _, name := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := git(dir, "rev-parse", "--verify", "--quiet", name+"^{commit}"); err == nil && !contains(list, name) {
			list = append(list, name)
		}
	}
	return list
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func inside(dir, root string) bool {
	if dir == "" {
		return false
	}
	dir, root = realPath(dir), realPath(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Remove удаляет копию и, если deleteBranch, её ветку. Отказывает, если в копии есть
// незакоммиченные файлы или другие агенты, а ветку не удаляет, если она никуда не отправлена
// и не влита. self — терминал завершаемой сессии; его сервер закрывает до удаления.
func Remove(wt model.Worktree, deleteBranch bool, self string) error {
	if wt.Repo == "" || realPath(wt.Path) == realPath(wt.Repo) {
		return ErrMainWorktree
	}
	f, err := Check(wt, self)
	if err != nil {
		return err
	}
	if len(f.Dirty) > 0 {
		return fmt.Errorf("%w: %s", ErrDirty, strings.Join(f.Dirty, ", "))
	}
	if len(f.Others) > 0 {
		return fmt.Errorf("%w: %s", ErrBusy, strings.Join(f.Others, ", "))
	}
	if deleteBranch && !f.CanDeleteBranch() {
		return fmt.Errorf("%w: %s", ErrNotPushed, wt.Branch)
	}
	// Без --force: git сам ещё раз откажет, если в копии что-то изменилось.
	if _, err := git(wt.Repo, "worktree", "remove", wt.Path); err != nil {
		return err
	}
	if deleteBranch && wt.Branch != "" {
		// -D: что ветка отправлена или влита, проверено выше; -d смотрел бы только на HEAD.
		if _, err := git(wt.Repo, "branch", "-D", wt.Branch); err != nil {
			return err
		}
	}
	return nil
}
