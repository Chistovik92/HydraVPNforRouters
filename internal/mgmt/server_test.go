package mgmt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
)

type fakeEngine struct {
	cfg     *config.Config
	reloads int
}

func (f *fakeEngine) GetStatus() map[string]interface{} {
	return map[string]interface{}{"state": "running"}
}
func (f *fakeEngine) GetConfig() *config.Config { return f.cfg }
func (f *fakeEngine) Reload(c *config.Config) error {
	f.cfg = c
	f.reloads++
	return nil
}
func (f *fakeEngine) ForceUpdateSubscription(section, url string) error { return nil }

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

func TestBruteForceIsLimited(t *testing.T) {
	ts, _, _, token := setup(t)
	for i := 0; i < 10; i++ {
		call(t, ts, "bad", "GET", "/api/v1/status", nil)
	}
	if code, _ := call(t, ts, token, "GET", "/api/v1/status", nil); code != 429 {
		t.Errorf("after 10 failures: %d, want 429", code)
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
