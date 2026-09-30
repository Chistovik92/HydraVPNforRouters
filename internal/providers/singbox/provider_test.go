package singbox

import (
	"encoding/json"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestConfigFromSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.DNSServers = []string{"77.88.8.8", "https://dns.example/dns-query", "1.1.1.1:5353"}
	cfg.Settings.FakeIPEnabled = true

	c := ConfigFromSettings(cfg, nil)
	data, err := c.Render()
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Inbounds  []map[string]interface{} `json:"inbounds"`
		Outbounds []map[string]interface{} `json:"outbounds"`
		DNS       struct {
			Servers []map[string]interface{} `json:"servers"`
			Final   string                   `json:"final"`
		} `json:"dns"`
		Route struct {
			Rules []map[string]interface{} `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	// DNS server tags must be unique and "final" must name a DNS server.
	tags := map[string]bool{}
	for _, s := range out.DNS.Servers {
		tag := s["tag"].(string)
		if tags[tag] {
			t.Errorf("duplicate DNS server tag %q", tag)
		}
		tags[tag] = true
		if _, legacy := s["address"]; legacy {
			t.Errorf("legacy DNS server format for %q", tag)
		}
	}
	if !tags[out.DNS.Final] {
		t.Errorf("dns.final %q is not a DNS server tag", out.DNS.Final)
	}
	if out.DNS.Servers[1]["type"] != "https" || out.DNS.Servers[1]["server"] != "dns.example" {
		t.Errorf("DoH server parsed wrong: %v", out.DNS.Servers[1])
	}
	if out.DNS.Servers[2]["server_port"] != float64(5353) {
		t.Errorf("custom port lost: %v", out.DNS.Servers[2])
	}
	if !tags["fakeip"] {
		t.Error("fakeip server missing")
	}

	// No removed special outbounds, no inbound sniff fields, no port clash.
	for _, ob := range out.Outbounds {
		if ob["type"] == "dns" || ob["type"] == "block" {
			t.Errorf("legacy outbound %v", ob)
		}
	}
	ports := map[float64]string{}
	for _, in := range out.Inbounds {
		if _, ok := in["sniff"]; ok {
			t.Errorf("inbound %v uses removed sniff field", in["tag"])
		}
		key := in["listen_port"].(float64)
		if prev, dup := ports[key]; dup && in["listen"] == "0.0.0.0" {
			t.Errorf("inbounds %s and %v share port %v", prev, in["tag"], key)
		}
		ports[key] = in["tag"].(string)
	}
	if out.Route.Rules[0]["action"] != "sniff" {
		t.Errorf("first route rule should sniff, got %v", out.Route.Rules[0])
	}
}
