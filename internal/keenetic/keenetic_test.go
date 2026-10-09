package keenetic

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

type fake struct {
	cmds []string
	fail string
}

func (f *fake) Run(_ context.Context, c string) (string, error) {
	f.cmds = append(f.cmds, c)
	if f.fail != "" && strings.Contains(c, f.fail) {
		return "", errors.New("boom")
	}
	return "", nil
}

func TestParseInterfaces(t *testing.T) {
	out := `interface, name = Wireguard0:
            id: Wireguard0
          type: Wireguard
   description: "Home VPN"
     connected: yes
interface, name = ISP:
            id: ISP
          type: GigabitEthernet
     connected: no
`
	got := ParseInterfaces(out)
	if len(got) != 2 || got[0].ID != "Wireguard0" || !got[0].Connected || got[0].Description != "Home VPN" || got[1].Connected {
		t.Errorf("interfaces: %+v", got)
	}
}

func TestRouteCommandsAndApply(t *testing.T) {
	cmds, err := RouteCommands("Wireguard0", []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24"), netip.MustParsePrefix("2001:db8::/32")})
	if err != nil || cmds[0] != "ip route 8.8.8.0 255.255.255.0 Wireguard0 auto" || cmds[1] != "ipv6 route 2001:db8::/32 Wireguard0 auto" {
		t.Fatalf("%v %v", cmds, err)
	}
	if _, err := RouteCommands("x; reboot", nil); err == nil {
		t.Error("injection accepted")
	}
	f := &fake{}
	if err := Apply(context.Background(), f, cmds); err != nil || f.cmds[len(f.cmds)-1] != "system configuration save" {
		t.Errorf("apply: %v %v", f.cmds, err)
	}
	f = &fake{fail: "ipv6"}
	if err := Apply(context.Background(), f, cmds); err == nil || strings.Contains(strings.Join(f.cmds, "|"), "save") {
		t.Errorf("must not save after a failure: %v %v", f.cmds, err)
	}
}

func TestProxyCommands(t *testing.T) {
	c, err := ProxyCommands("Proxy0", "127.0.0.1", 4534)
	if err != nil || c[2] != "interface Proxy0 proxy upstream 127.0.0.1 4534" {
		t.Errorf("%v %v", c, err)
	}
	if _, err := ProxyCommands("Proxy0", "not-an-ip", 1); err == nil {
		t.Error("bad host accepted")
	}
}
