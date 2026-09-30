//go:build windows
// +build windows

package firewall

import (
	"net"
)

// getLinks returns network links using net package (Windows fallback)
func getLinks() ([]Link, error) {
	// On Windows, we can't use netlink, so return empty
	// The code that calls this should handle the empty result
	_, _ = net.Interfaces()

	// Create a minimal implementation that satisfies the interface
	// We'll just return an empty slice since we can't use netlink on Windows
	return []Link{}, nil
}

// getAddrList returns addresses for a link (Windows fallback)
func getAddrList(link Link) ([]Addr, error) {
	return []Addr{}, nil
}

// Stub netlink types for Windows compilation
type Link interface {
	Attrs() *LinkAttrs
}

type LinkAttrs struct {
	Name  string
	Index int
	MTU   int
}

type Addr struct {
	IPNet *net.IPNet
}

func AddrList(link Link, family int) ([]Addr, error) {
	return nil, nil
}
