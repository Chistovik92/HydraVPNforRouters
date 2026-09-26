package version

var (
	Version   = "1.0.0"
	Commit    = "dev"
	Date      = "unknown"
	BuiltBy   = "unknown"
	GoVersion = "unknown"
)

func FullVersion() string {
	return Version + "-" + Commit
}

func UserAgent() string {
	return "PodkopPlus/" + Version
}