package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/components"
	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/dns"
	"github.com/Chistovik92/hydravpn-router/internal/dnsredirect"
	"github.com/Chistovik92/hydravpn-router/internal/firewall"
	"github.com/Chistovik92/hydravpn-router/internal/lists"
	"github.com/Chistovik92/hydravpn-router/internal/providers/byedpi"
	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
	"github.com/Chistovik92/hydravpn-router/internal/providers/zapret"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
	"github.com/Chistovik92/hydravpn-router/internal/wanmon"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// EngineState represents the current state of the engine
type EngineState string

const (
	EngineStateStopped  EngineState = "stopped"
	EngineStateStarting EngineState = "starting"
	EngineStateRunning  EngineState = "running"
	EngineStateStopping EngineState = "stopping"
	EngineStateError    EngineState = "error"
)

// Engine manages all HydraVPN for Router components
type Engine struct {
	mu      sync.RWMutex
	config  *config.Config
	state   EngineState
	stateMu sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc

	// Providers
	singboxProvider *singbox.Provider
	zapretProvider  *zapret.Provider
	byedpiProvider  *byedpi.Provider

	// Core services
	dnsManager      *dns.Manager
	firewallManager *firewall.Manager
	subscriptionMgr *subscription.Manager
	listsMgr        *lists.Manager
	dnsRedirect     *dnsredirect.Redirect
	wan             *wanmon.Monitor
	comps           *components.Checker

	// Status tracking
	startTime   time.Time
	lastReload  time.Time
	reloadCount int
	errorCount  int
	lastError   string

	// subscription updates are applied by a goroutine so the update loop
	// never waits for the engine lock (Stop waits for the update loop).
	subsChanged chan struct{}

	// Callbacks
	onStateChange func(EngineState)
	onLog         func(level, message string)
}

// EngineOptions configures the engine
type EngineOptions struct {
	Config        *config.Config
	OnStateChange func(EngineState)
	OnLog         func(level, message string)
}

// NewEngine creates a new HydraVPN for Router engine
func NewEngine(opts EngineOptions) (*Engine, error) {
	e := &Engine{
		config:        opts.Config,
		state:         EngineStateStopped,
		subsChanged:   make(chan struct{}, 1),
		onStateChange: opts.OnStateChange,
		onLog:         opts.OnLog,
	}

	if e.config == nil {
		e.config = config.DefaultConfig()
	}

	e.dnsManager = dns.NewManager(dns.Options{Config: e.config, OnLog: e.log})
	e.firewallManager = firewall.NewManager(firewall.Options{Config: e.config, OnLog: e.log})
	notify := func() {
		select {
		case e.subsChanged <- struct{}{}:
		default:
		}
	}
	e.subscriptionMgr = subscription.NewManager(subscription.Options{Config: e.config, OnLog: e.log, OnUpdate: notify})
	e.listsMgr = lists.NewManager(lists.Options{Config: e.config, OnLog: e.log, OnUpdate: notify})
	e.dnsRedirect = dnsredirect.New(e.log)
	e.comps = components.New(e.config, e.log)

	return e, nil
}

// Start starts the engine. On failure every component that was already
// started is stopped again, so Start can be retried.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if st := e.GetState(); st != EngineStateStopped && st != EngineStateError {
		return fmt.Errorf("engine already %s", st)
	}

	e.setState(EngineStateStarting)
	e.log("info", "Starting HydraVPN for Router "+version.Version)
	e.ctx, e.cancel = context.WithCancel(context.Background())

	if err := e.startComponents(); err != nil {
		e.stopComponents()
		e.cancel()
		e.recordError(err)
		e.setState(EngineStateError)
		return err
	}

	e.startTime = time.Now()
	e.setState(EngineStateRunning)
	go e.applySubscriptionUpdates(e.ctx)
	e.log("info", "HydraVPN for Router started successfully")
	return nil
}

func (e *Engine) startComponents() error {
	e.initProviders()

	// sing-box must listen before the firewall redirects traffic to it.
	if err := e.startProviders(); err != nil {
		return fmt.Errorf("failed to start providers: %w", err)
	}
	if err := e.dnsManager.Start(e.ctx); err != nil {
		return fmt.Errorf("failed to start DNS manager: %w", err)
	}
	e.firewallManager.SetNFQueue(e.nfqueueOptions())
	if err := e.firewallManager.Start(e.ctx); err != nil {
		return fmt.Errorf("failed to start firewall manager: %w", err)
	}
	if err := e.subscriptionMgr.Start(e.ctx); err != nil {
		return fmt.Errorf("failed to start subscription manager: %w", err)
	}
	if err := e.listsMgr.Start(e.ctx); err != nil {
		return fmt.Errorf("failed to start list manager: %w", err)
	}
	e.applyDNSRedirect()
	e.startWANMonitor()
	e.comps.Start(e.ctx)
	return nil
}

// applySubscriptionUpdates re-renders the sing-box config whenever the set of
// subscription nodes changed.
func (e *Engine) applySubscriptionUpdates(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.subsChanged:
		}
		// Let several updates that arrive together (start-up) collapse.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		e.mu.Lock()
		if ctx.Err() == nil && e.GetState() == EngineStateRunning {
			if err := e.singboxProvider.Reload(e.singboxConfig()); err != nil {
				e.log("error", "Failed to apply subscription update to sing-box: "+err.Error())
				e.recordError(err)
			} else {
				e.log("info", "sing-box configuration updated from subscriptions")
			}
		}
		e.mu.Unlock()
	}
}

// applyDNSRedirect points dnsmasq at sing-box when FakeIP needs it and
// restores dnsmasq otherwise.
func (e *Engine) applyDNSRedirect() {
	var err error
	if dnsredirect.Wanted(e.config) {
		err = e.dnsRedirect.Apply()
	} else {
		err = e.dnsRedirect.Remove()
	}
	if err != nil {
		e.log("warn", "DNS redirect: "+err.Error())
	}
}

func (e *Engine) startWANMonitor() {
	s := e.config.Settings
	if !s.EnableBadWANInterfaceMonitoring || len(s.BadWANMonitoredInterfaces) == 0 {
		return
	}
	delay := time.Duration(s.BadWANReloadDelay) * time.Millisecond
	if delay <= 0 {
		delay = 2 * time.Second
	}
	e.wan = &wanmon.Monitor{
		Interfaces: s.BadWANMonitoredInterfaces,
		Delay:      delay,
		OnChange:   e.onWANChange,
		OnLog:      e.log,
	}
	e.wan.Start(e.ctx)
}

func (e *Engine) stopWANMonitor() {
	if e.wan != nil {
		e.wan.Stop()
		e.wan = nil
	}
}

// onWANChange rebuilds routing after a WAN interface came back: the tproxy
// rules are re-applied and sing-box drops its stale connections.
func (e *Engine) onWANChange(iface, reason string) {
	// Stop and Reload hold e.mu while they wait for this monitor to stop;
	// they rebuild everything anyway, so skip instead of deadlocking.
	if !e.mu.TryLock() {
		return
	}
	defer e.mu.Unlock()
	if e.GetState() != EngineStateRunning {
		return
	}
	e.log("info", "WAN "+iface+" ("+reason+"): reloading firewall and sing-box")
	if err := e.firewallManager.Reload(e.config); err != nil {
		e.log("error", "firewall reload after WAN change: "+err.Error())
	}
	if err := e.singboxProvider.Reload(e.singboxConfig()); err != nil {
		e.log("error", "sing-box reload after WAN change: "+err.Error())
	}
}

// singboxConfig renders the sing-box configuration for the current settings
// and the subscription nodes fetched so far.
func (e *Engine) singboxConfig() *singbox.Config {
	return singbox.ConfigFromSettings(e.config, e.subscriptionMgr, singbox.WithLists(e.listsMgr))
}

// stopComponents stops everything in reverse order; stopping a component
// that was never started is a no-op.
func (e *Engine) stopComponents() {
	e.comps.Stop()
	e.stopWANMonitor()
	if err := e.dnsRedirect.Remove(); err != nil {
		e.log("warn", "dnsmasq restore: "+err.Error())
	}
	e.listsMgr.Stop()
	e.subscriptionMgr.Stop()
	e.firewallManager.Stop()
	e.dnsManager.Stop()
	e.stopProviders()
}

// Stop stops the engine
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if st := e.GetState(); st == EngineStateStopped || st == EngineStateStopping {
		return nil
	}

	e.setState(EngineStateStopping)
	e.log("info", "Stopping HydraVPN for Router")

	e.stopComponents()
	if e.cancel != nil {
		e.cancel()
	}

	e.setState(EngineStateStopped)
	e.log("info", "HydraVPN for Router stopped")
	return nil
}

// Reload applies a new configuration
func (e *Engine) Reload(newConfig *config.Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if newConfig == nil {
		return fmt.Errorf("reload: nil config")
	}

	e.log("info", "Reloading configuration")
	e.config = newConfig
	running := e.GetState() == EngineStateRunning

	var firstErr error
	note := func(what string, err error) {
		if err != nil {
			e.log("error", "Failed to reload "+what+": "+err.Error())
			e.recordError(err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	note("DNS", e.dnsManager.Reload(newConfig))
	// Subscriptions first: the sing-box config is built from their nodes.
	note("subscriptions", e.subscriptionMgr.Reload(newConfig))
	note("lists", e.listsMgr.Reload(newConfig))
	e.comps.Reload(newConfig)
	if running {
		e.applyDNSRedirect()
		e.stopWANMonitor()
		e.startWANMonitor()
	}
	if running {
		note("providers", e.reloadProviders())
	}
	e.firewallManager.SetNFQueue(e.nfqueueOptions())
	note("firewall", e.firewallManager.Reload(newConfig))

	e.lastReload = time.Now()
	e.reloadCount++
	e.log("info", "Configuration reloaded")
	return firstErr
}

// GetState returns the current engine state
func (e *Engine) GetState() EngineState {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return e.state
}

// GetStatus returns detailed status information
func (e *Engine) GetStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := map[string]interface{}{
		"version":      version.Version,
		"state":        e.GetState(),
		"uptime":       "",
		"last_reload":  "",
		"reload_count": e.reloadCount,
		"error_count":  e.errorCount,
		"last_error":   e.lastError,
	}
	if e.GetState() == EngineStateRunning {
		status["uptime"] = time.Since(e.startTime).Round(time.Second).String()
	}
	if !e.lastReload.IsZero() {
		status["last_reload"] = e.lastReload.Format(time.RFC3339)
	}

	status["providers"] = e.providersStatusLocked()
	status["dns"] = e.dnsManager.GetStatus()
	status["firewall"] = e.firewallManager.GetStatus()
	status["subscriptions"] = e.subscriptionMgr.GetStatus()
	status["lists"] = e.listsMgr.GetStatus()
	status["components"] = e.comps.GetStatus()

	return status
}

// GetConfig returns the current configuration
func (e *Engine) GetConfig() *config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

func (e *Engine) setState(state EngineState) {
	e.stateMu.Lock()
	e.state = state
	e.stateMu.Unlock()

	if e.onStateChange != nil {
		e.onStateChange(state)
	}
}

func (e *Engine) recordError(err error) {
	e.errorCount++
	e.lastError = err.Error()
}

func (e *Engine) log(level, message string) {
	if e.onLog != nil {
		e.onLog(level, message)
	}
}

// initProviders creates providers enabled by the configuration.
func (e *Engine) initProviders() {
	// sing-box is the core and always runs.
	e.singboxProvider = singbox.NewProvider(singbox.Options{
		Config: e.singboxConfig(),
		OnLog:  e.log,
	})

	e.zapretProvider = nil
	if e.zapretEnabled() {
		e.zapretProvider = zapret.NewProvider(zapret.Options{Config: zapret.ConfigFromSettings(e.config), OnLog: e.log})
	}

	e.byedpiProvider = nil
	if e.config.ProviderEnabled(config.ProviderTypeByeDPI) {
		e.byedpiProvider = byedpi.NewProvider(byedpi.Options{Config: byedpi.ConfigFromSettings(e.config), OnLog: e.log})
	}
}

func (e *Engine) zapretEnabled() bool {
	return e.config.ProviderEnabled(config.ProviderTypeZapret) || e.config.ProviderEnabled(config.ProviderTypeZapret2)
}

// nfqueueOptions returns the firewall queue settings for nfqws, or nil.
func (e *Engine) nfqueueOptions() *firewall.NFQueueOptions {
	if e.zapretProvider == nil {
		return nil
	}
	return &firewall.NFQueueOptions{
		QueueNum:   e.zapretProvider.QueueNum(),
		DesyncMark: zapret.DesyncMark,
		TCPPorts:   []int{80, 443},
		UDPPorts:   []int{443},
	}
}

func (e *Engine) startProviders() error {
	if err := e.singboxProvider.Start(e.ctx); err != nil {
		return fmt.Errorf("sing-box: %w", err)
	}
	if e.zapretProvider != nil {
		if err := e.zapretProvider.Start(e.ctx); err != nil {
			return fmt.Errorf("zapret: %w", err)
		}
	}
	if e.byedpiProvider != nil {
		if err := e.byedpiProvider.Start(e.ctx); err != nil {
			return fmt.Errorf("byedpi: %w", err)
		}
	}
	return nil
}

func (e *Engine) stopProviders() {
	if e.byedpiProvider != nil {
		e.byedpiProvider.Stop()
	}
	if e.zapretProvider != nil {
		e.zapretProvider.Stop()
	}
	if e.singboxProvider != nil {
		e.singboxProvider.Stop()
	}
}

// reloadProviders reloads running providers and starts or stops the
// optional ones when they were enabled or disabled in the new config.
func (e *Engine) reloadProviders() error {
	if err := e.singboxProvider.Reload(e.singboxConfig()); err != nil {
		return fmt.Errorf("sing-box: %w", err)
	}

	switch {
	case e.zapretEnabled() && e.zapretProvider == nil:
		e.zapretProvider = zapret.NewProvider(zapret.Options{Config: zapret.ConfigFromSettings(e.config), OnLog: e.log})
		if err := e.zapretProvider.Start(e.ctx); err != nil {
			return fmt.Errorf("zapret: %w", err)
		}
	case !e.zapretEnabled() && e.zapretProvider != nil:
		e.zapretProvider.Stop()
		e.zapretProvider = nil
	case e.zapretProvider != nil:
		if err := e.zapretProvider.Reload(zapret.ConfigFromSettings(e.config)); err != nil {
			return fmt.Errorf("zapret: %w", err)
		}
	}

	byedpiEnabled := e.config.ProviderEnabled(config.ProviderTypeByeDPI)
	switch {
	case byedpiEnabled && e.byedpiProvider == nil:
		e.byedpiProvider = byedpi.NewProvider(byedpi.Options{Config: byedpi.ConfigFromSettings(e.config), OnLog: e.log})
		if err := e.byedpiProvider.Start(e.ctx); err != nil {
			return fmt.Errorf("byedpi: %w", err)
		}
	case !byedpiEnabled && e.byedpiProvider != nil:
		e.byedpiProvider.Stop()
		e.byedpiProvider = nil
	case e.byedpiProvider != nil:
		if err := e.byedpiProvider.Reload(byedpi.ConfigFromSettings(e.config)); err != nil {
			return fmt.Errorf("byedpi: %w", err)
		}
	}
	return nil
}

// ExecuteCommand executes a CLI command
func (e *Engine) ExecuteCommand(cmd string, args []string) (string, error) {
	switch cmd {
	case "status":
		data, _ := json.MarshalIndent(e.GetStatus(), "", "  ")
		return string(data), nil
	case "config":
		data, _ := json.MarshalIndent(e.GetConfig(), "", "  ")
		return string(data), nil
	case "providers":
		e.mu.RLock()
		data, _ := json.MarshalIndent(e.providersStatusLocked(), "", "  ")
		e.mu.RUnlock()
		return string(data), nil
	case "dns":
		return e.dnsManager.GetStatusJSON(), nil
	case "firewall":
		return e.firewallManager.GetStatusJSON(), nil
	case "subscriptions":
		return e.subscriptionMgr.GetStatusJSON(), nil
	default:
		return "", fmt.Errorf("unknown command: %s", cmd)
	}
}

func (e *Engine) providersStatusLocked() map[string]interface{} {
	status := map[string]interface{}{}
	if e.singboxProvider != nil {
		status["singbox"] = e.singboxProvider.GetStatus()
	}
	if e.zapretProvider != nil {
		status["zapret"] = e.zapretProvider.GetStatus()
	}
	if e.byedpiProvider != nil {
		status["byedpi"] = e.byedpiProvider.GetStatus()
	}
	return status
}
