// Package remote — доступ к «Нити» с телефона: узел Tailscale в самом процессе (tsnet),
// вход по ключу из ссылки, устройства с долгоживущей cookie и проверка Host и Origin
// для запросов, пришедших не с компьютера.
//
// Секреты лежат в <папка данных>/remote.json с правами 0600: ключ входа и хеши ключей
// устройств. Ни в state.json, ни в выгрузку они не попадают.
package remote

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// maxDevices — сверх этого забывается устройство, которое дольше всех не заходило.
	maxDevices = 100
	// touchEvery — не чаще этого обновляется last_seen устройства и пишется файл.
	touchEvery = time.Minute
	// minKeyLen — ключ короче не принимается: столько требовал и gate.
	minKeyLen = 16
)

// Device — подключённый телефон. Сам ключ устройства не хранится, только его sha256.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hash     string `json:"token_sha256"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"last_seen"`
}

// keysFile — вид remote.json.
type keysFile struct {
	Version int      `json:"version"`
	Key     string   `json:"key,omitempty"`
	Devices []Device `json:"devices"`
	// Imported — sha256 ключа, однажды взятого из файла gate: тот же файл второй раз
	// не подменяет ключ, который человек уже сменил кнопкой «Новый ключ».
	Imported string `json:"imported_sha256,omitempty"`
}

// Keys — ключ входа и устройства. Методы можно звать из нескольких горутин.
type Keys struct {
	path string

	mu       sync.Mutex
	f        keysFile
	byHash   map[string]int // хеш ключа устройства → индекс в f.Devices
	onChange []func()
	now      func() time.Time
}

// Open читает remote.json. Файла нет — пусто, файл появится при первом изменении.
// Повреждённый файл не затирается: Open возвращает ошибку.
func Open(path string) (*Keys, error) {
	k := &Keys{path: path, f: keysFile{Version: 1}, now: time.Now}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &k.f); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		k.f.Version = 1
		k.f.Devices = slices.DeleteFunc(k.f.Devices, func(d Device) bool { return d.ID == "" || d.Hash == "" })
	}
	k.reindex()
	return k, nil
}

// Path — файл с секретами.
func (k *Keys) Path() string { return k.path }

// OnChange подписывает fn на изменения ключа и устройств. fn зовётся вне блокировки.
func (k *Keys) OnChange(fn func()) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.onChange = append(k.onChange, fn)
}

func (k *Keys) changed() {
	k.mu.Lock()
	hooks := slices.Clone(k.onChange)
	k.mu.Unlock()
	for _, fn := range hooks {
		fn()
	}
}

func (k *Keys) reindex() {
	k.byHash = make(map[string]int, len(k.f.Devices))
	for i, d := range k.f.Devices {
		k.byHash[d.Hash] = i
	}
}

// Key — ключ входа; пусто, если его ещё не создавали.
func (k *Keys) Key() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.f.Key
}

// EnsureKey создаёт ключ входа, если его нет, и возвращает его.
func (k *Keys) EnsureKey() (string, error) {
	k.mu.Lock()
	if k.f.Key != "" {
		defer k.mu.Unlock()
		return k.f.Key, nil
	}
	next := k.f
	next.Key = randomHex(16)
	err := k.saveLocked(next)
	key := k.f.Key
	k.mu.Unlock()
	if err != nil {
		return "", err
	}
	k.changed()
	return key, nil
}

// NewKey меняет ключ входа и отключает все устройства. Возвращает ID отключённых.
func (k *Keys) NewKey() ([]string, error) {
	k.mu.Lock()
	next := k.f
	next.Key = randomHex(16)
	next.Devices = nil
	ids := deviceIDs(k.f.Devices)
	err := k.saveLocked(next)
	k.mu.Unlock()
	if err != nil {
		return nil, err
	}
	k.changed()
	return ids, nil
}

// KeyOK сверяет ключ из ссылки или из cookie gate с ключом входа за постоянное время.
func (k *Keys) KeyOK(key string) bool {
	k.mu.Lock()
	cur := k.f.Key
	k.mu.Unlock()
	if len(cur) < minKeyLen || len(key) < minKeyLen {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(cur)) == 1
}

// AddDevice заводит устройство и возвращает его ключ для cookie. Ключ нигде не хранится.
func (k *Keys) AddDevice(name string) (token string, d Device, err error) {
	token = randomToken()
	now := k.now().Unix()
	d = Device{ID: randomHex(8), Name: name, Hash: hashToken(token), Created: now, LastSeen: now}
	k.mu.Lock()
	next := k.f
	next.Devices = append(slices.Clone(k.f.Devices), d)
	if len(next.Devices) > maxDevices {
		// Устройств слишком много: забывается то, что дольше всех не заходило.
		oldest := 0
		for i, x := range next.Devices {
			if x.LastSeen < next.Devices[oldest].LastSeen {
				oldest = i
			}
		}
		next.Devices = slices.Delete(next.Devices, oldest, oldest+1)
	}
	err = k.saveLocked(next)
	k.mu.Unlock()
	if err != nil {
		return "", Device{}, err
	}
	k.changed()
	return token, d, nil
}

// Device — устройство по ключу из cookie.
func (k *Keys) Device(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	i, ok := k.byHash[hashToken(token)]
	if !ok {
		return Device{}, false
	}
	return k.f.Devices[i], true
}

// Touch отмечает, что устройство заходило. Файл пишется не чаще раза в touchEvery;
// true — отметка записана.
func (k *Keys) Touch(id string) bool {
	now := k.now()
	k.mu.Lock()
	i := slices.IndexFunc(k.f.Devices, func(d Device) bool { return d.ID == id })
	if i < 0 || now.Unix()-k.f.Devices[i].LastSeen < int64(touchEvery/time.Second) {
		k.mu.Unlock()
		return false
	}
	next := k.f
	next.Devices = slices.Clone(k.f.Devices)
	next.Devices[i].LastSeen = now.Unix()
	err := k.saveLocked(next)
	k.mu.Unlock()
	if err != nil {
		return false
	}
	k.changed()
	return true
}

// Revoke отключает устройство; false — такого нет.
func (k *Keys) Revoke(id string) (bool, error) {
	k.mu.Lock()
	i := slices.IndexFunc(k.f.Devices, func(d Device) bool { return d.ID == id })
	if i < 0 {
		k.mu.Unlock()
		return false, nil
	}
	next := k.f
	next.Devices = slices.Delete(slices.Clone(k.f.Devices), i, i+1)
	err := k.saveLocked(next)
	k.mu.Unlock()
	if err != nil {
		return false, err
	}
	k.changed()
	return true, nil
}

// Devices — устройства, последние заходившие первыми. Хешей в копии нет.
func (k *Keys) Devices() []Device {
	k.mu.Lock()
	out := slices.Clone(k.f.Devices)
	k.mu.Unlock()
	for i := range out {
		out[i].Hash = ""
	}
	slices.SortStableFunc(out, func(a, b Device) int {
		switch {
		case a.LastSeen > b.LastSeen:
			return -1
		case a.LastSeen < b.LastSeen:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// ImportGateToken берёт ключ входа из файла gate (gate/state/token.txt), чтобы телефоны,
// вошедшие через gate, остались подключены. Один и тот же файл второй раз ключ не меняет.
// Устройства остаются. true — ключ заменён.
func (k *Keys) ImportGateToken(file string) (bool, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	token := strings.TrimSpace(string(raw))
	if err := checkImported(token); err != nil {
		return false, fmt.Errorf("%s: %w", file, err)
	}
	sum := hashToken(token)
	k.mu.Lock()
	if k.f.Imported == sum {
		k.mu.Unlock()
		return false, nil
	}
	next := k.f
	next.Key, next.Imported = token, sum
	err = k.saveLocked(next)
	k.mu.Unlock()
	if err != nil {
		return false, err
	}
	k.changed()
	return true, nil
}

// checkImported — ключ gate попадает в ссылку как есть, поэтому только безопасные для URL символы.
func checkImported(token string) error {
	if len(token) < minKeyLen || len(token) > 256 {
		return fmt.Errorf("ключ должен быть от %d до 256 символов", minKeyLen)
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '~') {
			return errors.New("в ключе допустимы только латиница, цифры и -_.~")
		}
	}
	return nil
}

// saveLocked пишет файл и только потом подменяет состояние в памяти.
func (k *Keys) saveLocked(next keysFile) error {
	next.Version = 1
	if next.Devices == nil {
		next.Devices = []Device{}
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSecret(k.path, append(raw, '\n')); err != nil {
		return err
	}
	k.f = next
	k.reindex()
	return nil
}

// writeSecret пишет атомарно с правами 0600: временный файл рядом и rename.
func writeSecret(path string, raw []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".remote-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(raw); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func deviceIDs(list []Device) []string {
	ids := make([]string, len(list))
	for i, d := range list {
		ids[i] = d.ID
	}
	return ids
}

// randomHex — n случайных байт в hex: ключ входа (16 байт) и ID устройства (8 байт).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// randomToken — ключ устройства: 256 бит в base64url.
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
