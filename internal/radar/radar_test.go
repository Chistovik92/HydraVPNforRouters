package radar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestNormalizeServer(t *testing.T) {
	good := map[string]string{
		" radar.example.org/ ":                 "https://radar.example.org",
		"https://radar.example.org:8443/panel": "https://radar.example.org:8443",
		"http://192.168.1.5:8080":              "http://192.168.1.5:8080",
		"http://localhost:8080":                "http://localhost:8080",
		"http://10.0.0.2":                      "http://10.0.0.2",
		"http://bot.local":                     "http://bot.local",
	}
	for in, want := range good {
		got, err := NormalizeServer(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "ftp://radar.example.org", "https://user:pass@radar.example.org",
		"radar example.org", "http://radar.example.org", "http://8.8.8.8",
	} {
		if got, err := NormalizeServer(in); err == nil {
			t.Errorf("%q must be refused, got %q", in, got)
		}
	}
}

func TestStateRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	if st, err := LoadState(dir); st != nil || err != nil {
		t.Fatalf("empty dir: %v %v", st, err)
	}
	if err := SaveState(dir, &State{Server: "https://b", Token: "tok", Username: "ivan"}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(dir)
	if err != nil || st == nil || st.Token != "tok" || st.Username != "ivan" {
		t.Fatalf("load: %+v %v", st, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(dir, StateFile))
		if fi.Mode().Perm() != 0600 {
			t.Errorf("state mode %v, want 0600", fi.Mode().Perm())
		}
	}
	if err := ClearState(dir); err != nil {
		t.Fatal(err)
	}
	if st, _ := LoadState(dir); st != nil {
		t.Error("state survived ClearState")
	}
	if err := ClearState(dir); err != nil {
		t.Errorf("second ClearState: %v", err)
	}
}

func TestPlan(t *testing.T) {
	existing := []config.SubscriptionURL{{Section: "main", URL: "https://have/1"}}
	subs := []Subscription{
		{Title: "a", LinkKind: "subscription", State: "ok", Enabled: true, URL: "https://have/1"},
		{Title: "b", LinkKind: "subscription", State: "ok", Enabled: true, URL: "https://new/2"},
		{Title: "b-dup", LinkKind: "subscription", State: "ok", Enabled: true, URL: "https://new/2"},
		{Title: "c", LinkKind: "key", State: "ok", Enabled: true, URL: "ss://x"},
		{Title: "d", LinkKind: "subscription", State: "panel_error", Enabled: true},
		{Title: "e", LinkKind: "subscription", State: "ok", Enabled: false, URL: "https://off/3"},
	}
	add, res := Plan(existing, "main", subs)
	if res.Added != 1 || res.Present != 2 || res.Skipped != 3 || len(add) != 1 {
		t.Fatalf("result %+v, add %d", res, len(add))
	}
	got := add[0]
	if got.URL != "https://new/2" || got.Section != "main" || !got.SubscriptionUpdateEnabled ||
		!got.AutoHWID || !got.AutoUserAgent || got.SubscriptionUpdateInterval == 0 {
		t.Errorf("new subscription: %+v", got)
	}
}

func TestPickSection(t *testing.T) {
	c := &config.Config{}
	if _, err := PickSection(c, ""); err == nil {
		t.Error("no sections must be an error")
	}
	c.Sections = []config.Section{{Name: "first"}, {Name: "main"}}
	if n, _ := PickSection(c, ""); n != "main" {
		t.Errorf("default %q, want main", n)
	}
	c.Sections = []config.Section{{Name: "first"}, {Name: "x"}}
	if n, _ := PickSection(c, ""); n != "first" {
		t.Errorf("fallback %q, want first", n)
	}
	if n, _ := PickSection(c, "x"); n != "x" {
		t.Errorf("explicit %q", n)
	}
	if _, err := PickSection(c, "nope"); err == nil {
		t.Error("unknown section must be refused")
	}
}

// fakeBot is the Radar bot API for apps, reduced to what the router calls.
func fakeBot(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app/link":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["code"] != "12345678" || b["app"] != AppName {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"код не подошёл или устарел"}`))
				return
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"token":"device-token","device_id":"a1"}`))
		case "GET /api/v1/app/me", "GET /api/v1/app/subscriptions", "DELETE /api/v1/app/session":
			if r.Header.Get("Authorization") != "Bearer device-token" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"токен не подошёл или отключён"}`))
				return
			}
			switch r.URL.Path {
			case "/api/v1/app/me":
				w.Write([]byte(`{"user_id":"42","username":"ivan"}`))
			case "/api/v1/app/subscriptions":
				w.Write([]byte(`{"subscriptions":[{"panel":"1","title":"Main","link_kind":"subscription","state":"ok","enabled":true,"url":"https://sub.example/xyz"}]}`))
			default:
				w.Write([]byte(`{"status":"revoked"}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestClient(t *testing.T) {
	bot := fakeBot(t)
	c := NewClient()
	ctx := context.Background()

	if _, err := c.Link(ctx, bot.URL, "00000000", "r"); !IsAuth(err) {
		t.Fatalf("wrong code: %v", err)
	}
	token, err := c.Link(ctx, bot.URL, "1234 5678", "router")
	if err != nil || token != "device-token" {
		t.Fatalf("link: %q %v", token, err)
	}
	if name, err := c.Username(ctx, bot.URL, token); err != nil || name != "ivan" {
		t.Errorf("username: %q %v", name, err)
	}
	subs, err := c.Subscriptions(ctx, bot.URL, token)
	if err != nil || len(subs) != 1 || !subs[0].Importable() || subs[0].URL != "https://sub.example/xyz" {
		t.Fatalf("subscriptions: %+v %v", subs, err)
	}
	if _, err := c.Subscriptions(ctx, bot.URL, "revoked"); !IsAuth(err) {
		t.Errorf("revoked token: %v", err)
	}
	if err := c.Logout(ctx, bot.URL, token); err != nil {
		t.Errorf("logout: %v", err)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer other.Close()
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer bot.Close()
	if _, err := NewClient().Subscriptions(context.Background(), bot.URL, "tok"); err == nil {
		t.Error("a redirect must be an error")
	}
	if hit {
		t.Error("the redirect was followed: the token would have left for another address")
	}
}

func TestClientUnreachable(t *testing.T) {
	bot := httptest.NewServer(http.NotFoundHandler())
	url := bot.URL
	bot.Close()
	_, err := NewClient().Subscriptions(context.Background(), url, "tok")
	var e *Error
	if err == nil || IsAuth(err) {
		t.Fatalf("unreachable bot: %v", err)
	}
	if ee, ok := err.(*Error); ok {
		e = ee
	}
	if e == nil || e.Code != 0 {
		t.Errorf("want Error with code 0, got %#v", err)
	}
}
