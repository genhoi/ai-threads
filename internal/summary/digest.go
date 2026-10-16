package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
)

const (
	// excerptBudget — длина выдержки из переписки одной сессии в файле для агента.
	excerptBudget = 900
	readyMax      = 600 // готовая сводка в файле для агента
	inputFile     = "digest-input.txt"
	journalsDir   = "journals" // переписка текстом для сессий из баз SQLite

	projectsMax = 30
	itemsMax    = 40
	leadMax     = 1500
	itemMax     = 1000
	leftMax     = 1500
)

// Digest — сводка за несколько дней, как её показывает страница «Сводка»: заголовок, вводный
// абзац и по разделу на проект. Текст в Lead, Item.Text и Project.Left — одна строка с разметкой
// `код`, **жирный** и ссылками [текст](ключ сессии); ключи ссылок — только из Item.Keys
// этой сводки или других сессий периода.
type Digest struct {
	Days     int       `json:"days"`
	From     string    `json:"from"`  // первый день периода, ГГГГ-ММ-ДД
	To       string    `json:"to"`    // последний день, день составления
	Title    string    `json:"title"` // «Итоги за 13–15 октября»
	Lead     string    `json:"lead"`
	Projects []Project `json:"projects"`
	Journals int       `json:"journals"` // по скольким сессиям составлена; у сводки первой версии 0
	At       int64     `json:"at"`       // когда составлена, секунды Unix
	Model    string    `json:"model"`    // кто составил: kimi, claude, codex, grok
	Lang     string    `json:"lang"`     // ru или en
	Legacy   bool      `json:"legacy,omitempty"`
}

// Project — раздел сводки.
type Project struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // папка проекта; пусто, если агент назвал проект не из списка
	Items []Item `json:"items"`
	Left  string `json:"left"` // «Осталось.»; пусто — ничего не осталось
}

// Item — пункт раздела: что сделано, и сессии, о которых он.
type Item struct {
	Text string   `json:"text"`
	Keys []string `json:"keys"`
}

// DigestSession — сессия периода для сводки. Отбирает их вызывающий: без служебных, скрытых
// и пустых.
type DigestSession struct {
	Session model.Session
	Source  agents.Agent // агент сессии: по нему читается выдержка
	Title   string       // своё название человека; пусто — из журнала
	Path    string       // папка проекта
	Summary *store.Summary
	Done    bool        // человек отметил завершённой
	State   model.State // ждёт ответа или работает сейчас
}

// DigestInput — что нужно для сводки за несколько дней.
type DigestInput struct {
	Days     int
	Now      time.Time
	Sessions []DigestSession
}

// ValidDays — периоды страницы «Сводка»: 3, 5 дней и неделя.
func ValidDays(days int) bool { return days == 3 || days == 5 || days == 7 }

// Period — первый и последний день периода: с начала дня «сегодня − (days − 1)» по сегодня.
func Period(days int, now time.Time) (from, to time.Time) {
	y, m, d := now.Date()
	return time.Date(y, m, d-(days-1), 0, 0, 0, 0, now.Location()), time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// InPeriod — сессия была активна в период.
func InPeriod(s model.Session, days int, now time.Time) bool {
	from, _ := Period(days, now)
	return !s.Updated.Before(from)
}

// MakeDigest пишет сводку за in.Days дней. Ход работы уходит в job; при отмене ctx возвращает
// ошибку контекста.
func MakeDigest(ctx context.Context, job *Job, run Run, in DigestInput) (Digest, error) {
	if len(in.Sessions) == 0 {
		return Digest{}, &Error{Text: text(run.Lang, "no_sessions", "", "")}
	}
	dir, err := run.workdir(KindDigest)
	if err != nil {
		return Digest{}, err
	}
	from, to := Period(in.Days, in.Now)
	l, err := buildListing(ctx, dir, in, from, to)
	if err != nil {
		if ctx.Err() != nil {
			return Digest{}, ctx.Err()
		}
		return Digest{}, &Error{Text: text(run.Lang, "workdir", "", err.Error())}
	}
	answer, err := run.exec(ctx, job, dir, "digest-agent.md", digestAgent, digestPrompt(in.Days, from, to, run.Lang), l.addDirs)
	if err != nil {
		return Digest{}, err
	}
	d, err := parseDigest(answer, l.names, l.paths)
	if err != nil {
		return Digest{}, &Error{Text: text(run.Lang, "bad_digest", run.name(), ""), Output: clip(answer, tailChars)}
	}
	d.Days, d.From, d.To = in.Days, from.Format(time.DateOnly), to.Format(time.DateOnly)
	d.Title = periodTitle(from, to, run.Lang)
	d.Journals = len(in.Sessions)
	d.At = time.Now().Unix()
	d.Model = run.Runner.ID()
	d.Lang = run.Lang
	return d, nil
}

// listing — файл для агента и то, что нужно для проверки его ответа.
type listing struct {
	addDirs []string
	names   map[string]string // имя проекта в файле → папка
	paths   map[string]string // ключ сессии → папка проекта
}

// buildListing пишет digest-input.txt: сессии по проектам, у каждой готовая сводка, выдержка
// из переписки и путь к журналу. Проекты — по последней активности, внутри — сначала недавние.
func buildListing(ctx context.Context, dir string, in DigestInput, from, to time.Time) (listing, error) {
	l := listing{names: map[string]string{}, paths: map[string]string{}}
	groups := map[string][]DigestSession{}
	var order []string
	for _, s := range in.Sessions {
		if _, ok := groups[s.Path]; !ok {
			order = append(order, s.Path)
		}
		groups[s.Path] = append(groups[s.Path], s)
		l.paths[s.Session.Key()] = s.Path
	}
	latest := func(path string) time.Time {
		var t time.Time
		for _, s := range groups[path] {
			if s.Session.Updated.After(t) {
				t = s.Session.Updated
			}
		}
		return t
	}
	slices.SortStableFunc(order, func(a, b string) int { return latest(b).Compare(latest(a)) })
	base := map[string]int{}
	for _, path := range order {
		base[projectName(path)]++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Период: с %s по %s (%s).\n", from.Format(time.DateOnly), to.Format(time.DateOnly), daysRu(in.Days))
	fmt.Fprintf(&b, "Сессий: %d, проектов: %d.\n", len(in.Sessions), len(order))
	dirs := map[string]bool{}
	for _, path := range order {
		name := projectName(path)
		if base[name] > 1 {
			// Два проекта с одним именем в разных папках: имя с папкой различает их.
			name += " (" + path + ")"
		}
		l.names[name] = path
		fmt.Fprintf(&b, "\n## Проект %s\nПапка: %s\n", name, orNone(path))
		list := groups[path]
		slices.SortStableFunc(list, func(x, y DigestSession) int { return y.Session.Updated.Compare(x.Session.Updated) })
		for _, s := range list {
			if err := ctx.Err(); err != nil {
				return listing{}, err
			}
			journal, err := journalFor(dir, s)
			if err != nil {
				return listing{}, err
			}
			if journal == s.Session.Journal && journal != "" {
				dirs[filepath.Dir(journal)] = true
			}
			writeSession(&b, s, journal)
		}
	}
	for d := range dirs {
		l.addDirs = append(l.addDirs, d)
	}
	slices.Sort(l.addDirs)
	return l, os.WriteFile(filepath.Join(dir, inputFile), []byte(b.String()), 0o600)
}

// journalFor — файл, который агент может прочитать: журнал как есть или переписка текстом.
func journalFor(dir string, s DigestSession) (string, error) {
	if s.Source == nil || s.Source.Info().PlainJournal {
		return s.Session.Journal, nil
	}
	folder := filepath.Join(dir, journalsDir)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(folder, strings.ReplaceAll(s.Session.Key(), ":", "-")+".txt")
	return path, exportTranscript(s.Source, s.Session, path)
}

func writeSession(b *strings.Builder, s DigestSession, journal string) {
	sess := s.Session
	name := sess.Agent
	if s.Source != nil {
		name = s.Source.Info().Name
	}
	title := s.Title
	if title == "" {
		title = sess.Title
	}
	fmt.Fprintf(b, "\n- ключ: %s\n  агент: %s\n  название: %s\n", sess.Key(), name, orNone(oneLine(title, 200)))
	if sess.Branch != "" {
		fmt.Fprintf(b, "  ветка: %s\n", sess.Branch)
	}
	if !sess.Updated.IsZero() {
		fmt.Fprintf(b, "  последняя активность: %s\n", sess.Updated.Local().Format("2006-01-02 15:04"))
	}
	switch s.State {
	case model.StateWaiting:
		b.WriteString("  сейчас: ждёт ответа человека\n")
	case model.StateWorking, model.StateStarting:
		b.WriteString("  сейчас: агент работает\n")
	}
	if s.Done {
		b.WriteString("  человек отметил сессию завершённой\n")
	}
	ready := "нет"
	if s.Summary != nil {
		ready = readyText(*s.Summary)
	}
	fmt.Fprintf(b, "  готовая сводка: %s\n", ready)
	fmt.Fprintf(b, "  выдержка: %s\n", orNone(excerpt(s.Source, sess)))
	size := "файла нет"
	if info, err := os.Stat(journal); err == nil {
		size = strconv.FormatInt(info.Size(), 10) + " байт"
	}
	fmt.Fprintf(b, "  журнал: %s (%s)\n", orNone(journal), size)
}

// readyText — готовая сводка одной строкой, как в первой версии.
func readyText(s store.Summary) string {
	var parts []string
	if t := strings.TrimRight(strings.TrimSpace(s.Title), "."); t != "" {
		parts = append(parts, t+".")
	}
	if s.Summary != "" {
		parts = append(parts, s.Summary)
	}
	if s.NextStep != "" {
		parts = append(parts, "Следующий шаг: "+s.NextStep)
	}
	text := oneLine(strings.Join(parts, " "), readyMax)
	if text == "" {
		return "есть, но пустая"
	}
	return text
}

// excerpt — выдержка из переписки: первый запрос человека и три последних сообщения, коротко.
func excerpt(a agents.Agent, s model.Session) string {
	if a == nil {
		return ""
	}
	messages := a.Excerpt(s)
	if len(messages) == 0 {
		return ""
	}
	var parts []string
	for _, m := range messages {
		if m.Role == "user" {
			if t := oneLine(m.Text, 500); t != "" {
				parts = append(parts, "начало: "+t)
			}
			break
		}
	}
	if len(messages) > 1 {
		var ending []string
		for _, m := range messages[max(0, len(messages)-3):] {
			if t := oneLine(m.Text, 300); t != "" {
				ending = append(ending, m.Role+": "+t)
			}
		}
		if len(ending) > 0 {
			parts = append(parts, "конец: "+strings.Join(ending, " | "))
		}
	}
	return oneLine(strings.Join(parts, " … "), excerptBudget)
}

// digestPrompt — короткий запрос: список сессий агент читает из файла, потому что запрос
// в аргументах у CLI ограничен (у Kimi — 128 КБ).
func digestPrompt(days int, from, to time.Time, lang string) string {
	return fmt.Sprintf("Составь итоги за %s: с %s по %s. Сессии за период с готовыми сводками и выдержками "+
		"лежат в файле %s в текущей папке. Прочитай его первым инструментом Read и действуй по инструкции агента. "+
		"Верни только JSON. %s", daysRu(days), from.Format(time.DateOnly), to.Format(time.DateOnly), inputFile,
		languageLine(lang))
}

var link = regexp.MustCompile(`\[([^\[\]\n]+)\]\(([^()\s]+)\)`)

// inline приводит текст к одной строке и проверяет ссылки: ссылка на неизвестный ключ
// становится простым текстом. Возвращает текст и ключи ссылок.
func inline(s string, keys map[string]string, limit int) (string, []string) {
	s = strings.Join(strings.Fields(s), " ")
	var found []string
	s = link.ReplaceAllStringFunc(s, func(m string) string {
		sub := link.FindStringSubmatch(m)
		if _, ok := keys[sub[2]]; ok {
			found = append(found, sub[2])
			return m
		}
		return sub[1]
	})
	if len([]rune(s)) > limit {
		s = strings.TrimSpace(clip(s, limit)) + "…"
	}
	return s, found
}

// parseDigest проверяет ответ агента. Нужны непустой lead и список projects; в пунктах
// остаются только ключи сессий из списка; проект без пунктов и без «осталось» пропускается.
func parseDigest(answer string, names, paths map[string]string) (Digest, error) {
	data, err := loadsAnswer(answer)
	if err != nil {
		return Digest{}, err
	}
	lead, _ := data["lead"].(string)
	lead, _ = inline(lead, paths, leadMax)
	if lead == "" {
		return Digest{}, fmt.Errorf("no string field lead")
	}
	raw, ok := data["projects"].([]any)
	if !ok {
		return Digest{}, fmt.Errorf("projects is not a list")
	}
	d := Digest{Lead: lead, Projects: []Project{}}
	for _, value := range raw {
		obj, _ := value.(map[string]any)
		name := oneLine(str(obj["name"]), 200)
		if name == "" {
			continue
		}
		p := Project{Name: name, Items: []Item{}}
		items, _ := obj["items"].([]any)
		for _, value := range items {
			item, _ := value.(map[string]any)
			text, linked := inline(str(item["text"]), paths, itemMax)
			if text == "" {
				continue
			}
			keys := []string{}
			list, _ := item["keys"].([]any)
			for _, k := range list {
				if key, ok := k.(string); ok {
					if _, known := paths[key]; known {
						keys = appendNew(keys, key)
					}
				}
			}
			keys = appendNew(keys, linked...)
			p.Items = append(p.Items, Item{Text: text, Keys: keys})
			if len(p.Items) == itemsMax {
				break
			}
		}
		p.Left, _ = inline(str(obj["left"]), paths, leftMax)
		if len(p.Items) == 0 && p.Left == "" {
			continue
		}
		p.Path = resolvePath(p, names, paths)
		d.Projects = append(d.Projects, p)
		if len(d.Projects) == projectsMax {
			break
		}
	}
	if len(d.Projects) == 0 {
		return Digest{}, fmt.Errorf("no projects")
	}
	return d, nil
}

// resolvePath — папка проекта: по имени из файла, а если агент назвал проект иначе — та,
// к которой относится больше всего сессий его пунктов.
func resolvePath(p Project, names, paths map[string]string) string {
	if path, ok := names[p.Name]; ok {
		return path
	}
	for name, path := range names {
		if strings.EqualFold(name, p.Name) {
			return path
		}
	}
	count := map[string]int{}
	for _, item := range p.Items {
		for _, key := range item.Keys {
			if path := paths[key]; path != "" {
				count[path]++
			}
		}
	}
	best, n := "", 0
	for path, c := range count {
		if c > n || c == n && path < best {
			best, n = path, c
		}
	}
	return best
}

// appendNew добавляет значения, которых в списке ещё нет.
func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

func projectName(path string) string {
	if path == "" {
		return "без папки"
	}
	return filepath.Base(path)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "нет"
	}
	return s
}

// daysRu — «3 дня», «5 дней».
func daysRu(n int) string {
	word := "дней"
	switch {
	case n%10 == 1 && n%100 != 11:
		word = "день"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = "дня"
	}
	return strconv.Itoa(n) + " " + word
}

var monthsRu = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа",
	"сентября", "октября", "ноября", "декабря"}

// periodTitle — заголовок сводки: «Итоги за 13–15 октября», «Summary for October 13–15».
func periodTitle(from, to time.Time, lang string) string {
	if lang == "en" {
		switch {
		case from.Year() != to.Year():
			return "Summary for " + from.Format("January 2, 2006") + " – " + to.Format("January 2, 2006")
		case from.Month() != to.Month():
			return "Summary for " + from.Format("January 2") + " – " + to.Format("January 2")
		case from.Day() == to.Day():
			return "Summary for " + to.Format("January 2")
		}
		return fmt.Sprintf("Summary for %s %d–%d", to.Format("January"), from.Day(), to.Day())
	}
	day := func(t time.Time) string { return strconv.Itoa(t.Day()) + " " + monthsRu[t.Month()-1] }
	switch {
	case from.Year() != to.Year():
		return fmt.Sprintf("Итоги за %s %d – %s %d", day(from), from.Year(), day(to), to.Year())
	case from.Month() != to.Month():
		return "Итоги за " + day(from) + " – " + day(to)
	case from.Day() == to.Day():
		return "Итоги за " + day(to)
	}
	return fmt.Sprintf("Итоги за %d–%s", from.Day(), day(to))
}

// Decode — сводка из state.json. Сводку первой версии (поле result) перекладывает в новый
// вид с Legacy: пункты из bullets, «осталось» из tails, заголовок по дате составления.
// paths даёт папку проекта по ключу сессии; nil, если сводки нет.
func Decode(raw json.RawMessage, days int, lang string, paths func(key string) string) (*Digest, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["result"]; ok {
		return legacy(raw, days, lang, paths)
	}
	var d Digest
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	if d.Projects == nil {
		d.Projects = []Project{}
	}
	for i := range d.Projects {
		if d.Projects[i].Items == nil {
			d.Projects[i].Items = []Item{}
		}
		for j := range d.Projects[i].Items {
			if d.Projects[i].Items[j].Keys == nil {
				d.Projects[i].Items[j].Keys = []string{}
			}
		}
	}
	return &d, nil
}

func legacy(raw json.RawMessage, days int, lang string, paths func(string) string) (*Digest, error) {
	var v1 struct {
		At     float64 `json:"at"`
		Model  string  `json:"model"`
		Result struct {
			Lead     string `json:"lead"`
			Projects []struct {
				Name    string `json:"name"`
				Bullets []struct {
					Text string   `json:"text"`
					Keys []string `json:"keys"`
				} `json:"bullets"`
			} `json:"projects"`
			Tails []struct {
				Text string `json:"text"`
				Key  string `json:"key"`
			} `json:"tails"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &v1); err != nil {
		return nil, err
	}
	if paths == nil {
		paths = func(string) string { return "" }
	}
	at := time.Unix(int64(v1.At), 0)
	from, to := Period(days, at)
	d := &Digest{Days: days, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
		Title: periodTitle(from, to, lang), Lead: oneLine(v1.Result.Lead, leadMax), Projects: []Project{},
		At: at.Unix(), Model: v1.Model, Lang: lang, Legacy: true}
	known := map[string]string{}
	for _, p := range v1.Result.Projects {
		project := Project{Name: oneLine(p.Name, 200), Items: []Item{}}
		for _, b := range p.Bullets {
			if t := oneLine(b.Text, itemMax); t != "" {
				keys := slices.DeleteFunc(slices.Clone(b.Keys), func(k string) bool { return k == "" })
				if keys == nil {
					keys = []string{}
				}
				for _, k := range keys {
					known[k] = paths(k)
				}
				project.Items = append(project.Items, Item{Text: t, Keys: keys})
			}
		}
		if len(project.Items) > 0 {
			project.Path = resolvePath(project, nil, known)
			d.Projects = append(d.Projects, project)
		}
	}
	// «Что осталось» первой версии — к проекту, где упомянута та же сессия или та же папка.
	for _, t := range v1.Result.Tails {
		text := oneLine(t.Text, leftMax)
		if text == "" {
			continue
		}
		owner := -1
		for i, p := range d.Projects {
			for _, item := range p.Items {
				if slices.Contains(item.Keys, t.Key) {
					owner = i
				}
			}
			if owner < 0 && p.Path != "" && p.Path == paths(t.Key) {
				owner = i
			}
			if owner >= 0 {
				break
			}
		}
		if owner < 0 {
			continue
		}
		p := &d.Projects[owner]
		p.Left = strings.TrimSpace(p.Left + " " + sentence(text))
	}
	return d, nil
}

// sentence — текст с точкой в конце, если её нет.
func sentence(s string) string {
	if s == "" || strings.ContainsRune(".!?…", []rune(s)[len([]rune(s))-1]) {
		return s
	}
	return s + "."
}
