package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/catalog"
	"github.com/genhoi/ai-threads/internal/remote"
)

const (
	// coalesce — столько копятся изменения перед отправкой: экран агента меняется каждую
	// секунду, каталог сообщает о сотнях сессий за обход.
	coalesce = 100 * time.Millisecond
	// heartbeat — комментарий в потоке, чтобы прокси и браузер не закрыли молчащее соединение.
	heartbeat = 15 * time.Second
	// clientBuffer — столько событий ждут медленного читателя; дальше соединение рвётся,
	// и страница переподключается и перечитывает всё.
	clientBuffer = 256
)

// hub копит изменения и раздаёт события всем открытым потокам.
type hub struct {
	mu       sync.Mutex
	clients  map[*client]struct{}
	dirty    map[string]bool
	all      bool
	renamed  map[string]string
	projects bool
	remote   bool // поменялся доступ с телефона
	prog     *catalog.Progress
	sentProg *catalog.Progress
	sentProj []byte
	wake     chan struct{}
}

type client struct {
	ch     chan []byte
	gone   chan struct{}
	remote bool // поток открыт с телефона: событий remote с ключом входа он не получает
}

func newHub() *hub {
	return &hub{clients: map[*client]struct{}{}, dirty: map[string]bool{}, renamed: map[string]string{},
		wake: make(chan struct{}, 1)}
}

func (h *hub) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// touch отмечает сессии, которые изменились или исчезли.
func (h *hub) touch(keys ...string) {
	if len(keys) == 0 {
		return
	}
	h.mu.Lock()
	for _, k := range keys {
		h.dirty[k] = true
	}
	h.mu.Unlock()
	h.signal()
}

// touchRemote — поменялись состояние узла, ключ входа или устройства.
func (h *hub) touchRemote() {
	h.mu.Lock()
	h.remote = true
	h.mu.Unlock()
	h.signal()
}

func (h *hub) touchProjects() {
	h.mu.Lock()
	h.projects = true
	h.mu.Unlock()
	h.signal()
}

// touchAll — поменяться могло что угодно: загрузили выгрузку или правила служебных сессий.
func (h *hub) touchAll() {
	h.mu.Lock()
	h.all, h.projects = true, true
	h.mu.Unlock()
	h.signal()
}

// rename — у запущенной сессии появился настоящий ключ.
func (h *hub) rename(temp, real string) {
	h.mu.Lock()
	h.renamed[temp] = real
	h.dirty[temp], h.dirty[real], h.projects = true, true, true
	h.mu.Unlock()
	h.signal()
}

// progress — ход чтения журналов. Пока идёт первый обход, интерфейс показывает его ход;
// после — только последнее событие с ready.
func (h *hub) progress(p catalog.Progress) {
	h.mu.Lock()
	if !p.Ready || h.sentProg == nil || !h.sentProg.Ready {
		h.prog = &p
	}
	h.projects = true
	h.mu.Unlock()
	h.signal()
}

func (h *hub) add(remote bool) *client {
	c := &client{ch: make(chan []byte, clientBuffer), gone: make(chan struct{}), remote: remote}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.gone)
	}
	h.mu.Unlock()
}

// frame — событие SSE: имя и данные одной строкой JSON.
func frame(event string, v any) []byte {
	var buf bytes.Buffer
	buf.WriteString("event: " + event + "\ndata: ")
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // Encode заканчивает строку переводом
	buf.WriteString("\n")
	return buf.Bytes()
}

// broadcast отправляет событие всем. Кто не успевает читать, отключается.
func (h *hub) broadcast(data []byte) { h.send(data, false) }

// broadcastLocal — только страницам на компьютере.
func (h *hub) broadcastLocal(data []byte) { h.send(data, true) }

func (h *hub) send(data []byte, localOnly bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if localOnly && c.remote {
			continue
		}
		select {
		case c.ch <- data:
		default:
			delete(h.clients, c)
			close(c.gone)
		}
	}
}

func (h *hub) listeners() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// notice — всплывающее сообщение для всех открытых страниц.
func (s *Server) notice(kind, key, text string) {
	s.events.broadcast(frame("notice", NoticeEvent{Kind: kind, Key: key, Text: text}))
}

// pump раз в coalesce собирает накопленные изменения в события.
func (s *Server) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.events.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(coalesce):
		}
		s.flush()
	}
}

// flush отправляет накопленное: сессии, ход чтения и проекты, если список поменялся.
func (s *Server) flush() {
	h := s.events
	h.mu.Lock()
	dirty, all, renamed, projects, prog, rem := h.dirty, h.all, h.renamed, h.projects, h.prog, h.remote
	h.dirty, h.all, h.renamed, h.projects, h.prog, h.remote = map[string]bool{}, false, map[string]string{}, false, nil, false
	if prog != nil {
		h.sentProg = prog
	}
	h.mu.Unlock()
	if h.listeners() == 0 {
		// Страница, которая подключится позже, сначала прочитает всё целиком.
		h.mu.Lock()
		h.sentProj = nil
		h.mu.Unlock()
		return
	}
	c := s.viewCtx()
	if len(dirty) > 0 || all {
		event := SessionsEvent{Changed: []SessionView{}, Removed: []string{}}
		if all {
			for _, sess := range s.sessions() {
				dirty[sess.Key()] = true
			}
		}
		for key := range dirty {
			if sess, ok := s.session(key); ok {
				event.Changed = append(event.Changed, c.view(sess))
			} else {
				event.Removed = append(event.Removed, key)
			}
		}
		if len(renamed) > 0 {
			event.Renamed = renamed
		}
		h.broadcast(frame("sessions", event))
	}
	if prog != nil {
		h.broadcast(frame("progress", progressView(*prog)))
	}
	if rem {
		h.broadcastLocal(frame("remote", s.remoteView(s.lang(nil))))
	}
	if projects {
		data := frame("projects", c.projects(s.sessions()))
		h.mu.Lock()
		same := bytes.Equal(data, h.sentProj)
		h.sentProj = data
		h.mu.Unlock()
		if !same {
			h.broadcast(data)
		}
	}
}

// handleEvents — поток событий SSE. Каждое событие сразу уходит в сеть: буферизации нет ни
// здесь, ни у обратного прокси (X-Accel-Buffering).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	c := s.events.add(remote.FromRemote(r))
	defer s.events.remove(c)
	// Переподключение через 3 секунды; ход чтения — сразу, чтобы страница знала, где каталог.
	if _, err := w.Write([]byte("retry: 3000\n\n")); err != nil {
		return
	}
	if _, err := w.Write(frame("progress", progressView(s.catalog.Progress()))); err != nil {
		return
	}
	if rc.Flush() != nil {
		return
	}
	ping := time.NewTicker(heartbeat)
	defer ping.Stop()
	for {
		var data []byte
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-c.gone:
			return
		case data = <-c.ch:
		case <-ping.C:
			data = []byte(": ping\n\n")
		}
		if _, err := w.Write(data); err != nil {
			return
		}
		if rc.Flush() != nil {
			return
		}
	}
}
