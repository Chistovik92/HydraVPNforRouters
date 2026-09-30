package dnsredirect

import (
	"os"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestApplyAndRemove(t *testing.T) {
	restarts := 0
	r := &Redirect{ConfDir: t.TempDir(), Restart: func() error { restarts++; return nil }}

	if err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(r.file())
	if !strings.Contains(string(data), "server=127.0.0.42") || restarts != 1 {
		t.Errorf("apply: %q restarts=%d", data, restarts)
	}
	// Applying the same content does not restart dnsmasq again.
	r.Apply()
	if restarts != 1 {
		t.Errorf("unchanged apply restarted dnsmasq (%d)", restarts)
	}
	if err := r.Remove(); err != nil || restarts != 2 {
		t.Errorf("remove: %v restarts=%d", err, restarts)
	}
	if err := r.Remove(); err != nil || restarts != 2 {
		t.Errorf("second remove must be a no-op: %v restarts=%d", err, restarts)
	}
}

func TestUnsupportedPlatformIsNotAnError(t *testing.T) {
	r := &Redirect{ConfDir: "/nonexistent/dnsmasq.d"}
	if err := r.Apply(); err != nil {
		t.Error(err)
	}
}

func TestWanted(t *testing.T) {
	cfg := config.DefaultConfig()
	if Wanted(cfg) {
		t.Error("off by default")
	}
	cfg.Settings.FakeIPEnabled = true
	if !Wanted(cfg) {
		t.Error("fakeip on")
	}
	cfg.Settings.DontTouchDHCP = true
	if Wanted(cfg) {
		t.Error("dont_touch_dhcp must win")
	}
}
