package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

const sampleLinks = "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=reality&sni=www.microsoft.com&pbk=KEY&sid=ab&type=tcp&flow=xtls-rprx-vision#%D0%A1%D0%B5%D1%80%D0%B2%D0%B5%D1%80%201\n" +
	"trojan://secret@t.example.com:8443?sni=t.example.com#test\n" +
	"ss://" + "YWVzLTI1Ni1nY206cGFzcw" + "@1.2.3.4:8388#ss-node\n"

func TestParseSubscriptionBase64WithTrailingNewline(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte(sampleLinks)) + "\n"

	obs, err := ParseSubscription(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 3 {
		t.Fatalf("got %d outbounds, want 3", len(obs))
	}

	if obs[0].Type != "vless" || obs[0].Name != "Сервер 1" || obs[0].PublicKey != "KEY" || obs[0].Port != 443 {
		t.Errorf("vless parsed wrong: %+v", obs[0])
	}
	// A name that happens to be valid base64 must not be decoded.
	if obs[1].Name != "test" || obs[1].Password != "secret" || obs[1].UUID != "" {
		t.Errorf("trojan parsed wrong: %+v", obs[1])
	}
	if obs[2].Type != "shadowsocks" || obs[2].Method != "aes-256-gcm" || obs[2].Password != "pass" {
		t.Errorf("shadowsocks parsed wrong: %+v", obs[2])
	}
}

func TestParseVMess(t *testing.T) {
	js := `{"v":"2","ps":"vm","add":"v.example.com","port":"443","id":"uuid-1","net":"ws","tls":"tls","path":"/ws"}`
	obs, err := ParseSubscription("vmess://" + base64.StdEncoding.EncodeToString([]byte(js)))
	if err != nil {
		t.Fatal(err)
	}
	ob := obs[0]
	if ob.Type != "vmess" || ob.Server != "v.example.com" || ob.Port != 443 || ob.UUID != "uuid-1" || ob.Transport != "ws" || ob.Name != "vm" {
		t.Errorf("vmess parsed wrong: %+v", ob)
	}
}

func TestParseSubscriptionEmpty(t *testing.T) {
	if _, err := ParseSubscription("<html>not a subscription</html>"); err == nil {
		t.Fatal("expected error for a body without proxies")
	}
}

// Start must pick up subscriptions from the config (previously only Reload did)
// and Stop must not deadlock with an in-flight update.
func TestStartFetchesConfiguredSubscriptions(t *testing.T) {
	fetched := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched <- r.Header.Get("User-Agent")
		w.Write([]byte(sampleLinks))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.SubscriptionURLs = []config.SubscriptionURL{{Section: "main", URL: srv.URL, SubscriptionUpdateEnabled: true}}

	m := NewManager(Options{Config: cfg})
	m.cacheDir = t.TempDir()
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case ua := <-fetched:
		if !strings.HasPrefix(ua, "HydraVPNRouter/") {
			t.Errorf("unexpected User-Agent %q", ua)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscription was not fetched after Start")
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(m.GetOutbounds("main")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(m.GetOutbounds("main")); n != 3 {
		t.Errorf("got %d outbounds, want 3", n)
	}

	done := make(chan struct{})
	go func() { m.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop deadlocked")
	}
}
