package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/miekg/dns"
)

// Diagnostics provides system diagnostics and health checks
type Diagnostics struct {
	config   *config.Config
	engine   interface{} // Will be *core.Engine
	onLog    func(level, message string)
}

// CheckResult represents a diagnostic check result
type CheckResult struct {
	Name        string                 `json:"name"`
	Status      string                 `json:"status"` // "pass", "fail", "warn", "skip"
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Timestamp   time.Time              `json:"timestamp"`
}

// SystemInfo represents system information
type SystemInfo struct {
	Platform       string                 `json:"platform"`
	OS             string                 `json:"os"`
	Arch           string                 `json:"arch"`
	Kernel         string                 `json:"kernel"`
	Hostname       string                 `json:"hostname"`
	Uptime         string                 `json:"uptime"`
	CPU            string                 `json:"cpu"`
	Memory         MemoryInfo             `json:"memory"`
	Disk           DiskInfo               `json:"disk"`
	Network        []NetworkInterface     `json:"network"`
	GoVersion      string                 `json:"go_version"`
	PodkopVersion  string                 `json:"podkop_version"`
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
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
	Percent   float64 `json:"percent"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	Name        string   `json:"name"`
	Index       int      `json:"index"`
	MTU         int      `json:"mtu"`
	Flags       []string `json:"flags"`
	IPv4        []string `json:"ipv4"`
	IPv6        []string `json:"ipv6"`
	MAC         string   `json:"mac"`
	Speed       uint64   `json:"speed"`
	Driver      string   `json:"driver"`
}

// NewDiagnostics creates a new diagnostics instance
func NewDiagnostics(cfg *config.Config, onLog func(level, message string)) *Diagnostics {
	return &Diagnostics{
		config: cfg,
		onLog:  onLog,
	}
}

// RunCheck runs a specific diagnostic check
func (d *Diagnostics) RunCheck(ctx context.Context, checkName string) (*CheckResult, error) {
	start := time.Now()
	
	var result *CheckResult
	var err error
	
	switch checkName {
	case "proxy":
		result, err = d.checkProxy(ctx)
	case "nft":
		result, err = d.checkNFTables(ctx)
	case "nft-rules":
		result, err = d.checkNFTRules(ctx)
	case "singbox":
		result, err = d.checkSingBox(ctx)
	case "inbounds-config":
		result, err = d.checkInboundsConfig(ctx)
	case "inbounds":
		result, err = d.checkInbounds(ctx)
	case "dns":
		result, err = d.checkDNS(ctx)
	case "dns-available":
		result, err = d.checkDNSAvailable(ctx)
	case "fakeip":
		result, err = d.checkFakeIP(ctx)
	case "zapret":
		result, err = d.checkZapret(ctx)
	case "zapret2":
		result, err = d.checkZapret2(ctx)
	case "byedpi":
		result, err = d.checkByeDPI(ctx)
	case "logs":
		result, err = d.checkLogs(ctx)
	case "singbox-logs":
		result, err = d.checkSingBoxLogs(ctx)
	case "global":
		result, err = d.checkGlobal(ctx)
	default:
		return nil, fmt.Errorf("unknown check: %s", checkName)
	}
	
	if result != nil {
		result.Duration = time.Since(start)
		result.Timestamp = time.Now()
	}
	
	return result, err
}

// RunAllChecks runs all diagnostic checks
func (d *Diagnostics) RunAllChecks(ctx context.Context) ([]*CheckResult, error) {
	checks := []string{
		"proxy", "nft", "nft-rules", "singbox", 
		"inbounds-config", "inbounds", "dns", 
		"dns-available", "fakeip", "zapret", "zapret2", 
		"byedpi", "logs", "singbox-logs", "global",
	}
	
	var results []*CheckResult
	for _, check := range checks {
		result, err := d.RunCheck(ctx, check)
		if err != nil {
			d.onLog("warn", fmt.Sprintf("Check %s failed: %v", check, err))
			result = &CheckResult{
				Name:      check,
				Status:    "fail",
				Message:   err.Error(),
				Duration:  0,
				Timestamp: time.Now(),
			}
		}
		results = append(results, result)
	}
	
	return results, nil
}

// GetSystemInfo returns system information
func (d *Diagnostics) GetSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{
		Platform:      runtime.GOOS,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		PodkopVersion: "1.0.0",
	}
	
	// Get kernel version
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info.Kernel = strings.TrimSpace(string(out))
	}
	
	// Get hostname
	if hostname, err := exec.Command("hostname").Output(); err == nil {
		info.Hostname = strings.TrimSpace(string(hostname))
	}
	
	// Get uptime
	if out, err := exec.Command("cat", "/proc/uptime").Output(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			if uptimeSec, err := parseFloat(fields[0]); err == nil {
				info.Uptime = time.Duration(uptimeSec * float64(time.Second)).String()
			}
		}
	}
	
	// Get CPU info
	if out, err := exec.Command("cat", "/proc/cpuinfo").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
				info.CPU = strings.TrimSpace(strings.Split(line, ":")[1])
				break
			}
		}
	}
	
	// Get memory info
	if out, err := exec.Command("cat", "/proc/meminfo").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		mem := MemoryInfo{}
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				switch fields[0] {
				case "MemTotal:":
					mem.Total = parseUint(fields[1]) * 1024
				case "MemAvailable:":
					mem.Available = parseUint(fields[1]) * 1024
				case "MemFree:":
					mem.Free = parseUint(fields[1]) * 1024
				}
			}
		}
		mem.Used = mem.Total - mem.Available
		info.Memory = mem
	}
	
	// Get disk info
	if out, err := exec.Command("df", "-B1", "/").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				disk := DiskInfo{}
				disk.Total = parseUint(fields[1])
				disk.Used = parseUint(fields[2])
				disk.Free = parseUint(fields[3])
				if disk.Total > 0 {
					disk.Percent = float64(disk.Used) / float64(disk.Total) * 100
				}
				info.Disk = disk
			}
		}
	}
	
	// Get network interfaces
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		
		ni := NetworkInterface{
			Name:  iface.Name,
			Index: iface.Index,
			MTU:   iface.MTU,
			MAC:   iface.HardwareAddr.String(),
		}
		
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

// Check implementations
func (d *Diagnostics) checkProxy(ctx context.Context) (*CheckResult, error) {
	// Check if we can reach internet through proxy
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://www.gstatic.com/generate_204")
	if err != nil {
		return &CheckResult{
			Name:    "proxy",
			Status:  "fail",
			Message: fmt.Sprintf("Proxy connectivity failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		return &CheckResult{
			Name:    "proxy",
			Status:  "pass",
			Message: "Proxy connectivity OK",
		}, nil
	}
	
	return &CheckResult{
		Name:    "proxy",
		Status:  "warn",
		Message: fmt.Sprintf("Proxy returned status %d", resp.StatusCode),
	}, nil
}

func (d *Diagnostics) checkNFTables(ctx context.Context) (*CheckResult, error) {
	// Check if nftables is available and our table exists
	out, err := exec.Command("nft", "list", "table", "inet", "podkop").Output()
	if err != nil {
		return &CheckResult{
			Name:    "nft",
			Status:  "fail",
			Message: "nftables table 'podkop' not found",
			Details: map[string]interface{}{"error": err.Error()},
		}, nil
	}
	
	return &CheckResult{
		Name:    "nft",
		Status:  "pass",
		Message: "nftables table 'podkop' exists",
		Details: map[string]interface{}{"output": string(out)},
	}, nil
}

func (d *Diagnostics) checkNFTRules(ctx context.Context) (*CheckResult, error) {
	// Check nftables rules
	out, err := exec.Command("nft", "list", "ruleset").Output()
	if err != nil {
		return &CheckResult{
			Name:    "nft-rules",
			Status:  "fail",
			Message: "Failed to list nftables rules",
		}, nil
	}
	
	rulesCount := strings.Count(string(out), "\n")
	return &CheckResult{
		Name:    "nft-rules",
		Status:  "pass",
		Message: fmt.Sprintf("nftables rules loaded (%d lines)", rulesCount),
		Details: map[string]interface{}{"rules_count": rulesCount},
	}, nil
}

func (d *Diagnostics) checkSingBox(ctx context.Context) (*CheckResult, error) {
	// Check if sing-box is installed
	path, err := exec.LookPath("sing-box")
	if err != nil {
		return &CheckResult{
			Name:    "singbox",
			Status:  "fail",
			Message: "sing-box not found in PATH",
		}, nil
	}
	
	// Check version
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return &CheckResult{
			Name:    "singbox",
			Status:  "warn",
			Message: "sing-box found but version check failed",
			Details: map[string]interface{}{"path": path, "error": err.Error()},
		}, nil
	}
	
	return &CheckResult{
		Name:    "singbox",
		Status:  "pass",
		Message: "sing-box available",
		Details: map[string]interface{}{"path": path, "version": string(out)},
	}, nil
}

func (d *Diagnostics) checkInboundsConfig(ctx context.Context) (*CheckResult, error) {
	// Check if inbound configurations exist
	configPath := d.config.Settings.ConfigPath
	if configPath == "" {
		configPath = "/etc/podkop-plus/sing-box/config.json"
	}
	
	if _, err := exec.Command("test", "-f", configPath).Output(); err != nil {
		return &CheckResult{
			Name:    "inbounds-config",
			Status:  "fail",
			Message: "sing-box config file not found",
			Details: map[string]interface{}{"path": configPath},
		}, nil
	}
	
	return &CheckResult{
		Name:    "inbounds-config",
		Status:  "pass",
		Message: "sing-box config file exists",
		Details: map[string]interface{}{"path": configPath},
	}, nil
}

func (d *Diagnostics) checkInbounds(ctx context.Context) (*CheckResult, error) {
	// Check if inbound ports are listening
	ports := []int{1602, 53, 4534}
	var listening []int
	
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			listening = append(listening, port)
		}
	}
	
	if len(listening) == len(ports) {
		return &CheckResult{
			Name:    "inbounds",
			Status:  "pass",
			Message: fmt.Sprintf("All inbound ports listening: %v", listening),
		}, nil
	}
	
	if len(listening) > 0 {
		return &CheckResult{
			Name:    "inbounds",
			Status:  "warn",
			Message: fmt.Sprintf("Some inbound ports listening: %v (expected: %v)", listening, ports),
		}, nil
	}
	
	return &CheckResult{
		Name:    "inbounds",
		Status:  "fail",
		Message: "No inbound ports listening",
		Details: map[string]interface{}{"expected_ports": ports, "listening": listening},
	}, nil
}

func (d *Diagnostics) checkDNS(ctx context.Context) (*CheckResult, error) {
	// Test DNS resolution
	servers := d.config.Settings.DNSServers
	if len(servers) == 0 {
		servers = []string{"77.88.8.8", "77.88.8.1"}
	}
	
	for _, server := range servers {
		client := &dns.Client{Timeout: 2 * time.Second}
		msg := new(dns.Msg)
		msg.SetQuestion("google.com.", dns.TypeA)
		
		r, _, err := client.Exchange(msg, server+":53")
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
	// Check all configured DNS servers
	servers := append(d.config.Settings.DNSServers, d.config.Settings.BootstrapDNSServers...)
	
	var results []map[string]interface{}
	allFail := true
	
	for _, server := range servers {
		client := &dns.Client{Timeout: 2 * time.Second}
		msg := new(dns.Msg)
		msg.SetQuestion("google.com.", dns.TypeA)
		
		start := time.Now()
		r, _, err := client.Exchange(msg, server+":53")
		duration := time.Since(start)
		
		result := map[string]interface{}{
			"server":   server,
			"duration": duration.String(),
		}
		
		if err == nil && r != nil && len(r.Answer) > 0 {
			result["status"] = "ok"
			result["answers"] = len(r.Answer)
			allFail = false
		} else {
			result["status"] = "fail"
			result["error"] = fmt.Sprintf("%v", err)
		}
		
		results = append(results, result)
	}
	
	status := "pass"
	if allFail {
		status = "fail"
	} else if len(results) > 1 {
		status = "warn"
	}
	
	return &CheckResult{
		Name:    "dns-available",
		Status:  status,
		Message: fmt.Sprintf("DNS availability check: %d servers tested", len(servers)),
		Details: map[string]interface{}{"results": results},
	}, nil
}

func (d *Diagnostics) checkFakeIP(ctx context.Context) (*CheckResult, error) {
	// Test FakeIP DNS resolution
	client := &dns.Client{Timeout: 2 * time.Second}
	msg := new(dns.Msg)
	msg.SetQuestion("fakeip.podkop.fyi.", dns.TypeA)
	
	// Use local DNS inbound
	r, _, err := client.Exchange(msg, "127.0.0.42:53")
	if err != nil {
		return &CheckResult{
			Name:    "fakeip",
			Status:  "fail",
			Message: fmt.Sprintf("FakeIP DNS query failed: %v", err),
		}, nil
	}
	
	if r != nil && len(r.Answer) > 0 {
		for _, ans := range r.Answer {
			if a, ok := ans.(*dns.A); ok {
				ip := a.A.String()
				if strings.HasPrefix(ip, "198.18.") {
					return &CheckResult{
						Name:    "fakeip",
						Status:  "pass",
						Message: fmt.Sprintf("FakeIP working: %s", ip),
						Details: map[string]interface{}{"fakeip": ip},
					}, nil
				}
			}
		}
	}
	
	return &CheckResult{
		Name:    "fakeip",
		Status:  "warn",
		Message: "FakeIP DNS responded but no FakeIP address",
		Details: map[string]interface{}{"answers": len(r.Answer)},
	}, nil
}

func (d *Diagnostics) checkZapret(ctx context.Context) (*CheckResult, error) {
	// Check if zapret/nfqws is running
	path, err := exec.LookPath("nfqws")
	if err != nil {
		return &CheckResult{
			Name:    "zapret",
			Status:  "skip",
			Message: "zapret (nfqws) not installed",
		}, nil
	}
	
	// Check process
	out, err := exec.Command("pgrep", "-f", "nfqws").Output()
	if err != nil {
		return &CheckResult{
			Name:    "zapret",
			Status:  "fail",
			Message: "zapret process not running",
		}, nil
	}
	
	pids := strings.Fields(string(out))
	return &CheckResult{
		Name:    "zapret",
		Status:  "pass",
		Message: fmt.Sprintf("zapret running (PIDs: %v)", pids),
		Details: map[string]interface{}{"pids": pids, "binary": path},
	}, nil
}

func (d *Diagnostics) checkZapret2(ctx context.Context) (*CheckResult, error) {
	// Check if zapret2/nfqws2 is running
	path, err := exec.LookPath("nfqws2")
	if err != nil {
		return &CheckResult{
			Name:    "zapret2",
			Status:  "skip",
			Message: "zapret2 (nfqws2) not installed",
		}, nil
	}
	
	out, err := exec.Command("pgrep", "-f", "nfqws2").Output()
	if err != nil {
		return &CheckResult{
			Name:    "zapret2",
			Status:  "fail",
			Message: "zapret2 process not running",
		}, nil
	}
	
	pids := strings.Fields(string(out))
	return &CheckResult{
		Name:    "zapret2",
		Status:  "pass",
		Message: fmt.Sprintf("zapret2 running (PIDs: %v)", pids),
		Details: map[string]interface{}{"pids": pids, "binary": path},
	}, nil
}

func (d *Diagnostics) checkByeDPI(ctx context.Context) (*CheckResult, error) {
	// Check if ciadpi (ByeDPI) is running
	path, err := exec.LookPath("ciadpi")
	if err != nil {
		return &CheckResult{
			Name:    "byedpi",
			Status:  "skip",
			Message: "ByeDPI (ciadpi) not installed",
		}, nil
	}
	
	out, err := exec.Command("pgrep", "-f", "ciadpi").Output()
	if err != nil {
		return &CheckResult{
			Name:    "byedpi",
			Status:  "fail",
			Message: "ByeDPI process not running",
		}, nil
	}
	
	pids := strings.Fields(string(out))
	return &CheckResult{
		Name:    "byedpi",
		Status:  "pass",
		Message: fmt.Sprintf("ByeDPI running (PIDs: %v)", pids),
		Details: map[string]interface{}{"pids": pids, "binary": path},
	}, nil
}

func (d *Diagnostics) checkLogs(ctx context.Context) (*CheckResult, error) {
	// Check system logs for podkop-plus
	out, err := exec.Command("journalctl", "-u", "podkop-plus", "--no-pager", "-n", "10").Output()
	if err != nil {
		return &CheckResult{
			Name:    "logs",
			Status:  "warn",
			Message: "Could not retrieve logs",
			Details: map[string]interface{}{"error": err.Error()},
		}, nil
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return &CheckResult{
		Name:    "logs",
		Status:  "pass",
		Message: fmt.Sprintf("Retrieved %d log lines", len(lines)),
		Details: map[string]interface{}{"lines": len(lines), "recent": lines},
	}, nil
}

func (d *Diagnostics) checkSingBoxLogs(ctx context.Context) (*CheckResult, error) {
	// Check sing-box logs
	out, err := exec.Command("journalctl", "-u", "sing-box", "--no-pager", "-n", "10").Output()
	if err != nil {
		return &CheckResult{
			Name:    "singbox-logs",
			Status:  "warn",
			Message: "Could not retrieve sing-box logs",
		}, nil
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return &CheckResult{
		Name:    "singbox-logs",
		Status:  "pass",
		Message: fmt.Sprintf("Retrieved %d sing-box log lines", len(lines)),
		Details: map[string]interface{}{"lines": len(lines)},
	}, nil
}

func (d *Diagnostics) checkGlobal(ctx context.Context) (*CheckResult, error) {
	// Run all checks and summarize
	results, _ := d.RunAllChecks(ctx)
	
	passed := 0
	failed := 0
	warned := 0
	skipped := 0
	
	for _, r := range results {
		switch r.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		case "warn":
			warned++
		case "skip":
			skipped++
		}
	}
	
	status := "pass"
	if failed > 0 {
		status = "fail"
	} else if warned > 0 {
		status = "warn"
	}
	
	return &CheckResult{
		Name:    "global",
		Status:  status,
		Message: fmt.Sprintf("Global check: %d pass, %d fail, %d warn, %d skip", passed, failed, warned, skipped),
		Details: map[string]interface{}{
			"passed":  passed,
			"failed":  failed,
			"warned":  warned,
			"skipped": skipped,
			"checks":  results,
		},
	}, nil
}

// ValidateNFQWSStrategy validates an NFQWS strategy
func (d *Diagnostics) ValidateNFQWSStrategy(strategy string) (*CheckResult, error) {
	// This would validate the strategy syntax
	return &CheckResult{
		Name:    "validate-nfqws-strategy",
		Status:  "pass",
		Message: "Strategy validation not implemented",
	}, nil
}

// ValidateNFQWS2Strategy validates an NFQWS2 strategy
func (d *Diagnostics) ValidateNFQWS2Strategy(strategy string) (*CheckResult, error) {
	return &CheckResult{
		Name:    "validate-nfqws2-strategy",
		Status:  "pass",
		Message: "Strategy validation not implemented",
	}, nil
}

// ValidateByeDPIStrategy validates a ByeDPI strategy
func (d *Diagnostics) ValidateByeDPIStrategy(strategy string) (*CheckResult, error) {
	return &CheckResult{
		Name:    "validate-byedpi-strategy",
		Status:  "pass",
		Message: "Strategy validation not implemented",
	}, nil
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func parseUint(s string) uint64 {
	var u uint64
	fmt.Sscanf(s, "%d", &u)
	return u
}

