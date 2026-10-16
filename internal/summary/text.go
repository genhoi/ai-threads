package summary

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Error — ошибка задания для человека: текст на языке интерфейса и хвост вывода агента.
type Error struct {
	Text   string
	Output string // хвост stderr агента или начало его ответа; может быть пустым
}

func (e *Error) Error() string { return e.Text }

// messages — тексты ошибок заданий: русский и английский.
var messages = map[string][2]string{
	"start_failed":  {"Не удалось запустить {agent}: {value}", "Couldn't start {agent}: {value}"},
	"timeout":       {"{agent} не ответил за {value}", "{agent} didn't answer in {value}"},
	"exit_code":     {"{agent} завершился с кодом {value}", "{agent} exited with code {value}"},
	"empty_answer":  {"{agent} вернул пустой ответ", "{agent} returned an empty answer"},
	"bad_summary":   {"{agent} вернул ответ без названия или сводки. Попробуйте ещё раз.", "{agent} returned an answer without a title or summary. Try again."},
	"bad_digest":    {"{agent} вернул неверный ответ. Попробуйте ещё раз.", "{agent} returned an invalid answer. Try again."},
	"no_sessions":   {"За этот период нет сессий с перепиской", "There are no sessions with messages in this period"},
	"workdir":       {"Не удалось подготовить папку для агента: {value}", "Couldn't prepare a folder for the agent: {value}"},
	"no_transcript": {"Не удалось выгрузить переписку: {value}", "Couldn't export the conversation: {value}"},
}

// text — сообщение code на языке lang (en — английский, иначе русский) с подстановками
// {agent} и {value}.
func text(lang, code, agent, value string) string {
	pair, ok := messages[code]
	if !ok {
		return code
	}
	s := pair[0]
	if lang == "en" {
		s = pair[1]
	}
	return strings.NewReplacer("{agent}", agent, "{value}", value).Replace(s)
}

// languageLine — строка запроса о языке ответа.
func languageLine(lang string) string {
	if lang == "en" {
		return "Write every text field of the JSON answer in English."
	}
	return "Все текстовые поля ответа пиши на русском языке."
}

// duration — время ожидания для сообщения: «5 мин», «1 с».
func duration(lang string, d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		n := strconv.Itoa(int(d / time.Minute))
		if lang == "en" {
			return n + " min"
		}
		return n + " мин"
	}
	n := strconv.Itoa(max(1, int(d.Round(time.Second)/time.Second)))
	if lang == "en" {
		return n + " s"
	}
	return n + " с"
}

var fenced = regexp.MustCompile("(?is)```(?:json)?\\s*\\n(.*?)```")

// loadsAnswer достаёт JSON-объект из ответа модели: весь ответ, блок ```json или кусок от
// первой «{» до последней «}», если модель добавила слова вокруг.
func loadsAnswer(answer string) (map[string]any, error) {
	content := strings.TrimSpace(answer)
	if m := fenced.FindStringSubmatch(content); m != nil {
		content = strings.TrimSpace(m[1])
	}
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
		if start < 0 || end <= start {
			return nil, err
		}
		if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
			return nil, err
		}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("the answer is not a JSON object")
	}
	return obj, nil
}

// clip обрезает текст до n символов.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// clipTail — последние n символов текста.
func clipTail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// oneLine — текст одной строкой, не длиннее n символов; обрезанный кончается «…».
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n])) + "…"
}

// str — строка из значения JSON; не строка — пусто.
func str(v any) string {
	s, _ := v.(string)
	return s
}
