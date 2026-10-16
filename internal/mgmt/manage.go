package mgmt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Full management from the Hydra app: editing servers and subscriptions,
// routing rules, community lists and settings, and a capabilities list so a
// client can hide what an older router does not have.
func (s *Server) manageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/capabilities", s.handleCapabilities)

	mux.HandleFunc("PUT /api/v1/servers/{name}", s.handleServersReplace)
	mux.HandleFunc("PUT /api/v1/subscriptions/{index}", s.handleSubsReplace)

	mux.HandleFunc("GET /api/v1/rules", s.handleRulesList)
	mux.HandleFunc("POST /api/v1/rules", s.handleRulesAdd)
	mux.HandleFunc("PUT /api/v1/rules/{index}", s.handleRulesReplace)
	mux.HandleFunc("DELETE /api/v1/rules/{index}", s.handleRulesDelete)

	mux.HandleFunc("GET /api/v1/lists", s.handleListsList)
	mux.HandleFunc("POST /api/v1/lists", s.handleListsAdd)
	mux.HandleFunc("PUT /api/v1/lists/{name}", s.handleListsReplace)
	mux.HandleFunc("DELETE /api/v1/lists/{name}", s.handleListsDelete)

	mux.HandleFunc("GET /api/v1/settings", s.handleSettingsGet)
	mux.HandleFunc("PATCH /api/v1/settings", s.handleSettingsPatch)
}

// ---------------------------------------------------------------- helpers

// indexOf returns the position of the item named name, or -1.
func indexOf[T any](items []T, name string, nameOf func(T) string) int {
	for i, v := range items {
		if nameOf(v) == name {
			return i
		}
	}
	return -1
}

// removeAt returns items without the element at i (a new slice).
func removeAt[T any](items []T, i int) []T {
	return append(append([]T{}, items[:i]...), items[i+1:]...)
}

func pathIndex(r *http.Request) (int, error) {
	i, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || i < 0 {
		return 0, fmt.Errorf("bad index %q", r.PathValue("index"))
	}
	return i, nil
}

func sectionName(s config.Section) string    { return s.Name }
func serverName(s config.Server) string      { return s.Name }
func listName(l config.CommunityList) string { return l.Name }

// ---------------------------------------------------------------- capabilities

// apiLevel grows when endpoints are added; clients compare it, not versions.
const apiLevel = 2

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	features := []string{
		"sections", "sections.unmask", "subscriptions", "subscriptions.edit",
		"servers", "servers.edit", "rules", "lists", "settings",
		"nodes", "logs", "logs.stream", "check", "radar",
		"domainmap", "genconfig", "keenetic",
	}
	if s.opts.Updater != nil && s.opts.Updater.Status().Supported {
		features = append(features, "update")
	}
	sort.Strings(features)
	writeJSON(w, 200, map[string]interface{}{
		"api":       apiLevel,
		"version":   version.Version,
		"features":  features,
		"providers": config.KnownProviders(),
		"actions":   []config.ActionType{config.ActionTypeConnection, config.ActionTypeBypass, config.ActionTypeBlock},
		"settings":  map[string]interface{}{"read_only": readOnlySettings()},
	})
}

// ---------------------------------------------------------------- servers, subscriptions

func (s *Server) handleServersReplace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var srv config.Server
	if err := readJSON(r, &srv); err != nil {
		writeErr(w, 400, err)
		return
	}
	srv.Name = name
	err := s.mutate(func(c *config.Config) error {
		i := indexOf(c.Servers, name, serverName)
		if i < 0 {
			return errors.New("no such server")
		}
		c.Servers[i] = config.UnmaskServer(srv, c.Servers[i])
		return nil
	})
	s.done(w, err, 200, "updated")
}

func (s *Server) handleSubsReplace(w http.ResponseWriter, r *http.Request) {
	idx, err := pathIndex(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var sub config.SubscriptionURL
	if err := readJSON(r, &sub); err != nil {
		writeErr(w, 400, err)
		return
	}
	err = s.mutate(func(c *config.Config) error {
		if idx >= len(c.SubscriptionURLs) {
			return errors.New("no such subscription")
		}
		c.SubscriptionURLs[idx] = config.UnmaskSubscription(sub, c.SubscriptionURLs[idx])
		return nil
	})
	s.done(w, err, 200, "updated")
}

// ---------------------------------------------------------------- rules

func (s *Server) handleRulesList(w http.ResponseWriter, r *http.Request) {
	rules := s.opts.Engine.GetConfig().Rules
	type item struct {
		Index int `json:"index"`
		config.Rule
	}
	out := make([]item, len(rules))
	for i, v := range rules {
		out[i] = item{i, v}
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRulesAdd(w http.ResponseWriter, r *http.Request) {
	var rule config.Rule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, 400, err)
		return
	}
	err := s.mutate(func(c *config.Config) error {
		c.Rules = append(c.Rules, rule)
		return nil
	})
	s.done(w, err, 201, "added")
}

func (s *Server) handleRulesReplace(w http.ResponseWriter, r *http.Request) {
	idx, err := pathIndex(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var rule config.Rule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, 400, err)
		return
	}
	err = s.mutate(func(c *config.Config) error {
		if idx >= len(c.Rules) {
			return errors.New("no such rule")
		}
		c.Rules[idx] = rule
		return nil
	})
	s.done(w, err, 200, "updated")
}

func (s *Server) handleRulesDelete(w http.ResponseWriter, r *http.Request) {
	idx, err := pathIndex(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	err = s.mutate(func(c *config.Config) error {
		if idx >= len(c.Rules) {
			return errors.New("no such rule")
		}
		c.Rules = removeAt(c.Rules, idx)
		return nil
	})
	s.done(w, err, 200, "deleted")
}

// ---------------------------------------------------------------- community lists

func (s *Server) handleListsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.opts.Engine.GetConfig().CommunityLists)
}

func (s *Server) handleListsAdd(w http.ResponseWriter, r *http.Request) {
	var l config.CommunityList
	if err := readJSON(r, &l); err != nil {
		writeErr(w, 400, err)
		return
	}
	if l.Name == "" {
		writeErr(w, 400, errors.New("name is required"))
		return
	}
	err := s.mutate(func(c *config.Config) error {
		if indexOf(c.CommunityLists, l.Name, listName) >= 0 {
			return errors.New("list already exists")
		}
		c.CommunityLists = append(c.CommunityLists, l)
		return nil
	})
	s.done(w, err, 201, "added")
}

func (s *Server) handleListsReplace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var l config.CommunityList
	if err := readJSON(r, &l); err != nil {
		writeErr(w, 400, err)
		return
	}
	l.Name = name
	err := s.mutate(func(c *config.Config) error {
		i := indexOf(c.CommunityLists, name, listName)
		if i < 0 {
			return errors.New("no such list")
		}
		c.CommunityLists[i] = l
		return nil
	})
	s.done(w, err, 200, "updated")
}

func (s *Server) handleListsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.mutate(func(c *config.Config) error {
		i := indexOf(c.CommunityLists, name, listName)
		if i < 0 {
			return errors.New("no such list")
		}
		for _, sec := range c.Sections {
			for _, used := range sec.CommunityLists {
				if used == name {
					return fmt.Errorf("list is used by section %q", sec.Name)
				}
			}
		}
		c.CommunityLists = removeAt(c.CommunityLists, i)
		return nil
	})
	s.done(w, err, 200, "deleted")
}

// ---------------------------------------------------------------- settings

// Settings the app may not change: the API's own access (a mistake there
// locks the app out and needs SSH to undo), file paths (writing them would
// point the service at arbitrary files) and bookkeeping.
var readOnly = map[string]bool{
	"api_listen": true, "api_token": true, "api_allow": true, "api_tls_cert": true, "api_tls_key": true,
	"singbox_binary": true, "config_path": true, "cache_path": true, "log_file": true,
	"config_version": true, "applied_migrations": true,
}

func readOnlySettings() []string {
	out := make([]string, 0, len(readOnly))
	for k := range readOnly {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var durationType = reflect.TypeOf(time.Duration(0))

// settingsView returns the settings keyed by their JSON names, with
// durations as Go duration strings ("24h0m0s") rather than nanoseconds.
func settingsView(st config.Settings) map[string]interface{} {
	out := map[string]interface{}{}
	v := reflect.ValueOf(st)
	for i := 0; i < v.NumField(); i++ {
		key := jsonKey(v.Type().Field(i))
		f := v.Field(i)
		if f.Type() == durationType {
			out[key] = time.Duration(f.Int()).String()
			continue
		}
		out[key] = f.Interface()
	}
	return out
}

func jsonKey(f reflect.StructField) string {
	k, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return k
}

// patchSettings applies a partial update; durations accept "24h" or
// nanoseconds. Unknown and read-only keys are refused.
func patchSettings(st *config.Settings, patch map[string]json.RawMessage) error {
	v := reflect.ValueOf(st).Elem()
	fields := map[string]int{}
	for i := 0; i < v.NumField(); i++ {
		fields[jsonKey(v.Type().Field(i))] = i
	}
	for key, raw := range patch {
		i, ok := fields[key]
		switch {
		case !ok:
			return fmt.Errorf("unknown setting %q", key)
		case readOnly[key]:
			return fmt.Errorf("setting %q cannot be changed through the API", key)
		}
		f := v.Field(i)
		var text string
		if f.Type() == durationType && json.Unmarshal(raw, &text) == nil {
			d, err := time.ParseDuration(text)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			f.SetInt(int64(d))
			continue
		}
		if err := json.Unmarshal(raw, f.Addr().Interface()); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, settingsView(s.opts.Engine.GetConfig().Masked().Settings))
}

func (s *Server) handleSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&patch); err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(patch) == 0 {
		writeErr(w, 400, errors.New("nothing to change"))
		return
	}
	err := s.mutate(func(c *config.Config) error { return patchSettings(&c.Settings, patch) })
	s.done(w, err, 200, "updated")
}
