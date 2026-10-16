package server

import (
	"net/http"
	"strings"
)

// apiError — ошибка запроса с текстом на языке интерфейса.
type apiError struct {
	status int
	code   string
	vars   map[string]string // подстановки {name} в тексте
	fields map[string]string
}

func (e *apiError) Error() string { return e.text("en") }

func (e *apiError) text(lang string) string {
	pair, ok := messages[e.code]
	if !ok {
		return e.code
	}
	text := pair[0]
	if lang == "en" {
		text = pair[1]
	}
	for k, v := range e.vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

// apiErr — ошибка с кодом сообщения; vars — пары «имя, значение» для подстановки.
func apiErr(status int, code string, vars ...string) *apiError {
	e := &apiError{status: status, code: code}
	if len(vars) > 0 {
		e.vars = map[string]string{}
		for i := 0; i+1 < len(vars); i += 2 {
			e.vars[vars[i]] = vars[i+1]
		}
	}
	return e
}

func notFound() *apiError { return apiErr(http.StatusNotFound, "no_session") }

// messages — тексты ошибок: русский и английский.
var messages = map[string][2]string{
	"bad_json":    {"Нужен JSON-объект", "A JSON object is required"},
	"too_large":   {"Тело запроса слишком большое", "The request body is too large"},
	"no_route":    {"Такого запроса нет", "No such request"},
	"no_session":  {"Сессия не найдена", "Session not found"},
	"need_field":  {"Нужно поле {field}", "Field {field} is required"},
	"no_terminal": {"Терминал этой сессии закрыт", "This session's terminal is closed"},
	"no_tmux": {"Для терминала нужен tmux. Установите его, например: sudo apt install tmux",
		"The terminal needs tmux. Install it, for example: sudo apt install tmux"},
	"no_resume": {"Эту сессию нельзя продолжить из терминала", "This session can't be resumed from the terminal"},
	"no_folder": {"Папки сессии больше нет: агенту негде продолжить", "The session folder is gone, so the agent has nowhere to continue"},
	"elsewhere_terminal": {"{agent} ещё работает с этой сессией в другом терминале. Закройте его, чтобы продолжить здесь.",
		"{agent} is still running this session in another terminal. Close it to continue here."},
	"elsewhere_background": {"{agent} ещё работает с этой сессией в фоне. Дождитесь конца, чтобы продолжить здесь.",
		"{agent} is still running this session in the background. Wait for it to finish to continue here."},
	"unknown_agent":  {"Неизвестный агент: {value}", "Unknown agent: {value}"},
	"agent_off":      {"{agent} выключен в настройках", "{agent} is turned off in settings"},
	"not_installed":  {"{agent} не установлен: программа не найдена", "{agent} is not installed: the program was not found"},
	"no_launch":      {"{agent} нельзя запустить в терминале", "{agent} can't be started in a terminal"},
	"bad_choice":     {"Неверная модель или уровень рассуждений", "Invalid model or reasoning level"},
	"bad_mode":       {"Где работать: folder или copy", "The mode must be folder or copy"},
	"bad_path":       {"Нужен абсолютный путь к папке", "An absolute folder path is required"},
	"no_dir":         {"Папки нет: {value}", "Folder not found: {value}"},
	"not_git":        {"Копию можно сделать только в репозитории git", "A copy can only be made in a git repository"},
	"bad_prompt":     {"Запрос длиннее 100 000 символов", "The prompt is longer than 100,000 characters"},
	"launch_failed":  {"Не удалось запустить {agent}: {value}", "Couldn't start {agent}: {value}"},
	"copy_failed":    {"Не удалось сделать копию: {value}", "Couldn't create the copy: {value}"},
	"bad_query":      {"Нужен запрос до 500 символов", "The query must be 1 to 500 characters"},
	"bad_cursor":     {"Неверный курсор страницы: нужен <время>:<ключ>", "Invalid page cursor: expected <updated>:<key>"},
	"bad_app":        {"Открыть можно в folder или vscode", "The app must be folder or vscode"},
	"explorer_wsl":   {"Не удалось открыть папку: {value}", "Couldn't open the folder: {value}"},
	"no_vscode":      {"Не нашёл VS Code: программы code нет в PATH", "VS Code not found: there is no code program in PATH"},
	"no_copy":        {"У сессии нет копии", "This session has no copy"},
	"dirty":          {"Копию не удалить: в ней есть незакоммиченные файлы: {value}", "The copy can't be removed: it has uncommitted files: {value}"},
	"busy":           {"Копию не удалить: в ней работают другие агенты: {value}", "The copy can't be removed: other agents are working in it: {value}"},
	"not_pushed":     {"Ветку {value} не удалить: она никуда не отправлена и не влита", "Branch {value} can't be deleted: it is neither pushed nor merged"},
	"remove_failed":  {"Не удалось удалить копию: {value}", "Couldn't remove the copy: {value}"},
	"bad_keys":       {"Неизвестная клавиша: {value}", "Unknown key: {value}"},
	"bad_text":       {"Текст длиннее 20 000 символов", "The text is longer than 20,000 characters"},
	"terminal_error": {"Терминал не ответил: {value}", "The terminal didn't respond: {value}"},
	"bad_days":       {"Период сводки: 3, 5 или 7 дней", "The digest period must be 3, 5 or 7 days"},
	"not_runner":     {"Сводки пишут только Kimi, Claude Code, Codex и Grok, а не {value}", "Only Kimi, Claude Code, Codex and Grok write summaries, not {value}"},
	"no_runner": {"Некому писать сводку: установите одного из агентов: {value}",
		"No agent can write the summary: install one of {value}"},
	"no_journal": {"Журнал сессии недоступен", "The session log is unavailable"},
	"local_only": {"Это можно сделать только на компьютере, не с телефона",
		"This can only be done on the computer, not from the phone"},
	"no_device": {"Такого телефона нет", "No such phone"},
	"no_link": {"Ссылки ещё нет: доступ с телефона не включён или узел не в сети",
		"There is no link yet: phone access is off or the node is not online"},
	"remote_no_https": {"Включите MagicDNS и HTTPS-сертификаты в админ-панели Tailscale: https://tailscale.com/s/https",
		"Turn on MagicDNS and HTTPS certificates in the Tailscale admin panel: https://tailscale.com/s/https"},
	"remote_no_funnel": {"Funnel не разрешён: добавьте атрибут funnel в ACL сети Tailscale (https://tailscale.com/s/no-funnel) или выключите Funnel",
		"Funnel is not allowed: add the funnel attribute to the Tailscale ACL (https://tailscale.com/s/no-funnel) or turn Funnel off"},
	"no_period_sessions": {"За этот период нет сессий с перепиской", "There are no sessions with messages in this period"},
}
