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
