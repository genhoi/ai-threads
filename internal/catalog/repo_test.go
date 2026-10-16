package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoRoot(t *testing.T) {
	root := t.TempDir()
	repo := mkdir(t, filepath.Join(root, "backend"))
	mkdir(t, filepath.Join(repo, ".git", "worktrees", "BACKEND-1"))
	sub := mkdir(t, filepath.Join(repo, "internal", "api"))
	worktree := mkdir(t, filepath.Join(root, "worktrees", "backend", "BACKEND-1"))
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(repo, ".git", "worktrees", "BACKEND-1")+"\n")
	relative := mkdir(t, filepath.Join(repo, "copies", "second"))
	writeFile(t, filepath.Join(relative, ".git"), "gitdir: ../../.git/worktrees/second\n")
	module := mkdir(t, filepath.Join(repo, "vendor", "lib"))
	writeFile(t, filepath.Join(module, ".git"), "gitdir: ../../.git/modules/lib\n")
	bare := mkdir(t, filepath.Join(root, "bare.git"))
	bareCopy := mkdir(t, filepath.Join(root, "bare-copy"))
	writeFile(t, filepath.Join(bareCopy, ".git"), "gitdir: "+filepath.Join(bare, "worktrees", "bare-copy"))
	outside := mkdir(t, filepath.Join(root, "notes"))
	cases := map[string]string{
		repo:                           repo,
		sub:                            repo,
		worktree:                       repo,
		filepath.Join(worktree, "."):   repo,
		relative:                       repo,
		module:                         module,
		bareCopy:                       bare,
		outside:                        "",
		filepath.Join(root, "missing"): "",
		"":                             "",
	}
	for dir, want := range cases {
		if got := RepoRoot(dir); got != want {
			t.Errorf("RepoRoot(%q) = %q, want %q", dir, got, want)
		}
	}
	s := model.Session{Cwd: worktree, Repo: RepoRoot(worktree)}
	if s.Project() != "backend" {
		t.Errorf("project = %q", s.Project())
	}
}

func TestRepoIsKeptForRemovedWorktree(t *testing.T) {
	root := t.TempDir()
	repo := mkdir(t, filepath.Join(root, "backend"))
	mkdir(t, filepath.Join(repo, ".git"))
	worktree := mkdir(t, filepath.Join(root, "copies", "BACKEND-1"))
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(repo, ".git", "worktrees", "BACKEND-1"))
	r := newRepos()
	if got := r.root(worktree); got != repo {
		t.Fatalf("root = %q", got)
	}
	// Копию удалили после завершения сессии: проект остаётся прежним.
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	r.checked[worktree] = time.Time{}
	if got := r.root(worktree); got != repo {
		t.Fatalf("root = %q", got)
	}
	// Папка есть, но больше не репозиторий — корня нет.
	mkdir(t, worktree)
	r.checked[worktree] = time.Time{}
	if got := r.root(worktree); got != "" {
		t.Fatalf("root = %q", got)
	}
}

func TestTempAndService(t *testing.T) {
	t.Setenv("HOME", "/home/user")
	dirs := []string{"/tmp", "/var/tmp/", "~/.local"}
	cases := map[string]bool{
		"/tmp":                        true,
		"/tmp/x/y":                    true,
		"/tmpfoo":                     false,
		"/var/tmp/":                   true,
		"/home/user/.local/share/app": true,
		"/home/user/projects/app":     false,
		"":                            false,
	}
	for cwd, want := range cases {
		if got := Temp(model.Session{Cwd: cwd}, dirs); got != want {
			t.Errorf("Temp(%q) = %v", cwd, got)
		}
	}
	if !Temp(model.Session{Cwd: "/anything"}, []string{"/"}) {
		t.Error("корень включает всё")
	}
	if !Service(model.Session{Cwd: "/home/user/app", Auto: true}, dirs) || !Service(model.Session{Cwd: "/tmp/a"}, dirs) ||
		Service(model.Session{Cwd: "/home/user/app"}, dirs) {
		t.Error("service")
	}
}
