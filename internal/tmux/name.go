package tmux

import (
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"
)

const (
	// DefaultPattern — имя сессии tmux по умолчанию: видно, какой проект и ветка.
	DefaultPattern = "nit-{project}-{branch}"
	// FallbackPattern — для сессии без ветки, например вне git.
	FallbackPattern = "nit-{agent}-{id}"
	maxName         = 64
)

// NameVars — из чего складывается имя сессии tmux.
type NameVars struct {
	Project, Branch, Agent, ID string
}

// Name — имя сессии tmux по шаблону с полями {project}, {branch}, {agent}, {id}.
// Если поле из шаблона пустое, берётся запасной шаблон FallbackPattern. Имя очищено через
// Sanitize, но не проверено на занятость: это делает Unique.
func Name(pattern string, v NameVars) string {
	if pattern == "" {
		pattern = DefaultPattern
	}
	name, full := expand(pattern, v)
	if !full {
		name, _ = expand(FallbackPattern, v)
	}
	return Sanitize(name)
}

func expand(pattern string, v NameVars) (string, bool) {
	full := true
	for _, f := range []struct{ field, value string }{
		{"{project}", v.Project}, {"{branch}", v.Branch}, {"{agent}", v.Agent}, {"{id}", v.ID},
	} {
		if strings.Contains(pattern, f.field) {
			full = full && f.value != ""
			pattern = strings.ReplaceAll(pattern, f.field, f.value)
		}
	}
	return pattern, full
}

// Sanitize делает из строки имя, которое tmux примет как есть. Точку и двоеточие tmux молча
// заменяет на «_», и «a.b» совпало бы с «a_b», поэтому к таким именам добавляется хеш исходной
// строки, как в первой версии. Остальные знаки, кроме букв, цифр, «-» и «_», становятся «-»:
// так имя без кавычек годится и для командной строки.
func Sanitize(raw string) string {
	var b strings.Builder
	hashed := false
	dash := false
	for _, r := range raw {
		switch {
		case r == '.' || r == ':':
			hashed = true
			b.WriteRune('_')
			dash = false
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			if r == '-' && dash {
				continue
			}
			b.WriteRune(r)
			dash = r == '-'
		default:
			if !dash {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if r := []rune(name); len(r) > maxName {
		name = strings.TrimRight(string(r[:maxName]), "-")
	}
	if name == "" {
		name = "nit"
	}
	if hashed {
		sum := sha1.Sum([]byte(raw))
		name += "-" + hex.EncodeToString(sum[:])[:6]
	}
	return name
}

// Unique — имя, которого ещё нет на сервере: name, затем name-2, name-3 и так далее.
func Unique(name string) (string, error) {
	panes, err := List()
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, p := range panes {
		taken[p.Name] = true
	}
	candidate := name
	for i := 2; taken[candidate]; i++ {
		candidate = name + "-" + strconv.Itoa(i)
	}
	return candidate, nil
}
