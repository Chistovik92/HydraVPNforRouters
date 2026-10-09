// Package selfupdate updates the running service to another release of
// HydraVPN for Router: it finds the release on GitHub, downloads the binary
// built for this machine, checks it against the release's checksums.txt,
// makes sure the new file runs and reports the expected version, replaces
// the executable and asks the caller to restart the process.
//
// The release asset names are the ones produced by scripts/build.sh and used
// by scripts/install.sh: hydravpn-router-<version>-<goos>-<arch>[.exe].
package selfupdate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo is the GitHub repository the releases come from.
const Repo = "Chistovik92/HydraVPNforRouters"

// States of an update.
const (
	StateIdle        = "idle"
	StateChecking    = "checking"
	StateDownloading = "downloading"
	StateVerifying   = "verifying"
	StateInstalling  = "installing"
	StateRestarting  = "restarting"
	StateFailed      = "failed"
)

// Errors returned by Start.
var (
	ErrBusy        = errors.New("an update is already in progress")
	ErrUpToDate    = errors.New("already up to date")
	ErrUnsupported = errors.New("self-update is not supported here")
)

const (
	maxBinarySize  = 100 << 20 // release binaries are ~10 MB
	freeSpaceSlack = 1 << 20
	checkTimeout   = 30 * time.Second
	applyTimeout   = 15 * time.Minute
)

// Status is what the API and the web UI show.
type Status struct {
	Current     string `json:"current"`
	Latest      string `json:"latest,omitempty"`
	Available   bool   `json:"update_available"`
	State       string `json:"state"`
	Target      string `json:"target,omitempty"`
	Downloaded  int64  `json:"downloaded_bytes,omitempty"`
	Size        int64  `json:"size_bytes,omitempty"`
	Error       string `json:"error,omitempty"`
	Checked     string `json:"checked,omitempty"`
	ReleaseURL  string `json:"release_url,omitempty"`
	Asset       string `json:"asset,omitempty"`
	Supported   bool   `json:"supported"`
	Unsupported string `json:"unsupported_reason,omitempty"`
}

// Options configure an Updater. Only Current and Restart are required.
type Options struct {
	Current string // running version (version.Version)
	Binary  string // executable to replace; default: os.Executable()
	Repo    string // default: Repo

	// Proxy returns the HTTP proxy URL to download through, or "".
	Proxy func() string
	// Restart is called after the binary was replaced; it must restart the
	// process (the new binary is already in place).
	Restart func()
	OnLog   func(level, message string)

	// Overridable for tests.
	APIBase      string // default https://api.github.com
	DownloadBase string // default https://github.com
	Client       *http.Client
	GOOS, GOARCH string
	// Flavor is the OS family the release file is named after (openwrt,
	// keeneticos, linux). Empty detects it from the running system.
	Flavor    string
	Container *bool
	Verify    func(ctx context.Context, bin, ver string) error
}

// Updater checks for and applies updates. It is safe for concurrent use.
type Updater struct {
	o     Options
	asset string

	mu     sync.Mutex
	status Status
	busy   bool
}

// New creates an Updater.
func New(o Options) *Updater {
	if o.Repo == "" {
		o.Repo = Repo
	}
	// HYDRAVPN_UPDATE_BASE_URL points both the API and the downloads at a
	// mirror or a test server laid out like GitHub
	// (<base>/repos/<repo>/releases/latest, <base>/<repo>/releases/download/…).
	// Checksums are still required.
	if base := strings.TrimRight(os.Getenv("HYDRAVPN_UPDATE_BASE_URL"), "/"); base != "" {
		if o.APIBase == "" {
			o.APIBase = base
		}
		if o.DownloadBase == "" {
			o.DownloadBase = base
		}
	}
	if o.APIBase == "" {
		o.APIBase = "https://api.github.com"
	}
	if o.DownloadBase == "" {
		o.DownloadBase = "https://github.com"
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Binary == "" {
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			o.Binary = exe
		}
	}
	if o.Flavor == "" {
		o.Flavor = DetectFlavor(o.Binary)
	}

	u := &Updater{o: o, status: Status{Current: o.Current, State: StateIdle}}
	if reason := u.unsupported(); reason != "" {
		u.status.Unsupported = reason
	} else {
		u.status.Supported = true
	}
	return u
}

// unsupported explains why this installation cannot update itself.
func (u *Updater) unsupported() string {
	inContainer := false
	if u.o.Container != nil {
		inContainer = *u.o.Container
	} else {
		inContainer = runningInContainer()
	}
	switch {
	case inContainer:
		return "running in a container: pull the new image instead"
	case u.o.Binary == "":
		return "cannot find the executable"
	}
	name, err := AssetNameFor(u.o.Flavor, "0.0.0", u.o.GOOS, u.o.GOARCH, goarm())
	if err != nil {
		return err.Error()
	}
	u.asset = strings.TrimPrefix(name, "hydravpn-router-0.0.0-")
	return ""
}

func runningInContainer() bool {
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return os.Getenv("container") != ""
}

// goarm returns the GOARM the binary was built with ("7" by default).
func goarm() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" && s.Value != "" {
				return s.Value[:1]
			}
		}
	}
	return "7"
}

// Release files are named after the OS they are installed on:
// hydravpn-router-<version>-<flavor>-<arch>.
const (
	FlavorLinux    = "linux" // other Linux hosts: only releases before 1.2.5 have such files
	FlavorOpenWrt  = "openwrt"
	FlavorKeenetic = "keeneticos"
	FlavorRouterOS = "routeros"
)

// DetectFlavor tells which OS family the binary runs on.
func DetectFlavor(binary string) string {
	if runtime.GOOS != "linux" {
		return FlavorLinux
	}
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return FlavorOpenWrt
	}
	if strings.HasPrefix(binary, "/opt/") {
		if _, err := os.Stat("/opt/bin/opkg"); err == nil {
			return FlavorKeenetic
		}
	}
	return FlavorLinux
}

// AssetName returns the generic Linux release file for a version and platform.
func AssetName(ver, goos, goarch, arm string) (string, error) {
	return AssetNameFor(FlavorLinux, ver, goos, goarch, arm)
}

// AssetNameFor returns the release file for an OS family, matching
// scripts/build.sh.
func AssetNameFor(flavor, ver, goos, goarch, arm string) (string, error) {
	suffix := goarch
	switch goos + "/" + goarch {
	case "linux/amd64", "linux/arm64", "linux/386", "linux/mips", "linux/mipsle", "linux/mips64", "linux/mips64le":
	case "linux/arm":
		switch arm {
		case "7":
			suffix = "armv7"
		case "5":
			suffix = "armv5"
		default:
			suffix = "armv6"
		}
	case "windows/amd64":
		return "hydravpn-router-" + ver + "-windows-amd64.exe", nil
	default:
		return "", fmt.Errorf("no release builds for %s/%s", goos, goarch)
	}
	if flavor == "" {
		flavor = FlavorLinux
	}
	return "hydravpn-router-" + ver + "-" + flavor + "-" + suffix, nil
}

// Binary is the executable that an update replaces.
func (u *Updater) Binary() string { return u.o.Binary }

// Status returns the current state without network access.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

func (u *Updater) set(fn func(*Status)) {
	u.mu.Lock()
	fn(&u.status)
	u.mu.Unlock()
}

// Check asks GitHub for the latest release (pre-releases are ignored, as in
// install.sh) and records whether it is newer than the running version.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	u.mu.Lock()
	idle := !u.busy
	if idle {
		u.status.State = StateChecking
	}
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	latest, err := u.latest(ctx)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.Checked = time.Now().Format(time.RFC3339)
	if idle {
		u.status.State = StateIdle
	}
	if err != nil {
		u.status.Error = "check: " + err.Error()
		return u.status, err
	}
	if idle {
		u.status.Error = ""
	}
	u.status.Latest = latest
	u.status.Available = Compare(latest, u.o.Current) > 0
	u.status.ReleaseURL = u.o.DownloadBase + "/" + u.o.Repo + "/releases/tag/v" + latest
	return u.status, nil
}

func (u *Updater) latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.o.APIBase+"/repos/"+u.o.Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(rel.Tag, "v")
	if !validVersion(v) {
		return "", fmt.Errorf("unexpected release tag %q", rel.Tag)
	}
	return v, nil
}

// Start updates to ver in the background ("" means the latest release).
// It returns at once; progress is visible in Status. An explicit version may
// be older than the running one (rollback) but not equal to it.
func (u *Updater) Start(ver string) error {
	ver = strings.TrimPrefix(strings.TrimSpace(ver), "v")
	if ver != "" && !validVersion(ver) {
		return fmt.Errorf("invalid version %q", ver)
	}

	u.mu.Lock()
	if !u.status.Supported {
		u.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnsupported, u.status.Unsupported)
	}
	if u.busy {
		u.mu.Unlock()
		return ErrBusy
	}
	if ver != "" && ver == u.o.Current {
		u.mu.Unlock()
		return ErrUpToDate
	}
	u.busy = true
	u.status.State = StateChecking
	u.status.Error = ""
	u.status.Target = ver
	u.status.Downloaded, u.status.Size = 0, 0
	u.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
		defer cancel()
		err := u.apply(ctx, ver)

		u.mu.Lock()
		u.busy = false
		switch {
		case errors.Is(err, ErrUpToDate):
			// Nothing to do is not a failure.
			u.status.State = StateIdle
			u.status.Error = ""
		case err != nil:
			u.status.State = StateFailed
			u.status.Error = err.Error()
		}
		u.mu.Unlock()
		switch {
		case errors.Is(err, ErrUpToDate):
			u.log("info", "no update: %v", err)
		case err != nil:
			u.log("error", "update failed: %v", err)
		}
	}()
	return nil
}

func (u *Updater) apply(ctx context.Context, ver string) error {
	if ver == "" {
		st, err := u.Check(ctx)
		if err != nil {
			return err
		}
		if !st.Available {
			return fmt.Errorf("%w (%s)", ErrUpToDate, u.o.Current)
		}
		ver = st.Latest
	}
	asset, err := AssetNameFor(u.o.Flavor, ver, u.o.GOOS, u.o.GOARCH, goarm())
	if err != nil {
		return err
	}
	base := u.o.DownloadBase + "/" + u.o.Repo + "/releases/download/v" + ver + "/"
	want, err := u.checksum(ctx, base+"checksums.txt", asset)
	if err != nil && u.o.Flavor != FlavorLinux {
		// Releases before 1.2.5 only have the generic Linux file.
		if generic, gerr := AssetName(ver, u.o.GOOS, u.o.GOARCH, goarm()); gerr == nil {
			if w, werr := u.checksum(ctx, base+"checksums.txt", generic); werr == nil {
				asset, want, err = generic, w, nil
			}
		}
	}
	if err != nil {
		return err
	}
	u.set(func(s *Status) { s.Target = ver; s.Asset = asset; s.State = StateDownloading })
	u.log("info", "updating %s -> %s (%s)", u.o.Current, ver, asset)

	tmp := u.o.Binary + ".new"
	defer os.Remove(tmp) // no-op once it has been renamed into place
	if err := u.download(ctx, base+asset, tmp, want); err != nil {
		return err
	}

	u.set(func(s *Status) { s.State = StateVerifying })
	verify := u.o.Verify
	if verify == nil {
		verify = verifyRuns
	}
	if err := verify(ctx, tmp, ver); err != nil {
		return err
	}

	u.set(func(s *Status) { s.State = StateInstalling })
	if err := replace(u.o.Binary, tmp); err != nil {
		return fmt.Errorf("replace %s: %w", u.o.Binary, err)
	}

	u.set(func(s *Status) { s.State = StateRestarting })
	u.log("info", "installed %s %s, restarting", u.o.Binary, ver)
	if u.o.Restart != nil {
		u.o.Restart()
	}
	return nil
}

// checksum returns the SHA-256 of asset listed in the release's checksums.txt.
// A release without a checksum for the asset is refused.
func (u *Updater) checksum(ctx context.Context, sumsURL, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("checksums.txt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			if _, err := hex.DecodeString(f[0]); err == nil && len(f[0]) == 64 {
				return strings.ToLower(f[0]), nil
			}
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

// download writes the asset to dst while hashing it and refuses it unless
// the hash matches.
func (u *Updater) download(ctx context.Context, src, dst, wantSHA string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", filepath.Base(src), resp.StatusCode)
	}
	if resp.ContentLength > maxBinarySize {
		return fmt.Errorf("download: file is too large (%d bytes)", resp.ContentLength)
	}
	u.set(func(s *Status) { s.Size = resp.ContentLength })

	if resp.ContentLength > 0 {
		if free, err := freeSpace(filepath.Dir(dst)); err == nil && free < uint64(resp.ContentLength)+freeSpaceSlack {
			return fmt.Errorf("not enough space in %s: %d KB free, %d KB needed", filepath.Dir(dst), free>>10, (resp.ContentLength+freeSpaceSlack)>>10)
		}
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	h := sha256.New()
	pw := &progressWriter{u: u}
	_, copyErr := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, maxBinarySize+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		return fmt.Errorf("download: %w", copyErr)
	case closeErr != nil:
		return closeErr
	case pw.n > maxBinarySize:
		return errors.New("download: file is too large")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, wantSHA)
	}
	return os.Chmod(dst, 0755)
}

type progressWriter struct {
	u *Updater
	n int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	n := p.n
	p.u.set(func(s *Status) { s.Downloaded = n })
	return len(b), nil
}

// verifyRuns starts the new binary with "version" and checks that it runs
// on this CPU and reports the expected version.
func verifyRuns(ctx context.Context, bin, ver string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run on this device: %v", err)
	}
	if !strings.Contains(string(out), ver) {
		return fmt.Errorf("the downloaded binary reports %q, expected %s", strings.TrimSpace(firstLine(string(out))), ver)
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (u *Updater) client() *http.Client {
	if u.o.Client != nil {
		return u.o.Client
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 30 * time.Second,
		TLSClientConfig:     &tls.Config{RootCAs: rootCAs()},
	}
	if u.o.Proxy != nil {
		if p := u.o.Proxy(); p != "" {
			if pu, err := url.Parse(p); err == nil {
				tr.Proxy = http.ProxyURL(pu)
			}
		}
	}
	// GitHub redirects release downloads to its CDN; keep the default
	// redirect policy. Downloads may be slow on a router.
	return &http.Client{Transport: tr, Timeout: applyTimeout}
}

// entwareCABundle is where Keenetic Entware keeps CA certificates; Go only
// looks in the system locations, which are empty in KeeneticOS.
const entwareCABundle = "/opt/etc/ssl/certs/ca-certificates.crt"

func rootCAs() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if pem, err := os.ReadFile(entwareCABundle); err == nil {
		pool.AppendCertsFromPEM(pem)
	}
	return pool
}

func (u *Updater) log(level, format string, args ...interface{}) {
	if u.o.OnLog != nil {
		u.o.OnLog(level, "[update] "+fmt.Sprintf(format, args...))
	}
}

// RunChecks checks for a new release now and then periodically while
// settings() reports checks enabled; it returns when ctx ends.
func (u *Updater) RunChecks(ctx context.Context, firstDelay time.Duration, settings func() (bool, time.Duration)) {
	delay := firstDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		enabled, interval := settings()
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		delay = interval
		if !enabled || !u.Status().Supported {
			continue
		}
		before := u.Status().Latest
		st, err := u.Check(ctx)
		if err != nil {
			u.log("warn", "cannot check for updates: %v", err)
			continue
		}
		if st.Available && st.Latest != before {
			u.log("info", "update available: %s -> %s (web UI or POST /api/v1/update)", st.Current, st.Latest)
		}
	}
}

// ---------------------------------------------------------------- versions

func validVersion(v string) bool {
	base, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil || p == "" {
			return false
		}
	}
	for _, r := range pre {
		if !(r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// Compare returns -1, 0 or 1. x.y.z is compared numerically; a pre-release
// ("1.3.0-debug.2") sorts before its release ("1.3.0"), and pre-releases of
// the same version compare their numeric parts, as in install.sh.
func Compare(a, b string) int {
	ab, ap, _ := strings.Cut(a, "-")
	bb, bp, _ := strings.Cut(b, "-")
	if c := compareDotted(ab, bb); c != 0 {
		return c
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return compareDotted(digitsOnly(ap), digitsOnly(bp))
}

func compareDotted(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// digitsOnly turns "debug.12" into "12" and "rc.1.2" into "1.2".
func digitsOnly(s string) string {
	var parts []string
	for _, p := range strings.Split(s, ".") {
		if _, err := strconv.Atoi(p); err == nil {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}
