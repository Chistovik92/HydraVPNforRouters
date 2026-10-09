package singbox

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/lists"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
)

const (
	directTag = "direct-out"
	rejectTag = "\x00reject" // pseudo target: rendered as the "reject" action
)

// NodeSource provides the proxy nodes of a section (subscription manager).
type NodeSource interface {
	GetOutbounds(section string) []subscription.OutboundInfo
}

// ConfigFromSettings builds the sing-box configuration from the HydraVPN
// settings: one selector (and an optional url-test group) per section with
// its subscription nodes, routing rules from sections/rules/lists, inbound
// servers, DNS and the tproxy inbound the firewall redirects LAN traffic to.
// nodes may be nil (no subscriptions yet).
func ConfigFromSettings(cfg *config.Config, nodes NodeSource, opts ...Option) *Config {
	var o options
	for _, f := range opts {
		f(&o)
	}
	// Fallback chain: without inbound servers/endpoints (they may need a
	// sing-box build with extra features), then without any sections.
	// The reduced configs are built lazily: they are rarely needed and a
	// large subscription is expensive to render.
	c := build(cfg, nodes, o, true, true)
	c.fallbacks = []func() *Config{
		func() *Config { return build(cfg, nodes, o, true, false) },
		func() *Config { return build(cfg, nil, o, false, false) },
	}
	return c
}

// ListSource provides downloaded plain-text lists (.lst).
type ListSource interface {
	ListEntries(url string) (domains, cidrs []string, ok bool)
}

// RuleSource is a ListSource that also serves exact, keyword and regexp
// domain rules; the builder uses it when available.
type RuleSource interface {
	ListRules(url string) (lists.Entries, bool)
}

type options struct{ lists ListSource }

// Option customizes ConfigFromSettings.
type Option func(*options)

// WithLists supplies downloaded .lst lists.
func WithLists(l ListSource) Option { return func(o *options) { o.lists = l } }

type builder struct {
	cfg   *config.Config
	nodes NodeSource
	lists ListSource
	c     *Config

	used     map[string]bool   // outbound tags in use
	suffix   map[string]int    // next numeric suffix per base tag
	groups   map[string]string // section -> its outbound tag
	ruleSets map[string]string // url -> rule-set tag
	detours  map[string]string // node tag -> detour section
}

func build(cfg *config.Config, nodes NodeSource, o options, sections, servers bool) *Config {
	b := &builder{
		cfg:      cfg,
		nodes:    nodes,
		lists:    o.lists,
		used:     map[string]bool{directTag: true},
		suffix:   map[string]int{},
		groups:   map[string]string{},
		ruleSets: map[string]string{},
		detours:  map[string]string{},
	}
	b.c = &Config{
		BinaryPath: cfg.Settings.SingBoxBinary,
		ConfigPath: cfg.Settings.ConfigPath,
		PidFile:    config.PidFile(cfg, "singbox"),
		LogLevel:   cfg.Settings.LogLevel,
		DNS:        &DNSConfig{Servers: []DNSServer{}},
	}
	c := b.c

	// A single tproxy inbound; the firewall redirects LAN traffic to it.
	c.Inbounds = []Inbound{
		{"type": "tproxy", "tag": "tproxy-in", "listen": tproxyListen(cfg), "listen_port": config.TProxyPort},
		{"type": "direct", "tag": "dns-in", "listen": config.DNSListenAddress, "listen_port": 53},
		{"type": "mixed", "tag": "service-mixed-in", "listen": "127.0.0.1", "listen_port": config.MixedProxyPort},
	}
	c.Outbounds = []Outbound{{"type": "direct", "tag": directTag}}
	for _, t := range []string{"tproxy-in", "dns-in", "service-mixed-in"} {
		b.used[t] = true
	}

	c.Route = &Route{
		Rules: []Rule{
			{"action": "sniff"},
			{"inbound": []string{"dns-in"}, "action": "hijack-dns"},
			{"protocol": "dns", "action": "hijack-dns"},
		},
		Final: directTag,
	}
	if cfg.Settings.EnableOutputNetworkInterface && cfg.Settings.OutputNetworkInterface != "" {
		c.Route.DefaultInterface = cfg.Settings.OutputNetworkInterface
	} else {
		c.Route.AutoDetectInterface = true
	}
	if cfg.Settings.DisableQUIC {
		// Clients fall back from QUIC to TCP, which can then be routed by SNI.
		c.Route.Rules = append(c.Route.Rules, Rule{"protocol": "quic", "action": "reject"})
	}
	if cfg.Settings.ExcludeNTP {
		c.Route.Rules = append(c.Route.Rules, Rule{"protocol": "ntp", "action": "route", "outbound": directTag})
	}

	b.buildDNS()

	if sections {
		// Section tags are reserved first so node names cannot take them.
		for _, sec := range cfg.Sections {
			b.used[sec.Name] = true
		}
		if servers {
			b.buildServers()
		}
		for _, sec := range cfg.Sections {
			if sec.Enabled {
				b.buildSection(sec)
			}
		}
		b.finalize()
	}

	c.Experimental = &ExperimentalConfig{
		// The cache keeps the selected node across restarts.
		CacheFile: &CacheFileConfig{
			Enabled:     true,
			Path:        cfg.Settings.CachePath,
			StoreFakeIP: cfg.Settings.FakeIPEnabled,
		},
	}
	// Local-only Clash API: used by the management API for node status,
	// latency tests and manual selection (and by a dashboard if enabled).
	c.Experimental.ClashAPI = &ClashAPIConfig{ExternalController: config.ClashAPIAddress}
	return c
}

func (b *builder) warn(format string, args ...interface{}) {
	b.c.Warnings = append(b.c.Warnings, fmt.Sprintf(format, args...))
}

// uniqueTag returns base, or base with a numeric suffix if it is taken.
func (b *builder) uniqueTag(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "node"
	}
	tag := base
	if b.used[tag] {
		// Remember where the numbering stopped: identical names in a large
		// subscription would otherwise make this quadratic.
		i := b.suffix[base]
		if i < 2 {
			i = 2
		}
		for ; b.used[base+" "+strconv.Itoa(i)]; i++ {
		}
		b.suffix[base] = i + 1
		tag = base + " " + strconv.Itoa(i)
	}
	b.used[tag] = true
	return tag
}

func (b *builder) buildDNS() {
	cfg, c := b.cfg, b.c
	for i, server := range cfg.Settings.DNSServers {
		c.DNS.Servers = append(c.DNS.Servers, dnsServer(fmt.Sprintf("dns-server-%d", i), server, cfg.Settings.DNSType))
	}
	for i, server := range cfg.Settings.BootstrapDNSServers {
		c.DNS.Servers = append(c.DNS.Servers, dnsServer(fmt.Sprintf("bootstrap-dns-%d", i), server, "udp"))
	}
	if len(cfg.Settings.DNSServers) > 0 {
		c.DNS.Final = "dns-server-0"
	} else if len(cfg.Settings.BootstrapDNSServers) > 0 {
		c.DNS.Final = "bootstrap-dns-0"
	}
	// Host-name DNS servers (DoH/DoT) and outbounds need a resolver:
	// the first bootstrap server, or the system resolver without one.
	resolver := "bootstrap-dns-0"
	if len(cfg.Settings.BootstrapDNSServers) == 0 {
		resolver = "local-dns"
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{Type: "local", Tag: resolver})
		if c.DNS.Final == "" {
			c.DNS.Final = resolver
		}
	}
	c.Route.DefaultDomainResolver = resolver
	for i := range c.DNS.Servers {
		if s := &c.DNS.Servers[i]; s.Server != "" && net.ParseIP(s.Server) == nil {
			s.DomainResolver = resolver
		}
	}
	c.DNS.Strategy = string(cfg.Settings.DNSStrategy)

	if cfg.Settings.FakeIPEnabled {
		c.DNS.Servers = append(c.DNS.Servers, DNSServer{
			Type:       "fakeip",
			Tag:        "fakeip",
			Inet4Range: "198.18.0.0/15",
			Inet6Range: "fc00::/18",
		})
		c.DNS.Rules = append(c.DNS.Rules, DNSRule{QueryType: []string{"A", "AAAA"}, Server: "fakeip"})
	}
}

// dnsServer converts "1.1.1.1", "1.1.1.1:5353", "[2606:4700::1111]",
// "tls://dns.example" or "https://dns.example/dns-query" into a typed server.
func dnsServer(tag, address, defaultType string) DNSServer {
	s := DNSServer{Tag: tag, Type: defaultType}
	switch s.Type {
	case "udp", "tcp", "tls", "https", "quic", "h3":
	default:
		s.Type = "udp"
	}

	if strings.Contains(address, "://") {
		if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
			s.Type = u.Scheme
			s.Server = u.Hostname()
			if port, err := strconv.Atoi(u.Port()); err == nil {
				s.ServerPort = port
			}
			if (s.Type == "https" || s.Type == "h3") && u.Path != "" && u.Path != "/dns-query" {
				s.Path = u.Path
			}
			return s
		}
	}

	if host, port, err := net.SplitHostPort(address); err == nil {
		s.Server = host
		if p, err := strconv.Atoi(port); err == nil {
			s.ServerPort = p
		}
		return s
	}
	s.Server = strings.Trim(address, "[]")
	return s
}

// ---------------------------------------------------------------- sections

// target is the outbound a section sends matched traffic to.
func (b *builder) buildSection(sec config.Section) {
	target := ""
	switch sec.Action {
	case config.ActionTypeBlock:
		target = rejectTag
	case config.ActionTypeBypass:
		target = directTag
	default:
		provider := sec.Provider
		if provider == "" || provider == config.ProviderTypeAuto {
			provider = config.ProviderTypeSingBox
		}
		switch provider {
		case config.ProviderTypeByeDPI:
			b.c.Outbounds = append(b.c.Outbounds, Outbound{
				"type": "socks", "tag": sec.Name, "version": "5",
				"server": "127.0.0.1", "server_port": config.ByeDPIPort,
			})
			b.groups[sec.Name] = sec.Name
			target = sec.Name
		case config.ProviderTypeZapret, config.ProviderTypeZapret2:
			// nfqws works on packets; the connection itself stays direct.
			target = directTag
		default:
			target = b.proxyGroup(sec)
		}
	}
	if target == "" {
		b.warn("section %q: no proxy nodes yet (waiting for the subscription); its traffic goes direct", sec.Name)
		return
	}

	matched := 0
	add := func(r Rule) {
		if target == rejectTag {
			r["action"] = "reject"
		} else {
			r["action"] = "route"
			r["outbound"] = target
		}
		b.c.Route.Rules = append(b.c.Route.Rules, r)
		matched++
	}

	if len(sec.FullyRoutedIPs) > 0 {
		add(Rule{"source_ip_cidr": sec.FullyRoutedIPs})
	}
	for _, r := range b.cfg.Rules {
		if r.Enabled && r.Section == sec.Name {
			b.addConfigRule(sec, target, r)
		}
	}

	for _, r := range inlineDomainRules(sec.Domains) {
		add(r)
	}
	for _, name := range sec.CommunityLists {
		for _, r := range b.listRules(name) {
			add(r)
		}
	}
	for _, name := range append(append([]string(nil), sec.RuleSet...), sec.RuleSetWithSubnets...) {
		for _, r := range b.listRules(name) {
			add(r)
		}
	}
	if matched == 0 && !hasConfigRules(b.cfg, sec.Name) && sec.Action != config.ActionTypeBypass {
		b.warn("section %q has no rules, lists or fully_routed_ips: no traffic is routed through it", sec.Name)
	}
}

func hasConfigRules(cfg *config.Config, section string) bool {
	for _, r := range cfg.Rules {
		if r.Enabled && r.Section == section {
			return true
		}
	}
	return false
}

// addConfigRule converts a "rules:" entry.
func (b *builder) addConfigRule(sec config.Section, target string, r config.Rule) {
	out := Rule{}
	setList := func(key string, vals []string) {
		if vals = clean(vals); len(vals) > 0 {
			out[key] = vals
		}
	}
	setList("domain", r.Domain)
	setList("domain_suffix", r.DomainSuffix)
	setList("domain_keyword", r.DomainKeyword)
	regex := append([]string(nil), r.DomainRegex...)
	for _, w := range r.DomainWildcard {
		if re := lists.WildcardToRegexp(w); re != "" {
			regex = append(regex, re)
		}
	}
	setList("domain_regex", regex)
	setList("ip_cidr", r.Destination)
	setList("source_ip_cidr", r.Source)
	if r.Protocol != "" {
		out["protocol"] = strings.Fields(strings.ReplaceAll(r.Protocol, ",", " "))
	}
	if r.Network != "" {
		out["network"] = r.Network
	}
	if r.Inbound != "" {
		out["inbound"] = []string{r.Inbound}
	}
	if ports := parsePorts(r.Port); len(ports) > 0 {
		out["port"] = ports
	}
	if r.PortRange != "" {
		out["port_range"] = strings.Fields(strings.ReplaceAll(r.PortRange, ",", " "))
	}
	if len(r.GeoIP) > 0 || len(r.GeoSite) > 0 {
		b.warn("section %q: geoip/geosite rules are not supported by sing-box 1.12+, use rule_set instead", sec.Name)
	}
	if r.Process != "" || r.ProcessPath != "" || r.UID != "" || r.GID != "" {
		b.warn("section %q: process/uid/gid rules cannot match forwarded LAN traffic and were skipped", sec.Name)
	}
	if len(out) == 0 {
		b.warn("section %q: a rule without matchers was skipped (it would match everything)", sec.Name)
		return
	}
	if r.Invert {
		out["invert"] = true
	}

	t := target
	switch strings.ToLower(r.Outbound) {
	case "direct":
		t = directTag
	case "block":
		t = rejectTag
	}
	if t == rejectTag {
		out["action"] = "reject"
	} else {
		out["action"] = "route"
		out["outbound"] = t
	}
	b.c.Route.Rules = append(b.c.Route.Rules, out)
}

func clean(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parsePorts(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if p, err := strconv.Atoi(f); err == nil && p > 0 && p < 65536 {
			out = append(out, p)
		}
	}
	return out
}

// listRules resolves a community list or rule set name (or a rule set URL)
// to routing rules.
func (b *builder) listRules(name string) []Rule {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.Contains(name, "://") {
		return b.remoteRule(name, 0, false)
	}
	found := false
	var rules []Rule
	for _, l := range b.cfg.CommunityLists {
		if l.Name != name {
			continue
		}
		found = true
		if len(l.Entries) > 0 {
			key := "domain_suffix"
			if t := strings.ToLower(l.Type); t == "ip" || t == "subnet" || t == "cidr" {
				key = "ip_cidr"
			}
			r := Rule{key: clean(l.Entries)}
			if l.Invert {
				r["invert"] = true
			}
			rules = append(rules, r)
		}
		if l.URL != "" {
			rules = append(rules, b.remoteRule(l.URL, l.Interval, l.Invert)...)
		}
	}
	for _, rs := range b.cfg.RuleSets {
		if rs.Name != name {
			continue
		}
		found = true
		rules = append(rules, b.remoteRule(rs.URL, rs.Interval, false)...)
	}
	if !found {
		b.warn("list %q is not defined in community_lists or rule_sets", name)
	}
	return rules
}

// remoteRule registers a remote sing-box rule set and returns a rule using it.
// Only .srs (binary) and .json (source) files can be used as rule sets.
func (b *builder) remoteRule(u string, interval time.Duration, invert bool) []Rule {
	if lists.NeedsDownload(u) {
		return b.localList(u, invert)
	}
	tag, ok := b.ruleSets[u]
	if !ok {
		format := ""
		switch strings.ToLower(path.Ext(strings.SplitN(u, "?", 2)[0])) {
		case ".srs":
			format = "binary"
		case ".json":
			format = "source"
		default:
			b.warn("list %s: only .srs and .json rule sets are supported, skipped", u)
			return nil
		}
		if interval <= 0 {
			interval = b.cfg.Settings.UpdateInterval
		}
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		sum := sha1.Sum([]byte(u))
		base := strings.TrimSuffix(path.Base(strings.SplitN(u, "?", 2)[0]), path.Ext(path.Base(u)))
		tag = "rs-" + base + "-" + hex.EncodeToString(sum[:3])
		b.ruleSets[u] = tag
		b.c.Route.RuleSet = append(b.c.Route.RuleSet, Rule{
			"type":            "remote",
			"tag":             tag,
			"format":          format,
			"url":             u,
			"download_detour": directTag,
			"update_interval": interval.String(),
		})
	}
	r := Rule{"rule_set": []string{tag}}
	if invert {
		r["invert"] = true
	}
	return []Rule{r}
}

// localList turns a downloaded .lst list into inline domain / subnet rules.
func (b *builder) localList(u string, invert bool) []Rule {
	if b.lists == nil {
		b.warn("list %s: not downloaded yet", config.MaskURL(u))
		return nil
	}
	var e lists.Entries
	var ok bool
	if rs, isRS := b.lists.(RuleSource); isRS {
		e, ok = rs.ListRules(u)
	} else {
		e.Suffix, e.CIDR, ok = b.lists.ListEntries(u)
	}
	if !ok {
		b.warn("list %s: not downloaded yet", config.MaskURL(u))
		return nil
	}
	rules := entryRules(e)
	if invert {
		for _, r := range rules {
			r["invert"] = true
		}
	}
	return rules
}

// BuildOutbound converts a parsed proxy link into a sing-box outbound.
func BuildOutbound(info subscription.OutboundInfo) (map[string]interface{}, error) {
	ob, err := buildOutbound(info)
	return ob, err
}

// entryRules converts parsed list entries into sing-box rules, one per kind.
func entryRules(e lists.Entries) []Rule {
	var rules []Rule
	for _, kv := range []struct {
		key  string
		vals []string
	}{
		{"domain_suffix", e.Suffix}, {"domain", e.Exact}, {"domain_keyword", e.Keyword},
		{"domain_regex", e.Regex}, {"ip_cidr", e.CIDR},
	} {
		if len(kv.vals) > 0 {
			rules = append(rules, Rule{kv.key: kv.vals})
		}
	}
	return rules
}

// inlineDomainRules converts a section's "domains:" entries (same syntax as a
// .lst list: namespace:, full:, keyword:, wildcard:, regexp:, subnets).
func inlineDomainRules(entries []string) []Rule {
	if len(entries) == 0 {
		return nil
	}
	return entryRules(lists.ParseEntries(strings.Join(entries, "\n")))
}

// proxyGroup builds the selector (and url-test) outbounds of a section and
// returns the selector tag, or "" if the section has no usable nodes.
func (b *builder) proxyGroup(sec config.Section) string {
	var infos []subscription.OutboundInfo
	if b.nodes != nil {
		infos = append(infos, b.nodes.GetOutbounds(sec.Name)...)
	}
	for _, link := range sec.SelectorProxyLinks {
		if obs, err := subscription.ParseSubscription(link); err == nil {
			infos = append(infos, obs...)
		} else {
			b.warn("section %q: bad selector_proxy_link: %v", sec.Name, err)
		}
	}
	for _, raw := range sec.OutboundJsons {
		if obs, err := subscription.ParseSubscription(raw); err == nil {
			infos = append(infos, obs...)
		} else {
			b.warn("section %q: bad outbound_json: %v", sec.Name, err)
		}
	}

	type node struct {
		tag  string
		name string
	}
	var nodes []node
	for _, info := range infos {
		ob, err := buildOutbound(info)
		if err != nil {
			b.warn("section %q: node %q skipped: %v", sec.Name, nodeName(info), err)
			continue
		}
		tag := b.uniqueTag(nodeName(info))
		ob["tag"] = tag
		if sec.OutboundDetourEnabled && sec.OutboundDetourSection != "" && sec.OutboundDetourSection != sec.Name {
			b.detours[tag] = sec.OutboundDetourSection
		}
		b.c.Outbounds = append(b.c.Outbounds, ob)
		nodes = append(nodes, node{tag: tag, name: nodeName(info)})
	}
	if len(nodes) == 0 {
		return ""
	}

	members := make([]string, 0, len(nodes))
	ut := b.urltestFor(sec.Name)
	for _, n := range nodes {
		if ut == nil || urltestAllows(ut, n.name) {
			members = append(members, n.tag)
		}
	}
	if len(members) == 0 {
		members = append(members, nodes[0].tag)
	}

	selector := []string{}
	def := nodes[0].tag
	if len(members) > 1 || ut != nil {
		autoTag := sec.Name + "-auto"
		if ut != nil && ut.Name != "" {
			autoTag = ut.Name
		}
		autoTag = b.uniqueTag(autoTag)
		test := Outbound{
			"type":      "urltest",
			"tag":       autoTag,
			"outbounds": members,
			"url":       firstNonEmpty(testURL(ut), b.cfg.Settings.LatencyTestURL),
			"interval":  durationOr(ut, func(u *config.URLTest) time.Duration { return u.CheckInterval }, 3*time.Minute),
			"tolerance": toleranceOr(ut, 50),
		}
		if ut != nil {
			if ut.IdleTimeout > 0 {
				test["idle_timeout"] = ut.IdleTimeout.String()
			}
			if ut.InterruptExistConnections {
				test["interrupt_exist_connections"] = true
			}
		}
		b.c.Outbounds = append(b.c.Outbounds, test)
		selector = append(selector, autoTag)
		def = autoTag
	}
	for _, n := range nodes {
		selector = append(selector, n.tag)
	}
	b.c.Outbounds = append(b.c.Outbounds, Outbound{
		"type": "selector", "tag": sec.Name, "outbounds": selector, "default": def,
	})
	b.groups[sec.Name] = sec.Name
	return sec.Name
}

func (b *builder) urltestFor(section string) *config.URLTest {
	for i := range b.cfg.URLTests {
		if b.cfg.URLTests[i].Section == section {
			return &b.cfg.URLTests[i]
		}
	}
	return nil
}

func urltestAllows(u *config.URLTest, name string) bool {
	country := CountryOf(name)
	if len(u.IncludeCountries) > 0 && !matchCountry(u.IncludeCountries, country) {
		return false
	}
	if matchCountry(u.ExcludeCountries, country) {
		return false
	}
	if len(u.IncludeOutbounds) > 0 && !contains(u.IncludeOutbounds, name) {
		return false
	}
	if contains(u.ExcludeOutbounds, name) {
		return false
	}
	if len(u.IncludeRegex) > 0 && !matchesAny(u.IncludeRegex, name) {
		return false
	}
	if matchesAny(u.ExcludeRegex, name) {
		return false
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func matchesAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(s) {
			return true
		}
	}
	return false
}

func testURL(u *config.URLTest) string {
	if u == nil {
		return ""
	}
	return u.TestingURL
}

func durationOr(u *config.URLTest, get func(*config.URLTest) time.Duration, def time.Duration) string {
	if u != nil {
		if d := get(u); d > 0 {
			return d.String()
		}
	}
	return def.String()
}

func toleranceOr(u *config.URLTest, def int) int {
	if u != nil && u.Tolerance > 0 {
		return u.Tolerance
	}
	return def
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// finalize resolves references that need all sections to exist.
func (b *builder) finalize() {
	for _, ob := range b.c.Outbounds {
		tag, _ := ob["tag"].(string)
		if sec, ok := b.detours[tag]; ok {
			if g, ok := b.groups[sec]; ok && g != tag {
				ob["detour"] = g
			} else {
				b.warn("node %q: detour section %q has no outbound, ignored", tag, sec)
			}
		}
	}
	if b.cfg.Settings.DownloadListsViaProxy {
		if g, ok := b.groups[b.cfg.Settings.DownloadListsViaProxySection]; ok {
			for _, rs := range b.c.Route.RuleSet {
				rs["download_detour"] = g
			}
		}
	}
}

// ---------------------------------------------------------------- nodes

func nodeName(info subscription.OutboundInfo) string {
	switch {
	case info.Name != "":
		return info.Name
	case info.Tag != "":
		return info.Tag
	case info.Server != "":
		return info.Type + "-" + info.Server + ":" + strconv.Itoa(info.Port)
	}
	return info.Type
}

// buildOutbound converts a parsed subscription node to a sing-box outbound.
func buildOutbound(info subscription.OutboundInfo) (Outbound, error) {
	if info.Raw != nil {
		ob := Outbound{}
		for k, v := range info.Raw {
			ob[k] = v
		}
		return ob, nil
	}

	ob := Outbound{"type": info.Type, "server": info.Server, "server_port": info.Port}
	switch info.Type {
	case "vless":
		ob["uuid"] = info.UUID
		if info.Flow != "" {
			ob["flow"] = info.Flow
		}
		ob["packet_encoding"] = "xudp"
	case "vmess":
		ob["uuid"] = info.UUID
		ob["security"] = firstNonEmpty(info.Method, "auto")
		ob["alter_id"] = 0
	case "trojan":
		ob["password"] = info.Password
	case "shadowsocks":
		ob["method"] = info.Method
		ob["password"] = info.Password
	case "hysteria2":
		ob["password"] = info.Password
	default:
		return nil, fmt.Errorf("unsupported protocol %q", info.Type)
	}

	if tls := buildTLS(info); tls != nil {
		ob["tls"] = tls
	}
	if info.Type != "shadowsocks" && info.Type != "hysteria2" {
		tr, err := buildTransport(info)
		if err != nil {
			return nil, err
		}
		if tr != nil {
			ob["transport"] = tr
		}
	}
	return ob, nil
}

func buildTLS(info subscription.OutboundInfo) map[string]interface{} {
	sec := strings.ToLower(info.Security)
	enabled := sec == "tls" || sec == "reality" || info.Type == "hysteria2"
	if !enabled {
		return nil
	}
	tls := map[string]interface{}{"enabled": true}
	sni := info.SNI
	if sni == "" && info.Host != "" {
		sni = info.Host
	}
	if sni == "" && net.ParseIP(info.Server) == nil {
		sni = info.Server
	}
	if sni != "" {
		tls["server_name"] = sni
	}
	if info.Extra["insecure"] == "1" {
		tls["insecure"] = true
	}
	if alpn := clean(strings.Split(info.Extra["alpn"], ",")); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	fp := info.Fingerprint
	if sec == "reality" && fp == "" {
		fp = "chrome"
	}
	if fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}
	if sec == "reality" {
		tls["reality"] = map[string]interface{}{
			"enabled":    true,
			"public_key": info.PublicKey,
			"short_id":   info.ShortID,
		}
	}
	return tls
}

func buildTransport(info subscription.OutboundInfo) (map[string]interface{}, error) {
	path := info.Path
	switch t := strings.ToLower(info.Transport); t {
	case "", "tcp", "raw":
		return nil, nil
	case "ws", "websocket":
		tr := map[string]interface{}{"type": "ws", "path": firstNonEmpty(path, "/")}
		if info.Host != "" {
			tr["headers"] = map[string]interface{}{"Host": info.Host}
		}
		return tr, nil
	case "grpc":
		return map[string]interface{}{"type": "grpc", "service_name": firstNonEmpty(info.Extra["serviceName"], path)}, nil
	case "http", "h2":
		tr := map[string]interface{}{"type": "http", "path": firstNonEmpty(path, "/")}
		if info.Host != "" {
			tr["host"] = []string{info.Host}
		}
		return tr, nil
	case "httpupgrade":
		tr := map[string]interface{}{"type": "httpupgrade", "path": firstNonEmpty(path, "/")}
		if info.Host != "" {
			tr["host"] = info.Host
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", t)
	}
}

// ---------------------------------------------------------------- servers

// buildServers converts inbound "servers:" entries to sing-box inbounds.
func (b *builder) buildServers() {
	for _, srv := range b.cfg.Servers {
		if !srv.Enabled {
			continue
		}
		if srv.Protocol == "tailscale" {
			b.buildTailscale(srv)
			continue
		}
		if srv.Protocol == "mtproto" && srv.InboundJSON == "" {
			b.warn("server %q: MTProto is not supported by sing-box; use a separate MTProto proxy", srv.Name)
			continue
		}
		in, err := buildInbound(srv)
		if err != nil {
			b.warn("server %q skipped: %v", srv.Name, err)
			continue
		}
		tag := b.uniqueTag(srv.Name)
		in["tag"] = tag
		b.c.Inbounds = append(b.c.Inbounds, in)

		switch srv.RoutingMode {
		case config.RoutingModeDirect:
			b.c.Route.Rules = append(b.c.Route.Rules, Rule{"inbound": []string{tag}, "action": "route", "outbound": directTag})
		case config.RoutingModeBlock:
			b.c.Route.Rules = append(b.c.Route.Rules, Rule{"inbound": []string{tag}, "action": "reject"})
		}
	}
}

func buildInbound(srv config.Server) (Inbound, error) {
	if srv.InboundJSON != "" {
		in := Inbound{}
		if err := json.Unmarshal([]byte(srv.InboundJSON), &in); err != nil {
			return nil, fmt.Errorf("inbound_json: %w", err)
		}
		return in, nil
	}
	if srv.ListenPort <= 0 {
		return nil, fmt.Errorf("listen_port is required")
	}
	in := Inbound{
		"type":        srv.Protocol,
		"listen":      firstNonEmpty(srv.Listen, "::"),
		"listen_port": srv.ListenPort,
	}
	switch srv.Protocol {
	case "tailscale":
		return nil, fmt.Errorf("tailscale is an endpoint")
	case "vless":
		user := map[string]interface{}{"uuid": srv.ServerUUID}
		if srv.VLESSFlow != "" && srv.VLESSFlow != "none" {
			user["flow"] = srv.VLESSFlow
		}
		in["users"] = []interface{}{user}
	default:
		return nil, fmt.Errorf("protocol %q needs inbound_json (only vless is generated automatically)", srv.Protocol)
	}
	if srv.Security == "reality" {
		port := srv.RealityHandshakeServerPort
		if port == 0 {
			port = 443
		}
		reality := map[string]interface{}{
			"enabled":     true,
			"handshake":   map[string]interface{}{"server": srv.RealityHandshakeServer, "server_port": port},
			"private_key": srv.RealityPrivateKey,
			"short_id":    []string{srv.RealityShortID},
		}
		if srv.RealityMaxTimeDifference != "" {
			reality["max_time_difference"] = srv.RealityMaxTimeDifference
		}
		in["tls"] = map[string]interface{}{
			"enabled":     true,
			"server_name": firstNonEmpty(srv.TLSServerName, srv.RealityHandshakeServer),
			"reality":     reality,
		}
	}
	return in, nil
}

// buildTailscale adds a sing-box tailscale endpoint (needs a sing-box build
// with the with_tailscale tag; "sing-box check" rejects the config otherwise
// and the fallback chain drops it).
func (b *builder) buildTailscale(srv config.Server) {
	ep := Outbound{
		"type":            "tailscale",
		"tag":             b.uniqueTag(srv.Name),
		"state_directory": filepath.Join(filepath.Dir(b.cfg.Settings.ConfigPath), "tailscale-"+srv.Name),
	}
	set := func(k, v string) {
		if v != "" {
			ep[k] = v
		}
	}
	set("auth_key", srv.TailscaleAuthKey)
	set("control_url", srv.TailscaleControlURL)
	set("hostname", srv.TailscaleHostname)
	if srv.TailscaleAcceptRoutes {
		ep["accept_routes"] = true
	}
	if srv.TailscaleAdvertiseExitNode {
		ep["advertise_exit_node"] = true
	}
	if routes := clean(srv.TailscaleAdvertiseRoutes); len(routes) > 0 {
		ep["advertise_routes"] = routes
	}
	b.c.Endpoints = append(b.c.Endpoints, ep)
}

// tproxyListen is dual-stack when IPv6 interception is enabled.
func tproxyListen(cfg *config.Config) string {
	if cfg.Settings.EnableIPv6 {
		return "::"
	}
	return "0.0.0.0"
}
