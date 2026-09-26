package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	hdns "github.com/Chistovik92/hydravpn-router/internal/dns"
	"github.com/Chistovik92/hydravpn-router/internal/firewall"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
	"github.com/miekg/dns"
)

// Diagnostics provides system diagnostics and health checks
type Diagnostics struct {
	config *config.Config
	onLog  func(level, message string)
}

// CheckResult represents a diagnostic check result
type CheckResult struct {
	Name      string                 `json:"name"`
	Status    string                 `json:"status"` // "pass", "fail", "warn", "skip"
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
}

// SystemInfo represents system information
type SystemInfo struct {
	Platform  string             `json:"platform"`
	OS        string             `json:"os"`
	Arch      string             `json:"arch"`
	Kernel    string             `json:"kernel"`
	Hostname  string             `json:"hostname"`
	Uptime    string             `json:"uptime"`
	CPU       string             `json:"cpu"`
	Memory    MemoryInfo         `json:"memory"`
	Disk      DiskInfo           `json:"disk"`
	Network   []NetworkInterface `json:"network"`
	GoVersion string             `json:"go_version"`
	Version   string             `json:"version"`
}

// MemoryInfo represents memory information
type MemoryInfo struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
}

// DiskInfo represents disk information
type DiskInfo struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	Name  string   `json:"name"`
	Index int      `json:"index"`
	MTU   int      `json:"mtu"`
	IPv4  []string `json:"ipv4"`
	IPv6  []string `json:"ipv6"`
	MAC   string   `json:"mac"`
}

type checkFunc func(d *Diagnostics, ctx context.Context) (*CheckResult, error)

// checks lists the individual checks, in execution order. "global" is not
// part of it: it summarises this list.
var checks = []struct {
	name string
	fn   checkFunc
}{
	{"proxy", (*Diagnostics).checkProxy},
	{"nft", (*Diagnostics).checkNFTables},
	{"nft-rules", (*Diagnostics).checkNFTRules},
	{"singbox", (*Diagnostics).checkSingBox},
	{"inbounds-config", (*Diagnostics).checkInboundsConfig},
	{"inbounds", (*Diagnostics).checkInbounds},
	{"dns", (*Diagnostics).checkDNS},
	{"dns-available", (*Diagnostics).checkDNSAvailable},
	{"fakeip", (*Diagnostics).checkFakeIP},
	{"zapret", func(d *Diagnostics, ctx context.Context) (*CheckResult, error) {
		return d.checkProcess("zapret", "nfqws")
	}},
	{"zapret2", func(d *Diagnostics, ctx context.Context) (*CheckResult, error) {
		return d.checkProcess("zapret2", "nfqws2")
	}},
	{"byedpi", func(d *Diagnostics, ctx context.Context) (*CheckResult, error) {
		return d.checkProcess("byedpi", "ciadpi")
	}},
	{"logs", func(d *Diagnostics, ctx context.Context) (*CheckResult, error) {
		return d.checkLogs("logs", "hydravpn-router")
	}},
	{"singbox-logs", func(d *Diagnostics, ctx context.Context) (*CheckResult, error) {
		return d.checkLogs("singbox-logs", "sing-box")
	}},
}

// CheckNames returns the names accepted by RunCheck.
func CheckNames() []string {
	names := make([]string, 0, len(checks)+1)
	for _, c := range checks {
		names = append(names, c.name)
	}
	return append(names, "global")
}

// NewDiagnostics creates a new diagnostics instance
func NewDiagnostics(cfg *config.Config, onLog func(level, message string)) *Diagnostics {
	if onLog == nil {
		onLog = func(string, string) {}
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return &Diagnostics{config: cfg, onLog: onLog}
}

// RunCheck runs a specific diagnostic check
func (d *Diagnostics) RunCheck(ctx context.Context, checkName string) (*CheckResult, error) {
	start := time.Now()

	var result *CheckResult
	var err error
	if checkName == "global" {
		result, err = d.checkGlobal(ctx)
	} else {
		found := false
		for _, c := range checks {
			if c.name == checkName {
				result, err = c.fn(d, ctx)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown check: %s (available: %s)", checkName, strings.Join(CheckNames(), ", "))
		}
	}

	if result != nil {
		result.Duration = time.Since(start)
		result.Timestamp = time.Now()
	}
	return result, err
}

// RunAllChecks runs all individual diagnostic checks
func (d *Diagnostics) RunAllChecks(ctx context.Context) ([]*CheckResult, error) {
	var results []*CheckResult
	for _, c := range checks {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		result, err := d.RunCheck(ctx, c.name)
		if err != nil {
			d.onLog("warn", fmt.Sprintf("Check %s failed: %v", c.name, err))
			result = &CheckResult{Name: c.name, Status: "fail", Message: err.Error(), Timestamp: time.Now()}
		}
		results = append(results, result)
	}
	return results, nil
}

// GetSystemInfo returns system information
func (d *Diagnostics) GetSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{
		Platform:  runtime.GOOS,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Version:   version.Version,
	}

	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info.Kernel = strings.TrimSpace(string(out))
	}
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}
	if out, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(out)); len(fields) > 0 {
			if sec, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.Uptime = time.Duration(sec * float64(time.Second)).Round(time.Second).String()
			}
		}
	}
	if out, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") || strings.HasPrefix(line, "cpu model") {
				if _, v, ok := strings.Cut(line, ":"); ok {
					info.CPU = strings.TrimSpace(v)
					break
				}
			}
		}
	}
	if out, err := os.ReadFile("/proc/meminfo"); err == nil {
		mem := MemoryInfo{}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				mem.Total = v * 1024
			case "MemAvailable:":
				mem.Available = v * 1024
			case "MemFree:":
				mem.Free = v * 1024
			}
		}
		// Old kernels have no MemAvailable.
		if mem.Available == 0 {
			mem.Available = mem.Free
		}
		if mem.Total >= mem.Available {
			mem.Used = mem.Total - mem.Available
		}
		info.Memory = mem
	}
	// BusyBox df has no -B; -k works everywhere.
	if out, err := exec.Command("df", "-k", "/").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			if fields := strings.Fields(lines[1]); len(fields) >= 4 {
				total, _ := strconv.ParseUint(fields[1], 10, 64)
				used, _ := strconv.ParseUint(fields[2], 10, 64)
				free, _ := strconv.ParseUint(fields[3], 10, 64)
				disk := DiskInfo{Total: total * 1024, Used: used * 1024, Free: free * 1024}
				if disk.Total > 0 {
					disk.Percent = float64(disk.Used) / float64(disk.Total) * 100
				}
				info.Disk = disk
			}
		}
	}

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ni := NetworkInterface{Name: iface.Name, Index: iface.Index, MTU: iface.MTU, MAC: iface.HardwareAddr.String()}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ipnet.IP.To4() != nil {
					ni.IPv4 = append(ni.IPv4, ipnet.String())
				} else {
					ni.IPv6 = append(ni.IPv6, ipnet.String())
				}
			}
		}
		if len(ni.IPv4) > 0 || len(ni.IPv6) > 0 {
			info.Network = append(info.Network, ni)
		}
	}

	return info, nil
}

// GetSystemInfoJSON returns system info as JSON
func (d *Diagnostics) GetSystemInfoJSON() (string, error) {
	info, err := d.GetSystemInfo()
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return string(data), nil
}

func (d *Diagnostics) checkProxy(ctx context.Context) (*CheckResult, error) {
	testURL := d.config.Settings.LatencyTestURL
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return &CheckResult{Name: "proxy", Status: "fail", Message: fmt.Sprintf("Connectivity check failed: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return &CheckResult{Name: "proxy", Status: "pass", Message: "Connectivity OK"}, nil
	}
	return &CheckResult{Name: "proxy", Status: "warn", Message: fmt.Sprintf("%s returned status %d", testURL, resp.StatusCode)}, nil
}

func (d *Diagnostics) checkNFTables(ctx context.Context) (*CheckResult, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return &CheckResult{Name: "nft", Status: "skip", Message: "nft is not installed"}, nil
	}
	if err := exec.CommandContext(ctx, "nft", "list", "table", "inet", firewall.TableName).Run(); err != nil {
		return &CheckResult{
			Name:    "nft",
			Status:  "fail",
			Message: fmt.Sprintf("nftables table '%s' not found", firewall.TableName),
			Details: map[string]interface{}{"error": err.Error()},
		}, nil
	}
	return &CheckResult{Name: "nft", Status: "pass", Message: fmt.Sprintf("nftables table '%s' exists", firewall.TableName)}, nil
}

func (d *Diagnostics) checkNFTRules(ctx context.Context) (*CheckResult, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return &CheckResult{Name: "nft-rules", Status: "skip", Message: "nft is not installed"}, nil
	}
	out, err := exec.CommandContext(ctx, "nft", "list", "table", "inet", firewall.TableName).Output()
	if err != nil {
		return &CheckResult{Name: "nft-rules", Status: "fail", Message: "Failed to list nftables rules"}, nil
	}
	n := strings.Count(string(out), "tproxy")
	if n == 0 {
		return &CheckResult{Name: "nft-rules", Status: "fail", Message: "No tproxy rules in table " + firewall.TableName}, nil
	}
	return &CheckResult{
		Name:    "nft-rules",
		Status:  "pass",
		Message: fmt.Sprintf("%d tproxy rules loaded", n),
		Details: map[string]interface{}{"tproxy_rules": n},
	}, nil
}

func (d *Diagnostics) checkSingBox(ctx context.Context) (*CheckResult, error) {
	bin := d.config.Settings.SingBoxBinary
	if bin == "" {
		bin = "sing-box"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return &CheckResult{Name: "singbox", Status: "fail", Message: bin + " not found"}, nil
	}

	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return &CheckResult{
			Name:    "singbox",
			Status:  "warn",
			Message: "sing-box found but version check failed",
			Details: map[string]interface{}{"path": path, "error": err.Error()},
		}, nil
	}
	firstLine, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return &CheckResult{
		Name:    "singbox",
		Status:  "pass",
		Message: firstLine,
		Details: map[string]interface{}{"path": path, "version": string(out)},
	}, nil
}

func (d *Diagnostics) checkInboundsConfig(ctx context.Context) (*CheckResult, error) {
	configPath := d.config.Settings.ConfigPath
	if configPath == "" {
		configPath = config.DefaultConfigDir + "/sing-box/config.json"
	}
	details := map[string]interface{}{"path": configPath}

	if _, err := os.Stat(configPath); err != nil {
		return &CheckResult{Name: "inbounds-config", Status: "fail", Message: "sing-box config file not found", Details: details}, nil
	}

	bin := d.config.Settings.SingBoxBinary
	if bin == "" {
		bin = "sing-box"
	}
	if path, err := exec.LookPath(bin); err == nil {
		if out, err := exec.CommandContext(ctx, path, "check", "-c", configPath).CombinedOutput(); err != nil {
			details["output"] = strings.TrimSpace(string(out))
			return &CheckResult{Name: "inbounds-config", Status: "fail", Message: "sing-box rejects the generated config", Details: details}, nil
		}
	}
	return &CheckResult{Name: "inbounds-config", Status: "pass", Message: "sing-box config file is valid", Details: details}, nil
}

func (d *Diagnostics) checkInbounds(ctx context.Context) (*CheckResult, error) {
	// A direct connection to the tproxy port would be proxied back to
	// itself, so that one is checked via /proc/net instead.
	endpoints := map[string]bool{
		fmt.Sprintf("tproxy :%d", config.TProxyPort):             listeningTCP(config.TProxyPort),
		fmt.Sprintf("mixed 127.0.0.1:%d", config.MixedProxyPort): dialOK(fmt.Sprintf("127.0.0.1:%d", config.MixedProxyPort)),
		fmt.Sprintf("dns %s:53", config.DNSListenAddress):        dnsOK(net.JoinHostPort(config.DNSListenAddress, "53")),
	}
	var expected, listening []string
	for name, ok := range endpoints {
		expected = append(expected, name)
		if ok {
			listening = append(listening, name)
		}
	}
	sort.Strings(expected)
	sort.Strings(listening)

	switch {
	case len(listening) == len(expected):
		return &CheckResult{Name: "inbounds", Status: "pass", Message: fmt.Sprintf("All inbounds listening: %v", listening)}, nil
	case len(listening) > 0:
		return &CheckResult{Name: "inbounds", Status: "warn", Message: fmt.Sprintf("Some inbounds listening: %v (expected: %v)", listening, expected)}, nil
	default:
		return &CheckResult{
			Name:    "inbounds",
			Status:  "fail",
			Message: "No inbounds listening",
			Details: map[string]interface{}{"expected": expected},
		}, nil
	}
}

func exchange(server string) (*dns.Msg, time.Duration, error) {
	addr, network, ok := hdns.ServerAddress(server)
	if !ok {
		return nil, 0, fmt.Errorf("transport of %s cannot be checked", server)
	}
	msg := new(dns.Msg)
	msg.SetQuestion("google.com.", dns.TypeA)
	return (&dns.Client{Timeout: 2 * time.Second, Net: network}).Exchange(msg, addr)
}

func (d *Diagnostics) checkDNS(ctx context.Context) (*CheckResult, error) {
	servers := d.config.Settings.DNSServers
	if len(servers) == 0 {
		servers = []string{"77.88.8.8", "77.88.8.1"}
	}

	for _, server := range servers {
		r, _, err := exchange(server)
		if err == nil && r != nil && len(r.Answer) > 0 {
			return &CheckResult{
				Name:    "dns",
				Status:  "pass",
				Message: fmt.Sprintf("DNS resolution OK via %s", server),
				Details: map[string]interface{}{"server": server, "answers": len(r.Answer)},
			}, nil
		}
	}

	return &CheckResult{
		Name:    "dns",
		Status:  "fail",
		Message: "All DNS servers failed",
		Details: map[string]interface{}{"servers": servers},
	}, nil
}

func (d *Diagnostics) checkDNSAvailable(ctx context.Context) (*CheckResult, error) {
	// Copy to avoid appending into the config's backing array.
	servers := append(append([]string(nil), d.config.Settings.DNSServers...), d.config.Settings.BootstrapDNSServers...)

	var results []map[string]interface{}
	failures := 0
	for _, server := range servers {
		r, rtt, err := exchange(server)
		result := map[string]interface{}{"server": server, "duration": rtt.String()}
		if err == nil && r != nil && len(r.Answer) > 0 {
			result["status"] = "ok"
			result["answers"] = len(r.Answer)
		} else {
			failures++
			result["status"] = "fail"
			result["error"] = fmt.Sprintf("%v", err)
		}
		results = append(results, result)
	}

	status := "pass"
	switch {
	case len(servers) == 0 || failures == len(servers):
		status = "fail"
	case failures > 0:
		status = "warn"
	}

	return &CheckResult{
		Name:    "dns-available",
		Status:  status,
		Message: fmt.Sprintf("%d of %d DNS servers answered", len(servers)-failures, len(servers)),
		Details: map[string]interface{}{"results": results},
	}, nil
}

func (d *Diagnostics) checkFakeIP(ctx context.Context) (*CheckResult, error) {
	if !d.config.Settings.FakeIPEnabled {
		return &CheckResult{Name: "fakeip", Status: "skip", Message: "FakeIP is disabled"}, nil
	}

	msg := new(dns.Msg)
	msg.SetQuestion("fakeip.example.com.", dns.TypeA)
	r, _, err := (&dns.Client{Timeout: 2 * time.Second}).Exchange(msg, net.JoinHostPort(config.DNSListenAddress, "53"))
	if err != nil {
		return &CheckResult{Name: "fakeip", Status: "fail", Message: fmt.Sprintf("FakeIP DNS query failed: %v", err)}, nil
	}

	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			_, fakeNet, _ := net.ParseCIDR("198.18.0.0/15")
			if fakeNet.Contains(a.A) {
				return &CheckResult{
					Name:    "fakeip",
					Status:  "pass",
					Message: fmt.Sprintf("FakeIP working: %s", a.A),
					Details: map[string]interface{}{"fakeip": a.A.String()},
				}, nil
			}
		}
	}

	return &CheckResult{
		Name:    "fakeip",
		Status:  "warn",
		Message: "DNS responded but returned no FakeIP address",
		Details: map[string]interface{}{"answers": len(r.Answer)},
	}, nil
}

// checkProcess checks that a provider binary runs (exact process name).
func (d *Diagnostics) checkProcess(name, proc string) (*CheckResult, error) {
	out, err := exec.Command("pgrep", "-x", proc).Output()
	if err != nil {
		return &CheckResult{Name: name, Status: "skip", Message: proc + " is not running"}, nil
	}
	pids := strings.Fields(string(out))
	return &CheckResult{
		Name:    name,
		Status:  "pass",
		Message: fmt.Sprintf("%s running (PIDs: %v)", proc, pids),
		Details: map[string]interface{}{"pids": pids},
	}, nil
}

func (d *Diagnostics) checkLogs(name, tag string) (*CheckResult, error) {
	out, err := readLogs(tag)
	if err != nil {
		return &CheckResult{
			Name:    name,
			Status:  "warn",
			Message: "Could not retrieve logs",
			Details: map[string]interface{}{"error": err.Error()},
		}, nil
	}
	text := strings.TrimSpace(string(out))
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	return &CheckResult{
		Name:    name,
		Status:  "pass",
		Message: fmt.Sprintf("Retrieved %d log lines", len(lines)),
		Details: map[string]interface{}{"lines": len(lines), "recent": lines},
	}, nil
}

func (d *Diagnostics) checkGlobal(ctx context.Context) (*CheckResult, error) {
	results, _ := d.RunAllChecks(ctx)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.Status]++
	}

	status := "pass"
	if counts["fail"] > 0 {
		status = "fail"
	} else if counts["warn"] > 0 {
		status = "warn"
	}

	return &CheckResult{
		Name:    "global",
		Status:  status,
		Message: fmt.Sprintf("Global check: %d pass, %d fail, %d warn, %d skip", counts["pass"], counts["fail"], counts["warn"], counts["skip"]),
		Details: map[string]interface{}{
			"passed":  counts["pass"],
			"failed":  counts["fail"],
			"warned":  counts["warn"],
			"skipped": counts["skip"],
			"checks":  results,
		},
	}, nil
}

// readLogs returns recent log lines from journald or, on OpenWrt/Keenetic,
// from logread.
func readLogs(tag string) ([]byte, error) {
	if _, err := exec.LookPath("journalctl"); err == nil {
		return exec.Command("journalctl", "-u", tag, "--no-pager", "-n", "10").Output()
	}
	if _, err := exec.LookPath("logread"); err == nil {
		out, err := exec.Command("logread", "-e", tag).Output()
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		return []byte(strings.Join(lines, "\n")), nil
	}
	return nil, fmt.Errorf("neither journalctl nor logread is available")
}

// listeningTCP reports whether a TCP socket listens on port (Linux /proc).
func listeningTCP(port int) bool {
	hexPort := fmt.Sprintf(":%04X", port)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			// fields[1] = local address, fields[3] = state (0A = LISTEN)
			if len(fields) > 3 && strings.HasSuffix(fields[1], hexPort) && fields[3] == "0A" {
				return true
			}
		}
	}
	return false
}

func dialOK(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func dnsOK(addr string) bool {
	msg := new(dns.Msg)
	msg.SetQuestion("google.com.", dns.TypeA)
	_, _, err := (&dns.Client{Timeout: 2 * time.Second}).Exchange(msg, addr)
	return err == nil
}
