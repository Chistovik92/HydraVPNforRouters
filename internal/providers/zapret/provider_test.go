package zapret

import (
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestArgsUseQueueAndCustomStrategy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "z", Enabled: true, Provider: config.ProviderTypeZapret, ProviderOptions: "--filter-tcp=443 --dpi-desync=fake"}}
	p := NewProvider(Options{Config: ConfigFromSettings(cfg)})
	args := strings.Join(p.buildArgs(), " ")
	if !strings.HasPrefix(args, "--qnum=4000 ") || !strings.Contains(args, "--dpi-desync=fake") || strings.Contains(args, "multidisorder") {
		t.Errorf("args: %s", args)
	}
	if p.config.ProviderType != "zapret" || !strings.HasSuffix(p.config.BinaryPath, "nfqws") {
		t.Errorf("provider: %+v", p.config)
	}
}

func TestZapret2WinsAndHasDefaultStrategy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{
		{Name: "a", Enabled: true, Provider: config.ProviderTypeZapret},
		{Name: "b", Enabled: true, Provider: config.ProviderTypeZapret2},
	}
	p := NewProvider(Options{Config: ConfigFromSettings(cfg)})
	if p.config.ProviderType != "zapret2" || !strings.Contains(strings.Join(p.buildArgs(), " "), "--lua-desync") {
		t.Errorf("provider: %+v", p.config)
	}
}
