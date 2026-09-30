package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
)

type fakeEngine struct {
	cfg     *config.Config
	reloads int
	block   chan struct{} // if set, Reload waits for it
}

func (f *fakeEngine) GetStatus() map[string]interface{} {
	return map[string]interface{}{"state": "running"}
}
func (f *fakeEngine) GetConfig() *config.Config { return f.cfg }
func (f *fakeEngine) Reload(c *config.Config) error {
	if f.block != nil {
		<-f.block
	}
	f.cfg = c
	f.reloads++
	return nil
}
func (f *fakeEngine) ForceUpdateSubscription(section, url string) error { return nil }
func (f *fakeEngine) Restart() error                                    { return nil }

func setup(t *testing.T) (*httptest.Server, *fakeEngine, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig()
	cfg.Settings.APIListen = "127.0.0.1:0"
	cfg.Settings.APIToken = "secret-token-1234567890"
	cfg.Sections = []config.Section{{Name: "main", Enabled: true}}
	if err := cfg.SaveToFile(cfgFile); err != nil {
		t.Fatal(err)
	}
	eng := &fakeEngine{cfg: cfg}
	logger, _ := logx.New(logx.Options{Out: &bytes.Buffer{}})
	s, err := New(Options{Engine: eng, Logger: logger, ConfigFile: cfgFile, RuntimeDir: dir})
	if err != nil || s == nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(s.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, eng, cfgFile, "secret-token-1234567890"
}

func call(t *testing.T, ts *httptest.Server, token, method, path string, body interface{}) (int, string) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func TestAuthRequired(t *testing.T) {
	ts, _, _, token := setup(t)
	if code, _ := call(t, ts, "", "GET", "/api/v1/status", nil); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code, _ := call(t, ts, "wrong", "GET", "/api/v1/status", nil); code != 401 {
		t.Errorf("wrong token: %d", code)
	}
	if code, _ := call(t, ts, token, "GET", "/api/v1/status", nil); code != 200 {
		t.Errorf("good token: %d", code)
	}
	// The UI shell needs no token, the data does.
	if code, body := call(t, ts, "", "GET", "/", nil); code != 200 || !strings.Contains(body, "HydraVPN") {
		t.Errorf("ui: %d", code)
	}
}

func TestWrongTokensNeverLockOutTheRightOne(t *testing.T) {
	ts, _, _, token := setup(t)
	// Many wrong guesses from the same address (all test requests share one).
	for i := 0; i < 12; i++ {
		go statusOf(ts, "bad")
	}
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	if code, _ := call(t, ts, token, "GET", "/api/v1/status", nil); code != 200 {
		t.Errorf("valid token rejected while wrong ones are being tried: %d", code)
	}
	if time.Since(start) > time.Second {
		t.Errorf("valid token was delayed: %v", time.Since(start))
	}
}

func TestWrongTokensAreSlowedAndCapped(t *testing.T) {
	ts, _, _, _ := setup(t)
	// The fifth parallel wrong attempt is refused at once instead of waiting.
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		go func() { codes <- statusOf(ts, "bad") }()
	}
	got429 := 0
	timeout := time.After(15 * time.Second)
	for i := 0; i < 8; i++ {
		select {
		case c := <-codes:
			if c == 429 {
				got429++
			} else if c != 401 {
				t.Errorf("unexpected status %d", c)
			}
		case <-timeout:
			t.Fatal("requests hung")
		}
	}
	if got429 == 0 {
		t.Error("no request was refused although more than the parallel limit was in flight")
	}
}

func TestQueryTokenOnlyOnStream(t *testing.T) {
	ts, _, _, token := setup(t)
	resp, err := http.Get(ts.URL + "/api/v1/status?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("?token= accepted on a normal path: %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/logs/stream?token="+token, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream with ?token= must be accepted: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("stream: %d", resp.StatusCode)
	}
}

func TestShortConfiguredTokenIsRejected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.APIListen = "127.0.0.1:0"
	cfg.Settings.APIToken = "short"
	if _, err := New(Options{Engine: &fakeEngine{cfg: cfg}, RuntimeDir: t.TempDir()}); err == nil {
		t.Error("a 5-character token must be refused")
	}
}

func TestConfigEndpointMasksSecrets(t *testing.T) {
	ts, eng, _, token := setup(t)
	eng.cfg.SubscriptionURLs = []config.SubscriptionURL{{Section: "main", URL: "https://panel.example/sub/TOPSECRET"}}
	_, body := call(t, ts, token, "GET", "/api/v1/config", nil)
	if strings.Contains(body, "TOPSECRET") || strings.Contains(body, token) {
		t.Errorf("secrets leaked: %s", body)
	}
}

func TestAddAndDeleteSubscriptionPersistsAndReloads(t *testing.T) {
	ts, eng, cfgFile, token := setup(t)

	code, body := call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "main", "url": "https://panel.example/sub/abc"})
	if code != 201 {
		t.Fatalf("add: %d %s", code, body)
	}
	saved, err := config.LoadFromFile(cfgFile)
	if err != nil || len(saved.SubscriptionURLs) != 1 || !saved.SubscriptionURLs[0].SubscriptionUpdateEnabled || eng.reloads != 1 {
		t.Fatalf("saved=%+v err=%v reloads=%d", saved.SubscriptionURLs, err, eng.reloads)
	}
	if _, err := os.Stat(cfgFile + ".bak"); err != nil {
		t.Error("no backup of the previous config")
	}

	if code, _ := call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "nope", "url": "https://x.example/s"}); code != 400 {
		t.Errorf("unknown section accepted: %d", code)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "main", "url": "ftp://x"}); code != 400 {
		t.Errorf("bad url accepted: %d", code)
	}
	if code, _ := call(t, ts, token, "DELETE", "/api/v1/subscriptions/0", nil); code != 200 {
		t.Errorf("delete: %d", code)
	}
	saved, _ = config.LoadFromFile(cfgFile)
	if len(saved.SubscriptionURLs) != 0 {
		t.Error("subscription not removed")
	}
}

func TestAddedServerAppearsInList(t *testing.T) {
	ts, _, _, token := setup(t)
	if code, b := call(t, ts, token, "POST", "/api/v1/servers", map[string]interface{}{"name": "in1", "enabled": true, "protocol": "vless", "listen_port": 8443}); code != 201 {
		t.Fatalf("add server: %d %s", code, b)
	}
	if _, body := call(t, ts, token, "GET", "/api/v1/servers", nil); !strings.Contains(body, "in1") {
		t.Errorf("server missing: %s", body)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/servers", map[string]interface{}{"name": "in1"}); code != 400 {
		t.Error("duplicate server accepted")
	}
}

func TestGeneratedTokenIsStable(t *testing.T) {
	dir := t.TempDir()
	a, err := resolveToken("", dir)
	if err != nil || len(a) < 32 {
		t.Fatalf("token %q %v", a, err)
	}
	b, _ := resolveToken("", dir)
	if a != b {
		t.Error("token changed between calls")
	}
	if c, _ := resolveToken("fixed", dir); c != "fixed" {
		t.Error("configured token ignored")
	}
}

func TestDisabledWithoutListen(t *testing.T) {
	cfg := config.DefaultConfig()
	s, err := New(Options{Engine: &fakeEngine{cfg: cfg}, RuntimeDir: t.TempDir()})
	if s != nil || err != nil {
		t.Errorf("api must be off without api_listen: %v %v", s, err)
	}
}

func TestSectionsCRUDAndReferentialCheck(t *testing.T) {
	ts, _, cfgFile, token := setup(t)
	if code, b := call(t, ts, token, "POST", "/api/v1/sections", map[string]interface{}{"name": "second", "enabled": true, "action": "bypass"}); code != 201 {
		t.Fatalf("add: %d %s", code, b)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/sections", map[string]interface{}{"name": "second"}); code != 400 {
		t.Error("duplicate section accepted")
	}
	if code, _ := call(t, ts, token, "PUT", "/api/v1/sections/second", map[string]interface{}{"enabled": false, "action": "block"}); code != 200 {
		t.Error("replace failed")
	}
	saved, _ := config.LoadFromFile(cfgFile)
	if len(saved.Sections) != 2 || saved.Sections[1].Action != "block" || saved.Sections[1].Enabled {
		t.Errorf("sections: %+v", saved.Sections)
	}
	// A section that a subscription points to cannot be deleted.
	call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "main", "url": "https://x.example/s"})
	if code, _ := call(t, ts, token, "DELETE", "/api/v1/sections/main", nil); code != 400 {
		t.Errorf("deleting a referenced section: %d", code)
	}
	if code, _ := call(t, ts, token, "DELETE", "/api/v1/sections/second", nil); code != 200 {
		t.Error("delete failed")
	}
}

func TestPairURI(t *testing.T) {
	s := config.DefaultConfig().Settings
	if _, err := PairURI(s, "192.168.1.1", "tok"); err == nil {
		t.Error("pairing without api_listen must fail")
	}
	s.APIListen = "0.0.0.0:8088"
	u, err := PairURI(s, "192.168.1.1", "tok")
	if err != nil || u != "hydravpn-router://192.168.1.1:8088?tls=0&token=tok" {
		t.Errorf("uri: %q %v", u, err)
	}
}

// statusOf is safe to call from goroutines (no testing.T).
func statusOf(ts *httptest.Server, token string) int {
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestSlowReloadDoesNotHangTheRequest(t *testing.T) {
	old := applyTimeout
	applyTimeout = 200 * time.Millisecond
	defer func() { applyTimeout = old }()

	ts, eng, cfgFile, token := setup(t)
	eng.block = make(chan struct{})
	defer close(eng.block)

	start := time.Now()
	code, body := call(t, ts, token, "POST", "/api/v1/subscriptions", map[string]string{"section": "main", "url": "https://x.example/s"})
	if code != 202 || !strings.Contains(body, "applying") {
		t.Errorf("slow apply: %d %s", code, body)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("request hung for %v", time.Since(start))
	}
	// The change is saved even though it is still being applied.
	saved, _ := config.LoadFromFile(cfgFile)
	if len(saved.SubscriptionURLs) != 1 {
		t.Error("change not saved")
	}
	// The status endpoint stays available meanwhile.
	if code, _ := call(t, ts, token, "GET", "/api/v1/status", nil); code != 200 {
		t.Errorf("status while applying: %d", code)
	}
}
