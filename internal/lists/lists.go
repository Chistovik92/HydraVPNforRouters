// Package lists downloads plain-text domain / subnet lists (.lst) that
// sing-box cannot load as rule sets, caches them next to the config and
// serves the parsed entries to the config builder.
package lists

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

const (
	maxBody       = 20 << 20
	retryInterval = 10 * time.Minute
)

// NeedsDownload reports whether a list URL must be fetched by us: sing-box
// loads .srs and .json rule sets itself.
func NeedsDownload(u string) bool {
	switch strings.ToLower(path.Ext(strings.SplitN(u, "?", 2)[0])) {
	case ".srs", ".json":
		return false
	}
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// URLs returns the list URLs of enabled sections that need downloading,
// with their update interval.
func URLs(cfg *config.Config) map[string]time.Duration {
	out := map[string]time.Duration{}
	add := func(u string, interval time.Duration) {
		if !NeedsDownload(u) {
			return
		}
		if interval <= 0 {
			interval = cfg.Settings.UpdateInterval
		}
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		if cur, ok := out[u]; !ok || interval < cur {
			out[u] = interval
		}
	}
	resolve := func(name string) {
		if strings.Contains(name, "://") {
			add(name, 0)
			return
		}
		for _, l := range cfg.CommunityLists {
			if l.Name == name {
				add(l.URL, l.Interval)
			}
		}
		for _, r := range cfg.RuleSets {
			if r.Name == name {
				add(r.URL, r.Interval)
			}
		}
	}
	for _, sec := range cfg.Sections {
		if !sec.Enabled {
			continue
		}
		for _, n := range sec.CommunityLists {
			resolve(n)
		}
		for _, n := range sec.RuleSet {
			resolve(n)
		}
		for _, n := range sec.RuleSetWithSubnets {
			resolve(n)
		}
	}
	return out
}

type entry struct {
	Domains []string // domain and all subdomains
	CIDRs   []string
	Exact   []string // exactly this domain
	Keyword []string
	Regex   []string
	Updated time.Time
	Next    time.Time
	Err     string
}

// Manager keeps the downloaded lists up to date.
type Manager struct {
	mu       sync.RWMutex
	cfg      *config.Config
	lists    map[string]*entry
	cacheDir string
	client   *http.Client
	onLog    func(level, message string)
	onUpdate func()

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// Options for NewManager.
type Options struct {
	Config   *config.Config
	OnLog    func(level, message string)
	OnUpdate func()
}

// NewManager creates a manager.
func NewManager(o Options) *Manager {
	dir := config.DefaultConfigDir
	if o.Config != nil && o.Config.Settings.ConfigPath != "" {
		dir = filepath.Dir(o.Config.Settings.ConfigPath)
	}
	return &Manager{
		cfg:      o.Config,
		lists:    map[string]*entry{},
		cacheDir: filepath.Join(dir, "lists-cache"),
		client:   &http.Client{Timeout: 60 * time.Second},
		onLog:    o.OnLog,
		onUpdate: o.OnUpdate,
	}
}

// Start loads the cache and starts the update loop.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	m.syncLocked()
	m.wg.Add(1)
	go m.loop(ctx)
	return nil
}

// Stop stops the update loop.
func (m *Manager) Stop() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

// Reload applies a new configuration.
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	m.cfg = cfg
	m.syncLocked()
	m.mu.Unlock()
	return nil
}

func (m *Manager) syncLocked() {
	wanted := URLs(m.cfg)
	for u := range wanted {
		if _, ok := m.lists[u]; !ok {
			e := &entry{}
			m.loadCache(u, e)
			m.lists[u] = e
		}
	}
	for u := range m.lists {
		if _, ok := wanted[u]; !ok {
			delete(m.lists, u)
		}
	}
}

// ListEntries returns the parsed content of a downloaded list.
func (m *Manager) ListEntries(u string) (domains, cidrs []string, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.lists[u]
	if e == nil || e.Updated.IsZero() {
		return nil, nil, false
	}
	return e.Domains, e.CIDRs, true
}

// ListRules returns every kind of rule of a downloaded list: suffix, exact,
// keyword and regular-expression domains plus subnets.
func (m *Manager) ListRules(u string) (Entries, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.lists[u]
	if e == nil || e.Updated.IsZero() {
		return Entries{}, false
	}
	return Entries{Suffix: e.Domains, Exact: e.Exact, Keyword: e.Keyword, Regex: e.Regex, CIDR: e.CIDRs}, true
}

// GetStatus reports the state of every list.
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]interface{}{}
	for u, e := range m.lists {
		out[config.MaskURL(u)] = map[string]interface{}{
			"domains": len(e.Domains), "subnets": len(e.CIDRs),
			"updated": e.Updated.Format(time.RFC3339), "error": e.Err,
		}
	}
	return out
}

func (m *Manager) loop(ctx context.Context) {
	defer m.wg.Done()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		m.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Manager) refresh(ctx context.Context) {
	m.mu.RLock()
	wanted := URLs(m.cfg)
	var due []string
	now := time.Now()
	for u := range wanted {
		if e := m.lists[u]; e != nil && !now.Before(e.Next) {
			due = append(due, u)
		}
	}
	m.mu.RUnlock()

	changed := false
	for _, u := range due {
		if ctx.Err() != nil {
			return
		}
		if m.fetch(ctx, u, wanted[u]) {
			changed = true
		}
	}
	if changed && m.onUpdate != nil {
		m.onUpdate()
	}
}

// fetch downloads one list; it returns true if the entries changed.
func (m *Manager) fetch(ctx context.Context, u string, interval time.Duration) bool {
	body, err := m.download(ctx, u)
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.lists[u]
	if e == nil {
		return false
	}
	if err != nil {
		e.Err = err.Error()
		e.Next = time.Now().Add(retryInterval)
		m.log("error", "list %s: %v", config.MaskURL(u), err)
		return false
	}
	p := ParseEntries(body)
	domains, cidrs := p.Suffix, p.CIDR
	changed := len(domains) != len(e.Domains) || len(cidrs) != len(e.CIDRs) ||
		len(p.Exact) != len(e.Exact) || len(p.Keyword) != len(e.Keyword) || len(p.Regex) != len(e.Regex) || e.Updated.IsZero()
	e.Domains, e.CIDRs, e.Err = domains, cidrs, ""
	e.Exact, e.Keyword, e.Regex = p.Exact, p.Keyword, p.Regex
	e.Updated = time.Now()
	e.Next = e.Updated.Add(interval)
	m.saveCache(u, body)
	m.log("info", "list %s updated: %d domains, %d subnets", config.MaskURL(u), len(domains), len(cidrs))
	return changed
}

func (m *Manager) download(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := m.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return string(b), err
}

// Parse splits a list into domains (with subdomains) and IPv4/IPv6 subnets.
// It is ParseEntries without the exact, keyword and regexp kinds.
func Parse(body string) (domains, cidrs []string) {
	e := ParseEntries(body)
	return e.Suffix, e.CIDR
}

func (m *Manager) cachePath(u string) string {
	sum := sha1.Sum([]byte(u))
	return filepath.Join(m.cacheDir, hex.EncodeToString(sum[:6])+".lst")
}

func (m *Manager) loadCache(u string, e *entry) {
	data, err := os.ReadFile(m.cachePath(u))
	if err != nil {
		return
	}
	p := ParseEntries(string(data))
	e.Domains, e.CIDRs, e.Exact, e.Keyword, e.Regex = p.Suffix, p.CIDR, p.Exact, p.Keyword, p.Regex
	if st, err := os.Stat(m.cachePath(u)); err == nil {
		e.Updated = st.ModTime()
	}
}

func (m *Manager) saveCache(u, body string) {
	if err := os.MkdirAll(m.cacheDir, 0700); err != nil {
		return
	}
	tmp := m.cachePath(u) + ".tmp"
	if os.WriteFile(tmp, []byte(body), 0600) == nil {
		os.Rename(tmp, m.cachePath(u))
	}
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		m.onLog(level, "[lists] "+fmt.Sprintf(format, args...))
	}
}
