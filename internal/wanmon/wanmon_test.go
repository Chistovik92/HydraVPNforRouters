package wanmon

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestReason(t *testing.T) {
	up := State{Up: true, Addrs: "1.1.1.1"}
	cases := []struct {
		prev, cur State
		want      string
	}{
		{State{}, up, "interface came up"},
		{up, State{Up: true, Addrs: "2.2.2.2"}, "address changed"},
		{up, up, ""},
		{up, State{}, ""},
		{State{}, State{}, ""},
	}
	for i, c := range cases {
		if got := Reason(c.prev, c.cur); got != c.want {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}

func TestMonitorCallsBackAfterOutage(t *testing.T) {
	var mu sync.Mutex
	state := State{Up: true, Addrs: "1.1.1.1"}
	probe := func(string) State { mu.Lock(); defer mu.Unlock(); return state }

	got := make(chan string, 4)
	m := &Monitor{
		Interfaces: []string{"wan"}, Interval: 10 * time.Millisecond, Probe: probe,
		OnChange: func(iface, reason string) { got <- iface + ":" + reason },
	}
	m.Start(context.Background())
	defer m.Stop()

	set := func(s State) { mu.Lock(); state = s; mu.Unlock() }
	set(State{})
	time.Sleep(50 * time.Millisecond)
	select {
	case v := <-got:
		t.Fatalf("going down must not trigger a reload: %s", v)
	default:
	}
	set(State{Up: true, Addrs: "3.3.3.3"})
	select {
	case v := <-got:
		if v != "wan:interface came up" {
			t.Errorf("got %s", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no callback after the interface came back")
	}
}
