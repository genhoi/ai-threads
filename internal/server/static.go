package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/web"
)

// staticFiles — собранный интерфейс в памяти: каждый файл сжат один раз при запуске.
// Интерфейсом пользуются и с телефона через туннель, а чанк xterm весит треть мегабайта.
type staticFiles struct {
	files map[string]*staticFile
}

type staticFile struct {
	data, gz     []byte
	etag, etagGz string
	ctype        string
	cache        string
}

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".mjs": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8", ".map": "application/json; charset=utf-8",
	".svg": "image/svg+xml", ".txt": "text/plain; charset=utf-8", ".woff2": "font/woff2",
	".woff": "font/woff", ".png": "image/png", ".ico": "image/x-icon",
	".webmanifest": "application/manifest+json",
}

// compressible — текст сжимается хорошо; шрифты woff2 и картинки уже сжаты.
var compressible = map[string]bool{".html": true, ".js": true, ".mjs": true, ".css": true, ".json": true,
	".map": true, ".svg": true, ".txt": true, ".webmanifest": true}

func loadStatic() *staticFiles {
	st := &staticFiles{files: map[string]*staticFile{}}
	dist := web.Dist()
	_ = fs.WalkDir(dist, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(dist, name)
		if err != nil {
			return nil
		}
		ext := path.Ext(name)
		f := &staticFile{data: data, etag: etag(data, ""), ctype: contentTypes[ext], cache: cachePolicy(name)}
		if f.ctype == "" {
			if f.ctype = mime.TypeByExtension(ext); f.ctype == "" {
				f.ctype = "application/octet-stream"
			}
		}
		if compressible[ext] && len(data) > 256 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(data)
			_ = zw.Close()
			if buf.Len() < len(data) {
				f.gz, f.etagGz = buf.Bytes(), etag(data, "-gz")
			}
		}
		st.files["/"+name] = f
		return nil
	})
	return st
}

func etag(data []byte, suffix string) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:8]) + suffix + `"`
}

// cachePolicy: чанки с хешем в имени не меняются никогда; шрифты меняются редко; точки входа
// (index.html, app.js, app.css) браузер сверяет каждый раз, иначе не увидит новую сборку.
func cachePolicy(name string) string {
	switch {
	case strings.HasPrefix(name, "chunks/"):
		return "public, max-age=31536000, immutable"
	case strings.HasPrefix(name, "fonts/"):
		return "public, max-age=604800"
	}
	return "no-cache"
}

// ServeHTTP отдаёт файл интерфейса. Неизвестный путь без расширения — адрес внутри
// приложения: отдаётся index.html, а маршрут разбирает сам интерфейс.
func (st *staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	if p == "/" {
		p = "/index.html"
	}
	f := st.files[p]
	if f == nil && path.Ext(p) == "" {
		f = st.files["/index.html"]
	}
	if f == nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.ctype)
	h.Set("Cache-Control", f.cache)
	h.Set("X-Content-Type-Options", "nosniff")
	data, tag := f.data, f.etag
	if f.gz != nil {
		h.Add("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			data, tag = f.gz, f.etagGz
			h.Set("Content-Encoding", "gzip")
		}
	}
	h.Set("ETag", tag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// acceptsGzip — клиент принимает gzip и не запретил его через q=0.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}
