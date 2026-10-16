// Package terminal — WebSocket между xterm.js на странице и терминалом агента в tmux.
//
// Одно соединение — одно подключение `tmux attach-session`. Вывод tmux уходит странице
// двоичными сообщениями, ввод человека приходит двоичными, новый размер окна — текстовым
// {"type":"resize","cols":N,"rows":M}. Агент от соединения не зависит: при обрыве отключается
// только клиент tmux. Origin проверяет сервер до вызова обработчика.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/genhoi/ai-threads/internal/tmux"
)

// Resolver находит имя сессии tmux по ключу сессии; false — терминал не открыт.
type Resolver func(key string) (name string, ok bool)

const (
	// Ввод с клавиатуры и вставка из буфера: мегабайта хватает с запасом.
	maxMessage = 1 << 20
	// Пинг замечает пропавшую страницу (уснувший ноутбук), даже когда агент молчит.
	pingEvery   = 30 * time.Second
	pingTimeout = 15 * time.Second
)

// Handler — обработчик GET /api/terminal/{key}. Ключ берётся из пути ({key}) или из ?key=.
// ?cols= и ?rows= — начальный размер; ?view=full подключается без смены размера окна агента.
func Handler(resolve Resolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		name, ok := resolve(key)
		if !ok {
			http.Error(w, "terminal is not open", http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		// Подключение до ответа 101: если терминала уже нет, странице уйдёт обычная ошибка HTTP.
		att, err := tmux.Attach(name, number(q.Get("cols")), number(q.Get("rows")),
			tmux.AttachOptions{IgnoreSize: q.Get("view") == "full"})
		if errors.Is(err, tmux.ErrNotFound) {
			http.Error(w, "terminal is not open", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer att.Close()
		// Origin уже проверен сервером: своя проверка библиотеки не знала бы адреса для телефона.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(maxMessage)
		bridge(conn, att)
	})
}

// bridge гоняет байты между соединением и подключением к tmux, пока одна из сторон не закроется.
// Терминал закрыли — соединение закрывается с кодом 1000; страница ушла — отключается клиент tmux.
func bridge(conn *websocket.Conn, att *tmux.Attachment) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := att.Read(buf)
			if n > 0 {
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				// Клиент tmux завершился: терминал закрыли.
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}()
	go ping(ctx, conn, cancel)
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := att.Write(data); err != nil {
				return
			}
			continue
		}
		var msg struct {
			Type string `json:"type"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
			_ = att.Resize(msg.Cols, msg.Rows)
		}
	}
}

func ping(ctx context.Context, conn *websocket.Conn, cancel func()) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, stop := context.WithTimeout(ctx, pingTimeout)
			err := conn.Ping(pctx)
			stop()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func number(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
