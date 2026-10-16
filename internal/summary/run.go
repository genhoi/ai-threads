package summary

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
)

const (
	// runsKeep — столько живут рабочие папки запусков: в них остаются запрос, файл со списком
	// сессий и ответ, по ним видно, что агент получил.
	runsKeep = 7 * 24 * time.Hour
	// tailChars — длина хвоста вывода агента в сообщении об ошибке.
	tailChars = 1200
	// waitDelay — столько ждать закрытия вывода после выхода агента: его потомки могли
	// унести поток с собой.
	waitDelay = 2 * time.Second
)

// Run — кто и как пишет сводку.
type Run struct {
	Runner  agents.Agent // агент без человека: kimi, claude, codex или grok
	Model   string       // пусто — как настроено в CLI
	Effort  string
	Timeout time.Duration // 0 — без предела
	Lang    string        // язык ответа и сообщений: ru или en
	Dir     string        // папка для рабочих папок запусков
}

func (r Run) name() string { return r.Runner.Info().Name }

// workdir — новая рабочая папка запуска. Заодно удаляются папки старше недели.
func (r Run) workdir(kind string) (string, error) {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return "", &Error{Text: text(r.Lang, "workdir", "", err.Error())}
	}
	if entries, err := os.ReadDir(r.Dir); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && e.IsDir() && time.Since(info.ModTime()) > runsKeep {
				_ = os.RemoveAll(filepath.Join(r.Dir, e.Name()))
			}
		}
	}
	dir, err := os.MkdirTemp(r.Dir, time.Now().Format("20060102-150405-")+kind+"-")
	if err != nil {
		return "", &Error{Text: text(r.Lang, "workdir", "", err.Error())}
	}
	return dir, nil
}

// exec запускает агента в папке dir с инструкцией и запросом и возвращает его ответ.
// Ход работы уходит в job. Отменённый ctx и предел времени убивают агента со всеми потомками.
func (r Run) exec(ctx context.Context, job *Job, dir, file, instructions, prompt string, addDirs []string) (string, error) {
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(instructions), 0o600); err != nil {
		return "", &Error{Text: text(r.Lang, "workdir", "", err.Error())}
	}
	cmd, err := r.Runner.Headless(agents.HeadlessOptions{Prompt: prompt, Instructions: path, AddDirs: addDirs,
		OutFile: filepath.Join(dir, "answer.txt"), Model: r.Model, Effort: r.Effort})
	if err != nil {
		return "", &Error{Text: text(r.Lang, "start_failed", r.name(), err.Error())}
	}
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if r.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.Timeout)
	}
	defer cancel()

	st := &stream{agent: r.Runner.ID(), job: job}
	stderr, stdout := &tail{}, &tail{}
	out := &lines{fn: st.line, tail: stdout}
	c := exec.CommandContext(runCtx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Dir = dir
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	c.Stdout, c.Stderr = out, stderr
	// Своя группа процессов: отмена убивает и потомков агента.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = waitDelay
	err = c.Run()
	out.flush()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	output := stderr.text()
	if output == "" {
		output = stdout.text()
	}
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case runCtx.Err() != nil:
		return "", &Error{Text: text(r.Lang, "timeout", r.name(), duration(r.Lang, r.Timeout)), Output: output}
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", &Error{Text: text(r.Lang, "exit_code", r.name(), strconv.Itoa(exit.ExitCode())), Output: output}
		}
		return "", &Error{Text: text(r.Lang, "start_failed", r.name(), err.Error()), Output: output}
	}
	answer := st.answer()
	if cmd.OutFile != "" {
		if data, err := os.ReadFile(cmd.OutFile); err == nil && strings.TrimSpace(string(data)) != "" {
			answer = string(data)
		}
	}
	if strings.TrimSpace(answer) == "" {
		return "", &Error{Text: text(r.Lang, "empty_answer", r.name(), ""), Output: output}
	}
	_ = os.WriteFile(filepath.Join(dir, "answer.txt"), []byte(answer), 0o600)
	return answer, nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// tail — последние байты вывода процесса.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 64<<10 {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-16<<10:]...)
	}
	return len(p), nil
}

// text — хвост без цветовых кодов, не длиннее tailChars символов.
func (t *tail) text() string {
	t.mu.Lock()
	raw := string(t.buf)
	t.mu.Unlock()
	raw = strings.ToValidUTF8(ansi.ReplaceAllString(raw, ""), "")
	return strings.TrimSpace(clipTail(strings.ReplaceAll(raw, "\r", ""), tailChars))
}
