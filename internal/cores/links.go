// Package cores runs the proxy cores sing-box cannot replace, the same ones
// the Hydra app uses: Xray-core (xhttp, VLESS encryption), OpenFlux, olcRTC
// and AmneziaWG. Each node of a subscription that needs one of them gets a
// local SOCKS5 port (or, for AmneziaWG, a network interface) and sing-box
// routes the node's traffic there, so sections, rules and url-test groups
// work for these nodes as for any other.
//
// Link formats and generated configs follow the Hydra app
// (shared/.../data/subscription in github.com/Chistovik92/HydraVPN).
package cores

import (
	"net/url"
	"strings"
)

// Kind names a core.
type Kind string

const (
	Xray      Kind = "xray"
	OpenFlux  Kind = "openflux"
	OlcRTC    Kind = "olcrtc"
	AmneziaWG Kind = "amneziawg"
)

// Kinds lists every core, in the order they are started.
var Kinds = []Kind{Xray, OpenFlux, OlcRTC, AmneziaWG}

// Node is a subscription entry served by a core.
type Node struct {
	Kind Kind
	Name string
	Link string
}

// xrayTransports are transports only Xray implements.
var xrayTransports = map[string]bool{"xhttp": true, "splithttp": true}

// Detect reports whether link needs a core instead of a sing-box outbound.
func Detect(link string) (Node, bool) {
	link = strings.TrimSpace(link)
	scheme, _, _ := strings.Cut(strings.ToLower(link), "://")
	var kind Kind
	switch scheme {
	case "vless", "trojan":
		if !needsXray(link) {
			return Node{}, false
		}
		kind = Xray
	case "vmess":
		if v, err := parseVMess(link); err != nil || !xrayTransports[strings.ToLower(v.Net)] {
			return Node{}, false
		}
		kind = Xray
	case "openflux":
		if _, err := ParseOpenFlux(link); err != nil {
			return Node{}, false
		}
		kind = OpenFlux
	case "olcrtc":
		if _, err := ParseOlcRTC(link); err != nil {
			return Node{}, false
		}
		kind = OlcRTC
	case "wireguard", "wg", "awg", "amnezia", "vpn":
		if _, err := ParseWireGuard(link); err != nil {
			return Node{}, false
		}
		kind = AmneziaWG
	default:
		return Node{}, false
	}
	return Node{Kind: kind, Name: nodeName(link, kind), Link: link}, true
}

// needsXray: a VLESS/Trojan link with an Xray-only transport, or VLESS
// encryption other than "none" (post-quantum VLESS), which sing-box lacks.
func needsXray(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	q := u.Query()
	if xrayTransports[strings.ToLower(q.Get("type"))] {
		return true
	}
	enc := q.Get("encryption")
	return u.Scheme == "vless" && enc != "" && enc != "none"
}

func nodeName(link string, kind Kind) string {
	switch kind {
	case OlcRTC:
		if n, err := ParseOlcRTC(link); err == nil && n.Name != "" {
			return n.Name
		}
	case OpenFlux:
		if c, err := ParseOpenFlux(link); err == nil && c.Name != "" {
			return c.Name
		}
	case Xray:
		if strings.HasPrefix(strings.ToLower(link), "vmess://") {
			if v, err := parseVMess(link); err == nil && v.PS != "" {
				return v.PS
			}
		}
	}
	if _, frag, ok := strings.Cut(link, "#"); ok {
		if n, err := url.PathUnescape(frag); err == nil && n != "" {
			return n
		}
	}
	return string(kind)
}
