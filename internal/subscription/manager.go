package subscription

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// Manager manages subscription fetching and parsing
type Manager struct {
	mu           sync.RWMutex
	config       *config.Config
	ctx          context.Context
	cancel       context.CancelFunc
	started      bool
	onLog        func(level, message string)
	wg           sync.WaitGroup
	
	// State
	subscriptions map[string]*SubscriptionState
	cacheDir      string
	client        *http.Client
	
	// Stats
	lastUpdate    time.Time
	updateCount   int
	errorCount    int
	lastError     string
}

// SubscriptionState tracks a subscription's state
type SubscriptionState struct {
	Section       string
	URL           string
	LastUpdate    time.Time
	NextUpdate    time.Time
	LastError     string
	Outbounds     []OutboundInfo
	Metadata      *SubscriptionMetadata
	UpdateEnabled bool
	UpdateInterval time.Duration
}

// OutboundInfo represents a parsed outbound
type OutboundInfo struct {
	Name       string
	Tag        string
	Type       string
	Server     string
	Port       int
	UUID       string
	Password   string
	Method     string
	Flow       string
	Transport  string
	Security   string
	SNI        string
	Fingerprint string
	PublicKey   string
	ShortID     string
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
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		config:        opts.Config,
		ctx:           ctx,
		cancel:        cancel,
		onLog:         opts.OnLog,
		subscriptions: make(map[string]*SubscriptionState),
		cacheDir:      "/etc/podkop-plus/subscription-cache",
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
	
	os.MkdirAll(m.cacheDir, 0755)
	
	return m
}

// Start starts the subscription manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return nil
	}
	
	m.started = true
	m.log("info", "Subscription manager started")
	
	// Load cached subscriptions
	m.loadCache()
	
	// Start update loop
	m.wg.Add(1)
	go m.updateLoop()
	
	return nil
}

// Stop stops the subscription manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started {
		return nil
	}
	
	m.started = false
	m.cancel()
	
	// Save cache
	m.saveCache()
	
	m.wg.Wait()
	m.log("info", "Subscription manager stopped")
	return nil
}

// Reload reloads subscription configuration
func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.config = cfg
	
	// Update subscription states
	for _, subURL := range cfg.SubscriptionURLs {
		key := subURL.Section + ":" + subURL.URL
		if state, exists := m.subscriptions[key]; exists {
			state.UpdateEnabled = subURL.SubscriptionUpdateEnabled
			state.UpdateInterval = subURL.SubscriptionUpdateInterval
			if state.NextUpdate.IsZero() {
				state.NextUpdate = time.Now()
			}
		} else {
			m.subscriptions[key] = &SubscriptionState{
				Section:        subURL.Section,
				URL:            subURL.URL,
				UpdateEnabled:  subURL.SubscriptionUpdateEnabled,
				UpdateInterval: subURL.SubscriptionUpdateInterval,
				NextUpdate:     time.Now(),
			}
		}
	}
	
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
			"last_update":    v.LastUpdate.Format(time.RFC3339),
			"next_update":    v.NextUpdate.Format(time.RFC3339),
			"last_error":     v.LastError,
			"outbounds":      len(v.Outbounds),
			"update_enabled": v.UpdateEnabled,
		}
	}
	
	return map[string]interface{}{
		"running":      m.started,
		"subscriptions": subs,
		"last_update":  m.lastUpdate.Format(time.RFC3339),
		"update_count": m.updateCount,
		"error_count":  m.errorCount,
		"last_error":   m.lastError,
	}
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
func (m *Manager) ForceUpdate(section, url string) error {
	key := section + ":" + url
	m.mu.RLock()
	state, exists := m.subscriptions[key]
	m.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("subscription not found: %s", key)
	}
	
	return m.updateSubscription(state)
}

// updateLoop periodically updates subscriptions
func (m *Manager) updateLoop() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkUpdates()
		}
	}
}

// checkUpdates checks for subscriptions that need updating
func (m *Manager) checkUpdates() {
	m.mu.RLock()
	var toUpdate []*SubscriptionState
	now := time.Now()
	for _, state := range m.subscriptions {
		if state.UpdateEnabled && now.After(state.NextUpdate) {
			toUpdate = append(toUpdate, state)
		}
	}
	m.mu.RUnlock()
	
	for _, state := range toUpdate {
		m.updateSubscription(state)
	}
}

// updateSubscription fetches and parses a subscription
func (m *Manager) updateSubscription(state *SubscriptionState) error {
	m.log("info", "Updating subscription: %s", state.URL)
	
	req, err := http.NewRequestWithContext(m.ctx, "GET", state.URL, nil)
	if err != nil {
		return m.setError(state, fmt.Errorf("create request: %w", err))
	}
	
	// Set headers
	req.Header.Set("User-Agent", m.getUserAgent(state))
	
	resp, err := m.client.Do(req)
	if err != nil {
		return m.setError(state, fmt.Errorf("fetch: %w", err))
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return m.setError(state, fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return m.setError(state, fmt.Errorf("read body: %w", err))
	}
	
	// Parse subscription
	outbounds, metadata, err := m.parseSubscription(string(body))
	if err != nil {
		return m.setError(state, fmt.Errorf("parse: %w", err))
	}
	
	// Update state
	m.mu.Lock()
	state.LastUpdate = time.Now()
	state.NextUpdate = time.Now().Add(state.UpdateInterval)
	state.Outbounds = outbounds
	state.Metadata = metadata
	state.LastError = ""
	m.lastUpdate = time.Now()
	m.updateCount++
	m.mu.Unlock()
	
	// Save cache
	m.saveCache()
	
	m.log("info", "Subscription updated: %s (%d outbounds)", state.URL, len(outbounds))
	return nil
}

// setError sets error state
func (m *Manager) setError(state *SubscriptionState, err error) error {
	m.mu.Lock()
	state.LastError = err.Error()
	state.NextUpdate = time.Now().Add(5 * time.Minute) // Retry sooner on error
	m.errorCount++
	m.lastError = err.Error()
	m.mu.Unlock()
	
	m.log("error", "Subscription update failed: %s - %v", state.URL, err)
	return err
}

// getUserAgent returns the User-Agent header
func (m *Manager) getUserAgent(state *SubscriptionState) string {
	if m.config != nil {
		for _, subURL := range m.config.SubscriptionURLs {
			if subURL.URL == state.URL {
				if subURL.AutoUserAgent {
					return "PodkopPlus/" + "1.0.0"
				}
				if subURL.UserAgent != "" {
					return subURL.UserAgent
				}
			}
		}
	}
	return "PodkopPlus/1.0.0"
}

// parseSubscription parses a subscription response
func (m *Manager) parseSubscription(content string) ([]OutboundInfo, *SubscriptionMetadata, error) {
	// Try base64 decode first
	decoded := content
	if isBase64(content) {
		data, err := base64.StdEncoding.DecodeString(content)
		if err == nil {
			decoded = string(data)
		}
	}
	
	lines := strings.Split(decoded, "\n")
	var outbounds []OutboundInfo
	var metadata *SubscriptionMetadata
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Try parsing as URI
		if strings.Contains(line, "://") {
			ob, err := m.parseURI(line)
			if err == nil {
				outbounds = append(outbounds, ob)
			}
			continue
		}
		
		// Try parsing as JSON
		if strings.HasPrefix(line, "{") {
			ob, err := m.parseJSON(line)
			if err == nil {
				outbounds = append(outbounds, ob)
			}
			continue
		}
		
		// Try parsing as Clash/YAML
		if strings.Contains(line, "proxies:") || strings.Contains(line, "proxy-groups:") {
			// Would need YAML parser
			continue
		}
	}
	
	return outbounds, metadata, nil
}

// parseURI parses a proxy URI (vmess://, vless://, trojan://, etc.)
func (m *Manager) parseURI(uri string) (OutboundInfo, error) {
	var ob OutboundInfo
	ob.Extra = make(map[string]string)
	
	parsed, err := url.Parse(uri)
	if err != nil {
		return ob, err
	}
	
	ob.Type = parsed.Scheme
	ob.Server = parsed.Hostname()
	
	if port := parsed.Port(); port != "" {
		fmt.Sscanf(port, "%d", &ob.Port)
	}
	
	// Parse user info
	if parsed.User != nil {
		ob.UUID = parsed.User.Username()
		if pwd, ok := parsed.User.Password(); ok {
			ob.Password = pwd
		}
	}
	
	// Parse query parameters
	query := parsed.Query()
	ob.Security = query.Get("security")
	ob.Flow = query.Get("flow")
	ob.Transport = query.Get("type")
	ob.SNI = query.Get("sni")
	ob.Fingerprint = query.Get("fp")
	ob.PublicKey = query.Get("pbk")
	ob.ShortID = query.Get("sid")
	ob.Method = query.Get("method")
	ob.Name = query.Get("name")
	
	// Decode name if base64
	if ob.Name != "" {
		if decoded, err := base64.URLEncoding.DecodeString(ob.Name); err == nil {
			ob.Name = string(decoded)
		}
	}
	
	// Set defaults based on type
	switch ob.Type {
	case "vmess":
		if ob.Security == "" { ob.Security = "auto" }
		if ob.Transport == "" { ob.Transport = "tcp" }
	case "vless":
		if ob.Security == "" { ob.Security = "tls" }
		if ob.Transport == "" { ob.Transport = "tcp" }
	case "trojan":
		if ob.Security == "" { ob.Security = "tls" }
		if ob.Transport == "" { ob.Transport = "tcp" }
	case "ss", "shadowsocks":
		if ob.Method == "" { ob.Method = "aes-256-gcm" }
		ob.Type = "shadowsocks"
	case "hysteria2", "hy2":
		ob.Type = "hysteria2"
	}
	
	return ob, nil
}

// parseJSON parses a JSON outbound
func (m *Manager) parseJSON(content string) (OutboundInfo, error) {
	var ob OutboundInfo
	ob.Extra = make(map[string]string)
	
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return ob, err
	}
	
	ob.Type = getString(data, "type")
	ob.Tag = getString(data, "tag")
	ob.Server = getString(data, "server")
	
	if port, ok := data["server_port"].(float64); ok {
		ob.Port = int(port)
	}
	
	ob.UUID = getString(data, "uuid")
	ob.Password = getString(data, "password")
	ob.Method = getString(data, "method")
	ob.Flow = getString(data, "flow")
	ob.Transport = getString(data, "transport")
	ob.Security = getString(data, "security")
	ob.SNI = getString(data, "server_name")
	ob.Fingerprint = getString(data, "utls", "fingerprint")
	ob.PublicKey = getString(data, "reality", "public_key")
	ob.ShortID = getString(data, "reality", "short_id")
	
	return ob, nil
}

func getString(data map[string]interface{}, keys ...string) string {
	current := data
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(string); ok {
				return v
			}
		} else {
			if v, ok := current[key].(map[string]interface{}); ok {
				current = v
			} else {
				return ""
			}
		}
	}
	return ""
}

func isBase64(s string) bool {
	// Check if string looks like base64
	matched, _ := regexp.MatchString(`^[A-Za-z0-9+/]*={0,2}$`, s)
	return matched && len(s)%4 == 0 && len(s) > 20
}

// loadCache loads subscription cache from disk
func (m *Manager) loadCache() {
	// Implementation would load from cacheDir
}

// saveCache saves subscription cache to disk
func (m *Manager) saveCache() {
	// Implementation would save to cacheDir
}

var wg sync.WaitGroup

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		m.onLog(level, "[subscription] "+msg)
	}
}

