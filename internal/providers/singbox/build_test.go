package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
	"github.com/Chistovik92/hydravpn-router/internal/lists"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
)

type fakeNodes map[string][]subscription.OutboundInfo

func (f fakeNodes) GetOutbounds(section string) []subscription.OutboundInfo { return f[section] }

func testNodes(t *testing.T) fakeNodes {
	t.Helper()
	body := "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=reality&sni=www.microsoft.com&pbk=KEY&sid=ab&type=tcp&flow=xtls-rprx-vision#node\n" +
		"vless://11111111-2222-3333-4444-555555555555@example.org:443?security=tls&type=ws&host=cdn.example.org&path=%2Fws#node\n" +
		"trojan://secret@t.example.com:8443?sni=t.example.com#trojan\n" +
		"vless://11111111-2222-3333-4444-555555555555@x.example.com:443?type=xhttp#unsupported\n"
	obs, err := subscription.ParseSubscription(body)
	if err != nil {
		t.Fatal(err)
	}
	return fakeNodes{"main": obs}
}

func render(t *testing.T, c *Config) map[string]interface{} {
	t.Helper()
	data, err := c.Render()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSectionsProduceRoutingThroughProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{
		{Name: "main", Enabled: true, Action: config.ActionTypeConnection, CommunityLists: []string{"youtube", "ru"}, FullyRoutedIPs: []string{"192.168.1.50"}},
		{Name: "blocked", Enabled: true, Action: config.ActionTypeBlock, RuleSet: []string{"https://example.com/ads.srs"}},
		{Name: "dpi", Enabled: true, Provider: config.ProviderTypeByeDPI, Action: config.ActionTypeConnection, CommunityLists: []string{"discord"}},
		{Name: "off", Enabled: false},
	}
	cfg.CommunityLists = []config.CommunityList{
		{Name: "youtube", Type: "domain", Entries: []string{"youtube.com", "googlevideo.com"}},
		{Name: "ru", URL: "https://example.com/russia_inside.srs"},
		{Name: "discord", Type: "domain", Entries: []string{"discord.com"}},
	}
	cfg.Rules = []config.Rule{{Section: "main", Enabled: true, DomainSuffix: []string{"example.net"}, Port: "443,8443"}}

	c := ConfigFromSettings(cfg, testNodes(t))
	out := render(t, c)

	// Every tag referenced by the router must be defined.
	tags := map[string]bool{}
	for _, ob := range out["outbounds"].([]interface{}) {
		m := ob.(map[string]interface{})
		tag := m["tag"].(string)
		if tags[tag] {
			t.Errorf("duplicate outbound tag %q", tag)
		}
		tags[tag] = true
	}
	for _, ob := range out["outbounds"].([]interface{}) {
		m := ob.(map[string]interface{})
		if list, ok := m["outbounds"].([]interface{}); ok {
			for _, member := range list {
				if !tags[member.(string)] {
					t.Errorf("%v references unknown outbound %v", m["tag"], member)
				}
			}
			if d, ok := m["default"].(string); ok && !tags[d] {
				t.Errorf("selector default %q undefined", d)
			}
		}
	}
	route := out["route"].(map[string]interface{})
	ruleSets := map[string]bool{}
	for _, rs := range route["rule_set"].([]interface{}) {
		ruleSets[rs.(map[string]interface{})["tag"].(string)] = true
	}
	proxied, rejected := 0, 0
	for _, r := range route["rules"].([]interface{}) {
		rule := r.(map[string]interface{})
		if o, ok := rule["outbound"].(string); ok && !tags[o] {
			t.Errorf("rule routes to unknown outbound %q: %v", o, rule)
		}
		if rs, ok := rule["rule_set"].([]interface{}); ok {
			for _, x := range rs {
				if !ruleSets[x.(string)] {
					t.Errorf("rule uses undefined rule set %v", x)
				}
			}
		}
		if rule["outbound"] == "main" {
			proxied++
		}
		if rule["action"] == "reject" && rule["rule_set"] != nil {
			rejected++
		}
	}
	// FullyRoutedIPs + config rule + youtube entries + ru rule set.
	if proxied != 4 {
		t.Errorf("want 4 rules routed via main, got %d", proxied)
	}
	if rejected != 1 {
		t.Errorf("block section should reject its rule set, got %d", rejected)
	}

	// The selector holds an auto group and the three usable nodes; the xhttp
	// node is skipped with a warning; duplicate node names get unique tags.
	if !tags["main"] || !tags["main-auto"] || !tags["node"] || !tags["node 2"] || !tags["trojan"] {
		t.Errorf("unexpected outbound tags: %v", tags)
	}
	if tags["unsupported"] {
		t.Error("xhttp node must not be emitted")
	}
	if !warned(c, "unsupported") {
		t.Errorf("no warning for the xhttp node: %v", c.Warnings)
	}
	if !tags["dpi"] {
		t.Error("byedpi section needs a socks outbound")
	}
	if tags["off"] {
		t.Error("disabled section produced outbounds")
	}

	// Reality and websocket nodes are converted correctly.
	for _, ob := range out["outbounds"].([]interface{}) {
		m := ob.(map[string]interface{})
		switch m["tag"] {
		case "node":
			tls := m["tls"].(map[string]interface{})
			if tls["reality"].(map[string]interface{})["public_key"] != "KEY" || m["flow"] != "xtls-rprx-vision" {
				t.Errorf("reality node wrong: %v", m)
			}
		case "node 2":
			if m["transport"].(map[string]interface{})["type"] != "ws" {
				t.Errorf("ws node wrong: %v", m)
			}
		}
	}

	// The fallback config has no sections at all.
	fb := render(t, c.fallbacks[1]())
	if len(fb["outbounds"].([]interface{})) != 1 {
		t.Errorf("fallback should only have direct-out: %v", fb["outbounds"])
	}
}

func TestSectionWithoutNodesStaysDirect(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, Action: config.ActionTypeConnection, CommunityLists: []string{"x"}}}
	cfg.CommunityLists = []config.CommunityList{{Name: "x", Entries: []string{"example.com"}}}

	c := ConfigFromSettings(cfg, nil)
	out := render(t, c)
	if len(out["outbounds"].([]interface{})) != 1 {
		t.Errorf("no nodes: only direct-out expected, got %v", out["outbounds"])
	}
	if !warned(c, "no proxy nodes") {
		t.Errorf("expected a warning, got %v", c.Warnings)
	}
}

func TestRuleWithoutMatchersIsSkipped(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, SelectorProxyLinks: []string{"trojan://p@h.example:443#n"}}}
	cfg.Rules = []config.Rule{{Section: "main", Enabled: true}}

	c := ConfigFromSettings(cfg, nil)
	if !warned(c, "without matchers") {
		t.Errorf("empty rule must be skipped with a warning: %v", c.Warnings)
	}
	for _, r := range c.Route.Rules {
		if r["outbound"] == "main" {
			t.Errorf("matcher-less rule would route everything: %v", r)
		}
	}
}

func TestInvalidConfigFallsBack(t *testing.T) {
	old := checkConfig
	defer func() { checkConfig = old }()
	calls := 0
	checkConfig = func(binary, path string) error {
		calls++
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "main-auto") {
			return os.ErrInvalid
		}
		return nil
	}

	cfg := config.DefaultConfig()
	cfg.Settings.ConfigPath = filepath.Join(t.TempDir(), "sing-box", "config.json")
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, CommunityLists: []string{"x"}}}
	cfg.CommunityLists = []config.CommunityList{{Name: "x", Entries: []string{"example.com"}}}

	p := NewProvider(Options{Config: ConfigFromSettings(cfg, testNodes(t))})
	if err := p.writeConfig(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.Settings.ConfigPath)
	if strings.Contains(string(data), "main-auto") || calls != 3 {
		t.Errorf("fallback config expected (check calls: %d)", calls)
	}
}

func warned(c *Config, sub string) bool {
	for _, w := range c.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

type fakeLists map[string][2][]string

func (f fakeLists) ListEntries(u string) ([]string, []string, bool) {
	v, ok := f[u]
	return v[0], v[1], ok
}

func TestLstListsBecomeInlineRules(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, SelectorProxyLinks: []string{"trojan://p@h.example:443#n"}, CommunityLists: []string{"ru", "missing"}}}
	cfg.CommunityLists = []config.CommunityList{
		{Name: "ru", URL: "https://x.example/ru.lst"},
		{Name: "missing", URL: "https://x.example/none.lst"},
	}
	c := ConfigFromSettings(cfg, nil, WithLists(fakeLists{"https://x.example/ru.lst": {{"a.example"}, {"10.0.0.0/8"}}}))
	var domain, cidr bool
	for _, r := range c.Route.Rules {
		if r["outbound"] != "main" {
			continue
		}
		domain = domain || r["domain_suffix"] != nil
		cidr = cidr || r["ip_cidr"] != nil
	}
	if !domain || !cidr {
		t.Errorf("lst rules missing: domain=%v cidr=%v", domain, cidr)
	}
	if !warned(c, "not downloaded yet") {
		t.Errorf("no warning for the list that is not downloaded: %v", c.Warnings)
	}
	if len(c.Route.RuleSet) != 0 {
		t.Error(".lst must not become a remote rule set")
	}
}

func TestCountryOf(t *testing.T) {
	for name, want := range map[string]string{
		"🇩🇪 Frankfurt 1": "DE",
		"[NL] Amsterdam": "NL",
		"US-East":        "US",
		"VIP fast":       "",
		"Server 1":       "",
		"🇯🇵JP Tokyo":     "JP",
	} {
		if got := CountryOf(name); got != want {
			t.Errorf("CountryOf(%q)=%q want %q", name, got, want)
		}
	}
}

func TestURLTestCountryFilters(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true}}
	cfg.URLTests = []config.URLTest{{Section: "main", IncludeCountries: []string{"DE", "🇳🇱"}, ExcludeOutbounds: []string{"[DE] slow"}}}
	nodes := fakeNodes{"main": {
		{Type: "trojan", Name: "[DE] fast", Server: "a.example", Port: 1, Password: "p", Security: "tls"},
		{Type: "trojan", Name: "[DE] slow", Server: "b.example", Port: 1, Password: "p", Security: "tls"},
		{Type: "trojan", Name: "[NL] one", Server: "c.example", Port: 1, Password: "p", Security: "tls"},
		{Type: "trojan", Name: "[US] one", Server: "d.example", Port: 1, Password: "p", Security: "tls"},
	}}
	c := ConfigFromSettings(cfg, nodes)
	for _, ob := range c.Outbounds {
		if ob["type"] == "urltest" {
			members := ob["outbounds"].([]string)
			if len(members) != 2 || members[0] != "[DE] fast" || members[1] != "[NL] one" {
				t.Errorf("urltest members: %v", members)
			}
			return
		}
	}
	t.Error("no urltest group")
}

func TestServersAndFallbackChain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Servers = []config.Server{
		{Name: "vless-in", Enabled: true, Protocol: "vless", ListenPort: 8443, ServerUUID: "u", Security: "reality",
			RealityHandshakeServer: "www.microsoft.com", RealityPrivateKey: "k", RealityShortID: "ab", RoutingMode: config.RoutingModeDirect},
		{Name: "ts", Enabled: true, Protocol: "tailscale", TailscaleAuthKey: "tskey", TailscaleAdvertiseExitNode: true},
		{Name: "mt", Enabled: true, Protocol: "mtproto"},
	}
	c := ConfigFromSettings(cfg, nil)
	out := render(t, c)
	if eps, ok := out["endpoints"].([]interface{}); !ok || eps[0].(map[string]interface{})["type"] != "tailscale" {
		t.Errorf("tailscale endpoint missing: %v", out["endpoints"])
	}
	found := false
	for _, in := range out["inbounds"].([]interface{}) {
		if in.(map[string]interface{})["tag"] == "vless-in" {
			found = true
		}
	}
	if !found {
		t.Error("vless inbound missing")
	}
	if !warned(c, "MTProto") {
		t.Errorf("mtproto must warn: %v", c.Warnings)
	}
	// The first fallback keeps sections but drops servers and endpoints.
	fb := render(t, c.fallbacks[0]())
	if fb["endpoints"] != nil || len(fb["inbounds"].([]interface{})) != 3 {
		t.Errorf("first fallback still has servers: %v", fb["inbounds"])
	}
}

func TestManyIdenticalNamesGetUniqueTags(t *testing.T) {
	b := &builder{used: map[string]bool{directTag: true}, suffix: map[string]int{}}
	seen := map[string]bool{}
	for i := 0; i < 3000; i++ {
		tag := b.uniqueTag("Node")
		if seen[tag] {
			t.Fatalf("duplicate tag %q at %d", tag, i)
		}
		seen[tag] = true
	}
	// A name that collides with a generated suffix is still made unique.
	b2 := &builder{used: map[string]bool{"Node 2": true}, suffix: map[string]int{}}
	b2.used["Node"] = true
	if tag := b2.uniqueTag("Node"); tag != "Node 3" {
		t.Errorf("got %q", tag)
	}
}

func TestCacheDirectoryIsCreated(t *testing.T) {
	old := checkConfig
	defer func() { checkConfig = old }()
	checkConfig = func(string, string) error { return nil }

	cfg := config.DefaultConfig()
	dir := t.TempDir()
	cfg.Settings.ConfigPath = filepath.Join(dir, "sing-box", "config.json")
	cfg.Settings.CachePath = filepath.Join(dir, "missing", "run", "cache.db")
	p := NewProvider(Options{Config: ConfigFromSettings(cfg, nil)})
	if err := p.writeConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing", "run")); err != nil {
		t.Errorf("cache directory was not created: %v", err)
	}
}

type fakeRuleLists map[string]lists.Entries

func (f fakeRuleLists) ListEntries(u string) ([]string, []string, bool) {
	e, ok := f[u]
	return e.Suffix, e.CIDR, ok
}

func (f fakeRuleLists) ListRules(u string) (lists.Entries, bool) {
	e, ok := f[u]
	return e, ok
}

func TestDomainRuleKinds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{
		Name: "main", Enabled: true, SelectorProxyLinks: []string{"trojan://p@h.example:443#n"},
		CommunityLists: []string{"https://x.example/k.lst"},
		Domains:        []string{"namespace:inline.example", "wildcard:cdn*.inline.example", "keyword:tracker"},
	}}
	cfg.Rules = []config.Rule{{Section: "main", Enabled: true, DomainWildcard: []string{"a?.rule.example"}, DomainRegex: []string{`^x\d\.rule$`}}}
	src := fakeRuleLists{"https://x.example/k.lst": {Exact: []string{"only.example"}, Regex: []string{`^r\.example$`}}}
	c := ConfigFromSettings(cfg, nil, WithLists(src))
	got := map[string][]string{}
	for _, r := range c.Route.Rules {
		if r["outbound"] != "main" {
			continue
		}
		for _, k := range []string{"domain", "domain_suffix", "domain_keyword", "domain_regex"} {
			if v, ok := r[k].([]string); ok {
				got[k] = append(got[k], v...)
			}
		}
	}
	want := map[string]string{
		"domain":         "only.example",
		"domain_suffix":  "inline.example",
		"domain_keyword": "tracker",
		"domain_regex":   `^a.\.rule\.example$`,
	}
	for k, w := range want {
		found := false
		for _, v := range got[k] {
			found = found || v == w
		}
		if !found {
			t.Errorf("%s: %q missing in %v", k, w, got[k])
		}
	}
	if len(got["domain_regex"]) < 4 {
		t.Errorf("domain_regex: %v", got["domain_regex"])
	}
}

func TestComponentDownloadsGoThroughChosenSection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, Action: config.ActionTypeConnection, CommunityLists: []string{"x"}}}
	cfg.CommunityLists = []config.CommunityList{{Name: "x", Entries: []string{"github.com"}}}
	cfg.Settings.DownloadComponentsViaProxy = true
	cfg.Settings.DownloadComponentsViaProxySection = "main"

	c := ConfigFromSettings(cfg, testNodes(t))
	rules := c.Route.Rules
	at := -1
	for i, r := range rules {
		if in, ok := r["inbound"].([]string); ok && len(in) == 1 && in[0] == "service-mixed-in" {
			at = i
			if r["outbound"] != "main" {
				t.Errorf("mixed inbound routed to %v, want main", r["outbound"])
			}
		}
	}
	if at < 0 {
		t.Fatalf("no rule for service-mixed-in: %v", rules)
	}
	for _, r := range rules[:at] {
		if r["action"] != "sniff" && r["action"] != "hijack-dns" {
			t.Errorf("rule %v precedes the component download rule", r)
		}
	}

	cfg.Settings.DownloadComponentsViaProxySection = "missing"
	if c := ConfigFromSettings(cfg, testNodes(t)); !warned(c, "download_components_via_proxy_section") {
		t.Errorf("expected a warning for a missing section, got %v", c.Warnings)
	}
}

func TestCoreNodesReachTheirCore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, Action: config.ActionTypeConnection, CommunityLists: []string{"x"}}}
	cfg.CommunityLists = []config.CommunityList{{Name: "x", Entries: []string{"example.com"}}}
	link := "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?type=xhttp&security=tls#XH"
	obs, err := subscription.ParseSubscription(link)
	if err != nil || len(obs) != 1 || obs[0].Core != "xray" {
		t.Fatalf("parse: %v %+v", err, obs)
	}
	nodes := fakeNodes{"main": obs}

	if c := ConfigFromSettings(cfg, nodes); !warned(c, "need the core manager") {
		t.Errorf("without a plan the node must be skipped with a warning: %v", c.Warnings)
	}

	plan := cores.NewPlan([]cores.Node{{Kind: cores.Xray, Name: "XH", Link: link}}, cores.Binaries{cores.Xray: "/bin/xray"})
	c := ConfigFromSettings(cfg, nodes, WithCores(plan))
	found := false
	for _, ob := range c.Outbounds {
		if ob["tag"] == "XH" {
			found = ob["type"] == "socks" && ob["server_port"] == cores.BasePort
		}
	}
	if !found {
		t.Errorf("no socks outbound for the xray node: %v", c.Outbounds)
	}
}

func TestDNSSectionsRouteMatchedDomains(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.DNSServers = []string{"77.88.8.8", "77.88.8.1"}
	cfg.Sections = []config.Section{
		{
			Name:       "kids",
			Enabled:    true,
			Action:     config.ActionTypeDNS,
			DNSServers: []string{"94.140.14.14", "94.140.15.15"},
			Domains:    []string{"youtube.com", "full:www.example.org"},
		},
		{
			Name:       "single",
			Enabled:    true,
			Action:     config.ActionTypeDNS,
			DNSServers: []string{"9.9.9.9"},
			Domains:    []string{"example.net"},
		},
	}

	c := ConfigFromSettings(cfg, testNodes(t))
	out := render(t, c)

	dns, ok := out["dns"].(map[string]interface{})
	if !ok {
		t.Fatalf("no dns section in %v", out)
	}
	servers := dns["servers"].([]interface{})
	rules := dns["rules"].([]interface{})

	serverTags := map[string]bool{}
	for _, s := range servers {
		serverTags[s.(map[string]interface{})["tag"].(string)] = true
	}
	for _, tag := range []string{"dns-kids-0", "dns-kids-1", "dns-single-0"} {
		if !serverTags[tag] {
			t.Fatalf("missing DNS server %q, have %v", tag, serverTags)
		}
	}

	// Collect the DNS rules that carry the kids section's domain matchers.
	type rule struct {
		action, server, tag, matchResponse, rcode string
		domainSuffix                              []string
	}
	var kids []rule
	for _, raw := range rules {
		m := raw.(map[string]interface{})
		ds, _ := m["domain_suffix"].([]interface{})
		var suffix []string
		for _, v := range ds {
			if s, ok := v.(string); ok {
				suffix = append(suffix, s)
			}
		}
		if len(suffix) == 0 || suffix[0] != "youtube.com" {
			continue
		}
		kids = append(kids, rule{
			action:        str(m["action"]),
			server:        str(m["server"]),
			tag:           str(m["tag"]),
			matchResponse: str(m["match_response"]),
			rcode:         str(m["response_rcode"]),
			domainSuffix:  suffix,
		})
	}

	// Two servers: evaluate each in parallel, then respond to the first that
	// answered NOERROR or NXDOMAIN, falling back in order.
	var evals []rule
	var responds []rule
	for _, r := range kids {
		switch r.action {
		case "evaluate":
			evals = append(evals, r)
		case "respond":
			responds = append(responds, r)
		}
	}
	if len(evals) != 2 {
		t.Fatalf("want 2 evaluate rules, got %d: %v", len(evals), kids)
	}
	if evals[0].server != "dns-kids-0" || evals[0].tag != "dns-kids-0-eval" {
		t.Errorf("primary evaluate = %+v, want server dns-kids-0 tag dns-kids-0-eval", evals[0])
	}
	if evals[1].server != "dns-kids-1" || evals[1].tag != "dns-kids-1-eval" {
		t.Errorf("fallback evaluate = %+v, want server dns-kids-1 tag dns-kids-1-eval", evals[1])
	}
	if len(responds) != 4 {
		t.Fatalf("want 4 respond rules (2 servers x NOERROR/NXDOMAIN), got %d: %v", len(responds), kids)
	}
	want := [][2]string{
		{"dns-kids-0-eval", "NOERROR"},
		{"dns-kids-0-eval", "NXDOMAIN"},
		{"dns-kids-1-eval", "NOERROR"},
		{"dns-kids-1-eval", "NXDOMAIN"},
	}
	for i, w := range want {
		if responds[i].matchResponse != w[0] || responds[i].rcode != w[1] {
			t.Errorf("respond[%d] = %+v, want match_response %s response_rcode %s", i, responds[i], w[0], w[1])
		}
	}

	// A single server is routed directly.
	var single bool
	for _, raw := range rules {
		m := raw.(map[string]interface{})
		ds, _ := m["domain_suffix"].([]interface{})
		if len(ds) == 1 && ds[0] == "example.net" {
			single = true
			if str(m["action"]) != "route" || str(m["server"]) != "dns-single-0" {
				t.Errorf("single-server rule = %v, want action route server dns-single-0", m)
			}
		}
	}
	if !single {
		t.Errorf("no route rule for the single-server DNS section: %v", rules)
	}
}

// str reads a JSON string field that may be nil.
func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
