package openwrt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Platform implements the OpenWRT platform integration
type Platform struct {
	engine       *core.Engine
	config       *config.Config
	ucodeRuntime bool
	luciEnabled  bool
}

// NewPlatform creates a new OpenWRT platform
func NewPlatform(cfg *config.Config) *Platform {
	return &Platform{
		config:       cfg,
		ucodeRuntime: fileExists("/usr/bin/ucode"),
		luciEnabled:  fileExists("/usr/lib/lua/luci"),
	}
}

// Initialize initializes the platform
func (p *Platform) Initialize(ctx context.Context) error {
	// Create directory structure
	dirs := []string{
		"/etc/hydravpn-router",
		"/etc/hydravpn-router/sing-box",
		"/etc/hydravpn-router/zapret",
		"/etc/hydravpn-router/byedpi",
		"/var/run/hydravpn-router",
		"/var/run/hydravpn-router/sing-box",
		"/var/run/hydravpn-router/zapret",
		"/var/run/hydravpn-router/byedpi",
		"/tmp/hydravpn-router",
		"/www/luci-static/resources/view/hydravpn-router",
	}

	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}

	// Write default config if not exists
	configPath := "/etc/hydravpn-router/config.yaml"
	if !fileExists(configPath) {
		if err := p.config.SaveToFile(configPath); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	// Create init script
	if err := p.createInitScript(); err != nil {
		return fmt.Errorf("create init script: %w", err)
	}

	// Create UCI config
	if err := p.createUCIConfig(); err != nil {
		return fmt.Errorf("create UCI config: %w", err)
	}

	return nil
}

// Start starts the platform services
func (p *Platform) Start(ctx context.Context) error {
	// Create engine
	engine, err := core.NewEngine(core.EngineOptions{
		Config: p.config,
		OnStateChange: func(state core.EngineState) {
			p.updateUCIState(state)
		},
		OnLog: func(level, msg string) {
			p.logToSyslog(level, msg)
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

// createInitScript creates the procd init script
func (p *Platform) createInitScript() error {
	return os.WriteFile("/etc/init.d/hydravpn-router", []byte(InitScript), 0755)
}

// InitScript is the procd init script. procd sends SIGTERM on stop and
// "reload" sends SIGHUP, which the daemon handles by re-reading its config.
const InitScript = `#!/bin/sh /etc/rc.common

START=99
STOP=10
USE_PROCD=1

PROG=/usr/bin/hydravpn-router
CONFIG_FILE=/etc/hydravpn-router/config.yaml

start_service() {
    procd_open_instance
    procd_set_param command $PROG start -c $CONFIG_FILE
    procd_set_param respawn 3600 5 5
    procd_set_param term_timeout 20
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param file $CONFIG_FILE
    procd_close_instance
}

reload_service() {
    $PROG reload -c $CONFIG_FILE
}
`

// createUCIConfig creates the UCI configuration file. An existing file is
// left untouched so user settings survive re-initialisation.
func (p *Platform) createUCIConfig() error {
	if fileExists("/etc/config/hydravpn-router") {
		return nil
	}
	uciConfig := `config hydravpn-router 'main'
	option enabled '1'
	option config_file '/etc/hydravpn-router/config.yaml'
	option log_level 'warn'

config section 'subscription'
	option label 'Subscription'
	option enabled '1'
	option action 'connection'

config settings 'dns'
	option dns_type 'udp'
	list dns_server '77.88.8.8'
	list dns_server '77.88.8.1'
	option dns_strategy 'prefer_ipv4'
	option dns_check_interval '10s'
	option dns_recovery_check_interval '60s'

config settings 'firewall'
	option enabled '1'
	list source_interface 'br-lan'
	option mark_value '0x08000000'
	option table_name 'hydravpn'
	option chain_name 'proxy_pre'

config settings 'singbox'
	option enabled '1'
	option binary '/usr/bin/sing-box'
	option config_dir '/etc/hydravpn-router/sing-box'

config settings 'zapret'
	option enabled '0'
	option type 'zapret2'
	option binary '/opt/zapret2/nfq2/nfqws2'
	option queue_num '4000'

config settings 'byedpi'
	option enabled '0'
	option binary '/usr/bin/ciadpi'
	option listen_address '127.0.0.1'
	option port '1080'
	option options '-o 2 -d 2'

config settings 'subscription'
	option enabled '1'
	option update_interval '1h'
	option cache_dir '/etc/hydravpn-router/subscription-cache'

config settings 'ui'
	option enabled '1'
	option luci_enabled '1'
	option port '8080'
	option bind_address '0.0.0.0'
`
	return os.WriteFile("/etc/config/hydravpn-router", []byte(uciConfig), 0644)
}

// updateUCIState updates UCI with engine state
func (p *Platform) updateUCIState(state core.EngineState) {
	stateStr := string(state)
	exec.Command("uci", "set", "hydravpn-router.main.state="+stateStr).Run()
	exec.Command("uci", "commit", "hydravpn-router").Run()
}

// logToSyslog logs to syslog
func (p *Platform) logToSyslog(level, msg string) {
	priority := "info"
	switch level {
	case "error":
		priority = "err"
	case "warn":
		priority = "warning"
	case "debug":
		priority = "debug"
	}
	exec.Command("logger", "-t", "hydravpn-router", "-p", "daemon."+priority, msg).Run()
}

// GenerateIPK generates an OpenWRT IPK package containing only this
// program's files: the binary at binaryPath, the init script and the
// default configuration. arch is the OpenWrt package architecture
// (e.g. mipsel_24kc) matching the binary.
func (p *Platform) GenerateIPK(pkgVersion, arch, binaryPath, outputDir string) error {
	pkgDir := filepath.Join(outputDir, "pkg")
	for _, dir := range []string{"CONTROL", "usr/bin", "etc/init.d", "etc/hydravpn-router"} {
		if err := os.MkdirAll(filepath.Join(pkgDir, dir), 0755); err != nil {
			return err
		}
	}

	if err := copyFile(binaryPath, filepath.Join(pkgDir, "usr/bin/hydravpn-router"), 0755); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "etc/init.d/hydravpn-router"), []byte(InitScript), 0755); err != nil {
		return err
	}
	if err := config.DefaultConfig().SaveToFile(filepath.Join(pkgDir, "etc/hydravpn-router/config.yaml")); err != nil {
		return err
	}

	// Write control file
	control := fmt.Sprintf(`Package: hydravpn-router
Version: %s
Depends: libc, ca-bundle, kmod-nft-tproxy, kmod-nft-queue, nftables, ip-full, sing-box
Conflicts: https-dns-proxy, nextdns, luci-app-passwall, luci-app-passwall2
License: GPL-3.0-or-later
Section: net
URL: https://github.com/Chistovik92/hydravpn-router
Maintainer: Chistovik92 <chistovik92@users.noreply.github.com>
Architecture: %s
Description: HydraVPN for Router - Multi-platform DPI bypass solution with sing-box, zapret, and ByeDPI support
`, pkgVersion, arch)

	if err := os.WriteFile(filepath.Join(pkgDir, "CONTROL/control"), []byte(control), 0644); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(pkgDir, "CONTROL/conffiles"), []byte("/etc/hydravpn-router/config.yaml\n"), 0644)

	// Write postinst
	postinst := `#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/hydravpn-router enable
/etc/init.d/hydravpn-router start
`
	os.WriteFile(filepath.Join(pkgDir, "CONTROL/postinst"), []byte(postinst), 0755)

	// Write prerm
	prerm := `#!/bin/sh
/etc/init.d/hydravpn-router stop
/etc/init.d/hydravpn-router disable
`
	os.WriteFile(filepath.Join(pkgDir, "CONTROL/prerm"), []byte(prerm), 0755)

	// Build IPK
	cmd := exec.Command("ipkg-build", "-o", "root", "-g", "root", pkgDir, outputDir)
	cmd.Dir = outputDir
	return cmd.Run()
}

// GenerateAPK generates OpenWRT APK package (for 24.10+)
func (p *Platform) GenerateAPK(version, outputDir string) error {
	// Similar to IPK but using apk tools
	return nil
}

// InstallLUCI installs the LuCI web interface
func (p *Platform) InstallLUCI() error {
	// Copy LuCI files
	luciDir := "/www/luci-static/resources/view/hydravpn-router"
	os.MkdirAll(luciDir, 0755)

	// Generate main.js from Vue/TypeScript source
	// This would be built from the web/luci directory
	return nil
}

// GetSystemInfo returns OpenWRT system information
func (p *Platform) GetSystemInfo() map[string]interface{} {
	info := map[string]interface{}{
		"platform":      "openwrt",
		"ucode_runtime": p.ucodeRuntime,
		"luci_enabled":  p.luciEnabled,
	}

	// Get OpenWRT version
	if out, err := exec.Command("cat", "/etc/openwrt_release").Output(); err == nil {
		info["openwrt_release"] = string(out)
	}

	// Get kernel version
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info["kernel"] = strings.TrimSpace(string(out))
	}

	// Get architecture
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		info["arch"] = strings.TrimSpace(string(out))
	}

	// Get memory info
	if out, err := exec.Command("cat", "/proc/meminfo").Output(); err == nil {
		info["meminfo"] = string(out)
	}

	return info
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// UCIConfig represents UCI configuration structure
type UCIConfig struct {
	Main     UCISection            `json:"main"`
	Sections map[string]UCISection `json:"sections"`
	Settings map[string]UCISection `json:"settings"`
}

type UCISection struct {
	Type    string              `json:"type"`
	Options map[string]string   `json:"options"`
	Lists   map[string][]string `json:"lists"`
}

// ToUCI converts HydraVPN config to UCI format
func (p *Platform) ToUCI(cfg *config.Config) *UCIConfig {
	uci := &UCIConfig{
		Main: UCISection{
			Type: "hydravpn-router",
			Options: map[string]string{
				"enabled":        "1",
				"config_version": cfg.Settings.ConfigVersion,
				"version":        version.Version,
				"config_file":    cfg.Settings.ConfigPath,
				"log_level":      cfg.Settings.LogLevel,
			},
		},
		Sections: make(map[string]UCISection),
		Settings: make(map[string]UCISection),
	}

	// Add sections
	for _, section := range cfg.Sections {
		uci.Sections[section.Name] = UCISection{
			Type: "section",
			Options: map[string]string{
				"label":   section.Label,
				"enabled": boolToStr(section.Enabled),
				"action":  string(section.Action),
			},
			Lists: map[string][]string{
				"selector_proxy_links": section.SelectorProxyLinks,
				"community_lists":      section.CommunityLists,
				"rule_set":             section.RuleSet,
			},
		}
	}

	// Add settings
	uci.Settings["dns"] = UCISection{
		Type: "settings",
		Options: map[string]string{
			"dns_type":                    cfg.Settings.DNSType,
			"dns_strategy":                string(cfg.Settings.DNSStrategy),
			"dns_check_interval":          cfg.Settings.DNSCheckInterval.String(),
			"dns_recovery_check_interval": cfg.Settings.DNSRecoveryCheckInterval.String(),
			"dns_check_timeout":           cfg.Settings.DNSCheckTimeout.String(),
			"dns_rewrite_ttl":             fmt.Sprintf("%d", cfg.Settings.DNSRewriteTTL),
		},
		Lists: map[string][]string{
			"dns_server":           cfg.Settings.DNSServers,
			"bootstrap_dns_server": cfg.Settings.BootstrapDNSServers,
		},
	}

	uci.Settings["firewall"] = UCISection{
		Type: "settings",
		Options: map[string]string{
			"enabled":    "1",
			"mark_value": "0x08000000",
			"table_name": "hydravpn",
			"chain_name": "proxy_pre",
		},
		Lists: map[string][]string{
			"source_interface": cfg.Settings.SourceNetworkInterfaces,
		},
	}

	return uci
}

func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// FromUCI converts UCI config to HydraVPN config
func (p *Platform) FromUCI(uci *UCIConfig) *config.Config {
	cfg := config.DefaultConfig()

	if v, ok := uci.Main.Options["config_version"]; ok {
		cfg.Settings.ConfigVersion = v
	}
	if v, ok := uci.Main.Options["config_file"]; ok {
		cfg.Settings.ConfigPath = v
	}
	if v, ok := uci.Main.Options["log_level"]; ok {
		cfg.Settings.LogLevel = v
	}

	if dns, ok := uci.Settings["dns"]; ok {
		if v, ok := dns.Options["dns_type"]; ok {
			cfg.Settings.DNSType = v
		}
		if v, ok := dns.Options["dns_strategy"]; ok {
			cfg.Settings.DNSStrategy = config.DNSStrategy(v)
		}
		cfg.Settings.DNSServers = dns.Lists["dns_server"]
		cfg.Settings.BootstrapDNSServers = dns.Lists["bootstrap_dns_server"]
	}

	if fw, ok := uci.Settings["firewall"]; ok {
		cfg.Settings.SourceNetworkInterfaces = fw.Lists["source_interface"]
	}

	return cfg
}

// MarshalJSON implements json.Marshaler
func (u *UCIConfig) MarshalJSON() ([]byte, error) {
	type Alias UCIConfig
	return json.Marshal((*Alias)(u))
}
