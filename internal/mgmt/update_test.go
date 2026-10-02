package mgmt

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
	"github.com/Chistovik92/hydravpn-router/internal/selfupdate"
)

type fakeUpdater struct {
	st      selfupdate.Status
	started []string
	err     error
}

func (f *fakeUpdater) Status() selfupdate.Status { return f.st }
func (f *fakeUpdater) Check(ctx context.Context) (selfupdate.Status, error) {
	f.st.Latest, f.st.Available = "1.2.5", true
	return f.st, nil
}
func (f *fakeUpdater) Start(v string) error {
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, v)
	f.st.State = selfupdate.StateDownloading
	return nil
}

func setupUpdate(t *testing.T, u Updater) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Settings.APIListen = "127.0.0.1:0"
	cfg.Settings.APIToken = "secret-token-1234567890"
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := cfg.SaveToFile(cfgFile); err != nil {
		t.Fatal(err)
	}
	logger, _ := logx.New(logx.Options{Out: &bytes.Buffer{}})
	s, err := New(Options{Engine: &fakeEngine{cfg: cfg}, Logger: logger, ConfigFile: cfgFile, RuntimeDir: dir, Updater: u})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, "secret-token-1234567890"
}

func TestUpdateEndpoints(t *testing.T) {
	u := &fakeUpdater{st: selfupdate.Status{Current: "1.2.4", State: "idle", Supported: true}}
	ts, tok := setupUpdate(t, u)

	if code, body := call(t, ts, "", "POST", "/api/v1/update", nil); code != 401 {
		t.Fatalf("update without token: %d %s", code, body)
	}
	if code, body := call(t, ts, tok, "GET", "/api/v1/update", nil); code != 200 || !strings.Contains(body, `"current":"1.2.4"`) {
		t.Fatalf("GET update: %d %s", code, body)
	}
	if code, body := call(t, ts, tok, "POST", "/api/v1/update/check", nil); code != 200 || !strings.Contains(body, `"update_available":true`) {
		t.Fatalf("check: %d %s", code, body)
	}
	// No body installs the latest release; a body picks a version.
	if code, body := call(t, ts, tok, "POST", "/api/v1/update", nil); code != 202 {
		t.Fatalf("start: %d %s", code, body)
	}
	if code, body := call(t, ts, tok, "POST", "/api/v1/update", map[string]string{"version": "1.2.3"}); code != 202 {
		t.Fatalf("start with version: %d %s", code, body)
	}
	if len(u.started) != 2 || u.started[0] != "" || u.started[1] != "1.2.3" {
		t.Fatalf("started = %q", u.started)
	}
	if code, body := call(t, ts, tok, "GET", "/api/v1/status", nil); code != 200 || !strings.Contains(body, `"update":{`) {
		t.Fatalf("status lacks update: %d %s", code, body)
	}

	u.err = selfupdate.ErrBusy
	if code, _ := call(t, ts, tok, "POST", "/api/v1/update", nil); code != 409 {
		t.Errorf("busy: %d", code)
	}
	u.err = selfupdate.ErrUnsupported
	if code, _ := call(t, ts, tok, "POST", "/api/v1/update", nil); code != 501 {
		t.Errorf("unsupported: %d", code)
	}
	if code, _ := call(t, ts, tok, "POST", "/api/v1/update", map[string]string{"bogus": "x"}); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
}

func TestUpdateDisabledWithoutUpdater(t *testing.T) {
	ts, tok := setupUpdate(t, nil)
	if code, _ := call(t, ts, tok, "POST", "/api/v1/update", nil); code != 501 {
		t.Errorf("without updater: %d", code)
	}
}
