package summary

import (
	"bytes"
	"encoding/json"
	"strings"
)

// stream разбирает поток агента без человека, строку за строкой: когда он вызывает инструменты
// и что отвечает. Форматы — как в первой версии: Claude Code (stream-json) и Grok
// (streaming-messages-json) пишут одинаковые записи assistant и result, Kimi — сообщения
// с role, Codex (exec --json) — события item.*; его ответ приходит ещё и в файл.
type stream struct {
	agent string
	job   *Job
	text  string // последний текст модели
	final string // итоговый ответ из записи result
}

func (s *stream) line(raw []byte) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var row map[string]any
	if json.Unmarshal(raw, &row) != nil {
		return
	}
	switch s.agent {
	case "claude", "grok":
		s.wire(row)
	case "kimi":
		s.kimi(row)
	case "codex":
		s.codex(row)
	}
}

// answer — ответ модели: итоговый, а без него — последний текст.
func (s *stream) answer() string {
	if strings.TrimSpace(s.final) != "" {
		return s.final
	}
	return s.text
}

func (s *stream) said(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.text = text
	s.job.Writing()
}

func (s *stream) wire(row map[string]any) {
	switch row["type"] {
	case "assistant":
		message, _ := row["message"].(map[string]any)
		blocks, _ := message["content"].([]any)
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			switch block["type"] {
			case "tool_use":
				s.job.Read()
			case "text":
				s.said(str(block["text"]))
			}
		}
	case "result":
		if result := str(row["result"]); strings.TrimSpace(result) != "" {
			s.final = result
			s.job.Writing()
		}
	}
}

func (s *stream) kimi(row map[string]any) {
	if row["role"] != "assistant" {
		return
	}
	calls, _ := row["tool_calls"].([]any)
	for range calls {
		s.job.Read()
	}
	switch content := row["content"].(type) {
	case string:
		s.said(content)
	case []any:
		var parts []string
		for _, raw := range content {
			if part, _ := raw.(map[string]any); part["type"] == "text" {
				parts = append(parts, str(part["text"]))
			}
		}
		s.said(strings.Join(parts, ""))
	}
}

func (s *stream) codex(row map[string]any) {
	item, _ := row["item"].(map[string]any)
	switch row["type"] {
	case "item.started":
		if item["type"] == "command_execution" {
			s.job.Read()
		}
	case "item.completed":
		if item["type"] == "agent_message" {
			s.said(str(item["text"]))
		}
	}
}

// lines — io.Writer, который отдаёт вывод процесса построчно.
type lines struct {
	buf  []byte
	fn   func([]byte)
	tail *tail
}

func (l *lines) Write(p []byte) (int, error) {
	l.tail.Write(p)
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.fn(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) == 0 {
		l.buf = nil // большая строка не держит память после себя
	}
	return len(p), nil
}

// flush отдаёт последнюю строку без перевода.
func (l *lines) flush() {
	if len(l.buf) > 0 {
		l.fn(l.buf)
		l.buf = nil
	}
}
