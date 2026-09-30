package singbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Endpoints    []Outbound
	Route        *Route
	DNS          *DNSConfig
	Experimental *ExperimentalConfig

	// Warnings lists things that were skipped while building the config
	// (unsupported nodes, rules without matchers, missing lists).
	Warnings []string
	// fallbacks build reduced configs (without inbound servers, then
	// without sections). They are used when the full config is rejected by
	// "sing-box check", so a broken node cannot take the router offline.
	fallbacks []func() *Config
}

// Inbound, Outbound and Rule are sing-box JSON objects.
type (
	Inbound  map[string]interface{}
	Outbound map[string]interface{}
	Rule     map[string]interface{}
)

// Route represents routing configuration
type Route struct {
	Rules                 []Rule `json:"rules"`
	RuleSet               []Rule `json:"rule_set,omitempty"`
	Final                 string `json:"final,omitempty"`
	AutoDetectInterface   bool   `json:"auto_detect_interface,omitempty"`
	DefaultInterface      string `json:"default_interface,omitempty"`
	DefaultDomainResolver string `json:"default_domain_resolver,omitempty"`
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

// Reload rewrites the configuration and asks sing-box to reload it. A
// configuration that sing-box rejects is not applied.
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	old := p.config
	p.config = withDefaults(cfg)
	if err := p.writeConfig(); err != nil {
		p.config = old
		return fmt.Errorf("write config: %w", err)
	}

	if !p.proc.Status().Running {
		return p.proc.Start(p.config.BinaryPath, p.args())
	}
	// sing-box re-reads its configuration on SIGHUP; restart when the
	// binary changed or signals are unsupported.
	if old.BinaryPath == p.config.BinaryPath {
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
	if len(c.Endpoints) > 0 {
		full["endpoints"] = c.Endpoints
	}
	if c.Experimental != nil {
		full["experimental"] = c.Experimental
	}
	data, err := json.Marshal(full)
	if err != nil || len(data) > 256<<10 {
		return data, err // large configs stay compact: memory matters more
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return data, nil
	}
	return out.Bytes(), nil
}

// checkConfig validates a config file with "sing-box check". It is a
// variable so tests can run without the sing-box binary.
var checkConfig = func(binary, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "check", "-c", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writeConfig validates the configuration and writes it atomically. If the
// configuration is rejected, the fallback chain is tried (first without
// inbound servers, then without sections) so the router keeps working; the
// error is logged.
func (p *Provider) writeConfig() error {
	if err := os.MkdirAll(p.config.ConfigDir, 0755); err != nil {
		return err
	}
	for _, w := range p.config.Warnings {
		p.log("warn", "%s", w)
	}

	first := p.writeChecked(p.config)
	if first == nil {
		return nil
	}
	err := first
	for _, build := range p.config.fallbacks {
		p.log("error", "config rejected, trying a reduced config: %v", err)
		if err = p.writeChecked(withDefaults(build())); err == nil {
			return nil
		}
	}
	return first
}

func (p *Provider) writeChecked(c *Config) error {
	data, err := c.Render()
	if err != nil {
		return err
	}
	tmp := p.config.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := checkConfig(p.config.BinaryPath, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("sing-box check: %w", err)
	}
	return os.Rename(tmp, p.config.ConfigPath)
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		p.onLog(level, "[singbox] "+fmt.Sprintf(format, args...))
	}
}
