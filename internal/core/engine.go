package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/dns"
	"github.com/Chistovik92/hydravpn-router/internal/firewall"
	"github.com/Chistovik92/hydravpn-router/internal/providers/byedpi"
	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
	"github.com/Chistovik92/hydravpn-router/internal/providers/zapret"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
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
	mu           sync.RWMutex
	config       *config.Config
	state        EngineState
	stateMu      sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	
	// Providers
	singboxProvider *singbox.Provider
	zapretProvider  *zapret.Provider
	byedpiProvider  *byedpi.Provider
	
	// Core services
	dnsManager       *dns.Manager
	firewallManager  *firewall.Manager
	subscriptionMgr  *subscription.Manager
	
	// Status tracking
	startTime     time.Time
	lastReload    time.Time
	reloadCount   int
	errorCount    int
	lastError     string
	
	// Callbacks
	onStateChange  func(EngineState)
	onLog          func(level, message string)
}

// EngineOptions configures the engine
type EngineOptions struct {
	Config          *config.Config
	OnStateChange   func(EngineState)
	OnLog           func(level, message string)
}

// NewEngine creates a new HydraVPN for Router engine
func NewEngine(opts EngineOptions) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	e := &Engine{
		config:       opts.Config,
		state:        EngineStateStopped,
		ctx:          ctx,
		cancel:       cancel,
		onStateChange: opts.OnStateChange,
		onLog:         opts.OnLog,
	}
	
	if e.config == nil {
		e.config = config.DefaultConfig()
	}
	
	// Initialize managers
	e.dnsManager = dns.NewManager(dns.Options{
		Config:    e.config,
		OnLog:     e.log,
	})
	
	e.firewallManager = firewall.NewManager(firewall.Options{
		Config:   e.config,
		OnLog:    e.log,
	})
	
	e.subscriptionMgr = subscription.NewManager(subscription.Options{
		Config:   e.config,
		OnLog:    e.log,
	})
	
	return e, nil
}

// Start starts the engine
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.state != EngineStateStopped {
		return fmt.Errorf("engine already started")
	}
	
	e.setState(EngineStateStarting)
	e.log("info", "Starting HydraVPN for Router "+version.Version)
	
	// Initialize providers based on config
	if err := e.initProviders(); err != nil {
		e.setState(EngineStateError)
		return fmt.Errorf("failed to initialize providers: %w", err)
	}
	
	// Start DNS manager
	if err := e.dnsManager.Start(e.ctx); err != nil {
		e.setState(EngineStateError)
		return fmt.Errorf("failed to start DNS manager: %w", err)
	}
	
	// Start firewall manager
	if err := e.firewallManager.Start(e.ctx); err != nil {
		e.setState(EngineStateError)
		return fmt.Errorf("failed to start firewall manager: %w", err)
	}
	
	// Start subscription manager
	if err := e.subscriptionMgr.Start(e.ctx); err != nil {
		e.setState(EngineStateError)
		return fmt.Errorf("failed to start subscription manager: %w", err)
	}
	
	// Start active providers
	if err := e.startProviders(); err != nil {
		e.setState(EngineStateError)
		return fmt.Errorf("failed to start providers: %w", err)
	}
	
	e.startTime = time.Now()
	e.setState(EngineStateRunning)
	e.log("info", "HydraVPN for Router started successfully")
	
	return nil
}

// Stop stops the engine
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.state == EngineStateStopped || e.state == EngineStateStopping {
		return nil
	}
	
	e.setState(EngineStateStopping)
	e.log("info", "Stopping HydraVPN for Router")
	
	// Stop providers
	e.stopProviders()
	
	// Stop managers
	e.subscriptionMgr.Stop()
	e.firewallManager.Stop()
	e.dnsManager.Stop()
	
	// Cancel context and wait for goroutines
	e.cancel()
	e.wg.Wait()
	
	e.setState(EngineStateStopped)
	e.log("info", "HydraVPN for Router stopped")
	
	return nil
}

// Reload reloads the configuration
func (e *Engine) Reload(newConfig *config.Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	e.log("info", "Reloading configuration")
	
	// Update config
	e.config = newConfig
	
	// Reload managers
	if err := e.dnsManager.Reload(newConfig); err != nil {
		e.log("error", "Failed to reload DNS: "+err.Error())
	}
	
	if err := e.firewallManager.Reload(newConfig); err != nil {
		e.log("error", "Failed to reload firewall: "+err.Error())
	}
	
	if err := e.subscriptionMgr.Reload(newConfig); err != nil {
		e.log("error", "Failed to reload subscriptions: "+err.Error())
	}
	
	// Reload providers
	if err := e.reloadProviders(); err != nil {
		e.log("error", "Failed to reload providers: "+err.Error())
	}
	
	e.lastReload = time.Now()
	e.reloadCount++
	e.log("info", "Configuration reloaded")
	
	return nil
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
		"version":       version.Version,
		"state":         e.state,
		"uptime":        time.Since(e.startTime).String(),
		"last_reload":   e.lastReload.Format(time.RFC3339),
		"reload_count":  e.reloadCount,
		"error_count":   e.errorCount,
		"last_error":    e.lastError,
		"config":        e.config,
	}
	
	if e.singboxProvider != nil {
		status["singbox"] = e.singboxProvider.GetStatus()
	}
	if e.zapretProvider != nil {
		status["zapret"] = e.zapretProvider.GetStatus()
	}
	if e.byedpiProvider != nil {
		status["byedpi"] = e.byedpiProvider.GetStatus()
	}
	
	status["dns"] = e.dnsManager.GetStatus()
	status["firewall"] = e.firewallManager.GetStatus()
	status["subscriptions"] = e.subscriptionMgr.GetStatus()
	
	return status
}

// GetConfig returns the current configuration
func (e *Engine) GetConfig() *config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// setState updates the engine state
func (e *Engine) setState(state EngineState) {
	e.stateMu.Lock()
	e.state = state
	e.stateMu.Unlock()
	
	if e.onStateChange != nil {
		e.onStateChange(state)
	}
}

// log logs a message
func (e *Engine) log(level, message string) {
	if e.onLog != nil {
		e.onLog(level, message)
	}
}

// initProviders initializes providers based on configuration
func (e *Engine) initProviders() error {
	// Always initialize sing-box as it's the core
	sbConfig := singbox.ConfigFromPodkop(e.config)
	e.singboxProvider = singbox.NewProvider(singbox.Options{
		Config:   sbConfig,
		OnLog:    e.log,
	})
	
	// Initialize zapret if enabled
	if e.isProviderEnabled(config.ProviderTypeZapret) || e.isProviderEnabled(config.ProviderTypeZapret2) {
		zConfig := zapret.ConfigFromPodkop(e.config)
		e.zapretProvider = zapret.NewProvider(zapret.Options{
			Config:   zConfig,
			OnLog:    e.log,
		})
	}
	
	// Initialize ByeDPI if enabled
	if e.isProviderEnabled(config.ProviderTypeByeDPI) {
		bConfig := byedpi.ConfigFromPodkop(e.config)
		e.byedpiProvider = byedpi.NewProvider(byedpi.Options{
			Config:   bConfig,
			OnLog:    e.log,
		})
	}
	
	return nil
}

// isProviderEnabled checks if a provider type is enabled in config
func (e *Engine) isProviderEnabled(providerType config.ProviderType) bool {
	// Check sections for provider usage
	for _, section := range e.config.Sections {
		if section.Enabled {
			// In a real implementation, we'd check which provider the section uses
			// For now, enable all configured providers
			return true
		}
	}
	return false
}

// startProviders starts all initialized providers
func (e *Engine) startProviders() error {
	if e.singboxProvider != nil {
		if err := e.singboxProvider.Start(e.ctx); err != nil {
			return fmt.Errorf("sing-box: %w", err)
		}
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

// stopProviders stops all providers
func (e *Engine) stopProviders() {
	if e.singboxProvider != nil {
		e.singboxProvider.Stop()
	}
	if e.zapretProvider != nil {
		e.zapretProvider.Stop()
	}
	if e.byedpiProvider != nil {
		e.byedpiProvider.Stop()
	}
}

// reloadProviders reloads all providers
func (e *Engine) reloadProviders() error {
	if e.singboxProvider != nil {
		sbConfig := singbox.ConfigFromPodkop(e.config)
		if err := e.singboxProvider.Reload(sbConfig); err != nil {
			return fmt.Errorf("sing-box: %w", err)
		}
	}
	
	if e.zapretProvider != nil {
		zConfig := zapret.ConfigFromPodkop(e.config)
		if err := e.zapretProvider.Reload(zConfig); err != nil {
			return fmt.Errorf("zapret: %w", err)
		}
	}
	
	if e.byedpiProvider != nil {
		bConfig := byedpi.ConfigFromPodkop(e.config)
		if err := e.byedpiProvider.Reload(bConfig); err != nil {
			return fmt.Errorf("byedpi: %w", err)
		}
	}
	
	return nil
}

// ExecuteCommand executes a CLI command
func (e *Engine) ExecuteCommand(cmd string, args []string) (string, error) {
	switch cmd {
	case "status":
		status := e.GetStatus()
		data, _ := json.MarshalIndent(status, "", "  ")
		return string(data), nil
	case "config":
		data, _ := json.MarshalIndent(e.GetConfig(), "", "  ")
		return string(data), nil
	case "providers":
		return e.getProvidersStatus(), nil
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

func (e *Engine) getProvidersStatus() string {
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
	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data)
}

