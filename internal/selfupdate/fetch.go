package selfupdate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// Shared by the self-updater, the core installer and the component version
// check: one HTTP client setup and one verified download.

// NewHTTPClient returns a client that trusts the Entware CA bundle as well
// (KeeneticOS has no system store) and uses proxy when set.
func NewHTTPClient(proxy string, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 30 * time.Second,
		TLSClientConfig:     &tls.Config{RootCAs: RootCAs()},
	}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// Fetch downloads src to dst (mode 0755), refusing files over maxSize, a
// full disk and a SHA-256 other than wantSHA (hex). progress, if set,
// receives the bytes so far and the size (-1 when unknown).
func Fetch(ctx context.Context, client *http.Client, src, dst, wantSHA string, maxSize int64, progress func(done, total int64)) error {
	wantSHA = strings.ToLower(strings.TrimPrefix(wantSHA, "sha256:"))
	if len(wantSHA) != 64 {
		return errors.New("download: no checksum to verify against")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", filepath.Base(src), resp.StatusCode)
	}
	if resp.ContentLength > maxSize {
		return fmt.Errorf("download: file is too large (%d bytes)", resp.ContentLength)
	}
	if resp.ContentLength > 0 {
		if free, err := freeSpace(filepath.Dir(dst)); err == nil && free < uint64(resp.ContentLength)+freeSpaceSlack {
			return fmt.Errorf("not enough space in %s: %d KB free, %d KB needed", filepath.Dir(dst), free>>10, (resp.ContentLength+freeSpaceSlack)>>10)
		}
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, report: progress}
	_, copyErr := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, maxSize+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		return fmt.Errorf("download: %w", copyErr)
	case closeErr != nil:
		return closeErr
	case pw.n > maxSize:
		return errors.New("download: file is too large")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, wantSHA)
	}
	return os.Chmod(dst, 0755)
}

type progressWriter struct {
	n, total int64
	report   func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if p.report != nil {
		p.report(p.n, p.total)
	}
	return len(b), nil
}

// FreeSpace returns the bytes available to the service in dir.
func FreeSpace(dir string) (uint64, error) { return freeSpace(dir) }

// GOARM returns the GOARM the binary was built with ("7" by default).
func GOARM() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" && s.Value != "" {
				return s.Value[:1]
			}
		}
	}
	return "7"
}
