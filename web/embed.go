// Package web вшивает собранный интерфейс «Нити» в бинарник.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist — содержимое web/dist: index.html и собранные скрипты и стили.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
