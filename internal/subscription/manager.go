package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

const (
	defaultUpdateInterval = 24 * time.Hour
	retryInterval         = 5 * time.Minute
	maxBodySize           = 10 << 20
)

// Manager manages subscription fetching and parsing
type Manager struct {
	mu      sync.RWMutex
	config  *config.Config
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	onLog   func(level, message string)
	wg      sync.WaitGroup

	// State
	subscriptions map[string]*SubscriptionState
	cacheDir      string
	client        *http.Client

	// Stats
	lastUpdate  time.Time
	updateCount int
	errorCount  int
	lastError   string
}

// SubscriptionState tracks a subscription's state
type SubscriptionState struct {
	Section        string
	URL            string
	LastUpdate     time.Time
	NextUpdate     time.Time
	LastError      string
	Outbounds      []OutboundInfo
	Metadata       *SubscriptionMetadata
	UpdateEnabled  bool
	UpdateInterval time.Duration
}

// OutboundInfo represents a parsed outbound
type OutboundInfo struct {
	Name        string
	Tag         string
	Type        string
	Server      string
	Port        int
	UUID        string
	Password    string
	Method      string
	Flow        string
	Transport   string
	Security    string
	SNI         string
	Fingerprint string
	PublicKey   string
	ShortID     string
	Path        string
	Host        string
	Extra       map[string]string
}

// SubscriptionMetadata represents subscription metadata for UI
type SubscriptionMetadata struct {
	Title       string
	Description string
	Version     string
	UpdatedAt   time.Time
	Nodes       []NodeInfo
	Groups      []GroupInfo
}

// NodeInfo represents a node in subscription
type NodeInfo struct {
	Name     string
	Type     string
	Country  string
	Latency  int
	Selected bool
}

// GroupInfo represents a group in subscription
type GroupInfo struct {
	Name  string
	Type  string
	Nodes []string
}

// Options for creating a new manager
type Options struct {
	Config *config.Config
	OnLog  func(level, message string)
}

// NewManager creates a new subscription manager
func NewManager(opts Options) *Manager {
	return &Manager{
		config:        opts.Config,
		onLog:         opts.OnLog,
		subscriptions: make(map[string]*SubscriptionState),
		cacheDir:      config.DefaultConfigDir + "/subscription-cache",
		// TLS certificates are verified: subscriptions carry credentials.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func key(section, u string) string { return section + ":" + u }

// syncLocked makes the subscription list match the configuration.
func (m *Manager) syncLocked(cfg *config.Config) {
	wanted := make(map[string]bool)
	for _, subURL := range cfg.SubscriptionURLs {
		if subURL.URL == "" {
			continue
		}
		k := key(subURL.Section, subURL.URL)
		wanted[k] = true

		interval := subURL.SubscriptionUpdateInterval
		if interval <= 0 {
			interval = cfg.Settings.UpdateInterval
		}
		if interval <= 0 {
			interval = defaultUpdateInterval
		}

		state, exists := m.subscriptions[k]
		if !exists {
			state = &SubscriptionState{Section: subURL.Section, URL: subURL.URL}
			m.subscriptions[k] = state
		}
		state.UpdateEnabled = subURL.SubscriptionUpdateEnabled
		state.UpdateInterval = interval
	}
	for k := range m.subscriptions {
		if !wanted[k] {
			delete(m.subscriptions, k)
		}
	}
}

// Start starts the subscription manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	m.loadCacheLocked()
	m.syncLocked(m.config)
	m.log("info", "Subscription manager started (%d subscriptions)", len(m.subscriptions))

	m.wg.Add(1)
	go m.updateLoop(m.ctx)

	return nil
}

// Stop stops the subscription manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	m.cancel()
	m.mu.Unlock()

	// Wait without holding the lock: in-flight updates take it too.
	m.wg.Wait()
	m.saveCache()
	m.log("info", "Subscription manager stopped")
	return nil
}

// Reload reloads subscription configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	m.syncLocked(cfg)
	m.log("info", "Subscription configuration reloaded")
	return nil
}

// GetStatus returns subscription manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subs := make(map[string]interface{})
	for k, v := range m.subscriptions {
		subs[k] = map[string]interface{}{
			"section":        v.Section,
			"url":            v.URL,
			"last_update":    formatTime(v.LastUpdate),
			"next_update":    formatTime(v.NextUpdate),
			"last_error":     v.LastError,
			"outbounds":      len(v.Outbounds),
			"update_enabled": v.UpdateEnabled,
		}
	}

	return map[string]interface{}{
		"running":       m.started,
		"subscriptions": subs,
		"last_update":   formatTime(m.lastUpdate),
		"update_count":  m.updateCount,
		"error_count":   m.errorCount,
		"last_error":    m.lastError,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// GetStatusJSON returns status as JSON
func (m *Manager) GetStatusJSON() string {
	data, _ := json.MarshalIndent(m.GetStatus(), "", "  ")
	return string(data)
}

// GetOutbounds returns all parsed outbounds for a section
func (m *Manager) GetOutbounds(section string) []OutboundInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var outbounds []OutboundInfo
	for _, state := range m.subscriptions {
		if state.Section == section {
			outbounds = append(outbounds, state.Outbounds...)
		}
	}
	return outbounds
}

// ForceUpdate forces an update of a specific subscription
func (m *Manager) ForceUpdate(section, u string) error {
	m.mu.RLock()
	state, exists := m.subscriptions[key(section, u)]
	ctx := m.ctx
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("subscription not found: %s", key(section, u))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return m.updateSubscription(ctx, state)
}

// updateLoop fetches due subscriptions right away and then every minute.
func (m *Manager) updateLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		m.checkUpdates(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// checkUpdates updates subscriptions that are due. A subscription that was
// never fetched is fetched once even if periodic updates are disabled.
func (m *Manager) checkUpdates(ctx context.Context) {
	m.mu.RLock()
	var toUpdate []*SubscriptionState
	now := time.Now()
	for _, state := range m.subscriptions {
		if !now.Before(state.NextUpdate) && (state.UpdateEnabled || state.LastUpdate.IsZero()) {
			toUpdate = append(toUpdate, state)
		}
	}
	m.mu.RUnlock()

	for _, state := range toUpdate {
		if ctx.Err() != nil {
			return
		}
		m.updateSubscription(ctx, state)
	}
}

// updateSubscription fetches and parses a subscription
func (m *Manager) updateSubscription(ctx context.Context, state *SubscriptionState) error {
	m.log("info", "Updating subscription: %s", state.URL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, state.URL, nil)
	if err != nil {
		return m.setError(state, fmt.Errorf("create request: %w", err))
	}
	req.Header.Set("User-Agent", m.getUserAgent(state))

	resp, err := m.client.Do(req)
	if err != nil {
		return m.setError(state, fmt.Errorf("fetch: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return m.setError(state, fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return m.setError(state, fmt.Errorf("read body: %w", err))
	}

	outbounds, err := ParseSubscription(string(body))
	if err != nil {
		return m.setError(state, fmt.Errorf("parse: %w", err))
	}

	m.mu.Lock()
	now := time.Now()
	state.LastUpdate = now
	state.NextUpdate = now.Add(state.UpdateInterval)
	state.Outbounds = outbounds
	state.LastError = ""
	m.lastUpdate = now
	m.updateCount++
	m.mu.Unlock()

	m.saveCache()
	m.log("info", "Subscription updated: %s (%d outbounds)", state.URL, len(outbounds))
	return nil
}

// setError sets error state
func (m *Manager) setError(state *SubscriptionState, err error) error {
	m.mu.Lock()
	state.LastError = err.Error()
	state.NextUpdate = time.Now().Add(retryInterval)
	m.errorCount++
	m.lastError = err.Error()
	m.mu.Unlock()

	m.log("error", "Subscription update failed: %s - %v", state.URL, err)
	return err
}

// getUserAgent returns the User-Agent header
func (m *Manager) getUserAgent(state *SubscriptionState) string {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	if cfg != nil {
		for _, subURL := range cfg.SubscriptionURLs {
			if subURL.URL == state.URL && !subURL.AutoUserAgent && subURL.UserAgent != "" {
				return subURL.UserAgent
			}
		}
	}
	return version.UserAgent()
}

// ParseSubscription parses a subscription body: a list of proxy URIs or
// sing-box JSON outbounds, optionally base64-encoded as a whole.
func ParseSubscription(content string) ([]OutboundInfo, error) {
	decoded := strings.TrimSpace(content)
	if !strings.Contains(decoded, "://") && !strings.HasPrefix(decoded, "{") {
		if data, ok := decodeBase64(decoded); ok {
			decoded = string(data)
		}
	}

	var outbounds []OutboundInfo
	for _, line := range strings.Split(decoded, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		switch {
		case strings.Contains(line, "://"):
			if ob, err := parseURI(line); err == nil {
				outbounds = append(outbounds, ob)
			}
		case strings.HasPrefix(line, "{"):
			if ob, err := parseJSON(line); err == nil {
				outbounds = append(outbounds, ob)
			}
		}
	}

	if len(outbounds) == 0 {
		return nil, fmt.Errorf("no supported proxies found")
	}
	return outbounds, nil
}

// decodeBase64 accepts standard and URL-safe alphabets, with or without
// padding and line breaks.
func decodeBase64(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return nil, false
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if data, err := enc.DecodeString(s); err == nil {
			return data, true
		}
	}
	return nil, false
}

// parseURI parses a proxy URI (vmess://, vless://, trojan://, ss://, hysteria2://)
func parseURI(uri string) (OutboundInfo, error) {
	if strings.HasPrefix(uri, "vmess://") {
		return parseVMess(strings.TrimPrefix(uri, "vmess://"))
	}

	ob := OutboundInfo{Extra: make(map[string]string)}
	parsed, err := url.Parse(uri)
	if err != nil {
		return ob, err
	}

	ob.Type = parsed.Scheme
	ob.Server = parsed.Hostname()
	ob.Port, _ = strconv.Atoi(parsed.Port())
	// The node name is the URI fragment; url.Parse already unescapes it.
	ob.Name = parsed.Fragment

	if parsed.User != nil {
		ob.UUID = parsed.User.Username()
		if pwd, ok := parsed.User.Password(); ok {
			ob.Password = pwd
		}
	}

	query := parsed.Query()
	ob.Security = query.Get("security")
	ob.Flow = query.Get("flow")
	ob.Transport = query.Get("type")
	ob.SNI = query.Get("sni")
	ob.Fingerprint = query.Get("fp")
	ob.PublicKey = query.Get("pbk")
	ob.ShortID = query.Get("sid")
	ob.Path = query.Get("path")
	ob.Host = query.Get("host")

	switch ob.Type {
	case "vless", "trojan":
		if ob.Type == "trojan" {
			ob.Password, ob.UUID = ob.UUID, ""
		}
		if ob.Security == "" {
			ob.Security = "tls"
			if ob.Type == "vless" {
				ob.Security = "none"
			}
		}
		if ob.Transport == "" {
			ob.Transport = "tcp"
		}
	case "ss", "shadowsocks":
		ob.Type = "shadowsocks"
		// SIP002: userinfo is base64(method:password) or percent-encoded method:password.
		userinfo := ob.UUID
		if ob.Password == "" {
			if data, ok := decodeBase64(userinfo); ok {
				userinfo = string(data)
			}
			if method, pwd, ok := strings.Cut(userinfo, ":"); ok {
				ob.Method, ob.Password = method, pwd
			}
		} else {
			ob.Method = userinfo
		}
		ob.UUID = ""
		if ob.Method == "" {
			return ob, fmt.Errorf("shadowsocks: missing method")
		}
	case "hysteria2", "hy2":
		ob.Type = "hysteria2"
		ob.Password, ob.UUID = ob.UUID, ""
		if ob.Password == "" {
			ob.Password = query.Get("auth")
		}
	default:
		return ob, fmt.Errorf("unsupported scheme: %s", ob.Type)
	}

	if ob.Server == "" || ob.Port == 0 {
		return ob, fmt.Errorf("missing server or port")
	}
	return ob, nil
}

// parseVMess parses the v2rayN "vmess://base64(json)" format.
func parseVMess(payload string) (OutboundInfo, error) {
	ob := OutboundInfo{Type: "vmess", Extra: make(map[string]string)}
	data, ok := decodeBase64(payload)
	if !ok {
		return ob, fmt.Errorf("vmess: invalid base64")
	}
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return ob, fmt.Errorf("vmess: %w", err)
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	ob.Name = str("ps")
	ob.Server = str("add")
	ob.Port, _ = strconv.Atoi(str("port"))
	ob.UUID = str("id")
	ob.Transport = str("net")
	ob.Security = str("tls")
	ob.SNI = str("sni")
	ob.Path = str("path")
	ob.Host = str("host")
	ob.Method = str("scy")
	if ob.Method == "" {
		ob.Method = "auto"
	}
	if ob.Transport == "" {
		ob.Transport = "tcp"
	}
	if ob.Server == "" || ob.Port == 0 || ob.UUID == "" {
		return ob, fmt.Errorf("vmess: missing server, port or id")
	}
	return ob, nil
}

// parseJSON parses a sing-box JSON outbound
func parseJSON(content string) (OutboundInfo, error) {
	ob := OutboundInfo{Extra: make(map[string]string)}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return ob, err
	}

	ob.Type = getString(data, "type")
	ob.Tag = getString(data, "tag")
	ob.Name = ob.Tag
	ob.Server = getString(data, "server")
	if port, ok := data["server_port"].(float64); ok {
		ob.Port = int(port)
	}
	ob.UUID = getString(data, "uuid")
	ob.Password = getString(data, "password")
	ob.Method = getString(data, "method")
	ob.Flow = getString(data, "flow")
	ob.Transport = getString(data, "transport", "type")
	ob.SNI = getString(data, "tls", "server_name")
	ob.Fingerprint = getString(data, "tls", "utls", "fingerprint")
	ob.PublicKey = getString(data, "tls", "reality", "public_key")
	ob.ShortID = getString(data, "tls", "reality", "short_id")

	if ob.Type == "" {
		return ob, fmt.Errorf("json outbound without type")
	}
	return ob, nil
}

func getString(data map[string]interface{}, keys ...string) string {
	current := data
	for i, k := range keys {
		if i == len(keys)-1 {
			v, _ := current[k].(string)
			return v
		}
		next, ok := current[k].(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

type cacheEntry struct {
	Section    string         `json:"section"`
	URL        string         `json:"url"`
	LastUpdate time.Time      `json:"last_update"`
	Outbounds  []OutboundInfo `json:"outbounds"`
}

func (m *Manager) cacheFile() string {
	return filepath.Join(m.cacheDir, "subscriptions.json")
}

// loadCacheLocked restores previously fetched outbounds so the router keeps
// working if the subscription server is unreachable at boot.
func (m *Manager) loadCacheLocked() {
	data, err := os.ReadFile(m.cacheFile())
	if err != nil {
		return
	}
	var entries []cacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		m.log("warn", "Ignoring broken subscription cache: %v", err)
		return
	}
	for _, e := range entries {
		m.subscriptions[key(e.Section, e.URL)] = &SubscriptionState{
			Section:    e.Section,
			URL:        e.URL,
			LastUpdate: e.LastUpdate,
			Outbounds:  e.Outbounds,
		}
	}
}

// saveCache writes fetched outbounds to disk.
func (m *Manager) saveCache() {
	m.mu.RLock()
	entries := make([]cacheEntry, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		if !s.LastUpdate.IsZero() {
			entries = append(entries, cacheEntry{s.Section, s.URL, s.LastUpdate, s.Outbounds})
		}
	}
	m.mu.RUnlock()

	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	if err := os.MkdirAll(m.cacheDir, 0700); err != nil {
		m.log("warn", "Cannot create subscription cache dir: %v", err)
		return
	}
	tmp := m.cacheFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err == nil {
		os.Rename(tmp, m.cacheFile())
	}
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		m.onLog(level, "[subscription] "+fmt.Sprintf(format, args...))
	}
}
