// Package launch — запуск сессии агента: копия репозитория (git worktree), терминал в tmux,
// имя ветки по запросу и проверки перед удалением копии.
//
// Команду агента собирает пакет агента, здесь она только запускается.
package launch

import (
	"errors"
	"regexp"
	"strings"

	"github.com/genhoi/ai-threads/internal/tmux"
)

// Options — что запустить в терминале.
type Options struct {
	Name       string   // желаемое имя сессии tmux, обычно tmux.Name(шаблон, поля); занятое получит -2, -3…
	Key        string   // ключ сессии агента, если уже известен
	Cwd        string   // папка проекта или копии
	Argv       []string // команда агента
	Cols, Rows int      // начальный размер окна; 0 — по умолчанию
	Env        []string // дополнительные переменные окружения агента, KEY=VALUE
}

// Start открывает терминал с агентом и возвращает его: имя сессии tmux, ключ, время запуска
// (после него появится журнал новой сессии) и папку. Если терминал с тем же ключом уже открыт
// и агент в нём работает, второй агент не запускается — возвращается этот терминал. Если агент
// в нём вышел, терминал открывается заново под тем же именем.
func Start(o Options) (tmux.Pane, error) {
	if !tmux.Available() {
		return tmux.Pane{}, tmux.ErrNoTmux
	}
	if o.Name == "" || len(o.Argv) == 0 {
		return tmux.Pane{}, errors.New("launch: name and command are required")
	}
	if o.Key != "" {
		panes, err := tmux.List()
		if err != nil {
			return tmux.Pane{}, err
		}
		for _, p := range panes {
			if p.Key != o.Key {
				continue
			}
			if !p.Dead {
				return p, nil
			}
			if err := tmux.Close(p.Name); err != nil {
				return tmux.Pane{}, err
			}
			o.Name = p.Name
		}
	}
	name, err := tmux.Unique(o.Name)
	if err != nil {
		return tmux.Pane{}, err
	}
	return tmux.Open(tmux.OpenOptions{Name: name, Key: o.Key, Cwd: o.Cwd, Argv: o.Argv,
		Cols: o.Cols, Rows: o.Rows, Env: o.Env})
}

var (
	// Номер задачи из трекера: BACKEND-10312, AB-1.
	ticket = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	// То, что похоже на номер задачи, но им не бывает.
	notTicket = regexp.MustCompile(`^(UTF|ISO|SHA|MD|RFC|AES|RSA|TLS|SSL|HTTP|IPV|X)[0-9]*-`)
	word      = regexp.MustCompile(`[a-z0-9]+`)
)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z",
	'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r",
	'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// SuggestBranch предлагает имя ветки по запросу: номер задачи, если он есть (BACKEND-10312),
// иначе первые слова запроса латиницей через «-», не длиннее 40 знаков. Пустой запрос — пустое имя.
func SuggestBranch(prompt string) string {
	for _, t := range ticket.FindAllString(prompt, -1) {
		if !notTicket.MatchString(t) {
			return t
		}
	}
	var b strings.Builder
	for _, r := range strings.ToLower(prompt) {
		if s, ok := translit[r]; ok {
			b.WriteString(s)
		} else {
			b.WriteRune(r)
		}
	}
	words := word.FindAllString(b.String(), -1)
	if len(words) == 0 {
		return ""
	}
	slug := words[0][:min(len(words[0]), 40)]
	for i, w := range words[1:] {
		if i == 4 || len(slug)+1+len(w) > 40 {
			break
		}
		slug += "-" + w
	}
	return slug
}
