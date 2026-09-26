//go:build !windows
// +build !windows

package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Ensure wg is initialized in NewManager
func init() {
	// This ensures the package initializes correctly
}

// Manager manages firewall rules for traffic routing
type Manager struct {
	mu          sync.RWMutex
	config      *config.Config
	ctx         context.Context
	cancel      context.CancelFunc
	started     bool
	onLog       func(level, message string)
	wg          sync.WaitGroup
	
	backend     FirewallBackend
	tableName   string
	chainName   string
	markValue   string
	
	// Rule tracking
	appliedRules  map[string]bool
	interfaces    []string
	sourceIPs     []string
	
	// Stats
	rulesApplied  int
	rulesFailed   int
	lastApply     time.Time
}

// NewManager creates a new firewall manager
func NewManager(opts Options) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		config:       opts.Config,
		ctx:          ctx,
		cancel:       cancel,
		onLog:        opts.OnLog,
		tableName:    "podkop",
		chainName:    "podkop-chain",
		markValue:    "0x08000000",
		appliedRules: make(map[string]bool),
		interfaces:   opts.Config.Settings.SourceNetworkInterfaces,
		wg:           sync.WaitGroup{},
	}
	
	// Detect backend
	m.backend = m.detectBackend()
	
	return m
}

// detectBackend detects the available firewall backend
func (m *Manager) detectBackend() FirewallBackend {
	// Check for nftables
	if _, err := exec.LookPath("nft"); err == nil {
		return FirewallBackendNFTables
	}
	
	// Check for iptables
	if _, err := exec.LookPath("iptables"); err == nil {
		return FirewallBackendIPTables
	}
	
	// Check for RouterOS (MikroTik)
	if runtime.GOOS == "linux" && fileExists("/etc/routeros") {
		return FirewallBackendRouterOS
	}
	
	return FirewallBackendNFTables // Default
}

func fileExists(path string) bool {
	_, err := exec.LookPath(path)
	return err == nil
}

// Start starts the firewall manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return nil
	}
	
	m.log("info", "Starting firewall manager with backend: %s", m.backend)
	
	// Create table and chains
	if err := m.createTableAndChains(); err != nil {
		return fmt.Errorf("create table/chains: %w", err)
	}
	
	// Apply initial rules
	if err := m.applyRules(); err != nil {
		return fmt.Errorf("apply rules: %w", err)
	}
	
	m.started = true
	m.log("info", "Firewall manager started")
	return nil
}

// Stop stops the firewall manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started {
		return nil
	}
	
	m.log("info", "Stopping firewall manager")
	
	// Remove rules
	m.removeRules()
	
	// Clean up table/chains
	m.cleanupTableAndChains()
	
	m.started = false
	m.cancel()
	m.wg.Wait()
	m.log("info", "Firewall manager stopped")
	return nil
}

// Reload reloads firewall configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.config = cfg
	m.interfaces = cfg.Settings.SourceNetworkInterfaces
	
	// Re-apply rules
	m.removeRules()
	if err := m.applyRules(); err != nil {
		return fmt.Errorf("apply rules: %w", err)
	}
	
	m.log("info", "Firewall rules reloaded")
	return nil
}

// GetStatus returns firewall manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return map[string]interface{}{
		"running":        m.started,
		"backend":        m.backend,
		"table_name":     m.tableName,
		"chain_name":     m.chainName,
		"mark_value":     m.markValue,
		"interfaces":     m.interfaces,
		"source_ips":     m.sourceIPs,
		"rules_applied":  m.rulesApplied,
		"rules_failed":   m.rulesFailed,
		"last_apply":     m.lastApply.Format(time.RFC3339),
		"active_rules":   len(m.appliedRules),
	}
}

// GetStatusJSON returns status as JSON
func (m *Manager) GetStatusJSON() string {
	data, _ := json.MarshalIndent(m.GetStatus(), "", "  ")
	return string(data)
}

// createTableAndChains creates the firewall table and chains
func (m *Manager) createTableAndChains() error {
	switch m.backend {
	case FirewallBackendNFTables:
		return m.createNFTablesStructures()
	case FirewallBackendIPTables:
		return m.createIPTablesStructures()
	case FirewallBackendRouterOS:
		return m.createRouterOSStructures()
	}
	return fmt.Errorf("unsupported backend: %s", m.backend)
}

// createNFTablesStructures creates nftables table and chains
func (m *Manager) createNFTablesStructures() error {
	cmds := [][]string{
		{"nft", "add", "table", "inet", m.tableName},
		{"nft", "add", "chain", "inet", m.tableName, m.chainName, "{ type filter hook prerouting priority -150; }"},
		{"nft", "add", "set", "inet", m.tableName, "local_v4", "{ type ipv4_addr; flags interval; }"},
		{"nft", "add", "set", "inet", m.tableName, "local_v6", "{ type ipv6_addr; flags interval; }"},
		{"nft", "add", "set", "inet", m.tableName, "forkop_subnets", "{ type ipv4_addr; flags interval; }"},
		{"nft", "add", "set", "inet", m.tableName, "forkop_subnets6", "{ type ipv6_addr; flags interval; }"},
		{"nft", "add", "set", "inet", m.tableName, "forkop_ports", "{ type inet_service; flags interval; }"},
		{"nft", "add", "set", "inet", m.tableName, "forkop_interfaces", "{ type ifname; }"},
	}
	
	for _, cmd := range cmds {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			// Ignore "File exists" errors
			if !strings.Contains(string(out), "File exists") && !strings.Contains(string(out), "already exists") {
				m.log("warn", "nft command failed: %s - %s", cmd, string(out))
			}
		}
	}
	
	// Populate local subnets
	m.populateLocalSubnets()
	
	return nil
}

// createIPTablesStructures creates iptables chains
func (m *Manager) createIPTablesStructures() error {
	cmds := [][]string{
		{"iptables", "-t", "mangle", "-N", m.chainName},
		{"iptables", "-t", "mangle", "-A", "PREROUTING", "-j", m.chainName},
		{"ip6tables", "-t", "mangle", "-N", m.chainName},
		{"ip6tables", "-t", "mangle", "-A", "PREROUTING", "-j", m.chainName},
	}
	
	for _, cmd := range cmds {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			if !strings.Contains(string(out), "Chain already exists") {
				m.log("warn", "iptables command failed: %s - %s", cmd, string(out))
			}
		}
	}
	
	return nil
}

// createRouterOSStructures creates RouterOS firewall structures
func (m *Manager) createRouterOSStructures() error {
	// RouterOS uses /ip firewall mangle and routing marks
	// This would use the RouterOS API
	m.log("info", "RouterOS firewall backend - using API")
	return nil
}

// cleanupTableAndChains removes the firewall table and chains
func (m *Manager) cleanupTableAndChains() {
	switch m.backend {
	case FirewallBackendNFTables:
		exec.Command("nft", "delete", "table", "inet", m.tableName).Run()
	case FirewallBackendIPTables:
		exec.Command("iptables", "-t", "mangle", "-F", m.chainName).Run()
		exec.Command("iptables", "-t", "mangle", "-X", m.chainName).Run()
		exec.Command("ip6tables", "-t", "mangle", "-F", m.chainName).Run()
		exec.Command("ip6tables", "-t", "mangle", "-X", m.chainName).Run()
	case FirewallBackendRouterOS:
		// Cleanup via RouterOS API
	}
}

// applyRules applies routing rules
func (m *Manager) applyRules() error {
	// Mark traffic from source interfaces
	for _, iface := range m.interfaces {
		if err := m.markInterfaceTraffic(iface); err != nil {
			m.rulesFailed++
			m.log("error", "Failed to mark traffic for interface %s: %v", iface, err)
		} else {
			m.rulesApplied++
		}
	}
	
	// Apply routing rules for marked traffic
	if err := m.applyRoutingRules(); err != nil {
		m.rulesFailed++
		m.log("error", "Failed to apply routing rules: %v", err)
	}
	
	// Apply DNS rules
	if err := m.applyDNSRules(); err != nil {
		m.rulesFailed++
		m.log("error", "Failed to apply DNS rules: %v", err)
	}
	
	m.lastApply = time.Now()
	return nil
}

// removeRules removes all applied rules
func (m *Manager) removeRules() {
	// Remove interface marking rules
	for _, iface := range m.interfaces {
		m.unmarkInterfaceTraffic(iface)
	}
	
	// Flush chains
	switch m.backend {
	case FirewallBackendNFTables:
		exec.Command("nft", "flush", "chain", "inet", m.tableName, m.chainName).Run()
	case FirewallBackendIPTables:
		exec.Command("iptables", "-t", "mangle", "-F", m.chainName).Run()
		exec.Command("ip6tables", "-t", "mangle", "-F", m.chainName).Run()
	}
	
	m.appliedRules = make(map[string]bool)
}

// markInterfaceTraffic marks traffic from an interface
func (m *Manager) markInterfaceTraffic(iface string) error {
	ruleKey := "mark-" + iface
	if m.appliedRules[ruleKey] {
		return nil
	}
	
	switch m.backend {
	case FirewallBackendNFTables:
		cmd := exec.Command("nft", "add", "rule", "inet", m.tableName, m.chainName,
			"iifname", iface, "meta", "mark", "set", m.markValue)
		return cmd.Run()
	case FirewallBackendIPTables:
		cmd := exec.Command("iptables", "-t", "mangle", "-A", m.chainName,
			"-i", iface, "-j", "MARK", "--set-mark", m.markValue)
		return cmd.Run()
	}
	return nil
}

// unmarkInterfaceTraffic removes interface marking
func (m *Manager) unmarkInterfaceTraffic(iface string) {
	ruleKey := "mark-" + iface
	delete(m.appliedRules, ruleKey)
	
	switch m.backend {
	case FirewallBackendNFTables:
		// Would need to track rule handles for precise deletion
		exec.Command("nft", "delete", "rule", "inet", m.tableName, m.chainName,
			"iifname", iface, "meta", "mark", "set", m.markValue).Run()
	case FirewallBackendIPTables:
		exec.Command("iptables", "-t", "mangle", "-D", m.chainName,
			"-i", iface, "-j", "MARK", "--set-mark", m.markValue).Run()
	}
}

// applyRoutingRules applies policy routing for marked traffic
func (m *Manager) applyRoutingRules() error {
	// Create routing table
	tableName := "podkop"
	
	// Add routing table entry
	exec.Command("ip", "route", "add", "default", "dev", "tun0", "table", tableName).Run()
	exec.Command("ip", "rule", "add", "fwmark", m.markValue, "lookup", tableName, "priority", "100").Run()
	
	// IPv6
	exec.Command("ip", "-6", "route", "add", "default", "dev", "tun0", "table", tableName).Run()
	exec.Command("ip", "-6", "rule", "add", "fwmark", m.markValue, "lookup", tableName, "priority", "100").Run()
	
	return nil
}

// applyDNSRules applies DNS interception rules
func (m *Manager) applyDNSRules() error {
	dnsPort := "53"
	
	switch m.backend {
	case FirewallBackendNFTables:
		// Redirect DNS to local resolver
		cmds := [][]string{
			{"nft", "add", "rule", "inet", m.tableName, m.chainName,
				"ip", "protocol", "udp", "udp", "dport", dnsPort,
				"counter", "redirect", "to", ":53"},
			{"nft", "add", "rule", "inet", m.tableName, m.chainName,
				"ip6", "nexthdr", "udp", "udp", "dport", dnsPort,
				"counter", "redirect", "to", ":53"},
		}
		for _, cmdArgs := range cmds {
			exec.Command(cmdArgs[0], cmdArgs[1:]...).Run()
		}
	case FirewallBackendIPTables:
		exec.Command("iptables", "-t", "nat", "-A", "PREROUTING",
			"-p", "udp", "--dport", dnsPort, "-j", "REDIRECT", "--to-port", "53").Run()
		exec.Command("ip6tables", "-t", "nat", "-A", "PREROUTING",
			"-p", "udp", "--dport", dnsPort, "-j", "REDIRECT", "--to-port", "53").Run()
	}
	
	return nil
}

// populateLocalSubnets populates local subnet sets
func (m *Manager) populateLocalSubnets() {
	localNets := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fe80::/10",
		"fc00::/7",
	}
	
	for _, netStr := range localNets {
		if strings.Contains(netStr, ":") {
			exec.Command("nft", "add", "element", "inet", m.tableName, "local_v6", "{", netStr, "}").Run()
			exec.Command("nft", "add", "element", "inet", m.tableName, "forkop_subnets6", "{", netStr, "}").Run()
		} else {
			exec.Command("nft", "add", "element", "inet", m.tableName, "local_v4", "{", netStr, "}").Run()
			exec.Command("nft", "add", "element", "inet", m.tableName, "forkop_subnets", "{", netStr, "}").Run()
		}
	}
	
// Get actual interface subnets
	links, _ := getLinks()
	for _, link := range links {
		// Use the platform-specific AddrList
		addrs, _ := getAddrList(link)
		for _, addr := range addrs {
			exec.Command("nft", "add", "element", "inet", m.tableName, "forkop_subnets", "{", addr.IPNet.String(), "}").Run()
			if addr.IP.To4() == nil {
				exec.Command("nft", "add", "element", "inet", m.tableName, "forkop_subnets6", "{", addr.IPNet.String(), "}").Run()
			}
		}
	}
}

// AddSourceIP adds a source IP to routing
func (m *Manager) AddSourceIP(ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.sourceIPs = append(m.sourceIPs, ip)
	
	switch m.backend {
	case FirewallBackendNFTables:
		return exec.Command("nft", "add", "element", "inet", m.tableName, "forkop_subnets", "{", ip, "}").Run()
	case FirewallBackendIPTables:
		return exec.Command("iptables", "-t", "mangle", "-A", m.chainName,
			"-s", ip, "-j", "MARK", "--set-mark", m.markValue).Run()
	}
	return nil
}

// RemoveSourceIP removes a source IP from routing
func (m *Manager) RemoveSourceIP(ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for i, src := range m.sourceIPs {
		if src == ip {
			m.sourceIPs = append(m.sourceIPs[:i], m.sourceIPs[i+1:]...)
			break
		}
	}
	
	switch m.backend {
	case FirewallBackendNFTables:
		return exec.Command("nft", "delete", "element", "inet", m.tableName, "forkop_subnets", "{", ip, "}").Run()
	case FirewallBackendIPTables:
		return exec.Command("iptables", "-t", "mangle", "-D", m.chainName,
			"-s", ip, "-j", "MARK", "--set-mark", m.markValue).Run()
	}
	return nil
}

var wg sync.WaitGroup

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		m.onLog(level, "[firewall] "+msg)
	}
}

