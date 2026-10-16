package mgmt

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
)

type fakeInstaller struct {
	started   []string
	installed func(k cores.Kind, version, bin string)
	busy      bool
}

func (f *fakeInstaller) Releases(ctx context.Context, k cores.Kind) ([]cores.Release, error) {
	return []cores.Release{{Version: "1.15.0-beta.1", Prerelease: true}, {Version: "1.14.3"}}, nil
}
func (f *fakeInstaller) Start(k cores.Kind, v string) error {
	if f.busy {
		return cores.ErrInstallBusy
	}
	f.started = append(f.started, string(k)+" "+v)
	return nil
}
func (f *fakeInstaller) Status() map[cores.Kind]cores.InstallStatus {
	return map[cores.Kind]cores.InstallStatus{cores.SingBox: {State: "idle"}, cores.Xray: {State: "idle"}}
}
func (f *fakeInstaller) Versions(k cores.Kind) []string                          { return nil }
func (f *fakeInstaller) Link(k cores.Kind) string                                { return "/opt/cores/bin/" + cores.BinaryName(k) }
func (f *fakeInstaller) SetInstalled(fn func(k cores.Kind, version, bin string)) { f.installed = fn }

func TestCoreEndpoints(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig()
	cfg.Settings.APIListen = "127.0.0.1:0"
	cfg.Settings.APIToken = "secret-token-1234567890"
	cfg.SaveToFile(cfgFile)
	eng := &fakeEngine{cfg: cfg}
	logger, _ := logx.New(logx.Options{Out: &bytes.Buffer{}})
	fi := &fakeInstaller{}
	s, err := New(Options{Engine: eng, Logger: logger, ConfigFile: cfgFile, RuntimeDir: dir, Cores: fi})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.srv.Handler)
	defer ts.Close()
	token := "secret-token-1234567890"

	if code, body := call(t, ts, token, "GET", "/api/v1/cores", nil); code != 200 || !strings.Contains(body, `"name":"sing-box"`) || !strings.Contains(body, `"name":"amneziawg"`) {
		t.Errorf("list: %d %s", code, body)
	}
	if code, body := call(t, ts, token, "GET", "/api/v1/cores/xray/versions", nil); code != 200 || !strings.Contains(body, "1.15.0-beta.1") {
		t.Errorf("versions: %d %s", code, body)
	}
	if code, _ := call(t, ts, token, "GET", "/api/v1/cores/nope/versions", nil); code != 404 {
		t.Errorf("unknown core versions: %d", code)
	}
	if code, _ := call(t, ts, token, "POST", "/api/v1/cores/sing-box/install", map[string]string{"version": "1.15.0-beta.1"}); code != 202 || len(fi.started) != 1 {
		t.Errorf("install: %d %v", code, fi.started)
	}
	fi.busy = true
	if code, _ := call(t, ts, token, "POST", "/api/v1/cores/sing-box/install", map[string]string{"version": "1.14.3"}); code != 409 {
		t.Errorf("busy: %d", code)
	}

	// A new sing-box version points the service at the managed link.
	fi.installed(cores.SingBox, "1.15.0-beta.1", "/opt/cores/bin/sing-box")
	if eng.cfg.Settings.SingBoxBinary != "/opt/cores/bin/sing-box" {
		t.Errorf("singbox_binary = %q", eng.cfg.Settings.SingBoxBinary)
	}
}
