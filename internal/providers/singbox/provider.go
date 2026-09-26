package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/process"
)

const stopTimeout = 10 * time.Second

// Provider manages sing-box
type Provider struct {
	mu     sync.RWMutex
	config *Config
	proc   *process.Supervisor
	onLog  func(level, message string)
}

// Config represents sing-box configuration.
//
// The generated JSON targets the sing-box 1.12+ format: DNS servers use the
// typed "type/server" form, sniffing and DNS hijacking are route rule actions
// and the legacy "dns"/"block" outbounds are not used.
type Config struct {
	BinaryPath   string
	ConfigPath   string
	ConfigDir    string
	LogLevel     string
	Inbounds     []Inbound
	Outbounds    []Outbound
	Route        *Route
	DNS          *DNSConfig
	Experimental *ExperimentalConfig
}

// Inbound represents a sing-box inbound
type Inbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
}

// Outbound represents a sing-box outbound
type Outbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

// Route represents routing configuration
type Route struct {
	Rules                 []Rule `json:"rules"`
	Final                 string `json:"final,omitempty"`
	AutoDetectInterface   bool   `json:"auto_detect_interface,omitempty"`
	DefaultDomainResolver string `json:"default_domain_resolver,omitempty"`
}

// Rule represents a route rule
type Rule struct {
	Inbound  []string `json:"inbound,omitempty"`
	Protocol string   `json:"protocol,omitempty"`
	Action   string   `json:"action,omitempty"`
	Outbound string   `json:"outbound,omitempty"`
}

// DNSConfig represents DNS configuration
type DNSConfig struct {
	Servers  []DNSServer `json:"servers"`
	Rules    []DNSRule   `json:"rules,omitempty"`
	Final    string      `json:"final,omitempty"`
	Strategy string      `json:"strategy,omitempty"`
}

// DNSServer represents a DNS server (sing-box 1.12+ typed format)
type DNSServer struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Path       string `json:"path,omitempty"`
	// DomainResolver resolves Server when it is a host name.
	DomainResolver string `json:"domain_resolver,omitempty"`
	Inet4Range     string `json:"inet4_range,omitempty"`
	Inet6Range     string `json:"inet6_range,omitempty"`
}

// DNSRule represents a DNS rule
type DNSRule struct {
	QueryType []string `json:"query_type,omitempty"`
	Server    string   `json:"server,omitempty"`
	Action    string   `json:"action,omitempty"`
}

// ExperimentalConfig represents experimental features
type ExperimentalConfig struct {
	ClashAPI  *ClashAPIConfig  `json:"clash_api,omitempty"`
	CacheFile *CacheFileConfig `json:"cache_file,omitempty"`
}

// ClashAPIConfig represents Clash API configuration
type ClashAPIConfig struct {
	ExternalController string `json:"external_controller"`
	Secret             string `json:"secret,omitempty"`
	DefaultMode        string `json:"default_mode,omitempty"`
}

// CacheFileConfig stores selected outbounds and FakeIP mappings.
type CacheFileConfig struct {
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path,omitempty"`
	StoreFakeIP bool   `json:"store_fakeip,omitempty"`
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new sing-box provider
func NewProvider(opts Options) *Provider {
	p := &Provider{
		config: withDefaults(opts.Config),
		onLog:  opts.OnLog,
	}
	p.proc = &process.Supervisor{Name: "singbox", RespawnDelay: 5 * time.Second, OnLog: opts.OnLog}
	return p
}

func withDefaults(c *Config) *Config {
	if c == nil {
		c = &Config{}
	}
	if c.BinaryPath == "" {
		c.BinaryPath = "sing-box"
	}
	if c.ConfigPath == "" {
		c.ConfigPath = config.DefaultConfigDir + "/sing-box/config.json"
	}
	if c.ConfigDir == "" {
		c.ConfigDir = filepath.Dir(c.ConfigPath)
	}
	if c.LogLevel == "" {
		c.LogLevel = "warn"
	}
	return c
}

// ConfigFromPodkop creates sing-box config from the HydraVPN config
func ConfigFromPodkop(cfg *config.Config) *Config {
	c := &Config{
		BinaryPath: cfg.Settings.SingBoxBinary,
		ConfigPath: cfg.Settings.ConfigPath,
		LogLevel:   cfg.Settings.LogLevel,
		DNS:        &DNSConfig{Servers: []DNSServer{}},
	}

	// A single tproxy inbound; the firewall redirects LAN traffic to it.
	c.Inbounds = []Inbound{
		{Type: "tproxy", Tag: "tproxy-in", Listen: "0.0.0.0", ListenPort: config.TProxyPort},
		{Type: "direct", Tag: "dns-in", Listen: config.DNSListenAddress, ListenPort: 53},
		{Type: "mixed", Tag: "service-mixed-in", Listen: "127.0.0.1", ListenPort: config.MixedProxyPort},
	}
	c.Outbounds = []Outbound{{Type: "direct", Tag: "direct-out"}}

	c.Route = &Route{
		Rules: []Rule{
			{Action: "sniff"},
			{Inbound: []string{"dns-in"}, Action: "hijack-dns"},
			{Protocol: "dns", Action: "hijack-dns"},
		},
		Final: "direct-out",
	}

	for i, server := range cfg.Settings.DNSServers {
		c.DNS.Servers = append(c.DNS.Servers, dnsServer(fmt.Sprintf("dns-server-%d", i), server, cfg.Settings.DNSType))
	}
	for i, server := range cfg.Settings.BootstrapDNSServers {
		c.DNS.Servers = append(c.DNS.Servers, dnsServer(fmt.Sprintf("bootstrap-dns-%d", i), server, "udp"))
	}
	if len(cfg.Settings.DNSServers) > 0 {
		c.DNS.Final = "dns-server-0"
	} else if len(cfg.Settings.BootstrapDNSServers) > 0 {
		c.DNS.Final = "bootstrap-dns-0"
	}
	// Host-name DNS servers (DoH/DoT) and outbounds need a resolver:
	// the first bootstrap server, or the system resolver without one.
	resolver := "bootstrap-dns-0"
	if len(cfg.Settings.BootstrapDNSServers) == 0 {
		resolver = "local-dns"
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{Type: "local", Tag: resolver})
	}
	c.Route.DefaultDomainResolver = resolver
	for i := range c.DNS.Servers {
		if s := &c.DNS.Servers[i]; s.Server != "" && net.ParseIP(s.Server) == nil {
			s.DomainResolver = resolver
		}
	}
	c.DNS.Strategy = string(cfg.Settings.DNSStrategy)

	if cfg.Settings.FakeIPEnabled {
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{
			Type:       "fakeip",
			Tag:        "fakeip",
			Inet4Range: "198.18.0.0/15",
			Inet6Range: "fc00::/18",
		})
		c.DNS.Rules = append(c.DNS.Rules, DNSRule{QueryType: []string{"A", "AAAA"}, Server: "fakeip"})
	}

	if cfg.Settings.EnableYACD || cfg.Settings.FakeIPEnabled {
		c.Experimental = &ExperimentalConfig{
			CacheFile: &CacheFileConfig{
				Enabled:     true,
				Path:        cfg.Settings.CachePath,
				StoreFakeIP: cfg.Settings.FakeIPEnabled,
			},
		}
		if cfg.Settings.EnableYACD {
			c.Experimental.ClashAPI = &ClashAPIConfig{ExternalController: "127.0.0.1:9090"}
		}
	}

	return c
}

// dnsServer converts "1.1.1.1", "1.1.1.1:5353", "[2606:4700::1111]",
// "tls://dns.example" or "https://dns.example/dns-query" into a typed server.
func dnsServer(tag, address, defaultType string) DNSServer {
	s := DNSServer{Tag: tag, Type: defaultType}
	switch s.Type {
	case "udp", "tcp", "tls", "https", "quic", "h3":
	default:
		s.Type = "udp"
	}

	if strings.Contains(address, "://") {
		if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
			s.Type = u.Scheme
			s.Server = u.Hostname()
			if port, err := strconv.Atoi(u.Port()); err == nil {
				s.ServerPort = port
			}
			if (s.Type == "https" || s.Type == "h3") && u.Path != "" && u.Path != "/dns-query" {
				s.Path = u.Path
			}
			return s
		}
	}

	if host, port, err := net.SplitHostPort(address); err == nil {
		s.Server = host
		if p, err := strconv.Atoi(port); err == nil {
			s.ServerPort = p
		}
		return s
	}
	s.Server = strings.Trim(address, "[]")
	return s
}

// Start starts sing-box
func (p *Provider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.writeConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return p.proc.Start(p.config.BinaryPath, p.args())
}

// Stop stops sing-box
func (p *Provider) Stop() error {
	p.proc.Stop(stopTimeout)
	return nil
}

// Reload rewrites the configuration and asks sing-box to reload it.
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldBinary := p.config.BinaryPath
	p.config = withDefaults(cfg)
	if err := p.writeConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if !p.proc.Status().Running {
		return nil
	}
	// sing-box re-reads its configuration on SIGHUP; restart when the
	// binary changed or signals are unsupported.
	if oldBinary == p.config.BinaryPath {
		if err := p.proc.Reload(); err == nil {
			p.log("info", "sing-box reloaded")
			return nil
		}
	}
	return p.proc.Restart(p.config.BinaryPath, p.args(), stopTimeout)
}

func (p *Provider) args() []string {
	return []string{"run", "-c", p.config.ConfigPath}
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	configPath := p.config.ConfigPath
	p.mu.RUnlock()

	st := p.proc.Status()
	status := map[string]interface{}{
		"running":     st.Running,
		"pid":         st.PID,
		"uptime":      "",
		"restarts":    st.Restarts,
		"last_error":  st.LastError,
		"config_path": configPath,
	}
	if st.Running {
		status["uptime"] = st.Uptime.Round(time.Second).String()
	}
	return status
}

// GetStatusJSON returns status as JSON
func (p *Provider) GetStatusJSON() string {
	data, _ := json.MarshalIndent(p.GetStatus(), "", "  ")
	return string(data)
}

// Render returns the sing-box JSON configuration.
func (c *Config) Render() ([]byte, error) {
	full := map[string]interface{}{
		"log":       map[string]interface{}{"level": c.LogLevel, "timestamp": true},
		"inbounds":  c.Inbounds,
		"outbounds": c.Outbounds,
		"route":     c.Route,
		"dns":       c.DNS,
	}
	if c.Experimental != nil {
		full["experimental"] = c.Experimental
	}
	return json.MarshalIndent(full, "", "  ")
}

// writeConfig writes the sing-box configuration atomically.
func (p *Provider) writeConfig() error {
	if err := os.MkdirAll(p.config.ConfigDir, 0755); err != nil {
		return err
	}
	data, err := p.config.Render()
	if err != nil {
		return err
	}
	tmp := p.config.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p.config.ConfigPath)
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		p.onLog(level, "[singbox] "+fmt.Sprintf(format, args...))
	}
}
