package agents

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/genhoi/ai-threads/internal/model"
)

const (
	headBytes = 1 << 20   // начало журнала: название, папка, первый запрос
	tailBytes = 256 << 10 // конец журнала: последний ответ и время
	rawBytes  = 128 << 20 // больше этого с конца журнала полный поиск не читает

	// MessageBytes — сколько байт с конца журнала читает экран сессии.
	MessageBytes = 8 << 20
	// SearchBytes — сколько байт с конца журнала читает быстрый поиск по переписке.
	SearchBytes = 512 << 10

	textLimit = 12000 // длиннее сообщение в выдержке для сводки обрезается
)

// jsonRows — объекты JSON из начала файла или из его конца, не больше limit байт.
// Строки, разрезанные границей чтения, пропускаются.
func jsonRows(path string, limit int64, tail bool) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	start := int64(0)
	if tail && size > limit {
		start = size - limit
	}
	// Один байт перед срезом отличает начало строки от её середины.
	from := start
	if start > 0 {
		from--
	}
	buf := make([]byte, min(limit, size-start)+start-from)
	n, _ := f.ReadAt(buf, from)
	raw := buf[:n]
	if start > 0 {
		if len(raw) == 0 {
			return nil
		}
		boundary := raw[0]
		raw = raw[1:]
		if boundary != '\n' {
			_, raw, _ = bytes.Cut(raw, []byte{'\n'})
		}
	}
	if !tail && size > limit {
		if i := bytes.LastIndexByte(raw, '\n'); i >= 0 {
			raw = raw[:i]
		} else {
			raw = nil
		}
	}
	var rows []map[string]any
	for line := range bytes.SplitSeq(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if json.Unmarshal(line, &row) == nil && row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

// readJSON — объект JSON из файла не больше headBytes; пустой при ошибке.
func readJSON(path string) map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return map[string]any{}
	}
	defer f.Close()
	buf := make([]byte, headBytes)
	n, _ := f.Read(buf)
	var data map[string]any
	if json.Unmarshal(buf[:n], &data) != nil || data == nil {
		return map[string]any{}
	}
	return data
}

// Значения JSON без проверки типа: как dict.get в первой версии.

func str(v any) string {
	s, _ := v.(string)
	return s
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// truthy — значение, которое в Python считалось бы истинным.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// or — первое истинное значение, иначе последнее: как «a or b» в Python.
func or(values ...any) any {
	for _, v := range values {
		if truthy(v) {
			return v
		}
	}
	return values[len(values)-1]
}

// contentText — текст из содержимого сообщения: строки или списка частей.
func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			if text := contentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		switch x["type"] {
		case "text", "input_text", "output_text":
			return str(x["text"])
		}
	}
	return ""
}

// serviceTags — вставки, которые CLI добавляет в сообщение пользователя; человек их не писал.
var serviceTags = []string{"INSTRUCTIONS", "environment_context", "recommended_plugins", "user_instructions", "system-reminder"}

var (
	serviceOpen  = regexp.MustCompile(`<(` + strings.Join(serviceTags, "|") + `)>`)
	agentsHeader = regexp.MustCompile(`^` + space + `*#*` + space + `*AGENTS\.md instructions[^\n]*\n?`)
	imageTag     = regexp.MustCompile(`</?image\b[^>]*>`)
)

// space — пробельный символ, как \s в Python: с неразрывным пробелом и прочими.
const space = `[\s\v\x{85}\pZ]`

// visibleText — текст сообщения без служебных вставок.
func visibleText(v any) string {
	text := contentText(v)
	text = dropServiceBlocks(text)
	text = agentsHeader.ReplaceAllString(text, "")
	text = imageTag.ReplaceAllString(text, "")
	// Незакрытая служебная вставка не должна становиться сообщением или названием.
	if loc := serviceOpen.FindStringIndex(text); loc != nil {
		text = text[:loc[0]]
	}
	return strings.TrimSpace(text)
}

// dropServiceBlocks убирает <тег>…</тег> служебных вставок. Регулярное выражение Go не
// умеет ссылаться на найденный тег, поэтому пара ищется вручную.
func dropServiceBlocks(text string) string {
	var out strings.Builder
	pos := 0
	for {
		loc := serviceOpen.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		closing := "</" + text[pos+loc[2]:pos+loc[3]] + ">"
		end := strings.Index(text[pos+loc[1]:], closing)
		if end < 0 {
			// Тег без пары остаётся; его отрежет проверка незакрытых вставок.
			out.WriteString(text[pos : pos+loc[1]])
			pos += loc[1]
			continue
		}
		out.WriteString(text[pos : pos+loc[0]])
		pos += loc[1] + end + len(closing)
	}
	out.WriteString(text[pos:])
	return out.String()
}

// claudeServicePrefixes — сообщения Claude Code от имени пользователя, которые на деле — вывод
// команд и служебные строки.
var claudeServicePrefixes = []string{"<local-command-caveat>", "<local-command-stdout>", "<local-command-stderr>",
	"<command-name>", "<command-message>", "<bash-stdout>", "<bash-stderr>"}

var (
	bashInput    = regexp.MustCompile(`(?s)^<bash-input>(.*?)</bash-input>$`)
	pastedTag    = regexp.MustCompile(`</?pasted_content\b[^>]*>`)
	markdownLead = regexp.MustCompile(`(?m)^` + space + `{0,3}(?:#{1,6}` + space + `+|[-*+]` + space + `+|>` + space + `?)`)
)

// claudeUserText — текст, который человек набрал в Claude Code; ok=false для служебной записи.
func claudeUserText(row map[string]any, value any) (string, bool) {
	if truthy(row["isMeta"]) || truthy(row["isCompactSummary"]) {
		return "", false
	}
	if kind, ok := obj(row["origin"])["kind"]; ok && kind != nil && kind != "human" {
		return "", false
	}
	text := strings.TrimSpace(contentText(value))
	for _, prefix := range claudeServicePrefixes {
		if strings.HasPrefix(text, prefix) {
			return "", false
		}
	}
	if m := bashInput.FindStringSubmatch(text); m != nil {
		return "! " + strings.TrimSpace(m[1]), true
	}
	return pastedTag.ReplaceAllString(text, ""), true
}

// cleanTitle — название из текста: одна строка без служебных вставок, до 150 символов.
func cleanTitle(v any) string {
	text := strings.Join(strings.Fields(visibleText(v)), " ")
	text = strings.TrimLeft(text, "#>*- ")
	return strings.TrimSpace(cut(text, 150))
}

// plainLine — одна строка без разметки Markdown, для строки сессии в ленте.
func plainLine(text string) string {
	text = markdownLead.ReplaceAllString(text, "")
	text = strings.NewReplacer("**", "", "__", "", "`", "").Replace(text)
	return cut(strings.Join(strings.Fields(text), " "), 300)
}

// prefix — начало текста для сравнения запросов: пробелы схлопнуты, до 50 символов.
func prefix(v any) string {
	return cut(strings.Join(strings.Fields(contentText(v)), " "), 50)
}

// cut — первые n символов (не байт).
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// timestamp — секунды Unix из числа (секунды или миллисекунды) или строки ISO 8601.
// Время без пояса считается UTC.
func timestamp(v any, fallback float64) float64 {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fallback
		}
		if x > 1e11 {
			return x / 1000
		}
		return x
	case int64:
		return timestamp(float64(x), fallback)
	case string:
		if t, ok := parseISO(x, time.UTC); ok {
			return seconds(t)
		}
	}
	return fallback
}

var isoLayouts = []string{
	"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05-07", "2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05-0700", "2006-01-02 15:04:05-07", "2006-01-02 15:04:05",
	"2006-01-02T15:04Z07:00", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02",
}

// parseISO — время ISO 8601; без пояса — в поясе naive.
func parseISO(s string, naive *time.Location) (time.Time, bool) {
	for _, layout := range isoLayouts {
		if t, err := time.ParseInLocation(layout, s, naive); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func seconds(t time.Time) float64 {
	return float64(t.UnixMicro()) / 1e6
}

// unixTime — время из секунд Unix; ноль — нулевое время.
func unixTime(sec float64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.UnixMicro(int64(math.Round(sec * 1e6))).UTC()
}

func message(role, text string, at float64, limit int) model.Message {
	if limit > 0 {
		text = cut(text, limit)
	}
	return model.Message{Role: role, Text: text, At: unixTime(at)}
}

var uuidRe = regexp.MustCompile(`^(?i)[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

// stamp — размер и время изменения файла; -1, -1, если файла нет.
func stamp(path string) [2]int64 {
	info, err := os.Stat(path)
	if err != nil {
		return [2]int64{-1, -1}
	}
	return [2]int64{info.Size(), info.ModTime().UnixNano()}
}
