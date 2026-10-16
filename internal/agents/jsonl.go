package agents

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/genhoi/ai-threads/internal/model"
)

// jsonlAgent — агент, у которого переписка каждой сессии — файл построчного JSON.
type jsonlAgent struct {
	*base
	discover func(home string) []string
	// journal — файл переписки по единице чтения; nil — сама единица.
	journal func(unit string) string
	// message — роль, содержимое и время из строки журнала; ok=false — не сообщение.
	message func(row map[string]any) (role string, value any, at any, ok bool)
	// info — данные сессии до разбора переписки; ok=false — файл не сессия. head читает
	// начало журнала один раз на разбор: его потом разбирает и parse.
	info func(unit string, head func() []map[string]any) (map[string]any, bool)
	// describe дополняет сессию данными агента.
	describe func(p *parsed)
	validID  func(id string) bool
	// indexes читает индексы, нужные finish.
	indexes func(home string, c *Cache) map[string]IndexValue
	// finish — последние правки сессии по индексам: название, папка, автозапуск.
	finish func(s *model.Session, e *Entry, index map[string]IndexValue)
}

// parsed — сессия во время разбора журнала; время — в секундах, как в журналах.
type parsed struct {
	unit             string
	info             map[string]any
	head, rows       []map[string]any
	id, title        string
	cwd, branch, by  string
	created, updated float64
	auto             bool
	entry            *Entry
}

func (a *jsonlAgent) journalOf(unit string) string {
	if a.journal == nil {
		return unit
	}
	return a.journal(unit)
}

func (a *jsonlAgent) Discover() []string {
	return a.discover(a.Home())
}

// parseWorkers — сколько журналов разбирается одновременно. Разбор JSON упирается в
// процессор, а при первом обходе журналов сотни.
const parseWorkers = 4

// unitState — единица чтения во время обхода.
type unitState struct {
	unit, journal     string
	unitStamp, jStamp [2]int64
	entry             *Entry
	parsed            chan struct{} // закрывается, когда разбор готов; nil — разбор не нужен
}

func (a *jsonlAgent) Scan(c *Cache, units []string, onUnit func([]model.Session)) []model.Session {
	c.init()
	var index map[string]IndexValue
	if a.indexes != nil {
		index = a.indexes(a.Home(), c)
	}
	states := make([]unitState, len(units))
	for i, unit := range units {
		st := &states[i]
		st.unit, st.journal = unit, a.journalOf(unit)
		st.unitStamp = stamp(unit)
		st.jStamp = st.unitStamp
		if st.journal != unit {
			st.jStamp = stamp(st.journal)
		}
		if entry := c.Files[unit]; entry != nil && slices.Equal(entry.Stamp, st.now()) {
			st.entry = entry
		} else if st.unitStamp[0] >= 0 {
			st.parsed = make(chan struct{})
		}
	}
	// Изменившиеся файлы разбираются параллельно и по порядку, а сессии выдаются в прежнем порядке.
	go func() {
		slots := make(chan struct{}, parseWorkers)
		for i := range states {
			st := &states[i]
			if st.parsed == nil {
				continue
			}
			slots <- struct{}{}
			go func() {
				defer func() { <-slots; close(st.parsed) }()
				st.entry = a.parse(st.unit, st.journal, st.unitStamp, st.jStamp)
				st.entry.Stamp = st.now()
			}()
		}
	}()
	var result []model.Session
	for i := range states {
		st := &states[i]
		if st.parsed != nil {
			<-st.parsed
		}
		var found []model.Session
		if st.entry != nil {
			if !st.entry.Skip {
				s := st.entry.Session
				s.Journal, s.Meta = st.journal, st.unit
				if s.Branch == "HEAD" {
					// Отсоединённый HEAD — не ветка; в строке сессии он только мешает.
					s.Branch = ""
				}
				if a.finish != nil {
					a.finish(&s, st.entry, index)
				}
				st.entry.Session = s
				found = append(found, s)
				result = append(result, s)
			}
			c.Files[st.unit] = st.entry
		}
		if onUnit != nil {
			onUnit(found)
		}
	}
	c.keep(units)
	return result
}

func (st *unitState) now() []int64 {
	return []int64{st.unitStamp[0], st.unitStamp[1], st.jStamp[0], st.jStamp[1]}
}

// parse разбирает начало и конец журнала. Без времени в журнале сессия датируется по
// времени изменения единицы чтения.
func (a *jsonlAgent) parse(unit, journal string, unitStamp, journalStamp [2]int64) *Entry {
	var head []map[string]any
	read := false
	readHead := func() []map[string]any {
		if !read {
			head, read = jsonRows(journal, headBytes, false), true
		}
		return head
	}
	info, ok := a.info(unit, readHead)
	if !ok {
		return &Entry{Skip: true}
	}
	rows := readHead()
	if journalStamp[0] > headBytes {
		rows = slices.Concat(head, jsonRows(journal, tailBytes, true))
	}
	messages := a.messages(rows, 0)
	first := ""
	for _, m := range messages {
		if m.Role == "user" {
			first = cleanTitle(m.Text)
			break
		}
	}
	var times []float64
	for _, row := range rows {
		if t := timestamp(or(row["timestamp"], row["time"]), 0); t != 0 {
			times = append(times, t)
		}
	}
	updated := float64(unitStamp[1]) / 1e9
	created := updated
	if len(times) > 0 {
		updated, created = slices.Max(times), slices.Min(times)
	}
	entry := &Entry{First: first}
	p := &parsed{unit: unit, info: info, head: head, rows: rows, id: stem(unit), title: first,
		created: created, updated: updated, entry: entry}
	if a.describe != nil {
		a.describe(p)
	}
	if !a.validID(p.id) {
		return &Entry{Skip: true}
	}
	last := ""
	for _, m := range slices.Backward(messages) {
		if m.Role == "assistant" {
			last = plainLine(m.Text)
			break
		}
	}
	entry.Session = model.Session{Agent: a.ID(), ID: p.id, Title: p.title, Cwd: p.cwd, Branch: p.branch,
		Created: unixTime(p.created), Updated: unixTime(p.updated), Auto: p.auto, By: p.by,
		Last: last, Empty: len(messages) == 0, Journal: journal, Meta: unit}
	return entry
}

// messages — сообщения человека и агента из строк журнала; limit > 0 обрезает длинные.
func (a *jsonlAgent) messages(rows []map[string]any, limit int) []model.Message {
	var result []model.Message
	for _, row := range rows {
		role, value, at, ok := a.message(row)
		if !ok || role != "user" && role != "assistant" {
			continue
		}
		if text := visibleText(value); text != "" {
			result = append(result, message(role, text, timestamp(at, 0), limit))
		}
	}
	return result
}

func (a *jsonlAgent) Transcript(s model.Session, limitBytes int64) ([]model.Message, bool) {
	messages := a.messages(jsonRows(s.Journal, limitBytes, true), 0)
	return messages, stamp(s.Journal)[0] > limitBytes
}

func (a *jsonlAgent) Excerpt(s model.Session) []model.Message {
	rows := jsonRows(s.Journal, headBytes, false)
	if stamp(s.Journal)[0] > headBytes {
		rows = append(rows, jsonRows(s.Journal, tailBytes, true)...)
	}
	return a.messages(rows, textLimit)
}

func (a *jsonlAgent) RawText(s model.Session) []byte {
	f, err := os.Open(s.Journal)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	start := max(0, info.Size()-rawBytes)
	buf := make([]byte, info.Size()-start)
	n, _ := f.ReadAt(buf, start)
	return buf[:n]
}

// stem — имя файла без расширения.
func stem(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// glob — файлы по шаблону от папки агента, по порядку.
func glob(home, pattern string) []string {
	found, _ := filepath.Glob(filepath.Join(home, pattern))
	slices.Sort(found)
	return found
}

// walk — файлы с именем по шаблону name во всех подпапках root (как «root/**/name»), по порядку.
func walk(root, name string) []string {
	var found []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() || d.Type()&fs.ModeSymlink != 0 {
			if ok, _ := filepath.Match(name, d.Name()); ok {
				found = append(found, path)
			}
		}
		return nil
	})
	slices.Sort(found)
	return found
}
