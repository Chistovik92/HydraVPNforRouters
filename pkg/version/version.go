// Package version holds build information.
//
// Version is the single source of truth for the application version:
// Makefile, scripts/build.sh and the Dockerfiles read it from this file.
// Release builds may override the values with -ldflags "-X ...".
package version

var (
	Version   = "1.2.0"
	Commit    = "dev"
	Date      = "unknown"
	BuiltBy   = "unknown"
	GoVersion = "unknown"
)

// FullVersion returns the version with the commit suffix.
func FullVersion() string {
	return Version + "-" + Commit
}

// UserAgent returns the HTTP User-Agent used for outgoing requests.
func UserAgent() string {
	return "HydraVPNRouter/" + Version
}
