package mgmt

import (
	"context"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Fixes from the 1.3.0 review.

func TestCallRecoversInsteadOfCrashingTheService(t *testing.T) {
	_, _, cfgFile, _ := setup(t)
	cfg, _ := config.LoadFromFile(cfgFile)
	s := NewInProcess(Options{Engine: &fakeEngine{cfg: cfg}, ConfigFile: cfgFile})
	// A control character makes the request builder panic.
	code, body := s.Call(context.Background(), "test", "GET", "/api/v1/logs?n=5\x7f", nil)
	if code != 500 || !strings.Contains(string(body), "internal error") {
		t.Errorf("%d %s", code, body)
	}
}

func TestMaskedListThatChangedLengthIsRefused(t *testing.T) {
	ts, eng, _, token := setup(t)
	links := []string{"trojan://a@x.example:443#1", "trojan://b@y.example:443#2"}
	call(t, ts, token, "POST", "/api/v1/sections", config.Section{Name: "vpn", Enabled: true, SelectorProxyLinks: links})
	// One of the two masked links removed: they can no longer be matched.
	code, body := call(t, ts, token, "PUT", "/api/v1/sections/vpn", config.Section{Enabled: true, SelectorProxyLinks: []string{"********"}})
	if code != 400 || !strings.Contains(body, "changed its length") {
		t.Errorf("%d %s", code, body)
	}
	if got := eng.cfg.Sections[len(eng.cfg.Sections)-1].SelectorProxyLinks; len(got) != 2 {
		t.Errorf("stored links changed: %v", got)
	}
}

func TestDurationsOnlyAsText(t *testing.T) {
	ts, _, _, token := setup(t)
	for _, bad := range []map[string]interface{}{{"dns_check_interval": 10}, {"dns_check_interval": "10ns"}, {"update_interval": "5s"}} {
		if code, body := call(t, ts, token, "PATCH", "/api/v1/settings", bad); code != 400 {
			t.Errorf("%v accepted: %d %s", bad, code, body)
		}
	}
	if code, body := call(t, ts, token, "PATCH", "/api/v1/settings", map[string]interface{}{"dns_check_interval": "30s"}); code != 200 {
		t.Errorf("30s refused: %d %s", code, body)
	}
}

func TestOldRuleOfRemovedSectionStillLoads(t *testing.T) {
	_, _, cfgFile, _ := setup(t)
	cfg, _ := config.LoadFromFile(cfgFile)
	cfg.Rules = append(cfg.Rules, config.Rule{Section: "gone"})
	if err := cfg.SaveToFile(cfgFile); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFromFile(cfgFile); err != nil {
		t.Errorf("an old config no longer loads: %v", err)
	}
}
