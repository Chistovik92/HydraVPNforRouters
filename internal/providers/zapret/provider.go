package zapret

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

const stopTimeout = 5 * time.Second

// DefaultQueueNum is the NFQUEUE number shared with the firewall rules.
const DefaultQueueNum = 4000

// DesyncMark is the fwmark nfqws/nfqws2 put on their own packets by default;
// the firewall excludes packets carrying it from the queue to avoid loops.
const DesyncMark = "0x40000000"

// Default strategies. They only use documented nfqws/nfqws2 options and can
// be replaced per section with "provider_options".
const (
	defaultZapretStrategy = `--filter-tcp=80 --dpi-desync=fake,multisplit --dpi-desync-split-pos=method+2 --dpi-desync-fooling=md5sig
--new
--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=11 --dpi-desync-fooling=md5sig --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com
--new
--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11`

	defaultZapret2Strategy = `--lua-init=@/opt/zapret2/lua/zapret-lib.lua --lua-init=@/opt/zapret2/lua/zapret-antidpi.lua
--filter-tcp=80 --filter-l7=http --payload=http_req --lua-desync=fake:blob=fake_default_http:tcp_md5 --lua-desync=multisplit:pos=method+2
--new
--filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5 --lua-desync=multidisorder:pos=1,midsld
--new
--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=6`
)

// Provider manages zapret/zapret2 (nfqws/nfqws2)
type Provider struct {
	mu     sync.RWMutex
	config *Config
	proc   *process.Supervisor
	onLog  func(level, message string)
}

// Config represents zapret configuration
type Config struct {
	BinaryPath   string
	QueueNum     int
	Options      string // custom strategy, replaces the default one
	RespawnDelay int
	ProviderType string // "zapret" or "zapret2"
}

// Options for creating a new provider
type Options struct {
	Config *Config
	OnLog  func(level, message string)
}

// NewProvider creates a new zapret provider
func NewProvider(opts Options) *Provider {
	c := withDefaults(opts.Config)
	return &Provider{
		config: c,
		onLog:  opts.OnLog,
		proc: &process.Supervisor{
			Name:         c.ProviderType,
			RespawnDelay: time.Duration(c.RespawnDelay) * time.Second,
			OnLog:        opts.OnLog,
		},
	}
}

func withDefaults(c *Config) *Config {
	if c == nil {
		c = &Config{}
	}
	if c.ProviderType == "" {
		c.ProviderType = string(config.ProviderTypeZapret2)
	}
	if c.BinaryPath == "" {
		if c.ProviderType == string(config.ProviderTypeZapret2) {
			c.BinaryPath = "/opt/zapret2/nfq2/nfqws2"
		} else {
			c.BinaryPath = "/opt/zapret/nfq/nfqws"
		}
	}
	if c.QueueNum == 0 {
		c.QueueNum = DefaultQueueNum
	}
	if c.RespawnDelay == 0 {
		c.RespawnDelay = 5
	}
	return c
}

// ConfigFromSettings creates zapret config from the HydraVPN config.
// zapret2 wins when both zapret and zapret2 sections are enabled.
func ConfigFromSettings(cfg *config.Config) *Config {
	pt := config.ProviderTypeZapret2
	if !cfg.ProviderEnabled(config.ProviderTypeZapret2) && cfg.ProviderEnabled(config.ProviderTypeZapret) {
		pt = config.ProviderTypeZapret
	}
	return withDefaults(&Config{
		ProviderType: string(pt),
		Options:      cfg.ProviderOptions(pt),
	})
}

// Start starts zapret
func (p *Provider) Start(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.proc.Start(p.config.BinaryPath, p.buildArgs())
}

// Stop stops zapret
func (p *Provider) Stop() error {
	p.proc.Stop(stopTimeout)
	return nil
}

// Reload restarts nfqws with the new strategy (nfqws has no reload signal).
func (p *Provider) Reload(cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = withDefaults(cfg)
	if !p.proc.Status().Running {
		return nil
	}
	if err := p.proc.Restart(p.config.BinaryPath, p.buildArgs(), stopTimeout); err != nil {
		return err
	}
	p.log("info", "%s restarted with new strategy", p.config.ProviderType)
	return nil
}

// QueueNum returns the NFQUEUE number nfqws listens on.
func (p *Provider) QueueNum() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.QueueNum
}

// GetStatus returns provider status
func (p *Provider) GetStatus() map[string]interface{} {
	p.mu.RLock()
	providerType, queue := p.config.ProviderType, p.config.QueueNum
	p.mu.RUnlock()

	st := p.proc.Status()
	status := map[string]interface{}{
		"running":    st.Running,
		"provider":   providerType,
		"queue":      queue,
		"pid":        st.PID,
		"uptime":     "",
		"restarts":   st.Restarts,
		"last_error": st.LastError,
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

// buildArgs builds nfqws/nfqws2 command line arguments
func (p *Provider) buildArgs() []string {
	args := []string{"--qnum=" + strconv.Itoa(p.config.QueueNum)}

	strategy := p.config.Options
	if strategy == "" {
		if p.config.ProviderType == string(config.ProviderTypeZapret2) {
			strategy = defaultZapret2Strategy
		} else {
			strategy = defaultZapretStrategy
		}
	}
	return append(args, strings.Fields(strategy)...)
}

func (p *Provider) log(level, format string, args ...interface{}) {
	if p.onLog != nil {
		p.onLog(level, "["+p.config.ProviderType+"] "+fmt.Sprintf(format, args...))
	}
}
