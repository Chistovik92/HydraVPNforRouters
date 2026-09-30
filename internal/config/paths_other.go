//go:build !windows

package config

// platformDirs returns the config, runtime and cache directories.
func platformDirs() (configDir, runtimeDir, cacheDir string) {
	return "/etc/hydravpn-router", "/var/run/hydravpn-router", "/tmp/hydravpn-router"
}
