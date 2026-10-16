package server

import (
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/store"
	"github.com/genhoi/ai-threads/internal/summary"
)

// Типы ответов и запросов API. Время — секунды Unix. Имена полей через подчёркивание, как
// в settings.json и state.json. Те же типы на TypeScript лежат в web/src/api/types.ts:
// меняя здесь, меняйте и там.

// SessionView — сессия, как её видит интерфейс: журнал, состояние и пометки человека вместе.
type SessionView struct {
	Key      string        `json:"key"`
	Agent    string        `json:"agent"`
	ID       string        `json:"id"`
	Title    string        `json:"title"`              // своё название, иначе из журнала, иначе пусто
	Original string        `json:"original,omitempty"` // название из журнала, если человек дал своё
	Project  string        `json:"project"`
	Repo     string        `json:"repo,omitempty"` // корень основного репозитория
	Cwd      string        `json:"cwd"`
	Branch   string        `json:"branch,omitempty"`
	Worktree *WorktreeView `json:"worktree,omitempty"` // сессия работает в копии
	Created  int64         `json:"created"`
	Updated  int64         `json:"updated"`

	State    model.State `json:"state"`
	Since    int64       `json:"since,omitempty"`     // с какого момента в этом состоянии
	ExitCode int         `json:"exit_code,omitempty"` // для error
	Unseen   bool        `json:"unseen,omitempty"`    // «новое»
	Last     string      `json:"last,omitempty"`      // последний ответ агента
	LastLine string      `json:"last_line,omitempty"` // последняя строка терминала, для «Работают»
	Terminal bool        `json:"terminal"`            // терминал открыт в «Нити»
	Where    string      `json:"where,omitempty"`     // сессия идёт вне «Нити»: terminal или background

	Pinned    bool   `json:"pinned,omitempty"`
	Done      bool   `json:"done,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
	Service   bool   `json:"service,omitempty"` // автоматический запуск или временная папка
	By        string `json:"by,omitempty"`      // кто запустил без человека
	Missing   bool   `json:"missing,omitempty"` // папки сессии нет
	Resumable bool   `json:"resumable"`         // можно продолжить в терминале
}

// GET /api/sessions?project=&agent=&service=1&limit=&cursor=&before= отдаёт []SessionView: сначала
// новые, при равном updated — по key. Заголовки ответа: X-Total-Count — сколько сессий подходит
// под фильтры без учёта страниц; X-Next-Cursor — cursor следующей страницы (<updated>:<key>),
// пусто — страниц больше нет. Неверный cursor — 400.

type WorktreeView struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Repo   string `json:"repo"`
}

// ProjectView — проект в панели: папка основного репозитория или папка сессий вне git.
type ProjectView struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Git          bool   `json:"git"`
	Pinned       bool   `json:"pinned,omitempty"`
	Hidden       bool   `json:"hidden,omitempty"`
	DefaultAgent string `json:"default_agent,omitempty"`
	Sessions     int    `json:"sessions"` // без служебных
	Updated      int64  `json:"updated"`  // последняя активность в проекте
}

// AgentView — агент: установлен ли, где журналы, что умеет.
type AgentView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Short      string `json:"short"` // «Cl», «Cx»: метка вместо иконки
	Enabled    bool   `json:"enabled"`
	Program    string `json:"program,omitempty"` // пусто — программа не найдена
	Home       string `json:"home"`
	HomeSource string `json:"home_source"` // env, settings или default
	HomeFound  bool   `json:"home_found"`
	Sessions   int    `json:"sessions"`
	Resumable  bool   `json:"resumable"` // сессию можно продолжить в терминале (у ZCode нельзя)
	Runner     bool   `json:"runner"`    // умеет писать сводки
	SkipFlag   string `json:"skip_flag,omitempty"`
}

type EnvView struct {
	Version     string `json:"version"`
	Addr        string `json:"addr"` // 127.0.0.1:8765
	Tmux        bool   `json:"tmux"`
	TmuxVersion string `json:"tmux_version,omitempty"`
	TmuxPath    string `json:"tmux_path,omitempty"`
	WSL         bool   `json:"wsl"`
	VSCode      bool   `json:"vscode"`
	Platform    string `json:"platform"` // linux или darwin
	// Via — откуда пришёл запрос: local (с компьютера) или remote (с телефона через узел
	// Tailscale). С телефона управление доступом недоступно: /api/remote отвечает 403.
	Via          string      `json:"via"`
	Remote       RemoteBrief `json:"remote"`        // доступ с телефона коротко, без ключа
	SettingsPath string      `json:"settings_path"` // файл настроек, который читает «Нить»
	DataDir      string      `json:"data_dir"`      // папка данных: пометки, кэш каталога, ключи телефона
}

// RemoteBrief — доступ с телефона для первой отрисовки.
type RemoteBrief struct {
	State string `json:"state"`          // как RemoteView.State
	Host  string `json:"host,omitempty"` // nit.<сеть>.ts.net, когда узел в сети
}

type ProgressView struct {
	Done   int               `json:"done"`
	Total  int               `json:"total"`
	Ready  bool              `json:"ready"`
	Agents map[string]string `json:"agents"` // id → waiting, reading или done
}

// Bootstrap — ответ GET /api/bootstrap: всё, что нужно для первой отрисовки.
// Служебных сессий в Sessions нет, их отдаёт GET /api/sessions?service=1.
type Bootstrap struct {
	Env      EnvView           `json:"env"`
	Agents   []AgentView       `json:"agents"`
	Projects []ProjectView     `json:"projects"`
	Sessions []SessionView     `json:"sessions"`
	Counts   SessionCounts     `json:"counts"`
	Progress ProgressView      `json:"progress"`
	Settings settings.Settings `json:"settings"`
}

// SessionCounts — сколько сессий всего и сколько из них служебных. All считает те же сессии,
// что AgentView.Sessions, вместе с запущенными, которых в каталоге ещё нет; All − Service —
// длина Bootstrap.Sessions.
type SessionCounts struct {
	Service int `json:"service"`
	All     int `json:"all"`
}

// События потока GET /api/events (SSE). Имя события — поле event, данные — JSON.
//
//	sessions  SessionsEvent   изменились или исчезли сессии
//	projects  []ProjectView   список проектов целиком
//	progress  ProgressView    ход чтения журналов
//	notice    NoticeEvent     сообщение для всплывающего окна
//	search    SearchEvent     ход и итог полного поиска
//	job       JobView         ход и итог сводки: на запуске, на каждом этапе и в конце
//	digest    summary.Digest  сохранена сводка за несколько дней
//	remote    RemoteView      поменялись состояние узла, ключ или устройства; только на компьютер
type SessionsEvent struct {
	Changed []SessionView `json:"changed"`
	Removed []string      `json:"removed"`
	// Renamed — временный ключ запущенной сессии → настоящий, когда агент назвал свой ID.
	// Старый ключ тогда есть и в Removed, новый — в Changed.
	Renamed map[string]string `json:"renamed,omitempty"`
}

type NoticeEvent struct {
	Kind string `json:"kind"` // error, waiting, info
	Key  string `json:"key,omitempty"`
	Text string `json:"text"`
}

// Details — ответ GET /api/sessions/{key}: сведения о сессии.
type Details struct {
	Session   SessionView    `json:"session"`
	Messages  []MessageView  `json:"messages"`  // последние сообщения переписки, не больше 60
	Truncated bool           `json:"truncated"` // показаны не все
	Summary   *store.Summary `json:"summary,omitempty"`
	Resume    string         `json:"resume,omitempty"` // команда продолжения в другом терминале, с cd
	Attach    string         `json:"attach,omitempty"` // подключиться к терминалу «Нити»
	Launch    *LaunchInfo    `json:"launch,omitempty"` // если сессию запустила «Нить»
}

type MessageView struct {
	Role string `json:"role"` // user или assistant
	Text string `json:"text"`
	At   int64  `json:"at,omitempty"`
}

type LaunchInfo struct {
	At     int64  `json:"at"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// LaunchCheck — ответ GET /api/launch/check?path=&prompt=: что известно о папке до запуска.
// По prompt предлагается имя ветки.
type LaunchCheck struct {
	Path         string         `json:"path"`
	Project      string         `json:"project"`
	Exists       bool           `json:"exists"`
	Git          bool           `json:"git"`
	Busy         []BusyView     `json:"busy"` // кто уже работает в этой папке
	Mode         string         `json:"mode"` // предложение: folder или copy
	Branch       string         `json:"branch,omitempty"`
	DefaultAgent string         `json:"default_agent,omitempty"`
	Recent       []BusyView     `json:"recent,omitempty"` // последние сессии проекта, чтобы продолжить
	Worktrees    []WorktreeView `json:"worktrees,omitempty"`
}

type BusyView struct {
	Key   string      `json:"key"`
	Agent string      `json:"agent"`
	Title string      `json:"title"`
	State model.State `json:"state"`
}

// LaunchRequest — тело POST /api/launch. Prompt может быть пустым: агент запустится и будет ждать.
type LaunchRequest struct {
	Path   string `json:"path"`
	Agent  string `json:"agent"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Mode   string `json:"mode"` // folder или copy
	Branch string `json:"branch,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// LaunchResponse — ключ новой сессии. Пока агент не назвал свой ID, ключ временный
// (<агент>:new-…); когда ID найдётся, придёт событие sessions с новым ключом и старый в removed.
type LaunchResponse struct {
	Key string `json:"key"`
}

// FinishCheck — ответ GET /api/sessions/{key}/finish: что произойдёт при завершении.
type FinishCheck struct {
	Worktree        *WorktreeView `json:"worktree,omitempty"`
	Dirty           []string      `json:"dirty"`  // незакоммиченные файлы копии
	Pushed          bool          `json:"pushed"` // ветка отправлена
	Merged          bool          `json:"merged"` // ветка влита в основную
	Others          []string      `json:"others"` // ключи других сессий с терминалом в этой копии
	CanRemove       bool          `json:"can_remove"`
	CanDeleteBranch bool          `json:"can_delete_branch"`
}

type FinishRequest struct {
	RemoveWorktree bool `json:"remove_worktree"`
	DeleteBranch   bool `json:"delete_branch"`
}

// RebootView — ответ GET /api/reboot: терминалы, открытые до перезагрузки, которых больше нет.
type RebootView struct {
	Sessions []RebootSession `json:"sessions"`
}

type RebootSession struct {
	Session SessionView `json:"session"`
	Was     model.State `json:"was"`               // последнее известное состояние: waiting, working, error
	Code    int         `json:"code,omitempty"`    // для error
	Minutes int         `json:"minutes,omitempty"` // сколько работал, для working
}

// ErrorView — тело ответа с ошибкой. Fields — ошибки по полям формы, как их отдаёт settings.
type ErrorView struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields,omitempty"`
}

// EnvCheck — ответ GET /api/env: проверка для первого запуска.
type EnvCheck struct {
	Env    EnvView     `json:"env"`
	Agents []AgentView `json:"agents"`
}

// ModelsView — ответ GET /api/models?agent=: что можно выбрать при запуске. Пустые значения —
// «как настроено в CLI».
type ModelsView struct {
	DefaultModel  string      `json:"default_model"`
	DefaultEffort string      `json:"default_effort"`
	Models        []ModelView `json:"models"`
	Efforts       []string    `json:"efforts"` // уровни рассуждений для всех моделей сразу
}

type ModelView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
}

// RenameRequest — тело POST /api/sessions/{key}/rename. Пустое название возвращает название из журнала.
type RenameRequest struct {
	Name string `json:"name"`
}

// FlagRequest — тело POST /api/sessions/{key}/{pin,hide,done}.
type FlagRequest struct {
	On bool `json:"on"`
}

// ProjectRequest — тело POST /api/project: пометки проекта. Пустые поля не меняются.
type ProjectRequest struct {
	Path         string  `json:"path"`
	Pinned       *bool   `json:"pinned,omitempty"`
	Hidden       *bool   `json:"hidden,omitempty"`
	DefaultAgent *string `json:"default_agent,omitempty"` // пустая строка снимает
}

// ScreenView — ответ GET /api/terminal/{key}/screen?history=N: экран терминала текстом.
type ScreenView struct {
	Text string `json:"text"`
}

// KeysRequest — тело POST /api/terminal/{key}/keys: сначала текст как есть, потом клавиши
// по именам tmux (Enter, Escape, C-c, Up, 1…).
type KeysRequest struct {
	Text string   `json:"text,omitempty"`
	Keys []string `json:"keys,omitempty"`
}

// SearchHit — находка поиска: сессия, поле с совпадением и фрагмент вокруг него.
// Field: title, original, project, branch, path, id, summary, conversation или log (журнал целиком).
type SearchHit struct {
	Session SessionView `json:"session"`
	Field   string      `json:"field"`
	Snippet string      `json:"snippet"`
}

// FullSearchRequest — тело POST /api/search/full. Новый запрос останавливает идущий.
type FullSearchRequest struct {
	Q       string `json:"q"`
	Service bool   `json:"service,omitempty"` // искать и в служебных и скрытых
}

// FullSearchStarted — ответ POST /api/search/full: ход и итог придут событиями search с этим id.
type FullSearchStarted struct {
	ID string `json:"id"`
}

// SearchEvent — событие search: ход полного поиска. Hits приходят в последнем событии,
// у которого Finished; в остальных их нет.
type SearchEvent struct {
	ID       string      `json:"id"`
	Done     int         `json:"done"`
	Total    int         `json:"total"`
	Finished bool        `json:"finished"`
	Hits     []SearchHit `json:"hits"`
}

// RebootRequest — тело POST /api/reboot: продолжить сессии Keys; Dismiss — забыть остальные.
type RebootRequest struct {
	Keys    []string `json:"keys,omitempty"`
	Dismiss bool     `json:"dismiss,omitempty"`
}

// RebootResult — ответ POST /api/reboot.
type RebootResult struct {
	Resumed []string          `json:"resumed"`
	Failed  map[string]string `json:"failed,omitempty"` // ключ → почему не продолжилась
}

// RevealRequest — тело POST /api/reveal: папка сессии (Key) или любая папка (Path).
type RevealRequest struct {
	Key  string `json:"key,omitempty"`
	Path string `json:"path,omitempty"`
	App  string `json:"app"` // folder или vscode
}

// --- Сводки ---
//
// Сводку пишет агент без человека в задании. Ход задания приходит событиями job. Готовая
// сводка по сессии сохраняется до последнего события job, поэтому после него её уже отдаёт
// GET /api/sessions/{key}, а страницы получают сессию событием sessions. Готовая сводка
// за несколько дней приходит событием digest.

// SummaryRequest — тело POST /api/sessions/{key}/summary, необязательное.
type SummaryRequest struct {
	Agent string `json:"agent,omitempty"` // auto, kimi, claude, codex или grok; пусто — из настроек
}

// DigestRequest — тело POST /api/digest.
type DigestRequest struct {
	Days  int    `json:"days"`            // 3, 5 или 7
	Agent string `json:"agent,omitempty"` // как в SummaryRequest
}

// JobView — задание сводки. Stage: starting (готовит данные и запускает агента), reading
// (агент читает журналы), writing (пишет ответ) и итог: done, failed или cancelled.
type JobView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`             // summary или digest
	Key     string `json:"key,omitempty"`    // сессия, для summary
	Days    int    `json:"days,omitempty"`   // период, для digest
	Agent   string `json:"agent"`            // кто пишет: kimi, claude, codex или grok
	Stage   string `json:"stage"`            // starting, reading, writing, done, failed, cancelled
	Reads   int    `json:"reads,omitempty"`  // сколько раз агент открывал журналы
	Started int64  `json:"started"`          // начало, секунды Unix
	Elapsed int    `json:"elapsed"`          // секунды от начала до события
	Done    bool   `json:"done"`             // задание закончилось: done, failed или cancelled
	Error   string `json:"error,omitempty"`  // почему failed
	Output  string `json:"output,omitempty"` // хвост вывода агента при ошибке
}

// JobStarted — ответ на запуск сводки. Если по той же сессии или периоду задание уже шло,
// это оно, новое не запускается.
type JobStarted struct {
	Job JobView `json:"job"`
}

// DigestView — ответ GET /api/digest?days=: сохранённая сводка за период и идущее задание.
type DigestView struct {
	Digest *summary.Digest `json:"digest"` // null — сводку за этот период ещё не составляли
	Job    *JobView        `json:"job"`    // null — задание не идёт
}

// CancelResult — ответ POST /api/jobs/{id}/cancel. false — задания нет или оно уже закончилось.
type CancelResult struct {
	Cancelled bool `json:"cancelled"`
}

// --- Доступ с телефона ---
//
// Узел Tailscale живёт в самой «Нити» и слушает https://<host>/. Телефон входит по ссылке
// Link с ключом и получает cookie устройства. Все запросы /api/remote — только с компьютера:
// с телефона они отвечают 403.

// RemoteView — ответ GET /api/remote, POST /api/remote, POST /api/remote/key,
// POST /api/remote/devices/{id}/revoke и данные события remote.
//
// State: off (выключен), starting (узел поднимается), needs_login (ждёт входа в Tailscale
// по LoginURL), certificate (адрес есть, выпускается первый сертификат, до минуты),
// running (телефон может войти), error (не вышло; новая попытка через время, сразу —
// POST /api/remote {"enabled": true}).
type RemoteView struct {
	Enabled  bool         `json:"enabled"`             // включён в настройках
	Funnel   bool         `json:"funnel"`              // адрес открыт и из интернета
	State    string       `json:"state"`               // off, starting, needs_login, certificate, running, error
	Since    int64        `json:"since,omitempty"`     // с какого момента в этом состоянии
	LoginURL string       `json:"login_url,omitempty"` // для needs_login: ссылка входа в Tailscale
	Host     string       `json:"host,omitempty"`      // nit.<сеть>.ts.net; в certificate и running
	URL      string       `json:"url,omitempty"`       // https://<host>/
	Link     string       `json:"link,omitempty"`      // https://<host>/?key=<ключ>: ссылка входа для QR-кода
	Error    string       `json:"error,omitempty"`     // для error: причина на языке интерфейса
	Devices  []DeviceView `json:"devices"`             // подключённые телефоны, последние заходившие первыми
}

// DeviceView — подключённый телефон. Name — из User-Agent: «iPhone · Safari», «Pixel 8 · Chrome».
type DeviceView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"last_seen"` // обновляется не чаще раза в минуту
}

// RemoteRequest — тело POST /api/remote. Пустые поля не меняются.
type RemoteRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	Funnel  *bool `json:"funnel,omitempty"`
}
