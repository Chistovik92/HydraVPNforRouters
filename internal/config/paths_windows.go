//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// platformDirs returns the config, runtime and cache directories. Unix paths
// such as /tmp do not exist on Windows (sing-box failed with "cannot find the
// path"), so ProgramData and the temp directory are used.
func platformDirs() (configDir, runtimeDir, cacheDir string) {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	tmp := filepath.Join(os.TempDir(), "hydravpn-router")
	return filepath.Join(base, "hydravpn-router"), tmp, tmp
}
