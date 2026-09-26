//go:build windows
// +build windows

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
	mu         sync.RWMutex
	config     *config.Config
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	onLog      func(level, message string)
	
	backend    FirewallBackend
	tableName  string
	chainName  string
	markValue  string
	
	// Rule tracking / Отслеживание правил
	appliedRules map[string]bool
	interfaces   []string
	sourceIPs    []string
	
	// Stats / Статистика
	rulesApplied int
	rulesFailed  int
	lastApply    time.Time
}

// NewManager создает новый менеджер файрвола (Windows заглушка)
// NewManager creates a new firewall manager (Windows stub)
func NewManager(opts Options) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		config:       opts.Config,
		ctx:          ctx,
		cancel:       cancel,
		onLog:        opts.OnLog,
		tableName:    "hydravpn",
		chainName:    "hydravpn-chain",
		markValue:    "0x08000000",
		appliedRules: make(map[string]bool),
		interfaces:   opts.Config.Settings.SourceNetworkInterfaces,
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
	m.cancel()
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
		"running":        m.started,
		"backend":        "windows",
		"table_name":     m.tableName,
		"chain_name":     m.chainName,
		"mark_value":     m.markValue,
		"interfaces":     m.interfaces,
		"source_ips":     m.sourceIPs,
		"rules_applied":  m.rulesApplied,
		"rules_failed":   m.rulesFailed,
		"last_apply":     m.lastApply.Format(time.RFC3339),
		"active_rules":   len(m.appliedRules),
		"note":           "Windows firewall management not fully implemented",
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