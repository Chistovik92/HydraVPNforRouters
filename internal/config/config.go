package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderType represents the DPI bypass provider type
type ProviderType string

const (
	ProviderTypeSingBox ProviderType = "singbox"
	ProviderTypeZapret  ProviderType = "zapret"
	ProviderTypeZapret2 ProviderType = "zapret2"
	ProviderTypeByeDPI  ProviderType = "byedpi"
	ProviderTypeAuto    ProviderType = "auto"
)

// Well-known paths and ports shared by all components.
const (
	DefaultConfigDir  = "/etc/hydravpn-router"
	DefaultConfigFile = DefaultConfigDir + "/config.yaml"
	DefaultRuntimeDir = "/var/run/hydravpn-router"
	DefaultCacheDir   = "/tmp/hydravpn-router"

	// TProxyPort is the port of the sing-box tproxy inbound used by the firewall.
	TProxyPort = 1602
	// DNSListenAddress is where the sing-box DNS inbound listens.
	DNSListenAddress = "127.0.0.42"
	// MixedProxyPort is the port of the local sing-box mixed (HTTP/SOCKS) inbound.
	MixedProxyPort = 4534
	// ByeDPIPort is the SOCKS5 port of the local ByeDPI (ciadpi) instance;
	// sections using the byedpi provider are routed to it by sing-box.
	ByeDPIPort = 1080
)

// ActionType represents what action a section performs
type ActionType string

const (
	ActionTypeConnection ActionType = "connection"
	ActionTypeBypass     ActionType = "bypass"
	ActionTypeBlock      ActionType = "block"
)

// RoutingMode represents routing behavior
type RoutingMode string

const (
	RoutingModeRules  RoutingMode = "rules"
	RoutingModeDirect RoutingMode = "direct"
	RoutingModeBlock  RoutingMode = "block"
)

// DNSStrategy represents DNS resolution strategy
type DNSStrategy string

const (
	DNSStrategyPreferIPv4 DNSStrategy = "prefer_ipv4"
	DNSStrategyPreferIPv6 DNSStrategy = "prefer_ipv6"
	DNSStrategyIPv4Only   DNSStrategy = "ipv4_only"
	DNSStrategyIPv6Only   DNSStrategy = "ipv6_only"
)

// Config represents the main HydraVPN for Router configuration
type Config struct {
	Settings         Settings           `yaml:"settings" json:"settings"`
	Sections         []Section          `yaml:"sections" json:"sections"`
	Interfaces       []SectionInterface `yaml:"interfaces" json:"interfaces"`
	SubscriptionURLs []SubscriptionURL  `yaml:"subscription_urls" json:"subscription_urls"`
	URLTests         []URLTest          `yaml:"urltests" json:"urltests"`
	Servers          []Server           `yaml:"servers" json:"servers"`
	Rules            []Rule             `yaml:"rules" json:"rules"`
	RuleSets         []RuleSet          `yaml:"rule_sets" json:"rule_sets"`
	CommunityLists   []CommunityList    `yaml:"community_lists" json:"community_lists"`
}

// Settings represents global settings
type Settings struct {
	ConfigVersion                     string        `yaml:"config_version" json:"config_version"`
	AppliedMigrations                 []string      `yaml:"applied_migrations" json:"applied_migrations"`
	DNSType                           string        `yaml:"dns_type" json:"dns_type"`
	DNSServers                        []string      `yaml:"dns_server" json:"dns_server"`
	BootstrapDNSServers               []string      `yaml:"bootstrap_dns_server" json:"bootstrap_dns_server"`
	DNSCheckInterval                  time.Duration `yaml:"dns_check_interval" json:"dns_check_interval"`
	DNSRecoveryCheckInterval          time.Duration `yaml:"dns_recovery_check_interval" json:"dns_recovery_check_interval"`
	DNSCheckTimeout                   time.Duration `yaml:"dns_check_timeout" json:"dns_check_timeout"`
	DNSRewriteTTL                     int           `yaml:"dns_rewrite_ttl" json:"dns_rewrite_ttl"`
	DNSStrategy                       DNSStrategy   `yaml:"dns_strategy" json:"dns_strategy"`
	DNSDetourEnabled                  bool          `yaml:"dns_detour_enabled" json:"dns_detour_enabled"`
	DNSDetourSection                  string        `yaml:"dns_detour_section" json:"dns_detour_section"`
	FakeIPEnabled                     bool          `yaml:"fakeip_enabled" json:"fakeip_enabled"`
	SingBoxBinary                     string        `yaml:"singbox_binary" json:"singbox_binary"`
	SourceNetworkInterfaces           []string      `yaml:"source_network_interfaces" json:"source_network_interfaces"`
	EnableOutputNetworkInterface      bool          `yaml:"enable_output_network_interface" json:"enable_output_network_interface"`
	OutputNetworkInterface            string        `yaml:"output_network_interface" json:"output_network_interface"`
	EnableBadWANInterfaceMonitoring   bool          `yaml:"enable_badwan_interface_monitoring" json:"enable_badwan_interface_monitoring"`
	BadWANMonitoredInterfaces         []string      `yaml:"badwan_monitored_interfaces" json:"badwan_monitored_interfaces"`
	BadWANReloadDelay                 int           `yaml:"badwan_reload_delay" json:"badwan_reload_delay"`
	EnableYACD                        bool          `yaml:"enable_yacd" json:"enable_yacd"`
	DisableQUIC                       bool          `yaml:"disable_quic" json:"disable_quic"`
	ListUpdateEnabled                 bool          `yaml:"list_update_enabled" json:"list_update_enabled"`
	UpdateInterval                    time.Duration `yaml:"update_interval" json:"update_interval"`
	ComponentUpdateCheckEnabled       bool          `yaml:"component_update_check_enabled" json:"component_update_check_enabled"`
	ComponentUpdateCheckInterval      time.Duration `yaml:"component_update_check_interval" json:"component_update_check_interval"`
	LatencyTestURL                    string        `yaml:"latency_test_url" json:"latency_test_url"`
	DownloadListsViaProxy             bool          `yaml:"download_lists_via_proxy" json:"download_lists_via_proxy"`
	DownloadComponentsViaProxy        bool          `yaml:"download_components_via_proxy" json:"download_components_via_proxy"`
	DownloadListsViaProxySection      string        `yaml:"download_lists_via_proxy_section" json:"download_lists_via_proxy_section"`
	DownloadComponentsViaProxySection string        `yaml:"download_components_via_proxy_section" json:"download_components_via_proxy_section"`
	DontTouchDHCP                     bool          `yaml:"dont_touch_dhcp" json:"dont_touch_dhcp"`
	ConfigPath                        string        `yaml:"config_path" json:"config_path"`
	CachePath                         string        `yaml:"cache_path" json:"cache_path"`
	LogLevel                          string        `yaml:"log_level" json:"log_level"`
	ExcludeNTP                        bool          `yaml:"exclude_ntp" json:"exclude_ntp"`
	ShutdownCorrectly                 bool          `yaml:"shutdown_correctly" json:"shutdown_correctly"`
}

// Section represents a configuration section (subscription, json_outbound, etc.)
type Section struct {
	Name    string     `yaml:"name" json:"name"`
	Label   string     `yaml:"label" json:"label"`
	Enabled bool       `yaml:"enabled" json:"enabled"`
	Action  ActionType `yaml:"action" json:"action"`
	// Provider selects the DPI bypass engine used by this section
	// (singbox, zapret, zapret2, byedpi). Empty means singbox.
	Provider ProviderType `yaml:"provider" json:"provider"`
	// ProviderOptions overrides the default command line options of the
	// provider (nfqws/nfqws2/ciadpi strategy).
	ProviderOptions              string   `yaml:"provider_options" json:"provider_options"`
	SelectorProxyLinks           []string `yaml:"selector_proxy_links" json:"selector_proxy_links"`
	CommunityLists               []string `yaml:"community_lists" json:"community_lists"`
	RuleSet                      []string `yaml:"rule_set" json:"rule_set"`
	RuleSetWithSubnets           []string `yaml:"rule_set_with_subnets" json:"rule_set_with_subnets"`
	DashboardFilterMode          string   `yaml:"dashboard_filter_mode" json:"dashboard_filter_mode"`
	DashboardDetectServerCountry string   `yaml:"dashboard_detect_server_country" json:"dashboard_detect_server_country"`
	DashboardIncludeOutbounds    []string `yaml:"dashboard_include_outbounds" json:"dashboard_include_outbounds"`
	DashboardIncludeGroups       []string `yaml:"dashboard_include_groups" json:"dashboard_include_groups"`
	DashboardExcludeOutbounds    []string `yaml:"dashboard_exclude_outbounds" json:"dashboard_exclude_outbounds"`
	DashboardExcludeGroups       []string `yaml:"dashboard_exclude_groups" json:"dashboard_exclude_groups"`
	OutboundDetourEnabled        bool     `yaml:"outbound_detour_enabled" json:"outbound_detour_enabled"`
	OutboundDetourSection        string   `yaml:"outbound_detour_section" json:"outbound_detour_section"`
	SortByLatency                bool     `yaml:"sort_by_latency" json:"sort_by_latency"`
	FullyRoutedIPs               []string `yaml:"fully_routed_ips" json:"fully_routed_ips"`
	OutboundJsons                []string `yaml:"outbound_jsons" json:"outbound_jsons"`
}

// SectionInterface represents an interface bound to a section
type SectionInterface struct {
	Section                 string `yaml:"section" json:"section"`
	Name                    string `yaml:"name" json:"name"`
	DomainResolverEnabled   bool   `yaml:"domain_resolver_enabled" json:"domain_resolver_enabled"`
	DomainResolverDNSType   string `yaml:"domain_resolver_dns_type" json:"domain_resolver_dns_type"`
	DomainResolverDNSServer string `yaml:"domain_resolver_dns_server" json:"domain_resolver_dns_server"`
}

// SubscriptionURL represents a subscription source URL
type SubscriptionURL struct {
	Section                    string        `yaml:"section" json:"section"`
	URL                        string        `yaml:"url" json:"url"`
	AutoUserAgent              bool          `yaml:"auto_user_agent" json:"auto_user_agent"`
	UserAgent                  string        `yaml:"user_agent" json:"user_agent"`
	AutoHWID                   bool          `yaml:"auto_hwid" json:"auto_hwid"`
	SubscriptionUpdateEnabled  bool          `yaml:"subscription_update_enabled" json:"subscription_update_enabled"`
	SubscriptionUpdateInterval time.Duration `yaml:"subscription_update_interval" json:"subscription_update_interval"`
	DownloadViaProxyEnabled    bool          `yaml:"download_via_proxy_enabled" json:"download_via_proxy_enabled"`
	ShowDashboardMetadata      bool          `yaml:"show_dashboard_metadata" json:"show_dashboard_metadata"`
	PrefixNodes                bool          `yaml:"prefix_nodes" json:"prefix_nodes"`
	NodePrefix                 string        `yaml:"node_prefix" json:"node_prefix"`
	IncludeURLTestGroups       bool          `yaml:"include_urltest_groups" json:"include_urltest_groups"`
	HideURLTestGroupOutbounds  bool          `yaml:"hide_urltest_group_outbounds" json:"hide_urltest_group_outbounds"`
	HideDetourOutbounds        bool          `yaml:"hide_detour_outbounds" json:"hide_detour_outbounds"`
}

// URLTest represents a latency test group
type URLTest struct {
	Section                   string        `yaml:"section" json:"section"`
	Name                      string        `yaml:"name" json:"name"`
	CheckInterval             time.Duration `yaml:"check_interval" json:"check_interval"`
	Tolerance                 int           `yaml:"tolerance" json:"tolerance"`
	TestingURL                string        `yaml:"testing_url" json:"testing_url"`
	IdleTimeout               time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	InterruptExistConnections bool          `yaml:"interrupt_exist_connections" json:"interrupt_exist_connections"`
	PinDashboard              bool          `yaml:"pin_dashboard" json:"pin_dashboard"`
	FilterMode                string        `yaml:"filter_mode" json:"filter_mode"`
	DetectServerCountry       string        `yaml:"detect_server_country" json:"detect_server_country"`
	ExcludeCountries          []string      `yaml:"exclude_countries" json:"exclude_countries"`
	ExcludeOutbounds          []string      `yaml:"exclude_outbounds" json:"exclude_outbounds"`
	ExcludeRegex              []string      `yaml:"exclude_regex" json:"exclude_regex"`
	IncludeCountries          []string      `yaml:"include_countries" json:"include_countries"`
	IncludeOutbounds          []string      `yaml:"include_outbounds" json:"include_outbounds"`
	IncludeRegex              []string      `yaml:"include_regex" json:"include_regex"`
}

// Server represents an inbound server
type Server struct {
	Name        string      `yaml:"name" json:"name"`
	Label       string      `yaml:"label" json:"label"`
	Enabled     bool        `yaml:"enabled" json:"enabled"`
	Protocol    string      `yaml:"protocol" json:"protocol"`
	Listen      string      `yaml:"listen" json:"listen"`
	ListenPort  int         `yaml:"listen_port" json:"listen_port"`
	PublicHost  string      `yaml:"public_host" json:"public_host"`
	RoutingMode RoutingMode `yaml:"routing_mode" json:"routing_mode"`
	InboundJSON string      `yaml:"inbound_json" json:"inbound_json"`

	// VLESS/VMess/Trojan/SS/Hysteria2 specific
	Security                   string `yaml:"security" json:"security"`
	ServerUUID                 string `yaml:"server_uuid" json:"server_uuid"`
	VLESSFlow                  string `yaml:"vless_flow" json:"vless_flow"`
	TLSServerName              string `yaml:"tls_server_name" json:"tls_server_name"`
	ClientFingerprint          string `yaml:"client_fingerprint" json:"client_fingerprint"`
	RealityHandshakeServer     string `yaml:"reality_handshake_server" json:"reality_handshake_server"`
	RealityHandshakeServerPort int    `yaml:"reality_handshake_server_port" json:"reality_handshake_server_port"`
	RealityPrivateKey          string `yaml:"reality_private_key" json:"reality_private_key"`
	RealityPublicKey           string `yaml:"reality_public_key" json:"reality_public_key"`
	RealityShortID             string `yaml:"reality_short_id" json:"reality_short_id"`
	RealityMaxTimeDifference   string `yaml:"reality_max_time_difference" json:"reality_max_time_difference"`
	Transport                  string `yaml:"transport" json:"transport"`
	TransportPath              string `yaml:"transport_path" json:"transport_path"`
	TransportXHTTPMode         string `yaml:"transport_xhttp_mode" json:"transport_xhttp_mode"`

	// MTProto specific
	MTProtoSecret               string `yaml:"mtproto_secret" json:"mtproto_secret"`
	MTProtoFakeTLS              string `yaml:"mtproto_faketls" json:"mtproto_faketls"`
	MTProtoPadding              string `yaml:"mtproto_padding" json:"mtproto_padding"`
	MTProtoDomainFrontingPort   int    `yaml:"mtproto_domain_fronting_port" json:"mtproto_domain_fronting_port"`
	MTProtoPreferIP             string `yaml:"mtproto_prefer_ip" json:"mtproto_prefer_ip"`
	MTProtoTolerateTimeSkewness string `yaml:"mtproto_tolerate_time_skewness" json:"mtproto_tolerate_time_skewness"`
	MTProtoIdleTimeout          string `yaml:"mtproto_idle_timeout" json:"mtproto_idle_timeout"`
	MTProtoHandshakeTimeout     string `yaml:"mtproto_handshake_timeout" json:"mtproto_handshake_timeout"`

	// Tailscale specific
	TailscaleControlURL        string   `yaml:"tailscale_control_url" json:"tailscale_control_url"`
	TailscaleHostname          string   `yaml:"tailscale_hostname" json:"tailscale_hostname"`
	TailscaleAuthKey           string   `yaml:"tailscale_auth_key" json:"tailscale_auth_key"`
	TailscaleAcceptRoutes      bool     `yaml:"tailscale_accept_routes" json:"tailscale_accept_routes"`
	TailscaleAdvertiseExitNode bool     `yaml:"tailscale_advertise_exit_node" json:"tailscale_advertise_exit_node"`
	TailscaleAdvertiseRoutes   []string `yaml:"tailscale_advertise_routes" json:"tailscale_advertise_routes"`
}

// Rule represents a routing rule
type Rule struct {
	Section       string   `yaml:"section" json:"section"`
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	Outbound      string   `yaml:"outbound" json:"outbound"`
	Protocol      string   `yaml:"protocol" json:"protocol"`
	Port          string   `yaml:"port" json:"port"`
	PortRange     string   `yaml:"port_range" json:"port_range"`
	Process       string   `yaml:"process" json:"process"`
	ProcessPath   string   `yaml:"process_path" json:"process_path"`
	UID           string   `yaml:"uid" json:"uid"`
	GID           string   `yaml:"gid" json:"gid"`
	Network       string   `yaml:"network" json:"network"`
	Inbound       string   `yaml:"inbound" json:"inbound"`
	Source        []string `yaml:"source" json:"source"`
	Destination   []string `yaml:"destination" json:"destination"`
	Domain        []string `yaml:"domain" json:"domain"`
	DomainSuffix  []string `yaml:"domain_suffix" json:"domain_suffix"`
	DomainKeyword []string `yaml:"domain_keyword" json:"domain_keyword"`
	GeoIP         []string `yaml:"geoip" json:"geoip"`
	GeoSite       []string `yaml:"geosite" json:"geosite"`
	Invert        bool     `yaml:"invert" json:"invert"`
}

// RuleSet represents a rule set
type RuleSet struct {
	Name     string        `yaml:"name" json:"name"`
	URL      string        `yaml:"url" json:"url"`
	Interval time.Duration `yaml:"interval" json:"interval"`
}

// CommunityList represents a community list (like russia_inside)
type CommunityList struct {
	Name     string        `yaml:"name" json:"name"`
	Type     string        `yaml:"type" json:"type"` // "domain", "ip", "geoip", "geosite"
	Entries  []string      `yaml:"entries" json:"entries"`
	URL      string        `yaml:"url" json:"url"`
	Interval time.Duration `yaml:"interval" json:"interval"`
	Invert   bool          `yaml:"invert" json:"invert"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Settings: Settings{
			ConfigVersion:                   "1.0.0",
			AppliedMigrations:               []string{},
			DNSType:                         "udp",
			DNSServers:                      []string{"77.88.8.8", "77.88.8.1"},
			BootstrapDNSServers:             []string{"77.88.8.8", "77.88.8.1"},
			DNSCheckInterval:                10 * time.Second,
			DNSRecoveryCheckInterval:        60 * time.Second,
			DNSCheckTimeout:                 2 * time.Second,
			DNSRewriteTTL:                   60,
			DNSStrategy:                     DNSStrategyPreferIPv4,
			DNSDetourEnabled:                false,
			SourceNetworkInterfaces:         []string{"br-lan"},
			EnableOutputNetworkInterface:    false,
			EnableBadWANInterfaceMonitoring: false,
			EnableYACD:                      false,
			DisableQUIC:                     false,
			ListUpdateEnabled:               true,
			UpdateInterval:                  24 * time.Hour,
			ComponentUpdateCheckEnabled:     true,
			ComponentUpdateCheckInterval:    24 * time.Hour,
			LatencyTestURL:                  "https://www.gstatic.com/generate_204",
			DownloadListsViaProxy:           false,
			DownloadComponentsViaProxy:      false,
			DontTouchDHCP:                   false,
			ConfigPath:                      DefaultConfigDir + "/sing-box/config.json",
			CachePath:                       DefaultCacheDir + "/cache.db",
			LogLevel:                        "warn",
			SingBoxBinary:                   "sing-box",
			ExcludeNTP:                      false,
			ShutdownCorrectly:               false,
		},
		Sections:         []Section{},
		Interfaces:       []SectionInterface{},
		SubscriptionURLs: []SubscriptionURL{},
		URLTests:         []URLTest{},
		Servers:          []Server{},
		Rules:            []Rule{},
		RuleSets:         []RuleSet{},
		CommunityLists:   []CommunityList{},
	}
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Apply defaults for missing values
	if cfg.Settings.ConfigVersion == "" {
		cfg.Settings.ConfigVersion = "1.0.0"
	}
	if cfg.Settings.DNSType == "" {
		cfg.Settings.DNSType = "udp"
	}
	if len(cfg.Settings.DNSServers) == 0 {
		cfg.Settings.DNSServers = []string{"77.88.8.8", "77.88.8.1"}
	}
	if len(cfg.Settings.BootstrapDNSServers) == 0 {
		cfg.Settings.BootstrapDNSServers = []string{"77.88.8.8", "77.88.8.1"}
	}
	if cfg.Settings.DNSCheckInterval == 0 {
		cfg.Settings.DNSCheckInterval = 10 * time.Second
	}
	if cfg.Settings.DNSRecoveryCheckInterval == 0 {
		cfg.Settings.DNSRecoveryCheckInterval = 60 * time.Second
	}
	if cfg.Settings.DNSCheckTimeout == 0 {
		cfg.Settings.DNSCheckTimeout = 2 * time.Second
	}
	if cfg.Settings.DNSRewriteTTL == 0 {
		cfg.Settings.DNSRewriteTTL = 60
	}
	if cfg.Settings.DNSStrategy == "" {
		cfg.Settings.DNSStrategy = DNSStrategyPreferIPv4
	}
	if cfg.Settings.UpdateInterval == 0 {
		cfg.Settings.UpdateInterval = 24 * time.Hour
	}
	if cfg.Settings.ComponentUpdateCheckInterval == 0 {
		cfg.Settings.ComponentUpdateCheckInterval = 24 * time.Hour
	}
	if cfg.Settings.LatencyTestURL == "" {
		cfg.Settings.LatencyTestURL = "https://www.gstatic.com/generate_204"
	}
	if cfg.Settings.ConfigPath == "" {
		cfg.Settings.ConfigPath = DefaultConfigDir + "/sing-box/config.json"
	}
	if cfg.Settings.CachePath == "" {
		cfg.Settings.CachePath = DefaultCacheDir + "/cache.db"
	}
	if cfg.Settings.LogLevel == "" {
		cfg.Settings.LogLevel = "warn"
	}
	if cfg.Settings.SingBoxBinary == "" {
		cfg.Settings.SingBoxBinary = "sing-box"
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks values that would otherwise cause runtime failures.
func (c *Config) Validate() error {
	s := c.Settings
	if s.DNSCheckInterval <= 0 || s.DNSRecoveryCheckInterval <= 0 || s.DNSCheckTimeout <= 0 {
		return fmt.Errorf("dns check intervals and timeout must be positive")
	}
	if s.UpdateInterval < 0 || s.ComponentUpdateCheckInterval < 0 {
		return fmt.Errorf("update intervals must not be negative")
	}
	names := make(map[string]bool, len(c.Sections))
	for _, sec := range c.Sections {
		if sec.Name == "" {
			return fmt.Errorf("section without a name")
		}
		if names[sec.Name] {
			return fmt.Errorf("duplicate section %q", sec.Name)
		}
		names[sec.Name] = true
		switch sec.Provider {
		case "", ProviderTypeSingBox, ProviderTypeZapret, ProviderTypeZapret2, ProviderTypeByeDPI, ProviderTypeAuto:
		default:
			return fmt.Errorf("section %q: unknown provider %q", sec.Name, sec.Provider)
		}
		switch sec.Action {
		case "", ActionTypeConnection, ActionTypeBypass, ActionTypeBlock:
		default:
			return fmt.Errorf("section %q: unknown action %q", sec.Name, sec.Action)
		}
	}
	for _, sub := range c.SubscriptionURLs {
		if sub.URL == "" {
			continue
		}
		if !names[sub.Section] {
			return fmt.Errorf("subscription %q refers to unknown section %q", sub.URL, sub.Section)
		}
		if u, err := url.Parse(sub.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("subscription url %q must be a valid http(s) URL", sub.URL)
		}
	}
	return nil
}

// ProviderEnabled reports whether any enabled section uses the given provider.
// Sections without an explicit provider use sing-box.
func (c *Config) ProviderEnabled(p ProviderType) bool {
	for _, sec := range c.Sections {
		if !sec.Enabled {
			continue
		}
		sp := sec.Provider
		if sp == "" || sp == ProviderTypeAuto {
			sp = ProviderTypeSingBox
		}
		if sp == p {
			return true
		}
	}
	return false
}

// ProviderOptions returns the custom options of the first enabled section
// using the given provider, or an empty string.
func (c *Config) ProviderOptions(p ProviderType) string {
	for _, sec := range c.Sections {
		if sec.Enabled && sec.Provider == p && sec.ProviderOptions != "" {
			return sec.ProviderOptions
		}
	}
	return ""
}

// SaveToFile saves configuration to a YAML file
func (c *Config) SaveToFile(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
