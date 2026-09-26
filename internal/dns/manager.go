package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/miekg/dns"
)

// Manager manages DNS resolution and failover
type Manager struct {
	mu           sync.RWMutex
	config       *config.Config
	ctx          context.Context
	cancel       context.CancelFunc
	started      bool
	onLog        func(level, message string)
	wg           sync.WaitGroup
	
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
	
	// Cache
	cache            map[string]*dns.Msg
	cacheMu          sync.RWMutex
	cacheTTL         time.Duration
	
	// Stats
	queriesTotal     uint64
	queriesSuccess   uint64
	queriesFailed    uint64
	lastCheck        time.Time
	lastSuccess      time.Time
}

// Options for creating a new manager
type Options struct {
	Config *config.Config
	OnLog  func(level, message string)
}

// NewManager creates a new DNS manager
func NewManager(opts Options) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		config:           opts.Config,
		ctx:              ctx,
		cancel:           cancel,
		onLog:            opts.OnLog,
		primaryServers:   opts.Config.Settings.DNSServers,
		bootstrapServers: opts.Config.Settings.BootstrapDNSServers,
		currentServers:   opts.Config.Settings.DNSServers,
		checkInterval:    opts.Config.Settings.DNSCheckInterval,
		recoveryInterval: opts.Config.Settings.DNSRecoveryCheckInterval,
		checkTimeout:     opts.Config.Settings.DNSCheckTimeout,
		cache:            make(map[string]*dns.Msg),
		cacheTTL:         time.Duration(opts.Config.Settings.DNSRewriteTTL) * time.Second,
	}
	
	return m
}

// Start starts the DNS manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return nil
	}
	
	m.started = true
	m.log("info", "DNS manager started with servers: %v", m.currentServers)
	
	// Start health check loop
	m.wg.Add(1)
	go m.healthCheckLoop()
	
	return nil
}

// Stop stops the DNS manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started {
		return nil
	}
	
	m.started = false
	m.cancel()
	
	if m.failoverTimer != nil {
		m.failoverTimer.Stop()
	}
	
	m.wg.Wait()
	m.log("info", "DNS manager stopped")
	
	return nil
}

// Reload reloads DNS configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.config = cfg
	m.primaryServers = cfg.Settings.DNSServers
	m.bootstrapServers = cfg.Settings.BootstrapDNSServers
	m.checkInterval = cfg.Settings.DNSCheckInterval
	m.recoveryInterval = cfg.Settings.DNSRecoveryCheckInterval
	m.checkTimeout = cfg.Settings.DNSCheckTimeout
	m.cacheTTL = time.Duration(cfg.Settings.DNSRewriteTTL) * time.Second
	
	// Reset to primary servers
	m.currentServers = m.primaryServers
	m.serverIndex = 0
	
	m.log("info", "DNS configuration reloaded")
	return nil
}

// GetStatus returns DNS manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return map[string]interface{}{
		"running":             m.started,
		"primary_servers":     m.primaryServers,
		"bootstrap_servers":   m.bootstrapServers,
		"current_servers":     m.currentServers,
		"server_index":        m.serverIndex,
		"failover_enabled":    m.failoverEnabled,
		"check_interval":      m.checkInterval.String(),
		"recovery_interval":   m.recoveryInterval.String(),
		"queries_total":       m.queriesTotal,
		"queries_success":     m.queriesSuccess,
		"queries_failed":      m.queriesFailed,
		"last_check":          m.lastCheck.Format(time.RFC3339),
		"last_success":        m.lastSuccess.Format(time.RFC3339),
		"cache_size":          len(m.cache),
	}
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
	m.mu.RUnlock()
	
	if len(servers) == 0 {
		return nil, fmt.Errorf("no DNS servers available")
	}
	
	// Try each server
	for i, server := range servers {
		msg, err := m.queryServer(server, name, qtype)
		if err == nil {
			m.mu.Lock()
			m.queriesTotal++
			m.queriesSuccess++
			m.lastSuccess = time.Now()
			m.serverIndex = i
			m.mu.Unlock()
			return msg, nil
		}
		
		m.mu.Lock()
		m.queriesTotal++
		m.queriesFailed++
		m.mu.Unlock()
		
		m.log("warn", "DNS query to %s failed: %v", server, err)
	}
	
	// All servers failed, trigger failover
	m.triggerFailover()
	return nil, fmt.Errorf("all DNS servers failed")
}

// queryServer queries a specific DNS server
func (m *Manager) queryServer(server, name string, qtype uint16) (*dns.Msg, error) {
	client := &dns.Client{
		Timeout: m.checkTimeout,
		Net:     "udp",
	}
	
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), qtype)
	msg.RecursionDesired = true
	
	r, _, err := client.Exchange(msg, server+":53")
	return r, err
}

// healthCheckLoop periodically checks DNS server health
func (m *Manager) healthCheckLoop() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkServers()
		}
	}
}

// checkServers checks all DNS servers
func (m *Manager) checkServers() {
	m.mu.Lock()
	m.lastCheck = time.Now()
	m.mu.Unlock()
	
	// Check primary servers
	for _, server := range m.primaryServers {
		if m.checkServer(server) {
			m.mu.Lock()
			m.lastSuccess = time.Now()
			m.mu.Unlock()
			return // At least one server works
		}
	}
	
	// All primary failed, check bootstrap
	for _, server := range m.bootstrapServers {
		if m.checkServer(server) {
			m.triggerFailover()
			return
		}
	}
	
	// All failed
	m.log("error", "All DNS servers failed")
}

// checkServer checks a single DNS server
func (m *Manager) checkServer(server string) bool {
	client := &dns.Client{
		Timeout: m.checkTimeout,
		Net:     "udp",
	}
	
	msg := new(dns.Msg)
	msg.SetQuestion("google.com.", dns.TypeA)
	msg.RecursionDesired = true
	
	_, _, err := client.Exchange(msg, server+":53")
	return err == nil
}

// triggerFailover switches to bootstrap servers
func (m *Manager) triggerFailover() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.failoverEnabled {
		return // Already in failover
	}
	
	m.log("warn", "Triggering DNS failover to bootstrap servers")
	m.currentServers = m.bootstrapServers
	m.serverIndex = 0
	m.failoverEnabled = true
	
	// Schedule recovery check
	if m.failoverTimer != nil {
		m.failoverTimer.Stop()
	}
	m.failoverTimer = time.AfterFunc(m.recoveryInterval, m.attemptRecovery)
}

// attemptRecovery tries to recover to primary servers
func (m *Manager) attemptRecovery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.log("info", "Attempting DNS recovery to primary servers")
	
	for _, server := range m.primaryServers {
		if m.checkServer(server) {
			m.currentServers = m.primaryServers
			m.serverIndex = 0
			m.failoverEnabled = false
			m.log("info", "DNS recovery successful")
			return
		}
	}
	
	m.log("warn", "DNS recovery failed, staying on bootstrap servers")
	// Retry later
	m.failoverTimer = time.AfterFunc(m.recoveryInterval, m.attemptRecovery)
}

// GetCurrentServers returns current DNS servers
func (m *Manager) GetCurrentServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentServers
}

// SetServers manually sets DNS servers
func (m *Manager) SetServers(servers []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentServers = servers
	m.serverIndex = 0
	m.log("info", "DNS servers manually set: %v", servers)
}

// FlushCache clears the DNS cache
func (m *Manager) FlushCache() {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.cache = make(map[string]*dns.Msg)
	m.log("info", "DNS cache flushed")
}

var wg sync.WaitGroup

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		m.onLog(level, "[dns] "+msg)
	}
}

