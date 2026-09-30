// Package version holds build information.
//
// Version is the single source of truth for the application version:
// Makefile, scripts/build.sh and the Dockerfiles read it from this file.
// Release builds may override the values with -ldflags "-X ...".
package version

import "strings"

var (
	Version   = "1.2.3"
	Commit    = "dev"
	Date      = "unknown"
	BuiltBy   = "unknown"
	GoVersion = "unknown"
)

// FullVersion returns the version followed by the commit in parentheses.
func FullVersion() string {
	return Version + " (" + Commit + ")"
}

// UserAgent returns the HTTP User-Agent used for outgoing requests.
func UserAgent() string {
	return "HydraVPNRouter/" + Version
}

// IsDebug reports whether this is a debug pre-release build (vX.Y.Z-debug.N).
func IsDebug() bool {
	return strings.Contains(Version, "-debug")
}
