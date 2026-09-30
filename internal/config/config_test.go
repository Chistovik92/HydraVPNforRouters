package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := LoadFromFile(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil || cfg.Settings.DNSCheckInterval == 0 || cfg.Settings.AppLogLevel != "info" {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := LoadFromFile(write(t, "settings:\n  log_level: debug\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Settings
	if s.LogLevel != "debug" || s.SingBoxBinary != "sing-box" || len(s.DNSServers) == 0 || s.DNSStrategy != DNSStrategyPreferIPv4 {
		t.Errorf("defaults not applied: %+v", s)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]string{
		"duplicate section": "sections:\n  - name: a\n  - name: a\n",
		"unnamed section":   "sections:\n  - enabled: true\n",
		"unknown provider":  "sections:\n  - name: a\n    provider: nope\n",
		"unknown action":    "sections:\n  - name: a\n    action: nope\n",
		"unknown section":   "sections:\n  - name: a\nsubscription_urls:\n  - section: b\n    url: https://x.example/s\n",
		"non-http url":      "sections:\n  - name: a\nsubscription_urls:\n  - section: a\n    url: ftp://x.example/s\n",
		"negative interval": "settings:\n  update_interval: -1h\n",
		"bad yaml":          "settings: [\n",
	}
	for name, body := range cases {
		if _, err := LoadFromFile(write(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestProviderEnabledAndOptions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sections = []Section{
		{Name: "a", Enabled: true},
		{Name: "b", Enabled: true, Provider: ProviderTypeZapret, ProviderOptions: "--x"},
		{Name: "c", Enabled: false, Provider: ProviderTypeByeDPI},
	}
	if !cfg.ProviderEnabled(ProviderTypeSingBox) || !cfg.ProviderEnabled(ProviderTypeZapret) || cfg.ProviderEnabled(ProviderTypeByeDPI) {
		t.Error("ProviderEnabled wrong")
	}
	if cfg.ProviderOptions(ProviderTypeZapret) != "--x" {
		t.Error("ProviderOptions wrong")
	}
}

func TestMaskedHidesSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings.APIToken = "tok"
	cfg.SubscriptionURLs = []SubscriptionURL{{Section: "a", URL: "https://user:pw@panel.example/sub/SECRET?t=1"}}
	cfg.Servers = []Server{{Name: "s", ServerUUID: "uuid", RealityPrivateKey: "priv", TailscaleAuthKey: "ts"}}
	cfg.Sections = []Section{{Name: "a", SelectorProxyLinks: []string{"vless://x@h:1"}}}

	m := cfg.Masked()
	dump := strings.Join([]string{
		m.Settings.APIToken, m.SubscriptionURLs[0].URL, m.Servers[0].ServerUUID,
		m.Servers[0].RealityPrivateKey, m.Servers[0].TailscaleAuthKey, m.Sections[0].SelectorProxyLinks[0],
	}, " ")
	for _, secret := range []string{"tok", "SECRET", "pw", "uuid", "priv", "ts", "vless://"} {
		if strings.Contains(dump, secret) {
			t.Errorf("secret %q leaked: %s", secret, dump)
		}
	}
	if !strings.Contains(m.SubscriptionURLs[0].URL, "panel.example") {
		t.Error("host must stay visible")
	}
	// The original is untouched.
	if cfg.Servers[0].RealityPrivateKey != "priv" || cfg.Sections[0].SelectorProxyLinks[0] != "vless://x@h:1" {
		t.Error("Masked modified the original")
	}
}

func TestSaveAndReload(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sections = []Section{{Name: "a", Enabled: true}}
	p := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := cfg.SaveToFile(p); err != nil {
		t.Fatal(err)
	}
	back, err := LoadFromFile(p)
	if err != nil || len(back.Sections) != 1 || back.Sections[0].Name != "a" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}
