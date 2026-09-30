package lists

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestParse(t *testing.T) {
	d, c := Parse("# comment\nYouTube.com\n.example.org\ndomain:a.example\n*.b.example\n1.2.3.0/24\n8.8.8.8\n2001:db8::/32\nnot a domain\nlocalhost\nyoutube.com\n")
	if len(d) != 4 || d[0] != "youtube.com" || d[1] != "example.org" || d[3] != "b.example" {
		t.Errorf("domains: %v", d)
	}
	if len(c) != 3 || c[1] != "8.8.8.8/32" {
		t.Errorf("cidrs: %v", c)
	}
}

func TestNeedsDownloadAndURLs(t *testing.T) {
	if NeedsDownload("https://x/y.srs") || !NeedsDownload("https://x/y.lst") || NeedsDownload("name") {
		t.Error("NeedsDownload")
	}
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{
		{Name: "a", Enabled: true, CommunityLists: []string{"ru", "srs"}},
		{Name: "off", Enabled: false, CommunityLists: []string{"other"}},
	}
	cfg.CommunityLists = []config.CommunityList{
		{Name: "ru", URL: "https://x/ru.lst", Interval: time.Hour},
		{Name: "srs", URL: "https://x/a.srs"},
		{Name: "other", URL: "https://x/other.lst"},
	}
	got := URLs(cfg)
	if len(got) != 1 || got["https://x/ru.lst"] != time.Hour {
		t.Errorf("URLs: %v", got)
	}
}

func TestFetchServesEntriesAndNotifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("a.example\n10.0.0.0/8\n"))
	}))
	defer srv.Close()
	cfg := config.DefaultConfig()
	u := srv.URL + "/ru.lst"
	cfg.Sections = []config.Section{{Name: "a", Enabled: true, CommunityLists: []string{u}}}
	updated := 0
	m := NewManager(Options{Config: cfg, OnUpdate: func() { updated++ }})
	m.cacheDir = t.TempDir()
	m.mu.Lock()
	m.syncLocked()
	m.mu.Unlock()
	m.refresh(context.Background())
	d, c, ok := m.ListEntries(u)
	if !ok || len(d) != 1 || len(c) != 1 || updated != 1 {
		t.Fatalf("entries %v %v %v updated=%d", d, c, ok, updated)
	}
	// A restart uses the cache.
	m2 := NewManager(Options{Config: cfg})
	m2.cacheDir = m.cacheDir
	m2.mu.Lock()
	m2.syncLocked()
	m2.mu.Unlock()
	if _, _, ok := m2.ListEntries(u); !ok {
		t.Error("cache not loaded")
	}
}
