package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/genhoi/ai-threads/internal/agents/agentstest"
	"github.com/genhoi/ai-threads/internal/model"
)

func zkey(n int) string { return "zcode:sess_" + agentstest.SID(n) }
func ckey(n int) string { return "cursor:" + agentstest.SID(n) }

func zcodeSetup(t *testing.T) (Agent, string) {
	t.Helper()
	homes := agentstest.Homes(t)
	db := agentstest.ZCode(t, homes["zcode"])
	return New("zcode", Config{Home: homes["zcode"]}), db
}

func cursorSetup(t *testing.T) (Agent, string) {
	t.Helper()
	homes := agentstest.Homes(t)
	agentstest.CursorChats(t, homes["cursor"])
	return New("cursor", Config{Home: homes["cursor"]}), homes["cursor"]
}

func names(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var result []string
	for _, e := range entries {
		result = append(result, e.Name())
	}
	return result
}

// lock держит базу занятой: открытая транзакция BEGIN EXCLUSIVE.
func lock(t *testing.T, path string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		conn.Close()
		db.Close()
	}
}

// touch сдвигает время изменения файла, не меняя содержимого.
func touch(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	when := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func shortBusy(t *testing.T) {
	old := busyTimeout
	busyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { busyTimeout = old })
}

func TestZCodeSessions(t *testing.T) {
	zcode, db := zcodeSetup(t)
	if got := zcode.Discover(); !slices.Equal(got, []string{db}) {
		t.Fatalf("discover = %v", got)
	}
	var calls [][]model.Session
	sessions := map[string]model.Session{}
	for _, s := range zcode.Scan(&Cache{}, zcode.Discover(), func(found []model.Session) { calls = append(calls, found) }) {
		sessions[s.Key()] = s
	}
	if len(calls) != 1 || len(calls[0]) != len(sessions) {
		t.Fatalf("calls = %d", len(calls))
	}
	// Подагент 002 и прогон workflow 005 — не отдельные сессии.
	if got := slices.Sorted(maps.Keys(sessions)); !slices.Equal(got, []string{zkey(1), zkey(3), zkey(4)}) {
		t.Fatalf("keys = %v", got)
	}
	manual := sessions[zkey(1)]
	if manual.ID != "sess_"+agentstest.SID(1) || manual.Title != "Проверить каталог" || manual.Cwd != agentstest.Cwd ||
		manual.Branch != "" || manual.Auto || manual.By != "" || manual.Empty || manual.Last != "Запись найдена. Проверка завершена." {
		t.Fatalf("manual = %+v", manual)
	}
	if manual.Created.Unix() != agentstest.Created || manual.Updated.Unix() != agentstest.Updated {
		t.Errorf("created = %v, updated = %v", manual.Created, manual.Updated)
	}
	if manual.Journal != db || manual.Meta != db {
		t.Errorf("journal = %q, meta = %q", manual.Journal, manual.Meta)
	}
	if zcode.Resume(manual) != nil || zcode.Info().Resumable || len(zcode.Launch(LaunchOptions{}).Argv) != 0 {
		t.Error("ZCode не продолжить и не запустить из терминала")
	}
	empty := sessions[zkey(3)]
	if messages, _ := zcode.Transcript(empty, MessageBytes); !empty.Empty || empty.Last != "" || empty.Title != "" || len(messages) != 0 {
		t.Errorf("empty = %+v", empty)
	}
	// Название по умолчанию — заглушка с датой, вместо неё первый запрос.
	if fork := sessions[zkey(4)]; fork.Title != "Продолжи проверку в ответвлении" || fork.Last != "Ответвление проверено." {
		t.Errorf("fork = %+v", fork)
	}
}

func TestZCodeTranscriptHasOnlyVisibleTextInOrder(t *testing.T) {
	zcode, _ := zcodeSetup(t)
	s := scan(zcode, &Cache{})[zkey(1)]
	messages, truncated := zcode.Transcript(s, MessageBytes)
	want := [][2]string{{"user", "Найди запись в каталоге и проверь её название"}, {"assistant", "Запись найдена.\nПроверка завершена."}}
	if !slices.Equal(pairs(messages), want) || truncated {
		t.Fatalf("messages = %v, truncated = %v", pairs(messages), truncated)
	}
	if got := messages[0].At.UTC().Format(time.RFC3339); got != "2026-09-28T08:01:00Z" {
		t.Errorf("at = %s", got)
	}
	if excerpt := zcode.Excerpt(s); !slices.Equal(pairs(excerpt), want) {
		t.Errorf("excerpt = %v", pairs(excerpt))
	}
	if zcode.Info().PlainJournal {
		t.Error("базу SQLite модели как есть не дать")
	}
	raw := string(zcode.RawText(s))
	for _, hidden := range []string{"Служебная вставка", "Напоминание", "Скрытый ответ", "Рассуждение", "Ответ инструмента", "Пересказ"} {
		if strings.Contains(raw, hidden) {
			t.Errorf("%q в тексте сессии", hidden)
		}
	}
}

func TestZCodeHiddenMessages(t *testing.T) {
	cases := map[string]string{
		`{"role":"user"}`: "user",
		`{"role":"assistant","semantics":{"uiVisibility":"visible"}}`: "assistant",
		`{"role":"user","synthetic":true}`:                            "",
		`{"role":"assistant","visibility":"model-only"}`:              "",
		`{"role":"user","semantics":{"uiVisibility":"hidden"}}`:       "",
		`{"role":"system"}`: "",
		"не JSON":           "",
	}
	for data, want := range cases {
		if got := zcodeVisibleRole(data); got != want {
			t.Errorf("role(%s) = %q, want %q", data, got, want)
		}
	}
}

func TestZCodeCacheRereadsOnlyChangedSessions(t *testing.T) {
	zcode, db := zcodeSetup(t)
	cache := &Cache{}
	first := scan(zcode, cache)
	data, _ := json.Marshal(cache)
	restored := &Cache{}
	if err := json.Unmarshal(data, restored); err != nil {
		t.Fatal(err)
	}
	if again := scan(zcode, restored); !sameSessions(again, first) {
		t.Fatal("после кэша сессии другие")
	}
	// Текст ответа поменялся без нового time_updated: переписка не перечитывается.
	agentstest.Exec(t, db, `UPDATE part SET data = '{"type":"text","text":"Подменённый ответ"}' WHERE id = 'prt_04'`)
	// Новое название без новых сообщений видно сразу.
	agentstest.Exec(t, db, "UPDATE session SET title = 'Проверить каталог еще раз' WHERE id = ?", "sess_"+agentstest.SID(1))
	got := scan(zcode, cache)[zkey(1)]
	if got.Title != "Проверить каталог еще раз" || got.Last != "Запись найдена. Проверка завершена." {
		t.Fatalf("session = %+v", got)
	}
	fork := "sess_" + agentstest.SID(4)
	agentstest.Exec(t, db, "INSERT INTO message VALUES ('msg_d4', ?, 1790590000000, 1790590000000, ?, 4)", fork, `{"role":"assistant"}`)
	agentstest.Exec(t, db, "INSERT INTO part VALUES ('prt_d4', 'msg_d4', ?, 1790590000000, 1790590000000, ?, 1)", fork,
		`{"type":"text","text":"Новый ответ"}`)
	agentstest.Exec(t, db, "UPDATE session SET time_updated = 1790590000000 WHERE id = ?", fork)
	changed := scan(zcode, cache)
	if changed[zkey(4)].Last != "Новый ответ" || changed[zkey(4)].Updated.Unix() != 1790590000 {
		t.Fatalf("fork = %+v", changed[zkey(4)])
	}
	if changed[zkey(1)].Last != "Запись найдена. Проверка завершена." {
		t.Fatalf("сессия 1 перечитана: %q", changed[zkey(1)].Last)
	}
}

func TestZCodeBusyOrBrokenDatabase(t *testing.T) {
	shortBusy(t)
	zcode, db := zcodeSetup(t)
	cache := &Cache{}
	first := scan(zcode, cache)
	saved, _ := json.Marshal(cache)
	release := lock(t, db)
	touch(t, db)
	var calls int
	sessions := map[string]model.Session{}
	for _, s := range zcode.Scan(cache, zcode.Discover(), func([]model.Session) { calls++ }) {
		sessions[s.Key()] = s
	}
	if !sameSessions(sessions, first) || calls != 1 {
		t.Fatalf("занятая база: %v", sessions)
	}
	if now, _ := json.Marshal(cache); string(now) != string(saved) {
		t.Fatal("кэш изменился, хотя база не прочитана")
	}
	if len(scan(zcode, &Cache{})) != 0 {
		t.Fatal("без кэша сессий быть не должно")
	}
	if messages, truncated := zcode.Transcript(first[zkey(1)], MessageBytes); messages != nil || truncated {
		t.Fatal("переписка из занятой базы")
	}
	if zcode.Excerpt(first[zkey(1)]) != nil {
		t.Fatal("выдержка из занятой базы")
	}
	release()
	if err := os.WriteFile(db, []byte(strings.Repeat("not a database ", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !sameSessions(scan(zcode, cache), first) || len(scan(zcode, &Cache{})) != 0 {
		t.Fatal("испорченная база")
	}
}

func TestZCodeReadsWALWithoutWritingNextToDatabase(t *testing.T) {
	zcode, db := zcodeSetup(t)
	agentstest.Exec(t, db, "PRAGMA journal_mode=WAL")
	folder := filepath.Dir(db)
	if got := names(folder); !slices.Equal(got, []string{"db.sqlite"}) {
		t.Fatalf("files = %v", got)
	}
	if len(scan(zcode, &Cache{})) != 3 {
		t.Fatal("сессии не прочитаны")
	}
	if got := names(folder); !slices.Equal(got, []string{"db.sqlite"}) {
		t.Fatalf("рядом с базой появились файлы: %v", got)
	}
	// Приложение запущено: свежая запись лежит в -wal и ещё не перенесена в основной файл.
	writer, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec("UPDATE session SET title = 'Название из журнала', time_updated = time_updated + 1 WHERE id = ?",
		"sess_"+agentstest.SID(1)); err != nil {
		t.Fatal(err)
	}
	if stamp(db + "-wal")[0] <= 0 {
		t.Fatal("-wal пуст")
	}
	if got := scan(zcode, &Cache{})[zkey(1)].Title; got != "Название из журнала" {
		t.Fatalf("title = %q", got)
	}
}

func TestCursorSessions(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cursor, _ := cursorSetup(t)
	var calls int
	sessions := map[string]model.Session{}
	for _, s := range cursor.Scan(&Cache{}, cursor.Discover(), func([]model.Session) { calls++ }) {
		sessions[s.Key()] = s
	}
	if len(cursor.Discover()) != 4 || calls != 4 {
		t.Fatalf("discover = %d, calls = %d", len(cursor.Discover()), calls)
	}
	if got := slices.Sorted(maps.Keys(sessions)); !slices.Equal(got, []string{ckey(1), ckey(2), ckey(3), ckey(4)}) {
		t.Fatalf("keys = %v", got)
	}
	manual := sessions[ckey(1)]
	folder := filepath.Dir(manual.Meta)
	if manual.ID != agentstest.SID(1) || manual.Title != "Проверить каталог" || manual.Cwd != agentstest.Cwd || manual.Branch != "" ||
		manual.Auto || manual.Empty || manual.Last != "Запись найдена. Проверка завершена." {
		t.Fatalf("manual = %+v", manual)
	}
	if manual.Created.Unix() != agentstest.Created || manual.Updated.Unix() != agentstest.Updated {
		t.Errorf("created = %v, updated = %v", manual.Created, manual.Updated)
	}
	if manual.Journal != filepath.Join(folder, "store.db") || manual.Meta != filepath.Join(folder, "meta.json") {
		t.Errorf("journal = %q, meta = %q", manual.Journal, manual.Meta)
	}
	if got := cursor.Resume(manual); !slices.Equal(got, []string{"cursor-agent", "--resume", agentstest.SID(1)}) {
		t.Errorf("resume = %q", got)
	}
	titled := sessions[ckey(2)]
	if titled.Title != "Проверить изменения" || titled.Cwd != "/home/user/projects/other app" || titled.Last != "Изменений нет." {
		t.Errorf("titled = %+v", titled)
	}
	// База в режиме WAL прочитана, рядом не появилось ни -wal, ни -shm.
	if got := names(filepath.Dir(titled.Meta)); !slices.Equal(got, []string{"meta.json", "store.db"}) {
		t.Errorf("files = %v", got)
	}
	empty := sessions[ckey(3)]
	if messages, _ := cursor.Transcript(empty, MessageBytes); !empty.Empty || empty.Title != "" || empty.Cwd != "" || empty.Last != "" || len(messages) != 0 {
		t.Errorf("empty = %+v", empty)
	}
	if sessions[ckey(4)].Title != "Собери отчёт по каталогу" {
		t.Errorf("title = %q", sessions[ckey(4)].Title)
	}
}

func TestCursorTranscriptHasOnlyQueriesAndAnswers(t *testing.T) {
	cursor, _ := cursorSetup(t)
	sessions := scan(cursor, &Cache{})
	messages, truncated := cursor.Transcript(sessions[ckey(1)], MessageBytes)
	want := [][2]string{{"user", "Найди запись в каталоге и проверь её название"}, {"assistant", "Ищу запись."},
		{"assistant", "Запись найдена. Проверка завершена."}}
	if !slices.Equal(pairs(messages), want) || truncated {
		t.Fatalf("messages = %v", pairs(messages))
	}
	if got := messages[0].At.UTC().Format(time.RFC3339); got != "2026-09-28T08:01:00Z" || !messages[1].At.IsZero() {
		t.Errorf("at = %s, %v", got, messages[1].At)
	}
	if excerpt := cursor.Excerpt(sessions[ckey(1)]); !slices.Equal(pairs(excerpt), want) {
		t.Errorf("excerpt = %v", pairs(excerpt))
	}
	second, _ := cursor.Transcript(sessions[ckey(2)], MessageBytes)
	if second[0] != (model.Message{Role: "user", Text: "Проверь изменения"}) {
		t.Errorf("first = %+v", second[0])
	}
}

func TestCursorCacheRereadsOnlyChangedChats(t *testing.T) {
	cursor, _ := cursorSetup(t)
	cache := &Cache{}
	first := scan(cursor, cache)
	data, _ := json.Marshal(cache)
	if strings.Contains(string(data), agentstest.Secret) || strings.Contains(string(data), "blobEncryptionKey") {
		t.Fatal("ключ шифрования в кэше")
	}
	restored := &Cache{}
	if err := json.Unmarshal(data, restored); err != nil {
		t.Fatal(err)
	}
	if !sameSessions(scan(cursor, restored), first) {
		t.Fatal("после кэша сессии другие")
	}
	restored.Files[first[ckey(1)].Meta].Session.Last = "из кэша"
	store := first[ckey(4)].Journal
	if err := os.Remove(store); err != nil {
		t.Fatal(err)
	}
	agentstest.CursorStore(t, store, "", []any{agentstest.Query("Собери отчёт по каталогу", agentstest.SentAt),
		agentstest.Answer("Отчёт готов."), agentstest.Answer("Новый ответ")}, false)
	changed := scan(cursor, restored)
	if changed[ckey(4)].Last != "Новый ответ" || changed[ckey(1)].Last != "из кэша" {
		t.Fatalf("4: %q, 1: %q", changed[ckey(4)].Last, changed[ckey(1)].Last)
	}
}

func TestCursorBusyOrBrokenStore(t *testing.T) {
	shortBusy(t)
	cursor, _ := cursorSetup(t)
	cache := &Cache{}
	first := scan(cursor, cache)
	s := first[ckey(1)]
	release := lock(t, s.Journal)
	// Без кэша остаются данные из meta.json.
	alone := scan(cursor, &Cache{})
	if len(alone) != len(first) || alone[ckey(1)].Updated.Unix() != agentstest.Updated {
		t.Fatalf("alone = %v", alone)
	}
	// Чат изменился, пока база занята: остаётся прежняя запись кэша.
	touch(t, s.Meta)
	cached := scan(cursor, cache)[ckey(1)]
	if cached.Title != "Проверить каталог" || cached.Empty {
		t.Fatalf("cached = %+v", cached)
	}
	if messages, truncated := cursor.Transcript(s, MessageBytes); messages != nil || truncated {
		t.Fatal("переписка из занятой базы")
	}
	if cursor.Excerpt(s) != nil {
		t.Fatal("выдержка из занятой базы")
	}
	release()
	// Блокировка снята — база читается снова.
	if got := scan(cursor, cache)[ckey(1)].Last; got != s.Last {
		t.Fatalf("last = %q", got)
	}
	if err := os.WriteFile(s.Journal, []byte(strings.Repeat("not a database ", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := scan(cursor, cache)[ckey(1)]; got.Title != "Проверить каталог" {
		t.Fatalf("title = %q", got.Title)
	}
	if !scan(cursor, &Cache{})[ckey(1)].Empty {
		t.Fatal("испорченная база без кэша — пустой чат")
	}
}

func TestCursorSentAt(t *testing.T) {
	cases := map[string]float64{
		"Monday, Sep 28, 2026, 5:01 PM (UTC+9)": agentstest.Created + 60,
		"Sep 28, 2026, 8:01 AM (UTC)":           agentstest.Created + 60,
		"Mon Sep 28 2026 03:31:00 (UTC-04:30)":  agentstest.Created + 60,
		"2026-09-28T08:01:00Z":                  agentstest.Created + 60,
		"":                                      0,
		"Feb 30, 2026, 1:00 PM (UTC)":           0,
		"Xyz 28, 2026, 1:00 PM (UTC)":           0,
		"Sep 28, 2026, 1:00 PM (UTC+25)":        0,
	}
	for in, want := range cases {
		if got := cursorSentAt(in); got != want {
			t.Errorf("sentAt(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLastMessagesLimits(t *testing.T) {
	var items []textMessage
	for n := range maxMessages + 50 {
		items = append(items, textMessage{key: fmt.Sprint(n), role: "assistant", text: fmt.Sprintf("ответ %d", n)})
	}
	rows, truncated := lastMessages(backward(items), 1<<20)
	if !truncated || len(rows) != maxMessages || rows[0].Text != "ответ 50" || rows[len(rows)-1].Text != fmt.Sprintf("ответ %d", maxMessages+49) {
		t.Fatalf("len = %d, truncated = %v", len(rows), truncated)
	}
	rows, truncated = lastMessages(backward(items[:3]), 1<<20)
	if truncated || len(rows) != 3 || rows[0].Text != "ответ 0" {
		t.Fatalf("rows = %v", rows)
	}
	rows, truncated = lastMessages(backward(items[:3]), int64(len("ответ 2"))*2)
	if !truncated || len(rows) != 2 || rows[0].Text != "ответ 1" {
		t.Fatalf("rows = %v", rows)
	}
	// Одно сообщение больше лимита всё равно показывается.
	rows, truncated = lastMessages(backward([]textMessage{{role: "user", text: strings.Repeat("x", 100)}}), 10)
	if truncated || len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
}

func TestCursorMessageRefsSurviveDamagedRoot(t *testing.T) {
	ref := make([]byte, 32)
	ref[0] = 1
	data := append([]byte{0x0a, 0x20}, ref...)
	// Обрезанное поле после ссылки не мешает прочитать её.
	refs := cursorMessageRefs(append(data, 0x0a, 0x20, 1, 2))
	if len(refs) != 1 || !strings.HasPrefix(refs[0], "01") {
		t.Fatalf("refs = %v", refs)
	}
	if refs := cursorMessageRefs([]byte{0xff, 0xff}); refs != nil {
		t.Fatalf("refs = %v", refs)
	}
}
