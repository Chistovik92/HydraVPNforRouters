package logx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelFilterAndRing(t *testing.T) {
	var out bytes.Buffer
	l, _ := New(Options{Level: "warn", Out: &out, BufferSz: 3})
	l.Log("info", "hidden")
	for i := 0; i < 5; i++ {
		l.Log("warn", fmt.Sprintf("w%d", i))
	}
	if strings.Contains(out.String(), "hidden") {
		t.Error("info line passed the warn threshold")
	}
	got := l.Recent(0, "")
	if len(got) != 3 || got[0].Message != "w2" || got[2].Message != "w4" {
		t.Errorf("ring: %+v", got)
	}
	if e := l.Recent(1, "error"); len(e) != 0 {
		t.Errorf("level filter: %+v", e)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := New(Options{Level: "info", Out: &bytes.Buffer{}, File: path, MaxSize: 200, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		l.Log("info", "0123456789 0123456789 0123456789")
	}
	l.Close()
	for _, n := range []string{"app.log", "app.log.1", "app.log.2"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s missing: %v", n, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app.log.3")); err == nil {
		t.Error("more rotated files than Keep")
	}
}

func TestSubscribe(t *testing.T) {
	l, _ := New(Options{Out: &bytes.Buffer{}})
	ch, cancel := l.Subscribe()
	defer cancel()
	l.Log("info", "hello")
	if e := <-ch; e.Message != "hello" {
		t.Errorf("got %+v", e)
	}
}
