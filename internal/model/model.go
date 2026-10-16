// Package model — общие типы «Нити»: сессия агента, её состояние, проект и сообщение переписки.
//
// Каталог заполняет Session по журналам агентов. Live — то, что известно о запущенной сессии
// сейчас. Пометки человека (название, закрепление и т. п.) хранит пакет store, а сервер
// собирает из всего этого то, что видит интерфейс.
package model

import (
	"path/filepath"
	"strings"
	"time"
)

// Session — сессия агента, как её видно по журналу.
type Session struct {
	Agent   string    // id агента: claude, codex, grok, kimi, zcode, cursor
	ID      string    // id сессии у агента; у Kimi — session_<uuid>
	Title   string    // название из журнала: заголовок агента или первый запрос
	Cwd     string    // папка, где работал агент; может быть пустой
	Repo    string    // корень основного репозитория; для копии (git worktree) — исходного; пусто вне git
	Branch  string    // ветка git на момент сессии
	Created time.Time //
	Updated time.Time // последняя активность
	Auto    bool      // запуск без человека: codex exec, claude -p, grok -p, kimi -p и т. п.
	By      string    // кто запустил без человека, например «codex exec»; пусто — человек
	Last    string    // последний ответ агента, до 300 символов
	Empty   bool      // в журнале нет ни одного текстового сообщения

	Journal string // файл с перепиской
	Meta    string // файл, по которому сессия найдена
}

// Key — ключ сессии, по которому хранятся пометки человека. Формат как в первой версии.
func (s Session) Key() string { return s.Agent + ":" + s.ID }

// Project — имя проекта: папка основного репозитория, а вне git — папка сессии.
// Так сессия в копии ~/.nit/worktrees/backend/BACKEND-10300 относится к проекту backend.
func (s Session) Project() string {
	dir := s.Repo
	if dir == "" {
		dir = s.Cwd
	}
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// State — состояние сессии в «Нити».
type State string

const (
	StateStarting State = "starting" // терминал открыт, агент ещё ничего не вывел
	StateWorking  State = "working"  // агент выполняет задачу
	StateWaiting  State = "waiting"  // агент закончил ход или спрашивает разрешение
	StateError    State = "error"    // процесс агента вышел с ненулевым кодом
	StateExited   State = "exited"   // процесс вышел без ошибки, терминал ещё открыт
	StateIdle     State = "idle"     // терминала нет, сессию можно продолжить
)

// Live — то, что известно о сессии сейчас, а не из журнала.
type Live struct {
	State    State
	Since    time.Time // с какого момента сессия в этом состоянии
	ExitCode int       // для StateError
	Unseen   bool      // агент закончил ход после того, как человек последний раз смотрел сессию
	LastLine string    // последняя непустая строка экрана терминала, для «Работают»
	Terminal string    // имя сессии tmux, если терминал открыт
	Where    string    // где идёт сессия, запущенная вне «Нити»: «терминал», «VS Code» и т. п.
}

// Worktree — копия репозитория, в которой работает сессия.
type Worktree struct {
	Path   string // ~/.nit/worktrees/<проект>/<ветка>
	Branch string
	Repo   string // папка основного репозитория
}

// Message — сообщение переписки.
type Message struct {
	Role string    // user или assistant
	Text string    //
	At   time.Time // может быть нулевым, если агент время не пишет
}
