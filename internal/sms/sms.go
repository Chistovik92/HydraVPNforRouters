// Package sms reads SMS from the router's modem (built in or USB) so the
// service can forward them to Telegram, as sms2gram does on Keenetic
// (github.com/spatiumstas/sms2gram):
//
//   - KeeneticOS: the firmware runs /opt/etc/ndm/sms.d/* for every new SMS
//     with interface_id and message_id; our hook drops them into a spool
//     directory and the text is read with "ndmc -c sms <iface> list id <id>".
//   - OpenWrt and other Linux: ModemManager (mmcli).
//   - USB modems with smstools3: files in the incoming spool.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/keenetic"
)

// Message is one received SMS.
type Message struct {
	ID     string `json:"id"`
	Modem  string `json:"modem,omitempty"`
	From   string `json:"from"`
	Time   string `json:"time,omitempty"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

// Source is a place SMS come from.
type Source interface {
	Name() string
	// Poll returns the messages not marked Done yet.
	Poll(ctx context.Context) ([]Message, error)
	// Done marks a message as handled (delivered, or not worth retrying):
	// it is not returned again, also after a restart of the service.
	Done(m Message)
	// Delete removes a forwarded message from the modem.
	Delete(ctx context.Context, m Message) error
	// Send sends an SMS through the modem.
	Send(ctx context.Context, to, text string) error
}

// ErrNoModem means no SMS source was found on this router.
var ErrNoModem = errors.New("no modem with SMS support was found")

var phoneRe = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

// ValidNumber reports whether to is a phone number SMS can be sent to.
func ValidNumber(to string) bool { return phoneRe.MatchString(to) }

// Detect returns the sources available on this router.
func Detect(spoolDir string) []Source {
	var out []Source
	if keenetic.Available() {
		out = append(out, &Keenetic{Spool: spoolDir, Ndmc: keenetic.Ndmc{}})
	}
	if _, err := exec.LookPath("mmcli"); err == nil {
		out = append(out, &ModemManager{seen: newSeen(filepath.Join(spoolDir, ".seen-modemmanager"))})
	}
	for _, dir := range []string{"/opt/var/spool/sms", "/var/spool/sms"} {
		if st, err := os.Stat(filepath.Join(dir, "incoming")); err == nil && st.IsDir() {
			out = append(out, &SMSTools{Dir: dir, seen: newSeen(filepath.Join(spoolDir, ".seen-smstools3"))})
			break
		}
	}
	return out
}

// ---------------------------------------------------------------- Keenetic

// Keenetic reads SMS announced by the ndm/sms.d hook.
type Keenetic struct {
	Spool string
	Ndmc  keenetic.Runner
	// RCI is the local KeeneticOS API (default http://127.0.0.1:79/rci/).
	RCI string

	mu        sync.Mutex
	lastModem string         // the modem interface that delivered the last SMS
	failures  map[string]int // spool entry -> failed reads
}

// maxReadFailures: a spool entry that cannot be read this many times (the
// SMS is gone from the modem) is dropped instead of being retried forever.
const maxReadFailures = 3

func (k *Keenetic) Name() string { return "keenetic" }

// Poll reads the spool entries "<iface> <id>" the hook wrote; an entry
// stays until Done, so an SMS that did not reach Telegram is retried.
func (k *Keenetic) Poll(ctx context.Context) ([]Message, error) {
	entries, err := os.ReadDir(k.Spool)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Message
	var firstErr error
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // seen files of other sources
		}
		path := filepath.Join(k.Spool, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		iface, id, ok := strings.Cut(strings.TrimSpace(string(data)), " ")
		if !ok || !safeToken(iface) || !safeToken(id) {
			os.Remove(path)
			continue
		}
		raw, err := k.Ndmc.Run(ctx, "sms "+iface+" list id "+id)
		if err != nil {
			// One bad entry must not hold up the others.
			k.mu.Lock()
			if k.failures == nil {
				k.failures = map[string]int{}
			}
			k.failures[path]++
			if k.failures[path] >= maxReadFailures {
				os.Remove(path)
				delete(k.failures, path)
			}
			k.mu.Unlock()
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		m := ParseNdmc(raw)
		m.ID, m.Modem, m.Source = id, iface, k.Name()
		out = append(out, m)
		k.mu.Lock()
		k.lastModem = iface
		k.mu.Unlock()
	}
	return out, firstErr
}

// Done drops the spool entry.
func (k *Keenetic) Done(m Message) {
	if safeToken(m.Modem) && safeToken(m.ID) {
		os.Remove(filepath.Join(k.Spool, m.Modem+"-"+m.ID))
	}
}

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// safeToken guards the values that go into an ndmc command line.
func safeToken(s string) bool { return tokenRe.MatchString(s) }

// ParseNdmc reads the "sms <iface> list id <id>" output: from:, timestamp:
// and text: (continued on indented lines). Once the text has started, a
// line such as "from: bank" inside it stays text: it cannot replace the
// sender.
func ParseNdmc(out string) Message {
	var m Message
	inText := false
	var text []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		key, val, _ := strings.Cut(trimmed, ":")
		if inText && trimmed != "" && !ndmcKeys[strings.ToLower(key)] {
			text = append(text, trimmed)
			continue
		}
		if inText && (strings.ToLower(key) == "from" || strings.ToLower(key) == "timestamp") && m.From != "" {
			text = append(text, trimmed)
			continue
		}
		switch strings.ToLower(key) {
		case "from":
			m.From, inText = strings.TrimSpace(val), false
			continue
		case "timestamp":
			m.Time, inText = strings.TrimSpace(val), false
			continue
		case "text":
			text, inText = []string{strings.TrimSpace(val)}, true
			continue
		}
		if inText && trimmed != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			text = append(text, trimmed)
		}
	}
	m.Text = strings.Join(text, " ")
	return m
}

func (k *Keenetic) rci(ctx context.Context, body interface{}) error {
	base := k.RCI
	if base == "" {
		base = "http://127.0.0.1:79/rci/"
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.Contains(strings.ToLower(buf.String()), `"error"`) {
		return fmt.Errorf("rci: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	return nil
}

func (k *Keenetic) Delete(ctx context.Context, m Message) error {
	return k.rci(ctx, []interface{}{map[string]interface{}{"sms": map[string]interface{}{
		"delete": []map[string]string{{"interface": m.Modem, "id": m.ID}}}}})
}

// Send goes through the modem that delivered the last SMS.
func (k *Keenetic) Send(ctx context.Context, to, text string) error {
	k.mu.Lock()
	iface := k.lastModem
	k.mu.Unlock()
	if iface == "" {
		return errors.New("keenetic: no SMS has arrived yet, the modem interface is unknown")
	}
	return k.rci(ctx, []interface{}{map[string]interface{}{"sms": map[string]interface{}{
		"send": map[string]string{"interface": iface, "to": to, "message": text}}}})
}

// ndmcKeys are the fields of an "sms list" entry.
var ndmcKeys = map[string]bool{"id": true, "from": true, "timestamp": true, "text": true}

// ---------------------------------------------------------------- seen

// seen remembers the ids of handled messages in a file in the runtime dir
// (kept across restarts of the service, not across reboots), so a restart
// does not forward all messages kept on the modem again.
type seen struct {
	path string
	ids  map[string]bool
}

const maxSeen = 500

func newSeen(path string) *seen {
	s := &seen{path: path, ids: map[string]bool{}}
	if path == "" {
		return s
	}
	if data, err := os.ReadFile(path); err == nil {
		for _, id := range strings.Fields(string(data)) {
			s.ids[id] = true
		}
	}
	return s
}

func (s *seen) has(id string) bool { return s != nil && s.ids[id] }

func (s *seen) add(id string) {
	if s == nil || s.ids[id] {
		return
	}
	if len(s.ids) >= maxSeen {
		s.ids = map[string]bool{} // ids of messages long gone: start over
	}
	s.ids[id] = true
	if s.path == "" {
		return
	}
	ids := make([]string, 0, len(s.ids))
	for k := range s.ids {
		ids = append(ids, k)
	}
	os.MkdirAll(filepath.Dir(s.path), 0700)
	os.WriteFile(s.path, []byte(strings.Join(ids, " ")+" "), 0600)
}

// ---------------------------------------------------------------- ModemManager

// ModemManager reads SMS with mmcli.
type ModemManager struct {
	seen *seen
	// run is replaced in tests.
	run func(ctx context.Context, args ...string) (string, error)
}

func (m *ModemManager) Name() string { return "modemmanager" }

func (m *ModemManager) mmcli(ctx context.Context, args ...string) (string, error) {
	if m.run != nil {
		return m.run(ctx, args...)
	}
	out, err := exec.CommandContext(ctx, "mmcli", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("mmcli %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

var smsPathRe = regexp.MustCompile(`/SMS/(\d+)\s*\((\w+)\)`)

func (m *ModemManager) Poll(ctx context.Context) ([]Message, error) {
	if m.seen == nil {
		m.seen = newSeen("")
	}
	list, err := m.mmcli(ctx, "-m", "any", "--messaging-list-sms")
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, match := range smsPathRe.FindAllStringSubmatch(list, -1) {
		id, state := match[1], match[2]
		if state != "received" || m.seen.has(id) {
			continue
		}
		raw, err := m.mmcli(ctx, "-K", "-s", id)
		if err != nil {
			return out, err
		}
		msg := ParseMmcli(raw)
		msg.ID, msg.Source = id, m.Name()
		out = append(out, msg)
	}
	return out, nil
}

// ParseMmcli reads "mmcli -K -s N" (key : value lines).
func ParseMmcli(out string) Message {
	var m Message
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if val == "--" {
			val = ""
		}
		switch strings.TrimSpace(key) {
		case "sms.content.number":
			m.From = val
		case "sms.content.text":
			m.Text = val
		case "sms.properties.timestamp":
			m.Time = val
		}
	}
	return m
}

// Done marks the message handled.
func (m *ModemManager) Done(msg Message) { m.seen.add(msg.ID) }

func (m *ModemManager) Delete(ctx context.Context, msg Message) error {
	_, err := m.mmcli(ctx, "-m", "any", "--messaging-delete-sms="+msg.ID)
	return err
}

var createdRe = regexp.MustCompile(`/SMS/(\d+)`)

func (m *ModemManager) Send(ctx context.Context, to, text string) error {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }
	out, err := m.mmcli(ctx, "-m", "any", "--messaging-create-sms=text="+quote(text)+",number="+quote(to))
	if err != nil {
		return err
	}
	id := createdRe.FindStringSubmatch(out)
	if id == nil {
		return fmt.Errorf("mmcli did not create the SMS: %s", strings.TrimSpace(out))
	}
	_, err = m.mmcli(ctx, "-s", id[1], "--send")
	return err
}

// ---------------------------------------------------------------- smstools3

// SMSTools reads the smstools3 spool (incoming/, outgoing/).
type SMSTools struct {
	Dir  string
	seen *seen // files already handled (kept when not deleted)
}

func (s *SMSTools) Name() string { return "smstools3" }

func (s *SMSTools) Poll(ctx context.Context) ([]Message, error) {
	in := filepath.Join(s.Dir, "incoming")
	entries, err := os.ReadDir(in)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if s.seen == nil {
		s.seen = newSeen("")
	}
	var out []Message
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || s.seen.has(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(in, e.Name()))
		if err != nil {
			continue
		}
		m := ParseSMSTools(string(data))
		m.ID, m.Source = e.Name(), s.Name()
		out = append(out, m)
	}
	return out, nil
}

// ParseSMSTools reads a smstools3 message file: headers, a blank line, text.
func ParseSMSTools(data string) Message {
	var m Message
	head, body, _ := strings.Cut(strings.ReplaceAll(data, "\r\n", "\n"), "\n\n")
	for _, line := range strings.Split(head, "\n") {
		key, val, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(key) {
		case "From":
			m.From = strings.TrimSpace(val)
		case "Received":
			m.Time = strings.TrimSpace(val)
		}
	}
	m.Text = strings.TrimSpace(body)
	return m
}

// Done marks the file handled.
func (s *SMSTools) Done(m Message) { s.seen.add(m.ID) }

// Delete removes the file (smstools3 messages are files: forwarded = done).
func (s *SMSTools) Delete(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.ID, `/\`) || m.ID == ".." {
		return fmt.Errorf("bad message id %q", m.ID)
	}
	return os.Remove(filepath.Join(s.Dir, "incoming", m.ID))
}

func (s *SMSTools) Send(ctx context.Context, to, text string) error {
	name := fmt.Sprintf("hydravpn-%d", time.Now().UnixNano())
	return os.WriteFile(filepath.Join(s.Dir, "outgoing", name), []byte("To: "+strings.TrimPrefix(to, "+")+"\n\n"+text+"\n"), 0644)
}
