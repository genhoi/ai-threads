package launch_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/genhoi/ai-threads/internal/launch"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/tmux"
	"github.com/genhoi/ai-threads/internal/tmux/tmuxtest"
)

func TestMain(m *testing.M) {
	// git без настроек человека: подпись коммитов и хуки из ~/.gitconfig тестам не нужны.
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		os.Setenv(k, v)
	}
	tmuxtest.Main(m)
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo — репозиторий с одним коммитом в main и сервером origin, куда main отправлена.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "backend")
	remote := filepath.Join(root, "origin.git")
	run(t, root, "init", "-q", "--bare", "-b", "main", remote)
	run(t, root, "init", "-q", "-b", "main", dir)
	write(t, filepath.Join(dir, "README"), "hello\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "first")
	run(t, dir, "remote", "add", "origin", remote)
	run(t, dir, "push", "-q", "-u", "origin", "main")
	run(t, dir, "remote", "set-head", "origin", "main")
	return dir
}

func TestRepoOf(t *testing.T) {
	if _, err := launch.RepoOf(t.TempDir()); !errors.Is(err, launch.ErrNotRepo) {
		t.Fatalf("plain folder: %v", err)
	}
	if _, err := launch.RepoOf(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatal("missing folder must fail")
	}
	dir := repo(t)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	r, err := launch.RepoOf(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if r != (launch.Repo{Root: dir, Main: dir, Branch: "main"}) {
		t.Fatalf("repo = %+v", r)
	}
	if _, ok := r.Worktree(); ok {
		t.Fatal("main repository is not a worktree")
	}

	copyDir := filepath.Join(t.TempDir(), "backend", "BACKEND-1")
	wt, err := launch.CreateWorktree(dir, copyDir, "BACKEND-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if wt != (model.Worktree{Path: copyDir, Branch: "BACKEND-1", Repo: dir}) {
		t.Fatalf("worktree = %+v", wt)
	}
	r, err = launch.RepoOf(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Worktree(); !ok || got != wt {
		t.Fatalf("worktree of copy = %+v %v", got, ok)
	}
	// Копию копии делать можно: она всё равно привязана к основному репозиторию.
	second, err := launch.CreateWorktree(copyDir, filepath.Join(t.TempDir(), "two"), "two", "main")
	if err != nil || second.Repo != dir {
		t.Fatalf("second = %+v, %v", second, err)
	}
	list, err := launch.Worktrees(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Worktree{{Path: dir, Branch: "main", Repo: dir}, wt, second}
	if !reflect.DeepEqual(list, want) {
		t.Fatalf("worktrees = %+v", list)
	}
}

func TestCreateWorktreeRefuses(t *testing.T) {
	dir := repo(t)
	if _, err := launch.CreateWorktree(dir, filepath.Join(t.TempDir(), "x"), "main", ""); !errors.Is(err, launch.ErrBranchExists) {
		t.Fatalf("existing branch: %v", err)
	}
	if _, err := launch.CreateWorktree(dir, filepath.Join(t.TempDir(), "x"), "bad..name", ""); err == nil {
		t.Fatal("bad branch name must fail")
	}
	if _, err := launch.CreateWorktree(dir, t.TempDir(), "new", ""); err == nil {
		t.Fatal("existing folder must fail")
	}
	if launch.BranchExists(dir, "new") {
		t.Fatal("failed attempt must not leave a branch")
	}
	if got := launch.FreeBranch(dir, "main"); got != "main-2" {
		t.Fatalf("FreeBranch = %s", got)
	}
	if got := launch.FreeBranch(dir, "BACKEND-2"); got != "BACKEND-2" {
		t.Fatalf("FreeBranch = %s", got)
	}
}

func TestWorktreeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := launch.WorktreeDir("backend", "feature/login")
	if err != nil || dir != filepath.Join(home, ".nit", "worktrees", "backend", "feature-login") {
		t.Fatalf("dir = %s, %v", dir, err)
	}
	os.MkdirAll(dir, 0o755)
	if again, _ := launch.WorktreeDir("backend", "feature/login"); again != dir+"-2" {
		t.Fatalf("taken folder: %s", again)
	}
}

func TestFinishChecksAndRemove(t *testing.T) {
	tmuxtest.Server(t)
	dir := repo(t)
	wt, err := launch.CreateWorktree(dir, filepath.Join(t.TempDir(), "BACKEND-2"), "BACKEND-2", "")
	if err != nil {
		t.Fatal(err)
	}

	// Новая ветка без коммитов: её вершина и так есть в origin/main, ничего не потеряется.
	f, err := launch.Check(wt, "")
	if err != nil {
		t.Fatal(err)
	}
	if !f.CanRemove() || !f.Merged || !f.Pushed || f.Default != "origin/main" {
		t.Fatalf("fresh worktree: %+v", f)
	}

	// Незакоммиченные и новые файлы, коммит, которого нет нигде.
	write(t, filepath.Join(wt.Path, "README"), "changed\n")
	write(t, filepath.Join(wt.Path, "new.txt"), "new\n")
	f, _ = launch.Check(wt, "")
	if !reflect.DeepEqual(f.Dirty, []string{"README", "new.txt"}) || f.CanRemove() {
		t.Fatalf("dirty: %+v", f)
	}
	if err := launch.Remove(wt, true, ""); !errors.Is(err, launch.ErrDirty) {
		t.Fatalf("remove dirty: %v", err)
	}
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "work")
	f, _ = launch.Check(wt, "")
	if len(f.Dirty) != 0 || f.Merged || f.Pushed || f.CanDeleteBranch() {
		t.Fatalf("committed, not pushed: %+v", f)
	}
	if err := launch.Remove(wt, true, ""); !errors.Is(err, launch.ErrNotPushed) {
		t.Fatalf("remove unpushed: %v", err)
	}

	// В копии работает другой агент — удалять нельзя; свой терминал не считается.
	agent := tmuxtest.AgentTTY(t)
	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-other", Cwd: wt.Path, Argv: []string{agent}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-self", Cwd: filepath.Join(wt.Path), Argv: []string{agent}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux.Open(tmux.OpenOptions{Name: "nit-main", Cwd: dir, Argv: []string{agent}}); err != nil {
		t.Fatal(err)
	}
	run(t, wt.Path, "push", "-q", "origin", "BACKEND-2")
	f, _ = launch.Check(wt, "nit-self")
	if !reflect.DeepEqual(f.Others, []string{"nit-other"}) || !f.Pushed || f.CanRemove() {
		t.Fatalf("other agent: %+v", f)
	}
	if err := launch.Remove(wt, true, "nit-self"); !errors.Is(err, launch.ErrBusy) {
		t.Fatalf("remove busy: %v", err)
	}
	tmux.Close("nit-other")
	tmux.Close("nit-self")

	if err := launch.Remove(wt, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree folder remains: %v", err)
	}
	if launch.BranchExists(dir, "BACKEND-2") {
		t.Fatal("branch remains")
	}
	if err := launch.Remove(model.Worktree{Path: dir, Branch: "main", Repo: dir}, false, ""); !errors.Is(err, launch.ErrMainWorktree) {
		t.Fatalf("remove main: %v", err)
	}
}

func TestRemoveMergedKeepsOrDeletesBranch(t *testing.T) {
	tmuxtest.Server(t)
	dir := repo(t)
	wt, err := launch.CreateWorktree(dir, filepath.Join(t.TempDir(), "feature-x"), "feature/x", "")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt.Path, "x.txt"), "x\n")
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "x")
	// Влита в локальную main, на сервер не отправлялась.
	run(t, dir, "merge", "-q", "--no-ff", "-m", "merge", "feature/x")
	f, _ := launch.Check(wt, "")
	if !f.Merged || f.Pushed || !f.CanDeleteBranch() {
		t.Fatalf("merged: %+v", f)
	}
	if err := launch.Remove(wt, false, ""); err != nil {
		t.Fatal(err)
	}
	if !launch.BranchExists(dir, "feature/x") {
		t.Fatal("branch must stay when deleteBranch is false")
	}
}

func TestStart(t *testing.T) {
	tmuxtest.Server(t)
	agent := tmuxtest.AgentTTY(t)
	dir := t.TempDir()
	name := tmux.Name("", tmux.NameVars{Project: "backend", Branch: "main"})
	first, err := launch.Start(launch.Options{Name: name, Key: "claude:abc", Cwd: dir, Argv: []string{agent, "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "nit-backend-main" || first.Key != "claude:abc" || first.Cwd != dir {
		t.Fatalf("first = %+v", first)
	}
	// Тот же ключ: агент работает, второй не запускается.
	again, err := launch.Start(launch.Options{Name: name, Key: "claude:abc", Cwd: dir, Argv: []string{agent, "two"}})
	if err != nil || again.Name != first.Name || again.PID != first.PID {
		t.Fatalf("again = %+v, %v", again, err)
	}
	// Другой агент в той же ветке: имя занято, нужен суффикс.
	other, err := launch.Start(launch.Options{Name: name, Key: "codex:x", Cwd: dir, Argv: []string{agent}})
	if err != nil || other.Name != "nit-backend-main-2" {
		t.Fatalf("other = %+v, %v", other, err)
	}
	// Агент вышел: терминал открывается заново под тем же именем.
	tmux.SendText(first.Name, "quit")
	tmux.SendKeys(first.Name, "Enter")
	tmuxtest.Wait(t, "agent exited", func() bool {
		p, _, _ := tmux.Get(first.Name)
		return p.Dead
	})
	restarted, err := launch.Start(launch.Options{Name: "ignored", Key: "claude:abc", Cwd: dir, Argv: []string{agent, "three"}})
	if err != nil || restarted.Name != first.Name || restarted.Dead {
		t.Fatalf("restarted = %+v, %v", restarted, err)
	}
	tmuxtest.Screen(t, first.Name, "fake agent: three")

	if _, err := launch.Start(launch.Options{Name: "x", Cwd: filepath.Join(dir, "gone"), Argv: []string{agent}}); err == nil {
		t.Fatal("missing folder must fail")
	}
}

func TestSuggestBranch(t *testing.T) {
	cases := map[string]string{
		"BACKEND-10312: падает импорт":                           "BACKEND-10312",
		"Посмотри задачу backend-1 и BACKEND-77":                 "BACKEND-77",
		"Почини падение тестов в каталоге":                       "pochini-padenie-testov-v-kataloge",
		"Fix the UTF-8 decoding of journals":                     "fix-the-utf-8-decoding",
		"Add a very long description that goes on and on":        "add-a-very-long-description",
		"Rewrite authentication middleware configuration loader": "rewrite-authentication-middleware",
		"":                 "",
		"???":              "",
		"Щука съела ёжика": "schuka-sela-ezhika",
	}
	for prompt, want := range cases {
		if got := launch.SuggestBranch(prompt); got != want {
			t.Errorf("SuggestBranch(%q) = %q, want %q", prompt, got, want)
		}
	}
	if got := launch.SuggestBranch(strings.Repeat("a", 60)); len(got) != 40 {
		t.Errorf("long word: %q", got)
	}
}
