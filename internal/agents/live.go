package agents

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Реестры запущенных сессий — маленькие файлы, которые агенты ведут сами. Журналы при
// опросе не читаются: только реестр и проверка, жив ли процесс.

// liveMaxBytes — больше этого реестр уже не «маленький файл статуса».
const liveMaxBytes = 1_000_000

func readSmallJSON(path string) any {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > liveMaxBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}

// processAlive — процесс с этим pid жив. Если в записи есть время старта процесса, оно должно
// совпасть с полем 22 /proc/<pid>/stat: так запись не цепляется к новому процессу с тем же pid.
func processAlive(pid, procStart any) bool {
	n, ok := pid.(float64)
	// pid <= 0 нельзя проверять: 0 и -1 означают группу процессов.
	if !ok || n <= 0 || n != math.Trunc(n) || n > math.MaxInt32 {
		return false
	}
	if syscall.Kill(int(n), 0) != nil {
		return false
	}
	want := pyString(procStart)
	if procStart == nil || want == "" {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(n)) + "/stat")
	if err != nil {
		return true
	}
	actual, ok := startTime(string(data))
	return !ok || actual == want
}

// startTime — поле 22 (starttime) из /proc/<pid>/stat. Имя процесса в скобках может содержать
// пробелы и скобки, поэтому строка режется по последней «)».
func startTime(stat string) (string, bool) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) <= 19 {
		return "", false
	}
	return fields[19], true
}

// pyString — значение строкой, как str() в Python: целые числа без дробной части.
func pyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e18 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return ""
}

// unixSeconds — секунды Unix из числа (секунды или миллисекунды), строки с числом или ISO 8601.
// Время ISO без пояса — местное.
func unixSeconds(v any) (float64, bool) {
	fromNumber := func(f float64) (float64, bool) {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		if f >= 1e11 {
			f /= 1000
		}
		return f, true
	}
	switch x := v.(type) {
	case float64:
		return fromNumber(x)
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return fromNumber(f)
		}
		if t, ok := parseISO(text, time.Local); ok {
			return seconds(t), true
		}
	}
	return 0, false
}
