package cores

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// One Xray process serves every Xray node: an inbound per node on its own
// port, routed to that node's outbound. Same outbound/stream layout as the
// Hydra app's XrayConfigBuilder.

type vmessLink struct {
	PS   string      `json:"ps"`
	Add  string      `json:"add"`
	Port json.Number `json:"port"`
	ID   string      `json:"id"`
	Aid  json.Number `json:"aid"`
	Net  string      `json:"net"`
	Type string      `json:"type"`
	Host string      `json:"host"`
	Path string      `json:"path"`
	TLS  string      `json:"tls"`
	SNI  string      `json:"sni"`
	ALPN string      `json:"alpn"`
	FP   string      `json:"fp"`
}

func decodeB64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimRight(s, "="))
	return base64.RawStdEncoding.DecodeString(s)
}

func parseVMess(link string) (vmessLink, error) {
	var v vmessLink
	body := link[len("vmess://"):]
	data, err := decodeB64(body)
	if err != nil {
		return v, fmt.Errorf("vmess: %w", err)
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("vmess: %w", err)
	}
	return v, nil
}

// xrayNode is a link in the terms of an Xray outbound.
type xrayNode struct {
	protocol, address, id, flow, encryption string
	port, alterID                           int
	network, security, sni, host, path      string
	alpn, fingerprint, publicKey, shortID   string
	spiderX, mode, headerType               string
	insecure                                bool
}

func parseXrayLink(link string) (xrayNode, error) {
	if strings.HasPrefix(strings.ToLower(link), "vmess://") {
		v, err := parseVMess(link)
		if err != nil {
			return xrayNode{}, err
		}
		port, _ := v.Port.Int64()
		aid, _ := v.Aid.Int64()
		n := xrayNode{protocol: "vmess", address: v.Add, port: int(port), id: v.ID, alterID: int(aid),
			network: v.Net, sni: v.SNI, host: v.Host, path: v.Path, alpn: v.ALPN, fingerprint: v.FP, headerType: v.Type}
		if v.TLS == "tls" {
			n.security = "tls"
		}
		return n, n.check()
	}
	u, err := url.Parse(link)
	if err != nil {
		return xrayNode{}, err
	}
	q := u.Query()
	port, _ := strconv.Atoi(u.Port())
	n := xrayNode{
		protocol: strings.ToLower(u.Scheme), address: u.Hostname(), port: port,
		flow: q.Get("flow"), encryption: q.Get("encryption"),
		network: q.Get("type"), security: q.Get("security"), sni: firstOf(q.Get("sni"), q.Get("peer")),
		host: q.Get("host"), path: firstOf(q.Get("path"), q.Get("serviceName")), alpn: q.Get("alpn"),
		fingerprint: q.Get("fp"), publicKey: q.Get("pbk"), shortID: q.Get("sid"), spiderX: q.Get("spx"),
		mode: q.Get("mode"), headerType: q.Get("headerType"),
	}
	if u.User != nil {
		n.id = u.User.Username()
	}
	for _, k := range []string{"insecure", "allowInsecure", "allow_insecure"} {
		if v := q.Get(k); v == "1" || strings.EqualFold(v, "true") {
			n.insecure = true
		}
	}
	return n, n.check()
}

func (n xrayNode) check() error {
	if n.address == "" || n.port <= 0 || n.id == "" {
		return fmt.Errorf("%s: missing server, port or credentials", n.protocol)
	}
	return nil
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

type obj = map[string]interface{}

func (n xrayNode) outbound(tag string) obj {
	ob := obj{"tag": tag, "protocol": n.protocol}
	server := obj{"address": n.address, "port": n.port}
	switch n.protocol {
	case "trojan":
		server["password"] = n.id
		ob["settings"] = obj{"servers": []obj{server}}
	case "vmess":
		server["users"] = []obj{{"id": n.id, "security": "auto", "alterId": n.alterID}}
		ob["settings"] = obj{"vnext": []obj{server}}
	default: // vless
		user := obj{"id": n.id, "encryption": firstOf(n.encryption, "none")}
		if n.flow != "" {
			user["flow"] = n.flow
		}
		server["users"] = []obj{user}
		ob["settings"] = obj{"vnext": []obj{server}}
	}
	ob["streamSettings"] = n.stream()
	return ob
}

func (n xrayNode) stream() obj {
	network := xrayNetwork(n.network)
	security := strings.ToLower(n.security)
	if security == "" && n.protocol == "trojan" {
		security = "tls" // trojan implies TLS
	}
	if security != "tls" && security != "reality" {
		security = "none"
	}
	s := obj{"network": network, "security": security,
		// Find dead connections after a silent drop (as in the app).
		"sockopt": obj{"tcpKeepAliveIdle": 30, "tcpKeepAliveInterval": 15}}
	sni := firstOf(n.sni, n.host, n.address)
	switch security {
	case "tls":
		tls := obj{"serverName": sni, "allowInsecure": n.insecure}
		if alpn := splitList(n.alpn); len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		if n.fingerprint != "" {
			tls["fingerprint"] = n.fingerprint
		}
		s["tlsSettings"] = tls
	case "reality":
		s["realitySettings"] = obj{"serverName": sni, "publicKey": n.publicKey, "shortId": n.shortID,
			"spiderX": firstOf(n.spiderX, "/"), "fingerprint": firstOf(n.fingerprint, "chrome")}
	}
	switch network {
	case "ws", "httpupgrade":
		set := obj{"path": firstOf(n.path, "/")}
		if n.host != "" {
			set["host"] = n.host
		}
		s[network+"Settings"] = set
	case "grpc":
		s["grpcSettings"] = obj{"serviceName": n.path, "multiMode": n.mode == "multi"}
	case "xhttp":
		set := obj{"path": firstOf(n.path, "/")}
		if n.host != "" {
			set["host"] = n.host
		}
		if n.mode != "" {
			set["mode"] = n.mode
		}
		s["xhttpSettings"] = set
	case "tcp":
		if n.headerType != "" && n.headerType != "none" {
			s["tcpSettings"] = obj{"header": obj{"type": n.headerType}}
		}
	}
	return s
}

// xrayNetwork maps link transports to Xray's; "http"/"h2" were removed from
// Xray and xhttp is the replacement (as in the app).
func xrayNetwork(t string) string {
	switch t = strings.ToLower(t); t {
	case "ws", "grpc", "httpupgrade", "xhttp":
		return t
	case "splithttp", "http", "h2":
		return "xhttp"
	}
	return "tcp"
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// XrayConfig renders the Xray configuration for the given instances; nodes
// whose link cannot be parsed are reported in skipped.
func XrayConfig(instances []Instance, logLevel string) (data []byte, skipped map[string]error) {
	skipped = map[string]error{}
	var inbounds, outbounds, rules []obj
	for _, in := range instances {
		n, err := parseXrayLink(in.Link)
		if err != nil {
			skipped[in.Name] = err
			continue
		}
		inTag, outTag := fmt.Sprintf("in-%d", in.Port), fmt.Sprintf("out-%d", in.Port)
		inbounds = append(inbounds, obj{"tag": inTag, "listen": "127.0.0.1", "port": in.Port,
			"protocol": "socks", "settings": obj{"auth": "noauth", "udp": true}})
		outbounds = append(outbounds, n.outbound(outTag))
		rules = append(rules, obj{"type": "field", "inboundTag": []string{inTag}, "outboundTag": outTag})
	}
	outbounds = append(outbounds, obj{"tag": "direct", "protocol": "freedom"})
	cfg := obj{
		"log":       obj{"loglevel": firstOf(logLevel, "warning")},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   obj{"rules": rules},
	}
	data, _ = json.MarshalIndent(cfg, "", "  ")
	return data, skipped
}
