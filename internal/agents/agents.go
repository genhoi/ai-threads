// Package agents знает, где каждый агент для программирования хранит журналы сессий, как их
// читать, как запустить агента и продолжить сессию, какие у него модели и что он пишет в реестр
// запущенных сессий.
//
// Агент — значение Agent. Общее у всех (папка, программа, команды) живёт в base, а чтение
// журналов — в jsonlAgent (Claude Code, Codex, Grok, Kimi) и в агентах на SQLite (ZCode,
// Cursor Agent). Настройки приходят готовым Config: файл настроек читает другой пакет.
package agents

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

// IDs — агенты в порядке интерфейса.
var IDs = []string{"codex", "claude", "grok", "kimi", "zcode", "cursor"}

// Config — настройки одного агента. Нулевое значение — всё по умолчанию.
type Config struct {
	Home          string // папка агента; «~/» раскрывается; пусто — ~/.<агент>
	Program       string // путь к программе; пусто — поиск в PATH
	Disabled      bool   // агент выключен в настройках
	SkipApprovals bool   // добавлять флаг «без подтверждений» к запуску и продолжению
}

// Info — постоянные сведения об агенте.
type Info struct {
	ID           string   // claude, codex, …; часть ключа сессии
	Name         string   // Claude Code
	Short        string   // Cl — для значка
	HomeEnv      string   // переменная окружения с папкой агента
	HomeDefault  string   // папка по умолчанию относительно домашней, например .claude
	Programs     []string // имена программы в PATH
	SkipFlag     string   // флаг «без подтверждений»
	Resumable    bool     // сессию можно продолжить из терминала
	Runner       bool     // умеет работать без человека: сводки
	PlainJournal bool     // журнал можно дать модели как есть; иначе переписку надо выгрузить текстом
}

// Agent — один агент: его журналы, команды и модели.
type Agent interface {
	ID() string
	Info() Info
	Home() string
	// HomeSource — откуда взята папка: env, settings или default.
	HomeSource() string
	Enabled() bool
	// Program — путь к программе или пустая строка, если её нет.
	Program() string

	// Discover — файлы, по которым находятся сессии (единицы чтения).
	Discover() []string
	// Scan читает сессии из единиц units. Файлы с прежними размером и временем изменения не
	// открываются: их сессии берутся из c. Записи c о файлах не из units забываются.
	// onUnit, если задан, вызывается после каждой единицы с её сессиями. Repo заполняет каталог.
	Scan(c *Cache, units []string, onUnit func([]model.Session)) []model.Session
	// Transcript — последние сообщения переписки в пределах limitBytes и признак, что
	// показаны не все.
	Transcript(s model.Session, limitBytes int64) ([]model.Message, bool)
	// Excerpt — сообщения начала и конца переписки, для сводки за несколько дней.
	Excerpt(s model.Session) []model.Message
	// RawText — весь текст сессии для полного поиска, вместе с выводом команд.
	RawText(s model.Session) []byte

	// Resume — команда продолжения; nil, если из терминала продолжить нельзя.
	Resume(s model.Session) []string
	// Launch — команда новой сессии в терминале; пустая, если запустить нельзя.
	Launch(o LaunchOptions) Command
	// Models — модели и уровни рассуждений, которые знает CLI. Кэшируется на минуту.
	Models() Models
	// Headless — команда запуска без человека для сводок.
	Headless(o HeadlessOptions) (HeadlessCommand, error)
	// Live — запущенные сейчас сессии по реестру агента; has отбирает известные каталогу.
	Live(has func(key string) bool) []LiveStatus
}

// LaunchOptions — новая сессия в терминале.
type LaunchOptions struct {
	Model     string // пусто — как настроено в CLI
	Effort    string // уровень рассуждений; пусто — как настроено в CLI
	Prompt    string // первый запрос; пусто — без него
	SessionID string // ID новой сессии для агентов, которые его принимают; пусто — новый UUID
}

// Command — команда новой сессии.
type Command struct {
	Argv []string
	// SessionID известен заранее, если агент принимает его в аргументах (Claude Code, Grok).
	// Иначе пусто, и новую сессию надо найти в каталоге.
	SessionID string
	// PromptInArgs — запрос передан в аргументах. Если запрос есть, а флаг false (Kimi),
	// его надо набрать в терминале после запуска.
	PromptInArgs bool
}

// HeadlessOptions — запуск без человека: сводка или ответ в начатую ею сессию.
type HeadlessOptions struct {
	Prompt       string
	Instructions string   // файл инструкции агента: Markdown, возможно с шапкой YAML
	AddDirs      []string // папки, которые агенту можно читать
	OutFile      string   // файл для ответа; нужен Codex
	Session      string   // продолжить эту сессию сообщением Prompt
	Model        string
	Effort       string
}

// HeadlessCommand — команда запуска без человека.
type HeadlessCommand struct {
	Argv    []string
	Stdin   string // пусто — stdin не нужен
	OutFile string // ответ модели придёт в этот файл, а не в поток
}

// Model — модель CLI.
type Model struct {
	ID            string
	Name          string
	Efforts       []string
	DefaultEffort string
}

// Models — что можно выбрать при запуске. Пустые значения — «как настроено в CLI».
type Models struct {
	DefaultModel  string
	DefaultEffort string
	Models        []Model
	Efforts       []string // уровни рассуждений для всех моделей сразу
}

// LiveStatus — запущенная сессия по реестру агента.
type LiveStatus struct {
	Key   string
	State model.State // StateWorking или StateWaiting
	Where string      // terminal или background; подпись ставит интерфейс
	Name  string      // имя, которое человек дал сессии; может быть пустым
	Since time.Time   // с какого момента в этом состоянии; может быть нулевым
	// Guess — реестр знает только, что процесс жив, и State — догадка. Состояние терминала
	// «Нити» такая запись не перебивает: экран точнее.
	Guess bool
}

// spec — чем агенты отличаются, кроме чтения журналов.
type spec struct {
	Info
	fallbacks   []string // пути к программе относительно папки агента
	resume      []string // команда продолжения; "{id}" заменяется на ID
	resumeAuto  []string // для автоматических сессий, если отличается
	sessionFlag string   // флаг, которым новой сессии задаётся ID
	promptArg   bool     // первый запрос можно передать аргументом
	choice      func(model, effort string) []string
	models      func(b *base) Models
	headless    func(program string, o HeadlessOptions) (argv []string, stdin string, err error)
	answerFile  bool // ответ модели без человека приходит в файл, а не в поток
	live        func(b *base, has func(string) bool) []LiveStatus
}

// base — общее для всех агентов: папка, программа, команды, модели, реестр.
type base struct {
	spec spec
	cfg  Config

	mu       sync.Mutex
	modelsAt time.Time
	cached   Models
}

func (b *base) ID() string    { return b.spec.ID }
func (b *base) Info() Info    { return b.spec.Info }
func (b *base) Enabled() bool { return !b.cfg.Disabled }

func (b *base) Home() string {
	if b.spec.HomeEnv != "" {
		if value := os.Getenv(b.spec.HomeEnv); value != "" {
			return value
		}
	}
	if b.cfg.Home != "" {
		return expand(b.cfg.Home)
	}
	return filepath.Join(userHome(), b.spec.HomeDefault)
}

func (b *base) HomeSource() string {
	switch {
	case b.spec.HomeEnv != "" && os.Getenv(b.spec.HomeEnv) != "":
		return "env"
	case b.cfg.Home != "":
		return "settings"
	}
	return "default"
}

func (b *base) Program() string {
	if b.cfg.Program != "" {
		path := expand(b.cfg.Program)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path
		}
		return ""
	}
	for _, name := range b.spec.Programs {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
	}
	for _, relative := range b.spec.fallbacks {
		candidate := filepath.Join(b.Home(), relative)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// command — первое слово команды запуска и продолжения. Имя из PATH короче и понятнее
// человеку; полный путь — только для программы из настроек или из папки агента.
func (b *base) command() string {
	if b.cfg.Program != "" {
		return expand(b.cfg.Program)
	}
	if len(b.spec.Programs) == 0 {
		return ""
	}
	name := b.spec.Programs[0]
	if _, err := exec.LookPath(name); err != nil {
		if found := b.Program(); found != "" {
			return found
		}
	}
	return name
}

func (b *base) Resume(s model.Session) []string {
	template := b.spec.resume
	if s.Auto && b.spec.resumeAuto != nil {
		template = b.spec.resumeAuto
	}
	if template == nil {
		return nil
	}
	argv := make([]string, 0, len(template)+1)
	for i, part := range template {
		switch {
		case i == 0:
			argv = append(argv, b.command())
		case part == "{id}":
			argv = append(argv, s.ID)
		default:
			argv = append(argv, part)
		}
	}
	if b.cfg.SkipApprovals && b.spec.SkipFlag != "" {
		argv = append(argv, b.spec.SkipFlag)
	}
	return argv
}

func (b *base) Launch(o LaunchOptions) Command {
	if len(b.spec.Programs) == 0 {
		return Command{}
	}
	cmd := Command{Argv: []string{b.command()}}
	cmd.Argv = append(cmd.Argv, b.choiceArgs(o.Model, o.Effort)...)
	if b.spec.sessionFlag != "" {
		cmd.SessionID = o.SessionID
		if cmd.SessionID == "" {
			cmd.SessionID = newUUID()
		}
		cmd.Argv = append(cmd.Argv, b.spec.sessionFlag, cmd.SessionID)
	}
	if b.cfg.SkipApprovals && b.spec.SkipFlag != "" {
		cmd.Argv = append(cmd.Argv, b.spec.SkipFlag)
	}
	if o.Prompt != "" && b.spec.promptArg {
		// Запрос, похожий на флаг, CLI принял бы за флаг.
		if strings.HasPrefix(o.Prompt, "-") {
			cmd.Argv = append(cmd.Argv, "--")
		}
		cmd.Argv = append(cmd.Argv, o.Prompt)
		cmd.PromptInArgs = true
	}
	return cmd
}

func (b *base) choiceArgs(model, effort string) []string {
	if b.spec.choice == nil {
		return nil
	}
	return b.spec.choice(model, effort)
}

// modelsTTL — сколько живёт список моделей: Grok спрашивает его у своей программы.
const modelsTTL = time.Minute

func (b *base) Models() Models {
	if b.spec.models == nil {
		return Models{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.modelsAt.IsZero() && time.Since(b.modelsAt) < modelsTTL {
		return b.cached
	}
	b.cached = b.spec.models(b)
	b.modelsAt = time.Now()
	return b.cached
}

func (b *base) Headless(o HeadlessOptions) (HeadlessCommand, error) {
	if !b.spec.Runner || b.spec.headless == nil {
		return HeadlessCommand{}, fmt.Errorf("%s can't run without a person", b.spec.Name)
	}
	program := b.Program()
	if program == "" {
		return HeadlessCommand{}, fmt.Errorf("%s is not installed", b.spec.Name)
	}
	if b.spec.answerFile && o.OutFile == "" {
		return HeadlessCommand{}, fmt.Errorf("%s needs a file for the answer", b.spec.Name)
	}
	argv, stdin, err := b.spec.headless(program, o)
	if err != nil {
		return HeadlessCommand{}, err
	}
	// Модель и уровень рассуждений — сразу после имени программы: так их понимают все CLI.
	argv = slices.Concat(argv[:1], b.choiceArgs(o.Model, o.Effort), argv[1:])
	cmd := HeadlessCommand{Argv: argv, Stdin: stdin}
	if b.spec.answerFile {
		cmd.OutFile = o.OutFile
	}
	return cmd, nil
}

func (b *base) Live(has func(string) bool) []LiveStatus {
	if b.spec.live == nil {
		return nil
	}
	return b.spec.live(b, has)
}

// New — агент с настройками c; nil для неизвестного id.
func New(id string, c Config) Agent {
	switch id {
	case "claude":
		return newClaude(c)
	case "codex":
		return newCodex(c)
	case "grok":
		return newGrok(c)
	case "kimi":
		return newKimi(c)
	case "zcode":
		return newZCode(c)
	case "cursor":
		return newCursor(c)
	}
	return nil
}

// All — все агенты в порядке интерфейса; настройки берутся из configs по id.
func All(configs map[string]Config) []Agent {
	list := make([]Agent, 0, len(IDs))
	for _, id := range IDs {
		list = append(list, New(id, configs[id]))
	}
	return list
}

// Active — включённые агенты, у которых есть папка.
func Active(list []Agent) []Agent {
	var result []Agent
	for _, a := range list {
		if a.Enabled() && isDir(a.Home()) {
			result = append(result, a)
		}
	}
	return result
}

// LiveAll — запущенные сессии всех агентов по ключу. Если реестр называет сессию дважды,
// остаётся более свежая запись.
func LiveAll(list []Agent, has func(key string) bool) map[string]LiveStatus {
	found := map[string]LiveStatus{}
	for _, a := range list {
		for _, status := range a.Live(has) {
			if old, ok := found[status.Key]; ok && status.Since.Before(old.Since) {
				continue
			}
			found[status.Key] = status
		}
	}
	return found
}

// ValidChoice — модель или уровень рассуждений из запроса годятся для командной строки.
// Пустая строка — «как настроено в CLI».
func ValidChoice(value string) bool {
	if len(value) > 100 {
		return false
	}
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/@+-", r)
		if !ok {
			return false
		}
	}
	return true
}

// instructionsText — инструкция агента без шапки YAML: для CLI, которым файл агента не передать.
func instructionsText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	if strings.HasPrefix(text, "---") {
		if end := strings.Index(text[3:], "\n---"); end >= 0 {
			text = text[3+end+4:]
		}
	}
	return strings.TrimSpace(text), nil
}

// shellPrompt — инструкция и запрос одним текстом. Без файла инструкции — только запрос.
func shellPrompt(instructions, prompt string) (string, error) {
	if instructions == "" {
		return prompt, nil
	}
	text, err := instructionsText(instructions)
	if err != nil {
		return "", err
	}
	return text + "\n\n---\n\n" + prompt, nil
}

func addDirArgs(dirs []string) []string {
	var argv []string
	for _, dir := range dirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return home
}

// expand раскрывает «~» и «~/…» в начале пути.
func expand(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(userHome(), path[1:])
	}
	return path
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// newUUID — случайный UUID версии 4 для новой сессии.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
