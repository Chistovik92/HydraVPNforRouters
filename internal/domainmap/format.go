package domainmap

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

// Formats lists the supported output formats.
var Formats = []string{
	"plain", "json", "keenetic-cli", "keenetic-bat", "mikrotik",
	"wireguard", "openvpn", "nft", "ipset",
}

// FormatOptions tune Format.
type FormatOptions struct {
	Interface  string // keenetic-cli: interface ("Wireguard0"); keenetic-bat: gateway
	ListName   string // mikrotik address-list, nft set, ipset name
	MaxPerPart int    // split into parts of at most this many entries (0 = one part)
}

// Format renders prefixes in a format. Large outputs may be split into
// several parts (each a complete file).
func Format(format string, prefixes []netip.Prefix, o FormatOptions) ([]string, error) {
	if o.ListName == "" {
		o.ListName = "hydravpn"
	}
	prefixes = append([]netip.Prefix(nil), prefixes...)
	sort.Slice(prefixes, func(i, j int) bool { return prefixes[i].Addr().Less(prefixes[j].Addr()) })

	if format == "json" {
		list := make([]string, len(prefixes))
		for i, p := range prefixes {
			list[i] = p.String()
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		return []string{string(b) + "\n"}, nil
	}

	var v4, v6 []netip.Prefix
	for _, p := range prefixes {
		if p.Addr().Is4() {
			v4 = append(v4, p)
		} else {
			v6 = append(v6, p)
		}
	}

	switch format {
	case "wireguard":
		// One AllowedIPs line per part.
		var out []string
		for _, part := range chunk(prefixes, o.MaxPerPart) {
			s := make([]string, len(part))
			for i, p := range part {
				s[i] = p.String()
			}
			out = append(out, "AllowedIPs = "+strings.Join(s, ", ")+"\n")
		}
		return out, nil
	case "nft":
		var out []string
		for _, part := range chunk(v4, o.MaxPerPart) {
			s := make([]string, len(part))
			for i, p := range part {
				s[i] = p.String()
			}
			out = append(out, fmt.Sprintf("table inet hydravpn {\n\tset %s {\n\t\ttype ipv4_addr\n\t\tflags interval\n\t\telements = { %s }\n\t}\n}\n",
				o.ListName, strings.Join(s, ", ")))
		}
		return out, nil
	case "plain", "keenetic-cli", "keenetic-bat", "mikrotik", "openvpn", "ipset":
	default:
		return nil, fmt.Errorf("unknown format %q (%s)", format, strings.Join(Formats, ", "))
	}

	var lines []string
	for _, p := range append(v4, v6...) {
		if l, ok := routeLine(format, p, o); ok {
			lines = append(lines, l)
		}
	}
	var out []string
	for _, part := range chunk(lines, o.MaxPerPart) {
		head := ""
		switch format {
		case "keenetic-bat":
			head = "@echo off\n"
		case "ipset":
			head = fmt.Sprintf("create %s hash:net -exist\n", o.ListName)
		}
		out = append(out, head+strings.Join(part, "\n")+"\n")
	}
	return out, nil
}

func routeLine(format string, p netip.Prefix, o FormatOptions) (string, bool) {
	addr, mask := p.Addr().String(), maskOf(p)
	switch format {
	case "plain":
		return p.String(), true
	case "keenetic-cli":
		if !p.Addr().Is4() {
			return fmt.Sprintf("ipv6 route %s %s auto", p, o.Interface), true
		}
		return fmt.Sprintf("ip route %s %s %s auto", addr, mask, o.Interface), true
	case "keenetic-bat":
		if !p.Addr().Is4() {
			return "", false
		}
		return fmt.Sprintf("route add %s mask %s %s", addr, mask, o.Interface), true
	case "mikrotik":
		if p.Addr().Is4() {
			return fmt.Sprintf("/ip firewall address-list add list=%s address=%s", o.ListName, p), true
		}
		return fmt.Sprintf("/ipv6 firewall address-list add list=%s address=%s", o.ListName, p), true
	case "openvpn":
		if !p.Addr().Is4() {
			return fmt.Sprintf("route-ipv6 %s", p), true
		}
		return fmt.Sprintf("route %s %s", addr, mask), true
	case "ipset":
		if !p.Addr().Is4() {
			return "", false
		}
		return fmt.Sprintf("add %s %s", o.ListName, p), true
	}
	return "", false
}

func chunk[T any](in []T, n int) [][]T {
	if n <= 0 || len(in) <= n {
		return [][]T{in}
	}
	var out [][]T
	for len(in) > 0 {
		k := n
		if k > len(in) {
			k = len(in)
		}
		out = append(out, in[:k])
		in = in[k:]
	}
	return out
}

func maskOf(p netip.Prefix) string {
	if !p.Addr().Is4() {
		return ""
	}
	return net.IP(net.CIDRMask(p.Bits(), 32)).String()
}
