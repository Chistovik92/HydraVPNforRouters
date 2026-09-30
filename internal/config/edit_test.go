package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const handWritten = `# My router
settings:
  # LAN interface
  source_network_interfaces: ["br-lan"]   # bridge
  log_level: "warn"

# Sections
sections:
  # first one
  - name: "main"
    enabled: true
    action: "connection"

subscription_urls: []

servers:
  # - name: "example"   (commented out example)
  #   protocol: "vless"
`

func file(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEditKeepsCommentsAndStyle(t *testing.T) {
	p := file(t, handWritten)
	cfg, err := ApplyEdits(p, []Edit{
		{Key: "subscription_urls", Op: OpAppend, Value: SubscriptionURL{Section: "main", URL: "https://panel.example/sub/x", SubscriptionUpdateEnabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SubscriptionURLs) != 1 {
		t.Fatalf("not applied: %+v", cfg.SubscriptionURLs)
	}
	out, _ := os.ReadFile(p)
	text := string(out)
	for _, keep := range []string{"# My router", "# LAN interface", "# bridge", "# first one", "# Sections", `#   protocol: "vless"`} {
		if !strings.Contains(text, keep) {
			t.Errorf("comment %q lost:\n%s", keep, text)
		}
	}
	// Two-space indentation, no dump of empty fields.
	if strings.Contains(text, "\n    name:") || strings.Contains(text, "label: \"\"") || strings.Contains(text, "selector_proxy_links") {
		t.Errorf("style changed or empty fields written:\n%s", text)
	}
	if !strings.Contains(text, "subscription_urls:\n  - section: main\n    url: https://panel.example/sub/x") {
		t.Errorf("appended entry looks wrong:\n%s", text)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Error("no backup")
	}
	if bak, _ := os.ReadFile(p + ".bak"); string(bak) != handWritten {
		t.Error("backup differs from the original")
	}
}

func TestEditRemoveReplaceAndEmptyList(t *testing.T) {
	p := file(t, handWritten)
	if _, err := ApplyEdits(p, []Edit{
		{Key: "sections", Op: OpAppend, Value: Section{Name: "second", Enabled: true, Action: ActionTypeBypass}},
		{Key: "sections", Op: OpReplaceByName, Name: "main", Value: Section{Name: "main", Enabled: false, Action: ActionTypeBlock}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadFromFile(p)
	if len(cfg.Sections) != 2 || cfg.Sections[0].Action != ActionTypeBlock || cfg.Sections[0].Enabled || cfg.Sections[1].Name != "second" {
		t.Fatalf("sections: %+v", cfg.Sections)
	}
	if text, _ := os.ReadFile(p); !strings.Contains(string(text), "# first one") {
		t.Error("comment of a replaced item lost")
	}

	if _, err := ApplyEdits(p, []Edit{{Key: "sections", Op: OpRemoveByName, Name: "second"}, {Key: "sections", Op: OpRemoveByName, Name: "main"}}); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(p)
	if !strings.Contains(string(text), "sections: []") {
		t.Errorf("an emptied list must print as []:\n%s", text)
	}
}

func TestInvalidEditWritesNothing(t *testing.T) {
	p := file(t, handWritten)
	_, err := ApplyEdits(p, []Edit{{Key: "subscription_urls", Op: OpAppend, Value: SubscriptionURL{Section: "nope", URL: "https://x.example/s"}}})
	if err == nil {
		t.Fatal("subscription of an unknown section accepted")
	}
	if out, _ := os.ReadFile(p); string(out) != handWritten {
		t.Error("file changed although the edit was invalid")
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("backup written for a rejected edit")
	}
	if _, err := ApplyEdits(p, []Edit{{Key: "sections", Op: OpRemoveByName, Name: "missing"}}); err == nil {
		t.Error("removing a missing item must fail")
	}
}

func TestEditCreatesMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new", "config.yaml")
	cfg, err := ApplyEdits(p, []Edit{{Key: "sections", Op: OpAppend, Value: Section{Name: "main", Enabled: true}}})
	if err != nil || len(cfg.Sections) != 1 {
		t.Fatalf("%v %+v", err, cfg)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("file not created")
	}
}

func TestSaveUsesTwoSpaceIndent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	c := DefaultConfig()
	c.Sections = []Section{{Name: "a"}}
	if err := c.SaveToFile(p); err != nil {
		t.Fatal(err)
	}
	if out, _ := os.ReadFile(p); strings.Contains(string(out), "\n    ") && !strings.Contains(string(out), "\n  settings") && strings.Contains(string(out), "\n    config_version") {
		t.Errorf("four-space indentation:\n%s", out[:200])
	}
}
