package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
	"github.com/gorilla/websocket"
)

// ClashAPI provides Clash-compatible REST API
type ClashAPI struct {
	mu       sync.RWMutex
	engine   *core.Engine
	config   *config.Config
	server   *http.Server
	clients  map[*websocket.Conn]bool
	upgrader websocket.Upgrader
	secret   string
	onLog    func(level, message string)
}

// Clash API structures
type ClashConfig struct {
	Port               int            `json:"port"`
	SocksPort          int            `json:"socks-port"`
	RedirPort          int            `json:"redir-port"`
	MixedPort          int            `json:"mixed-port"`
	AllowLan           bool           `json:"allow-lan"`
	BindAddress        string         `json:"bind-address"`
	Mode               string         `json:"mode"`
	LogLevel           string         `json:"log-level"`
	ExternalController string         `json:"external-controller"`
	Secret             string         `json:"secret,omitempty"`
	ExternalUI         string         `json:"external-ui,omitempty"`
	ExternalUIURL      string         `json:"external-ui-url,omitempty"`
	Profile            *ProfileConfig `json:"profile,omitempty"`
	DNS                *DNSConfig     `json:"dns,omitempty"`
	TUN                *TUNConfig     `json:"tun,omitempty"`
}

type ProfileConfig struct {
	StoreMode     bool `json:"store-mode"`
	StoreSelected bool `json:"store-selected"`
	StoreFakeIP   bool `json:"store-fakeip"`
}

type DNSConfig struct {
	Enable            bool     `json:"enable"`
	Listen            string   `json:"listen"`
	IPv6              bool     `json:"ipv6"`
	EnhancedMode      string   `json:"enhanced-mode"`
	FakeIPRange       string   `json:"fake-ip-range"`
	FakeIPFilter      []string `json:"fake-ip-filter"`
	DefaultNameserver []string `json:"default-nameserver"`
	Nameserver        []string `json:"nameserver"`
	Fallback          []string `json:"fallback"`
}

type TUNConfig struct {
	Enable              bool   `json:"enable"`
	Stack               string `json:"stack"`
	DNSHijack           bool   `json:"dns-hijack"`
	AutoRoute           bool   `json:"auto-route"`
	AutoDetectInterface bool   `json:"auto-detect-interface"`
}

type Proxy struct {
	Name           string                 `json:"name"`
	Type           string                 `json:"type"`
	Server         string                 `json:"server"`
	Port           int                    `json:"port"`
	UUID           string                 `json:"uuid,omitempty"`
	Password       string                 `json:"password,omitempty"`
	Cipher         string                 `json:"cipher,omitempty"`
	UDP            bool                   `json:"udp,omitempty"`
	TLS            bool                   `json:"tls,omitempty"`
	SkipCertVerify bool                   `json:"skip-cert-verify,omitempty"`
	ServerName     string                 `json:"servername,omitempty"`
	Network        string                 `json:"network,omitempty"`
	WSPath         string                 `json:"ws-path,omitempty"`
	WSHeaders      map[string]string      `json:"ws-headers,omitempty"`
	Flow           string                 `json:"flow,omitempty"`
	PublicKey      string                 `json:"public-key,omitempty"`
	ShortID        string                 `json:"short-id,omitempty"`
	Extra          map[string]interface{} `json:"-"`
}

type ProxyGroup struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Proxies    []string `json:"proxies"`
	URL        string   `json:"url,omitempty"`
	Interval   int      `json:"interval,omitempty"`
	Timeout    int      `json:"timeout,omitempty"`
	DisableUDP bool     `json:"disable-udp,omitempty"`
}

type Rule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

type ConnectionsResponse struct {
	Connections []Connection `json:"connections"`
}

type Connection struct {
	ID            string   `json:"id"`
	Start         string   `json:"start"`
	Source        string   `json:"source"`
	Destination   string   `json:"destination"`
	Inbound       string   `json:"inbound"`
	Network       string   `json:"network"`
	Process       string   `json:"process"`
	Upload        int64    `json:"upload"`
	Download      int64    `json:"download"`
	UploadSpeed   float64  `json:"uploadSpeed"`
	DownloadSpeed float64  `json:"downloadSpeed"`
	Rule          string   `json:"rule"`
	Proxy         string   `json:"proxy"`
	Chains        []string `json:"chains"`
}

type TrafficsResponse struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type MemoryResponse struct {
	Alloc int64 `json:"alloc"`
	Total int64 `json:"total"`
	Sys   int64 `json:"sys"`
	NumGC int   `json:"numGC"`
}

// NewClashAPI creates a new Clash API server. When secret is not empty,
// every request must carry "Authorization: Bearer <secret>" (or ?token= for
// WebSocket clients), as in the Clash API specification.
func NewClashAPI(engine *core.Engine, cfg *config.Config, secret string, onLog func(level, message string)) *ClashAPI {
	api := &ClashAPI{
		engine:  engine,
		config:  cfg,
		clients: make(map[*websocket.Conn]bool),
		secret:  secret,
		onLog:   onLog,
	}
	api.upgrader = websocket.Upgrader{
		// Dashboards are served from other origins; with a secret the
		// token check protects the endpoint, without one only same-host
		// pages may connect.
		CheckOrigin: func(r *http.Request) bool {
			if api.secret != "" {
				return true
			}
			origin := r.Header.Get("Origin")
			return origin == "" || strings.HasSuffix(origin, "://"+r.Host)
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/configs", api.handleConfigs)
	mux.HandleFunc("/proxies", api.handleProxies)
	mux.HandleFunc("/proxies/", api.handleProxy)
	mux.HandleFunc("/rules", api.handleRules)
	mux.HandleFunc("/connections", api.handleConnections)
	mux.HandleFunc("/traffics", api.handleTraffics)
	mux.HandleFunc("/memory", api.handleMemory)
	mux.HandleFunc("/version", api.handleVersion)
	mux.HandleFunc("/logs", api.handleLogs)
	mux.HandleFunc("/connections/", api.handleConnection)

	api.server = &http.Server{
		Handler:           api.authorize(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	return api
}

// authorize enforces the bearer secret.
func (c *ClashAPI) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.secret != "" {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(c.secret)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Start starts the Clash API server
func (c *ClashAPI) Start(addr string) error {
	c.server.Addr = addr
	c.log("info", "Starting Clash API on %s", addr)
	if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop stops the Clash API server
func (c *ClashAPI) Stop() error {
	c.log("info", "Stopping Clash API")

	// Close all WebSocket connections
	c.mu.Lock()
	for conn := range c.clients {
		conn.Close()
	}
	c.clients = make(map[*websocket.Conn]bool)
	c.mu.Unlock()

	return c.server.Close()
}

// handleConfigs handles GET /configs
func (c *ClashAPI) handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := c.engine.GetConfig()

	clashConfig := &ClashConfig{
		Port:               7890,
		SocksPort:          7891,
		MixedPort:          7892,
		AllowLan:           true,
		BindAddress:        "0.0.0.0",
		Mode:               "rule",
		LogLevel:           cfg.Settings.LogLevel,
		ExternalController: c.server.Addr,
		Profile: &ProfileConfig{
			StoreMode:     true,
			StoreSelected: true,
			StoreFakeIP:   true,
		},
		DNS: &DNSConfig{
			Enable:            true,
			Listen:            "0.0.0.0:53",
			IPv6:              true,
			EnhancedMode:      "fake-ip",
			FakeIPRange:       "198.18.0.0/15",
			DefaultNameserver: cfg.Settings.DNSServers,
			Nameserver:        cfg.Settings.DNSServers,
			Fallback:          cfg.Settings.BootstrapDNSServers,
		},
	}

	c.writeJSON(w, clashConfig)
}

// handleProxies handles GET/POST /proxies
func (c *ClashAPI) handleProxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c.listProxies(w, r)
	case http.MethodPost:
		c.addProxy(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (c *ClashAPI) listProxies(w http.ResponseWriter, r *http.Request) {
	proxies := map[string]interface{}{
		"proxies": []Proxy{
			{Name: "DIRECT", Type: "direct"},
			{Name: "REJECT", Type: "reject"},
		},
		"groups": []ProxyGroup{
			{Name: "PROXY", Type: "select", Proxies: []string{"DIRECT", "REJECT"}},
			{Name: "GLOBAL", Type: "select", Proxies: []string{"PROXY", "DIRECT", "REJECT"}},
		},
	}
	c.writeJSON(w, proxies)
}

func (c *ClashAPI) addProxy(w http.ResponseWriter, r *http.Request) {
	var proxy Proxy
	if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Add proxy to engine
	c.writeJSON(w, map[string]string{"message": "Proxy added"})
}

// handleProxy handles GET/PUT/PATCH/DELETE /proxies/{name}
func (c *ClashAPI) handleProxy(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/proxies/"):]

	switch r.Method {
	case http.MethodGet:
		c.getProxy(w, r, name)
	case http.MethodPut, http.MethodPatch:
		c.updateProxy(w, r, name)
	case http.MethodDelete:
		c.deleteProxy(w, r, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (c *ClashAPI) getProxy(w http.ResponseWriter, r *http.Request, name string) {
	c.writeJSON(w, Proxy{Name: name, Type: "direct"})
}

func (c *ClashAPI) updateProxy(w http.ResponseWriter, r *http.Request, name string) {
	var proxy Proxy
	if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update proxy in engine
	c.writeJSON(w, map[string]string{"message": "Proxy updated"})
}

func (c *ClashAPI) deleteProxy(w http.ResponseWriter, r *http.Request, name string) {
	// Delete proxy from engine
	c.writeJSON(w, map[string]string{"message": "Proxy deleted"})
}

// handleRules handles GET /rules
func (c *ClashAPI) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rules come from the configuration instead of placeholder data.
	rules := []Rule{}
	for _, rule := range c.engine.GetConfig().Rules {
		if !rule.Enabled {
			continue
		}
		add := func(typ string, payloads []string) {
			for _, p := range payloads {
				rules = append(rules, Rule{Type: typ, Payload: p, Proxy: rule.Outbound})
			}
		}
		add("DOMAIN", rule.Domain)
		add("DOMAIN-SUFFIX", rule.DomainSuffix)
		add("DOMAIN-KEYWORD", rule.DomainKeyword)
		add("IP-CIDR", rule.Destination)
		add("SRC-IP-CIDR", rule.Source)
	}
	rules = append(rules, Rule{Type: "MATCH", Proxy: "DIRECT"})

	c.writeJSON(w, map[string]interface{}{"rules": rules})
}

// handleConnections handles GET /connections
func (c *ClashAPI) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get connections from sing-box
	connections := []Connection{}

	c.writeJSON(w, ConnectionsResponse{Connections: connections})
}

// handleTraffics handles GET /traffics
func (c *ClashAPI) handleTraffics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.writeJSON(w, TrafficsResponse{Up: 0, Down: 0})
}

// handleMemory handles GET /memory
func (c *ClashAPI) handleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	c.writeJSON(w, MemoryResponse{
		Alloc: int64(ms.Alloc),
		Total: int64(ms.TotalAlloc),
		Sys:   int64(ms.Sys),
		NumGC: int(ms.NumGC),
	})
}

// handleVersion handles GET /version
func (c *ClashAPI) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.writeJSON(w, map[string]string{
		"version": version.Version,
		"meta":    "HydraVPN for Router",
	})
}

// handleLogs handles GET /logs
func (c *ClashAPI) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Upgrade to WebSocket for log streaming
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log("error", "WebSocket upgrade failed: %v", err)
		return
	}

	c.mu.Lock()
	c.clients[conn] = true
	c.mu.Unlock()

	// Send logs
	go c.streamLogs(conn)
}

// handleConnection handles DELETE /connections/{id}
func (c *ClashAPI) handleConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Close connection
	c.writeJSON(w, map[string]string{"message": "Connection closed"})
}

// streamLogs streams logs to a WebSocket client until it disconnects.
func (c *ClashAPI) streamLogs(conn *websocket.Conn) {
	defer func() {
		c.mu.Lock()
		delete(c.clients, conn)
		c.mu.Unlock()
		conn.Close()
	}()

	// The read loop detects client disconnects (and handles control frames).
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-closed:
			return
		case <-ticker.C:
			state := string(c.engine.GetState())
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := conn.WriteJSON(map[string]interface{}{
				"type":    "info",
				"payload": "engine state: " + state,
				"time":    time.Now().Format(time.RFC3339),
			})
			if err != nil {
				return
			}
		}
	}
}

func (c *ClashAPI) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (c *ClashAPI) log(level, format string, args ...interface{}) {
	if c.onLog != nil {
		msg := fmt.Sprintf(format, args...)
		c.onLog(level, "[clash-api] "+msg)
	}
}
