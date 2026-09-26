package byedpi

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

// Provider manages ByeDPI (ciadpi)
type Provider struct {
	mu         sync.RWMutex
	config     *Config
	ctx        context.Context
	cancel     context.CancelFunc
	cmd        *exec.Cmd
	started    bool
	onLog      func(level, message string)
	wg         sync.WaitGroup
	
	// Status
	startTime  time.Time
	restarts   int
	lastError  string
	activeConns int
}

// Config represents ByeDPI configuration
type Config struct {
	BinaryPath       string
	ConfigPath       string
	StateDir         string
	ListenAddress    string
	PortBase         int
	RespawnDelay     int
	OpenFilesLimit   int
	CmdOptions       string
	Interfaces       []string
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new ByeDPI provider
func NewProvider(opts Options) *Provider {
	ctx, cancel := context.WithCancel(context.Background())
	
	p := &Provider{
		config: opts.Config,
		ctx:    ctx,
		cancel: cancel,
		onLog:  opts.OnLog,
	}
	
	if p.config == nil {
		p.config = &Config{}
	}
	
	if p.config.BinaryPath == "" {
		p.config.BinaryPath = "/usr/bin/ciadpi"
	}
	if p.config.ConfigPath == "" {
		p.config.ConfigPath = "/etc/podkop-plus/byedpi/config.json"
	}
	if p.config.StateDir == "" {
		p.config.StateDir = "/var/run/podkop-plus/byedpi"
	}
	if p.config.ListenAddress == "" {
		p.config.ListenAddress = "127.0.0.1"
	}
	if p.config.PortBase == 0 {
		p.config.PortBase = 1080
	}
	if p.config.RespawnDelay == 0 {
		p.config.RespawnDelay = 5
	}
	if p.config.OpenFilesLimit == 0 {
		p.config.OpenFilesLimit = 4096
	}
	if p.config.CmdOptions == "" {
		p.config.CmdOptions = "-o 2 --auto=t,r,a,s -d 2"
	}
	
	return p
}

// ConfigFromPodkop creates ByeDPI config from Podkop config
func ConfigFromPodkop(cfg *config.Config) *Config {
	c := &Config{
		ConfigPath:     "/etc/podkop-plus/byedpi/config.json",
		StateDir:       "/var/run/podkop-plus/byedpi",
		ListenAddress:  "127.0.0.1",
		PortBase:       1080,
		RespawnDelay:   5,
		OpenFilesLimit: 4096,
		CmdOptions:     "-o 2 --auto=t,r,a,s -d 2",
	}
	
	return c
}

// Start starts ByeDPI
func (p *Provider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.started {
		return nil
	}
	
	// Create directories
	os.MkdirAll(p.config.StateDir, 0755)
	os.MkdirAll(filepath.Dir(p.config.ConfigPath), 0755)
	
	// Write config file
	if err := p.writeConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	
	// Build command
	args := p.buildArgs()
	p.cmd = exec.CommandContext(ctx, p.config.BinaryPath, args...)
	p.cmd.Stdout = nil
	p.cmd.Stderr = nil
	
	// Set process group on Unix
	setProcessGroup(p.cmd)
	
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start byedpi: %w", err)
	}
	
	p.started = true
	p.startTime = time.Now()
	p.log("info", "ByeDPI started (PID: %d)", p.cmd.Process.Pid)
	
	// Monitor process
	p.wg.Add(1)
	go p.monitor()
	
	return nil
}

// Stop stops ByeDPI
func (p *Provider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.started {
		return nil
	}
	
	p.cancel()
	
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Signal(syscall.SIGTERM)
		p.cmd.Wait()
	}
	
	p.started = false
	p.log("info", "ByeDPI stopped")
	
	return nil
}

// Reload reloads ByeDPI configuration
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.config = cfg
	
	if p.started {
		// Write new config
		if err := p.writeConfig(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		
		// Restart process
if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Signal(syscall.SIGTERM)
			p.cmd.Wait()
			
			args := p.buildArgs()
			p.cmd = exec.CommandContext(p.ctx, p.config.BinaryPath, args...)
			p.cmd.Stdout = nil
			p.cmd.Stderr = nil
			setProcessGroup(p.cmd)
			
			if err := p.cmd.Start(); err != nil {
				return fmt.Errorf("restart byedpi: %w", err)
			}
			
			p.restarts++
			p.log("info", "ByeDPI reloaded (PID: %d)", p.cmd.Process.Pid)
		}
	}
	
	return nil
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	status := map[string]interface{}{
		"running":       p.started,
		"pid":           0,
		"uptime":        "",
		"restarts":      p.restarts,
		"last_error":    p.lastError,
		"active_conns":  p.activeConns,
		"listen_addr":   p.config.ListenAddress,
		"port_base":     p.config.PortBase,
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

// buildArgs builds command line arguments
func (p *Provider) buildArgs() []string {
	args := []string{
		"-l", p.config.ListenAddress,
		"-p", fmt.Sprintf("%d", p.config.PortBase),
	}
	
	// Add custom options
	if p.config.CmdOptions != "" {
		// Parse options string
		// For simplicity, split by space
		// In production, use proper shell parsing
	}
	
	return args
}

// writeConfig writes the ByeDPI configuration
func (p *Provider) writeConfig() error {
	config := map[string]interface{}{
		"listen":     p.config.ListenAddress,
		"port_base":  p.config.PortBase,
		"options":    p.config.CmdOptions,
		"interfaces": p.config.Interfaces,
	}
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(p.config.ConfigPath, data, 0644)
}

// monitor monitors the ByeDPI process
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
		p.log("error", "ByeDPI exited: %v", err)
	} else {
		p.log("info", "ByeDPI exited cleanly")
	}
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		p.onLog(level, "[byedpi] "+msg)
	}
}

