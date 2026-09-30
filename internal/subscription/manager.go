package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	onUpdate      func()

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
	Quota          *Quota
	Metadata       *SubscriptionMetadata
	UpdateEnabled  bool
	UpdateInterval time.Duration
}

// Quota is the traffic information a provider reports in the
// "Subscription-Userinfo" response header (all values in bytes).
type Quota struct {
	Upload   int64     `json:"upload"`
	Download int64     `json:"download"`
	Total    int64     `json:"total"`
	Expire   time.Time `json:"expire,omitempty"`
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
	// Raw holds a complete sing-box outbound (JSON subscriptions); it is
	// used as is instead of being built from the fields above.
	Raw map[string]interface{} `json:"raw,omitempty"`
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
	// OnUpdate is called (without locks held) after the set of outbounds
	// changed: a subscription was fetched or the configuration reloaded.
	OnUpdate func()
}

// NewManager creates a new subscription manager
func NewManager(opts Options) *Manager {
	return &Manager{
		config:        opts.Config,
		onLog:         opts.OnLog,
		onUpdate:      opts.OnUpdate,
		subscriptions: make(map[string]*SubscriptionState),
		cacheDir:      cacheDirFor(opts.Config),
		// TLS certificates are verified: subscriptions carry credentials.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// cacheDirFor keeps the cache next to the sing-box config (persistent storage,
// unlike the runtime cache in /tmp) so the router works offline after a reboot.
func cacheDirFor(cfg *config.Config) string {
	if cfg != nil && cfg.Settings.ConfigPath != "" {
		return filepath.Join(filepath.Dir(cfg.Settings.ConfigPath), "subscription-cache")
	}
	return config.DefaultConfigDir + "/subscription-cache"
}

func (m *Manager) notifyUpdate() {
	if m.onUpdate != nil {
		m.onUpdate()
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

	m.config = cfg
	m.syncLocked(cfg)
	m.mu.Unlock()
	m.log("info", "Subscription configuration reloaded")
	return nil
}

// GetStatus returns subscription manager status
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subs := make(map[string]interface{})
	for k, v := range m.subscriptions {
		info := map[string]interface{}{
			"section":        v.Section,
			"url":            redactURL(v.URL),
			"last_update":    formatTime(v.LastUpdate),
			"next_update":    formatTime(v.NextUpdate),
			"last_error":     v.LastError,
			"outbounds":      len(v.Outbounds),
			"update_enabled": v.UpdateEnabled,
		}
		if v.Quota != nil {
			info["quota"] = v.Quota
		}
		subs[redactURL(k)] = info
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

// redactURL hides the path and query of a subscription URL: the token that
// identifies the user lives there and must not end up in status output or logs.
func redactURL(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if j := strings.IndexAny(rest, "/?"); j >= 0 {
			return s[:i+3] + rest[:j] + "/…"
		}
	}
	return s
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
	m.log("info", "Updating subscription: %s", redactURL(state.URL))
	subCfg := m.subscriptionConfig(state)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, state.URL, nil)
	if err != nil {
		return m.setError(state, fmt.Errorf("create request: %w", err))
	}
	req.Header.Set("User-Agent", userAgent(subCfg))
	if subCfg.AutoHWID {
		for k, v := range hwidHeaders() {
			req.Header.Set(k, v)
		}
	}

	resp, err := m.client.Do(req)
	if err != nil {
		// The URL (and its token) is part of *url.Error; do not log it.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
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
	if subCfg.PrefixNodes && subCfg.NodePrefix != "" {
		for i := range outbounds {
			outbounds[i].Name = subCfg.NodePrefix + outbounds[i].Name
		}
	}

	m.mu.Lock()
	now := time.Now()
	state.LastUpdate = now
	state.NextUpdate = now.Add(state.UpdateInterval)
	state.Outbounds = outbounds
	state.Quota = parseUserinfo(resp.Header.Get("Subscription-Userinfo"))
	state.LastError = ""
	m.lastUpdate = now
	m.updateCount++
	m.mu.Unlock()

	m.saveCache()
	m.log("info", "Subscription updated: %s (%d outbounds)", redactURL(state.URL), len(outbounds))
	m.notifyUpdate()
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

	m.log("error", "Subscription update failed: %s - %v", redactURL(state.URL), err)
	return err
}

// subscriptionConfig returns the configuration entry of a subscription.
func (m *Manager) subscriptionConfig(state *SubscriptionState) config.SubscriptionURL {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	if cfg != nil {
		for _, subURL := range cfg.SubscriptionURLs {
			if subURL.URL == state.URL && subURL.Section == state.Section {
				return subURL
			}
		}
	}
	return config.SubscriptionURL{}
}

// userAgent returns the User-Agent header. Panels serve different formats
// depending on it, so a custom value can be configured.
func userAgent(sub config.SubscriptionURL) string {
	if !sub.AutoUserAgent && sub.UserAgent != "" {
		return sub.UserAgent
	}
	return version.UserAgent()
}

// hwidHeaders identifies the router to panels that limit devices per
// subscription (x-hwid). The id is a hash of the machine id, stable across
// reboots and updates.
func hwidHeaders() map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"x-hwid":         hardwareID(),
		"x-device-os":    runtime.GOOS,
		"x-device-model": "HydraVPN Router",
		"x-ver-os":       runtime.GOARCH,
		"x-device-name":  host,
	}
}

func hardwareID() string {
	seed := ""
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			seed = strings.TrimSpace(string(data))
			break
		}
	}
	if seed == "" {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, ifc := range ifaces {
				if ifc.Flags&net.FlagLoopback == 0 && len(ifc.HardwareAddr) > 0 {
					seed = ifc.HardwareAddr.String()
					break
				}
			}
		}
	}
	if seed == "" {
		seed, _ = os.Hostname()
	}
	sum := sha256.Sum256([]byte("hydravpn-router:" + seed))
	return hex.EncodeToString(sum[:8])
}

// parseUserinfo parses "upload=1; download=2; total=3; expire=1700000000".
func parseUserinfo(h string) *Quota {
	if strings.TrimSpace(h) == "" {
		return nil
	}
	q := &Quota{}
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			q.Upload = n
		case "download":
			q.Download = n
		case "total":
			q.Total = n
		case "expire":
			if n > 0 {
				q.Expire = time.Unix(n, 0).UTC()
			}
		}
	}
	return q
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

	// A complete sing-box configuration: {"outbounds": [...]}.
	if strings.HasPrefix(decoded, "{") {
		if obs, ok := parseJSONConfig(decoded); ok {
			return obs, nil
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
	uri = normalizeLegacySS(uri)
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
	for _, k := range []string{"serviceName", "alpn", "mode", "headerType", "encryption"} {
		if v := query.Get(k); v != "" {
			ob.Extra[k] = v
		}
	}
	if v := firstNonEmpty(query.Get("insecure"), query.Get("allowInsecure"), query.Get("allow_insecure")); v == "1" || v == "true" {
		ob.Extra["insecure"] = "1"
	}
	if ob.SNI == "" {
		ob.SNI = query.Get("peer")
	}

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

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizeLegacySS converts the legacy "ss://base64(method:pass@host:port)#name"
// form to SIP002 ("ss://base64(method:pass)@host:port#name").
func normalizeLegacySS(uri string) string {
	if !strings.HasPrefix(uri, "ss://") {
		return uri
	}
	rest := strings.TrimPrefix(uri, "ss://")
	frag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		rest, frag = rest[:i], rest[i:]
	}
	if strings.Contains(rest, "@") {
		return uri
	}
	data, ok := decodeBase64(strings.SplitN(rest, "?", 2)[0])
	if !ok || !strings.Contains(string(data), "@") {
		return uri
	}
	cred, hostport, _ := strings.Cut(string(data), "@")
	return "ss://" + base64.RawURLEncoding.EncodeToString([]byte(cred)) + "@" + hostport + frag
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

	ob.Raw = data
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

// nonProxyTypes are sing-box outbound types that are not subscription nodes.
var nonProxyTypes = map[string]bool{
	"direct": true, "block": true, "dns": true, "selector": true, "urltest": true,
}

// parseJSONConfig extracts the proxy nodes of a sing-box configuration.
func parseJSONConfig(content string) ([]OutboundInfo, bool) {
	var doc struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil || len(doc.Outbounds) == 0 {
		return nil, false
	}
	var out []OutboundInfo
	for _, raw := range doc.Outbounds {
		b, _ := json.Marshal(raw)
		ob, err := parseJSON(string(b))
		if err != nil || nonProxyTypes[ob.Type] {
			continue
		}
		out = append(out, ob)
	}
	return out, len(out) > 0
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
