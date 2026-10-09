package domainmap

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/lists"
)

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, v := range s {
		out[i] = netip.MustParseAddr(v)
	}
	return out
}

func strs(p []netip.Prefix) string {
	s := make([]string, len(p))
	for i, v := range p {
		s[i] = v.String()
	}
	return strings.Join(s, " ")
}

func TestAggregate(t *testing.T) {
	in := addrs("8.8.8.8", "8.8.8.9", "8.8.4.4", "93.184.216.34", "2001:4860::1")
	for mode, want := range map[Aggregation]string{
		AggNone:   "8.8.4.4/32 8.8.8.8/32 8.8.8.9/32 93.184.216.34/32 2001:4860::1/128",
		Agg24:     "8.8.4.0/24 8.8.8.0/24 93.184.216.0/24 2001:4860::1/128",
		Agg16:     "8.8.0.0/16 93.184.0.0/16 2001:4860::1/128",
		Agg24Plus: "8.8.4.4/32 8.8.8.0/24 93.184.216.34/32 2001:4860::1/128",
	} {
		if got := strs(Aggregate(in, mode)); got != want {
			t.Errorf("%s: got %s want %s", mode, got, want)
		}
	}
	// Extra subnets absorb the addresses they cover.
	got := strs(Aggregate(addrs("10.1.1.1", "8.8.8.8"), AggNone, netip.MustParsePrefix("8.8.0.0/16")))
	if got != "8.8.0.0/16 10.1.1.1/32" {
		t.Errorf("extra: %s", got)
	}
}

func TestParseAggregation(t *testing.T) {
	for in, want := range map[string]Aggregation{"": AggNone, "/24": Agg24, "16": Agg16, "24+32": Agg24Plus, "24+/32": Agg24Plus} {
		if got, err := ParseAggregation(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := ParseAggregation("20"); err == nil {
		t.Error("20 must be rejected")
	}
}

func TestResolveMergesServersAndDropsPlaceholders(t *testing.T) {
	lookup := func(_ context.Context, server, host string, _ bool) ([]netip.Addr, error) {
		switch {
		case host == "dead.example":
			return nil, errors.New("nxdomain")
		case host == "blocked.example":
			return addrs("0.0.0.0", "127.0.0.1", "1.1.1.1"), nil // 1.1.1.1 is the DNS server itself
		case server == "1.1.1.1":
			return addrs("93.184.216.34"), nil
		default:
			return addrs("93.184.216.35", "104.16.0.1"), nil
		}
	}
	res := Resolve(context.Background(), []string{"a.example", "A.example", "dead.example", "blocked.example"},
		Options{Servers: []string{"1.1.1.1", "8.8.8.8:53"}, Lookup: lookup})
	if got := strings.Join(addrStrs(res.Addrs), " "); got != "93.184.216.34 93.184.216.35 104.16.0.1" {
		t.Errorf("addrs: %s", got)
	}
	if strings.Join(res.Failed, ",") != "blocked.example,dead.example" {
		t.Errorf("failed: %v", res.Failed)
	}
	if got := DropCloudflare(res.Addrs); len(got) != 2 {
		t.Errorf("cloudflare not dropped: %v", got)
	}
}

func addrStrs(a []netip.Addr) []string {
	s := make([]string, len(a))
	for i, v := range a {
		s[i] = v.String()
	}
	return s
}

func TestRunWithSource(t *testing.T) {
	var src Source
	src.Add(listsParse("example.com\nfull:www.example.com\nkeyword:x\nregexp:^a$\n104.16.0.0/13\n198.51.100.0/24\n"))
	if len(src.Domains) != 2 || src.Skipped != 2 || len(src.Prefixes) != 2 {
		t.Fatalf("source: %+v", src)
	}
	run := Run{
		Resolve: Options{Servers: []string{"9.9.9.9"}, Lookup: func(context.Context, string, string, bool) ([]netip.Addr, error) {
			return addrs("93.184.216.34", "104.16.5.5"), nil
		}},
		Aggregation:    Agg24,
		DropCloudflare: true,
	}
	got, failed := run.Do(context.Background(), src)
	if strs(got) != "93.184.216.0/24 198.51.100.0/24" || len(failed) != 0 {
		t.Errorf("run: %s failed=%v", strs(got), failed)
	}
}

func TestFormats(t *testing.T) {
	p := []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24"), netip.MustParsePrefix("1.2.3.4/32"), netip.MustParsePrefix("2001:db8::/32")}
	cases := map[string]string{
		"plain":        "1.2.3.4/32\n8.8.8.0/24\n2001:db8::/32\n",
		"keenetic-cli": "ip route 1.2.3.4 255.255.255.255 Wireguard0 auto\nip route 8.8.8.0 255.255.255.0 Wireguard0 auto\nipv6 route 2001:db8::/32 Wireguard0 auto\n",
		"keenetic-bat": "@echo off\nroute add 1.2.3.4 mask 255.255.255.255 Wireguard0\nroute add 8.8.8.0 mask 255.255.255.0 Wireguard0\n",
		"mikrotik":     "/ip firewall address-list add list=L address=1.2.3.4/32\n/ip firewall address-list add list=L address=8.8.8.0/24\n/ipv6 firewall address-list add list=L address=2001:db8::/32\n",
		"openvpn":      "route 1.2.3.4 255.255.255.255\nroute 8.8.8.0 255.255.255.0\nroute-ipv6 2001:db8::/32\n",
		"wireguard":    "AllowedIPs = 1.2.3.4/32, 8.8.8.0/24, 2001:db8::/32\n",
		"ipset":        "create L hash:net -exist\nadd L 1.2.3.4/32\nadd L 8.8.8.0/24\n",
	}
	for f, want := range cases {
		out, err := Format(f, p, FormatOptions{Interface: "Wireguard0", ListName: "L"})
		if err != nil || len(out) != 1 || out[0] != want {
			t.Errorf("%s: %q err=%v want %q", f, out, err, want)
		}
	}
	nft, _ := Format("nft", p, FormatOptions{ListName: "L"})
	if !strings.Contains(nft[0], "elements = { 1.2.3.4/32, 8.8.8.0/24 }") {
		t.Errorf("nft: %s", nft[0])
	}
	if _, err := Format("nope", p, FormatOptions{}); err == nil {
		t.Error("unknown format accepted")
	}
	parts, _ := Format("plain", p, FormatOptions{MaxPerPart: 2})
	if len(parts) != 2 || parts[1] != "2001:db8::/32\n" {
		t.Errorf("parts: %q", parts)
	}
}

func listsParse(s string) lists.Entries { return lists.ParseEntries(s) }
