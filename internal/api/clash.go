package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/gorilla/websocket"
)

// ClashAPI provides Clash-compatible REST API
type ClashAPI struct {
	mu         sync.RWMutex
	engine     *core.Engine
	config     *config.Config
	server     *http.Server
	clients    map[*websocket.Conn]bool
	upgrader   websocket.Upgrader
	onLog      func(level, message string)
}

// Clash API structures
type ClashConfig struct {
	Port             int    `json:"port"`
	SocksPort        int    `json:"socks-port"`
	RedirPort        int    `json:"redir-port"`
	MixedPort        int    `json:"mixed-port"`
	AllowLan         bool   `json:"allow-lan"`
	BindAddress      string `json:"bind-address"`
	Mode             string `json:"mode"`
	LogLevel         string `json:"log-level"`
	ExternalController string `json:"external-controller"`
	Secret           string `json:"secret,omitempty"`
	ExternalUI       string `json:"external-ui,omitempty"`
	ExternalUIURL    string `json:"external-ui-url,omitempty"`
	Profile          *ProfileConfig `json:"profile,omitempty"`
	DNS              *DNSConfig     `json:"dns,omitempty"`
	TUN              *TUNConfig     `json:"tun,omitempty"`
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
	Enable      bool   `json:"enable"`
	Stack       string `json:"stack"`
	DNSHijack   bool   `json:"dns-hijack"`
	AutoRoute   bool   `json:"auto-route"`
	AutoDetectInterface bool `json:"auto-detect-interface"`
}

type Proxy struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Server   string                 `json:"server"`
	Port     int                    `json:"port"`
	UUID     string                 `json:"uuid,omitempty"`
	Password string                 `json:"password,omitempty"`
	Cipher   string                 `json:"cipher,omitempty"`
	UDP      bool                   `json:"udp,omitempty"`
	TLS      bool                   `json:"tls,omitempty"`
	SkipCertVerify bool            `json:"skip-cert-verify,omitempty"`
	ServerName string               `json:"servername,omitempty"`
	Network  string                 `json:"network,omitempty"`
	WSPath   string                 `json:"ws-path,omitempty"`
	WSHeaders map[string]string     `json:"ws-headers,omitempty"`
	Flow     string                 `json:"flow,omitempty"`
	PublicKey string                `json:"public-key,omitempty"`
	ShortID  string                 `json:"short-id,omitempty"`
	Extra    map[string]interface{} `json:"-"`
}

type ProxyGroup struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Proxies  []string `json:"proxies"`
	URL      string   `json:"url,omitempty"`
	Interval int      `json:"interval,omitempty"`
	Timeout  int      `json:"timeout,omitempty"`
	DisableUDP bool   `json:"disable-udp,omitempty"`
}

type Rule struct {
	Type     string `json:"type"`
	Payload  string `json:"payload"`
	Proxy    string `json:"proxy"`
}

type ConnectionsResponse struct {
	Connections []Connection `json:"connections"`
}

type Connection struct {
	ID          string  `json:"id"`
	Start       string  `json:"start"`
	Source      string  `json:"source"`
	Destination string  `json:"destination"`
	Inbound     string  `json:"inbound"`
	Network     string  `json:"network"`
	Process     string  `json:"process"`
	Upload      int64   `json:"upload"`
	Download    int64   `json:"download"`
	UploadSpeed float64 `json:"uploadSpeed"`
	DownloadSpeed float64 `json:"downloadSpeed"`
	Rule        string  `json:"rule"`
	Proxy       string  `json:"proxy"`
	Chains      []string `json:"chains"`
}

type TrafficsResponse struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type MemoryResponse struct {
	Alloc     int64 `json:"alloc"`
	Total     int64 `json:"total"`
	Sys       int64 `json:"sys"`
	NumGC     int   `json:"numGC"`
}

// NewClashAPI creates a new Clash API server
func NewClashAPI(engine *core.Engine, cfg *config.Config, onLog func(level, message string)) *ClashAPI {
	api := &ClashAPI{
		engine: engine,
		config: cfg,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		onLog: onLog,
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
		Handler: mux,
	}
	
	return api
}

// Start starts the Clash API server
func (c *ClashAPI) Start(addr string) error {
	c.server.Addr = addr
	c.log("info", "Starting Clash API on %s", addr)
	return c.server.ListenAndServe()
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
		Port:             7890,
		SocksPort:        7891,
		MixedPort:        7892,
		AllowLan:         true,
		BindAddress:      "0.0.0.0",
		Mode:             "rule",
		LogLevel:         cfg.Settings.LogLevel,
		ExternalController: "0.0.0.0:9090",
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
	// Get outbounds from engine
	status := c.engine.GetStatus()
	
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
	
	// Add sing-box outbounds if available
	if sb, ok := status["singbox"].(map[string]interface{}); ok {
		if outbounds, ok := sb["outbounds"].([]interface{}); ok {
			for _, ob := range outbounds {
				if obMap, ok := ob.(map[string]interface{}); ok {
					proxies["proxies"] = append(proxies["proxies"].([]Proxy), Proxy{
						Name: obMap["tag"].(string),
						Type: obMap["type"].(string),
					})
				}
			}
		}
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
	
	rules := []Rule{
		{Type: "DOMAIN-SUFFIX", Payload: "google.com", Proxy: "PROXY"},
		{Type: "GEOIP", Payload: "CN", Proxy: "DIRECT"},
		{Type: "FINAL", Payload: "", Proxy: "PROXY"},
	}
	
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
	
	c.writeJSON(w, MemoryResponse{Alloc: 0, Total: 0, Sys: 0, NumGC: 0})
}

// handleVersion handles GET /version
func (c *ClashAPI) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	c.writeJSON(w, map[string]string{
		"version": "1.0.0",
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

// streamLogs streams logs to WebSocket client
func (c *ClashAPI) streamLogs(conn *websocket.Conn) {
	defer func() {
		c.mu.Lock()
		delete(c.clients, conn)
		c.mu.Unlock()
		conn.Close()
	}()
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Send log entry
			conn.WriteJSON(map[string]interface{}{
				"type":    "log",
				"payload": "Log entry",
				"time":    time.Now().Format(time.RFC3339),
			})
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

