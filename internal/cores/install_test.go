package cores

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tarGz(t *testing.T, name, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// fakeGitHub serves releases of sing-box and Xray with real archives.
type fakeGitHub struct {
	srv      *httptest.Server
	files    map[string][]byte
	releases map[string][]ghRelease
	badSum   bool
	hits     atomic.Int32
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{files: map[string][]byte{}, releases: map[string][]ghRelease{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		p := r.URL.Path
		if data, ok := f.files[p]; ok {
			w.Write(data)
			return
		}
		for repo, rels := range f.releases {
			if p == "/repos/"+repo+"/releases" {
				json.NewEncoder(w).Encode(rels)
				return
			}
			for _, rel := range rels {
				if p == "/repos/"+repo+"/releases/tags/"+rel.Tag {
					json.NewEncoder(w).Encode(rel)
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) add(repo, tag string, pre bool, assets map[string][]byte) {
	rel := ghRelease{Tag: tag, Prerelease: pre, Published: time.Now().Format(time.RFC3339)}
	for name, data := range assets {
		path := "/dl/" + tag + "/" + name
		f.files[path] = data
		digest := sum(data)
		if f.badSum {
			digest = sum([]byte("other"))
		}
		rel.Assets = append(rel.Assets, ghAsset{Name: name, URL: f.srv.URL + path, Digest: digest})
	}
	f.releases[repo] = append(f.releases[repo], rel)
}

func newTestInstaller(t *testing.T, gh *fakeGitHub, goarch string) (*Installer, chan string) {
	done := make(chan string, 4)
	inst := NewInstaller(InstallerOptions{
		Dir: t.TempDir(), APIBase: gh.srv.URL, Client: gh.srv.Client(), GOARCH: goarch, GOARM: "7",
		Verify: func(ctx context.Context, bin, version string) error {
			data, err := os.ReadFile(bin)
			if err != nil || !strings.Contains(string(data), version) {
				return fmt.Errorf("binary %s does not report %s", bin, version)
			}
			return nil
		},
		Installed: func(k Kind, version, bin string) { done <- string(k) + " " + version },
	})
	return inst, done
}

func waitDone(t *testing.T, inst *Installer, k Kind) InstallStatus {
	t.Helper()
	for i := 0; i < 200; i++ {
		st := inst.Status()[k]
		if st.State == "done" || st.State == "failed" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: install did not finish", k)
	return InstallStatus{}
}

func TestInstallSingBoxVersionsAndRollback(t *testing.T) {
	gh := newFakeGitHub(t)
	for _, v := range []string{"1.14.3", "1.15.0-alpha.12", "1.13.9"} {
		name := "sing-box-" + v + "-linux-arm64.tar.gz"
		gh.add("SagerNet/sing-box", "v"+v, strings.Contains(v, "-"), map[string][]byte{
			name: tarGz(t, "sing-box-"+v+"-linux-arm64/sing-box", "sing-box version "+v),
			// A file for another device is not offered.
			"sing-box-" + v + "-linux-amd64.tar.gz": []byte("x"),
		})
	}
	inst, done := newTestInstaller(t, gh, "arm64")

	rels, err := inst.Releases(context.Background(), SingBox)
	if err != nil || len(rels) != 3 || rels[0].Version != "1.15.0-alpha.12" || !rels[0].Prerelease || rels[1].Version != "1.14.3" {
		t.Fatalf("releases: %v %+v", err, rels)
	}

	for _, v := range []string{"1.14.3", "1.15.0-alpha.12", "1.13.9"} {
		if err := inst.Start(SingBox, v); err != nil {
			t.Fatal(err)
		}
		if st := waitDone(t, inst, SingBox); st.State != "done" {
			t.Fatalf("%s: %+v", v, st)
		}
		if got := <-done; got != "sing-box "+v {
			t.Errorf("callback %q", got)
		}
		data, _ := os.ReadFile(inst.Link(SingBox))
		if !strings.Contains(string(data), v) {
			t.Errorf("link points at %q, want %s", data, v)
		}
		time.Sleep(20 * time.Millisecond) // distinct mtimes for pruning
	}
	if kept := inst.Versions(SingBox); len(kept) != keepVersions {
		t.Errorf("kept %v, want %d versions", kept, keepVersions)
	}

	// Rolling back to a kept version needs no network.
	before := gh.hits.Load()
	inst.Start(SingBox, "1.15.0-alpha.12")
	if st := waitDone(t, inst, SingBox); st.State != "done" || gh.hits.Load() != before {
		t.Errorf("rollback: %+v, %d requests", st, gh.hits.Load()-before)
	}
}

func TestInstallXrayPicksSoftfloatOnMips(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.add("XTLS/Xray-core", "v26.10.10", true, map[string][]byte{
		"Xray-linux-mips32le.zip": zipOf(t, map[string]string{"xray": "hardfloat 26.10.10", "xray_softfloat": "softfloat 26.10.10", "geoip.dat": "x"}),
	})
	inst, _ := newTestInstaller(t, gh, "mipsle")
	if err := inst.Start(Xray, "26.10.10"); err != nil {
		t.Fatal(err)
	}
	if st := waitDone(t, inst, Xray); st.State != "done" {
		t.Fatalf("%+v", st)
	}
	data, _ := os.ReadFile(inst.Link(Xray))
	if !strings.HasPrefix(string(data), "softfloat") {
		t.Errorf("installed %q", data)
	}
	if filepath.Base(inst.Link(Xray)) != "xray" {
		t.Errorf("link %s", inst.Link(Xray))
	}
}

func TestInstallRefusesABadChecksum(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.badSum = true
	gh.add("SagerNet/sing-box", "v1.14.3", false, map[string][]byte{
		"sing-box-1.14.3-linux-amd64.tar.gz": tarGz(t, "d/sing-box", "sing-box version 1.14.3"),
	})
	inst, _ := newTestInstaller(t, gh, "amd64")
	inst.Start(SingBox, "1.14.3")
	st := waitDone(t, inst, SingBox)
	if st.State != "failed" || !strings.Contains(st.Error, "checksum") {
		t.Errorf("%+v", st)
	}
	if _, err := os.Lstat(inst.Link(SingBox)); err == nil {
		t.Error("a binary with a wrong checksum became active")
	}
	if err := inst.Start(OpenFlux, "1"); err == nil {
		t.Error("openflux version accepted")
	}
}

func TestAssetNames(t *testing.T) {
	for _, c := range []struct {
		k            Kind
		arch, arm, w string
	}{
		{SingBox, "mipsle", "", "sing-box-1.0.0-linux-mipsle-softfloat.tar.gz"},
		{SingBox, "arm", "7", "sing-box-1.0.0-linux-armv7.tar.gz"},
		{Xray, "arm", "7", "Xray-linux-arm32-v7a.zip"},
		{Xray, "amd64", "", "Xray-linux-64.zip"},
		{Xray, "arm64", "", "Xray-linux-arm64-v8a.zip"},
	} {
		if got, err := assetName(c.k, "1.0.0", c.arch, c.arm); err != nil || got != c.w {
			t.Errorf("%s %s/%s: %s %v", c.k, c.arch, c.arm, got, err)
		}
	}
	if _, err := assetName(Xray, "1", "wasm", ""); err == nil {
		t.Error("unknown arch accepted")
	}
}
