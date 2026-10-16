package core

import (
	"path/filepath"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
)

// coreNodes collects the nodes of enabled proxy sections that need a core:
// from subscriptions and from the section's own links.
func coreNodes(cfg *config.Config, subs singbox.NodeSource) []cores.Node {
	var out []cores.Node
	for _, sec := range cfg.Sections {
		if !sec.Enabled || sec.Action == config.ActionTypeBlock || sec.Action == config.ActionTypeBypass {
			continue
		}
		if p := sec.Provider; p != "" && p != config.ProviderTypeSingBox && p != config.ProviderTypeAuto {
			continue
		}
		if subs != nil {
			for _, ob := range subs.GetOutbounds(sec.Name) {
				if ob.Core != "" {
					out = append(out, cores.Node{Kind: cores.Kind(ob.Core), Name: ob.Name, Link: ob.Link})
				}
			}
		}
		for _, link := range sec.SelectorProxyLinks {
			if n, ok := cores.Detect(link); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

// CoresDir is where the service keeps the core versions it installed: next
// to the config file (/opt/etc/hydravpn-router/cores on Keenetic,
// /etc/hydravpn-router/cores on OpenWrt).
func CoresDir(configFile string) string {
	if configFile == "" {
		return filepath.Join(config.DefaultConfigDir, "cores")
	}
	return filepath.Join(filepath.Dir(configFile), "cores")
}

// coreBinaries resolves the installed cores: the configured path, the
// versions the service installed, then the usual locations.
func coreBinaries(cfg *config.Config, coresDir string) cores.Binaries {
	s := cfg.Settings
	dir := filepath.Join(coresDir, "bin")
	return cores.Binaries{
		cores.Xray:      cores.FindBinary(cores.Xray, s.XrayBinary, dir),
		cores.OpenFlux:  cores.FindBinary(cores.OpenFlux, s.OpenFluxBinary, dir),
		cores.OlcRTC:    cores.FindBinary(cores.OlcRTC, s.OlcRTCBinary, dir),
		cores.AmneziaWG: cores.FindBinary(cores.AmneziaWG, s.AmneziaWGBinary, dir),
	}
}

// applyCores plans the core nodes, starts or updates their processes and
// returns the plan the sing-box config is built with. It runs whenever the
// sing-box config is rendered, so both always agree.
func (e *Engine) applyCores() *cores.Plan {
	plan := cores.NewPlan(coreNodes(e.config, e.subscriptionMgr), coreBinaries(e.config, e.coresDir))
	for name, why := range plan.Skipped {
		e.log("warn", "node "+name+" skipped: "+why)
	}
	if err := e.cores.Apply(plan); err != nil {
		e.log("error", err.Error())
	}
	return plan
}
