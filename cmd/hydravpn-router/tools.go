package main

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/domainmap"
	"github.com/Chistovik92/hydravpn-router/internal/genconfig"
	"github.com/Chistovik92/hydravpn-router/internal/keenetic"
	"github.com/Chistovik92/hydravpn-router/internal/lists"
)

// DomainMapCmd resolves domains to addresses and prints route lists
// (the DomainMapper idea: see docs/INTEGRATIONS.md).
type DomainMapCmd struct {
	List          []string      `short:"l" help:"List file or http(s) URL with domains / subnets (repeatable)"`
	Domain        []string      `short:"d" help:"A domain to resolve (repeatable)"`
	Section       string        `short:"s" help:"Take the domains of this config section (domains: and its community lists)"`
	ConfigFile    string        `short:"c" help:"Configuration file path" default:"${config_file}"`
	DNS           []string      `help:"DNS server to ask (repeatable; all are asked and merged). Default: dns_server of the config, else the system resolver"`
	Aggregate     string        `short:"a" help:"Grouping: host, 24, 16, 24+32" default:"24+32"`
	Format        string        `short:"f" help:"Output: ${formats}" default:"plain"`
	Interface     string        `help:"Interface for keenetic-cli / gateway for keenetic-bat"`
	Name          string        `help:"List name for mikrotik / nft / ipset" default:"hydravpn"`
	Output        string        `short:"o" help:"Write to this file instead of stdout (parts get .1, .2 ...)"`
	Split         int           `help:"Split the output into parts of this many entries (0 = one file)"`
	NoCloudflare  bool          `help:"Drop Cloudflare addresses"`
	IPv6          bool          `help:"Also resolve AAAA records"`
	Timeout       time.Duration `help:"Per-query timeout" default:"5s"`
	Exec          string        `help:"Shell command to run after the files are written"`
	ApplyKeenetic string        `name:"apply-keenetic" help:"Add the routes to this Keenetic interface through ndmc (KeeneticOS only)"`
	DryRun        bool          `help:"With --apply-keenetic: only print the ndmc commands"`
}

func (c *DomainMapCmd) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	agg, err := domainmap.ParseAggregation(c.Aggregate)
	if err != nil {
		return err
	}
	var src domainmap.Source
	src.Domains = append(src.Domains, c.Domain...)
	for _, l := range c.List {
		if err := src.Load(ctx, l); err != nil {
			return fmt.Errorf("list %s: %w", l, err)
		}
	}
	servers := c.DNS
	if c.Section != "" || len(servers) == 0 {
		if cfg, err := config.LoadFromFile(c.ConfigFile); err == nil {
			if len(servers) == 0 {
				servers = cfg.Settings.DNSServers
			}
			if c.Section != "" {
				if err := loadSectionSource(ctx, cfg, c.Section, &src); err != nil {
					return err
				}
			}
		} else if c.Section != "" {
			return fmt.Errorf("load config: %w", err)
		}
	}
	if len(src.Domains) == 0 && len(src.Prefixes) == 0 {
		return fmt.Errorf("nothing to resolve: pass --list, --domain or --section")
	}

	run := domainmap.Run{
		Resolve:        domainmap.Options{Servers: servers, Timeout: c.Timeout, IPv6: c.IPv6},
		Aggregation:    agg,
		DropCloudflare: c.NoCloudflare,
	}
	fmt.Fprintf(os.Stderr, "resolving %d domains through %d DNS server(s)...\n", len(src.Domains), max(len(servers), 1))
	prefixes, failed := run.Do(ctx, src)
	fmt.Fprintf(os.Stderr, "%d routes (%d domains had no address, %d patterns skipped)\n", len(prefixes), len(failed), src.Skipped)

	if c.ApplyKeenetic != "" {
		return c.applyKeenetic(ctx, prefixes)
	}

	parts, err := domainmap.Format(c.Format, prefixes, domainmap.FormatOptions{Interface: c.Interface, ListName: c.Name, MaxPerPart: c.Split})
	if err != nil {
		return err
	}
	if c.Output == "" {
		for _, p := range parts {
			fmt.Print(p)
		}
	} else {
		for i, p := range parts {
			name := c.Output
			if len(parts) > 1 {
				ext := filepath.Ext(name)
				name = fmt.Sprintf("%s.%d%s", strings.TrimSuffix(name, ext), i+1, ext)
			}
			if err := os.WriteFile(name, []byte(p), 0o644); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "written", name)
		}
	}
	if c.Exec != "" {
		return runShell(ctx, c.Exec)
	}
	return nil
}

func (c *DomainMapCmd) applyKeenetic(ctx context.Context, prefixes []netip.Prefix) error {
	cmds, err := keenetic.RouteCommands(c.ApplyKeenetic, prefixes)
	if err != nil {
		return err
	}
	if c.DryRun {
		fmt.Println(strings.Join(append(cmds, "system configuration save"), "\n"))
		return nil
	}
	if !keenetic.Available() {
		return fmt.Errorf("ndmc not found: --apply-keenetic works on KeeneticOS only (use --dry-run to print the commands)")
	}
	if err := keenetic.Apply(ctx, keenetic.Ndmc{}, cmds); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d routes added to %s and saved\n", len(cmds), c.ApplyKeenetic)
	return nil
}

// loadSectionSource adds the inline domains and the lists of a section.
func loadSectionSource(ctx context.Context, cfg *config.Config, name string, src *domainmap.Source) error {
	var sec *config.Section
	for i := range cfg.Sections {
		if cfg.Sections[i].Name == name {
			sec = &cfg.Sections[i]
		}
	}
	if sec == nil {
		return fmt.Errorf("section %q not found in the config", name)
	}
	src.Add(lists.ParseEntries(strings.Join(sec.Domains, "\n")))
	refs := append(append(append([]string(nil), sec.CommunityLists...), sec.RuleSet...), sec.RuleSetWithSubnets...)
	for _, ref := range refs {
		for _, u := range listURLs(cfg, ref) {
			if !lists.NeedsDownload(u) {
				fmt.Fprintf(os.Stderr, "skipped %s: only plain-text lists can be resolved\n", config.MaskURL(u))
				continue
			}
			if err := src.Load(ctx, u); err != nil {
				fmt.Fprintf(os.Stderr, "list %s: %v\n", config.MaskURL(u), err)
			}
		}
	}
	return nil
}

func listURLs(cfg *config.Config, ref string) []string {
	if strings.Contains(ref, "://") {
		return []string{ref}
	}
	var out []string
	for _, l := range cfg.CommunityLists {
		if l.Name == ref && l.URL != "" {
			out = append(out, l.URL)
		}
	}
	for _, r := range cfg.RuleSets {
		if r.Name == ref && r.URL != "" {
			out = append(out, r.URL)
		}
	}
	return out
}

func runShell(ctx context.Context, command string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// GenConfigCmd builds a standalone Xray or sing-box config from a link.
type GenConfigCmd struct {
	Link      string `arg:"" help:"vless:// vmess:// trojan:// ss:// hysteria2:// link"`
	Core      string `short:"k" help:"Target core" default:"xray" enum:"xray,sing-box"`
	Listen    string `help:"Address of the local inbounds" default:"127.0.0.1"`
	SocksPort int    `help:"Local SOCKS5 port" default:"1080"`
	HTTPPort  int    `help:"Local HTTP port (Xray; -1 disables)" default:"1081"`
	Output    string `short:"o" help:"Write to this file instead of stdout"`
}

func (c *GenConfigCmd) Run() error {
	info, err := genconfig.Parse(c.Link)
	if err != nil {
		return err
	}
	o := genconfig.Options{Listen: c.Listen, SocksPort: c.SocksPort, HTTPPort: c.HTTPPort}
	var data []byte
	if c.Core == "xray" {
		data, err = genconfig.Xray(info, o)
	} else {
		data, err = genconfig.SingBox(info, o)
	}
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if c.Output == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(c.Output, data, 0o600)
}

// KeeneticCmd works with KeeneticOS through ndmc.
type KeeneticCmd struct {
	Interfaces KeeneticInterfacesCmd `cmd:"" help:"List the interfaces of the router (ndmc show interface)"`
	Proxy      KeeneticProxyCmd      `cmd:"" help:"Create a Proxy interface that points at the local proxy of the service"`
}

type KeeneticInterfacesCmd struct{}

func (c *KeeneticInterfacesCmd) Run() error {
	if !keenetic.Available() {
		return fmt.Errorf("ndmc not found: this is not KeeneticOS")
	}
	ifaces, err := keenetic.Interfaces(context.Background(), keenetic.Ndmc{})
	if err != nil {
		return err
	}
	for _, i := range ifaces {
		state := "down"
		if i.Connected {
			state = "up"
		}
		fmt.Printf("%-16s %-18s %-5s %s\n", i.ID, i.Type, state, i.Description)
	}
	return nil
}

type KeeneticProxyCmd struct {
	Name  string `help:"Interface name" default:"Proxy0"`
	Host  string `help:"Upstream address" default:"127.0.0.1"`
	Port  int    `help:"Upstream SOCKS5 port (the mixed inbound of the service)" default:"4534"`
	Apply bool   `help:"Run the commands (default: only print them)"`
}

func (c *KeeneticProxyCmd) Run() error {
	cmds, err := keenetic.ProxyCommands(c.Name, c.Host, c.Port)
	if err != nil {
		return err
	}
	if !c.Apply {
		fmt.Println(strings.Join(append(cmds, "system configuration save"), "\n"))
		return nil
	}
	if !keenetic.Available() {
		return fmt.Errorf("ndmc not found: this is not KeeneticOS")
	}
	return keenetic.Apply(context.Background(), keenetic.Ndmc{}, cmds)
}
