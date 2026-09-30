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

func TestParseLegacyShadowsocksAndJSONConfig(t *testing.T) {
	legacy := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass@1.2.3.4:8388")) + "#legacy"
	obs, err := ParseSubscription(legacy)
	if err != nil || len(obs) != 1 {
		t.Fatalf("legacy ss: %v %v", obs, err)
	}
	if o := obs[0]; o.Method != "aes-256-gcm" || o.Password != "pass" || o.Server != "1.2.3.4" || o.Port != 8388 || o.Name != "legacy" {
		t.Errorf("legacy ss parsed wrong: %+v", o)
	}

	doc := `{"outbounds":[{"type":"selector","tag":"sel","outbounds":["a"]},
		{"type":"vless","tag":"a","server":"h.example","server_port":443,"uuid":"u"},{"type":"direct","tag":"d"}]}`
	obs, err = ParseSubscription(doc)
	if err != nil || len(obs) != 1 || obs[0].Raw == nil || obs[0].Name != "a" {
		t.Fatalf("json config: %+v %v", obs, err)
	}
}

func TestParseUserinfoAndRedact(t *testing.T) {
	q := parseUserinfo("upload=1; download=2; total=3; expire=1700000000")
	if q == nil || q.Upload != 1 || q.Download != 2 || q.Total != 3 || q.Expire.Unix() != 1700000000 {
		t.Errorf("quota: %+v", q)
	}
	if parseUserinfo("") != nil {
		t.Error("empty header must give no quota")
	}
	if got := redactURL("https://panel.example.com/sub/SECRET-TOKEN?x=1"); strings.Contains(got, "SECRET") {
		t.Errorf("token leaked: %s", got)
	}
}

func TestUpdateNotifiesAndKeepsQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=5; download=6; total=100")
		_, _ = w.Write([]byte(sampleLinks))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.SubscriptionURLs = []config.SubscriptionURL{{Section: "main", URL: srv.URL, PrefixNodes: true, NodePrefix: "P-"}}
	updated := make(chan struct{}, 1)
	m := NewManager(Options{Config: cfg, OnUpdate: func() { updated <- struct{}{} }})
	m.cacheDir = t.TempDir()
	m.mu.Lock()
	m.syncLocked(cfg)
	m.mu.Unlock()

	if err := m.ForceUpdate("main", srv.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	default:
		t.Error("OnUpdate was not called")
	}
	obs := m.GetOutbounds("main")
	if len(obs) != 3 || !strings.HasPrefix(obs[0].Name, "P-") {
		t.Errorf("outbounds/prefix: %+v", obs)
	}
	if st := m.GetStatus(); !strings.Contains(m.GetStatusJSON(), `"total": 100`) || st["update_count"] != 1 {
		t.Errorf("status: %s", m.GetStatusJSON())
	}
}
