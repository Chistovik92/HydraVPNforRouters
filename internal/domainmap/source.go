package domainmap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/lists"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// Source is what a run starts from: domains to resolve and subnets that
// are routed as they are.
type Source struct {
	Domains  []string
	Prefixes []netip.Prefix
	// Skipped counts wildcard / regexp / keyword entries: they cannot be
	// turned into addresses.
	Skipped int
}

// Add merges parsed list entries into the source.
func (s *Source) Add(e lists.Entries) {
	s.Domains = append(s.Domains, e.Suffix...)
	s.Domains = append(s.Domains, e.Exact...)
	s.Skipped += len(e.Keyword) + len(e.Regex)
	for _, c := range e.CIDR {
		if p, err := netip.ParsePrefix(c); err == nil {
			s.Prefixes = append(s.Prefixes, p)
		}
	}
}

// Load reads a list from a file path or an http(s) URL and adds it.
func (s *Source) Load(ctx context.Context, ref string) error {
	body, err := ReadRef(ctx, ref)
	if err != nil {
		return err
	}
	s.Add(lists.ParseEntries(body))
	return nil
}

// ReadRef returns the text behind a file path or an http(s) URL.
func ReadRef(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		b, err := os.ReadFile(ref)
		return string(b), err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch list: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	return string(b), err
}

// Run is the whole pipeline: resolve, optionally drop Cloudflare, aggregate.
type Run struct {
	Resolve        Options
	Aggregation    Aggregation
	DropCloudflare bool
}

// Do resolves the source and returns the route prefixes and the domains
// that could not be resolved.
func (r Run) Do(ctx context.Context, src Source) (prefixes []netip.Prefix, failed []string) {
	res := Resolve(ctx, src.Domains, r.Resolve)
	addrs := res.Addrs
	if r.DropCloudflare {
		addrs = DropCloudflare(addrs)
	}
	extra := src.Prefixes
	if r.DropCloudflare {
		kept := extra[:0:0]
		for _, p := range extra {
			if !IsCloudflare(p.Addr()) {
				kept = append(kept, p)
			}
		}
		extra = kept
	}
	return Aggregate(addrs, r.Aggregation, extra...), res.Failed
}
