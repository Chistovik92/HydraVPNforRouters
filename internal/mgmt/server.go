// Package mgmt is the management API and web UI of the service: status,
// journal, subscriptions, servers, node selection and diagnostics over JSON.
//
// Every request needs the API token (Authorization: Bearer <token>). Only
// private and loopback addresses may connect unless api_allow says
// otherwise, and every state-changing call is written to the journal.
package mgmt

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/diagnostics"
	"github.com/Chistovik92/hydravpn-router/internal/logx"
	"github.com/Chistovik92/hydravpn-router/internal/radar"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

//go:embed ui/*
var uiFS embed.FS

// Engine is what the API needs from the service core.
type Engine interface {
	GetStatus() map[string]interface{}
	GetConfig() *config.Config
	Reload(*config.Config) error
	ForceUpdateSubscription(section, url string) error
	Restart() error
}

// Options configure a Server.
type Options struct {
	Engine     Engine
	Logger     *logx.Logger
	ConfigFile string // the file that mutations are saved to
	RuntimeDir string // where a generated token is stored
	Clash      *ClashClient
	Radar      *radar.Client // the bot client; tests replace it
	Updater    Updater       // self-update; nil disables /api/v1/update
}

// Server is the management API.
type Server struct {
	opts  Options
	token string
	srv   *http.Server
	allow []*net.IPNet

	cfgMu sync.Mutex // serializes read-modify-write of the config file

	stopRadar chan struct{} // ends the periodic sync with the bot

	failMu  sync.Mutex
	fails   map[string][]time.Time
	waiting map[string]int
}

// New builds a Server from the settings. It returns nil if api_listen is empty.
func New(o Options) (*Server, error) {
	s := o.Engine.GetConfig().Settings
	if s.APIListen == "" {
		return nil, nil
	}
	if o.Clash == nil {
		o.Clash = NewClashClient(config.ClashAPIAddress)
	}
	srv := &Server{opts: o, fails: map[string][]time.Time{}, waiting: map[string]int{}}
	if s.APIToken != "" && len(s.APIToken) < 16 {
		return nil, fmt.Errorf("api_token must be at least 16 characters (leave it empty to generate one)")
	}
	tok, err := resolveToken(s.APIToken, o.RuntimeDir)
	if err != nil {
		return nil, err
	}
	srv.token = tok

	allow := s.APIAllow
	if len(allow) == 0 {
		allow = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", "fe80::/10"}
	}
	for _, a := range allow {
		if !strings.Contains(a, "/") {
			if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
				a += "/32"
			} else {
				a += "/128"
			}
		}
		_, n, err := net.ParseCIDR(a)
		if err != nil {
			return nil, fmt.Errorf("api_allow %q: %w", a, err)
		}
		srv.allow = append(srv.allow, n)
	}

	mux := http.NewServeMux()
	srv.routes(mux)
	srv.srv = &http.Server{Addr: s.APIListen, Handler: srv.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	return srv, nil
}

// TokenFile is where a generated token is stored.
func TokenFile(runtimeDir string) string { return filepath.Join(runtimeDir, "api-token") }

// resolveToken returns the configured token or a generated one that is kept
// in the runtime directory (mode 0600) so it survives restarts of the CLI.
func resolveToken(configured, runtimeDir string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	path := TokenFile(runtimeDir)
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return "", err
	}
	return tok, os.WriteFile(path, []byte(tok+"\n"), 0600)
}

// Start serves in the background.
func (s *Server) Start() error {
	cfg := s.opts.Engine.GetConfig().Settings
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("management API: %w", err)
	}
	useTLS := cfg.APITLSCert != "" && cfg.APITLSKey != ""
	if !useTLS && !isLoopbackAddr(s.srv.Addr) {
		s.log("warn", "API listens on %s without TLS: the token travels in clear text. Set api_tls_cert/api_tls_key or use an SSH/VPN tunnel", s.srv.Addr)
	}
	go func() {
		var err error
		if useTLS {
			s.srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			err = s.srv.ServeTLS(ln, cfg.APITLSCert, cfg.APITLSKey)
		} else {
			err = s.srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log("error", "API stopped: %v", err)
		}
	}()
	scheme := map[bool]string{true: "https", false: "http"}[useTLS]
	s.log("info", "API listening on %s://%s", scheme, s.srv.Addr)
	s.stopRadar = make(chan struct{})
	go s.radarLoop(s.stopRadar)
	return nil
}

// Stop shuts the server down.
func (s *Server) Stop() {
	if s.stopRadar != nil {
		close(s.stopRadar)
		s.stopRadar = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// guard applies the address filter, the token check, brute-force limiting
// and the audit log.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip == nil || !s.allowed(ip) {
			s.log("warn", "denied %s %s %s: address not allowed", host, r.Method, r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// The UI shell is public inside the allowed networks; data is not.
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/ui/") {
			next.ServeHTTP(w, r)
			return
		}
		// A valid token is never throttled: another device in the LAN that
		// guesses wrong tokens must not lock the app out. Wrong tokens are
		// slowed down instead (see rejectBadToken).
		if !s.authorized(r) {
			s.rejectBadToken(w, r, host)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if r.Method != http.MethodGet {
			s.log("info", "%s %s %s -> %d", host, r.Method, r.URL.Path, rec.status)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

// Flush keeps server-sent events working through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) allowed(ip net.IP) bool {
	for _, n := range s.allow {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// streamPath is the only path that accepts the token in the query string:
// EventSource cannot set headers. Elsewhere a URL token would end up in logs
// and browser history.
const streamPath = "/api/v1/logs/stream"

func (s *Server) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" && r.URL.Path == streamPath {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

const (
	maxDelay        = 5 * time.Second
	maxParallelFail = 4
)

// rejectBadToken answers a request with a wrong token. Each recent failure
// from the address adds a second of delay (up to five) and only a few such
// requests may wait at once, so guessing is slow without blocking anyone who
// has the right token.
func (s *Server) rejectBadToken(w http.ResponseWriter, r *http.Request, host string) {
	delay, ok := s.enterFail(host)
	s.log("warn", "denied %s %s %s: bad token", host, r.Method, r.URL.Path)
	if !ok {
		http.Error(w, "too many failed attempts", http.StatusTooManyRequests)
		return
	}
	defer s.leaveFail(host)
	select {
	case <-time.After(delay):
	case <-r.Context().Done():
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// enterFail records a failure and returns the delay to apply; ok is false
// when too many failed requests from this address are already waiting.
func (s *Server) enterFail(host string) (delay time.Duration, ok bool) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	cut := time.Now().Add(-time.Minute)
	kept := s.fails[host][:0]
	for _, t := range s.fails[host] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if s.waiting[host] >= maxParallelFail {
		s.fails[host] = kept
		return 0, false
	}
	kept = append(kept, time.Now())
	s.fails[host] = kept
	s.waiting[host]++
	delay = time.Duration(len(kept)) * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay, true
}

func (s *Server) leaveFail(host string) {
	s.failMu.Lock()
	s.waiting[host]--
	s.failMu.Unlock()
}

func (s *Server) log(level, format string, args ...interface{}) {
	if s.opts.Logger != nil {
		s.opts.Logger.Log(level, "[api] "+fmt.Sprintf(format, args...))
	}
}

// ---------------------------------------------------------------- routes

func (s *Server) routes(mux *http.ServeMux) {
	sub, _ := fs.Sub(uiFS, "ui")
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := fs.ReadFile(sub, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"version": version.Version, "commit": version.Commit})
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		st := s.opts.Engine.GetStatus()
		if s.opts.Updater != nil {
			st["update"] = s.opts.Updater.Status()
		}
		writeJSON(w, 200, st)
	})
	mux.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.opts.Engine.GetConfig().Masked())
	})
	mux.HandleFunc("POST /api/v1/reload", s.handleReload)
	mux.HandleFunc("POST /api/v1/restart", s.handleRestart)

	mux.HandleFunc("GET /api/v1/sections", s.handleSectionsList)
	mux.HandleFunc("POST /api/v1/sections", s.handleSectionsAdd)
	mux.HandleFunc("PUT /api/v1/sections/{name}", s.handleSectionsReplace)
	mux.HandleFunc("DELETE /api/v1/sections/{name}", s.handleSectionsDelete)

	mux.HandleFunc("GET /api/v1/logs", s.handleLogs)
	mux.HandleFunc("GET /api/v1/logs/stream", s.handleLogStream)

	mux.HandleFunc("GET /api/v1/subscriptions", s.handleSubsList)
	mux.HandleFunc("POST /api/v1/subscriptions", s.handleSubsAdd)
	mux.HandleFunc("DELETE /api/v1/subscriptions/{index}", s.handleSubsDelete)
	mux.HandleFunc("POST /api/v1/subscriptions/{index}/refresh", s.handleSubsRefresh)

	mux.HandleFunc("GET /api/v1/servers", s.handleServersList)
	mux.HandleFunc("POST /api/v1/servers", s.handleServersAdd)
	mux.HandleFunc("DELETE /api/v1/servers/{name}", s.handleServersDelete)

	mux.HandleFunc("GET /api/v1/nodes", s.handleNodes)
	mux.HandleFunc("POST /api/v1/nodes/select", s.handleNodeSelect)
	mux.HandleFunc("POST /api/v1/nodes/test", s.handleNodeTest)

	mux.HandleFunc("GET /api/v1/check/{name}", s.handleCheck)

	mux.HandleFunc("GET /api/v1/radar", s.handleRadarStatus)
	mux.HandleFunc("POST /api/v1/radar/link", s.handleRadarLink)
	mux.HandleFunc("POST /api/v1/radar/sync", s.handleRadarSync)
	mux.HandleFunc("DELETE /api/v1/radar", s.handleRadarUnlink)

	s.updateRoutes(mux)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// mutate lets fn change the configuration, then writes just that change to
// the config file and reloads the service. The file is re-read first so
// manual edits made since start are not lost, and it is edited as a YAML tree
// so its comments and layout survive (see config.ApplyEdits).
func (s *Server) mutate(fn func(*config.Config) error) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	before, err := config.LoadFromFile(s.opts.ConfigFile)
	if err != nil {
		return err
	}
	after, err := config.LoadFromFile(s.opts.ConfigFile)
	if err != nil {
		return err
	}
	if err := fn(after); err != nil {
		return err
	}
	edits := diffConfig(before, after)
	if len(edits) == 0 {
		return nil
	}
	cfg, err := config.ApplyEdits(s.opts.ConfigFile, edits)
	if err != nil {
		return err
	}
	return s.apply(cfg)
}

// applyTimeout bounds how long a request waits for the service to apply a
// change; applying stops child processes and can take a while.
var applyTimeout = 25 * time.Second

// errApplying means the change is saved and still being applied.
var errApplying = errors.New("still applying")

// apply reloads the service but never holds the HTTP request longer than
// applyTimeout. The reload keeps running in the background.
func (s *Server) apply(cfg *config.Config) error {
	done := make(chan error, 1)
	go func() { done <- s.opts.Engine.Reload(cfg) }()
	select {
	case err := <-done:
		return err
	case <-time.After(applyTimeout):
		s.log("warn", "applying the configuration takes longer than %s; continuing in the background", applyTimeout)
		return errApplying
	}
}

// done answers a state-changing request: 202 if the change is saved but
// still being applied, an error status on failure, ok otherwise.
func (s *Server) done(w http.ResponseWriter, err error, okCode int, status string) {
	switch {
	case err == nil:
		writeJSON(w, okCode, map[string]string{"status": status})
	case errors.Is(err, errApplying):
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "applying"})
	default:
		writeErr(w, 400, err)
	}
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadFromFile(s.opts.ConfigFile)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	switch err := s.apply(cfg); {
	case errors.Is(err, errApplying):
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "applying"})
	case err != nil:
		writeErr(w, 500, err)
	default:
		writeJSON(w, 200, map[string]string{"status": "reloaded"})
	}
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	// The reply is sent first: a restart closes listeners of the service.
	writeJSON(w, 202, map[string]string{"status": "restarting"})
	go func() {
		if err := s.opts.Engine.Restart(); err != nil {
			s.log("error", "restart failed: %v", err)
		}
	}()
}

// ---------------------------------------------------------------- sections

func (s *Server) handleSectionsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.opts.Engine.GetConfig().Masked().Sections)
}

func (s *Server) handleSectionsAdd(w http.ResponseWriter, r *http.Request) {
	var sec config.Section
	if err := readJSON(r, &sec); err != nil {
		writeErr(w, 400, err)
		return
	}
	err := s.mutate(func(c *config.Config) error {
		for _, e := range c.Sections {
			if e.Name == sec.Name {
				return errors.New("section already exists")
			}
		}
		c.Sections = append(c.Sections, sec)
		return nil
	})
	s.done(w, err, 201, "added")
}

func (s *Server) handleSectionsReplace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var sec config.Section
	if err := readJSON(r, &sec); err != nil {
		writeErr(w, 400, err)
		return
	}
	sec.Name = name
	err := s.mutate(func(c *config.Config) error {
		for i, e := range c.Sections {
			if e.Name == name {
				c.Sections[i] = sec
				return nil
			}
		}
		return errors.New("no such section")
	})
	s.done(w, err, 200, "updated")
}

func (s *Server) handleSectionsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.mutate(func(c *config.Config) error {
		for i, e := range c.Sections {
			if e.Name == name {
				c.Sections = append(c.Sections[:i], c.Sections[i+1:]...)
				return nil
			}
		}
		return errors.New("no such section")
	})
	s.done(w, err, 200, "deleted")
}

// ---------------------------------------------------------------- logs

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 1000 {
		n = 200
	}
	writeJSON(w, 200, s.opts.Logger.Recent(n, r.URL.Query().Get("level")))
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.opts.Logger.Subscribe()
	defer cancel()
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

// ---------------------------------------------------------------- subscriptions

func (s *Server) handleSubsList(w http.ResponseWriter, r *http.Request) {
	cfg := s.opts.Engine.GetConfig().Masked()
	type item struct {
		Index int `json:"index"`
		config.SubscriptionURL
	}
	out := make([]item, len(cfg.SubscriptionURLs))
	for i, v := range cfg.SubscriptionURLs {
		out[i] = item{i, v}
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleSubsAdd(w http.ResponseWriter, r *http.Request) {
	var sub config.SubscriptionURL
	if err := readJSON(r, &sub); err != nil {
		writeErr(w, 400, err)
		return
	}
	if sub.Section == "" || sub.URL == "" {
		writeErr(w, 400, errors.New("section and url are required"))
		return
	}
	// Fresh subscriptions refresh on their own unless the caller says no.
	if sub.SubscriptionUpdateInterval == 0 {
		sub.SubscriptionUpdateEnabled = true
		sub.SubscriptionUpdateInterval = 24 * time.Hour
	}
	err := s.mutate(func(c *config.Config) error {
		found := false
		for _, sec := range c.Sections {
			found = found || sec.Name == sub.Section
		}
		if !found {
			return fmt.Errorf("unknown section %q", sub.Section)
		}
		for _, existing := range c.SubscriptionURLs {
			if existing.Section == sub.Section && existing.URL == sub.URL {
				return errors.New("subscription already exists")
			}
		}
		c.SubscriptionURLs = append(c.SubscriptionURLs, sub)
		return nil
	})
	s.done(w, err, 201, "added")
}

func (s *Server) handleSubsDelete(w http.ResponseWriter, r *http.Request) {
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	err = s.mutate(func(c *config.Config) error {
		if idx < 0 || idx >= len(c.SubscriptionURLs) {
			return errors.New("no such subscription")
		}
		c.SubscriptionURLs = append(c.SubscriptionURLs[:idx], c.SubscriptionURLs[idx+1:]...)
		return nil
	})
	s.done(w, err, 200, "deleted")
}

func (s *Server) handleSubsRefresh(w http.ResponseWriter, r *http.Request) {
	idx, err := strconv.Atoi(r.PathValue("index"))
	cfg := s.opts.Engine.GetConfig()
	if err != nil || idx < 0 || idx >= len(cfg.SubscriptionURLs) {
		writeErr(w, 404, errors.New("no such subscription"))
		return
	}
	sub := cfg.SubscriptionURLs[idx]
	if err := s.opts.Engine.ForceUpdateSubscription(sub.Section, sub.URL); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

// ---------------------------------------------------------------- servers

func (s *Server) handleServersList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.opts.Engine.GetConfig().Masked().Servers)
}

func (s *Server) handleServersAdd(w http.ResponseWriter, r *http.Request) {
	var srv config.Server
	if err := readJSON(r, &srv); err != nil {
		writeErr(w, 400, err)
		return
	}
	if srv.Name == "" {
		writeErr(w, 400, errors.New("name is required"))
		return
	}
	err := s.mutate(func(c *config.Config) error {
		for _, e := range c.Servers {
			if e.Name == srv.Name {
				return errors.New("server already exists")
			}
		}
		c.Servers = append(c.Servers, srv)
		return nil
	})
	s.done(w, err, 201, "added")
}

func (s *Server) handleServersDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.mutate(func(c *config.Config) error {
		for i, e := range c.Servers {
			if e.Name == name {
				c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
				return nil
			}
		}
		return errors.New("no such server")
	})
	s.done(w, err, 200, "deleted")
}

// ---------------------------------------------------------------- nodes

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.opts.Clash.Nodes(r.Context())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, nodes)
}

func (s *Server) handleNodeSelect(w http.ResponseWriter, r *http.Request) {
	var req struct{ Group, Node string }
	if err := readJSON(r, &req); err != nil || req.Group == "" || req.Node == "" {
		writeErr(w, 400, errors.New("group and node are required"))
		return
	}
	if err := s.opts.Clash.Select(r.Context(), req.Group, req.Node); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "selected"})
}

func (s *Server) handleNodeTest(w http.ResponseWriter, r *http.Request) {
	var req struct{ Node, URL string }
	if err := readJSON(r, &req); err != nil || req.Node == "" {
		writeErr(w, 400, errors.New("node is required"))
		return
	}
	if req.URL == "" {
		req.URL = s.opts.Engine.GetConfig().Settings.LatencyTestURL
	}
	ms, err := s.opts.Clash.Delay(r.Context(), req.Node, req.URL)
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]int{"delay_ms": ms})
}

// ---------------------------------------------------------------- checks

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	d := diagnostics.NewDiagnostics(s.opts.Engine.GetConfig(), nil)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := d.RunCheck(ctx, r.PathValue("name"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}
