package settings

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Поля первой версии, которых больше нет. Файл с ними остаётся рабочим: поля отбрасываются
// и пропадут из файла при следующем сохранении. search — поиск агентом, во второй версии его нет.
var retired = []string{"open_with", "tmux_session", "search"}

// Parse проверяет настройки в JSON и накладывает их на умолчания. Значение null — умолчание.
// При ошибках возвращает Errors и настройки, в которых верные поля применены, а неверные
// остались по умолчанию.
func Parse(raw []byte) (Settings, error) {
	s := Defaults()
	p := parser{s: &s}
	var top map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&top); err != nil || top == nil {
		detail := "null"
		if err != nil {
			detail = err.Error()
		}
		return s, Errors{{Code: "json", Value: detail}}
	}
	for _, name := range sortedKeys(top) {
		value := top[name]
		if value == nil {
			continue
		}
		switch name {
		case "agents":
			p.agents(value)
		case "summary":
			p.runner("summary", value, &s.Summary)
		case "digest":
			p.runner("digest", value, &s.Digest)
		case "language":
			if v, ok := value.(string); ok && slices.Contains(Languages, v) {
				s.Language = v
			} else {
				p.fail("language", "one_of", strings.Join(Languages, ", "))
			}
		case "service":
			p.service(value)
		case "temp_dirs":
			// Первая версия держала временные папки на верхнем уровне; service.temp_dirs важнее.
			if svc, ok := top["service"].(map[string]any); !ok || svc["temp_dirs"] == nil {
				p.dirs("temp_dirs", value, &s.Service.TempDirs)
			}
		case "tmux":
			p.tmux(value)
		case "remote":
			p.remote(value)
		default:
			if !slices.Contains(retired, name) {
				p.fail(name, "unknown", "")
			}
		}
	}
	if len(p.errs) > 0 {
		return s, p.errs
	}
	return s, nil
}

type parser struct {
	s    *Settings
	errs Errors
}

func (p *parser) fail(field, code, value string) {
	p.errs = append(p.errs, FieldError{Field: field, Code: code, Value: value})
}

func (p *parser) object(field string, value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		p.fail(field, "object", "")
	}
	return m, ok
}

// text — строка до limit символов в одну строку, без пробелов по краям.
func (p *parser) text(field string, value any, limit int, required bool) (string, bool) {
	v, ok := value.(string)
	v = strings.TrimSpace(v)
	if !ok || utf8.RuneCountInString(v) > limit || strings.ContainsFunc(v, unicode.IsControl) || (required && v == "") {
		code := "string"
		if required {
			code = "required"
		}
		p.fail(field, code, strconv.Itoa(limit))
		return "", false
	}
	return v, true
}

func (p *parser) agents(value any) {
	m, ok := p.object("agents", value)
	if !ok {
		return
	}
	for _, id := range sortedKeys(m) {
		field := "agents." + id
		if !slices.Contains(Agents, id) {
			p.fail(field, "unknown_agent", "")
			continue
		}
		if m[id] == nil {
			continue
		}
		values, ok := p.object(field, m[id])
		if !ok {
			continue
		}
		a := p.s.Agent(id)
		for _, name := range sortedKeys(values) {
			f, v := field+"."+name, values[name]
			if v == nil {
				continue
			}
			switch name {
			case "enabled", "skip_approvals":
				b, ok := v.(bool)
				if !ok {
					p.fail(f, "bool", "")
				} else if name == "enabled" {
					a.Enabled = b
				} else {
					a.SkipApprovals = b
				}
			case "home":
				if text, ok := p.text(f, v, 1000, false); ok {
					a.Home = text
				}
			case "program":
				if text, ok := p.text(f, v, 1000, false); ok {
					a.Program = text
				}
			case "flags":
				if text, ok := p.text(f, v, 1000, false); ok {
					if _, err := SplitFlags(text); err != nil {
						p.fail(f, "quote", "")
					} else {
						a.Flags = text
					}
				}
			default:
				p.fail(f, "unknown", "")
			}
		}
		p.s.Agents[id] = a
	}
}

func (p *parser) runner(kind string, value any, r *Runner) {
	m, ok := p.object(kind, value)
	if !ok {
		return
	}
	for _, name := range sortedKeys(m) {
		f, v := kind+"."+name, m[name]
		if v == nil {
			continue
		}
		switch name {
		case "agent":
			if a, ok := v.(string); ok && (a == "auto" || slices.Contains(Runners, a)) {
				r.Agent = a
			} else {
				p.fail(f, "runner", strings.Join(Runners, ", "))
			}
		case "minutes":
			n, ok := number(v)
			if !ok || n != math.Trunc(n) || n < 1 || n > 120 {
				p.fail(f, "minutes", "")
			} else {
				r.Minutes = int(n)
			}
		case "timeout":
			// Секунды первой версии; если задано и minutes, timeout не нужен.
			if m["minutes"] != nil {
				continue
			}
			sec, ok := number(v)
			if !ok || sec < 10 || sec > 7200 {
				p.fail(f, "seconds", "")
			} else {
				r.Minutes = int(math.Ceil(sec / 60))
			}
		default:
			p.fail(f, "unknown", "")
		}
	}
}

func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func (p *parser) service(value any) {
	m, ok := p.object("service", value)
	if !ok {
		return
	}
	for _, name := range sortedKeys(m) {
		f, v := "service."+name, m[name]
		if v == nil {
			continue
		}
		switch name {
		case "auto_commands":
			p.commands(f, v)
		case "temp_dirs":
			p.dirs(f, v, &p.s.Service.TempDirs)
		default:
			p.fail(f, "unknown", "")
		}
	}
}

// list — список строк до 50 штук без повторов. Неверный элемент — ошибка у всего поля,
// значение остаётся по умолчанию.
func (p *parser) list(field string, value any, check func(string) (string, bool)) ([]string, bool) {
	items, ok := value.([]any)
	if !ok || len(items) > 50 {
		p.fail(field, "list", "50")
		return nil, false
	}
	out := []string{}
	good := true
	for _, item := range items {
		v, ok := check(fieldValue(item))
		if !ok {
			good = false
			continue
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out, good
}

// commands — команды автоматических запусков; лишние пробелы внутри схлопываются.
func (p *parser) commands(field string, value any) {
	out, ok := p.list(field, value, func(item string) (string, bool) {
		v, ok := p.text(field, item, 200, true)
		return strings.Join(strings.Fields(v), " "), ok
	})
	if ok {
		p.s.Service.AutoCommands = out
	}
}

func (p *parser) dirs(field string, value any, into *[]string) {
	out, ok := p.list(field, value, func(item string) (string, bool) {
		v, ok := p.text(field, item, 500, true)
		if ok && v != "~" && !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "~/") {
			p.fail(field, "temp_path", v)
			return "", false
		}
		return v, ok
	})
	if ok {
		*into = out
	}
}

// fieldValue — элемент списка как строка; не строка превращается в недопустимый символ,
// чтобы проверка строки его отвергла.
func fieldValue(item any) string {
	if v, ok := item.(string); ok {
		return v
	}
	return "\x00"
}

func (p *parser) tmux(value any) {
	m, ok := p.object("tmux", value)
	if !ok {
		return
	}
	for _, name := range sortedKeys(m) {
		f, v := "tmux."+name, m[name]
		if v == nil {
			continue
		}
		if name != "session_name" {
			p.fail(f, "unknown", "")
			continue
		}
		text, ok := p.text(f, v, 100, true)
		if !ok {
			continue
		}
		if code, bad := checkPattern(text); code != "" {
			p.fail(f, code, bad)
			continue
		}
		p.s.Tmux.SessionName = text
	}
}

func (p *parser) remote(value any) {
	m, ok := p.object("remote", value)
	if !ok {
		return
	}
	r := &p.s.Remote
	for _, name := range sortedKeys(m) {
		f, v := "remote."+name, m[name]
		if v == nil {
			continue
		}
		switch name {
		case "enabled", "funnel":
			b, ok := v.(bool)
			switch {
			case !ok:
				p.fail(f, "bool", "")
			case name == "enabled":
				r.Enabled = b
			default:
				r.Funnel = b
			}
		case "hostname":
			// Имя узла — одна метка DNS: латиница в нижнем регистре, цифры и дефис.
			if text, ok := p.text(f, v, 63, true); ok {
				if !hostLabel(text) {
					p.fail(f, "hostname", "")
				} else {
					r.Hostname = text
				}
			}
		case "state_dir":
			if text, ok := p.text(f, v, 500, true); ok {
				if text == "~" || !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "~/") {
					p.fail(f, "temp_path", text)
				} else {
					r.StateDir = text
				}
			}
		default:
			p.fail(f, "unknown", "")
		}
	}
}

func hostLabel(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// checkPattern проверяет шаблон имени сессии tmux. Вне подстановок — только латиница, цифры,
// «-» и «_»: точку и двоеточие tmux понимает как разделители окна и панели.
func checkPattern(pattern string) (code, bad string) {
	rest := pattern
	for rest != "" {
		start := strings.IndexByte(rest, '{')
		literal := rest
		if start >= 0 {
			literal = rest[:start]
		}
		for _, r := range literal {
			if r != '-' && r != '_' && !(r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r))) {
				return "name_chars", string(r)
			}
		}
		if start < 0 {
			return "", ""
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return "placeholder", rest[start:]
		}
		name := rest[start+1 : start+end]
		if !slices.Contains(Placeholders, name) {
			return "placeholder", rest[start : start+end+1]
		}
		rest = rest[start+end+1:]
	}
	return "", ""
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// SplitFlags разбирает флаги запуска как командная строка: пробелы разделяют, кавычки
// и обратная косая черта экранируют. Ошибка — незакрытая кавычка или «\» в конце.
func SplitFlags(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	word, quote, escaped := false, rune(0), false
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]):
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, word = true, true
		case r == '\'' || r == '"':
			quote, word = r, true
		case unicode.IsSpace(r):
			if word {
				args = append(args, cur.String())
				cur.Reset()
				word = false
			}
		default:
			cur.WriteRune(r)
			word = true
		}
	}
	if quote != 0 || escaped {
		return nil, FieldError{Field: "flags", Code: "quote"}
	}
	if word {
		args = append(args, cur.String())
	}
	return args, nil
}

// FieldError — неверное значение одной настройки. Field — путь в JSON, например
// agents.codex.home; у ошибки всего файла он пустой. Интерфейс показывает Text рядом с полем.
type FieldError struct {
	Field string
	Code  string
	Value string // подставляется в текст вместо {value}
}

var messages = map[string][2]string{
	"json":          {"файл не читается как JSON-объект: {value}", "not a JSON object: {value}"},
	"object":        {"нужен объект", "must be an object"},
	"unknown":       {"неизвестная настройка", "unknown setting"},
	"unknown_agent": {"неизвестный агент", "unknown agent"},
	"bool":          {"нужно true или false", "must be true or false"},
	"string":        {"нужна строка до {value} символов без переводов строк", "must be a single line of up to {value} characters"},
	"required":      {"нужна непустая строка до {value} символов без переводов строк", "must be a non-empty single line of up to {value} characters"},
	"quote":         {"незакрытая кавычка или \\ в конце", "unclosed quote or trailing \\"},
	"minutes":       {"нужно целое число минут от 1 до 120", "must be a whole number of minutes from 1 to 120"},
	"seconds":       {"нужно число секунд от 10 до 7200", "must be a number of seconds from 10 to 7200"},
	"runner":        {"auto или один из: {value}", "auto or one of: {value}"},
	"one_of":        {"одно из: {value}", "one of: {value}"},
	"list":          {"нужен список до {value} строк", "must be a list of up to {value} strings"},
	"temp_path":     {"путь {value} должен начинаться с / или ~/", "path {value} must start with / or ~/"},
	"placeholder":   {"неизвестная подстановка {value}; есть {project}, {branch}, {agent}, {id}", "unknown placeholder {value}; available: {project}, {branch}, {agent}, {id}"},
	"hostname":      {"латиница в нижнем регистре, цифры и -, до 63 символов; - не в начале и не в конце", "lowercase latin letters, digits and -, up to 63 characters; no - at the start or end"},
	"name_chars":    {"символ «{value}» нельзя: вне подстановок только латиница, цифры, - и _", "character “{value}” is not allowed: outside placeholders use only latin letters, digits, - and _"},
}

// Text — сообщение на языке интерфейса: en — английский, иначе русский.
func (e FieldError) Text(lang string) string {
	pair, ok := messages[e.Code]
	if !ok {
		return e.Code
	}
	text := pair[0]
	if lang == "en" {
		text = pair[1]
	}
	return strings.ReplaceAll(text, "{value}", e.Value)
}

func (e FieldError) Error() string {
	if e.Field == "" {
		return e.Text("ru")
	}
	return e.Field + ": " + e.Text("ru")
}

// Errors — все ошибки проверки разом, чтобы интерфейс показал каждую у своего поля.
type Errors []FieldError

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.Error()
	}
	return strings.Join(parts, "; ")
}
