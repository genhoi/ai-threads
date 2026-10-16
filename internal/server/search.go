package server

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/store"
)

const (
	queryMax     = 500
	quickResults = 200
	// convText — больше этого текста переписки одной сессии быстрый поиск не держит в памяти.
	convText    = 256 << 10
	convWorkers = 8
	// fullResults — столько сессий показывает полный поиск, как в первой версии.
	fullResults  = 100
	fullWorkers  = 2 // журналы бывают по сотне мегабайт: больше двух сразу — много памяти
	snippetChars = 220
	quickSnippet = 160
)

// --- Быстрый поиск ---

// handleSearch — быстрый поиск: запрос целиком, без учёта регистра, по названию, своему
// названию, проекту, ветке, папке, ID и сводке, а с conv=1 — и по переписке. Как в первой
// версии, без service=1 служебные и скрытые сессии не ищутся. Сначала недавние.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	if utf8.RuneCountInString(query) > queryMax {
		return apiErr(http.StatusBadRequest, "bad_query")
	}
	hits := []SearchHit{}
	if query == "" {
		writeJSON(w, http.StatusOK, hits)
		return nil
	}
	needle := strings.ToLower(query)
	conv, service := q.Get("conv") == "1", q.Get("service") == "1"
	c := s.viewCtx()
	var rest []model.Session
	for _, sess := range s.sessions() {
		m := c.marks.Marks(sess.Key())
		if !service && (m.Hidden || c.service(sess)) {
			continue
		}
		if field, snippet := matchFields(sess, m, needle); field != "" {
			hits = append(hits, SearchHit{Session: c.view(sess), Field: field, Snippet: snippet})
		} else if conv && !sess.Empty && sess.Journal != "" {
			rest = append(rest, sess)
		}
	}
	if conv && len(rest) > 0 {
		found := s.conv.search(r.Context(), s.allAgents(), rest, needle)
		if r.Context().Err() != nil {
			return nil // страница уже ждёт другой запрос
		}
		for _, sess := range rest {
			if snippet, ok := found[sess.Key()]; ok {
				hits = append(hits, SearchHit{Session: c.view(sess), Field: "conversation", Snippet: snippet})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b SearchHit) int { return int(sign(b.Session.Updated - a.Session.Updated)) })
	if len(hits) > quickResults {
		hits = hits[:quickResults]
	}
	writeJSON(w, http.StatusOK, hits)
	return nil
}

func sign(n int64) int64 {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// matchFields — первое поле сессии, где есть запрос, и фрагмент вокруг совпадения.
func matchFields(sess model.Session, m store.Marks, needle string) (string, string) {
	title, original := sess.Title, ""
	if m.Name != "" {
		title, original = m.Name, sess.Title
	}
	fields := [][2]string{{"title", title}, {"original", original}, {"project", sess.Project()},
		{"branch", sess.Branch}, {"path", sess.Cwd}, {"id", sess.ID}}
	if m.Summary != nil {
		fields = append(fields, [2]string{"summary", m.Summary.Title + " " + m.Summary.Summary + " " + m.Summary.NextStep})
	}
	for _, f := range fields {
		if snippet, ok := around(f[1], needle, 50, quickSnippet); ok {
			return f[0], snippet
		}
	}
	return "", ""
}

// around — фрагмент текста вокруг первого совпадения needle (уже в нижнем регистре):
// before символов до него, всего не больше width, одной строкой.
func around(text, needle string, before, width int) (string, bool) {
	if text == "" {
		return "", false
	}
	lower := strings.ToLower(text)
	at := strings.Index(lower, needle)
	if at < 0 {
		return "", false
	}
	return aroundAt(text, lower, at, before, width)
}

// convCache — текст переписки сессий для быстрого поиска. Запись верна, пока не изменились
// журнал и время сессии: у баз в режиме WAL время из каталога меняется раньше файла.
type convCache struct {
	mu    sync.Mutex
	items map[string]convItem
}

type convItem struct {
	stamp        [3]int64
	text, lowerT string
}

func newConvCache() *convCache { return &convCache{items: map[string]convItem{}} }

func journalStamp(sess model.Session) [3]int64 {
	info, err := os.Stat(sess.Journal)
	if err != nil {
		return [3]int64{-1, -1, sess.Updated.UnixNano()}
	}
	return [3]int64{info.Size(), info.ModTime().UnixNano(), sess.Updated.UnixNano()}
}

func (c *convCache) get(a agents.Agent, sess model.Session) convItem {
	key, stamp := sess.Key(), journalStamp(sess)
	c.mu.Lock()
	item, ok := c.items[key]
	c.mu.Unlock()
	if ok && item.stamp == stamp {
		return item
	}
	messages, _ := a.Transcript(sess, agents.SearchBytes)
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(m.Text)
		b.WriteByte('\n')
	}
	text := b.String()
	if len(text) > convText {
		// Остаётся конец переписки: свежие сообщения ищут чаще.
		text = strings.ToValidUTF8(text[len(text)-convText:], "")
	}
	item = convItem{stamp: stamp, text: text, lowerT: strings.ToLower(text)}
	c.mu.Lock()
	c.items[key] = item
	c.mu.Unlock()
	return item
}

// search ищет needle в переписке сессий и возвращает фрагменты по ключам. Отменённый ctx
// останавливает поиск: страница уже ждёт ответа на другой запрос.
func (c *convCache) search(ctx context.Context, list []agents.Agent, pool []model.Session, needle string) map[string]string {
	byID := map[string]agents.Agent{}
	for _, a := range list {
		byID[a.ID()] = a
	}
	found := map[string]string{}
	var mu sync.Mutex
	work := make(chan model.Session)
	var wg sync.WaitGroup
	for range convWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sess := range work {
				a := byID[sess.Agent]
				if a == nil || ctx.Err() != nil {
					continue
				}
				item := c.get(a, sess)
				if at := strings.Index(item.lowerT, needle); at >= 0 {
					snippet := conversationSnippet(item, at)
					mu.Lock()
					found[sess.Key()] = snippet
					mu.Unlock()
				}
			}
		}()
	}
	for _, sess := range pool {
		if ctx.Err() != nil {
			break
		}
		work <- sess
	}
	close(work)
	wg.Wait()
	c.prune(pool)
	return found
}

// conversationSnippet — 160 символов вокруг совпадения, как в первой версии: 50 до него.
func conversationSnippet(item convItem, at int) string {
	src := item.text
	if len(item.lowerT) != len(item.text) {
		src = item.lowerT
	}
	start := max(0, at-50)
	for start > 0 && !utf8.RuneStart(src[start]) {
		start--
	}
	end := min(len(src), start+quickSnippet*2)
	text := strings.ToValidUTF8(src[start:end], "")
	if r := []rune(text); len(r) > quickSnippet {
		text = string(r[:quickSnippet])
	}
	return strings.Join(strings.Fields(text), " ")
}

// prune забывает переписку сессий, которых больше нет.
func (c *convCache) prune(pool []model.Session) {
	if len(pool) == 0 {
		return
	}
	keep := make(map[string]bool, len(pool))
	for _, sess := range pool {
		keep[sess.Key()] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) <= len(keep)*2 {
		return
	}
	for key := range c.items {
		if !keep[key] {
			delete(c.items, key)
		}
	}
}

// --- Полный поиск ---

// fullSearch — идущий полный поиск. Новый запрос останавливает прежний.
type fullSearch struct {
	mu   sync.Mutex
	n    int
	stop context.CancelFunc
}

func (f *fullSearch) start() (string, context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stop != nil {
		f.stop()
	}
	f.n++
	ctx, cancel := context.WithCancel(context.Background())
	f.stop = cancel
	return "full:" + strconv.Itoa(f.n), ctx
}

func (f *fullSearch) cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stop != nil {
		f.stop()
		f.stop = nil
	}
}

func (s *Server) handleFullSearch(w http.ResponseWriter, r *http.Request) error {
	var req FullSearchRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	query := strings.Join(strings.Fields(req.Q), " ")
	if query == "" || utf8.RuneCountInString(req.Q) > queryMax {
		return apiErr(http.StatusBadRequest, "bad_query")
	}
	id, ctx := s.full.start()
	go s.runFullSearch(ctx, id, query, req.Service)
	writeJSON(w, http.StatusOK, FullSearchStarted{ID: id})
	return nil
}

// runFullSearch — полный поиск без агента, как в первой версии: журналы целиком, вместе
// с выводом команд и прочитанными файлами. Слова запроса ищутся без учёта регистра; сессия
// подходит, если в ней есть все слова. Редкие слова весят больше частых.
func (s *Server) runFullSearch(ctx context.Context, id, query string, service bool) {
	terms := dedupeFold(strings.Fields(query))
	c := s.viewCtx()
	var pool []model.Session
	for _, sess := range s.catalog.Sessions() {
		if sess.Empty {
			continue
		}
		if !service && (c.marks.Marks(sess.Key()).Hidden || c.service(sess)) {
			continue
		}
		pool = append(pool, sess)
	}
	byID := map[string]agents.Agent{}
	for _, a := range s.allAgents() {
		byID[a.ID()] = a
	}
	variants := make([][][]byte, len(terms))
	for i, term := range terms {
		variants[i] = needles(term)
	}
	emit := func(e SearchEvent) {
		if ctx.Err() == nil {
			s.events.broadcast(frame("search", e))
		}
	}
	total := len(pool)
	emit(SearchEvent{ID: id, Total: total})

	type found struct {
		sess   model.Session
		counts []int
		meta   []bool
	}
	var results []found
	var mu sync.Mutex
	var done atomic.Int64
	work := make(chan model.Session)
	var wg sync.WaitGroup
	for range fullWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sess := range work {
				if ctx.Err() == nil {
					f := found{sess: sess, counts: make([]int, len(terms)), meta: make([]bool, len(terms))}
					if a := byID[sess.Agent]; a != nil {
						// Журнал бывает в сотню мегабайт: регистр меняется на месте, без копии.
						data := asciiLower(a.RawText(sess))
						for i := range terms {
							for _, v := range variants[i] {
								f.counts[i] += bytes.Count(data, v)
							}
						}
					}
					meta := strings.ToLower(metaText(sess, c.marks.Marks(sess.Key())))
					any := false
					for i, term := range terms {
						f.meta[i] = strings.Contains(meta, strings.ToLower(term))
						any = any || f.counts[i] > 0 || f.meta[i]
					}
					if any {
						mu.Lock()
						results = append(results, f)
						mu.Unlock()
					}
				}
				done.Add(1)
			}
		}()
	}
	stopTicker := make(chan struct{})
	go func() {
		tick := time.NewTicker(400 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopTicker:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				emit(SearchEvent{ID: id, Done: int(done.Load()), Total: total})
			}
		}
	}()
	for _, sess := range pool {
		if ctx.Err() != nil {
			break
		}
		work <- sess
	}
	close(work)
	wg.Wait()
	close(stopTicker)
	if ctx.Err() != nil {
		return
	}

	// Вес слова — log(1 + всего / в скольких сессиях оно есть): редкое слово важнее.
	freq := make([]int, len(terms))
	for _, f := range results {
		for i := range terms {
			if f.counts[i] > 0 || f.meta[i] {
				freq[i]++
			}
		}
	}
	type ranked struct {
		sess    model.Session
		score   float64
		matched []string
	}
	var list []ranked
	for _, f := range results {
		var score float64
		var matched []string
		for i, term := range terms {
			if f.counts[i] == 0 && !f.meta[i] {
				continue
			}
			matched = append(matched, term)
			weight := math.Log(1 + float64(max(1, total))/float64(freq[i]))
			if f.counts[i] > 0 {
				score += weight * (1 + math.Log(float64(f.counts[i])))
			}
			if f.meta[i] {
				score += 2 * weight
			}
		}
		if len(matched) == len(terms) {
			list = append(list, ranked{f.sess, math.Round(score*1000) / 1000, matched})
		}
	}
	slices.SortFunc(list, func(a, b ranked) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return b.sess.Updated.Compare(a.sess.Updated)
	})
	if len(list) > fullResults {
		list = list[:fullResults]
	}
	hits := []SearchHit{}
	for _, item := range list {
		if ctx.Err() != nil {
			return
		}
		a := byID[item.sess.Agent]
		field, snippet := "conversation", transcriptSnippet(a, item.sess, item.matched)
		if snippet == "" {
			field, snippet = "log", rawSnippet(a, item.sess, item.matched)
		}
		hits = append(hits, SearchHit{Session: c.view(item.sess), Field: field, Snippet: snippet})
	}
	emit(SearchEvent{ID: id, Done: total, Total: total, Finished: true, Hits: hits})
}

// metaText — что о сессии знает человек: названия, сводка, проект, ветка.
func metaText(sess model.Session, m store.Marks) string {
	parts := []string{sess.Title, m.Name, sess.Project(), sess.Branch}
	if m.Summary != nil {
		parts = append(parts, m.Summary.Title, m.Summary.Summary)
	}
	return strings.Join(parts, " ")
}

func dedupeFold(words []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		k := strings.ToLower(w)
		if w != "" && !seen[k] {
			seen[k] = true
			out = append(out, w)
		}
	}
	return out
}

// needles — как слово выглядит в журнале, где латиница приведена к нижнему регистру, как
// bytes.lower() первой версии. Кириллица регистр не меняет, поэтому ищутся написания «база»,
// «База», «БАЗА» — как есть и в экранированном виде JSON (\u0441). Смешанный регистр
// вроде «бАза» не найдётся.
func needles(term string) [][]byte {
	low := strings.ToLower(term)
	if isASCII(low) {
		return [][]byte{[]byte(low)}
	}
	var out [][]byte
	seen := map[string]bool{}
	for _, form := range []string{low, upperFirst(low), titleWords(low), strings.ToUpper(low)} {
		for _, v := range []string{string(asciiLower([]byte(form))), string(asciiLower([]byte(jsonEscape(form))))} {
			if !seen[v] {
				seen[v] = true
				out = append(out, []byte(v))
			}
		}
	}
	return out
}

// asciiLower меняет регистр латиницы на месте; длина и позиции не меняются.
func asciiLower(data []byte) []byte {
	for i, b := range data {
		if 'A' <= b && b <= 'Z' {
			data[i] = b + 'a' - 'A'
		}
	}
	return data
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func titleWords(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		if w != "" {
			words[i] = upperFirst(w)
		}
	}
	return strings.Join(words, " ")
}

// jsonEscape — не-ASCII символы как \uXXXX, как json.dumps первой версии.
func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			r1, r2 := utf16Pair(r)
			b.WriteString(`\u` + hex4(r1) + `\u` + hex4(r2))
		default:
			b.WriteString(`\u` + hex4(r))
		}
	}
	return b.String()
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xD800 + (r>>10)&0x3FF, 0xDC00 + r&0x3FF
}

func hex4(r rune) string {
	s := strconv.FormatInt(int64(r), 16)
	return strings.Repeat("0", 4-len(s)) + s
}

// transcriptSnippet — фрагмент видимой переписки с совпадением; пусто, если слова нашлись
// только в выводе команд и прочитанных файлах.
func transcriptSnippet(a agents.Agent, sess model.Session, terms []string) string {
	if a == nil {
		return ""
	}
	messages, _ := a.Transcript(sess, agents.SearchBytes*4)
	for _, m := range messages {
		lower := strings.ToLower(m.Text)
		at := -1
		for _, t := range terms {
			if i := strings.Index(lower, strings.ToLower(t)); i >= 0 && (at < 0 || i < at) {
				at = i
			}
		}
		if at >= 0 {
			snippet, _ := aroundAt(m.Text, lower, at, 80, snippetChars)
			return snippet
		}
	}
	return ""
}

// aroundAt — фрагмент вокруг байтовой позиции at в lower (тексте в нижнем регистре).
// ToLower меняет символ на символ, но не всегда сохраняет длину в байтах: позиция — в символах.
func aroundAt(text, lower string, at, before, width int) (string, bool) {
	runes := []rune(text)
	if lowerRunes := []rune(lower); len(lowerRunes) != len(runes) {
		runes = lowerRunes
	}
	pos := utf8.RuneCountInString(lower[:at])
	start := max(0, pos-before)
	end := min(len(runes), start+width)
	return strings.Join(strings.Fields(string(runes[start:end])), " "), true
}

var (
	escapedSpace = regexp.MustCompile(`\\+[nrt]`)
	escapedChar  = regexp.MustCompile(`\\+(["/\\])`)
)

// rawSnippet — фрагмент журнала как есть вокруг последнего совпадения: в выводе команд или
// прочитанном файле. Вывод команд лежит в журнале строкой JSON, иногда вложенной не раз:
// экранирование убирается.
func rawSnippet(a agents.Agent, sess model.Session, terms []string) string {
	if a == nil {
		return ""
	}
	data := a.RawText(sess)
	lower := asciiLower(bytes.Clone(data))
	for _, term := range terms {
		for _, needle := range needles(term) {
			at := bytes.LastIndex(lower, needle)
			if at < 0 {
				continue
			}
			raw := strings.ToValidUTF8(string(data[max(0, at-400):min(len(data), at+400)]), "")
			raw = escapedSpace.ReplaceAllString(raw, " ")
			raw = escapedChar.ReplaceAllString(raw, "$1")
			raw = strings.Join(strings.Fields(raw), " ")
			rawLower := strings.ToLower(raw)
			middle := strings.Index(rawLower, strings.ToLower(term))
			if middle < 0 {
				snippet, _ := aroundAt(raw, rawLower, 0, 0, snippetChars)
				return snippet
			}
			snippet, _ := aroundAt(raw, rawLower, middle, snippetChars/2, snippetChars)
			return snippet
		}
	}
	return ""
}
