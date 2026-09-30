package version

import "testing"

func TestIsDebug(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	for v, want := range map[string]bool{"1.2.2": false, "1.2.2-debug.1": true, "1.2.2-rc.1": false} {
		Version = v
		if IsDebug() != want {
			t.Errorf("IsDebug(%q) = %v, want %v", v, !want, want)
		}
	}
}
