//go:build windows

package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Manager управляет правилами файрвола для маршрутизации трафика (Windows заглушка)
// Manager manages firewall rules for traffic routing (Windows stub)
type Manager struct {
	mu      sync.RWMutex
	config  *config.Config
	started bool
	onLog   func(level, message string)

	backend   FirewallBackend
	tableName string
	chainName string
	markValue string

	interfaces []string
	sourceIPs  []string

	// Stats / Статистика
	rulesApplied int
	rulesFailed  int
	lastApply    time.Time
}

// NewManager создает новый менеджер файрвола (Windows заглушка)
// NewManager creates a new firewall manager (Windows stub)
func NewManager(opts Options) *Manager {
	m := &Manager{
		config:     opts.Config,
		onLog:      opts.OnLog,
		tableName:  TableName,
		chainName:  ChainName,
		markValue:  MarkValue,
		interfaces: opts.Config.Settings.SourceNetworkInterfaces,
	}

	m.backend = FirewallBackendWindows // Будет использовать Windows Firewall / Will use Windows Firewall

	return m
}

// Start starts the firewall manager (Windows stub)
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.log("info", "Starting firewall manager (Windows stub)")
	m.started = true
	m.log("info", "Firewall manager started (Windows - limited functionality)")
	return nil
}

// Stop stops the firewall manager (Windows stub)
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.log("info", "Stopping firewall manager")
	m.started = false
	m.log("info", "Firewall manager stopped")
	return nil
}

// Reload reloads firewall configuration (Windows stub)
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	m.interfaces = cfg.Settings.SourceNetworkInterfaces
	m.log("info", "Firewall configuration reloaded (Windows stub)")
	return nil
}

// GetStatus returns firewall manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"running":       m.started,
		"backend":       "windows",
		"table_name":    m.tableName,
		"chain_name":    m.chainName,
		"mark_value":    m.markValue,
		"interfaces":    m.interfaces,
		"source_ips":    m.sourceIPs,
		"rules_applied": m.rulesApplied,
		"rules_failed":  m.rulesFailed,
		"last_apply":    "",
		"note":          "Windows firewall management not fully implemented",
	}
}

// GetStatusJSON returns status as JSON
func (m *Manager) GetStatusJSON() string {
	data, _ := json.MarshalIndent(m.GetStatus(), "", "  ")
	return string(data)
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		m.onLog(level, "[firewall] "+msg)
	}
}

// AddSourceIP adds a source IP to routing (stub)
func (m *Manager) AddSourceIP(ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sourceIPs = append(m.sourceIPs, ip)
	return nil
}

// RemoveSourceIP removes a source IP from routing (stub)
func (m *Manager) RemoveSourceIP(ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, src := range m.sourceIPs {
		if src == ip {
			m.sourceIPs = append(m.sourceIPs[:i], m.sourceIPs[i+1:]...)
			break
		}
	}
	return nil
}

// SetNFQueue is a no-op on Windows (stub)
func (m *Manager) SetNFQueue(opts *NFQueueOptions) error {
	return nil
}
