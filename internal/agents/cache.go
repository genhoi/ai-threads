package agents

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.com/genhoi/ai-threads/internal/model"
)

// CacheVersion меняется вместе с правилами разбора: кэш другой версии читается заново.
const CacheVersion = 1

// Cache — разобранные журналы одного агента между обходами. Каталог хранит его на диске;
// поля — внутреннее дело агента. Нулевое значение готово к работе.
type Cache struct {
	Files   map[string]*Entry // по пути единицы чтения
	Indexes map[string]*Index // дописываемые индексы: история ввода, названия
}

// Entry — разобранная единица чтения.
type Entry struct {
	Stamp   []int64       // размеры и времена изменения файлов, по которым запись верна
	Skip    bool          `json:",omitempty"` // файл — не сессия
	Session model.Session // сессия после последних правок по индексам
	First   string        `json:",omitempty"` // название из первого запроса

	// Для правок по индексам.
	AITitle    string   `json:",omitempty"` // Claude Code: заголовок ai-title из журнала
	JournalCwd string   `json:",omitempty"` // Claude Code: папка из журнала
	Prompts    []string `json:",omitempty"` // Kimi: начала запросов turn.prompt

	// ZCode: в одной базе много сессий.
	Sessions map[string]*Entry `json:",omitempty"`
	Rev      string            `json:",omitempty"` // time_updated сессии ZCode
}

// Index — дописываемый индекс, прочитанный до Offset.
type Index struct {
	Stamp  [2]int64
	Offset int64
	Values map[string]IndexValue
}

// IndexValue — запись индекса: название и папка сессии или отметка, что запрос набран человеком.
type IndexValue struct {
	Title string `json:",omitempty"`
	Cwd   string `json:",omitempty"`
}

func (c *Cache) init() {
	if c.Files == nil {
		c.Files = map[string]*Entry{}
	}
	if c.Indexes == nil {
		c.Indexes = map[string]*Index{}
	}
}

// keep забывает записи о файлах не из units.
func (c *Cache) keep(units []string) {
	set := make(map[string]bool, len(units))
	for _, unit := range units {
		set[unit] = true
	}
	for path := range c.Files {
		if !set[path] {
			delete(c.Files, path)
		}
	}
}

// keepIndexes забывает индексы, которые этот обход не читал.
func (c *Cache) keepIndexes(read map[string]bool) {
	for path := range c.Indexes {
		if !read[path] {
			delete(c.Indexes, path)
		}
	}
}

// updateIndex дочитывает индекс с последней полной строки. apply добавляет строку в values.
// Если файл стал короче, он читается заново.
func (c *Cache) updateIndex(path string, apply func(values map[string]IndexValue, row map[string]any)) map[string]IndexValue {
	c.init()
	now := stamp(path)
	old := c.Indexes[path]
	if old != nil && old.Stamp == now && old.Values != nil {
		return old.Values
	}
	offset := int64(0)
	values := map[string]IndexValue{}
	if old != nil && old.Offset > 0 && old.Offset <= now[0] && now[0] >= old.Stamp[0] {
		offset = old.Offset
		for k, v := range old.Values {
			values[k] = v
		}
	}
	if now[0] >= 0 {
		f, err := os.Open(path)
		if err != nil {
			return values
		}
		defer f.Close()
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return values
		}
		reader := bufio.NewReaderSize(f, 64<<10)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				// Последняя строка без перевода ещё дописывается: прочитается в следующий раз.
				break
			}
			offset += int64(len(line))
			var row map[string]any
			if json.Unmarshal(line, &row) == nil && row != nil {
				apply(values, row)
			}
		}
	}
	c.Indexes[path] = &Index{Stamp: now, Offset: offset, Values: values}
	return values
}
