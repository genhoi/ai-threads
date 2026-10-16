package remote

import (
	"regexp"
	"strings"
	"unicode"
)

// androidModel — модель из «Linux; Android 14; Pixel 8)» или «Android 13; SM-S918B Build/…».
var androidModel = regexp.MustCompile(`Android [\d.]+; ([^;)]+)`)

// DeviceName — имя устройства для списка: «iPhone · Safari», «Pixel 8 · Chrome», «Android · Chrome».
func DeviceName(ua string) string {
	ua = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ua)
	return deviceKind(ua) + " · " + browserName(ua)
}

func deviceKind(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		if m := androidModel.FindStringSubmatch(ua); m != nil {
			model, _, _ := strings.Cut(m[1], " Build/")
			model = strings.TrimSpace(model)
			// Chrome с урезанным User-Agent пишет вместо модели «K».
			if model != "" && model != "K" && !strings.EqualFold(model, "wv") && len(model) <= 40 {
				return model
			}
		}
		return "Android"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "CrOS"):
		return "Chromebook"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	}
	return "Устройство"
}

func browserName(ua string) string {
	// Порядок важен: Edge, Opera и Яндекс пишут о себе и «Chrome», Chrome на iOS — «Safari».
	for _, b := range []struct{ mark, name string }{
		{"YaBrowser", "Яндекс Браузер"},
		{"SamsungBrowser", "Samsung Internet"},
		{"EdgiOS", "Edge"}, {"EdgA/", "Edge"}, {"Edg/", "Edge"},
		{"OPiOS", "Opera"}, {"OPR/", "Opera"},
		{"FxiOS", "Firefox"}, {"Firefox/", "Firefox"},
		{"CriOS", "Chrome"}, {"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.mark) {
			return b.name
		}
	}
	return "браузер"
}
