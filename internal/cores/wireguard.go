package cores

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// WireGuard / AmneziaWG configs come as wireguard://, wg://, awg://,
// amnezia:// or vpn:// (3x-ui) with the whole .conf in base64, or in the URI
// form wireguard://<private key>@host:port?publickey=…&address=…#name.
// amneziawg-go runs plain WireGuard too, so both use the same core.

// WireGuardConf is a parsed [Interface] + first [Peer].
type WireGuardConf struct {
	Interface map[string]string // lower-case keys
	Peer      map[string]string
	// Keys in the order of the file, to write them back.
	ifaceOrder, peerOrder []string
}

// awgParams are the AmneziaWG obfuscation keys of every generation.
var awgParams = map[string]bool{"jc": true, "jmin": true, "jmax": true, "s1": true, "s2": true, "s3": true, "s4": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "i1": true, "i2": true, "i3": true, "i4": true, "i5": true,
	"headerprotectionkey": true, "contentpaddingaddition": true, "rekeyaftertime": true, "rekeytimeout": true,
	"rejectaftertime": true, "keepalivetimeout": true, "maxhandshakeattempts": true, "randomtrailers": true,
	"disablecookies": true}

// Amnezia reports whether the config has obfuscation parameters.
func (c WireGuardConf) Amnezia() bool {
	for k := range c.Interface {
		if awgParams[k] {
			return true
		}
	}
	return false
}

// ParseWireGuard reads a WireGuard/AmneziaWG link or a pasted .conf.
func ParseWireGuard(link string) (WireGuardConf, error) {
	text, err := unwrapWireGuard(strings.TrimSpace(link))
	if err != nil {
		return WireGuardConf{}, err
	}
	c := WireGuardConf{Interface: map[string]string{}, Peer: map[string]string{}}
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.EqualFold(line, "[Interface]"):
			section = "i"
		case strings.EqualFold(line, "[Peer]"):
			if section == "p" {
				goto done // only the first peer
			}
			section = "p"
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok || section == "" {
				continue
			}
			k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
			if section == "i" {
				c.Interface[k] = v
				c.ifaceOrder = append(c.ifaceOrder, k)
			} else {
				c.Peer[k] = v
				c.peerOrder = append(c.peerOrder, k)
			}
		}
	}
done:
	if c.Peer["publickey"] == "" || c.Interface["privatekey"] == "" || c.Peer["endpoint"] == "" {
		return c, errors.New("wireguard: private key, peer public key and endpoint are required")
	}
	return c, nil
}

func unwrapWireGuard(t string) (string, error) {
	if strings.Contains(t, "[Interface]") {
		return t, nil
	}
	scheme, body, ok := strings.Cut(t, "://")
	if !ok {
		return "", errors.New("not a wireguard config")
	}
	scheme = strings.ToLower(scheme)
	if scheme == "wireguard" || scheme == "wg" {
		if conf, ok := wireGuardURI(body); ok {
			return conf, nil
		}
	}
	body, _, _ = strings.Cut(body, "#")
	data, err := decodeB64(body)
	if err != nil {
		if dec, err2 := url.QueryUnescape(body); err2 == nil && strings.Contains(dec, "[Interface]") {
			return dec, nil
		}
		return "", errors.New("wireguard: config is neither base64 nor text")
	}
	if !strings.Contains(string(data), "[Interface]") {
		// vpn:// of the AmneziaVPN app is a compressed JSON, not a .conf.
		return "", errors.New("wireguard: not a .conf")
	}
	return string(data), nil
}

// wireGuardURI converts the URI form to .conf text.
func wireGuardURI(body string) (string, bool) {
	main, _, _ := strings.Cut(body, "#")
	at := strings.LastIndex(main, "@")
	if at < 0 || !strings.Contains(strings.ToLower(main), "publickey=") {
		return "", false
	}
	// "+" in base64 keys is not a space.
	dec := func(v string) string {
		out, err := url.QueryUnescape(strings.ReplaceAll(v, "+", "%2B"))
		if err != nil {
			return v
		}
		return out
	}
	hostPort, query, _ := strings.Cut(main[at+1:], "?")
	q := map[string]string{}
	for _, kv := range strings.Split(query, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			q[strings.ToLower(k)] = dec(v)
		}
	}
	var b strings.Builder
	b.WriteString("[Interface]\nPrivateKey = " + dec(main[:at]) + "\n")
	for _, k := range []string{"address", "mtu", "dns"} {
		if q[k] != "" {
			b.WriteString(strings.ToUpper(k[:1]) + k[1:] + " = " + q[k] + "\n")
		}
	}
	b.WriteString("[Peer]\nPublicKey = " + q["publickey"] + "\n")
	if q["presharedkey"] != "" {
		b.WriteString("PresharedKey = " + q["presharedkey"] + "\n")
	}
	b.WriteString("AllowedIPs = " + firstOf(q["allowedips"], "0.0.0.0/0, ::/0") + "\n")
	b.WriteString("Endpoint = " + strings.TrimSuffix(hostPort, "/") + "\n")
	if q["keepalive"] != "" {
		b.WriteString("PersistentKeepalive = " + q["keepalive"] + "\n")
	}
	return b.String(), true
}

// wgQuickOnly are wg-quick keys that `awg setconf` rejects; they are applied
// with ip(8) instead.
var wgQuickOnly = map[string]bool{"address": true, "dns": true, "mtu": true, "table": true,
	"preup": true, "postup": true, "predown": true, "postdown": true, "saveconfig": true}

// SetConf renders the config for `awg setconf`.
func (c WireGuardConf) SetConf() string {
	var b strings.Builder
	b.WriteString("[Interface]\n")
	for _, k := range c.ifaceOrder {
		if !wgQuickOnly[k] {
			b.WriteString(k + " = " + c.Interface[k] + "\n")
		}
	}
	b.WriteString("\n[Peer]\n")
	for _, k := range c.peerOrder {
		b.WriteString(k + " = " + c.Peer[k] + "\n")
	}
	return b.String()
}

// Addresses are the interface addresses (Address = a, b).
func (c WireGuardConf) Addresses() []string { return splitList(c.Interface["address"]) }

// WireGuardResolver is the first DNS server of the config (or 1.1.1.1),
// used to resolve the names of the tunnel's own traffic.
func WireGuardResolver(link string) string {
	if c, err := ParseWireGuard(link); err == nil {
		for _, d := range splitList(c.Interface["dns"]) {
			if net.ParseIP(d) != nil {
				return d
			}
		}
	}
	return "1.1.1.1"
}

// MTU of the interface, "" for the default.
func (c WireGuardConf) MTU() string { return c.Interface["mtu"] }
