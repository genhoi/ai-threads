package live

import (
	"regexp"
	"strings"
	"unicode"
)

// ansi — управляющие последовательности терминала: цвета (CSI) и заголовки, ссылки (OSC).
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-_]`)

// LastLine — последняя осмысленная строка экрана для раздела «Работают»: без цветов,
// рамок и знаков индикатора, только строки с буквами или цифрами. Поле ввода внизу экрана
// (рамка из линий, как у Claude Code) и подсказки под ним пропускаются: нужен вывод агента.
func LastLine(screen string) string {
	lines := strings.Split(ansi.ReplaceAllString(screen, ""), "\n")
	// Самая верхняя линия-рамка в нижних строках — начало поля ввода.
	end := len(lines)
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-10; i-- {
		if rule(lines[i]) {
			end = i
		}
	}
	if line := last(lines[:end]); line != "" || end == len(lines) {
		return line
	}
	return last(lines)
}

func last(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimFunc(lines[i], decor)
		if strings.IndexFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
			continue
		}
		if r := []rune(line); len(r) > maxLastLine {
			line = string(r[:maxLastLine]) + "…"
		}
		return line
	}
	return ""
}

// rule — строка из одних линий рамки, не короче 10 знаков.
func rule(line string) bool {
	n := 0
	for _, r := range line {
		switch {
		case r >= 0x2500 && r <= 0x257F:
			n++
		case !unicode.IsSpace(r):
			return false
		}
	}
	return n >= 10
}

// decor — знаки по краям строки, которые не текст: пробелы, рамки, блоки, индикаторы
// (кружки, звёздочки, точки Брайля), стрелки и приглашение ввода.
func decor(r rune) bool {
	switch {
	case unicode.IsSpace(r):
		return true
	case r >= 0x2190 && r <= 0x21FF, // стрелки
		r >= 0x2300 && r <= 0x23FF, // технические знаки: ⏺ ⎿
		r >= 0x2500 && r <= 0x25FF, // рамки, блоки, фигуры: │ █ ● ◐
		r >= 0x2600 && r <= 0x27BF, // символы и дингбаты: ✻ ✶ ✳ ❯
		r >= 0x2800 && r <= 0x28FF: // точки Брайля: индикатор ⠋⠙⠹
		return true
	}
	return strings.ContainsRune("·•*>|›»", r)
}
