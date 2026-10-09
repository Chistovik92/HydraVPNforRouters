// Package genconfig turns proxy links (vless://, vmess://, trojan://, ss://,
// hysteria2://) into ready-to-run Xray and sing-box configs, like the
// config generator of NeoFit (github.com/pegakmop/neofit). The output is a
// standalone config with local SOCKS5/HTTP inbounds, usable without the
// rest of the service.
package genconfig

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
)

// Options of the generated config.
type Options struct {
	Listen    string // local address of the inbounds, default 127.0.0.1
	SocksPort int    // default 1080
	HTTPPort  int    // default 1081, -1 disables the HTTP inbound (Xray only)
	LogLevel  string // default warning (Xray) / warn (sing-box)
}

func (o *Options) defaults() {
	if o.Listen == "" {
		o.Listen = "127.0.0.1"
	}
	if o.SocksPort == 0 {
		o.SocksPort = 1080
	}
	if o.HTTPPort == 0 {
		o.HTTPPort = 1081
	}
}

// Parse reads one proxy link and returns its node.
func Parse(link string) (subscription.OutboundInfo, error) {
	infos, err := subscription.ParseSubscription(strings.TrimSpace(link))
	if err != nil {
		return subscription.OutboundInfo{}, err
	}
	if len(infos) == 0 {
		return subscription.OutboundInfo{}, fmt.Errorf("no proxy found in the link")
	}
	return infos[0], nil
}

// Xray returns an Xray config (JSON) for the node.
func Xray(info subscription.OutboundInfo, o Options) ([]byte, error) {
	o.defaults()
	if info.Raw != nil {
		return nil, fmt.Errorf("JSON outbounds cannot be converted to Xray")
	}
	ob, err := xrayOutbound(info)
	if err != nil {
		return nil, err
	}
	level := o.LogLevel
	if level == "" {
		level = "warning"
	}
	inbounds := []map[string]interface{}{{
		"tag": "socks-in", "listen": o.Listen, "port": o.SocksPort, "protocol": "socks",
		"settings": map[string]interface{}{"auth": "noauth", "udp": true},
		"sniffing": map[string]interface{}{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
	}}
	if o.HTTPPort > 0 {
		inbounds = append(inbounds, map[string]interface{}{
			"tag": "http-in", "listen": o.Listen, "port": o.HTTPPort, "protocol": "http",
		})
	}
	cfg := map[string]interface{}{
		"log":      map[string]interface{}{"loglevel": level},
		"inbounds": inbounds,
		"outbounds": []interface{}{
			ob,
			map[string]interface{}{"tag": "direct", "protocol": "freedom"},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func xrayOutbound(info subscription.OutboundInfo) (map[string]interface{}, error) {
	ob := map[string]interface{}{"tag": "proxy"}
	switch info.Type {
	case "vless":
		user := map[string]interface{}{"id": info.UUID, "encryption": "none"}
		if info.Flow != "" {
			user["flow"] = info.Flow
		}
		ob["protocol"] = "vless"
		ob["settings"] = map[string]interface{}{"vnext": []interface{}{map[string]interface{}{
			"address": info.Server, "port": info.Port, "users": []interface{}{user},
		}}}
	case "vmess":
		ob["protocol"] = "vmess"
		sec := info.Method
		if sec == "" {
			sec = "auto"
		}
		ob["settings"] = map[string]interface{}{"vnext": []interface{}{map[string]interface{}{
			"address": info.Server, "port": info.Port,
			"users": []interface{}{map[string]interface{}{"id": info.UUID, "alterId": 0, "security": sec}},
		}}}
	case "trojan":
		ob["protocol"] = "trojan"
		ob["settings"] = map[string]interface{}{"servers": []interface{}{map[string]interface{}{
			"address": info.Server, "port": info.Port, "password": info.Password,
		}}}
	case "shadowsocks":
		ob["protocol"] = "shadowsocks"
		ob["settings"] = map[string]interface{}{"servers": []interface{}{map[string]interface{}{
			"address": info.Server, "port": info.Port, "method": info.Method, "password": info.Password,
		}}}
	default:
		return nil, fmt.Errorf("xray does not support %q (use sing-box)", info.Type)
	}
	if info.Type != "shadowsocks" {
		ob["streamSettings"] = xrayStream(info)
	}
	return ob, nil
}

func xrayStream(info subscription.OutboundInfo) map[string]interface{} {
	network := strings.ToLower(info.Transport)
	switch network {
	case "", "tcp", "raw":
		network = "tcp"
	case "h2", "http":
		network = "h2"
	}
	ss := map[string]interface{}{"network": network}
	sni := info.SNI
	if sni == "" {
		sni = info.Host
	}
	if sni == "" && net.ParseIP(info.Server) == nil {
		sni = info.Server
	}
	switch strings.ToLower(info.Security) {
	case "tls":
		ss["security"] = "tls"
		tls := map[string]interface{}{"serverName": sni}
		if info.Fingerprint != "" {
			tls["fingerprint"] = info.Fingerprint
		}
		if alpn := strings.Split(info.Extra["alpn"], ","); alpn[0] != "" {
			tls["alpn"] = alpn
		}
		if info.Extra["insecure"] == "1" {
			tls["allowInsecure"] = true
		}
		ss["tlsSettings"] = tls
	case "reality":
		ss["security"] = "reality"
		fp := info.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		ss["realitySettings"] = map[string]interface{}{
			"serverName": sni, "fingerprint": fp, "publicKey": info.PublicKey,
			"shortId": info.ShortID, "spiderX": info.Extra["spx"],
		}
	default:
		ss["security"] = "none"
	}
	switch network {
	case "ws":
		ws := map[string]interface{}{"path": firstNonEmpty(info.Path, "/")}
		if info.Host != "" {
			ws["headers"] = map[string]interface{}{"Host": info.Host}
		}
		ss["wsSettings"] = ws
	case "grpc":
		ss["grpcSettings"] = map[string]interface{}{"serviceName": info.Path}
	case "h2":
		ss["httpSettings"] = map[string]interface{}{"path": firstNonEmpty(info.Path, "/"), "host": []string{info.Host}}
	}
	return ss
}

// SingBox returns a standalone sing-box config (JSON) for the node.
func SingBox(info subscription.OutboundInfo, o Options) ([]byte, error) {
	o.defaults()
	ob, err := singbox.BuildOutbound(info)
	if err != nil {
		return nil, err
	}
	ob["tag"] = "proxy"
	level := o.LogLevel
	if level == "" {
		level = "warn"
	}
	cfg := map[string]interface{}{
		"log": map[string]interface{}{"level": level},
		"inbounds": []interface{}{map[string]interface{}{
			"type": "mixed", "tag": "mixed-in", "listen": o.Listen, "listen_port": o.SocksPort,
		}},
		"outbounds": []interface{}{ob, map[string]interface{}{"type": "direct", "tag": "direct"}},
		"route": map[string]interface{}{
			"final": "proxy",
			"rules": []interface{}{map[string]interface{}{"action": "sniff"}},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
