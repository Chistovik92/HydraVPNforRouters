package selftest

import (
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTProxyDetection(t *testing.T) {
	// Nothing on the system: fail.
	env := Env{Root: t.TempDir()}
	if r := checkTProxy(env); r.Status != Fail {
		t.Errorf("empty system: %+v", r)
	}

	// Loaded module.
	root := t.TempDir()
	write(t, root, "proc/modules", "nft_tproxy 16384 0 - Live 0x0000\n")
	if r := checkTProxy(Env{Root: root}); r.Status != Pass {
		t.Errorf("loaded: %+v", r)
	}

	// Built-in via modules.builtin.
	root = t.TempDir()
	write(t, root, "lib/modules/5.4.0/modules.builtin", "kernel/net/netfilter/xt_TPROXY.ko\n")
	if r := checkTProxy(Env{Root: root}); r.Status != Pass {
		t.Errorf("builtin: %+v", r)
	}

	// Kernel config.
	root = t.TempDir()
	p := filepath.Join(root, "proc")
	os.MkdirAll(p, 0755)
	f, _ := os.Create(filepath.Join(p, "config.gz"))
	zw := gzip.NewWriter(f)
	zw.Write([]byte("CONFIG_NFT_TPROXY=m\n"))
	zw.Close()
	f.Close()
	if r := checkTProxy(Env{Root: root}); r.Status != Pass {
		t.Errorf("config.gz: %+v", r)
	}

	// Module file present but not loaded: warn, not fail.
	root = t.TempDir()
	write(t, root, "lib/modules/5.4.0/kernel/net/netfilter/nft_tproxy.ko", "")
	if r := checkTProxy(Env{Root: root}); r.Status != Warn {
		t.Errorf("unloaded module: %+v", r)
	}
}

func TestKeeneticChecks(t *testing.T) {
	lookNone := func(string) (string, error) { return "", errors.New("no") }

	// Not a Keenetic: nothing to report.
	if r := checkKeenetic(Env{Root: t.TempDir(), Look: lookNone}); r != nil {
		t.Errorf("non-keenetic: %+v", r)
	}
	// Keenetic without Entware: fail.
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc/ndm"), 0755)
	if r := checkKeenetic(Env{Root: root, Look: lookNone}); len(r) != 1 || r[0].Status != Fail {
		t.Errorf("no entware: %+v", r)
	}
	// Entware without the hook: warn; with it: pass.
	write(t, root, "opt/bin/opkg", "")
	if r := checkKeenetic(Env{Root: root, Look: lookNone}); r[0].Status != Warn {
		t.Errorf("no hook: %+v", r)
	}
	write(t, root, "opt/etc/ndm/netfilter.d/50-hydravpn-router.sh", "")
	if r := checkKeenetic(Env{Root: root, Look: lookNone}); r[0].Status != Pass {
		t.Errorf("with hook: %+v", r)
	}
}

func TestRunReportsMissingTools(t *testing.T) {
	env := Env{
		Root: t.TempDir(),
		Look: func(string) (string, error) { return "", errors.New("not found") },
		Run: func(context.Context, string, string, ...string) (string, error) {
			return "", errors.New("must not run")
		},
	}
	res := Run(context.Background(), config.DefaultConfig(), env)
	byName := map[string]Result{}
	for _, r := range res {
		byName[r.Name] = r
	}
	if byName["sing-box"].Status != Fail || byName["ip"].Status != Fail || byName["kernel tproxy"].Status != Fail {
		t.Errorf("missing tools not reported: %+v", res)
	}
	if !Failed(res) {
		t.Error("Failed() must be true")
	}
	if runtime.GOOS == "linux" && byName["nft"].Status != Fail {
		t.Errorf("nft: %+v", byName["nft"])
	}
}

func TestRunPassesWithWorkingTools(t *testing.T) {
	var checked []string
	env := Env{
		Root: t.TempDir(),
		Look: func(n string) (string, error) {
			if n == "ndmc" {
				return "", errors.New("not a keenetic")
			}
			return "/bin/" + n, nil
		},
		Run: func(_ context.Context, stdin, name string, args ...string) (string, error) {
			checked = append(checked, name+" "+strings.Join(args[:1], ""))
			return "sing-box version 1.14.2", nil
		},
	}
	write(t, env.Root, "proc/modules", "xt_TPROXY 1 0 - Live 0x0\n")
	res := Run(context.Background(), config.DefaultConfig(), env)
	if Failed(res) {
		t.Errorf("unexpected failure: %+v", res)
	}
	joined := strings.Join(checked, "|")
	if !strings.Contains(joined, "sing-box check") {
		t.Errorf("sing-box check was not run: %s", joined)
	}
	if runtime.GOOS == "linux" && !strings.Contains(joined, "nft --check") {
		t.Errorf("nft --check was not run: %s", joined)
	}
}
