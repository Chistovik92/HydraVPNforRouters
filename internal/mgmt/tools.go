package mgmt

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/domainmap"
	"github.com/Chistovik92/hydravpn-router/internal/genconfig"
	"github.com/Chistovik92/hydravpn-router/internal/keenetic"
	"github.com/Chistovik92/hydravpn-router/internal/lists"
)

const maxMapDomains = 5000

func (s *Server) toolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/domainmap", s.handleDomainMap)
	mux.HandleFunc("POST /api/v1/genconfig", s.handleGenConfig)
	mux.HandleFunc("GET /api/v1/keenetic", s.handleKeenetic)
	mux.HandleFunc("POST /api/v1/keenetic/proxy", s.handleKeeneticProxy)
}

type domainMapRequest struct {
	Domains      []string `json:"domains"` // list syntax, one entry per element
	Lists        []string `json:"lists"`   // http(s) URLs only
	DNS          []string `json:"dns"`
	Aggregate    string   `json:"aggregate"`
	Format       string   `json:"format"`
	Interface    string   `json:"interface"`
	Name         string   `json:"name"`
	NoCloudflare bool     `json:"no_cloudflare"`
	IPv6         bool     `json:"ipv6"`
	Apply        bool     `json:"apply"` // KeeneticOS: add the routes through ndmc
}

// handleDomainMap resolves domains and returns a route list; with apply it
// also adds the routes to a Keenetic interface.
func (s *Server) handleDomainMap(w http.ResponseWriter, r *http.Request) {
	var req domainMapRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	agg, err := domainmap.ParseAggregation(req.Aggregate)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Format == "" {
		req.Format = "plain"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	var src domainmap.Source
	src.Add(lists.ParseEntries(strings.Join(req.Domains, "\n")))
	for _, u := range req.Lists {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			writeErr(w, 400, errors.New("lists must be http(s) URLs"))
			return
		}
		if err := src.Load(ctx, u); err != nil {
			writeErr(w, 502, err)
			return
		}
	}
	if len(src.Domains) == 0 && len(src.Prefixes) == 0 {
		writeErr(w, 400, errors.New("nothing to resolve"))
		return
	}
	if len(src.Domains) > maxMapDomains {
		writeErr(w, 400, errors.New("too many domains (limit 5000)"))
		return
	}
	servers := req.DNS
	if len(servers) == 0 {
		servers = s.opts.Engine.GetConfig().Settings.DNSServers
	}
	prefixes, failed := domainmap.Run{
		Resolve:        domainmap.Options{Servers: servers, IPv6: req.IPv6},
		Aggregation:    agg,
		DropCloudflare: req.NoCloudflare,
	}.Do(ctx, src)

	resp := map[string]interface{}{"routes": len(prefixes), "failed": failed, "skipped": src.Skipped}
	if req.Apply {
		cmds, err := keenetic.RouteCommands(req.Interface, prefixes)
		if err == nil && !keenetic.Available() {
			err = errors.New("ndmc not found: applying routes works on KeeneticOS only")
		}
		if err == nil {
			err = keenetic.Apply(ctx, keenetic.Ndmc{}, cmds)
		}
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		resp["applied"] = len(cmds)
	}
	parts, err := domainmap.Format(req.Format, prefixes, domainmap.FormatOptions{Interface: req.Interface, ListName: req.Name})
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	resp["output"] = strings.Join(parts, "")
	writeJSON(w, 200, resp)
}

func (s *Server) handleGenConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Link string `json:"link"`
		Core string `json:"core"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	info, err := genconfig.Parse(req.Link)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var data []byte
	if req.Core == "sing-box" {
		data, err = genconfig.SingBox(info, genconfig.Options{})
	} else {
		data, err = genconfig.Xray(info, genconfig.Options{})
	}
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]string{"config": string(data)})
}

func (s *Server) handleKeenetic(w http.ResponseWriter, r *http.Request) {
	if !keenetic.Available() {
		writeJSON(w, 200, map[string]interface{}{"available": false, "interfaces": []keenetic.Interface{}})
		return
	}
	ifaces, err := keenetic.Interfaces(r.Context(), keenetic.Ndmc{})
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"available": true, "interfaces": ifaces})
}

// handleKeeneticProxy returns the ndmc commands of a Proxy interface that
// points at the local proxy; with apply they are run.
func (s *Server) handleKeeneticProxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Port  int    `json:"port"`
		Apply bool   `json:"apply"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Name == "" {
		req.Name = "Proxy0"
	}
	if req.Port == 0 {
		req.Port = 4534
	}
	cmds, err := keenetic.ProxyCommands(req.Name, "127.0.0.1", req.Port)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Apply {
		if !keenetic.Available() {
			writeErr(w, 400, errors.New("ndmc not found: this is not KeeneticOS"))
			return
		}
		if err := keenetic.Apply(r.Context(), keenetic.Ndmc{}, cmds); err != nil {
			writeErr(w, 502, err)
			return
		}
	}
	writeJSON(w, 200, map[string]interface{}{"commands": append(cmds, "system configuration save"), "applied": req.Apply})
}
