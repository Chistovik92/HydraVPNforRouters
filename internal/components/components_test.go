package components

import (
	"context"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

func TestParseAndCompare(t *testing.T) {
	if Parse("sing-box version 1.12.3\n\nEnvironment: go1.24") != "1.12.3" || Parse("v1.13") != "1.13" || Parse("none") != "" {
		t.Error("Parse")
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.12.0", "1.12.0", 0}, {"1.12.3", "1.12.10", -1}, {"1.13", "1.12.9", 1}, {"1.12", "1.12.0", 0}} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckAllFlagsUpdateAndOldVersion(t *testing.T) {
	c := New(config.DefaultConfig(), nil)
	c.installed = func(string) (string, error) { return "1.11.0", nil }
	c.latest = func(context.Context, string, string) (string, error) { return "1.13.1", nil }
	c.CheckAll(context.Background())

	info := c.GetStatus()["sing-box"].(Info)
	if !info.Update || !info.TooOld || info.Latest != "1.13.1" {
		t.Errorf("info: %+v", info)
	}

	c.installed = func(string) (string, error) { return "1.13.1", nil }
	c.CheckAll(context.Background())
	if info := c.GetStatus()["sing-box"].(Info); info.Update || info.TooOld {
		t.Errorf("current version flagged: %+v", info)
	}
}

func TestNewerThanTestedIsFlagged(t *testing.T) {
	c := New(config.DefaultConfig(), nil)
	c.latest = func(context.Context, string, string) (string, error) { return "1.16.0", nil }
	for v, want := range map[string]bool{"1.14.9": false, "1.15.0": true, "2.0.0": true, "1.12.5": false} {
		c.installed = func(string) (string, error) { return v, nil }
		c.CheckAll(context.Background())
		if got := c.GetStatus()["sing-box"].(Info).Untested; got != want {
			t.Errorf("%s untested=%v want %v", v, got, want)
		}
	}
}

func TestPreReleaseVersions(t *testing.T) {
	if got := Parse("sing-box version 1.13.0-beta.5\n"); got != "1.13.0-beta.5" {
		t.Errorf("Parse pre-release = %q", got)
	}
	// A beta is older than its release and than the minimum it precedes.
	if Compare("1.13.0-beta.5", "1.13.0") >= 0 || Compare("1.12.0-rc.1", "1.12.0") >= 0 {
		t.Error("pre-release must sort before its release")
	}
	if majorMinor("1.15.0-alpha.12") != "1.15" {
		t.Error("majorMinor must drop the pre-release suffix")
	}

	c := New(config.DefaultConfig(), nil)
	c.installed = func(string) (string, error) { return "1.13.0-beta.5", nil }
	c.latest = func(context.Context, string, string) (string, error) { return "1.13.0", nil }
	c.CheckAll(context.Background())
	if info := c.GetStatus()["sing-box"].(Info); !info.Update {
		t.Errorf("beta -> release not offered: %+v", info)
	}
}

func TestLatestNewerThanTestedIsMarked(t *testing.T) {
	c := New(config.DefaultConfig(), nil)
	c.installed = func(string) (string, error) { return "1.14.2", nil }
	for latest, want := range map[string]bool{"1.14.3": false, "1.15.0": true} {
		c.latest = func(context.Context, string, string) (string, error) { return latest, nil }
		c.CheckAll(context.Background())
		info := c.GetStatus()["sing-box"].(Info)
		if !info.Update || info.LatestUntested != want {
			t.Errorf("latest %s: %+v, want latest_untested=%v", latest, info, want)
		}
	}
}
