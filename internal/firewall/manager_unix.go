//go:build !windows

package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Manager redirects LAN traffic to the sing-box tproxy inbound and, when
// enabled, queues packets to nfqws (zapret).
//
// nftables rules are generated as one script and applied atomically with
// "nft -f -", so a reload replaces the whole table instead of appending
// duplicate rules.
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
	nfqueue    *NFQueueOptions

	// Stats
	rulesApplied int
	rulesFailed  int
	lastApply    time.Time
	lastError    string
}

// NewManager creates a new firewall manager
func NewManager(opts Options) *Manager {
	m := &Manager{
		config:     opts.Config,
		onLog:      opts.OnLog,
		tableName:  TableName,
		chainName:  ChainName,
		markValue:  MarkValue,
		interfaces: opts.Config.Settings.SourceNetworkInterfaces,
	}
	m.backend = detectBackend()
	return m
}

// detectBackend detects the available firewall backend
func detectBackend() FirewallBackend {
	if _, err := exec.LookPath("nft"); err == nil {
		return FirewallBackendNFTables
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		return FirewallBackendIPTables
	}
	return FirewallBackendNFTables
}

// Start applies the rules
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.log("info", "Starting firewall manager with backend: %s", m.backend)
	if err := m.applyLocked(); err != nil {
		m.cleanupLocked()
		return err
	}

	m.started = true
	m.log("info", "Firewall manager started")
	return nil
}

// Stop removes all rules
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.log("info", "Stopping firewall manager")
	m.cleanupLocked()
	m.started = false
	m.log("info", "Firewall manager stopped")
	return nil
}

// Reload re-applies rules for the new configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	m.interfaces = cfg.Settings.SourceNetworkInterfaces
	if !m.started {
		return nil
	}
	if err := m.applyLocked(); err != nil {
		return err
	}
	m.log("info", "Firewall rules reloaded")
	return nil
}

// SetNFQueue enables (opts != nil) or disables queueing to nfqws.
func (m *Manager) SetNFQueue(opts *NFQueueOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nfqueue = opts
	if !m.started {
		return nil
	}
	return m.applyLocked()
}

// GetStatus returns firewall manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := map[string]interface{}{
		"running":       m.started,
		"backend":       m.backend,
		"table_name":    m.tableName,
		"chain_name":    m.chainName,
		"mark_value":    m.markValue,
		"route_table":   RouteTable,
		"tproxy_port":   config.TProxyPort,
		"interfaces":    m.interfaces,
		"source_ips":    m.sourceIPs,
		"nfqueue":       m.nfqueue != nil,
		"rules_applied": m.rulesApplied,
		"rules_failed":  m.rulesFailed,
		"last_error":    m.lastError,
		"last_apply":    "",
	}
	if !m.lastApply.IsZero() {
		status["last_apply"] = m.lastApply.Format(time.RFC3339)
	}
	return status
}

// GetStatusJSON returns status as JSON
func (m *Manager) GetStatusJSON() string {
	data, _ := json.MarshalIndent(m.GetStatus(), "", "  ")
	return string(data)
}

// applyLocked (re)creates all firewall rules and the policy routing entry.
func (m *Manager) applyLocked() error {
	var err error
	switch m.backend {
	case FirewallBackendNFTables:
		err = runWithStdin("nft", m.nftScript(), "-f", "-")
	case FirewallBackendIPTables:
		err = m.applyIPTables()
	default:
		err = fmt.Errorf("unsupported backend: %s", m.backend)
	}
	if err == nil {
		err = m.applyPolicyRouting()
	}

	m.lastApply = time.Now()
	if err != nil {
		m.rulesFailed++
		m.lastError = err.Error()
		return fmt.Errorf("apply firewall rules: %w", err)
	}
	m.rulesApplied++
	m.lastError = ""
	return nil
}

// nftScript renders the complete table definition. The leading
// "add table"+"delete table" pair makes the script idempotent.
func (m *Manager) nftScript() string {
	var b strings.Builder
	mark := m.markValue
	tproxy := fmt.Sprintf("meta mark set %s tproxy ip to 127.0.0.1:%d accept", mark, config.TProxyPort)

	fmt.Fprintf(&b, "add table inet %s\n", m.tableName)
	fmt.Fprintf(&b, "delete table inet %s\n", m.tableName)
	fmt.Fprintf(&b, "table inet %s {\n", m.tableName)

	writeSet(&b, "local_v4", "ipv4_addr", localSubnets())
	writeSet(&b, "source_v4", "ipv4_addr", m.sourceIPs)

	fmt.Fprintf(&b, "\tchain %s {\n", m.chainName)
	b.WriteString("\t\ttype filter hook prerouting priority mangle; policy accept;\n")
	b.WriteString("\t\tmeta nfproto != ipv4 return\n")
	b.WriteString("\t\tip daddr @local_v4 return\n")
	if ifaces := quoteAll(m.interfaces); len(ifaces) > 0 {
		fmt.Fprintf(&b, "\t\tiifname { %s } meta l4proto { tcp, udp } %s\n", strings.Join(ifaces, ", "), tproxy)
	}
	fmt.Fprintf(&b, "\t\tip saddr @source_v4 meta l4proto { tcp, udp } %s\n", tproxy)
	b.WriteString("\t}\n")

	if q := m.nfqueue; q != nil {
		ports := joinPorts(q.TCPPorts)
		b.WriteString("\tchain zapret_post {\n")
		b.WriteString("\t\ttype filter hook postrouting priority mangle; policy accept;\n")
		fmt.Fprintf(&b, "\t\tmeta mark & %s != 0 return\n", q.DesyncMark)
		if ports != "" {
			fmt.Fprintf(&b, "\t\ttcp dport { %s } ct original packets 1-9 queue num %d bypass\n", ports, q.QueueNum)
		}
		if udp := joinPorts(q.UDPPorts); udp != "" {
			fmt.Fprintf(&b, "\t\tudp dport { %s } ct original packets 1-9 queue num %d bypass\n", udp, q.QueueNum)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func writeSet(b *strings.Builder, name, typ string, elems []string) {
	fmt.Fprintf(b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\tauto-merge\n", name, typ)
	if len(elems) > 0 {
		fmt.Fprintf(b, "\t\telements = { %s }\n", strings.Join(elems, ", "))
	}
	b.WriteString("\t}\n")
}

func quoteAll(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, strconv.Quote(it))
		}
	}
	return out
}

func joinPorts(ports []int) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ", ")
}

// localSubnets returns reserved IPv4 ranges plus the networks of local
// interfaces; traffic to them is never redirected.
func localSubnets() []string {
	nets := map[string]bool{
		"0.0.0.0/8":      true,
		"10.0.0.0/8":     true,
		"100.64.0.0/10":  true,
		"127.0.0.0/8":    true,
		"169.254.0.0/16": true,
		"172.16.0.0/12":  true,
		"192.168.0.0/16": true,
		"224.0.0.0/4":    true,
		"240.0.0.0/4":    true,
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			masked := &net.IPNet{IP: ipnet.IP.Mask(ipnet.Mask).To4(), Mask: ipnet.Mask}
			nets[masked.String()] = true
		}
	}
	out := make([]string, 0, len(nets))
	for n := range nets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// applyIPTables creates an equivalent ruleset for iptables (IPv4 only).
func (m *Manager) applyIPTables() error {
	m.cleanupIPTables()

	mark := m.markValue + "/" + m.markValue
	port := strconv.Itoa(config.TProxyPort)
	rules := [][]string{
		{"-t", "mangle", "-N", m.chainName},
	}
	for _, n := range localSubnets() {
		rules = append(rules, []string{"-t", "mangle", "-A", m.chainName, "-d", n, "-j", "RETURN"})
	}
	for _, iface := range m.interfaces {
		for _, proto := range []string{"tcp", "udp"} {
			rules = append(rules, []string{"-t", "mangle", "-A", m.chainName, "-i", iface, "-p", proto,
				"-j", "TPROXY", "--on-ip", "127.0.0.1", "--on-port", port, "--tproxy-mark", mark})
		}
	}
	for _, src := range m.sourceIPs {
		for _, proto := range []string{"tcp", "udp"} {
			rules = append(rules, []string{"-t", "mangle", "-A", m.chainName, "-s", src, "-p", proto,
				"-j", "TPROXY", "--on-ip", "127.0.0.1", "--on-port", port, "--tproxy-mark", mark})
		}
	}
	rules = append(rules, []string{"-t", "mangle", "-I", "PREROUTING", "-j", m.chainName})

	if q := m.nfqueue; q != nil {
		post := m.chainName + "-post"
		rules = append(rules, []string{"-t", "mangle", "-N", post})
		base := []string{"-m", "connbytes", "--connbytes-dir=original", "--connbytes-mode=packets", "--connbytes", "1:9",
			"-m", "mark", "!", "--mark", q.DesyncMark + "/" + q.DesyncMark,
			"-j", "NFQUEUE", "--queue-num", strconv.Itoa(q.QueueNum), "--queue-bypass"}
		if len(q.TCPPorts) > 0 {
			r := []string{"-t", "mangle", "-A", post, "-p", "tcp", "-m", "multiport", "--dports", strings.ReplaceAll(joinPorts(q.TCPPorts), " ", "")}
			rules = append(rules, append(r, base...))
		}
		if len(q.UDPPorts) > 0 {
			r := []string{"-t", "mangle", "-A", post, "-p", "udp", "-m", "multiport", "--dports", strings.ReplaceAll(joinPorts(q.UDPPorts), " ", "")}
			rules = append(rules, append(r, base...))
		}
		rules = append(rules, []string{"-t", "mangle", "-I", "POSTROUTING", "-j", post})
	}

	for _, r := range rules {
		if out, err := exec.Command("iptables", r...).CombinedOutput(); err != nil {
			return fmt.Errorf("iptables %s: %v: %s", strings.Join(r, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (m *Manager) cleanupIPTables() {
	post := m.chainName + "-post"
	for _, args := range [][]string{
		{"-t", "mangle", "-D", "PREROUTING", "-j", m.chainName},
		{"-t", "mangle", "-F", m.chainName},
		{"-t", "mangle", "-X", m.chainName},
		{"-t", "mangle", "-D", "POSTROUTING", "-j", post},
		{"-t", "mangle", "-F", post},
		{"-t", "mangle", "-X", post},
	} {
		// Repeat deletes of jump rules in case older versions added duplicates.
		for i := 0; i < 10; i++ {
			if exec.Command("iptables", args...).Run() != nil || args[2] != "-D" {
				break
			}
		}
	}
}

// applyPolicyRouting delivers marked packets to the local tproxy socket:
// ip rule fwmark MARK/MARK lookup RouteTable; local default route via lo.
func (m *Manager) applyPolicyRouting() error {
	m.cleanupPolicyRouting()
	table := strconv.Itoa(RouteTable)
	mark := m.markValue + "/" + m.markValue
	if out, err := exec.Command("ip", "rule", "add", "fwmark", mark, "lookup", table, "priority", table).CombinedOutput(); err != nil {
		return fmt.Errorf("ip rule add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", table).CombinedOutput(); err != nil {
		return fmt.Errorf("ip route replace: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) cleanupPolicyRouting() {
	table := strconv.Itoa(RouteTable)
	mark := m.markValue + "/" + m.markValue
	for i := 0; i < 10; i++ {
		if exec.Command("ip", "rule", "del", "fwmark", mark, "lookup", table).Run() != nil {
			break
		}
	}
	exec.Command("ip", "route", "flush", "table", table).Run()
}

func (m *Manager) cleanupLocked() {
	switch m.backend {
	case FirewallBackendNFTables:
		exec.Command("nft", "delete", "table", "inet", m.tableName).Run()
	case FirewallBackendIPTables:
		m.cleanupIPTables()
	}
	m.cleanupPolicyRouting()
}

// AddSourceIP redirects traffic from an additional IPv4 address or subnet.
func (m *Manager) AddSourceIP(ip string) error {
	if err := validateIPv4(ip); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sourceIPs {
		if s == ip {
			return nil
		}
	}
	m.sourceIPs = append(m.sourceIPs, ip)
	if !m.started {
		return nil
	}
	return m.applyLocked()
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
	if !m.started {
		return nil
	}
	return m.applyLocked()
}

func validateIPv4(s string) error {
	if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
		return nil
	}
	if ip, _, err := net.ParseCIDR(s); err == nil && ip.To4() != nil {
		return nil
	}
	return fmt.Errorf("invalid IPv4 address or subnet: %q", s)
}

func runWithStdin(name, stdin string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(out.String()))
	}
	return nil
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		m.onLog(level, "[firewall] "+fmt.Sprintf(format, args...))
	}
}
