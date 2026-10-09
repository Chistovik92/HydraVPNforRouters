// Package domainmap resolves domain names to IP addresses and turns them
// into route lists: aggregated into /24 or /16 subnets, without Cloudflare
// ranges, in the formats that routers and VPN clients understand (Keenetic,
// MikroTik, WireGuard, OpenVPN, nftables and others).
//
// The idea comes from DomainMapper (github.com/Ground-Zerro/DomainMapper):
// every selected DNS server is asked, the answers are merged, duplicates and
// placeholder addresses are dropped. This is an independent Go implementation.
package domainmap

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// Aggregation selects how resolved addresses are grouped.
type Aggregation string

const (
	AggNone   Aggregation = "host"  // every address as /32 (/128)
	Agg24     Aggregation = "24"    // every IPv4 address becomes its /24
	Agg16     Aggregation = "16"    // every IPv4 address becomes its /16
	Agg24Plus Aggregation = "24+32" // /24 when two addresses share it, else /32
)

// ParseAggregation accepts "host", "24", "16", "24+32" (also with a "/").
func ParseAggregation(s string) (Aggregation, error) {
	switch a := Aggregation(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "/")); a {
	case "", "32", "none":
		return AggNone, nil
	case AggNone, Agg24, Agg16, Agg24Plus:
		return a, nil
	case "24+/32":
		return Agg24Plus, nil
	default:
		return "", fmt.Errorf("unknown aggregation %q (host, 24, 16, 24+32)", s)
	}
}

// LookupFunc resolves host through one DNS server ("ip:port").
type LookupFunc func(ctx context.Context, server, host string, ipv6 bool) ([]netip.Addr, error)

// Options for Resolve.
type Options struct {
	// Servers are DNS servers ("1.1.1.1" or "1.1.1.1:53"). Empty uses the
	// system resolver.
	Servers     []string
	Timeout     time.Duration // per query, default 5s
	Concurrency int           // parallel queries, default 16
	IPv6        bool          // also ask for AAAA
	Lookup      LookupFunc    // for tests; default queries over UDP/TCP
}

// Result of Resolve.
type Result struct {
	Addrs  []netip.Addr
	Failed []string // domains that returned nothing
}

// Resolve asks every server for every domain and returns the merged,
// de-duplicated addresses without placeholders (0.0.0.0, loopback, link-local,
// private and the DNS servers themselves).
func Resolve(ctx context.Context, domains []string, o Options) Result {
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 16
	}
	lookup := o.Lookup
	if lookup == nil {
		lookup = netLookup
	}
	servers := o.Servers
	if len(servers) == 0 {
		servers = []string{""}
	}
	skip := map[netip.Addr]bool{}
	for _, s := range servers {
		if a, err := netip.ParseAddr(strings.Trim(strings.SplitN(s, ":", 2)[0], "[]")); err == nil {
			skip[a] = true
		}
	}

	var (
		mu     sync.Mutex
		seen   = map[netip.Addr]bool{}
		failed []string
		wg     sync.WaitGroup
		sem    = make(chan struct{}, o.Concurrency)
	)
	for _, d := range uniq(domains) {
		wg.Add(1)
		sem <- struct{}{}
		go func(d string) {
			defer wg.Done()
			defer func() { <-sem }()
			got := false
			for _, s := range servers {
				qctx, cancel := context.WithTimeout(ctx, o.Timeout)
				addrs, err := lookup(qctx, s, d, o.IPv6)
				cancel()
				if err != nil {
					continue
				}
				mu.Lock()
				for _, a := range addrs {
					a = a.Unmap()
					if usable(a) && !skip[a] {
						seen[a] = true
						got = true
					}
				}
				mu.Unlock()
			}
			if !got {
				mu.Lock()
				failed = append(failed, d)
				mu.Unlock()
			}
		}(d)
	}
	wg.Wait()

	res := Result{Failed: failed}
	for a := range seen {
		res.Addrs = append(res.Addrs, a)
	}
	sort.Slice(res.Addrs, func(i, j int) bool { return res.Addrs[i].Less(res.Addrs[j]) })
	sort.Strings(res.Failed)
	return res
}

func usable(a netip.Addr) bool {
	return a.IsValid() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsLinkLocalUnicast() &&
		!a.IsMulticast() && !a.IsPrivate()
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func netLookup(ctx context.Context, server, host string, ipv6 bool) ([]netip.Addr, error) {
	r := net.DefaultResolver
	if server != "" {
		addr := server
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(strings.Trim(addr, "[]"), "53")
		}
		r = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}}
	}
	network := "ip4"
	if ipv6 {
		network = "ip"
	}
	return r.LookupNetIP(ctx, network, host)
}

// Aggregate groups addresses by mode and returns sorted, non-overlapping
// prefixes. Extra prefixes (for example subnets from a list) are merged in.
func Aggregate(addrs []netip.Addr, mode Aggregation, extra ...netip.Prefix) []netip.Prefix {
	set := map[netip.Prefix]bool{}
	count24 := map[netip.Prefix]int{}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.Is4() || mode == AggNone || mode == "" {
			set[netip.PrefixFrom(a, a.BitLen())] = true
			continue
		}
		switch mode {
		case Agg24:
			set[netip.PrefixFrom(a, 24).Masked()] = true
		case Agg16:
			set[netip.PrefixFrom(a, 16).Masked()] = true
		case Agg24Plus:
			count24[netip.PrefixFrom(a, 24).Masked()]++
		}
	}
	if mode == Agg24Plus {
		for _, a := range addrs {
			a = a.Unmap()
			if !a.Is4() {
				continue
			}
			if p := netip.PrefixFrom(a, 24).Masked(); count24[p] > 1 {
				set[p] = true
			} else {
				set[netip.PrefixFrom(a, 32)] = true
			}
		}
	}
	for _, p := range extra {
		set[p.Masked()] = true
	}
	return dropCovered(set)
}

// dropCovered removes prefixes that lie inside another prefix of the set.
func dropCovered(set map[netip.Prefix]bool) []netip.Prefix {
	all := make([]netip.Prefix, 0, len(set))
	for p := range set {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Bits() != all[j].Bits() {
			return all[i].Bits() < all[j].Bits() // wider first
		}
		return all[i].Addr().Less(all[j].Addr())
	})
	var out []netip.Prefix
	for _, p := range all {
		covered := false
		for _, q := range out {
			if q.Addr().Is4() == p.Addr().Is4() && q.Bits() <= p.Bits() && q.Contains(p.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Addr() != out[j].Addr() {
			return out[i].Addr().Less(out[j].Addr())
		}
		return out[i].Bits() < out[j].Bits()
	})
	return out
}

// cloudflare lists the published IPv4 ranges of Cloudflare
// (https://www.cloudflare.com/ips-v4).
var cloudflare = mustPrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
)

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, v := range s {
		out[i] = netip.MustParsePrefix(v)
	}
	return out
}

// IsCloudflare reports whether a lies in a Cloudflare range.
func IsCloudflare(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range cloudflare {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// DropCloudflare removes Cloudflare addresses (do this before Aggregate: a
// /16 built around one Cloudflare address would swallow unrelated hosts).
func DropCloudflare(addrs []netip.Addr) []netip.Addr {
	out := addrs[:0:0]
	for _, a := range addrs {
		if !IsCloudflare(a) {
			out = append(out, a)
		}
	}
	return out
}
