package agents

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/model"
)

// Cursor Agent (cursor-agent): чаты в $CURSOR_HOME/chats/<md5 от папки>/<id чата>/.
//
// В папке чата meta.json с названием и временем и база store.db. В её таблице meta под
// ключом '0' лежит JSON в шестнадцатеричной записи со ссылкой на корневой blob, в таблице
// blobs — сообщения в JSON и служебные записи в protobuf. Корневой blob перечисляет сообщения
// по порядку: каждое поле 1 длиной 32 байта — sha256 сообщения, он же id в blobs. У пустого
// чата базы может не быть.

var (
	cursorQuery     = regexp.MustCompile(`(?s)<user_query>(.*?)</user_query>`)
	cursorSent      = regexp.MustCompile(`(?s)<timestamp>(.*?)</timestamp>`)
	cursorWorkspace = regexp.MustCompile(`(?m)^` + space + `*Workspace Path:` + space + `*(\S.*?)` + space + `*$`)
	// «Monday, Sep 28, 2026, 5:00 PM (UTC+9)» — так cursor-agent помечает время запроса.
	cursorHumanTime = regexp.MustCompile(`\b([A-Za-z]{3})[A-Za-z]*\.?\s+(\d{1,2}),?\s+(\d{4}),?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?` +
		`\s*([AaPp][Mm])?(\s*\(UTC(?:([+-])(\d{1,2})(?::?(\d{2}))?)?\))?`)
	cursorMonths = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
)

// cursorChunk — больше ссылок за раз в запрос не передаётся: старые SQLite принимают до 999 параметров.
const cursorChunk = 500

type cursorAgent struct{ *base }

func newCursor(c Config) Agent {
	return &cursorAgent{&base{cfg: c, spec: spec{
		Info: Info{ID: "cursor", Name: "Cursor Agent", Short: "Cu", HomeEnv: "CURSOR_HOME", HomeDefault: ".cursor",
			Programs: []string{"cursor-agent"}, SkipFlag: "--force", Resumable: true},
		resume:    []string{"cursor-agent", "--resume", "{id}"},
		promptArg: true,
		choice: func(model, _ string) []string {
			return flags("--model", model)
		},
	}}}
}

func (a *cursorAgent) Discover() []string {
	return glob(a.Home(), "chats/*/*/meta.json")
}

func (a *cursorAgent) Scan(c *Cache, units []string, onUnit func([]model.Session)) []model.Session {
	c.init()
	var result []model.Session
	for _, unit := range units {
		var found []model.Session
		if s, ok := a.session(unit, c); ok {
			found = append(found, s)
			result = append(result, s)
		}
		if onUnit != nil {
			onUnit(found)
		}
	}
	c.keep(units)
	return result
}

// cursorMessage — сообщение чата: номер в переписке, роль, текст и время.
type cursorMessage = textMessage

func (a *cursorAgent) session(meta string, c *Cache) (model.Session, bool) {
	chat := filepath.Base(filepath.Dir(meta))
	store := filepath.Join(filepath.Dir(meta), "store.db")
	m, s, w := stamp(meta), stamp(store), stamp(store+"-wal")
	now := []int64{m[0], m[1], s[0], s[1], w[0], w[1]}
	if !isUUID(chat) || m[0] < 0 {
		return model.Session{}, false
	}
	entry := c.Files[meta]
	if entry == nil || !slices.Equal(entry.Stamp, now) {
		var name, workspace string
		var messages []cursorMessage
		var err error
		if isFile(store) {
			name, messages, workspace, err = cursorReadStore(store)
		}
		if err != nil {
			// База занята или испорчена: прежняя запись или данные из meta.json. В кэш не
			// пишем, чтобы прочитать базу ещё раз при следующем обходе.
			if entry == nil {
				entry = cursorRecord(readJSON(meta), "", nil, "", m)
			}
		} else {
			entry = cursorRecord(readJSON(meta), name, messages, workspace, m)
			entry.Stamp = now
			c.Files[meta] = entry
		}
	}
	session := entry.Session
	session.Agent, session.ID, session.Journal, session.Meta = "cursor", chat, store, meta
	return session, true
}

func cursorRecord(meta map[string]any, name string, messages []cursorMessage, workspace string, metaStamp [2]int64) *Entry {
	first, last := "", ""
	for _, m := range messages {
		if m.role == "user" {
			first = cleanTitle(m.text)
			break
		}
	}
	for _, m := range slices.Backward(messages) {
		if m.role == "assistant" {
			last = plainLine(m.text)
			break
		}
	}
	updated := timestamp(meta["updatedAtMs"], float64(metaStamp[1])/1e9)
	return &Entry{Session: model.Session{
		Title:   firstNonEmpty(cleanTitle(meta["title"]), cleanTitle(name), first),
		Cwd:     firstNonEmpty(str(meta["cwd"]), workspace),
		Created: unixTime(timestamp(meta["createdAtMs"], updated)),
		Updated: unixTime(updated),
		Last:    last,
		Empty:   len(messages) == 0,
	}}
}

// cursorReadStore — название чата, сообщения и папка из служебного сообщения.
func cursorReadStore(store string) (string, []cursorMessage, string, error) {
	db, err := openReadOnly(store)
	if err != nil {
		return "", nil, "", err
	}
	defer db.Close()
	var value any
	if err := db.QueryRow("SELECT value FROM meta WHERE key = '0'").Scan(&value); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", nil, "", err
	}
	name, rootID := cursorStoreMeta(value)
	var refs []string
	if rootID != "" {
		var root any
		err := db.QueryRow("SELECT data FROM blobs WHERE id = ?", rootID).Scan(&root)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", nil, "", err
		}
		if data, ok := root.([]byte); ok {
			refs = cursorMessageRefs(data)
		}
	}
	blobs := map[string][]byte{}
	unique := slices.Compact(slices.Sorted(slices.Values(refs)))
	for start := 0; start < len(unique); start += cursorChunk {
		chunk := unique[start:min(start+cursorChunk, len(unique))]
		args := make([]any, len(chunk))
		for i, ref := range chunk {
			args[i] = ref
		}
		query := "SELECT id, data FROM blobs WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(chunk)), ", ") + ")"
		rows, err := db.Query(query, args...)
		if err != nil {
			return "", nil, "", err
		}
		for rows.Next() {
			var id, data any
			if err := rows.Scan(&id, &data); err != nil {
				rows.Close()
				return "", nil, "", err
			}
			key, _ := asString(id)
			text, _ := asString(data)
			blobs[key] = []byte(text)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", nil, "", err
		}
	}
	var ordered [][]byte
	for _, ref := range refs {
		if data, ok := blobs[ref]; ok {
			ordered = append(ordered, data)
		}
	}
	messages, workspace := cursorConversation(ordered)
	return name, messages, workspace, nil
}

// cursorStoreMeta — название чата и id корневого blob. Остальные поля (среди них бывает ключ
// шифрования) не сохраняются.
func cursorStoreMeta(value any) (string, string) {
	text, ok := asString(value)
	if !ok {
		return "", ""
	}
	raw, err := hex.DecodeString(text)
	if err != nil {
		return "", ""
	}
	var info map[string]any
	if json.Unmarshal(raw, &info) != nil {
		return "", ""
	}
	return str(info["name"]), str(info["latestRootBlobId"])
}

// cursorMessageRefs — id сообщений из корневого blob: поля 1 длиной 32 байта по порядку.
// Разбор protobuf по типу поля: 0 — varint, 1 — 8 байт, 2 — длина и данные, 5 — 4 байта.
func cursorMessageRefs(data []byte) []string {
	var refs []string
	i := 0
	for i < len(data) {
		key, next, ok := varint(data, i)
		if !ok {
			break
		}
		i = next
		switch key & 7 {
		case 0:
			if _, i, ok = varint(data, i); !ok {
				return refs
			}
		case 1:
			i += 8
		case 2:
			size, next, ok := varint(data, i)
			if !ok {
				return refs
			}
			i = next
			end := min(uint64(len(data)), uint64(i)+size)
			if key>>3 == 1 && end-uint64(i) == 32 {
				refs = append(refs, hex.EncodeToString(data[i:end]))
			}
			if uint64(i)+size > uint64(len(data)) {
				return refs
			}
			i += int(size)
		case 5:
			i += 4
		default:
			return refs
		}
	}
	return refs
}

func varint(data []byte, i int) (uint64, int, bool) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if i >= len(data) {
			return 0, i, false
		}
		b := data[i]
		i++
		value |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return value, i, true
		}
	}
	return 0, i, false
}

// cursorConversation — запросы человека и ответы агента. Первое сообщение пользователя —
// служебный контекст без <user_query>: из него берётся только папка.
func cursorConversation(blobs [][]byte) ([]cursorMessage, string) {
	var messages []cursorMessage
	workspace := ""
	for index, data := range blobs {
		var row map[string]any
		if json.Unmarshal(data, &row) != nil || row == nil {
			continue
		}
		var text string
		var at float64
		switch row["role"] {
		case "user":
			raw := contentText(row["content"])
			queries := cursorQuery.FindAllStringSubmatch(raw, -1)
			if len(queries) == 0 {
				if found := cursorWorkspace.FindStringSubmatch(raw); found != nil && workspace == "" {
					workspace = found[1]
				}
				continue
			}
			parts := make([]string, len(queries))
			for i, q := range queries {
				parts[i] = strings.TrimSpace(q[1])
			}
			text = visibleText(strings.Join(parts, "\n\n"))
			if sent := cursorSent.FindStringSubmatch(raw); sent != nil {
				at = cursorSentAt(sent[1])
			}
		case "assistant":
			text = visibleText(row["content"])
		default:
			continue
		}
		if text != "" {
			messages = append(messages, cursorMessage{key: strconv.Itoa(index), role: str(row["role"]), text: text, at: at})
		}
	}
	return messages, workspace
}

// cursorSentAt — время из <timestamp>: «Monday, Sep 28, 2026, 5:00 PM (UTC+9)» или ISO 8601.
// Без пояса время считается местным.
func cursorSentAt(value string) float64 {
	m := cursorHumanTime.FindStringSubmatch(value)
	if m == nil {
		return timestamp(strings.TrimSpace(value), 0)
	}
	month := slices.Index(cursorMonths, strings.ToLower(m[1]))
	if month < 0 {
		return 0
	}
	day, _ := strconv.Atoi(m[2])
	year, _ := strconv.Atoi(m[3])
	hour, _ := strconv.Atoi(m[4])
	minute, _ := strconv.Atoi(m[5])
	second, _ := strconv.Atoi(m[6])
	if m[7] != "" {
		hour = hour % 12
		if strings.ToLower(m[7]) == "pm" {
			hour += 12
		}
	}
	location := time.Local
	if m[8] != "" {
		hours, _ := strconv.Atoi(m[10])
		minutes, _ := strconv.Atoi(m[11])
		offset := hours*3600 + minutes*60
		if offset >= 24*3600 {
			return 0
		}
		if m[9] == "-" {
			offset = -offset
		}
		location = time.FixedZone("", offset)
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0
	}
	moment := time.Date(year, time.Month(month+1), day, hour, minute, second, 0, location)
	if moment.Day() != day || int(moment.Month()) != month+1 {
		return 0
	}
	return seconds(moment)
}

func (a *cursorAgent) messages(s model.Session) []cursorMessage {
	if !isFile(s.Journal) {
		return nil
	}
	_, messages, _, err := cursorReadStore(s.Journal)
	if err != nil {
		return nil
	}
	return messages
}

func (a *cursorAgent) Transcript(s model.Session, limitBytes int64) ([]model.Message, bool) {
	return lastMessages(backward(a.messages(s)), limitBytes)
}

func (a *cursorAgent) Excerpt(s model.Session) []model.Message {
	messages := a.messages(s)
	return edgeMessages(slices.Values(messages), backward(messages))
}

func (a *cursorAgent) RawText(s model.Session) []byte {
	messages, _ := a.Transcript(s, rawBytes)
	return joinTexts(messages)
}
