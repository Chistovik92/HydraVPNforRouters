//go:build !windows

package firewall

import (
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestNFTScript(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewManager(Options{Config: cfg})
	m.sourceIPs = []string{"192.168.1.50"}
	m.nfqueue = &NFQueueOptions{QueueNum: 4000, DesyncMark: "0x40000000", TCPPorts: []int{80, 443}, UDPPorts: []int{443}}

	s := m.nftScript()
	for _, want := range []string{
		"add table inet hydravpn\ndelete table inet hydravpn\n", // idempotent replace
		"type filter hook prerouting priority mangle",
		`iifname { "br-lan" } meta nfproto ipv4 meta l4proto tcp meta mark set 0x08000000 tproxy ip to 127.0.0.1:1602 accept`,
		`iifname { "br-lan" } meta nfproto ipv4 meta l4proto udp meta mark set 0x08000000 tproxy ip to 127.0.0.1:1602 accept`,
		`ip saddr @source_v4 meta l4proto udp meta mark set`,
		"elements = { 192.168.1.50 }",
		"queue num 4000 bypass",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
	for _, bad := range []string{"tun0", "redirect"} {
		if strings.Contains(s, bad) {
			t.Errorf("script contains %q", bad)
		}
	}
}

func TestLocalSubnetsAreIPv4Networks(t *testing.T) {
	for _, n := range localSubnets() {
		if strings.Contains(n, ":") {
			t.Errorf("IPv6 network %s in IPv4 set", n)
		}
	}
}

func TestAddSourceIPValidation(t *testing.T) {
	m := NewManager(Options{Config: config.DefaultConfig()})
	if err := m.AddSourceIP("fe80::1"); err == nil {
		t.Error("IPv6 address accepted into IPv4 set")
	}
	if err := m.AddSourceIP("10.0.0.0/24"); err != nil {
		t.Error(err)
	}
}

func TestNFTScriptIPv6(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.EnableIPv6 = true
	s := NewManager(Options{Config: cfg}).nftScript()
	for _, want := range []string{
		"set local_v6",
		"type ipv6_addr",
		"meta nfproto ipv6 ip6 daddr @local_v6 return",
		`iifname { "br-lan" } meta nfproto ipv6 meta l4proto tcp meta mark set 0x08000000 tproxy ip6 to [::1]:1602 accept`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q in: %s", want, s)
		}
	}
	if strings.Contains(s, "meta nfproto != ipv4 return") {
		t.Error("IPv6 must not be dropped when enabled")
	}
	off := NewManager(Options{Config: config.DefaultConfig()}).nftScript()
	if strings.Contains(off, "local_v6") || !strings.Contains(off, "meta nfproto != ipv4 return") {
		t.Errorf("IPv6 disabled but script has v6 rules: %s", off)
	}
}
