package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Provider manages sing-box
type Provider struct {
	mu       sync.RWMutex
	config   *Config
	ctx      context.Context
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	started  bool
	onLog    func(level, message string)
	wg       sync.WaitGroup
	
	// Status
	startTime time.Time
	restarts  int
	lastError string
}

// Config represents sing-box configuration
type Config struct {
	BinaryPath       string
	ConfigPath       string
	ConfigDir        string
	RuntimeDir       string
	LogLevel         string
	Inbounds         []Inbound
	Outbounds        []Outbound
	Route            *Route
	DNS              *DNSConfig
	Experimental     *ExperimentalConfig
}

// Inbound represents a sing-box inbound
type Inbound struct {
	Type        string                 `json:"type"`
	Tag         string                 `json:"tag"`
	Listen      string                 `json:"listen,omitempty"`
	ListenPort  int                    `json:"listen_port,omitempty"`
	Users       []User                 `json:"users,omitempty"`
	TLS         *TLSConfig             `json:"tls,omitempty"`
	Transport   *TransportConfig       `json:"transport,omitempty"`
	Multiplex   *MultiplexConfig       `json:"multiplex,omitempty"`
	Sniff       *SniffConfig           `json:"sniff,omitempty"`
	SetSystemProxy bool                `json:"set_system_proxy,omitempty"`
}

// Outbound represents a sing-box outbound
type Outbound struct {
	Type         string                 `json:"type"`
	Tag          string                 `json:"tag"`
	Server       string                 `json:"server,omitempty"`
	ServerPort   int                    `json:"server_port,omitempty"`
	UUID         string                 `json:"uuid,omitempty"`
	Password     string                 `json:"password,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Flow         string                 `json:"flow,omitempty"`
	TLS          *TLSConfig             `json:"tls,omitempty"`
	Transport    *TransportConfig       `json:"transport,omitempty"`
	Multiplex    *MultiplexConfig       `json:"multiplex,omitempty"`
	ProxySettings string                `json:"proxy_settings,omitempty"`
	Detour       string                 `json:"detour,omitempty"`
	DialerOptions *DialerOptions        `json:"dialer_options,omitempty"`
}

// Route represents routing configuration
type Route struct {
	Rules            []Rule              `json:"rules"`
	AutoDetectInterface bool             `json:"auto_detect_interface,omitempty"`
	OverrideAndroidDNS bool              `json:"override_android_dns,omitempty"`
	Final            string              `json:"final,omitempty"`
	GeoIP            *GeoIPConfig        `json:"geoip,omitempty"`
	GeoSite          *GeoSiteConfig      `json:"geosite,omitempty"`
}

// Rule represents a routing rule
type Rule struct {
	Type              string   `json:"type,omitempty"`
	Outbound          string   `json:"outbound,omitempty"`
	Inbound           []string `json:"inbound,omitempty"`
	Source            []string `json:"source,omitempty"`
	Destination       []string `json:"destination,omitempty"`
	Domain            []string `json:"domain,omitempty"`
	DomainSuffix      []string `json:"domain_suffix,omitempty"`
	DomainKeyword     []string `json:"domain_keyword,omitempty"`
	GeoIP             []string `json:"geoip,omitempty"`
	GeoSite           []string `json:"geosite,omitempty"`
	Port              string   `json:"port,omitempty"`
	PortRange         string   `json:"port_range,omitempty"`
	Process           string   `json:"process,omitempty"`
	ProcessPath       string   `json:"process_path,omitempty"`
	UID               string   `json:"uid,omitempty"`
	GID               string   `json:"gid,omitempty"`
	Network           string   `json:"network,omitempty"`
	Protocol          string   `json:"protocol,omitempty"`
	Invert            bool     `json:"invert,omitempty"`
}

// DNSConfig represents DNS configuration
type DNSConfig struct {
	Servers          []DNSServer       `json:"servers"`
	Rules            []DNSRule         `json:"rules"`
	Final            string            `json:"final,omitempty"`
	Strategy         string            `json:"strategy,omitempty"`
	DisableCache     bool              `json:"disable_cache,omitempty"`
	DisableExpire    bool              `json:"disable_expire,omitempty"`
	IndependentCache bool              `json:"independent_cache,omitempty"`
	FakeIP           *FakeIPConfig     `json:"fakeip,omitempty"`
}

// DNSServer represents a DNS server
type DNSServer struct {
	Tag         string   `json:"tag,omitempty"`
	Address     string   `json:"address"`
	AddressStrategy string `json:"address_strategy,omitempty"`
	Detour      string   `json:"detour,omitempty"`
}

// DNSRule represents a DNS rule
type DNSRule struct {
	Type          string   `json:"type,omitempty"`
	Server        string   `json:"server,omitempty"`
	Domain        []string `json:"domain,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	GeoIP         []string `json:"geoip,omitempty"`
	GeoSite       []string `json:"geosite,omitempty"`
	Invert        bool     `json:"invert,omitempty"`
}

// FakeIPConfig represents FakeIP configuration
type FakeIPConfig struct {
	Enabled       bool     `json:"enabled"`
	Inet4Range    string   `json:"inet4_range,omitempty"`
	Inet6Range    string   `json:"inet6_range,omitempty"`
	TTL           int      `json:"ttl,omitempty"`
}

// ExperimentalConfig represents experimental features
type ExperimentalConfig struct {
	ClashAPI *ClashAPIConfig `json:"clash_api,omitempty"`
}

// ClashAPIConfig represents Clash API configuration
type ClashAPIConfig struct {
	ExternalController string `json:"external_controller"`
	Secret             string `json:"secret,omitempty"`
	DefaultMode        string `json:"default_mode,omitempty"`
	StoreMode          bool   `json:"store_mode,omitempty"`
	StoreSelected      bool   `json:"store_selected,omitempty"`
	StoreFakeIP        bool   `json:"store_fakeip,omitempty"`
}

// Supporting types
type User struct {
	Name     string `json:"name"`
	Password string `json:"password,omitempty"`
	UUID     string `json:"uuid,omitempty"`
}

type TLSConfig struct {
	Enabled           bool     `json:"enabled"`
	ServerName        string   `json:"server_name,omitempty"`
	Insecure          bool     `json:"insecure,omitempty"`
	CertificatePath   string   `json:"certificate_path,omitempty"`
	KeyPath           string   `json:"key_path,omitempty"`
	Certificate       string   `json:"certificate,omitempty"`
	Key               string   `json:"key,omitempty"`
	Reality           *RealityConfig `json:"reality,omitempty"`
	UTLS              *UTLSConfig    `json:"utls,omitempty"`
}

type RealityConfig struct {
	Enabled     bool     `json:"enabled"`
	Handshake   *HandshakeConfig `json:"handshake,omitempty"`
	PrivateKey  string   `json:"private_key,omitempty"`
	PublicKey   string   `json:"public_key,omitempty"`
	ShortID     []string `json:"short_id,omitempty"`
	MaxTimeDiff string   `json:"max_time_difference,omitempty"`
}

type HandshakeConfig struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}

type UTLSConfig struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type TransportConfig struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Host       []string `json:"host,omitempty"`
}

type MultiplexConfig struct {
	Enabled   bool   `json:"enabled"`
	Protocol  string `json:"protocol,omitempty"`
	MaxStreams int   `json:"max_streams,omitempty"`
	Padding   bool   `json:"padding,omitempty"`
}

type SniffConfig struct {
	Enabled       bool     `json:"enabled"`
	DestOverride  []string `json:"dest_override,omitempty"`
	ParsePureIP   bool     `json:"parse_pure_ip,omitempty"`
	OverrideDest  string   `json:"override_dest,omitempty"`
}

type DialerOptions struct {
	Interface string `json:"interface,omitempty"`
	RoutingMark int  `json:"routing_mark,omitempty"`
}

type GeoIPConfig struct {
	Path string `json:"path,omitempty"`
	Data string `json:"data,omitempty"`
}

type GeoSiteConfig struct {
	Path string `json:"path,omitempty"`
	Data string `json:"data,omitempty"`
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new sing-box provider
func NewProvider(opts Options) *Provider {
	ctx, cancel := context.WithCancel(context.Background())
	
	p := &Provider{
		config: opts.Config,
		ctx:    ctx,
		cancel: cancel,
		onLog:  opts.OnLog,
	}
	
	if p.config.BinaryPath == "" {
		p.config.BinaryPath = "sing-box"
	}
	if p.config.ConfigPath == "" {
		p.config.ConfigPath = "/etc/podkop-plus/sing-box/config.json"
	}
	if p.config.ConfigDir == "" {
		p.config.ConfigDir = "/etc/podkop-plus/sing-box"
	}
	if p.config.RuntimeDir == "" {
		p.config.RuntimeDir = "/var/run/podkop-plus/sing-box"
	}
	
	return p
}

// ConfigFromPodkop creates sing-box config from Podkop config
func ConfigFromPodkop(cfg *config.Config) *Config {
	c := &Config{
		ConfigPath:  cfg.Settings.ConfigPath,
		ConfigDir:   filepath.Dir(cfg.Settings.ConfigPath),
		RuntimeDir:  "/var/run/podkop-plus/sing-box",
		LogLevel:    cfg.Settings.LogLevel,
		Inbounds:    []Inbound{},
		Outbounds:   []Outbound{},
		Route:       &Route{Rules: []Rule{}, Final: "direct-out"},
		DNS:         &DNSConfig{Servers: []DNSServer{}, Rules: []DNSRule{}},
	}
	
	// Add default inbounds
	c.Inbounds = append(c.Inbounds, Inbound{
		Type:       "tproxy",
		Tag:        "tproxy-in",
		Listen:     "0.0.0.0",
		ListenPort: 1602,
		Sniff: &SniffConfig{
			Enabled:      true,
			DestOverride: []string{"tls", "http", "quic"},
		},
	})
	
	c.Inbounds = append(c.Inbounds, Inbound{
		Type:       "tproxy",
		Tag:        "tproxy6-in",
		Listen:     "::",
		ListenPort: 1602,
		Sniff: &SniffConfig{
			Enabled:      true,
			DestOverride: []string{"tls", "http", "quic"},
		},
	})
	
	c.Inbounds = append(c.Inbounds, Inbound{
		Type:       "mixed",
		Tag:        "dns-in",
		Listen:     "127.0.0.42",
		ListenPort: 53,
	})
	
	c.Inbounds = append(c.Inbounds, Inbound{
		Type:       "mixed",
		Tag:        "service-mixed-in",
		Listen:     "127.0.0.1",
		ListenPort: 4534,
	})
	
	// Add default outbounds
	c.Outbounds = append(c.Outbounds, Outbound{
		Type: "direct",
		Tag:  "direct-out",
	})
	
	c.Outbounds = append(c.Outbounds, Outbound{
		Type: "block",
		Tag:  "block-out",
	})
	
	c.Outbounds = append(c.Outbounds, Outbound{
		Type: "dns",
		Tag:  "dns-out",
	})
	
	// Add DNS servers
	for _, server := range cfg.Settings.DNSServers {
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{
			Tag:     "dns-server",
			Address: server,
			Detour:  "direct-out",
		})
	}
	
	for _, server := range cfg.Settings.BootstrapDNSServers {
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{
			Tag:     "bootstrap-dns-server",
			Address: server,
			Detour:  "direct-out",
		})
	}
	
	c.DNS.Strategy = string(cfg.Settings.DNSStrategy)
	c.DNS.Final = "dns-out"
	
	// Add FakeIP if enabled
	if cfg.Settings.DNSDetourEnabled {
		c.DNS.FakeIP = &FakeIPConfig{
			Enabled:    true,
			Inet4Range: "198.18.0.0/15",
			Inet6Range: "fc00::/18",
			TTL:        cfg.Settings.DNSRewriteTTL,
		}
	}
	
	// Add experimental Clash API if enabled
	if cfg.Settings.EnableYACD {
		c.Experimental = &ExperimentalConfig{
			ClashAPI: &ClashAPIConfig{
				ExternalController: "127.0.0.1:9090",
				StoreMode:          true,
				StoreSelected:      true,
				StoreFakeIP:        true,
			},
		}
	}
	
	return c
}

// Start starts sing-box
func (p *Provider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.started {
		return nil
	}
	
	// Write config file
	if err := p.writeConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	
	// Create runtime directory
	os.MkdirAll(p.config.RuntimeDir, 0755)
	
// Start sing-box
	p.cmd = exec.CommandContext(ctx, p.config.BinaryPath, "run", "-c", p.config.ConfigPath)
	p.cmd.Stdout = nil // Will be handled by systemd/journald
	p.cmd.Stderr = nil
	
	setProcessGroup(p.cmd)
	
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start sing-box: %w", err)
	}
	
	p.started = true
	p.startTime = time.Now()
	p.log("info", "sing-box started (PID: %d)", p.cmd.Process.Pid)
	
	// Monitor process
	p.wg.Add(1)
	go p.monitor()
	
	return nil
}

// Stop stops sing-box
func (p *Provider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.started {
		return nil
	}
	
	p.cancel()
	
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Signal(os.Interrupt)
		p.cmd.Wait()
	}
	
	p.started = false
	p.log("info", "sing-box stopped")
	
	return nil
}

// Reload reloads sing-box configuration
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.config = cfg
	
	if p.started {
		// Write new config
		if err := p.writeConfig(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		
		// Send SIGHUP for reload
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Signal(os.Signal(syscall.SIGHUP))
			p.restarts++
			p.log("info", "sing-box reloaded")
		}
	}
	
	return nil
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	status := map[string]interface{}{
		"running":     p.started,
		"pid":         0,
		"uptime":      "",
		"restarts":    p.restarts,
		"last_error":  p.lastError,
		"config_path": p.config.ConfigPath,
	}
	
	if p.started && p.cmd != nil && p.cmd.Process != nil {
		status["pid"] = p.cmd.Process.Pid
		status["uptime"] = time.Since(p.startTime).String()
	}
	
	return status
}

// GetStatusJSON returns status as JSON
func (p *Provider) GetStatusJSON() string {
	data, _ := json.MarshalIndent(p.GetStatus(), "", "  ")
	return string(data)
}

// writeConfig writes the sing-box configuration to file
func (p *Provider) writeConfig() error {
	os.MkdirAll(p.config.ConfigDir, 0755)
	
	// Create full config
	fullConfig := map[string]interface{}{
		"log": map[string]interface{}{
			"level": p.config.LogLevel,
		},
		"inbounds":  p.config.Inbounds,
		"outbounds": p.config.Outbounds,
		"route":     p.config.Route,
		"dns":       p.config.DNS,
	}
	
	if p.config.Experimental != nil {
		fullConfig["experimental"] = p.config.Experimental
	}
	
	data, err := json.MarshalIndent(fullConfig, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(p.config.ConfigPath, data, 0644)
}

// monitor monitors the sing-box process
func (p *Provider) monitor() {
	defer p.wg.Done()
	
	if p.cmd == nil {
		return
	}
	
	err := p.cmd.Wait()
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.started = false
	if err != nil {
		p.lastError = err.Error()
		p.log("error", "sing-box exited: %v", err)
	} else {
		p.log("info", "sing-box exited cleanly")
	}
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		p.onLog(level, "[singbox] "+msg)
	}
}

