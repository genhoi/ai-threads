// Package tmux — терминалы агентов: сессии tmux на своём сервере «Нити».
//
// Агент живёт в сессии tmux, а не в процессе «Нити»: закрытая вкладка браузера, перезапуск «Нити»
// или оборванная связь его не останавливают. Если «Нить» работает службой systemd, сервер tmux
// запускается в отдельной группе процессов, иначе systemctl restart завершил бы его вместе со
// службой. Сервер tmux свой (сокет ai-threads, без ~/.tmux.conf),
// поэтому настройки ниже не задевают tmux человека: префикса нет и Ctrl+B уходит агенту, строки
// состояния нет, колесо мыши листает историю, а после выхода агента окно остаётся, чтобы был виден
// код выхода. У каждой сессии «Нити» есть метка @nit_key — ключ сессии агента.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Socket — имя сокета сервера tmux «Нити», то же, что в первой версии.
const Socket = "ai-threads"

// SocketEnv — путь к сокету вместо Socket: тесты запускают свой сервер tmux и убирают его за собой.
const SocketEnv = "AI_THREADS_TMUX_SOCKET"

// KeyOption — метка сессии tmux с ключом сессии агента.
const KeyOption = "@nit_key"

var (
	ErrNoTmux   = errors.New("tmux is not installed")
	ErrNotFound = errors.New("tmux session not found")
)

// options — настройки сервера, как в первой версии.
// -q: настройки, которых нет в старом tmux, пропускаются молча, а не ломают запуск.
var options = [][]string{
	{"-sq", "escape-time", "0"},
	// Копирование мышью уходит в буфер обмена браузера через OSC 52.
	{"-sq", "set-clipboard", "on"},
	// Свой номер в списке, а не -a: настройки ставятся при каждом запуске агента, и -a
	// добавлял бы в список ещё одну такую же запись.
	{"-sq", "terminal-features[90]", "xterm-256color:RGB:clipboard"},
	{"-gq", "prefix", "None"},
	{"-gq", "prefix2", "None"},
	{"-gq", "status", "off"},
	{"-gq", "mouse", "on"},
	{"-gq", "history-limit", "50000"},
	{"-gq", "remain-on-exit", "on"},
	{"-gq", "remain-on-exit-format", ""},
	{"-gq", "default-terminal", "tmux-256color"},
	{"-gq", "window-size", "latest"},
	{"-gq", "focus-events", "on"},
}

// Pane — открытый терминал: сессия tmux с одной панелью.
type Pane struct {
	Name     string    // имя сессии tmux
	Key      string    // ключ сессии агента из @nit_key; пусто у сессий, открытых не «Нитью»
	Dead     bool      // агент вышел, окно осталось
	ExitCode int       // при Dead: код выхода; убитый сигналом — 128 + номер сигнала; -1 — неизвестен
	DiedAt   time.Time // при Dead: когда агент вышел
	Activity time.Time // последний вывод в окне, с точностью до секунды
	Created  time.Time
	PID      int    // процесс в панели: оболочка, которая запустила агента
	Cwd      string // папка, в которой открыт терминал
}

// Available — установлен ли tmux.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func base() []string {
	if path := os.Getenv(SocketEnv); path != "" {
		return []string{"-S", path}
	}
	return []string{"-L", Socket}
}

func env() []string {
	// Внутри tmux человека attach отказывается работать, пока задан TMUX.
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "TMUX" || name == "TMUX_PANE" || name == "TERM" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color")
}

// serverScope — начало команды, которая запускает сервер tmux в своей группе процессов systemd,
// или nil. Сервер tmux, запущенный из службы systemd, попадает в группу процессов службы, и
// systemctl restart или stop завершает его вместе со всеми агентами. systemd-run --scope даёт ему
// свою группу. Если systemd-run не работает, сервер запускается как обычно.
var serverScope = sync.OnceValue(func() []string {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || !inService(string(raw)) {
		return nil
	}
	scope := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect"}
	if out, err := exec.Command(scope[0], append(scope[1:], "true")...).CombinedOutput(); err != nil {
		log.Printf("tmux: systemd-run does not work, restarting the service will stop the agents: %v %s", err, bytes.TrimSpace(out))
		return nil
	}
	return append(scope, "--description=Nit: tmux server")
})

// inService — работает ли процесс службой systemd: его группа процессов в /proc/self/cgroup —
// юнит .service. В терминале это session-N.scope, в панели tmux — tmux-spawn-….scope.
func inService(cgroup string) bool {
	for line := range strings.Lines(cgroup) {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) == 3 && (parts[1] == "" || parts[1] == "name=systemd") && strings.HasSuffix(parts[2], ".service") {
			return true
		}
	}
	return false
}

func run(args ...string) (string, error) { return runIn(nil, args...) }

// runIn — run, но перед tmux стоит prefix: так сервер tmux запускается через systemd-run.
func runIn(prefix []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// -f /dev/null: сервер «Нити» не читает ~/.tmux.conf человека.
	argv := slices.Concat(prefix, []string{"tmux"}, base(), []string{"-f", os.DevNull}, args)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return "", ErrNoTmux
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return string(out), nil
}

// arg — значение для командной строки tmux. tmux режет команды по аргументу, который кончается
// на «;», поэтому такая точка с запятой экранируется.
func arg(value string) string {
	if strings.HasSuffix(value, ";") {
		return value[:len(value)-1] + `\;`
	}
	return value
}

// target — точная цель: имя сессии целиком, а не префикс, и её текущая панель.
func target(name string) string { return "=" + name + ":" }

// OpenOptions — что запустить в новом терминале.
type OpenOptions struct {
	Name       string   // имя сессии tmux; см. Name и Unique
	Cwd        string   // папка агента
	Argv       []string // команда агента
	Cols, Rows int      // начальный размер окна; 0 — 120×32
	Key        string   // ключ сессии агента для @nit_key; можно задать позже через SetKey
	Env        []string // дополнительные переменные окружения агента, KEY=VALUE
}

// Open запускает агента в новой сессии tmux.
func Open(o OpenOptions) (Pane, error) {
	if o.Name == "" || len(o.Argv) == 0 {
		return Pane{}, errors.New("tmux: name and command are required")
	}
	if info, err := os.Stat(o.Cwd); err != nil || !info.IsDir() {
		return Pane{}, fmt.Errorf("folder %s does not exist", o.Cwd)
	}
	// Через systemd-run отсутствие tmux не распознать по ошибке запуска
	if !Available() {
		return Pane{}, ErrNoTmux
	}
	cols, rows := Clamp(o.Cols, 20, 500, 120), Clamp(o.Rows, 5, 200, 32)
	// Агент запускается через интерактивную оболочку человека: так подхватываются PATH и алиасы
	// из ~/.bashrc, как в обычной вкладке терминала. Код выхода оболочки — код выхода агента.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	args := []string{"start-server"}
	for _, opt := range options {
		args = append(append(args, ";", "set-option"), opt...)
	}
	args = append(args, ";", "new-session", "-d", "-s", arg(o.Name),
		"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), "-c", arg(o.Cwd), "-e", "COLORTERM=truecolor")
	for _, kv := range o.Env {
		args = append(args, "-e", arg(kv))
	}
	args = append(args, "--", shell, "-ic", arg(ShellJoin(o.Argv)))
	if o.Key != "" {
		args = append(args, ";", "set-option", "-t", target(o.Name), KeyOption, arg(o.Key))
	}
	// Только эта команда запускает сервер tmux, если его ещё нет
	_, err := runIn(serverScope(), args...)
	if err != nil {
		return Pane{}, err
	}
	pane, ok, err := Get(o.Name)
	if err != nil {
		return Pane{}, err
	}
	if !ok {
		return Pane{}, fmt.Errorf("tmux: session %s did not start", o.Name)
	}
	return pane, nil
}

// SetKey ставит метку @nit_key: ключ сессии бывает известен только после запуска агента.
func SetKey(name, key string) error {
	_, err := run("set-option", "-t", target(name), KeyOption, arg(key))
	return err
}

// Close закрывает терминал: tmux завершает агента вместе с сессией. Закрытого терминала нет — не ошибка.
func Close(name string) error {
	if !Exists(name) {
		return nil
	}
	_, err := run("kill-session", "-t", "="+name)
	return err
}

// Exists — есть ли сессия tmux с таким именем.
func Exists(name string) bool {
	_, err := run("has-session", "-t", "="+name)
	return err == nil
}

const listFormat = "#{session_name}\t#{@nit_key}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_dead_signal}\t" +
	"#{pane_dead_time}\t#{window_activity}\t#{session_created}\t#{pane_pid}\t#{pane_start_path}"

// List — все сессии на сервере «Нити». Сервер не запущен — пустой список.
func List() ([]Pane, error) {
	panes, pending, err := list()
	if err != nil || !pending {
		return panes, err
	}
	// Панель мертва, а кода выхода нет. Так бывает, если агент вышел сразу после запуска
	// сервера tmux: tmux пропускает SIGCHLD и не забирает процесс. Любой другой завершённый
	// процесс сервера заставляет его забрать всех, поэтому run-shell true и список заново.
	// Если и после этого кода нет, остаётся -1.
	_, _ = run("run-shell", "true")
	panes, _, err = list()
	return panes, err
}

func list() (panes []Pane, pending bool, err error) {
	out, err := run("list-panes", "-a", "-F", listFormat)
	if errors.Is(err, ErrNoTmux) {
		return nil, false, err
	}
	if err != nil {
		return nil, false, nil // сервер tmux не запущен: терминалов нет
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 10)
		if len(f) != 10 || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		p := Pane{Name: f[0], Key: f[1], Dead: f[2] == "1", Activity: unix(f[6]), Created: unix(f[7]), Cwd: f[9]}
		p.PID, _ = strconv.Atoi(f[8])
		if p.Dead {
			p.DiedAt = unix(f[5])
			if code, err := strconv.Atoi(f[3]); err == nil {
				p.ExitCode = code
			} else if sig, err := strconv.Atoi(f[4]); err == nil {
				p.ExitCode = 128 + sig
			} else {
				// Терминал панели уже закрыт, а код выхода tmux ещё не узнал.
				p.ExitCode, pending = -1, true
			}
		}
		panes = append(panes, p)
	}
	return panes, pending, nil
}

// Get — сессия по имени.
func Get(name string) (Pane, bool, error) {
	panes, err := List()
	if err != nil {
		return Pane{}, false, err
	}
	for _, p := range panes {
		if p.Name == name {
			return p, true, nil
		}
	}
	return Pane{}, false, nil
}

func unix(s string) time.Time {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// Capture — текст экрана: history строк истории над экраном и сам экран. Перенесённые строки
// склеены (-J), чтобы телефон переносил их по своей ширине; пробелы в конце строк и пустые строки
// внизу убраны.
func Capture(name string, history int) (string, error) {
	args := []string{"capture-pane", "-p", "-J", "-t", target(name)}
	if history > 0 {
		args = append(args, "-S", strconv.Itoa(-history))
	}
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

// SendText вводит текст в терминал как есть, без Enter. Многострочный текст вставляется
// как из буфера обмена (bracketed paste): иначе каждый перевод строки отправил бы ввод агенту.
func SendText(name, text string) error {
	if text == "" {
		return nil
	}
	if !strings.ContainsAny(text, "\r\n") {
		_, err := run("send-keys", "-l", "-t", target(name), "--", arg(text))
		return err
	}
	buffer := "nit-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err := run("set-buffer", "-b", buffer, "--", arg(text), ";",
		"paste-buffer", "-p", "-d", "-b", buffer, "-t", target(name))
	return err
}

// keyName — клавиши, которые можно нажать с телефона: ответить на запрос разрешения
// и походить по меню любого агента.
var keyName = regexp.MustCompile(`^(Enter|Escape|Tab|BTab|Space|BSpace|Up|Down|Left|Right|Home|End|PageUp|PageDown|[0-9]|[yn]|C-[a-z]|F([1-9]|1[0-2]))$`)

// SendKeys нажимает клавиши по именам tmux: Enter, Escape, C-c, Up, Down, Tab, цифры.
func SendKeys(name string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := []string{"send-keys", "-t", target(name)}
	for _, k := range keys {
		if !keyName.MatchString(k) {
			return fmt.Errorf("tmux: unknown key %q", k)
		}
		args = append(args, k)
	}
	_, err := run(args...)
	return err
}

// AttachCommand — команда, которая подключит к тому же агенту любой другой терминал.
func AttachCommand(name string) string {
	return ShellJoin(append(append([]string{"tmux"}, base()...), "attach-session", "-t", "="+name))
}

// Clamp — число в пределах [low, high]; ноль и меньше — def.
func Clamp(value, low, high, def int) int {
	if value <= 0 {
		return def
	}
	return max(low, min(high, value))
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ShellJoin склеивает argv в строку для оболочки. Простые слова не берутся в кавычки:
// так первое слово остаётся доступным для алиасов из ~/.bashrc.
func ShellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if plainWord.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}
