package server

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/genhoi/ai-threads/internal/terminal"
	"github.com/genhoi/ai-threads/internal/tmux"
)

const (
	maxHistory = 10000
	maxKeyText = 20000
)

// v1Name — имя сессии tmux, которое давала терминалу первая версия: nit-<агент>-<id>.
// Точку tmux в имени не принимает: такие имена получали хеш ключа.
func v1Name(key string) string {
	agent, id, _ := strings.Cut(key, ":")
	name := "nit-" + agent + "-" + id
	if strings.Contains(name, ".") {
		sum := sha1.Sum([]byte(key))
		name = strings.ReplaceAll(name, ".", "_") + "-" + hex.EncodeToString(sum[:])[:6]
	}
	return name
}

// terminalHandler — WebSocket терминала. Пока страница показывает терминал, закончивший ход
// агент не получает отметку «новое», а время просмотра пишется в пометки.
func (s *Server) terminalHandler() http.Handler {
	inner := terminal.Handler(s.terminalName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if _, ok := s.terminalName(key); !ok {
			s.fail(w, r, apiErr(http.StatusNotFound, "no_terminal"))
			return
		}
		release := s.tracker.View(key)
		_ = s.store.MarkSeen(key, time.Now())
		defer func() {
			release()
			_ = s.store.MarkSeen(key, time.Now())
		}()
		inner.ServeHTTP(w, r)
	})
}

// handleScreen — экран терминала текстом для телефона: history строк истории и сам экран.
func (s *Server) handleScreen(w http.ResponseWriter, r *http.Request) error {
	name, ok := s.terminalName(r.PathValue("key"))
	if !ok {
		return apiErr(http.StatusNotFound, "no_terminal")
	}
	history, _ := strconv.Atoi(r.URL.Query().Get("history"))
	history = max(0, min(history, maxHistory))
	text, err := tmux.Capture(name, history)
	if err != nil {
		return apiErr(http.StatusInternalServerError, "terminal_error", "value", err.Error())
	}
	writeJSON(w, http.StatusOK, ScreenView{Text: text})
	return nil
}

// handleKeys — ответ с телефона: текст как есть и клавиши по именам tmux.
func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	var req KeysRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	if utf8.RuneCountInString(req.Text) > maxKeyText {
		return apiErr(http.StatusBadRequest, "bad_text")
	}
	name, ok := s.terminalName(key)
	if !ok {
		return apiErr(http.StatusNotFound, "no_terminal")
	}
	if err := tmux.SendText(name, req.Text); err != nil {
		return apiErr(http.StatusInternalServerError, "terminal_error", "value", err.Error())
	}
	if err := tmux.SendKeys(name, req.Keys...); err != nil {
		if strings.Contains(err.Error(), "unknown key") {
			return apiErr(http.StatusBadRequest, "bad_keys", "value", err.Error())
		}
		return apiErr(http.StatusInternalServerError, "terminal_error", "value", err.Error())
	}
	// Ответил — значит, видел.
	_ = s.markSeen(key)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
