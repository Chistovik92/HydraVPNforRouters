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
)

// Platform implements the MikroTik RouterOS platform integration
type Platform struct {
	engine       *core.Engine
	config       *config.Config
	container    bool
	routerOSVer  string
	arch         string
	webUIPort    int
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
			if strings.Contains(line, "version") {
				p.routerOSVer = strings.TrimSpace(strings.Split(line, ":")[1])
			}
			if strings.Contains(line, "architecture-name") {
				p.arch = strings.TrimSpace(strings.Split(line, ":")[1])
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
		"/etc/podkop-plus",
		"/etc/podkop-plus/sing-box",
		"/etc/podkop-plus/zapret",
		"/etc/podkop-plus/byedpi",
		"/var/run/podkop-plus",
		"/var/run/podkop-plus/sing-box",
		"/var/run/podkop-plus/zapret",
		"/var/run/podkop-plus/byedpi",
	}
	
	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}
	
	configPath := "/etc/podkop-plus/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}
	
	// Create container entrypoint
	entrypoint := `#!/bin/sh
# HydraVPN for Router container entrypoint
set -e

CONFIG_FILE="/etc/podkop-plus/config.yaml"

# Apply RouterOS configuration from environment
if [ -n "$ROUTEROS_CONFIG" ]; then
    echo "$ROUTEROS_CONFIG" | base64 -d > "$CONFIG_FILE"
fi

# Start HydraVPN for Router
exec /usr/bin/podkop-plus start -c "$CONFIG_FILE"
`
	os.WriteFile("/entrypoint.sh", []byte(entrypoint), 0755)
	
	// Create Dockerfile
	dockerfile := `FROM alpine:3.20

# Install dependencies
RUN apk add --no-cache \
    sing-box \
    nftables \
    iptables \
    iproute2 \
    curl \
    ca-certificates \
    bash \
    coreutils \
    bind-tools

# Copy HydraVPN for Router binary
COPY podkop-plus /usr/bin/podkop-plus
COPY entrypoint.sh /entrypoint.sh

# Create directories
RUN mkdir -p /etc/podkop-plus/sing-box \
    /etc/podkop-plus/zapret \
    /etc/podkop-plus/byedpi \
    /var/run/podkop-plus

# Set entrypoint
ENTRYPOINT ["/entrypoint.sh"]

# Labels
LABEL org.opencontainers.image.title="HydraVPN for Router"
LABEL org.opencontainers.image.description="Multi-platform DPI bypass for MikroTik"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.authors="Chistovik92"
LABEL org.opencontainers.image.url="https://github.com/Chistovik92/hydravpn-router"
LABEL org.opencontainers.image.licenses="GPL-3.0"
`
	os.WriteFile("/Dockerfile", []byte(dockerfile), 0644)
	
	// Create docker-compose.yml
	compose := `version: '3.8'

services:
  podkop-plus:
    build: .
    image: podkop-plus:latest
    container_name: podkop-plus
    restart: unless-stopped
    network_mode: host
    privileged: true
    cap_add:
      - NET_ADMIN
      - NET_RAW
      - SYS_MODULE
    volumes:
      - /etc/podkop-plus:/etc/podkop-plus
      - /var/run/podkop-plus:/var/run/podkop-plus
      - /lib/modules:/lib/modules:ro
    environment:
      - ROUTEROS_CONFIG=${ROUTEROS_CONFIG}
    logging:
      driver: journald
`
	os.WriteFile("/docker-compose.yml", []byte(compose), 0644)
	
	return nil
}

// initializeNative initializes for native RouterOS deployment (via NPK)
func (p *Platform) initializeNative(ctx context.Context) error {
	// RouterOS native packages use .npk format
	// This would be built separately using MikroTik's build system
	
	dirs := []string{
		"/flash/podkop-plus",
		"/flash/podkop-plus/sing-box",
		"/flash/podkop-plus/zapret",
		"/flash/podkop-plus/byedpi",
		"/flash/podkop-plus/scripts",
		"/flash/podkop-plus/web",
	}
	
	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}
	
	configPath := "/flash/podkop-plus/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}
	
	// Create RouterOS scripts
	scripts := map[string]string{
		"start.rsc":      startScript(),
		"stop.rsc":       stopScript(),
		"reload.rsc":     reloadScript(),
		"status.rsc":     statusScript(),
		"install.rsc":    installScript(),
		"uninstall.rsc":  uninstallScript(),
		"wan-up.rsc":     wanUpScript(),
		"config.rsc":     configScript(),
	}
	
	for name, content := range scripts {
		os.WriteFile(filepath.Join("/flash/podkop-plus/scripts", name), []byte(content), 0644)
	}
	
	// Create scheduler entries
	scheduler := `/system scheduler
add name="podkop-plus-start" on-event="/system script run podkop-plus-start" start-time=startup interval=0
add name="podkop-plus-wan-up" on-event="/system script run podkop-plus-wan-up" interval=1m
add name="podkop-plus-health" on-event="/system script run podkop-plus-health" interval=5m
`
	os.WriteFile("/flash/podkop-plus/scheduler.rsc", []byte(scheduler), 0644)
	
	// Create NPK package structure
	npkDir := "/flash/podkop-plus.npk"
	os.MkdirAll(filepath.Join(npkDir, "scripts"), 0755)
	os.MkdirAll(filepath.Join(npkDir, "web"), 0755)
	os.MkdirAll(filepath.Join(npkDir, "bin"), 0755)
	
	// Copy files to NPK structure
	copyDir("/flash/podkop-plus/scripts", filepath.Join(npkDir, "scripts"))
	copyDir("/flash/podkop-plus/web", filepath.Join(npkDir, "web"))
	
	// Binary would be cross-compiled for RouterOS architecture
	// cp podkop-plus-${arch} /flash/podkop-plus.npk/bin/podkop-plus
	
	// Create package.xml
	packageXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package>
    <name>podkop-plus</name>
    <version>1.0.0</version>
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
    <binary path="bin/podkop-plus"/>
    <config file="config.yaml"/>
</package>
`, p.arch)
	
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
	exec.Command("/system", "script", "run", "podkop-plus-set-state", "state="+stateStr).Run()
}

// logToRouterOS logs to RouterOS log
func (p *Platform) logToRouterOS(level, msg string) {
	exec.Command("/log", "print", "where", "topics~podkop-plus", "message="+msg).Run()
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
	cmd := exec.Command("docker", "build", "-t", "podkop-plus:"+version, ".")
	cmd.Dir = "/flash/podkop-plus.npk"
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
:global podkopPlusState "starting"
:log info "Starting HydraVPN for Router"

/system script run podkop-plus-health

# Start the service
/container start podkop-plus

:global podkopPlusState "running"
:log info "HydraVPN for Router started"
`
}

func stopScript() string {
	return `# HydraVPN for Router stop script
:global podkopPlusState "stopping"
:log info "Stopping HydraVPN for Router"

/container stop podkop-plus

:global podkopPlusState "stopped"
:log info "HydraVPN for Router stopped"
`
}

func reloadScript() string {
	return `# HydraVPN for Router reload script
:log info "Reloading HydraVPN for Router configuration"

/container exec podkop-plus podkop-plus reload -c /etc/podkop-plus/config.yaml

:log info "HydraVPN for Router reloaded"
`
}

func statusScript() string {
	return `# HydraVPN for Router status script
:global podkopPlusState

:put "HydraVPN for Router State: \$podkopPlusState"
:put "Container Status: [/container get podkop-plus status]"

/container exec podkop-plus podkop-plus status
`
}

func installScript() string {
	return `# HydraVPN for Router install script
:log info "Installing HydraVPN for Router"

# Create container
/container add name=podkop-plus \
    image=podkop-plus:latest \
    interface=veth1 \
    mounts=podkop-plus-config:/etc/podkop-plus,podkop-plus-runtime:/var/run/podkop-plus \
    dns=77.88.8.8,77.88.8.1 \
    envlist="ROUTEROS_CONFIG" \
    workdir=/ \
    entrypoint=/entrypoint.sh \
    cmd="" \
    root-dir=/flash/podkop-plus \
    logging=yes

# Add mount points
/container mounts add name=podkop-plus-config src=/flash/podkop-plus dst=/etc/podkop-plus
/container mounts add name=podkop-plus-runtime src=/flash/podkop-plus/runtime dst=/var/run/podkop-plus

# Add scheduler
/system script run podkop-plus-scheduler

:log info "HydraVPN for Router installed"
`
}

func uninstallScript() string {
	return `# HydraVPN for Router uninstall script
:log info "Uninstalling HydraVPN for Router"

# Stop and remove container
/container stop podkop-plus
/container remove podkop-plus

# Remove mounts
/container mounts remove [find name=podkop-plus-config]
/container mounts remove [find name=podkop-plus-runtime]

# Remove scheduler
/system scheduler remove [find name~"podkop-plus"]

# Remove files
/file remove [find name~"podkop-plus"]

:log info "HydraVPN for Router uninstalled"
`
}

func wanUpScript() string {
	return `# HydraVPN for Router WAN up script
:local wanInterface [/ip route get [find dst-address=0.0.0.0/0] gateway]
:if (\$wanInterface != "") do={
    :log info "WAN interface \$wanInterface up, reloading HydraVPN for Router"
    /system script run podkop-plus-reload
}
`
}

func configScript() string {
	return `# HydraVPN for Router config script
# This script generates RouterOS configuration from HydraVPN for Router config

:local configFile "/flash/podkop-plus/config.yaml"
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
:global podkopPlusState

:if (\$podkopPlusState = "running") do={
    :local containerStatus [/container get podkop-plus status]
    :if (\$containerStatus != "running") do={
        :log error "HydraVPN for Router container not running, restarting"
        /system script run podkop-plus-start
    }
}
`
}

