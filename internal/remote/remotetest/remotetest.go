// Package remotetest — узел Tailscale без Tailscale для тестов: сразу в сети, слушает TLS
// на 127.0.0.1 с самоподписанным сертификатом на Host. Клиент ходит на https://Host
// через этот слушатель и проверяет сертификат по-настоящему.
package remotetest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/genhoi/ai-threads/internal/remote"
)

// Host — адрес узла в тестах.
const Host = "nit.example.ts.net"

var (
	certOnce sync.Once
	cert     tls.Certificate
	pool     *x509.CertPool
)

func testCert() (tls.Certificate, *x509.CertPool) {
	certOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: Host}, DNSNames: []string{Host},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			IsCA: true, BasicConstraintsValid: true,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		leaf, _ := x509.ParseCertificate(der)
		cert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
		pool = x509.NewCertPool()
		pool.AddCert(leaf)
	})
	return cert, pool
}

// Node — узел, который сразу в сети.
type Node struct {
	Cfg remote.Config

	mu     sync.Mutex
	ln     net.Listener
	addr   string
	funnel bool
	closed bool
}

func (n *Node) Up(ctx context.Context) (string, error)            { return Host, nil }
func (n *Node) Login(ctx context.Context) (string, string, error) { return "Running", "", nil }
func (n *Node) Cert(ctx context.Context, host string) error       { return nil }

func (n *Node) Listen(funnel bool) (net.Listener, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, errors.New("node closed")
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c, _ := testCert()
	n.addr, n.funnel = tcp.Addr().String(), funnel
	n.ln = tls.NewListener(tcp, &tls.Config{Certificates: []tls.Certificate{c}})
	return n.ln, nil
}

func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	if n.ln != nil {
		n.ln.Close()
	}
	return nil
}

// Closed — узел закрыт.
func (n *Node) Closed() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

// Funnel — слушатель открыт через Funnel.
func (n *Node) Funnel() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.funnel
}

// Addr — адрес слушателя на 127.0.0.1.
func (n *Node) Addr() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.addr
}

// Nodes — фабрика узлов для Options.RemoteNode.
type Nodes struct {
	mu  sync.Mutex
	all []*Node
}

func (f *Nodes) Factory(cfg remote.Config) remote.Node {
	n := &Node{Cfg: cfg}
	f.mu.Lock()
	f.all = append(f.all, n)
	f.mu.Unlock()
	return n
}

// Last — последний созданный узел; nil — ни одного.
func (f *Nodes) Last() *Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.all) == 0 {
		return nil
	}
	return f.all[len(f.all)-1]
}

// Transport — HTTPS к https://Host через слушатель узла по адресу addr.
func Transport(addr string) *http.Transport {
	_, roots := testCert()
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: Host},
	}
}
