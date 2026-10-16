package remote

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"

	"tailscale.com/tsnet"
)

// listenAddr — всегда ":443", не host:443: имя узла tsnet понимает только внутри tailnet,
// и слушатель с именем не слушает (грабля #7 gate).
const listenAddr = ":443"

// tsNode — узел tsnet. Здесь только вызовы tsnet: состояние и повторы живут в Manager.
type tsNode struct {
	srv *tsnet.Server
}

func newTSNode(cfg Config, logf func(string, ...any)) Node {
	srv := &tsnet.Server{
		Hostname: cfg.Hostname,
		Dir:      cfg.Dir,
		// Ссылка для входа и смены состояния; подробный журнал узла — только по NIT_REMOTE_DEBUG=1.
		UserLogf: func(format string, args ...any) { logf("remote: tsnet: "+format, args...) },
	}
	if os.Getenv("NIT_REMOTE_DEBUG") == "1" {
		srv.Logf = func(format string, args ...any) { logf("remote: tsnet: "+format, args...) }
	}
	return &tsNode{srv: srv}
}

func (n *tsNode) Up(ctx context.Context) (string, error) {
	st, err := n.srv.Up(ctx)
	if err != nil {
		return "", err
	}
	host := ""
	if st.Self != nil {
		host = strings.TrimSuffix(st.Self.DNSName, ".")
	}
	if host == "" && len(st.CertDomains) > 0 {
		host = strings.TrimSuffix(st.CertDomains[0], ".")
	}
	if host == "" {
		return "", errors.New("tsnet: you must enable MagicDNS and HTTPS in the admin panel. See https://tailscale.com/s/https")
	}
	return host, nil
}

func (n *tsNode) Login(ctx context.Context) (string, string, error) {
	lc, err := n.srv.LocalClient()
	if err != nil {
		return "", "", err
	}
	st, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		return "", "", err
	}
	return st.BackendState, st.AuthURL, nil
}

func (n *tsNode) Listen(funnel bool) (net.Listener, error) {
	if funnel {
		// Funnel слушает и интернет, и tailnet; нужен атрибут funnel в ACL tailnet.
		return n.srv.ListenFunnel("tcp", listenAddr)
	}
	return n.srv.ListenTLS("tcp", listenAddr)
}

func (n *tsNode) Cert(ctx context.Context, host string) error {
	lc, err := n.srv.LocalClient()
	if err != nil {
		return err
	}
	_, _, err = lc.CertPair(ctx, host)
	return err
}

func (n *tsNode) Close() error { return n.srv.Close() }
