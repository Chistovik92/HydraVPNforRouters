package keenetic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
)

// Platform implements the KeeneticOS platform integration
type Platform struct {
	engine     *core.Engine
	config     *config.Config
	entware    bool
	knap       bool
	webUIPort  int
}

// NewPlatform creates a new KeeneticOS platform
func NewPlatform(cfg *config.Config) *Platform {
	return &Platform{
		config:    cfg,
		entware:   fileExists("/opt/bin/opkg"),
		knap:      fileExists("/usr/bin/knap"),
		webUIPort: 8080,
	}
}

// Initialize initializes the platform
func (p *Platform) Initialize(ctx context.Context) error {
	// Create directory structure
	dirs := []string{
		"/opt/etc/podkop-plus",
		"/opt/etc/podkop-plus/sing-box",
		"/opt/etc/podkop-plus/zapret",
		"/opt/etc/podkop-plus/byedpi",
		"/opt/var/run/podkop-plus",
		"/opt/var/run/podkop-plus/sing-box",
		"/opt/var/run/podkop-plus/zapret",
		"/opt/var/run/podkop-plus/byedpi",
		"/opt/tmp/podkop-plus",
		"/opt/share/www/podkop-plus",
	}
	
	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}
	
	// Write default config if not exists
	configPath := "/opt/etc/podkop-plus/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}
	
	// Create init script for Entware
	if p.entware {
		if err := p.createEntwareInitScript(); err != nil {
			return fmt.Errorf("create Entware init script: %w", err)
		}
	}
	
	// Create KNP package descriptor
	if p.knap {
		if err := p.createKNPDdescriptor(); err != nil {
			return fmt.Errorf("create KNP descriptor: %w", err)
		}
	}
	
	// Create ndm (Keenetic NDMS) hook scripts
	if err := p.createNDMHooks(); err != nil {
		return fmt.Errorf("create NDM hooks: %w", err)
	}
	
	return nil
}

// Start starts the platform services
func (p *Platform) Start(ctx context.Context) error {
	engine, err := core.NewEngine(core.EngineOptions{
		Config: p.config,
		OnStateChange: func(state core.EngineState) {
			p.updateNDMState(state)
		},
		OnLog: func(level, msg string) {
			p.logToNDM(level, msg)
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

// createEntwareInitScript creates the Entware init script
func (p *Platform) createEntwareInitScript() error {
	initScript := `#!/bin/sh

# Entware init script for HydraVPN for Router
# Place in /opt/etc/init.d/S99podkop-plus

NAME=podkop-plus
DAEMON=/opt/bin/podkop-plus
CONFIG=/opt/etc/podkop-plus/config.yaml
PIDFILE=/opt/var/run/podkop-plus.pid
LOGFILE=/opt/var/log/podkop-plus.log

start() {
    echo "Starting $NAME..."
    if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
        echo "$NAME already running"
        return 1
    fi
    
    $DAEMON start -c $CONFIG >> $LOGFILE 2>&1 &
    echo $! > $PIDFILE
    echo "$NAME started"
}

stop() {
    echo "Stopping $NAME..."
    if [ ! -f $PIDFILE ] || ! kill -0 $(cat $PIDFILE) 2>/dev/null; then
        echo "$NAME not running"
        return 1
    fi
    
    kill $(cat $PIDFILE)
    rm -f $PIDFILE
    echo "$NAME stopped"
}

reload() {
    echo "Reloading $NAME..."
    $DAEMON reload -c $CONFIG
}

status() {
    $DAEMON status
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        start
        ;;
    reload)
        reload
        ;;
    status)
        status
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|reload|status}"
        exit 1
        ;;
esac

exit 0
`
	return os.WriteFile("/opt/etc/init.d/S99podkop-plus", []byte(initScript), 0755)
}

// createKNPDdescriptor creates the KNP package descriptor
func (p *Platform) createKNPDdescriptor() error {
	descriptor := `{
  "name": "podkop-plus",
  "version": "1.0.0",
  "title": "HydraVPN for Router",
  "description": "Multi-platform DPI bypass solution for KeeneticOS",
  "author": "Chistovik92",
  "license": "GPL-3.0",
  "homepage": "https://github.com/Chistovik92/hydravpn-router",
  "category": "network",
  "dependencies": [
    "sing-box",
    "nftables",
    "iptables",
    "curl",
    "ca-certificates"
  ],
  "conflicts": [
    "https-dns-proxy",
    "nextdns"
  ],
  "architectures": [
    "mipsel",
    "mips",
    "armv7",
    "aarch64"
  ],
  "min_firmware": "3.7",
  "scripts": {
    "pre_install": "scripts/pre-install.sh",
    "post_install": "scripts/post-install.sh",
    "pre_uninstall": "scripts/pre-uninstall.sh",
    "post_uninstall": "scripts/post-uninstall.sh",
    "start": "scripts/start.sh",
    "stop": "scripts/stop.sh",
    "restart": "scripts/restart.sh"
  },
  "web_ui": {
    "enabled": true,
    "port": 8080,
    "path": "/podkop-plus",
    "title": "HydraVPN for Router"
  },
  "config": {
    "file": "/opt/etc/podkop-plus/config.yaml",
    "web_editor": true
  },
  "hooks": {
    "wan_up": "hooks/wan-up.sh",
    "wan_down": "hooks/wan-down.sh",
    "config_changed": "hooks/config-changed.sh"
  }
}
`
	return os.WriteFile("/opt/etc/podkop-plus/knp.json", []byte(descriptor), 0644)
}

// createNDMHooks creates NDMS hook scripts
func (p *Platform) createNDMHooks() error {
	hooksDir := "/opt/etc/ndm"
	os.MkdirAll(filepath.Join(hooksDir, "wan.d"), 0755)
	os.MkdirAll(filepath.Join(hooksDir, "config.d"), 0755)
	
	// WAN up hook
	wanUp := `#!/bin/sh
# NDM WAN up hook for HydraVPN for Router
# Place in /opt/etc/ndm/wan.d/podkop-plus

if [ "$1" = "up" ]; then
    logger -t podkop-plus "WAN interface $2 up, reloading HydraVPN for Router"
    /opt/bin/podkop-plus reload -c /opt/etc/podkop-plus/config.yaml
fi
`
	os.WriteFile(filepath.Join(hooksDir, "wan.d", "podkop-plus"), []byte(wanUp), 0755)
	
	// Config changed hook
	configChanged := `#!/bin/sh
# NDM config changed hook for HydraVPN for Router
# Place in /opt/etc/ndm/config.d/podkop-plus

logger -t podkop-plus "Configuration changed, reloading HydraVPN for Router"
/opt/bin/podkop-plus reload -c /opt/etc/podkop-plus/config.yaml
`
	os.WriteFile(filepath.Join(hooksDir, "config.d", "podkop-plus"), []byte(configChanged), 0755)
	
	return nil
}

// updateNDMState updates NDMS with engine state
func (p *Platform) updateNDMState(state core.EngineState) {
	// Could use ndmq to update component status
	stateStr := string(state)
	exec.Command("ndmq", "-p", "component set podkop-plus state "+stateStr).Run()
}

// logToNDM logs to NDMS log
func (p *Platform) logToNDM(level, msg string) {
	priority := "info"
	switch level {
	case "error":
		priority = "err"
	case "warn":
		priority = "warning"
	case "debug":
		priority = "debug"
	}
	exec.Command("logger", "-t", "podkop-plus", "-p", "daemon."+priority, msg).Run()
}

// GenerateKNP generates KeeneticOS KNP package
func (p *Platform) GenerateKNP(version, outputDir string) error {
	pkgDir := filepath.Join(outputDir, "podkop-plus-"+version)
	os.MkdirAll(filepath.Join(pkgDir, "scripts"), 0755)
	os.MkdirAll(filepath.Join(pkgDir, "hooks"), 0755)
	os.MkdirAll(filepath.Join(pkgDir, "web"), 0755)
	
	// Copy files
	copyDir("/opt/etc/podkop-plus", filepath.Join(pkgDir, "etc/podkop-plus"))
	copyDir("/opt/etc/init.d", filepath.Join(pkgDir, "etc/init.d"))
	copyDir("/opt/etc/ndm", filepath.Join(pkgDir, "etc/ndm"))
	copyDir("/opt/bin", filepath.Join(pkgDir, "opt/bin"))
	copyDir("/opt/share/www/podkop-plus", filepath.Join(pkgDir, "opt/share/www/podkop-plus"))
	
	// Write scripts
	scripts := map[string]string{
		"pre-install.sh":  preInstallScript(),
		"post-install.sh": postInstallScript(),
		"pre-uninstall.sh": preUninstallScript(),
		"post-uninstall.sh": postUninstallScript(),
		"start.sh":        startScript(),
		"stop.sh":         stopScript(),
		"restart.sh":      restartScript(),
	}
	
	for name, content := range scripts {
		os.WriteFile(filepath.Join(pkgDir, "scripts", name), []byte(content), 0755)
	}
	
	// Write hooks
	hooks := map[string]string{
		"wan-up.sh":       wanUpHook(),
		"wan-down.sh":     wanDownHook(),
		"config-changed.sh": configChangedHook(),
	}
	
	for name, content := range hooks {
		os.WriteFile(filepath.Join(pkgDir, "hooks", name), []byte(content), 0755)
	}
	
	// Copy knp.json
	os.WriteFile(filepath.Join(pkgDir, "knp.json"), []byte(knpDescriptor(version)), 0644)
	
	// Create KNP archive
	cmd := exec.Command("tar", "-czf", filepath.Join(outputDir, "podkop-plus-"+version+".knp"), "-C", pkgDir, ".")
	return cmd.Run()
}

// GetSystemInfo returns KeeneticOS system information
func (p *Platform) GetSystemInfo() map[string]interface{} {
	info := map[string]interface{}{
		"platform":   "keenetic",
		"entware":    p.entware,
		"knap":       p.knap,
		"web_ui_port": p.webUIPort,
	}
	
	// Get KeeneticOS version
	if out, err := exec.Command("ndmq", "-p", "show version").Output(); err == nil {
		var versionInfo map[string]interface{}
		json.Unmarshal(out, &versionInfo)
		info["keenetic_version"] = versionInfo
	}
	
	// Get model
	if out, err := exec.Command("ndmq", "-p", "show hardware").Output(); err == nil {
		var hwInfo map[string]interface{}
		json.Unmarshal(out, &hwInfo)
		info["hardware"] = hwInfo
	}
	
	// Get interfaces
	if out, err := exec.Command("ndmq", "-p", "show interface").Output(); err == nil {
		var ifaces []map[string]interface{}
		json.Unmarshal(out, &ifaces)
		info["interfaces"] = ifaces
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

// KNP scripts
func preInstallScript() string {
	return `#!/bin/sh
# Pre-install script for HydraVPN for Router KNP package
exit 0
`
}

func postInstallScript() string {
	return `#!/bin/sh
# Post-install script for HydraVPN for Router KNP package
/opt/etc/init.d/S99podkop-plus start
ndmq -p "component set podkop-plus state running"
exit 0
`
}

func preUninstallScript() string {
	return `#!/bin/sh
# Pre-uninstall script for HydraVPN for Router KNP package
/opt/etc/init.d/S99podkop-plus stop
ndmq -p "component set podkop-plus state stopped"
exit 0
`
}

func postUninstallScript() string {
	return `#!/bin/sh
# Post-uninstall script for HydraVPN for Router KNP package
rm -rf /opt/etc/podkop-plus /opt/var/run/podkop-plus /opt/var/log/podkop-plus.log
exit 0
`
}

func startScript() string {
	return `#!/bin/sh
# Start script for HydraVPN for Router
/opt/etc/init.d/S99podkop-plus start
`
}

func stopScript() string {
	return `#!/bin/sh
# Stop script for HydraVPN for Router
/opt/etc/init.d/S99podkop-plus stop
`
}

func restartScript() string {
	return `#!/bin/sh
# Restart script for HydraVPN for Router
/opt/etc/init.d/S99podkop-plus restart
`
}

func wanUpHook() string {
	return `#!/bin/sh
# WAN up hook for HydraVPN for Router
if [ "$1" = "up" ]; then
    logger -t podkop-plus "WAN interface $2 up, reloading"
    /opt/bin/podkop-plus reload -c /opt/etc/podkop-plus/config.yaml
fi
`
}

func wanDownHook() string {
	return `#!/bin/sh
# WAN down hook for HydraVPN for Router
if [ "$1" = "down" ]; then
    logger -t podkop-plus "WAN interface $2 down"
fi
`
}

func configChangedHook() string {
	return `#!/bin/sh
# Config changed hook for HydraVPN for Router
logger -t podkop-plus "Configuration changed, reloading"
/opt/bin/podkop-plus reload -c /opt/etc/podkop-plus/config.yaml
`
}

func knpDescriptor(version string) string {
	return fmt.Sprintf(`{
  "name": "podkop-plus",
  "version": "%s",
  "title": "HydraVPN for Router",
  "description": "Multi-platform DPI bypass solution for KeeneticOS",
  "author": "Chistovik92",
  "license": "GPL-3.0",
  "homepage": "https://github.com/Chistovik92/hydravpn-router",
  "category": "network",
  "dependencies": ["sing-box", "nftables", "iptables", "curl", "ca-certificates"],
  "conflicts": ["https-dns-proxy", "nextdns"],
  "architectures": ["mipsel", "mips", "armv7", "aarch64"],
  "min_firmware": "3.7",
  "scripts": {
    "pre_install": "scripts/pre-install.sh",
    "post_install": "scripts/post-install.sh",
    "pre_uninstall": "scripts/pre-uninstall.sh",
    "post_uninstall": "scripts/post-uninstall.sh",
    "start": "scripts/start.sh",
    "stop": "scripts/stop.sh",
    "restart": "scripts/restart.sh"
  },
  "web_ui": {
    "enabled": true,
    "port": 8080,
    "path": "/podkop-plus",
    "title": "HydraVPN for Router"
  },
  "config": {
    "file": "/opt/etc/podkop-plus/config.yaml",
    "web_editor": true
  },
  "hooks": {
    "wan_up": "hooks/wan-up.sh",
    "wan_down": "hooks/wan-down.sh",
    "config_changed": "hooks/config-changed.sh"
  }
}`, version)
}

