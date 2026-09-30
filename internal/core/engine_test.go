package core

import (
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestStoppedEngineStatusAndReloadValidation(t *testing.T) {
	e, err := NewEngine(EngineOptions{Config: config.DefaultConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if e.GetState() != EngineStateStopped {
		t.Fatalf("state %s", e.GetState())
	}
	st := e.GetStatus()
	for _, k := range []string{"version", "state", "providers", "dns", "firewall", "subscriptions"} {
		if _, ok := st[k]; !ok {
			t.Errorf("status lacks %q", k)
		}
	}
	if err := e.Reload(nil); err == nil {
		t.Error("Reload(nil) must fail")
	}
	if err := e.Stop(); err != nil {
		t.Errorf("Stop on a stopped engine: %v", err)
	}
}

func TestProvidersFollowSections(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{
		{Name: "a", Enabled: true, Provider: config.ProviderTypeZapret2},
		{Name: "b", Enabled: true, Provider: config.ProviderTypeByeDPI},
	}
	e, _ := NewEngine(EngineOptions{Config: cfg})
	e.initProviders()
	if e.zapretProvider == nil || e.byedpiProvider == nil || e.singboxProvider == nil {
		t.Error("providers of enabled sections were not created")
	}
	if q := e.nfqueueOptions(); q == nil || q.QueueNum == 0 {
		t.Errorf("nfqueue options: %+v", q)
	}
}
