package adapter

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func VersionInfo() map[string]string {
	return map[string]string{
		"version":    version,
		"commit":     commit,
		"build_time": buildTime,
	}
}
