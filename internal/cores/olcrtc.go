package cores

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// olcRTC (github.com/openlibrecommunity/olcrtc), URI format v1:
//
//	olcrtc://<Provider>?<Transport>[<key=value&…>]@<RoomID>#<EncryptionKey>$<Name>

// OlcRTCNode is a decoded olcRTC link.
type OlcRTCNode struct {
	Provider, Transport, Room, Key, Name string
	Params                               map[string]string
}

// ParseOlcRTC decodes an olcrtc:// link.
func ParseOlcRTC(link string) (OlcRTCNode, error) {
	var n OlcRTCNode
	if !strings.HasPrefix(strings.ToLower(link), "olcrtc://") {
		return n, errors.New("not an olcrtc link")
	}
	body := strings.TrimSpace(link[len("olcrtc://"):])
	main, name, _ := strings.Cut(body, "$")
	if dec, err := url.QueryUnescape(strings.ReplaceAll(strings.TrimSpace(name), "+", "%2B")); err == nil {
		name = dec
	}
	n.Name = strings.TrimSpace(name)

	withoutKey, key := main, ""
	if i := strings.LastIndex(main, "#"); i >= 0 {
		withoutKey, key = main[:i], main[i+1:]
	}
	head, room, _ := strings.Cut(withoutKey, "@")
	n.Key, n.Room = strings.TrimSpace(key), strings.TrimSpace(room)

	provider, transport, _ := strings.Cut(head, "?")
	n.Provider = strings.ToLower(strings.TrimSpace(provider))
	tname, payload, _ := strings.Cut(transport, "<")
	n.Transport = strings.TrimSpace(tname)
	if n.Transport == "" {
		n.Transport = "datachannel"
	}
	n.Params = map[string]string{}
	payload = strings.TrimSuffix(payload, ">")
	for _, kv := range strings.Split(payload, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			n.Params[k] = v
		}
	}
	if n.Provider == "" || n.Room == "" || n.Key == "" {
		return n, errors.New("olcrtc: provider, room and key are required")
	}
	return n, nil
}

// olcRTCParams maps URI parameters to YAML sections (docs/uri.md upstream).
var olcRTCParams = map[string][2]string{
	"vp8-fps": {"vp8", "fps"}, "vp8-batch": {"vp8", "batch_size"},
	"fps": {"sei", "fps"}, "batch": {"sei", "batch_size"}, "frag": {"sei", "fragment_size"}, "ack-ms": {"sei", "ack_timeout_ms"},
	"video-w": {"video", "width"}, "video-h": {"video", "height"}, "video-fps": {"video", "fps"},
	"video-codec": {"video", "codec"}, "video-qr-size": {"video", "qr_size"}, "video-qr-recovery": {"video", "qr_recovery"},
	"video-tile-module": {"video", "tile_module"}, "video-tile-rs": {"video", "tile_rs"},
}

// OlcRTCConfig renders the client YAML (mode: cnc) for `olcrtc <file>`.
func OlcRTCConfig(n OlcRTCNode, socksPort int, dns string) string {
	var b strings.Builder
	line := func(format string, args ...interface{}) { fmt.Fprintf(&b, format+"\n", args...) }
	line("mode: cnc")
	line("auth:\n  provider: %s", yamlQuote(n.Provider))
	line("room:\n  id: %s", yamlQuote(n.Room))
	line("crypto:\n  key: %s", yamlQuote(n.Key))
	line("net:\n  transport: %s\n  dns: %s", yamlQuote(n.Transport), yamlQuote(firstOf(dns, "1.1.1.1:53")))
	line("liveness:\n  interval: 10s\n  timeout: 15s\n  failures: 4")
	line("socks:\n  host: \"127.0.0.1\"\n  port: %d", socksPort)

	sections := map[string][]string{}
	keys := make([]string, 0, len(n.Params))
	for k := range n.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var order []string
	for _, k := range keys {
		m, ok := olcRTCParams[k]
		if !ok {
			continue
		}
		if _, seen := sections[m[0]]; !seen {
			order = append(order, m[0])
		}
		sections[m[0]] = append(sections[m[0]], fmt.Sprintf("  %s: %s", m[1], yamlScalar(n.Params[k])))
	}
	for _, s := range order {
		line("%s:\n%s", s, strings.Join(sections[s], "\n"))
	}
	line("debug: false")
	return b.String()
}

// yamlQuote writes a double-quoted YAML string. Not JSON quoting: "\/" is
// not a YAML escape and a room such as meet.jit.si/Room broke the config.
func yamlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < ' ':
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func yamlScalar(v string) string {
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return v
	}
	return yamlQuote(v)
}
