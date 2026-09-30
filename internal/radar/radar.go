// Package radar connects the router to a user's account in the "Radar" bot
// (bot API for apps, bot 5.9.1; the contract is docs/API_APPS.md in
// github.com/Chistovik92/radar) and fetches the subscriptions issued to them.
//
// The flow: the person opens "VPN" -> "Connect an app" in the bot and gets a
// one-time code; the router exchanges it for a device token, which is kept in
// the runtime directory (mode 0600, like the management API token), and then
// reads the subscriptions. The API is read-only: access is issued and revoked
// in the bot, never here.
package radar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

const (
	// StateFile is the file in the runtime directory that holds the link.
	StateFile = "radar.json"
	// AppName is how the router introduces itself to the bot.
	AppName = "hydravpn-router"

	maxBody = 1 << 20
)

// State is the link to the bot account.
type State struct {
	Server   string    `json:"server"`
	Token    string    `json:"token"`
	Username string    `json:"username,omitempty"`
	Linked   time.Time `json:"linked"`
}

func statePath(dir string) string { return filepath.Join(dir, StateFile) }

// LoadState returns nil, nil when the router is not linked.
func LoadState(dir string) (*State, error) {
	data, err := os.ReadFile(statePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", statePath(dir), err)
	}
	if st.Server == "" || st.Token == "" {
		return nil, nil
	}
	return &st, nil
}

// SaveState writes the state atomically with mode 0600.
func SaveState(dir string, st *State) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, StateFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := os.Chmod(tmp.Name(), 0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), statePath(dir))
}

// ClearState forgets the link. A missing file is not an error.
func ClearState(dir string) error {
	if err := os.Remove(statePath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Subscription is one entry of GET /api/v1/app/subscriptions.
type Subscription struct {
	Panel        string `json:"panel"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	LinkKind     string `json:"link_kind"`
	State        string `json:"state"`
	Enabled      bool   `json:"enabled"`
	Expire       int64  `json:"expire"`
	TrafficLimit int64  `json:"traffic_limit"`
	TrafficUsed  int64  `json:"traffic_used"`
	URL          string `json:"url"`
}

// Importable reports whether the entry is a working subscription link.
// Single keys (Outline) and config files (wg-easy) are not subscriptions.
func (s Subscription) Importable() bool {
	kind := s.LinkKind
	if kind == "" {
		kind = "subscription"
	}
	return s.State == "ok" && s.Enabled && kind == "subscription" && s.URL != ""
}

// Error is an answer of the bot. Code 0 means the bot was not reached.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string {
	if e.Code == 0 {
		return "bot unreachable: " + e.Msg
	}
	return fmt.Sprintf("bot answered %d: %s", e.Code, e.Msg)
}

// IsAuth reports that the token (or code) is not accepted: the device was
// disconnected in the bot, or the code is wrong or expired.
func IsAuth(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == 401
}

// NormalizeServer turns the input into scheme://host[:port]. The token and
// the subscription links travel over this connection, so plain http is
// accepted only inside the local network.
func NormalizeServer(input string) (string, error) {
	s := strings.TrimRight(strings.TrimSpace(input), "/")
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return "", errors.New("bot server address is empty or contains spaces")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("bot server address %q is not valid", input)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("bot server address must be http(s)")
	}
	if u.User != nil {
		return "", errors.New("bot server address must not contain a login or password")
	}
	if u.Scheme == "http" && !isLocalHost(u.Hostname()) {
		return "", errors.New("plain http is accepted only for localhost and private networks; use https")
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLocalHost(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" || strings.HasSuffix(h, ".local") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// Client talks to the bot.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a client with timeouts and without redirects: a redirect
// could carry the token to another address or downgrade https to http.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) do(ctx context.Context, method, server, path, token string, body interface{}, out interface{}) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, server+"/api/v1/app"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return &Error{Msg: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return &Error{Code: resp.StatusCode, Msg: e.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Code: resp.StatusCode, Msg: "unexpected answer of the bot"}
	}
	return nil
}

// Link exchanges a one-time code for a device token.
func (c *Client) Link(ctx context.Context, server, code, device string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	var out struct {
		Token string `json:"token"`
	}
	err := c.do(ctx, http.MethodPost, server, "/link", "",
		map[string]string{"code": digits, "device": device, "app": AppName}, &out)
	if err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", &Error{Msg: "the bot returned no token"}
	}
	return out.Token, nil
}

// Username returns the bot username of the linked person (may be empty).
func (c *Client) Username(ctx context.Context, server, token string) (string, error) {
	var out struct {
		Username string `json:"username"`
	}
	if err := c.do(ctx, http.MethodGet, server, "/me", token, nil, &out); err != nil {
		return "", err
	}
	return out.Username, nil
}

// Subscriptions lists what the bot has issued to the person.
func (c *Client) Subscriptions(ctx context.Context, server, token string) ([]Subscription, error) {
	var out struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := c.do(ctx, http.MethodGet, server, "/subscriptions", token, nil, &out); err != nil {
		return nil, err
	}
	return out.Subscriptions, nil
}

// Logout disconnects this device on the bot side.
func (c *Client) Logout(ctx context.Context, server, token string) error {
	return c.do(ctx, http.MethodDelete, server, "/session", token, nil, nil)
}

// Result of one synchronisation.
type Result struct {
	Added   int    `json:"added"`   // new subscriptions put into the config
	Present int    `json:"present"` // already in the config
	Skipped int    `json:"skipped"` // not usable here: not a subscription link, disabled, panel error
	Section string `json:"section"`
}

// Plan decides which of the bot's subscriptions are new for the config.
// Subscriptions that disappeared from the bot are not removed: the router
// keeps what it has, as the app does.
func Plan(existing []config.SubscriptionURL, section string, subs []Subscription) ([]config.SubscriptionURL, Result) {
	res := Result{Section: section}
	have := map[string]bool{}
	for _, e := range existing {
		have[e.URL] = true
	}
	var add []config.SubscriptionURL
	for _, s := range subs {
		switch {
		case !s.Importable():
			res.Skipped++
		case have[s.URL]:
			res.Present++
		default:
			have[s.URL] = true
			res.Added++
			add = append(add, config.SubscriptionURL{
				Section:                    section,
				URL:                        s.URL,
				AutoUserAgent:              true,
				AutoHWID:                   true,
				SubscriptionUpdateEnabled:  true,
				SubscriptionUpdateInterval: 24 * time.Hour,
			})
		}
	}
	return add, res
}

// PickSection returns the requested section, else "main" if it exists, else
// the first one.
func PickSection(c *config.Config, want string) (string, error) {
	if len(c.Sections) == 0 {
		return "", errors.New("the config has no sections to put subscriptions into")
	}
	if want != "" {
		for _, s := range c.Sections {
			if s.Name == want {
				return want, nil
			}
		}
		return "", fmt.Errorf("unknown section %q", want)
	}
	for _, s := range c.Sections {
		if s.Name == "main" {
			return "main", nil
		}
	}
	return c.Sections[0].Name, nil
}
