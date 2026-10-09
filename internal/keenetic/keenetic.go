// Package keenetic talks to KeeneticOS through ndmc, its built-in command
// line client (the same one NeoFit and HydraRoute use). It lists interfaces,
// adds static routes and creates a proxy interface that points at the local
// proxy of the service.
//
// Not verified on real hardware: every function takes a Runner, so the
// commands can be inspected (--dry-run) before anything is applied.
package keenetic

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Runner runs one ndmc command ("ip route 1.2.3.0 255.255.255.0 Wireguard0 auto").
type Runner interface {
	Run(ctx context.Context, command string) (string, error)
}

// Ndmc runs commands through the ndmc binary.
type Ndmc struct{ Binary string }

// Available reports whether ndmc is on this router.
func Available() bool {
	_, err := exec.LookPath("ndmc")
	return err == nil
}

// Run implements Runner.
func (n Ndmc) Run(ctx context.Context, command string) (string, error) {
	bin := n.Binary
	if bin == "" {
		bin = "ndmc"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-c", command).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("ndmc -c %q: %w: %s", command, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Interface is a Keenetic network interface.
type Interface struct {
	ID          string // "Wireguard0"
	Type        string
	Description string
	Connected   bool
}

var kvLine = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+):\s*(.*?)\s*$`)

// ParseInterfaces parses the output of "show interface": blocks of
// "key: value" lines, each block starting with "id: <name>".
func ParseInterfaces(out string) []Interface {
	var res []Interface
	var cur *Interface
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := kvLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		key, val := m[1], strings.Trim(m[2], `"`)
		if key == "id" {
			res = append(res, Interface{ID: val})
			cur = &res[len(res)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "type":
			cur.Type = val
		case "description":
			cur.Description = val
		case "connected":
			cur.Connected = val == "yes" || val == "true"
		}
	}
	return res
}

// Interfaces lists the interfaces of the router.
func Interfaces(ctx context.Context, r Runner) ([]Interface, error) {
	out, err := r.Run(ctx, "show interface")
	if err != nil {
		return nil, err
	}
	return ParseInterfaces(out), nil
}

var ifaceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

// RouteCommands returns the ndmc commands that route prefixes through an
// interface. "auto" lets Keenetic keep the route in step with the interface.
func RouteCommands(iface string, prefixes []netip.Prefix) ([]string, error) {
	if !ifaceName.MatchString(iface) {
		return nil, fmt.Errorf("bad interface name %q", iface)
	}
	cmds := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p.Addr().Is4() {
			cmds = append(cmds, fmt.Sprintf("ip route %s %s %s auto", p.Addr(), net.IP(net.CIDRMask(p.Bits(), 32)), iface))
		} else {
			cmds = append(cmds, fmt.Sprintf("ipv6 route %s %s auto", p, iface))
		}
	}
	return cmds, nil
}

// ProxyCommands returns the commands that create a Proxy interface sending
// the router's traffic to a local SOCKS5 server (the service's mixed inbound),
// so Keenetic policies can route through it.
func ProxyCommands(name, host string, port int) ([]string, error) {
	if !ifaceName.MatchString(name) {
		return nil, fmt.Errorf("bad interface name %q", name)
	}
	if net.ParseIP(host) == nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("bad upstream %s:%d", host, port)
	}
	return []string{
		"interface " + name,
		"interface " + name + " proxy protocol socks5",
		fmt.Sprintf("interface %s proxy upstream %s %d", name, host, port),
		"interface " + name + " security-level public",
		"interface " + name + " ip global auto",
		"interface " + name + " up",
	}, nil
}

// Apply runs the commands and saves the configuration. It stops at the first
// error and does not save a half-applied configuration.
func Apply(ctx context.Context, r Runner, cmds []string) error {
	if len(cmds) == 0 {
		return errors.New("nothing to apply")
	}
	for _, c := range cmds {
		if _, err := r.Run(ctx, c); err != nil {
			return err
		}
	}
	_, err := r.Run(ctx, "system configuration save")
	return err
}
