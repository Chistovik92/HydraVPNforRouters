package version

var (
	Version   = "1.0.1"
	Commit    = "dev"
	Date      = "unknown"
	BuiltBy   = "unknown"
	GoVersion = "unknown"
)

func FullVersion() string {
	return Version + "-" + Commit
}

func UserAgent() string {
	return "HydraVPNRouter/" + Version
}