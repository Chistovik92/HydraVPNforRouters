package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.4", "1.2.3", 1},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.9.9", 1},
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.3-debug.1", 1}, // a release is newer than its pre-release
		{"1.2.3-debug.1", "1.2.3", -1},
		{"1.2.3-debug.2", "1.2.3-debug.10", -1},
		{"1.2.4-debug.1", "1.2.3", 1},
	}
	for _, tt := range tests {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct{ goos, goarch, arm, want string }{
		{"linux", "amd64", "", "hydravpn-router-1.2.4-linux-amd64"},
		{"linux", "mipsle", "", "hydravpn-router-1.2.4-linux-mipsle"},
		{"linux", "arm", "7", "hydravpn-router-1.2.4-linux-armv7"},
		{"linux", "arm", "6", "hydravpn-router-1.2.4-linux-armv6"},
		{"windows", "amd64", "", "hydravpn-router-1.2.4-windows-amd64.exe"},
	}
	for _, tt := range tests {
		got, err := AssetName("1.2.4", tt.goos, tt.goarch, tt.arm)
		if err != nil || got != tt.want {
			t.Errorf("AssetName(%s/%s) = %q, %v; want %q", tt.goos, tt.goarch, got, err, tt.want)
		}
	}
	if _, err := AssetName("1.2.4", "darwin", "arm64", ""); err == nil {
		t.Error("darwin has no release build")
	}
}

// fakeGitHub serves the releases API, checksums.txt and the asset.
type fakeGitHub struct {
	latest   string
	content  []byte
	checksum string // overrides the real checksum when set
	noSum    bool
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	asset, _ := AssetName(f.latest, "linux", "amd64", "")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/"+Repo+"/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v%s","prerelease":false}`, f.latest)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			sum := sha256.Sum256(f.content)
			s := hex.EncodeToString(sum[:])
			if f.checksum != "" {
				s = f.checksum
			}
			if !f.noSum {
				fmt.Fprintf(w, "%s  %s\n", s, asset)
			}
			fmt.Fprintf(w, "%s  hydravpn-router-%s-linux-arm64\n", strings.Repeat("0", 64), f.latest)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			w.Write(f.content)
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestUpdater(t *testing.T, srv *httptest.Server, current string) (*Updater, string, chan struct{}) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hydravpn-router")
	if err := os.WriteFile(bin, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	restarted := make(chan struct{}, 1)
	no := false
	u := New(Options{
		Current:      current,
		Binary:       bin,
		APIBase:      srv.URL,
		DownloadBase: srv.URL,
		Client:       srv.Client(),
		GOOS:         "linux",
		GOARCH:       "amd64",
		Container:    &no,
		Restart:      func() { restarted <- struct{}{} },
		Verify:       func(ctx context.Context, bin, ver string) error { return nil },
	})
	return u, bin, restarted
}

func waitDone(t *testing.T, u *Updater) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		u.mu.Lock()
		busy := u.busy
		u.mu.Unlock()
		if !busy {
			return u.Status()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("update did not finish")
	return Status{}
}

func TestUpdateInstallsAndRestarts(t *testing.T) {
	gh := &fakeGitHub{latest: "1.2.5", content: []byte("new binary")}
	srv := gh.server(t)
	defer srv.Close()
	u, bin, restarted := newTestUpdater(t, srv, "1.2.4")

	st, err := u.Check(context.Background())
	if err != nil || !st.Available || st.Latest != "1.2.5" {
		t.Fatalf("Check = %+v, %v", st, err)
	}

	if err := u.Start(""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("not restarted: %+v", u.Status())
	}
	st = waitDone(t, u)
	if st.State != StateRestarting || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	if data, _ := os.ReadFile(bin); string(data) != "new binary" {
		t.Fatalf("binary not replaced: %q", data)
	}
	if _, err := os.Stat(bin + ".new"); !os.IsNotExist(err) {
		t.Error("temporary file left behind")
	}
}

func TestUpdateRefusesBadChecksum(t *testing.T) {
	for name, gh := range map[string]*fakeGitHub{
		"mismatch": {latest: "1.2.5", content: []byte("tampered"), checksum: strings.Repeat("a", 64)},
		"missing":  {latest: "1.2.5", content: []byte("new binary"), noSum: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := gh.server(t)
			defer srv.Close()
			u, bin, restarted := newTestUpdater(t, srv, "1.2.4")
			if err := u.Start(""); err != nil {
				t.Fatal(err)
			}
			st := waitDone(t, u)
			if st.State != StateFailed || st.Error == "" {
				t.Fatalf("status = %+v", st)
			}
			if data, _ := os.ReadFile(bin); string(data) != "old binary" {
				t.Fatal("binary replaced despite a bad checksum")
			}
			if _, err := os.Stat(bin + ".new"); !os.IsNotExist(err) {
				t.Error("temporary file left behind")
			}
			select {
			case <-restarted:
				t.Fatal("restarted after a failed update")
			default:
			}
		})
	}
}

func TestUpdateUpToDate(t *testing.T) {
	gh := &fakeGitHub{latest: "1.2.4", content: []byte("x")}
	srv := gh.server(t)
	defer srv.Close()
	u, _, _ := newTestUpdater(t, srv, "1.2.4")

	if err := u.Start("1.2.4"); !errors.Is(err, ErrUpToDate) {
		t.Fatalf("Start(current) = %v", err)
	}
	if err := u.Start(""); err != nil {
		t.Fatal(err)
	}
	if st := waitDone(t, u); st.State != StateIdle || st.Error != "" || st.Available {
		t.Fatalf("status = %+v", st)
	}
}

func TestUpdateFailedVerification(t *testing.T) {
	gh := &fakeGitHub{latest: "1.2.5", content: []byte("wrong arch")}
	srv := gh.server(t)
	defer srv.Close()
	u, bin, _ := newTestUpdater(t, srv, "1.2.4")
	u.o.Verify = func(ctx context.Context, bin, ver string) error { return errors.New("exec format error") }

	if err := u.Start(""); err != nil {
		t.Fatal(err)
	}
	if st := waitDone(t, u); st.State != StateFailed || !strings.Contains(st.Error, "exec format") {
		t.Fatalf("status = %+v", st)
	}
	if data, _ := os.ReadFile(bin); string(data) != "old binary" {
		t.Fatal("binary replaced although the new one does not run")
	}
}

func TestUnsupportedInContainer(t *testing.T) {
	yes := true
	u := New(Options{Current: "1.2.4", Binary: "/usr/bin/hydravpn-router", Container: &yes, GOOS: "linux", GOARCH: "amd64"})
	if st := u.Status(); st.Supported || st.Unsupported == "" {
		t.Fatalf("status = %+v", st)
	}
	if err := u.Start(""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Start = %v", err)
	}
}

func TestInvalidVersion(t *testing.T) {
	no := false
	u := New(Options{Current: "1.2.4", Binary: "x", Container: &no, GOOS: "linux", GOARCH: "amd64"})
	for _, v := range []string{"latest", "1.2", "1.2.4/../../x", "1.2.4-$(id)"} {
		if err := u.Start(v); err == nil {
			t.Errorf("Start(%q) accepted", v)
			waitDone(t, u)
		}
	}
}

func TestAssetNameForFlavors(t *testing.T) {
	for _, tt := range []struct{ flavor, goarch, arm, want string }{
		{"openwrt", "mipsle", "", "hydravpn-router-1.2.5-openwrt-mipsle"},
		{"keeneticos", "arm64", "", "hydravpn-router-1.2.5-keeneticos-arm64"},
		{"keeneticos", "arm", "5", "hydravpn-router-1.2.5-keeneticos-armv5"},
		{"", "amd64", "", "hydravpn-router-1.2.5-linux-amd64"},
	} {
		got, err := AssetNameFor(tt.flavor, "1.2.5", "linux", tt.goarch, tt.arm)
		if err != nil || got != tt.want {
			t.Errorf("%s/%s: %q %v want %q", tt.flavor, tt.goarch, got, err, tt.want)
		}
	}
}

func TestFlavorFallsBackToGenericAsset(t *testing.T) {
	// The release only has the generic Linux file (version before 1.2.5).
	f := &fakeGitHub{latest: "1.2.4", content: []byte("new binary")}
	srv := f.server(t)
	defer srv.Close()
	u, bin, restarted := newTestUpdater(t, srv, "1.2.3")
	u.o.Flavor = FlavorOpenWrt
	if err := u.apply(context.Background(), "1.2.4"); err != nil {
		t.Fatal(err)
	}
	<-restarted
	if got, _ := os.ReadFile(bin); string(got) != "new binary" {
		t.Errorf("binary not replaced: %q", got)
	}
}
