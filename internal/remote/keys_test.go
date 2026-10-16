package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeysFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "remote.json")
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if k.Key() != "" || len(k.Devices()) != 0 {
		t.Fatal("new file must be empty")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file must not appear before the first change")
	}
	key, err := k.EnsureKey()
	if err != nil || len(key) != 32 {
		t.Fatalf("key %q %v", key, err)
	}
	if again, _ := k.EnsureKey(); again != key {
		t.Fatal("EnsureKey must keep the key")
	}
	token, d, err := k.AddDevice("iPhone · Safari")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), hashToken(token)) {
		t.Fatalf("file must hold only the token hash: %s", raw)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Key() != key {
		t.Fatal("key is not persisted")
	}
	if got, ok := reopened.Device(token); !ok || got.ID != d.ID || got.Name != "iPhone · Safari" {
		t.Fatalf("device %+v %v", got, ok)
	}
	if _, ok := reopened.Device(token + "x"); ok {
		t.Fatal("wrong token accepted")
	}
	if _, ok := reopened.Device(""); ok {
		t.Fatal("empty token accepted")
	}
	for _, dev := range reopened.Devices() {
		if dev.Hash != "" {
			t.Fatal("Devices must not expose hashes")
		}
	}
}

func TestKeyOK(t *testing.T) {
	k, _ := Open(filepath.Join(t.TempDir(), "remote.json"))
	if k.KeyOK("") || k.KeyOK("0123456789abcdef") {
		t.Fatal("no key yet: nothing matches")
	}
	key, _ := k.EnsureKey()
	if !k.KeyOK(key) || k.KeyOK(key[:31]) || k.KeyOK(key+"0") || k.KeyOK(strings.ToUpper(key)) {
		t.Fatal("KeyOK")
	}
}

func TestTouchIsThrottled(t *testing.T) {
	k, _ := Open(filepath.Join(t.TempDir(), "remote.json"))
	now := time.Unix(1_800_000_000, 0)
	k.now = func() time.Time { return now }
	changes := 0
	k.OnChange(func() { changes++ })
	_, d, _ := k.AddDevice("Mac · Safari")
	changes = 0
	now = now.Add(30 * time.Second)
	if k.Touch(d.ID) || changes != 0 {
		t.Fatal("touch within a minute must not write")
	}
	now = now.Add(31 * time.Second)
	if !k.Touch(d.ID) || changes != 1 || k.Devices()[0].LastSeen != now.Unix() {
		t.Fatalf("touch after a minute: %+v", k.Devices())
	}
	if k.Touch("nope") {
		t.Fatal("unknown device")
	}
}

func TestRevokeAndNewKey(t *testing.T) {
	k, _ := Open(filepath.Join(t.TempDir(), "remote.json"))
	key, _ := k.EnsureKey()
	ta, a, _ := k.AddDevice("a")
	tb, b, _ := k.AddDevice("b")
	if ok, err := k.Revoke(a.ID); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, ok := k.Device(ta); ok {
		t.Fatal("revoked device still valid")
	}
	if _, ok := k.Device(tb); !ok {
		t.Fatal("other device revoked")
	}
	ids, err := k.NewKey()
	if err != nil || len(ids) != 1 || ids[0] != b.ID {
		t.Fatal(ids, err)
	}
	if k.Key() == key || len(k.Devices()) != 0 || k.KeyOK(key) {
		t.Fatal("new key must replace the old one and drop devices")
	}
}

func TestTooManyDevices(t *testing.T) {
	k, _ := Open(filepath.Join(t.TempDir(), "remote.json"))
	now := time.Unix(1_800_000_000, 0)
	k.now = func() time.Time { return now }
	first, _, _ := k.AddDevice("first")
	for i := range maxDevices {
		now = now.Add(time.Second)
		if _, _, err := k.AddDevice("d" + string(rune('a'+i%26))); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(k.Devices()); n != maxDevices {
		t.Fatalf("devices %d", n)
	}
	if _, ok := k.Device(first); ok {
		t.Fatal("the longest unseen device must be dropped")
	}
}

func TestBrokenFileIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote.json")
	os.WriteFile(path, []byte("{broken"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("broken file opened")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{broken" {
		t.Fatal("broken file overwritten")
	}
}

// Ключ из файла gate: телефоны с cookie gate остаются подключены. Тот же файл второй раз
// не отменяет «Новый ключ».
func TestImportGateToken(t *testing.T) {
	dir := t.TempDir()
	k, _ := Open(filepath.Join(dir, "remote.json"))
	_, d, _ := k.AddDevice("kept")
	token := "0123456789abcdef0123456789abcdef"
	file := filepath.Join(dir, "token.txt")
	os.WriteFile(file, []byte(token+"\n"), 0o600)
	if changed, err := k.ImportGateToken(file); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if k.Key() != token || len(k.Devices()) != 1 || k.Devices()[0].ID != d.ID {
		t.Fatalf("import must set the key and keep devices: %+v", k.Devices())
	}
	if changed, _ := k.ImportGateToken(file); changed {
		t.Fatal("second import of the same file must do nothing")
	}
	k.NewKey()
	fresh := k.Key()
	if changed, _ := k.ImportGateToken(file); changed || k.Key() != fresh {
		t.Fatal("import after a new key must not bring the old key back")
	}
	for _, bad := range []string{"short", "has space in the middle!!", strings.Repeat("я", 20)} {
		os.WriteFile(file, []byte(bad), 0o600)
		_, err := k.ImportGateToken(file)
		if err == nil {
			t.Fatalf("%q accepted", bad)
		}
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error shows the token: %v", err)
		}
	}
	if _, err := k.ImportGateToken(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("missing file accepted")
	}
}
