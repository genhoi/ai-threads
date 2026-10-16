package remote

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newManager(t *testing.T, f *nodes) (*Manager, *atomic.Int32) {
	t.Helper()
	keys, err := Open(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	var changes atomic.Int32
	m := NewManager(Options{Keys: keys, Handler: http.NotFoundHandler(), NewNode: f.factory,
		OnChange: func() { changes.Add(1) }, Logf: func(string, ...any) {}, Retry: []time.Duration{time.Hour}})
	t.Cleanup(m.Close)
	return m, &changes
}

func waitNode(t *testing.T, f *nodes, n int) *fakeNode {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("nodes %d, want %d", f.count(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return f.last()
}

// Состояния по порядку: starting → needs_login со ссылкой → certificate → running → off.
func TestManagerStates(t *testing.T) {
	f := &nodes{t: t}
	m, changes := newManager(t, f)
	if m.Status().State != StateOff {
		t.Fatal(m.Status())
	}
	cfg := Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()}
	m.Apply(cfg)
	node := waitNode(t, f, 1)
	if node.cfg != cfg {
		t.Fatalf("node config %+v", node.cfg)
	}
	waitState(t, m, StateStarting)

	node.mu.Lock()
	node.login, node.loginURL = "NeedsLogin", "https://login.tailscale.com/a/abc"
	node.mu.Unlock()
	if st := waitState(t, m, StateNeedsLogin); st.LoginURL != "https://login.tailscale.com/a/abc" || st.Host != "" {
		t.Fatalf("needs login: %+v", st)
	}
	if m.Handler().(*Guard).host() != "" {
		t.Fatal("no host before the node is up")
	}

	close(node.upGate)
	if st := waitState(t, m, StateCertificate); st.Host != testHost || st.LoginURL != "" {
		t.Fatalf("certificate: %+v", st)
	}
	if m.Keys().Key() == "" {
		t.Fatal("the pairing key must exist once the node starts")
	}
	close(node.certGate)
	st := waitState(t, m, StateRunning)
	if st.Host != testHost || st.Since.IsZero() || node.funnel {
		t.Fatalf("running: %+v funnel %v", st, node.funnel)
	}
	if changes.Load() < 4 {
		t.Fatalf("changes %d", changes.Load())
	}
	// Те же настройки — узел не трогается.
	m.Apply(cfg)
	if f.count() != 1 || node.isClosed() {
		t.Fatal("same config must keep the node")
	}

	m.Apply(Config{})
	if m.Status().State != StateOff || !node.isClosed() {
		t.Fatalf("off: %+v closed %v", m.Status(), node.isClosed())
	}
	// Выключенный с другим именем — всё ещё выключенный.
	m.Apply(Config{Hostname: "other"})
	if f.count() != 1 {
		t.Fatal("disabled config must not start a node")
	}
}

// Выключение, пока узел ждёт входа: Up отменяется, узел закрывается.
func TestManagerStopWhileWaitingLogin(t *testing.T) {
	f := &nodes{t: t}
	m, _ := newManager(t, f)
	m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
	node := waitNode(t, f, 1)
	done := make(chan struct{})
	go func() {
		m.Apply(Config{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Apply(off) hangs while Up waits for login")
	}
	if !node.isClosed() || m.Status().State != StateOff {
		t.Fatal("node must be closed")
	}
}

// Новые настройки: старый узел закрыт до того, как создан новый (у каталога tsnet один владелец).
func TestManagerRestartsOnChange(t *testing.T) {
	f := &nodes{t: t, open: true}
	var closedBefore []bool
	f.prepare = func(n *fakeNode) {
		if prev := f.last(); prev != nil {
			closedBefore = append(closedBefore, prev.isClosed())
		}
	}
	m, _ := newManager(t, f)
	dir := t.TempDir()
	m.Apply(Config{Enabled: true, Hostname: "nit", Dir: dir})
	waitState(t, m, StateRunning)
	m.Apply(Config{Enabled: true, Funnel: true, Hostname: "nit", Dir: dir})
	waitState(t, m, StateRunning)
	node := waitNode(t, f, 2)
	if len(closedBefore) != 1 || !closedBefore[0] || !node.funnel {
		t.Fatalf("closed before restart: %v, funnel %v", closedBefore, node.funnel)
	}
}

// Ошибка на любом этапе — состояние error с причиной и закрытый узел; Retry пробует сразу.
func TestManagerErrors(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*fakeNode)
		code    string
	}{
		{"up", func(n *fakeNode) { n.upErr = errors.New("tsnet.Up: backend: boom") }, ""},
		{"https", func(n *fakeNode) {
			n.listenErr = errors.New("tsnet: you must enable HTTPS in the admin panel to proceed. See https://tailscale.com/s/https")
		}, "no_https"},
		{"funnel", func(n *fakeNode) {
			n.listenErr = errors.New(`Funnel not available; "funnel" node attribute not set. See https://tailscale.com/s/no-funnel.`)
		}, "no_funnel"},
		{"certificate", func(n *fakeNode) { n.certErr = errors.New("acme: rate limited") }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &nodes{t: t, open: true, prepare: c.prepare}
			m, _ := newManager(t, f)
			m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
			st := waitState(t, m, StateError)
			if st.Err == "" || st.ErrCode != c.code || st.Host != "" {
				t.Fatalf("status %+v", st)
			}
			node := f.last()
			deadline := time.Now().Add(5 * time.Second)
			for !node.isClosed() && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if !node.isClosed() {
				t.Fatal("failed node must be closed")
			}
			f.prepare = nil
			m.Retry()
			waitState(t, m, StateRunning)
			if f.count() != 2 {
				t.Fatalf("nodes %d", f.count())
			}
		})
	}
}

// Слушатель узла умер — это ошибка, а не тихая остановка.
func TestManagerListenerDies(t *testing.T) {
	f := &nodes{t: t, open: true}
	m, _ := newManager(t, f)
	m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
	waitState(t, m, StateRunning)
	node := f.last()
	node.mu.Lock()
	node.ln.Close()
	node.mu.Unlock()
	if st := waitState(t, m, StateError); st.Err == "" {
		t.Fatal(st)
	}
}

func TestManagerClose(t *testing.T) {
	f := &nodes{t: t, open: true}
	m, _ := newManager(t, f)
	m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
	waitState(t, m, StateRunning)
	m.Close()
	if !f.last().isClosed() || m.Status().State != StateOff {
		t.Fatal("Close must stop the node")
	}
	m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
	if f.count() != 1 {
		t.Fatal("Apply after Close must do nothing")
	}
}

func TestListenAddr(t *testing.T) {
	// Только порт: tsnet с host:443 не слушает (грабля #7).
	if listenAddr != ":443" {
		t.Fatal(listenAddr)
	}
}
