package remote

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"testing"

	"rsc.io/qr"
)

func TestDeviceName(t *testing.T) {
	for ua, want := range map[string]string{
		iphoneUA: "iPhone · Safari",
		pixelUA:  "Pixel 8 · Chrome",
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36":                              "Android · Chrome",
		"Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0":  "SM-S918B · Samsung Internet",
		"Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/AP1A; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0 Mobile Safari/537.36": "Pixel 7 · Chrome",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0 Mobile/15E148 Safari/604.1":       "iPhone · Chrome",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/140.0 Mobile/15E148 Safari/605.1.15":    "iPhone · Firefox",
		"Mozilla/5.0 (iPad; CPU OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1":               "iPad · Safari",
		"Mozilla/5.0 (Linux; arm_64; Android 14; 2201117TY) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 YaBrowser/25.6.1 Mobile Safari/537.36": "2201117TY · Яндекс Браузер",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0":                "Windows · Edge",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15":                        "Mac · Safari",
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                                       "Linux · Firefox",
		"curl/8.5.0": "Устройство · браузер",
		"":           "Устройство · браузер",
		"Mozilla/5.0 (Linux; Android 14; Evil\x00\nPhone) Chrome/1.0": "EvilPhone · Chrome",
	} {
		if got := DeviceName(ua); got != want {
			t.Errorf("%q: %q, want %q", ua, got, want)
		}
	}
}

// QR-код в SVG: правильный XML, а модули совпадают с кодом из кодировщика.
func TestQRSVG(t *testing.T) {
	text := "https://nit.example.ts.net/?key=0123456789abcdef0123456789abcdef"
	svg, err := QRSVG(text)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
		Path    struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(svg, &doc); err != nil {
		t.Fatal(err)
	}
	code, _ := qr.Encode(text, qr.M)
	n := code.Size + 8
	if doc.ViewBox != "0 0 "+strconv.Itoa(n)+" "+strconv.Itoa(n) {
		t.Fatalf("viewBox %q", doc.ViewBox)
	}
	black := map[[2]int]bool{}
	for _, m := range regexp.MustCompile(`M(\d+) (\d+)h(\d+)v1h-(\d+)z`).FindAllStringSubmatch(doc.Path.D, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		w, _ := strconv.Atoi(m[3])
		for i := range w {
			black[[2]int{x + i - 4, y - 4}] = true
		}
	}
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) != black[[2]int{x, y}] {
				t.Fatalf("module %d,%d differs", x, y)
			}
		}
	}
}
