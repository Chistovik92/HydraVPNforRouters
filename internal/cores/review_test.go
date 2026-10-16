package cores

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
)

// Fixes from the 1.3.0 review.

func TestWireGuardRejectsWhatWouldReachIP(t *testing.T) {
	for name, conf := range map[string]string{
		"address": strings.Replace(awgConf, "Address = 10.8.0.2/32, fd00::2/128", "Address = 10.8.0.2/32 dev eth0", 1),
		"mtu":     strings.Replace(awgConf, "MTU = 1280", "MTU = 99999", 1),
	} {
		if _, err := ParseWireGuard("awg://" + base64.StdEncoding.EncodeToString([]byte(conf))); err == nil {
			t.Errorf("%s: a bad value was accepted", name)
		}
	}
	// A %0A in the URI form must not inject keys into the .conf.
	if _, err := ParseWireGuard("wireguard://cHJpdmF0ZQ%3D%3D@203.0.113.1:51820?publickey=cHVi&address=10.8.0.2%2F32%0AAddress%20%3D%20192.168.1.1%2F24#x"); err == nil {
		t.Error("a line break in a URI value was accepted")
	}
}

func TestOverlapWithRouterNetworks(t *testing.T) {
	_, lan, _ := net.ParseCIDR("192.168.1.1/24")
	old := localNets
	localNets = func() []*net.IPNet { return []*net.IPNet{lan} }
	defer func() { localNets = old }()
	if a, _ := overlapsLocal([]string{"10.8.0.2/32", "192.168.1.1/24"}); a != "192.168.1.1/24" {
		t.Errorf("LAN address not caught: %q", a)
	}
	if a, _ := overlapsLocal([]string{"10.8.0.2/32"}); a != "" {
		t.Errorf("false overlap: %q", a)
	}
}

func TestXrayLatestIsTheNewestRelease(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.add("XTLS/Xray-core", "v26.10.10", true, map[string][]byte{"Xray-linux-64.zip": []byte("x")})
	gh.add("XTLS/Xray-core", "v26.9.30", true, map[string][]byte{"Xray-linux-64.zip": []byte("x")})
	inst, _ := newTestInstaller(t, gh, "amd64")
	if v, err := inst.PickVersion(context.Background(), Xray, "latest"); err != nil || v != "26.10.10" {
		t.Errorf("latest = %q %v", v, err)
	}
	for _, bad := range []string{"../1", "1 2", "1;rm"} {
		if _, err := inst.PickVersion(context.Background(), SingBox, bad); err == nil {
			t.Errorf("version %q accepted", bad)
		}
		if err := inst.Start(SingBox, bad); err == nil {
			t.Errorf("Start accepted %q", bad)
		}
	}
}

func TestVersionArgsNeverCreateAnInterface(t *testing.T) {
	// amneziawg-go takes any other single argument as an interface name.
	if a := VersionArgs(AmneziaWG); len(a) != 1 || a[0] != "--version" {
		t.Errorf("amneziawg-go: %v", a)
	}
	if VersionArgs(OpenFlux) != nil || VersionArgs(OlcRTC) != nil {
		t.Error("no safe version argument is known for OpenFlux and olcRTC")
	}
	if a := probeArgs(AmneziaWG); a[0] != "--version" {
		t.Errorf("probe: %v", a)
	}
}

func TestPlanSkipsBrokenXrayAndKeepsPortsOnRename(t *testing.T) {
	bins := Binaries{Xray: "/bin/xray"}
	a := "vless://11111111-2222-3333-4444-555555555555@a.example.com:443?type=xhttp#A"
	b := "vless://11111111-2222-3333-4444-555555555555@b.example.com:443?type=xhttp#B"
	p := NewPlan([]Node{{Kind: Xray, Name: "A", Link: a}, {Kind: Xray, Name: "B", Link: b},
		{Kind: Xray, Name: "broken", Link: "vless://@:0?type=xhttp"}}, bins)
	if len(p.Instances) != 2 || p.Skipped["broken"] == "" {
		t.Fatalf("instances %+v skipped %v", p.Instances, p.Skipped)
	}
	// A renamed node (its #name changed) keeps the order and so the ports.
	renamed := strings.Replace(a, "#A", "#Z: 3 GB left", 1)
	q := NewPlan([]Node{{Kind: Xray, Name: "Z", Link: renamed}, {Kind: Xray, Name: "B", Link: b}}, bins)
	ob1, _ := p.Outbound(b)
	ob2, _ := q.Outbound(b)
	if ob1["server_port"] != ob2["server_port"] {
		t.Errorf("renaming another node moved B: %v -> %v", ob1["server_port"], ob2["server_port"])
	}
}
