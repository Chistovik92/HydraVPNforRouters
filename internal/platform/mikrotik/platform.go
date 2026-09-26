package mikrotik

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Platform implements the MikroTik RouterOS platform integration
type Platform struct {
	engine      *core.Engine
	config      *config.Config
	container   bool
	routerOSVer string
	arch        string
	webUIPort   int
}

// NewPlatform creates a new MikroTik platform
func NewPlatform(cfg *config.Config) *Platform {
	p := &Platform{
		config:    cfg,
		container: fileExists("/.dockerenv") || fileExists("/run/.containerenv"),
		webUIPort: 8080,
	}

	// Detect RouterOS version and architecture
	p.detectSystem()

	return p
}

// detectSystem detects RouterOS version and architecture
func (p *Platform) detectSystem() {
	if out, err := exec.Command("/system", "resource", "print").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "version":
				p.routerOSVer = strings.TrimSpace(value)
			case "architecture-name":
				p.arch = strings.TrimSpace(value)
			}
		}
	}
}

// Initialize initializes the platform
func (p *Platform) Initialize(ctx context.Context) error {
	if p.container {
		return p.initializeContainer(ctx)
	}
	return p.initializeNative(ctx)
}

// initializeContainer initializes for container deployment
func (p *Platform) initializeContainer(ctx context.Context) error {
	dirs := []string{
		"/etc/hydravpn-router",
		"/etc/hydravpn-router/sing-box",
		"/etc/hydravpn-router/zapret",
		"/etc/hydravpn-router/byedpi",
		"/var/run/hydravpn-router",
		"/var/run/hydravpn-router/sing-box",
		"/var/run/hydravpn-router/zapret",
		"/var/run/hydravpn-router/byedpi",
	}

	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}

	configPath := "/etc/hydravpn-router/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	return nil
}

// initializeNative initializes for native RouterOS deployment (via NPK)
func (p *Platform) initializeNative(ctx context.Context) error {
	// RouterOS native packages use .npk format
	// This would be built separately using MikroTik's build system

	dirs := []string{
		"/flash/hydravpn-router",
		"/flash/hydravpn-router/sing-box",
		"/flash/hydravpn-router/zapret",
		"/flash/hydravpn-router/byedpi",
		"/flash/hydravpn-router/scripts",
		"/flash/hydravpn-router/web",
	}

	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}

	configPath := "/flash/hydravpn-router/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	// Create RouterOS scripts
	scripts := map[string]string{
		"start.rsc":     startScript(),
		"stop.rsc":      stopScript(),
		"reload.rsc":    reloadScript(),
		"status.rsc":    statusScript(),
		"install.rsc":   installScript(),
		"uninstall.rsc": uninstallScript(),
		"wan-up.rsc":    wanUpScript(),
		"config.rsc":    configScript(),
	}

	for name, content := range scripts {
		os.WriteFile(filepath.Join("/flash/hydravpn-router/scripts", name), []byte(content), 0644)
	}

	// Create scheduler entries
	scheduler := `/system scheduler
add name="hydravpn-router-start" on-event="/system script run hydravpn-router-start" start-time=startup interval=0
add name="hydravpn-router-wan-up" on-event="/system script run hydravpn-router-wan-up" interval=1m
add name="hydravpn-router-health" on-event="/system script run hydravpn-router-health" interval=5m
`
	os.WriteFile("/flash/hydravpn-router/scheduler.rsc", []byte(scheduler), 0644)

	// Create NPK package structure
	npkDir := "/flash/hydravpn-router.npk"
	os.MkdirAll(filepath.Join(npkDir, "scripts"), 0755)
	os.MkdirAll(filepath.Join(npkDir, "web"), 0755)
	os.MkdirAll(filepath.Join(npkDir, "bin"), 0755)

	// Copy files to NPK structure
	copyDir("/flash/hydravpn-router/scripts", filepath.Join(npkDir, "scripts"))

	// Binary would be cross-compiled for RouterOS architecture
	// cp hydravpn-router-${arch} /flash/hydravpn-router.npk/bin/hydravpn-router

	// Create package.xml
	packageXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package>
    <name>hydravpn-router</name>
    <version>%s</version>
    <description>Multi-platform DPI bypass solution for RouterOS</description>
    <author>Chistovik92</author>
    <license>GPL-3.0</license>
    <homepage>https://github.com/Chistovik92/hydravpn-router</homepage>
    <architecture>%s</architecture>
    <min-routeros-version>7.0</min-routeros-version>
    <scripts>
        <script name="start" file="scripts/start.rsc"/>
        <script name="stop" file="scripts/stop.rsc"/>
        <script name="reload" file="scripts/reload.rsc"/>
        <script name="status" file="scripts/status.rsc"/>
        <script name="install" file="scripts/install.rsc"/>
        <script name="uninstall" file="scripts/uninstall.rsc"/>
        <script name="wan-up" file="scripts/wan-up.rsc"/>
        <script name="config" file="scripts/config.rsc"/>
    </scripts>
    <scheduler file="scheduler.rsc"/>
    <web path="web"/>
    <binary path="bin/hydravpn-router"/>
    <config file="config.yaml"/>
</package>
`, version.Version, p.arch)

	os.WriteFile(filepath.Join(npkDir, "package.xml"), []byte(packageXML), 0644)

	return nil
}

// Start starts the platform services
func (p *Platform) Start(ctx context.Context) error {
	engine, err := core.NewEngine(core.EngineOptions{
		Config: p.config,
		OnStateChange: func(state core.EngineState) {
			p.updateRouterOSState(state)
		},
		OnLog: func(level, msg string) {
			p.logToRouterOS(level, msg)
		},
	})
	if err != nil {
		return err
	}

	p.engine = engine
	return engine.Start()
}

// Stop stops the platform services
func (p *Platform) Stop() error {
	if p.engine != nil {
		return p.engine.Stop()
	}
	return nil
}

// Reload reloads configuration
func (p *Platform) Reload(cfg *config.Config) error {
	p.config = cfg
	if p.engine != nil {
		return p.engine.Reload(cfg)
	}
	return nil
}

// updateRouterOSState updates RouterOS with engine state
func (p *Platform) updateRouterOSState(state core.EngineState) {
	stateStr := string(state)
	// Use RouterOS API to update global variable
	exec.Command("/system", "script", "run", "hydravpn-router-set-state", "state="+stateStr).Run()
}

// logToRouterOS logs to RouterOS log
func (p *Platform) logToRouterOS(level, msg string) {
	exec.Command("/log", "print", "where", "topics~hydravpn-router", "message="+msg).Run()
}

// GenerateNPK generates MikroTik NPK package
func (p *Platform) GenerateNPK(version, outputDir string) error {
	// NPK packages are created using MikroTik's build system
	// This is a placeholder for the build process
	return nil
}

// GenerateContainerImage generates Docker container image
func (p *Platform) GenerateContainerImage(version, outputDir string) error {
	// Build Docker image
	cmd := exec.Command("docker", "build", "-t", "hydravpn-router:"+version, ".")
	cmd.Dir = "/flash/hydravpn-router.npk"
	return cmd.Run()
}

// GetSystemInfo returns RouterOS system information
func (p *Platform) GetSystemInfo() map[string]interface{} {
	info := map[string]interface{}{
		"platform":     "mikrotik",
		"routeros_ver": p.routerOSVer,
		"arch":         p.arch,
		"container":    p.container,
		"web_ui_port":  p.webUIPort,
	}

	// Get system resources
	if out, err := exec.Command("/system", "resource", "print").Output(); err == nil {
		info["resources"] = string(out)
	}

	// Get interfaces
	if out, err := exec.Command("/interface", "print").Output(); err == nil {
		info["interfaces"] = string(out)
	}

	// Get firewall rules
	if out, err := exec.Command("/ip", "firewall", "filter", "print").Output(); err == nil {
		info["firewall_filter"] = string(out)
	}

	// Get NAT rules
	if out, err := exec.Command("/ip", "firewall", "nat", "print").Output(); err == nil {
		info["firewall_nat"] = string(out)
	}

	// Get mangle rules
	if out, err := exec.Command("/ip", "firewall", "mangle", "print").Output(); err == nil {
		info["firewall_mangle"] = string(out)
	}

	// Get routing table
	if out, err := exec.Command("/ip", "route", "print").Output(); err == nil {
		info["routes"] = string(out)
	}

	return info
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// RouterOS Scripts
func startScript() string {
	return `# HydraVPN for Router start script
:global hydravpnState "starting"
:log info "Starting HydraVPN for Router"

/system script run hydravpn-router-health

# Start the service
/container start hydravpn-router

:global hydravpnState "running"
:log info "HydraVPN for Router started"
`
}

func stopScript() string {
	return `# HydraVPN for Router stop script
:global hydravpnState "stopping"
:log info "Stopping HydraVPN for Router"

/container stop hydravpn-router

:global hydravpnState "stopped"
:log info "HydraVPN for Router stopped"
`
}

func reloadScript() string {
	return `# HydraVPN for Router reload script
:log info "Reloading HydraVPN for Router configuration"

/container exec hydravpn-router hydravpn-router reload -c /etc/hydravpn-router/config.yaml

:log info "HydraVPN for Router reloaded"
`
}

func statusScript() string {
	return `# HydraVPN for Router status script
:global hydravpnState

:put "HydraVPN for Router State: \$hydravpnState"
:put "Container Status: [/container get hydravpn-router status]"

/container exec hydravpn-router hydravpn-router status
`
}

func installScript() string {
	return `# HydraVPN for Router install script
:log info "Installing HydraVPN for Router"

# Create container
/container add name=hydravpn-router \
    image=hydravpn-router:latest \
    interface=veth1 \
    mounts=hydravpn-router-config:/etc/hydravpn-router,hydravpn-router-runtime:/var/run/hydravpn-router \
    dns=77.88.8.8,77.88.8.1 \
    envlist="ROUTEROS_CONFIG" \
    workdir=/ \
    entrypoint=/entrypoint.sh \
    cmd="" \
    root-dir=/flash/hydravpn-router \
    logging=yes

# Add mount points
/container mounts add name=hydravpn-router-config src=/flash/hydravpn-router dst=/etc/hydravpn-router
/container mounts add name=hydravpn-router-runtime src=/flash/hydravpn-router/runtime dst=/var/run/hydravpn-router

# Add scheduler
/system script run hydravpn-router-scheduler

:log info "HydraVPN for Router installed"
`
}

func uninstallScript() string {
	return `# HydraVPN for Router uninstall script
:log info "Uninstalling HydraVPN for Router"

# Stop and remove container
/container stop hydravpn-router
/container remove hydravpn-router

# Remove mounts
/container mounts remove [find name=hydravpn-router-config]
/container mounts remove [find name=hydravpn-router-runtime]

# Remove scheduler
/system scheduler remove [find name~"hydravpn-router"]

# Remove files
/file remove [find name~"hydravpn-router"]

:log info "HydraVPN for Router uninstalled"
`
}

func wanUpScript() string {
	return `# HydraVPN for Router WAN up script
:local wanInterface [/ip route get [find dst-address=0.0.0.0/0] gateway]
:if (\$wanInterface != "") do={
    :log info "WAN interface \$wanInterface up, reloading HydraVPN for Router"
    /system script run hydravpn-router-reload
}
`
}

func configScript() string {
	return `# HydraVPN for Router config script
# This script generates RouterOS configuration from HydraVPN for Router config

:local configFile "/flash/hydravpn-router/config.yaml"
:if ([/file find name=\$configFile] = "") do={
    :error "Config file not found"
}

# Parse YAML and apply to RouterOS
# This would be implemented with a YAML parser in RouterOS v7+
:log info "Applying HydraVPN for Router configuration to RouterOS"
`
}

func healthScript() string {
	return `# HydraVPN for Router health check script
:global hydravpnState

:if (\$hydravpnState = "running") do={
    :local containerStatus [/container get hydravpn-router status]
    :if (\$containerStatus != "running") do={
        :log error "HydraVPN for Router container not running, restarting"
        /system script run hydravpn-router-start
    }
}
`
}
