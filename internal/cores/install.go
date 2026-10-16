package cores

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/selfupdate"
)

// SingBox is installable like the cores (it is the main engine, not a
// node core, so it is not in Kinds).
const SingBox Kind = "sing-box"

// Installable lists the cores the service can install, in display order.
var Installable = []Kind{SingBox, Xray, OpenFlux, OlcRTC, AmneziaWG}

// BundleRepo publishes the cores that have no router builds upstream
// (amneziawg-go, olcRTC, OpenFlux for every router CPU): the release
// workflow builds them from pinned sources (scripts/build-cores.sh) as
// core-<binary>-linux-<arch> files of each release.
const BundleRepo = "Chistovik92/HydraVPNforRouters"

// source is where a core's builds come from.
type source struct {
	repo string
	// upstream: the version is the upstream release and the binary
	// reports it; otherwise the version is our release that carries it.
	upstream bool
}

var sources = map[Kind]source{
	SingBox:   {repo: "SagerNet/sing-box", upstream: true},
	Xray:      {repo: "XTLS/Xray-core", upstream: true},
	OpenFlux:  {repo: BundleRepo},
	OlcRTC:    {repo: BundleRepo},
	AmneziaWG: {repo: BundleRepo},
}

const (
	maxArchive   = 150 << 20
	keepVersions = 2 // the active one and the previous, for a rollback
	listTimeout  = 30 * time.Second
	fetchTimeout = 15 * time.Minute
)

// Release is a version that can be installed.
type Release struct {
	Version    string `json:"version"`
	Prerelease bool   `json:"prerelease"`
	Published  string `json:"published,omitempty"`
}

// InstallStatus is the progress of the last install of a core.
type InstallStatus struct {
	State      string `json:"state"` // idle, downloading, installing, done, failed
	Version    string `json:"version,omitempty"`
	Downloaded int64  `json:"downloaded_bytes,omitempty"`
	Size       int64  `json:"size_bytes,omitempty"`
	Error      string `json:"error,omitempty"`
}

// InstallerOptions configure an Installer.
type InstallerOptions struct {
	Dir   string // <Dir>/versions/<core>/<version>/, <Dir>/bin/<binary>
	Proxy func() string
	// Installed runs after a version became active (bin is the link).
	Installed func(k Kind, version, bin string)
	OnLog     func(level, message string)

	// For tests.
	APIBase       string
	Client        *http.Client
	GOARCH, GOARM string
	Verify        func(ctx context.Context, bin, version string) error
}

// Installer downloads and switches versions of sing-box and the cores.
type Installer struct {
	o      InstallerOptions
	mu     sync.Mutex
	status map[Kind]InstallStatus
	busy   map[Kind]bool
}

// NewInstaller creates an Installer.
func NewInstaller(o InstallerOptions) *Installer {
	if o.APIBase == "" {
		o.APIBase = "https://api.github.com"
	}
	if o.GOARCH == "" {
		o.GOARCH, o.GOARM = runtime.GOARCH, selfupdate.GOARM()
	}
	if o.Verify == nil {
		o.Verify = verifyVersion
	}
	return &Installer{o: o, status: map[Kind]InstallStatus{}, busy: map[Kind]bool{}}
}

// BinaryName is the file name of a core in <Dir>/bin.
func BinaryName(k Kind) string {
	if k == SingBox {
		return "sing-box"
	}
	return defaultNames[k][0]
}

// SetInstalled sets the callback run after a version became active.
func (i *Installer) SetInstalled(fn func(k Kind, version, bin string)) {
	i.mu.Lock()
	i.o.Installed = fn
	i.mu.Unlock()
}

// Link is the path the active version is reached through.
func (i *Installer) Link(k Kind) string { return filepath.Join(i.o.Dir, "bin", BinaryName(k)) }

func (i *Installer) client(timeout time.Duration) *http.Client {
	if i.o.Client != nil {
		return i.o.Client
	}
	proxy := ""
	if i.o.Proxy != nil {
		proxy = i.o.Proxy()
	}
	return selfupdate.NewHTTPClient(proxy, timeout)
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  string    `json:"published_at"`
	Assets     []ghAsset `json:"assets"`
}

func (i *Installer) get(ctx context.Context, path string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.o.APIBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := i.client(listTimeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}

// Releases lists recent versions of k, betas included, newest first.
func (i *Installer) Releases(ctx context.Context, k Kind) ([]Release, error) {
	src, ok := sources[k]
	if !ok {
		return nil, fmt.Errorf("%s cannot be installed", k)
	}
	var rels []ghRelease
	if err := i.get(ctx, "/repos/"+src.repo+"/releases?per_page=20", &rels); err != nil {
		return nil, err
	}
	var out []Release
	for _, r := range rels {
		if r.Draft {
			continue
		}
		if _, err := assetFor(k, strings.TrimPrefix(r.Tag, "v"), r.Assets, i.o.GOARCH, i.o.GOARM); err != nil {
			continue // nothing for this device
		}
		out = append(out, Release{Version: strings.TrimPrefix(r.Tag, "v"), Prerelease: r.Prerelease, Published: r.Published})
	}
	sort.SliceStable(out, func(a, b int) bool { return selfupdate.Compare(out[a].Version, out[b].Version) > 0 })
	return out, nil
}

// assetFor picks the release file for this device.
func assetFor(k Kind, version string, assets []ghAsset, goarch, goarm string) (ghAsset, error) {
	name, err := assetName(k, version, goarch, goarm)
	if err != nil {
		return ghAsset{}, err
	}
	for _, a := range assets {
		if a.Name == name {
			if a.Digest == "" {
				return a, fmt.Errorf("%s has no checksum in the release", name)
			}
			return a, nil
		}
	}
	return ghAsset{}, fmt.Errorf("the release has no %s", name)
}

func assetName(k Kind, version, goarch, goarm string) (string, error) {
	switch k {
	case SingBox:
		arch := map[string]string{"amd64": "amd64", "386": "386", "arm64": "arm64", "mipsle": "mipsle-softfloat",
			"mips": "mips-softfloat", "mips64le": "mips64le", "mips64": "mips64-softfloat", "riscv64": "riscv64"}[goarch]
		if goarch == "arm" {
			arch = "armv" + goarm
		}
		if arch == "" {
			break
		}
		return fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", version, arch), nil
	case Xray:
		arch := map[string]string{"amd64": "64", "386": "32", "arm64": "arm64-v8a", "mipsle": "mips32le", "mips": "mips32",
			"mips64le": "mips64le", "mips64": "mips64", "riscv64": "riscv64"}[goarch]
		if goarch == "arm" {
			arch = map[string]string{"5": "arm32-v5", "6": "arm32-v6", "7": "arm32-v7a"}[goarm]
		}
		if arch == "" {
			break
		}
		return "Xray-linux-" + arch + ".zip", nil
	default:
		if _, ok := sources[k]; !ok {
			break
		}
		arch, err := selfupdate.ArchLabel("linux", goarch, goarm)
		if err != nil {
			break
		}
		return "core-" + BinaryName(k) + "-linux-" + arch, nil
	}
	return "", fmt.Errorf("%s: no build for linux/%s", k, goarch)
}

// Status returns the install progress of every installable core.
func (i *Installer) Status() map[Kind]InstallStatus {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := map[Kind]InstallStatus{}
	for _, k := range Installable {
		st := i.status[k]
		if st.State == "" {
			st.State = "idle"
		}
		out[k] = st
	}
	return out
}

func (i *Installer) set(k Kind, fn func(*InstallStatus)) {
	i.mu.Lock()
	st := i.status[k]
	fn(&st)
	i.status[k] = st
	i.mu.Unlock()
}

// PickVersion turns "latest" (newest stable) and "beta" (newest, betas
// included) into a version; anything else is returned as is.
func (i *Installer) PickVersion(ctx context.Context, k Kind, v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v != "latest" && v != "beta" && v != "" {
		return v, nil
	}
	rels, err := i.Releases(ctx, k)
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if v == "beta" || !r.Prerelease {
			return r.Version, nil
		}
	}
	// A long series of betas can push every stable release off the page.
	var rel ghRelease
	if err := i.get(ctx, "/repos/"+sources[k].repo+"/releases/latest", &rel); err == nil && rel.Tag != "" {
		return strings.TrimPrefix(rel.Tag, "v"), nil
	}
	return "", fmt.Errorf("%s: no release with a build for this device", k)
}

// InstallNow installs synchronously (the CLI and install.sh).
func (i *Installer) InstallNow(ctx context.Context, k Kind, version string) (string, error) {
	if _, ok := sources[k]; !ok {
		return "", fmt.Errorf("%s cannot be installed", k)
	}
	v, err := i.PickVersion(ctx, k, version)
	if err != nil {
		return "", err
	}
	if err := i.install(ctx, k, v); err != nil {
		return "", err
	}
	return v, nil
}

// ErrInstallBusy means an install of the core is already running.
var ErrInstallBusy = errors.New("an install of this core is already running")

// Start installs version of k in the background (Status follows it).
func (i *Installer) Start(k Kind, version string) error {
	if _, ok := sources[k]; !ok {
		return fmt.Errorf("%s cannot be installed", k)
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.ContainsAny(version, "/\\ ") {
		return fmt.Errorf("bad version %q", version)
	}
	i.mu.Lock()
	if i.busy[k] {
		i.mu.Unlock()
		return ErrInstallBusy
	}
	i.busy[k] = true
	i.status[k] = InstallStatus{State: "downloading", Version: version}
	i.mu.Unlock()

	go func() {
		defer func() {
			i.mu.Lock()
			i.busy[k] = false
			i.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		version, err := i.PickVersion(ctx, k, version)
		if err == nil {
			i.set(k, func(s *InstallStatus) { s.Version = version })
			err = i.install(ctx, k, version)
		}
		if err != nil {
			i.set(k, func(s *InstallStatus) { s.State, s.Error = "failed", err.Error() })
			i.log("error", "%s %s: %v", k, version, err)
			return
		}
		i.set(k, func(s *InstallStatus) { s.State = "done" })
		i.log("info", "%s %s installed", k, version)
		i.mu.Lock()
		installed := i.o.Installed
		i.mu.Unlock()
		if installed != nil {
			installed(k, version, i.Link(k))
		}
	}()
	return nil
}

func (i *Installer) install(ctx context.Context, k Kind, version string) error {
	src := sources[k]
	// Bundled builds do not know our release number: only check they run.
	reported := version
	if !src.upstream {
		reported = ""
	}
	// A version still kept on the device (rollback) needs no download.
	kept := filepath.Join(i.o.Dir, "versions", string(k), version, BinaryName(k))
	if _, err := os.Stat(kept); err == nil && i.o.Verify(ctx, kept, reported) == nil {
		return activate(i.Link(k), kept)
	}
	var rel ghRelease
	if err := i.get(ctx, "/repos/"+src.repo+"/releases/tags/v"+version, &rel); err != nil {
		return fmt.Errorf("release v%s: %w", version, err)
	}
	asset, err := assetFor(k, version, rel.Assets, i.o.GOARCH, i.o.GOARM)
	if err != nil {
		return err
	}
	verDir := filepath.Join(i.o.Dir, "versions", string(k), version)
	if err := os.MkdirAll(verDir, 0755); err != nil {
		return err
	}
	archive := filepath.Join(verDir, asset.Name)
	err = selfupdate.Fetch(ctx, i.client(fetchTimeout), asset.URL, archive, asset.Digest, maxArchive, func(done, total int64) {
		i.set(k, func(s *InstallStatus) { s.Downloaded, s.Size = done, total })
	})
	if err != nil {
		os.RemoveAll(verDir)
		return err
	}
	i.set(k, func(s *InstallStatus) { s.State = "installing" })
	bin := filepath.Join(verDir, BinaryName(k))
	err = extract(archive, bin, k, i.o.GOARCH)
	os.Remove(archive) // only the binary is kept: flash is small
	if err == nil {
		err = i.o.Verify(ctx, bin, reported)
	}
	if err != nil {
		os.RemoveAll(verDir)
		return err
	}
	if err := activate(i.Link(k), bin); err != nil {
		return err
	}
	i.prune(k, version)
	return nil
}

// extract writes the core binary from a sing-box .tar.gz, an Xray .zip or
// a bare binary (bundled cores). On MIPS the Xray zip has xray_softfloat
// next to xray; routers need it.
func extract(archive, dst string, k Kind, goarch string) error {
	if !strings.HasSuffix(archive, ".zip") && !strings.HasSuffix(archive, ".tar.gz") {
		return os.Rename(archive, dst)
	}
	want := map[string]bool{BinaryName(k): true}
	if k == Xray && strings.HasPrefix(goarch, "mips") {
		want = map[string]bool{"xray_softfloat": true}
	}
	write := func(r io.Reader) error {
		tmp := dst + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(r, maxArchive))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		return os.Rename(tmp, dst)
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if want[filepath.Base(f.Name)] {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				return write(rc)
			}
		}
		return fmt.Errorf("%s: no binary in the archive", filepath.Base(archive))
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s: no binary in the archive", filepath.Base(archive))
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && want[filepath.Base(h.Name)] {
			return write(tr)
		}
	}
}

// verifyVersion runs the new binary: it must start on this CPU and report
// the version that was asked for. With no version (bundled builds) it only
// has to start: --help may well exit with an error.
func verifyVersion(ctx context.Context, bin, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if version == "" {
		cmd := exec.CommandContext(ctx, bin, "--help")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("the downloaded binary does not run on this device: %v", err)
		}
		cmd.Wait()
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run on this device: %v", err)
	}
	if !strings.Contains(string(out), version) {
		return fmt.Errorf("the downloaded binary reports %q, expected %s", strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), version)
	}
	return nil
}

// activate points link at bin atomically (a new symlink renamed over the
// old one); where symlinks are not available the file is copied.
func activate(link, bin string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	tmp := link + ".new"
	os.Remove(tmp)
	if err := os.Symlink(bin, tmp); err != nil {
		data, rerr := os.ReadFile(bin)
		if rerr != nil {
			return rerr
		}
		if err := os.WriteFile(tmp, data, 0755); err != nil {
			return err
		}
	}
	return os.Rename(tmp, link)
}

// prune removes versions other than the active one and the previous one.
func (i *Installer) prune(k Kind, active string) {
	dir := filepath.Join(i.o.Dir, "versions", string(k))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type v struct {
		name string
		mod  time.Time
	}
	var vs []v
	for _, e := range entries {
		if info, err := e.Info(); err == nil && e.IsDir() && e.Name() != active {
			vs = append(vs, v{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(vs, func(a, b int) bool { return vs[a].mod.After(vs[b].mod) })
	for n, old := range vs {
		if n >= keepVersions-1 {
			os.RemoveAll(filepath.Join(dir, old.name))
		}
	}
}

// Versions lists the versions kept on the device (for an offline rollback).
func (i *Installer) Versions(k Kind) []string {
	entries, _ := os.ReadDir(filepath.Join(i.o.Dir, "versions", string(k)))
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(a, b int) bool { return selfupdate.Compare(out[a], out[b]) > 0 })
	return out
}

func (i *Installer) log(level, format string, args ...interface{}) {
	if i.o.OnLog != nil {
		i.o.OnLog(level, "[cores] "+fmt.Sprintf(format, args...))
	}
}
