package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
	"github.com/alecthomas/kong"
)

var CLI struct {
	Start     StartCmd     `cmd:"" help:"Start HydraVPN for Router service"`
	Stop      StopCmd      `cmd:"" help:"Stop HydraVPN for Router service"`
	Reload    ReloadCmd    `cmd:"" help:"Reload configuration"`
	Status    StatusCmd    `cmd:"" help:"Show service status"`
	Config    ConfigCmd    `cmd:"" help:"Show current configuration"`
	Version   VersionCmd   `cmd:"" help:"Show version information"`
	Providers ProvidersCmd `cmd:"" help:"Show providers status"`
	DNS       DNSCmd       `cmd:"" help:"Show DNS status"`
	Firewall  FirewallCmd  `cmd:"" help:"Show firewall status"`
	Subs      SubsCmd      `cmd:"" help:"Show subscriptions status"`
	Check     CheckCmd     `cmd:"" help:"Run diagnostics"`
}

type StartCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"/etc/hydravpn-router/config.yaml"`
	Daemon     bool   `short:"d" help:"Run as daemon"`
}

func (c *StartCmd) Run() error {
	cfg, err := config.LoadFromFile(c.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	
	engine, err := core.NewEngine(core.EngineOptions{
		Config: cfg,
		OnStateChange: func(state core.EngineState) {
			fmt.Printf("State changed: %s\n", state)
		},
		OnLog: func(level, msg string) {
			fmt.Printf("[%s] %s\n", level, msg)
		},
	})
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}
	
	if err := engine.Start(); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	
	fmt.Println("HydraVPN for Router started")
	
	if c.Daemon {
		// Wait for signal
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("Shutting down...")
		engine.Stop()
	} else {
		// Run in foreground
		select {}
	}
	
	return nil
}

type StopCmd struct{}

func (c *StopCmd) Run() error {
	fmt.Println("Stop command - would connect to running daemon")
	return nil
}

type ReloadCmd struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"/etc/hydravpn-router/config.yaml"`
}

func (c *ReloadCmd) Run() error {
	fmt.Println("Reload command - would connect to running daemon")
	return nil
}

type StatusCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text"`
}

func (c *StatusCmd) Run() error {
	fmt.Println("Status command - would connect to running daemon")
	return nil
}

type ConfigCmd struct {
	Format string `short:"f" help:"Output format (json, yaml)" default:"yaml"`
}

func (c *ConfigCmd) Run() error {
	cfg := config.DefaultConfig()
	
	if c.Format == "json" {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
	} else {
		// YAML output
		fmt.Println("# HydraVPN for Router Configuration")
		fmt.Println("# Generated at:", time.Now().Format(time.RFC3339))
	}
	return nil
}

type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("HydraVPN for Router %s\n", version.FullVersion())
	fmt.Printf("Commit: %s\n", version.Commit)
	fmt.Printf("Built: %s\n", version.Date)
	fmt.Printf("Go: %s\n", version.GoVersion)
	return nil
}

type ProvidersCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text"`
}

func (c *ProvidersCmd) Run() error {
	fmt.Println("Providers command - would connect to running daemon")
	return nil
}

type DNSCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text"`
}

func (c *DNSCmd) Run() error {
	fmt.Println("DNS command - would connect to running daemon")
	return nil
}

type FirewallCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text"`
}

func (c *FirewallCmd) Run() error {
	fmt.Println("Firewall command - would connect to running daemon")
	return nil
}

type SubsCmd struct {
	Format string `short:"f" help:"Output format (json, text)" default:"text"`
}

func (c *SubsCmd) Run() error {
	fmt.Println("Subscriptions command - would connect to running daemon")
	return nil
}

type CheckCmd struct {
	Check string `arg:"" help:"Check type (proxy, nft, singbox, inbounds, dns, fakeip, all)" default:"all"`
}

func (c *CheckCmd) Run() error {
	fmt.Printf("Running check: %s\n", c.Check)
	return nil
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("hydravpn-router"),
		kong.Description("HydraVPN for Router - Multi-platform DPI bypass solution"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}


