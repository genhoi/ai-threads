package remote

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// State — состояние доступа с телефона.
type State string

const (
	StateOff         State = "off"         // выключен
	StateStarting    State = "starting"    // узел поднимается
	StateNeedsLogin  State = "needs_login" // ждёт входа в Tailscale по ссылке
	StateCertificate State = "certificate" // адрес есть, выпускается первый сертификат (до минуты)
	StateRunning     State = "running"     // телефон может открыть адрес
	StateError       State = "error"       // не вышло; через время будет новая попытка
)

// Config — что включено. Dir — каталог состояния tsnet с раскрытым ~.
type Config struct {
	Enabled  bool
	Funnel   bool
	Hostname string
	Dir      string
}

func (c Config) normal() Config {
	if !c.Enabled {
		return Config{}
	}
	return c
}

// Status — состояние для интерфейса.
type Status struct {
	State    State
	Since    time.Time // когда началось это состояние
	LoginURL string    // для needs_login
	Host     string    // nit.<tailnet>.ts.net, когда узел в сети
	Err      string
	ErrCode  string // no_https, no_funnel или пусто
}

// Node — узел Tailscale. Настоящий — tsnet, в тестах — подделка со своим TLS-слушателем.
type Node interface {
	// Up ждёт, пока узел войдёт в сеть (сколько угодно, пока не отменён ctx), и возвращает
	// адрес вида nit.<tailnet>.ts.net.
	Up(ctx context.Context) (host string, err error)
	// Login — состояние входа, пока Up ждёт: BackendState tailscale и ссылка входа.
	Login(ctx context.Context) (state, url string, err error)
	// Listen открывает HTTPS-слушатель на :443: только в tailnet или ещё и через Funnel.
	Listen(funnel bool) (net.Listener, error)
	// Cert выпускает сертификат заранее, чтобы первый запрос телефона не ждал минуту.
	Cert(ctx context.Context, host string) error
	Close() error
}

// NodeFactory создаёт узел по настройкам; узел ещё не запущен.
type NodeFactory func(Config) Node

// Options — для NewManager.
type Options struct {
	Keys    *Keys
	Handler http.Handler // запросы, прошедшие Guard
	NewNode NodeFactory  // nil — tsnet
	// OnChange зовётся, когда поменялись состояние, ключ или устройства. Вне блокировок.
	OnChange func()
	Logf     func(format string, args ...any)
	// Retry — паузы перед новыми попытками после ошибки; последняя повторяется.
	Retry []time.Duration
}

// Manager поднимает и останавливает узел по настройкам и держит на нём HTTPS-сервер.
type Manager struct {
	keys     *Keys
	guard    *Guard
	newNode  NodeFactory
	onChange func()
	logf     func(string, ...any)
	retry    []time.Duration

	apply sync.Mutex // Apply и Close по одному

	mu     sync.Mutex
	cfg    Config
	status Status
	cancel context.CancelFunc
	done   chan struct{}
	kick   chan struct{}
	closed bool
}

func NewManager(o Options) *Manager {
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	if o.OnChange == nil {
		o.OnChange = func() {}
	}
	if len(o.Retry) == 0 {
		o.Retry = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	}
	m := &Manager{keys: o.Keys, newNode: o.NewNode, onChange: o.OnChange, logf: o.Logf, retry: o.Retry,
		status: Status{State: StateOff, Since: time.Now()}, kick: make(chan struct{}, 1)}
	if m.newNode == nil {
		m.newNode = func(c Config) Node { return newTSNode(c, o.Logf) }
	}
	m.guard = NewGuard(o.Keys, m.host, o.Handler, o.Logf)
	o.Keys.OnChange(o.OnChange)
	return m
}

// Handler — проверка запросов с телефона перед Options.Handler; её слушает узел.
func (m *Manager) Handler() http.Handler { return m.guard }

// Keys — ключ входа и устройства.
func (m *Manager) Keys() *Keys { return m.keys }

func (m *Manager) host() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State == StateCertificate || m.status.State == StateRunning {
		return m.status.Host
	}
	return ""
}

// Status — текущее состояние.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Config — применённые настройки; у выключенного — пустые.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Apply включает, выключает или перезапускает узел, если настройки поменялись.
// Старый узел закрывается до того, как поднимется новый: у каталога tsnet один владелец.
func (m *Manager) Apply(cfg Config) {
	cfg = cfg.normal()
	m.apply.Lock()
	defer m.apply.Unlock()
	m.mu.Lock()
	if m.closed || cfg == m.cfg {
		m.mu.Unlock()
		return
	}
	cancel, done := m.cancel, m.done
	m.cfg, m.cancel, m.done = cfg, nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	m.mu.Lock()
	if !cfg.Enabled {
		m.setLocked(Status{State: StateOff})
		m.mu.Unlock()
		m.onChange()
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	m.cancel, m.done = stop, make(chan struct{})
	m.setLocked(Status{State: StateStarting})
	go m.run(ctx, cfg, m.done)
	m.mu.Unlock()
	m.onChange()
}

// Retry — после ошибки попробовать сразу, не дожидаясь паузы.
func (m *Manager) Retry() {
	if m.Status().State != StateError {
		return
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Close останавливает узел при выходе из «Нити» и больше его не поднимает.
func (m *Manager) Close() {
	m.apply.Lock()
	defer m.apply.Unlock()
	m.mu.Lock()
	m.closed = true
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
		m.set(Status{State: StateOff})
	case <-time.After(10 * time.Second):
		m.logf("remote: узел не закрылся за 10 с")
	}
}

// Revoke отключает устройство и обрывает его соединения; false — такого нет.
func (m *Manager) Revoke(id string) (bool, error) {
	ok, err := m.keys.Revoke(id)
	if ok {
		m.guard.cut([]string{id}, false)
		m.logf("remote: телефон %s отключён", id)
	}
	return ok, err
}

// NewKey меняет ключ входа, отключает все телефоны и обрывает их соединения.
func (m *Manager) NewKey() error {
	ids, err := m.keys.NewKey()
	if err != nil {
		return err
	}
	m.guard.cut(ids, true)
	m.logf("remote: новый ключ входа, отключено телефонов: %d", len(ids))
	return nil
}

func (m *Manager) set(st Status) {
	m.mu.Lock()
	m.setLocked(st)
	m.mu.Unlock()
	m.onChange()
}

// setLocked меняет состояние; Since остаётся прежним, если само состояние то же.
func (m *Manager) setLocked(st Status) {
	st.Since = time.Now()
	if st.State == m.status.State {
		st.Since = m.status.Since
	}
	m.status = st
}

// run поднимает узел и при ошибке пробует снова, пока не отменён ctx.
func (m *Manager) run(ctx context.Context, cfg Config, done chan struct{}) {
	defer close(done)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			m.set(Status{State: StateStarting})
		}
		err := m.serve(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("сервер остановился")
		}
		m.logf("remote: %v", err)
		m.set(Status{State: StateError, Err: err.Error(), ErrCode: errorCode(err)})
		pause := m.retry[min(attempt, len(m.retry)-1)]
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		case <-time.After(pause):
		}
	}
}

// serve — одна попытка: узел, слушатель, сертификат и HTTPS-сервер до отмены ctx или сбоя.
// Узел закрывается только здесь, при выходе: не раньше, чем он отслужил (грабля #1 gate).
func (m *Manager) serve(ctx context.Context, cfg Config) error {
	if _, err := m.keys.EnsureKey(); err != nil {
		return err
	}
	node := m.newNode(cfg)
	var closeOnce sync.Once
	closeNode := func() { closeOnce.Do(func() { _ = node.Close() }) }
	defer closeNode()

	host, err := m.up(ctx, node)
	if err != nil {
		return err
	}
	// Дальше вызовы узла не знают о ctx; отмена закрывает узел, и они возвращаются.
	stop := context.AfterFunc(ctx, closeNode)
	defer stop()

	ln, err := node.Listen(cfg.Funnel)
	if err != nil {
		return err
	}
	srv := NewServer(m.guard)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	defer func() {
		srv.Close()
		// Close не трогает соединения, отданные WebSocket: их обрывает Guard.
		m.guard.cut(nil, true)
	}()

	m.set(Status{State: StateCertificate, Host: host})
	certCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	err = node.Cert(certCtx, host)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	m.set(Status{State: StateRunning, Host: host})
	via := "в tailnet"
	if cfg.Funnel {
		via = "в tailnet и в интернете через Funnel"
	}
	m.logf("remote: https://%s %s", host, via)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-served:
		return err
	}
}

// up ждёт входа в сеть и, пока ждёт, показывает ссылку для входа в Tailscale.
func (m *Manager) up(ctx context.Context, node Node) (string, error) {
	watch, stop := context.WithCancel(ctx)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		m.watchLogin(watch, node)
	}()
	host, err := node.Up(ctx)
	stop()
	<-watched
	if err != nil {
		return "", err
	}
	return host, nil
}

func (m *Manager) watchLogin(ctx context.Context, node Node) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		state, url, err := node.Login(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			next := Status{State: StateStarting}
			switch state {
			case "NeedsLogin":
				if url != "" {
					next = Status{State: StateNeedsLogin, LoginURL: url}
				}
			case "NeedsMachineAuth":
				// Вход прошёл, но устройство ждёт одобрения в админ-панели.
				next = Status{State: StateNeedsLogin, LoginURL: "https://login.tailscale.com/admin/machines"}
			}
			m.mu.Lock()
			waiting := m.status.State == StateStarting || m.status.State == StateNeedsLogin
			changed := waiting && (m.status.State != next.State || m.status.LoginURL != next.LoginURL)
			if changed {
				m.setLocked(next)
			}
			m.mu.Unlock()
			if changed {
				m.onChange()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// errorCode — известные причины, которые человек исправляет в админ-панели Tailscale.
func errorCode(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, `"funnel" node attribute`) || strings.Contains(text, "not allowed for funnel"):
		return "no_funnel"
	case strings.Contains(text, "MagicDNS") || strings.Contains(text, "HTTPS must be enabled") ||
		strings.Contains(text, "enable HTTPS"):
		return "no_https"
	}
	return ""
}
