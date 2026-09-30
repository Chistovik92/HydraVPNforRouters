// Package selftest checks, on the router itself, that everything the service
// needs is present and that the generated configuration is accepted by the
// real tools: "nft --check" for the firewall rules and "sing-box check" for
// the proxy config. It is the way to verify a new router model.
package selftest

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/firewall"
	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
	"github.com/Chistovik92/hydravpn-router/internal/providers/zapret"
)

// Status values.
const (
	Pass = "pass"
	Warn = "warn"
	Fail = "fail"
	Skip = "skip"
)

// Result is one check.
type Result struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Env abstracts the parts of the system the checks look at so they can be
// tested and pointed at a different root.
type Env struct {
	Root string // filesystem root, "" for the real one
	Run  func(ctx context.Context, stdin string, name string, args ...string) (string, error)
	Look func(name string) (string, error)
}

// SystemEnv is the real system.
func SystemEnv() Env {
	return Env{
		Run: func(ctx context.Context, stdin, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			if stdin != "" {
				cmd.Stdin = strings.NewReader(stdin)
			}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			return strings.TrimSpace(out.String()), err
		},
		Look: exec.LookPath,
	}
}

func (e Env) path(p string) string { return filepath.Join(e.Root, p) }

// Run executes all checks for cfg.
func Run(ctx context.Context, cfg *config.Config, env Env) []Result {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var res []Result
	add := func(name, status, format string, args ...interface{}) {
		res = append(res, Result{name, status, fmt.Sprintf(format, args...)})
	}

	if runtime.GOOS != "linux" && env.Root == "" {
		add("platform", Warn, "%s: firewall and routing checks need Linux; only the sing-box config is checked", runtime.GOOS)
	}

	// sing-box binary and config
	sbBin := cfg.Settings.SingBoxBinary
	if p, err := env.Look(sbBin); err != nil {
		add("sing-box", Fail, "%q not found in PATH: install sing-box (>= 1.12)", sbBin)
	} else {
		out, _ := env.Run(ctx, "", p, "version")
		add("sing-box", Pass, "%s (%s)", firstLine(out), p)
		res = append(res, checkSingBoxConfig(ctx, cfg, env, p)...)
	}

	// nftables ruleset
	res = append(res, checkNFT(ctx, cfg, env)...)

	if runtime.GOOS != "linux" && env.Root == "" {
		return res
	}

	// kernel support for transparent proxying
	res = append(res, checkTProxy(env))

	// routing tools and forwarding
	if _, err := env.Look("ip"); err != nil {
		add("ip", Fail, "the ip command is missing (install ip-full / iproute2)")
	} else if out, err := env.Run(ctx, "", "ip", "rule", "show"); err != nil {
		add("ip", Fail, "ip rule does not work: %s", out)
	} else {
		add("ip", Pass, "policy routing available")
	}
	if b, err := os.ReadFile(env.path("/proc/sys/net/ipv4/ip_forward")); err == nil && strings.TrimSpace(string(b)) != "1" {
		add("ip_forward", Warn, "net.ipv4.ip_forward is 0: the router will not forward LAN traffic")
	} else if err == nil {
		add("ip_forward", Pass, "IPv4 forwarding is on")
	}

	// optional providers
	for _, bin := range providerBinaries(cfg) {
		if p, err := env.Look(bin.path); err != nil {
			if _, statErr := os.Stat(env.path(bin.path)); statErr != nil {
				add(bin.name, Fail, "%s not found (section uses provider %s)", bin.path, bin.name)
				continue
			}
		} else {
			_ = p
		}
		add(bin.name, Pass, "%s present", bin.path)
	}

	// KeeneticOS specifics
	res = append(res, checkKeenetic(env)...)
	return res
}

type providerBin struct{ name, path string }

func providerBinaries(cfg *config.Config) []providerBin {
	var out []providerBin
	if cfg.ProviderEnabled(config.ProviderTypeZapret) || cfg.ProviderEnabled(config.ProviderTypeZapret2) {
		zc := zapret.ConfigFromSettings(cfg)
		out = append(out, providerBin{zc.ProviderType, zc.BinaryPath})
	}
	if cfg.ProviderEnabled(config.ProviderTypeByeDPI) {
		out = append(out, providerBin{"byedpi", "/usr/bin/ciadpi"})
	}
	return out
}

func checkSingBoxConfig(ctx context.Context, cfg *config.Config, env Env, bin string) []Result {
	c := singbox.ConfigFromSettings(cfg, nil)
	data, err := c.Render()
	if err != nil {
		return []Result{{"sing-box config", Fail, err.Error()}}
	}
	dir, err := os.MkdirTemp("", "hydravpn-selftest")
	if err != nil {
		return []Result{{"sing-box config", Fail, err.Error()}}
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return []Result{{"sing-box config", Fail, err.Error()}}
	}
	out, err := env.Run(ctx, "", bin, "check", "-c", path)
	if err != nil {
		return []Result{{"sing-box config", Fail, "rejected by sing-box: " + out}}
	}
	res := []Result{{"sing-box config", Pass, "accepted by sing-box check (without subscription nodes)"}}
	for _, w := range c.Warnings {
		res = append(res, Result{"config warning", Warn, w})
	}
	return res
}

func checkNFT(ctx context.Context, cfg *config.Config, env Env) []Result {
	script := firewall.NFTScript(cfg, nil)
	if script == "" {
		return []Result{{"nft", Skip, "nftables is not available on this OS"}}
	}
	if _, err := env.Look("nft"); err != nil {
		if _, err := env.Look("iptables"); err == nil {
			return []Result{{"nft", Warn, "nft not found; the service will use iptables (TPROXY target needed)"}}
		}
		return []Result{{"nft", Fail, "neither nft nor iptables found"}}
	}
	out, err := env.Run(ctx, script, "nft", "--check", "-f", "-")
	if err != nil {
		return []Result{{"nft", Fail, "the generated ruleset is rejected: " + out}}
	}
	return []Result{{"nft", Pass, "generated ruleset accepted by nft --check"}}
}

// tproxyNames are the kernel pieces that provide TPROXY.
var tproxyModules = []string{"nft_tproxy", "xt_TPROXY", "nf_tproxy_ipv4"}
var tproxyConfigs = []string{"CONFIG_NFT_TPROXY", "CONFIG_NETFILTER_XT_TARGET_TPROXY", "CONFIG_NF_TPROXY_IPV4"}

// checkTProxy looks for TPROXY support: loaded modules, built-in support
// (modules.builtin or /proc/config.gz) or module files that can be loaded.
func checkTProxy(env Env) Result {
	name := "kernel tproxy"
	if b, err := os.ReadFile(env.path("/proc/modules")); err == nil {
		for _, m := range tproxyModules {
			if strings.Contains(string(b), m+" ") {
				return Result{name, Pass, m + " is loaded"}
			}
		}
	}
	if matches, _ := filepath.Glob(env.path("/lib/modules/*/modules.builtin")); len(matches) > 0 {
		for _, f := range matches {
			if b, err := os.ReadFile(f); err == nil {
				for _, m := range tproxyModules {
					if strings.Contains(string(b), strings.ToLower(m)+".ko") || strings.Contains(string(b), m+".ko") {
						return Result{name, Pass, m + " is built into the kernel"}
					}
				}
			}
		}
	}
	if f, err := os.Open(env.path("/proc/config.gz")); err == nil {
		defer f.Close()
		if zr, err := gzip.NewReader(f); err == nil {
			data, _ := io.ReadAll(zr)
			for _, c := range tproxyConfigs {
				if strings.Contains(string(data), c+"=y") || strings.Contains(string(data), c+"=m") {
					return Result{name, Pass, c + " is enabled in the kernel config"}
				}
			}
		}
	}
	for _, pat := range []string{"/lib/modules/*/kernel/net/netfilter/nft_tproxy.ko*", "/lib/modules/*/kernel/net/netfilter/xt_TPROXY.ko*", "/lib/modules/*/nft_tproxy.ko*", "/lib/modules/*/xt_TPROXY.ko*"} {
		if m, _ := filepath.Glob(env.path(pat)); len(m) > 0 {
			return Result{name, Warn, "module file found but not loaded: " + filepath.Base(m[0]) + " (it loads on first use; OpenWrt: install kmod-nft-tproxy)"}
		}
	}
	return Result{name, Fail, "no TPROXY support found (nft_tproxy / xt_TPROXY). Transparent proxying (provider singbox) will not work on this firmware; zapret/ByeDPI sections may still work"}
}

func checkKeenetic(env Env) []Result {
	if _, err := env.Look("ndmc"); err != nil {
		if _, statErr := os.Stat(env.path("/etc/ndm")); statErr != nil {
			return nil
		}
	}
	if _, err := os.Stat(env.path("/opt/bin/opkg")); err != nil {
		return []Result{{"keenetic", Fail, "KeeneticOS without Entware: install Entware (OPKG) first, see INSTALL.md"}}
	}
	hook := env.path("/opt/etc/ndm/netfilter.d/50-hydravpn-router.sh")
	if _, err := os.Stat(hook); err != nil {
		return []Result{{"keenetic", Warn, "the NDM netfilter hook is missing: rules may disappear when KeeneticOS rebuilds its firewall (re-run install.sh)"}}
	}
	return []Result{{"keenetic", Pass, "Entware and the NDM netfilter hook are in place"}}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Failed reports whether any check failed.
func Failed(res []Result) bool {
	for _, r := range res {
		if r.Status == Fail {
			return true
		}
	}
	return false
}
