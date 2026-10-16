package cores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/process"
)

const (
	stopTimeout  = 5 * time.Second
	respawnDelay = 5 * time.Second
	ifaceWait    = 10 * time.Second
)

// Options configure a Manager.
type Options struct {
	// Dir holds generated configs and key files (mode 0600); a tmpfs
	// directory on routers.
	Dir      string
	LogLevel string // Xray log level
	DNS      string // resolver for olcRTC, host:port
	OnLog    func(level, message string)
}

// spec is one process the plan needs.
type spec struct {
	key       string
	kind      Kind
	names     []string
	bin       string
	args      []string
	files     map[string]string // path -> content
	inst      *Instance         // AmneziaWG: interface to configure
	wg        *WireGuardConf    // AmneziaWG: its config
	identity  string            // extra input of the signature
	signature string
}

type running struct {
	spec spec
	sup  *process.Supervisor
	// stopped is closed when the process is stopped: an interface setup
	// still running for it must not touch the next instance.
	stopped chan struct{}
}

// Manager starts, restarts and stops the core processes of a Plan.
type Manager struct {
	mu    sync.Mutex
	opts  Options
	procs map[string]*running
	last  *Plan
	// run executes helper commands (awg, ip); replaced in tests.
	run func(name string, args ...string) error
	// uapi writes a request to an amneziawg-go control socket.
	uapi func(socket, req string) error
}

// NewManager creates a Manager.
func NewManager(opts Options) *Manager {
	return &Manager{opts: opts, procs: map[string]*running{}, run: runCommand, uapi: writeUAPI}
}

func runCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", filepath.Base(name), strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Apply brings the processes in line with p: unchanged ones keep running,
// changed ones restart, the rest stop.
func (m *Manager) Apply(p *Plan) error {
	specs, err := m.specs(p)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = p
	want := map[string]spec{}
	for _, s := range specs {
		want[s.key] = s
	}
	for key, r := range m.procs {
		if s, ok := want[key]; !ok || s.signature != r.spec.signature {
			m.stopLocked(key, r)
		}
	}
	var errs []string
	for _, s := range specs {
		if _, ok := m.procs[s.key]; ok {
			continue
		}
		if err := m.startLocked(s); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cores: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Stop stops every core process.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, r := range m.procs {
		m.stopLocked(key, r)
	}
}

func (m *Manager) startLocked(s spec) error {
	if err := os.MkdirAll(m.opts.Dir, 0700); err != nil {
		return err
	}
	for path, content := range s.files {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return fmt.Errorf("%s: %w", s.key, err)
		}
	}
	sup := &process.Supervisor{Name: s.key, RespawnDelay: respawnDelay, OnLog: m.opts.OnLog,
		PidFile: filepath.Join(m.opts.Dir, s.key+".pid")}
	stopped := make(chan struct{})
	if s.inst != nil {
		inst, conf := *s.inst, *s.wg
		sup.OnStart = func(int) { m.setupInterface(inst, conf, stopped) }
	}
	if err := sup.Start(s.bin, s.args); err != nil {
		return err
	}
	m.procs[s.key] = &running{spec: s, sup: sup, stopped: stopped}
	return nil
}

func (m *Manager) stopLocked(key string, r *running) {
	close(r.stopped) // first: a pending interface setup gives up
	r.sup.Stop(stopTimeout)
	if r.spec.inst != nil {
		m.teardownInterface(*r.spec.inst)
	}
	for path := range r.spec.files {
		os.Remove(path) // keys must not outlive the process
	}
	delete(m.procs, key)
}

func (m *Manager) path(name string) string { return filepath.Join(m.opts.Dir, name) }

// specs turns a plan into process specs.
func (m *Manager) specs(p *Plan) ([]spec, error) {
	var out []spec
	if xs := p.Of(Xray); len(xs) > 0 {
		data, skipped := XrayConfig(xs, m.opts.LogLevel)
		for name, err := range skipped {
			m.log("warn", "xray node %q skipped: %v", name, err)
		}
		cfg := m.path("xray.json")
		out = append(out, spec{key: "xray", kind: Xray, names: names(xs), bin: Resolve(p.Bins[Xray]),
			args: []string{"run", "-c", cfg}, files: map[string]string{cfg: string(data)}})
	}
	for _, in := range p.Of(OpenFlux) {
		c, err := ParseOpenFlux(in.Link)
		if err != nil {
			m.log("warn", "openflux node %q skipped: %v", in.Name, err)
			continue
		}
		key := fmt.Sprintf("openflux-%d", in.Port)
		files := map[string]string{}
		keyFile := ""
		if c.Secret != "" {
			keyFile = m.path(key + ".key")
			files[keyFile] = c.Secret
		}
		out = append(out, spec{key: key, kind: OpenFlux, names: []string{in.Name}, bin: Resolve(p.Bins[OpenFlux]),
			args: OpenFluxArgs(c, in.Port, keyFile), files: files})
	}
	for _, in := range p.Of(OlcRTC) {
		n, err := ParseOlcRTC(in.Link)
		if err != nil {
			m.log("warn", "olcrtc node %q skipped: %v", in.Name, err)
			continue
		}
		key := fmt.Sprintf("olcrtc-%d", in.Port)
		cfg := m.path(key + ".yaml")
		out = append(out, spec{key: key, kind: OlcRTC, names: []string{in.Name}, bin: Resolve(p.Bins[OlcRTC]),
			args: []string{cfg}, files: map[string]string{cfg: OlcRTCConfig(n, in.Port, m.opts.DNS)}})
	}
	for _, in := range p.Of(AmneziaWG) {
		c, err := ParseWireGuard(in.Link)
		if err != nil {
			m.log("warn", "amneziawg node %q skipped: %v", in.Name, err)
			continue
		}
		inst, conf := in, c
		// The config goes over the control socket, never to disk; the link
		// is the identity, so a changed config restarts the interface.
		out = append(out, spec{key: in.Iface, kind: AmneziaWG, names: []string{in.Name}, bin: Resolve(p.Bins[AmneziaWG]),
			args: []string{"-f", in.Iface}, inst: &inst, wg: &conf, identity: in.Link})
	}
	for i := range out {
		out[i].signature = signature(out[i])
	}
	return out, nil
}

func names(in []Instance) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.Name
	}
	return out
}

func signature(s spec) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", s.bin, strings.Join(s.args, "\x00"), s.identity)
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%s\x00", p, s.files[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// setupInterface configures awgN after amneziawg-go created it: keys, peer
// and obfuscation over the UAPI socket, then addresses, MTU and a routing
// table used only by sockets bound to the interface.
func (m *Manager) setupInterface(in Instance, conf WireGuardConf, stopped <-chan struct{}) {
	isStopped := func() bool {
		select {
		case <-stopped:
			return true
		default:
			return false
		}
	}
	deadline := time.Now().Add(ifaceWait)
	for !ifaceExists(in.Iface) || !fileExists(uapiSocket(in.Iface)) {
		if isStopped() {
			return
		}
		if time.Now().After(deadline) {
			m.log("error", "%s: interface did not appear", in.Iface)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The config comes from a subscription: an address inside a network of
	// the router (its LAN) would pull that traffic into the tunnel.
	if a, n := overlapsLocal(conf.Addresses()); a != "" {
		m.log("error", "%s (%s): address %s overlaps the router network %s; node not started", in.Iface, in.Name, a, n)
		return
	}
	if isStopped() {
		return
	}
	req, err := conf.UAPI(resolveHost)
	if err == nil {
		err = m.uapi(uapiSocket(in.Iface), req)
	}
	if err != nil {
		m.log("error", "%s: %v", in.Iface, err)
		return
	}
	ip := FindTool("ip")
	if ip == "" {
		m.log("error", "%s: the ip tool is needed (ip-full)", in.Iface)
		return
	}
	table := strconv.Itoa(in.Table)
	var steps [][]string
	v6 := false
	for _, a := range conf.Addresses() {
		fam := "-4"
		if strings.Contains(a, ":") {
			fam, v6 = "-6", true
		}
		steps = append(steps, []string{ip, fam, "address", "replace", a, "dev", in.Iface})
	}
	if mtu := conf.MTU(); mtu != "" {
		steps = append(steps, []string{ip, "link", "set", "dev", in.Iface, "mtu", mtu})
	}
	steps = append(steps,
		[]string{ip, "link", "set", "dev", in.Iface, "up"},
		[]string{ip, "-4", "route", "replace", "default", "dev", in.Iface, "table", table},
	)
	if v6 {
		steps = append(steps, []string{ip, "-6", "route", "replace", "default", "dev", in.Iface, "table", table})
	}
	m.ruleDel(ip, in) // a respawn must not stack rules
	steps = append(steps, []string{ip, "-4", "rule", "add", "oif", in.Iface, "lookup", table})
	if v6 {
		steps = append(steps, []string{ip, "-6", "rule", "add", "oif", in.Iface, "lookup", table})
	}
	for _, st := range steps {
		if isStopped() {
			return // the next instance configures the interface itself
		}
		if err := m.run(st[0], st[1:]...); err != nil {
			m.log("error", "%s: %v", in.Iface, err)
			return
		}
	}
	m.log("info", "%s is up (%s)", in.Iface, in.Name)
}

// localNets lists the networks of the router's interfaces (replaced in tests).
var localNets = func() []*net.IPNet {
	var out []*net.IPNet
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if strings.HasPrefix(ifc.Name, "awg") {
			continue // our own tunnels
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
				out = append(out, n)
			}
		}
	}
	return out
}

// overlapsLocal returns the first tunnel address that overlaps a network of
// the router, and that network.
func overlapsLocal(addrs []string) (string, string) {
	nets := localNets()
	for _, a := range addrs {
		ip, n, err := net.ParseCIDR(a)
		if err != nil {
			if ip = net.ParseIP(a); ip == nil {
				continue
			}
			n = &net.IPNet{IP: ip, Mask: net.CIDRMask(len(ip)*8, len(ip)*8)}
		}
		for _, l := range nets {
			if l.Contains(n.IP) || n.Contains(l.IP) {
				return a, l.String()
			}
		}
	}
	return "", ""
}

// uapiSocket is where amneziawg-go listens (ipc/uapi_unix.go upstream).
func uapiSocket(iface string) string { return "/var/run/amneziawg/" + iface + ".sock" }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// resolveHost prefers an IPv4 address for the endpoint.
func resolveHost(host string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return "", fmt.Errorf("no address for %s: %v", host, err)
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			return a, nil
		}
	}
	return addrs[0], nil
}

// writeUAPI sends a set request and checks the "errno=0" answer.
func writeUAPI(socket, req string) error {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("uapi: %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte(req)); err != nil {
		return fmt.Errorf("uapi: %w", err)
	}
	buf := make([]byte, 256)
	n, _ := c.Read(buf)
	if ans := strings.TrimSpace(string(buf[:n])); ans != "errno=0" {
		return fmt.Errorf("uapi: amneziawg-go answered %q", ans)
	}
	return nil
}

func (m *Manager) teardownInterface(in Instance) {
	if ip := FindTool("ip"); ip != "" {
		m.ruleDel(ip, in)
	}
}

func (m *Manager) ruleDel(ip string, in Instance) {
	for _, fam := range []string{"-4", "-6"} {
		// One rule per start; a few left over from crashes at most.
		for i := 0; i < 10 && m.run(ip, fam, "rule", "del", "oif", in.Iface, "lookup", strconv.Itoa(in.Table)) == nil; i++ {
		}
	}
}

var ifaceExists = func(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

// NodeStatus is the state of one core node.
type NodeStatus struct {
	Name      string `json:"name"`
	Core      Kind   `json:"core"`
	Port      int    `json:"port,omitempty"`
	Iface     string `json:"iface,omitempty"`
	Running   bool   `json:"running"`
	Restarts  int    `json:"restarts,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// Status lists the planned nodes, the processes serving them and the nodes
// left out with the reason.
func (m *Manager) Status() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodes := []NodeStatus{}
	skipped := map[string]string{}
	if m.last != nil {
		for _, in := range m.last.Instances {
			key := in.Iface
			switch in.Kind {
			case Xray:
				key = "xray"
			case OpenFlux, OlcRTC:
				key = fmt.Sprintf("%s-%d", in.Kind, in.Port)
			}
			st := NodeStatus{Name: in.Name, Core: in.Kind, Port: in.Port, Iface: in.Iface}
			if r, ok := m.procs[key]; ok {
				ps := r.sup.Status()
				st.Running, st.Restarts, st.LastError = ps.Running, ps.Restarts, ps.LastError
			}
			nodes = append(nodes, st)
		}
		skipped = m.last.Skipped
	}
	return map[string]interface{}{"nodes": nodes, "skipped": skipped}
}

func (m *Manager) log(level, format string, args ...interface{}) {
	if m.opts.OnLog != nil {
		m.opts.OnLog(level, "[cores] "+fmt.Sprintf(format, args...))
	}
}
