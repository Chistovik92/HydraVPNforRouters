// Package components checks the versions of the external programs the
// service runs (sing-box, nfqws, ciadpi) against their latest GitHub
// releases and reports when an update is available. It never installs
// anything: updates go through the package manager of the router.
package components

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
	"github.com/Chistovik92/hydravpn-router/internal/selfupdate"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Component describes a tracked program.
type Component struct {
	Name       string
	Repo       string // GitHub owner/name
	MinVersion string // oldest version the generated configs work with
	Tested     string // newest major.minor the configs were tested with (see docs/components.tsv)
	// Binary returns the executable to ask for its version ("" = skip).
	Binary func(cfg *config.Config) string
	// AnyRelease: upstream marks every release pre-release (Xray), so
	// the newest release is taken instead of /releases/latest.
	AnyRelease bool
}

// Tracked are the components whose versions are checked. Keep in sync with
// docs/COMPONENTS.md.
var Tracked = []Component{
	{Name: "sing-box", Repo: "SagerNet/sing-box", MinVersion: "1.12.0", Tested: "1.14",
		Binary: func(c *config.Config) string { return c.Settings.SingBoxBinary }},
	{Name: "xray", Repo: "XTLS/Xray-core", Tested: "26.10", AnyRelease: true,
		Binary: func(c *config.Config) string { return cores.FindBinary(cores.Xray, c.Settings.XrayBinary) }},
}

// Info is the result for one component.
type Info struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Update    bool   `json:"update_available"`
	// LatestUntested: the latest release is a major.minor newer than the
	// tested one, so updating to it may need changes to the generated config.
	LatestUntested bool   `json:"latest_untested,omitempty"`
	TooOld         bool   `json:"too_old"`
	Untested       bool   `json:"untested_newer"`
	Error          string `json:"error,omitempty"`
	Checked        string `json:"checked,omitempty"`
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?(-[0-9A-Za-z][0-9A-Za-z.]*)?`)

// Parse extracts the first x.y[.z][-pre] from text such as
// "sing-box version 1.12.3" or "v1.13.0-beta.5".
func Parse(text string) string { return versionRe.FindString(text) }

// Compare returns -1, 0 or 1 comparing dotted numeric versions; a
// pre-release ("1.13.0-beta.5") sorts before its release ("1.13.0").
func Compare(a, b string) int { return selfupdate.Compare(a, b) }

func majorMinor(v string) string {
	v, _, _ = strings.Cut(v, "-")
	p := strings.Split(v, ".")
	if len(p) > 2 {
		p = p[:2]
	}
	return strings.Join(p, ".")
}

// Checker runs periodic checks.
type Checker struct {
	mu      sync.RWMutex
	cfg     *config.Config
	results map[string]Info
	onLog   func(level, message string)
	// Overridable for tests.
	installed func(binary string) (string, error)
	latest    func(ctx context.Context, repo string, proxy string) (string, error)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Checker.
func New(cfg *config.Config, onLog func(level, message string)) *Checker {
	return &Checker{cfg: cfg, results: map[string]Info{}, onLog: onLog, installed: installedVersion, latest: latestRelease}
}

// Reload applies a new configuration.
func (c *Checker) Reload(cfg *config.Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// Start begins checking in the background if enabled in the settings.
func (c *Checker) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		// Give sing-box and the network a moment before the first check.
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		for {
			c.mu.RLock()
			enabled, interval := c.cfg.Settings.ComponentUpdateCheckEnabled, c.cfg.Settings.ComponentUpdateCheckInterval
			c.mu.RUnlock()
			if interval <= 0 {
				interval = 24 * time.Hour
			}
			if enabled {
				c.CheckAll(ctx)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
}

// Stop stops the background checks.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

// CheckAll checks every tracked component once.
func (c *Checker) CheckAll(ctx context.Context) {
	c.mu.RLock()
	cfg := c.cfg
	c.mu.RUnlock()
	proxy := ""
	if cfg.Settings.DownloadComponentsViaProxy {
		proxy = fmt.Sprintf("http://127.0.0.1:%d", config.MixedProxyPort)
	}

	for _, comp := range Tracked {
		info := Info{Name: comp.Name, Checked: time.Now().Format(time.RFC3339)}
		bin := comp.Binary(cfg)
		if bin == "" {
			continue // not installed: nothing to compare (Xray is optional)
		}
		if v, err := c.installed(bin); err != nil {
			info.Error = err.Error()
		} else {
			info.Installed = v
			info.TooOld = comp.MinVersion != "" && Compare(v, comp.MinVersion) < 0
			info.Untested = comp.Tested != "" && Compare(majorMinor(v), comp.Tested) > 0
		}
		repo := comp.Repo
		if comp.AnyRelease {
			repo += anyReleaseMark
		}
		if latest, err := c.latest(ctx, repo, proxy); err != nil {
			if info.Error == "" {
				info.Error = err.Error()
			}
		} else {
			info.Latest = latest
			info.Update = info.Installed != "" && Compare(latest, info.Installed) > 0
			info.LatestUntested = comp.Tested != "" && Compare(majorMinor(latest), comp.Tested) > 0
		}
		c.mu.Lock()
		c.results[comp.Name] = info
		c.mu.Unlock()

		switch {
		case info.TooOld:
			c.log("error", "%s %s is older than the supported %s: update it", comp.Name, info.Installed, comp.MinVersion)
		case info.Untested:
			c.log("warn", "%s %s is newer than the tested %s: report problems if routing breaks", comp.Name, info.Installed, comp.Tested)
		case info.Update && info.LatestUntested:
			c.log("info", "%s update available: %s -> %s (not tested yet with the generated config, tested %s)", comp.Name, info.Installed, info.Latest, comp.Tested)
		case info.Update:
			c.log("info", "%s update available: %s -> %s", comp.Name, info.Installed, info.Latest)
		}
	}
}

// GetStatus returns the last results.
func (c *Checker) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]interface{}{}
	for k, v := range c.results {
		out[k] = v
	}
	return out
}

func installedVersion(binary string) (string, error) {
	out, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", binary, err)
	}
	v := Parse(string(out))
	if v == "" {
		return "", fmt.Errorf("cannot parse %s version output", binary)
	}
	return v, nil
}

// anyReleaseMark asks latestRelease for the newest release of a repo
// whatever its pre-release flag.
const anyReleaseMark = "#any"

func latestRelease(ctx context.Context, repo, proxy string) (string, error) {
	path := "/releases/latest"
	if r, ok := strings.CutSuffix(repo, anyReleaseMark); ok {
		repo, path = r, "/releases?per_page=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	// Same client as the self-updater: KeeneticOS has no system CA store
	// and only Entware's bundle makes api.github.com verifiable.
	client := selfupdate.NewHTTPClient(proxy, 30*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	type release struct {
		Tag string `json:"tag_name"`
	}
	var rel release
	if path == "/releases/latest" {
		err = json.NewDecoder(resp.Body).Decode(&rel)
	} else {
		var list []release
		if err = json.NewDecoder(resp.Body).Decode(&list); err == nil && len(list) > 0 {
			rel = list[0]
		}
	}
	if err != nil {
		return "", err
	}
	v := Parse(rel.Tag)
	if v == "" {
		return "", fmt.Errorf("unexpected tag %q", rel.Tag)
	}
	return v, nil
}

func (c *Checker) log(level, format string, args ...interface{}) {
	if c.onLog != nil {
		c.onLog(level, "[components] "+fmt.Sprintf(format, args...))
	}
}
