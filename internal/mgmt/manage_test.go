package mgmt

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestEditingAMaskedSectionKeepsItsSecrets(t *testing.T) {
	ts, eng, cfgFile, token := setup(t)
	link := "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls#n"
	sec := config.Section{Name: "vpn", Enabled: true, SelectorProxyLinks: []string{link}}
	if code, body := call(t, ts, token, "POST", "/api/v1/sections", sec); code != 201 {
		t.Fatalf("add: %d %s", code, body)
	}
	// The client edits what it got back: links are masked there.
	var got []config.Section
	_, body := call(t, ts, token, "GET", "/api/v1/sections", nil)
	json.Unmarshal([]byte(body), &got)
	edited := got[len(got)-1]
	edited.Label = "VPN"
	if code, body := call(t, ts, token, "PUT", "/api/v1/sections/vpn", edited); code != 200 {
		t.Fatalf("replace: %d %s", code, body)
	}
	last := eng.cfg.Sections[len(eng.cfg.Sections)-1]
	if last.Label != "VPN" || len(last.SelectorProxyLinks) != 1 || last.SelectorProxyLinks[0] != link {
		t.Errorf("secret lost: %+v", last)
	}
	if data, _ := os.ReadFile(cfgFile); strings.Contains(string(data), "********") {
		t.Error("the mask was written to the config file")
	}
}

func TestServerAndSubscriptionEditKeepSecrets(t *testing.T) {
	ts, eng, _, token := setup(t)
	call(t, ts, token, "POST", "/api/v1/servers", config.Server{Name: "in", Protocol: "vless", ListenPort: 8443, ServerUUID: "11111111-2222-3333-4444-555555555555"})
	if code, body := call(t, ts, token, "PUT", "/api/v1/servers/in", config.Server{Protocol: "vless", ListenPort: 9443, ServerUUID: "********"}); code != 200 {
		t.Fatalf("server: %d %s", code, body)
	}
	if s := eng.cfg.Servers[0]; s.ListenPort != 9443 || s.ServerUUID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("server: %+v", s)
	}

	url := "https://panel.example/sub/secret-token"
	call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "main", "url": url})
	edit := config.SubscriptionURL{Section: "main", URL: config.MaskURL(url), SubscriptionUpdateEnabled: true, SubscriptionUpdateInterval: 6 * time.Hour}
	if code, body := call(t, ts, token, "PUT", "/api/v1/subscriptions/0", edit); code != 200 {
		t.Fatalf("subscription: %d %s", code, body)
	}
	if s := eng.cfg.SubscriptionURLs[0]; s.URL != url || s.SubscriptionUpdateInterval != 6*time.Hour {
		t.Errorf("subscription: %+v", s)
	}
}

func TestRulesCRUD(t *testing.T) {
	ts, eng, _, token := setup(t)
	if code, body := call(t, ts, token, "POST", "/api/v1/rules", config.Rule{Section: "main", Enabled: true, DomainSuffix: []string{"example.com"}}); code != 201 {
		t.Fatalf("add: %d %s", code, body)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/rules", config.Rule{Section: "nope", Enabled: true}); code != 400 {
		t.Errorf("rule to an unknown section accepted: %d", code)
	}
	if code, _ := call(t, ts, token, "PUT", "/api/v1/rules/0", config.Rule{Section: "main", Port: "443"}); code != 200 {
		t.Errorf("replace: %d", code)
	}
	if r := eng.cfg.Rules; len(r) != 1 || r[0].Port != "443" || r[0].Enabled {
		t.Errorf("rules: %+v", r)
	}
	_, body := call(t, ts, token, "GET", "/api/v1/rules", nil)
	if !strings.Contains(body, `"index":0`) {
		t.Errorf("list: %s", body)
	}
	if code, _ := call(t, ts, token, "DELETE", "/api/v1/rules/5", nil); code != 400 {
		t.Errorf("delete missing: %d", code)
	}
	if code, _ := call(t, ts, token, "DELETE", "/api/v1/rules/0", nil); code != 200 || len(eng.cfg.Rules) != 0 {
		t.Errorf("delete: %d %+v", code, eng.cfg.Rules)
	}
}

func TestListsCRUDAndInUseCheck(t *testing.T) {
	ts, eng, _, token := setup(t)
	if code, body := call(t, ts, token, "POST", "/api/v1/lists", config.CommunityList{Name: "work", Type: "domain", Entries: []string{"corp.example"}}); code != 201 {
		t.Fatalf("add: %d %s", code, body)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/lists", config.CommunityList{Name: "work"}); code != 400 {
		t.Errorf("duplicate accepted: %d", code)
	}
	call(t, ts, token, "PUT", "/api/v1/sections/main", config.Section{Enabled: true, CommunityLists: []string{"work"}})
	if code, body := call(t, ts, token, "DELETE", "/api/v1/lists/work", nil); code != 400 || !strings.Contains(body, "used by section") {
		t.Errorf("list in use deleted: %d %s", code, body)
	}
	if code, _ := call(t, ts, token, "PUT", "/api/v1/lists/work", config.CommunityList{Type: "domain", Entries: []string{"a.example", "b.example"}}); code != 200 {
		t.Errorf("replace: %d", code)
	}
	if l := eng.cfg.CommunityLists[len(eng.cfg.CommunityLists)-1]; l.Name != "work" || len(l.Entries) != 2 {
		t.Errorf("list: %+v", l)
	}
}

func TestSettingsPatch(t *testing.T) {
	ts, eng, cfgFile, token := setup(t)
	_, body := call(t, ts, token, "GET", "/api/v1/settings", nil)
	if !strings.Contains(body, `"update_interval":"24h0m0s"`) || strings.Contains(body, "secret-token") {
		t.Errorf("view: %s", body)
	}
	patch := map[string]interface{}{"disable_quic": true, "update_interval": "6h", "dns_server": []string{"1.1.1.1"}}
	if code, body := call(t, ts, token, "PATCH", "/api/v1/settings", patch); code != 200 {
		t.Fatalf("patch: %d %s", code, body)
	}
	st := eng.cfg.Settings
	if !st.DisableQUIC || st.UpdateInterval != 6*time.Hour || len(st.DNSServers) != 1 {
		t.Errorf("settings: quic=%v interval=%v dns=%v", st.DisableQUIC, st.UpdateInterval, st.DNSServers)
	}
	if data, _ := os.ReadFile(cfgFile); !strings.Contains(string(data), "disable_quic: true") {
		t.Errorf("not written:\n%s", data)
	}
	for _, bad := range []map[string]interface{}{
		{"api_token": "x"}, {"singbox_binary": "/tmp/x"}, {"no_such": 1}, {"update_interval": "soon"}, {"dns_check_timeout": "-1s"},
	} {
		if code, _ := call(t, ts, token, "PATCH", "/api/v1/settings", bad); code != 400 {
			t.Errorf("%v accepted: %d", bad, code)
		}
	}
}

func TestCapabilities(t *testing.T) {
	ts, _, _, token := setup(t)
	_, body := call(t, ts, token, "GET", "/api/v1/capabilities", nil)
	var c struct {
		API       int      `json:"api"`
		Features  []string `json:"features"`
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &c); err != nil || c.API < 2 {
		t.Fatalf("%v %s", err, body)
	}
	has := map[string]bool{}
	for _, f := range c.Features {
		has[f] = true
	}
	if !has["rules"] || !has["settings"] || has["update"] {
		t.Errorf("features without an updater: %v", c.Features)
	}
}
