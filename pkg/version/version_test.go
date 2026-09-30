package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The version is duplicated in docs, the web UI and scripts. This test fails
// when they drift apart, which is how a release once shipped with 1.0.1 in
// its source tree.
func TestVersionIsConsistent(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version %q is not x.y.z", Version)
	}

	checks := []struct{ file, pattern string }{
		{"INSTALL.md", `Version\*\*: (\S+)`},
		{"CHANGELOG.md", `(?m)^## (\d+\.\d+\.\d+)`}, // first entry is the latest
		{"scripts/install.sh", `--version (\d+\.\d+\.\d+)`},
	}
	for _, c := range checks {
		m := regexp.MustCompile(c.pattern).FindStringSubmatch(read(c.file))
		if m == nil {
			t.Errorf("%s: no version found (%s)", c.file, c.pattern)
			continue
		}
		if strings.TrimSpace(m[1]) != Version {
			t.Errorf("%s has version %s, pkg/version has %s", c.file, m[1], Version)
		}
	}
}
