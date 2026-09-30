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
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Component describes a tracked program.
type Component struct {
	Name       string
	Repo       string // GitHub owner/name
	MinVersion string // oldest version the generated configs work with
}

// Tracked are the components whose versions are checked. Keep in sync with
// docs/COMPONENTS.md.
var Tracked = []Component{
	{Name: "sing-box", Repo: "SagerNet/sing-box", MinVersion: "1.12.0"},
}

// Info is the result for one component.
type Info struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Update    bool   `json:"update_available"`
	TooOld    bool   `json:"too_old"`
	Error     string `json:"error,omitempty"`
	Checked   string `json:"checked,omitempty"`
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// Parse extracts the first x.y[.z] from text such as "sing-box version 1.12.3".
func Parse(text string) string { return versionRe.FindString(text) }

// Compare returns -1, 0 or 1 comparing dotted numeric versions.
func Compare(a, b string) int {
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
		bin := cfg.Settings.SingBoxBinary
		if v, err := c.installed(bin); err != nil {
			info.Error = err.Error()
		} else {
			info.Installed = v
			info.TooOld = comp.MinVersion != "" && Compare(v, comp.MinVersion) < 0
		}
		if latest, err := c.latest(ctx, comp.Repo, proxy); err != nil {
			if info.Error == "" {
				info.Error = err.Error()
			}
		} else {
			info.Latest = latest
			info.Update = info.Installed != "" && Compare(latest, info.Installed) > 0
		}
		c.mu.Lock()
		c.results[comp.Name] = info
		c.mu.Unlock()

		switch {
		case info.TooOld:
			c.log("error", "%s %s is older than the supported %s: update it", comp.Name, info.Installed, comp.MinVersion)
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

func latestRelease(ctx context.Context, repo, proxy string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	client := &http.Client{Timeout: 30 * time.Second}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
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
