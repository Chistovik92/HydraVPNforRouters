package byedpi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/process"
)

const (
	stopTimeout    = 5 * time.Second
	defaultOptions = "-o 2 -d 2"
)

// Provider manages ByeDPI (ciadpi), a local SOCKS5 proxy
type Provider struct {
	mu     sync.RWMutex
	config *Config
	proc   *process.Supervisor
	onLog  func(level, message string)
}

// Config represents ByeDPI configuration
type Config struct {
	BinaryPath    string
	ListenAddress string
	Port          int
	RespawnDelay  int
	CmdOptions    string
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new ByeDPI provider
func NewProvider(opts Options) *Provider {
	c := withDefaults(opts.Config)
	return &Provider{
		config: c,
		onLog:  opts.OnLog,
		proc: &process.Supervisor{
			Name:         "byedpi",
			RespawnDelay: time.Duration(c.RespawnDelay) * time.Second,
			OnLog:        opts.OnLog,
		},
	}
}

func withDefaults(c *Config) *Config {
	if c == nil {
		c = &Config{}
	}
	if c.BinaryPath == "" {
		c.BinaryPath = "/usr/bin/ciadpi"
	}
	if c.ListenAddress == "" {
		c.ListenAddress = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 1080
	}
	if c.RespawnDelay == 0 {
		c.RespawnDelay = 5
	}
	if c.CmdOptions == "" {
		c.CmdOptions = defaultOptions
	}
	return c
}

// ConfigFromSettings creates ByeDPI config from the HydraVPN config
func ConfigFromSettings(cfg *config.Config) *Config {
	return withDefaults(&Config{CmdOptions: cfg.ProviderOptions(config.ProviderTypeByeDPI)})
}

// Start starts ByeDPI
func (p *Provider) Start(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.proc.Start(p.config.BinaryPath, p.buildArgs())
}

// Stop stops ByeDPI
func (p *Provider) Stop() error {
	p.proc.Stop(stopTimeout)
	return nil
}

// Reload restarts ByeDPI with the new configuration
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = withDefaults(cfg)
	if !p.proc.Status().Running {
		return nil
	}
	if err := p.proc.Restart(p.config.BinaryPath, p.buildArgs(), stopTimeout); err != nil {
		return fmt.Errorf("restart byedpi: %w", err)
	}
	p.log("info", "ByeDPI restarted with new options")
	return nil
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	listen, port := p.config.ListenAddress, p.config.Port
	p.mu.RUnlock()

	st := p.proc.Status()
	status := map[string]interface{}{
		"running":     st.Running,
		"pid":         st.PID,
		"uptime":      "",
		"restarts":    st.Restarts,
		"last_error":  st.LastError,
		"listen_addr": listen,
		"port":        port,
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

// buildArgs builds ciadpi arguments: -i/-p set the listen address and port,
// the rest is the desync strategy.
func (p *Provider) buildArgs() []string {
	args := []string{
		"-i", p.config.ListenAddress,
		"-p", strconv.Itoa(p.config.Port),
	}
	return append(args, strings.Fields(p.config.CmdOptions)...)
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		p.onLog(level, "[byedpi] "+fmt.Sprintf(format, args...))
	}
}
