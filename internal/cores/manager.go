package cores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	signature string
}

type running struct {
	spec spec
	sup  *process.Supervisor
}

// Manager starts, restarts and stops the core processes of a Plan.
type Manager struct {
	mu    sync.Mutex
	opts  Options
	procs map[string]*running
	last  *Plan
	// run executes helper commands (awg, ip); replaced in tests.
	run func(name string, args ...string) error
}

// NewManager creates a Manager.
func NewManager(opts Options) *Manager {
	return &Manager{opts: opts, procs: map[string]*running{}, run: runCommand}
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
	if s.inst != nil {
		inst := *s.inst
		conf := s.files[m.path(inst.Iface+".conf")]
		sup.OnStart = func(int) { m.setupInterface(inst, conf) }
	}
	if err := sup.Start(s.bin, s.args); err != nil {
		return err
	}
	m.procs[s.key] = &running{spec: s, sup: sup}
	return nil
}

func (m *Manager) stopLocked(key string, r *running) {
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
		out = append(out, spec{key: "xray", kind: Xray, names: names(xs), bin: p.Bins[Xray],
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
		out = append(out, spec{key: key, kind: OpenFlux, names: []string{in.Name}, bin: p.Bins[OpenFlux],
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
		out = append(out, spec{key: key, kind: OlcRTC, names: []string{in.Name}, bin: p.Bins[OlcRTC],
			args: []string{cfg}, files: map[string]string{cfg: OlcRTCConfig(n, in.Port, m.opts.DNS)}})
	}
	for _, in := range p.Of(AmneziaWG) {
		c, err := ParseWireGuard(in.Link)
		if err != nil {
			m.log("warn", "amneziawg node %q skipped: %v", in.Name, err)
			continue
		}
		inst := in
		conf := m.path(in.Iface + ".conf")
		out = append(out, spec{key: in.Iface, kind: AmneziaWG, names: []string{in.Name}, bin: p.Bins[AmneziaWG],
			args: []string{"-f", in.Iface}, files: map[string]string{conf: c.SetConf()}, inst: &inst,
			// Addresses and MTU are applied by setupInterface: part of the identity.
		})
		out[len(out)-1].files[conf+".addr"] = strings.Join(c.Addresses(), ",") + ";" + c.MTU()
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
	fmt.Fprintf(h, "%s\x00%s\x00", s.bin, strings.Join(s.args, "\x00"))
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

// setupInterface configures awgN after amneziawg-go created it: keys and
// peer, addresses, MTU, and a routing table used only by sockets bound to it.
func (m *Manager) setupInterface(in Instance, confPath string) {
	deadline := time.Now().Add(ifaceWait)
	for !ifaceExists(in.Iface) {
		if time.Now().After(deadline) {
			m.log("error", "%s: interface did not appear", in.Iface)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	awg, ip := FindTool("awg"), FindTool("ip")
	if awg == "" || ip == "" {
		m.log("error", "%s: the awg and ip tools are needed (install amneziawg-tools and ip-full)", in.Iface)
		return
	}
	addrMTU, _ := os.ReadFile(confPath + ".addr")
	addrs, mtu, _ := strings.Cut(string(addrMTU), ";")
	table := strconv.Itoa(in.Table)
	steps := [][]string{{awg, "setconf", in.Iface, confPath}}
	v6 := false
	for _, a := range splitList(addrs) {
		fam := "-4"
		if strings.Contains(a, ":") {
			fam, v6 = "-6", true
		}
		steps = append(steps, []string{ip, fam, "address", "replace", a, "dev", in.Iface})
	}
	if mtu != "" {
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
		if err := m.run(st[0], st[1:]...); err != nil {
			m.log("error", "%s: %v", in.Iface, err)
			return
		}
	}
	m.log("info", "%s is up (%s)", in.Iface, in.Name)
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
