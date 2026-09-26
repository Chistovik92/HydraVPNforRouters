//go:build !windows
// +build !windows

package firewall

import (
	"github.com/vishvananda/netlink"
)

// getLinks returns network links using netlink (Unix only)
func getLinks() ([]netlink.Link, error) {
	return netlink.LinkList()
}

// getAddrList returns addresses for a link (Unix only)
func getAddrList(link netlink.Link) ([]netlink.Addr, error) {
	return netlink.AddrList(link, netlink.FAMILY_ALL)
}