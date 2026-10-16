package cores

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const (
	xhttpLink  = "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?type=xhttp&security=reality&pbk=KEY&sid=ab&sni=www.example.com&path=%2Fx&mode=auto#XHTTP%20node"
	plainVless = "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?type=ws&security=tls#ws"
	awgConf    = "[Interface]\nPrivateKey = cHJpdmF0ZQ==\nAddress = 10.8.0.2/32, fd00::2/128\nDNS = 9.9.9.9\nMTU = 1280\nJc = 4\nH1 = 1\n\n[Peer]\nPublicKey = cHVibGlj\nAllowedIPs = 0.0.0.0/0\nEndpoint = 203.0.113.1:51820\n"
)

func awgLink() string { return "awg://" + base64.StdEncoding.EncodeToString([]byte(awgConf)) }

func TestDetect(t *testing.T) {
	ofLink, err := encodeOpenFluxV1(OpenFluxConfig{Name: "Flux", Transports: []OpenFluxTransport{{Type: "yandex", URL: "https://disk.example/doc"}}})
	if err != nil {
		t.Fatal(err)
	}
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"ps":"VM","add":"v.example","port":"443","id":"11111111-2222-3333-4444-555555555555","net":"xhttp","tls":"tls"}`))
	key := strings.Repeat("ab", 32)
	for link, want := range map[string]Kind{
		xhttpLink: Xray,
		"vless://11111111-2222-3333-4444-555555555555@h:443?encryption=mlkem768x25519plus.native.0rtt.abc&security=none#pq": Xray,
		vmess:  Xray,
		ofLink: OpenFlux,
		"openflux://yandex?url=https%3A%2F%2Fdisk.example%2Fd&key=0123456789abcdef#Old":  OpenFlux,
		"olcrtc://telemost?datachannel<fps=30&video-w=640>@room-1#" + key + "$My%20room": OlcRTC,
		awgLink(): AmneziaWG,
		"wireguard://cHJpdmF0ZQ%3D%3D@203.0.113.1:51820?publickey=cHVibGlj&address=10.8.0.2%2F32#WG": AmneziaWG,
		plainVless:                          "",
		"trojan://p@h:443#t":                "",
		"ss://YWVzLTI1Ni1nY206cA@h:8388#ss": "",
		"vpn://bm90IGEgY29uZg":              "", // AmneziaVPN JSON, not a .conf
	} {
		n, ok := Detect(link)
		if want == "" {
			if ok {
				t.Errorf("%s detected as %s", link, n.Kind)
			}
			continue
		}
		if !ok || n.Kind != want {
			t.Errorf("%s: got %q %v, want %s", link, n.Kind, ok, want)
		}
	}
	if n, _ := Detect(xhttpLink); n.Name != "XHTTP node" {
		t.Errorf("name: %q", n.Name)
	}
	if n, _ := Detect(vmess); n.Name != "VM" {
		t.Errorf("vmess name: %q", n.Name)
	}
}

func TestXrayConfigServesEveryNodeOnItsPort(t *testing.T) {
	data, skipped := XrayConfig([]Instance{
		{Node: Node{Name: "a", Link: xhttpLink}, Port: 4700},
		{Node: Node{Name: "bad", Link: "vless://@:0?type=xhttp"}, Port: 4701},
	}, "")
	if len(skipped) != 1 || skipped["bad"] == nil {
		t.Errorf("skipped: %v", skipped)
	}
	var cfg struct {
		Inbounds []struct {
			Port int `json:"port"`
		} `json:"inbounds"`
		Routing struct {
			Rules []map[string]interface{} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Port != 4700 || len(cfg.Routing.Rules) != 1 {
		t.Fatalf("config: %s", data)
	}
	s := string(data)
	for _, want := range []string{`"network": "xhttp"`, `"security": "reality"`, `"publicKey": "KEY"`, `"mode": "auto"`, `"path": "/x"`, `"encryption": "none"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
}

func TestOpenFluxArgs(t *testing.T) {
	sess := OpenFluxConfig{Negotiate: true, Secret: "0123456789abcdef0123", Context: "ctx",
		Transports: []OpenFluxTransport{{Type: "direct", Dial: "203.0.113.5:9000", Priority: 100}, {Type: "yandex", URL: "https://d/x", Priority: 50}}}
	link, err := encodeOpenFluxV1(sess)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseOpenFlux(link)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(OpenFluxArgs(c, 4702, "/run/k"), " ")
	want := "--role client --inbound socks5 --socks5 127.0.0.1:4702 --negotiate --transports direct:100,yandex:50 --direct-dial 203.0.113.5:9000 --yandex-url https://d/x --session-context ctx --encryption-key-file /run/k"
	if got != want {
		t.Errorf("args:\n got %s\nwant %s", got, want)
	}
	old, err := ParseOpenFlux("openflux://oneme?maxToken=T&maxUid=7#M")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(OpenFluxArgs(old, 1, ""), " "); !strings.Contains(got, "--transport oneme --maxToken T --maxUid 7") {
		t.Errorf("oneme: %s", got)
	}
	if _, err := ParseOpenFlux("openflux://v1/%%%"); err == nil {
		t.Error("damaged link accepted")
	}
	short, _ := encodeOpenFluxV1(OpenFluxConfig{Secret: "short", Transports: []OpenFluxTransport{{Type: "yandex", URL: "u"}}})
	if _, err := ParseOpenFlux(short); err == nil {
		t.Error("short secret accepted")
	}
}

func TestOlcRTC(t *testing.T) {
	key := strings.Repeat("ab", 32)
	n, err := ParseOlcRTC("olcrtc://jitsi?datachannel<fps=30&video-codec=vp8>@meet.jit.si/Room#" + key + "$My%20room")
	if err != nil {
		t.Fatal(err)
	}
	if n.Provider != "jitsi" || n.Room != "meet.jit.si/Room" || n.Key != key || n.Name != "My room" || n.Params["fps"] != "30" {
		t.Fatalf("%+v", n)
	}
	y := OlcRTCConfig(n, 4703, "")
	for _, want := range []string{"mode: cnc", `id: "meet.jit.si/Room"`, "port: 4703", "sei:\n  fps: 30", "video:\n  codec: \"vp8\"", `dns: "1.1.1.1:53"`} {
		if !strings.Contains(y, want) {
			t.Errorf("missing %q in\n%s", want, y)
		}
	}
	if _, err := ParseOlcRTC("olcrtc://telemost?datachannel@room"); err == nil {
		t.Error("link without a key accepted")
	}
}

func TestWireGuard(t *testing.T) {
	c, err := ParseWireGuard(awgLink())
	if err != nil {
		t.Fatal(err)
	}
	if !c.Amnezia() || c.MTU() != "1280" || len(c.Addresses()) != 2 {
		t.Errorf("%+v", c)
	}
	sc := c.SetConf()
	for _, bad := range []string{"address", "dns", "mtu"} {
		if strings.Contains(sc, bad+" =") {
			t.Errorf("setconf keeps %s:\n%s", bad, sc)
		}
	}
	if !strings.Contains(sc, "jc = 4") || !strings.Contains(sc, "endpoint = 203.0.113.1:51820") {
		t.Errorf("setconf:\n%s", sc)
	}
	if r := WireGuardResolver(awgLink()); r != "9.9.9.9" {
		t.Errorf("resolver %s", r)
	}
	u, err := ParseWireGuard("wireguard://cHJpdmF0ZQ%3D%3D@203.0.113.1:51820?publickey=cHVi+bGlj&address=10.8.0.2%2F32&mtu=1420#WG")
	if err != nil || u.Interface["privatekey"] != "cHJpdmF0ZQ==" || u.Peer["publickey"] != "cHVi+bGlj" || u.Amnezia() {
		t.Errorf("uri form: %v %+v", err, u)
	}
}

func TestPlanIsStableAndBounded(t *testing.T) {
	bins := Binaries{Xray: "/bin/xray", OlcRTC: "/bin/olcrtc", AmneziaWG: "/bin/awg"}
	var nodes []Node
	for i := 0; i < MaxProcesses+2; i++ {
		c := string(rune('a' + i))
		nodes = append(nodes, Node{Kind: OlcRTC, Name: "o" + c, Link: "olcrtc://t?d@r" + c + "#k"})
	}
	nodes = append(nodes, Node{Kind: Xray, Name: "x", Link: xhttpLink}, Node{Kind: Xray, Name: "x-dup", Link: xhttpLink},
		Node{Kind: OpenFlux, Name: "of", Link: "openflux://yandex?url=u"},
		Node{Kind: AmneziaWG, Name: "w", Link: awgLink()})
	reordered := append([]Node{nodes[len(nodes)-1]}, nodes[:len(nodes)-1]...)
	p1, p2 := NewPlan(nodes, bins), NewPlan(reordered, bins)
	if len(p1.Instances) != len(p2.Instances) {
		t.Fatal("order changed the plan")
	}
	for i := range p1.Instances {
		if p1.Instances[i].Port != p2.Instances[i].Port || p1.Instances[i].Link != p2.Instances[i].Link {
			t.Errorf("instance %d differs: %+v vs %+v", i, p1.Instances[i], p2.Instances[i])
		}
	}
	if len(p1.Of(OlcRTC)) != MaxProcesses || len(p1.Of(Xray)) != 1 {
		t.Errorf("olcrtc %d xray %d", len(p1.Of(OlcRTC)), len(p1.Of(Xray)))
	}
	if !strings.Contains(p1.Skipped["of"], "not installed") {
		t.Errorf("skipped: %v", p1.Skipped)
	}
	ob, err := p1.Outbound(xhttpLink)
	if err != nil || ob["type"] != "socks" || ob["server_port"].(int) < BasePort {
		t.Errorf("xray outbound: %v %v", ob, err)
	}
	if ob, err := p1.Outbound(awgLink()); err != nil || ob["bind_interface"] != "awg0" {
		t.Errorf("awg outbound: %v %v", ob, err)
	}
}

func TestManagerSpecsAndSignatures(t *testing.T) {
	m := NewManager(Options{Dir: t.TempDir()})
	p := NewPlan([]Node{{Kind: Xray, Name: "x", Link: xhttpLink}, {Kind: AmneziaWG, Name: "w", Link: awgLink()}},
		Binaries{Xray: "/bin/xray", AmneziaWG: "/bin/amneziawg-go"})
	specs, err := m.specs(p)
	if err != nil || len(specs) != 2 {
		t.Fatalf("%v %+v", err, specs)
	}
	if specs[0].key != "xray" || specs[1].key != "awg0" || specs[1].inst == nil || specs[1].args[0] != "-f" {
		t.Errorf("specs: %+v", specs)
	}
	again, _ := m.specs(p)
	if again[0].signature != specs[0].signature {
		t.Error("signature is not stable")
	}
}
