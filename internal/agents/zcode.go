package agents

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/genhoi/ai-threads/internal/model"
)

// ZCode: сессии в базе SQLite $ZCODE_HOME/cli/db/db.sqlite.
//
// Таблица session — сессии, message — сообщения с ролью в JSON-поле data, part — части
// сообщений: текст, рассуждения, вызовы инструментов. Продолжить сессию из терминала нельзя.

// Подагенты и прогоны workflow записаны с parent_id — это не отдельные сессии. Ответвления
// и побочные чаты тоже ссылаются на родителя, но их начинает человек.
const (
	zcodeSessions = `SELECT id, title, title_source, directory, time_created, time_updated FROM session
		WHERE parent_id IS NULL OR task_type IN ('fork', 'selection_side_chat')`
	zcodeMessages = `SELECT id, time_created, data FROM message WHERE session_id = ? ORDER BY sequence, time_created, id`
	zcodeParts    = `SELECT data FROM part WHERE message_id = ? ORDER BY sequence, time_created, id`
)

type zcodeAgent struct{ *base }

func newZCode(c Config) Agent {
	return &zcodeAgent{&base{cfg: c, spec: spec{
		Info: Info{ID: "zcode", Name: "ZCode", Short: "ZC", HomeEnv: "ZCODE_HOME", HomeDefault: ".zcode"},
	}}}
}

func (z *zcodeAgent) database() string {
	return filepath.Join(z.Home(), "cli", "db", "db.sqlite")
}

func (z *zcodeAgent) Discover() []string {
	if path := z.database(); isFile(path) {
		return []string{path}
	}
	return nil
}

func (z *zcodeAgent) Scan(c *Cache, units []string, onUnit func([]model.Session)) []model.Session {
	c.init()
	var result []model.Session
	for _, unit := range units {
		sessions := z.read(unit, c)
		result = append(result, sessions...)
		if onUnit != nil {
			onUnit(sessions)
		}
	}
	c.keep(units)
	return result
}

// read — сессии из базы. Пока база и её -wal не менялись, база не открывается. Переписка
// перечитывается только у сессий с новым time_updated. Если база недоступна — сессии из кэша.
func (z *zcodeAgent) read(path string, c *Cache) []model.Session {
	db, wal := stamp(path), stamp(path+"-wal")
	now := []int64{db[0], db[1], wal[0], wal[1]}
	entry := c.Files[path]
	if entry != nil && slices.Equal(entry.Stamp, now) {
		return zcodeSessionsOf(entry)
	}
	var known map[string]*Entry
	if entry != nil {
		known = entry.Sessions
	}
	records, err := z.query(path, known)
	if err != nil {
		if entry == nil {
			return nil
		}
		return zcodeSessionsOf(entry)
	}
	entry = &Entry{Stamp: now, Sessions: records}
	c.Files[path] = entry
	return zcodeSessionsOf(entry)
}

func zcodeSessionsOf(entry *Entry) []model.Session {
	var sessions []model.Session
	for _, id := range slices.Sorted(maps.Keys(entry.Sessions)) {
		sessions = append(sessions, entry.Sessions[id].Session)
	}
	return sessions
}

func (z *zcodeAgent) query(path string, known map[string]*Entry) (map[string]*Entry, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	type sessionRow struct{ id, title, source, dir, created, updated any }
	rows, err := db.Query(zcodeSessions)
	if err != nil {
		return nil, err
	}
	var found []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.title, &r.source, &r.dir, &r.created, &r.updated); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	records := map[string]*Entry{}
	for _, r := range found {
		sid, _ := asString(r.id)
		if sid == "" {
			continue
		}
		rev := fmt.Sprint(r.updated)
		record := &Entry{Rev: rev}
		if old := known[sid]; old != nil && old.Rev == rev {
			record.First, record.Session.Last, record.Session.Empty = old.First, old.Session.Last, old.Session.Empty
		} else if err := zcodeOverview(db, sid, record); err != nil {
			return nil, err
		}
		// Название по умолчанию — заглушка с датой; вместо неё первый запрос.
		own := ""
		if source, _ := asString(r.source); source != "default" {
			title, _ := asString(r.title)
			own = cleanTitle(title)
		}
		cwd, _ := asString(r.dir)
		changed := timestamp(r.updated, 0)
		record.Session.Agent, record.Session.ID = "zcode", sid
		record.Session.Title = firstNonEmpty(own, record.First)
		record.Session.Cwd = cwd
		record.Session.Created = unixTime(timestamp(r.created, changed))
		record.Session.Updated = unixTime(changed)
		record.Session.Journal, record.Session.Meta = path, path
		records[sid] = record
	}
	return records, nil
}

// zcodeOverview — первый запрос, последний ответ и признак пустой переписки.
func zcodeOverview(db *sql.DB, sid string, record *Entry) error {
	candidates, err := zcodeCandidates(db, sid)
	if err != nil {
		return err
	}
	var first, last *textMessage
	for m := range zcodeTexts(db, candidates, false, "user", &err) {
		first = &m
		break
	}
	for m := range zcodeTexts(db, candidates, true, "assistant", &err) {
		last = &m
		break
	}
	if err != nil {
		return err
	}
	if first != nil {
		record.First = cleanTitle(first.text)
	}
	if last != nil {
		record.Session.Last = plainLine(last.text)
	}
	record.Session.Empty = first == nil && last == nil
	return nil
}

// zcodeCandidates — сообщения, которые ZCode показывает человеку, по порядку; текст ещё не прочитан.
func zcodeCandidates(db *sql.DB, sid string) ([]textMessage, error) {
	rows, err := db.Query(zcodeMessages, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []textMessage
	for rows.Next() {
		var id, created, data any
		if err := rows.Scan(&id, &created, &data); err != nil {
			return nil, err
		}
		if role := zcodeVisibleRole(data); role != "" {
			key, _ := asString(id)
			found = append(found, textMessage{key: key, role: role, at: timestamp(created, 0)})
		}
	}
	return found, rows.Err()
}

// zcodeTexts — сообщения с видимым текстом от начала или от конца, только с ролью role, если
// она задана. Части читаются по одному сообщению, поэтому для последних сообщений вся
// переписка не читается.
func zcodeTexts(db *sql.DB, candidates []textMessage, fromEnd bool, role string, errp *error) iter.Seq[textMessage] {
	return func(yield func(textMessage) bool) {
		order := slices.Values(candidates)
		if fromEnd {
			order = backward(candidates)
		}
		for m := range order {
			if role != "" && m.role != role {
				continue
			}
			text, err := zcodeMessageText(db, m.key)
			if err != nil {
				*errp = err
				return
			}
			if text == "" {
				continue
			}
			m.text = text
			if !yield(m) {
				return
			}
		}
	}
}

// zcodeVisibleRole — роль сообщения, которое видит человек. Напоминания, уведомления о фоновых
// задачах, пересказ после сжатия контекста и копия переписки в побочном чате помечены
// synthetic или скрыты из интерфейса.
func zcodeVisibleRole(data any) string {
	info := jsonObject(data)
	if truthy(info["synthetic"]) || info["visibility"] == "model-only" || obj(info["semantics"])["uiVisibility"] == "hidden" {
		return ""
	}
	if role := str(info["role"]); role == "user" || role == "assistant" {
		return role
	}
	return ""
}

func zcodeMessageText(db *sql.DB, mid string) (string, error) {
	rows, err := db.Query(zcodeParts, mid)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var texts []string
	for rows.Next() {
		var data any
		if err := rows.Scan(&data); err != nil {
			return "", err
		}
		part := jsonObject(data)
		if text, ok := part["text"].(string); ok && part["type"] == "text" && !truthy(part["synthetic"]) {
			texts = append(texts, text)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return visibleText(strings.Join(texts, "\n")), nil
}

func (z *zcodeAgent) Transcript(s model.Session, limitBytes int64) ([]model.Message, bool) {
	db, err := openReadOnly(s.Journal)
	if err != nil {
		return nil, false
	}
	defer db.Close()
	candidates, err := zcodeCandidates(db, s.ID)
	if err != nil {
		return nil, false
	}
	messages, truncated := lastMessages(zcodeTexts(db, candidates, true, "", &err), limitBytes)
	if err != nil {
		return nil, false
	}
	return messages, truncated
}

func (z *zcodeAgent) Excerpt(s model.Session) []model.Message {
	db, err := openReadOnly(s.Journal)
	if err != nil {
		return nil
	}
	defer db.Close()
	candidates, err := zcodeCandidates(db, s.ID)
	if err != nil {
		return nil
	}
	messages := edgeMessages(zcodeTexts(db, candidates, false, "", &err), zcodeTexts(db, candidates, true, "", &err))
	if err != nil {
		return nil
	}
	return messages
}

func (z *zcodeAgent) RawText(s model.Session) []byte {
	messages, _ := z.Transcript(s, rawBytes)
	return joinTexts(messages)
}

// jsonObject — объект JSON из значения столбца; пустой, если это не объект.
func jsonObject(data any) map[string]any {
	text, ok := asString(data)
	if !ok {
		return map[string]any{}
	}
	var value map[string]any
	if json.Unmarshal([]byte(text), &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}
