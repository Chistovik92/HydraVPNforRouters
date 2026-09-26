package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/miekg/dns"
)

// Manager monitors upstream DNS servers and fails over to bootstrap servers.
//
// Network checks are never performed while holding mu, so Stop and status
// requests are not blocked by slow or unreachable servers.
type Manager struct {
	mu      sync.RWMutex
	config  *config.Config
	cancel  context.CancelFunc
	ctx     context.Context
	started bool
	onLog   func(level, message string)
	wg      sync.WaitGroup

	// DNS servers
	primaryServers   []string
	bootstrapServers []string
	currentServers   []string
	serverIndex      int

	// Failover
	failoverEnabled  bool
	failoverTimer    *time.Timer
	checkInterval    time.Duration
	recoveryInterval time.Duration
	checkTimeout     time.Duration

	// Stats
	queriesTotal   uint64
	queriesSuccess uint64
	queriesFailed  uint64
	lastCheck      time.Time
	lastSuccess    time.Time
}

// Options for creating a new manager
type Options struct {
	Config *config.Config
	OnLog  func(level, message string)
}

// NewManager creates a new DNS manager
func NewManager(opts Options) *Manager {
	m := &Manager{onLog: opts.OnLog}
	m.applyConfig(opts.Config)
	return m
}

func (m *Manager) applyConfig(cfg *config.Config) {
	m.config = cfg
	m.primaryServers = append([]string(nil), cfg.Settings.DNSServers...)
	m.bootstrapServers = append([]string(nil), cfg.Settings.BootstrapDNSServers...)
	m.currentServers = m.primaryServers
	m.serverIndex = 0
	m.failoverEnabled = false
	m.checkInterval = positive(cfg.Settings.DNSCheckInterval, 10*time.Second)
	m.recoveryInterval = positive(cfg.Settings.DNSRecoveryCheckInterval, 60*time.Second)
	m.checkTimeout = positive(cfg.Settings.DNSCheckTimeout, 2*time.Second)
}

func positive(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// ServerAddress converts a configured DNS server ("1.1.1.1", "1.1.1.1:5353",
// "2606:4700::1111", "udp://…", "tcp://…", "tls://…") to a dial address and
// miekg/dns network. ok is false for transports that cannot be probed
// directly (DoH, DoQ).
func ServerAddress(server string) (addr, network string, ok bool) {
	network, port := "udp", "53"
	if strings.Contains(server, "://") {
		u, err := url.Parse(server)
		if err != nil || u.Hostname() == "" {
			return "", "", false
		}
		switch u.Scheme {
		case "udp":
		case "tcp":
			network = "tcp"
		case "tls":
			network, port = "tcp-tls", "853"
		default:
			return "", "", false
		}
		if u.Port() != "" {
			port = u.Port()
		}
		return net.JoinHostPort(u.Hostname(), port), network, true
	}
	if host, p, err := net.SplitHostPort(server); err == nil {
		return net.JoinHostPort(host, p), network, true
	}
	return net.JoinHostPort(strings.Trim(server, "[]"), port), network, true
}

// Start starts the DNS manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	m.log("info", "DNS manager started with servers: %v", m.currentServers)

	m.wg.Add(1)
	go m.healthCheckLoop(m.ctx, m.checkInterval)

	return nil
}

// Stop stops the DNS manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	m.cancel()
	if m.failoverTimer != nil {
		m.failoverTimer.Stop()
		m.failoverTimer = nil
	}
	m.mu.Unlock()

	// Wait without holding the lock: the check loop takes it too.
	m.wg.Wait()
	m.log("info", "DNS manager stopped")
	return nil
}

// Reload reloads DNS configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	if m.failoverTimer != nil {
		m.failoverTimer.Stop()
		m.failoverTimer = nil
	}
	oldInterval := m.checkInterval
	m.applyConfig(cfg)
	restart := m.started && oldInterval != m.checkInterval
	m.mu.Unlock()

	if restart {
		m.Stop()
		m.Start(context.Background())
	}

	m.log("info", "DNS configuration reloaded")
	return nil
}

// GetStatus returns DNS manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"running":           m.started,
		"primary_servers":   m.primaryServers,
		"bootstrap_servers": m.bootstrapServers,
		"current_servers":   m.currentServers,
		"server_index":      m.serverIndex,
		"failover_enabled":  m.failoverEnabled,
		"check_interval":    m.checkInterval.String(),
		"recovery_interval": m.recoveryInterval.String(),
		"queries_total":     m.queriesTotal,
		"queries_success":   m.queriesSuccess,
		"queries_failed":    m.queriesFailed,
		"last_check":        formatTime(m.lastCheck),
		"last_success":      formatTime(m.lastSuccess),
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// GetStatusJSON returns status as JSON
func (m *Manager) GetStatusJSON() string {
	data, _ := json.MarshalIndent(m.GetStatus(), "", "  ")
	return string(data)
}

// Resolve resolves a domain name
func (m *Manager) Resolve(name string, qtype uint16) (*dns.Msg, error) {
	m.mu.RLock()
	servers := m.currentServers
	timeout := m.checkTimeout
	m.mu.RUnlock()

	if len(servers) == 0 {
		return nil, fmt.Errorf("no DNS servers available")
	}

	for i, server := range servers {
		msg, err := query(server, name, qtype, timeout)
		m.mu.Lock()
		m.queriesTotal++
		if err == nil {
			m.queriesSuccess++
			m.lastSuccess = time.Now()
			m.serverIndex = i
		} else {
			m.queriesFailed++
		}
		m.mu.Unlock()
		if err == nil {
			return msg, nil
		}
		m.log("warn", "DNS query to %s failed: %v", server, err)
	}

	m.triggerFailover()
	return nil, fmt.Errorf("all DNS servers failed")
}

// query queries a specific DNS server
func query(server, name string, qtype uint16, timeout time.Duration) (*dns.Msg, error) {
	addr, network, ok := ServerAddress(server)
	if !ok {
		return nil, fmt.Errorf("unsupported DNS transport: %s", server)
	}
	client := &dns.Client{Timeout: timeout, Net: network}

	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), qtype)
	msg.RecursionDesired = true

	r, _, err := client.Exchange(msg, addr)
	if err != nil {
		return nil, err
	}
	if r.Rcode == dns.RcodeServerFailure || r.Rcode == dns.RcodeRefused {
		return nil, fmt.Errorf("rcode %s", dns.RcodeToString[r.Rcode])
	}
	return r, nil
}

// healthCheckLoop periodically checks DNS server health
func (m *Manager) healthCheckLoop(ctx context.Context, interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkServers(ctx)
		}
	}
}

// checkServers checks all DNS servers
func (m *Manager) checkServers(ctx context.Context) {
	m.mu.Lock()
	m.lastCheck = time.Now()
	primary, bootstrap, timeout := m.primaryServers, m.bootstrapServers, m.checkTimeout
	m.mu.Unlock()

	for _, server := range primary {
		if ctx.Err() != nil {
			return
		}
		if checkServer(server, timeout) {
			m.mu.Lock()
			m.lastSuccess = time.Now()
			m.mu.Unlock()
			return
		}
	}

	for _, server := range bootstrap {
		if ctx.Err() != nil {
			return
		}
		if checkServer(server, timeout) {
			m.triggerFailover()
			return
		}
	}

	m.log("error", "All DNS servers failed")
}

// checkServer checks a single DNS server. Transports that cannot be probed
// are treated as healthy so they never trigger a failover.
func checkServer(server string, timeout time.Duration) bool {
	if _, _, ok := ServerAddress(server); !ok {
		return true
	}
	_, err := query(server, "google.com", dns.TypeA, timeout)
	return err == nil
}

// triggerFailover switches to bootstrap servers
func (m *Manager) triggerFailover() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failoverEnabled || !m.started || len(m.bootstrapServers) == 0 {
		return
	}

	m.log("warn", "Triggering DNS failover to bootstrap servers")
	m.currentServers = m.bootstrapServers
	m.serverIndex = 0
	m.failoverEnabled = true
	m.scheduleRecoveryLocked()
}

func (m *Manager) scheduleRecoveryLocked() {
	if m.failoverTimer != nil {
		m.failoverTimer.Stop()
	}
	m.failoverTimer = time.AfterFunc(m.recoveryInterval, m.attemptRecovery)
}

// attemptRecovery tries to recover to primary servers
func (m *Manager) attemptRecovery() {
	m.mu.RLock()
	if !m.started || !m.failoverEnabled {
		m.mu.RUnlock()
		return
	}
	primary, timeout := m.primaryServers, m.checkTimeout
	m.mu.RUnlock()

	m.log("info", "Attempting DNS recovery to primary servers")
	recovered := false
	for _, server := range primary {
		if checkServer(server, timeout) {
			recovered = true
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || !m.failoverEnabled {
		return
	}
	if recovered {
		m.currentServers = m.primaryServers
		m.serverIndex = 0
		m.failoverEnabled = false
		m.failoverTimer = nil
		m.log("info", "DNS recovery successful")
		return
	}
	m.log("warn", "DNS recovery failed, staying on bootstrap servers")
	m.scheduleRecoveryLocked()
}

// GetCurrentServers returns current DNS servers
func (m *Manager) GetCurrentServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.currentServers...)
}

// SetServers manually sets DNS servers
func (m *Manager) SetServers(servers []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentServers = append([]string(nil), servers...)
	m.serverIndex = 0
	m.log("info", "DNS servers manually set: %v", servers)
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		m.onLog(level, "[dns] "+fmt.Sprintf(format, args...))
	}
}
