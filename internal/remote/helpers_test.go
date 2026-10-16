package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testHost = "nit.example.ts.net"

// testCert — самоподписанный сертификат на testHost: клиент проверяет его по-настоящему.
type testCert struct {
	tls  tls.Certificate
	pool *x509.CertPool
}

var (
	certOnce sync.Once
	cert     testCert
)

func newCert(t *testing.T) testCert {
	t.Helper()
	certOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: testHost}, DNSNames: []string{testHost},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			IsCA: true, BasicConstraintsValid: true,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		leaf, _ := x509.ParseCertificate(der)
		cert.tls = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
		cert.pool = x509.NewCertPool()
		cert.pool.AddCert(leaf)
	})
	return cert
}

// fakeNode — узел без Tailscale: Up ждёт, пока его не пустят, Listen отдаёт TLS-слушатель
// на 127.0.0.1 с сертификатом на testHost.
type fakeNode struct {
	t   *testing.T
	cfg Config

	mu        sync.Mutex
	login     string // BackendState, пока Up ждёт
	loginURL  string
	upGate    chan struct{} // закрыт — Up проходит
	certGate  chan struct{} // закрыт — Cert проходит
	upErr     error
	listenErr error
	certErr   error
	addr      string
	ln        net.Listener
	closed    bool
	funnel    bool
}

func (n *fakeNode) Up(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-n.upGate:
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.upErr != nil {
		return "", n.upErr
	}
	return testHost, nil
}

func (n *fakeNode) Login(ctx context.Context) (string, string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.login, n.loginURL, nil
}

func (n *fakeNode) Listen(funnel bool) (net.Listener, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, errors.New("node closed")
	}
	if n.listenErr != nil {
		return nil, n.listenErr
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	n.funnel = funnel
	n.addr = tcp.Addr().String()
	n.ln = tls.NewListener(tcp, &tls.Config{Certificates: []tls.Certificate{newCert(n.t).tls}})
	return n.ln, nil
}

func (n *fakeNode) Cert(ctx context.Context, host string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-n.certGate:
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.certErr
}

// Close закрывает слушатель: так tsnet закрывает свои при Close.
func (n *fakeNode) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return errors.New("closed twice")
	}
	n.closed = true
	if n.ln != nil {
		n.ln.Close()
	}
	return nil
}

func (n *fakeNode) isClosed() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

func (n *fakeNode) address() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.addr
}

// nodes — фабрика подделок: каждая новая открыта сразу, если open.
type nodes struct {
	t    *testing.T
	open bool
	mu   sync.Mutex
	all  []*fakeNode
	// prepare настраивает узел до того, как Manager его получит.
	prepare func(*fakeNode)
}

func (f *nodes) factory(cfg Config) Node {
	n := &fakeNode{t: f.t, cfg: cfg, upGate: make(chan struct{}), certGate: make(chan struct{}), login: "Starting"}
	if f.open {
		close(n.upGate)
		close(n.certGate)
	}
	if f.prepare != nil {
		f.prepare(n)
	}
	f.mu.Lock()
	f.all = append(f.all, n)
	f.mu.Unlock()
	return n
}

func (f *nodes) last() *fakeNode {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.all) == 0 {
		return nil
	}
	return f.all[len(f.all)-1]
}

func (f *nodes) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.all)
}

// logBuf — журнал для проверки, что секреты в него не попадают.
type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (l *logBuf) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// rig — Manager с узлом-подделкой, уже в running, и обработчиком после Guard.
type rig struct {
	t     *testing.T
	m     *Manager
	keys  *Keys
	nodes *nodes
	log   *logBuf
}

func newRig(t *testing.T, inner http.Handler) *rig {
	t.Helper()
	keys, err := Open(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, keys: keys, nodes: &nodes{t: t, open: true}, log: &logBuf{}}
	r.m = NewManager(Options{Keys: keys, Handler: inner, NewNode: r.nodes.factory, Logf: r.log.logf,
		Retry: []time.Duration{time.Hour}})
	t.Cleanup(r.m.Close)
	r.m.Apply(Config{Enabled: true, Hostname: "nit", Dir: t.TempDir()})
	r.waitState(StateRunning)
	return r
}

func (r *rig) waitState(want State) Status {
	r.t.Helper()
	return waitState(r.t, r.m, want)
}

func waitState(t *testing.T, m *Manager, want State) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.Status(); st.State == want {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state %s, want %s", m.Status().State, want)
	return Status{}
}

// transport ходит на адрес узла testHost через слушатель подделки и проверяет сертификат.
func (r *rig) transport() *http.Transport {
	addr := r.nodes.last().address()
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: newCert(r.t).pool, ServerName: testHost},
	}
}

// phone — браузер телефона: свои cookie, редиректы не проходит сам.
func (r *rig) phone(ua string) *phone {
	jar, _ := cookiejar.New(nil)
	return &phone{t: r.t, ua: ua, jar: jar, client: &http.Client{Transport: r.transport(), Jar: jar, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type phone struct {
	t      *testing.T
	ua     string
	jar    http.CookieJar
	client *http.Client
}

// do — запрос к https://testHost; header — дополнительные заголовки.
func (p *phone) do(method, path string, header map[string]string) (*http.Response, string) {
	p.t.Helper()
	req, err := http.NewRequest(method, "https://"+testHost+path, nil)
	if err != nil {
		p.t.Fatal(err)
	}
	req.Header.Set("User-Agent", p.ua)
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (p *phone) cookie(name string) string {
	u, _ := url.Parse("https://" + testHost + "/")
	for _, c := range p.jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (p *phone) setCookie(name, value string) {
	u, _ := url.Parse("https://" + testHost + "/")
	p.jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true}})
}

// login входит по ссылке с ключом и проверяет, что вход удался.
func (p *phone) login(key string) {
	p.t.Helper()
	resp, body := p.do(http.MethodGet, "/?key="+key, nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		p.t.Fatalf("login: %d %q %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if p.cookie(DeviceCookie) == "" {
		p.t.Fatal("no device cookie after login")
	}
}

const (
	iphoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1"
	pixelUA  = "Mozilla/5.0 (Linux; Android 15; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36"
)
