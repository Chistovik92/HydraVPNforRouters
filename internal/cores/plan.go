package cores

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// BasePort is the first local SOCKS5 port given to core nodes.
	BasePort = 4700
	// MaxProcesses bounds the OpenFlux and olcRTC clients (one process
	// each per node): a router has little memory.
	MaxProcesses = 8
	// MaxInterfaces bounds the AmneziaWG interfaces.
	MaxInterfaces = 4
	// awgTableBase + n is the routing table of interface awgN.
	awgTableBase = 4700
)

// Instance is a node with the local endpoint sing-box reaches it on.
type Instance struct {
	Node
	Port  int    // SOCKS5 port (Xray, OpenFlux, olcRTC)
	Iface string // network interface (AmneziaWG)
	Table int    // routing table of Iface
}

// Binaries are the core executables; an empty path means not installed.
type Binaries map[Kind]string

// Plan assigns endpoints to the core nodes of all sections. The same input
// gives the same plan, so the sing-box builder and the process manager agree
// without talking to each other.
type Plan struct {
	Instances []Instance
	Bins      Binaries
	byLink    map[string]int
	// Skipped explains nodes left out (core missing, limits).
	Skipped map[string]string
}

// NewPlan builds a plan for nodes (duplicates by link are merged).
func NewPlan(nodes []Node, bins Binaries) *Plan {
	p := &Plan{Bins: bins, byLink: map[string]int{}, Skipped: map[string]string{}}
	uniq := map[string]Node{}
	for _, n := range nodes {
		uniq[n.Link] = n
	}
	links := make([]string, 0, len(uniq))
	for l := range uniq {
		links = append(links, l)
	}
	// Ordered by the link without its #name: a provider renaming a node
	// (names often carry the traffic left) must not shift every port.
	key := func(l string) string { k, frag, _ := strings.Cut(l, "#"); return k + "\x00" + frag }
	sort.Slice(links, func(i, j int) bool { return key(links[i]) < key(links[j]) })

	port, procs, ifaces := BasePort, 0, 0
	for _, l := range links {
		n := uniq[l]
		if bins[n.Kind] == "" {
			p.Skipped[n.Name] = fmt.Sprintf("%s is not installed", n.Kind)
			continue
		}
		if n.Kind == Xray {
			if _, err := parseXrayLink(l); err != nil {
				p.Skipped[n.Name] = err.Error() // nothing would listen on its port
				continue
			}
		}
		in := Instance{Node: n}
		switch n.Kind {
		case AmneziaWG:
			if ifaces >= MaxInterfaces {
				p.Skipped[n.Name] = fmt.Sprintf("more than %d AmneziaWG nodes", MaxInterfaces)
				continue
			}
			in.Iface, in.Table = fmt.Sprintf("awg%d", ifaces), awgTableBase+ifaces
			ifaces++
		case OpenFlux, OlcRTC:
			if procs >= MaxProcesses {
				p.Skipped[n.Name] = fmt.Sprintf("more than %d OpenFlux/olcRTC nodes", MaxProcesses)
				continue
			}
			procs++
			fallthrough
		default:
			in.Port = port
			port++
		}
		p.byLink[l] = len(p.Instances)
		p.Instances = append(p.Instances, in)
	}
	return p
}

// Of returns the instances of one core.
func (p *Plan) Of(k Kind) []Instance {
	var out []Instance
	for _, in := range p.Instances {
		if in.Kind == k {
			out = append(out, in)
		}
	}
	return out
}

// Outbound returns the sing-box outbound (without tag) that reaches the node
// of link, or a reason it is not available.
func (p *Plan) Outbound(link string) (map[string]interface{}, error) {
	i, ok := p.byLink[link]
	if !ok {
		n, _ := Detect(link)
		if why := p.Skipped[n.Name]; why != "" {
			return nil, fmt.Errorf("%s", why)
		}
		return nil, fmt.Errorf("%s node is not planned", n.Kind)
	}
	in := p.Instances[i]
	if in.Iface != "" {
		// Sockets bound to the interface use its own routing table
		// (ip rule oif awgN), so this works without touching the main one.
		return map[string]interface{}{"type": "direct", "bind_interface": in.Iface}, nil
	}
	return map[string]interface{}{"type": "socks", "version": "5", "server": "127.0.0.1", "server_port": in.Port}, nil
}

// defaultNames are the executables looked up when no path is configured.
var defaultNames = map[Kind][]string{
	Xray:      {"xray"},
	OpenFlux:  {"openflux"},
	OlcRTC:    {"olcrtc"},
	AmneziaWG: {"amneziawg-go"},
}

// searchDirs are tried after PATH: Entware (Keenetic) and OpenWrt layouts,
// and the directory versions installed by the service go to.
var searchDirs = []string{"/opt/sbin", "/opt/bin", "/usr/sbin", "/usr/bin", "/usr/local/bin"}

// FindBinary resolves a configured path or looks the core up.
func FindBinary(k Kind, configured string, extraDirs ...string) string {
	if configured != "" {
		if isExecutable(configured) {
			return configured
		}
		return ""
	}
	for _, name := range defaultNames[k] {
		for _, d := range extraDirs {
			if p := filepath.Join(d, name); isExecutable(p) {
				return p
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		for _, d := range searchDirs {
			if p := filepath.Join(d, name); isExecutable(p) {
				return p
			}
		}
	}
	return ""
}

// Resolve follows symlinks, so a version switched behind the same link
// (bin/xray -> versions/xray/26.10.10/xray) is seen as a different binary
// and its process restarts.
func Resolve(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// FindTool looks up a helper (awg, ip) the same way.
func FindTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, d := range searchDirs {
		if p := filepath.Join(d, name); isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0111 != 0
}
