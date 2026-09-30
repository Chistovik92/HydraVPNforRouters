package byedpi

import (
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestArgsListenOnSocksPortUsedByRouting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "d", Enabled: true, Provider: config.ProviderTypeByeDPI, ProviderOptions: "-o1"}}
	p := NewProvider(Options{Config: ConfigFromSettings(cfg)})
	args := strings.Join(p.buildArgs(), " ")
	if !strings.Contains(args, "-p 1080") || !strings.HasSuffix(args, "-o1") {
		t.Errorf("args: %s", args)
	}
	if p.config.Port != config.ByeDPIPort {
		t.Errorf("port %d differs from config.ByeDPIPort %d: sing-box would route to a dead port", p.config.Port, config.ByeDPIPort)
	}
}
