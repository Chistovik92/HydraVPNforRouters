package firewall

import "github.com/Chistovik92/hydravpn-router/internal/config"

// FirewallBackend представляет тип бэкенда файрвола
// FirewallBackend represents the firewall backend type
type FirewallBackend string

const (
	FirewallBackendNFTables FirewallBackend = "nftables"
	FirewallBackendIPTables FirewallBackend = "iptables"
	FirewallBackendRouterOS FirewallBackend = "routeros"
	FirewallBackendAuto     FirewallBackend = "auto"
	FirewallBackendWindows  FirewallBackend = "windows"
)

// Options для создания нового менеджера
// Options for creating a new manager
type Options struct {
	Config *config.Config
	OnLog  func(level, message string)
}