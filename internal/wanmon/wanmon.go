// Package wanmon watches WAN interfaces and calls back when one comes back
// after an outage or changes its address, so tproxy rules and proxy
// connections can be rebuilt (a dead WAN leaves stale sockets behind).
package wanmon

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is a snapshot of an interface.
type State struct {
	Up    bool
	Addrs string // sorted, comma separated
}

// Probe reads the state of a network interface.
type Probe func(name string) State

// SystemProbe reads the state from the OS: up and with an address.
func SystemProbe(name string) State {
	ifc, err := net.InterfaceByName(name)
	if err != nil || ifc.Flags&net.FlagUp == 0 {
		return State{}
	}
	addrs, _ := ifc.Addrs()
	var list []string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLinkLocalUnicast() {
			list = append(list, ipnet.IP.String())
		}
	}
	sort.Strings(list)
	return State{Up: len(list) > 0, Addrs: strings.Join(list, ",")}
}

// Monitor polls interfaces.
type Monitor struct {
	Interfaces []string
	Interval   time.Duration
	Delay      time.Duration // wait after a change before calling OnChange
	Probe      Probe
	OnChange   func(iface, reason string)
	OnLog      func(level, message string)

	mu     sync.Mutex
	last   map[string]State
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Start begins polling; it returns immediately.
func (m *Monitor) Start(ctx context.Context) {
	if len(m.Interfaces) == 0 {
		return
	}
	if m.Probe == nil {
		m.Probe = SystemProbe
	}
	if m.Interval <= 0 {
		m.Interval = 5 * time.Second
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Lock()
	m.last = map[string]State{}
	for _, n := range m.Interfaces {
		m.last[n] = m.Probe(n)
	}
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(m.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.poll(ctx)
			}
		}
	}()
}

// Stop stops polling.
func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

// Reason describes a transition that requires a reload, or "" if none.
// Going down is not acted on (nothing to rebuild yet); coming up or getting
// a new address is.
func Reason(prev, cur State) string {
	switch {
	case cur.Up && !prev.Up:
		return "interface came up"
	case cur.Up && prev.Up && cur.Addrs != prev.Addrs:
		return "address changed"
	}
	return ""
}

func (m *Monitor) poll(ctx context.Context) {
	for _, n := range m.Interfaces {
		cur := m.Probe(n)
		m.mu.Lock()
		prev := m.last[n]
		m.last[n] = cur
		m.mu.Unlock()

		if !cur.Up && prev.Up {
			m.log("warn", "%s went down", n)
		}
		reason := Reason(prev, cur)
		if reason == "" {
			continue
		}
		m.log("info", "%s: %s", n, reason)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.Delay):
		}
		if m.OnChange != nil {
			m.OnChange(n, reason)
		}
	}
}

func (m *Monitor) log(level, format string, args ...interface{}) {
	if m.OnLog != nil {
		m.OnLog(level, "[wan] "+sprintf(format, args...))
	}
}
