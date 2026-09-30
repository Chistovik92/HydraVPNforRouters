package mgmt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
	"github.com/Chistovik92/hydravpn-router/internal/radar"
)

// radarSetup starts the router API against a fake bot. The bot hands out one
// subscription; revoke makes it refuse the device token (disconnected in the bot).
func radarSetup(t *testing.T) (ts *httptest.Server, eng *fakeEngine, dir, token string, revoke *atomic.Bool, logouts *atomic.Int32) {
	t.Helper()
	revoke, logouts = &atomic.Bool{}, &atomic.Int32{}
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app/link":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["code"] != "12345678" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"code is wrong"}`))
				return
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"token":"device-token","device_id":"a1"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer device-token" || revoke.Load() {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"token rejected"}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/app/me":
			w.Write([]byte(`{"username":"ivan"}`))
		case "/api/v1/app/subscriptions":
			w.Write([]byte(`{"subscriptions":[{"panel":"1","title":"Main","link_kind":"subscription","state":"ok","enabled":true,"url":"https://sub.example/xyz"},{"panel":"2","title":"Key","link_kind":"key","state":"ok","enabled":true,"url":"ss://k"}]}`))
		case "/api/v1/app/session":
			logouts.Add(1)
			w.Write([]byte(`{"status":"revoked"}`))
		}
	}))
	t.Cleanup(bot.Close)

	dir = t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig()
	cfg.Settings.APIListen = "127.0.0.1:0"
	cfg.Settings.APIToken = "secret-token-1234567890"
	cfg.Sections = []config.Section{{Name: "main", Enabled: true}}
	if err := cfg.SaveToFile(cfgFile); err != nil {
		t.Fatal(err)
	}
	eng = &fakeEngine{cfg: cfg}
	logger, _ := logx.New(logx.Options{Out: &bytes.Buffer{}})
	s, err := New(Options{Engine: eng, Logger: logger, ConfigFile: cfgFile, RuntimeDir: dir, Radar: radar.NewClient()})
	if err != nil || s == nil {
		t.Fatalf("New: %v", err)
	}
	ts = httptest.NewServer(s.srv.Handler)
	t.Cleanup(ts.Close)
	t.Setenv("RADAR_TEST_BOT", bot.URL)
	return ts, eng, dir, "secret-token-1234567890", revoke, logouts
}

func botURL(t *testing.T) string {
	t.Helper()
	// Plain http to 127.0.0.1 is allowed: it is a local address.
	return strings.TrimSpace(os.Getenv("RADAR_TEST_BOT"))
}

func TestRadarRequiresToken(t *testing.T) {
	ts, _, _, _, _, _ := radarSetup(t)
	// Every refused request is slowed down, so two paths are enough: the guard is shared.
	for _, p := range []struct{ m, path string }{
		{"POST", "/api/v1/radar/link"}, {"DELETE", "/api/v1/radar"},
	} {
		if code, _ := call(t, ts, "", p.m, p.path, nil); code != 401 {
			t.Errorf("%s %s without token: %d", p.m, p.path, code)
		}
	}
}

func TestRadarLinkSyncUnlink(t *testing.T) {
	ts, eng, dir, token, _, logouts := radarSetup(t)
	bot := botURL(t)

	if code, body := call(t, ts, token, "GET", "/api/v1/radar", nil); code != 200 || !strings.Contains(body, `"linked":false`) {
		t.Fatalf("status before: %d %s", code, body)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/radar/sync", nil); code != 409 {
		t.Errorf("sync while not linked: %d, want 409", code)
	}

	// A wrong code is 422 here (not 401: that would read as a wrong router token).
	if code, _ := call(t, ts, token, "POST", "/api/v1/radar/link", map[string]string{"server": bot, "code": "00000000"}); code != 422 {
		t.Errorf("wrong code: %d, want 422", code)
	}
	if st, _ := radar.LoadState(dir); st != nil {
		t.Fatal("a failed link left a state behind")
	}
	// http to a public name is refused before anything is sent.
	if code, _ := call(t, ts, token, "POST", "/api/v1/radar/link", map[string]string{"server": "http://radar.example.org", "code": "12345678"}); code != 400 {
		t.Errorf("plain http to a public host: %d, want 400", code)
	}

	code, body := call(t, ts, token, "POST", "/api/v1/radar/link", map[string]string{"server": bot, "code": "1234-5678"})
	if code != 201 || !strings.Contains(body, `"added":1`) || !strings.Contains(body, `"skipped":1`) {
		t.Fatalf("link: %d %s", code, body)
	}
	if strings.Contains(body, "device-token") {
		t.Error("the device token leaked into the link answer")
	}
	if len(eng.cfg.SubscriptionURLs) != 1 || eng.cfg.SubscriptionURLs[0].URL != "https://sub.example/xyz" || eng.reloads != 1 {
		t.Fatalf("config after link: %+v, reloads %d", eng.cfg.SubscriptionURLs, eng.reloads)
	}

	_, body = call(t, ts, token, "GET", "/api/v1/radar", nil)
	if !strings.Contains(body, `"linked":true`) || !strings.Contains(body, "ivan") || strings.Contains(body, "device-token") {
		t.Errorf("status after: %s", body)
	}

	// A second sync finds nothing new and does not reload.
	code, body = call(t, ts, token, "POST", "/api/v1/radar/sync", nil)
	if code != 200 || !strings.Contains(body, `"added":0`) || eng.reloads != 1 {
		t.Errorf("second sync: %d %s, reloads %d", code, body, eng.reloads)
	}

	if code, _ := call(t, ts, token, "DELETE", "/api/v1/radar", nil); code != 200 {
		t.Errorf("unlink: %d", code)
	}
	if logouts.Load() != 1 {
		t.Errorf("bot logouts: %d, want 1", logouts.Load())
	}
	if st, _ := radar.LoadState(dir); st != nil {
		t.Error("state survived unlink")
	}
	if len(eng.cfg.SubscriptionURLs) != 1 {
		t.Error("unlink must keep the subscriptions already added")
	}
}

func TestRadarDisconnectedInBotDropsTheLink(t *testing.T) {
	ts, _, dir, token, revoke, _ := radarSetup(t)
	bot := botURL(t)
	if code, _ := call(t, ts, token, "POST", "/api/v1/radar/link", map[string]string{"server": bot, "code": "12345678"}); code != 201 {
		t.Fatalf("link: %d", code)
	}
	revoke.Store(true)
	code, body := call(t, ts, token, "POST", "/api/v1/radar/sync", nil)
	if code != 409 || !strings.Contains(body, "link it again") {
		t.Errorf("sync after revoke: %d %s", code, body)
	}
	if st, _ := radar.LoadState(dir); st != nil {
		t.Error("the dead token stays on disk")
	}
}
