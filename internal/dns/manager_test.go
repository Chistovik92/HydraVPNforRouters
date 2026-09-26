package dns

import (
	"context"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestServerAddress(t *testing.T) {
	tests := []struct {
		in, addr, network string
		ok                bool
	}{
		{"77.88.8.8", "77.88.8.8:53", "udp", true},
		{"77.88.8.8:5353", "77.88.8.8:5353", "udp", true},
		{"2a02:6b8::feed:0ff", "[2a02:6b8::feed:ff]:53", "udp", true},
		{"[2a02:6b8::feed:ff]", "[2a02:6b8::feed:ff]:53", "udp", true},
		{"tcp://1.1.1.1", "1.1.1.1:53", "tcp", true},
		{"tls://dns.example", "dns.example:853", "tcp-tls", true},
		{"https://dns.example/dns-query", "", "", false},
	}
	for _, tt := range tests {
		addr, network, ok := ServerAddress(tt.in)
		if tt.in == "2a02:6b8::feed:0ff" {
			// SplitHostPort fails; the address is used verbatim with port 53.
			tt.addr = "[2a02:6b8::feed:0ff]:53"
		}
		if addr != tt.addr || network != tt.network || ok != tt.ok {
			t.Errorf("ServerAddress(%q) = %q, %q, %v; want %q, %q, %v", tt.in, addr, network, ok, tt.addr, tt.network, tt.ok)
		}
	}
}

// Stop used to hold the mutex while waiting for the check loop, which
// itself needs the mutex: a check in progress deadlocked Stop.
func TestStopDuringCheck(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.DNSServers = []string{"192.0.2.1"} // TEST-NET, never answers
	cfg.Settings.BootstrapDNSServers = nil
	cfg.Settings.DNSCheckInterval = 10 * time.Millisecond
	cfg.Settings.DNSCheckTimeout = 300 * time.Millisecond

	m := NewManager(Options{Config: cfg})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let a check start

	done := make(chan struct{})
	go func() { m.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop deadlocked")
	}
}
