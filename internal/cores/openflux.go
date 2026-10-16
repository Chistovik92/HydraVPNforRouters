package cores

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// OpenFlux (github.com/p1neappleXpress/OpenFlux). The official link is
// openflux://v1/<base64url(raw DEFLATE(JSON))> (upstream "share" package);
// the app's older openflux://<transport>?url=…&key=…#name is read too.

const (
	openFluxPrefix     = "openflux://v1/"
	openFluxMinSecret  = 16
	openFluxMaxPayload = 16 << 10
)

var openFluxKnown = map[string]bool{"yandex": true, "vyandex": true, "boards": true, "mailru": true,
	"cupsonline": true, "avito": true, "direct": true, "telemost": true}

var openFluxLegacy = map[string]bool{"yandex": true, "vyandex": true, "oneme": true, "cupsonline": true,
	"avito": true, "mailru": true, "telemost": true, "direct": true}

// OpenFluxTransport is one transport of a session.
type OpenFluxTransport struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Dial     string `json:"dial,omitempty"`
}

// OpenFluxConfig is the decoded link.
type OpenFluxConfig struct {
	Name       string              `json:"name,omitempty"`
	Negotiate  bool                `json:"negotiate,omitempty"`
	Codec      string              `json:"codec,omitempty"`
	Secret     string              `json:"secret,omitempty"`
	Context    string              `json:"context,omitempty"`
	Mode       string              `json:"mode,omitempty"`
	Transports []OpenFluxTransport `json:"transports"`
	// Legacy links only.
	MaxToken, MaxUID string `json:"-"`
}

// ParseOpenFlux decodes an official or legacy OpenFlux link.
func ParseOpenFlux(link string) (OpenFluxConfig, error) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(strings.ToLower(link), "openflux://") {
		return OpenFluxConfig{}, errors.New("not an openflux link")
	}
	if strings.HasPrefix(strings.ToLower(link), openFluxPrefix) {
		return decodeOpenFluxV1(link)
	}
	return parseOpenFluxLegacy(link)
}

func decodeOpenFluxV1(link string) (OpenFluxConfig, error) {
	var c OpenFluxConfig
	if !strings.HasPrefix(link, openFluxPrefix) {
		return c, errors.New("openflux: case_changed")
	}
	packed, err := decodeB64(link[len(openFluxPrefix):])
	if err != nil {
		return c, errors.New("openflux: damaged")
	}
	data, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(packed)), openFluxMaxPayload+1))
	if err != nil {
		return c, errors.New("openflux: damaged")
	}
	if len(data) > openFluxMaxPayload {
		return c, errors.New("openflux: too_large")
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, errors.New("openflux: bad_payload")
	}
	return c, c.validate()
}

// validate mirrors share.Validate of the core (codes as there).
func (c OpenFluxConfig) validate() error {
	fail := func(code string) error { return errors.New("openflux: " + code) }
	switch {
	case len(c.Transports) == 0:
		return fail("no_transports")
	case c.Mode != "":
		// Serverless "stream" mode has no client part to run here.
		return fail("stream_unsupported")
	case len(c.Transports) > 1 && !c.Negotiate:
		return fail("several_need_session")
	case c.Negotiate && len([]rune(c.Secret)) < openFluxMinSecret:
		return fail("session_secret")
	case c.Secret != "" && len([]rune(c.Secret)) < openFluxMinSecret:
		return fail("short_secret")
	case c.Codec != "" && c.Codec != "batched" && c.Codec != "legacy":
		return fail("unknown_codec")
	}
	for _, t := range c.Transports {
		if !openFluxKnown[t.Type] {
			return fail("unknown_transport")
		}
		if t.Type == "direct" && (t.Dial == "" || !c.Negotiate) {
			return fail("direct_needs_session")
		}
	}
	return nil
}

func parseOpenFluxLegacy(link string) (OpenFluxConfig, error) {
	body := link[len("openflux://"):]
	main, frag, _ := strings.Cut(body, "#")
	name, _ := url.PathUnescape(frag)
	transport, query, _ := strings.Cut(main, "?")
	transport = strings.ToLower(strings.TrimSpace(transport))
	if !openFluxLegacy[transport] {
		return OpenFluxConfig{}, fmt.Errorf("openflux: unknown transport %q", transport)
	}
	q, _ := url.ParseQuery(query)
	c := OpenFluxConfig{Name: strings.TrimSpace(name), Codec: q.Get("codec"), Secret: q.Get("key"),
		MaxToken: q.Get("maxToken"), MaxUID: q.Get("maxUid")}
	u := q.Get("url")
	switch {
	case transport == "direct" && (u == "" || c.Secret == ""):
		return c, errors.New("openflux: direct needs url and key")
	case transport == "oneme" && (c.MaxToken == "" || c.MaxUID == ""):
		return c, errors.New("openflux: oneme needs maxToken and maxUid")
	case transport != "oneme" && transport != "cupsonline" && u == "":
		return c, errors.New("openflux: url is required")
	}
	if transport == "direct" {
		c.Negotiate = true
		c.Transports = []OpenFluxTransport{{Type: "direct", Dial: u, Priority: 100}}
	} else {
		c.Transports = []OpenFluxTransport{{Type: transport, URL: u}}
	}
	return c, nil
}

var openFluxURLFlags = map[string]string{"yandex": "--yandex-url", "vyandex": "--vyandex-url",
	"boards": "--boards-url", "mailru": "--mailru-url", "cupsonline": "--cupsonline-url", "avito": "--avito-url"}

// OpenFluxArgs builds the client command line (as the app's OpenFluxArgs).
// keyFile holds the secret; it is used only when the link has one.
func OpenFluxArgs(c OpenFluxConfig, socksPort int, keyFile string) []string {
	args := []string{"--role", "client", "--inbound", "socks5", "--socks5", "127.0.0.1:" + strconv.Itoa(socksPort)}
	ts := c.Transports
	switch {
	case len(ts) == 1 && ts[0].Type == "oneme":
		args = append(args, "--transport", "oneme", "--maxToken", c.MaxToken, "--maxUid", c.MaxUID)
	case len(ts) == 1 && !c.Negotiate && ts[0].Type != "direct":
		args = append(args, "--transport", ts[0].Type)
		if ts[0].URL != "" {
			args = append(args, "--url", ts[0].URL)
		}
	default:
		if c.Negotiate {
			args = append(args, "--negotiate")
		}
		list := make([]string, len(ts))
		for i, t := range ts {
			list[i] = t.Type
			if t.Priority != 0 {
				list[i] += ":" + strconv.Itoa(t.Priority)
			}
		}
		args = append(args, "--transports", strings.Join(list, ","))
		seen := map[string]bool{}
		for _, t := range ts {
			if seen[t.Type] {
				continue // one address flag per type
			}
			seen[t.Type] = true
			if t.Type == "direct" && t.Dial != "" {
				args = append(args, "--direct-dial", t.Dial)
			} else if flag := openFluxURLFlags[t.Type]; flag != "" && t.URL != "" {
				args = append(args, flag, t.URL)
			}
		}
	}
	if c.Context != "" {
		args = append(args, "--session-context", c.Context)
	}
	if c.Codec == "legacy" {
		args = append(args, "--codec", "legacy")
	}
	if c.Secret != "" && keyFile != "" {
		args = append(args, "--encryption-key-file", keyFile)
	}
	return args
}

// encodeOpenFluxV1 makes an official link (tests and round trips).
func encodeOpenFluxV1(c OpenFluxConfig) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	w.Write(data)
	w.Close()
	return openFluxPrefix + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}
