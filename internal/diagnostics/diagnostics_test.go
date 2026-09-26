package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// "global" used to include itself and recursed until the stack overflowed.
func TestGlobalDoesNotRecurse(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.DNSServers = []string{"192.0.2.1"}
	cfg.Settings.BootstrapDNSServers = nil
	cfg.Settings.LatencyTestURL = "http://127.0.0.1:1/"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := NewDiagnostics(cfg, nil).RunCheck(ctx, "global")
	if err != nil {
		t.Fatal(err)
	}
	checks, ok := res.Details["checks"].([]*CheckResult)
	if !ok || len(checks) != len(CheckNames())-1 {
		t.Fatalf("unexpected checks: %v", res.Details["checks"])
	}
	for _, c := range checks {
		if c.Name == "global" {
			t.Fatal("global check ran itself")
		}
	}
}

func TestUnknownCheck(t *testing.T) {
	if _, err := NewDiagnostics(nil, nil).RunCheck(context.Background(), "nope"); err == nil {
		t.Fatal("expected error")
	}
}
