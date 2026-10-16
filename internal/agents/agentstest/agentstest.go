// Package agentstest собирает папки агентов для тестов: копирует эталоны из testdata/fixtures
// во временную папку и собирает базы ZCode и Cursor Agent. Настоящие ~/.claude и прочие
// папки тесты не видят.
package agentstest

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

const (
	Created = 1790582400 // 2026-09-28T08:00:00Z — начало эталонных сессий
	Updated = 1790589600 // 2026-09-28T10:00:00Z — их последняя активность
	Cwd     = "/home/user/projects/sample app"
	// Secret — ключ шифрования в базе Cursor Agent; в кэш он попасть не должен.
	Secret = "ключ-шифрования-не-показывать"
)

// HomeEnvs — переменные окружения с папками агентов. Тесты очищают их, чтобы окружение
// разработчика не подменило папки.
var HomeEnvs = []string{"CODEX_HOME", "CLAUDE_HOME", "GROK_HOME", "KIMI_CODE_HOME", "ZCODE_HOME", "CURSOR_HOME"}

// SID — UUID эталонной сессии: 1 — ручная, 2 — автоматическая, 3 — пустая, 9 — подагент.
func SID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// Fixtures — папка testdata/fixtures.
func Fixtures() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures")
}

// Homes копирует эталоны во временную папку и возвращает папку каждого агента по id.
// Папки zcode и cursor пустые: базы собирают ZCode и CursorChats.
func Homes(t testing.TB) map[string]string {
	t.Helper()
	for _, name := range HomeEnvs {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(Fixtures())); err != nil {
		t.Fatal(err)
	}
	// CopyFS оставляет файлы только для чтения, а тесты дописывают журналы.
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chmod(path, 0o644)
		}
		return nil
	})
	homes := map[string]string{}
	for _, id := range []string{"codex", "claude", "grok", "kimi", "zcode", "cursor"} {
		homes[id] = filepath.Join(root, id)
	}
	return homes
}

// ZCode собирает базу ZCode из zcode/db.sql и возвращает её путь.
func ZCode(t testing.TB, home string) string {
	t.Helper()
	path := filepath.Join(home, "cli", "db", "db.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(Fixtures(), "zcode", "db.sql"))
	if err != nil {
		t.Fatal(err)
	}
	Exec(t, path, string(script))
	return path
}

// Exec выполняет SQL в базе path.
func Exec(t testing.TB, path, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// Query — сообщение человека в чате Cursor Agent с временем отправки at.
func Query(text, at string) map[string]any {
	text = "<timestamp>" + at + "</timestamp>\n<user_query>\n" + text + "\n</user_query>"
	return map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}
}

// Answer — ответ агента в чате Cursor Agent.
func Answer(text string) map[string]any {
	return map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}
}

// SentAt — время запроса в формате cursor-agent: 2026-09-28T08:01:00Z.
const SentAt = "Monday, Sep 28, 2026, 5:01 PM (UTC+9)"

// Chat — переписка с системным сообщением, служебным контекстом, рассуждением, вызовом
// инструмента и напоминанием; человеку видны запрос и два ответа.
func Chat() []any {
	return []any{
		map[string]any{"role": "system", "content": "Ты помощник для программирования."},
		map[string]any{"role": "user", "content": "<user_info>\nOS Version: linux\nWorkspace Path: " + Cwd + "\n</user_info>\n" +
			"<git_status>Изменений нет</git_status>\n<rules>Правила проекта</rules>"},
		Query("Найди запись в каталоге и проверь её название", SentAt),
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "reasoning", "text": "Рассуждение модели"},
			map[string]any{"type": "text", "text": "Ищу запись."},
			map[string]any{"type": "tool-call", "toolCallId": "call-1", "toolName": "Read", "args": map[string]any{"path": "catalog.json"}},
		}},
		map[string]any{"role": "tool", "content": []any{map[string]any{"type": "tool-result", "toolCallId": "call-1", "result": "Ответ инструмента"}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "<system_reminder>Служебное напоминание</system_reminder>"}}},
		Answer("Запись найдена. Проверка завершена."),
	}
}

// CursorChats собирает четыре чата Cursor Agent: 1 — с базой и служебными сообщениями,
// 2 — с названием и папкой в meta.json и базой в режиме WAL, 3 — пустой без базы,
// 4 — без названия.
func CursorChats(t testing.TB, home string) {
	t.Helper()
	CursorChat(t, home, 1, nil, Chat(), "Проверить каталог", false)
	CursorChat(t, home, 2, map[string]any{"title": "Проверить изменения", "cwd": "/home/user/projects/other app"},
		[]any{Query("Проверь изменения", ""), Answer("Изменений нет.")}, "Название из базы", true)
	CursorChat(t, home, 3, nil, nil, "", false)
	CursorChat(t, home, 4, nil, []any{Query("Собери отчёт по каталогу", SentAt), Answer("Отчёт готов.")}, "", false)
}

// CursorChat — папка чата n с meta.json и, если rows не nil, базой store.db.
func CursorChat(t testing.TB, home string, n int, meta map[string]any, rows []any, name string, wal bool) string {
	t.Helper()
	sum := md5.Sum([]byte(Cwd))
	folder := filepath.Join(home, "chats", hex.EncodeToString(sum[:]), SID(n))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"schemaVersion": 1, "createdAtMs": Created * 1000, "updatedAtMs": Updated * 1000,
		"hasConversation": rows != nil}
	for k, v := range meta {
		data[k] = v
	}
	if err := os.WriteFile(filepath.Join(folder, "meta.json"), marshal(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows != nil {
		CursorStore(t, filepath.Join(folder, "store.db"), name, rows, wal)
	}
	return folder
}

// CursorStore — store.db как у cursor-agent: сообщения в JSON под своим sha256 и корневой
// protobuf, где поле 1 перечисляет ссылки на сообщения. Остальные поля разбор пропускает.
func CursorStore(t testing.TB, path, name string, rows []any, wal bool) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	if wal {
		exec("PRAGMA journal_mode=WAL")
	}
	exec("CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT); CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB);")
	// Сообщение, на которое ссылается не поле 1, в переписку попасть не должно.
	other, otherData := blob(Answer("Ответ из другой ветки"))
	exec("INSERT INTO blobs VALUES (?, ?)", hex.EncodeToString(other), otherData)
	// varint, 64 и 32 бита, ссылка длиной 32 байта в поле 8, вложенные данные в поле 18.
	noise := [][]byte{{0x10, 0x96, 0x01}, append([]byte{0x19}, make([]byte, 8)...), append([]byte{0x15}, make([]byte, 4)...),
		append([]byte{0x42, 0x20}, other...), append([]byte{0x92, 0x01, 0x05}, "hello"...)}
	root := bytes.Clone(noise[0])
	for i, row := range rows {
		digest, data := blob(row)
		exec("INSERT OR IGNORE INTO blobs VALUES (?, ?)", hex.EncodeToString(digest), data)
		root = append(root, 0x0a, 0x20)
		root = append(root, digest...)
		root = append(root, noise[1+i%4]...)
	}
	rootSum := sha256.Sum256(root)
	rootID := hex.EncodeToString(rootSum[:])
	exec("INSERT INTO blobs VALUES (?, ?)", rootID, root)
	exec("INSERT INTO blobs VALUES (?, ?)", hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32)), []byte{0x0a, 0x03, 1, 2, 3})
	meta := map[string]any{"agentId": "agent", "latestRootBlobId": rootID, "name": name, "mode": "default",
		"createdAt": Created * 1000, "blobEncryptionKey": Secret}
	exec("INSERT INTO meta VALUES ('0', ?)", hex.EncodeToString(marshal(meta)))
}

func blob(row any) ([]byte, []byte) {
	data := marshal(row)
	sum := sha256.Sum256(data)
	return sum[:], data
}

func marshal(v any) []byte {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})
}
