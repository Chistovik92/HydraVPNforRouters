package cores

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
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

// uapiKeys maps .conf keys of the interface to amneziawg-go UAPI keys
// (device/uapi.go); base64 keys go as hex, booleans as 1/0.
var uapiKeys = map[string]string{
	"jc": "jc", "jmin": "jmin", "jmax": "jmax", "s1": "s1", "s2": "s2", "s3": "s3", "s4": "s4",
	"h1": "h1", "h2": "h2", "h3": "h3", "h4": "h4", "i1": "i1", "i2": "i2", "i3": "i3", "i4": "i4", "i5": "i5",
	"headerprotectionkey": "header_protection_key", "contentpaddingaddition": "content_padding_addition",
	"rekeyaftertime": "rekey_after_time", "rekeytimeout": "rekey_timeout", "rejectaftertime": "reject_after_time",
	"keepalivetimeout": "keepalive_timeout", "maxhandshakeattempts": "max_handshake_attempts",
	"randomtrailers": "random_trailers", "disablecookies": "disable_cookies",
}

var uapiBool = map[string]bool{"random_trailers": true, "disable_cookies": true}

func keyHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("wireguard: bad key %q", b64)
	}
	return hex.EncodeToString(raw), nil
}

// UAPI renders the "set=1" request amneziawg-go reads from its control
// socket (what `awg setconf` sends), so no awg tool is needed on the router.
// resolve turns the endpoint host into an address (UAPI takes IP:port only).
func (c WireGuardConf) UAPI(resolve func(host string) (string, error)) (string, error) {
	var b strings.Builder
	set := func(k, v string) { b.WriteString(k + "=" + v + "\n") }
	set("set", "1")
	priv, err := keyHex(c.Interface["privatekey"])
	if err != nil {
		return "", err
	}
	set("private_key", priv)
	if p := c.Interface["listenport"]; p != "" {
		set("listen_port", p)
	}
	for _, k := range c.ifaceOrder {
		uk, ok := uapiKeys[k]
		if !ok {
			continue
		}
		v := c.Interface[k]
		switch {
		case uapiBool[uk]:
			v = map[bool]string{true: "1", false: "0"}[strings.EqualFold(v, "on") || strings.EqualFold(v, "true") || v == "1"]
		case uk == "header_protection_key":
			if v, err = keyHex(v); err != nil {
				return "", err
			}
		}
		set(uk, v)
	}
	pub, err := keyHex(c.Peer["publickey"])
	if err != nil {
		return "", err
	}
	set("replace_peers", "true")
	set("public_key", pub)
	if psk := c.Peer["presharedkey"]; psk != "" {
		h, err := keyHex(psk)
		if err != nil {
			return "", err
		}
		set("preshared_key", h)
	}
	host, port, err := net.SplitHostPort(c.Peer["endpoint"])
	if err != nil {
		return "", fmt.Errorf("wireguard: endpoint %q: %w", c.Peer["endpoint"], err)
	}
	if net.ParseIP(host) == nil {
		if host, err = resolve(host); err != nil {
			return "", fmt.Errorf("wireguard: resolve %s: %w", c.Peer["endpoint"], err)
		}
	}
	set("endpoint", net.JoinHostPort(host, port))
	if ka := c.Peer["persistentkeepalive"]; ka != "" {
		set("persistent_keepalive_interval", ka)
	}
	set("replace_allowed_ips", "true")
	for _, ip := range splitList(firstOf(c.Peer["allowedips"], "0.0.0.0/0, ::/0")) {
		set("allowed_ip", ip)
	}
	b.WriteString("\n") // an empty line ends the request
	return b.String(), nil
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
