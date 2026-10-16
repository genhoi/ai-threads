package agents

import (
	"bytes"
	"database/sql"
	"fmt"
	"iter"
	"net/url"
	"os"
	"time"

	"github.com/genhoi/ai-threads/internal/model"

	_ "modernc.org/sqlite"
)

// Агенты, которые хранят переписку в SQLite. Базу может держать запущенный агент: ошибки
// чтения и блокировки агент ловит сам и отдаёт то, что есть в кэше, чтобы каталог не падал.

// busyTimeout — сколько ждать, пока база занята записью.
var busyTimeout = time.Second

const (
	maxMessages     = 200 // больше сообщений экран сессии и выгрузка для сводки не показывают
	excerptMessages = 5   // сообщений из начала и из конца переписки в выдержке для сводки
)

// openReadOnly открывает базу только для чтения.
func openReadOnly(path string) (*sql.DB, error) {
	uri := "file:" + (&url.URL{Path: path}).EscapedPath() + "?mode=ro"
	// Даже с mode=ro SQLite у базы в режиме WAL пишет в индекс -shm, а если файла -wal
	// нет — создаёт пустые -wal и -shm. Когда -wal нет или он пуст, вся база лежит в
	// основном файле: её читаем как неизменяемую и рядом ничего не трогаем.
	if walMode(path) && stamp(path + "-wal")[0] <= 0 {
		uri += "&immutable=1"
	}
	uri += fmt.Sprintf("&_busy_timeout=%d", busyTimeout.Milliseconds())
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func walMode(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 20)
	if n, _ := f.Read(header); n < 20 {
		return false
	}
	return bytes.Equal(header[:16], []byte("SQLite format 3\x00")) && header[18] == 2 && header[19] == 2
}

// textMessage — сообщение переписки из базы: ключ для сравнения, роль, текст и время.
type textMessage struct {
	key  string
	role string
	text string
	at   float64
}

// lastMessages — сообщения из found (от конца к началу), не больше maxMessages и около
// limitBytes текста, по порядку. Одно сообщение больше лимита всё равно показывается.
func lastMessages(found iter.Seq[textMessage], limitBytes int64) ([]model.Message, bool) {
	var result []model.Message
	used := int64(0)
	truncated := false
	for m := range found {
		used += int64(len(m.text))
		if len(result) > 0 && (len(result) >= maxMessages || used > limitBytes) {
			truncated = true
			break
		}
		result = append(result, message(m.role, m.text, m.at, 0))
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, truncated
}

// edgeMessages — первые и последние excerptMessages сообщений.
func edgeMessages(forward, backward iter.Seq[textMessage]) []model.Message {
	var head, tail []textMessage
	seen := map[string]bool{}
	for m := range forward {
		if len(head) == excerptMessages {
			break
		}
		head = append(head, m)
		seen[m.key] = true
	}
	taken := 0
	for m := range backward {
		if taken == excerptMessages {
			break
		}
		taken++
		if !seen[m.key] {
			tail = append(tail, m)
		}
	}
	var result []model.Message
	for _, m := range head {
		result = append(result, message(m.role, m.text, m.at, textLimit))
	}
	for i := len(tail) - 1; i >= 0; i-- {
		result = append(result, message(tail[i].role, tail[i].text, tail[i].at, textLimit))
	}
	return result
}

// backward — элементы среза от конца к началу.
func backward[T any](items []T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := len(items) - 1; i >= 0; i-- {
			if !yield(items[i]) {
				return
			}
		}
	}
}

// joinTexts — тексты сообщений одной строкой, для полного поиска.
func joinTexts(messages []model.Message) []byte {
	var buf bytes.Buffer
	for i, m := range messages {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(m.Text)
	}
	return buf.Bytes()
}

// asString — строка из значения столбца SQLite.
func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}
