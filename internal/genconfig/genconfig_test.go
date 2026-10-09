package genconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

const vlessReality = "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=reality&sni=www.example.org&fp=chrome&pbk=PUBKEY&sid=ab12&type=tcp&flow=xtls-rprx-vision#node"

func TestXrayVlessReality(t *testing.T) {
	info, err := Parse(vlessReality)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Xray(info, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"protocol": "vless"`, `"flow": "xtls-rprx-vision"`, `"security": "reality"`, `"publicKey": "PUBKEY"`, `"shortId": "ab12"`, `"serverName": "www.example.org"`, `"port": 1080`, `"port": 1081`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
}

func TestSingBoxAndUnsupported(t *testing.T) {
	info, _ := Parse(vlessReality)
	b, err := SingBox(info, Options{SocksPort: 2080})
	if err != nil || !strings.Contains(string(b), `"listen_port": 2080`) || !strings.Contains(string(b), `"type": "vless"`) {
		t.Fatalf("%v\n%s", err, b)
	}
	hy, err := Parse("hysteria2://pw@example.com:443?sni=example.com#h")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Xray(hy, Options{}); err == nil {
		t.Error("hysteria2 must be refused for Xray")
	}
	if _, err := SingBox(hy, Options{}); err != nil {
		t.Errorf("sing-box hysteria2: %v", err)
	}
	if _, err := Parse("not a link"); err == nil {
		t.Error("garbage accepted")
	}
}
