package zapret

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

// Provider manages zapret/zapret2/nfqws
type Provider struct {
	mu          sync.RWMutex
	config      *Config
	ctx         context.Context
	cancel      context.CancelFunc
	cmd         *exec.Cmd
	started     bool
	onLog       func(level, message string)
	wg          sync.WaitGroup
	
	// Status
	startTime   time.Time
	restarts    int
	lastError   string
	activeConns int
}

// Config represents zapret configuration
type Config struct {
	BinaryPath      string
	ConfigPath      string
	StrategyFile    string
	HostlistDir     string
	IPSetDir        string
	LogDir          string
	PIDDir          string
	StateDir        string
	NFQWSOptions    string
	QueueNum        int
	Mark            string
	DesyncMark      string
	DesyncMarkPost  string
	RespawnDelay    int
	ProviderType    string // "zapret" or "zapret2"
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new zapret provider
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
		p.config.BinaryPath = "nfqws"
	}
	if p.config.ConfigPath == "" {
		p.config.ConfigPath = "/etc/podkop-plus/zapret/config.json"
	}
	if p.config.StateDir == "" {
		p.config.StateDir = "/var/run/podkop-plus/zapret"
	}
	if p.config.QueueNum == 0 {
		p.config.QueueNum = 4000
	}
	if p.config.Mark == "" {
		p.config.Mark = "0x01000000"
	}
	if p.config.DesyncMark == "" {
		p.config.DesyncMark = "0x40000000"
	}
	if p.config.DesyncMarkPost == "" {
		p.config.DesyncMarkPost = "0x20000000"
	}
	if p.config.RespawnDelay == 0 {
		p.config.RespawnDelay = 5
	}
	if p.config.ProviderType == "" {
		p.config.ProviderType = "zapret2"
	}
	
	return p
}

// ConfigFromPodkop creates zapret config from Podkop config
func ConfigFromPodkop(cfg *config.Config) *Config {
	c := &Config{
		ConfigPath:  "/etc/podkop-plus/zapret/config.json",
		StateDir:    "/var/run/podkop-plus/zapret",
		ProviderType: "zapret2",
		QueueNum:     4000,
		Mark:         "0x01000000",
		DesyncMark:   "0x40000000",
		DesyncMarkPost: "0x20000000",
		RespawnDelay: 5,
	}
	
	// Determine binary based on provider type
	if c.ProviderType == "zapret2" {
		c.BinaryPath = "/opt/zapret2/nfq2/nfqws2"
		c.StrategyFile = "/etc/podkop-plus/zapret/strategy.json"
	} else {
		c.BinaryPath = "/opt/zapret/nfq/nfqws"
		c.StrategyFile = "/etc/podkop-plus/zapret/strategy"
	}
	
	return c
}

// Start starts zapret
func (p *Provider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.started {
		return nil
	}
	
	// Create directories
	os.MkdirAll(p.config.StateDir, 0755)
	os.MkdirAll(filepath.Dir(p.config.ConfigPath), 0755)
	
	// Write strategy file
	if err := p.writeStrategy(); err != nil {
		return fmt.Errorf("write strategy: %w", err)
	}
	
// Build command
	args := p.buildArgs()
	p.cmd = exec.CommandContext(ctx, p.config.BinaryPath, args...)
	p.cmd.Stdout = nil
	p.cmd.Stderr = nil
	
	setProcessGroup(p.cmd)
	
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", p.config.ProviderType, err)
	}
	
	p.started = true
	p.startTime = time.Now()
	p.log("info", "%s started (PID: %d)", p.config.ProviderType, p.cmd.Process.Pid)
	
	// Monitor process
	p.wg.Add(1)
	go p.monitor()
	
	return nil
}

// Stop stops zapret
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
	p.log("info", "%s stopped", p.config.ProviderType)
	
	return nil
}

// Reload reloads zapret configuration
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.config = cfg
	
	if p.started {
		// Write new strategy
		if err := p.writeStrategy(); err != nil {
			return fmt.Errorf("write strategy: %w", err)
		}
		
		// Send SIGHUP for reload
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Signal(syscall.SIGHUP)
			p.restarts++
			p.log("info", "%s reloaded", p.config.ProviderType)
		}
	}
	
	return nil
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	status := map[string]interface{}{
		"running":      p.started,
		"provider":     p.config.ProviderType,
		"pid":          0,
		"uptime":       "",
		"restarts":     p.restarts,
		"last_error":   p.lastError,
		"active_conns": p.activeConns,
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
		"--queue=" + fmt.Sprintf("%d", p.config.QueueNum),
		"--mark=" + p.config.Mark,
		"--desync-mark=" + p.config.DesyncMark,
		"--desync-mark-postnat=" + p.config.DesyncMarkPost,
		"--respawn-delay=" + fmt.Sprintf("%d", p.config.RespawnDelay),
	}
	
	if p.config.ProviderType == "zapret2" {
		args = append(args, "--lua-file="+p.config.StrategyFile)
	} else {
		args = append(args, "--strategy="+p.config.StrategyFile)
	}
	
	// Add NFQWS options from config
	if p.config.NFQWSOptions != "" {
		// Parse and add custom options
	}
	
	return args
}

// writeStrategy writes the desync strategy file
func (p *Provider) writeStrategy() error {
	if p.config.ProviderType == "zapret2" {
		// Write Lua strategy for zapret2
		strategy := `local desync = require("desync")

-- TCP 80 - HTTP
desync.fake_tls({
    filter = {tcp = 80, l7 = "http", payload = "http_req"},
    blob = "fake_default_http",
    tcp_md5 = true,
})

desync.multisplit({
    filter = {tcp = 80, l7 = "http", payload = "http_req"},
    pos = "method+2",
})

-- TCP 443 - TLS
desync.fake_tls({
    filter = {tcp = 443, l7 = "tls", payload = "tls_client_hello"},
    blob = "fake_default_tls",
    tcp_md5 = true,
    tcp_seq = -10000,
})

desync.multidisorder({
    filter = {tcp = 443, l7 = "tls", payload = "tls_client_hello"},
    pos = {1, "midsld"},
})

-- UDP 443 - QUIC
desync.fake_quic({
    filter = {udp = 443, l7 = "quic", payload = "quic_initial"},
    blob = "fake_default_quic",
    repeats = 6,
})
`
		return os.WriteFile(p.config.StrategyFile, []byte(strategy), 0644)
	}
	
	// Write text strategy for zapret
	strategy := `--filter-tcp=80 --dpi-desync=fake,fakedsplit --dpi-desync-autottl=2 --dpi-desync-fooling=badsum
--new
--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=11 --dpi-desync-fooling=badsum --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com
--new
--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11 --dpi-desync-fake-quic=/opt/zapret/files/fake/quic_initial_www_google_com.bin
--new
--filter-udp=443 <HOSTLIST_NOAUTO> --dpi-desync=fake --dpi-desync-repeats=11
--new
--filter-tcp=443 <HOSTLIST> --dpi-desync=multidisorder --dpi-desync-split-pos=1,sniext+1,host+1,midsld-2,midsld,midsld+2,endhost-1
`
	return os.WriteFile(p.config.StrategyFile, []byte(strategy), 0644)
}

// monitor monitors the zapret process
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
		p.log("error", "%s exited: %v", p.config.ProviderType, err)
	} else {
		p.log("info", "%s exited cleanly", p.config.ProviderType)
	}
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		p.onLog(level, "["+p.config.ProviderType+"] "+msg)
	}
}

