package firewall

import "github.com/Chistovik92/hydravpn-router/internal/config"

// FirewallBackend представляет тип бэкенда файрвола
// FirewallBackend represents the firewall backend type
type FirewallBackend string

const (
	FirewallBackendNFTables FirewallBackend = "nftables"
	FirewallBackendIPTables FirewallBackend = "iptables"
	FirewallBackendWindows  FirewallBackend = "windows"
)

// Names and marks used by the rules. RouteTable is numeric so no
// /etc/iproute2/rt_tables entry is required.
const (
	TableName  = "hydravpn"
	ChainName  = "proxy_pre"
	MarkValue  = "0x08000000"
	RouteTable = 105
)

// NFQueueOptions describes packets queued to nfqws (zapret).
type NFQueueOptions struct {
	QueueNum   int
	DesyncMark string
	TCPPorts   []int
	UDPPorts   []int
}

// Options для создания нового менеджера
// Options for creating a new manager
type Options struct {
	Config *config.Config
	OnLog  func(level, message string)
}
