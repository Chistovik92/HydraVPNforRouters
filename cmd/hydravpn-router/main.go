package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/internal/diagnostics"
	"github.com/Chistovik92/hydravpn-router/internal/firewall"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
	"github.com/Chistovik92/hydravpn-router/internal/mgmt"
	"github.com/Chistovik92/hydravpn-router/internal/process"
	"github.com/Chistovik92/hydravpn-router/internal/selftest"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"
)

// Globals are flags shared by all commands.
type Globals struct {
	RuntimeDir string `help:"Directory for the PID and status files." default:"${runtime_dir}" env:"HYDRAVPN_RUNTIME_DIR" type:"path"`
}

func (g *Globals) pidFile() string    { return filepath.Join(g.RuntimeDir, "hydravpn-router.pid") }
func (g *Globals) statusFile() string { return filepath.Join(g.RuntimeDir, "status.json") }

var CLI struct {
	Globals

	Start     StartCmd     `cmd:"" help:"Start HydraVPN for Router service (runs in the foreground)"`
	Stop      StopCmd      `cmd:"" help:"Stop the running service"`
	Reload    ReloadCmd    `cmd:"" help:"Reload configuration of the running service"`
	Status    StatusCmd    `cmd:"" help:"Show service status"`
	Config    ConfigCmd    `cmd:"" help:"Show configuration"`
	Version   VersionCmd   `cmd:"" help:"Show version information"`
	Providers ProvidersCmd `cmd:"" help:"Show providers status"`
	DNS       DNSCmd       `cmd:"" name:"dns" help:"Show DNS status"`
	Firewall  FirewallCmd  `cmd:"" help:"Show firewall status"`
	Subs      SubsCmd      `cmd:"" help:"Show subscriptions status"`
	Check     CheckCmd     `cmd:"" help:"Run diagnostics"`
	APIToken  APITokenCmd  `cmd:"" name:"api-token" help:"Print the management API token"`
	Pair      PairCmd      `cmd:"" help:"Print the link that adds this router to the HydraVPN app"`
	Selftest  SelftestCmd  `cmd:"" help:"Check this router: nft --check, sing-box check, kernel tproxy support"`
}

type StartCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
	Daemon     bool   `short:"d" help:"Deprecated, ignored: the service always runs in the foreground under procd/systemd/rc."`
}

func (c *StartCmd) Run(g *Globals) error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	newJournal(cfg.Settings)
	if err := process.KillChildrenOnExit(); err != nil {
		logLine("warn", "cannot tie child processes to this process: "+err.Error())
	}
	defer journal.Close()
	if _, err := os.Stat(c.ConfigFile); os.IsNotExist(err) {
		logLine("warn", "Config "+c.ConfigFile+" not found, running with defaults (no sections: nothing is routed)")
	}
	if len(cfg.Sections) == 0 {
		logLine("warn", "No sections configured: all traffic goes direct. Add sections and subscription_urls to the config")
	}

	if pid, ok := readPID(g.pidFile()); ok && processAlive(pid) && pid != os.Getpid() {
		return fmt.Errorf("already running (PID %d)", pid)
	}

	engine, err := core.NewEngine(core.EngineOptions{
		Config: cfg,
		OnStateChange: func(state core.EngineState) {
			logLine("info", "State changed: "+string(state))
		},
		OnLog: logLine,
	})
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	if err := os.MkdirAll(g.RuntimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	if err := os.WriteFile(g.pidFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(g.pidFile())
	defer os.Remove(g.statusFile())

	// Register signals before starting so an early SIGTERM still cleans up.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	if err := engine.Start(); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	defer engine.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go writeStatusLoop(ctx, engine, g.statusFile())

	api, err := mgmt.New(mgmt.Options{Engine: engine, Logger: journal, ConfigFile: c.ConfigFile, RuntimeDir: g.RuntimeDir})
	if err != nil {
		logLine("error", "management API disabled: "+err.Error())
	} else if api != nil {
		if err := api.Start(); err != nil {
			logLine("error", err.Error())
		} else {
			defer api.Stop()
		}
	}

	for sig := range sigCh {
		if sig != syscall.SIGHUP {
			logLine("info", "Received "+sig.String()+", shutting down")
			return nil
		}
		logLine("info", "Received SIGHUP, reloading "+c.ConfigFile)
		newCfg, err := config.LoadFromFile(c.ConfigFile)
		if err != nil {
			logLine("error", "Reload failed, keeping current config: "+err.Error())
			continue
		}
		if err := engine.Reload(newCfg); err != nil {
			logLine("error", "Reload finished with errors: "+err.Error())
		}
		writeStatus(engine, g.statusFile())
	}
	return nil
}

// journal is the application log; replaced by newJournal once the config is read.
var journal, _ = logx.New(logx.Options{Level: "info"})

func logLine(level, msg string) { journal.Log(level, msg) }

func newJournal(s config.Settings) {
	l, err := logx.New(logx.Options{
		Level:   s.AppLogLevel,
		File:    s.LogFile,
		MaxSize: int64(s.LogMaxSizeMB) << 20,
		Keep:    s.LogKeep,
	})
	if err != nil {
		journal.Log("warn", "log file disabled: "+err.Error())
		return
	}
	journal = l
}

// writeStatusLoop publishes the engine status for the CLI status commands.
func writeStatusLoop(ctx context.Context, engine *core.Engine, path string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		writeStatus(engine, path)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func writeStatus(engine *core.Engine, path string) {
	status := engine.GetStatus()
	status["pid"] = os.Getpid()
	status["updated_at"] = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, path)
	}
}

func readPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, err == nil && pid > 0
}

// runningPID returns the PID of the running service.
func runningPID(g *Globals) (int, error) {
	pid, ok := readPID(g.pidFile())
	if !ok || !processAlive(pid) {
		return 0, errors.New("service is not running")
	}
	return pid, nil
}

type StopCmd struct {
	Timeout time.Duration `help:"How long to wait for shutdown." default:"30s"`
}

func (c *StopCmd) Run(g *Globals) error {
	pid, err := runningPID(g)
	if err != nil {
		return err
	}
	if err := terminateProcess(pid); err != nil {
		return fmt.Errorf("signal PID %d: %w", pid, err)
	}
	deadline := time.Now().Add(c.Timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			fmt.Println("HydraVPN for Router stopped")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("PID %d did not stop within %s", pid, c.Timeout)
}

type ReloadCmd struct {
	ConfigFile string `short:"c" help:"Configuration file to validate before reloading" default:"${config_file}"`
}

func (c *ReloadCmd) Run(g *Globals) error {
	// Validate first so a broken file is reported here, not only in the log.
	if _, err := config.LoadFromFile(c.ConfigFile); err != nil {
		return fmt.Errorf("config is invalid, not reloading: %w", err)
	}
	pid, err := runningPID(g)
	if err != nil {
		return err
	}
	if err := reloadProcess(pid); err != nil {
		return fmt.Errorf("signal PID %d: %w", pid, err)
	}
	fmt.Println("Reload requested")
	return nil
}

// readStatus loads the status file written by the running service.
func readStatus(g *Globals) (map[string]interface{}, error) {
	if _, err := runningPID(g); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(g.statusFile())
	if err != nil {
		return nil, fmt.Errorf("status not available yet: %w", err)
	}
	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	return status, nil
}

func printSection(g *Globals, key, format string) error {
	status, err := readStatus(g)
	if err != nil {
		return err
	}
	var v interface{} = status
	if key != "" {
		v = status[key]
	}
	return printValue(v, format)
}

func printValue(v interface{}, format string) error {
	if format == "json" {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printText(v, "")
	return nil
}

func printText(v interface{}, indent string) {
	m, ok := v.(map[string]interface{})
	if !ok {
		fmt.Printf("%s%v\n", indent, v)
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if sub, ok := m[k].(map[string]interface{}); ok {
			fmt.Printf("%s%s:\n", indent, k)
			printText(sub, indent+"  ")
			continue
		}
		fmt.Printf("%s%s: %v\n", indent, k, m[k])
	}
}

type StatusCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *StatusCmd) Run(g *Globals) error { return printSection(g, "", c.Format) }

type ConfigCmd struct {
	ConfigFile  string `short:"c" help:"Configuration file path" default:"${config_file}"`
	Format      string `short:"f" help:"Output format (json, yaml)" default:"yaml" enum:"json,yaml"`
	ShowSecrets bool   `help:"Print keys, tokens and subscription URLs unmasked."`
}

func (c *ConfigCmd) Run() error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return err
	}
	if !c.ShowSecrets {
		cfg = cfg.Masked()
	}
	if c.Format == "json" {
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	enc.Close()
	fmt.Printf("# HydraVPN for Router configuration (%s)\n%s", c.ConfigFile, buf.String())
	return nil
}

type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("HydraVPN for Router %s\n", version.FullVersion())
	fmt.Printf("Commit: %s\n", version.Commit)
	fmt.Printf("Built: %s\n", version.Date)
	fmt.Printf("Go: %s\n", runtime.Version())
	return nil
}

type ProvidersCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *ProvidersCmd) Run(g *Globals) error { return printSection(g, "providers", c.Format) }

type DNSCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *DNSCmd) Run(g *Globals) error { return printSection(g, "dns", c.Format) }

type FirewallCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *FirewallCmd) Run(g *Globals) error { return printSection(g, "firewall", c.Format) }

type SubsCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *SubsCmd) Run(g *Globals) error { return printSection(g, "subscriptions", c.Format) }

type APITokenCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
}

func (c *APITokenCmd) Run(g *Globals) error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return err
	}
	if cfg.Settings.APIToken != "" {
		fmt.Println(cfg.Settings.APIToken)
		return nil
	}
	data, err := os.ReadFile(mgmt.TokenFile(g.RuntimeDir))
	if err != nil {
		return errors.New("no token yet: set api_listen in the config and start the service")
	}
	fmt.Print(string(data))
	return nil
}

type SelftestCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
	Format     string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
	PrintNFT   bool   `name:"print-nft" help:"Only print the generated nftables ruleset (for nft --check in CI)"`
}

func (c *SelftestCmd) Run() error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return err
	}
	if c.PrintNFT {
		script := firewall.NFTScript(cfg, nil)
		if script == "" {
			return errors.New("nftables rules are not available on " + runtime.GOOS)
		}
		fmt.Print(script)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res := selftest.Run(ctx, cfg, selftest.SystemEnv())
	if c.Format == "json" {
		if err := printValue(res, "json"); err != nil {
			return err
		}
	} else {
		for _, r := range res {
			fmt.Printf("[%-4s] %-16s %s\n", strings.ToUpper(r.Status), r.Name, r.Message)
		}
	}
	if selftest.Failed(res) {
		return errors.New("selftest found problems")
	}
	return nil
}

type PairCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
	Host       string `required:"" help:"Address of the router as the app sees it (LAN IP, VPN IP or host name)"`
}

func (c *PairCmd) Run(g *Globals) error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return err
	}
	token := cfg.Settings.APIToken
	if token == "" {
		data, err := os.ReadFile(mgmt.TokenFile(g.RuntimeDir))
		if err != nil {
			return errors.New("no token yet: set api_listen in the config and start the service")
		}
		token = strings.TrimSpace(string(data))
	}
	uri, err := mgmt.PairURI(cfg.Settings, c.Host, token)
	if err != nil {
		return err
	}
	fmt.Println(uri)
	return nil
}

type CheckCmd struct {
	Check      string `arg:"" optional:"" help:"Check name (${checks}) or 'all'" default:"all"`
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
	Format     string `short:"f" help:"Output format (json, text)" default:"text" enum:"json,text"`
}

func (c *CheckCmd) Run() error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return err
	}
	d := diagnostics.NewDiagnostics(cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := c.Check
	if name == "all" {
		name = "global"
	}
	result, err := d.RunCheck(ctx, name)
	if err != nil {
		return err
	}
	if c.Format == "json" {
		return printValue(result, "json")
	}

	results := []*diagnostics.CheckResult{result}
	if name == "global" {
		if checks, ok := result.Details["checks"].([]*diagnostics.CheckResult); ok {
			results = append(checks, result)
		}
	}
	failed := false
	for _, r := range results {
		fmt.Printf("[%-4s] %-16s %s\n", strings.ToUpper(r.Status), r.Name, r.Message)
		failed = failed || r.Status == "fail"
	}
	if failed {
		return errors.New("some checks failed")
	}
	return nil
}

// Keenetic Entware keeps everything under /opt.
const (
	entwareConfigFile = "/opt/etc/hydravpn-router/config.yaml"
	entwareRuntimeDir = "/opt/var/run/hydravpn-router"
)

func isEntware() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, errSystem := os.Stat(config.DefaultConfigFile)
	_, errEntware := os.Stat(entwareConfigFile)
	return errSystem != nil && errEntware == nil
}

func defaultConfigFile() string {
	if isEntware() {
		return entwareConfigFile
	}
	return config.DefaultConfigFile
}

func defaultRuntimeDir() string {
	switch {
	case isEntware():
		return entwareRuntimeDir
	}
	return config.DefaultRuntimeDir
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("hydravpn-router"),
		kong.Description("HydraVPN for Router "+version.Version+" - Multi-platform DPI bypass solution"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.Vars{
			"config_file": defaultConfigFile(),
			"runtime_dir": defaultRuntimeDir(),
			"checks":      strings.Join(diagnostics.CheckNames(), ", "),
		},
		kong.Bind(&CLI.Globals),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
